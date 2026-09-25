// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import "fmt"

func enrichStorageEvent(ev *InventoryEvent, labels map[string]string) {
	ctx := map[string]any{}
	if a := labels["array"]; a != "" {
		ctx["raid"] = a
	} else if ev.EntityType == "RAID" {
		if r := firstNonEmpty(labels["device"], labels["array"]); r != "" {
			ctx["raid"] = r
		}
	}
	if u := firstNonEmpty(labels["md_uuid"], labels["uuid"]); u != "" {
		ctx["raid_uuid"] = u
	}
	if c := firstNonEmpty(labels["controller"], labels["storage_id"]); c != "" {
		ctx["storage_controller"] = c
	}
	if pci := labels["pci_address"]; pci != "" {
		ctx["pci_address"] = pci
	}
	if vd := firstNonEmpty(labels["vd"], labels["volume_id"]); vd != "" {
		ctx["virtual_drive"] = vd
	}
	if pd := labels["pd"]; pd != "" {
		ctx["physical_drive"] = pd
	}
	if d := labels["device"]; d != "" {
		ctx["device"] = d
	}
	if len(ctx) > 0 {
		ev.Context = ctx
	}

	switch ev.Metric {
	case "node_md_member_info":
		raid := firstNonEmpty(labels["array"], "unknown")
		disk := firstNonEmpty(labels["device"], ev.EntityName)
		ev.Summary = mdMemberSummary(ev.Action, disk, raid)
		raidID := map[string]string{"hostname": ev.Hostname, "name": raid}
		diskID := map[string]string{"hostname": ev.Hostname, "device": disk}
		switch ev.Action {
		case "remove":
			ev.KgOps = []KgOp{
				{Op: "delete_edge", EdgeType: "IN_RAID", NodeLabel: "OsDisk", Identity: diskID, ToLabel: "RAID", ToIdentity: raidID},
				{Op: "merge_edge", EdgeType: "HAS_OS_DISK", NodeLabel: "Device", Identity: map[string]string{"hostname": ev.Hostname}, ToLabel: "OsDisk", ToIdentity: diskID},
			}
		case "create":
			ev.KgOps = []KgOp{
				{Op: "merge_edge", EdgeType: "IN_RAID", NodeLabel: "OsDisk", Identity: diskID, ToLabel: "RAID", ToIdentity: raidID},
				{Op: "delete_edge", EdgeType: "HAS_OS_DISK", NodeLabel: "Device", Identity: map[string]string{"hostname": ev.Hostname}, ToLabel: "OsDisk", ToIdentity: diskID},
			}
		}
	case "node_md_array_info", "node_md_array_size_bytes":
		raid := firstNonEmpty(labels["array"], labels["device"], ev.EntityName)
		ev.Summary = fmt.Sprintf("%s RAID %s on %s", titleAction(ev.Action), raid, ev.Hostname)
		ev.KgOps = []KgOp{{
			Op: "merge_node", NodeLabel: "RAID",
			Identity: map[string]string{"hostname": ev.Hostname, "name": raid},
		}}
	}
}

func mdMemberSummary(action, disk, raid string) string {
	switch action {
	case "remove":
		return fmt.Sprintf("Removed %s from RAID %s", disk, raid)
	case "create":
		return fmt.Sprintf("Added %s to RAID %s", disk, raid)
	default:
		return fmt.Sprintf("Updated RAID member %s on %s", disk, raid)
	}
}

func titleAction(action string) string {
	switch action {
	case "create":
		return "Added"
	case "remove":
		return "Removed"
	default:
		return "Updated"
	}
}
