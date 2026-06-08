// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver"

import (
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.uber.org/multierr"
)

const (
	defaultReadTimeout        = "10s"
	defaultWriteTimeout       = "10s"
	defaultPath               = "/redfish/events"
	defaultHealthPath         = "/health_check"
	defaultMaxRequestBodySize = 1024 * 1024 // 1MB
)

// SourceHeaderKeys maps logical BMC fields to HTTP header names for enrichment.
type SourceHeaderKeys struct {
	Vendor         string `mapstructure:"vendor"`
	IP             string `mapstructure:"ip"`
	Model          string `mapstructure:"model"`
	Firmware       string `mapstructure:"firmware"`
	Hostname       string `mapstructure:"hostname"`
	Tenant         string `mapstructure:"tenant"`
	SenderAddress  string `mapstructure:"sender_address"`
}

// Config defines configuration for the Redfish EventService push receiver.
type Config struct {
	confighttp.ServerConfig `mapstructure:",squash"`
	ReadTimeout             string           `mapstructure:"read_timeout"`
	WriteTimeout            string           `mapstructure:"write_timeout"`
	Path                    string           `mapstructure:"path"`
	HealthPath              string           `mapstructure:"health_path"`
	SourceHeaders           SourceHeaderKeys `mapstructure:"source_headers"`
}

func createDefaultConfig() component.Config {
	return &Config{
		ServerConfig: confighttp.ServerConfig{
			MaxRequestBodySize: defaultMaxRequestBodySize,
		},
		ReadTimeout:  defaultReadTimeout,
		WriteTimeout: defaultWriteTimeout,
		Path:         defaultPath,
		HealthPath:   defaultHealthPath,
		SourceHeaders: SourceHeaderKeys{
			Vendor:        "X-BMC-Vendor",
			IP:            "X-BMC-IP",
			Model:         "X-BMC-Model",
			Firmware:      "X-BMC-Firmware",
			Hostname:      "X-BMC-Hostname",
			Tenant:        "X-Tenant",
			SenderAddress: "Sender-Address",
		},
	}
}

func (cfg *Config) Validate() error {
	var errs error
	maxReadWriteTimeout, _ := time.ParseDuration("10s")

	if cfg.Endpoint == "" {
		errs = multierr.Append(errs, errors.New("missing receiver server endpoint from config"))
	}

	if cfg.ReadTimeout != "" {
		readTimeout, err := time.ParseDuration(cfg.ReadTimeout)
		if err != nil {
			errs = multierr.Append(errs, err)
		} else if readTimeout > maxReadWriteTimeout {
			errs = multierr.Append(errs, errors.New("read_timeout exceeds maximum allowed value of 10s"))
		}
	}

	if cfg.WriteTimeout != "" {
		writeTimeout, err := time.ParseDuration(cfg.WriteTimeout)
		if err != nil {
			errs = multierr.Append(errs, err)
		} else if writeTimeout > maxReadWriteTimeout {
			errs = multierr.Append(errs, errors.New("write_timeout exceeds maximum allowed value of 10s"))
		}
	}

	if cfg.MaxRequestBodySize == 0 {
		cfg.MaxRequestBodySize = int64(defaultMaxRequestBodySize)
	}

	if cfg.Path == "" {
		errs = multierr.Append(errs, errors.New("path must be set"))
	}
	if cfg.HealthPath == "" {
		errs = multierr.Append(errs, errors.New("health_path must be set"))
	}

	return errs
}
