// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package parse // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/parse"

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Event is a parsed Redfish alert from Events[].
type Event struct {
	Vendor              string
	EventType           string
	EventID             string
	MessageID           string
	Message             string
	MessageArgs         []string
	Severity            string
	MessageSeverity     string
	EventTimestamp      string
	OriginOfCondition   string
	Context             string
	HPEResource         string
	HPEHostname         string
	DellServerHostname  string
	LenovoSerial        string
	LenovoCommonEventID string
	LenovoServiceable   string
}

type redfishEventOem struct {
	Hpe struct {
		Hostname string `json:"Hostname"`
		Resource string `json:"Resource"`
	} `json:"Hpe"`
	Dell struct {
		ServerHostname string `json:"ServerHostname"`
	} `json:"Dell"`
	Lenovo struct {
		SystemSerialNumber string `json:"SystemSerialNumber"`
		EventInformation   struct {
			CommonEventID string `json:"CommonEventID"`
			Serviceable   string `json:"Serviceable"`
		} `json:"EventInformation"`
	} `json:"Lenovo"`
}

type redfishEventWire struct {
	EventType         string          `json:"EventType"`
	EventID           string          `json:"EventId"`
	EventIDAlt        string          `json:"EventID"`
	MessageID         string          `json:"MessageId"`
	MessageIDAlt      string          `json:"MessageID"`
	Message           string          `json:"Message"`
	MessageArgs       []string        `json:"MessageArgs"`
	Severity          string          `json:"Severity"`
	MessageSeverity   string          `json:"MessageSeverity"`
	EventTimestamp    string          `json:"EventTimestamp"`
	OriginOfCondition json.RawMessage `json:"OriginOfCondition"`
	Context           string          `json:"Context"`
	Oem               redfishEventOem `json:"Oem"`
}

type redfishEventPayload struct {
	Context string             `json:"Context"`
	Events  []redfishEventWire `json:"Events"`
	Oem     redfishEventOem    `json:"Oem"`
}

var knownBMCIPs = map[string]string{
	"10.25.40.206": "dell",
	"10.25.40.207": "hpe",
	"10.25.40.208": "lenovo",
}

// ParsePayload expands a raw Redfish EventService POST body into alert records.
func ParsePayload(body []byte, bmcIP, envelopeContext string) ([]Event, error) {
	if len(body) == 0 {
		return nil, nil
	}

	var payload redfishEventPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		var single redfishEventWire
		if errSingle := json.Unmarshal(body, &single); errSingle != nil {
			return nil, fmt.Errorf("invalid redfish event json: %w", err)
		}
		payload.Events = []redfishEventWire{single}
	}

	if strings.TrimSpace(payload.Context) != "" {
		envelopeContext = strings.TrimSpace(payload.Context)
	}
	if envelopeContext == "" {
		envelopeContext = strings.TrimSpace(payload.Context)
	}

	out := make([]Event, 0, len(payload.Events))
	for _, wire := range payload.Events {
		ev := wireToEvent(wire, envelopeContext, bmcIP, payload.Oem)
		if !hasSubstance(ev) {
			continue
		}
		if ev.Vendor == "" {
			ev.Vendor = detectVendor(ev, bmcIP)
		}
		if ev.Message == "" {
			ev.Message = ev.MessageID
		}
		if ev.Severity == "" {
			ev.Severity = ev.MessageSeverity
		}
		out = append(out, ev)
	}
	return out, nil
}

func wireToEvent(wire redfishEventWire, envelopeContext, bmcIP string, envelopeOem redfishEventOem) Event {
	oem := wire.Oem
	if oem.Hpe.Hostname == "" && oem.Dell.ServerHostname == "" && oem.Lenovo.SystemSerialNumber == "" {
		oem = envelopeOem
	}

	eventContext := strings.TrimSpace(wire.Context)
	if eventContext == "" {
		eventContext = envelopeContext
	}
	if bmcIP != "" {
		eventContext = enrichContext(eventContext, bmcIP)
	}

	return Event{
		Vendor:              detectVendorFromOem(oem),
		EventType:           wire.EventType,
		EventID:             firstNonEmpty(wire.EventID, wire.EventIDAlt),
		MessageID:           firstNonEmpty(wire.MessageID, wire.MessageIDAlt),
		Message:             strings.TrimSpace(wire.Message),
		MessageArgs:         wire.MessageArgs,
		Severity:            firstNonEmpty(wire.Severity, wire.MessageSeverity),
		MessageSeverity:     wire.MessageSeverity,
		EventTimestamp:      wire.EventTimestamp,
		OriginOfCondition:   parseOriginOfCondition(wire.OriginOfCondition),
		Context:             eventContext,
		HPEResource:         strings.TrimSpace(oem.Hpe.Resource),
		HPEHostname:         strings.TrimSpace(oem.Hpe.Hostname),
		DellServerHostname:  strings.TrimSpace(oem.Dell.ServerHostname),
		LenovoSerial:        strings.TrimSpace(oem.Lenovo.SystemSerialNumber),
		LenovoCommonEventID: strings.TrimSpace(oem.Lenovo.EventInformation.CommonEventID),
		LenovoServiceable:   strings.TrimSpace(oem.Lenovo.EventInformation.Serviceable),
	}
}

func detectVendor(ev Event, bmcIP string) string {
	if ev.HPEHostname != "" || ev.HPEResource != "" {
		return "hpe"
	}
	if ev.DellServerHostname != "" {
		return "dell"
	}
	if ev.LenovoSerial != "" || ev.LenovoCommonEventID != "" {
		return "lenovo"
	}
	mid := ev.MessageID
	switch {
	case strings.HasPrefix(mid, "iLOEvents."):
		return "hpe"
	case strings.HasPrefix(mid, "EventRegistry.") || strings.HasPrefix(mid, "FQXSP"):
		return "lenovo"
	case strings.HasPrefix(mid, "IDRAC.") || strings.HasPrefix(mid, "PDR") || strings.HasPrefix(mid, "AMP"):
		return "dell"
	}
	if v, ok := knownBMCIPs[bmcIP]; ok {
		return v
	}
	return ""
}

func detectVendorFromOem(oem redfishEventOem) string {
	if oem.Hpe.Hostname != "" || oem.Hpe.Resource != "" {
		return "hpe"
	}
	if oem.Dell.ServerHostname != "" {
		return "dell"
	}
	if oem.Lenovo.SystemSerialNumber != "" || oem.Lenovo.EventInformation.CommonEventID != "" {
		return "lenovo"
	}
	return ""
}

func hasSubstance(ev Event) bool {
	return firstNonEmpty(
		ev.MessageID,
		ev.Message,
		ev.EventType,
		ev.EventID,
		ev.OriginOfCondition,
		ev.HPEResource,
	) != ""
}

func parseOriginOfCondition(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var asObject struct {
		ODataID string `json:"@odata.id"`
	}
	if err := json.Unmarshal(raw, &asObject); err == nil {
		return strings.TrimSpace(asObject.ODataID)
	}
	return ""
}

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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ParseEventTime parses Redfish event timestamps.
func ParseEventTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
