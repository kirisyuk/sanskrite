package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
	result, _, err := a.ExtractWithGrounding(ctx, domain, graphContext, focus, keywords)
	return result, err
}

// ExtractWithGrounding returns extracted overlay plus grounding evidence.
func (a *StrategicOverlayAgent) ExtractWithGrounding(ctx context.Context, domain *models.HorizonFragment, graphContext string, focus string, keywords []string) (*StrategicOverlayResult, *ai.GroundingEvidence, error) {
	if a == nil || a.executor == nil {
		return nil, nil, fmt.Errorf("strategic overlay agent not configured")
	}
	if domain == nil || strings.TrimSpace(domain.CanonicalPath) == "" {
		return nil, nil, fmt.Errorf("domain is required")
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
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                16000,
			MaxOutputSize:                   12000,
		},
		Verifier: verifier,
	}
	raw, fullResult, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, nil, fmt.Errorf("strategic overlay extraction failed: %w", err)
	}
	var groundingEvidence *ai.GroundingEvidence
	if fullResult != nil {
		groundingEvidence = fullResult.GroundingEvidence
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, groundingEvidence, fmt.Errorf("marshal overlay result: %w", err)
	}
	var out StrategicOverlayResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, groundingEvidence, fmt.Errorf("unmarshal overlay result: %w", err)
	}
	return &out, groundingEvidence, nil
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
Your task: identify WHO DECIDES WHAT in this graph, HOW they decide, and — CRITICAL — WHAT/WHO is AFFECTED (downstream impact).

PURPOSE: Understand decision mechanics in the lens context: who decides, who influences, and ONE STEP FURTHER — what entities (org/facility/product/market) are affected by each decision and mechanism.
Do NOT create abstract decision points or mechanisms. Only create when you can name CONCRETE affected targets from the GRAPH CONTEXT.

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

═══════════════════════════════════════════════════════════════════════════════
CORE SEMANTIC DISTINCTION: DECISION POINT vs MECHANISM
═══════════════════════════════════════════════════════════════════════════════

DECISION POINT = A CONCRETE DECISION/ACTION (verb-based)
  - Answers: "What decision is being made?"
  - Title is an ACTION (verb phrase): "Grant License", "Approve Export", "Block Access"
  - Represents a MOMENT of choice where something can be blocked/enabled/delayed
  - Scoped to a domain context (domain-slug in path)
  - Examples of GOOD titles:
    ✓ "Grant Banking License" (action: grant)
    ✓ "Approve EUV Export to China" (action: approve)
    ✓ "Block Maritime Transit" (action: block)
    ✓ "Authorize Capital Market Operations" (action: authorize)
  - Examples of BAD titles (too abstract, not actionable):
    ✗ "Export Control" (noun, not action)
    ✗ "Banking Oversight" (noun, not action)
    ✗ "Regulatory Supervision" (noun, not action)
    ✗ "Compliance" (noun, not action)

MECHANISM = A SPECIFIC INSTRUMENT/REGIME OF CONTROL (noun-based)
  - Answers: "What tool/regime is used to exercise control?"
  - Title is an INSTRUMENT (noun phrase): "Export Control Regime", "Entity List", "Banking License"
  - Represents a REUSABLE tool that agents possess and apply
  - NOT scoped to domain (mechanisms are shared across domains)
  - MUST specify the object ("license WHAT?", "controls WHAT?")
  - Examples of GOOD titles:
    ✓ "EUV Export Control" (specific: export control for EUV)
    ✓ "Entity List" (specific sanction mechanism)
    ✓ "Banking License" (specific: license for banking)
    ✓ "Mining License" (specific: license for mining operations)
    ✓ "Pharmaceutical Approval" (specific: approval for drugs)
  - Examples of BAD titles (too abstract):
    ✗ "Licensing" (license what?)
    ✗ "Regulation" (regulate what?)
    ✗ "Oversight" (oversight of what?)
    ✗ "Compliance" (compliance with what?)

KEY DIFFERENCE:
  - DECISION POINT: "The act of granting a license" (verb: grant) — e.g., grant mining license, grant banking license
  - MECHANISM: "The license regime itself" (noun) — e.g., mining-license, banking-license
  - One agent GATEKEEPS the decision, another agent USES the mechanism to enforce it.

═══════════════════════════════════════════════════════════════════════════════
DECISION POINT PATH & FORMAT RULES
═══════════════════════════════════════════════════════════════════════════════

- canonicalPath: /decision/{domain-slug}/{decision-slug}
  - domain-slug: extract from the domain contract (e.g., "ai-chips" from /domain/v1/power-dynamics/sector/ai-chips)
  - If the domain facet is geo (e.g., /domain/.../geo/ye), do NOT use the geo slug as domain-slug.
    Instead use the contract/topic slug (e.g., "geopolitical-risk") and include geo in decision-slug if needed.
  - decision-slug: lowercase kebab-case describing the decision action (e.g., "approve-euv-export-to-china")
- title: MUST start with a verb (Grant, Approve, Block, Authorize, Deny, Permit, Revoke, Suspend)
- description: MUST answer "What can be blocked/enabled/delayed here?"
- Examples:
  - /decision/ai-chips/approve-euv-export-to-china → "Approve EUV Export to China"
  - /decision/ai-chips/grant-advanced-fab-license → "Grant Advanced Fab License"
  - /decision/geopolitical-risk/authorize-ye-port-transit → "Authorize Yemen Port Transit"

═══════════════════════════════════════════════════════════════════════════════
MECHANISM PATH & FORMAT RULES
═══════════════════════════════════════════════════════════════════════════════

- canonicalPath: /mechanism/{category}/{slug}
- category MUST be one of:
  - regulatory: export control, licensing, certification, sanctions
  - economic: pricing power, capacity allocation, subsidies
  - technological: IP lock-in, standards control, patent portfolio
  - contractual: exclusivity, long-term agreements, joint ventures
  - political: diplomatic pressure, trade negotiations, alliances
- slug: lowercase kebab-case, MUST include the object (e.g., %s — NOT "licensing")
- VALIDATION: If slug is generic (licensing, regulation, oversight, supervision, compliance, monitoring, enforcement), REJECT and rephrase with object.
- Mechanisms are REUSABLE across domains. Same mechanism can be used by multiple agents.
- Examples:
  - /mechanism/regulatory/euv-export-control → "EUV Export Control Regime"
  - /mechanism/regulatory/entity-list → "Entity List"
  - /mechanism/regulatory/mining-license → "Mining License"
  - /mechanism/regulatory/banking-license → "Banking License"
  - /mechanism/economic/capacity-allocation → "Capacity Allocation"
  - /mechanism/contractual/exclusivity-agreement → "Exclusivity Agreement"

AGENT ROLES (for context, not in output):
- gatekeeper: Can block/enable access (export control authority)
- approver: Must approve before action (regulatory body)
- influencer: Can affect but not block (lobbying group, standards body)
- enforcer: Ensures compliance (audit agency, inspector)
- arbiter: Resolves disputes (court, arbitration panel)

STRATEGIC EDGE RULES:

1. CONTROLS_ACCESS (gatekeeping):
   - Source: DECISION_POINT, ORGANIZATION, GOV, or COMMUNITY
   - Target: ORGANIZATION, FACILITY, PRODUCT, MARKET, or PROGRAM
   - Qualifiers (REQUIRED):
     - type: "block" | "enable" | "delay" | "monitor"
     - mechanism: /mechanism/... canonical path (if known)
     - condition: what triggers the control (optional)
   - Example: US BIS --[CONTROLS_ACCESS]--> ASML-SMIC supply
     {"type": "block", "mechanism": "/mechanism/regulatory/export-control", "condition": "advanced EUV equipment"}

2. IMPOSES_CONDITION (soft power):
   - Source: ORGANIZATION, GOV, COMMUNITY, or DECISION_POINT
   - Target: ORGANIZATION, FACILITY, or PRODUCT
   - Qualifiers (REQUIRED):
     - type: "cost" | "compliance" | "technical"
     - impact: description of impact
   - Example: EU --[IMPOSES_CONDITION]--> TSMC
     {"type": "compliance", "impact": "ESG reporting requirements for EU market access"}

3. GATEKEEPS_DECISION (strong control over a decision point):
   - Source: ORGANIZATION, GOV, or COMMUNITY
   - Target: DECISION_POINT
   - Qualifiers (REQUIRED):
     - mode: "block" | "enable" | "delay" | "monitor"
     - mechanism: /mechanism/... canonical path (if known)
     - reason: short rationale (optional)
   - Examples: /gov/sa/sama --[GATEKEEPS_DECISION]--> /decision/sa-banking/grant-banking-license; /gov/us/bis --[GATEKEEPS_DECISION]--> /decision/export-control/approve-euv-export

4. USES_MECHANISM (tool possession):
   - Source: DECISION_POINT, ORGANIZATION, GOV, or COMMUNITY
   - Target: MECHANISM (MUST be /mechanism/...)
   - Indicates which tools an agent possesses to exercise control.
   - Example: /gov/us/bis --[USES_MECHANISM]--> /mechanism/regulatory/entity-list

5. APPLIES_TO (mechanism scope):
   - Source: MECHANISM
   - Target: ORGANIZATION, FACILITY, PRODUCT, or MARKET
   - Shows where a mechanism has effect.
   - Example: /mechanism/regulatory/export-control --[APPLIES_TO]--> /product/.../euv-lithography

EDGE TYPE SAFETY:
- Never connect /org or /gov directly to /decision via USES_MECHANISM (use GATEKEEPS_DECISION instead).
- Never use USES_MECHANISM unless target is /mechanism/...

CANONICAL PATH REMINDERS:
- ALL paths must be lowercase with hyphens (not spaces or underscores)
- Organizations: /org/{iso2}/{slug} or /gov/{iso2}/{slug}
- Facilities: /fac/{geo}/{facility-slug} — geo like org/gov (ISO2, na, or regional)
- Products: /product/{category}/{type}/{stage}/{slug}
- Reuse existing paths from GRAPH CONTEXT when possible!

CONFIDENCE GUIDELINES:
- 0.9+: Direct regulatory action with public documentation
- 0.7-0.9: Clear strategic control with evidence
- 0.5-0.7: Inferred control based on market position
- <0.5: Speculative, should have strong reason

DOWNSTREAM IMPACT (MANDATORY):
- Each DECISION_POINT MUST have at least one CONTROLS_ACCESS or IMPOSES_CONDITION edge with target = concrete entity (org/facility/product/market) FROM THE GRAPH.
- Each MECHANISM MUST have at least one APPLIES_TO edge with target = concrete entity FROM THE GRAPH.
- Do NOT create a decision point or mechanism if you cannot name a specific affected entity. Fewer is better than vague.
- Targets MUST be paths that appear in GRAPH CONTEXT — do not invent entities.

OUTPUT RULES:
- Output ONLY valid JSON. No markdown, no comments.
- Each decision point MUST have at least one CONTROLS_ACCESS/IMPOSES_CONDITION edge to a graph entity (what it affects).
- Each mechanism MUST have at least one APPLIES_TO edge to a graph entity (what it affects).
- STRICT: Prefer 2–5 high-quality, well-connected decision points over 10+ abstract ones. When in doubt, omit.
- Focus on ACTIONABLE control: what can actually be blocked/enabled/delayed, and WHO/WHAT is affected.
`, domainPath, domainTitle, domainDesc, focusBlock, graphBlock, taxonomy.MechanismSlugExamplesString())
}
