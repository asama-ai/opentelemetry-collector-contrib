// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bmcfaultsignalprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmcfaultsignalprocessor"

import (
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
)

const (
	defaultFaultCatalogPath = "/etc/bmc/fault-eligible-events.json"
	defaultEndpoint         = "http://localhost:8080/api/v1/faults/ingest/bmc"
	defaultTimeout          = 5 * time.Second
	defaultTenantHeader     = "X-Tenant"
)

// Config configures the BMC fault signal processor.
type Config struct {
	FaultCatalogPath string               `mapstructure:"fault_catalog_path"`
	Endpoint         string               `mapstructure:"endpoint"`
	Tenant           string               `mapstructure:"tenant"`
	TenantHeader     string               `mapstructure:"tenant_header"`
	TenantAttribute  string               `mapstructure:"tenant_attribute"`
	Timeout          time.Duration        `mapstructure:"timeout"`
	ClientConfig     confighttp.ClientConfig `mapstructure:",squash"`
}

func createDefaultConfig() component.Config {
	return &Config{
		FaultCatalogPath: defaultFaultCatalogPath,
		Endpoint:         defaultEndpoint,
		TenantHeader:     defaultTenantHeader,
		TenantAttribute:  "tenant.id",
		Timeout:          defaultTimeout,
	}
}

func (c *Config) Validate() error {
	if c.FaultCatalogPath == "" {
		return errors.New("fault_catalog_path must be set")
	}
	if c.Endpoint == "" {
		return errors.New("endpoint must be set")
	}
	if c.Tenant == "" && c.TenantAttribute == "" {
		return errors.New("tenant or tenant_attribute must be set")
	}
	if c.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if c.TenantHeader == "" {
		return errors.New("tenant_header must be set")
	}
	return nil
}
