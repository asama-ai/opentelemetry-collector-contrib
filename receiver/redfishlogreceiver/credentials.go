// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfishlogreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver"

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/collector/config/configopaque"

	"gopkg.in/yaml.v3"
)

type credsYAML struct {
	Default struct {
		Username string              `yaml:"username"`
		Password configopaque.String `yaml:"password"`
	} `yaml:"default"`
	Hosts map[string]struct {
		Username string              `yaml:"username"`
		Password configopaque.String `yaml:"password"`
	} `yaml:"hosts"`
}

type credentialsStore struct {
	defaultUser string
	defaultPwd  configopaque.String
	hosts       map[string]struct {
		username string
		password configopaque.String
	}
}

func loadCredentialsFile(path string) (*credentialsStore, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	var raw credsYAML
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse credentials yaml: %w", err)
	}
	cs := &credentialsStore{
		defaultUser: strings.TrimSpace(raw.Default.Username),
		defaultPwd:  raw.Default.Password,
		hosts: make(map[string]struct {
			username string
			password configopaque.String
		}),
	}
	for k, v := range raw.Hosts {
		cs.hosts[strings.TrimSpace(k)] = struct {
			username string
			password configopaque.String
		}{username: strings.TrimSpace(v.Username), password: v.Password}
	}
	if cs.defaultUser == "" {
		return nil, fmt.Errorf("credentials file %q: default.username is required", path)
	}
	return cs, nil
}

func (c *credentialsStore) resolve(hostKey string) (user string, pwd configopaque.String, err error) {
	hostKey = strings.TrimSpace(hostKey)
	if hostKey == "" {
		return "", "", fmt.Errorf("empty credential lookup key")
	}
	if h, ok := c.hosts[hostKey]; ok {
		if h.username == "" {
			return "", "", fmt.Errorf("no username for host %q", hostKey)
		}
		return h.username, h.password, nil
	}
	return c.defaultUser, c.defaultPwd, nil
}

func credentialKeyForTarget(t TargetConfig) string {
	if t.CredentialKey != "" {
		return t.CredentialKey
	}
	u, err := url.Parse(t.Endpoint)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
