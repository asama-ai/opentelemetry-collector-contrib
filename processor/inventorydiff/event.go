// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import "encoding/json"

// InventoryEvent is one UI/KG-ready change derived from a metric series diff.
type InventoryEvent struct {
	Hostname     string         `json:"hostname"`
	Component    string         `json:"component"`
	Action       string         `json:"action"` // create | update | remove
	EntityType   string         `json:"entity_type"`
	EntityName   string         `json:"entity_name"`
	Summary      string         `json:"summary"`
	IdentityKeys map[string]string `json:"identity_keys"` // leaf node props (no hostname assumption long-term)
	Payload      map[string]string `json:"payload,omitempty"`
	Topology     []BoundPathHop    `json:"topology,omitempty"` // A→B→C→D walk to the leaf
	Changes      []FieldChange     `json:"changes,omitempty"`  // label/value diffs for update
	Context      map[string]any    `json:"context,omitempty"`
	KgOps        []KgOp            `json:"kg_ops,omitempty"`
	Metric       string            `json:"metric"`
	ObservedAt   string            `json:"observed_at,omitempty"`
}

// FieldChange is one label or gauge-value difference on an update event.
type FieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// BoundPathHop is one resolved step of Topology for this event.
type BoundPathHop struct {
	Edge      string            `json:"edge,omitempty"`
	Direction string            `json:"direction,omitempty"` // out | in
	NodeType  string            `json:"node_type"`
	Match     map[string]string `json:"match,omitempty"` // KG props bound from metric labels
}

// KgOp is a targeted graph mutation hint for CISS / UI.
type KgOp struct {
	Op         string            `json:"op"` // merge_node | delete_edge | merge_edge
	NodeLabel  string            `json:"node_label,omitempty"`
	EdgeType   string            `json:"edge_type,omitempty"`
	Identity   map[string]string `json:"identity,omitempty"`
	ToLabel    string            `json:"to_label,omitempty"`
	ToIdentity map[string]string `json:"to_identity,omitempty"`
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
