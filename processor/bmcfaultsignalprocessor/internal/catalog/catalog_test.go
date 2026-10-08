// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFaultLabelsFromJSON(t *testing.T) {
	t.Parallel()

	cat, err := Load(filepath.Join("..", "..", "registries", "fault-eligible-events.json"))
	require.NoError(t, err)

	action, rule, ok := cat.Action("storage.drive.failure", "assert")
	require.True(t, ok)
	require.Equal(t, "open", action)
	require.Equal(t, "Storage", rule.SensorType)
	require.Equal(t, "Storage Drive Failure", rule.EventType)

	action, rule, ok = cat.Action("power.redundancy.lost", "assert")
	require.True(t, ok)
	require.Equal(t, "open", action)
	require.Equal(t, "Power Supply", rule.SensorType)
	require.Equal(t, "Power Redundancy Loss", rule.EventType)
}
