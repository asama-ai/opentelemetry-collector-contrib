// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configwatchsnapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver"
)

func TestProcessLogsWrittenAppendsSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watched.yaml")
	if err := os.WriteFile(path, []byte("watch_probe: v3\netcd:\n  password: secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ld := actorLogs(path, configfilereceiver.ActorEventWritten)
	out, err := newSnapshotProcessor(zap.NewNop(), createDefaultConfig().(*Config)).processLogs(context.Background(), ld)
	if err != nil {
		t.Fatal(err)
	}
	recs := out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	if recs.Len() != 2 {
		t.Fatalf("got %d records, want actor+snapshot", recs.Len())
	}
	actor := attrMap(recs.At(0))
	if actor["config.event"] != configfilereceiver.ActorEventWritten {
		t.Fatalf("actor event=%q", actor["config.event"])
	}
	if actor["config.actor.pid"] != "42" {
		t.Fatalf("actor pid should stay on original row: %v", actor)
	}
	snap := attrMap(recs.At(1))
	if snap["config.event"] != "changed" {
		t.Fatalf("snapshot event=%q", snap["config.event"])
	}
	if snap["config.key.watch_probe"] != "v3" {
		t.Fatalf("watch_probe=%q attrs=%v", snap["config.key.watch_probe"], snap)
	}
	if _, ok := snap["config.key.etcd.password"]; ok {
		t.Fatalf("password should be omitted, got %q", snap["config.key.etcd.password"])
	}
	if snap["config.checksum"] == "" || snap["config.file"] != path {
		t.Fatalf("snapshot contract missing: %v", snap)
	}
	if recs.At(1).Body().AsString() != "configfile changed" {
		t.Fatalf("body=%q", recs.At(1).Body().AsString())
	}
	if snap["config.actor.pid"] != "42" || snap["config.actor.user"] != "root" || snap["config.actor.loginuser"] != "alice" {
		t.Fatalf("snapshot missing who: %v", snap)
	}
	if snap[configfilereceiver.AttrTrigger] != configfilereceiver.ActorEventWritten {
		t.Fatalf("trigger=%q", snap[configfilereceiver.AttrTrigger])
	}
	if recs.At(1).Timestamp() != recs.At(0).Timestamp() {
		t.Fatalf("snapshot timestamp should match actor event")
	}
}

func TestProcessLogsReplacedAppendsSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watched.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ld := actorLogs(path, configfilereceiver.ActorEventReplaced)
	out, err := newSnapshotProcessor(zap.NewNop(), createDefaultConfig().(*Config)).processLogs(context.Background(), ld)
	if err != nil {
		t.Fatal(err)
	}
	if out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().Len() != 2 {
		t.Fatal("replaced should emit snapshot")
	}
}

func TestProcessLogsDeletedNoSnapshot(t *testing.T) {
	ld := actorLogs("/no/such.yaml", configfilereceiver.ActorEventDeleted)
	out, err := newSnapshotProcessor(zap.NewNop(), createDefaultConfig().(*Config)).processLogs(context.Background(), ld)
	if err != nil {
		t.Fatal(err)
	}
	if out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().Len() != 1 {
		t.Fatal("deleted must not read or snapshot")
	}
}

func TestProcessLogsMissingFileKeepsActor(t *testing.T) {
	ld := actorLogs(filepath.Join(t.TempDir(), "gone.yaml"), configfilereceiver.ActorEventWritten)
	out, err := newSnapshotProcessor(zap.NewNop(), createDefaultConfig().(*Config)).processLogs(context.Background(), ld)
	if err != nil {
		t.Fatal(err)
	}
	recs := out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	if recs.Len() != 1 {
		t.Fatalf("missing file should skip snapshot, got %d", recs.Len())
	}
}

func TestFactoryType(t *testing.T) {
	if got := NewFactory().Type().String(); got != typeStr {
		t.Fatalf("type=%q", got)
	}
}

func actorLogs(path, event string) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "asama-configfile")
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("configfile " + event)
	lr.Attributes().PutStr("config.file", path)
	lr.Attributes().PutStr("config.event", event)
	lr.Attributes().PutStr("config.actor.pid", "42")
	lr.Attributes().PutStr("config.actor.uid", "0")
	lr.Attributes().PutStr("config.actor.user", "root")
	lr.Attributes().PutStr("config.actor.loginuid", "1000")
	lr.Attributes().PutStr("config.actor.loginuser", "alice")
	lr.SetTimestamp(pcommon.Timestamp(1_700_000_000_000_000_000))
	return ld
}

func attrMap(lr plog.LogRecord) map[string]string {
	out := map[string]string{}
	lr.Attributes().Range(func(k string, v pcommon.Value) bool {
		out[k] = v.AsString()
		return true
	})
	return out
}
