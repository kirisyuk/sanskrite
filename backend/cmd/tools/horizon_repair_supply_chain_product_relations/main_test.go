package main

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

func TestFragmentsCollapseToSelfLoopByPath(t *testing.T) {
	t.Parallel()

	if !fragmentsCollapseToSelfLoop(nil, nil, "/product/raw/mineral/helium", "/product/raw/mineral/helium") {
		t.Fatal("expected equal canonical paths to collapse into self-loop")
	}
}

func TestFragmentsCollapseToSelfLoopByFragmentID(t *testing.T) {
	t.Parallel()

	source := &models.HorizonFragment{ID: "cp_same"}
	target := &models.HorizonFragment{ID: "cp_same"}
	if !fragmentsCollapseToSelfLoop(source, target, "/product/raw/mineral/helium", "/product/finished/mechanical/assembly/mri-scanner") {
		t.Fatal("expected equal fragment ids to collapse into self-loop")
	}
}

func TestNormalizeAssemblyEdgeListsDropsSelfLoops(t *testing.T) {
	t.Parallel()

	r := &runner{ctx: context.Background(), tenantID: "global"}
	transforms, assemblies, changed := r.normalizeAssemblyEdgeLists(
		[]models.SupplyChainAssemblyTransformEdge{
			{
				FromProductRef: "/product/raw/mineral/helium",
				ToProductRef:   "/product/raw/mineral/helium",
				Relation:       string(models.DependencyTypeTransformsInto),
			},
		},
		nil,
	)
	if !changed {
		t.Fatal("expected self-loop artifact edge to be removed")
	}
	if len(transforms) != 0 || len(assemblies) != 0 {
		t.Fatalf("expected self-loop artifact edges to be dropped, got transforms=%d assemblies=%d", len(transforms), len(assemblies))
	}
}

func TestResolveFragmentForDecisionPrefersFragmentCanonicalPathOverBadQualifiers(t *testing.T) {
	t.Parallel()

	ore := &models.HorizonFragment{ID: "ore-id", CanonicalPath: "/product/raw/mineral/extraction/niobium-ore"}
	oxide := &models.HorizonFragment{ID: "oxide-id", CanonicalPath: "/product/finished/chemical/refining/niobium-oxide"}
	r := &runner{
		ctx: context.Background(),
		fragmentByID: map[string]*models.HorizonFragment{
			ore.ID:   ore,
			oxide.ID: oxide,
		},
		fragmentByPath: map[string]*models.HorizonFragment{
			ore.CanonicalPath:   ore,
			oxide.CanonicalPath: oxide,
		},
	}
	edge := &models.DependencyEdge{
		SourceFragmentID: ore.ID,
		TargetFragmentID: oxide.ID,
	}

	sourceFrag, err := r.resolveFragmentForDecision(
		edge,
		"/product/finished/chemical/refining/niobium-oxide",
		"/product/raw/mineral/extraction/niobium-ore",
		ore.CanonicalPath,
		oxide.CanonicalPath,
		true,
	)
	if err != nil {
		t.Fatalf("resolveFragmentForDecision(source) error = %v", err)
	}
	if sourceFrag == nil || sourceFrag.ID != ore.ID {
		t.Fatalf("sourceFrag=%v, want ore-id", sourceFrag)
	}

	targetFrag, err := r.resolveFragmentForDecision(
		edge,
		"/product/finished/chemical/refining/niobium-oxide",
		"/product/raw/mineral/extraction/niobium-ore",
		ore.CanonicalPath,
		oxide.CanonicalPath,
		false,
	)
	if err != nil {
		t.Fatalf("resolveFragmentForDecision(target) error = %v", err)
	}
	if targetFrag == nil || targetFrag.ID != oxide.ID {
		t.Fatalf("targetFrag=%v, want oxide-id", targetFrag)
	}
}

func TestDecideRelationDoesNotUseReasonReverseAgainstClearStageProgression(t *testing.T) {
	t.Parallel()

	r := &runner{
		ctx:         context.Background(),
		tenantID:    "global",
		productDict: productdict.NewRuntime(nil).WithTenantID("global"),
	}
	decision, ok := r.decideRelation(
		"/product/raw/chemical/refining/lithium-carbonate",
		"/product/component/energy/manufacturing/lithium-ion-battery",
		models.DependencyTypeTransformsInto,
		"Supply chain: Lithium-ion Battery transforms into Lithium Carbonate at Ultium Cells Lordstown.",
		"Lithium Carbonate",
		"Lithium-ion Battery",
	)
	if !ok {
		t.Fatal("expected decideRelation to keep a forward heuristic relation")
	}
	if decision.Source == "reason_reverse" {
		t.Fatalf("unexpected decision source=%q", decision.Source)
	}
	if decision.SourcePath != "/product/raw/chemical/refining/lithium-carbonate" || decision.TargetPath != "/product/component/energy/manufacturing/lithium-ion-battery" {
		t.Fatalf("unexpected normalized paths: %q -> %q", decision.SourcePath, decision.TargetPath)
	}
}

func TestDecisionFromReasonReverseRecognizesUsedIn(t *testing.T) {
	t.Parallel()

	decision, ok := decisionFromReasonReverse(
		"/product/finished/medical/assembly/mri-scanner",
		"/product/raw/mineral/helium",
		models.DependencyTypeTransformsInto,
		"Helium is used in MRI Scanner cooling systems.",
		"MRI Scanner",
		"Helium",
	)
	if !ok {
		t.Fatal("expected reverse reason detection for used-in phrasing")
	}
	if decision.EdgeType != models.DependencyTypeUsedIn {
		t.Fatalf("edgeType=%q, want USED_IN", decision.EdgeType)
	}
	if decision.RelationKind != "usage" {
		t.Fatalf("relationKind=%q, want usage", decision.RelationKind)
	}
	if decision.SourcePath != "/product/raw/mineral/helium" || decision.TargetPath != "/product/finished/medical/assembly/mri-scanner" {
		t.Fatalf("unexpected normalized paths: %q -> %q", decision.SourcePath, decision.TargetPath)
	}
}
