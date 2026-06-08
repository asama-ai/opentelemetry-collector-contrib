// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmcfaultsignalprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmcfaultsignalprocessor"

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmcfaultsignalprocessor/internal/catalog"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmcfaultsignalprocessor/internal/metadata"
)

var processorCapabilities = consumer.Capabilities{MutatesData: false}

type ingestPayload struct {
	Action       string `json:"action"`
	Hostname     string `json:"hostname"`
	SensorType   string `json:"sensor_type"`
	SensorNumber string `json:"sensor_number"`
	EventType    string `json:"event_type"`
	Description  string `json:"description"`
	FaultType    string `json:"fault_type,omitempty"`
	SensorName   string `json:"sensor_name,omitempty"`
	EventID      string `json:"event_id,omitempty"`
	Timestamp    string `json:"timestamp,omitempty"`
}

type faultSignalProcessor struct {
	cfg      *Config
	catalog  *catalog.Catalog
	client   *http.Client
	logger   *zap.Logger
}

func newFaultSignalProcessor(cfg *Config, logger *zap.Logger) (*faultSignalProcessor, error) {
	cat, err := catalog.Load(cfg.FaultCatalogPath)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &faultSignalProcessor{
		cfg:     cfg,
		catalog: cat,
		client:  &http.Client{Timeout: timeout},
		logger:  logger,
	}, nil
}

func (p *faultSignalProcessor) ProcessLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	var firstErr error
	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		rl := ld.ResourceLogs().At(i)
		resAttrs := rl.Resource().Attributes()
		bmcIP := firstNonEmpty(attrStr(resAttrs, "bmc.ip"), attrStr(resAttrs, "bmc.hostname"))
		tenant := p.cfg.Tenant
		if tenant == "" && p.cfg.TenantAttribute != "" {
			tenant = attrStr(resAttrs, p.cfg.TenantAttribute)
		}

		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				lr := sl.LogRecords().At(k)
				attrs := lr.Attributes()
				if attrStr(attrs, "asama.mapping_status") != "mapped" {
					continue
				}
				asamaID := attrStr(attrs, "asama.id")
				lifecycle := attrStr(attrs, "asama.lifecycle")
				action, rule, ok := p.catalog.Action(asamaID, lifecycle)
				if !ok {
					continue
				}
				component := attrStr(attrs, "asama.component")
				faultKey := computeFaultKey(bmcIP, asamaID, component)
				hostname := firstNonEmpty(bmcIP, attrStr(resAttrs, "bmc.hostname"))

				payload := ingestPayload{
					Action:       action,
					Hostname:     hostname,
					SensorType:   "bmc",
					SensorNumber: faultKey,
					EventType:    asamaID,
					Description:  attrStr(attrs, "asama.message"),
					FaultType:    rule.FaultType,
					SensorName:   component,
					Timestamp:    attrStr(attrs, "redfish.event_timestamp"),
				}
				if msgID := attrStr(attrs, "redfish.message_id"); msgID != "" {
					payload.EventID = "bmc-" + msgID
				}

				if err := p.postIngest(ctx, tenant, payload); err != nil {
					p.logger.Error("bmc fault ingest failed", zap.Error(err), zap.String("asama_id", asamaID), zap.String("action", action))
					if firstErr == nil {
						firstErr = err
					}
				}
			}
		}
	}
	return ld, firstErr
}

func (p *faultSignalProcessor) postIngest(ctx context.Context, tenant string, payload ingestPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set(p.cfg.TenantHeader, tenant)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("fault ingest HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func computeFaultKey(bmcIP, asamaID, component string) string {
	sum := sha256.Sum256([]byte(bmcIP + "|" + asamaID + "|" + component))
	return hex.EncodeToString(sum[:])
}

func attrStr(m pcommon.Map, key string) string {
	v, ok := m.Get(key)
	if !ok {
		return ""
	}
	return v.Str()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func NewFactory() processor.Factory {
	return processor.NewFactory(
		metadata.Type,
		createDefaultConfig,
		processor.WithLogs(createLogsProcessor, metadata.LogsStability),
	)
}

func createLogsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Logs,
) (processor.Logs, error) {
	oCfg := cfg.(*Config)
	if err := oCfg.Validate(); err != nil {
		return nil, err
	}
	proc, err := newFaultSignalProcessor(oCfg, set.Logger)
	if err != nil {
		return nil, err
	}
	return processorhelper.NewLogs(
		ctx,
		set,
		cfg,
		nextConsumer,
		proc.ProcessLogs,
		processorhelper.WithCapabilities(processorCapabilities),
	)
}
