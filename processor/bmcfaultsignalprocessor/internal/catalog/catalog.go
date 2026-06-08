// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// EventRule describes a fault-eligible Asama event.
type EventRule struct {
	AsamaID         string `json:"asama_id"`
	AsamaMessageKey string `json:"asama_message_key"`
	FaultType       string `json:"fault_type"`
	CreateFault     bool   `json:"create_fault"`
}

// Catalog loads fault-eligible-events.json and lifecycle rules.
type Catalog struct {
	openLifecycles  map[string]struct{}
	closeLifecycles map[string]struct{}
	events          map[string]EventRule
}

type faultCatalogFile struct {
	FaultRules struct {
		OpenOnLifecycle  []string `json:"open_on_lifecycle"`
		CloseOnLifecycle []string `json:"close_on_lifecycle"`
	} `json:"fault_rules"`
	Categories map[string]struct {
		Events []EventRule `json:"events"`
	} `json:"categories"`
}

func Load(path string) (*Catalog, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fault catalog: %w", err)
	}
	var raw faultCatalogFile
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return nil, fmt.Errorf("parse fault catalog: %w", err)
	}

	c := &Catalog{
		openLifecycles:  make(map[string]struct{}),
		closeLifecycles: make(map[string]struct{}),
		events:          make(map[string]EventRule),
	}
	for _, lc := range raw.FaultRules.OpenOnLifecycle {
		c.openLifecycles[lc] = struct{}{}
	}
	for _, lc := range raw.FaultRules.CloseOnLifecycle {
		c.closeLifecycles[lc] = struct{}{}
	}
	for _, cat := range raw.Categories {
		for _, ev := range cat.Events {
			if ev.AsamaID != "" {
				c.events[ev.AsamaID] = ev
			}
		}
	}
	return c, nil
}

func (c *Catalog) Action(asamaID, lifecycle string) (string, EventRule, bool) {
	rule, ok := c.events[asamaID]
	if !ok || !rule.CreateFault {
		return "", EventRule{}, false
	}
	if _, ok := c.openLifecycles[lifecycle]; ok {
		return "open", rule, true
	}
	if _, ok := c.closeLifecycles[lifecycle]; ok {
		return "close", rule, true
	}
	return "", EventRule{}, false
}

func ContainsLifecycle(set map[string]struct{}, lifecycle string) bool {
	_, ok := set[lifecycle]
	return ok
}

func LifecycleIn(list []string, lifecycle string) bool {
	return slices.Contains(list, lifecycle)
}
