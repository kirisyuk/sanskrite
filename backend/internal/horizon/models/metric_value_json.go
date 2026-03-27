package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reLooseNumber = regexp.MustCompile(`(?i)[^0-9.,_+\-e%kmnb ]+`)
	reSpaces      = regexp.MustCompile(`\s+`)
)

// UnmarshalJSON is intentionally tolerant for LLM-produced JSON.
//
// It accepts:
// - value / confidence as JSON number OR string ("45%", "10M", "1,234.56", "1 234,56")
// - observedAt as RFC3339/RFC3339Nano OR date-only "YYYY-MM-DD" (treated as midnight UTC)
//
// Rationale: It is better to keep partially-canonical data than to drop whole frames/edges due to strict parsing.
func (m *MetricValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*m = MetricValue{}
		return nil
	}

	// Accept scalar metric formats used by LLM outputs, e.g.:
	// { "metrics": { "ownership_percent": 45, "market_share": "12.5%" } }
	if trimmed[0] != '{' {
		var scalar any
		if err := json.Unmarshal(trimmed, &scalar); err != nil {
			return err
		}
		val, ok := parseLooseFloat(scalar)
		if !ok {
			return fmt.Errorf("metricValue: invalid scalar number: %v", scalar)
		}
		unit := inferMetricUnitFromScalar(scalar)
		*m = MetricValue{
			Value:      val,
			Unit:       unit,
			Confidence: 0.6,
			ObservedAt: time.Now().UTC(),
		}
		return nil
	}

	var raw struct {
		Value       any          `json:"value"`
		Unit        *string      `json:"unit"`
		ErrorMargin *ErrorMargin `json:"errorMargin"`
		Confidence  any          `json:"confidence"`
		ObservedAt  any          `json:"observedAt"`
		FreshUntil  any          `json:"freshUntil"`
		SourceKind  *string      `json:"sourceKind"`
		Provenance  Provenance   `json:"provenance"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	val, ok := parseLooseFloat(raw.Value)
	if !ok {
		return fmt.Errorf("metricValue.value: invalid number: %v", raw.Value)
	}

	conf := 0.6 // safe default
	if raw.Confidence != nil {
		if c, ok := parseLooseFloat(raw.Confidence); ok {
			conf = c
		}
	}
	// Normalize confidence: allow 0..100 style outputs.
	if conf > 1.0 && conf <= 100.0 {
		conf = conf / 100.0
	}
	if conf < 0 {
		conf = 0
	}
	if conf > 1.0 {
		conf = 1.0
	}

	obs := time.Now().UTC()
	if t, ok := parseLooseTime(raw.ObservedAt); ok {
		obs = t
	} else if raw.Provenance.ObservedAt != nil && !raw.Provenance.ObservedAt.IsZero() {
		obs = raw.Provenance.ObservedAt.UTC()
	}
	var freshUntil *time.Time
	if t, ok := parseLooseTime(raw.FreshUntil); ok {
		tt := t.UTC()
		freshUntil = &tt
	}

	m.Value = val
	m.Unit = raw.Unit
	m.ErrorMargin = raw.ErrorMargin
	m.Confidence = conf
	m.ObservedAt = obs
	m.FreshUntil = freshUntil
	m.SourceKind = raw.SourceKind
	m.Provenance = raw.Provenance
	return nil
}

func inferMetricUnitFromScalar(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if strings.Contains(strings.TrimSpace(s), "%") {
		unit := "percent"
		return &unit
	}
	return nil
}

func parseLooseTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		if t.IsZero() {
			return time.Time{}, false
		}
		return t.UTC(), true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}, false
		}
		// Try RFC3339 variants.
		if tt, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return tt.UTC(), true
		}
		if tt, err := time.Parse(time.RFC3339, s); err == nil {
			return tt.UTC(), true
		}
		// Date-only.
		if tt, err := time.Parse("2006-01-02", s); err == nil {
			return tt.UTC(), true
		}
		// Year-month.
		if tt, err := time.Parse("2006-01", s); err == nil {
			return time.Date(tt.Year(), tt.Month(), 1, 0, 0, 0, 0, time.UTC), true
		}
		// Year-only.
		if tt, err := time.Parse("2006", s); err == nil {
			return time.Date(tt.Year(), 1, 1, 0, 0, 0, 0, time.UTC), true
		}
		return time.Time{}, false
	case float64:
		// Unix seconds (best-effort). If looks like ms, convert.
		sec := t
		if sec > 1e12 {
			sec = sec / 1000.0
		}
		return time.Unix(int64(sec), 0).UTC(), true
	case int64:
		return time.Unix(t, 0).UTC(), true
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return time.Unix(i, 0).UTC(), true
		}
		if f, err := t.Float64(); err == nil {
			return time.Unix(int64(f), 0).UTC(), true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func parseLooseFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, true
		}
		return 0, false
	case string:
		return parseLooseFloatString(t)
	default:
		// Try JSON decoding for boxed primitives.
		b, err := json.Marshal(t)
		if err != nil {
			return 0, false
		}
		var n json.Number
		if err := json.Unmarshal(b, &n); err == nil {
			if f, err := n.Float64(); err == nil {
				return f, true
			}
		}
		return 0, false
	}
}

func parseLooseFloatString(in string) (float64, bool) {
	s := strings.TrimSpace(in)
	if s == "" {
		return 0, false
	}
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "~")
	s = strings.TrimPrefix(s, "≈")
	s = strings.TrimPrefix(s, "=")

	// Remove common non-numeric decorations, keep % and suffix letters.
	s = reLooseNumber.ReplaceAllString(s, "")
	s = reSpaces.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "_", "")

	mult := 1.0
	isPercent := strings.HasSuffix(s, "%")
	if isPercent {
		s = strings.TrimSuffix(s, "%")
	}

	// Suffix multipliers (k/m/b/n) and a couple of common forms.
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1e3
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult = 1e6
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "mn"):
		mult = 1e6
		s = strings.TrimSuffix(s, "mn")
	case strings.HasSuffix(s, "mln"):
		mult = 1e6
		s = strings.TrimSuffix(s, "mln")
	case strings.HasSuffix(s, "b"):
		mult = 1e9
		s = strings.TrimSuffix(s, "b")
	case strings.HasSuffix(s, "bn"):
		mult = 1e9
		s = strings.TrimSuffix(s, "bn")
	}

	// Heuristic for comma decimal separator.
	// If we have both '.' and ',', assume ',' is thousands separator and drop it.
	// If we only have ',', assume it's decimal separator and replace with '.'.
	if strings.Contains(s, ",") {
		if strings.Contains(s, ".") {
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.ReplaceAll(s, ",", ".")
		}
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	f = f * mult
	_ = isPercent // percent handling is caller/unit-specific; we only strip sign.
	return f, true
}
