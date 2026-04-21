package horizon

import (
	"strings"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestBuildGroundedCompletionPromptUsesWideMapSlice(t *testing.T) {
	canvas := &CompleteSubstrateCanvas{}
	lens := &horizonmodels.Lens{ID: "lens-1", Name: "ASML", TenantID: "tenant-1"}
	scope := &identifyStressScope{
		SelectedCanonicalPath: "/org/nl/asml-holding-nv",
		NodeRefs: []string{
			"/org/nl/asml-holding-nv",
			"/org/de/carl-zeiss-smt-gmbh",
			"/fac/nl/veldhoven-campus",
		},
		NodeFragments: []*horizonmodels.HorizonFragment{
			{CanonicalPath: "/fac/nl/veldhoven-campus"},
			{CanonicalPath: "/fac/de/oberkochen-optics-campus"},
		},
		Artifacts: []*horizonmodels.MaterializedArtifact{
			{
				CanonicalKey:    "artifact/asml-euv",
				AnchorSupplier:  "/org/de/carl-zeiss-smt-gmbh",
				AnchorCustomer:  "/org/nl/asml-holding-nv",
				ProductAxis:     "/product/component/optics/euv-projection-optics",
				PathStressScore: 0.33,
				LegCount:        2,
			},
		},
		VisibleRelations: []IdentifyStressVisibleRelation{
			{
				SourceCanonicalPath: "/org/de/carl-zeiss-smt-gmbh",
				TargetCanonicalPath: "/org/nl/asml-holding-nv",
				RelationType:        string(ah.RelationNameSupplies),
				RelationFamily:      "context",
				RelationRole:        "context",
				BusinessMeaning:     "ZEISS optics to ASML",
				Criticality:         0.91,
			},
		},
	}
	gaps := []SubstrateGap{
		{
			SupplyLinkRef:      "/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
			SupplierRef:        "/org/de/carl-zeiss-smt-gmbh",
			CustomerRef:        "/org/nl/asml-holding-nv",
			ProductRef:         "/product/component/optics/euv-projection-optics",
			GapType:            "missing_operational_detail",
			ExistingArtifact:   "artifact/asml-euv",
			ExistingMatchKind:  substrateMatchExactSupplyLinkProduct,
			ContextualArtifact: "artifact/context-surrogate",
			MissingFields:      []string{"transportViaRefs", "mode"},
		},
	}

	prompt := canvas.buildGroundedCompletionPrompt(lens, scope, gaps, "2026-03-20")

	expectedSnippets := []string{
		"You MUST NOT invent new objects.",
		"Prefer exact supplyLinkRef + productRef first",
		"You MUST ONLY return completions for supplyLinkRef values explicitly listed",
		"Do NOT build long indirect chains.",
		"visible lens scope, visible supply-path artifacts, and distant map relations",
		"/org/de/carl-zeiss-smt-gmbh",
		"/fac/de/oberkochen-optics-campus",
		"artifactKey=artifact/asml-euv",
		"ZEISS optics to ASML",
		"/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
		"existingMatchKind=exact_supply_link_product",
		"contextualArtifact=artifact/context-surrogate",
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", snippet, prompt)
		}
	}
}

func TestExactArtifactGapForSupplyLinkRequiresMatchingProduct(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		CanonicalKey: "artifact/context-only",
		ProductAxis:  "/product/component/electronic/photomask-set",
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/de/carl-zeiss-smt-gmbh"},
				{Class: ah.FragmentClassProduct, Ref: "/product/component/electronic/photomask-set"},
				{Class: ah.FragmentClassOrg, Ref: "/org/nl/asml-holding-nv"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:                   0,
					SupplierRef:             "/org/de/carl-zeiss-smt-gmbh",
					CustomerRef:             "/org/nl/asml-holding-nv",
					ProductRef:              "/product/component/electronic/photomask-set",
					SupplyLinkCanonicalPath: "/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
				},
			},
			nil,
			nil,
		),
	}

	match, contextual := exactArtifactGapForSupplyLink(
		[]*horizonmodels.MaterializedArtifact{artifact},
		"/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
		"/org/de/carl-zeiss-smt-gmbh",
		"/org/nl/asml-holding-nv",
		"/product/component/optics/euv-projection-optics",
	)
	if match != nil || contextual != nil {
		t.Fatalf("expected mismatched product-family artifact to be ignored, got match=%+v contextual=%+v", match, contextual)
	}
}

func TestExactArtifactGapForSupplyLinkPrefersExactLinkAndProduct(t *testing.T) {
	artifacts := []*horizonmodels.MaterializedArtifact{
		{
			CanonicalKey:   "artifact/exact-endpoint",
			AnchorSupplier: "/org/de/carl-zeiss-smt-gmbh",
			AnchorCustomer: "/org/nl/asml-holding-nv",
			ProductAxis:    "/product/component/optics/euv-projection-optics",
			Payload: horizonmodels.BuildSupplyChainPathPayload(
				[]horizonmodels.SupplyChainPathNode{
					{Class: ah.FragmentClassOrg, Ref: "/org/de/carl-zeiss-smt-gmbh"},
					{Class: ah.FragmentClassProduct, Ref: "/product/component/optics/euv-projection-optics"},
					{Class: ah.FragmentClassOrg, Ref: "/org/nl/asml-holding-nv"},
				},
				nil,
				[]horizonmodels.SupplyChainPathLeg{
					{
						Index:       0,
						SupplierRef: "/org/de/carl-zeiss-smt-gmbh",
						CustomerRef: "/org/nl/asml-holding-nv",
						ProductRef:  "/product/component/optics/euv-projection-optics",
						Mode:        "air",
					},
				},
				nil,
				nil,
			),
		},
		{
			CanonicalKey:   "artifact/exact-link",
			AnchorSupplier: "/org/de/carl-zeiss-smt-gmbh",
			AnchorCustomer: "/org/nl/asml-holding-nv",
			ProductAxis:    "/product/component/optics/euv-projection-optics",
			Payload: horizonmodels.BuildSupplyChainPathPayload(
				[]horizonmodels.SupplyChainPathNode{
					{Class: ah.FragmentClassOrg, Ref: "/org/de/carl-zeiss-smt-gmbh"},
					{Class: ah.FragmentClassProduct, Ref: "/product/component/optics/euv-projection-optics"},
					{Class: ah.FragmentClassOrg, Ref: "/org/nl/asml-holding-nv"},
				},
				nil,
				[]horizonmodels.SupplyChainPathLeg{
					{
						Index:                   0,
						SupplierRef:             "/org/de/carl-zeiss-smt-gmbh",
						CustomerRef:             "/org/nl/asml-holding-nv",
						ProductRef:              "/product/component/optics/euv-projection-optics",
						SupplyLinkCanonicalPath: "/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
						Mode:                    "air",
					},
				},
				nil,
				nil,
			),
		},
	}

	match, contextual := exactArtifactGapForSupplyLink(
		artifacts,
		"/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
		"/org/de/carl-zeiss-smt-gmbh",
		"/org/nl/asml-holding-nv",
		"/product/component/optics/euv-projection-optics",
	)
	if contextual != nil {
		t.Fatalf("did not expect contextual surrogate when exact matches exist, got %+v", contextual)
	}
	if match == nil || match.Artifact == nil {
		t.Fatalf("expected exact match, got %+v", match)
	}
	if match.MatchKind != substrateMatchExactSupplyLinkProduct {
		t.Fatalf("expected exact supply-link/product match, got %+v", match)
	}
	if match.Artifact.CanonicalKey != "artifact/exact-link" {
		t.Fatalf("expected exact-link artifact, got %+v", match.Artifact)
	}
}

func TestExactArtifactGapForSupplyLinkReturnsContextualSurrogateSeparately(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		CanonicalKey:   "artifact/contextual-optics",
		AnchorSupplier: "/org/de/carl-zeiss-smt-gmbh",
		AnchorCustomer: "/org/nl/asml-holding-nv",
		ProductAxis:    "/product/component/optics/optical-column",
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/de/carl-zeiss-smt-gmbh"},
				{Class: ah.FragmentClassProduct, Ref: "/product/component/optics/optical-column"},
				{Class: ah.FragmentClassOrg, Ref: "/org/nl/asml-holding-nv"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:       0,
					SupplierRef: "/org/de/carl-zeiss-smt-gmbh",
					CustomerRef: "/org/nl/asml-holding-nv",
					ProductRef:  "/product/component/optics/optical-column",
				},
			},
			nil,
			nil,
		),
	}

	match, contextual := exactArtifactGapForSupplyLink(
		[]*horizonmodels.MaterializedArtifact{artifact},
		"/supply-link/global/carl-zeiss-smt-gmbh/euv-projection-optics/asml-holding-nv",
		"/org/de/carl-zeiss-smt-gmbh",
		"/org/nl/asml-holding-nv",
		"/product/component/optics/euv-projection-optics",
	)
	if match != nil {
		t.Fatalf("did not expect direct binding for surrogate artifact, got %+v", match)
	}
	if contextual == nil || contextual.Artifact == nil {
		t.Fatalf("expected contextual surrogate, got %+v", contextual)
	}
	if contextual.MatchKind != substrateMatchContextualSurrogate {
		t.Fatalf("expected contextual surrogate kind, got %+v", contextual)
	}
}

func TestMergeSubstrateCompletionsPrefersInferredOperationalFields(t *testing.T) {
	canvas := &CompleteSubstrateCanvas{}
	base := []SubstrateCompletion{
		{
			Source:        "graph",
			SupplyLinkRef: "/supply-link/global/a/product/b",
			ProductRef:    "/product/b",
			Confidence:    0.72,
		},
	}
	inferred := []SubstrateCompletion{
		{
			Source:           "llm_grounded",
			SupplyLinkRef:    "/supply-link/global/a/product/b",
			TransportViaRefs: []string{"/fac/sg/port"},
			Mode:             "sea",
			VesselType:       "container",
			Confidence:       0.88,
			Summary:          "Bound via Singapore feeder leg.",
		},
	}

	merged := canvas.mergeSubstrateCompletions(base, inferred)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged completion, got %d", len(merged))
	}
	if merged[0].Source != "llm_grounded" {
		t.Fatalf("expected inferred source to win, got %q", merged[0].Source)
	}
	if merged[0].Mode != "sea" || merged[0].VesselType != "container" {
		t.Fatalf("expected operational detail from inferred completion, got %+v", merged[0])
	}
	if merged[0].Confidence != 0.88 {
		t.Fatalf("expected higher confidence to win, got %v", merged[0].Confidence)
	}
}

func TestMergeSubstrateCompletionsIgnoresUnknownSupplyLinks(t *testing.T) {
	canvas := &CompleteSubstrateCanvas{}
	base := []SubstrateCompletion{
		{
			Source:        "graph",
			SupplyLinkRef: "/supply-link/global/a/product/b",
			ProductRef:    "/product/b",
			Confidence:    0.72,
		},
	}
	inferred := []SubstrateCompletion{
		{
			Source:        "llm_grounded",
			SupplyLinkRef: "/supply-link/global/a/product/b",
			Mode:          "air",
			Confidence:    0.88,
		},
		{
			Source:        "llm_grounded",
			SupplyLinkRef: "/supply-link/global/x/product/y",
			ProductRef:    "/product/y",
			Confidence:    0.99,
			Summary:       "Should be ignored because no such gap was requested.",
		},
	}

	merged := canvas.mergeSubstrateCompletions(base, inferred)
	if len(merged) != 1 {
		t.Fatalf("expected only known gap completions to survive, got %+v", merged)
	}
	if merged[0].SupplyLinkRef != "/supply-link/global/a/product/b" {
		t.Fatalf("unexpected merged completion %+v", merged[0])
	}
	if merged[0].Mode != "air" {
		t.Fatalf("expected known gap to absorb inferred detail, got %+v", merged[0])
	}
}
