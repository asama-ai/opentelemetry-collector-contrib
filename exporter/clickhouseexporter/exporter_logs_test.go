// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package clickhouseexporter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRenderCreateLogsTableDedup(t *testing.T) {
	cfg := withDefaultConfig()
	cfg.LogsDedupKeyAttribute = "redfish.entry_fingerprint"
	sql := renderCreateLogsTableSQL(cfg)
	require.Contains(t, sql, "DedupKey")
	require.Contains(t, sql, "ReplacingMergeTree(Timestamp)")
	require.Contains(t, sql, "ORDER BY (DedupKey)")
}

func TestLogsExporter_New(t *testing.T) {
	type validate func(*testing.T, *logsExporter, error)

	failWithMsg := func(msg string) validate {
		return func(t *testing.T, _ *logsExporter, err error) {
			require.ErrorContains(t, err, msg)
		}
	}

	tests := map[string]struct {
		config *Config
		want   validate
	}{
		"no dsn": {
			config: withDefaultConfig(),
			want:   failWithMsg("parse dsn address failed"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var err error
			exporter := newLogsExporter(zap.NewNop(), test.config)

			if exporter != nil {
				err = errors.Join(err, exporter.start(t.Context(), nil))
				defer func() {
					require.NoError(t, exporter.shutdown(t.Context()))
				}()
			}

			test.want(t, exporter, err)
		})
	}
}
