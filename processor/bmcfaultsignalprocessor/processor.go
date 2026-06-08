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
	Action    string `json:"action"`
	AsamaID   string `json:"asama_id"`
	Lifecycle string `json:"lifecycle"`
	Severity  string `json:"severity"`
	FaultKey  string `json:"fault_key"`
	Message   string `json:"message"`
	FaultType string `json:"fault_type"`
	BMC       struct {
		Vendor string `json:"vendor"`
		IP     string `json:"ip"`
		Model  string `json:"model"`
	} `json:"bmc"`
	Component struct {
		Location string `json:"location"`
	} `json:"component"`
	SourceEvent struct {
		VendorMessageID string `json:"vendor_message_id"`
		EventTime       string `json:"event_time"`
	} `json:"source_event"`
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
		bmcVendor := attrStr(resAttrs, "bmc.vendor")
		bmcModel := attrStr(resAttrs, "bmc.model")
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

				payload := ingestPayload{
					Action:    action,
					AsamaID:   asamaID,
					Lifecycle: lifecycle,
					Severity:  attrStr(attrs, "asama.severity"),
					FaultKey:  faultKey,
					Message:   attrStr(attrs, "asama.message"),
					FaultType: rule.FaultType,
				}
				payload.BMC.Vendor = bmcVendor
				payload.BMC.IP = bmcIP
				payload.BMC.Model = bmcModel
				payload.Component.Location = component
				payload.SourceEvent.VendorMessageID = attrStr(attrs, "redfish.message_id")
				payload.SourceEvent.EventTime = attrStr(attrs, "redfish.event_timestamp")

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
