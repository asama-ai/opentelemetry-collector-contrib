// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import (
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
)

// ChangelogExport configures the OTLP HTTP logs destination for change events.
type ChangelogExport struct {
	Endpoint string            `mapstructure:"endpoint"`
	Headers  map[string]string `mapstructure:"headers"`
	Timeout  time.Duration     `mapstructure:"timeout"`
}

// Config is the inventorydiff processor configuration.
type Config struct {
	Metrics   []string        `mapstructure:"metrics"`
	Changelog ChangelogExport `mapstructure:"changelog"`
}

func createDefaultConfig() component.Config {
	return &Config{
		Changelog: ChangelogExport{Timeout: 10 * time.Second},
	}
}

// Validate checks required fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is nil")
	}
	if len(c.Metrics) == 0 {
		return errors.New("metrics list must not be empty")
	}
	for i, m := range c.Metrics {
		if m == "" {
			return fmt.Errorf("metrics[%d] must not be empty", i)
		}
	}
	if c.Changelog.Endpoint == "" {
		return errors.New("changelog.endpoint is required")
	}
	return nil
}
