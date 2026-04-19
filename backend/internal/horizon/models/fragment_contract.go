package models

import (
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// FragmentPatch represents partial output from Research/Critic agents.
// It contains only the fields that agents may produce; required persist invariants
// (Governance, TenantID, Metadata, Provenance) are NOT included and must be
// applied by ContractNormalizer before persistence.
type FragmentPatch struct {
	CanonicalPath string `json:"canonicalPath,omitempty"`
	// Class replaces the legacy `Type FragmentType` field. Agents emit
	// the new lowercase AH class (org/site/product/decision/...). For
	// transitional reads, ApplyPatchToFragment also accepts a non-empty
	// Subtype to refine the class.
	Class             ah.FragmentClass       `json:"class,omitempty"`
	Subtype           string                 `json:"subtype,omitempty"`
	Labels            map[string]string      `json:"labels,omitempty"`
	State             map[string]MetricValue `json:"state,omitempty"`
	Identifiers       map[string]string      `json:"identifiers,omitempty"`
	Description       string                 `json:"description,omitempty"`
	EntityPath        string                 `json:"entityPath,omitempty"`
	HomeLocation      *Location              `json:"homeLocation,omitempty"`
	MarketSegments    []string               `json:"marketSegments,omitempty"`
	OrgSize           string                 `json:"orgSize,omitempty"`
	RadiusKm          float64                `json:"radiusKm,omitempty"`
	Tags              []string               `json:"tags,omitempty"`
	KBRefs            []KBReference          `json:"kbRefs,omitempty"`
	NameHistory       []NameHistoryEntry     `json:"nameHistory,omitempty"`
	IndustryPaths     []string               `json:"industryPaths,omitempty"`
	SpherePaths       []string               `json:"spherePaths,omitempty"`
	ValidFrom         *time.Time             `json:"validFrom,omitempty"`
	ValidTo           *time.Time             `json:"validTo,omitempty"`
	LifecycleStage    string                 `json:"lifecycleStage,omitempty"`
	ExpectedStartDate *time.Time             `json:"expectedStartDate,omitempty"`
	ActualStartDate   *time.Time             `json:"actualStartDate,omitempty"`
	ExpectedEndDate   *time.Time             `json:"expectedEndDate,omitempty"`
	ActualEndDate     *time.Time             `json:"actualEndDate,omitempty"`
}

// ToPatch extracts a FragmentPatch from a HorizonFragment (for backward compatibility).
// Only non-empty fields that agents may have produced are copied.
func (f *HorizonFragment) ToPatch() *FragmentPatch {
	if f == nil {
		return nil
	}
	p := &FragmentPatch{}
	if v := strings.TrimSpace(f.CanonicalPath); v != "" {
		p.CanonicalPath = v
	}
	if f.Class != "" {
		p.Class = f.Class
	}
	if v := strings.TrimSpace(f.Subtype); v != "" {
		p.Subtype = v
	}
	if len(f.Labels) > 0 {
		p.Labels = f.Labels
	}
	if len(f.State) > 0 {
		p.State = f.State
	}
	if len(f.Identifiers) > 0 {
		p.Identifiers = f.Identifiers
	}
	if v := strings.TrimSpace(f.Description); v != "" {
		p.Description = v
	}
	if v := strings.TrimSpace(f.EntityPath); v != "" {
		p.EntityPath = v
	}
	if f.HomeLocation != nil {
		p.HomeLocation = f.HomeLocation
	}
	if len(f.MarketSegments) > 0 {
		p.MarketSegments = f.MarketSegments
	}
	if v := strings.TrimSpace(f.OrgSize); v != "" {
		p.OrgSize = v
	}
	if f.RadiusKm > 0 {
		p.RadiusKm = f.RadiusKm
	}
	if len(f.Tags) > 0 {
		p.Tags = f.Tags
	}
	if len(f.KBRefs) > 0 {
		p.KBRefs = f.KBRefs
	}
	if len(f.NameHistory) > 0 {
		p.NameHistory = f.NameHistory
	}
	if len(f.IndustryPaths) > 0 {
		p.IndustryPaths = f.IndustryPaths
	}
	if len(f.SpherePaths) > 0 {
		p.SpherePaths = f.SpherePaths
	}
	if f.ValidFrom != nil {
		p.ValidFrom = f.ValidFrom
	}
	if f.ValidTo != nil {
		p.ValidTo = f.ValidTo
	}
	if v := strings.TrimSpace(f.LifecycleStage); v != "" {
		p.LifecycleStage = v
	}
	if f.ExpectedStartDate != nil {
		p.ExpectedStartDate = f.ExpectedStartDate
	}
	if f.ActualStartDate != nil {
		p.ActualStartDate = f.ActualStartDate
	}
	if f.ExpectedEndDate != nil {
		p.ExpectedEndDate = f.ExpectedEndDate
	}
	if f.ActualEndDate != nil {
		p.ActualEndDate = f.ActualEndDate
	}
	return p
}

// ApplyPatch merges patch into base using fill-first semantics (patch fills empty base fields).
// The result is written into base; base is modified in place.
// Caller must still run ContractNormalizer.Normalize for full validation.
func ApplyPatchToFragment(base *HorizonFragment, patch *FragmentPatch, effectivePath string) {
	if base == nil || patch == nil {
		return
	}
	if base.State == nil {
		base.State = make(map[string]MetricValue)
	}
	if base.Identifiers == nil {
		base.Identifiers = make(map[string]string)
	}
	if base.Labels == nil {
		base.Labels = make(map[string]string)
	}

	// CanonicalPath: use effectivePath if base/patch empty, else patch if more complete
	cp := strings.TrimSpace(patch.CanonicalPath)
	if cp != "" && (strings.TrimSpace(base.CanonicalPath) == "" || isMoreCanonical(strings.TrimSpace(base.CanonicalPath), cp)) {
		base.CanonicalPath = cp
		if parts, err := taxonomy.ParseCanonicalPathParts(cp); err == nil && parts != nil {
			base.CanonicalParts = parts
		}
	}
	if effectivePath != "" && strings.TrimSpace(base.CanonicalPath) == "" {
		base.CanonicalPath = strings.TrimSpace(effectivePath)
	}

	if patch.Class != "" {
		base.Class = patch.Class
	}
	if v := strings.TrimSpace(patch.Subtype); v != "" {
		base.Subtype = v
	}
	if v := strings.TrimSpace(patch.EntityPath); v != "" {
		base.EntityPath = v
	}
	if v := strings.TrimSpace(patch.Description); v != "" {
		base.Description = v
	}
	if patch.HomeLocation != nil {
		if base.HomeLocation == nil {
			base.HomeLocation = &Location{}
		}
		mergeLocation(base.HomeLocation, patch.HomeLocation)
	}
	if patch.Labels != nil {
		for k, v := range patch.Labels {
			if strings.TrimSpace(v) != "" && strings.TrimSpace(base.Labels[k]) == "" {
				base.Labels[k] = v
			}
		}
	}
	if patch.State != nil {
		for k, v := range patch.State {
			base.State[k] = v
		}
	}
	if len(patch.MarketSegments) > 0 {
		seen := make(map[string]struct{})
		for _, s := range base.MarketSegments {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = struct{}{}
			}
		}
		for _, s := range patch.MarketSegments {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			base.MarketSegments = append(base.MarketSegments, s)
		}
	}
	if v := strings.TrimSpace(patch.OrgSize); v != "" {
		cur := strings.ToLower(strings.TrimSpace(base.OrgSize))
		if cur == "" || cur == "na" {
			base.OrgSize = v
		}
	}
	if patch.RadiusKm > 0 && base.RadiusKm <= 0 {
		base.RadiusKm = patch.RadiusKm
	}
	if patch.Identifiers != nil {
		for k, v := range patch.Identifiers {
			if strings.TrimSpace(v) != "" && strings.TrimSpace(base.Identifiers[k]) == "" {
				base.Identifiers[k] = v
			}
		}
	}
	if len(patch.Tags) > 0 {
		seen := make(map[string]struct{})
		for _, t := range base.Tags {
			t = strings.TrimSpace(t)
			if t != "" {
				seen[t] = struct{}{}
			}
		}
		for _, t := range patch.Tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			base.Tags = append(base.Tags, t)
		}
	}
	if len(patch.KBRefs) > 0 {
		base.KBRefs = append(base.KBRefs, patch.KBRefs...)
	}
	if len(patch.NameHistory) > 0 {
		seen := make(map[string]struct{})
		for _, e := range base.NameHistory {
			key := strings.ToLower(strings.TrimSpace(e.Kind)) + "|" + strings.ToLower(strings.TrimSpace(e.Name))
			seen[key] = struct{}{}
		}
		for _, e := range patch.NameHistory {
			name := strings.TrimSpace(e.Name)
			if name == "" {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(e.Kind))
			if kind == "" {
				kind = "short"
			}
			key := kind + "|" + strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			base.NameHistory = append(base.NameHistory, e)
		}
	}
	if len(patch.SpherePaths) > 0 {
		base.SpherePaths = append(base.SpherePaths, patch.SpherePaths...)
	}
	if patch.ValidFrom != nil {
		base.ValidFrom = patch.ValidFrom
	}
	if patch.ValidTo != nil {
		base.ValidTo = patch.ValidTo
	}
	if v := strings.TrimSpace(patch.LifecycleStage); v != "" {
		base.LifecycleStage = v
	}
	if patch.ExpectedStartDate != nil {
		base.ExpectedStartDate = patch.ExpectedStartDate
	}
	if patch.ActualStartDate != nil {
		base.ActualStartDate = patch.ActualStartDate
	}
	if patch.ExpectedEndDate != nil {
		base.ExpectedEndDate = patch.ExpectedEndDate
	}
	if patch.ActualEndDate != nil {
		base.ActualEndDate = patch.ActualEndDate
	}
}

func mergeLocation(dst, src *Location) {
	if dst == nil || src == nil {
		return
	}
	if strings.TrimSpace(dst.Name) == "" && strings.TrimSpace(src.Name) != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if (dst.ISO3166_1 == nil || strings.TrimSpace(*dst.ISO3166_1) == "") &&
		src.ISO3166_1 != nil && strings.TrimSpace(*src.ISO3166_1) != "" {
		v := strings.TrimSpace(*src.ISO3166_1)
		dst.ISO3166_1 = &v
	}
	if (dst.ISO3166_2 == nil || strings.TrimSpace(*dst.ISO3166_2) == "") &&
		src.ISO3166_2 != nil && strings.TrimSpace(*src.ISO3166_2) != "" {
		v := strings.TrimSpace(*src.ISO3166_2)
		dst.ISO3166_2 = &v
	}
	if (dst.ISO6709 == nil || strings.TrimSpace(*dst.ISO6709) == "") &&
		src.ISO6709 != nil && strings.TrimSpace(*src.ISO6709) != "" {
		v := strings.TrimSpace(*src.ISO6709)
		dst.ISO6709 = &v
	}
}

// isMoreCanonical returns true if candidate has more path segments or looks more complete.
func isMoreCanonical(current, candidate string) bool {
	cur := strings.Trim(strings.TrimSpace(current), "/")
	cand := strings.Trim(strings.TrimSpace(candidate), "/")
	if cur == "" {
		return cand != ""
	}
	if cand == "" {
		return false
	}
	curParts := strings.Split(cur, "/")
	candParts := strings.Split(cand, "/")
	return len(candParts) >= len(curParts)
}
