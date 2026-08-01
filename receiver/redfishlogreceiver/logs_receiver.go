// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package redfishlogreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver"

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/redfish"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/adapter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redfishlogreceiver/internal/metadata"
)

type redfishLogReceiver struct {
	cfg      *Config
	settings receiver.Settings
	consumer consumer.Logs
	logger   *zap.Logger

	wg            sync.WaitGroup
	shutdown      chan struct{}
	shutdownOnce  sync.Once
	storageClient *checkpointPersister
	credsPath     string
}

func newReceiver(cfg *Config, settings receiver.Settings, next consumer.Logs) *redfishLogReceiver {
	return &redfishLogReceiver{
		cfg:       cfg,
		settings:  settings,
		consumer:  next,
		logger:    settings.Logger,
		shutdown:  make(chan struct{}),
		credsPath: cfg.CredentialsFile,
	}
}

func (r *redfishLogReceiver) Start(ctx context.Context, host component.Host) error {
	client, err := adapter.GetStorageClient(ctx, host, r.cfg.StorageID, r.settings.ID)
	if err != nil {
		return fmt.Errorf("redfishlog storage: %w", err)
	}
	r.storageClient = newCheckpointPersister(client, r.logger)

	r.wg.Add(1)
	go r.runPollLoop(ctx)
	return nil
}

func (r *redfishLogReceiver) Shutdown(ctx context.Context) error {
	r.shutdownOnce.Do(func() {
		close(r.shutdown)
	})
	r.wg.Wait()
	if r.storageClient != nil {
		return r.storageClient.close(ctx)
	}
	return nil
}

func (r *redfishLogReceiver) runPollLoop(ctx context.Context) {
	defer r.wg.Done()

	interval := r.cfg.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	r.pollOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.shutdown:
			return
		case <-t.C:
			r.pollOnce(ctx)
		}
	}
}

func (r *redfishLogReceiver) pollOnce(ctx context.Context) {
	creds, err := loadCredentialsFile(r.credsPath)
	if err != nil {
		r.logger.Error("redfishlog credentials", zap.Error(err))
		return
	}

	all := plog.NewLogs()
	pending := make(map[string]*checkpointState)

	for _, t := range r.cfg.Targets {
		lg, updates, err := r.collectTarget(ctx, creds, t)
		if err != nil {
			r.logger.Error("redfishlog collect target",
				zap.String("endpoint", t.Endpoint), zap.Error(err))
			continue
		}
		src := lg.ResourceLogs()
		for src.Len() > 0 {
			src.At(0).MoveTo(all.ResourceLogs().AppendEmpty())
		}
		for k, v := range updates {
			pending[k] = v
		}
	}

	if all.ResourceLogs().Len() == 0 {
		return
	}

	if err := r.consumer.ConsumeLogs(ctx, all); err != nil {
		r.logger.Error("redfishlog consume", zap.Error(err))
		return
	}

	for k, v := range pending {
		if v == nil {
			continue
		}
		if err := r.storageClient.set(ctx, k, v); err != nil {
			r.logger.Error("redfishlog checkpoint", zap.String("key", k), zap.Error(err))
		}
	}
}

func (r *redfishLogReceiver) collectTarget(
	ctx context.Context,
	creds *credentialsStore,
	t TargetConfig,
) (plog.Logs, map[string]*checkpointState, error) {
	updates := make(map[string]*checkpointState)
	out := plog.NewLogs()

	lookup := credentialKeyForTarget(t)
	user, pwd, err := creds.resolve(lookup)
	if err != nil {
		return out, updates, err
	}

	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := &http.Client{Timeout: timeout}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       nil,
	}
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: r.cfg.InsecureSkipVerify} //nolint:gosec // user-controlled for BMC HTTPS
	httpClient.Transport = transport

	gcfg := gofish.ClientConfig{
		Endpoint:         t.Endpoint,
		Username:         user,
		Password:         string(pwd),
		Insecure:         r.cfg.InsecureSkipVerify,
		HTTPClient:       httpClient,
		BasicAuth:        true,
		ReuseConnections: false,
	}

	client, err := gofish.ConnectContext(ctx, gcfg)
	if err != nil {
		return out, updates, fmt.Errorf("gofish connect: %w", err)
	}
	defer client.Logout()

	managers, err := client.Service.Managers()
	if err != nil {
		return out, updates, fmt.Errorf("managers: %w", err)
	}

	for _, mgr := range managers {
		logSvcs, err := mgr.LogServices()
		if err != nil {
			r.logger.Debug("log services", zap.String("manager", mgr.Name), zap.Error(err))
			continue
		}
		for _, ls := range logSvcs {
			entries, err := ls.Entries()
			if err != nil {
				r.logger.Debug("entries", zap.String("log_service", ls.Name), zap.Error(err))
				continue
			}
			ckKey := checkpointStorageKey(t.Endpoint, ls.ODataID)
			var cp *checkpointState
			if r.storageClient != nil {
				cp, err = r.storageClient.get(ctx, ckKey)
				if err != nil {
					r.logger.Warn("checkpoint get", zap.String("key", ckKey), zap.Error(err))
					cp = nil
				}
			}

			sorted := append([]*redfish.LogEntry(nil), entries...)
			sortLogEntries(sorted)

			skew := r.cfg.LogResetClockSkew
			if skew <= 0 {
				skew = defaultLogResetClockSkew
			}
			initialN := r.cfg.InitialLastN
			if initialN < 1 {
				initialN = defaultInitialLastN
			}

			reset := shouldResetCheckpoint(cp, sorted, skew)
			if reset {
				r.logger.Info("redfishlog log reset detected",
					zap.String("endpoint", t.Endpoint),
					zap.String("log_service", ls.ODataID))
			}

			toEmit, newCk := selectEntriesToEmit(cp, sorted, initialN, reset)
			if len(toEmit) == 0 {
				continue
			}

			rl := out.ResourceLogs().AppendEmpty()
			res := rl.Resource()
			attrs := res.Attributes()
			attrs.PutStr("bmc.endpoint", t.Endpoint)
			attrs.PutStr("bmc.credential_key", lookup)
			attrs.PutStr("redfish.manager.id", mgr.ID)
			attrs.PutStr("redfish.manager.name", mgr.Name)
			attrs.PutStr("redfish.log_service.id", ls.ID)
			attrs.PutStr("redfish.log_service.odata_id", ls.ODataID)

			sl := rl.ScopeLogs().AppendEmpty()
			sl.Scope().SetName(metadata.ScopeName)
			sl.Scope().SetVersion(r.settings.BuildInfo.Version)

			for _, ent := range toEmit {
				appendLogRecord(sl, t.Endpoint, ls.ODataID, ent)
			}

			if newCk != nil {
				updates[ckKey] = newCk
			}
		}
	}

	return out, updates, nil
}

func appendLogRecord(sl plog.ScopeLogs, endpoint, logSvcOData string, ent *redfish.LogEntry) {
	lr := sl.LogRecords().AppendEmpty()
	ts := entryTimestamp(ent)
	lr.SetTimestamp(pcommon.NewTimestampFromTime(ts))
	lr.SetObservedTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	body := strings.TrimSpace(ent.Message)
	if body == "" {
		body = strings.TrimSpace(ent.Description)
	}
	if body == "" {
		body = fmt.Sprintf("%s %s", ent.EntryType, ent.Severity)
	}
	lr.Body().SetStr(body)

	sev, sevText := mapSeverity(ent.Severity)
	lr.SetSeverityNumber(sev)
	lr.SetSeverityText(sevText)

	fp := entryFingerprint(endpoint, logSvcOData, ent.ID, ent.Created)
	la := lr.Attributes()
	la.PutStr("redfish.entry_fingerprint", fp)
	la.PutStr("redfish.log_entry.id", ent.ID)
	la.PutStr("redfish.log_entry.created", ent.Created)
	la.PutStr("redfish.log_entry.type", string(ent.EntryType))
	if ent.EventID != "" {
		la.PutStr("redfish.log_entry.event_id", ent.EventID)
	}
	if ent.GeneratorID != "" {
		la.PutStr("redfish.log_entry.generator_id", ent.GeneratorID)
	}
}

func entryTimestamp(ent *redfish.LogEntry) time.Time {
	if ent.EventTimestamp != "" {
		if t, ok := parseRedfishTime(ent.EventTimestamp); ok {
			return t
		}
	}
	if t, ok := parseRedfishTime(ent.Created); ok {
		return t
	}
	return time.Now()
}

func mapSeverity(s redfish.EventSeverity) (plog.SeverityNumber, string) {
	switch s {
	case redfish.CriticalEventSeverity:
		return plog.SeverityNumberFatal, string(s)
	case redfish.WarningEventSeverity:
		return plog.SeverityNumberWarn, string(s)
	case redfish.OKEventSeverity:
		return plog.SeverityNumberInfo, string(s)
	default:
		if s == "" {
			return plog.SeverityNumberUnspecified, ""
		}
		return plog.SeverityNumberInfo, string(s)
	}
}

func entryFingerprint(endpoint, logSvcOData, id, created string) string {
	sum := sha256.Sum256([]byte(endpoint + "\x00" + logSvcOData + "\x00" + id + "\x00" + created))
	return hex.EncodeToString(sum[:])
}

func sortLogEntries(entries []*redfish.LogEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return logEntryOrderLess(entries[i], entries[j])
	})
}

func logEntryOrderLess(a, b *redfish.LogEntry) bool {
	ta, aOK := parseRedfishTime(a.Created)
	tb, bOK := parseRedfishTime(b.Created)
	if aOK && bOK && !ta.Equal(tb) {
		return ta.Before(tb)
	}
	if aOK != bOK {
		return !aOK
	}
	return compareIDs(a.ID, b.ID) < 0
}

func compareIDs(a, b string) int {
	na, okA := parseNumericID(a)
	nb, okB := parseNumericID(b)
	if okA && okB {
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
}

func parseNumericID(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		v, err := strconv.ParseUint(s[2:], 16, 64)
		return v, err == nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

func maxCreatedTime(entries []*redfish.LogEntry) (time.Time, bool) {
	var maxT time.Time
	ok := false
	for _, e := range entries {
		t, valid := parseRedfishTime(e.Created)
		if !valid {
			continue
		}
		if !ok || t.After(maxT) {
			maxT = t
			ok = true
		}
	}
	return maxT, ok
}

func maxNumericID(entries []*redfish.LogEntry) (uint64, bool) {
	var max uint64
	ok := false
	for _, e := range entries {
		n, parsed := parseNumericID(e.ID)
		if !parsed {
			continue
		}
		if !ok || n > max {
			max = n
			ok = true
		}
	}
	return max, ok
}

func shouldResetCheckpoint(cp *checkpointState, sorted []*redfish.LogEntry, skew time.Duration) bool {
	if cp == nil || len(sorted) == 0 {
		return false
	}
	maxT, okMax := maxCreatedTime(sorted)
	tcp, okCp := parseRedfishTime(cp.Created)
	if okCp && okMax && tcp.After(maxT.Add(skew)) {
		return true
	}
	maxN, maxNOK := maxNumericID(sorted)
	cpN, cpNumOK := parseNumericID(cp.EntryID)
	if cpNumOK && maxNOK && maxN < cpN {
		return true
	}
	return false
}

func selectEntriesToEmit(cp *checkpointState, sorted []*redfish.LogEntry, initialN int, reset bool) ([]*redfish.LogEntry, *checkpointState) {
	if len(sorted) == 0 {
		return nil, nil
	}
	if cp == nil || reset {
		tail := sorted
		if len(tail) > initialN {
			tail = tail[len(tail)-initialN:]
		}
		ck := checkpointFromEmitted(tail, len(sorted))
		return tail, ck
	}
	var out []*redfish.LogEntry
	for _, e := range sorted {
		if logEntryAfterCheckpoint(e, cp) {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	ck := checkpointFromEmitted(out, len(sorted))
	return out, ck
}

func logEntryAfterCheckpoint(e *redfish.LogEntry, cp *checkpointState) bool {
	te, okE := parseRedfishTime(e.Created)
	tcp, okC := parseRedfishTime(cp.Created)
	if !okC {
		if !okE {
			return compareIDs(e.ID, cp.EntryID) > 0
		}
		return true
	}
	if !okE {
		// Malformed entry timestamps are not treated as newer than the checkpoint,
		// to avoid re-emitting the same bad entries on every poll.
		return false
	}
	if te.After(tcp) {
		return true
	}
	if te.Equal(tcp) && compareIDs(e.ID, cp.EntryID) > 0 {
		return true
	}
	return false
}

func checkpointFromEmitted(emitted []*redfish.LogEntry, totalCount int) *checkpointState {
	if len(emitted) == 0 {
		return nil
	}
	last := emitted[len(emitted)-1]
	return &checkpointState{
		Created:        last.Created,
		EntryID:        last.ID,
		LastEntryCount: totalCount,
	}
}

func parseRedfishTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
