package watchruntime

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildPathOverlayUniverseIncludesArtifactAndLegRefs(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		TenantID:         "global",
		Kind:             horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey:     "/supply-chain/test/path-1",
		NodeRefs:         []string{"/org/us/tesla", "/product/battery"},
		GeoRefs:          []string{"/loc/us", "/loc/cn"},
		FlowRefs:         []string{"/flow/shipping/pacific"},
		ZoneRefs:         []string{"/zone/pacific"},
		AffectedZoneRefs: []string{"/zone/red-sea"},
		Payload: map[string]any{
			"v": 1,
			"nodes": []map[string]any{
				{"type": "org", "ref": "/org/us/tesla"},
				{"type": "product", "ref": "/product/battery"},
				{"type": "org", "ref": "/org/cn/catl"},
			},
			"legs": []map[string]any{
				{
					"index":             0,
					"supplierRef":       "/org/cn/catl",
					"customerRef":       "/org/us/tesla",
					"productRef":        "/product/battery",
					"geoRefs":           []string{"/loc/cn", "/loc/us"},
					"transportViaRefs":  []string{"/route/pacific"},
					"zoneRefs":          []string{"/zone/pacific"},
					"affectedZoneRefs":  []string{"/zone/red-sea"},
					"bindingConfidence": 0.9,
				},
			},
		},
	}

	universe := buildPathOverlayUniverse(artifact)
	expected := []string{
		"/artifact/supply-chain/test/path-1",
		"/org/us/tesla",
		"/org/cn/catl",
		"/product/battery",
		"/loc/us",
		"/loc/cn",
		"/flow/shipping/pacific",
		"/zone/pacific",
		"/zone/red-sea",
		"/route/pacific",
	}
	for _, path := range expected {
		found := false
		for _, actual := range universe {
			if actual == path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected universe to include %q, got %#v", path, universe)
		}
	}
}

func TestMergeInheritedOverlayPoliciesCopiesLensSourceMixDefaults(t *testing.T) {
	subject := &horizonmodels.WatchSubject{
		Metadata: map[string]any{},
	}
	lensSubject := &horizonmodels.WatchSubject{
		Metadata: map[string]any{
			"sourceMixPolicy":  map[string]any{"lookbackDays": 14},
			"timeWindowPolicy": map[string]any{"lookbackDays": 7},
		},
	}

	mergeInheritedOverlayPolicies(subject, lensSubject, nil)

	if got := mapFromAny(subject.Metadata["sourceMixPolicy"]); got["lookbackDays"] != 14 {
		t.Fatalf("expected inherited sourceMixPolicy, got %#v", subject.Metadata["sourceMixPolicy"])
	}
	if got := mapFromAny(subject.Metadata["timeWindowPolicy"]); got["lookbackDays"] != 7 {
		t.Fatalf("expected inherited timeWindowPolicy, got %#v", subject.Metadata["timeWindowPolicy"])
	}
}

func TestSubjectKindForMacroClass(t *testing.T) {
	tests := map[string]horizonmodels.WatchSubjectKind{
		string(horizonmodels.MacroAgentClassCountry):      horizonmodels.WatchSubjectKindGeography,
		string(horizonmodels.MacroAgentClassMarket):       horizonmodels.WatchSubjectKindMarket,
		string(horizonmodels.MacroAgentClassPathOverlay):  horizonmodels.WatchSubjectKindPathOverlay,
		string(horizonmodels.MacroAgentClassEntity):       horizonmodels.WatchSubjectKindExplicitTarget,
		string(horizonmodels.MacroAgentClassLensUniverse): horizonmodels.WatchSubjectKindExplicitTarget,
	}
	for class, want := range tests {
		if got := subjectKindForMacroClass(class); got != want {
			t.Fatalf("subjectKindForMacroClass(%q) = %q, want %q", class, got, want)
		}
	}
}
