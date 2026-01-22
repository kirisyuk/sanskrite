package profile

import (
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
			normalizeDatasetsContainer(sec, "datasets", st)
		}
	}

	// Market section shape: datasets in section object
	normalizeDatasetsContainer(in, "datasets", st)

	if st.changed > 0 {
		log.Printf("[ProfileGenerator] Repaired LLM JSON (changed=%d confidenceNorm=%d probNorm=%d sourcesNorm=%d yParsed=%d metaWrapped=%d)",
			st.changed, st.confidenceNormalized, st.probabilityNormalized, st.sourcesNormalized, st.yParsed, st.metaWrapped)
	}

	return in, nil
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

