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

func preferredCanonicalPath(rel agents.DiscoveredRelation) string {
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
		return ""
	}

	slug := slugify(short)
	if slug == "" {
		return ""
	}

	switch t {
	case models.FragmentTypeOrganization:
		return "/org/" + slug
	case models.FragmentTypeFacility:
		return "/fac/" + slug
	case models.FragmentTypeProduct:
		return "/product/" + slug
	case models.FragmentTypeEvent:
		return "/event/" + slug
	case models.FragmentTypeGeoRegion:
		// For GEO, do NOT guess /loc without explicit country/admin1.
		// We'll use:
		// - normalized input paths (e.g., /region/na/us/tx/austin -> /loc/us/tx/austin)
		// - Vertex Search / existing fragments
		// - LLM canonicalization with strict /loc schema
		return ""
	case models.FragmentTypePerson:
		return "/person/" + slug
	default:
		return "/org/" + slug
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
			if pref := preferredCanonicalPath(rel); pref != "" {
				if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
					return pref, nil
				}
			}
		}
		return strings.TrimSpace(rel.TargetCanonicalPath), nil
	}

	// 1.5) Hard preference: if a preferred canonical fragment already exists, use it immediately.
	if e.fragmentsRepo != nil {
		if pref := preferredCanonicalPath(rel); pref != "" {
			if f, err := e.fragmentsRepo.GetByCanonicalPath(ctx, pref); err == nil && f != nil && f.ID != "" {
				return pref, nil
			}
		}
	}

	// 2) Try Vertex AI Search over already indexed fragments
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
			// Prefer search hit that matches our shortName-derived canonicalPath
			pref := preferredCanonicalPath(rel)
			if pref != "" {
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
			MinCapabilityScore:       8,
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
		if pref := preferredCanonicalPath(rel); pref != "" {
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
- Use these namespaces:
  - ORGANIZATION: /org/{lowercase-slug}
  - PERSON: /person/{lowercase-slug}
  - PRODUCT: /product/{lowercase-slug}
  - FACILITY: /fac/{lowercase-slug}
  - GEO_REGION: MUST be /loc/{country}/{admin1}/{city}
    - country: ISO3166-1 alpha2 lowercase (examples: us, de, ru, fr, br, cn)
    - admin1: state/province/region code or slug lowercase; use "na" if not applicable
    - city: lowercase slug
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


