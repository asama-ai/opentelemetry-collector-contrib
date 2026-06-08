// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package normalize

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/parse"
	"github.com/stretchr/testify/require"
)

func TestApplyClickHouseResourceAttrs(t *testing.T) {
	attrs := map[string]string{
		"bmc.ip":     "10.25.40.207",
		"tenant.id":  "nxtgen",
	}
	put := func(key, value string) {
		if value != "" {
			attrs[key] = value
		}
	}
	get := func(key string) string {
		return attrs[key]
	}

	ApplyClickHouseResourceAttrs(get, put, parse.Event{
		HPEHostname: "10.25.40.207",
	}, Identity{
		Hostname:     "nxtegn-test-02",
		SerialNumber: "SGH810WXP1",
	})

	require.Equal(t, "nxtegn-test-02", attrs["host.name"])
	require.Equal(t, "SGH810WXP1", attrs["serial.number"])
	require.Equal(t, "nxtegn-test-02", attrs["bmc.hostname"])
}

func TestApplyClickHouseResourceAttrsDellHostname(t *testing.T) {
	attrs := map[string]string{"bmc.ip": "10.25.40.206"}
	put := func(key, value string) {
		if value != "" {
			attrs[key] = value
		}
	}
	ApplyClickHouseResourceAttrs(func(k string) string { return attrs[k] }, put, parse.Event{
		DellServerHostname: "WIN-K3TVDQPKNMU",
	}, Identity{})

	require.Equal(t, "WIN-K3TVDQPKNMU", attrs["host.name"])
}

func TestApplyClickHouseResourceAttrsFallbackToIP(t *testing.T) {
	attrs := map[string]string{"bmc.ip": "10.25.40.207"}
	put := func(key, value string) {
		if value != "" {
			attrs[key] = value
		}
	}
	ApplyClickHouseResourceAttrs(func(k string) string { return attrs[k] }, put, parse.Event{
		HPEHostname: "10.25.40.207",
	}, Identity{})

	require.Equal(t, "10.25.40.207", attrs["host.name"])
}
