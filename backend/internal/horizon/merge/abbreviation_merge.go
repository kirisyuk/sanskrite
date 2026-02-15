package merge

import (
	"context"
	"log"
	"sort"
	"strings"
	"unicode"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// First-letter acronym stopwords: words typically excluded from abbreviations.
var acronymStopwords = map[string]struct{}{
	"and": {}, "of": {}, "the": {}, "in": {}, "on": {}, "for": {}, "at": {}, "by": {},
	"to": {}, "with": {}, "a": {}, "an": {}, "or": {}, "as": {}, "into": {}, "from": {},
}

// AcronymFromFullName extracts first letters of significant words (excludes stopwords).
// Example: "Abu Dhabi Agriculture And Food Safety Authority" -> "ADAFSA"
func AcronymFromFullName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	words := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	var out strings.Builder
	for _, w := range words {
		w = strings.TrimSpace(strings.ToLower(w))
		if w == "" {
			continue
		}
		if _, skip := acronymStopwords[w]; skip {
			continue
		}
		for _, r := range w {
			if unicode.IsLetter(r) {
				out.WriteRune(unicode.ToUpper(r))
				break
			}
		}
	}
	return out.String()
}

// LettersOnly returns uppercase letters only from s.
// Example: "Adafsa" -> "ADAFSA"
func LettersOnly(s string) string {
	var out strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			out.WriteRune(unicode.ToUpper(r))
		}
	}
	return out.String()
}

// IsAbbreviationOf returns true if shortName is a first-letter abbreviation of fullName.
// Both shortName and fullName are labels (e.g. "Adafsa", "Abu Dhabi Agriculture And Food Safety Authority").
func IsAbbreviationOf(shortName, fullName string) bool {
	acronym := AcronymFromFullName(fullName)
	normalized := LettersOnly(shortName)
	if acronym == "" || normalized == "" {
		return false
	}
	return acronym == normalized
}

// IsLegalNameSuffixContainment returns true if shortLabel is a suffix of fullLabel (case-insensitive).
// Used to catch "Executive Council" -> "Abu Dhabi Executive Council" style duplicates.
// Requires shortLabel to be at least 10 chars or 2 words to avoid overly generic matches ("Council", "Authority").
func IsLegalNameSuffixContainment(shortLabel, fullLabel string) bool {
	short := strings.TrimSpace(shortLabel)
	full := strings.TrimSpace(fullLabel)
	if short == "" || full == "" || len(short) >= len(full) {
		return false
	}
	shortWords := strings.Fields(short)
	if len(shortWords) < 2 && len(short) < 10 {
		return false
	}
	shortLower := strings.ToLower(short)
	fullLower := strings.ToLower(full)
	if !strings.HasSuffix(fullLower, shortLower) {
		return false
	}
	// Require word boundary: fullLabel should end with shortLabel as a whole.
	// "Executive Council" suffix of "Abu Dhabi Executive Council" is valid.
	prefix := strings.TrimSuffix(fullLower, shortLower)
	if prefix == "" {
		return true
	}
	last := rune(prefix[len(prefix)-1])
	return !unicode.IsLetter(last) && !unicode.IsNumber(last) // space, hyphen, etc.
}

// GeoPrefix returns the parent path (geo segment) for grouping.
// /gov/ae/adafsa -> /gov/ae/, /org/ford -> /org/
func GeoPrefix(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return ""
	}
	i := strings.LastIndex(cp, "/")
	if i <= 0 {
		return cp
	}
	return cp[:i+1]
}

// FragmentLabel returns the primary display name for a fragment.
func FragmentLabel(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if v := strings.TrimSpace(f.Identifiers["legalName"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(f.Labels["en"]); v != "" {
		return v
	}
	// Fallback: last segment of canonical path, humanized
	parts := strings.Split(strings.Trim(strings.TrimSpace(f.CanonicalPath), "/"), "/")
	if len(parts) > 0 {
		return strings.ReplaceAll(parts[len(parts)-1], "-", " ")
	}
	return ""
}

// LabelFromPath returns the humanized label from the last segment of a canonical path.
func LabelFromPath(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.ReplaceAll(parts[len(parts)-1], "-", " ")
}

// FragmentsRepoForAbbrevResolve is a minimal interface for abbreviation resolution.
type FragmentsRepoForAbbrevResolve interface {
	ListByCanonicalPathPrefix(ctx context.Context, prefix string, limit int) ([]*horizonmodels.HorizonFragment, error)
}

// ResolveAbbreviationToFull checks if the given path is an abbreviation of an existing fragment in the same geo.
// If so, returns the full fragment's canonical path and the fragment. Otherwise returns "", nil, false.
// Used by CanonicalPathResolver to avoid creating duplicate abbreviation fragments.
func ResolveAbbreviationToFull(ctx context.Context, path string, repo FragmentsRepoForAbbrevResolve) (resolvedPath string, existing *horizonmodels.HorizonFragment, ok bool) {
	path = strings.TrimSpace(path)
	if path == "" || repo == nil {
		return "", nil, false
	}
	if !strings.HasPrefix(path, "/gov/") && !strings.HasPrefix(path, "/org/") {
		return "", nil, false
	}
	geo := GeoPrefix(path)
	if geo == "" || geo == path {
		return "", nil, false
	}
	ourLabel := LabelFromPath(path)
	if ourLabel == "" {
		return "", nil, false
	}
	fragments, err := repo.ListByCanonicalPathPrefix(ctx, geo, 300)
	if err != nil || len(fragments) == 0 {
		return "", nil, false
	}
	var best *horizonmodels.HorizonFragment
	for _, f := range fragments {
		if f == nil || strings.TrimSpace(f.CanonicalPath) == "" {
			continue
		}
		if f.CanonicalPath == path {
			continue
		}
		fullLabel := FragmentLabel(f)
		if fullLabel == "" {
			continue
		}
		abbrevMatch := IsAbbreviationOf(ourLabel, fullLabel)
		suffixMatch := IsLegalNameSuffixContainment(ourLabel, fullLabel)
		if !abbrevMatch && !suffixMatch {
			continue
		}
		if len(fullLabel) <= len(ourLabel) {
			continue
		}
		if best == nil || len(f.CanonicalPath) > len(best.CanonicalPath) {
			best = f
		}
	}
	if best == nil {
		return "", nil, false
	}
	return strings.TrimSpace(best.CanonicalPath), best, true
}

// AbbreviationMergePair is a candidate pair: abbreviation fragment -> full-name fragment.
type AbbreviationMergePair struct {
	Abbrev *horizonmodels.HorizonFragment
	Full   *horizonmodels.HorizonFragment
}

// FindAbbreviationPairs finds pairs (abbrev, full) within the same geo segment.
func FindAbbreviationPairs(fragments []*horizonmodels.HorizonFragment) []AbbreviationMergePair {
	byGeo := make(map[string][]*horizonmodels.HorizonFragment)
	for _, f := range fragments {
		if f == nil || strings.TrimSpace(f.CanonicalPath) == "" {
			continue
		}
		geo := GeoPrefix(f.CanonicalPath)
		byGeo[geo] = append(byGeo[geo], f)
	}

	seen := make(map[string]bool)
	var pairs []AbbreviationMergePair
	for _, group := range byGeo {
		if len(group) < 2 {
			continue
		}
		for i := 0; i < len(group); i++ {
			for j := 0; j < len(group); j++ {
				if i == j {
					continue
				}
				a, b := group[i], group[j]
				labelA := FragmentLabel(a)
				labelB := FragmentLabel(b)
				if labelA == "" || labelB == "" {
					continue
				}
				// Abbreviation: shorter label, letters-only matches acronym of longer
				lenA, lenB := len(labelA), len(labelB)
				var abbrev, full *horizonmodels.HorizonFragment
				if lenA < lenB && IsAbbreviationOf(labelA, labelB) {
					abbrev, full = a, b
				} else if lenB < lenA && IsAbbreviationOf(labelB, labelA) {
					abbrev, full = b, a
				}
				if abbrev == nil || full == nil {
					continue
				}
				key := abbrev.ID + "->" + full.ID
				if seen[key] {
					continue
				}
				seen[key] = true
				pairs = append(pairs, AbbreviationMergePair{Abbrev: abbrev, Full: full})
			}
		}
	}
	return pairs
}

// MergeFull merges all data from both fragments into dst (the full-name fragment).
// Fill-only: only set fields in dst when they are empty.
func MergeFull(dst, src *horizonmodels.HorizonFragment) bool {
	changed := false

	if dst.Labels == nil {
		dst.Labels = make(map[string]string)
		changed = true
	}
	for k, v := range src.Labels {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Labels[k]) == "" {
			dst.Labels[k] = v
			changed = true
		}
	}

	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
		changed = true
	}
	for k, v := range src.Identifiers {
		if k == "legacy_org_canonical_path" || k == "legacy_gov_canonical_path" {
			continue
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Identifiers[k]) == "" {
			dst.Identifiers[k] = v
			changed = true
		}
	}

	if strings.TrimSpace(dst.Description) == "" && strings.TrimSpace(src.Description) != "" {
		dst.Description = src.Description
		changed = true
	}

	if strings.TrimSpace(dst.OrgSize) == "" && strings.TrimSpace(src.OrgSize) != "" {
		dst.OrgSize = src.OrgSize
		changed = true
	}

	if len(src.MarketSegments) > 0 {
		if dst.MarketSegments == nil {
			dst.MarketSegments = make([]string, 0)
		}
		seen := make(map[string]bool)
		for _, s := range dst.MarketSegments {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = true
			}
		}
		for _, s := range src.MarketSegments {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			dst.MarketSegments = append(dst.MarketSegments, s)
			changed = true
		}
	}

	tagSet := make(map[string]struct{})
	for _, t := range dst.Tags {
		tagSet[t] = struct{}{}
	}
	for _, t := range src.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := tagSet[t]; !ok {
			dst.Tags = append(dst.Tags, t)
			tagSet[t] = struct{}{}
			changed = true
		}
	}

	if dst.Identifiers["legacy_gov_canonical_path"] == "" && strings.TrimSpace(src.CanonicalPath) != "" {
		if dst.Identifiers == nil {
			dst.Identifiers = make(map[string]string)
		}
		dst.Identifiers["legacy_gov_canonical_path"] = src.CanonicalPath
		changed = true
	}

	return changed
}

// RunConfig holds configuration for RunAbbreviationMerge.
type RunConfig struct {
	ProjectID       string
	TenantID        string
	Apply           bool
	Limit           int
	Prefixes        []string // e.g. ["/gov/", "/org/"] - which path prefixes to process
	FixStaleFrames  bool     // run migrateFrames for legacy redirects (from failed runs)
}

// LegacyFramesRedirects: abbrev paths -> full paths from 2026-02-15 run (frames failed due to missing index).
// Frames with targetCanonicalPath in fromPath need to be updated to toPath.
var LegacyFramesRedirects = []struct{ From, To string }{
	{"/org/tw/taiwan-semiconductor-manufacturing-company-limited-ltd", "/org/tw/tsmc-ltd"},
	{"/org/tw/tsmc-corp", "/org/tw/tsmc-ltd"},
	{"/org/tw/tsmc", "/org/tw/tsmc-ltd"},
	{"/org/ae/na-llc", "/org/ae/najwa-aaraj-llc"},
	{"/org/ae/e11-llc", "/org/ae/emirates-llc"},
	{"/gov/sg/mas", "/gov/sg/monetary-authority-of-singapore"},
	{"/org/cn/smic-llc", "/org/cn/semiconductor-manufacturing-international-corp"},
	{"/gov/us-ca/cppa", "/gov/us-ca/california-privacy-protection-agency"},
	{"/org/sa/kaust-sa", "/org/sa/king-abdullah-university-of-science-and-technology-sa"},
	{"/org/sa/pif-sa", "/org/sa/public-investment-fund-sa"},
	{"/gov/sa/misa", "/gov/sa/ministry-of-investment"},
	{"/gov/ca/cppa", "/gov/ca/california-privacy-protection-agency"},
	{"/org/kz/nazarbayev-university-institute-of-smart-systems-and-artificial-intelligence-sa", "/org/kz/institute-of-smart-systems-and-artificial-intelligence-llc"},
	{"/org/kz/nazarbayev-university-issai-llc", "/org/kz/institute-of-smart-systems-and-artificial-intelligence-llc"},
	{"/org/int/ebrd", "/org/int/european-bank-for-reconstruction-and-development"},
	{"/org/be/society-for-worldwide-interbank-financial-telecommunication-scrl-sca", "/org/be/society-for-worldwide-interbank-financial-telecommunication-as"},
	{"/org/be/swift-plc", "/org/be/society-for-worldwide-interbank-financial-telecommunication-as"},
	{"/org/us/google-ventures-llc", "/org/us/gft-ventures-inc"},
	{"/org/us/c3-ai-inc", "/org/us/cerebra-ai-inc"},
	{"/gov/om/cma", "/gov/om/capital-market-authority"},
	{"/gov/om/cbo", "/gov/om/central-bank-of-oman"},
	{"/org/na/fsb-llc", "/org/na/financial-stability-board"},
	{"/gov/ae/adafsa", "/gov/ae/abu-dhabi-agriculture-and-food-safety-authority"},
	{"/gov/ae/adio", "/gov/ae/abu-dhabi-investment-office"},
}

// RunResult holds the outcome of RunAbbreviationMerge.
type RunResult struct {
	Processed       int
	Merged          int
	UpdatedFull     int
	DeletedAbbrev   int
	UpdatedEdges    int
	DeletedDupEdges int
	UpdatedFrames   int
}

// RunAbbreviationMerge finds abbreviation->full pairs and merges them.
func RunAbbreviationMerge(ctx context.Context, db *firestore.Client, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, edgesRepo *horizonrepo.DependencyEdgesRepository, framesRepo *horizonrepo.FramesRepository, cfg RunConfig) (RunResult, error) {
	var result RunResult

	fragments, err := listFragmentsByPrefixes(ctx, db, cfg.TenantID, cfg.Prefixes, cfg.Limit)
	if err != nil {
		return result, err
	}
	log.Printf("[AbbreviationMerge] Found %d fragments to consider", len(fragments))

	pairs := FindAbbreviationPairs(fragments)
	log.Printf("[AbbreviationMerge] Found %d abbreviation->full pairs", len(pairs))

	merged := make(map[string]bool)
	for _, p := range pairs {
		if merged[p.Abbrev.ID] || merged[p.Full.ID] {
			continue
		}
		abbrevPath := strings.TrimSpace(p.Abbrev.CanonicalPath)
		fullPath := strings.TrimSpace(p.Full.CanonicalPath)
		log.Printf("[AbbreviationMerge] Pair: %s (abbrev) -> %s (full)", abbrevPath, fullPath)

		changed := MergeFull(p.Full, p.Abbrev)
		if cfg.Apply && changed {
			if err := fragmentsRepo.Update(ctx, p.Full); err != nil {
				log.Printf("[AbbreviationMerge] ERROR: failed to update %s: %v", fullPath, err)
				continue
			}
			result.UpdatedFull++
		}

		if cfg.Apply {
			u, dd, err := migrateEdges(ctx, db, edgesRepo, p.Abbrev.ID, p.Full.ID)
			if err != nil {
				log.Printf("[AbbreviationMerge] ERROR: migrate edges failed: %v", err)
				continue
			}
			result.UpdatedEdges += u
			result.DeletedDupEdges += dd

			n, err := migrateFrames(ctx, db, framesRepo, abbrevPath, fullPath, cfg.TenantID)
			if err != nil {
				log.Printf("[AbbreviationMerge] ERROR: migrate frames failed: %v", err)
			} else {
				result.UpdatedFrames += n
			}

			if err := fragmentsRepo.Delete(ctx, p.Abbrev.ID); err != nil {
				log.Printf("[AbbreviationMerge] ERROR: delete %s failed: %v", abbrevPath, err)
				continue
			}
			result.DeletedAbbrev++
		}

		merged[p.Abbrev.ID] = true
		result.Merged++
		result.Processed++
	}

	if cfg.Apply && cfg.FixStaleFrames && len(LegacyFramesRedirects) > 0 {
		log.Printf("[AbbreviationMerge] Fixing stale frames for %d legacy redirects", len(LegacyFramesRedirects))
		for _, r := range LegacyFramesRedirects {
			from := strings.TrimSpace(r.From)
			to := strings.TrimSpace(r.To)
			if from == "" || to == "" || from == to {
				continue
			}
			n, err := migrateFrames(ctx, db, framesRepo, from, to, cfg.TenantID)
			if err != nil {
				log.Printf("[AbbreviationMerge] WARN: migrate frames %s -> %s: %v", from, to, err)
				continue
			}
			if n > 0 {
				log.Printf("[AbbreviationMerge] Fixed %d frames: %s -> %s", n, from, to)
				result.UpdatedFrames += n
			}
		}
	}

	return result, nil
}

func listFragmentsByPrefixes(ctx context.Context, db *firestore.Client, tenantID string, prefixes []string, limit int) ([]*horizonmodels.HorizonFragment, error) {
	seen := make(map[string]bool)
	var out []*horizonmodels.HorizonFragment

	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		end := prefix + "\uf8ff"
		q := db.Collection("horizon_fragments").
			Where("tenantId", "==", tenantID).
			Where("canonicalPath", ">=", prefix).
			Where("canonicalPath", "<=", end)
		iter := q.Documents(ctx)
		for {
			if limit > 0 && len(out) >= limit {
				iter.Stop()
				break
			}
			doc, err := iter.Next()
			if err != nil {
				if err == iterator.Done {
					break
				}
				return nil, err
			}
			var f horizonmodels.HorizonFragment
			if err := doc.DataTo(&f); err != nil {
				continue
			}
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			out = append(out, &f)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		ci, cj := len(out[i].CanonicalPath), len(out[j].CanonicalPath)
		if ci == cj {
			return out[i].CanonicalPath < out[j].CanonicalPath
		}
		return ci > cj // longer paths first (fuller names tend to have longer paths)
	})
	return out, nil
}

func migrateEdges(ctx context.Context, db *firestore.Client, edgesRepo *horizonrepo.DependencyEdgesRepository, oldID, newID string) (updated int, deletedDup int, err error) {
	edgesCol := db.Collection("horizon_dependency_edges")
	u1, d1, err := migrateEdgesByQuery(ctx, edgesCol.Where("tenantId", "==", "global").Where("sourceFragmentId", "==", oldID), edgesRepo, func(e *horizonmodels.DependencyEdge) {
		e.SourceFragmentID = newID
	})
	if err != nil {
		return 0, 0, err
	}
	u2, d2, err := migrateEdgesByQuery(ctx, edgesCol.Where("tenantId", "==", "global").Where("targetFragmentId", "==", oldID), edgesRepo, func(e *horizonmodels.DependencyEdge) {
		e.TargetFragmentID = newID
	})
	if err != nil {
		return 0, 0, err
	}
	return u1 + u2, d1 + d2, nil
}

func migrateEdgesByQuery(ctx context.Context, q firestore.Query, edgesRepo *horizonrepo.DependencyEdgesRepository, mutate func(*horizonmodels.DependencyEdge)) (updated int, deletedDup int, err error) {
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range docs {
		var edge horizonmodels.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		origID := edge.ID
		mutate(&edge)
		if edge.SourceFragmentID == edge.TargetFragmentID {
			if err := edgesRepo.Delete(ctx, origID); err != nil {
				return updated, deletedDup, err
			}
			deletedDup++
			continue
		}
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, edge.TenantID, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != origID {
			if err := edgesRepo.Delete(ctx, origID); err != nil {
				return updated, deletedDup, err
			}
			deletedDup++
			continue
		}
		if err := edgesRepo.Update(ctx, &edge); err != nil {
			return updated, deletedDup, err
		}
		updated++
	}
	return updated, deletedDup, nil
}

func migrateFrames(ctx context.Context, db *firestore.Client, framesRepo *horizonrepo.FramesRepository, fromCanonicalPath, toCanonicalPath, tenantID string) (int, error) {
	frames, err := framesRepo.ListByTargetCanonicalPath(ctx, tenantID, fromCanonicalPath, 1000)
	if err != nil {
		return 0, err
	}
	if len(frames) == 0 {
		return 0, nil
	}
	col := db.Collection("horizon_frames")
	updated := 0
	for _, f := range frames {
		if f == nil || f.ID == "" {
			continue
		}
		_, err := col.Doc(f.ID).Update(ctx, []firestore.Update{
			{Path: "targetCanonicalPath", Value: toCanonicalPath},
		})
		if err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
