// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Identity holds resolved BMC vendor/model/firmware for bundle selection.
type Identity struct {
	Vendor          string
	BMCModel        string
	FirmwareVersion string
	BundleID        string
	Hostname        string
	SerialNumber    string
	Source          string
}

// InventoryResolver resolves BMC identity from IP and optional MessageId fallback.
type InventoryResolver struct {
	neo4jEndpoint  string
	neo4jDatabase  string
	neo4jUsername  string
	neo4jPassword  string
	neo4jQuery     string
	promEndpoint   string
	promQuery      string
	httpClient     *http.Client
	lookupCache    map[string]lookupCacheEntry
	lookupCacheMu  sync.RWMutex
	cacheTTL       time.Duration
	messageIDLookup bool
	indexIPLookup   bool

	engine *Engine
}

type lookupCacheEntry struct {
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

type neo4jCommitRequest struct {
	Statements []neo4jStatement `json:"statements"`
}

type neo4jStatement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters"`
}

type neo4jCommitResponse struct {
	Results []struct {
		Columns []string `json:"columns"`
		Data    []struct {
			Row []any `json:"row"`
		} `json:"data"`
	} `json:"results"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

const (
	identitySourceNeo4j      = "neo4j"
	identitySourcePrometheus = "prometheus"
	identitySourceIndexIP    = "index_ip"
	identitySourceMessageID  = "message_id"

	defaultNeo4jDatabase = "neo4j"
	defaultNeo4jQuery      = `MATCH (d:Device)
WITH d, split(coalesce(d.oob_ip, d.out_of_band_ip, d.bmc_ip, ''), '/')[0] AS oob_host
WHERE toLower(oob_host) = toLower(split($bmc_ip, '/')[0])
OPTIONAL MATCH (d)-[:HAS_TYPE]->(:DeviceType)-[:MANUFACTURED_BY]->(m:Manufacturer)
OPTIONAL MATCH (d)-[:HAS_BMC_SLOT]->(:BMCSlot)-[:HAS_BMC]->(:BMC)-[:IS_OF_TYPE]->(bt:BmcType)
RETURN coalesce(d.hostname, d.name, d.host_name) AS hostname,
       coalesce(d.serial_number, d.serial, d.service_tag) AS serial_number,
       m.name AS manufacturer,
       coalesce(bt.part_number, bt.bmc_id) AS model,
       bt.name AS firmware_version
LIMIT 1`
)

// InventoryConfig configures optional identity resolution.
type InventoryConfig struct {
	Neo4jEndpoint  string
	Neo4jDatabase  string
	Neo4jUsername  string
	Neo4jPassword  string
	Neo4jQuery     string
	Neo4jTimeout   time.Duration
	Neo4jCacheTTL  time.Duration
	PrometheusEndpoint string
	PrometheusQuery    string
	PrometheusTimeout  time.Duration
	PrometheusCacheTTL time.Duration
	IndexIPLookup      bool
	MessageIDFallback  bool
}

// NewInventoryResolver creates a resolver; engine must be set before Resolve.
func NewInventoryResolver(cfg InventoryConfig) *InventoryResolver {
	timeout := cfg.Neo4jTimeout
	if timeout == 0 {
		timeout = cfg.PrometheusTimeout
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	cacheTTL := cfg.Neo4jCacheTTL
	if cacheTTL == 0 {
		cacheTTL = cfg.PrometheusCacheTTL
	}
	if cacheTTL == 0 {
		cacheTTL = time.Hour
	}

	neo4jQuery := cfg.Neo4jQuery
	if neo4jQuery == "" && cfg.Neo4jEndpoint != "" {
		neo4jQuery = defaultNeo4jQuery
	}

	promQuery := cfg.PrometheusQuery
	if promQuery == "" && cfg.PrometheusEndpoint != "" {
		promQuery = `last_over_time(redfish_bmc_manager_info{instance="$IP"}[24h])`
	}

	database := strings.TrimSpace(cfg.Neo4jDatabase)
	if database == "" {
		database = defaultNeo4jDatabase
	}

	return &InventoryResolver{
		neo4jEndpoint:   strings.TrimRight(cfg.Neo4jEndpoint, "/"),
		neo4jDatabase:   database,
		neo4jUsername:   cfg.Neo4jUsername,
		neo4jPassword:   cfg.Neo4jPassword,
		neo4jQuery:      neo4jQuery,
		promEndpoint:    strings.TrimRight(cfg.PrometheusEndpoint, "/"),
		promQuery:       promQuery,
		httpClient:      &http.Client{Timeout: timeout},
		lookupCache:     make(map[string]lookupCacheEntry),
		cacheTTL:        cacheTTL,
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
		if id, ok := r.lookupNeo4j(bmcIP); ok {
			return r.finalizeExternalLookup(bmcIP, vendor, bmcModel, firmware, id)
		}
		if id, ok := r.lookupPrometheus(bmcIP); ok {
			return r.finalizeExternalLookup(bmcIP, vendor, bmcModel, firmware, id)
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

func (r *InventoryResolver) finalizeExternalLookup(bmcIP, vendor, bmcModel, firmware string, id Identity) Identity {
	id = mergeIdentity(vendor, bmcModel, firmware, id)
	if id.BundleID == "" && r.indexIPLookup {
		if idx, ok := r.engine.lookupByIndexIP(bmcIP); ok {
			if id.Vendor == "" {
				id.Vendor = idx.Vendor
			}
			if id.BMCModel == "" {
				id.BMCModel = idx.BMCModel
			}
			if id.FirmwareVersion == "" {
				id.FirmwareVersion = idx.FirmwareVersion
			}
			id.BundleID = idx.BundleID
			if id.Source == "" {
				id.Source = identitySourceIndexIP
			}
		}
	}
	return id
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

func (r *InventoryResolver) cachedLookup(bmcIP string) (Identity, bool) {
	r.lookupCacheMu.RLock()
	defer r.lookupCacheMu.RUnlock()
	entry, ok := r.lookupCache[bmcIP]
	if !ok || time.Now().After(entry.expires) {
		return Identity{}, false
	}
	return entry.identity, true
}

func (r *InventoryResolver) storeLookup(bmcIP string, id Identity) {
	r.lookupCacheMu.Lock()
	r.lookupCache[bmcIP] = lookupCacheEntry{identity: id, expires: time.Now().Add(r.cacheTTL)}
	r.lookupCacheMu.Unlock()
}

func (r *InventoryResolver) identityFromInventoryLabels(labels map[string]string, source string) (Identity, bool) {
	id, matched := r.engine.matchBundleFromLabels(
		firstNonEmpty(labels, "manufacturer", "manufacturer_name", "vendor"),
		firstNonEmpty(labels, "model", "bmc_model", "part_number"),
		firstNonEmpty(labels, "firmware_version", "firmware"),
	)
	id.Hostname = strings.TrimSpace(firstNonEmpty(labels, "hostname", "host_name", "name"))
	id.SerialNumber = strings.TrimSpace(firstNonEmpty(labels, "serial_number", "serial", "service_tag"))
	if !matched {
		id.Vendor = normalizeVendor(firstNonEmpty(labels, "manufacturer", "manufacturer_name", "vendor"))
		if id.Vendor == "" && id.Hostname == "" && id.SerialNumber == "" {
			return Identity{}, false
		}
	}
	id.Source = source
	return id, true
}

func (r *InventoryResolver) lookupNeo4j(bmcIP string) (Identity, bool) {
	if r.neo4jEndpoint == "" || r.neo4jQuery == "" {
		return Identity{}, false
	}
	if id, ok := r.cachedLookup(bmcIP); ok {
		return id, true
	}

	statement := strings.ReplaceAll(r.neo4jQuery, "$IP", "$bmc_ip")
	var (
		id  Identity
		ok  bool
		err error
	)
	if isBoltNeo4jEndpoint(r.neo4jEndpoint) {
		id, ok, err = r.lookupNeo4jBolt(bmcIP, statement)
	} else {
		id, ok, err = r.lookupNeo4jHTTP(bmcIP, statement)
	}
	if err != nil || !ok {
		return Identity{}, false
	}
	r.storeLookup(bmcIP, id)
	return id, true
}

func isBoltNeo4jEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	return strings.HasPrefix(endpoint, "bolt://") || strings.HasPrefix(endpoint, "bolt+s://") ||
		strings.HasPrefix(endpoint, "neo4j://") || strings.HasPrefix(endpoint, "neo4j+s://")
}

func (r *InventoryResolver) lookupNeo4jBolt(bmcIP, statement string) (Identity, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.httpClient.Timeout)
	defer cancel()

	driver, err := neo4j.NewDriverWithContext(
		r.neo4jEndpoint,
		neo4j.BasicAuth(r.neo4jUsername, r.neo4jPassword, ""),
	)
	if err != nil {
		return Identity{}, false, err
	}
	defer func() { _ = driver.Close(ctx) }()

	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: r.neo4jDatabase})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, statement, map[string]any{"bmc_ip": bmcIP})
	if err != nil {
		return Identity{}, false, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return Identity{}, false, err
	}

	labels := make(map[string]string, len(record.Keys))
	for _, key := range record.Keys {
		val, found := record.Get(key)
		if !found || val == nil {
			continue
		}
		switch v := val.(type) {
		case string:
			labels[key] = v
		default:
			labels[key] = strings.TrimSpace(jsonValueAsString(v))
		}
	}
	id, ok := r.identityFromInventoryLabels(labels, identitySourceNeo4j)
	return id, ok, nil
}

func (r *InventoryResolver) lookupNeo4jHTTP(bmcIP, statement string) (Identity, bool, error) {
	payload, err := json.Marshal(neo4jCommitRequest{
		Statements: []neo4jStatement{{
			Statement:  statement,
			Parameters: map[string]any{"bmc_ip": bmcIP},
		}},
	})
	if err != nil {
		return Identity{}, false, err
	}

	reqURL := r.neo4jEndpoint + "/db/" + url.PathEscape(r.neo4jDatabase) + "/tx/commit"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return Identity{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.neo4jUsername != "" {
		req.SetBasicAuth(r.neo4jUsername, r.neo4jPassword)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return Identity{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, false, fmt.Errorf("neo4j http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Identity{}, false, err
	}

	var parsed neo4jCommitResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Identity{}, false, err
	}
	if len(parsed.Errors) > 0 || len(parsed.Results) == 0 || len(parsed.Results[0].Data) == 0 {
		return Identity{}, false, fmt.Errorf("neo4j empty result")
	}

	labels := neo4jRowToLabels(parsed.Results[0].Columns, parsed.Results[0].Data[0].Row)
	id, ok := r.identityFromInventoryLabels(labels, identitySourceNeo4j)
	return id, ok, nil
}

func (r *InventoryResolver) lookupPrometheus(bmcIP string) (Identity, bool) {
	if r.promEndpoint == "" || r.promQuery == "" {
		return Identity{}, false
	}
	if id, ok := r.cachedLookup(bmcIP); ok {
		return id, true
	}

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
	resp, err := r.httpClient.Do(req)
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

	id, ok := r.identityFromInventoryLabels(parsed.Data.Result[0].Metric, identitySourcePrometheus)
	if !ok {
		return Identity{}, false
	}
	r.storeLookup(bmcIP, id)
	return id, true
}

func neo4jRowToLabels(columns []string, row []any) map[string]string {
	labels := make(map[string]string, len(columns))
	for i, col := range columns {
		if i >= len(row) || row[i] == nil {
			continue
		}
		switch v := row[i].(type) {
		case string:
			labels[col] = v
		default:
			labels[col] = strings.TrimSpace(jsonValueAsString(v))
		}
	}
	return labels
}

func jsonValueAsString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func firstNonEmpty(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
	}
	return ""
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
