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
	Focus      string   `json:"focus"`
	SpherePaths []string `json:"spherePaths"`
	Keywords   []string `json:"keywords"`
	Anchors    []struct {
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
		cp := strings.TrimSpace(an.CanonicalPath)
		// Fix MSYS conversion if any.
		cp = normalizeCanonicalPathForTools(cp)
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
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
      "canonicalPath": "string (canonical path like /org/tesla, /product/tesla/4680-cell)",
      "type": "ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET",
      "confidence": 0.0,
      "reason": "short reason"
    }
  ]
}

RULES:
- spherePaths MUST be canonical under /sphere/... (if unsure, use /sphere/other).
- canonicalPath MUST be a valid Horizon canonical path with allowed namespaces:
  /org, /person, /product, /program, /market, /fac, /loc, /region, /sphere, /event
- Prefer STABLE anchors:
  - companies: /org/{slug}
  - products: /product/{orgSlug}/{productSlug} when org is known
  - facilities: /fac/{orgSlug}/{facilitySlug} when org is known
- Provide 1-3 anchors максимум (anchor = what the Lens should attach to).
- Provide 1-3 spherePaths максимум.
- Return ONLY JSON. No markdown. No commentary.`,
		query)
}

// normalizeCanonicalPathForTools fixes MSYS/Git-Bash path conversion for canonical paths.
// Example: "/org/tesla" -> "C:/Program Files/Git/org/tesla" (we strip prefix).
func normalizeCanonicalPathForTools(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	candidates := []string{"/org/", "/person/", "/product/", "/fac/", "/loc/", "/region/", "/event/", "/sphere/", "/program/", "/market/"}
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


