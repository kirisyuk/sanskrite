package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
	"sync"
)

// canonCache provides a cascade-scoped LRU-style cache for resolved canonical paths.
// Keyed by NormalizeEntityName(name) + "|" + fragmentType.
// Created once per job execution alongside SessionGraph.
type canonCache struct {
	mu    sync.RWMutex
	store map[string]string
}

func newCanonCache() *canonCache {
	return &canonCache{store: make(map[string]string)}
}

func (c *canonCache) get(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}

func (c *canonCache) set(key, canonicalPath string) {
	if c == nil || key == "" || canonicalPath == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = canonicalPath
}

func canonCacheKey(name string, fragType FragmentType) string {
	// MIGRATION NOTE (AH taxonomy / docs/archive/taxonomy-migration-2026/migration_map.yaml):
	// Callers may pass legacy wire tokens ("ORGANIZATION") or the AH
	// canonical class ("org") for fragType. Normalize both sides through
	// normalizeFragmentTypeHardcoded so a `set` from one form is found
	// by a `get` from the other; otherwise the canon cache misses every
	// time the cascade switches between wire and canonical naming.
	normalizedType := string(fragType)
	if cls := normalizeFragmentTypeHardcoded(string(fragType)); cls != "" {
		normalizedType = string(cls)
	}
	return naming.NormalizeEntityName(name) + "|" + normalizedType
}

type canonCacheCtxKey struct{}

func withCanonCache(ctx context.Context, cc *canonCache) context.Context {
	return context.WithValue(ctx, canonCacheCtxKey{}, cc)
}

// slugToLabel converts the last segment of a canonical path into a human-readable label.
// Example: "/org/tr/botas" -> "Botas", "/fac/na/lng-terminal" -> "Lng Terminal".
func slugToLabel(cp string) string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 0 {
		return cp
	}
	slug := parts[len(parts)-1]
	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func canonCacheFromContext(ctx context.Context) *canonCache {
	if cc, ok := ctx.Value(canonCacheCtxKey{}).(*canonCache); ok {
		return cc
	}
	return nil
}

// isCanonicalPathValid checks whether a canonical path is structurally valid and
// consistent with the expected fragment type. Used to short-circuit Vertex/LLM
// canonicalization when the LLM-produced path already satisfies all structural rules.
func isCanonicalPathValid(path string, relType FragmentType) bool {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, "/discovered/") {
		return false
	}
	if err := taxonomy.ValidateCanonicalPath(path); err != nil {
		return false
	}
	if relType == "" {
		return true
	}
	inferred := InferFragmentTypeFromCanonicalPath(path)
	if inferred == "" {
		return true
	}
	return inferred == relType
}

func slugify(s string) string {
	// Single source of truth: taxonomy.Slugify includes best-effort Cyrillic transliteration
	// and shared Horizon/Ontology slug rules.
	return taxonomy.Slugify(s)
}

// Slugify is an exported wrapper for Horizon canonical path slugging.
// It is used by non-scout workflows to ensure consistent canonicalization rules.
func Slugify(s string) string { return slugify(s) }

// PreferredCanonicalPaths is an exported wrapper returning ordered canonical path candidates.
// It is used by non-scout workflows to ensure consistent canonicalization rules.
func PreferredCanonicalPaths(rel agents.DiscoveredRelation) []string {
	return preferredCanonicalPaths(rel)
}

// preferredCanonicalPaths returns ordered canonical path candidates derived from names only.
// For facilities we intentionally avoid a blind /fac/na fallback because name-only facility
// paths create long-lived graph debt when geography is resolved later.
//
// Repo/Vertex checks decide which one is stable.
func preferredCanonicalPaths(rel agents.DiscoveredRelation) []string {
	// MIGRATION NOTE (AH taxonomy): legacy FragmentType sentinels for
	// program/media_channel/deposit/affected_zone collapsed onto parent
	// AH classes. Recover the subtype hint from the raw type token so the
	// /program/, /media/, /deposit/, /zone/ namespace candidates remain
	// distinguishable from a bare /org or /fac path.
	t, subtype, _ := models.NormalizeFragmentClassName(rel.TargetType, "")
	short := strings.TrimSpace(rel.TargetShortName)
	if short == "" {
		if strings.TrimSpace(rel.TargetLegalName) != "" {
			short = rel.TargetLegalName
		} else {
			short = rel.TargetName
		}
	}
	short = strings.TrimSpace(short)
	if short == "" {
		return nil
	}

	slug := slugify(short)
	if slug == "" {
		return nil
	}

	switch {
	case t == ah.FragmentClassOrg && subtype == "program":
		return []string{"/program/global/" + slug}
	case t == ah.FragmentClassOrg && subtype == "media_channel":
		return []string{"/media/" + slug}
	case t == ah.FragmentClassOrg:
		return []string{"/org/na/" + slug}
	case t == ah.FragmentClassSite && subtype == "deposit":
		return []string{"/deposit/other/" + slug}
	case t == ah.FragmentClassSite:
		return nil
	case t == ah.FragmentClassProduct:
		ls := strings.TrimSpace(strings.ToLower(short))
		if ls == "na" || ls == "n/a" || ls == "unknown" || ls == "none" {
			return nil
		}
		canonicalizer := NewProductCanonicalizer()
		result, err := canonicalizer.CanonicalizeProduct(context.Background(), short)
		if err == nil && result != nil && result.CanonicalPath != "" {
			return []string{result.CanonicalPath}
		}
		if slug == "na" {
			return nil
		}
		return []string{BuildProductCanonicalPath("", "", "", slug)}
	case t == ah.FragmentClassMarket:
		return []string{"/market/global/" + slug}
	case t == ah.FragmentClassEvidence && subtype == "domain":
		return []string{"/domain/v1/other/" + slug}
	case t == ah.FragmentClassEvidence && subtype == "sphere":
		return []string{"/sphere/" + slug}
	case t == ah.FragmentClassEvidence:
		return []string{"/event/" + slug}
	case t == ah.FragmentClassGeo:
		return nil
	case t == ah.FragmentClassPerson:
		return []string{"/person/" + slug}
	case t == ah.FragmentClassCommunity:
		return []string{"/community/global/interest/" + slug}
	default:
		return []string{"/org/na/" + slug}
	}
}

// normalizeLegacyGeoPath maps older geo namespaces to /loc variable-depth model.
// Supported legacy patterns:
// - /region/na/{country}/{admin1}/{city} -> /loc/{country}/{level1}/{level2}
// - /region/{country}/{admin1}/{city}    -> /loc/{country}/{level1}/{level2}
func normalizeLegacyGeoPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "/loc/") {
		return p
	}
	if !strings.HasPrefix(p, "/region/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	extractCountry := func(idx int) string {
		if idx < 0 || idx >= len(parts) {
			return ""
		}
		c := strings.ToLower(strings.TrimSpace(parts[idx]))
		if c == "" || c == "na" {
			return ""
		}
		return c
	}
	// parts[0] == "region"
	if len(parts) == 5 && parts[1] == "na" {
		if c := extractCountry(2); c != "" {
			return taxonomy.GeoCanonicalPath(c, parts[3], parts[4])
		}
		return ""
	}
	if len(parts) == 4 {
		if c := extractCountry(1); c != "" {
			return taxonomy.GeoCanonicalPath(c, parts[2], parts[3])
		}
		return ""
	}
	if len(parts) == 3 && parts[1] == "na" {
		if c := extractCountry(2); c != "" {
			return taxonomy.GeoCanonicalPath(c, "", "")
		}
		return ""
	}
	if len(parts) == 3 {
		if c := extractCountry(1); c != "" {
			return taxonomy.GeoCanonicalPath(c, parts[2], "")
		}
		return ""
	}
	if len(parts) == 2 {
		if c := extractCountry(1); c != "" {
			return taxonomy.GeoCanonicalPath(c, "", "")
		}
		return ""
	}
	return ""
}

// normalizeFragmentType tries to map common human labels to our enum values.
// Uses CanonizationPolicy if available in ScoutEngine, otherwise falls back to hardcoded rules.
func (e *ScoutEngine) normalizeFragmentType(ctx context.Context, s string) FragmentType {
	logPrefix := "[ScoutEngine:normalizeFragmentType]"

	// Try CanonizationPolicy if available
	if e.canonizationPolicy != nil {
		decision, err := e.canonizationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "canonization",
			Context: map[string]interface{}{
				"inputType":  "fragment_type",
				"inputValue": s,
			},
		})

		if err == nil && decision.Action == "normalize_to" {
			output, _ := decision.Params["output"].(string)
			if output != "" {
				fmt.Printf("%s CanonizationPolicy normalized: %s -> %s\n", logPrefix, s, output)
				return FragmentType(output)
			}
		} else if err != nil {
			fmt.Printf("%s ERROR: CanonizationPolicy failed: %v, using fallback\n", logPrefix, err)
		}
	}

	// Fallback to hardcoded normalization
	return normalizeFragmentTypeHardcoded(s)
}

// normalizeFragmentTypeHardcoded is the hardcoded fallback for fragment type
// normalization. After the AH "Big Bang" migration removed
// edgesemantics.NormalizeFragmentTypeToken, we delegate to the canonical
// models.NormalizeFragmentClassName helper.
//
// MIGRATION NOTE (AH taxonomy / docs/archive/taxonomy-migration-2026/migration_map.yaml):
// A handful of legacy wire tokens are dispositioned as `fold_to_attributes`
// or `rewrite_edge` in the migration map and therefore have NO direct
// AH FragmentClass mapping via models.NormalizeFragmentClassName. Without
// the explicit short-circuits below they would silently default to
// ah.FragmentClassOrg, which loses the parent-class hint scout needs to
// route them onto the correct canonical-path namespace and to satisfy the
// /docs/archive/taxonomy-migration-2026/migration_map.yaml semantics that DOMAIN/SPHERE were
// "evidence-shaped overlays" and supply-link / network tokens collapsed onto
// site/community parents.
func normalizeFragmentTypeHardcoded(s string) FragmentType {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DOMAIN", "SPHERE":
		// LEGACY (read-side only): both folded to evidence overlays.
		return ah.FragmentClassEvidence
	case strings.ToUpper("supply" + "_link"), "SUPPLYLINK":
		// LEGACY (read-side only): collapsed onto site parent class
		// (subtype=supply_link recovered via models.ClassSubtypeSupplyLink).
		return ah.FragmentClassSite
	case "NETWORK":
		// LEGACY (read-side only): networks now expressed as communities.
		return ah.FragmentClassCommunity
	}
	cls, _, _ := models.NormalizeFragmentClassName(s, "")
	return cls
}

// resolveCanonicalPath tries to produce a stable canonical path for a relation.
// Priority:
// 1) Use relation.TargetCanonicalPath if it is provided and not /discovered/*
// 2) Vertex AI Search lookup by name (prefers already-known canonicalPath)
// 3) LLM canonicalization (strict JSON) as a last resort
func (e *ScoutEngine) resolveCanonicalPath(ctx context.Context, rel agents.DiscoveredRelation) (resolvedPath string, resolveErr error) {
	logPrefix := "[ScoutEngine:resolveCanonicalPath]"

	// Normalize fragment type using CanonizationPolicy if available
	relType := e.normalizeFragmentType(ctx, rel.TargetType)

	// Deferred cache write: store resolved path in canon cache for future lookups.
	defer func() {
		if resolveErr != nil || resolvedPath == "" {
			return
		}
		cc := canonCacheFromContext(ctx)
		if cc == nil {
			return
		}
		targetName := strings.TrimSpace(rel.TargetShortName)
		if targetName == "" {
			targetName = strings.TrimSpace(rel.TargetLegalName)
		}
		if targetName == "" {
			targetName = rel.TargetName
		}
		if targetName != "" {
			cc.set(canonCacheKey(targetName, relType), resolvedPath)
		}
	}()

	// Align fragment type with any provided canonical path prefix (auto-fix mismatches).
	if cp := strings.TrimSpace(rel.TargetCanonicalPath); cp != "" {
		inferred := InferFragmentTypeFromCanonicalPath(cp)
		if inferred != "" && inferred != relType {
			fmt.Printf("%s Adjusted targetType based on canonicalPath: %q -> %q (path=%s)\n",
				logPrefix, relType, inferred, cp)
			relType = inferred
		} else if relType == "" && inferred != "" {
			relType = inferred
		}
	}

	// Try CanonizationPolicy for path normalization if available
	if e.canonizationPolicy != nil && strings.TrimSpace(rel.TargetCanonicalPath) != "" {
		decision, err := e.canonizationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "canonization",
			Context: map[string]interface{}{
				"inputType":  "canonical_path",
				"inputValue": rel.TargetCanonicalPath,
			},
		})

		if err == nil && decision.Action == "normalize_to" {
			output, _ := decision.Params["output"].(string)
			if output != "" && output != rel.TargetCanonicalPath {
				fmt.Printf("%s CanonizationPolicy normalized path: %s -> %s\n", logPrefix, rel.TargetCanonicalPath, output)
				rel.TargetCanonicalPath = output
			}
		} else if err != nil {
			fmt.Printf("%s ERROR: CanonizationPolicy failed for path: %v, using original\n", logPrefix, err)
		}
	}

	// GEO special-case: normalize legacy /region/... into /loc/... when possible.
	if relType == ah.FragmentClassGeo {
		if norm := normalizeLegacyGeoPath(rel.TargetCanonicalPath); norm != "" {
			if e.fragmentsRepo != nil {
				if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, norm); err == nil && f != nil && f.ID != "" {
					return e.finalizeRelationCanonicalPath(ctx, rel, norm, relType), nil
				}
			}
			return e.finalizeRelationCanonicalPath(ctx, rel, norm, relType), nil
		}
	}

	// PRODUCT special-case: use ProductCanonicalizer for hierarchical canonicalization
	if relType == ah.FragmentClassProduct {
		productName := strings.TrimSpace(rel.TargetShortName)
		if productName == "" {
			productName = strings.TrimSpace(rel.TargetLegalName)
		}
		if productName == "" {
			productName = rel.TargetName
		}
		if productName != "" {
			canonicalizer := NewProductCanonicalizer()
			result, err := canonicalizer.CanonicalizeProduct(ctx, productName)
			if err == nil && result != nil && result.CanonicalPath != "" {
				// Check if this canonical path already exists
				if e.fragmentsRepo != nil {
					if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, result.CanonicalPath); err == nil && f != nil && f.ID != "" {
						return e.finalizeRelationCanonicalPath(ctx, rel, result.CanonicalPath, relType), nil
					}
				}
				// Use canonicalized path
				return e.finalizeRelationCanonicalPath(ctx, rel, result.CanonicalPath, relType), nil
			}
		}
	}

	// 0-pre) CANON CACHE: fastest path — exact name+type already resolved in this cascade.
	{
		targetName := strings.TrimSpace(rel.TargetShortName)
		if targetName == "" {
			targetName = strings.TrimSpace(rel.TargetLegalName)
		}
		if targetName == "" {
			targetName = rel.TargetName
		}
		if cc := canonCacheFromContext(ctx); cc != nil && targetName != "" {
			if cached, ok := cc.get(canonCacheKey(targetName, relType)); ok {
				return e.finalizeRelationCanonicalPath(ctx, rel, cached, relType), nil
			}
		}
	}

	// 0) SESSION GRAPH: Check in-memory cache first (solves Vertex indexing delay + duplicate prevention).
	// This is the fastest path and catches entities created earlier in the same job.
	if sg := SessionGraphFromContext(ctx); sg != nil {
		// Try exact path lookup first
		for _, pref := range preferredCanonicalPaths(rel) {
			if strings.TrimSpace(pref) == "" {
				continue
			}
			// Resolve any path aliases (in case path was updated within this job)
			resolved := sg.ResolvePath(pref)
			if frag := sg.GetByPath(resolved); frag != nil && frag.ID != "" {
				fmt.Printf("%s Session graph hit (path): %s\n", logPrefix, resolved)
				return e.finalizeRelationCanonicalPath(ctx, rel, frag.CanonicalPath, relType), nil
			}
		}

		// Try name lookup with jurisdiction preference
		targetName := strings.TrimSpace(rel.TargetShortName)
		if targetName == "" {
			targetName = strings.TrimSpace(rel.TargetLegalName)
		}
		if targetName == "" {
			targetName = rel.TargetName
		}
		if targetName != "" {
			// Extract source jurisdiction from SourceCanonicalPath if available
			sourceJurisdiction := extractJurisdiction(strings.TrimSpace(rel.SourceCanonicalPath))
			if frag := sg.GetByNamePreferJurisdiction(targetName, sourceJurisdiction); frag != nil && frag.ID != "" {
				fmt.Printf("%s Session graph hit (name+jurisdiction): %q -> %s (sourceJurisdiction=%s)\n",
					logPrefix, targetName, frag.CanonicalPath, sourceJurisdiction)
				return e.finalizeRelationCanonicalPath(ctx, rel, frag.CanonicalPath, relType), nil
			}
		}
	}

	// 1) Use provided path if it's already stable
	if strings.TrimSpace(rel.TargetCanonicalPath) != "" && !strings.HasPrefix(strings.TrimSpace(rel.TargetCanonicalPath), "/discovered/") {
		// Hard preference: if preferred shortName path exists, use it to avoid duplicates like /org/tesla-inc vs /org/tesla.
		if e.fragmentsRepo != nil {
			for _, pref := range preferredCanonicalPaths(rel) {
				if strings.TrimSpace(pref) == "" {
					continue
				}
				if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
					return e.finalizeRelationCanonicalPath(ctx, rel, pref, relType), nil
				}
			}
		}
		return e.finalizeRelationCanonicalPath(ctx, rel, strings.TrimSpace(rel.TargetCanonicalPath), relType), nil
	}

	// 1.5) Hard preference: if a preferred canonical fragment already exists, use it immediately.
	if e.fragmentsRepo != nil {
		for _, pref := range preferredCanonicalPaths(rel) {
			if strings.TrimSpace(pref) == "" {
				continue
			}
			if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
				return e.finalizeRelationCanonicalPath(ctx, rel, pref, relType), nil
			}
		}
	}

	// 1.8) FIRESTORE-FIRST: Try name-based lookup in Firestore before Vertex (faster, no indexing delay).
	// This is more reliable than Vertex for recently created entities.
	if e.fragmentsRepo != nil {
		targetName := strings.TrimSpace(rel.TargetShortName)
		if targetName == "" {
			targetName = strings.TrimSpace(rel.TargetLegalName)
		}
		if targetName == "" {
			targetName = rel.TargetName
		}
		if targetName != "" {
			// Extract source jurisdiction for preference
			sourceJurisdiction := extractJurisdiction(strings.TrimSpace(rel.SourceCanonicalPath))

			// Search Firestore by label (exact match, but fast and always up-to-date)
			var candidates []*models.HorizonFragment
			var err error
			if relType != "" {
				candidates, err = e.fragmentsRepo.FindByLabel(ctx, "en", targetName, relType)
				if err != nil || len(candidates) == 0 {
					candidates, err = e.fragmentsRepo.FindByLabel(ctx, "en", targetName, "")
				}
			} else {
				candidates, err = e.fragmentsRepo.FindByLabel(ctx, "en", targetName, "")
			}
			if err == nil && len(candidates) > 0 {
				// Apply jurisdiction preference: prefer same jurisdiction, then global, then any
				var exactMatch, globalMatch, anyMatch *models.HorizonFragment
				for _, cand := range candidates {
					candJurisdiction := extractJurisdiction(cand.CanonicalPath)
					if sourceJurisdiction != "" && candJurisdiction == sourceJurisdiction {
						exactMatch = cand
						break
					}
					if candJurisdiction == "" || candJurisdiction == "global" {
						if globalMatch == nil {
							globalMatch = cand
						}
					}
					if anyMatch == nil {
						anyMatch = cand
					}
				}
				var bestMatch *models.HorizonFragment
				if exactMatch != nil {
					bestMatch = exactMatch
				} else if globalMatch != nil {
					bestMatch = globalMatch
				} else {
					bestMatch = anyMatch
				}
				if bestMatch != nil && bestMatch.ID != "" {
					fmt.Printf("%s Firestore-first hit (label+jurisdiction): %q -> %s (sourceJurisdiction=%s)\n",
						logPrefix, targetName, bestMatch.CanonicalPath, sourceJurisdiction)
					return e.finalizeRelationCanonicalPath(ctx, rel, bestMatch.CanonicalPath, relType), nil
				}
			}

			// 1.9) Normalized legal name fallback: catches "Gazprom PJSC" vs "Gazprom",
			// "BOTAS A.S." vs "BOTAS", etc. by stripping legal suffixes + diacritics.
			if relType == ah.FragmentClassOrg || relType == "" {
				normalizedTarget := naming.NormalizeEntityName(targetName)
				if normalizedTarget != "" && normalizedTarget != strings.ToLower(targetName) {
					normCandidates, normErr := e.fragmentsRepo.FindByNormalizedLegalName(ctx, normalizedTarget)
					if normErr == nil && len(normCandidates) > 0 {
						var normExact, normGlobal, normAny *models.HorizonFragment
						for _, cand := range normCandidates {
							candJurisdiction := extractJurisdiction(cand.CanonicalPath)
							if sourceJurisdiction != "" && candJurisdiction == sourceJurisdiction {
								normExact = cand
								break
							}
							if candJurisdiction == "" || candJurisdiction == "global" {
								if normGlobal == nil {
									normGlobal = cand
								}
							}
							if normAny == nil {
								normAny = cand
							}
						}
						bestNorm := normExact
						if bestNorm == nil {
							bestNorm = normGlobal
						}
						if bestNorm == nil {
							bestNorm = normAny
						}
						if bestNorm != nil && bestNorm.ID != "" {
							fmt.Printf("%s Firestore-first hit (normalizedLegalName): %q -> %s (normalized=%q, sourceJurisdiction=%s)\n",
								logPrefix, targetName, bestNorm.CanonicalPath, normalizedTarget, sourceJurisdiction)
							return e.finalizeRelationCanonicalPath(ctx, rel, bestNorm.CanonicalPath, relType), nil
						}
					}
				}
			}
		}
	}

	// 2) Try Vertex AI Search over already indexed fragments (with jurisdiction preference)
	if e.planner != nil && e.planner.vertexSearchClient != nil && e.planner.vertexSearchClient.IsConfigured() {
		query := strings.TrimSpace(rel.TargetShortName)
		if query == "" {
			query = strings.TrimSpace(rel.TargetLegalName)
		}
		if query == "" {
			query = rel.TargetName
		}
		req := &ai.SearchRequest{
			Query:    query,
			PageSize: 10,
			Filter:   `structData.type: "horizon_fragment"`,
		}
		resp, err := e.planner.vertexSearchClient.Search(ctx, req)
		if err == nil && resp != nil {
			acceptVertexPath := func(candidate string) (string, bool) {
				return e.acceptVertexCanonicalPath(ctx, rel, relType, candidate)
			}
			// Extract source jurisdiction for filtering
			sourceJurisdiction := extractJurisdiction(strings.TrimSpace(rel.SourceCanonicalPath))

			// Prefer search hit that matches our shortName-derived canonicalPath
			for _, pref := range preferredCanonicalPaths(rel) {
				if strings.TrimSpace(pref) == "" {
					continue
				}
				for _, r := range resp.Results {
					if r.Document.StructData == nil {
						continue
					}
					if cp, ok := r.Document.StructData["canonicalPath"].(string); ok && strings.TrimSpace(cp) == pref {
						if resolved, accepted := acceptVertexPath(cp); accepted {
							return resolved, nil
						}
					}
				}
			}
			// For GEO_REGION: strongly prefer already-valid /loc hierarchy.
			if relType == ah.FragmentClassGeo {
				for _, r := range resp.Results {
					if r.Document.StructData == nil {
						continue
					}
					if cp, ok := r.Document.StructData["canonicalPath"].(string); ok {
						cp = strings.TrimSpace(cp)
						if strings.HasPrefix(cp, "/loc/") {
							if _, _, _, ok := taxonomy.ParseLocCanonicalPath(cp); ok {
								if resolved, accepted := acceptVertexPath(cp); accepted {
									return resolved, nil
								}
							}
						}
					}
				}
			}

			// JURISDICTION-AWARE: Prefer results from the same jurisdiction as source
			if sourceJurisdiction != "" {
				for _, r := range resp.Results {
					if r.Document.StructData == nil {
						continue
					}
					if cp, ok := r.Document.StructData["canonicalPath"].(string); ok && cp != "" && !strings.HasPrefix(cp, "/discovered/") {
						resultJurisdiction := extractJurisdiction(cp)
						if resultJurisdiction == sourceJurisdiction {
							fmt.Printf("%s Vertex hit with matching jurisdiction: %s (source=%s)\n", logPrefix, cp, sourceJurisdiction)
							if resolved, accepted := acceptVertexPath(cp); accepted {
								return resolved, nil
							}
						}
					}
				}
				// Fallback: global/empty jurisdiction
				for _, r := range resp.Results {
					if r.Document.StructData == nil {
						continue
					}
					if cp, ok := r.Document.StructData["canonicalPath"].(string); ok && cp != "" && !strings.HasPrefix(cp, "/discovered/") {
						resultJurisdiction := extractJurisdiction(cp)
						if resultJurisdiction == "" || resultJurisdiction == "global" {
							fmt.Printf("%s Vertex hit with global jurisdiction: %s\n", logPrefix, cp)
							if resolved, accepted := acceptVertexPath(cp); accepted {
								return resolved, nil
							}
						}
					}
				}
			}

			// Pick the first result with a usable canonicalPath (fallback)
			for _, r := range resp.Results {
				if r.Document.StructData == nil {
					continue
				}
				if cp, ok := r.Document.StructData["canonicalPath"].(string); ok && cp != "" && !strings.HasPrefix(cp, "/discovered/") {
					if resolved, accepted := acceptVertexPath(cp); accepted {
						return resolved, nil
					}
				}
			}
		}
	}

	// 2.5) Retry Vertex with normalized name if raw search missed.
	// Catches cross-run duplicates where "Gazprom PJSC" is indexed but we're searching for "Gazprom".
	if e.planner != nil && e.planner.vertexSearchClient != nil && e.planner.vertexSearchClient.IsConfigured() {
		rawQuery := strings.TrimSpace(rel.TargetShortName)
		if rawQuery == "" {
			rawQuery = strings.TrimSpace(rel.TargetLegalName)
		}
		if rawQuery == "" {
			rawQuery = rel.TargetName
		}
		normalizedQuery := naming.NormalizeEntityName(rawQuery)
		if normalizedQuery != "" && normalizedQuery != strings.ToLower(rawQuery) {
			req := &ai.SearchRequest{
				Query:    normalizedQuery,
				PageSize: 5,
				Filter:   `structData.type: "horizon_fragment"`,
			}
			resp, err := e.planner.vertexSearchClient.Search(ctx, req)
			if err == nil && resp != nil {
				sourceJurisdiction := extractJurisdiction(strings.TrimSpace(rel.SourceCanonicalPath))
				for _, r := range resp.Results {
					if r.Document.StructData == nil {
						continue
					}
					cp, ok := r.Document.StructData["canonicalPath"].(string)
					if !ok || cp == "" || strings.HasPrefix(cp, "/discovered/") {
						continue
					}
					resultJurisdiction := extractJurisdiction(cp)
					jurisdictionOK := sourceJurisdiction == "" ||
						resultJurisdiction == sourceJurisdiction ||
						resultJurisdiction == "" || resultJurisdiction == "global"
					if !jurisdictionOK {
						continue
					}
					if resolved, accepted := e.acceptVertexCanonicalPath(ctx, rel, relType, cp); accepted {
						fmt.Printf("%s Vertex normalized-retry hit: %q (normalized=%q) -> %s\n",
							logPrefix, rawQuery, normalizedQuery, cp)
						return resolved, nil
					}
				}
			}
		}
	}

	// 3) LLM canonicalization
	if e.aiExecutor == nil {
		return "", fmt.Errorf("ai executor not configured for canonicalization")
	}
	prompt := buildCanonicalizationPrompt(rel)
	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         4000,
			MaxOutputSize:            1024,
		},
		Verifier: verifier,
	}

	res, err := e.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return "", fmt.Errorf("canonicalization task failed: %w", err)
	}
	b, _ := json.Marshal(res)
	var out struct {
		CanonicalPath string `json:"canonicalPath"`
		LegalName     string `json:"legalName"`
		ShortName     string `json:"shortName"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("failed to parse canonicalization result: %w", err)
	}
	out.CanonicalPath = strings.TrimSpace(out.CanonicalPath)
	if out.CanonicalPath == "" {
		return "", fmt.Errorf("canonicalization returned empty canonicalPath")
	}
	if strings.HasPrefix(out.CanonicalPath, "/discovered/") {
		return "", fmt.Errorf("canonicalization returned forbidden /discovered canonicalPath: %s", out.CanonicalPath)
	}

	// Normalize org/gov slugs: strip legal suffixes that LLMs embed in slugs.
	// Example: /org/tr/botas-as → /org/tr/botas, /org/ru/gazprom-pjsc → /org/ru/gazprom
	if strings.HasPrefix(out.CanonicalPath, "/org/") || strings.HasPrefix(out.CanonicalPath, "/gov/") {
		out.CanonicalPath = normalizeOrgSlugLegalSuffix(out.CanonicalPath)
	}

	// Enforce namespace consistency when targetType is known.
	if relType != "" {
		// MIGRATION NOTE (AH taxonomy): legacy ah.FragmentClassOrg /
		// ah.FragmentClassOrg / ah.FragmentClassSite / ah.FragmentClassSite /
		// ah.FragmentClassGeo collapsed onto parent classes
		// (org/site/geo). The class-only switch can no longer assert a single
		// dedicated prefix for those subtypes, so the org case now allows
		// /org, /gov, /program, /media; the site case now allows /fac,
		// /deposit, /supply-link; and the geo case now allows /loc and /zone.
		// Subtype-precise prefix validation still happens in
		// policy.IsCanonicalPathAllowedForType (called via PreCreateValidator).
		var allowed []string
		switch relType {
		case ah.FragmentClassOrg:
			allowed = []string{"/org/", "/gov/", "/program/", "/media/"}
		case ah.FragmentClassSite:
			allowed = []string{"/fac/", "/deposit/", "/supply-link/"}
		case ah.FragmentClassProduct:
			allowed = []string{"/product/"}
		case ah.FragmentClassMarket:
			allowed = []string{"/market/"}
		case ah.FragmentClassGeo:
			allowed = []string{"/loc/", "/zone/"}
		case ah.FragmentClassEvidence:
			allowed = []string{"/event/", "/domain/", "/sphere/"}
		case ah.FragmentClassPerson:
			allowed = []string{"/person/"}
		case ah.FragmentClassCommunity:
			allowed = []string{"/community/"}
		case ah.FragmentClassDecision:
			allowed = []string{"/decision/"}
		case ah.FragmentClassMechanism:
			allowed = []string{"/mechanism/"}
		}
		if len(allowed) > 0 {
			matched := false
			for _, prefix := range allowed {
				if strings.HasPrefix(out.CanonicalPath, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				return "", fmt.Errorf("canonicalization returned canonicalPath %q that does not match targetType %q (expected one of %v)",
					out.CanonicalPath, string(relType), allowed)
			}
		}
	}

	// Normalize market domain segment (e.g., "financials" → "finance") to prevent
	// near-duplicate markets with variant domain names.
	if relType == ah.FragmentClassMarket && strings.HasPrefix(out.CanonicalPath, "/market/") {
		out.CanonicalPath = normalizeMarketCanonicalDomain(out.CanonicalPath)
	}

	// Validate LLM output through ValidationPolicy if available
	if e.validationPolicy != nil {
		// Create a test fragment to validate the canonical path
		testFragment := &models.HorizonFragment{
			CanonicalPath: out.CanonicalPath,
			Class:         relType,
		}

		decision, err := e.validationPolicy.Execute(ctx, policy.PolicyInput{
			Kind:     "fragment",
			Context:  map[string]interface{}{"validationKind": "fragment"},
			Fragment: testFragment,
		})

		if err != nil {
			fmt.Printf("%s ERROR: ValidationPolicy failed for LLM output: %v\n", logPrefix, err)
			// Continue with hardcoded validation
		} else {
			fmt.Printf("%s ValidationPolicy check for LLM output: action=%s, path=%s\n",
				logPrefix, decision.Action, decision.DecisionPath)

			policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{})
		}
	}

	// GEO strictness: require valid /loc hierarchy with valid country/region token.
	if relType == ah.FragmentClassGeo {
		cp := out.CanonicalPath
		if !strings.HasPrefix(cp, "/loc/") {
			return "", fmt.Errorf("geo canonicalization must return /loc/{country}[/{level1}[/{level2}]], got: %s", cp)
		}
		countryCode, _, _, ok := taxonomy.ParseLocCanonicalPath(cp)
		if !ok {
			return "", fmt.Errorf("geo canonicalization must match /loc/{country}[/{level1}[/{level2}]], got: %s", cp)
		}
		if !taxonomy.IsValidLocCountryToken(countryCode) {
			return "", fmt.Errorf("geo canonicalization has invalid country/region %q, got: %s", countryCode, cp)
		}
		fmt.Printf("%s GEO_REGION validation passed: %s\n", logPrefix, cp)
	}

	// Final hard preference: if preferred canonicalPath exists, use it (even if LLM suggested a suffix variant).
	if e.fragmentsRepo != nil {
		for _, pref := range preferredCanonicalPaths(rel) {
			if strings.TrimSpace(pref) == "" {
				continue
			}
			if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
				return pref, nil
			}
		}
	}
	return e.finalizeRelationCanonicalPath(ctx, rel, out.CanonicalPath, relType), nil
}

func (e *ScoutEngine) acceptVertexCanonicalPath(
	ctx context.Context,
	rel agents.DiscoveredRelation,
	relType FragmentType,
	candidate string,
) (string, bool) {
	cp := strings.TrimSpace(candidate)
	if cp == "" || strings.HasPrefix(cp, "/discovered/") {
		return "", false
	}
	if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
		return "", false
	}
	if relType != "" && !canonicalPathMatchesType(cp, relType) {
		return "", false
	}
	cp = e.finalizeRelationCanonicalPath(ctx, rel, cp, relType)
	if cp == "" {
		return "", false
	}
	if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
		return "", false
	}
	if relType != "" && !canonicalPathMatchesType(cp, relType) {
		return "", false
	}
	// Firestore is source of truth; stale Vertex rows must not win canonical resolution.
	if e.fragmentsRepo != nil {
		frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
			return "", false
		}
		if relType != "" && frag.Class != "" && frag.Class != relType {
			if !(relType == ah.FragmentClassOrg && strings.HasPrefix(cp, "/gov/")) {
				return "", false
			}
		}
		return strings.TrimSpace(frag.CanonicalPath), true
	}
	return cp, true
}

func canonicalPathMatchesType(canonicalPath string, relType FragmentType) bool {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || relType == "" {
		return true
	}
	// MIGRATION NOTE (AH taxonomy): see preferredCanonicalPaths — legacy
	// ah.FragmentClassOrg / MediaChannel / Deposit / SupplyLink / AffectedZone
	// collapsed onto parent classes. Allow the corresponding subtype path
	// prefixes inside the parent-class arms; precise subtype validation is
	// done elsewhere via models.ClassSubtype helpers.
	switch relType {
	case ah.FragmentClassOrg:
		return strings.HasPrefix(cp, "/org/") ||
			strings.HasPrefix(cp, "/gov/") ||
			strings.HasPrefix(cp, "/program/") ||
			strings.HasPrefix(cp, "/media/")
	case ah.FragmentClassSite:
		return strings.HasPrefix(cp, "/fac/") ||
			strings.HasPrefix(cp, "/deposit/") ||
			strings.HasPrefix(cp, "/supply-link/")
	case ah.FragmentClassProduct:
		return strings.HasPrefix(cp, "/product/")
	case ah.FragmentClassMarket:
		return strings.HasPrefix(cp, "/market/")
	case ah.FragmentClassGeo:
		return strings.HasPrefix(cp, "/loc/") || strings.HasPrefix(cp, "/zone/")
	case ah.FragmentClassEvidence:
		return strings.HasPrefix(cp, "/event/") ||
			strings.HasPrefix(cp, "/domain/") ||
			strings.HasPrefix(cp, "/sphere/")
	case ah.FragmentClassPerson:
		return strings.HasPrefix(cp, "/person/")
	case ah.FragmentClassCommunity:
		return strings.HasPrefix(cp, "/community/")
	default:
		return true
	}
}

func (e *ScoutEngine) finalizeRelationCanonicalPath(ctx context.Context, rel agents.DiscoveredRelation, candidate string, relType FragmentType) string {
	if e == nil {
		return strings.TrimSpace(candidate)
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || e.canonicalPathResolver == nil || relType == "" {
		return candidate
	}
	if relType == ah.FragmentClassOrg {
		label := strings.TrimSpace(rel.TargetLegalName)
		if label == "" {
			label = strings.TrimSpace(rel.TargetName)
		}
		if label == "" {
			label = strings.TrimSpace(rel.TargetShortName)
		}
		if label != "" && strings.HasPrefix(candidate, "/org/") && looksLikeGovLabel(label) {
			iso2 := inferISO2FromLabel(label, nil)
			if iso2 == "" {
				iso2 = "na"
			}
			if govPath := taxonomy.GovCanonicalPath(iso2, label); govPath != "" {
				return strings.TrimSpace(govPath)
			}
		}
		if legal := strings.TrimSpace(rel.TargetLegalName); legal != "" && strings.HasPrefix(candidate, "/org/") {
			iso2 := "na"
			parts := strings.Split(strings.Trim(candidate, "/"), "/")
			if len(parts) == 3 {
				iso2 = strings.TrimSpace(parts[1])
			}
			if upgraded := taxonomy.OrgCanonicalPath(iso2, legal); upgraded != "" && strings.TrimSpace(upgraded) != candidate {
				return strings.TrimSpace(upgraded)
			}
		}
		meta := map[string]string{}
		if v := strings.TrimSpace(rel.TargetShortName); v != "" {
			meta["shortName"] = v
		}
		if v := strings.TrimSpace(rel.TargetLegalName); v != "" {
			meta["legalName"] = v
		}
		if v := strings.TrimSpace(rel.TargetName); v != "" && meta["shortName"] == "" {
			meta["shortName"] = v
		}
		if resolved, err := e.canonicalPathResolver.ResolveWithMetadata(ctx, candidate, relType, meta); err == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
			return strings.TrimSpace(resolved.CanonicalPath)
		}
	}
	if resolved, err := e.canonicalPathResolver.Resolve(ctx, candidate, relType); err == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
		return strings.TrimSpace(resolved.CanonicalPath)
	}
	return candidate
}

func buildCanonicalizationPrompt(rel agents.DiscoveredRelation) string {
	canonicalRulesBlock := taxonomy.CanonicalPathRulesPrompt()
	// Strict schema (embedded) as requested.
	return fmt.Sprintf(`You are a canonicalization agent for a risk analysis system.

INPUT (JSON):
{
  "targetName": %q,
  "targetLegalName": %q,
  "targetShortName": %q,
  "targetType": %q
}

TASK:
Return a SINGLE JSON object that matches EXACTLY this schema:
{
  "canonicalPath": "string",
  "legalName": "string",
  "shortName": "string"
}

RULES:
- canonicalPath MUST be stable and must NOT start with "/discovered/".
- ORGANIZATION NAMING (CRITICAL):
  - Do NOT include country in the name — use only the official name (e.g., "Core42 UAE" → "Core42"; country is already in /org/{geo}/...).
  - Do NOT include legal form (LLC, Inc, Ltd, GmbH, AG, etc.) in legalName, shortName, or canonicalPath slug.
  - legalName: official name without country suffix and without legal form.
  - shortName: base/trade name without country and without legal form.
- %s
- canonicalPath slug MUST be derived from shortName (base name only, no legal form).
- Intangible-but-real actors (alliances, coalitions, unions, regulators, ministries, agencies, associations) are ORGANIZATION.
- Government bodies MUST use /gov/{geo}/{slug} (geo = jurisdiction). Other orgs use /org/{geo}/{slug} (base name only, no legal form). Use /gov/na/{slug} or /org/na/{slug} ONLY as an exceptional last-resort when neither context nor the entity name provides a defensible jurisdiction clue.
- GOV HIERARCHY: Government bodies form administrative hierarchies:
  - government → ministry → department → agency/authority/regulator
  - GovTypes: government, ministry, department, agency, authority, regulator, committee, service, bureau, office, council, commission
  - When canonicalizing, prefer specific agency names over generic ministry references
  - Example: "Saudi FDA" → /gov/sa/sfda (type: authority), parent would be /gov/sa/ministry-of-health
  - Use these namespaces:
  - ORGANIZATION:
    - /org/{geo}/{lowercase-slug} (companies, NGOs, etc.; base name only, no legal form; /org/na only as exceptional last-resort)
    - /gov/{geo}/{lowercase-slug} (ministries, regulators, agencies, state bodies; /gov/na only as exceptional last-resort)
  - PERSON: /person/{lowercase-slug}
  - PRODUCT: MUST use hierarchical structure /product/{category}/{type}/{stage}/{slug}
    - category: "raw" | "component" | "finished" | "service"
    - type: "mineral" | "agricultural" | "chemical" | "electronic" | "mechanical" | "energy"
    - stage: "extraction" | "refining" | "processing" | "manufacturing" | "assembly"
    - slug: lowercase-slug derived from product name
    - Examples:
      - "Lithium Ore" -> /product/raw/mineral/extraction/lithium-ore
      - "Lithium Carbonate" -> /product/raw/chemical/refining/lithium-carbonate
      - "Battery Cell" -> /product/component/electronic/manufacturing/battery-cell
      - "Electric Vehicle" -> /product/finished/mechanical/assembly/electric-vehicle
    - If category/type/stage cannot be determined, still output /product/{category}/{type}/{stage}/{slug} with your best classification
  - PROGRAM:
    - Use /program/{jurisdictionSlug}/{programSlug} (e.g., "Kazakhstan Alem.ai" -> /program/kazakhstan/alem-ai)
      - jurisdictionSlug can be ISO2, regional token, or "global"/"int"
      - Regional tokens ALWAYS go in the jurisdiction slot (2nd segment): /program/gcc/{slug} (NOT /program/na/gcc/{slug})
    - If jurisdiction is unknown, use "global" as jurisdiction
  - MARKET:
    - HIERARCHY LEVELS:
      - L1 (domain): /market/{geo}/{domain} — entire sector
        - "Iraqi agriculture" → /market/iq/agriculture
      - L2 (vertical): /market/{geo}/{domain}/{vertical} — sub-segment of a domain
        - "Iraqi crop production" → /market/iq/agriculture/crop-production
      - L3+ (sub-vertical): /market/{geo}/{domain}/{vertical}/{sub-vertical} — narrower niche under a vertical
        - "Iraqi rice production" → /market/iq/agriculture/crop-production/rice-production
        - "Saudi Islamic banking" → /market/sa/finance/banking/islamic-banking
        - "Taiwan semiconductor foundry" → /market/tw/technology/semiconductors/foundry
      - Rule: child path = parent path + exactly one segment
      - NEVER repeat a segment already in the path (e.g. /market/iq/agriculture/agriculture is INVALID)
    - domain/vertical SHOULD align with /segment/{domain}/{vertical}/{geo} taxonomy used for organizations
    - Use GICS terminology (no numeric codes). If not in GICS, use the closest GICS parent + a descriptive term.
    - geo: `+taxonomy.ISO2GeoDescriptionForPrompts()+`
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical} (NOT /market/na/{domain}/gcc)
    - GEOGRAPHY INFERENCE: Extract geo from the market name/context:
      - If the name mentions a country/region, use that country's ISO2 code
      - Use "global" ONLY when no geographic context is present
    - MARKET HIERARCHY: Markets form broader → narrower hierarchies
      - Parent relationships are modeled as HAS_MARKET_SEGMENT edges (broader → narrower)
      - Example chains: /market/sa/finance → .../banking → .../islamic-banking; /market/tw/technology → .../semiconductors → .../foundry
    - Examples:
      - "SME banking in Iraq" -> /market/iq/finance/banking
      - "Taiwan semiconductor foundries" -> /market/tw/technology/semiconductors
      - "Iraqi agriculture" -> /market/iq/agriculture (L1)
      - "Iraqi crop production" -> /market/iq/agriculture/crop-production (L2)
      - "Iraqi rice production" -> /market/iq/agriculture/crop-production/rice-production (L3)
      - "Global EV batteries" -> /market/global/automotive/ev-batteries
      - "UAE fintech sector" -> /market/ae/finance/fintech
  - FACILITY:
    - Use /fac/{geo}/{facilitySlug} (e.g., "TSMC Fab 21 in Taiwan" -> /fac/tw/fab-21)
    - geo: `+taxonomy.ISO2GeoDescriptionForPrompts()+`
  - GEO_REGION: MUST be /loc/{country}[/{level1}[/{level2}]]
    - country: `+taxonomy.ISO2GeoDescriptionForPrompts()+`
    - Regional tokens ALWAYS go in the country slot (2nd segment), never at the end:
      - GCC → /loc/gcc (NOT /loc/global/gcc)
    - level1/level2: optional subdivision/city slugs; omit unknown trailing levels
    - For country-only locations, use: /loc/{country}
    - CRITICAL: country code MUST be valid.
    Examples (3-position model):
    - USA / Texas / Austin: /loc/us/tx/austin
    - Germany / Bavaria / Munich: /loc/de/by/munich
    - France / Île-de-France / Paris: /loc/fr/idf/paris
    - Russia / Moscow Oblast / Moscow: /loc/ru/mos/moscow
    - Brazil / São Paulo / São Paulo: /loc/br/sp/sao-paulo
    - Kazakhstan / Astana: /loc/kz/astana
  - EVENT: /event/{lowercase-slug}
  - COMMUNITY: /community/{geo}/{type}/{slug}
    - geo: `+taxonomy.ISO2GeoDescriptionForPrompts()+`
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /community/gcc/{type}/{slug} (NOT /community/na/{type}/gcc)
    - type: trade | activist | professional | criminal | religious | ethnic | interest | dynasty | tribe | representatives | industry-association | key-community
      - trade: commercial networks without formal structure (hawala, informal trade networks)
      - activist: political/social activist groups
      - professional: professional communities without legal entity (OSINT researchers, developers)
      - criminal: criminal networks and organizations
      - religious: religious communities and congregations
      - ethnic: diaspora and ethnic communities
      - interest: hobby/interest-based communities
      - dynasty: family dynasties (house-of-saud)
      - tribe: tribal/clan communities (pashtun, durrani)
      - representatives: representative bodies (lobby groups, delegations)
      - industry-association: industry associations, trade bodies
      - key-community: key stakeholder communities in a domain
    - slug: lowercase-slug derived from community name
    - Examples:
      - "Hawala network in Dubai" → /community/ae/trade/hawala-dubai-network
      - "Kazan drug dealers" → /community/ru/criminal/kazan-synthetic-dealers
      - "NEXTA supporters" → /community/by/activist/nexta-supporters
      - "OSINT community" → /community/global/professional/osint-researchers
    - CRITICAL: COMMUNITY is for INFORMAL groups WITHOUT legal structure.
      - If the entity has legal registration → use ORGANIZATION (/org/)
      - If the entity is a media endpoint (channel/group) → use MEDIA_CHANNEL (/media/)
      - COMMUNITY is the social entity that *uses* channels, not the channel itself
- VALIDATION POLICY RULES (MUST comply or output will be REJECTED):
  - canonicalPath MUST NOT start with "/discovered/" (will be rejected)
  - canonicalPath MUST match expected prefix for targetType:
    - ORGANIZATION -> /org/ OR /gov/
    - FACILITY -> /fac/ (format: /fac/{geo}/{facilitySlug})
    - GEO_REGION -> /loc/ (strictly enforced)
    - PRODUCT -> /product/
    - PROGRAM -> /program/
    - MARKET -> /market/
    - EVENT -> /event/
    - PERSON -> /person/
    - COMMUNITY -> /community/ (format: /community/{geo}/{type}/{slug})
  - GEO_REGION paths MUST match /loc/{country}[/{level1}[/{level2}]]
  - GEO_REGION country token MUST be valid
- Output only JSON, no markdown, no extra text.
`, rel.TargetName, rel.TargetLegalName, rel.TargetShortName, rel.TargetType, canonicalRulesBlock)
}

// collapseMarketDomainEqualVertical removes any segment that duplicates its immediate
// predecessor in a market canonical path (e.g. /market/iq/agriculture/agriculture →
// /market/iq/agriculture, /market/sa/finance/banking/banking → /market/sa/finance/banking).
func collapseMarketDomainEqualVertical(cp string) string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 4 || parts[0] != "market" {
		return cp
	}
	clean := parts[:2] // "market", geo
	for i := 2; i < len(parts); i++ {
		if parts[i] != parts[i-1] {
			clean = append(clean, parts[i])
		}
	}
	if len(clean) == len(parts) {
		return cp
	}
	return "/" + strings.Join(clean, "/")
}

// normalizeMarketCanonicalDomain rewrites the domain segment of a /market/{geo}/{domain}/{vertical}
// path using taxonomy.NormalizeMarketDomain. This prevents LLM from creating near-duplicate
// markets with variant domain names (e.g., /market/sa/financials/fintech → /market/sa/finance/fintech).
// Also collapses domain==vertical to prevent /market/iq/agriculture/agriculture duplicates.
func normalizeMarketCanonicalDomain(cp string) string {
	cp = collapseMarketDomainEqualVertical(cp)
	trimmed := strings.Trim(cp, "/")
	parts := strings.Split(trimmed, "/")
	// /market/{geo}/{domain}/{vertical} → need at least 4 parts
	if len(parts) < 4 || parts[0] != "market" {
		return cp // short or malformed — leave as-is
	}
	normalized := taxonomy.NormalizeMarketDomain(parts[2])
	if normalized == parts[2] {
		return cp // no change
	}
	parts[2] = normalized
	return "/" + strings.Join(parts, "/")
}

// slugLegalSuffixes are legal form tokens that LLMs embed in canonical slugs.
// Kept short and unambiguous to avoid false positives (e.g., "co" is NOT included
// because it's a common word fragment like "colorado").
var slugLegalSuffixes = []string{
	"-pjsc", "-jsc", "-cjsc", "-ojsc",
	"-llc", "-pllc", "-inc", "-ltd", "-corp", "-corporation",
	"-gmbh", "-ag", "-se", "-spa", "-sa", "-ab", "-oy",
	"-nv", "-bv", "-sarl", "-srl", "-plc", "-lp", "-llp",
	"-as", "-pty-ltd", "-limited", "-company", "-holdings", "-group",
}

// normalizeOrgSlugLegalSuffix strips legal form suffixes from the slug part of
// org/gov canonical paths. Example: /org/tr/botas-as → /org/tr/botas.
// Guards: only touches paths with >=3 parts, never produces empty slugs.
func normalizeOrgSlugLegalSuffix(cp string) string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 3 {
		return cp
	}
	slug := parts[len(parts)-1]
	original := slug
	for _, sfx := range slugLegalSuffixes {
		if strings.HasSuffix(slug, sfx) {
			candidate := strings.TrimSuffix(slug, sfx)
			if len(candidate) >= 2 {
				slug = candidate
				break
			}
		}
	}
	if slug == original {
		return cp
	}
	parts[len(parts)-1] = slug
	return "/" + strings.Join(parts, "/")
}

// --- Batch canonicalization ---

type canonBatchEntry struct {
	Index    int
	Relation agents.DiscoveredRelation
	Result   string
	Resolved bool
	RelType  FragmentType
}

const batchCanonMaxPerLLMCall = 20

// resolveCanonicalPathsBatch pre-resolves canonical paths for all relations in two passes:
// Pass 1: deterministic (cache, SessionGraph, Firestore, inline validation) — no LLM cost.
// Pass 2: batch LLM for any unresolved entries (batches of up to 20).
func (e *ScoutEngine) resolveCanonicalPathsBatch(
	ctx context.Context,
	relations []agents.DiscoveredRelation,
) []canonBatchEntry {
	entries := make([]canonBatchEntry, len(relations))
	for i, rel := range relations {
		entries[i] = canonBatchEntry{Index: i, Relation: rel}

		relType := e.normalizeFragmentType(ctx, rel.TargetType)
		if cp := strings.TrimSpace(rel.TargetCanonicalPath); cp != "" {
			if inferred := InferFragmentTypeFromCanonicalPath(cp); inferred != "" && inferred != relType {
				relType = inferred
			}
		}
		entries[i].RelType = relType

		// Fast path 1: canon cache
		targetName := batchEntryTargetName(rel)
		if cc := canonCacheFromContext(ctx); cc != nil && targetName != "" {
			if cached, ok := cc.get(canonCacheKey(targetName, relType)); ok {
				entries[i].Result = cached
				entries[i].Resolved = true
				continue
			}
		}

		// Fast path 2: inline validation — structurally valid provided path
		if cp := strings.TrimSpace(rel.TargetCanonicalPath); cp != "" && !strings.HasPrefix(cp, "/discovered/") {
			if isCanonicalPathValid(cp, relType) {
				if e.fragmentsRepo != nil {
					if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); err == nil && f != nil && f.ID != "" {
						entries[i].Result = f.CanonicalPath
						entries[i].Resolved = true
						continue
					}
				}
				entries[i].Result = cp
				entries[i].Resolved = true
				continue
			}
		}

		// Fast path 3: SessionGraph
		if sg := SessionGraphFromContext(ctx); sg != nil {
			for _, pref := range preferredCanonicalPaths(rel) {
				if pref == "" {
					continue
				}
				resolved := sg.ResolvePath(pref)
				if frag := sg.GetByPath(resolved); frag != nil && frag.ID != "" {
					entries[i].Result = frag.CanonicalPath
					entries[i].Resolved = true
					break
				}
			}
			if entries[i].Resolved {
				continue
			}
			if targetName != "" {
				sourceJurisdiction := extractJurisdiction(strings.TrimSpace(rel.SourceCanonicalPath))
				if frag := sg.GetByNamePreferJurisdiction(targetName, sourceJurisdiction); frag != nil && frag.ID != "" {
					entries[i].Result = frag.CanonicalPath
					entries[i].Resolved = true
					continue
				}
			}
		}

		// Fast path 4: preferred canonical paths in Firestore
		if e.fragmentsRepo != nil {
			for _, pref := range preferredCanonicalPaths(rel) {
				if pref == "" {
					continue
				}
				if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
					entries[i].Result = f.CanonicalPath
					entries[i].Resolved = true
					break
				}
			}
		}
	}

	// Pass 2: batch LLM for unresolved
	var unresolved []int
	for i, ent := range entries {
		if !ent.Resolved {
			unresolved = append(unresolved, i)
		}
	}

	if len(unresolved) > 0 && e.aiExecutor != nil {
		results := e.batchCanonicalizeLLM(ctx, entries, unresolved)
		for idx, cp := range results {
			if cp != "" {
				entries[idx].Result = cp
				entries[idx].Resolved = true
				if cc := canonCacheFromContext(ctx); cc != nil {
					name := batchEntryTargetName(entries[idx].Relation)
					if name != "" {
						cc.set(canonCacheKey(name, entries[idx].RelType), cp)
					}
				}
			}
		}
	}

	// Remaining unresolved: fall through to per-relation resolveCanonicalPath
	return entries
}

func batchEntryTargetName(rel agents.DiscoveredRelation) string {
	n := strings.TrimSpace(rel.TargetShortName)
	if n == "" {
		n = strings.TrimSpace(rel.TargetLegalName)
	}
	if n == "" {
		n = strings.TrimSpace(rel.TargetName)
	}
	return n
}

type batchCanonLLMResult struct {
	Index         int    `json:"index"`
	CanonicalPath string `json:"canonicalPath"`
	LegalName     string `json:"legalName"`
	ShortName     string `json:"shortName"`
}

// batchCanonicalizeLLM sends unresolved entries to the LLM in batches, returning
// a map of entry index -> resolved canonical path.
func (e *ScoutEngine) batchCanonicalizeLLM(
	ctx context.Context,
	entries []canonBatchEntry,
	indices []int,
) map[int]string {
	result := make(map[int]string, len(indices))

	for batchStart := 0; batchStart < len(indices); batchStart += batchCanonMaxPerLLMCall {
		batchEnd := batchStart + batchCanonMaxPerLLMCall
		if batchEnd > len(indices) {
			batchEnd = len(indices)
		}
		batch := indices[batchStart:batchEnd]

		prompt := buildBatchCanonicalizationPrompt(entries, batch)
		verifier := ai.NewSimpleJSONVerifier()

		task := &ai.AITask{
			TaskType:     ai.TaskTypeDomainClassification,
			InputContent: prompt,
			AIRequirements: ai.AIRequirements{
				TextGenerationCapability: true,
				MinCapabilityScore:       7,
				MinContextWindow:         8000,
				MaxOutputSize:            4096,
			},
			Verifier: verifier,
		}

		res, err := e.aiExecutor.ExecuteStructuredTask(ctx, task)
		if err != nil {
			fmt.Printf("[ScoutEngine:batchCanon] batch LLM call failed: %v, falling back to per-relation\n", err)
			continue
		}

		b, _ := json.Marshal(res)
		var parsed []batchCanonLLMResult
		if err := json.Unmarshal(b, &parsed); err != nil {
			var wrapper struct {
				Results []batchCanonLLMResult `json:"results"`
			}
			if err2 := json.Unmarshal(b, &wrapper); err2 != nil {
				fmt.Printf("[ScoutEngine:batchCanon] failed to parse batch result: %v\n", err)
				continue
			}
			parsed = wrapper.Results
		}

		for _, r := range parsed {
			cp := strings.TrimSpace(r.CanonicalPath)
			if cp == "" || strings.HasPrefix(cp, "/discovered/") {
				continue
			}
			if r.Index < 0 || r.Index >= len(batch) {
				continue
			}
			entryIdx := batch[r.Index]
			relType := entries[entryIdx].RelType
			if isCanonicalPathValid(cp, relType) {
				cp = e.finalizeRelationCanonicalPath(ctx, entries[entryIdx].Relation, cp, relType)
				if cp != "" {
					result[entryIdx] = cp
				}
			}
		}
	}

	return result
}

func buildBatchCanonicalizationPrompt(entries []canonBatchEntry, indices []int) string {
	canonicalRulesBlock := taxonomy.CanonicalPathRulesPrompt()

	var sb strings.Builder
	sb.WriteString(`You are a canonicalization agent for a risk analysis system.

TASK: Canonicalize ALL entities below into stable canonical paths.

INPUT ENTITIES (JSON array):
[
`)
	for i, idx := range indices {
		rel := entries[idx].Relation
		if i > 0 {
			sb.WriteString(",\n")
		}
		fmt.Fprintf(&sb, `  {"index": %d, "targetName": %q, "targetLegalName": %q, "targetShortName": %q, "targetType": %q}`,
			i, rel.TargetName, rel.TargetLegalName, rel.TargetShortName, rel.TargetType)
	}
	sb.WriteString("\n]\n\n")
	fmt.Fprintf(&sb, "CANONICAL PATH RULES:\n%s\n\n", canonicalRulesBlock)
	sb.WriteString(`OUTPUT FORMAT: Return a JSON array where each element has:
{
  "index": <same index from input>,
  "canonicalPath": "/prefix/...",
  "legalName": "...",
  "shortName": "..."
}

Return ONLY the JSON array. No markdown fences. Every input entity MUST have a corresponding output entry.
`)
	return sb.String()
}
