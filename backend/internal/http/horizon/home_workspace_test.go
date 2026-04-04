package horizon

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestHomeWorkspaceRankSignalSummariesPrioritizesScopeMatch(t *testing.T) {
	now := time.Now().UTC()
	ukraine := &horizonmodels.SignalLandingSummary{
		ID:               "ukraine",
		ScopeRef:         "/loc/ua",
		UpdatedAt:        now,
		TopClaims:        []string{"Odesa strike", "Black Sea port disruption"},
		TopConfirmedRefs: []string{"/loc/ua", "/fac/ua/port-of-odessa"},
		AffectedZoneRefs: []string{"/zone/ua/military_conflict/odesa"},
		Routes:           []string{"/fac/ua/port-of-odessa", "/fac/ua/izmail-port"},
	}
	gulf := &horizonmodels.SignalLandingSummary{
		ID:               "gulf",
		ScopeRef:         "/loc/sa",
		UpdatedAt:        now,
		TopClaims:        []string{"Hormuz disruption", "Ras Tanura refinery"},
		TopConfirmedRefs: []string{"/loc/sa", "/fac/sa/ras-tanura-refinery"},
		AffectedZoneRefs: []string{"/zone/int/military_conflict/strait-of-hormuz"},
		Routes:           []string{"/fac/sa/ras-tanura-refinery"},
	}

	ranked := homeWorkspaceRankSignalSummaries(
		"What disruptions are affecting southern Ukraine and the Black Sea right now, especially Odesa and Novorossiysk?",
		[]string{"/loc/ua", "/fac/ua/port-of-odessa", "/fac/ru/novorossiysk-seaport"},
		[]*horizonmodels.SignalLandingSummary{gulf, ukraine},
		2,
	)
	if len(ranked) == 0 {
		t.Fatalf("expected at least one summary, got %d", len(ranked))
	}
	if ranked[0] == nil || ranked[0].ID != "ukraine" {
		t.Fatalf("expected Ukraine summary first, got %#v", ranked[0])
	}
}

func TestFilterHomeWorkspaceSearchHitsDropsIrrelevantFreshSignalNoise(t *testing.T) {
	hits := []lensChatInternalHit{
		{
			ID:    "gulf-signal",
			Title: "Fresh signal around /fac/sa/ras-tanura-refinery",
			Kind:  "signal_landing_summary",
			Score: 2.9,
			StructData: map[string]any{
				"canonicalPaths": []string{"/loc/sa", "/zone/int/military_conflict/strait-of-hormuz"},
				"questionHints":  []string{"Hormuz disruption", "Ras Tanura refinery"},
			},
		},
		{
			ID:    "ukraine-signal",
			Title: "Fresh signal around /zone/ua/military_conflict/odesa",
			Kind:  "signal_landing_summary",
			Score: 2.2,
			StructData: map[string]any{
				"canonicalPaths": []string{"/loc/ua", "/zone/ua/military_conflict/odesa", "/fac/ua/port-of-odessa"},
				"questionHints":  []string{"Odesa strike", "Black Sea grain vessel", "Novorossiysk"},
			},
		},
	}

	filtered := filterHomeWorkspaceSearchHits(
		"How do strikes around Odesa, Izmail and Novorossiysk affect grain, ports, fuel, neon and Black Sea logistics right now?",
		hits,
		[]string{"/loc/ua", "/fac/ua/port-of-odessa", "/fac/ru/novorossiysk-seaport", "/zone/ua/military_conflict/odesa"},
		8,
	)
	if len(filtered) == 0 {
		t.Fatalf("expected at least one hit after filtering, got %d", len(filtered))
	}
	if filtered[0].ID != "ukraine-signal" {
		t.Fatalf("expected Ukraine signal first after filtering, got %q", filtered[0].ID)
	}
}
