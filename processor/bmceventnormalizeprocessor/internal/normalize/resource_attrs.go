// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package normalize

import (
	"net"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/bmceventnormalizeprocessor/internal/parse"
)

// ApplyClickHouseResourceAttrs sets OTEL resource keys that map to otel.logs computed columns:
// Hostname <- host.name, SerialNumber <- serial.number, Tenant <- tenant.id (from edge).
func ApplyClickHouseResourceAttrs(resAttrs func(string) string, put func(string, string), ev parse.Event, identity Identity) {
	hostName := firstNonEmptyStr(
		identity.Hostname,
		ev.DellServerHostname,
		hostnameIfNotIP(ev.HPEHostname),
	)
	serial := firstNonEmptyStr(identity.SerialNumber, ev.LenovoSerial)
	if hostName == "" {
		hostName = resAttrs("bmc.ip")
	}

	put("host.name", hostName)
	put("serial.number", serial)
	put("bmc.hostname", hostName)
}

func hostnameIfNotIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if net.ParseIP(value) != nil {
		return ""
	}
	return value
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
