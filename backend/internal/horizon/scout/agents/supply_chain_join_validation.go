package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

type SupplyChainJoinValidationPair struct {
	JoinIndex         int      `json:"joinIndex"`
	LeftLegIndex      int      `json:"leftLegIndex"`
	RightLegIndex     int      `json:"rightLegIndex"`
	LeftSupplierRef   string   `json:"leftSupplierRef,omitempty"`
	LeftProductRef    string   `json:"leftProductRef,omitempty"`
	LeftCustomerRef   string   `json:"leftCustomerRef,omitempty"`
	RightSupplierRef  string   `json:"rightSupplierRef,omitempty"`
	RightProductRef   string   `json:"rightProductRef,omitempty"`
	RightCustomerRef  string   `json:"rightCustomerRef,omitempty"`
	LeftTransportVia  []string `json:"leftTransportVia,omitempty"`
	RightTransportVia []string `json:"rightTransportVia,omitempty"`
}

type SupplyChainJoinValidationInput struct {
	ChainRef         string
	ChainReason      string
	GraphContext     string
	AllowedReuseRefs []string
	Pairs            []SupplyChainJoinValidationPair
}

type SupplyChainJoinValidationAssessment struct {
	JoinIndex        int      `json:"joinIndex"`
	LeftLegIndex     int      `json:"leftLegIndex"`
	RightLegIndex    int      `json:"rightLegIndex"`
	Status           string   `json:"status"`
	ReuseRefs        []string `json:"reuseRefs,omitempty"`
	ProductRelation  string   `json:"productRelation,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	Gaps             []string `json:"gaps,omitempty"`
	PolicyViolations []string `json:"policyViolations,omitempty"`
}

type SupplyChainJoinValidationOutput struct {
	Assessments []SupplyChainJoinValidationAssessment `json:"assessments"`
}

func (a *CriticAgent) ValidateSupplyChainJoins(ctx context.Context, input SupplyChainJoinValidationInput) (*SupplyChainJoinValidationOutput, error) {
	if a == nil || a.executor == nil || len(input.Pairs) == 0 {
		return nil, nil
	}
	prompt := buildSupplyChainJoinValidationPrompt(input)
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         12000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}
	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to validate supply chain joins: %w", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal supply chain join validation result: %w", err)
	}
	var out SupplyChainJoinValidationOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("failed to parse supply chain join validation result: %w", err)
	}
	allowed := make(map[string]struct{}, len(input.AllowedReuseRefs))
	for _, ref := range input.AllowedReuseRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			allowed[ref] = struct{}{}
		}
	}
	for i := range out.Assessments {
		out.Assessments[i].Status = normalizeJoinValidationStatus(out.Assessments[i].Status)
		out.Assessments[i].ProductRelation = normalizeJoinProductRelation(out.Assessments[i].ProductRelation)
		filtered := make([]string, 0, len(out.Assessments[i].ReuseRefs))
		for _, ref := range out.Assessments[i].ReuseRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if _, ok := allowed[ref]; !ok {
				out.Assessments[i].PolicyViolations = append(out.Assessments[i].PolicyViolations, "non_allowlisted_reuse_ref:"+ref)
				continue
			}
			filtered = append(filtered, ref)
		}
		out.Assessments[i].ReuseRefs = filtered
		out.Assessments[i].Gaps = uniqueJoinValidationStrings(out.Assessments[i].Gaps)
		out.Assessments[i].PolicyViolations = uniqueJoinValidationStrings(out.Assessments[i].PolicyViolations)
	}
	return &out, nil
}

func buildSupplyChainJoinValidationPrompt(input SupplyChainJoinValidationInput) string {
	pairsJSON, _ := json.MarshalIndent(input.Pairs, "", "  ")
	allowedRefsJSON, _ := json.MarshalIndent(input.AllowedReuseRefs, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a constrained supply-chain join validator.

Your task is to evaluate only the joins between already discovered supply legs.
Do NOT invent new canonical paths, products, facilities, or organizations.
You may reuse only refs from ALLOWED REUSE REFS. If evidence is insufficient, return "unresolved".

Allowed statuses:
- validated
- heuristic
- forensic_gap
- unresolved

Allowed productRelation values:
- same
- transforms_into
- unknown
- contradiction

Rules:
1. Evaluate each join independently using only the supplied legs and graph context.
2. Reuse refs must come only from ALLOWED REUSE REFS.
3. If a new ref would be needed to make the join work, do not invent it. Return unresolved or forensic_gap and explain the gap.
4. Prefer explicit rejection over speculation.
5. Return exactly one assessment per join pair.

CHAIN REF:
%s

CHAIN REASON:
%s

%sALLOWED REUSE REFS:
%s

JOIN PAIRS:
%s

Return ONLY valid JSON:
{
  "assessments": [
    {
      "joinIndex": 0,
      "leftLegIndex": 0,
      "rightLegIndex": 1,
      "status": "validated|heuristic|forensic_gap|unresolved",
      "reuseRefs": ["/org/...", "/product/..."],
      "productRelation": "same|transforms_into|unknown|contradiction",
      "reason": "short explanation",
      "gaps": ["specific missing evidence question"]
    }
  ]
}`, strings.TrimSpace(input.ChainRef), strings.TrimSpace(input.ChainReason), graphBlock, string(allowedRefsJSON), string(pairsJSON))
}

func normalizeJoinValidationStatus(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "validated":
		return "validated"
	case "heuristic":
		return "heuristic"
	case "forensic_gap":
		return "forensic_gap"
	case "unresolved":
		return "unresolved"
	default:
		return "unresolved"
	}
}

func normalizeJoinProductRelation(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "same":
		return "same"
	case "transforms_into":
		return "transforms_into"
	case "contradiction":
		return "contradiction"
	default:
		return "unknown"
	}
}

func uniqueJoinValidationStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
