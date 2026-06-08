// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadToLogs(t *testing.T) {
	body := []byte(`{
		"Events": [{
			"EventType": "Alert",
			"MessageId": "iLOEvents.3.14.0.DriveFailed",
			"Message": "Drive failed",
			"MessageArgs": ["Bay 1"],
			"Severity": "Critical",
			"EventTimestamp": "2026-06-08T12:00:00Z"
		}]
	}`)

	ld, n, err := payloadToLogs(body, sourceContext{
		Vendor: "hpe",
		IP:     "10.0.0.1",
		Model:  "ilo6",
	})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 1, ld.ResourceLogs().Len())

	rl := ld.ResourceLogs().At(0)
	vendor, ok := rl.Resource().Attributes().Get("bmc.vendor")
	require.True(t, ok)
	require.Equal(t, "hpe", vendor.Str())

	lr := rl.ScopeLogs().At(0).LogRecords().At(0)
	msgID, ok := lr.Attributes().Get("redfish.message_id")
	require.True(t, ok)
	require.Equal(t, "iLOEvents.3.14.0.DriveFailed", msgID.Str())
}
