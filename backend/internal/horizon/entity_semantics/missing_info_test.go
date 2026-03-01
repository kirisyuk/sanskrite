package entity_semantics

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEvaluateMissingInfoRules_TriggerSeed(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerSeed, QuestionTemplate: "seed question for %s"},
	}
	out := EvaluateMissingInfoRules(nil, nil, rules, nil, "/market/global/test", EvaluateModeSeed)
	if len(out) != 1 {
		t.Fatalf("seed mode: expected 1 question, got %d", len(out))
	}
	if !strings.Contains(out[0], "/market/global/test") {
		t.Errorf("expected path in question, got %q", out[0])
	}
	if !strings.Contains(out[0], "seed question") {
		t.Errorf("expected seed template, got %q", out[0])
	}

	// full mode: TriggerSeed does not fire
	outFull := EvaluateMissingInfoRules(nil, nil, rules, nil, "/market/global/test", EvaluateModeFull)
	if len(outFull) != 0 {
		t.Errorf("full mode: TriggerSeed should not fire, got %d questions", len(outFull))
	}
}

func TestEvaluateMissingInfoRules_TriggerRelationMissing(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerRelationMissing, RelationType: "HAS_MARKET_SEGMENT", QuestionTemplate: "market segments for %s"},
	}
	// No relations: rule fires
	out := EvaluateMissingInfoRules(nil, nil, rules, []string{taxonomy.DomainModeMarket}, "/market/global/test", EvaluateModeFull)
	if len(out) != 1 {
		t.Fatalf("expected 1 (relation missing), got %d", len(out))
	}

	// With relation: rule does not fire
	out2 := EvaluateMissingInfoRules(nil, []string{"HAS_MARKET_SEGMENT"}, rules, []string{taxonomy.DomainModeMarket}, "/market/global/test", EvaluateModeFull)
	if len(out2) != 0 {
		t.Errorf("with HAS_MARKET_SEGMENT: expected 0, got %d", len(out2))
	}
}

func TestEvaluateMissingInfoRules_TriggerRelationTypes(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerRelationMissing, RelationTypes: []string{"PRODUCES", "CONSUMES"}, DomainMode: taxonomy.DomainModeSupplyChain, QuestionTemplate: "products for %s"},
	}
	// Neither PRODUCES nor CONSUMES: fires
	out := EvaluateMissingInfoRules(nil, nil, rules, []string{taxonomy.DomainModeSupplyChain}, "/org/us/acme", EvaluateModeFull)
	if len(out) != 1 {
		t.Fatalf("expected 1 (both missing), got %d", len(out))
	}

	// Only PRODUCES: does not fire (rule requires ALL to be missing; having one satisfies partial coverage)
	out2 := EvaluateMissingInfoRules(nil, []string{"PRODUCES"}, rules, []string{taxonomy.DomainModeSupplyChain}, "/org/us/acme", EvaluateModeFull)
	if len(out2) != 0 {
		t.Fatalf("PRODUCES only: rule fires when all missing; expected 0, got %d", len(out2))
	}

	// Both present: does not fire
	out3 := EvaluateMissingInfoRules(nil, []string{"PRODUCES", "CONSUMES"}, rules, []string{taxonomy.DomainModeSupplyChain}, "/org/us/acme", EvaluateModeFull)
	if len(out3) != 0 {
		t.Errorf("both present: expected 0, got %d", len(out3))
	}
}

func TestEvaluateMissingInfoRules_TriggerFieldEmpty(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "label for %s"},
	}
	// Nil fragment: field_empty does not fire (fragment required)
	out := EvaluateMissingInfoRules(nil, nil, rules, nil, "/org/us/acme", EvaluateModeFull)
	if len(out) != 0 {
		t.Errorf("nil fragment: field_empty should not fire, got %d", len(out))
	}

	// Empty Labels.en: fires
	frag := &models.HorizonFragment{Type: models.FragmentTypeOrganization, CanonicalPath: "/org/us/acme"}
	out2 := EvaluateMissingInfoRules(frag, nil, rules, nil, "/org/us/acme", EvaluateModeFull)
	if len(out2) != 1 {
		t.Fatalf("empty Labels.en: expected 1, got %d", len(out2))
	}

	// Populated Labels.en: does not fire
	frag.Labels = map[string]string{"en": "Acme Inc"}
	out3 := EvaluateMissingInfoRules(frag, nil, rules, nil, "/org/us/acme", EvaluateModeFull)
	if len(out3) != 0 {
		t.Errorf("Labels.en set: expected 0, got %d", len(out3))
	}
}

func TestEvaluateMissingInfoRules_TriggerRelationCountBelow(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerRelationCountBelow, MinCount: 3, DomainMode: taxonomy.DomainModeSupplyChain, QuestionTemplate: "upstream/downstream for %s"},
	}
	out := EvaluateMissingInfoRules(nil, []string{"PRODUCES"}, rules, []string{taxonomy.DomainModeSupplyChain}, "/org/us/acme", EvaluateModeFull)
	if len(out) != 1 {
		t.Fatalf("1 relation < 3: expected 1, got %d", len(out))
	}

	out2 := EvaluateMissingInfoRules(nil, []string{"A", "B", "C"}, rules, []string{taxonomy.DomainModeSupplyChain}, "/org/us/acme", EvaluateModeFull)
	if len(out2) != 0 {
		t.Errorf("3 relations: expected 0, got %d", len(out2))
	}
}

func TestEvaluateMissingInfoRules_DomainModeFilter(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerRelationMissing, RelationType: "PARTICIPATES_IN", DomainMode: taxonomy.DomainModeMarket, QuestionTemplate: "participants for %s"},
	}
	// Market mode active: fires
	out := EvaluateMissingInfoRules(nil, nil, rules, []string{taxonomy.DomainModeMarket}, "/market/global/test", EvaluateModeFull)
	if len(out) != 1 {
		t.Fatalf("market mode: expected 1, got %d", len(out))
	}

	// Supply chain only: rule filtered out (domain mode mismatch)
	out2 := EvaluateMissingInfoRules(nil, nil, rules, []string{taxonomy.DomainModeSupplyChain}, "/market/global/test", EvaluateModeFull)
	if len(out2) != 0 {
		t.Errorf("supply_chain mode: market rule should not apply, got %d", len(out2))
	}
}

func TestEvaluateMissingInfoRules_GeneralRuleNoDomainFilter(t *testing.T) {
	rules := []MissingInfoRule{
		{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "label for %s"},
	}
	frag := &models.HorizonFragment{Type: models.FragmentTypeOrganization, CanonicalPath: "/org/us/acme"}
	out := EvaluateMissingInfoRules(frag, nil, rules, []string{taxonomy.DomainModeMarket}, "/org/us/acme", EvaluateModeFull)
	if len(out) != 1 {
		t.Fatalf("general rule (DomainMode empty): should apply regardless of domainModes, got %d", len(out))
	}
}

func TestEvaluateMissingInfoRules_MarketWithEmptyRelations(t *testing.T) {
	def := GetDefinition(EntityTypeMarket)
	if def == nil {
		t.Fatal("Market definition not found")
	}
	rules := def.MissingInfoRules
	rules = append(rules, DomainModeRulesForModes([]string{taxonomy.DomainModeMarket})...)

	out := EvaluateMissingInfoRules(nil, nil, rules, []string{taxonomy.DomainModeMarket}, "/market/global/public-safety", EvaluateModeFull)
	if len(out) == 0 {
		t.Fatal("Market with empty relations should produce missingInfo (relation_missing HAS_MARKET_SEGMENT, PARTICIPATES_IN)")
	}
	// Should include market-specific questions
	hasMarket := false
	for _, q := range out {
		if strings.Contains(strings.ToLower(q), "market") || strings.Contains(strings.ToLower(q), "segment") || strings.Contains(strings.ToLower(q), "participant") {
			hasMarket = true
			break
		}
	}
	if !hasMarket {
		t.Errorf("expected market-specific questions, got %v", out)
	}
}

func TestEvaluateMissingInfoRules_SeedModeForMarket(t *testing.T) {
	def := GetDefinition(EntityTypeMarket)
	if def == nil {
		t.Fatal("Market definition not found")
	}
	rules := def.MissingInfoRules

	out := EvaluateMissingInfoRules(nil, nil, rules, nil, "/market/global/public-safety", EvaluateModeSeed)
	if len(out) == 0 {
		t.Fatal("Market seed mode should produce seed questions")
	}
	// Seed rules should include TAM, CAGR, domain, etc.
	hasTAM := false
	for _, q := range out {
		if strings.Contains(strings.ToUpper(q), "TAM") || strings.Contains(q, "CAGR") || strings.Contains(q, "domain") {
			hasTAM = true
			break
		}
	}
	if !hasTAM {
		t.Logf("seed questions: %v", out)
		t.Errorf("expected market seed to include TAM/CAGR/domain questions")
	}
}

// TestResolveByPath_PathPrecedenceOverFragmentType verifies that canonical path overrides
// fragment type from extraction (e.g. taxonomy misclassifies /market/ as Organization).
func TestResolveByPath_PathPrecedenceOverFragmentType(t *testing.T) {
	// Bug scenario: taxonomy returns Organization for /market/global/public-safety.
	// ResolveByPath must return Market so we get Market MissingInfoRules, not Organization.
	key := ResolveByPath("/market/global/public-safety", models.FragmentTypeOrganization, "")
	if key != EntityTypeMarket {
		t.Fatalf("path /market/ must yield EntityTypeMarket even when fragmentType=ORGANIZATION; got %q", key)
	}
	def := GetDefinition(key)
	if def == nil || len(def.MissingInfoRules) == 0 {
		t.Fatal("Market definition must have MissingInfoRules")
	}
	// Verify we get market-specific rules (HAS_MARKET_SEGMENT, TAM, etc.)
	hasMarketRule := false
	for _, r := range def.MissingInfoRules {
		if r.RelationType == "HAS_MARKET_SEGMENT" || strings.Contains(r.FieldPath, "tam") || strings.Contains(r.QuestionTemplate, "TAM") {
			hasMarketRule = true
			break
		}
	}
	if !hasMarketRule {
		t.Errorf("Market definition should have market-specific rules")
	}
}

func TestIsFragmentFieldEmpty_StateMetric(t *testing.T) {
	frag := &models.HorizonFragment{State: map[string]models.MetricValue{}}
	// Unset key: empty
	if !isFragmentFieldEmpty(frag, "State./metric/market/tam_usd") {
		t.Error("unset State key should be empty")
	}
	// Zero value: empty
	frag.State["/metric/market/tam_usd"] = models.MetricValue{Value: 0}
	if !isFragmentFieldEmpty(frag, "State./metric/market/tam_usd") {
		t.Error("zero Value should be empty")
	}
	// Non-zero: not empty
	frag.State["/metric/market/tam_usd"] = models.MetricValue{Value: 1e9}
	if isFragmentFieldEmpty(frag, "State./metric/market/tam_usd") {
		t.Error("non-zero Value should not be empty")
	}
}
