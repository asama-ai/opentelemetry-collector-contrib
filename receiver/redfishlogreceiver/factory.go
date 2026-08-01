// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfishlogreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver/internal/metadata"
)

// NewFactory returns a factory for the redfishlog receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		func() component.Config { return createDefaultConfig() },
		receiver.WithLogs(createLogsReceiver, metadata.LogsStability),
	)
}

func createLogsReceiver(
	_ context.Context,
	settings receiver.Settings,
	cfg component.Config,
	next consumer.Logs,
) (receiver.Logs, error) {
	return newReceiver(cfg.(*Config), settings, next), nil
}
