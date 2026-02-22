package main

import "testing"

func TestPreferredTransformsDirectionAToB_ByStageOrder(t *testing.T) {
	stageOrder := map[string]int{
		"extraction": 0,
		"processing": 1,
	}
	preferAToB, ok, _ := preferredTransformsDirectionAToB(
		"/product/raw/mineral/extraction/iron-ore",
		"/product/finished/mineral/processing/iron-pellet",
		stageOrder,
	)
	if !ok {
		t.Fatalf("expected direction inference to succeed")
	}
	if !preferAToB {
		t.Fatalf("expected A->B to be preferred (extraction -> processing)")
	}
}

func TestPreferredTransformsDirectionAToB_Reversed(t *testing.T) {
	stageOrder := map[string]int{
		"extraction": 0,
		"processing": 1,
	}
	preferAToB, ok, _ := preferredTransformsDirectionAToB(
		"/product/finished/mineral/processing/iron-pellet",
		"/product/raw/mineral/extraction/iron-ore",
		stageOrder,
	)
	if !ok {
		t.Fatalf("expected direction inference to succeed")
	}
	if preferAToB {
		t.Fatalf("expected B->A to be preferred (extraction -> processing)")
	}
}

func TestPreferredTransformsDirectionAToB_UnknownStage(t *testing.T) {
	stageOrder := map[string]int{
		"extraction": 0,
		"processing": 1,
	}
	_, ok, _ := preferredTransformsDirectionAToB(
		"/product/iron-ore",
		"/product/iron-pellet",
		stageOrder,
	)
	if ok {
		t.Fatalf("expected direction inference to fail for flat/unknown stage paths")
	}
}

