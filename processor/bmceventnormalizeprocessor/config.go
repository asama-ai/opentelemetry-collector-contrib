// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmceventnormalizeprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor"

import (
	"errors"

	"go.opentelemetry.io/collector/component"
)

const (
	defaultAsamaRegistryPath = "/etc/bmc/asama-bmc-events.json"
	defaultMappingsIndexPath = "/etc/bmc/mappings/index.json"
	defaultMappingsDir       = "/etc/bmc/mappings"
)

// Config configures the BMC event normalize processor.
type Config struct {
	AsamaRegistryPath string `mapstructure:"asama_registry_path"`
	MappingsIndexPath string `mapstructure:"mappings_index_path"`
	MappingsDir       string `mapstructure:"mappings_dir"`
}

func createDefaultConfig() component.Config {
	return &Config{
		AsamaRegistryPath: defaultAsamaRegistryPath,
		MappingsIndexPath: defaultMappingsIndexPath,
		MappingsDir:       defaultMappingsDir,
	}
}

func (c *Config) Validate() error {
	if c.AsamaRegistryPath == "" {
		return errors.New("asama_registry_path must be set")
	}
	if c.MappingsIndexPath == "" {
		return errors.New("mappings_index_path must be set")
	}
	if c.MappingsDir == "" {
		return errors.New("mappings_dir must be set")
	}
	return nil
}
