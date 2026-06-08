// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver/internal/metadata"
)

// NewFactory returns a factory for the Redfish EventService push receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithLogs(createLogsReceiver, metadata.LogsStability),
	)
}

func createLogsReceiver(
	_ context.Context,
	params receiver.Settings,
	cfg component.Config,
	next consumer.Logs,
) (receiver.Logs, error) {
	return newLogsReceiver(params, cfg.(*Config), next)
}
