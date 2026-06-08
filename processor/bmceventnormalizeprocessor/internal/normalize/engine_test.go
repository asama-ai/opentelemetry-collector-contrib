package normalize

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func registriesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "registries"))
}

func TestNormalizeHPEDriveFailed(t *testing.T) {
	root := registriesRoot(t)
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

func TestLookupByIndexIP(t *testing.T) {
	root := registriesRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	id, ok := engine.lookupByIndexIP("10.25.40.207")
	require.True(t, ok)
	require.Equal(t, "hpe", id.Vendor)
	require.Equal(t, "ilo6", id.BMCModel)
	require.Equal(t, "3.14.0", id.FirmwareVersion)
	require.Equal(t, "hpe.ilo6.3.14.0", id.BundleID)
}

func TestLookupByUniqueMessageID(t *testing.T) {
	root := registriesRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	id, ok := engine.lookupByUniqueMessageID("iLOEvents.3.14.0.DriveFailed")
	require.True(t, ok)
	require.Equal(t, "hpe", id.Vendor)
	require.Equal(t, "hpe.ilo6.3.14.0", id.BundleID)
}

func TestInventoryResolveIndexIP(t *testing.T) {
	root := registriesRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	resolver := NewInventoryResolver(InventoryConfig{
		IndexIPLookup:     true,
		MessageIDFallback: true,
	})
	resolver.SetEngine(engine)

	id := resolver.Resolve("10.25.40.206", "", "", "", "")
	require.Equal(t, "dell", id.Vendor)
	require.Equal(t, "idrac9", id.BMCModel)
	require.Equal(t, identitySourceIndexIP, id.Source)
}

func TestInventoryResolveMessageIDFallback(t *testing.T) {
	root := registriesRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	resolver := NewInventoryResolver(InventoryConfig{
		IndexIPLookup:     false,
		MessageIDFallback: true,
	})
	resolver.SetEngine(engine)

	id := resolver.Resolve("", "iLOEvents.3.14.0.DriveFailed", "", "", "")
	require.Equal(t, "hpe", id.Vendor)
	require.Equal(t, identitySourceMessageID, id.Source)
}

func TestInventoryResolvePrometheus(t *testing.T) {
	root := registriesRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"result":[{"metric":{
				"manufacturer":"HPE",
				"model":"iLO 6",
				"firmware_version":"3.14.0"
			}}]}
		}`))
	}))
	t.Cleanup(srv.Close)

	resolver := NewInventoryResolver(InventoryConfig{
		PrometheusEndpoint: srv.URL,
		PrometheusQuery:    `redfish_bmc_manager_info{target="$IP"}`,
		IndexIPLookup:      false,
		MessageIDFallback:  false,
	})
	resolver.SetEngine(engine)

	id := resolver.Resolve("10.25.40.207", "", "", "", "")
	require.Equal(t, "hpe", id.Vendor)
	require.Equal(t, "ilo6", id.BMCModel)
	require.Equal(t, identitySourcePrometheus, id.Source)
}

func TestMatchBundleFromLabels(t *testing.T) {
	root := registriesRoot(t)
	engine, err := NewEngine(
		filepath.Join(root, "asama-bmc-events.json"),
		filepath.Join(root, "mappings/index.json"),
		filepath.Join(root, "mappings"),
	)
	require.NoError(t, err)

	id, ok := engine.matchBundleFromLabels("HPE", "iLO 6", "3.14.0")
	require.True(t, ok)
	require.Equal(t, "hpe.ilo6.3.14.0", id.BundleID)
}
