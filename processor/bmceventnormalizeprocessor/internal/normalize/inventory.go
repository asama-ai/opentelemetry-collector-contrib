// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package normalize

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Identity holds resolved BMC vendor/model/firmware for bundle selection.
type Identity struct {
	Vendor          string
	BMCModel        string
	FirmwareVersion string
	BundleID        string
	Source          string
}

// InventoryResolver resolves BMC identity from IP and optional MessageId fallback.
type InventoryResolver struct {
	promEndpoint    string
	promQuery       string
	promHTTP        *http.Client
	promCache       map[string]promCacheEntry
	promCacheMu     sync.RWMutex
	promCacheTTL    time.Duration
	messageIDLookup bool
	indexIPLookup   bool

	engine *Engine
}

type promCacheEntry struct {
	identity Identity
	expires  time.Time
}

type promQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
		} `json:"result"`
	} `json:"data"`
}

const (
	identitySourcePrometheus = "prometheus"
	identitySourceIndexIP    = "index_ip"
	identitySourceMessageID  = "message_id"
)

// InventoryConfig configures optional identity resolution.
type InventoryConfig struct {
	PrometheusEndpoint string
	PrometheusQuery    string
	PrometheusTimeout  time.Duration
	PrometheusCacheTTL time.Duration
	IndexIPLookup      bool
	MessageIDFallback  bool
}

// NewInventoryResolver creates a resolver; engine must be set before Resolve.
func NewInventoryResolver(cfg InventoryConfig) *InventoryResolver {
	timeout := cfg.PrometheusTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	cacheTTL := cfg.PrometheusCacheTTL
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute
	}
	query := cfg.PrometheusQuery
	if query == "" && cfg.PrometheusEndpoint != "" {
		query = `last_over_time(redfish_bmc_manager_info{target=~"$IP(:[0-9]+)?"}[24h])`
	}
	return &InventoryResolver{
		promEndpoint:    strings.TrimRight(cfg.PrometheusEndpoint, "/"),
		promQuery:       query,
		promHTTP:        &http.Client{Timeout: timeout},
		promCache:       make(map[string]promCacheEntry),
		promCacheTTL:    cacheTTL,
		indexIPLookup:   cfg.IndexIPLookup,
		messageIDLookup: cfg.MessageIDFallback,
	}
}

func (r *InventoryResolver) SetEngine(engine *Engine) {
	r.engine = engine
}

// Resolve fills missing vendor/model/firmware using IP and MessageId fallbacks.
func (r *InventoryResolver) Resolve(bmcIP, messageID, vendor, bmcModel, firmware string) Identity {
	if vendor != "" && bmcModel != "" && firmware != "" {
		return Identity{Vendor: vendor, BMCModel: bmcModel, FirmwareVersion: firmware}
	}
	if r.engine == nil {
		return Identity{Vendor: vendor, BMCModel: bmcModel, FirmwareVersion: firmware}
	}

	bmcIP = normalizeIP(bmcIP)
	if bmcIP != "" {
		if id, ok := r.lookupPrometheus(bmcIP); ok {
			return mergeIdentity(vendor, bmcModel, firmware, id)
		}
		if r.indexIPLookup {
			if id, ok := r.engine.lookupByIndexIP(bmcIP); ok {
				id = mergeIdentity(vendor, bmcModel, firmware, id)
				if id.Source == "" {
					id.Source = identitySourceIndexIP
				}
				return id
			}
		}
	}

	if r.messageIDLookup && messageID != "" {
		if id, ok := r.engine.lookupByUniqueMessageID(messageID); ok {
			id = mergeIdentity(vendor, bmcModel, firmware, id)
			if id.Source == "" {
				id.Source = identitySourceMessageID
			}
			return id
		}
	}

	return Identity{Vendor: vendor, BMCModel: bmcModel, FirmwareVersion: firmware}
}

func mergeIdentity(vendor, model, firmware string, id Identity) Identity {
	if vendor != "" {
		id.Vendor = vendor
	}
	if model != "" {
		id.BMCModel = model
	}
	if firmware != "" {
		id.FirmwareVersion = firmware
	}
	return id
}

func (r *InventoryResolver) lookupPrometheus(bmcIP string) (Identity, bool) {
	if r.promEndpoint == "" || r.promQuery == "" {
		return Identity{}, false
	}

	r.promCacheMu.RLock()
	if entry, ok := r.promCache[bmcIP]; ok && time.Now().Before(entry.expires) {
		r.promCacheMu.RUnlock()
		return entry.identity, true
	}
	r.promCacheMu.RUnlock()

	query := strings.ReplaceAll(r.promQuery, "$IP", bmcIP)
	endpoint, err := url.Parse(r.promEndpoint + "/api/v1/query")
	if err != nil {
		return Identity{}, false
	}
	q := endpoint.Query()
	q.Set("query", query)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return Identity{}, false
	}
	resp, err := r.promHTTP.Do(req)
	if err != nil {
		return Identity{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Identity{}, false
	}

	var parsed promQueryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Identity{}, false
	}
	if parsed.Status != "success" || len(parsed.Data.Result) == 0 {
		return Identity{}, false
	}

	labels := parsed.Data.Result[0].Metric
	id, ok := r.engine.matchBundleFromLabels(
		labels["manufacturer"],
		labels["model"],
		labels["firmware_version"],
	)
	if !ok {
		return Identity{}, false
	}
	id.Source = identitySourcePrometheus

	r.promCacheMu.Lock()
	r.promCache[bmcIP] = promCacheEntry{identity: id, expires: time.Now().Add(r.promCacheTTL)}
	r.promCacheMu.Unlock()
	return id, true
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
