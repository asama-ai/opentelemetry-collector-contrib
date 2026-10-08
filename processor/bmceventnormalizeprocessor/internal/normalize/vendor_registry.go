// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package normalize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type vendorRegistryMessage struct {
	Message  string `json:"Message"`
	Severity string `json:"Severity"`
}

type vendorRegistryFile struct {
	Messages map[string]vendorRegistryMessage `json:"Messages"`
}

// VendorRegistry resolves vendor message templates from registry JSON files.
type VendorRegistry struct {
	root string
	mu   sync.RWMutex
	cache map[string]vendorRegistryFile
}

func NewVendorRegistry(root string) *VendorRegistry {
	return &VendorRegistry{root: strings.TrimSpace(root), cache: make(map[string]vendorRegistryFile)}
}

func (v *VendorRegistry) ResolveMessage(bundle bundleFile, messageID string, args []string) string {
	if v == nil || v.root == "" || messageID == "" {
		return ""
	}
	path, ok := vendorRegistryPath(v.root, bundle)
	if !ok {
		return ""
	}

	reg, err := v.load(path)
	if err != nil {
		return ""
	}
	_, key := parseMessageID(messageID)
	msg, ok := reg.Messages[key]
	if !ok {
		return ""
	}
	return formatMessage(msg.Message, args)
}

func vendorRegistryPath(root string, bundle bundleFile) (string, bool) {
	dir := strings.TrimSpace(bundle.TestReference.RegistryDir)
	file := strings.TrimSpace(bundle.Registry.PrimaryFile)
	if dir == "" || file == "" {
		return "", false
	}
	return filepath.Join(root, dir, file), true
}

func (v *VendorRegistry) load(path string) (vendorRegistryFile, error) {
	v.mu.RLock()
	if reg, ok := v.cache[path]; ok {
		v.mu.RUnlock()
		return reg, nil
	}
	v.mu.RUnlock()

	bytes, err := os.ReadFile(path)
	if err != nil {
		return vendorRegistryFile{}, err
	}
	var reg vendorRegistryFile
	if err := json.Unmarshal(bytes, &reg); err != nil {
		return vendorRegistryFile{}, fmt.Errorf("parse vendor registry %s: %w", path, err)
	}
	v.mu.Lock()
	v.cache[path] = reg
	v.mu.Unlock()
	return reg, nil
}
