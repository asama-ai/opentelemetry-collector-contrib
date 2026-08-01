// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadToLogsRawEnvelope(t *testing.T) {
	body := []byte(`{
		"Context": "asama-event-listener",
		"Events": [{
			"MessageId": "iLOEvents.3.14.0.DriveFailed",
			"Severity": "Critical"
		}]
	}`)

	ld, n, err := payloadToLogs(body, sourceContext{
		IP:     "10.25.40.207",
		Tenant: "nxtgen",
	})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 1, ld.ResourceLogs().Len())

	rl := ld.ResourceLogs().At(0)
	ip, ok := rl.Resource().Attributes().Get("bmc.ip")
	require.True(t, ok)
	require.Equal(t, "10.25.40.207", ip.Str())

	lr := rl.ScopeLogs().At(0).LogRecords().At(0)
	raw, ok := lr.Attributes().Get("redfish.raw_payload")
	require.True(t, ok)
	require.JSONEq(t, string(body), raw.Str())

	_, ok = lr.Attributes().Get("redfish.message_id")
	require.False(t, ok, "receiver must not parse MessageId")

	ctx, ok := lr.Attributes().Get("redfish.context")
	require.True(t, ok)
	require.Contains(t, ctx.Str(), "asama-event-listener")
	require.Contains(t, ctx.Str(), "bmc_ip=10.25.40.207")
}

func TestEnrichContext(t *testing.T) {
	require.Equal(t, "bmc_ip=10.0.0.1", enrichContext("", "10.0.0.1"))
	require.Equal(t, "fleet-a|bmc_ip=10.0.0.1", enrichContext("fleet-a", "10.0.0.1"))
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
