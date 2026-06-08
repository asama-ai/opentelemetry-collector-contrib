// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmceventnormalizeprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor"

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/metadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/normalize"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/parse"
)

var processorCapabilities = consumer.Capabilities{MutatesData: true}

type normalizeProcessor struct {
	engine         *normalize.Engine
	inventory      *normalize.InventoryResolver
	vendorRegistry *normalize.VendorRegistry
}

func newNormalizeProcessor(cfg *Config) (*normalizeProcessor, error) {
	engine, err := normalize.NewEngine(cfg.AsamaRegistryPath, cfg.MappingsIndexPath, cfg.MappingsDir)
	if err != nil {
		return nil, fmt.Errorf("load normalization engine: %w", err)
	}
	inventory := normalize.NewInventoryResolver(normalize.InventoryConfig{
		Neo4jEndpoint:      firstNonEmptyStr(cfg.Identity.Neo4j.URL, cfg.Identity.Neo4j.Endpoint),
		Neo4jDatabase:      cfg.Identity.Neo4j.Database,
		Neo4jUsername:      cfg.Identity.Neo4j.Username,
		Neo4jPassword:      cfg.Identity.Neo4j.Password,
		Neo4jQuery:         cfg.Identity.Neo4j.Query,
		Neo4jTimeout:       cfg.Identity.Neo4j.Timeout,
		Neo4jCacheTTL:      cfg.Identity.Neo4j.CacheTTL,
		PrometheusEndpoint: cfg.Identity.Prometheus.Endpoint,
		PrometheusQuery:    cfg.Identity.Prometheus.Query,
		PrometheusTimeout:  cfg.Identity.Prometheus.Timeout,
		PrometheusCacheTTL: cfg.Identity.Prometheus.CacheTTL,
		IndexIPLookup:      cfg.indexIPLookupEnabled(),
		MessageIDFallback:  cfg.messageIDFallbackEnabled(),
	})
	inventory.SetEngine(engine)
	return &normalizeProcessor{
		engine:         engine,
		inventory:      inventory,
		vendorRegistry: normalize.NewVendorRegistry(cfg.VendorRegistriesDir),
	}, nil
}

func (p *normalizeProcessor) ProcessLogs(_ context.Context, ld plog.Logs) (plog.Logs, error) {
	out := plog.NewLogs()

	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		inRL := ld.ResourceLogs().At(i)
		inRes := inRL.Resource().Attributes()

		for j := 0; j < inRL.ScopeLogs().Len(); j++ {
			inSL := inRL.ScopeLogs().At(j)

			for k := 0; k < inSL.LogRecords().Len(); k++ {
				inLR := inSL.LogRecords().At(k)
				raw := attrStr(inLR.Attributes(), "redfish.raw_payload")
				if raw == "" {
					continue
				}

				bmcIP := attrStr(inRes, "bmc.ip")
				envelopeContext := attrStr(inLR.Attributes(), "redfish.context")
				events, err := parse.ParsePayload([]byte(raw), bmcIP, envelopeContext)
				if err != nil {
					appendParseError(out, inRL, inSL, inLR, inRes, err)
					continue
				}
				for _, ev := range events {
					appendParsedEvent(out, inRL, inSL, inRes, inLR, ev, p)
				}
			}
		}
	}

	return out, nil
}

func appendParseError(out plog.Logs, inRL plog.ResourceLogs, inSL plog.ScopeLogs, inLR plog.LogRecord, inRes pcommon.Map, err error) {
	_, _, lr := appendRecordShell(out, inRL, inSL, inLR, inRes)
	lr.Attributes().PutStr("redfish.raw_payload", attrStr(inLR.Attributes(), "redfish.raw_payload"))
	lr.Attributes().PutStr("redfish.parse_status", "error")
	lr.Attributes().PutStr("redfish.parse_error", err.Error())
	lr.Body().SetStr("redfish-parse-error")
}

func appendParsedEvent(out plog.Logs, inRL plog.ResourceLogs, inSL plog.ScopeLogs, inRes pcommon.Map, inLR plog.LogRecord, ev parse.Event, p *normalizeProcessor) {
	rl, _, lr := appendRecordShell(out, inRL, inSL, inLR, inRes)
	resAttrs := rl.Resource().Attributes()
	attrs := lr.Attributes()

	vendor := ev.Vendor
	bmcIP := attrStr(resAttrs, "bmc.ip")
	bmcModel := attrStr(resAttrs, "bmc.model")
	firmware := attrStr(resAttrs, "bmc.firmware_version")
	bundleID := attrStr(resAttrs, "bmc.bundle_id")

	if ev.HPEHostname != "" && bmcIP == "" {
		bmcIP = ev.HPEHostname
		putIfNonEmpty(resAttrs, "bmc.ip", bmcIP)
	}
	if ev.DellServerHostname != "" {
		putIfNonEmpty(resAttrs, "bmc.hostname", ev.DellServerHostname)
	}
	putIfNonEmpty(resAttrs, "bmc.vendor", vendor)

	putIfNonEmpty(attrs, "redfish.event_type", ev.EventType)
	putIfNonEmpty(attrs, "redfish.event_id", ev.EventID)
	putIfNonEmpty(attrs, "redfish.message_id", ev.MessageID)
	putIfNonEmpty(attrs, "redfish.message", ev.Message)
	putIfNonEmpty(attrs, "redfish.severity", ev.Severity)
	putIfNonEmpty(attrs, "redfish.message_severity", ev.MessageSeverity)
	putIfNonEmpty(attrs, "redfish.event_timestamp", ev.EventTimestamp)
	putIfNonEmpty(attrs, "redfish.origin_of_condition", firstNonEmpty(ev.OriginOfCondition, ev.HPEResource))
	putIfNonEmpty(attrs, "redfish.context", ev.Context)
	putIfNonEmpty(attrs, "redfish.oem.hpe.hostname", ev.HPEHostname)
	putIfNonEmpty(attrs, "redfish.oem.dell.server_hostname", ev.DellServerHostname)
	putIfNonEmpty(attrs, "redfish.oem.lenovo.serial", ev.LenovoSerial)
	putIfNonEmpty(attrs, "redfish.oem.lenovo.common_event_id", ev.LenovoCommonEventID)
	putIfNonEmpty(attrs, "redfish.oem.lenovo.serviceable", ev.LenovoServiceable)
	if len(ev.MessageArgs) > 0 {
		args := attrs.PutEmptySlice("redfish.message_args")
		for _, arg := range ev.MessageArgs {
			args.AppendEmpty().SetStr(arg)
		}
	}

	if ts, ok := parse.ParseEventTime(ev.EventTimestamp); ok {
		lr.SetTimestamp(pcommon.NewTimestampFromTime(ts))
	} else {
		lr.SetTimestamp(inLR.Timestamp())
	}

	identity := p.inventory.Resolve(bmcIP, ev.MessageID, vendor, bmcModel, firmware)
	if identity.Vendor != "" {
		vendor = identity.Vendor
	}
	if identity.BMCModel != "" {
		bmcModel = identity.BMCModel
	}
	if identity.FirmwareVersion != "" {
		firmware = identity.FirmwareVersion
	}
	if identity.BundleID != "" {
		bundleID = identity.BundleID
	}
	if identity.Source != "" {
		putIfNonEmpty(resAttrs, "bmc.vendor", vendor)
		putIfNonEmpty(resAttrs, "bmc.model", bmcModel)
		putIfNonEmpty(resAttrs, "bmc.firmware_version", firmware)
		putIfNonEmpty(resAttrs, "bmc.bundle_id", bundleID)
		attrs.PutStr("asama.identity_source", identity.Source)
	}

	normalize.ApplyClickHouseResourceAttrs(
		func(key string) string { return attrStr(resAttrs, key) },
		func(key, value string) { putIfNonEmpty(resAttrs, key, value) },
		ev,
		identity,
	)

	if ev.MessageID == "" {
		attrs.PutStr("asama.mapping_status", "unmapped")
		if ev.Message != "" {
			lr.Body().SetStr(ev.Message)
		} else if ev.EventType != "" {
			lr.Body().SetStr(ev.EventType)
		}
		return
	}

	vendorMessage := ev.Message
	if vendorMessage == "" || vendorMessage == ev.MessageID {
		if bundle, err := p.engine.LookupBundle(vendor, bmcModel, firmware, bundleID); err == nil {
			if resolved := p.vendorRegistry.ResolveMessage(bundle, ev.MessageID, ev.MessageArgs); resolved != "" {
				vendorMessage = resolved
				attrs.PutStr("redfish.message", resolved)
			}
		}
	}

	result := p.engine.Normalize(
		vendor,
		ev.MessageID,
		vendorMessage,
		ev.Severity,
		bmcIP,
		bmcModel,
		firmware,
		bundleID,
		ev.EventTimestamp,
		ev.MessageArgs,
	)

	attrs.PutStr("asama.mapping_status", result.MappingStatus)
	if result.MappingStatus != "mapped" {
		if vendorMessage != "" {
			lr.Body().SetStr(vendorMessage)
		} else if ev.MessageID != "" {
			lr.Body().SetStr(ev.MessageID)
		}
		return
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
	attrs.PutStr("asama.component_type", result.Component)
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

func appendRecordShell(out plog.Logs, inRL plog.ResourceLogs, inSL plog.ScopeLogs, inLR plog.LogRecord, inRes pcommon.Map) (plog.ResourceLogs, plog.ScopeLogs, plog.LogRecord) {
	var rl plog.ResourceLogs
	if out.ResourceLogs().Len() == 0 {
		rl = out.ResourceLogs().AppendEmpty()
		inRes.CopyTo(rl.Resource().Attributes())
	} else {
		rl = out.ResourceLogs().At(out.ResourceLogs().Len() - 1)
		if !resourceAttrsMatch(rl.Resource().Attributes(), inRes) {
			rl = out.ResourceLogs().AppendEmpty()
			inRes.CopyTo(rl.Resource().Attributes())
		}
	}

	var sl plog.ScopeLogs
	if rl.ScopeLogs().Len() == 0 || rl.ScopeLogs().At(rl.ScopeLogs().Len()-1).Scope().Name() != inSL.Scope().Name() {
		sl = rl.ScopeLogs().AppendEmpty()
		inSL.Scope().CopyTo(sl.Scope())
	} else {
		sl = rl.ScopeLogs().At(rl.ScopeLogs().Len() - 1)
	}

	lr := sl.LogRecords().AppendEmpty()
	return rl, sl, lr
}

func resourceAttrsMatch(a, b pcommon.Map) bool {
	if a.Len() != b.Len() {
		return false
	}
	match := true
	a.Range(func(k string, v pcommon.Value) bool {
		other, ok := b.Get(k)
		if !ok || v.AsString() != other.AsString() {
			match = false
			return false
		}
		return true
	})
	return match
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func attrStr(m pcommon.Map, key string) string {
	v, ok := m.Get(key)
	if !ok {
		return ""
	}
	return v.Str()
}

func putIfNonEmpty(attrs pcommon.Map, key, value string) {
	if value != "" {
		attrs.PutStr(key, value)
	}
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

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
