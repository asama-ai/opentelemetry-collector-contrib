// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configwatchsnapshot // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/configwatchsnapshot"

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver"
)

func (p *snapshotProcessor) processLogs(_ context.Context, ld plog.Logs) (plog.Logs, error) {
	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		rl := ld.ResourceLogs().At(i)
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			appendSnapshots(rl.ScopeLogs().At(j), p.cfg, p.logger)
		}
	}
	return ld, nil
}

func appendSnapshots(sl plog.ScopeLogs, cfg *Config, logger *zap.Logger) {
	records := sl.LogRecords()
	extra := plog.NewLogRecordSlice()
	for k := 0; k < records.Len(); k++ {
		lr := records.At(k)
		event := attrStr(lr.Attributes(), "config.event")
		if event != configfilereceiver.ActorEventWritten && event != configfilereceiver.ActorEventReplaced {
			continue
		}
		path := attrStr(lr.Attributes(), "config.file")
		if path == "" {
			continue
		}
		snap, err := configfilereceiver.BuildChangedSnapshot(path, cfg.MaxKeysPerFile, cfg.ExcludeKeys)
		if err != nil {
			if logger != nil {
				logger.Warn("configwatch_snapshot: read file", zap.String("path", path), zap.Error(err))
			}
			continue
		}
		rec := extra.AppendEmpty()
		ts := lr.Timestamp()
		if ts == 0 {
			ts = pcommon.NewTimestampFromTime(time.Now())
		}
		rec.SetTimestamp(ts)
		rec.SetObservedTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		rec.SetSeverityNumber(plog.SeverityNumberInfo)
		rec.SetSeverityText("INFO")
		rec.Body().SetStr("configfile changed")
		configfilereceiver.WriteSnapshotLogAttributes(rec.Attributes(), snap)
		copyActorAttrs(lr.Attributes(), rec.Attributes(), event)
	}
	if extra.Len() == 0 {
		return
	}
	for k := 0; k < extra.Len(); k++ {
		extra.At(k).CopyTo(records.AppendEmpty())
	}
}

func attrStr(attrs pcommon.Map, key string) string {
	v, ok := attrs.Get(key)
	if !ok {
		return ""
	}
	return v.Str()
}

func copyActorAttrs(from, to pcommon.Map, trigger string) {
	from.Range(func(k string, v pcommon.Value) bool {
		if strings.HasPrefix(k, "config.actor.") && v.Str() != "" {
			to.PutStr(k, v.Str())
		}
		return true
	})
	if trigger != "" {
		to.PutStr(configfilereceiver.AttrTrigger, trigger)
	}
}
