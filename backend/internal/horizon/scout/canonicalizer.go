package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

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
// For facilities we emit canonical /fac/{orgSlug}/{facilitySlug} candidates.
//
// Repo/Vertex checks decide which one is stable.
func preferredCanonicalPaths(rel agents.DiscoveredRelation) []string {
	t := normalizeFragmentType(rel.TargetType)
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

	switch t {
	case models.FragmentTypeOrganization:
		// Prefer jurisdiction-scoped org path to reduce cross-country name collisions.
		// Without jurisdiction in the relation payload, we default to "na".
		return []string{"/org/na/" + slug}
	case models.FragmentTypeFacility:
		out := make([]string, 0, 2)
		// Candidate nested: take first word as org token, remainder as facility label.
		if parts := strings.Fields(short); len(parts) >= 2 {
			orgTok := strings.TrimSpace(parts[0])
			rest := strings.TrimSpace(strings.TrimPrefix(short, parts[0]))
			orgSlug := slugify(orgTok)
			facSlug := slugify(rest)
			if orgSlug != "" && facSlug != "" {
				out = append(out, "/fac/"+orgSlug+"/"+facSlug)
			}
		}
		if len(out) == 0 {
			out = append(out, "/fac/na/"+slug)
		}
		return out
	case models.FragmentTypeProduct:
		ls := strings.TrimSpace(strings.ToLower(short))
		if ls == "na" || ls == "n/a" || ls == "unknown" || ls == "none" {
			return nil
		}
		// Use ProductCanonicalizer to generate hierarchical paths
		// Note: preferredCanonicalPaths is called without context, so we use Background
		// Full canonicalization with context happens in resolveCanonicalPath
		canonicalizer := NewProductCanonicalizer()
		result, err := canonicalizer.CanonicalizeProduct(context.Background(), short)
		if err == nil && result != nil && result.CanonicalPath != "" {
			return []string{result.CanonicalPath}
		}
		// Final fallback: keep strict hierarchical contract.
		if slug == "na" {
			return nil
		}
		return []string{BuildProductCanonicalPath("", "", "", slug)}
	case models.FragmentTypeProgram:
		// Canonical: /program/{jurisdiction}/{slug}. Without explicit jurisdiction, default to global.
		return []string{"/program/global/" + slug}
	case models.FragmentTypeMarket:
		// Canonical L1: /market/{geo}/{domain}. Without geo, default to global.
		return []string{"/market/global/" + slug}
	case models.FragmentTypeEvent:
		return []string{"/event/" + slug}
	case models.FragmentTypeGeoRegion:
		// For GEO, do NOT guess /loc without explicit country/admin1.
		// We'll use:
		// - normalized input paths (e.g., /region/na/us/tx/austin -> /loc/us/tx/austin)
		// - Vertex Search / existing fragments
		// - LLM canonicalization with strict /loc schema
		return nil
	case models.FragmentTypePerson:
		return []string{"/person/" + slug}
	case models.FragmentTypeCommunity:
		// Canonical: /community/{geo}/{type}/{slug}. Without explicit geo/type, default to global/interest.
		return []string{"/community/global/interest/" + slug}
	default:
		return []string{"/org/na/" + slug}
	}
}

// normalizeLegacyGeoPath maps older geo namespaces to the strict /loc/{country}/{admin1}/{city} model.
// Supported legacy patterns:
// - /region/na/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
// - /region/{country}/{admin1}/{city}    -> /loc/{country}/{admin1}/{city}
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
	// parts[0] == "region"
	if len(parts) == 5 && parts[1] == "na" {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[2], parts[3], parts[4])
	}
	if len(parts) == 4 {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[1], parts[2], parts[3])
	}
	return ""
}

// normalizeFragmentType tries to map common human labels to our enum values.
// Uses CanonizationPolicy if available in ScoutEngine, otherwise falls back to hardcoded rules.
func (e *ScoutEngine) normalizeFragmentType(ctx context.Context, s string) models.FragmentType {
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
				return models.FragmentType(output)
			}
		} else if err != nil {
			fmt.Printf("%s ERROR: CanonizationPolicy failed: %v, using fallback\n", logPrefix, err)
		}
	}

	// Fallback to hardcoded normalization
	return normalizeFragmentTypeHardcoded(s)
}

// normalizeFragmentTypeHardcoded is the hardcoded fallback for fragment type normalization.
func normalizeFragmentTypeHardcoded(s string) models.FragmentType {
	return edgesemantics.NormalizeFragmentTypeToken(s)
}

// Legacy function for backward compatibility (used in preferredCanonicalPaths)
func normalizeFragmentType(s string) models.FragmentType {
	return normalizeFragmentTypeHardcoded(s)
}

// resolveCanonicalPath tries to produce a stable canonical path for a relation.
// Priority:
// 1) Use relation.TargetCanonicalPath if it is provided and not /discovered/*
// 2) Vertex AI Search lookup by name (prefers already-known canonicalPath)
// 3) LLM canonicalization (strict JSON) as a last resort
func (e *ScoutEngine) resolveCanonicalPath(ctx context.Context, rel agents.DiscoveredRelation) (string, error) {
	logPrefix := "[ScoutEngine:resolveCanonicalPath]"

	// Normalize fragment type using CanonizationPolicy if available
	relType := e.normalizeFragmentType(ctx, rel.TargetType)

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
	if relType == models.FragmentTypeGeoRegion {
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
	if relType == models.FragmentTypeProduct {
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
			// For GEO_REGION: strongly prefer /loc/{country}/{admin1}/{city}
			if relType == models.FragmentTypeGeoRegion {
				for _, r := range resp.Results {
					if r.Document.StructData == nil {
						continue
					}
					if cp, ok := r.Document.StructData["canonicalPath"].(string); ok {
						cp = strings.TrimSpace(cp)
						if strings.HasPrefix(cp, "/loc/") {
							parts := strings.Split(strings.Trim(cp, "/"), "/")
							if len(parts) == 4 {
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

	// Enforce namespace consistency when targetType is known.
	if relType != "" {
		expectedPrefix := ""
		switch relType {
		case models.FragmentTypeOrganization:
			// Organizations can be either:
			// - /org/{iso2|na}/{slug} (companies, NGOs, etc.)
			// - /gov/{iso2|na}/{slug} (government bodies: ministries, regulators, agencies)
			expectedPrefix = "/org/"
		case models.FragmentTypeFacility:
			expectedPrefix = "/fac/"
		case models.FragmentTypeProduct:
			expectedPrefix = "/product/"
		case models.FragmentTypeProgram:
			expectedPrefix = "/program/"
		case models.FragmentTypeMarket:
			expectedPrefix = "/market/"
		case models.FragmentTypeGeoRegion:
			expectedPrefix = "/loc/"
		case models.FragmentTypeEvent:
			expectedPrefix = "/event/"
		case models.FragmentTypePerson:
			expectedPrefix = "/person/"
		case models.FragmentTypeDeposit:
			expectedPrefix = "/deposit/"
		case models.FragmentTypeCommunity:
			expectedPrefix = "/community/"
		}
		if expectedPrefix != "" {
			if relType == models.FragmentTypeOrganization {
				// Special-case: government bodies are modeled as ORGANIZATION, but use /gov/... namespace.
				if !strings.HasPrefix(out.CanonicalPath, "/org/") && !strings.HasPrefix(out.CanonicalPath, "/gov/") {
					return "", fmt.Errorf("canonicalization returned canonicalPath %q that does not match targetType %q (expected /org/ or /gov/)",
						out.CanonicalPath, string(relType))
				}
			} else if !strings.HasPrefix(out.CanonicalPath, expectedPrefix) {
				return "", fmt.Errorf("canonicalization returned canonicalPath %q that does not match targetType %q (expected prefix %q)",
					out.CanonicalPath, string(relType), expectedPrefix)
			}
		}
	}

	// Normalize market domain segment (e.g., "financials" → "finance") to prevent
	// near-duplicate markets with variant domain names.
	if relType == models.FragmentTypeMarket && strings.HasPrefix(out.CanonicalPath, "/market/") {
		out.CanonicalPath = normalizeMarketCanonicalDomain(out.CanonicalPath)
	}

	// Validate LLM output through ValidationPolicy if available
	if e.validationPolicy != nil {
		// Create a test fragment to validate the canonical path
		testFragment := &models.HorizonFragment{
			CanonicalPath: out.CanonicalPath,
			Type:          relType,
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

	// GEO strictness: require /loc/{country}/{admin1}/{city} with valid country code
	if relType == models.FragmentTypeGeoRegion {
		cp := out.CanonicalPath
		if !strings.HasPrefix(cp, "/loc/") {
			return "", fmt.Errorf("geo canonicalization must return /loc/{country}/{admin1}/{city}, got: %s", cp)
		}
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) != 4 {
			return "", fmt.Errorf("geo canonicalization must have 3 components after /loc, got: %s", cp)
		}
		// Validate country code format (ISO3166-1 alpha2, lowercase, 2 chars)
		countryCode := parts[1]
		if len(countryCode) != 2 || !reCountryAlpha2.MatchString(countryCode) {
			return "", fmt.Errorf("geo canonicalization has invalid country code %q (must be ISO3166-1 alpha2 lowercase, 2 characters), got: %s", countryCode, cp)
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
	relType models.FragmentType,
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
		if relType != "" && frag.Type != "" && frag.Type != relType {
			if !(relType == models.FragmentTypeOrganization && strings.HasPrefix(cp, "/gov/")) {
				return "", false
			}
		}
		return strings.TrimSpace(frag.CanonicalPath), true
	}
	return cp, true
}

func canonicalPathMatchesType(canonicalPath string, relType models.FragmentType) bool {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || relType == "" {
		return true
	}
	switch relType {
	case models.FragmentTypeOrganization:
		return strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/")
	case models.FragmentTypeFacility:
		return strings.HasPrefix(cp, "/fac/")
	case models.FragmentTypeProduct:
		return strings.HasPrefix(cp, "/product/")
	case models.FragmentTypeProgram:
		return strings.HasPrefix(cp, "/program/")
	case models.FragmentTypeMarket:
		return strings.HasPrefix(cp, "/market/")
	case models.FragmentTypeGeoRegion:
		return strings.HasPrefix(cp, "/loc/")
	case models.FragmentTypeEvent:
		return strings.HasPrefix(cp, "/event/")
	case models.FragmentTypePerson:
		return strings.HasPrefix(cp, "/person/")
	case models.FragmentTypeDeposit:
		return strings.HasPrefix(cp, "/deposit/")
	case models.FragmentTypeCommunity:
		return strings.HasPrefix(cp, "/community/")
	default:
		return true
	}
}

func (e *ScoutEngine) finalizeRelationCanonicalPath(ctx context.Context, rel agents.DiscoveredRelation, candidate string, relType models.FragmentType) string {
	if e == nil {
		return strings.TrimSpace(candidate)
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || e.canonicalPathResolver == nil || relType == "" {
		return candidate
	}
	if relType == models.FragmentTypeOrganization {
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
  - Do NOT include legal form (LLC, Inc, Ltd, GmbH, AG, etc.) in legalName or shortName — legal form belongs ONLY in canonicalPath as -{legal_form} suffix.
  - legalName: official name without country suffix and without legal form.
  - shortName: base/trade name without country and without legal form.
- %s
- canonicalPath slug MUST be derived from shortName (without ownership suffix) to avoid duplicates when legal form changes.
- If a legal form is known, append it as -{legal_form} to satisfy org canonical format.
- Intangible-but-real actors (alliances, coalitions, unions, regulators, ministries, agencies, associations) are ORGANIZATION.
- Government bodies MUST use /gov/{iso2|na}/{slug} (iso2 = jurisdiction). Other orgs use /org/{iso2|na}/{slug}-{legal_form}.
- GOV HIERARCHY: Government bodies form administrative hierarchies:
  - government → ministry → department → agency/authority/regulator
  - GovTypes: government, ministry, department, agency, authority, regulator, committee, service, bureau, office, council, commission
  - When canonicalizing, prefer specific agency names over generic ministry references
  - Example: "Saudi FDA" → /gov/sa/sfda (type: authority), parent would be /gov/sa/ministry-of-health
  - Use these namespaces:
  - ORGANIZATION:
    - /org/{iso2|na}/{lowercase-slug}-{legal_form} (companies, NGOs, etc.)
    - /gov/{iso2|na}/{lowercase-slug} (ministries, regulators, agencies, state bodies)
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
      - Rule: child path = parent path + exactly one segment
      - NEVER repeat a segment already in the path (e.g. /market/iq/agriculture/agriculture is INVALID)
    - domain/vertical SHOULD align with /segment/{domain}/{vertical}/{geo} taxonomy used for organizations
    - Use GICS terminology (no numeric codes). If not in GICS, use the closest GICS parent + a descriptive term.
    - geo: ISO3166-1 alpha2 lowercase, composite ISO2, regional token (eu, gcc, asean, opec), or "global"/"int"
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical} (NOT /market/na/{domain}/gcc)
    - GEOGRAPHY INFERENCE: Extract geo from the market name/context:
      - If the name mentions a country/region, use that country's ISO2 code
      - Use "global" ONLY when no geographic context is present
    - MARKET HIERARCHY: Markets form broader → narrower hierarchies
      - Parent relationships are modeled as HAS_MARKET_SEGMENT edges (broader → narrower)
      - Example chain: /market/sa/finance → /market/sa/finance/banking → /market/sa/finance/banking/islamic-banking
    - Examples:
      - "SME banking in Iraq" -> /market/iq/finance/banking
      - "Iraqi agriculture" -> /market/iq/agriculture (L1)
      - "Iraqi crop production" -> /market/iq/agriculture/crop-production (L2)
      - "Iraqi rice production" -> /market/iq/agriculture/crop-production/rice-production (L3)
      - "Global EV batteries" -> /market/global/automotive/ev-batteries
      - "UAE fintech sector" -> /market/ae/finance/fintech
  - FACILITY:
    - Use /fac/{orgSlug}/{facilitySlug} (e.g., "TSMC Fab 21" -> /fac/tsmc/fab-21)
    - If org is unknown, use /fac/na/{facilitySlug}
  - GEO_REGION: MUST be /loc/{country}/{admin1}/{city}
    - country: ISO3166-1 alpha2 lowercase, or regional token (asean, brics, gcc, eu, eaeu)
    - Regional tokens ALWAYS go in the country slot (2nd segment), never at the end:
      - GCC → /loc/gcc/na/na (NOT /loc/na/na/gcc)
    - admin1: state/province/region code or slug lowercase; use "na" if not applicable/unknown
    - city: lowercase slug; use "na" if not applicable/unknown
    - For country-only locations, use: /loc/{country}/na/na
    - CRITICAL: country code MUST be valid ISO3166-1 alpha2 or an allowed regional token.
    Examples (3-position model):
    - USA / Texas / Austin: /loc/us/tx/austin
    - Germany / Bavaria / Munich: /loc/de/by/munich
    - France / Île-de-France / Paris: /loc/fr/idf/paris
    - Russia / Moscow Oblast / Moscow: /loc/ru/mos/moscow
    - Brazil / São Paulo / São Paulo: /loc/br/sp/sao-paulo
    - Kazakhstan / Astana: /loc/kz/na/astana
  - EVENT: /event/{lowercase-slug}
  - COMMUNITY: /community/{geo}/{type}/{slug}
    - geo: ISO2, regional token, or "global"/"int"
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
    - FACILITY -> /fac/ (format: /fac/{orgSlug}/{facilitySlug})
    - GEO_REGION -> /loc/ (strictly enforced)
    - PRODUCT -> /product/
    - PROGRAM -> /program/
    - MARKET -> /market/
    - EVENT -> /event/
    - PERSON -> /person/
    - COMMUNITY -> /community/ (format: /community/{geo}/{type}/{slug})
  - GEO_REGION paths MUST have exactly 4 parts: /loc/{country}/{admin1}/{city}
  - GEO_REGION country code MUST be valid ISO3166-1 alpha2 (2 lowercase letters)
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
