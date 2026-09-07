// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configwatchsnapshot // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/configwatchsnapshot"

import (
	"go.opentelemetry.io/collector/component"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/configfilereceiver"
)

// Config controls snapshot flatten/redaction for actor-triggered reads.
type Config struct {
	// Files is the allowlist of absolute paths this processor may read.
	// Empty means no snapshots (fail closed). Must match ConfigWatch / configfile receiver lists.
	Files          []string `mapstructure:"files"`
	MaxKeysPerFile int      `mapstructure:"max_keys_per_file"`
	ExcludeKeys    []string `mapstructure:"exclude_keys"`
}

func createDefaultConfig() component.Config {
	return &Config{
		MaxKeysPerFile: configfilereceiver.DefaultMaxKeysPerFile,
		ExcludeKeys:    configfilereceiver.DefaultExcludeKeys,
	}
}
