// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configwatchsnapshot // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/configwatchsnapshot"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"
)

const (
	typeStr   = "configwatch_snapshot"
	stability = component.StabilityLevelDevelopment
)

// NewFactory creates the configwatch_snapshot logs processor factory.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithLogs(createLogsProcessor, stability),
	)
}

func createLogsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	next consumer.Logs,
) (processor.Logs, error) {
	oCfg := cfg.(*Config)
	return processorhelper.NewLogs(
		ctx,
		set,
		cfg,
		next,
		newSnapshotProcessor(set.Logger, oCfg).processLogs,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}

type snapshotProcessor struct {
	logger *zap.Logger
	cfg    *Config
}

func newSnapshotProcessor(logger *zap.Logger, cfg *Config) *snapshotProcessor {
	return &snapshotProcessor{logger: logger, cfg: cfg}
}
