package enrich

import (
	"context"
	"fmt"
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

	// Only meaningful for ORG/PERSON in v1.
	if fragment.Type != hmodels.FragmentTypeOrganization && fragment.Type != hmodels.FragmentTypePerson {
		return false, nil
	}

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
	return EnrichFragmentFromEntityData(fragment, ed, time.Now().UTC(), opts)
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

	// Type-gating (avoid applying human data to orgs and vice versa).
	p31 := extractEntityIDs(ed.PropertiesRaw, "P31")
	isHuman := false
	for _, x := range p31 {
		if x == "Q5" {
			isHuman = true
			break
		}
	}
	if fragment.Type == hmodels.FragmentTypePerson && !isHuman {
		return false, fmt.Errorf("qid %s is not a human (P31=%v)", qid, p31)
	}
	if fragment.Type == hmodels.FragmentTypeOrganization && isHuman {
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
	if fragment.Type == hmodels.FragmentTypeOrganization {
		inds := extractEntityIDs(ed.PropertiesRaw, "P452")
		if len(inds) > 0 {
			paths := taxonomy.MapIndustryQIDsToPaths(inds)
			if len(paths) > 0 {
				// Overwrite is fine; these are derived.
				if !stringSlicesEqual(fragment.IndustryPaths, paths) {
					fragment.IndustryPaths = paths
					changed = true
				}
			}
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


