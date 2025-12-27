package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// IntentResolution is a lightweight, reusable representation of user intent.
// It is intentionally taxonomy-first: spherePaths must be canonical (/sphere/...), and anchors must be canonical paths.
type IntentResolution struct {
	Focus       string   `json:"focus"`
	SpherePaths []string `json:"spherePaths"`
	Keywords    []string `json:"keywords"`
	Anchors     []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"` // ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET (best-effort)
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	} `json:"anchors"`
}

// IntentResolverAgent extracts anchors/spheres/keywords from a natural language query.
// This is a "precheck" step: it should be cheap, deterministic, and produce reusable outputs.
type IntentResolverAgent struct {
	executor *ai.AITaskExecutor
}

func NewIntentResolverAgent(executor *ai.AITaskExecutor) *IntentResolverAgent {
	return &IntentResolverAgent{executor: executor}
}

func (a *IntentResolverAgent) Resolve(ctx context.Context, query string) (*IntentResolution, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("intent resolver not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	prompt := a.buildPrompt(query)
	verifier := ai.NewGenericVerifier(func(result IntentResolution) error {
		// Basic structural checks; deeper normalization happens after.
		if strings.TrimSpace(result.Focus) == "" {
			return fmt.Errorf("focus is required")
		}
		if len(result.Anchors) == 0 {
			return fmt.Errorf("at least 1 anchor is required")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: false,
			MinCapabilityScore:        7,
			MinContextWindow:          4000,
			MaxOutputSize:             1536,
		},
		Verifier: verifier,
	}

	raw, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("intent resolve failed: %w", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal intent result: %w", err)
	}
	var out IntentResolution
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal intent result: %w", err)
	}

	// Normalize + validate (taxonomy-first).
	out.Focus = strings.TrimSpace(out.Focus)

	// Normalize spheres (synonyms + allowlist).
	out.SpherePaths = taxonomy.NormalizeSpherePaths(out.SpherePaths)

	// Normalize keywords (trim, dedup, cap).
	seen := map[string]struct{}{}
	kw := make([]string, 0, len(out.Keywords))
	for _, k := range out.Keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		key := strings.ToLower(k)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		kw = append(kw, k)
		if len(kw) >= 12 {
			break
		}
	}
	out.Keywords = kw

	// Normalize anchors: canonicalize formatting and drop invalid paths.
	nAnchors := make([]struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}, 0, len(out.Anchors))
	for _, an := range out.Anchors {
		originalCP := strings.TrimSpace(an.CanonicalPath)
		cp := originalCP
		// Fix MSYS conversion if any.
		cp = normalizeCanonicalPathForTools(cp)
		// Best-effort legacy geo normalization: older flows may still emit /region/... paths.
		// We normalize only supported legacy shapes into strict /loc/{country}/{admin1}/{city}.
		if strings.HasPrefix(cp, "/region/") {
			if norm := normalizeLegacyGeoPathForIntent(cp); norm != "" {
				cp = norm
			}
		}
		// Slugify each segment of the path to handle LLM outputs with spaces/uppercase.
		// Example: "/loc/Saudi Arabia" -> "/loc/saudi-arabia"
		cp = slugifyCanonicalPath(cp)
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
			// Log rejected anchors for debugging
			fmt.Printf("[IntentResolver] Anchor rejected: original=%q, normalized=%q, error=%v\n", originalCP, cp, err)
			continue
		}
		an.CanonicalPath = cp
		if an.Confidence <= 0 || an.Confidence > 1 {
			an.Confidence = 0.6
		}
		an.Reason = strings.TrimSpace(an.Reason)
		an.Type = strings.TrimSpace(an.Type)
		nAnchors = append(nAnchors, an)
		if len(nAnchors) >= 5 {
			break
		}
	}
	out.Anchors = nAnchors
	if len(out.Anchors) == 0 {
		return nil, fmt.Errorf("no valid anchors after canonical validation")
	}
	if out.Focus == "" {
		out.Focus = query
	}

	return &out, nil
}

func (a *IntentResolverAgent) buildPrompt(query string) string {
	return fmt.Sprintf(`You are an intent resolver for Horizon. You MUST convert a user query into a reusable, taxonomy-aligned intent.

USER QUERY:
%s

OUTPUT JSON SCHEMA:
{
  "focus": "string (short, reusable intent statement)",
  "spherePaths": ["string (canonical /sphere/... only)"],
  "keywords": ["string (non-canonical tokens)"],
  "anchors": [
    {
      "canonicalPath": "string (canonical path like /org/tesla, /loc/saudi-arabia)",
      "type": "ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET",
      "confidence": 0.0,
      "reason": "short reason"
    }
  ]
}

RULES:
- spherePaths MUST be canonical under /sphere/... (if unsure, use /sphere/other).
- canonicalPath MUST be a valid Horizon canonical path with allowed namespaces:
  /org, /person, /product, /program, /market, /fac, /loc, /sphere, /event
- ALL path segments MUST be lowercase slugs (use hyphens instead of spaces):
  - CORRECT: /loc/saudi-arabia, /loc/uae, /org/saudi-aramco
  - WRONG: /loc/Saudi Arabia, /loc/UAE (uppercase/spaces not allowed)
- For countries/regions use /loc/{country-slug}:
  - /loc/saudi-arabia (not /loc/Saudi Arabia)
  - /loc/uae (not /loc/UAE or /loc/United Arab Emirates)
  - /loc/usa, /loc/china, /loc/germany, etc.
- Prefer STABLE anchors:
  - companies: /org/{slug} (e.g., /org/saudi-aramco, /org/adnoc)
  - products: /product/{orgSlug}/{productSlug} when org is known
  - facilities: /fac/{orgSlug}/{facilitySlug} when org is known
  - countries/regions: /loc/{country-slug} (e.g., /loc/saudi-arabia, /loc/uae)
- Provide 1-3 anchors (anchor = what the Lens should attach to).
- Provide 1-3 spherePaths.
- Return ONLY JSON. No markdown. No commentary.`,
		query)
}

// slugifyCanonicalPath normalizes each segment of a canonical path to handle LLM outputs
// with spaces, uppercase, or special characters.
// Example: "/loc/Saudi Arabia" -> "/loc/saudi-arabia"
// Example: "/org/Tesla, Inc." -> "/org/tesla-inc"
func slugifyCanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	// Keep namespace as-is (it should already be lowercase), slugify rest
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i == 0 {
			// Namespace: just lowercase, no other changes
			out = append(out, strings.ToLower(p))
		} else {
			// Slugify: lowercase, replace non-alnum with hyphens
			out = append(out, taxonomy.Slugify(p))
		}
	}
	// Filter out empty segments
	filtered := make([]string, 0, len(out))
	for _, p := range out {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return path
	}
	return "/" + strings.Join(filtered, "/")
}

// normalizeCanonicalPathForTools fixes MSYS/Git-Bash path conversion for canonical paths.
// Example: "/org/tesla" -> "C:/Program Files/Git/org/tesla" (we strip prefix).
func normalizeCanonicalPathForTools(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	candidates := []string{"/org/", "/person/", "/product/", "/fac/", "/loc/", "/event/", "/sphere/", "/program/", "/market/"}
	for _, c := range candidates {
		if idx := strings.Index(s, c); idx > 0 {
			s = s[idx:]
			break
		}
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// normalizeLegacyGeoPathForIntent maps older geo namespaces to the strict /loc/{country}/{admin1}/{city} model.
// Supported legacy patterns:
// - /region/na/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
// - /region/{country}/{admin1}/{city}    -> /loc/{country}/{admin1}/{city}
func normalizeLegacyGeoPathForIntent(path string) string {
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
