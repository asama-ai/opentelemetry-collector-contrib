// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHPEStorageAlert(t *testing.T) {
	body := []byte(`{
		"@odata.type": "#Event.v1_2_9.Event",
		"Name": "Events",
		"Context": "asama-event-listener",
		"Events": [{
			"EventId": "d7a02a42-0e6c-7d30-7f6d-07461293c288",
			"EventTimestamp": "2026-05-12T11:07:11Z",
			"EventType": "Alert",
			"MessageId": "iLOEvents.3.14.DrvArrPhysDrvFailed",
			"MessageArgs": ["Port=1I:Box=3:Bay=2", "Slot 0", "4"],
			"Severity": "Critical",
			"OriginOfCondition": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/DiskDrives/2",
			"Oem": {
				"Hpe": {
					"Hostname": "10.25.40.207",
					"Resource": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/DiskDrives/2"
				}
			}
		}]
	}`)

	events, err := ParsePayload(body, "10.25.40.207", "")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "hpe", events[0].Vendor)
	require.Equal(t, "iLOEvents.3.14.DrvArrPhysDrvFailed", events[0].MessageID)
	require.Equal(t, "Critical", events[0].Severity)
	require.Contains(t, events[0].Context, "asama-event-listener")
}

func TestParseEmptyEventSkipped(t *testing.T) {
	body := []byte(`{
		"Context": "asama-event-listener",
		"Events": [{"Oem": {"Hpe": {"Hostname": "10.25.40.207"}}}]
	}`)
	events, err := ParsePayload(body, "10.25.40.207", "")
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestParseDellAlert(t *testing.T) {
	body := []byte(`{
		"Context": "asama-event-listener",
		"Events": [{
			"EventType": "Alert",
			"MessageId": "IDRAC.2.8.SYS336",
			"Message": "An existing hash value is updated.",
			"Severity": "Informational"
		}],
		"Oem": {"Dell": {"ServerHostname": "WIN-K3TVDQPKNMU"}}
	}`)
	events, err := ParsePayload(body, "10.25.40.206", "")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "dell", events[0].Vendor)
	require.Equal(t, "An existing hash value is updated.", events[0].Message)
}

func TestParseLenovoOriginObject(t *testing.T) {
	body := []byte(`{
		"Context": "asama-event-listener",
		"Events": [{
			"MessageId": "EventRegistry.1.0.FQXSPPP4000I",
			"Message": "Attempting to Shut Down server.",
			"MessageSeverity": "OK",
			"OriginOfCondition": {"@odata.id": "/redfish/v1/Systems/1/LogServices/AuditLog"},
			"Oem": {"Lenovo": {"SystemSerialNumber": "J9009026"}}
		}]
	}`)
	events, err := ParsePayload(body, "10.25.40.208", "")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "lenovo", events[0].Vendor)
	require.Equal(t, "/redfish/v1/Systems/1/LogServices/AuditLog", events[0].OriginOfCondition)
}
