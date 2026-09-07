// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configfilereceiver

import (
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestBuildChangedSnapshotRedactsByOmit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watched.yaml")
	if err := os.WriteFile(path, []byte("watch_probe: v1\netcd:\n  password: secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := BuildChangedSnapshot(path, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Event != "changed" {
		t.Fatalf("event=%q", snap.Event)
	}
	if snap.Keys["watch_probe"] != "v1" {
		t.Fatalf("keys=%v", snap.Keys)
	}
	if _, ok := snap.Keys["etcd.password"]; ok {
		t.Fatalf("password key should be omitted, got %v", snap.Keys)
	}

	attrs := pcommon.NewMap()
	WriteSnapshotLogAttributes(attrs, snap)
	got, _ := attrs.Get("config.event")
	if got.Str() != "changed" {
		t.Fatalf("attr event=%q", got.Str())
	}
}
