package discovery

import "testing"

func TestAhoMatcherBoundaries(t *testing.T) {
	m := newAhoMatcher([]string{"war", "air strike", "u s"})
	if m == nil {
		t.Fatal("expected matcher")
	}
	noise := m.MatchCounts("warning signals only")
	if noise["war"] != 0 {
		t.Fatalf("expected no standalone 'war' in warning text, got %d", noise["war"])
	}
	hit := m.MatchCounts("War report: U.S. confirms air-strike near coast")
	if hit["war"] == 0 {
		t.Fatalf("expected war match, got %v", hit)
	}
	if hit["air strike"] == 0 {
		t.Fatalf("expected air strike match, got %v", hit)
	}
	if hit["u s"] == 0 {
		t.Fatalf("expected u s match, got %v", hit)
	}
}

func TestClassifyHorizonMonitorNewsKeyword_Weighted(t *testing.T) {
	nonEvent := classifyHorizonMonitorNewsKeyword("Program Manager - ReliefWeb")
	if nonEvent.Level != "info" || nonEvent.Category != "general" {
		t.Fatalf("expected non-event classification, got %+v", nonEvent)
	}

	event := classifyHorizonMonitorNewsKeyword("US Navy submarine torpedoes Iranian frigate in Indian Ocean")
	if event.Level != "high" && event.Level != "critical" {
		t.Fatalf("expected high/critical event level, got %+v", event)
	}
	if event.Category != "military" && event.Category != "conflict" {
		t.Fatalf("expected military/conflict category, got %+v", event)
	}
}

func TestInferZoneConditionAndSubtype_Weighted(t *testing.T) {
	cond, zt, sub := inferZoneConditionAndSubtype(
		normalizeKeywordText("Massive cyber breach caused internet outage across the capital"),
		horizonMonitorNewsClassification{Category: "cyber", Level: "high"},
	)
	if cond != "cyber_disruption" {
		t.Fatalf("expected cyber_disruption, got %q (%q/%q)", cond, zt, sub)
	}
	if zt != "infrastructure_disruption" {
		t.Fatalf("expected infrastructure_disruption zoneType, got %q", zt)
	}
	if sub == "" {
		t.Fatal("expected non-empty zone subtype")
	}
}

func TestClassifyHorizonMonitorNewsKeyword_USGSMagnitudeHeadline(t *testing.T) {
	title := "M 5.7 - 228 km ESE of Attu Station, Alaska"
	cls := classifyHorizonMonitorNewsKeyword(title)
	if cls.Category != "disaster" {
		t.Fatalf("expected disaster category for seismic magnitude headline, got %+v", cls)
	}
	if cls.Level != "medium" && cls.Level != "high" && cls.Level != "critical" {
		t.Fatalf("expected medium/high/critical level for seismic magnitude headline, got %+v", cls)
	}
}

func TestInferZoneConditionAndSubtype_USGSMagnitudeHeadline(t *testing.T) {
	cond, zt, sub := inferZoneConditionAndSubtype(
		normalizeKeywordText("M 6.3 - 181 km SE of Kirakira, Solomon Islands"),
		horizonMonitorNewsClassification{Category: "disaster", Level: "high"},
	)
	if cond != "earthquake_affected" {
		t.Fatalf("expected earthquake_affected for seismic magnitude headline, got %q (%q/%q)", cond, zt, sub)
	}
	if zt != "natural_disaster" {
		t.Fatalf("expected natural_disaster zoneType, got %q", zt)
	}
	if sub != "earthquake" {
		t.Fatalf("expected earthquake subtype, got %q", sub)
	}
}

func TestClassifyHorizonMonitorNewsKeyword_ExpandedCategories(t *testing.T) {
	health := classifyHorizonMonitorNewsKeyword("Cholera outbreak escalates across coastal district")
	if health.Category != "health" {
		t.Fatalf("expected health category, got %+v", health)
	}
	terror := classifyHorizonMonitorNewsKeyword("Hostage crisis after terror attack in city center")
	if terror.Category != "terrorism" && terror.Category != "conflict" {
		t.Fatalf("expected terrorism/conflict category, got %+v", terror)
	}
}
