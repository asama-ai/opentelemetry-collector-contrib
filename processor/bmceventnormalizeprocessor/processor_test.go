// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmceventnormalizeprocessor

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	registries := filepath.Join("registries")
	return &Config{
		AsamaRegistryPath:   filepath.Join(registries, "asama-bmc-events.json"),
		MappingsIndexPath:   filepath.Join(registries, "mappings", "index.json"),
		MappingsDir:         filepath.Join(registries, "mappings"),
		VendorRegistriesDir: filepath.Join("..", "..", "..", "..", "bmc-registries"),
	}
}

func TestProcessLogsFromRawPayload(t *testing.T) {
	proc, err := newNormalizeProcessor(testConfig(t))
	require.NoError(t, err)

	body := `{
		"Context": "asama-event-listener",
		"Events": [{
			"MessageId": "iLOEvents.3.14.0.DriveFailed",
			"MessageArgs": ["1I:1:1"],
			"Severity": "Critical",
			"EventTimestamp": "2026-06-08T12:00:00Z",
			"EventType": "Alert",
			"Oem": {"Hpe": {"Hostname": "10.25.40.207"}}
		}]
	}`

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("bmc.ip", "10.25.40.207")
	rl.Resource().Attributes().PutStr("tenant.id", "nxtgen")
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Attributes().PutStr("redfish.raw_payload", body)

	out, err := proc.ProcessLogs(context.Background(), ld)
	require.NoError(t, err)
	require.Equal(t, 1, out.ResourceLogs().Len())
	require.Equal(t, 1, out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().Len())

	parsed := out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	msgID, ok := parsed.Attributes().Get("redfish.message_id")
	require.True(t, ok)
	require.Equal(t, "iLOEvents.3.14.0.DriveFailed", msgID.Str())

	status, ok := parsed.Attributes().Get("asama.mapping_status")
	require.True(t, ok)
	require.Equal(t, "mapped", status.Str())

	asamaID, ok := parsed.Attributes().Get("asama.id")
	require.True(t, ok)
	require.Equal(t, "storage.drive.failure", asamaID.Str())
	require.NotEmpty(t, parsed.Body().AsString())
}

func TestProcessLogsSkipsWithoutRawPayload(t *testing.T) {
	cfg := testConfig(t)
	cfg.VendorRegistriesDir = ""
	proc, err := newNormalizeProcessor(cfg)
	require.NoError(t, err)

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Attributes().PutStr("redfish.message_id", "legacy-should-not-process")

	out, err := proc.ProcessLogs(context.Background(), ld)
	require.NoError(t, err)
	require.Equal(t, 0, out.ResourceLogs().Len())
}
