// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

const (
	serviceNameChangelog = "asama-inventory-changelog"
	attrRequestID        = "request_id"
	attrAction           = "action"
	attrHostname         = "hostname"
	attrMetric           = "metric"
	attrPrechangeData    = "prechange_data"
	attrPostchangeData   = "postchange_data"
	attrPreObservedAt    = "pre_observed_at"
	attrPostObservedAt   = "post_observed_at"
)

type inventoryDiffProcessor struct {
	logger *zap.Logger
	cfg    *Config
	state  *stateStore
	sender changelogSender
	now    func() time.Time
	mu     sync.Mutex // serializes compare/update across concurrent ConsumeMetrics
}

func newProcessor(logger *zap.Logger, cfg *Config) *inventoryDiffProcessor {
	return &inventoryDiffProcessor{
		logger: logger,
		cfg:    cfg,
		state:  newStateStore(),
		now:    time.Now,
	}
}

func (p *inventoryDiffProcessor) start(_ context.Context, _ component.Host) error {
	if p.sender == nil {
		p.sender = newHTTPChangelogSender(p.cfg.Changelog)
	}
	return nil
}

func (p *inventoryDiffProcessor) shutdown(context.Context) error {
	return nil
}

// processMetrics compares watched metrics to cached snapshots and emits changelog events.
// Metrics are always forwarded unchanged (fail-open on changelog errors).
// Watched metrics absent from this batch are skipped (partial domain×tier pushes must not
// look like deletes).
func (p *inventoryDiffProcessor) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for _, hostname := range hostnamesInMetrics(md) {
		for _, metricName := range p.cfg.Metrics {
			current, present := snapshotFromMetrics(md, hostname, metricName, now)
			if !present {
				continue
			}
			prev, ok := p.state.Get(hostname, metricName)
			if !ok {
				p.state.Set(hostname, metricName, current)
				continue
			}
			if compareSnapshots(prev, current) {
				continue
			}
			if err := p.emitChangelog(ctx, hostname, metricName, prev, current); err != nil {
				p.logger.Warn("inventorydiff: changelog export failed",
					zap.String("hostname", hostname),
					zap.String("metric", metricName),
					zap.Error(err),
				)
			}
			p.state.Set(hostname, metricName, current)
		}
	}
	return md, nil
}

func (p *inventoryDiffProcessor) emitChangelog(
	ctx context.Context,
	hostname, metric string,
	pre, post MetricSnapshot,
) error {
	if p.sender == nil {
		return nil
	}
	requestID := uuid.NewString()
	action := "update"
	if len(pre.Series) > 0 && len(post.Series) == 0 {
		action = "delete"
	} else if len(pre.Series) == 0 && len(post.Series) > 0 {
		action = "create"
	}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", serviceNameChangelog)
	rl.Resource().Attributes().PutStr("hostname", hostname)
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	ts := pcommon.NewTimestampFromTime(p.now())
	lr.SetTimestamp(ts)
	lr.SetObservedTimestamp(ts)
	lr.SetSeverityNumber(plog.SeverityNumberInfo)
	lr.SetSeverityText("INFO")
	lr.Body().SetStr("metric identity changed")
	attrs := lr.Attributes()
	attrs.PutStr(attrRequestID, requestID)
	attrs.PutStr(attrAction, action)
	attrs.PutStr(attrHostname, hostname)
	attrs.PutStr(attrMetric, metric)
	attrs.PutStr(attrPrechangeData, snapshotToJSON(pre))
	attrs.PutStr(attrPostchangeData, snapshotToJSON(post))
	attrs.PutStr(attrPreObservedAt, pre.ObservedAt)
	attrs.PutStr(attrPostObservedAt, post.ObservedAt)

	return p.sender.Send(ctx, ld)
}
