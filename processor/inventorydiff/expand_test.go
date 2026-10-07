// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandMDMemberRemove(t *testing.T) {
	pre := MetricSnapshot{
		ObservedAt: "t0",
		Series: []SeriesPoint{
			{Labels: map[string]string{"array": "md0", "device": "sda"}, Value: 1},
			{Labels: map[string]string{"array": "md0", "device": "nvme0n1"}, Value: 1},
		},
	}
	post := MetricSnapshot{
		ObservedAt: "t1",
		Series: []SeriesPoint{
			{Labels: map[string]string{"array": "md0", "device": "sda"}, Value: 1},
		},
	}
	events := expandEvents("asama-test-02", "node_md_member_info", pre, post)
	require.Len(t, events, 1)
	ev := events[0]
	require.Equal(t, "remove", ev.Action)
	require.Equal(t, "storage", ev.Component)
	require.Equal(t, "OsDisk", ev.EntityType)
	require.Equal(t, "nvme0n1", ev.EntityName)
	require.Equal(t, "md0", ev.Context["raid"])
	require.Equal(t, "Removed nvme0n1 from RAID md0", ev.Summary)
	require.Len(t, ev.KgOps, 2)
	require.Equal(t, "delete_edge", ev.KgOps[0].Op)
	require.Equal(t, "IN_RAID", ev.KgOps[0].EdgeType)
	require.Equal(t, "merge_edge", ev.KgOps[1].Op)
	require.Equal(t, "HAS_OS_DISK", ev.KgOps[1].EdgeType)
}

func TestExpandMDMemberAdd(t *testing.T) {
	pre := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"array": "md0", "device": "sda"}, Value: 1},
	}}
	post := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"array": "md0", "device": "sda"}, Value: 1},
		{Labels: map[string]string{"array": "md0", "device": "sdb"}, Value: 1},
	}}
	events := expandEvents("host-a", "node_md_member_info", pre, post)
	require.Len(t, events, 1)
	require.Equal(t, "create", events[0].Action)
	require.Equal(t, "sdb", events[0].EntityName)
}

func TestExpandMemoryUpdate(t *testing.T) {
	pre := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"locator": "B11", "size_bytes": "1"}, Value: 1},
	}}
	post := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"locator": "B11", "size_bytes": "2"}, Value: 1},
	}}
	events := expandEvents("host-a", "dmidecode_memory_info", pre, post)
	require.Len(t, events, 1)
	require.Equal(t, "update", events[0].Action)
	require.Equal(t, "Memory", events[0].EntityType)
	require.Equal(t, "B11", events[0].EntityName)
	require.Equal(t, []FieldChange{{Field: "size_bytes", Before: "1", After: "2"}}, events[0].Changes)
	require.Contains(t, events[0].Summary, "size_bytes 1 → 2")
}

func TestExpandMDMemberSlotUpdate(t *testing.T) {
	pre := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"array": "md0", "device": "nvme3n1", "slot": "3", "serial_number": "J9009826"}, Value: 1},
	}}
	post := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"array": "md0", "device": "nvme3n1", "slot": "none", "serial_number": "J9009826"}, Value: 1},
	}}
	events := expandEvents("asama-test-02", "node_md_member_info", pre, post)
	require.Len(t, events, 1)
	require.Equal(t, "update", events[0].Action)
	require.Equal(t, []FieldChange{{Field: "slot", Before: "3", After: "none"}}, events[0].Changes)
	require.Equal(t, "Updated RAID member nvme3n1 on md0: slot 3 → none", events[0].Summary)
	require.Equal(t, "none", events[0].Payload["slot"])
}

func TestExpandUnchangedIdentityNoEvent(t *testing.T) {
	snap := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"array": "md0", "device": "sda"}, Value: 1},
	}}
	require.Empty(t, expandEvents("host-a", "node_md_member_info", snap, snap))
}
