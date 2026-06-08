// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package normalize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Result holds a normalized BMC event.
type Result struct {
	MappingStatus string
	AsamaMessageID string
	AsamaMessageKey string
	AsamaID string
	Message string
	Description string
	Severity string
	MessageSeverity string
	Lifecycle string
	Domain string
	Component string
	SubscriptionPriority string
	VendorName string
	VendorMessageID string
	VendorMessage string
	VendorSeverity string
	VendorMessageArgs []string
	AsamaMessageArgs []string
	ComponentArgIndex int
	BundleID string
	BMCIP string
	BMCModel string
	FirmwareVersion string
	EventTime string
}

type sideMapping struct {
	VendorMessageID     string `json:"vendor_message_id"`
	VendorKey           string `json:"vendor_key"`
	AsamaMessageKey     string `json:"asama_message_key"`
	AsamaMessageID      string `json:"asama_message_id"`
	AsamaID             string `json:"asama_id"`
	Lifecycle           string `json:"lifecycle"`
	ArgMap              []int  `json:"arg_map"`
	VendorNumberOfArgs  int    `json:"vendor_number_of_args"`
	AsamaNumberOfArgs   *int   `json:"asama_number_of_args"`
	SourceRegistry      string `json:"source_registry"`
	ComponentArgIndex   int    `json:"component_arg_index"`
	BundleID            string `json:"bundle_id"`
}

type bundleIndex struct {
	ByID  map[string]sideMapping
	ByKey map[string]sideMapping
}

type asamaRegistry struct {
	RegistryPrefix  string                       `json:"RegistryPrefix"`
	RegistryVersion string                       `json:"RegistryVersion"`
	Messages        map[string]asamaMessage      `json:"Messages"`
}

type asamaMessage struct {
	Message         string `json:"Message"`
	Description     string `json:"Description"`
	Severity        string `json:"Severity"`
	MessageSeverity string `json:"MessageSeverity"`
	NumberOfArgs    int    `json:"NumberOfArgs"`
		Oem             struct {
		Asama struct {
			AsamaID              string `json:"AsamaId"`
			DefaultLifecycle     string `json:"DefaultLifecycle"`
			Domain               string `json:"Domain"`
			Component            string `json:"Component"`
			SubscriptionPriority string `json:"SubscriptionPriority"`
			Serviceable          bool   `json:"Serviceable"`
			CallHome             bool   `json:"CallHome"`
			Audit                bool   `json:"Audit"`
		} `json:"Asama"`
	} `json:"Oem"`
}

type indexFile struct {
	Bundles []indexBundle `json:"bundles"`
}

type indexBundle struct {
	BundleID        string `json:"bundle_id"`
	Vendor          string `json:"vendor"`
	BMCModel        string `json:"bmc_model"`
	FirmwareVersion string `json:"firmware_version"`
	Path            string `json:"path"`
	TestReference   struct {
		BMCIP string `json:"bmc_ip"`
	} `json:"test_reference"`
}

type faultPair struct {
	AsamaID           string `json:"asama_id"`
	AsamaMessageKey   string `json:"asama_message_key"`
	ComponentArgIndex int    `json:"component_arg_index"`
	AssertMappings    []mappingSide `json:"assert_mappings"`
	DeassertMappings  []mappingSide `json:"deassert_mappings"`
}

type mappingSide struct {
	VendorMessageID    string `json:"vendor_message_id"`
	VendorKey          string `json:"vendor_key"`
	AsamaMessageID     string `json:"asama_message_id"`
	ArgMap             []int  `json:"arg_map"`
	VendorNumberOfArgs int    `json:"vendor_number_of_args"`
	AsamaNumberOfArgs  *int   `json:"asama_number_of_args"`
	SourceRegistry     string `json:"source_registry"`
}

type bundleFile struct {
	BundleID        string `json:"bundle_id"`
	Vendor          string `json:"vendor"`
	BMCModel        string `json:"bmc_model"`
	FirmwareVersion string `json:"firmware_version"`
	Registry        struct {
		PrimaryFile string `json:"primary_file"`
	} `json:"registry"`
	TestReference struct {
		BMCIP       string `json:"bmc_ip"`
		RegistryDir string `json:"registry_dir"`
	} `json:"test_reference"`
	FaultPairs []faultPair `json:"fault_pairs"`
}

// Engine loads Asama registry and vendor mapping bundles for normalization.
type Engine struct {
	asamaPath      string
	mappingsIndex  string
	mappingsDir    string

	mu            sync.RWMutex
	asama         asamaRegistry
	index         indexFile
	bundleCache   map[string]bundleIndex
	bundleMeta    map[string]bundleFile
}

func NewEngine(asamaPath, mappingsIndex, mappingsDir string) (*Engine, error) {
	e := &Engine{
		asamaPath:     asamaPath,
		mappingsIndex: mappingsIndex,
		mappingsDir:   mappingsDir,
		bundleCache:   make(map[string]bundleIndex),
		bundleMeta:    make(map[string]bundleFile),
	}
	if err := e.reload(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) Reload() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bundleCache = make(map[string]bundleIndex)
	e.bundleMeta = make(map[string]bundleFile)
	return e.reload()
}

func (e *Engine) reload() error {
	asamaBytes, err := os.ReadFile(e.asamaPath)
	if err != nil {
		return fmt.Errorf("read asama registry: %w", err)
	}
	if err := json.Unmarshal(asamaBytes, &e.asama); err != nil {
		return fmt.Errorf("parse asama registry: %w", err)
	}

	indexBytes, err := os.ReadFile(e.mappingsIndex)
	if err != nil {
		return fmt.Errorf("read mappings index: %w", err)
	}
	if err := json.Unmarshal(indexBytes, &e.index); err != nil {
		return fmt.Errorf("parse mappings index: %w", err)
	}
	return nil
}

// LookupBundle returns mapping bundle metadata for vendor registry resolution.
func (e *Engine) LookupBundle(vendor, bmcModel, firmwareVersion, bundleID string) (bundleFile, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, bundle, err := e.loadBundle(vendor, bmcModel, firmwareVersion, bundleID)
	return bundle, err
}

func (e *Engine) Normalize(vendor, messageID, vendorMessage, severity, bmcIP, bmcModel, firmwareVersion, bundleID, eventTime string, vendorArgs []string) Result {
	e.mu.RLock()
	defer e.mu.RUnlock()

	idx, bundle, err := e.loadBundle(vendor, bmcModel, firmwareVersion, bundleID)
	sourceMeta := Result{
		VendorName:      strings.ToLower(vendor),
		VendorMessageID: messageID,
		VendorMessage:   vendorMessage,
		VendorSeverity:  severity,
		VendorMessageArgs: vendorArgs,
		BMCIP:           bmcIP,
		BMCModel:        bmcModel,
		FirmwareVersion: firmwareVersion,
		EventTime:       eventTime,
	}

	if err != nil {
		sourceMeta.MappingStatus = "unmapped"
		sourceMeta.VendorName = strings.ToLower(vendor)
		return sourceMeta
	}

	if bundle.Vendor != "" {
		sourceMeta.VendorName = strings.ToLower(bundle.Vendor)
	}
	if sourceMeta.BMCModel == "" {
		sourceMeta.BMCModel = bundle.BMCModel
	}
	if sourceMeta.FirmwareVersion == "" {
		sourceMeta.FirmwareVersion = bundle.FirmwareVersion
	}
	sourceMeta.BundleID = bundle.BundleID

	mapping := lookupMapping(messageID, idx)
	if mapping.VendorMessageID == "" && mapping.VendorKey == "" {
		sourceMeta.MappingStatus = "unmapped"
		return sourceMeta
	}

	asamaKey := mapping.AsamaMessageKey
	asamaMsg, ok := e.asama.Messages[asamaKey]
	if !ok {
		sourceMeta.MappingStatus = "unmapped"
		return sourceMeta
	}

	argMap := mapping.ArgMap
	if argMap == nil {
		argMap = []int{}
	}
	asamaArgs := remapArgs(vendorArgs, argMap)
	resolvedMessage := formatMessage(asamaMsg.Message, asamaArgs)
	oem := asamaMsg.Oem.Asama

	asamaMessageID := mapping.AsamaMessageID
	if asamaMessageID == "" {
		asamaMessageID = fmt.Sprintf("%s.%s.%s", e.asama.RegistryPrefix, e.asama.RegistryVersion, asamaKey)
	}
	asamaID := mapping.AsamaID
	if asamaID == "" {
		asamaID = oem.AsamaID
	}
	lifecycle := mapping.Lifecycle
	if lifecycle == "" {
		lifecycle = oem.DefaultLifecycle
	}
	sev := asamaMsg.Severity
	if sev == "" {
		sev = severity
	}
	msgSev := asamaMsg.MessageSeverity
	if msgSev == "" {
		msgSev = severity
	}

	return Result{
		MappingStatus:        "mapped",
		AsamaMessageID:       asamaMessageID,
		AsamaMessageKey:      asamaKey,
		AsamaID:              asamaID,
		Message:              resolvedMessage,
		Description:          asamaMsg.Description,
		Severity:             sev,
		MessageSeverity:      msgSev,
		Lifecycle:            lifecycle,
		Domain:               oem.Domain,
		Component:            oem.Component,
		SubscriptionPriority: oem.SubscriptionPriority,
		VendorName:           sourceMeta.VendorName,
		VendorMessageID:      messageID,
		VendorMessage:        vendorMessage,
		VendorSeverity:       severity,
		VendorMessageArgs:    vendorArgs,
		AsamaMessageArgs:     asamaArgs,
		ComponentArgIndex:    mapping.ComponentArgIndex,
		BundleID:             mapping.BundleID,
		BMCIP:                bmcIP,
		BMCModel:             sourceMeta.BMCModel,
		FirmwareVersion:      sourceMeta.FirmwareVersion,
		EventTime:            eventTime,
	}
}

func (e *Engine) loadBundle(vendor, bmcModel, firmwareVersion, bundleID string) (bundleIndex, bundleFile, error) {
	cacheKey := strings.Join([]string{vendor, bmcModel, firmwareVersion, bundleID}, "|")
	if idx, ok := e.bundleCache[cacheKey]; ok {
		return idx, e.bundleMeta[cacheKey], nil
	}

	path, _, err := resolveBundlePath(e.index, e.mappingsDir, vendor, bmcModel, firmwareVersion, bundleID)
	if err != nil {
		return bundleIndex{}, bundleFile{}, err
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return bundleIndex{}, bundleFile{}, err
	}
	var bundle bundleFile
	if err := json.Unmarshal(bytes, &bundle); err != nil {
		return bundleIndex{}, bundleFile{}, err
	}

	idx := buildIndexFromBundle(bundle)
	e.bundleCache[cacheKey] = idx
	e.bundleMeta[cacheKey] = bundle
	return idx, bundle, nil
}

func (e *Engine) lookupByIndexIP(bmcIP string) (Identity, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	bmcIP = normalizeIP(bmcIP)
	for _, b := range e.index.Bundles {
		if b.TestReference.BMCIP == bmcIP {
			return Identity{
				Vendor:          strings.ToLower(b.Vendor),
				BMCModel:        b.BMCModel,
				FirmwareVersion: b.FirmwareVersion,
				BundleID:        b.BundleID,
				Source:          identitySourceIndexIP,
			}, true
		}
	}
	return Identity{}, false
}

func (e *Engine) lookupByUniqueMessageID(messageID string) (Identity, bool) {
	e.mu.RLock()
	index := e.index
	mappingsDir := e.mappingsDir
	e.mu.RUnlock()

	var matches []indexBundle
	for _, b := range index.Bundles {
		idx, err := e.loadBundleForEntry(b, mappingsDir)
		if err != nil {
			continue
		}
		if mapping := lookupMapping(messageID, idx); mapping.VendorMessageID != "" || mapping.VendorKey != "" {
			matches = append(matches, b)
		}
	}
	if len(matches) != 1 {
		return Identity{}, false
	}
	b := matches[0]
	return Identity{
		Vendor:          strings.ToLower(b.Vendor),
		BMCModel:        b.BMCModel,
		FirmwareVersion: b.FirmwareVersion,
		BundleID:        b.BundleID,
		Source:          identitySourceMessageID,
	}, true
}

func (e *Engine) loadBundleForEntry(b indexBundle, mappingsDir string) (bundleIndex, error) {
	e.mu.RLock()
	cacheKey := strings.Join([]string{b.Vendor, b.BMCModel, b.FirmwareVersion, b.BundleID}, "|")
	if idx, ok := e.bundleCache[cacheKey]; ok {
		e.mu.RUnlock()
		return idx, nil
	}
	e.mu.RUnlock()

	path := filepath.Join(mappingsDir, b.Path)
	bytes, err := os.ReadFile(path)
	if err != nil {
		return bundleIndex{}, err
	}
	var bundle bundleFile
	if err := json.Unmarshal(bytes, &bundle); err != nil {
		return bundleIndex{}, err
	}
	idx := buildIndexFromBundle(bundle)

	e.mu.Lock()
	e.bundleCache[cacheKey] = idx
	e.bundleMeta[cacheKey] = bundle
	e.mu.Unlock()
	return idx, nil
}

func (e *Engine) matchBundleFromLabels(manufacturer, model, firmware string) (Identity, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	vendor := normalizeVendor(manufacturer)
	if vendor == "" {
		return Identity{}, false
	}

	var matches []indexBundle
	for _, b := range e.index.Bundles {
		if strings.ToLower(b.Vendor) != vendor {
			continue
		}
		if firmware != "" && !firmwareMatches(b.FirmwareVersion, firmware) {
			continue
		}
		if model != "" && !modelMatchesBundle(b, model) {
			continue
		}
		matches = append(matches, b)
	}
	if len(matches) == 0 && firmware != "" {
		for _, b := range e.index.Bundles {
			if strings.ToLower(b.Vendor) == vendor && firmwareMatches(b.FirmwareVersion, firmware) {
				matches = append(matches, b)
			}
		}
	}
	if len(matches) != 1 {
		return Identity{}, false
	}
	b := matches[0]
	return Identity{
		Vendor:          strings.ToLower(b.Vendor),
		BMCModel:        b.BMCModel,
		FirmwareVersion: b.FirmwareVersion,
		BundleID:        b.BundleID,
	}, true
}

func normalizeVendor(manufacturer string) string {
	m := strings.ToLower(strings.TrimSpace(manufacturer))
	switch {
	case strings.Contains(m, "hpe"), strings.Contains(m, "hewlett"):
		return "hpe"
	case strings.Contains(m, "dell"):
		return "dell"
	case strings.Contains(m, "lenovo"):
		return "lenovo"
	default:
		return m
	}
}

func modelMatchesBundle(b indexBundle, promModel string) bool {
	promModel = strings.ToLower(strings.TrimSpace(promModel))
	if promModel == "" {
		return true
	}
	normalized := strings.ReplaceAll(promModel, "_", " ")
	bmcModel := strings.ToLower(b.BMCModel)
	if strings.Contains(promModel, bmcModel) || strings.Contains(normalized, bmcModel) {
		return true
	}
	switch bmcModel {
	case "ilo6":
		return strings.Contains(normalized, "ilo 6") || strings.Contains(promModel, "ilo6") ||
			strings.Contains(normalized, "ilo 5") || strings.Contains(promModel, "ilo5") ||
			strings.Contains(promModel, "ilo_5")
	case "idrac9":
		return strings.Contains(promModel, "idrac") || strings.Contains(promModel, "13g") ||
			strings.Contains(promModel, "14g") || strings.Contains(promModel, "15g")
	case "xcc":
		return strings.Contains(promModel, "xclarity") || strings.Contains(promModel, "xcc") ||
			strings.Contains(promModel, "lenovo_xclarity")
	}
	return false
}

func firmwareMatches(bundleFW, promFW string) bool {
	bundleFW = strings.TrimSpace(bundleFW)
	promFW = strings.TrimSpace(promFW)
	if bundleFW == promFW {
		return true
	}
	return normalizeFirmware(bundleFW) == normalizeFirmware(promFW)
}

func normalizeFirmware(v string) string {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, ".")
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}

func resolveBundlePath(index indexFile, mappingsDir, vendor, bmcModel, firmwareVersion, bundleID string) (string, bundleFile, error) {
	if bundleID != "" {
		for _, b := range index.Bundles {
			if b.BundleID == bundleID {
				return filepath.Join(mappingsDir, b.Path), bundleFile{
					BundleID: b.BundleID, Vendor: b.Vendor, BMCModel: b.BMCModel, FirmwareVersion: b.FirmwareVersion,
				}, nil
			}
		}
		return "", bundleFile{}, fmt.Errorf("bundle_id %q not found", bundleID)
	}
	if vendor == "" {
		return "", bundleFile{}, fmt.Errorf("vendor is required")
	}
	vendor = strings.ToLower(vendor)
	var matches []struct {
		BundleID        string
		Vendor          string
		BMCModel        string
		FirmwareVersion string
		Path            string
	}
	for _, b := range index.Bundles {
		if strings.ToLower(b.Vendor) != vendor {
			continue
		}
		matches = append(matches, struct {
			BundleID        string
			Vendor          string
			BMCModel        string
			FirmwareVersion string
			Path            string
		}{b.BundleID, b.Vendor, b.BMCModel, b.FirmwareVersion, b.Path})
	}
	if bmcModel != "" {
		filtered := matches[:0]
		for _, b := range matches {
			if strings.EqualFold(b.BMCModel, bmcModel) {
				filtered = append(filtered, b)
			}
		}
		matches = filtered
	}
	if firmwareVersion != "" {
		for _, b := range matches {
			if b.FirmwareVersion == firmwareVersion {
				return filepath.Join(mappingsDir, b.Path), bundleFile{
					BundleID: b.BundleID, Vendor: b.Vendor, BMCModel: b.BMCModel, FirmwareVersion: b.FirmwareVersion,
				}, nil
			}
		}
	}
	if len(matches) == 1 {
		b := matches[0]
		return filepath.Join(mappingsDir, b.Path), bundleFile{
			BundleID: b.BundleID, Vendor: b.Vendor, BMCModel: b.BMCModel, FirmwareVersion: b.FirmwareVersion,
		}, nil
	}
	if len(matches) > 0 && firmwareVersion == "" {
		b := matches[0]
		return filepath.Join(mappingsDir, b.Path), bundleFile{
			BundleID: b.BundleID, Vendor: b.Vendor, BMCModel: b.BMCModel, FirmwareVersion: b.FirmwareVersion,
		}, nil
	}
	return "", bundleFile{}, fmt.Errorf("no mapping bundle for vendor=%q bmc_model=%q firmware_version=%q", vendor, bmcModel, firmwareVersion)
}

func buildIndexFromBundle(bundle bundleFile) bundleIndex {
	idx := bundleIndex{ByID: make(map[string]sideMapping), ByKey: make(map[string]sideMapping)}
	for _, pair := range bundle.FaultPairs {
		for _, side := range pair.AssertMappings {
			entry := flattenSide(side, pair, "assert", bundle.BundleID)
			if entry.VendorMessageID != "" {
				idx.ByID[entry.VendorMessageID] = entry
			}
			if entry.VendorKey != "" {
				if _, exists := idx.ByKey[entry.VendorKey]; !exists {
					idx.ByKey[entry.VendorKey] = entry
				}
			}
		}
		for _, side := range pair.DeassertMappings {
			entry := flattenSide(side, pair, "deassert", bundle.BundleID)
			if entry.VendorMessageID != "" {
				idx.ByID[entry.VendorMessageID] = entry
			}
			if entry.VendorKey != "" {
				if _, exists := idx.ByKey[entry.VendorKey]; !exists {
					idx.ByKey[entry.VendorKey] = entry
				}
			}
		}
	}
	return idx
}

func flattenSide(side mappingSide, pair faultPair, lifecycle, bundleID string) sideMapping {
	return sideMapping{
		VendorMessageID:    side.VendorMessageID,
		VendorKey:          side.VendorKey,
		AsamaMessageKey:    pair.AsamaMessageKey,
		AsamaMessageID:     side.AsamaMessageID,
		AsamaID:            pair.AsamaID,
		Lifecycle:          lifecycle,
		ArgMap:             side.ArgMap,
		VendorNumberOfArgs: side.VendorNumberOfArgs,
		AsamaNumberOfArgs:  side.AsamaNumberOfArgs,
		SourceRegistry:     side.SourceRegistry,
		ComponentArgIndex:  pair.ComponentArgIndex,
		BundleID:           bundleID,
	}
}

var versionSegment = regexp.MustCompile(`^\d+\.\d+`)

func parseMessageID(messageID string) (prefix, key string) {
	parts := strings.Split(messageID, ".")
	if len(parts) < 2 {
		return messageID, messageID
	}
	key = parts[len(parts)-1]
	prefixParts := parts[:len(parts)-1]
	if len(prefixParts) >= 2 && versionSegment.MatchString(prefixParts[len(prefixParts)-1]) {
		prefix = strings.Join(prefixParts[:len(prefixParts)-1], ".")
	} else {
		prefix = prefixParts[0]
	}
	return prefix, key
}

func lookupMapping(messageID string, idx bundleIndex) sideMapping {
	if entry, ok := idx.ByID[messageID]; ok {
		return entry
	}
	prefix, key := parseMessageID(messageID)
	if entry, ok := idx.ByKey[key]; ok {
		candPrefix, _ := parseMessageID(entry.VendorMessageID)
		if candPrefix == prefix || strings.HasSuffix(entry.VendorMessageID, "."+key) {
			return entry
		}
	}
	for _, entry := range idx.ByID {
		if entry.VendorKey == key {
			candPrefix, _ := parseMessageID(entry.VendorMessageID)
			if candPrefix == prefix {
				return entry
			}
		}
	}
	return sideMapping{}
}

func remapArgs(vendorArgs []string, argMap []int) []string {
	out := make([]string, 0, len(argMap))
	for _, idx := range argMap {
		if idx >= 1 && idx <= len(vendorArgs) {
			out = append(out, vendorArgs[idx-1])
		} else {
			out = append(out, "")
		}
	}
	return out
}

var argPlaceholder = regexp.MustCompile(`%(\d+)`)

func formatMessage(template string, args []string) string {
	message := template
	for i, arg := range args {
		message = strings.ReplaceAll(message, fmt.Sprintf("%%%d", i+1), arg)
	}
	message = argPlaceholder.ReplaceAllStringFunc(message, func(match string) string {
		sub := argPlaceholder.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		var idx int
		fmt.Sscanf(sub[1], "%d", &idx)
		if idx > len(args) {
			return ""
		}
		return match
	})
	return message
}
