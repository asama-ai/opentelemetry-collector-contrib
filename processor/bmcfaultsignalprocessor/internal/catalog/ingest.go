// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// IngestConfig maps fault ingest JSON fields to log/resource attributes and rule fields.
type IngestConfig struct {
	Payload  map[string][]string `json:"payload"`
	FromRule map[string]string   `json:"from_rule"`
	EventID  struct {
		Prefix string `json:"prefix"`
		From   string `json:"from"`
	} `json:"event_id"`
}

// BuildIngestPayload copies configured fields into the faults-service ingest body.
func (c *Catalog) BuildIngestPayload(resAttrs, logAttrs pcommon.Map, rule EventRule) map[string]string {
	out := make(map[string]string, len(c.ingest.Payload)+len(c.ingest.FromRule)+1)

	for field, paths := range c.ingest.Payload {
		if value := firstAttr(resAttrs, logAttrs, paths); value != "" {
			out[field] = value
		}
	}
	for field, ruleField := range c.ingest.FromRule {
		if value := ruleFieldValue(rule, ruleField); value != "" {
			out[field] = value
		}
	}
	if from := c.ingest.EventID.From; from != "" {
		if value := attrAt(logAttrs, from); value != "" {
			out["event_id"] = c.ingest.EventID.Prefix + value
		}
	}
	return out
}

func ruleFieldValue(rule EventRule, name string) string {
	switch name {
	case "sensor_type":
		return rule.SensorType
	case "event_type":
		return rule.EventType
	case "fault_type":
		return rule.FaultType
	case "asama_id":
		return rule.AsamaID
	default:
		return ""
	}
}

func firstAttr(resAttrs, logAttrs pcommon.Map, paths []string) string {
	for _, path := range paths {
		if value := attrAt(resAttrs, path); value != "" {
			return value
		}
		if value := attrAt(logAttrs, path); value != "" {
			return value
		}
	}
	return ""
}

func attrAt(attrs pcommon.Map, key string) string {
	v, ok := attrs.Get(key)
	if !ok {
		return ""
	}
	return v.Str()
}
