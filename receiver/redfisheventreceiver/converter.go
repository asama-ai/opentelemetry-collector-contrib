// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver"

import (
	"encoding/json"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// redfishEvent mirrors the Redfish Event schema subset used for BMC push.
type redfishEvent struct {
	EventType      string   `json:"EventType"`
	EventID        string   `json:"EventId"`
	MessageID      string   `json:"MessageId"`
	Message        string   `json:"Message"`
	MessageArgs    []string `json:"MessageArgs"`
	Severity       string   `json:"Severity"`
	EventTimestamp string   `json:"EventTimestamp"`
	OriginOfCondition struct {
		ODataID string `json:"@odata.id"`
	} `json:"OriginOfCondition"`
	Context string `json:"Context"`
}

type redfishEventPayload struct {
	Events []redfishEvent `json:"Events"`
}

type sourceContext struct {
	Vendor   string
	IP       string
	Model    string
	Firmware string
	Hostname string
	Tenant   string
}

func payloadToLogs(body []byte, src sourceContext) (plog.Logs, int, error) {
	var payload redfishEventPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		var single redfishEvent
		if errSingle := json.Unmarshal(body, &single); errSingle != nil {
			return plog.Logs{}, 0, err
		}
		payload.Events = []redfishEvent{single}
	}

	if len(payload.Events) == 0 {
		return plog.NewLogs(), 0, nil
	}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	resAttrs := rl.Resource().Attributes()
	putIfNonEmpty(resAttrs, "bmc.vendor", src.Vendor)
	putIfNonEmpty(resAttrs, "bmc.ip", src.IP)
	putIfNonEmpty(resAttrs, "bmc.model", src.Model)
	putIfNonEmpty(resAttrs, "bmc.firmware_version", src.Firmware)
	putIfNonEmpty(resAttrs, "bmc.hostname", src.Hostname)
	putIfNonEmpty(resAttrs, "tenant.id", src.Tenant)

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver")

	for _, ev := range payload.Events {
		lr := sl.LogRecords().AppendEmpty()
		attrs := lr.Attributes()
		putIfNonEmpty(attrs, "redfish.event_type", ev.EventType)
		putIfNonEmpty(attrs, "redfish.event_id", ev.EventID)
		putIfNonEmpty(attrs, "redfish.message_id", ev.MessageID)
		putIfNonEmpty(attrs, "redfish.message", ev.Message)
		putIfNonEmpty(attrs, "redfish.severity", ev.Severity)
		putIfNonEmpty(attrs, "redfish.event_timestamp", ev.EventTimestamp)
		putIfNonEmpty(attrs, "redfish.origin_of_condition", ev.OriginOfCondition.ODataID)
		putIfNonEmpty(attrs, "redfish.context", ev.Context)

		if len(ev.MessageArgs) > 0 {
			args := attrs.PutEmptySlice("redfish.message_args")
			for _, arg := range ev.MessageArgs {
				args.AppendEmpty().SetStr(arg)
			}
		}

		if ts, ok := parseEventTime(ev.EventTimestamp); ok {
			lr.SetTimestamp(pcommon.NewTimestampFromTime(ts))
		} else {
			lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().UTC()))
		}

		if ev.Message != "" {
			lr.Body().SetStr(ev.Message)
		} else if ev.MessageID != "" {
			lr.Body().SetStr(ev.MessageID)
		}
	}

	return ld, len(payload.Events), nil
}

func putIfNonEmpty(attrs pcommon.Map, key, value string) {
	if value != "" {
		attrs.PutStr(key, value)
	}
}

func parseEventTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
