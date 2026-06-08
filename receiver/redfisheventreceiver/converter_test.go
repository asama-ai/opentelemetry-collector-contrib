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

	ctx, ok := lr.Attributes().Get("redfish.context")
	require.True(t, ok)
	require.Equal(t, "bmc_ip=10.0.0.1", ctx.Str())
}

func TestEnrichContext(t *testing.T) {
	require.Equal(t, "bmc_ip=10.0.0.1", enrichContext("", "10.0.0.1"))
	require.Equal(t, "fleet-a|bmc_ip=10.0.0.1", enrichContext("fleet-a", "10.0.0.1"))
	require.Equal(t, "fleet-a|bmc_ip=10.0.0.1", enrichContext("fleet-a|bmc_ip=10.0.0.1", "10.0.0.2"))
}

func TestHostFromRemoteAddr(t *testing.T) {
	require.Equal(t, "10.25.40.207", hostFromRemoteAddr("10.25.40.207:443"))
	require.Equal(t, "127.0.0.1", hostFromRemoteAddr("[::1]:8080"))
}

func TestResolveBMCSourceIP(t *testing.T) {
	hpeBody := []byte(`{"Events":[{"Oem":{"Hpe":{"Hostname":"10.25.40.207"}}}]}`)
	require.Equal(t, "10.0.0.9", resolveBMCSourceIP("10.0.0.9", "", "", nil))
	require.Equal(t, "10.0.0.8", resolveBMCSourceIP("", "10.0.0.8", "", nil))
	require.Equal(t, "10.25.40.207", resolveBMCSourceIP("", "", "", hpeBody))
	require.Equal(t, "10.25.40.206", resolveBMCSourceIP("", "", "10.25.40.206:443", nil))
}

func TestPayloadHPENoMessage(t *testing.T) {
	body := []byte(`{
		"Context": "asama-hpe-events",
		"Events": [{
			"MessageId": "iLOEvents.3.14.DrvArrPhysDrvFailed",
			"Severity": "Critical",
			"EventTimestamp": "2026-06-08T12:00:00Z",
			"OriginOfCondition": {"@odata.id": "/redfish/v1/Systems/1/Storage/2/Drives/2"},
			"MessageArgs": ["2"],
			"Oem": {"Hpe": {"Hostname": "10.25.40.207"}}
		}]
	}`)

	ld, n, err := payloadToLogs(body, sourceContext{})
	require.NoError(t, err)
	require.Equal(t, 1, n)

	rl := ld.ResourceLogs().At(0)
	ip, ok := rl.Resource().Attributes().Get("bmc.ip")
	require.True(t, ok)
	require.Equal(t, "10.25.40.207", ip.Str())

	lr := rl.ScopeLogs().At(0).LogRecords().At(0)
	ctx, ok := lr.Attributes().Get("redfish.context")
	require.True(t, ok)
	require.Contains(t, ctx.Str(), "asama-hpe-events")
	require.Contains(t, ctx.Str(), "bmc_ip=10.25.40.207")
}
