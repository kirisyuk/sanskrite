package discovery

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

func TestSelectSemanticAffectedZoneCanonicalPath_PrefersGeoAndCondition(t *testing.T) {
	candidate := &affectedZoneCandidate{
		Label:         "Kharkiv (armed tension)",
		GeoToken:      "ua",
		CountryCode:   "UA",
		ConditionType: "military_conflict",
	}
	results := []ai.SearchResult{
		{
			RelevanceScore: 0.93,
			Document: ai.SearchDocument{
				StructData: map[string]interface{}{
					"canonicalPath": "/zone/ru/military_conflict/belgorod",
					"fragmentType":  "AFFECTED_ZONE",
				},
			},
		},
		{
			RelevanceScore: 0.86,
			Document: ai.SearchDocument{
				StructData: map[string]interface{}{
					"canonicalPath": "/zone/ua/military_conflict/kharkiv",
					"fragmentType":  "AFFECTED_ZONE",
				},
			},
		},
	}
	cp, score := selectSemanticAffectedZoneCanonicalPath(candidate, results)
	if cp != "/zone/ua/military_conflict/kharkiv" {
		t.Fatalf("expected UA military conflict match, got %q (score=%.3f)", cp, score)
	}
}

func TestParseZoneCanonicalPath(t *testing.T) {
	geo, condition, slug, ok := parseZoneCanonicalPath("/zone/tr-sy/military_conflict/northern-syria")
	if !ok {
		t.Fatal("expected zone canonical path to parse")
	}
	if geo != "tr-sy" || condition != "military_conflict" || slug != "northern-syria" {
		t.Fatalf("unexpected parsed values geo=%q condition=%q slug=%q", geo, condition, slug)
	}
}
