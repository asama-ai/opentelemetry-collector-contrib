// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import "strings"

// FieldMap maps one metric label (or the sample value) onto a KG node property.
// ToKG empty means the metric field is used only for series identity / context / edges,
// not as a property on NodeType (e.g. member "array" → RAID via IN_RAID, not OsDisk.array).
type FieldMap struct {
	FromMetric string // metric label name, or "__value__" / "__hostname__"
	ToKG       string // Neo4j property on NodeType; "" = not a property on this node
}

// PathHop is one step in the KG walk from the graph root to the target node.
// Hostname is not assumed on leaf nodes — reach the target by following Topology.
//
//	Direction "out": (prev)-[:Edge]->(this)
//	Direction "in":  (this)-[:Edge]->(prev)   e.g. OsDisk -[:IN_RAID]-> RAID
type PathHop struct {
	Edge      string // "" for root hop
	Direction string // "out" | "in"; ignored when Edge == ""
	NodeType  string
	Match     []FieldMap // bind metric labels → properties on this hop
}

// MetricSpec is the code-owned map:
//
//	metric name  → which KG node type to touch (NodeType)
//	Identity     → which series = which entity (metric labels for diff)
//	Payload      → metric fields read into KG properties on create/update
//	Topology     → A-B-C-D edge path to reach that node (no hostname-on-leaf required)
//	Component    → Temporal inventory sync component name
type MetricSpec struct {
	Name      string
	Component string
	NodeType  string // KG node label: OsDisk, RAID, Memory, …
	Identity  []FieldMap
	Payload   []FieldMap
	Topology  []PathHop
}

// EntityType is an alias for NodeType (event / UI field name).
func (s MetricSpec) EntityType() string { return s.NodeType }

// IdentityLabels returns metric label names used to key a series (diff create/update/remove).
func (s MetricSpec) IdentityLabels() []string {
	out := make([]string, 0, len(s.Identity))
	for _, f := range s.Identity {
		if f.FromMetric != "" && f.FromMetric != "__value__" && f.FromMetric != "__hostname__" {
			out = append(out, f.FromMetric)
		}
	}
	return out
}

// pathFromDevice is Device -[:edge]-> nodeType matched by match.
func pathFromDevice(edge, nodeType string, match []FieldMap) []PathHop {
	return []PathHop{
		{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
		{Edge: edge, Direction: "out", NodeType: nodeType, Match: match},
	}
}

// pathDeviceRAIDOsDisk is Device -[:HAS_RAID]-> RAID <-[:IN_RAID]- OsDisk.
func pathDeviceRAIDOsDisk() []PathHop {
	return []PathHop{
		{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
		{Edge: "HAS_RAID", Direction: "out", NodeType: "RAID", Match: []FieldMap{{FromMetric: "array", ToKG: "name"}}},
		{Edge: "IN_RAID", Direction: "in", NodeType: "OsDisk", Match: []FieldMap{{FromMetric: "device", ToKG: "device"}}},
	}
}

// pathDeviceRAID is Device -[:HAS_RAID]-> RAID.
func pathDeviceRAID() []PathHop {
	return pathFromDevice("HAS_RAID", "RAID", []FieldMap{{FromMetric: "array", ToKG: "name"}})
}


var metricRegistry = map[string]MetricSpec{}

func init() {
	register := func(specs ...MetricSpec) {
		for _, s := range specs {
			metricRegistry[s.Name] = s
		}
	}

	register(
		// --- storage / mdadm ---
		// Device -[:HAS_RAID]-> RAID <-[:IN_RAID]- OsDisk
		MetricSpec{
			Name: "node_md_member_info", Component: "storage", NodeType: "OsDisk",
			Identity: []FieldMap{
				{FromMetric: "device", ToKG: "device"},
				{FromMetric: "array", ToKG: ""}, // binds RAID hop in Topology
			},
			Payload: []FieldMap{
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "slot", ToKG: "slot"},
			},
			Topology: pathDeviceRAIDOsDisk(),
		},
		// Device -[:HAS_RAID]-> RAID
		MetricSpec{
			Name: "node_md_array_info", Component: "storage", NodeType: "RAID",
			Identity: []FieldMap{
				{FromMetric: "array", ToKG: "name"},
			},
			Payload: []FieldMap{
				{FromMetric: "device", ToKG: "device"},
				{FromMetric: "level", ToKG: "level"},
				{FromMetric: "metadata", ToKG: "metadata"},
				{FromMetric: "md_uuid", ToKG: "uuid"},
				{FromMetric: "serial_number", ToKG: ""},
			},
			Topology: pathDeviceRAID(),
		},
		MetricSpec{
			Name: "node_md_array_size_bytes", Component: "storage", NodeType: "RAID",
			Identity: []FieldMap{
				{FromMetric: "array", ToKG: "name"},
			},
			Payload: []FieldMap{
				{FromMetric: "__value__", ToKG: "size_bytes"},
			},
			Topology: pathDeviceRAID(),
		},

		// --- storage / block ---
		MetricSpec{
			Name: "node_block_device_info", Component: "storage", NodeType: "OsDisk",
			Identity: []FieldMap{{FromMetric: "device", ToKG: "device"}},
			Payload: []FieldMap{
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "major_minor", ToKG: "major_minor"},
				{FromMetric: "rotational", ToKG: ""},
				{FromMetric: "type", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_OS_DISK", "OsDisk", []FieldMap{{FromMetric: "device", ToKG: "device"}}),
		},
		MetricSpec{
			Name: "node_disk_info", Component: "storage", NodeType: "OsDisk",
			Identity: []FieldMap{{FromMetric: "device", ToKG: "device"}},
			Payload: []FieldMap{
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "path", ToKG: "path"},
				{FromMetric: "major", ToKG: ""},
				{FromMetric: "minor", ToKG: ""},
				{FromMetric: "rotational", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_OS_DISK", "OsDisk", []FieldMap{{FromMetric: "device", ToKG: "device"}}),
		},

		// --- storage / hwraid ---
		MetricSpec{
			Name: "hwraid_controller_info", Component: "storage", NodeType: "StorageController",
			Identity: []FieldMap{{FromMetric: "controller", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "pci_address", ToKG: "pci_bdf"},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "serial", ToKG: "serial_number"},
				{FromMetric: "firmware", ToKG: "firmware_version"},
				{FromMetric: "raid_tool", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_CONTROLLER", "StorageController", []FieldMap{{FromMetric: "pci_address", ToKG: "pci_bdf"}}),
		},
		MetricSpec{
			Name: "hwraid_vd_info", Component: "storage", NodeType: "VirtualDrive",
			Identity: []FieldMap{
				{FromMetric: "controller", ToKG: ""},
				{FromMetric: "vd", ToKG: ""},
			},
			Payload: []FieldMap{
				{FromMetric: "name", ToKG: "name"},
				{FromMetric: "raid_level", ToKG: "raid_level"},
				{FromMetric: "array", ToKG: ""},
				{FromMetric: "device", ToKG: ""},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{{FromMetric: "controller", ToKG: ""}}},
				{Edge: "HAS_VIRTUAL_DRIVE", Direction: "out", NodeType: "VirtualDrive", Match: []FieldMap{{FromMetric: "vd", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "hwraid_vd_size_bytes", Component: "storage", NodeType: "VirtualDrive",
			Identity: []FieldMap{
				{FromMetric: "controller", ToKG: ""},
				{FromMetric: "vd", ToKG: ""},
			},
			Payload:  []FieldMap{{FromMetric: "__value__", ToKG: "size_bytes"}},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{{FromMetric: "controller", ToKG: ""}}},
				{Edge: "HAS_VIRTUAL_DRIVE", Direction: "out", NodeType: "VirtualDrive", Match: []FieldMap{{FromMetric: "vd", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "hwraid_pd_info", Component: "storage", NodeType: "PhysicalDrive",
			Identity: []FieldMap{
				{FromMetric: "controller", ToKG: ""},
				{FromMetric: "pd", ToKG: ""},
			},
			Payload: []FieldMap{
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "media", ToKG: "media_type"},
				{FromMetric: "interface", ToKG: "interface"},
				{FromMetric: "vd", ToKG: ""},
				{FromMetric: "array", ToKG: ""},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{{FromMetric: "controller", ToKG: ""}}},
				{Edge: "HAS_VIRTUAL_DRIVE", Direction: "out", NodeType: "VirtualDrive", Match: []FieldMap{{FromMetric: "vd", ToKG: ""}}},
				{Edge: "HAS_MEMBER", Direction: "out", NodeType: "PhysicalDrive", Match: []FieldMap{{FromMetric: "serial_number", ToKG: "serial_number"}}},
			},
		},
		MetricSpec{
			Name: "hwraid_pd_size_bytes", Component: "storage", NodeType: "PhysicalDrive",
			Identity: []FieldMap{
				{FromMetric: "controller", ToKG: ""},
				{FromMetric: "pd", ToKG: ""},
			},
			Payload: []FieldMap{{FromMetric: "__value__", ToKG: "size_bytes"}},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{{FromMetric: "controller", ToKG: ""}}},
				{Edge: "HAS_VIRTUAL_DRIVE", Direction: "out", NodeType: "VirtualDrive", Match: []FieldMap{{FromMetric: "vd", ToKG: ""}}},
				{Edge: "HAS_MEMBER", Direction: "out", NodeType: "PhysicalDrive", Match: []FieldMap{{FromMetric: "pd", ToKG: ""}}},
			},
		},

		// --- storage / redfish ---
		MetricSpec{
			Name: "redfish_storage_controller_info", Component: "storage", NodeType: "StorageController",
			Identity: []FieldMap{{FromMetric: "storage_id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "manufacturer", ToKG: "manufacturer"},
				{FromMetric: "name", ToKG: "name"},
				{FromMetric: "serial_number", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_CONTROLLER", "StorageController", []FieldMap{{FromMetric: "storage_id", ToKG: ""}}),
		},
		MetricSpec{
			Name: "redfish_storage_volume_info", Component: "storage", NodeType: "VirtualDrive",
			Identity: []FieldMap{
				{FromMetric: "storage_id", ToKG: ""},
				{FromMetric: "id", ToKG: ""},
			},
			Payload: []FieldMap{
				{FromMetric: "name", ToKG: "name"},
				{FromMetric: "raid_type", ToKG: "raid_type"},
				{FromMetric: "capacity_bytes", ToKG: "capacity_bytes"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{{FromMetric: "storage_id", ToKG: ""}}},
				{Edge: "HAS_VIRTUAL_DRIVE", Direction: "out", NodeType: "VirtualDrive", Match: []FieldMap{{FromMetric: "id", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "redfish_storage_drive_info", Component: "storage", NodeType: "PhysicalDrive",
			Identity: []FieldMap{
				{FromMetric: "storage_id", ToKG: ""},
				{FromMetric: "id", ToKG: ""},
			},
			Payload: []FieldMap{
				{FromMetric: "device_serial_number", ToKG: "serial_number"},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "media_type", ToKG: "media_type"},
				{FromMetric: "protocol", ToKG: "protocol"},
				{FromMetric: "capacity_bytes", ToKG: "capacity_bytes"},
				{FromMetric: "volume_id", ToKG: ""},
			},
			// CISS: Device-[:HAS_CONTROLLER]->SC-[:HAS_VIRTUAL_DRIVE]->VD-[:HAS_MEMBER]->PD
			// (unattached PDs use HAS_PHYSICAL_DRIVE instead of VD+HAS_MEMBER)
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{{FromMetric: "storage_id", ToKG: ""}}},
				{Edge: "HAS_VIRTUAL_DRIVE", Direction: "out", NodeType: "VirtualDrive", Match: []FieldMap{{FromMetric: "volume_id", ToKG: ""}}},
				{Edge: "HAS_MEMBER", Direction: "out", NodeType: "PhysicalDrive", Match: []FieldMap{{FromMetric: "device_serial_number", ToKG: "serial_number"}}},
			},
		},
		MetricSpec{
			Name: "smartctl_device", Component: "storage", NodeType: "PhysicalDrive",
			Identity: []FieldMap{{FromMetric: "device", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "exported_serial_number", ToKG: "serial_number"},
				{FromMetric: "model_name", ToKG: "model"},
				{FromMetric: "firmware_version", ToKG: "firmware_version"},
			},
			// CISS joins smartctl onto PD by serial under the host storage tree (no single fixed hop).
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_CONTROLLER", Direction: "out", NodeType: "StorageController", Match: []FieldMap{}},
				{Edge: "HAS_PHYSICAL_DRIVE", Direction: "out", NodeType: "PhysicalDrive", Match: []FieldMap{{FromMetric: "exported_serial_number", ToKG: "serial_number"}}},
			},
		},

		// --- network ---
		// CISS: Device-[:HAS_NIC_SLOT]->NICSlot-[:HAS_NIC]->NIC-[:NIC_HAS_INTERFACE]->NICPort
		MetricSpec{
			Name: "node_nic_adapter_info", Component: "network", NodeType: "NIC",
			Identity: []FieldMap{{FromMetric: "adapter_id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "product", ToKG: "model"},
				{FromMetric: "driver", ToKG: ""},
				{FromMetric: "firmware", ToKG: "firmware_version"},
				{FromMetric: "part_number", ToKG: "part_number"},
				{FromMetric: "bus", ToKG: "pci_bdf"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{{FromMetric: "adapter_id", ToKG: ""}}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{{FromMetric: "adapter_id", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "node_nic_port_info", Component: "network", NodeType: "NICPort",
			Identity: []FieldMap{
				{FromMetric: "adapter_id", ToKG: ""},
				{FromMetric: "interface", ToKG: "name"},
			},
			Payload: []FieldMap{
				{FromMetric: "pci_address", ToKG: ""},
				{FromMetric: "pci_function", ToKG: ""},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{{FromMetric: "adapter_id", ToKG: ""}}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{{FromMetric: "adapter_id", ToKG: ""}}},
				{Edge: "NIC_HAS_INTERFACE", Direction: "out", NodeType: "NICPort", Match: []FieldMap{{FromMetric: "interface", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name: "node_nic_interface_info", Component: "network", NodeType: "NICPort",
			Identity: []FieldMap{{FromMetric: "interface", ToKG: "name"}},
			Payload: []FieldMap{
				{FromMetric: "mac", ToKG: "mac"},
				{FromMetric: "operstate", ToKG: ""},
				{FromMetric: "link_role", ToKG: "link_role"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{}},
				{Edge: "NIC_HAS_INTERFACE", Direction: "out", NodeType: "NICPort", Match: []FieldMap{{FromMetric: "interface", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name: "node_nic_link_speed_megabits", Component: "network", NodeType: "NICPort",
			Identity: []FieldMap{{FromMetric: "interface", ToKG: "name"}},
			Payload:  []FieldMap{{FromMetric: "__value__", ToKG: "speed_mbps"}},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{}},
				{Edge: "NIC_HAS_INTERFACE", Direction: "out", NodeType: "NICPort", Match: []FieldMap{{FromMetric: "interface", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name: "redfish_network_adapter_info", Component: "network", NodeType: "NIC",
			Identity: []FieldMap{{FromMetric: "id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "name", ToKG: "name"},
				{FromMetric: "manufacturer", ToKG: "manufacturer"},
				{FromMetric: "model", ToKG: "model"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{{FromMetric: "id", ToKG: ""}}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{{FromMetric: "id", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "redfish_network_port_info", Component: "network", NodeType: "NICPort",
			Identity: []FieldMap{{FromMetric: "id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "name", ToKG: "name"},
				{FromMetric: "mac", ToKG: "mac"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{}},
				{Edge: "NIC_HAS_INTERFACE", Direction: "out", NodeType: "NICPort", Match: []FieldMap{{FromMetric: "name", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name:     "redfish_network", Component: "network", NodeType: "NIC",
			Identity: []FieldMap{{FromMetric: "id", ToKG: ""}},
			Payload:  []FieldMap{},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_NIC_SLOT", Direction: "out", NodeType: "NICSlot", Match: []FieldMap{}},
				{Edge: "HAS_NIC", Direction: "out", NodeType: "NIC", Match: []FieldMap{{FromMetric: "id", ToKG: ""}}},
			},
		},

		// --- compute ---
		MetricSpec{
			Name: "dmidecode_memory_info", Component: "memory", NodeType: "Memory",
			Identity: []FieldMap{{FromMetric: "locator", ToKG: "name"}},
			Payload: []FieldMap{
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "size_bytes", ToKG: ""},
				{FromMetric: "speed_mts", ToKG: "configured_speed"},
				{FromMetric: "memory_type", ToKG: ""},
				{FromMetric: "manufacturer", ToKG: ""},
				{FromMetric: "part_number", ToKG: ""},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_MEMORY_SLOT", Direction: "out", NodeType: "MemorySlot", Match: []FieldMap{{FromMetric: "locator", ToKG: ""}}},
				{Edge: "HAS_MEMORY", Direction: "out", NodeType: "Memory", Match: []FieldMap{{FromMetric: "locator", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name: "redfish_memory", Component: "memory", NodeType: "Memory",
			Identity: []FieldMap{{FromMetric: "id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "device_locator", ToKG: ""},
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "capacity_mib", ToKG: ""},
				{FromMetric: "operating_speed_mhz", ToKG: "configured_speed"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_MEMORY_SLOT", Direction: "out", NodeType: "MemorySlot", Match: []FieldMap{{FromMetric: "device_locator", ToKG: ""}}},
				{Edge: "HAS_MEMORY", Direction: "out", NodeType: "Memory", Match: []FieldMap{{FromMetric: "id", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "dmidecode_processor_info", Component: "processor", NodeType: "Processor",
			Identity: []FieldMap{{FromMetric: "socket", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "version", ToKG: "model"},
				{FromMetric: "manufacturer", ToKG: "manufacturer"},
				{FromMetric: "serial_number", ToKG: "serial_number"},
				{FromMetric: "core_count", ToKG: "cores"},
				{FromMetric: "thread_count", ToKG: "threads_count"},
				{FromMetric: "current_speed_mhz", ToKG: "current_speed_mhz"},
				{FromMetric: "max_speed_mhz", ToKG: "max_speed_mhz"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_PROCESSOR_SOCKET", Direction: "out", NodeType: "ProcessorSocket", Match: []FieldMap{{FromMetric: "socket", ToKG: ""}}},
				{Edge: "HAS_PROCESSOR", Direction: "out", NodeType: "Processor", Match: []FieldMap{{FromMetric: "socket", ToKG: ""}}},
			},
		},
		MetricSpec{
			Name: "redfish_processor_info", Component: "processor", NodeType: "Processor",
			Identity: []FieldMap{{FromMetric: "id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "socket", ToKG: ""},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "manufacturer", ToKG: "manufacturer"},
				{FromMetric: "total_cores", ToKG: "cores"},
				{FromMetric: "total_threads", ToKG: "threads_count"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_PROCESSOR_SOCKET", Direction: "out", NodeType: "ProcessorSocket", Match: []FieldMap{{FromMetric: "socket", ToKG: ""}}},
				{Edge: "HAS_PROCESSOR", Direction: "out", NodeType: "Processor", Match: []FieldMap{{FromMetric: "id", ToKG: ""}}},
			},
		},

		// --- nfs ---
		MetricSpec{
			Name: "node_filesystem_mount_info", Component: "nfs", NodeType: "NFSMount",
			Identity: []FieldMap{
				{FromMetric: "mountpoint", ToKG: "mountpoint"},
				{FromMetric: "device", ToKG: "device"},
			},
			Payload: []FieldMap{
				{FromMetric: "major", ToKG: ""},
				{FromMetric: "minor", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_NFS_MOUNT", "NFSMount", []FieldMap{
				{FromMetric: "mountpoint", ToKG: "mountpoint"},
				{FromMetric: "device", ToKG: "device"},
			}),
		},
		MetricSpec{
			Name: "node_filesystem_size_bytes", Component: "nfs", NodeType: "NFSMount",
			Identity: []FieldMap{{FromMetric: "mountpoint", ToKG: "mountpoint"}},
			Payload: []FieldMap{
				{FromMetric: "fstype", ToKG: "fstype"},
				{FromMetric: "__value__", ToKG: "size_bytes"},
			},
			Topology: pathFromDevice("HAS_NFS_MOUNT", "NFSMount", []FieldMap{{FromMetric: "mountpoint", ToKG: "mountpoint"}}),
		},

		// --- fan / psu / bmc / bios / os / gpu ---
		MetricSpec{
			Name: "redfish_thermal_fan_info", Component: "fan", NodeType: "Fan",
			Identity: []FieldMap{{FromMetric: "fan_name", ToKG: "name"}},
			Payload:  []FieldMap{{FromMetric: "chassis_id", ToKG: ""}},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_FAN_SLOT", Direction: "out", NodeType: "FanSlot", Match: []FieldMap{{FromMetric: "fan_name", ToKG: ""}}},
				{Edge: "HAS_FAN", Direction: "out", NodeType: "Fan", Match: []FieldMap{{FromMetric: "fan_name", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name: "redfish_powersupply_info", Component: "psu", NodeType: "PSU",
			Identity: []FieldMap{{FromMetric: "name", ToKG: "name"}},
			Payload: []FieldMap{
				{FromMetric: "manufacturer", ToKG: "manufacturer"},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "part_number", ToKG: "part_number"},
				{FromMetric: "psu_serial_number", ToKG: "serial_number"},
				{FromMetric: "firmware_version", ToKG: "firmware_version"},
				{FromMetric: "member_id", ToKG: ""},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_PSU_SLOT", Direction: "out", NodeType: "PSUSlot", Match: []FieldMap{{FromMetric: "name", ToKG: ""}}},
				{Edge: "HAS_PSU", Direction: "out", NodeType: "PSU", Match: []FieldMap{{FromMetric: "name", ToKG: "name"}}},
			},
		},
		MetricSpec{
			Name: "redfish_bmc_manager_info", Component: "bmc", NodeType: "BMC",
			Identity: []FieldMap{{FromMetric: "id", ToKG: ""}},
			Payload: []FieldMap{
				{FromMetric: "manufacturer", ToKG: "manufacturer"},
				{FromMetric: "model", ToKG: "model"},
				{FromMetric: "firmware_version", ToKG: "firmware_version"},
				{FromMetric: "serial_number", ToKG: "serial_number"},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_BMC_SLOT", Direction: "out", NodeType: "BMCSlot", Match: []FieldMap{}},
				{Edge: "HAS_BMC", Direction: "out", NodeType: "BMC", Match: []FieldMap{}},
			},
		},
		MetricSpec{
			Name: "node_dmi_info", Component: "bios", NodeType: "BIOS",
			Identity: []FieldMap{},
			Payload: []FieldMap{
				{FromMetric: "bios_vendor", ToKG: ""},
				{FromMetric: "bios_version", ToKG: "version"},
				{FromMetric: "bios_date", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_BIOS", "BIOS", nil),
		},
		MetricSpec{
			Name: "node_os_info", Component: "os", NodeType: "OS",
			Identity: []FieldMap{},
			Payload: []FieldMap{
				{FromMetric: "pretty_name", ToKG: "name"},
				{FromMetric: "version_id", ToKG: ""},
				{FromMetric: "id", ToKG: ""},
			},
			Topology: pathFromDevice("HAS_OS", "OS", nil),
		},
		MetricSpec{
			Name: "node_pcidevice_info", Component: "gpu", NodeType: "GPU",
			Identity: []FieldMap{
				{FromMetric: "segment", ToKG: ""},
				{FromMetric: "bus", ToKG: ""},
				{FromMetric: "device", ToKG: ""},
				{FromMetric: "function", ToKG: ""},
			},
			Payload: []FieldMap{
				{FromMetric: "vendor_name", ToKG: "manufacturer"},
				{FromMetric: "device_name", ToKG: "model"},
				{FromMetric: "class_id", ToKG: ""},
			},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_GPU_SLOT", Direction: "out", NodeType: "GPUSlot", Match: []FieldMap{}},
				{Edge: "HAS_GPU", Direction: "out", NodeType: "GPU", Match: []FieldMap{}},
			},
		},
		MetricSpec{
			Name: "DCGM_FI_DEV_FB_USED", Component: "gpu", NodeType: "GPU",
			Identity:  []FieldMap{{FromMetric: "UUID", ToKG: ""}},
			Payload:   []FieldMap{},
			Topology: []PathHop{
				{NodeType: "Device", Match: []FieldMap{{FromMetric: "__hostname__", ToKG: "hostname"}}},
				{Edge: "HAS_GPU_SLOT", Direction: "out", NodeType: "GPUSlot", Match: []FieldMap{}},
				{Edge: "HAS_GPU", Direction: "out", NodeType: "GPU", Match: []FieldMap{{FromMetric: "UUID", ToKG: ""}}},
			},
		},
	)
}

// Lookup returns the MetricSpec for a metric name.
func Lookup(metric string) (MetricSpec, bool) {
	s, ok := metricRegistry[strings.TrimSpace(metric)]
	return s, ok
}

// DefaultMetrics returns all registered metric names.
func DefaultMetrics() []string {
	out := make([]string, 0, len(metricRegistry))
	for name := range metricRegistry {
		out = append(out, name)
	}
	return out
}

// componentsForMetric returns Temporal component sync targets for a metric.
func componentsForMetric(metric string) []string {
	s, ok := Lookup(metric)
	if !ok || s.Component == "" {
		return nil
	}
	return []string{s.Component}
}
