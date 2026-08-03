// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configfilereceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver"

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver/internal/configfile"
)

const receiverType = "configfile"

var receiverTypeVal = component.MustNewType(receiverType)

type configfileReceiver struct {
	cfg      *Config
	logger   *zap.Logger
	consumer consumer.Logs

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newLogsReceiver(
	_ context.Context,
	settings receiver.Settings,
	cfg component.Config,
	next consumer.Logs,
) (receiver.Logs, error) {
	c := cfg.(*Config)
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &configfileReceiver{
		cfg:      c,
		logger:   settings.Logger,
		consumer: next,
	}, nil
}

func (r *configfileReceiver) Start(ctx context.Context, _ component.Host) error {
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.run(ctx)
	}()
	return nil
}

func (r *configfileReceiver) Shutdown(context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
	return nil
}

func (r *configfileReceiver) run(ctx context.Context) {
	poller := configfile.NewPoller(r.cfg.pollerConfig(), r.logger)
	if err := poller.LoadState(); err != nil {
		r.logger.Error("configfile receiver: load state failed", zap.Error(err))
		return
	}

	if r.emit(ctx, poller, true) {
		if err := poller.SaveState(); err != nil {
			r.logger.Error("configfile receiver: save state failed", zap.Error(err))
		}
	}

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.emit(ctx, poller, false) {
				if err := poller.SaveState(); err != nil {
					r.logger.Error("configfile receiver: save state failed", zap.Error(err))
				}
			}
		}
	}
}

// emit returns true when pending state was committed and should be persisted.
func (r *configfileReceiver) emit(ctx context.Context, poller *configfile.Poller, firstRun bool) bool {
	result := poller.Poll(firstRun)
	if len(result.Snapshots) == 0 {
		return false
	}
	ld := configfile.SnapshotsToLogs(result.Snapshots)
	if err := r.consumer.ConsumeLogs(ctx, ld); err != nil {
		r.logger.Warn("configfile receiver: consume logs failed", zap.Error(err))
		return false
	}
	poller.ApplyPending(result.Pending)
	return true
}

var _ receiver.Logs = (*configfileReceiver)(nil)
