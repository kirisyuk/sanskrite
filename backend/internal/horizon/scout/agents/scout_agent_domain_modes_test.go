package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestBuildExtractionPrompt_EnablesSupplyChainModuleFromDomainMode(t *testing.T) {
	a := NewScoutAgent(nil)
	prompt := a.buildExtractionPrompt(
		"content",
		"/org/us/example-inc",
		"/entity/organization",
		"/sector/industrial",
		nil,
		"",
		[]string{taxonomy.DomainModeSupplyChain},
	)
	if !strings.Contains(prompt, "MODULE: supply_chain") {
		t.Fatalf("expected supply_chain module block when domain mode is supply_chain")
	}
	if !strings.Contains(prompt, "DOMAIN MODE GUARDRAILS (strict)") {
		t.Fatalf("expected domain guardrails block in extraction prompt")
	}
	if !strings.Contains(prompt, "DEPENDS_ON") || !strings.Contains(prompt, "SUPPLIES_TO") {
		t.Fatalf("expected supply-chain relation taxonomy in domain guardrails")
	}
	if !strings.Contains(prompt, "DOMAIN-SCOPED RELATION CATALOG (strict override)") {
		t.Fatalf("expected strict relation catalog override in domain mode prompt")
	}
	if !strings.Contains(prompt, "Ignore generic relation catalog below") {
		t.Fatalf("expected generic relation catalog override instruction in domain mode prompt")
	}
	if !strings.Contains(prompt, "DOMAIN-SCOPED RELATION RULES (active contract)") {
		t.Fatalf("expected domain-scoped relation rules block")
	}
	if !strings.Contains(prompt, "targetCanonicalPath MUST NOT start with /domain/") {
		t.Fatalf("expected scoped guardrail that non-domain relations must not target /domain/")
	}
	if !strings.Contains(prompt, "UNIT-FIRST PHASE ORDER (MANDATORY)") {
		t.Fatalf("expected unit-first phase order block in scoped prompt")
	}
	if !strings.Contains(prompt, "\"unitProfiles\"") {
		t.Fatalf("expected unitProfiles field in extraction task contract")
	}
	if strings.Contains(prompt, "RELATIVE_OF") {
		t.Fatalf("supply_chain scoped prompt must not contain general-only relation taxonomy")
	}
	if strings.Contains(prompt, "RENAMED_TO") {
		t.Fatalf("supply_chain scoped prompt must not contain org-lifecycle relation taxonomy")
	}
}

func TestBuildExtractionPrompt_GeneralModeKeepsFullRelationCatalog(t *testing.T) {
	a := NewScoutAgent(nil)
	prompt := a.buildExtractionPrompt(
		"content",
		"/org/us/example-inc",
		"/entity/organization",
		"/sector/industrial",
		nil,
		"",
		nil,
	)
	if !strings.Contains(prompt, "RELATIVE_OF") {
		t.Fatalf("general prompt must include broad relation taxonomy")
	}
	if !strings.Contains(prompt, "RENAMED_TO") {
		t.Fatalf("general prompt must include org lifecycle relation taxonomy")
	}
	if !strings.Contains(prompt, "\"unitProfiles\"") {
		t.Fatalf("general prompt must still include unitProfiles contract")
	}
	if !strings.Contains(prompt, "targetCanonicalPath MUST NOT start with /domain/") {
		t.Fatalf("general prompt must include non-domain target guardrail for /domain/")
	}
}

func TestBuildExtractionPrompt_MixedGeneralAndSpecificUsesScopedCatalog(t *testing.T) {
	a := NewScoutAgent(nil)
	prompt := a.buildExtractionPrompt(
		"content",
		"/org/us/example-inc",
		"/entity/organization",
		"/sector/industrial",
		nil,
		"",
		[]string{taxonomy.DomainModeGeneral, taxonomy.DomainModeSupplyChain},
	)
	if !strings.Contains(prompt, "DOMAIN-SCOPED RELATION RULES (active contract)") {
		t.Fatalf("mixed general+specific should use scoped relation rules")
	}
	if strings.Contains(prompt, "RELATIVE_OF") {
		t.Fatalf("mixed general+specific should not include unrelated general taxonomy")
	}
}
