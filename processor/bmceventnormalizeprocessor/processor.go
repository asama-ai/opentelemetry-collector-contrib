// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmceventnormalizeprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor"

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/metadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/normalize"
)

var processorCapabilities = consumer.Capabilities{MutatesData: true}

type normalizeProcessor struct {
	engine *normalize.Engine
}

func newNormalizeProcessor(cfg *Config) (*normalizeProcessor, error) {
	engine, err := normalize.NewEngine(cfg.AsamaRegistryPath, cfg.MappingsIndexPath, cfg.MappingsDir)
	if err != nil {
		return nil, fmt.Errorf("load normalization engine: %w", err)
	}
	return &normalizeProcessor{engine: engine}, nil
}

func (p *normalizeProcessor) ProcessLogs(_ context.Context, ld plog.Logs) (plog.Logs, error) {
	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		rl := ld.ResourceLogs().At(i)
		resAttrs := rl.Resource().Attributes()
		vendor := attrStr(resAttrs, "bmc.vendor")
		bmcIP := attrStr(resAttrs, "bmc.ip")
		bmcModel := attrStr(resAttrs, "bmc.model")
		firmware := attrStr(resAttrs, "bmc.firmware_version")
		bundleID := attrStr(resAttrs, "bmc.bundle_id")

		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				lr := sl.LogRecords().At(k)
				attrs := lr.Attributes()
				messageID := attrStr(attrs, "redfish.message_id")
				if messageID == "" {
					continue
				}

				result := p.engine.Normalize(
					vendor,
					messageID,
					attrStr(attrs, "redfish.message"),
					attrStr(attrs, "redfish.severity"),
					bmcIP,
					bmcModel,
					firmware,
					bundleID,
					attrStr(attrs, "redfish.event_timestamp"),
					attrStringSlice(attrs, "redfish.message_args"),
				)

				attrs.PutStr("asama.mapping_status", result.MappingStatus)
				if result.MappingStatus != "mapped" {
					continue
				}

				attrs.PutStr("asama.message_id", result.AsamaMessageID)
				attrs.PutStr("asama.message_key", result.AsamaMessageKey)
				attrs.PutStr("asama.id", result.AsamaID)
				attrs.PutStr("asama.message", result.Message)
				attrs.PutStr("asama.description", result.Description)
				attrs.PutStr("asama.severity", result.Severity)
				attrs.PutStr("asama.message_severity", result.MessageSeverity)
				attrs.PutStr("asama.lifecycle", result.Lifecycle)
				attrs.PutStr("asama.domain", result.Domain)
				attrs.PutStr("asama.subscription_priority", result.SubscriptionPriority)
				attrs.PutStr("asama.bundle_id", result.BundleID)
				if result.ComponentArgIndex > 0 {
					attrs.PutInt("asama.component_arg_index", int64(result.ComponentArgIndex))
				}
				if len(result.AsamaMessageArgs) > 0 {
					args := attrs.PutEmptySlice("asama.message_args")
					for _, arg := range result.AsamaMessageArgs {
						args.AppendEmpty().SetStr(arg)
					}
				}
				if result.ComponentArgIndex > 0 {
					idx := result.ComponentArgIndex - 1
					if idx >= 0 && idx < len(result.AsamaMessageArgs) {
						attrs.PutStr("asama.component", result.AsamaMessageArgs[idx])
					}
				}
				if result.Message != "" {
					lr.Body().SetStr(result.Message)
				}
			}
		}
	}
	return ld, nil
}

func attrStr(m pcommon.Map, key string) string {
	v, ok := m.Get(key)
	if !ok {
		return ""
	}
	return v.Str()
}

func attrStringSlice(m pcommon.Map, key string) []string {
	v, ok := m.Get(key)
	if !ok || v.Type() != pcommon.ValueTypeSlice {
		return nil
	}
	out := make([]string, 0, v.Slice().Len())
	for i := 0; i < v.Slice().Len(); i++ {
		out = append(out, v.Slice().At(i).Str())
	}
	return out
}

// NewFactory returns a factory for the BMC event normalize processor.
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
	proc, err := newNormalizeProcessor(oCfg)
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
