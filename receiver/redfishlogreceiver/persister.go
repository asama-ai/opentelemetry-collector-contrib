// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfishlogreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/extension/xextension/storage"
	"go.uber.org/zap"
)

const checkpointKeyPrefix = "redfishlog/"

type checkpointState struct {
	Created        string `json:"created"`
	EntryID        string `json:"entry_id"`
	LastEntryCount int    `json:"last_entry_count,omitempty"`
}

type checkpointPersister struct {
	client storage.Client
	logger *zap.Logger
}

func newCheckpointPersister(client storage.Client, logger *zap.Logger) *checkpointPersister {
	return &checkpointPersister{client: client, logger: logger}
}

func checkpointStorageKey(targetKey, logServiceODataID string) string {
	safe := strings.ReplaceAll(strings.TrimPrefix(logServiceODataID, "/"), "/", "_")
	if len(safe) > 200 {
		safe = safe[:200]
	}
	return fmt.Sprintf("%s%s/%s", checkpointKeyPrefix, targetKey, safe)
}

func (p *checkpointPersister) get(ctx context.Context, key string) (*checkpointState, error) {
	if p.client == nil {
		return nil, errors.New("storage client is nil")
	}
	data, err := p.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var st checkpointState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	return &st, nil
}

func (p *checkpointPersister) set(ctx context.Context, key string, st *checkpointState) error {
	if p.client == nil {
		return errors.New("storage client is nil")
	}
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	if err := p.client.Set(ctx, key, data); err != nil {
		return fmt.Errorf("set checkpoint: %w", err)
	}
	if p.logger != nil {
		p.logger.Debug("checkpoint saved", zap.String("key", key))
	}
	return nil
}

func (p *checkpointPersister) close(ctx context.Context) error {
	if p.client == nil {
		return nil
	}
	return p.client.Close(ctx)
}
