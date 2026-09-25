// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

func enrichNetworkEvent(ev *InventoryEvent, labels map[string]string) {
	ctx := map[string]any{}
	for _, k := range []string{"adapter_id", "interface", "pci_address", "mac", "id"} {
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
}
