// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver"

import (
	"encoding/json"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

type redfishEventOem struct {
	Hpe struct {
		Hostname string `json:"Hostname"`
		Resource string `json:"Resource"`
	} `json:"Hpe"`
	Dell struct {
		ServerHostname string `json:"ServerHostname"`
	} `json:"Dell"`
}

// redfishEvent mirrors the Redfish Event schema subset used for BMC push.
type redfishEvent struct {
	EventType         string          `json:"EventType"`
	EventID           string          `json:"EventId"`
	MessageID         string          `json:"MessageId"`
	Message           string          `json:"Message"`
	MessageArgs       []string        `json:"MessageArgs"`
	Severity          string          `json:"Severity"`
	MessageSeverity   string          `json:"MessageSeverity"`
	EventTimestamp    string          `json:"EventTimestamp"`
	OriginOfCondition struct {
		ODataID string `json:"@odata.id"`
	} `json:"OriginOfCondition"`
	Context string          `json:"Context"`
	Oem       redfishEventOem `json:"Oem"`
}

type redfishEventPayload struct {
	Context string         `json:"Context"`
	Events  []redfishEvent `json:"Events"`
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

	src = enrichSourceFromPayload(payload, src)

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

	envelopeContext := strings.TrimSpace(payload.Context)
	for _, ev := range payload.Events {
		lr := sl.LogRecords().AppendEmpty()
		attrs := lr.Attributes()
		putIfNonEmpty(attrs, "redfish.event_type", ev.EventType)
		putIfNonEmpty(attrs, "redfish.event_id", ev.EventID)
		putIfNonEmpty(attrs, "redfish.message_id", ev.MessageID)
		putIfNonEmpty(attrs, "redfish.message", ev.Message)
		putIfNonEmpty(attrs, "redfish.severity", firstNonEmpty(ev.Severity, ev.MessageSeverity))
		putIfNonEmpty(attrs, "redfish.message_severity", ev.MessageSeverity)
		putIfNonEmpty(attrs, "redfish.event_timestamp", ev.EventTimestamp)
		putIfNonEmpty(attrs, "redfish.origin_of_condition", firstNonEmpty(ev.OriginOfCondition.ODataID, ev.Oem.Hpe.Resource))
		putIfNonEmpty(attrs, "redfish.oem.hpe.hostname", ev.Oem.Hpe.Hostname)
		putIfNonEmpty(attrs, "redfish.oem.dell.server_hostname", ev.Oem.Dell.ServerHostname)

		eventContext := strings.TrimSpace(ev.Context)
		if eventContext == "" {
			eventContext = envelopeContext
		}
		if ctx := enrichContext(eventContext, src.IP); ctx != "" {
			attrs.PutStr("redfish.context", ctx)
		}

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

func enrichSourceFromPayload(payload redfishEventPayload, src sourceContext) sourceContext {
	for _, ev := range payload.Events {
		if src.IP == "" {
			if ip := strings.TrimSpace(ev.Oem.Hpe.Hostname); ip != "" {
				src.IP = normalizeIP(ip)
			}
		}
		if src.Hostname == "" {
			if host := strings.TrimSpace(ev.Oem.Dell.ServerHostname); host != "" {
				src.Hostname = host
			}
		}
		if src.IP != "" && src.Hostname != "" {
			break
		}
	}
	return src
}

// resolveBMCSourceIP picks BMC IP: header override, HPE Sender-Address, JSON Oem.Hpe.Hostname, then HTTP remote.
func resolveBMCSourceIP(headerIP, senderAddress, remoteAddr string, body []byte) string {
	if ip := normalizeIP(headerIP); ip != "" {
		return ip
	}
	if ip := normalizeIP(senderAddress); ip != "" {
		return ip
	}
	if ip := hpeHostnameFromBody(body); ip != "" {
		return ip
	}
	return hostFromRemoteAddr(remoteAddr)
}

func hpeHostnameFromBody(body []byte) string {
	var payload redfishEventPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, ev := range payload.Events {
		if ip := strings.TrimSpace(ev.Oem.Hpe.Hostname); ip != "" {
			return normalizeIP(ip)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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

// enrichContext merges the Redfish subscription Context with the resolved BMC IP.
func enrichContext(eventContext, bmcIP string) string {
	bmcIP = strings.TrimSpace(bmcIP)
	eventContext = strings.TrimSpace(eventContext)
	if bmcIP == "" {
		return eventContext
	}
	ipPart := "bmc_ip=" + bmcIP
	if eventContext == "" {
		return ipPart
	}
	if strings.Contains(eventContext, "bmc_ip=") {
		return eventContext
	}
	return eventContext + "|" + ipPart
}

func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		if host == "::1" {
			return "127.0.0.1"
		}
		return strings.Trim(host, "[]")
	}
	return strings.Trim(ip, "[]")
}

// hostFromRemoteAddr returns the host portion of an HTTP RemoteAddr (host:port).
func hostFromRemoteAddr(remoteAddr string) string {
	return normalizeIP(remoteAddr)
}
