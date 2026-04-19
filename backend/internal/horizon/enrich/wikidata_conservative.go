package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ConservativeWikidataOptions controls enrichment behavior.
// This is intentionally conservative and deterministic:
// - It never calls Wikidata Search; only GetEntityData when a QID is already present.
// - It type-gates PERSON vs ORGANIZATION using P31/Q5.
type ConservativeWikidataOptions struct {
	// Force ignores TTL and overwrites fields even if recently synced.
	Force bool
	// MaxAge is the TTL for Wikidata KBRef LastSync. If <=0, defaults to 7 days.
	MaxAge time.Duration
	// Overwrite controls whether we overwrite existing fields (labels/description/legalName/shortName).
	// In our current data model, manual data is not expected, so Overwrite=true is typically safe.
	Overwrite bool
	// DisableCoordinates keeps Wikidata from setting fragment coordinates.
	// Use in the core graph pipeline where geocoding is the source of truth for spatial identity.
	DisableCoordinates bool
}

func (o ConservativeWikidataOptions) withDefaults() ConservativeWikidataOptions {
	out := o
	if out.MaxAge <= 0 {
		out.MaxAge = 7 * 24 * time.Hour
	}
	return out
}

// EnrichFragmentFromWikidataIfPresent enriches a fragment in-place using Wikidata entity data,
// but ONLY when a Wikidata QID is already present (KBRefs or identifiers.wikidataId).
//
// Returns changed=true if any field was modified.
func EnrichFragmentFromWikidataIfPresent(
	ctx context.Context,
	client coreontology.WikidataClientInterface,
	fragment *hmodels.HorizonFragment,
	opts ConservativeWikidataOptions,
) (changed bool, err error) {
	if client == nil || fragment == nil {
		return false, nil
	}
	opts = opts.withDefaults()

	qid := strings.TrimSpace(extractWikidataIDFromFragment(fragment))
	if qid == "" || !strings.HasPrefix(qid, "Q") {
		return false, nil
	}

	// TTL guard (KBRefs.Wikidata.LastSync).
	if !opts.Force {
		if t := wikidataLastSync(fragment); t != nil {
			if time.Since(t.UTC()) < opts.MaxAge {
				return false, nil
			}
		}
	}

	ed, err := client.GetEntityData(ctx, qid)
	if err != nil || ed == nil {
		return false, fmt.Errorf("GetEntityData(%s) failed: %w", qid, err)
	}
	changed, err = EnrichFragmentFromEntityData(fragment, ed, time.Now().UTC(), opts)
	if err != nil {
		return changed, err
	}
	if hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return changed, nil
	}

	// Fallback: some entities don't carry P625 directly, but have location links
	// (e.g., headquarters location P159) that do carry P625.
	locationQID := extractWikidataLocationEntityQID(ed.PropertiesRaw)
	if locationQID == "" || locationQID == qid {
		return changed, nil
	}
	locData, locErr := client.GetEntityData(ctx, locationQID)
	if locErr != nil || locData == nil {
		return changed, nil
	}
	if !opts.DisableCoordinates && applyCoordinatesFromWikidata(fragment, locData.PropertiesRaw, opts.Overwrite) {
		changed = true
	}
	return changed, nil
}

// EnrichFragmentFromEntityData applies enrichment from an already-fetched Wikidata entity data.
// The fragment must already have the correct wikidataId linkage (identifiers/KBRefs).
func EnrichFragmentFromEntityData(
	fragment *hmodels.HorizonFragment,
	ed *coreontology.WikidataEntityData,
	now time.Time,
	opts ConservativeWikidataOptions,
) (changed bool, err error) {
	if fragment == nil || ed == nil {
		return false, nil
	}
	opts = opts.withDefaults()

	qid := strings.TrimSpace(ed.ID)
	if qid == "" {
		// Fallback: try from fragment.
		qid = strings.TrimSpace(extractWikidataIDFromFragment(fragment))
	}
	if qid == "" || !strings.HasPrefix(qid, "Q") {
		return false, nil
	}

	// Coordinates from Wikidata P625 for all fragment kinds (best-effort).
	if !opts.DisableCoordinates && applyCoordinatesFromWikidata(fragment, ed.PropertiesRaw, opts.Overwrite) {
		changed = true
	}

	if fragment.Class == FragmentTypePerson || fragment.Class == FragmentTypeOrganization {
		// Type-gating (avoid applying human data to orgs and vice versa).
		p31 := extractEntityIDs(ed.PropertiesRaw, "P31")
		isHuman := false
		for _, x := range p31 {
			if x == "Q5" {
				isHuman = true
				break
			}
		}
		if fragment.Class == FragmentTypePerson && !isHuman {
			return false, fmt.Errorf("qid %s is not a human (P31=%v)", qid, p31)
		}
		if fragment.Class == FragmentTypeOrganization && isHuman {
			return false, fmt.Errorf("qid %s is a human (unexpected for org)", qid)
		}

		// Ensure maps exist
		if fragment.Labels == nil {
			fragment.Labels = map[string]string{}
		}
		if fragment.Identifiers == nil {
			fragment.Identifiers = map[string]string{}
		}

		// labels.en + description
		if s := strings.TrimSpace(ed.Label); s != "" {
			if opts.Overwrite || strings.TrimSpace(fragment.Labels["en"]) == "" {
				if fragment.Labels["en"] != s {
					fragment.Labels["en"] = s
					changed = true
				}
			}
		}
		if s := strings.TrimSpace(ed.Description); s != "" {
			if opts.Overwrite || strings.TrimSpace(fragment.Description) == "" {
				if fragment.Description != s {
					fragment.Description = s
					changed = true
				}
			}
		}

		// Official name (P1448) and short name (P1813)
		official := strings.TrimSpace(firstNonEmptyMonolingual(ed.PropertiesRaw, "P1448"))
		short := strings.TrimSpace(firstNonEmptyMonolingual(ed.PropertiesRaw, "P1813"))
		if official != "" {
			if opts.Overwrite || strings.TrimSpace(fragment.Identifiers["legalName"]) == "" {
				if fragment.Identifiers["legalName"] != official {
					fragment.Identifiers["legalName"] = official
					changed = true
				}
			}
		}
		if short != "" {
			if opts.Overwrite || strings.TrimSpace(fragment.Identifiers["shortName"]) == "" {
				if fragment.Identifiers["shortName"] != short {
					fragment.Identifiers["shortName"] = short
					changed = true
				}
			}
		}

		// IndustryPaths from P452 (ORG only).
		if fragment.Class == FragmentTypeOrganization {
			inds := extractEntityIDs(ed.PropertiesRaw, "P452")
			if len(inds) > 0 {
				paths := taxonomy.NormalizeIndustryPaths(
					taxonomy.MapIndustryQIDsToPaths(inds),
					string(fragment.Class),
				)
				if len(paths) > 0 {
					// Overwrite is fine; these are derived.
					if !stringSlicesEqual(fragment.IndustryPaths, paths) {
						fragment.IndustryPaths = paths
						changed = true
					}
				}
			}
		}

		// NameHistory: populate from Wikidata aliases and former names (P5768).
		if populateNameHistoryFromWikidata(fragment, ed) {
			changed = true
		}
	}

	// Mark Wikidata sync time on the fragment's Wikidata KBRef.
	if setWikidataLastSync(fragment, now) {
		changed = true
	}

	// Best-effort: keep identifiers/KBRefs mirrored and indexPaths updated.
	_ = fragment.Validate()
	return changed, nil
}

func applyCoordinatesFromWikidata(fragment *hmodels.HorizonFragment, props map[string]interface{}, overwrite bool) bool {
	if fragment == nil || props == nil {
		return false
	}
	lat, lon, ok := extractWikidataCoordinates(props)
	if !ok {
		return false
	}

	changed := false
	existingReal := hmodels.HasRealGeoCoordinates(fragment.Coordinates)
	if overwrite || !existingReal {
		if !existingReal || fragment.Coordinates.Lat != lat || fragment.Coordinates.Lon != lon {
			fragment.Coordinates = &hmodels.Coordinates{Lat: lat, Lon: lon}
			changed = true
		}
	}

	iso := fmt.Sprintf("%+.6f%+.6f/", lat, lon)
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
		changed = true
	}
	currentISO := ""
	if fragment.HomeLocation.ISO6709 != nil {
		currentISO = strings.TrimSpace(*fragment.HomeLocation.ISO6709)
	}
	if overwrite || currentISO == "" {
		if currentISO != iso {
			fragment.HomeLocation.ISO6709 = &iso
			changed = true
		}
	}
	return changed
}

func extractWikidataCoordinates(props map[string]interface{}) (float64, float64, bool) {
	if props == nil {
		return 0, 0, false
	}
	raw, ok := props["P625"]
	if !ok || raw == nil {
		return 0, 0, false
	}
	return extractWikidataCoordinatesValue(raw)
}

func extractWikidataCoordinatesValue(raw interface{}) (float64, float64, bool) {
	if raw == nil {
		return 0, 0, false
	}
	switch t := raw.(type) {
	case []interface{}:
		for _, it := range t {
			if lat, lon, ok := extractWikidataCoordinatesValue(it); ok {
				return lat, lon, true
			}
		}
		return 0, 0, false
	case map[string]interface{}:
		if lat, ok := anyToFloat64(t["latitude"]); ok {
			if lon, ok := anyToFloat64(t["longitude"]); ok && hmodels.IsValidCoordinates(lat, lon) {
				return lat, lon, true
			}
		}
		if lat, ok := anyToFloat64(t["lat"]); ok {
			if lon, ok := anyToFloat64(t["lon"]); ok && hmodels.IsValidCoordinates(lat, lon) {
				return lat, lon, true
			}
			if lng, ok := anyToFloat64(t["lng"]); ok && hmodels.IsValidCoordinates(lat, lng) {
				return lat, lng, true
			}
		}
		if v, ok := t["value"]; ok {
			return extractWikidataCoordinatesValue(v)
		}
		if v, ok := t["datavalue"]; ok {
			return extractWikidataCoordinatesValue(v)
		}
		if v, ok := t["mainsnak"]; ok {
			return extractWikidataCoordinatesValue(v)
		}
		return 0, 0, false
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, 0, false
		}
		if lat, lon, ok := hmodels.ParseISO6709Coordinates(s); ok {
			return lat, lon, true
		}
		if strings.HasPrefix(strings.ToLower(s), "point(") && strings.HasSuffix(s, ")") {
			inner := strings.TrimSpace(s[len("Point(") : len(s)-1])
			parts := strings.Fields(inner)
			if len(parts) == 2 {
				lon, lonOK := strconv.ParseFloat(parts[0], 64)
				lat, latOK := strconv.ParseFloat(parts[1], 64)
				if latOK == nil && lonOK == nil && hmodels.IsValidCoordinates(lat, lon) {
					return lat, lon, true
				}
			}
		}
		if strings.Contains(s, ",") {
			parts := strings.Split(s, ",")
			if len(parts) == 2 {
				lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
				lon, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if latErr == nil && lonErr == nil && hmodels.IsValidCoordinates(lat, lon) {
					return lat, lon, true
				}
			}
		}
		return 0, 0, false
	default:
		return 0, 0, false
	}
}

func anyToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// populateNameHistoryFromWikidata extracts aliases and former names from Wikidata entity data
// and merges them into fragment.NameHistory. Returns true if any entries were added.
func populateNameHistoryFromWikidata(fragment *hmodels.HorizonFragment, ed *coreontology.WikidataEntityData) bool {
	if fragment == nil || ed == nil {
		return false
	}

	// Collect existing name history entries to avoid duplicates.
	existing := make(map[string]struct{}, len(fragment.NameHistory))
	for _, e := range fragment.NameHistory {
		key := strings.ToLower(strings.TrimSpace(e.Kind)) + "|" + strings.ToLower(strings.TrimSpace(e.Name))
		existing[key] = struct{}{}
	}

	// Also skip names that match current labels/identifiers (not historical).
	currentNames := make(map[string]struct{})
	if fragment.Labels != nil {
		for _, v := range fragment.Labels {
			if v := strings.TrimSpace(v); v != "" {
				currentNames[strings.ToLower(v)] = struct{}{}
			}
		}
	}
	if fragment.Identifiers != nil {
		for _, k := range []string{"legalName", "shortName"} {
			if v := strings.TrimSpace(fragment.Identifiers[k]); v != "" {
				currentNames[strings.ToLower(v)] = struct{}{}
			}
		}
	}

	addEntry := func(name, kind string) bool {
		name = strings.TrimSpace(name)
		if name == "" {
			return false
		}
		lc := strings.ToLower(name)
		// Skip if it's the same as a current name.
		if _, ok := currentNames[lc]; ok {
			return false
		}
		key := kind + "|" + lc
		if _, ok := existing[key]; ok {
			return false
		}
		existing[key] = struct{}{}
		fragment.NameHistory = append(fragment.NameHistory, hmodels.NameHistoryEntry{
			Name: name,
			Kind: kind,
		})
		return true
	}

	added := false

	// 1. Former official names from P5768 (monolingual text values).
	formerNames := allMonolingualValues(ed.PropertiesRaw, "P5768")
	for _, fn := range formerNames {
		if addEntry(fn, "legal") {
			added = true
		}
	}

	// 2. Nicknames from P1449.
	nicknames := allMonolingualValues(ed.PropertiesRaw, "P1449")
	for _, nn := range nicknames {
		if addEntry(nn, "aka") {
			added = true
		}
	}

	// 3. Wikidata aliases (English) — treat as "aka".
	for _, alias := range ed.Aliases {
		if addEntry(alias, "aka") {
			added = true
		}
	}

	return added
}

// allMonolingualValues extracts all monolingual text values for a property from PropertiesRaw.
func allMonolingualValues(props map[string]interface{}, propID string) []string {
	if props == nil {
		return nil
	}
	raw, ok := props[propID]
	if !ok || raw == nil {
		return nil
	}
	var out []string
	switch t := raw.(type) {
	case []interface{}:
		for _, it := range t {
			if s := monolingualToString(it); strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		if s := strings.TrimSpace(t); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func wikidataLastSync(f *hmodels.HorizonFragment) *time.Time {
	if f == nil {
		return nil
	}
	for _, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "Wikidata") {
			if ref.LastSync != nil {
				t := ref.LastSync.UTC()
				return &t
			}
			return nil
		}
	}
	return nil
}

func setWikidataLastSync(f *hmodels.HorizonFragment, now time.Time) bool {
	if f == nil {
		return false
	}
	now = now.UTC()
	for i := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(f.KBRefs[i].KB), "Wikidata") {
			// Ensure stable casing.
			f.KBRefs[i].KB = "Wikidata"
			if f.KBRefs[i].LastSync == nil || !f.KBRefs[i].LastSync.UTC().Equal(now) {
				t := now
				f.KBRefs[i].LastSync = &t
				return true
			}
			return false
		}
	}
	// If missing, don't create a KBRef here; fragment.Validate() mirroring will add a KBRef from identifiers if needed.
	return false
}

func extractEntityIDs(props map[string]interface{}, propID string) []string {
	if props == nil {
		return nil
	}
	raw, ok := props[propID]
	if !ok || raw == nil {
		return nil
	}
	var out []string
	switch t := raw.(type) {
	case []interface{}:
		for _, it := range t {
			if s, ok := it.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
	case []string:
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	case string:
		if s := strings.TrimSpace(t); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractWikidataLocationEntityQID(props map[string]interface{}) string {
	if props == nil {
		return ""
	}
	for _, pid := range []string{"P159", "P276", "P131"} {
		ids := extractEntityIDs(props, pid)
		for _, id := range ids {
			qid := strings.TrimSpace(id)
			if strings.HasPrefix(qid, "Q") {
				return qid
			}
		}
	}
	return ""
}

func firstNonEmptyMonolingual(props map[string]interface{}, propID string) string {
	if props == nil {
		return ""
	}
	raw, ok := props[propID]
	if !ok || raw == nil {
		return ""
	}
	switch t := raw.(type) {
	case []interface{}:
		for _, it := range t {
			if s := monolingualToString(it); strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}

func monolingualToString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]interface{}); ok {
		if txt, ok := m["text"].(string); ok && strings.TrimSpace(txt) != "" {
			return txt
		}
		if vv, ok := m["value"].(map[string]interface{}); ok {
			if txt, ok := vv["text"].(string); ok && strings.TrimSpace(txt) != "" {
				return txt
			}
			if txt, ok := vv["value"].(string); ok && strings.TrimSpace(txt) != "" {
				return txt
			}
		}
		if txt, ok := m["value"].(string); ok && strings.TrimSpace(txt) != "" {
			return txt
		}
	}
	return ""
}

func extractWikidataIDFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	for _, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "Wikidata") {
			return strings.TrimSpace(ref.ExternalID)
		}
	}
	if f.Identifiers != nil {
		if wid := strings.TrimSpace(f.Identifiers["wikidataId"]); wid != "" {
			return wid
		}
		if wid := strings.TrimSpace(f.Identifiers["wikidata"]); wid != "" {
			return wid
		}
	}
	return ""
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
