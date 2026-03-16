package recommendations

import (
	"strings"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func TestEvaluateCoverageProfileDetectsMissingFamilies(t *testing.T) {
	profile := &strategicCoverageProfile{
		ID:               "lng_supply_chain_v1",
		Version:          "v1",
		ExpectedFamilies: strategicCoverageProfiles[1].ExpectedFamilies,
	}
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/qa/military_conflict/ras-laffan",
			Labels:        map[string]string{"en": "Ras Laffan"},
		},
		directTargets: []*horizonmodels.HorizonFragment{
			{CanonicalPath: "/product/raw/hydrocarbon/liquefaction/lng", Labels: map[string]string{"en": "Liquefied Natural Gas"}},
		},
		paths: []string{
			"/zone/qa/military_conflict/ras-laffan",
			"/product/raw/hydrocarbon/liquefaction/lng",
			"/fac/qa/ras-laffan-industrial-city",
			"/org/qa/qatarenergy",
			"/shipping/lng-carrier",
		},
		labels: []string{
			"ras laffan",
			"liquefied natural gas",
			"qatarenergy",
			"lng carrier operator",
		},
	}

	stats, missing := evaluateCoverageProfile(profile, snapshot, DetectionInput{RemoteEndpoints: 0, BusinessEndpoints: 1})
	if stats.MatchedFamilies == 0 {
		t.Fatalf("expected at least one matched family, got %#v", stats)
	}
	if len(missing) == 0 {
		t.Fatalf("expected missing families, got none")
	}
	if stats.ExpectedFamilies != len(profile.ExpectedFamilies) {
		t.Fatalf("expected families mismatch: got=%d want=%d", stats.ExpectedFamilies, len(profile.ExpectedFamilies))
	}
}

func TestNormalizeStrategicRecommendationTargetFamiliesStable(t *testing.T) {
	got := horizonrepo.NormalizeStrategicRecommendationTargetFamilies([]string{" MRI  ", "semiconductor", "mri", "Semiconductor "})
	if len(got) != 2 {
		t.Fatalf("expected 2 normalized families, got %v", got)
	}
	if got[0] != "mri" || got[1] != "semiconductor" {
		t.Fatalf("unexpected normalized families: %v", got)
	}
}

func TestMatchStrategicCoverageProfilePrefersHeliumProfile(t *testing.T) {
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/qa/military_conflict/ras-laffan",
			Labels:        map[string]string{"en": "Ras Laffan helium disruption"},
		},
		anchorPaths: []string{
			"/zone/qa/military_conflict/ras-laffan",
			"/product/raw/gas/helium",
		},
		anchorLabels: []string{
			"ras laffan helium disruption",
			"helium",
			"cryogenic export hub",
		},
		paths: []string{
			"/zone/qa/military_conflict/ras-laffan",
			"/product/raw/gas/helium",
		},
		labels: []string{
			"ras laffan",
			"helium",
			"cryogenic export hub",
		},
	}

	got := matchStrategicCoverageProfile(snapshot)
	if got == nil {
		t.Fatalf("expected profile match, got nil")
	}
	if got.ID != "helium_supply_chain_v1" {
		t.Fatalf("expected helium profile, got %q", got.ID)
	}
}

func TestMatchStrategicCoverageProfilePrefersBabElMandebProfile(t *testing.T) {
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/sea/military_conflict/bab-el-mandeb",
			Labels:        map[string]string{"en": "Bab el-Mandeb chokepoint"},
		},
		anchorPaths: []string{
			"/zone/sea/military_conflict/bab-el-mandeb",
			"/loc/int/na/bab-el-mandeb",
		},
		anchorLabels: []string{
			"bab el-mandeb chokepoint",
			"bab el-mandeb",
			"southern red sea",
		},
		paths: []string{
			"/zone/sea/military_conflict/bab-el-mandeb",
			"/loc/int/na/bab-el-mandeb",
		},
		labels: []string{
			"bab el-mandeb",
			"southern red sea",
		},
	}

	got := matchStrategicCoverageProfile(snapshot)
	if got == nil {
		t.Fatalf("expected profile match, got nil")
	}
	if got.ID != "bab_el_mandeb_chokepoint_v1" {
		t.Fatalf("expected bab-el-mandeb profile, got %q", got.ID)
	}
}

func TestMatchStrategicCoverageProfileIgnoresRemoteNeighborhoodNoise(t *testing.T) {
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/gb/cyber_disruption/united-kingdom",
			Labels:        map[string]string{"en": "United Kingdom cyber disruption"},
		},
		anchorPaths: []string{
			"/zone/gb/cyber_disruption/united-kingdom",
			"/loc/gb",
		},
		anchorLabels: []string{
			"united kingdom cyber disruption",
			"united kingdom",
		},
		paths: []string{
			"/zone/gb/cyber_disruption/united-kingdom",
			"/loc/gb",
			"/product/raw/hydrocarbon/liquefaction/lng",
			"/product/raw/gas/helium",
		},
		labels: []string{
			"united kingdom cyber disruption",
			"liquefied natural gas",
			"helium",
		},
	}

	got := matchStrategicCoverageProfile(snapshot)
	if got != nil {
		t.Fatalf("expected no profile match from remote neighborhood noise, got %q", got.ID)
	}
}

func TestEvaluateCoverageProfileRecognizesCompleteHeliumCoverage(t *testing.T) {
	profile := &strategicCoverageProfiles[0]
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/qa/military_conflict/ras-laffan",
			Labels:        map[string]string{"en": "Ras Laffan"},
		},
		paths: []string{
			"/zone/qa/military_conflict/ras-laffan",
			"/product/raw/gas/helium",
			"/hospital/imaging/mri-scanner",
			"/lab/instrumentation/nmr-spectrometer",
			"/semiconductor/fab",
			"/industrial/cryogenic-cooling-system",
		},
		labels: []string{
			"helium",
			"magnetic resonance imaging scanner",
			"nmr spectrometer",
			"semiconductor fab",
			"cryogenic cooling system",
		},
	}

	stats, missing := evaluateCoverageProfile(profile, snapshot, DetectionInput{RemoteEndpoints: 4, BusinessEndpoints: 2})
	if len(missing) != 0 {
		t.Fatalf("expected full coverage, got missing=%v", missing)
	}
	if stats.MatchedFamilies != len(profile.ExpectedFamilies) {
		t.Fatalf("expected all families matched, got %d of %d", stats.MatchedFamilies, len(profile.ExpectedFamilies))
	}
}

func TestComputeRecommendationConfidenceRewardsHotSparseCoverage(t *testing.T) {
	subject := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/qa/military_conflict/ras-laffan",
		Qualifiers: map[string]interface{}{
			"zone_hype_score": 96.0,
			"hypeStatus":      "hot",
			"conditionType":   "military_conflict",
		},
	}
	stats := horizonmodels.StrategicCoverageStats{
		ExpectedFamilies: 4,
		MatchedFamilies:  0,
	}

	got := computeRecommendationConfidence(subject, &strategicCoverageProfiles[1], stats, DetectionInput{
		RemoteEndpoints:   0,
		BusinessEndpoints: 0,
	})
	if got < 0.9 {
		t.Fatalf("expected high confidence for hot sparse coverage, got %.3f", got)
	}
}

func TestRecommendationPriorityUsesProfileHintAndHotFallback(t *testing.T) {
	if got := recommendationPriority(&strategicCoverageProfiles[0], nil); got != horizonmodels.StrategicRecommendationPriorityCritical {
		t.Fatalf("expected critical from profile hint, got %q", got)
	}

	got := recommendationPriority(nil, &horizonmodels.HorizonFragment{
		Qualifiers: map[string]interface{}{"hypeStatus": "hot"},
	})
	if got != horizonmodels.StrategicRecommendationPriorityCritical {
		t.Fatalf("expected critical from hot subject, got %q", got)
	}
}

func TestBuildEvidenceIncludesSourceSubjectAndCoverageStats(t *testing.T) {
	evidence := buildEvidence(&subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{CanonicalPath: "/zone/qa/military_conflict/ras-laffan"},
	}, DetectionInput{
		EventTitle: "Qatar remains offline due to overnight attack",
	}, horizonmodels.StrategicCoverageStats{
		DirectTargets:     3,
		MatchedFamilies:   1,
		ExpectedFamilies:  4,
		RemoteMatches:     0,
		BusinessEndpoints: 0,
	})

	joined := strings.Join(evidence, "\n")
	for _, want := range []string{
		"Source event: Qatar remains offline due to overnight attack",
		"Subject: /zone/qa/military_conflict/ras-laffan",
		"Direct targets: 3",
		"Matched families: 1/4",
		"Remote endpoints: 0",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected evidence to contain %q, got %q", want, joined)
		}
	}
}

func TestStrategicFocusFallsBackAndFormatsTargets(t *testing.T) {
	got := strategicFocus(&horizonmodels.StrategicRecommendation{
		SubjectCanonicalPath:   "/zone/qa/military_conflict/ras-laffan",
		ExpectedMissingTargets: []string{"mri_imaging", "cryogenics"},
	})
	if !strings.Contains(got, "mri_imaging, cryogenics") || !strings.Contains(got, "/zone/qa/military_conflict/ras-laffan") {
		t.Fatalf("unexpected focus string: %q", got)
	}

	got = strategicFocus(&horizonmodels.StrategicRecommendation{
		SubjectCanonicalPath: "/zone/sea/military_conflict/bab-el-mandeb",
	})
	if !strings.Contains(got, "strategic downstream dependencies") {
		t.Fatalf("expected fallback focus text, got %q", got)
	}
}

func TestBuildStrategicAnchorTermsIncludesDomainHintAndFamilies(t *testing.T) {
	got := buildStrategicAnchorTerms(&horizonmodels.StrategicRecommendation{
		SubjectCanonicalPath:   "/zone/qa/military_conflict/ras-laffan",
		DomainContractHint:     "/domain/v1/supply-chain/product/helium",
		ExpectedMissingTargets: []string{"mri_imaging", "cryogenics"},
	})
	joined := strings.Join(got, "|")
	for _, want := range []string{
		"/zone/qa/military_conflict/ras-laffan",
		"/domain/v1/supply-chain/product/helium",
		"mri imaging",
		"cryogenics",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected anchor terms to contain %q, got %v", want, got)
		}
	}
}

func TestShouldSuppressBroadSubjectRecommendationWhenDirectAnchorExists(t *testing.T) {
	profile := &strategicCoverageProfiles[2]
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/il/military_conflict/israel",
			Labels:        map[string]string{"en": "Israel"},
		},
		directTargets: []*horizonmodels.HorizonFragment{
			{
				CanonicalPath: "/loc/int/na/bab-el-mandeb",
				Labels:        map[string]string{"en": "Bab el-Mandeb"},
			},
		},
	}
	if !shouldSuppressBroadSubjectRecommendation(profile, snapshot) {
		t.Fatalf("expected suppression for broad country zone with precise direct chokepoint anchor")
	}
}

func TestShouldNotSuppressPreciseSubjectRecommendation(t *testing.T) {
	profile := &strategicCoverageProfiles[2]
	snapshot := &subjectSnapshot{
		subject: &horizonmodels.HorizonFragment{
			CanonicalPath: "/zone/sea/military_conflict/bab-el-mandeb",
			Labels:        map[string]string{"en": "Bab el-Mandeb"},
		},
		directTargets: []*horizonmodels.HorizonFragment{
			{
				CanonicalPath: "/loc/int/na/bab-el-mandeb",
				Labels:        map[string]string{"en": "Bab el-Mandeb"},
			},
		},
	}
	if shouldSuppressBroadSubjectRecommendation(profile, snapshot) {
		t.Fatalf("did not expect suppression for precise chokepoint subject")
	}
}
