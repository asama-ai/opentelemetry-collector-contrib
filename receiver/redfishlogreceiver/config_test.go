// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfishlogreceiver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stmcginnis/gofish/redfish"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
)

func TestCreateDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig()
	require.NoError(t, componenttest.CheckConfigStruct(cfg))
	require.GreaterOrEqual(t, cfg.InitialLastN, 1)
}

func TestConfigValidate(t *testing.T) {
	cfg := createDefaultConfig()
	cfg.CredentialsFile = filepath.Join(t.TempDir(), "creds.yaml")
	cfg.Targets = []TargetConfig{{Endpoint: "https://10.0.0.1"}}
	require.NoError(t, cfg.Validate())

	cfg.Targets[0].Endpoint = "not-a-url"
	require.Error(t, cfg.Validate())

	cfg = createDefaultConfig()
	cfg.CredentialsFile = "/x"
	cfg.Targets = []TargetConfig{{Endpoint: "https://h"}}
	cfg.InitialLastN = 0
	require.Error(t, cfg.Validate())

	cfg = createDefaultConfig()
	cfg.CredentialsFile = "/x"
	cfg.Targets = []TargetConfig{{Endpoint: "https://h"}}
	cfg.PollInterval = 0
	require.Error(t, cfg.Validate())

	cfg = createDefaultConfig()
	cfg.CredentialsFile = "/x"
	cfg.Targets = []TargetConfig{{Endpoint: "https://h"}}
	cfg.Timeout = 0
	require.Error(t, cfg.Validate())

	cfg = createDefaultConfig()
	cfg.CredentialsFile = "/x"
	cfg.Targets = []TargetConfig{{Endpoint: "https://h"}}
	cfg.LogResetClockSkew = -time.Second
	require.Error(t, cfg.Validate())
}

func TestSelectEntriesColdStart(t *testing.T) {
	sorted := []*redfish.LogEntry{
		testEntry("1", "2020-01-01T00:00:00Z"),
		testEntry("2", "2020-01-02T00:00:00Z"),
		testEntry("3", "2020-01-03T00:00:00Z"),
	}
	sortLogEntries(sorted)
	out, ck := selectEntriesToEmit(nil, sorted, 2, false)
	require.Len(t, out, 2)
	require.Equal(t, "2", out[0].ID)
	require.Equal(t, "3", out[1].ID)
	require.Equal(t, "3", ck.EntryID)
}

func TestResetByNumericID(t *testing.T) {
	cp := &checkpointState{Created: "2024-01-01T00:00:00Z", EntryID: "100"}
	sorted := []*redfish.LogEntry{
		testEntry("1", "2024-06-01T00:00:00Z"),
		testEntry("2", "2024-06-01T00:00:01Z"),
	}
	sortLogEntries(sorted)
	require.True(t, shouldResetCheckpoint(cp, sorted, time.Minute))
}

func TestResetByTimestampRegression(t *testing.T) {
	cp := &checkpointState{Created: "2025-01-01T00:00:00Z", EntryID: "1"}
	sorted := []*redfish.LogEntry{testEntry("1", "2024-01-01T00:00:00Z")}
	sortLogEntries(sorted)
	require.True(t, shouldResetCheckpoint(cp, sorted, time.Minute))
}

func testEntry(id, created string) *redfish.LogEntry {
	e := &redfish.LogEntry{Created: created}
	e.ID = id
	return e
}

func TestCredentialKeyForTarget(t *testing.T) {
	k := credentialKeyForTarget(TargetConfig{Endpoint: "https://10.25.40.12:443"})
	require.Equal(t, "10.25.40.12", k)
	k2 := credentialKeyForTarget(TargetConfig{Endpoint: "https://[::1]:8443", CredentialKey: "custom"})
	require.Equal(t, "custom", k2)
}

func TestLoadCredentials(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	content := `
default:
  username: u1
  password: "p1"
hosts:
  "10.0.0.1":
    username: u2
    password: "p2"
`
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))

	cs, err := loadCredentialsFile(p)
	require.NoError(t, err)
	u, pwd, err := cs.resolve("10.0.0.1")
	require.NoError(t, err)
	require.Equal(t, "u2", u)
	require.Equal(t, "p2", string(pwd))

	u2, pwd2, err := cs.resolve("192.168.0.1")
	require.NoError(t, err)
	require.Equal(t, "u1", u2)
	require.Equal(t, "p1", string(pwd2))
}
