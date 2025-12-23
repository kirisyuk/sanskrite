package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
var multiDash = regexp.MustCompile(`-+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// preferredCanonicalPaths returns ordered canonical path candidates derived from names only.
// For facilities we emit both:
//  1. /fac/{orgSlug}/{facilitySlug} (if name appears prefixed by an org token)
//  2. /fac/{facilitySlug} (flat fallback)
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
		return []string{"/org/" + slug}
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
		out = append(out, "/fac/"+slug)
		return out
	case models.FragmentTypeProduct:
		// Candidate nested: take first word as org token, remainder as product label.
		out := make([]string, 0, 2)
		if parts := strings.Fields(short); len(parts) >= 2 {
			orgTok := strings.TrimSpace(parts[0])
			rest := strings.TrimSpace(strings.TrimPrefix(short, parts[0]))
			orgSlug := slugify(orgTok)
			prodSlug := slugify(rest)
			if orgSlug != "" && prodSlug != "" {
				out = append(out, "/product/"+orgSlug+"/"+prodSlug)
			}
		}
		out = append(out, "/product/"+slug)
		return out
	case models.FragmentTypeProgram:
		// Preferred: /program/{jurisdiction}/{slug} when jurisdiction can be inferred; fallback: /program/{slug}
		// We avoid guessing jurisdiction aggressively; LLM/inputs may provide it explicitly.
		return []string{"/program/" + slug}
	case models.FragmentTypeMarket:
		// Preferred: /market/{domain}/{slug} when domain can be inferred; fallback: /market/{slug}
		return []string{"/market/" + slug}
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
	default:
		return []string{"/org/" + slug}
	}
}

// normalizeLegacyGeoPath maps older geo namespaces to the strict /loc/{country}/{admin1}/{city} model.
// Supported legacy patterns:
// - /region/na/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
// - /region/{country}/{admin1}/{city}    -> /loc/{country}/{admin1}/{city}
// - /region/iso-3166-1-alpha-2/{country} -> /loc/{country}/na/na
// - /region/country/{country}            -> /loc/{country}/na/na
// - /region/{country}                    -> /loc/{country}/na/na (if looks like ISO code)
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
	
	// Pattern: /region/na/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
	if len(parts) == 5 && parts[1] == "na" {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[2], parts[3], parts[4])
	}
	
	// Pattern: /region/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
	if len(parts) == 4 {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[1], parts[2], parts[3])
	}
	
	// Pattern: /region/iso-3166-1-alpha-2/{country} -> /loc/{country}/na/na
	if len(parts) == 3 && parts[1] == "iso-3166-1-alpha-2" {
		country := strings.ToLower(strings.TrimSpace(parts[2]))
		if len(country) == 2 {
			return fmt.Sprintf("/loc/%s/na/na", country)
		}
	}
	
	// Pattern: /region/country/{country} -> /loc/{country}/na/na
	if len(parts) == 3 && parts[1] == "country" {
		country := strings.ToLower(strings.TrimSpace(parts[2]))
		if len(country) == 2 {
			return fmt.Sprintf("/loc/%s/na/na", country)
		}
	}
	
	// Pattern: /region/{country} -> /loc/{country}/na/na (if looks like ISO code: 2 lowercase letters)
	if len(parts) == 2 {
		country := strings.ToLower(strings.TrimSpace(parts[1]))
		// Heuristic: if it's exactly 2 lowercase letters, treat as ISO country code
		if len(country) == 2 && strings.Trim(country, "abcdefghijklmnopqrstuvwxyz") == "" {
			return fmt.Sprintf("/loc/%s/na/na", country)
		}
	}
	
	return ""
}

// normalizeFragmentType tries to map common human labels to our enum values.
func normalizeFragmentType(s string) models.FragmentType {
	switch strings.TrimSpace(strings.ToUpper(s)) {
	case string(models.FragmentTypeOrganization), "ORG":
		return models.FragmentTypeOrganization
	case string(models.FragmentTypeFacility), "FAC":
		return models.FragmentTypeFacility
	case string(models.FragmentTypeGeoRegion), "REGION", "GEO", "GEOREGION":
		return models.FragmentTypeGeoRegion
	case string(models.FragmentTypeProduct):
		return models.FragmentTypeProduct
	case string(models.FragmentTypeProgram):
		return models.FragmentTypeProgram
	case string(models.FragmentTypeMarket):
		return models.FragmentTypeMarket
	case string(models.FragmentTypeEvent):
		return models.FragmentTypeEvent
	case string(models.FragmentTypePerson), "HUMAN", "INDIVIDUAL":
		return models.FragmentTypePerson
	default:
		return ""
	}
}

// resolveCanonicalPath tries to produce a stable canonical path for a relation.
// Priority:
// 1) Use relation.TargetCanonicalPath if it is provided and not /discovered/*
// 2) Vertex AI Search lookup by name (prefers already-known canonicalPath)
// 3) LLM canonicalization (strict JSON) as a last resort
func (e *ScoutEngine) resolveCanonicalPath(ctx context.Context, rel agents.DiscoveredRelation) (string, error) {
	relType := normalizeFragmentType(rel.TargetType)

	// GEO special-case: normalize legacy /region/... into /loc/... when possible.
	if relType == models.FragmentTypeGeoRegion {
		if norm := normalizeLegacyGeoPath(rel.TargetCanonicalPath); norm != "" {
			if e.fragmentsRepo != nil {
				if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, norm); err == nil && f != nil && f.ID != "" {
					return norm, nil
				}
			}
			return norm, nil
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
				// For nested facilities, ensure the org fragment exists before preferring the nested path.
				if strings.HasPrefix(pref, "/fac/") {
					parts := strings.Split(strings.Trim(pref, "/"), "/")
					// fac/{org}/{fac}
					if len(parts) == 3 {
						orgCP := "/org/" + parts[1]
						if fOrg, err := e.fragmentsRepo.GetByCanonicalPath(ctx, orgCP); err != nil || fOrg == nil || fOrg.ID == "" {
							continue
						}
					}
				}
				if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
					return pref, nil
				}
			}
		}
		return strings.TrimSpace(rel.TargetCanonicalPath), nil
	}

	// 1.5) Hard preference: if a preferred canonical fragment already exists, use it immediately.
	if e.fragmentsRepo != nil {
		for _, pref := range preferredCanonicalPaths(rel) {
			if strings.TrimSpace(pref) == "" {
				continue
			}
			// For nested facilities, ensure the org fragment exists before preferring the nested path.
			if strings.HasPrefix(pref, "/fac/") {
				parts := strings.Split(strings.Trim(pref, "/"), "/")
				if len(parts) == 3 {
					orgCP := "/org/" + parts[1]
					if fOrg, err := e.fragmentsRepo.GetByCanonicalPath(ctx, orgCP); err != nil || fOrg == nil || fOrg.ID == "" {
						continue
					}
				}
			}
			if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
				return pref, nil
			}
		}
	}

	// 2) Try Vertex AI Search over already indexed fragments
	if e.planner != nil && e.planner.vertexSearchClient != nil && e.planner.vertexSearchClient.IsConfigured() {
		vertex := horizonVertexClient(e.planner.vertexSearchClient)
		if vertex == nil || !vertex.IsConfigured() {
			goto LLM
		}
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
		}
		resp, err := vertex.Search(ctx, req)
		if err == nil && resp != nil {
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
						return pref, nil
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
								return cp, nil
							}
						}
					}
				}
			}
			// Pick the first result with a usable canonicalPath
			for _, r := range resp.Results {
				if r.Document.StructData == nil {
					continue
				}
				if cp, ok := r.Document.StructData["canonicalPath"].(string); ok && cp != "" && !strings.HasPrefix(cp, "/discovered/") {
					return cp, nil
				}
			}
		}
	}

	// 3) LLM canonicalization
LLM:
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
		}
		if expectedPrefix != "" && !strings.HasPrefix(out.CanonicalPath, expectedPrefix) {
			return "", fmt.Errorf("canonicalization returned canonicalPath %q that does not match targetType %q (expected prefix %q)",
				out.CanonicalPath, string(relType), expectedPrefix)
		}
	}

	// GEO strictness: require /loc/{country}/{admin1}/{city}
	if relType == models.FragmentTypeGeoRegion {
		cp := out.CanonicalPath
		if !strings.HasPrefix(cp, "/loc/") {
			return "", fmt.Errorf("geo canonicalization must return /loc/{country}/{admin1}/{city}, got: %s", cp)
		}
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) != 4 {
			return "", fmt.Errorf("geo canonicalization must have 3 components after /loc, got: %s", cp)
		}
	}

	// Final hard preference: if preferred canonicalPath exists, use it (even if LLM suggested a suffix variant).
	if e.fragmentsRepo != nil {
		for _, pref := range preferredCanonicalPaths(rel) {
			if strings.TrimSpace(pref) == "" {
				continue
			}
			// For nested facilities, ensure the org fragment exists before preferring the nested path.
			if strings.HasPrefix(pref, "/fac/") {
				parts := strings.Split(strings.Trim(pref, "/"), "/")
				if len(parts) == 3 {
					orgCP := "/org/" + parts[1]
					if fOrg, err := e.fragmentsRepo.GetByCanonicalPath(ctx, orgCP); err != nil || fOrg == nil || fOrg.ID == "" {
						continue
					}
				}
			}
			if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
				return pref, nil
			}
		}
	}
	return out.CanonicalPath, nil
}

func buildCanonicalizationPrompt(rel agents.DiscoveredRelation) string {
	// Strict schema (embedded) as requested.
	return fmt.Sprintf(`You are a canonicalization agent for Horizon.

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
- legalName MUST include the form of ownership when relevant (Inc., Ltd., LLC, GmbH, AG, S.A., PLC...).
- shortName MUST be the base name without ownership suffix.
- canonicalPath slug MUST be derived from shortName (without ownership suffix) to avoid duplicates when legal form changes.
- ONLY include ownership suffix in canonicalPath if (and only if) it is REQUIRED to disambiguate entities with same shortName.
- Intangible-but-real actors (alliances, coalitions, unions, regulators, ministries, associations) are ORGANIZATION and MUST use /org/{slug}.
- Use these namespaces:
  - ORGANIZATION: /org/{lowercase-slug}
  - PERSON: /person/{lowercase-slug}
  - PRODUCT:
    - Prefer /product/{orgSlug}/{productSlug} when org can be inferred from name (e.g., "Google Gemini" -> /product/google/gemini)
    - Otherwise use /product/{productSlug}
  - PROGRAM:
    - Prefer /program/{jurisdictionSlug}/{programSlug} when jurisdiction is known (e.g., "Kazakhstan Alem.ai" -> /program/kazakhstan/alem-ai)
    - Otherwise use /program/{programSlug}
  - MARKET:
    - Prefer /market/{domainSlug}/{marketSlug} when domain is known (e.g., "Energy secondary liquidity" -> /market/energy/secondary-liquidity)
    - Otherwise use /market/{marketSlug}
  - FACILITY:
    - Prefer /fac/{orgSlug}/{facilitySlug} when the org can be inferred from the facility name (e.g., "TSMC Fab 21" -> /fac/tsmc/fab-21)
    - Otherwise use /fac/{facilitySlug}
  - GEO_REGION: MUST be /loc/{country}/{admin1}/{city}
    - country: ISO3166-1 alpha2 lowercase (examples: us, de, ru, fr, br, cn)
    - admin1: state/province/region code or slug lowercase; use "na" if not applicable
    - city: lowercase slug
    - For country-only locations, use: /loc/{country}/na/na
    Examples (3-position model):
    - USA / Texas / Austin: /loc/us/tx/austin
    - Germany / Bavaria / Munich: /loc/de/by/munich
    - France / Île-de-France / Paris: /loc/fr/idf/paris
    - Russia / Moscow Oblast / Moscow: /loc/ru/mos/moscow
    - Brazil / São Paulo / São Paulo: /loc/br/sp/sao-paulo
  - EVENT: /event/{lowercase-slug}
- Output only JSON, no markdown, no extra text.
`, rel.TargetName, rel.TargetLegalName, rel.TargetShortName, rel.TargetType)
}
