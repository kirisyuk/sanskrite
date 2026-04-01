package supplychain

import (
	"strings"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestValidateNormalizedStepRejectsBroadLongJump(t *testing.T) {
	step := oneHopStep{
		SourceActorRef:   "/fac/au/pilbara/bauxite-mine",
		SourceProductRef: "/product/raw/mineral/extract/bauxite-ore",
		StepKind:         "transformation",
		TargetProductRef: "/product/finished/mechanical/assembly/electric-vehicle",
		TargetActorRef:   "/fac/de/wolfsburg/final-assembly",
		TargetActorType:  "FAC",
	}
	if err := validateNormalizedStep(step); err == nil {
		t.Fatalf("expected broad long-jump rejection")
	}
}

func TestValidateNormalizedStepAcceptsNearestAssembly(t *testing.T) {
	step := oneHopStep{
		SourceActorRef:   "/fac/kz/almaty/tire-plant",
		SourceProductRef: "/product/finished/mechanical/assembly/tire",
		StepKind:         "assembly",
		TargetProductRef: "/product/finished/mechanical/assembly/passenger-vehicle",
		TargetActorRef:   "/fac/de/wolfsburg/final-assembly",
		TargetActorType:  "FAC",
	}
	if err := validateNormalizedStep(step); err != nil {
		t.Fatalf("expected nearest assembly to be accepted, got %v", err)
	}
}

func TestValidateNormalizedStepAcceptsPassThrough(t *testing.T) {
	step := oneHopStep{
		SourceActorRef:   "/fac/us/gulf/liquefaction-train",
		SourceProductRef: "/product/intermediate/chemical/refined/liquefied-natural-gas",
		StepKind:         "pass_through",
		TargetProductRef: "/product/intermediate/chemical/refined/liquefied-natural-gas",
		TargetActorRef:   "/fac/jp/tokyo/lng-terminal",
		TargetActorType:  "FAC",
	}
	if err := validateNormalizedStep(step); err != nil {
		t.Fatalf("expected pass-through to be accepted, got %v", err)
	}
}

func TestShouldMaterializeStepRequiresFacilityToFacility(t *testing.T) {
	step := oneHopStep{StepKind: "transformation"}
	facA := &horizonmodels.HorizonFragment{Type: horizonmodels.FragmentTypeFacility}
	facB := &horizonmodels.HorizonFragment{Type: horizonmodels.FragmentTypeFacility}
	orgB := &horizonmodels.HorizonFragment{Type: horizonmodels.FragmentTypeOrganization}
	if !shouldMaterializeStep(step, facA, facB) {
		t.Fatalf("facility-to-facility step should materialize")
	}
	if shouldMaterializeStep(step, facA, orgB) {
		t.Fatalf("facility-to-org fallback must not materialize a path")
	}
}

func TestBuildPromptContainsStrictContract(t *testing.T) {
	service := &OneHopDownstreamService{}
	anchor := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeFacility,
		CanonicalPath: "/fac/nl/veldhoven/euv-system-plant",
		Labels:        map[string]string{"en": "EUV System Plant"},
	}
	ctx := &oneHopContext{
		Anchor: anchor,
		OutputProducts: []*horizonmodels.HorizonFragment{
			{Type: horizonmodels.FragmentTypeProduct, CanonicalPath: "/product/finished/electronic/assembly/euv-lithography-system", Labels: map[string]string{"en": "EUV Lithography System"}},
		},
	}
	prompt := service.buildPrompt(anchor, ctx, NormalizeOptions(OneHopDownstreamOptions{
		AnchorRef:             anchor.CanonicalPath,
		AnchorType:            string(anchor.Type),
		MaxBranchesPerProduct: 5,
		IncludeInputsContext:  true,
		AllowOrgFallback:      true,
		AllowLLMFallback:      true,
	}))
	for _, want := range []string{
		"Depth is STRICTLY one hop",
		"Allowed step kinds ONLY: pass_through, transformation, assembly",
		"ore -> vehicle",
		"tire -> vehicle",
		"semiconductor lithography tool",
		"industrial or institutional consuming facility",
		"Return EXACTLY one strict JSON object",
		"whyNearestStep",
		"whyNotBroaderStep",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
