package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

// IntentDecomposerAgent proposes a reusable, taxonomy-aligned set of candidate relations
// for a given (entity + intent focus + spheres) even when the local document corpus is empty.
//
// Output is intentionally hypothesis-like: low confidence, designed to seed exploration and ontology facts.
type IntentDecomposerAgent struct {
	executor *ai.AITaskExecutor
}

func NewIntentDecomposerAgent(executor *ai.AITaskExecutor) *IntentDecomposerAgent {
	return &IntentDecomposerAgent{executor: executor}
}

type intentDecompositionResult struct {
	Relations []DiscoveredRelation `json:"relations"`
}

func (a *IntentDecomposerAgent) Decompose(
	ctx context.Context,
	targetCanonicalPath string,
	focus string,
	spherePaths []string,
	keywords []string,
) ([]DiscoveredRelation, error) {
	targetCanonicalPath = strings.TrimSpace(targetCanonicalPath)
	if targetCanonicalPath == "" {
		return nil, fmt.Errorf("targetCanonicalPath is required")
	}

	prompt := a.buildPrompt(targetCanonicalPath, focus, spherePaths, keywords)
	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true, // ok to use grounded search when available
			MinCapabilityScore:        7,
			MinContextWindow:          16000,
			MaxOutputSize:             8192,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("intent decomposition failed: %w", err)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var out intentDecompositionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("failed to parse intent decomposition result: %w", err)
	}
	// Best-effort cleanup: drop empty rows, clamp confidence to low envelope.
	clean := make([]DiscoveredRelation, 0, len(out.Relations))
	for _, r := range out.Relations {
		r.TargetName = strings.TrimSpace(r.TargetName)
		r.TargetCanonicalPath = strings.TrimSpace(r.TargetCanonicalPath)
		r.TargetType = strings.TrimSpace(r.TargetType)
		r.RelationType = strings.TrimSpace(r.RelationType)
		r.Reason = strings.TrimSpace(r.Reason)
		if r.TargetName == "" || r.TargetCanonicalPath == "" || r.RelationType == "" || r.TargetType == "" {
			continue
		}
		if r.Confidence <= 0 || r.Confidence > 0.65 {
			// Force hypothesis envelope: we are seeding, not asserting.
			r.Confidence = 0.35
		}
		clean = append(clean, r)
		if len(clean) >= 12 {
			break
		}
	}
	return clean, nil
}

func (a *IntentDecomposerAgent) buildPrompt(targetCanonicalPath string, focus string, spherePaths []string, keywords []string) string {
	focus = strings.TrimSpace(focus)
	spheresLine := strings.Join(dedupNonEmptyStrings(spherePaths), ", ")
	keywordsLine := strings.Join(dedupNonEmptyStrings(keywords), ", ")

	ctxBlock := ""
	if focus != "" || spheresLine != "" || keywordsLine != "" {
		ctxBlock = "INTENT CONTEXT (steer towards reusable supply-chain/logistics structure; do NOT invent citations):\n"
		if focus != "" {
			ctxBlock += fmt.Sprintf("- focus: %s\n", focus)
		}
		if spheresLine != "" {
			ctxBlock += fmt.Sprintf("- spherePaths: %s\n", spheresLine)
		}
		if keywordsLine != "" {
			ctxBlock += fmt.Sprintf("- keywords: %s\n", keywordsLine)
		}
		ctxBlock += "\n"
	}

	return fmt.Sprintf(`You are an intent decomposition agent for Horizon.

GOAL:
Given a TARGET entity and intent context, propose a SMALL, reusable set of candidate relations (hypotheses) that describe the relevant structure.
This is used to seed exploration when the local corpus is empty. Be conservative: output only high-likelihood nodes in the structure.

TARGET:
- canonicalPath: %s

%s
RELATION DIRECTION RULES (STRICT):
Each relation is a directed edge FROM TARGET (source) TO targetCanonicalPath (target).
Choose relationType to match this direction:
- DEPENDS_ON: source depends on target (use for suppliers, contractors, inputs, infrastructure dependencies).
- SUPPLIES_TO: source supplies goods/services to target (use for customers/recipients).
- LOCATED_IN / OPERATES_IN: source located/operates in GEO target.
- OWNS: source owns target.
- PRODUCES: source produces target (target is a product).
- REGULATES: source regulates target (source is typically a government/agency org).
- COMPETES_WITH / INFLUENCES: as labeled.

OUTPUT:
Return ONLY JSON with this schema:
{
  "relations": [
    {
      "targetName": "string",
      "targetLegalName": "string",
      "targetShortName": "string",
      "targetType": "ORGANIZATION" | "FACILITY" | "GEO_REGION" | "PRODUCT" | "PROGRAM" | "MARKET" | "EVENT" | "PERSON",
      "relationType": "SUPPLIES_TO" | "LOCATED_IN" | "OWNS" | "PRODUCES" | "OPERATES_IN" | "REGULATES" | "COMPETES_WITH" | "INFLUENCES" | "DEPENDS_ON" | "SUBSIDIARY_OF",
      "reason": "string",
      "confidence": 0.0,
      "targetCanonicalPath": "string (REQUIRED, stable, NOT /discovered/*)"
    }
  ]
}

RULES:
- Output 5..12 relations max.
- Prefer supply-chain/logistics structure when the intent suggests it (suppliers, battery materials/components, logistics providers, ports/regions, manufacturing facilities, key dependencies).
- confidence MUST be <= 0.65 (hypothesis), use ~0.25..0.55 typically.
- Use stable canonical paths:
  - Organizations: /org/{slug}
  - Facilities: /fac/{orgSlug}/{slug} when possible
  - Products: /product/{orgSlug}/{slug} when org known
  - Geo regions/locations: /region/... or /loc/... (country-level ok)
- NEVER use "/discovered/*".
- Return ONLY valid JSON.`, targetCanonicalPath, ctxBlock)
}
