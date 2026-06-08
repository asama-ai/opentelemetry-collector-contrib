// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfisheventreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver"

import (
	"encoding/json"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

type sourceContext struct {
	IP             string
	Tenant         string
	SenderAddress  string
}

// payloadToLogs wraps the raw Redfish POST body for downstream parsing in bmceventnormalize.
func payloadToLogs(body []byte, src sourceContext) (plog.Logs, int, error) {
	if len(body) == 0 {
		return plog.NewLogs(), 0, nil
	}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	resAttrs := rl.Resource().Attributes()
	putIfNonEmpty(resAttrs, "bmc.ip", src.IP)
	putIfNonEmpty(resAttrs, "tenant.id", src.Tenant)

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfisheventreceiver")

	lr := sl.LogRecords().AppendEmpty()
	attrs := lr.Attributes()
	attrs.PutStr("redfish.raw_payload", string(body))
	putIfNonEmpty(attrs, "http.sender_address", src.SenderAddress)
	if ctx := topLevelContext(body); ctx != "" {
		attrs.PutStr("redfish.context", enrichContext(ctx, src.IP))
	}

	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().UTC()))
	lr.Body().SetStr("redfish-event")

	return ld, 1, nil
}

func topLevelContext(body []byte) string {
	var envelope struct {
		Context string `json:"Context"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Context)
}

// resolveBMCSourceIP picks BMC IP: header override, Sender-Address, JSON Oem.Hpe.Hostname peek, then HTTP remote.
func resolveBMCSourceIP(headerIP, senderAddress, remoteAddr string, body []byte) string {
	if ip := normalizeIP(headerIP); ip != "" {
		return ip
	}
	if ip := normalizeIP(senderAddress); ip != "" {
		return ip
	}
	if ip := hpeHostnameFromBody(body); ip != "" {
		return ip
	}
	return hostFromRemoteAddr(remoteAddr)
}

func hpeHostnameFromBody(body []byte) string {
	var payload struct {
		Events []struct {
			Oem struct {
				Hpe struct {
					Hostname string `json:"Hostname"`
				} `json:"Hpe"`
			} `json:"Oem"`
		} `json:"Events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, ev := range payload.Events {
		if ip := strings.TrimSpace(ev.Oem.Hpe.Hostname); ip != "" {
			return normalizeIP(ip)
		}
	}
	return ""
}

func putIfNonEmpty(attrs pcommon.Map, key, value string) {
	if value != "" {
		attrs.PutStr(key, value)
	}
}

// enrichContext merges the Redfish subscription Context with the resolved BMC IP.
func enrichContext(eventContext, bmcIP string) string {
	bmcIP = strings.TrimSpace(bmcIP)
	eventContext = strings.TrimSpace(eventContext)
	if bmcIP == "" {
		return eventContext
	}
	ipPart := "bmc_ip=" + bmcIP
	if eventContext == "" {
		return ipPart
	}
	if strings.Contains(eventContext, "bmc_ip=") {
		return eventContext
	}
	return eventContext + "|" + ipPart
}

func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		if host == "::1" {
			return "127.0.0.1"
		}
		return strings.Trim(host, "[]")
	}
	return strings.Trim(ip, "[]")
}

func hostFromRemoteAddr(remoteAddr string) string {
	return normalizeIP(remoteAddr)
}
