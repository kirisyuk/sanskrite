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
		Type          string  `json:"type"` // ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET|MEDIA_CHANNEL (best-effort)
		// Name is the preferred English display name for this anchor (NOT a slug, NOT a URL).
		// If the original name is non-English and no established English name exists, transliteration is acceptable.
		Name string `json:"name,omitempty"`
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
		Name          string  `json:"name,omitempty"`
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
		an.Name = strings.TrimSpace(an.Name)
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
	quoted := extractQuotedPhrases(query)
	quotedBlock := ""
	if len(quoted) > 0 {
		quotedBlock = "\n\nQUOTED PHRASES (treat each as a likely standalone entity name):\n"
		for i, q := range quoted {
			if i >= 5 {
				break
			}
			quotedBlock += fmt.Sprintf("- %s\n", q)
		}
	}
	return fmt.Sprintf(`You are an intent resolver for a risk analysis system. You MUST convert a user query into a reusable, taxonomy-aligned intent.

USER QUERY:
%s
%s

OUTPUT JSON SCHEMA:
{
  "focus": "string (short, reusable intent statement)",
  "spherePaths": ["string (canonical /sphere/... only)"],
  "keywords": ["string (non-canonical tokens)"],
  "anchors": [
    {
      "canonicalPath": "string (canonicalPath, must match the taxonomy rules below)",
      "type": "ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET|MEDIA_CHANNEL",
      "name": "string (preferred English display name; NOT a slug; translate to English when possible)",
      "confidence": 0.0,
      "reason": "short reason"
    }
  ]
}

RULES:
- focus MUST be in English (translate from the user query when needed). Keep it short and reusable.
- keywords SHOULD be in English when possible (translate), but can include raw tokens like handles.
- spherePaths MUST be canonical under /sphere/... (if unsure, use /sphere/other).
- canonicalPath MUST be a valid canonical path with allowed namespaces:
  /org, /person, /product, /program, /market, /fac, /loc, /sphere, /event, /media
- If the query refers to a media brand/channel (e.g. a Telegram channel), do NOT collapse it to a GEO_REGION just because it contains a city name.
  - Example: Telegram channel "В казани поймут" should be anchored as a MEDIA_CHANNEL: /media/telegram/{channel-slug}, not as /loc/... for Kazan.
  - Prefer using a stable handle/username if present; otherwise use the quoted title as the slug source.
  - Add keyword "telegram" and consider /sphere/social/information.
- ALL path segments MUST be lowercase slugs (use hyphens instead of spaces):
  - CORRECT: /org/saudi-aramco, /fac/aramco/ras-tanura-refinery, /loc/ae/na/na, /product/raw/chemical/refining/lithium-carbonate
  - WRONG: /loc/UAE, /org/Tesla Inc, /product/Lithium Carbonate

- The anchor type MUST match the canonicalPath namespace:
  - ORGANIZATION -> /org/...
  - FACILITY     -> /fac/...
  - GEO_REGION   -> /loc/...
  - PERSON       -> /person/...
  - PRODUCT      -> /product/...
  - MEDIA_CHANNEL -> /media/{platform}/{slug}
  - PROGRAM      -> /program/...
  - MARKET       -> /market/...
  - EVENT        -> /event/...

- GEO_REGION canonicalPath MUST be strict: /loc/{countryAlpha2}/{admin1}/{city}
  - countryAlpha2: ISO3166-1 alpha2 lowercase (e.g., ae, sa, us)
  - use "na" for unknown admin1/city
  - country-only locations MUST be /loc/{cc}/na/na
    - Saudi Arabia -> /loc/sa/na/na
    - United Arab Emirates -> /loc/ae/na/na

- PRODUCT canonicalPath MUST be strict hierarchical taxonomy:
  - /product/{category}/{type}/{stage}/{slug}
  - category: raw|component|finished|service
  - type: mineral|agricultural|chemical|electronic|mechanical|energy (best-effort)
  - stage: extraction|refining|processing|manufacturing|assembly (best-effort)
  - If you truly cannot classify, use fallback: /product/{slug}

- FACILITY canonicalPath:
  - prefer /fac/{orgSlug}/{facilitySlug} when the owning org is known and stable
  - otherwise use /fac/{facilitySlug}

- ORGANIZATION canonicalPath:
  - /org/{slug} (do not include legal suffixes like Inc/LLC in the path if possible)

- PROGRAM/MARKET/EVENT/PERSON canonicalPath:
  - /program/{slug}, /market/{slug}, /event/{slug}, /person/{slug}

- Prefer STABLE anchors:
  - choose country canonical paths for country scope, not adjective/alias forms
  - choose products/materials at the right level (raw vs component vs finished) when possible
- Provide 1-3 anchors (anchor = what the Lens should attach to).
- Provide 1-3 spherePaths.
- Return ONLY JSON. No markdown. No commentary.`,
		query, quotedBlock)
}

// extractQuotedPhrases returns a best-effort list of quoted substrings, preserving original casing.
// Supports: "..." and «...».
func extractQuotedPhrases(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := []string{}
	seen := map[string]struct{}{}

	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	// Parse "...".
	for {
		i := strings.Index(s, `"`)
		if i < 0 {
			break
		}
		rest := s[i+1:]
		j := strings.Index(rest, `"`)
		if j < 0 {
			break
		}
		add(rest[:j])
		s = rest[j+1:]
		if len(out) >= 10 {
			return out
		}
	}

	// Parse «...».
	s2 := strings.TrimSpace(s)
	for {
		i := strings.Index(s2, "«")
		if i < 0 {
			break
		}
		rest := s2[i+len("«"):]
		j := strings.Index(rest, "»")
		if j < 0 {
			break
		}
		add(rest[:j])
		s2 = rest[j+len("»"):]
		if len(out) >= 10 {
			return out
		}
	}

	return out
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
	candidates := []string{"/org/", "/person/", "/product/", "/fac/", "/loc/", "/event/", "/sphere/", "/program/", "/market/", "/media/"}
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
