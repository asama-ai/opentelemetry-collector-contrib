// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmceventnormalizeprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor"

import (
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
)

const (
	defaultAsamaRegistryPath   = "/etc/bmc/asama-bmc-events.json"
	defaultMappingsIndexPath   = "/etc/bmc/mappings/index.json"
	defaultMappingsDir         = "/etc/bmc/mappings"
	defaultVendorRegistriesDir = "/etc/bmc/vendor-registries"
	defaultInventoryCacheTTL   = time.Hour
)

// Neo4jInventoryConfig queries Neo4j to map BMC IP to vendor/model/firmware.
type Neo4jInventoryConfig struct {
	// URL or Endpoint: bolt://host:port or http(s)://host:port for the HTTP API.
	URL      string `mapstructure:"url"`
	Endpoint string `mapstructure:"endpoint"`
	Database string `mapstructure:"database"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	// Query is a Cypher statement; $bmc_ip (or $IP) is bound to the event BMC IP.
	Query string `mapstructure:"query"`
	// Timeout for Neo4j requests.
	Timeout time.Duration `mapstructure:"timeout"`
	// CacheTTL caches successful IP lookups.
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// PrometheusInventoryConfig queries Prometheus to map BMC IP to vendor/model/firmware.
type PrometheusInventoryConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	// Query is a PromQL instant query; $IP is replaced with the event BMC IP.
	Query string `mapstructure:"query"`
	// Timeout for Prometheus HTTP requests.
	Timeout time.Duration `mapstructure:"timeout"`
	// CacheTTL caches successful IP lookups.
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// IdentityConfig resolves vendor/model/firmware when not present on the log resource.
type IdentityConfig struct {
	Neo4j             Neo4jInventoryConfig      `mapstructure:"neo4j"`
	Prometheus        PrometheusInventoryConfig `mapstructure:"prometheus"`
	IndexIPLookup     *bool                     `mapstructure:"index_ip_lookup"`
	MessageIDFallback *bool                     `mapstructure:"message_id_fallback"`
}

// Config configures the BMC event normalize processor.
type Config struct {
	AsamaRegistryPath   string         `mapstructure:"asama_registry_path"`
	MappingsIndexPath   string         `mapstructure:"mappings_index_path"`
	MappingsDir         string         `mapstructure:"mappings_dir"`
	VendorRegistriesDir string         `mapstructure:"vendor_registries_dir"`
	Identity            IdentityConfig `mapstructure:"identity"`
}

func createDefaultConfig() component.Config {
	indexIP := true
	messageID := true
	return &Config{
		AsamaRegistryPath:   defaultAsamaRegistryPath,
		MappingsIndexPath:   defaultMappingsIndexPath,
		MappingsDir:         defaultMappingsDir,
		VendorRegistriesDir: defaultVendorRegistriesDir,
		Identity: IdentityConfig{
			IndexIPLookup:     &indexIP,
			MessageIDFallback: &messageID,
			Neo4j: Neo4jInventoryConfig{
				CacheTTL: defaultInventoryCacheTTL,
			},
			Prometheus: PrometheusInventoryConfig{
				CacheTTL: defaultInventoryCacheTTL,
			},
		},
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

func (c *Config) indexIPLookupEnabled() bool {
	if c.Identity.IndexIPLookup == nil {
		return true
	}
	return *c.Identity.IndexIPLookup
}

func (c *Config) messageIDFallbackEnabled() bool {
	if c.Identity.MessageIDFallback == nil {
		return true
	}
	return *c.Identity.MessageIDFallback
}
