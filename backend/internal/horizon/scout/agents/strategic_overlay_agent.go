package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type StrategicOverlayResult struct {
	DecisionPoints []StrategicDecisionPoint `json:"decisionPoints"`
	Mechanisms     []StrategicMechanism     `json:"mechanisms"`
	Edges          []StrategicEdge          `json:"edges"`
}

type StrategicDecisionPoint struct {
	CanonicalPath string `json:"canonicalPath"`
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
}

type StrategicMechanism struct {
	CanonicalPath string `json:"canonicalPath"`
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
}

type StrategicEdge struct {
	SourceCanonicalPath string                 `json:"sourceCanonicalPath"`
	SourceType          string                 `json:"sourceType,omitempty"`
	TargetCanonicalPath string                 `json:"targetCanonicalPath"`
	TargetType          string                 `json:"targetType,omitempty"`
	RelationType        string                 `json:"relationType"`
	Reason              string                 `json:"reason,omitempty"`
	Confidence          float64                `json:"confidence,omitempty"`
	Qualifiers          map[string]interface{} `json:"qualifiers,omitempty"`
}

// StrategicOverlayAgent extracts decision points and strategic edges from an existing graph context.
type StrategicOverlayAgent struct {
	executor *ai.AITaskExecutor
}

func NewStrategicOverlayAgent(executor *ai.AITaskExecutor) *StrategicOverlayAgent {
	return &StrategicOverlayAgent{executor: executor}
}

func (a *StrategicOverlayAgent) Extract(ctx context.Context, domain *models.HorizonFragment, graphContext string, focus string, keywords []string) (*StrategicOverlayResult, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("strategic overlay agent not configured")
	}
	if domain == nil || strings.TrimSpace(domain.CanonicalPath) == "" {
		return nil, fmt.Errorf("domain is required")
	}
	focus = strings.TrimSpace(focus)
	keywords = dedupStrings(keywords, 12)
	kwLine := strings.Join(keywords, ", ")

	domainTitle := strings.TrimSpace(domain.Labels["en"])
	if domainTitle == "" {
		domainTitle = strings.TrimSpace(domain.CanonicalPath)
	}
	domainDesc := strings.TrimSpace(domain.Description)
	if domainDesc == "" {
		domainDesc = "Domain contract for strategic overlay."
	}

	prompt := buildStrategicOverlayPrompt(domain.CanonicalPath, domainTitle, domainDesc, graphContext, focus, kwLine)
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			MinCapabilityScore:        7,
			MinContextWindow:          16000,
			MaxOutputSize:             12000,
		},
		Verifier: verifier,
	}
	raw, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("strategic overlay extraction failed: %w", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal overlay result: %w", err)
	}
	var out StrategicOverlayResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal overlay result: %w", err)
	}
	return &out, nil
}

func buildStrategicOverlayPrompt(domainPath, domainTitle, domainDesc, graphContext, focus, keywords string) string {
	focusBlock := ""
	if focus != "" || keywords != "" {
		focusBlock = "FOCUS CONTEXT:\n"
		if focus != "" {
			focusBlock += fmt.Sprintf("- focus: %s\n", focus)
		}
		if keywords != "" {
			focusBlock += fmt.Sprintf("- keywords: %s\n", keywords)
		}
		focusBlock += "\n"
	}
	graphBlock := ""
	if strings.TrimSpace(graphContext) != "" {
		graphBlock = "GRAPH CONTEXT (existing Lens graph):\n" + strings.TrimSpace(graphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are extracting STRATEGIC DECISION OVERLAYS for an existing graph.
Your task: identify WHO DECIDES WHAT in this graph, HOW they decide, and WHAT they can block/enable.

DOMAIN:
- canonicalPath: %s
- title: %s
- description: %s

%s%sOUTPUT JSON SCHEMA:
{
  "decisionPoints": [
    {
      "canonicalPath": "/decision/{domain-slug}/{decision-slug}",
      "title": "short title (English, Title Case)",
      "description": "what this decision controls"
    }
  ],
  "mechanisms": [
    {
      "canonicalPath": "/mechanism/{category}/{slug}",
      "title": "short title (English, Title Case)",
      "description": "how the mechanism works"
    }
  ],
  "edges": [
    {
      "sourceCanonicalPath": "/decision/...",
      "sourceType": "DECISION_POINT",
      "targetCanonicalPath": "/org/..",
      "targetType": "ORGANIZATION",
      "relationType": "CONTROLS_ACCESS",
      "reason": "evidence-based reason",
      "confidence": 0.85,
      "qualifiers": { "type": "block", "mechanism": "/mechanism/...", "condition": "..." }
    }
  ]
}

DECISION POINT RULES:
- canonicalPath MUST be: /decision/{domain-slug}/{decision-slug}
  - domain-slug: extract from the domain path (e.g., "ai-chips" from /domain/v1/power-dynamics/sector/ai-chips)
  - decision-slug: lowercase kebab-case describing the decision (e.g., "euv-export-to-china")
- title MUST be specific: "EUV Lithography Export to China" not "Export Control"
- description MUST answer: "What can be blocked/enabled/delayed here?"
- Examples:
  - /decision/ai-chips/euv-export-to-china
  - /decision/ai-chips/advanced-node-fab-licensing
  - /decision/bilateral-relations/oil-transit-agreement

MECHANISM RULES:
- canonicalPath MUST be: /mechanism/{category}/{slug}
- category MUST be one of:
  - regulatory: export control, licensing, certification, sanctions
  - economic: pricing power, capacity allocation, subsidies
  - technological: IP lock-in, standards control, patent portfolio
  - contractual: exclusivity, long-term agreements, joint ventures
  - political: diplomatic pressure, trade negotiations, alliances
- slug: lowercase kebab-case (e.g., "export-control", "entity-list")
- Mechanisms are REUSABLE across domains. Same mechanism can be used by multiple agents.
- Examples:
  - /mechanism/regulatory/export-control
  - /mechanism/regulatory/entity-list
  - /mechanism/economic/capacity-allocation
  - /mechanism/technological/ip-lock-in
  - /mechanism/contractual/exclusivity-agreement
  - /mechanism/political/diplomatic-pressure

AGENT ROLES (for context, not in output):
- gatekeeper: Can block/enable access (export control authority)
- approver: Must approve before action (regulatory body)
- influencer: Can affect but not block (lobbying group, standards body)
- enforcer: Ensures compliance (audit agency, inspector)
- arbiter: Resolves disputes (court, arbitration panel)

STRATEGIC EDGE RULES:

1. CONTROLS_ACCESS (gatekeeping):
   - Source: DECISION_POINT, ORGANIZATION, or GOV
   - Target: ORGANIZATION, FACILITY, PRODUCT, MARKET, or PROGRAM
   - Qualifiers (REQUIRED):
     - type: "block" | "enable" | "delay" | "monitor"
     - mechanism: /mechanism/... canonical path (if known)
     - condition: what triggers the control (optional)
   - Example: US BIS --[CONTROLS_ACCESS]--> ASML-SMIC supply
     {"type": "block", "mechanism": "/mechanism/regulatory/export-control", "condition": "advanced EUV equipment"}

2. IMPOSES_CONDITION (soft power):
   - Source: ORGANIZATION, GOV, or DECISION_POINT
   - Target: ORGANIZATION, FACILITY, or PRODUCT
   - Qualifiers (REQUIRED):
     - type: "cost" | "compliance" | "technical"
     - impact: description of impact
   - Example: EU --[IMPOSES_CONDITION]--> TSMC
     {"type": "compliance", "impact": "ESG reporting requirements for EU market access"}

3. USES_MECHANISM (tool possession):
   - Source: DECISION_POINT, ORGANIZATION, or GOV
   - Target: MECHANISM
   - Indicates which tools an agent possesses to exercise control.
   - Example: /gov/us/bis --[USES_MECHANISM]--> /mechanism/regulatory/entity-list

4. APPLIES_TO (mechanism scope):
   - Source: MECHANISM
   - Target: ORGANIZATION, FACILITY, PRODUCT, or MARKET
   - Shows where a mechanism has effect.
   - Example: /mechanism/regulatory/export-control --[APPLIES_TO]--> /product/.../euv-lithography

CANONICAL PATH REMINDERS:
- ALL paths must be lowercase with hyphens (not spaces or underscores)
- Organizations: /org/{iso2}/{slug} or /gov/{iso2}/{slug}
- Facilities: /fac/{org-slug}/{facility-slug}
- Products: /product/{category}/{type}/{stage}/{slug}
- Reuse existing paths from GRAPH CONTEXT when possible!

CONFIDENCE GUIDELINES:
- 0.9+: Direct regulatory action with public documentation
- 0.7-0.9: Clear strategic control with evidence
- 0.5-0.7: Inferred control based on market position
- <0.5: Speculative, should have strong reason

OUTPUT RULES:
- Output ONLY valid JSON. No markdown, no comments.
- Each decision point MUST have at least one edge connecting it to the graph.
- Prefer fewer, high-quality decision points over many vague ones.
- Focus on ACTIONABLE control: what can actually be blocked/enabled/delayed?
`, domainPath, domainTitle, domainDesc, focusBlock, graphBlock)
}
