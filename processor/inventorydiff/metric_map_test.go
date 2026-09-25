// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupAndDefaultMetrics(t *testing.T) {
	s, ok := Lookup("node_md_member_info")
	require.True(t, ok)
	require.Equal(t, "storage", s.Component)
	require.Equal(t, "OsDisk", s.NodeType)
	require.Equal(t, []string{"device", "array"}, s.IdentityLabels())
	require.Equal(t, "device", s.Identity[0].ToKG)
	require.Equal(t, "", s.Identity[1].ToKG) // array is edge context, not OsDisk prop

	raid, ok := Lookup("node_md_array_info")
	require.True(t, ok)
	require.Equal(t, "RAID", raid.NodeType)
	require.Equal(t, "array", raid.Identity[0].FromMetric)
	require.Equal(t, "name", raid.Identity[0].ToKG) // metric array → RAID.name

	all := DefaultMetrics()
	require.GreaterOrEqual(t, len(all), 20)
	require.Contains(t, all, "redfish_memory")
	require.Contains(t, all, "node_filesystem_mount_info")
	require.Contains(t, all, "smartctl_device")
}

func TestRegistryHasIdentityOrHostScope(t *testing.T) {
	for _, name := range DefaultMetrics() {
		s, ok := Lookup(name)
		require.True(t, ok, name)
		require.NotEmpty(t, s.Component, name)
		require.NotEmpty(t, s.NodeType, name)
		if s.NodeType == "BIOS" || s.NodeType == "OS" {
			require.Empty(t, s.Identity, name)
		} else {
			require.NotEmpty(t, s.Identity, name)
		}
	}
}

func TestMDMemberIdentityKeysUseKGProps(t *testing.T) {
	pre := MetricSnapshot{Series: []SeriesPoint{
		{Labels: map[string]string{"array": "md0", "device": "nvme0n1", "serial_number": "A06066AA"}, Value: 1},
	}}
	post := MetricSnapshot{Series: []SeriesPoint{}}
	events := expandEvents("asama-test-02", "node_md_member_info", pre, post)
	require.Len(t, events, 1)
	require.Equal(t, "nvme0n1", events[0].IdentityKeys["device"])
	_, hasHost := events[0].IdentityKeys["hostname"]
	require.False(t, hasHost)
	_, hasArray := events[0].IdentityKeys["array"]
	require.False(t, hasArray)
	require.Equal(t, "md0", events[0].Context["raid"])
	require.Equal(t, "A06066AA", events[0].Payload["serial_number"])

	require.Len(t, events[0].Topology, 3)
	require.Equal(t, "Device", events[0].Topology[0].NodeType)
	require.Equal(t, "asama-test-02", events[0].Topology[0].Match["hostname"])
	require.Equal(t, "HAS_RAID", events[0].Topology[1].Edge)
	require.Equal(t, "out", events[0].Topology[1].Direction)
	require.Equal(t, "RAID", events[0].Topology[1].NodeType)
	require.Equal(t, "md0", events[0].Topology[1].Match["name"])
	require.Equal(t, "IN_RAID", events[0].Topology[2].Edge)
	require.Equal(t, "in", events[0].Topology[2].Direction)
	require.Equal(t, "OsDisk", events[0].Topology[2].NodeType)
	require.Equal(t, "nvme0n1", events[0].Topology[2].Match["device"])
}

func TestRegistryHasTopology(t *testing.T) {
	for _, name := range DefaultMetrics() {
		s, ok := Lookup(name)
		require.True(t, ok, name)
		require.NotEmpty(t, s.Topology, name)
		require.Equal(t, "Device", s.Topology[0].NodeType, name)
	}
}

// TestTopologyMatchesCISSWriteKG locks edge names against CISS dcim/server/*/write_kg.go + mdadm.go.
func TestTopologyMatchesCISSWriteKG(t *testing.T) {
	cases := []struct {
		metric string
		edges  []string // hop[1:].Edge in order
		leaf   string
	}{
		{"node_md_member_info", []string{"HAS_RAID", "IN_RAID"}, "OsDisk"},
		{"node_md_array_info", []string{"HAS_RAID"}, "RAID"},
		{"node_disk_info", []string{"HAS_OS_DISK"}, "OsDisk"},
		{"node_block_device_info", []string{"HAS_OS_DISK"}, "OsDisk"},
		{"hwraid_controller_info", []string{"HAS_CONTROLLER"}, "StorageController"},
		{"hwraid_vd_info", []string{"HAS_CONTROLLER", "HAS_VIRTUAL_DRIVE"}, "VirtualDrive"},
		{"hwraid_pd_info", []string{"HAS_CONTROLLER", "HAS_VIRTUAL_DRIVE", "HAS_MEMBER"}, "PhysicalDrive"},
		{"redfish_storage_drive_info", []string{"HAS_CONTROLLER", "HAS_VIRTUAL_DRIVE", "HAS_MEMBER"}, "PhysicalDrive"},
		{"dmidecode_memory_info", []string{"HAS_MEMORY_SLOT", "HAS_MEMORY"}, "Memory"},
		{"dmidecode_processor_info", []string{"HAS_PROCESSOR_SOCKET", "HAS_PROCESSOR"}, "Processor"},
		{"node_nic_adapter_info", []string{"HAS_NIC_SLOT", "HAS_NIC"}, "NIC"},
		{"node_nic_port_info", []string{"HAS_NIC_SLOT", "HAS_NIC", "NIC_HAS_INTERFACE"}, "NICPort"},
		{"node_nic_interface_info", []string{"HAS_NIC_SLOT", "HAS_NIC", "NIC_HAS_INTERFACE"}, "NICPort"},
		{"node_filesystem_mount_info", []string{"HAS_NFS_MOUNT"}, "NFSMount"},
		{"redfish_thermal_fan_info", []string{"HAS_FAN_SLOT", "HAS_FAN"}, "Fan"},
		{"redfish_powersupply_info", []string{"HAS_PSU_SLOT", "HAS_PSU"}, "PSU"},
		{"redfish_bmc_manager_info", []string{"HAS_BMC_SLOT", "HAS_BMC"}, "BMC"},
		{"node_dmi_info", []string{"HAS_BIOS"}, "BIOS"},
		{"node_os_info", []string{"HAS_OS"}, "OS"},
		{"node_pcidevice_info", []string{"HAS_GPU_SLOT", "HAS_GPU"}, "GPU"},
	}
	for _, tc := range cases {
		s, ok := Lookup(tc.metric)
		require.True(t, ok, tc.metric)
		require.Equal(t, tc.leaf, s.NodeType, tc.metric)
		require.Equal(t, "Device", s.Topology[0].NodeType, tc.metric)
		require.Len(t, s.Topology, len(tc.edges)+1, tc.metric)
		for i, edge := range tc.edges {
			require.Equal(t, edge, s.Topology[i+1].Edge, "%s hop %d", tc.metric, i+1)
			wantDir := "out"
			if edge == "IN_RAID" {
				wantDir = "in"
			}
			require.Equal(t, wantDir, s.Topology[i+1].Direction, "%s hop %d", tc.metric, i+1)
		}
	}
}

