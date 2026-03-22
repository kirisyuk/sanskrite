package main

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildTransformReviewPromptIncludesHardRejects(t *testing.T) {
	prompt := buildTransformReviewPrompt(`[]`)
	required := []string{
		"These are NOT synonyms",
		"Only accept DIRECT or ADJACENT canonical transformation steps",
		"Direct final assembly is allowed",
		"Class-subclass, general-specific, family-member, or grade-variant relationships are NOT transformations",
		"Logistics, services, routes, handling, freight, ports, compliance, documents, and business activities are NOT products",
		"iron ore -> hot-rolled steel coil",
		"battery pack -> LFP battery cell",
		"strict JSON array",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestAcceptProductPairRejectsSynonymishAndServicePairs(t *testing.T) {
	if acceptProductPair(
		productRecord{ID: "a", CanonicalPath: "/product/raw/mineral/extraction/bauxite", PreferredName: "Bauxite", PathShape: "hierarchical", PathSlug: "bauxite"},
		productRecord{ID: "b", CanonicalPath: "/product/raw/mineral/extraction/bauxite-ore", PreferredName: "Bauxite", PathShape: "hierarchical", PathSlug: "bauxite-ore"},
	) {
		t.Fatalf("expected same-name pair to be rejected")
	}
	if acceptProductPair(
		productRecord{ID: "a", CanonicalPath: "/product/raw/mineral/extraction/bauxite", PreferredName: "Bauxite", PathShape: "hierarchical", PathSlug: "bauxite"},
		productRecord{ID: "b", CanonicalPath: "/product/service/logistics/freight/maritime-freight", PreferredName: "Maritime Freight", PathShape: "service", PathSlug: "maritime-freight"},
	) {
		t.Fatalf("expected service pair to be rejected")
	}
}

func TestBuildTransformCandidatesCanonicalizesSynonymInput(t *testing.T) {
	oldMaxInputs, oldMaxOutputs, oldMaxCross, oldMinScore := *maxActorInputs, *maxActorOutputs, *maxActorCross, *minScore
	*maxActorInputs, *maxActorOutputs, *maxActorCross, *minScore = 12, 12, 72, 0
	defer func() {
		*maxActorInputs, *maxActorOutputs, *maxActorCross, *minScore = oldMaxInputs, oldMaxOutputs, oldMaxCross, oldMinScore
	}()

	products := map[string]productRecord{
		"canon-bauxite": {
			ID:            "canon-bauxite",
			CanonicalPath: "/product/raw/mineral/extraction/bauxite",
			PreferredName: "Bauxite",
			PathShape:     "hierarchical",
			PathSlug:      "bauxite",
			ProductType:   "mineral",
			Stage:         "extraction",
			StageOrder:    1,
			QualityScore:  100,
		},
		"alumina": {
			ID:            "alumina",
			CanonicalPath: "/product/raw/mineral/refining/alumina",
			PreferredName: "Alumina",
			PathShape:     "hierarchical",
			PathSlug:      "alumina",
			ProductType:   "mineral",
			Stage:         "refining",
			StageOrder:    2,
			QualityScore:  100,
		},
	}
	actors := map[string]actorInfo{
		"fac-1": {ID: "fac-1", CanonicalPath: "/fac/global/refinery-1", PreferredName: "Refinery 1"},
	}
	canonicalizer := productCanonicalizer{
		byFragmentRoot: map[string]string{"syn-bauxite": "canon-bauxite"},
		byPathRoot:     map[string]string{"/product/raw/mineral/extraction/bauxite-ore": "/product/raw/mineral/extraction/bauxite"},
	}
	transformEdges := []models.DependencyEdge{
		{
			ID:               "edge-1",
			SourceFragmentID: "syn-bauxite",
			TargetFragmentID: "alumina",
			Type:             models.DependencyTypeTransformsInto,
		},
	}
	supplyLinks := []models.HorizonFragment{
		{
			ID:            "sl-in",
			CanonicalPath: "/supply-chain/global/sl-in",
			Type:          models.FragmentTypeSupplyLink,
			ProductRef:    strptr("/product/raw/mineral/extraction/bauxite-ore"),
			CustomerRef:   strptr("/fac/global/refinery-1"),
		},
		{
			ID:            "sl-out",
			CanonicalPath: "/supply-chain/global/sl-out",
			Type:          models.FragmentTypeSupplyLink,
			ProductRef:    strptr("/product/raw/mineral/refining/alumina"),
			SupplierRef:   strptr("/fac/global/refinery-1"),
		},
	}

	candidates := buildTransformCandidates(products, actors, canonicalizer, transformEdges, nil, supplyLinks, nil)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	got := candidates[0]
	if got.From.CanonicalPath != "/product/raw/mineral/extraction/bauxite" {
		t.Fatalf("unexpected from path %q", got.From.CanonicalPath)
	}
	if got.To.CanonicalPath != "/product/raw/mineral/refining/alumina" {
		t.Fatalf("unexpected to path %q", got.To.CanonicalPath)
	}
	if len(got.DirectEdges) != 1 {
		t.Fatalf("expected direct evidence")
	}
	if len(got.ActorEvidence) != 1 {
		t.Fatalf("expected actor evidence")
	}
}

func TestConservativeRejectReasonRejectsBatteryLongJumpAndElectricity(t *testing.T) {
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/chemical/processing/cathode-active-material",
		ToPath:   "/product/component/electronic/assembly/battery-pack",
	}); !reject {
		t.Fatalf("expected battery material long jump to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/finished/mechanical/assembly/gas-turbines",
		ToPath:   "/product/finished/mechanical/assembly/electricity",
	}); !reject {
		t.Fatalf("expected electricity output to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/electronic/manufacturing/wafer-bumping",
		FromName: "Wafer Bumping and Probing Services",
		ToPath:   "/product/finished/electronic/manufacturing/packaged-mobile-processor",
	}); !reject {
		t.Fatalf("expected service-like source to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/electronic/assembly/ev-battery-module",
		ToPath:   "/product/component/electronic/assembly/ev-battery-pack",
	}); reject {
		t.Fatalf("expected adjacent battery step to remain accepted")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/electronic/manufacturing/semiconductor",
		FromName: "Semiconductor",
		ToPath:   "/product/component/electronic/manufacturing/pcba",
	}); !reject {
		t.Fatalf("expected generic semiconductor source to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/mechanical/manufacturing/inconel-738-turbine-blades",
		ToPath:   "/product/component/mechanical/manufacturing/gas-turbine-blades",
	}); !reject {
		t.Fatalf("expected class/subclass blade pair to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/electronic/manufacturing/lithium-ion-cell",
		ToPath:   "/product/finished/mechanical/assembly/electric-vehicles",
		ToName:   "Electric Vehicles",
	}); !reject {
		t.Fatalf("expected battery cell to final vehicle to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/electronic/manufacturing/automotive-semiconductor-wafer",
		ToPath:   "/product/finished/mechanical/assembly/electronic-control-unit",
	}); !reject {
		t.Fatalf("expected wafer to ecu jump to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/chemical/processing/cathode-active-material",
		ToPath:   "/product/component/energy/manufacturing/lithium-ion-cell",
		ToName:   "Lithium-ion Battery Cell",
	}); reject {
		t.Fatalf("expected direct material to cell step to remain allowed")
	}
}

func TestConservativeAcceptReasonRescuesDirectBatteryMaterialToCellStep(t *testing.T) {
	if _, accept := conservativeAcceptReason(transformReviewEntry{
		Accepted: false,
		FromPath: "/product/component/chemical/processing/cathode-active-material",
		ToPath:   "/product/component/energy/manufacturing/lithium-ion-cell",
		ToName:   "Lithium-ion Battery Cell",
		Reason:   "Cathode active material is the essential component used to manufacture lithium-ion battery cells.",
	}); !accept {
		t.Fatalf("expected direct battery material to cell step to be rescued")
	}
	if _, accept := conservativeAcceptReason(transformReviewEntry{
		Accepted: false,
		FromPath: "/product/raw/chemical/refining/lithium-carbonate",
		ToPath:   "/product/component/electronic/assembly/battery-pack",
		ToName:   "Battery Pack",
	}); accept {
		t.Fatalf("expected long-jump lithium carbonate to battery pack to remain rejected")
	}
	if _, accept := conservativeAcceptReason(transformReviewEntry{
		Accepted: false,
		FromPath: "/product/component/chemical/processing/pcam",
		ToPath:   "/product/component/electronic/manufacturing/battery-cell",
		ToName:   "Battery Cell",
	}); accept {
		t.Fatalf("expected pCAM to battery cell to avoid rescue")
	}
}

func TestConservativeRejectReasonRejectsUpstreamBatteryPrecursorAndIronOreLongJump(t *testing.T) {
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/chemical/processing/pcam",
		ToPath:   "/product/component/electronic/manufacturing/battery-cell",
		ToName:   "Battery Cell",
	}); !reject {
		t.Fatalf("expected pCAM to battery cell to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/raw/mineral/extraction/iron-ore",
		ToPath:   "/product/component/metal/processing/hot-rolled-steel-sheet",
		ToName:   "Hot-Rolled Steel Sheet",
	}); !reject {
		t.Fatalf("expected iron ore to steel sheet long-jump to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/goods/manufacturing/pet-preforms",
		ToPath:   "/product/finished/consumer/manufacturing/bottled-water",
		ToName:   "Bottled Water",
	}); !reject {
		t.Fatalf("expected packaging component to bottled water to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/finished/mechanical/assembly/synthetic-rubber",
		ToPath:   "/product/finished/mechanical/assembly/passenger-car-tire",
		ToName:   "Passenger Car Tire",
	}); !reject {
		t.Fatalf("expected synthetic rubber to tire to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/finished/mechanical/assembly/lithium-ion-battery",
		ToPath:   "/product/finished/mechanical/assembly/electric-vehicles",
		ToName:   "Electric Vehicles",
	}); !reject {
		t.Fatalf("expected generic battery to vehicle to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/finished/mechanical/assembly/coking-coal",
		ToPath:   "/product/finished/mechanical/assembly/steel",
		ToName:   "Steel",
	}); !reject {
		t.Fatalf("expected coking coal to steel to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/finished/mechanical/assembly/flotation-reagents",
		ToPath:   "/product/finished/mechanical/assembly/rare-earth-concentrate",
		ToName:   "Rare Earth Concentrate",
	}); !reject {
		t.Fatalf("expected flotation reagents to concentrate to be rejected")
	}
	if _, reject := conservativeRejectReason(transformReviewEntry{
		Accepted: true,
		FromPath: "/product/component/chemical/manufacturing/industrial-gases",
		ToPath:   "/product/finished/chemical/processing/liquid-helium",
		ToName:   "Liquid Helium",
	}); !reject {
		t.Fatalf("expected industrial gases to liquid helium to be rejected")
	}
}

func strptr(s string) *string {
	return &s
}
