package scout

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyBestEffortEdgeEnrichment_OwnsSyncsOwnershipPercent(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation:       DependencyTypeOwns,
		Qualifiers: map[string]interface{}{},
		Metrics: map[string]models.MetricValue{
			"ownership_percent": {Value: 62.5},
		},
	}
	applyBestEffortEdgeEnrichment(edge)
	if got, ok := edge.Qualifiers["ownership_percent"]; !ok || got == nil {
		t.Fatalf("expected qualifiers.ownership_percent to be mirrored from metrics")
	}
}

func TestApplyBestEffortEdgeEnrichment_OwnsBackfillsMetricFromQualifier(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation:       DependencyTypeOwns,
		Qualifiers: map[string]interface{}{"stake_pct": "45%"},
	}
	applyBestEffortEdgeEnrichment(edge)
	mv, ok := edge.Metrics["ownership_percent"]
	if !ok {
		t.Fatalf("expected metrics.ownership_percent to be backfilled")
	}
	if mv.Value != 45 {
		t.Fatalf("expected ownership_percent=45, got %.2f", mv.Value)
	}
}

func TestApplyBestEffortEdgeEnrichment_AddsTemporalHintForHistoricalEmployment(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation:       DependencyTypeEmployedBy,
		Qualifiers: map[string]interface{}{"edgeModality": "historical"},
	}
	applyBestEffortEdgeEnrichment(edge)
	hints, ok := edge.Qualifiers["enrichmentHints"]
	if !ok || hints == nil {
		t.Fatalf("expected enrichment hint for historical employment without validTo")
	}
}

func TestApplyBestEffortEdgeEnrichment_SetsHistoricalFromDates(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	edge := &models.DependencyEdge{
		Relation:       DependencyTypeManages,
		Qualifiers: map[string]interface{}{},
		ValidTo:    &past,
	}
	applyBestEffortEdgeEnrichment(edge)
	if got := edge.Qualifiers["edgeModality"]; got != "historical" {
		t.Fatalf("expected edgeModality=historical, got %v", got)
	}
}

func TestApplyBestEffortEdgeEnrichment_DependsOnNormalizesTransmissionQualifiers(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation: DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"facet":               "logistics",
			"throughputShare":     "0.72",
			"reroutePenalty":      "0.40",
			"latency_delta_hours": 36,
			"rerouteCostDeltaPct": "18",
			"capacityUtilization": "0.81",
			"substitutability":    0.2,
		},
	}

	applyBestEffortEdgeEnrichment(edge)

	assertHasNumericQualifier(t, edge, models.QualifierThroughputShare)
	assertHasNumericQualifier(t, edge, models.QualifierReroutePenalty)
	assertHasNumericQualifier(t, edge, models.QualifierRerouteLatencyDeltaHrs)
	assertHasNumericQualifier(t, edge, models.QualifierRerouteCostDeltaPct)
	assertHasNumericQualifier(t, edge, models.QualifierCapacityConstraint)

	if _, ok := edge.Metrics[models.QualifierThroughputShare]; !ok {
		t.Fatalf("expected metrics.%s to be synced", models.QualifierThroughputShare)
	}
}

func TestApplyBestEffortEdgeEnrichment_DependsOnAddsTransmissionHintsWhenMissing(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation: DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"facet": "logistics",
		},
	}
	applyBestEffortEdgeEnrichment(edge)
	if _, ok := edge.Qualifiers["enrichmentHints"]; !ok {
		t.Fatalf("expected enrichmentHints for missing transmission qualifiers")
	}
}

func TestApplyBestEffortEdgeEnrichment_DependsOnSetsTransmissionContractComplete(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation: DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"facet":                         "logistics",
			models.QualifierThroughputShare: 0.62,
			models.QualifierReroutePenalty:  0.35,
		},
	}

	applyBestEffortEdgeEnrichment(edge)

	if got := edge.Qualifiers[QualifierTransmissionContractStatus]; got != TransmissionContractStatusComplete {
		t.Fatalf("expected contract status complete, got %v", got)
	}
	if got := edge.Qualifiers[QualifierTransmissionContractMode]; got != TransmissionContractModeExplicit {
		t.Fatalf("expected contract mode explicit, got %v", got)
	}
	if got := edge.Qualifiers[QualifierTransmissionContractVersion]; got != TransmissionContractVersionV1 {
		t.Fatalf("expected contract version %s, got %v", TransmissionContractVersionV1, got)
	}
}

func TestApplyBestEffortEdgeEnrichment_DependsOnSetsTransmissionContractPartial(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation: DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"facet":                         "logistics",
			models.QualifierThroughputShare: 0.62,
		},
	}

	applyBestEffortEdgeEnrichment(edge)

	if got := edge.Qualifiers[QualifierTransmissionContractStatus]; got != TransmissionContractStatusPartial {
		t.Fatalf("expected contract status partial, got %v", got)
	}
	missing, ok := edge.Qualifiers[QualifierTransmissionContractMissing]
	if !ok || missing == nil {
		t.Fatalf("expected missing fields for partial contract")
	}
}

func TestApplyBestEffortEdgeEnrichment_DependsOnSetsTransmissionContractMissing(t *testing.T) {
	edge := &models.DependencyEdge{
		Relation:       DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{},
	}

	applyBestEffortEdgeEnrichment(edge)

	if got := edge.Qualifiers[QualifierTransmissionContractStatus]; got != TransmissionContractStatusMissing {
		t.Fatalf("expected contract status missing, got %v", got)
	}
	if got := edge.Qualifiers[QualifierTransmissionContractMode]; got != TransmissionContractModeInferred {
		t.Fatalf("expected contract mode inferred, got %v", got)
	}
}

func assertHasNumericQualifier(t *testing.T, edge *models.DependencyEdge, key string) {
	t.Helper()
	raw, ok := edge.Qualifiers[key]
	if !ok {
		t.Fatalf("missing qualifier %s", key)
	}
	if _, ok := parseNumericQualifier(raw); !ok {
		t.Fatalf("qualifier %s is not numeric: %v", key, raw)
	}
}

func TestWithDeterministicDirectionalRoleQualifiers_OwnsFillsMissingRoles(t *testing.T) {
	quals, changed := withDeterministicDirectionalRoleQualifiers(
		DependencyTypeOwns,
		FragmentTypeOrganization,
		FragmentTypeOrganization,
		nil,
	)
	if !changed {
		t.Fatalf("expected changed=true for OWNS org->org without qualifiers")
	}
	if got := quals["source_role"]; got != "owner" {
		t.Fatalf("expected source_role=owner, got %v", got)
	}
	if got := quals["target_role"]; got != "owned_entity" {
		t.Fatalf("expected target_role=owned_entity, got %v", got)
	}
	if got := quals["direction_basis"]; got != "owner_to_owned" {
		t.Fatalf("expected direction_basis=owner_to_owned, got %v", got)
	}
}

func TestWithDeterministicDirectionalRoleQualifiers_DoesNotOverrideExisting(t *testing.T) {
	in := map[string]interface{}{
		"source_role": "investor",
	}
	quals, changed := withDeterministicDirectionalRoleQualifiers(
		DependencyTypeInvestsIn,
		FragmentTypeOrganization,
		FragmentTypeOrganization,
		in,
	)
	if !changed {
		t.Fatalf("expected changed=true because target role and direction basis are missing")
	}
	if got := quals["source_role"]; got != "investor" {
		t.Fatalf("expected existing source_role preserved, got %v", got)
	}
	if got := quals["target_role"]; got != "investee" {
		t.Fatalf("expected target_role=investee, got %v", got)
	}
	if got := quals["direction_basis"]; got != "investor_to_investee" {
		t.Fatalf("expected direction_basis=investor_to_investee, got %v", got)
	}
}

func TestWithDeterministicDirectionalRoleQualifiers_RepairsInvalidDirectionalPayload(t *testing.T) {
	in := map[string]interface{}{
		"source_role":     "shareholder",
		"target_role":     "bank",
		"direction_basis": "to_target",
	}
	quals, changed := withDeterministicDirectionalRoleQualifiers(
		DependencyTypeOwns,
		FragmentTypeOrganization,
		FragmentTypeOrganization,
		in,
	)
	if !changed {
		t.Fatalf("expected changed=true for invalid directional payload")
	}
	if got := quals["source_role"]; got != "owner" {
		t.Fatalf("expected repaired source_role=owner, got %v", got)
	}
	if got := quals["target_role"]; got != "owned_entity" {
		t.Fatalf("expected repaired target_role=owned_entity, got %v", got)
	}
	if got := quals["direction_basis"]; got != "owner_to_owned" {
		t.Fatalf("expected repaired direction_basis=owner_to_owned, got %v", got)
	}
}

func TestWithDeterministicDirectionalRoleQualifiers_SkipsNonAmbiguousTypePairs(t *testing.T) {
	quals, changed := withDeterministicDirectionalRoleQualifiers(
		DependencyTypeDependsOn,
		FragmentTypeOrganization,
		FragmentTypeProduct,
		nil,
	)
	if changed {
		t.Fatalf("expected changed=false for non actor-like DEPENDS_ON pair")
	}
	if quals != nil {
		t.Fatalf("expected nil qualifiers passthrough, got %#v", quals)
	}
}

func TestWithDeterministicDirectionalRoleQualifiers_ControlsAccessFillsPolicyRoles(t *testing.T) {
	quals, changed := withDeterministicDirectionalRoleQualifiers(
		DependencyTypeControlsAccess,
		FragmentTypeOrganization,
		FragmentTypeMarket,
		nil,
	)
	if !changed {
		t.Fatalf("expected changed=true for CONTROLS_ACCESS org->market without qualifiers")
	}
	if got := quals["source_role"]; got != "controller" {
		t.Fatalf("expected source_role=controller, got %v", got)
	}
	if got := quals["target_role"]; got != "access_subject" {
		t.Fatalf("expected target_role=access_subject, got %v", got)
	}
	if got := quals["direction_basis"]; got != "controller_to_access_subject" {
		t.Fatalf("expected direction_basis=controller_to_access_subject, got %v", got)
	}
}

func TestWithDeterministicDirectionalRoleQualifiers_InGeoScopeRepairsAliases(t *testing.T) {
	in := map[string]interface{}{
		"source_role":     "policy_source",
		"target_role":     "jurisdiction",
		"direction_basis": "instrument_to_jurisdiction",
	}
	quals, changed := withDeterministicDirectionalRoleQualifiers(
		DependencyTypeInGeoScope,
		FragmentTypeMechanism,
		FragmentTypeGeoRegion,
		in,
	)
	if !changed {
		t.Fatalf("expected changed=true for IN_GEO_SCOPE alias repair")
	}
	if got := quals["source_role"]; got != "scope_owner" {
		t.Fatalf("expected source_role=scope_owner, got %v", got)
	}
	if got := quals["target_role"]; got != "geo_scope" {
		t.Fatalf("expected target_role=geo_scope, got %v", got)
	}
	if got := quals["direction_basis"]; got != "scope_owner_to_geo_scope" {
		t.Fatalf("expected direction_basis=scope_owner_to_geo_scope, got %v", got)
	}
}
