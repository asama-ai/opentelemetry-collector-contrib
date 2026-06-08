// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestBuildIngestPayloadFromJSON(t *testing.T) {
	t.Parallel()

	cat, err := Load(filepath.Join("..", "..", "registries", "fault-eligible-events.json"))
	require.NoError(t, err)

	_, rule, ok := cat.Action("storage.drive.failure", "assert")
	require.True(t, ok)

	resAttrs := pcommon.NewMap()
	resAttrs.PutStr("host.name", "nxtegn-test-02")

	logAttrs := pcommon.NewMap()
	logAttrs.PutStr("asama.component", "1I:1:1")
	logAttrs.PutStr("asama.message", "Physical drive at location 1I:1:1 has failed.")
	logAttrs.PutStr("redfish.message_id", "iLOEvents.3.14.0.DriveFailed")
	logAttrs.PutStr("redfish.event_timestamp", "2026-05-12T15:52:39+05:30")

	payload := cat.BuildIngestPayload(resAttrs, logAttrs, rule)
	require.Equal(t, "nxtegn-test-02", payload["hostname"])
	require.Equal(t, "1I:1:1", payload["sensor_name"])
	require.Equal(t, "1I:1:1", payload["sensor_number"])
	require.Equal(t, "Storage", payload["sensor_type"])
	require.Equal(t, "Storage Drive Failure", payload["event_type"])
	require.Equal(t, "hardware", payload["fault_type"])
	require.Equal(t, "Physical drive at location 1I:1:1 has failed.", payload["description"])
	require.Equal(t, "Physical drive at location 1I:1:1 has failed.", payload["event_data"])
	require.Equal(t, "bmc-iLOEvents.3.14.0.DriveFailed", payload["event_id"])
	require.Equal(t, "2026-05-12T15:52:39+05:30", payload["timestamp"])
}
