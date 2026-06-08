package normalize

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../../bmc-registries"))
}

func TestNormalizeHPEDriveFailed(t *testing.T) {
	root := repoRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	result := engine.Normalize(
		"hpe",
		"iLOEvents.3.14.0.DriveFailed",
		"Drive failed",
		"Critical",
		"10.0.0.1",
		"ilo6",
		"3.14.0",
		"",
		"2026-06-08T12:00:00Z",
		[]string{"Bay 1"},
	)
	require.Equal(t, "mapped", result.MappingStatus)
	require.Equal(t, "storage.drive.failure", result.AsamaID)
	require.Equal(t, "assert", result.Lifecycle)
	require.Contains(t, result.Message, "Bay 1")
}
