// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComponentsForMetric(t *testing.T) {
	require.Equal(t, []string{"storage"}, componentsForMetric("node_md_member_info"))
	require.Equal(t, []string{"storage"}, componentsForMetric("node_block_device_info"))
	require.Equal(t, []string{"storage"}, componentsForMetric("hwraid_vd_info"))
	require.Equal(t, []string{"network"}, componentsForMetric("node_nic_adapter_info"))
	require.Equal(t, []string{"network"}, componentsForMetric("node_nic_port_info"))
	require.Equal(t, []string{"memory"}, componentsForMetric("dmidecode_memory_info"))
	require.Equal(t, []string{"memory"}, componentsForMetric("redfish_memory"))
	require.Equal(t, []string{"processor"}, componentsForMetric("dmidecode_processor_info"))
	require.Equal(t, []string{"nfs"}, componentsForMetric("node_filesystem_mount_info"))
	require.Equal(t, []string{"fan"}, componentsForMetric("redfish_thermal_fan_info"))
	require.Nil(t, componentsForMetric("unknown_metric"))
	require.Nil(t, componentsForMetric("node_network_interface_info"))
}

func TestComponentSyncWorkflowID(t *testing.T) {
	got := componentSyncWorkflowID("nxtgen", "asama-test-02", []string{"storage", "nfs"})
	require.True(t, strings.HasPrefix(got, "asama-test-02/component-sync/"))
	require.Contains(t, got, "nfs-storage-")
	require.Contains(t, got, "nxtgen-")

	a := componentSyncWorkflowID("acme/us", "host", []string{"storage"})
	b := componentSyncWorkflowID("acme?us", "host", []string{"storage"})
	require.NotEqual(t, a, b)
	require.Contains(t, a, "acme-us-")
	require.Contains(t, b, "acme-us-")
}

func TestComponentSyncConfigNormalized(t *testing.T) {
	cfg := (&ComponentSyncConfig{
		TemporalAddress: "localhost:7233",
		Tenant:          "nxtgen",
	}).normalized()
	require.Equal(t, defaultTemporalNamespace, cfg.Namespace)
	require.Equal(t, defaultComponentSyncTaskQueue, cfg.TaskQueue)
	require.Equal(t, defaultComponentSyncWorkflow, cfg.Workflow)
}
