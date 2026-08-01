// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package redfishlogreceiver implements a logs receiver that scrapes Redfish LogService
// entries from BMCs using the gofish client library.
package redfishlogreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver"

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"go.opentelemetry.io/collector/component"
)

const (
	defaultPollInterval      = time.Minute
	defaultTimeout           = 60 * time.Second
	defaultInitialLastN      = 500
	defaultLogResetClockSkew = 5 * time.Minute
)

// TargetConfig identifies one BMC to scrape.
type TargetConfig struct {
	// Endpoint is the Redfish base URL, e.g. https://10.0.0.1 or https://bmc.example.com:443
	Endpoint string `mapstructure:"endpoint"`
	// CredentialKey selects credentials from the external file's hosts map; if empty, derived from Endpoint host.
	CredentialKey string `mapstructure:"credential_key"`
}

// Config configures the redfishlog receiver.
type Config struct {
	CredentialsFile string         `mapstructure:"credentials_file"`
	Targets         []TargetConfig `mapstructure:"targets"`
	PollInterval    time.Duration  `mapstructure:"poll_interval"`
	Timeout         time.Duration  `mapstructure:"timeout"`
	// InsecureSkipVerify disables TLS certificate verification for Redfish HTTPS.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`
	// InitialLastN is how many newest log entries to ingest when there is no checkpoint or after a log reset.
	InitialLastN int `mapstructure:"initial_last_n"`
	// LogResetClockSkew is added to max(entry.Created) when comparing against the checkpoint timestamp to detect clears.
	LogResetClockSkew time.Duration `mapstructure:"log_reset_clock_skew"`
	// StorageID references a storage extension (for example file_storage) used to persist per-target checkpoints.
	StorageID *component.ID `mapstructure:"storage"`
}

func (c *Config) Validate() error {
	var errs error
	if c.CredentialsFile == "" {
		errs = errors.Join(errs, errors.New("credentials_file must be set"))
	}
	if len(c.Targets) == 0 {
		errs = errors.Join(errs, errors.New("targets must not be empty"))
	}
	if c.PollInterval < time.Second {
		errs = errors.Join(errs, errors.New("poll_interval must be at least 1s"))
	}
	if c.InitialLastN < 1 {
		errs = errors.Join(errs, errors.New("initial_last_n must be at least 1"))
	}
	if c.Timeout < time.Second {
		errs = errors.Join(errs, errors.New("timeout must be at least 1s"))
	}
	if c.LogResetClockSkew < 0 {
		errs = errors.Join(errs, errors.New("log_reset_clock_skew must be non-negative"))
	}
	for i, t := range c.Targets {
		if t.Endpoint == "" {
			errs = errors.Join(errs, fmt.Errorf("targets[%d].endpoint must be set", i))
			continue
		}
		u, err := url.Parse(t.Endpoint)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = errors.Join(errs, fmt.Errorf("targets[%d].endpoint must be a valid URL with scheme and host", i))
		}
	}
	return errs
}

func createDefaultConfig() *Config {
	pi := defaultPollInterval
	to := defaultTimeout
	return &Config{
		PollInterval:      pi,
		Timeout:           to,
		InitialLastN:      defaultInitialLastN,
		LogResetClockSkew: defaultLogResetClockSkew,
		Targets:           nil,
	}
}
