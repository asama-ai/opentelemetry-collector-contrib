package configfile

import (
	"os"
)

// Snapshot is a parsed config file ready for OTLP export.
type Snapshot struct {
	File      string
	Format    string
	Checksum  string
	Keys      map[string]string
	KeysTotal int
	Event     string
}

// Event type for config snapshots.
const (
	EventInitial = "initial"
	EventChanged = "changed"
)

// PendingUpdate holds state to commit after downstream delivery succeeds.
type PendingUpdate struct {
	Path  string
	State FileState
}

// BuildSnapshot parses path and returns a snapshot without comparing state.
func BuildSnapshot(path, format string, opts Options, event string) (*Snapshot, error) {
	resolved, keys, err := ParseFile(path, format, opts)
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		File:      path,
		Format:    resolved,
		Checksum:  Checksum(keys),
		Keys:      keys,
		KeysTotal: len(keys),
		Event:     event,
	}, nil
}

// ProcessEntry evaluates one configured file against state. Returns a snapshot and
// a pending state update when a log should be emitted; the caller must apply the
// pending update only after successful downstream delivery.
func ProcessEntry(entry FileEntry, st *State, opts Options, firstRun bool) (*Snapshot, *PendingUpdate, error) {
	if entry.Path == "" {
		return nil, nil, nil
	}

	info, err := os.Stat(entry.Path)
	if err != nil {
		return nil, nil, err
	}

	prev, hasPrev := st.Files[entry.Path]
	mtimeNS := info.ModTime().UnixNano()
	size := info.Size()

	if hasPrev && prev.MtimeNS == mtimeNS && prev.Size == size {
		return nil, nil, nil
	}

	snap, err := BuildSnapshot(entry.Path, entry.Format, opts, EventChanged)
	if err != nil {
		return nil, nil, err
	}
	if firstRun || !hasPrev {
		snap.Event = EventInitial
	}

	if hasPrev && snap.Checksum == prev.Checksum {
		st.Files[entry.Path] = FileState{
			MtimeNS:  mtimeNS,
			Size:     size,
			Checksum: prev.Checksum,
		}
		return nil, nil, nil
	}

	return snap, &PendingUpdate{
		Path: entry.Path,
		State: FileState{
			MtimeNS:  mtimeNS,
			Size:     size,
			Checksum: snap.Checksum,
		},
	}, nil
}

// ApplyPending merges delivered snapshot state into st.
func ApplyPending(st *State, updates []PendingUpdate) {
	for _, u := range updates {
		st.Files[u.Path] = u.State
	}
}
