package configfile

import (
	"errors"
	"os"

	"go.uber.org/zap"
)

// PollResult holds snapshots to emit and state updates to apply after delivery.
type PollResult struct {
	Snapshots []*Snapshot
	Pending   []PendingUpdate
}

// Poller watches configured files and yields snapshots when content changes.
type Poller struct {
	cfg    PollerConfig
	state  *State
	logger *zap.Logger
}

// NewPoller builds a poller from settings.
func NewPoller(cfg PollerConfig, logger *zap.Logger) *Poller {
	if cfg.StatePath == "" {
		cfg.StatePath = DefaultStatePath
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Poller{cfg: cfg, logger: logger}
}

// LoadState reads persisted mtime/checksum state from disk.
func (p *Poller) LoadState() error {
	st, err := LoadState(p.cfg.StatePath)
	if err != nil {
		return err
	}
	p.state = st
	return nil
}

// SaveState persists current state.
func (p *Poller) SaveState() error {
	return SaveState(p.cfg.StatePath, p.state)
}

// ApplyPending commits state after successful log delivery.
func (p *Poller) ApplyPending(updates []PendingUpdate) {
	ApplyPending(p.state, updates)
}

// Poll processes all configured files.
func (p *Poller) Poll(firstRun bool) PollResult {
	opts := p.cfg.options()
	var result PollResult

	for _, entry := range p.cfg.Files {
		if entry.Path == "" {
			continue
		}
		if _, err := os.Stat(entry.Path); err != nil {
			if os.IsNotExist(err) {
				p.logger.Debug("configfile: file missing", zap.String("path", entry.Path))
				continue
			}
			p.logger.Warn("configfile: stat failed", zap.String("path", entry.Path), zap.Error(err))
			continue
		}

		snap, pending, err := ProcessEntry(entry, p.state, opts, firstRun)
		if err != nil {
			if errors.Is(err, ErrFileTooLarge) || errors.Is(err, ErrTooManyKeys) {
				p.logger.Warn("configfile: skipping file", zap.String("path", entry.Path), zap.Error(err))
				continue
			}
			p.logger.Warn("configfile: process failed", zap.String("path", entry.Path), zap.Error(err))
			continue
		}
		if snap != nil && pending != nil {
			result.Snapshots = append(result.Snapshots, snap)
			result.Pending = append(result.Pending, *pending)
			p.logger.Info("configfile: snapshot",
				zap.String("path", snap.File),
				zap.String("event", snap.Event),
				zap.Int("keys", snap.KeysTotal),
				zap.String("checksum", snap.Checksum),
			)
		}
	}
	return result
}
