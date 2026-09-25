// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

func enrichComputeEvent(ev *InventoryEvent, labels map[string]string) {
	ctx := map[string]any{}
	for _, k := range []string{"locator", "socket", "id", "fan_name", "name", "member_id", "serial_number", "manufacturer", "model"} {
		if v := labels[k]; v != "" {
			ctx[k] = v
		}
	}
	if len(ctx) > 0 {
		ev.Context = ctx
	}
	id := map[string]string{"hostname": ev.Hostname}
	for k, v := range ev.IdentityKeys {
		id[k] = v
	}
	ev.KgOps = []KgOp{{
		Op: "merge_node", NodeLabel: ev.EntityType, Identity: id,
	}}
	if ev.Action == "remove" {
		ev.KgOps[0].Op = "delete_edge"
		ev.KgOps[0].EdgeType = "HAS_" + ev.EntityType
		ev.KgOps[0].NodeLabel = "Device"
		ev.KgOps[0].Identity = map[string]string{"hostname": ev.Hostname}
		ev.KgOps[0].ToLabel = ev.EntityType
		ev.KgOps[0].ToIdentity = id
	}
}
