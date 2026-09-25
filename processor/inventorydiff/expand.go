// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import (
	"fmt"
	"sort"
	"strings"
)

type seriesEntity struct {
	key    string
	labels map[string]string
	value  string
	point  SeriesPoint
}

// expandEvents turns a metric snapshot diff into UI/KG-ready inventory events.
func expandEvents(hostname, metric string, pre, post MetricSnapshot) []InventoryEvent {
	spec, ok := Lookup(metric)
	if !ok {
		return nil
	}
	idLabels := spec.IdentityLabels()
	preSet := indexByIdentity(pre, idLabels)
	postSet := indexByIdentity(post, idLabels)

	var events []InventoryEvent
	observed := post.ObservedAt
	if observed == "" {
		observed = pre.ObservedAt
	}

	for key, pe := range preSet {
		if _, still := postSet[key]; !still {
			ev := baseEvent(hostname, metric, spec, pe, "remove", observed)
			enrichEvent(&ev, pe.labels, nil)
			events = append(events, ev)
		}
	}
	for key, po := range postSet {
		pe, existed := preSet[key]
		if !existed {
			ev := baseEvent(hostname, metric, spec, po, "create", observed)
			enrichEvent(&ev, nil, po.labels)
			events = append(events, ev)
			continue
		}
		if pe.value != po.value || !labelsEqualIgnoring(pe.labels, po.labels, idLabels) {
			ev := baseEvent(hostname, metric, spec, po, "update", observed)
			ev.Changes = seriesFieldChanges(pe, po, idLabels)
			enrichEvent(&ev, pe.labels, po.labels)
			ev.Summary = appendChangeDetails(ev.Summary, ev.Changes)
			events = append(events, ev)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Action != events[j].Action {
			return events[i].Action < events[j].Action
		}
		return events[i].EntityName < events[j].EntityName
	})
	return events
}

func baseEvent(hostname, metric string, spec MetricSpec, ent seriesEntity, action, observed string) InventoryEvent {
	keys := kgIdentityKeys(hostname, ent.labels, spec.Identity)
	name := entityName(hostname, spec, ent.labels, keys)
	nodeType := spec.EntityType()
	return InventoryEvent{
		Hostname:     hostname,
		Component:    spec.Component,
		Action:       action,
		EntityType:   nodeType,
		EntityName:   name,
		Summary:      defaultSummary(action, nodeType, name),
		IdentityKeys: keys,
		Payload:      payloadFromSeries(ent, spec.Payload),
		Topology:     bindTopology(hostname, ent.labels, spec.Topology),
		Metric:       metric,
		ObservedAt:   observed,
	}
}

func enrichEvent(ev *InventoryEvent, preLabels, postLabels map[string]string) {
	labels := postLabels
	if labels == nil {
		labels = preLabels
	}
	switch ev.Component {
	case "storage":
		enrichStorageEvent(ev, labels)
	case "network":
		enrichNetworkEvent(ev, labels)
	case "nfs":
		enrichNFSEvent(ev, labels)
	default:
		enrichComputeEvent(ev, labels)
	}
}

func defaultSummary(action, entityType, name string) string {
	switch action {
	case "create":
		return fmt.Sprintf("Added %s %s", entityType, name)
	case "remove":
		return fmt.Sprintf("Removed %s %s", entityType, name)
	default:
		return fmt.Sprintf("Updated %s %s", entityType, name)
	}
}

func entityName(hostname string, spec MetricSpec, labels map[string]string, keys map[string]string) string {
	switch spec.EntityType() {
	case "OsDisk":
		if d := labels["device"]; d != "" {
			return d
		}
	case "RAID":
		if a := firstNonEmpty(labels["array"], labels["device"]); a != "" {
			return a
		}
	case "Memory":
		return firstNonEmpty(labels["locator"], labels["id"], labels["device_locator"])
	case "Processor":
		return firstNonEmpty(labels["socket"], labels["id"])
	case "Fan":
		return firstNonEmpty(labels["fan_name"], labels["name"])
	case "PSU":
		return firstNonEmpty(labels["name"], labels["member_id"])
	case "BMC":
		return firstNonEmpty(labels["id"], "bmc")
	case "BIOS", "OS":
		return hostname
	case "GPU":
		return pciName(labels)
	case "NIC":
		return firstNonEmpty(labels["adapter_id"], labels["id"])
	case "NICPort":
		return firstNonEmpty(labels["interface"], labels["id"])
	case "NFSMount":
		return firstNonEmpty(labels["mountpoint"], labels["device"])
	case "StorageController":
		return firstNonEmpty(labels["storage_id"], labels["controller"], labels["pci_address"])
	case "VirtualDrive":
		return firstNonEmpty(labels["vd"], labels["id"], labels["name"])
	case "PhysicalDrive":
		return firstNonEmpty(labels["pd"], labels["device"], labels["id"], labels["device_serial_number"])
	}
	for _, f := range spec.Identity {
		if f.ToKG != "" {
			if v := keys[f.ToKG]; v != "" {
				return v
			}
		}
		if v := labels[f.FromMetric]; v != "" {
			return v
		}
	}
	return metricFallbackName(keys)
}

func pciName(labels map[string]string) string {
	seg := labels["segment"]
	bus := labels["bus"]
	dev := labels["device"]
	fn := labels["function"]
	if bus == "" && dev == "" {
		return firstNonEmpty(labels["UUID"], labels["pci_address"])
	}
	if seg == "" {
		seg = "0000"
	}
	return fmt.Sprintf("%s:%s:%s.%s", seg, bus, dev, fn)
}

func metricFallbackName(keys map[string]string) string {
	parts := make([]string, 0, len(keys))
	for k, v := range keys {
		if k == "hostname" || v == "" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ",")
}

func indexByIdentity(snap MetricSnapshot, identityLabels []string) map[string]seriesEntity {
	out := make(map[string]seriesEntity, len(snap.Series))
	for _, sp := range snap.Series {
		key := identitySeriesKey(sp.Labels, identityLabels)
		out[key] = seriesEntity{
			key:    key,
			labels: sp.Labels,
			value:  sp.valueIdentity(),
			point:  sp,
		}
	}
	return out
}

func identitySeriesKey(labels map[string]string, identityLabels []string) string {
	if len(identityLabels) == 0 {
		// Host-scoped singleton (BIOS/OS): one series per host observation.
		return "_host_"
	}
	parts := make([]string, 0, len(identityLabels))
	for _, k := range identityLabels {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, "\x00")
}

// kgIdentityKeys builds leaf-node props from Identity FieldMaps with ToKG set.
// Hostname is not added here — use Topology root hop (Device) instead.
func kgIdentityKeys(_ string, labels map[string]string, identity []FieldMap) map[string]string {
	out := map[string]string{}
	for _, f := range identity {
		if f.ToKG == "" || f.FromMetric == "" || f.FromMetric == "__value__" || f.FromMetric == "__hostname__" {
			continue
		}
		if v := labels[f.FromMetric]; v != "" {
			out[f.ToKG] = v
		}
	}
	return out
}

// bindTopology resolves PathHop templates against metric labels (+ hostname for Device root).
func bindTopology(hostname string, labels map[string]string, hops []PathHop) []BoundPathHop {
	if len(hops) == 0 {
		return nil
	}
	out := make([]BoundPathHop, 0, len(hops))
	for _, h := range hops {
		match := map[string]string{}
		for _, f := range h.Match {
			if f.ToKG == "" {
				// Still bind metric-only keys under empty ToKG using FromMetric as key
				// so consumers can see unbound selectors (e.g. controller index).
				key := f.FromMetric
				if key == "" || key == "__value__" {
					continue
				}
				val := labelOrHost(hostname, labels, f.FromMetric)
				if val != "" {
					match[key] = val
				}
				continue
			}
			val := labelOrHost(hostname, labels, f.FromMetric)
			if val != "" {
				match[f.ToKG] = val
			}
		}
		out = append(out, BoundPathHop{
			Edge:      h.Edge,
			Direction: h.Direction,
			NodeType:  h.NodeType,
			Match:     match,
		})
	}
	return out
}

func labelOrHost(hostname string, labels map[string]string, from string) string {
	switch from {
	case "__hostname__":
		return hostname
	case "__value__":
		return ""
	default:
		return labels[from]
	}
}

// payloadFromSeries copies metric → KG property values for the event payload.
func payloadFromSeries(ent seriesEntity, payload []FieldMap) map[string]string {
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]string, len(payload))
	for _, f := range payload {
		if f.ToKG == "" {
			continue
		}
		switch f.FromMetric {
		case "__value__":
			if ent.point.exactInt {
				out[f.ToKG] = strings.TrimPrefix(ent.value, "i:")
			} else {
				out[f.ToKG] = strings.TrimPrefix(ent.value, "f:")
			}
		default:
			if v := ent.labels[f.FromMetric]; v != "" {
				out[f.ToKG] = v
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func labelsEqualIgnoring(a, b map[string]string, identity []string) bool {
	ignore := make(map[string]struct{}, len(identity))
	for _, k := range identity {
		ignore[k] = struct{}{}
	}
	check := func(src, dst map[string]string) bool {
		for k, v := range src {
			if _, skip := ignore[k]; skip {
				continue
			}
			if dst[k] != v {
				return false
			}
		}
		return true
	}
	return check(a, b) && check(b, a)
}

// seriesFieldChanges lists non-identity label diffs and gauge-value diffs for updates.
func seriesFieldChanges(pre, post seriesEntity, identityLabels []string) []FieldChange {
	ignore := make(map[string]struct{}, len(identityLabels)+1)
	for _, k := range identityLabels {
		ignore[k] = struct{}{}
	}
	ignore["hostname"] = struct{}{}

	keys := map[string]struct{}{}
	for k := range pre.labels {
		if _, skip := ignore[k]; !skip {
			keys[k] = struct{}{}
		}
	}
	for k := range post.labels {
		if _, skip := ignore[k]; !skip {
			keys[k] = struct{}{}
		}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	var out []FieldChange
	for _, k := range names {
		before := pre.labels[k]
		after := post.labels[k]
		if before == after {
			continue
		}
		out = append(out, FieldChange{Field: k, Before: before, After: after})
	}
	if pre.value != post.value {
		out = append(out, FieldChange{
			Field:  "value",
			Before: stripValuePrefix(pre.value),
			After:  stripValuePrefix(post.value),
		})
	}
	return out
}

func stripValuePrefix(v string) string {
	if strings.HasPrefix(v, "i:") || strings.HasPrefix(v, "f:") {
		return v[2:]
	}
	return v
}

func appendChangeDetails(summary string, changes []FieldChange) string {
	if len(changes) == 0 {
		return summary
	}
	parts := make([]string, 0, len(changes))
	for _, c := range changes {
		switch {
		case c.Before == "" && c.After != "":
			parts = append(parts, fmt.Sprintf("%s=%s", c.Field, c.After))
		case c.Before != "" && c.After == "":
			parts = append(parts, fmt.Sprintf("%s cleared (was %s)", c.Field, c.Before))
		default:
			parts = append(parts, fmt.Sprintf("%s %s → %s", c.Field, c.Before, c.After))
		}
	}
	return summary + ": " + strings.Join(parts, ", ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
