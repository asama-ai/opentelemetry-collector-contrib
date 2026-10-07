// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package inventorydiff // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/inventorydiff"

import (
	"encoding/binary"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// SeriesPoint is one time series under a metric.
type SeriesPoint struct {
	Labels    map[string]string `json:"labels"`
	Value     float64           `json:"-"`
	exactInt  bool
	intVal    int64
	Timestamp string `json:"timestamp"` // datapoint time (RFC3339); not used in compareSnapshots
}

func (sp SeriesPoint) MarshalJSON() ([]byte, error) {
	rawValue, err := sp.marshalValue()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Labels    map[string]string `json:"labels"`
		Value     json.RawMessage   `json:"value"`
		Timestamp string            `json:"timestamp"`
	}{
		Labels:    sp.Labels,
		Value:     rawValue,
		Timestamp: sp.Timestamp,
	})
}

func (sp SeriesPoint) marshalValue() (json.RawMessage, error) {
	if sp.exactInt {
		return json.RawMessage(strconv.FormatInt(sp.intVal, 10)), nil
	}
	return json.Marshal(sp.Value)
}

func (sp SeriesPoint) valueIdentity() string {
	if sp.exactInt {
		return "i:" + strconv.FormatInt(sp.intVal, 10)
	}
	return "f:" + strconv.FormatFloat(sp.Value, 'g', -1, 64)
}

// MetricSnapshot is all series for one metric on one host at one observation.
type MetricSnapshot struct {
	ObservedAt string        `json:"observed_at"`
	Series     []SeriesPoint `json:"series"`
}

// KeySet maps canonical label keys to value identities (timestamps ignored).
func (s MetricSnapshot) KeySet() map[string]string {
	out := make(map[string]string, len(s.Series))
	for _, sp := range s.Series {
		out[seriesKey(sp.Labels)] = sp.valueIdentity()
	}
	return out
}

// seriesKey is an injective encoding of a label set. Length prefixes keep
// values that contain separators from colliding with other label sets.
func seriesKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	var lenBuf [4]byte
	write := func(s string) {
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(s)))
		b = append(b, lenBuf[:]...)
		b = append(b, s...)
	}
	for _, k := range keys {
		write(k)
		write(labels[k])
	}
	return string(b)
}

// compareSnapshots returns true when label keys and values match (timestamps ignored).
func compareSnapshots(a, b MetricSnapshot) bool {
	ak, bk := a.KeySet(), b.KeySet()
	if len(ak) != len(bk) {
		return false
	}
	for k, v := range ak {
		if bv, ok := bk[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func snapshotToJSON(s MetricSnapshot) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// cloneSnapshot returns a deep copy with private Labels maps.
func cloneSnapshot(s MetricSnapshot) MetricSnapshot {
	out := MetricSnapshot{ObservedAt: s.ObservedAt}
	out.Series = make([]SeriesPoint, len(s.Series))
	for i, sp := range s.Series {
		labels := make(map[string]string, len(sp.Labels))
		for k, v := range sp.Labels {
			labels[k] = v
		}
		out.Series[i] = SeriesPoint{
			Labels:    labels,
			Value:     sp.Value,
			exactInt:  sp.exactInt,
			intVal:    sp.intVal,
			Timestamp: sp.Timestamp,
		}
	}
	return out
}

func resourceHostname(attrs pcommon.Map) string {
	for _, key := range []string{"hostname", "host.name", "host_name"} {
		if v, ok := attrs.Get(key); ok && v.Type() == pcommon.ValueTypeStr && v.Str() != "" {
			return v.Str()
		}
	}
	return ""
}

func attrsToMap(attrs pcommon.Map) map[string]string {
	out := make(map[string]string, attrs.Len())
	attrs.Range(func(k string, v pcommon.Value) bool {
		switch v.Type() {
		case pcommon.ValueTypeStr:
			out[k] = v.Str()
		case pcommon.ValueTypeBool:
			out[k] = strconv.FormatBool(v.Bool())
		case pcommon.ValueTypeInt:
			out[k] = strconv.FormatInt(v.Int(), 10)
		case pcommon.ValueTypeDouble:
			out[k] = strconv.FormatFloat(v.Double(), 'g', -1, 64)
		default:
			out[k] = v.AsString()
		}
		return true
	})
	return out
}

func tsRFC3339(ts pcommon.Timestamp) string {
	if ts == 0 {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}

// hostnamesInMetrics returns distinct hostnames present in the batch.
func hostnamesInMetrics(md pmetric.Metrics) []string {
	seen := make(map[string]struct{})
	var out []string
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		h := resourceHostname(rms.At(i).Resource().Attributes())
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

// snapshotFromMetrics builds a snapshot for one hostname + metric name from the batch.
// present is false when the metric name does not appear for that host (caller should skip).
// present is true with empty Series when the metric exists but has no datapoints.
func snapshotFromMetrics(md pmetric.Metrics, hostname, metricName string, now time.Time) (MetricSnapshot, bool) {
	snap := MetricSnapshot{
		ObservedAt: now.UTC().Format(time.RFC3339Nano),
		Series:     []SeriesPoint{},
	}
	present := false
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		if resourceHostname(rm.Resource().Attributes()) != hostname {
			continue
		}
		sms := rm.ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				if m.Name() != metricName {
					continue
				}
				if !appendMetricSeries(&snap, m) {
					continue
				}
				present = true
			}
		}
	}
	return snap, present
}

func appendMetricSeries(snap *MetricSnapshot, m pmetric.Metric) bool {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		dps := m.Gauge().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dp := dps.At(i)
			sp := SeriesPoint{
				Labels:    attrsToMap(dp.Attributes()),
				Timestamp: tsRFC3339(dp.Timestamp()),
			}
			applyNumber(&sp, dp)
			snap.Series = append(snap.Series, sp)
		}
		return true
	case pmetric.MetricTypeSum:
		dps := m.Sum().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dp := dps.At(i)
			sp := SeriesPoint{
				Labels:    attrsToMap(dp.Attributes()),
				Timestamp: tsRFC3339(dp.Timestamp()),
			}
			applyNumber(&sp, dp)
			snap.Series = append(snap.Series, sp)
		}
		return true
	default:
		// Histogram, Summary, and ExponentialHistogram are not inventory series.
		// Leaving them absent avoids emitting a false delete.
		return false
	}
}

func applyNumber(sp *SeriesPoint, dp pmetric.NumberDataPoint) {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeInt:
		sp.exactInt = true
		sp.intVal = dp.IntValue()
		sp.Value = float64(sp.intVal)
	case pmetric.NumberDataPointValueTypeDouble:
		sp.Value = dp.DoubleValue()
	default:
		sp.Value = 0
	}
}
