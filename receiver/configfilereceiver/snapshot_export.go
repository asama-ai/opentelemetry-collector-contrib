// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configfilereceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver/internal/configfile"
)

const (
	AttrTrigger        = "config.trigger"
	ActorEventWritten  = "written"
	ActorEventReplaced = "replaced"
	ActorEventDeleted  = "deleted"
)

// Defaults match the hourly configfile receiver.
var (
	DefaultMaxKeysPerFile = configfile.DefaultMaxKeysPerFile
	DefaultExcludeKeys    = configfile.DefaultExcludeKeyGlobs
)

// BuildChangedSnapshot reads path and returns a config.event=changed snapshot
// using the same parse/checksum/redact contract as the hourly receiver poll.
func BuildChangedSnapshot(path string, maxKeys int, excludeKeys []string) (*configfile.Snapshot, error) {
	return configfile.BuildSnapshot(path, "", configfile.Options{
		MaxKeysPerFile: maxKeys,
		ExcludeKeys:    excludeKeys,
	}, configfile.EventChanged)
}

// WriteSnapshotLogAttributes writes receiver-contract attributes onto an OTLP log record.
func WriteSnapshotLogAttributes(attrs pcommon.Map, snap *configfile.Snapshot) {
	if snap == nil {
		return
	}
	attrs.PutStr("config.file", snap.File)
	attrs.PutStr("config.format", snap.Format)
	attrs.PutStr("config.checksum", snap.Checksum)
	attrs.PutInt("config.keys_total", int64(snap.KeysTotal))
	attrs.PutStr("config.event", snap.Event)
	for key, value := range snap.Keys {
		attrs.PutStr("config.key."+key, value)
	}
}
