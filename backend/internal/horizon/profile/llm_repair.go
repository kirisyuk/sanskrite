package profile

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

type profileRepairStats struct {
	changed               int
	confidenceNormalized  int
	probabilityNormalized int
	sourcesNormalized     int
	yParsed               int
	metaWrapped           int
	scenarioMetricsFixed  int
	factsFixed            int
}

func (s *profileRepairStats) bump(n *int) {
	*n++
	s.changed++
}

var percentRe = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*%\s*$`)

// repairProfileLLMJSON performs best-effort normalization of LLM JSON output to reduce schema-drift failures.
// It is intentionally permissive and focuses on the most common issues:
// - confidence/probability emitted as 95 instead of 0.95
// - sources emitted as string instead of []string
// - dataset points.y emitted as string
// - dataset points.meta emitted as string/bool instead of object
func repairProfileLLMJSON(in map[string]interface{}) (map[string]interface{}, error) {
	if in == nil {
		return in, nil
	}
	st := &profileRepairStats{}

	// Root-level normalizations (dashboard shape)
	normalizeConfidenceKey(in, "confidence", st)
	normalizeConfidenceKey(in, "overallConfidence", st)
	normalizeSourcesKey(in, "sources", st)

	// Dashboard arrays
	if scenarios, ok := in["scenarios"].([]interface{}); ok {
		for _, it := range scenarios {
			m, _ := it.(map[string]interface{})
			if m == nil {
				continue
			}
			normalizeProbabilityKey(m, "probability", st)
			normalizeScenarioMetrics(m, st)
		}
	}

	// Normalize datasets containers at root (dashboard)
	normalizeDatasetsContainer(in, "datasets", st)
	normalizeDatasetsContainer(in, "charts", st)
	normalizeDatasetsContainer(in, "tables", st)

	// Legacy shape: sections map
	if secMap, ok := in["sections"].(map[string]interface{}); ok {
		for _, v := range secMap {
			sec, _ := v.(map[string]interface{})
			if sec == nil {
				continue
			}
			normalizeConfidenceKey(sec, "confidence", st)
			normalizeSourcesKey(sec, "sources", st)
			normalizeFactsContainer(sec, st)
			normalizeDatasetsContainer(sec, "datasets", st)
		}
	}

	// Market section shape: datasets in section object
	normalizeDatasetsContainer(in, "datasets", st)

	if st.changed > 0 {
		log.Printf("[ProfileGenerator] Repaired LLM JSON (changed=%d confidenceNorm=%d probNorm=%d sourcesNorm=%d yParsed=%d metaWrapped=%d scenarioMetrics=%d facts=%d)",
			st.changed, st.confidenceNormalized, st.probabilityNormalized, st.sourcesNormalized, st.yParsed, st.metaWrapped, st.scenarioMetricsFixed, st.factsFixed)
	}

	return in, nil
}

// normalizeFactsContainer ensures section facts are present/normalized and avoids raw JSON dumps.
// Preferred: facts: [{key,label,value,unit?,period?,notes?,sources?}]
// Legacy: structured: { "market_size_2023": "$693.7B", ... } or structured as a JSON string.
func normalizeFactsContainer(sec map[string]interface{}, st *profileRepairStats) {
	// If facts missing, derive from structured when possible.
	if _, ok := sec["facts"]; !ok {
		if raw, ok := sec["structured"]; ok && raw != nil {
			items := structuredToFacts(raw, st)
			if len(items) > 0 {
				sec["facts"] = items
				st.bump(&st.factsFixed)
			}
		}
		return
	}

	raw := sec["facts"]
	var arr []interface{}
	switch t := raw.(type) {
	case []interface{}:
		arr = t
	case map[string]interface{}:
		arr = []interface{}{t}
		sec["facts"] = arr
		st.bump(&st.factsFixed)
	case string:
		parsed := structuredToFacts(t, st)
		if len(parsed) > 0 {
			sec["facts"] = parsed
			st.bump(&st.factsFixed)
		} else {
			delete(sec, "facts")
			st.bump(&st.factsFixed)
		}
		return
	default:
		delete(sec, "facts")
		st.bump(&st.factsFixed)
		return
	}

	out := make([]interface{}, 0, len(arr))
	for _, it := range arr {
		m, _ := it.(map[string]interface{})
		if m == nil {
			continue
		}
		normalizeFactItem(m, st)
		if strings.TrimSpace(fmt.Sprint(m["label"])) == "" {
			k := strings.TrimSpace(fmt.Sprint(m["key"]))
			lbl, period, unit := humanizeKey(k)
			if lbl != "" {
				m["label"] = lbl
				st.bump(&st.factsFixed)
			}
			if strings.TrimSpace(fmt.Sprint(m["period"])) == "" && period != "" {
				m["period"] = period
				st.bump(&st.factsFixed)
			}
			if strings.TrimSpace(fmt.Sprint(m["unit"])) == "" && unit != "" {
				m["unit"] = unit
				st.bump(&st.factsFixed)
			}
		}
		// Must have label+value to be useful
		if strings.TrimSpace(fmt.Sprint(m["label"])) == "" || strings.TrimSpace(fmt.Sprint(m["value"])) == "" {
			continue
		}
		out = append(out, m)
	}
	sec["facts"] = out
}

func normalizeFactItem(m map[string]interface{}, st *profileRepairStats) {
	if v, ok := m["key"]; ok && v != nil {
		if _, ok := v.(string); !ok {
			m["key"] = fmt.Sprint(v)
			st.bump(&st.factsFixed)
		}
	}
	if v, ok := m["label"]; ok && v != nil {
		if _, ok := v.(string); !ok {
			m["label"] = fmt.Sprint(v)
			st.bump(&st.factsFixed)
		}
	}
	if v, ok := m["value"]; ok && v != nil {
		if _, ok := v.(string); !ok {
			m["value"] = fmt.Sprint(v)
			st.bump(&st.factsFixed)
		}
	}
	normalizeSourcesKey(m, "sources", st)
}

func structuredToFacts(raw interface{}, st *profileRepairStats) []interface{} {
	switch t := raw.(type) {
	case map[string]interface{}:
		out := make([]interface{}, 0, len(t))
		for k, v := range t {
			key := strings.TrimSpace(k)
			if key == "" || v == nil {
				continue
			}
			// keep simple values; complex objects become JSON string
			val := ""
			switch vv := v.(type) {
			case string:
				val = strings.TrimSpace(vv)
			case float64, int, int64, bool:
				val = strings.TrimSpace(fmt.Sprint(v))
			default:
				// If it's an array of strings, join.
				if arr, ok := v.([]interface{}); ok {
					ss := make([]string, 0, len(arr))
					for _, it := range arr {
						if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
							ss = append(ss, strings.TrimSpace(s))
						}
					}
					if len(ss) > 0 {
						val = strings.Join(ss, "; ")
					}
				}
				if val == "" {
					b, err := fmtJSON(v)
					if err == nil && strings.TrimSpace(b) != "" {
						val = b
					}
				}
			}
			if val == "" {
				continue
			}
			lbl, period, unit := humanizeKey(key)
			item := map[string]interface{}{
				"key":   key,
				"label": firstNonEmpty(lbl, key),
				"value": val,
			}
			if period != "" {
				item["period"] = period
			}
			if unit != "" {
				item["unit"] = unit
			}
			out = append(out, item)
		}
		return out
	case string:
		// Try parse as JSON object first
		var obj map[string]interface{}
		if err := jsonUnmarshalLoose(t, &obj); err == nil && obj != nil {
			return structuredToFacts(obj, st)
		}
		// Fallback: "k: v" lines
		lines := strings.Split(t, "\n")
		out := make([]interface{}, 0, len(lines))
		for _, ln := range lines {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			parts := strings.SplitN(ln, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "" || val == "" {
				continue
			}
			lbl, period, unit := humanizeKey(key)
			item := map[string]interface{}{
				"key":   key,
				"label": firstNonEmpty(lbl, key),
				"value": val,
			}
			if period != "" {
				item["period"] = period
			}
			if unit != "" {
				item["unit"] = unit
			}
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func humanizeKey(key string) (label string, period string, unit string) {
	// Reuse the scenario key humanizer; it's generic enough.
	return humanizeScenarioMetricKey(key)
}

// fmtJSON tries to marshal a value into compact JSON string.
func fmtJSON(v interface{}) (string, error) {
	// Use fmt.Sprint for non-marshalable (rare), but try best effort.
	b, err := jsonMarshalLoose(v)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(b), nil
}

func jsonMarshalLoose(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// jsonUnmarshalLoose tries to parse JSON even if it's wrapped in code fences or contains leading/trailing text.
func jsonUnmarshalLoose(s string, out interface{}) error {
	ss := strings.TrimSpace(s)
	ss = strings.TrimPrefix(ss, "```json")
	ss = strings.TrimPrefix(ss, "```JSON")
	ss = strings.TrimPrefix(ss, "```")
	ss = strings.TrimSuffix(ss, "```")
	ss = strings.TrimSpace(ss)
	// If still not pure JSON, try extract first object.
	if !strings.HasPrefix(ss, "{") {
		i := strings.Index(ss, "{")
		j := strings.LastIndex(ss, "}")
		if i >= 0 && j > i {
			ss = ss[i : j+1]
		}
	}
	return json.Unmarshal([]byte(ss), out)
}


// normalizeScenarioMetrics converts legacy/LLM-drifting scenario metrics into a stable, human-friendly shape.
// Expected preferred field: metricItems: [{key,label,value,unit?,period?,notes?,sources?}]
// Legacy: metrics: { "2032_Market_Valuation": "$1.3T" } OR metrics as a string like "foo: bar".
func normalizeScenarioMetrics(scenario map[string]interface{}, st *profileRepairStats) {
	// If metricItems is missing, try to derive it from metrics.
	if _, ok := scenario["metricItems"]; !ok {
		if raw, ok := scenario["metrics"]; ok && raw != nil {
			items := scenarioMetricsToItems(raw, st)
			if len(items) > 0 {
				scenario["metricItems"] = items
				st.bump(&st.scenarioMetricsFixed)
			}
		}
		return
	}

	// Normalize metricItems container.
	raw := scenario["metricItems"]
	var arr []interface{}
	switch t := raw.(type) {
	case []interface{}:
		arr = t
	case map[string]interface{}:
		arr = []interface{}{t}
		scenario["metricItems"] = arr
		st.bump(&st.scenarioMetricsFixed)
	case string:
		// Sometimes emitted as a single string; parse as "k: v" lines.
		parsed := scenarioMetricsToItems(t, st)
		if len(parsed) > 0 {
			scenario["metricItems"] = parsed
			st.bump(&st.scenarioMetricsFixed)
		} else {
			delete(scenario, "metricItems")
			st.bump(&st.scenarioMetricsFixed)
		}
		return
	default:
		delete(scenario, "metricItems")
		st.bump(&st.scenarioMetricsFixed)
		return
	}

	out := make([]interface{}, 0, len(arr))
	for _, it := range arr {
		m, _ := it.(map[string]interface{})
		if m == nil {
			continue
		}
		normalizeScenarioMetricItem(m, st)
		// Must have label+value to be useful.
		if strings.TrimSpace(fmt.Sprint(m["label"])) == "" && strings.TrimSpace(fmt.Sprint(m["key"])) == "" {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(m["value"])) == "" {
			// Try fallback from legacy "val" or "metric".
			if v := strings.TrimSpace(fmt.Sprint(m["val"])); v != "" {
				m["value"] = v
				delete(m, "val")
				st.bump(&st.scenarioMetricsFixed)
			} else if v := strings.TrimSpace(fmt.Sprint(m["metric"])); v != "" {
				m["value"] = v
				delete(m, "metric")
				st.bump(&st.scenarioMetricsFixed)
			}
		}
		if strings.TrimSpace(fmt.Sprint(m["label"])) == "" {
			k := strings.TrimSpace(fmt.Sprint(m["key"]))
			lbl, period, unit := humanizeScenarioMetricKey(k)
			if lbl != "" {
				m["label"] = lbl
				st.bump(&st.scenarioMetricsFixed)
			}
			if strings.TrimSpace(fmt.Sprint(m["period"])) == "" && period != "" {
				m["period"] = period
				st.bump(&st.scenarioMetricsFixed)
			}
			if strings.TrimSpace(fmt.Sprint(m["unit"])) == "" && unit != "" {
				m["unit"] = unit
				st.bump(&st.scenarioMetricsFixed)
			}
		}
		out = append(out, m)
	}
	scenario["metricItems"] = out
}

func normalizeScenarioMetricItem(m map[string]interface{}, st *profileRepairStats) {
	// key/label/value must be strings
	if v, ok := m["key"]; ok && v != nil {
		if _, ok := v.(string); !ok {
			m["key"] = fmt.Sprint(v)
			st.bump(&st.scenarioMetricsFixed)
		}
	}
	if v, ok := m["label"]; ok && v != nil {
		if _, ok := v.(string); !ok {
			m["label"] = fmt.Sprint(v)
			st.bump(&st.scenarioMetricsFixed)
		}
	}
	if v, ok := m["value"]; ok && v != nil {
		if _, ok := v.(string); !ok {
			m["value"] = fmt.Sprint(v)
			st.bump(&st.scenarioMetricsFixed)
		}
	}
	normalizeSourcesKey(m, "sources", st)
}

func scenarioMetricsToItems(raw interface{}, st *profileRepairStats) []interface{} {
	switch t := raw.(type) {
	case map[string]interface{}:
		out := make([]interface{}, 0, len(t))
		for k, v := range t {
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(fmt.Sprint(v))
			if key == "" || val == "" {
				continue
			}
			lbl, period, unit := humanizeScenarioMetricKey(key)
			item := map[string]interface{}{
				"key":   key,
				"label": firstNonEmpty(lbl, key),
				"value": val,
			}
			if period != "" {
				item["period"] = period
			}
			if unit != "" {
				item["unit"] = unit
			}
			out = append(out, item)
		}
		return out
	case map[string]string:
		out := make([]interface{}, 0, len(t))
		for k, v := range t {
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			if key == "" || val == "" {
				continue
			}
			lbl, period, unit := humanizeScenarioMetricKey(key)
			item := map[string]interface{}{
				"key":   key,
				"label": firstNonEmpty(lbl, key),
				"value": val,
			}
			if period != "" {
				item["period"] = period
			}
			if unit != "" {
				item["unit"] = unit
			}
			out = append(out, item)
		}
		return out
	case []interface{}:
		// Sometimes emitted as list of strings like "k: v"
		out := make([]interface{}, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok {
				out = append(out, scenarioMetricsToItems(s, st)...)
			}
		}
		return out
	case string:
		lines := strings.Split(t, "\n")
		out := make([]interface{}, 0, len(lines))
		for _, ln := range lines {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			// allow "key: value" or "key - value"
			sep := ":"
			if !strings.Contains(ln, ":") && strings.Contains(ln, " - ") {
				sep = " - "
			}
			parts := strings.SplitN(ln, sep, 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "" || val == "" {
				continue
			}
			lbl, period, unit := humanizeScenarioMetricKey(key)
			item := map[string]interface{}{
				"key":   key,
				"label": firstNonEmpty(lbl, key),
				"value": val,
			}
			if period != "" {
				item["period"] = period
			}
			if unit != "" {
				item["unit"] = unit
			}
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// humanizeScenarioMetricKey attempts to turn "2032_Market_Valuation" into ("Market valuation","2032","").
// Also infers unit if key ends with _usd/_eur/_percent/_pct.
func humanizeScenarioMetricKey(key string) (label string, period string, unit string) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", "", ""
	}
	// normalize spaces
	k = strings.ReplaceAll(k, " ", "_")
	k = strings.ReplaceAll(k, "__", "_")
	parts := strings.Split(k, "_")
	if len(parts) == 0 {
		return "", "", ""
	}
	// Leading year
	if len(parts[0]) == 4 {
		if _, err := strconv.Atoi(parts[0]); err == nil {
			period = parts[0]
			parts = parts[1:]
		}
	}
	// Unit suffix
	if len(parts) > 0 {
		last := strings.ToLower(parts[len(parts)-1])
		switch last {
		case "usd", "eur", "gbp":
			unit = strings.ToUpper(last)
			parts = parts[:len(parts)-1]
		case "percent", "pct":
			unit = "percent"
			parts = parts[:len(parts)-1]
		}
	}
	// Human label: Title-case with some acronym handling.
	words := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		low := strings.ToLower(p)
		switch low {
		case "ai":
			words = append(words, "AI")
		case "eu", "us", "sea":
			words = append(words, strings.ToUpper(low))
		case "mfg":
			words = append(words, "Manufacturing")
		default:
			words = append(words, strings.ToUpper(low[:1])+low[1:])
		}
	}
	label = strings.Join(words, " ")
	return strings.TrimSpace(label), strings.TrimSpace(period), strings.TrimSpace(unit)
}

func normalizeProbabilityKey(m map[string]interface{}, key string, st *profileRepairStats) {
	v, ok := m[key]
	if !ok {
		return
	}
	if f, ok := coercePercentOrNumberToUnitFloat(v); ok {
		if f < 0 {
			f = 0
		} else if f > 1 {
			f = 1
		}
		m[key] = f
		st.bump(&st.probabilityNormalized)
	}
}

func normalizeConfidenceKey(m map[string]interface{}, key string, st *profileRepairStats) {
	v, ok := m[key]
	if !ok {
		return
	}
	if f, ok := coercePercentOrNumberToUnitFloat(v); ok {
		if f < 0 {
			f = 0
		} else if f > 1 {
			f = 1
		}
		m[key] = f
		st.bump(&st.confidenceNormalized)
	}
}

func normalizeSourcesKey(m map[string]interface{}, key string, st *profileRepairStats) {
	v, ok := m[key]
	if !ok || v == nil {
		return
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			delete(m, key)
			st.bump(&st.sourcesNormalized)
			return
		}
		m[key] = []interface{}{t}
		st.bump(&st.sourcesNormalized)
	case []interface{}:
		out := make([]interface{}, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		m[key] = out
		st.bump(&st.sourcesNormalized)
	case []string:
		out := make([]interface{}, 0, len(t))
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		m[key] = out
		st.bump(&st.sourcesNormalized)
	default:
		// Unknown type; drop rather than fail.
		delete(m, key)
		st.bump(&st.sourcesNormalized)
	}
}

func normalizeDatasetsContainer(m map[string]interface{}, key string, st *profileRepairStats) {
	raw, ok := m[key]
	if !ok || raw == nil {
		return
	}

	var arr []interface{}
	switch t := raw.(type) {
	case []interface{}:
		arr = t
	case map[string]interface{}:
		arr = []interface{}{t}
	default:
		// Not a dataset container; drop.
		delete(m, key)
		st.bump(&st.changed)
		return
	}

	out := make([]interface{}, 0, len(arr))
	for _, it := range arr {
		ds, _ := it.(map[string]interface{})
		if ds == nil {
			continue
		}
		normalizeDataset(ds, st)
		out = append(out, ds)
	}
	m[key] = out
}

func normalizeDataset(ds map[string]interface{}, st *profileRepairStats) {
	normalizeConfidenceKey(ds, "confidence", st)
	normalizeSourcesKey(ds, "sources", st)

	typ, _ := ds["type"].(string)
	typ = strings.TrimSpace(strings.ToLower(typ))
	if typ == "" {
		// Infer
		if _, ok := ds["table"].(map[string]interface{}); ok {
			ds["type"] = "table"
		}
	}

	if strings.ToLower(fmt.Sprint(ds["type"])) == "table" {
		normalizeTable(ds, st)
		return
	}
	normalizeSeries(ds, st)
	// If table exists for non-table types but is not an object, drop it.
	if t, ok := ds["table"]; ok && t != nil {
		if _, ok := t.(map[string]interface{}); !ok {
			delete(ds, "table")
			st.bump(&st.changed)
		}
	}
}

func normalizeTable(ds map[string]interface{}, st *profileRepairStats) {
	t, ok := ds["table"].(map[string]interface{})
	if !ok || t == nil {
		return
	}
	// columns
	if cols, ok := t["columns"]; ok && cols != nil {
		switch c := cols.(type) {
		case []interface{}:
			// keep as-is
		case map[string]interface{}:
			t["columns"] = []interface{}{c}
			st.bump(&st.changed)
		default:
			delete(t, "columns")
			st.bump(&st.changed)
		}
	}
	// rows
	if rows, ok := t["rows"]; ok && rows != nil {
		switch r := rows.(type) {
		case []interface{}:
			// keep
		case map[string]interface{}:
			t["rows"] = []interface{}{r}
			st.bump(&st.changed)
		default:
			delete(t, "rows")
			st.bump(&st.changed)
		}
	}
}

func normalizeSeries(ds map[string]interface{}, st *profileRepairStats) {
	raw := ds["series"]
	if raw == nil {
		return
	}
	var seriesArr []interface{}
	switch t := raw.(type) {
	case []interface{}:
		seriesArr = t
	case map[string]interface{}:
		seriesArr = []interface{}{t}
		ds["series"] = seriesArr
		st.bump(&st.changed)
	default:
		delete(ds, "series")
		st.bump(&st.changed)
		return
	}

	for _, s := range seriesArr {
		sm, _ := s.(map[string]interface{})
		if sm == nil {
			continue
		}
		normalizePoints(sm, st)
	}
}

func normalizePoints(series map[string]interface{}, st *profileRepairStats) {
	raw := series["points"]
	if raw == nil {
		return
	}
	var pts []interface{}
	switch t := raw.(type) {
	case []interface{}:
		pts = t
	case map[string]interface{}:
		pts = []interface{}{t}
		series["points"] = pts
		st.bump(&st.changed)
	default:
		delete(series, "points")
		st.bump(&st.changed)
		return
	}

	for _, p := range pts {
		pm, _ := p.(map[string]interface{})
		if pm == nil {
			continue
		}
		// x -> string
		if x, ok := pm["x"]; ok && x != nil {
			if _, ok := x.(string); !ok {
				pm["x"] = fmt.Sprint(x)
				st.bump(&st.changed)
			}
		}
		// y -> number
		if y, ok := pm["y"]; ok && y != nil {
			switch yy := y.(type) {
			case float64:
				// ok
			case int:
				pm["y"] = float64(yy)
				st.bump(&st.yParsed)
			case int64:
				pm["y"] = float64(yy)
				st.bump(&st.yParsed)
			case bool:
				if yy {
					pm["y"] = float64(1)
				} else {
					pm["y"] = float64(0)
				}
				st.bump(&st.yParsed)
			case string:
				f, ok := parseFloatLoose(yy)
				if ok {
					pm["y"] = f
					st.bump(&st.yParsed)
				} else {
					// drop invalid
					delete(pm, "y")
					st.bump(&st.changed)
				}
			default:
				delete(pm, "y")
				st.bump(&st.changed)
			}
		}
		// meta must be an object (map) for stable downstream usage.
		if meta, ok := pm["meta"]; ok && meta != nil {
			if _, ok := meta.(map[string]interface{}); !ok {
				pm["meta"] = map[string]interface{}{"value": meta}
				st.bump(&st.metaWrapped)
			}
		}
	}
}

func coercePercentOrNumberToUnitFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		if t > 1 && t <= 100 {
			return t / 100.0, true
		}
		return t, true
	case int:
		f := float64(t)
		if f > 1 && f <= 100 {
			return f / 100.0, true
		}
		return f, true
	case int64:
		f := float64(t)
		if f > 1 && f <= 100 {
			return f / 100.0, true
		}
		return f, true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		if m := percentRe.FindStringSubmatch(s); len(m) == 2 {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				return f / 100.0, true
			}
		}
		if f, ok := parseFloatLoose(s); ok {
			if f > 1 && f <= 100 {
				return f / 100.0, true
			}
			return f, true
		}
	}
	return 0, false
}

func parseFloatLoose(s string) (float64, bool) {
	ss := strings.TrimSpace(s)
	ss = strings.TrimSuffix(ss, ",")
	ss = strings.ReplaceAll(ss, "_", "")
	ss = strings.ReplaceAll(ss, " ", "")
	if ss == "" {
		return 0, false
	}
	// Allow leading "+".
	ss = strings.TrimPrefix(ss, "+")
	f, err := strconv.ParseFloat(ss, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

