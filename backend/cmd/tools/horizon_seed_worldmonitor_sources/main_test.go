package main

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestParseWorldMonitorIntelSources(t *testing.T) {
	src := `
export const INTEL_SOURCES: Feed[] = [
  { name: 'Defense One', url: rss('https://www.defenseone.com/rss/all/'), type: 'defense' },
  { name: 'Atlantic Council', url: railwayRss('https://www.atlanticcouncil.org/feed/'), type: 'intl' },
  { name: 'Defense One', url: rss('https://www.defenseone.com/rss/all/'), type: 'defense' },
];`

	feeds, err := parseWorldMonitorIntelSources(src)
	if err != nil {
		t.Fatalf("parseWorldMonitorIntelSources error: %v", err)
	}
	if len(feeds) != 2 {
		t.Fatalf("expected deduped 2 feeds, got %d", len(feeds))
	}
	if feeds[0].Name != "Atlantic Council" || feeds[1].Name != "Defense One" {
		t.Fatalf("unexpected sort/order: %+v", feeds)
	}
}

func TestParseWorldMonitorIntelSources_NoBlock(t *testing.T) {
	_, err := parseWorldMonitorIntelSources("export const OTHER=[]")
	if err == nil {
		t.Fatalf("expected parse error for missing INTEL_SOURCES block")
	}
}

func TestParseWorldMonitorIntelSources_NormalizesProblematicFeeds(t *testing.T) {
	src := `
export const INTEL_SOURCES: Feed[] = [
  { name: 'CSIS', url: rss('https://www.csis.org/analysis?type=analysis'), type: 'defense' },
  { name: 'NTI', url: rss('https://www.nti.org/rss/'), type: 'research' },
];`

	feeds, err := parseWorldMonitorIntelSources(src)
	if err != nil {
		t.Fatalf("parseWorldMonitorIntelSources error: %v", err)
	}
	if len(feeds) != 2 {
		t.Fatalf("expected 2 feeds, got %d", len(feeds))
	}
	for _, f := range feeds {
		if !strings.Contains(f.URL, "news.google.com/rss/search") {
			t.Fatalf("expected fallback Google News RSS url for %q, got %q", f.Name, f.URL)
		}
	}
}

func TestDeriveTaxonomyAssignment(t *testing.T) {
	cases := []struct {
		feed         worldMonitorFeed
		wantCategory string
		wantFormat   string
	}{
		{
			feed:         worldMonitorFeed{Name: "Defense One", URL: "https://www.defenseone.com/rss/all/", Type: "defense"},
			wantCategory: "/source/defense/defense-industry",
			wantFormat:   "industry-media",
		},
		{
			feed:         worldMonitorFeed{Name: "FAO News", URL: "https://www.fao.org/feeds/fao-newsroom-rss", Type: "economic"},
			wantCategory: "/source/economy/macro",
			wantFormat:   "intl-org",
		},
		{
			feed:         worldMonitorFeed{Name: "Unknown", URL: "https://example.com/feed.xml", Type: ""},
			wantCategory: "/source/geopolitics/intelligence",
			wantFormat:   "research",
		},
		{
			feed:         worldMonitorFeed{Name: "Gov Feed", URL: "https://example.gov/feed.xml", Type: "gov"},
			wantCategory: "/source/geopolitics/diplomacy",
			wantFormat:   "gov",
		},
		{
			feed:         worldMonitorFeed{Name: "Shipping Feed", URL: "https://example.com/rss", Type: "shipping"},
			wantCategory: "/source/geopolitics/intelligence",
			wantFormat:   "industry-media",
		},
	}

	for _, tc := range cases {
		got, err := deriveTaxonomyAssignment(tc.feed)
		if err != nil {
			t.Fatalf("deriveTaxonomyAssignment(%+v) error: %v", tc.feed, err)
		}
		if got.CategoryPath != tc.wantCategory {
			t.Fatalf("category mismatch: got %q want %q", got.CategoryPath, tc.wantCategory)
		}
		if got.FormatFacet != tc.wantFormat {
			t.Fatalf("format mismatch: got %q want %q", got.FormatFacet, tc.wantFormat)
		}
		if got.GeoFacet != "global" {
			t.Fatalf("expected global geo facet, got %q", got.GeoFacet)
		}
	}
}

func TestInferFormatFacet_Heuristics(t *testing.T) {
	feed := worldMonitorFeed{Name: "Reuters World", URL: "https://feeds.reuters.com/reuters/worldNews"}
	if got := inferFormatFacet(feed, "research"); got != "wire" {
		t.Fatalf("expected wire for Reuters, got %q", got)
	}

	feed = worldMonitorFeed{Name: "Some Dept", URL: "https://agency.gov/feed"}
	if got := inferFormatFacet(feed, "research"); got != "gov" {
		t.Fatalf("expected gov for .gov, got %q", got)
	}

	feed = worldMonitorFeed{Name: "Bellingcat", URL: "https://www.bellingcat.com/feed/"}
	if got := inferFormatFacet(feed, "research"); got != "osint" {
		t.Fatalf("expected osint for Bellingcat, got %q", got)
	}
}

func TestNormalizeStatus(t *testing.T) {
	if got := normalizeStatus(" paused "); got != "paused" {
		t.Fatalf("expected paused, got %q", got)
	}
	if got := normalizeStatus("bad"); got != "" {
		t.Fatalf("expected empty for invalid status, got %q", got)
	}
}

func TestResolveFeedsPath(t *testing.T) {
	p := resolveFeedsPath("")
	if !strings.Contains(p, "worldmonitor") {
		t.Fatalf("expected default worldmonitor path, got %q", p)
	}
}

func TestMergeWorldMonitorFeeds_DedupByURL(t *testing.T) {
	base := []worldMonitorFeed{
		{Name: "A", URL: "https://example.com/feed.xml", Type: "intl"},
	}
	extra := []worldMonitorFeed{
		{Name: "B", URL: "https://example.com/feed.xml", Type: "gov"},
		{Name: "C", URL: "https://example.net/rss", Type: "cyber"},
	}
	got := mergeWorldMonitorFeeds(base, extra)
	if len(got) != 2 {
		t.Fatalf("expected 2 merged feeds, got %d: %+v", len(got), got)
	}
}

func TestMergeExpectedSource_UpdatesDisableLLMProcessing(t *testing.T) {
	dst := &models.DataSource{
		DisplayName: "WM: test",
		Kind:        models.SourceKindRSS,
		Status:      "active",
		Details:     models.SourceDetails{},
	}
	expect := &models.DataSource{
		DisplayName: "WM: test",
		Kind:        models.SourceKindRSS,
		Status:      "active",
		Details: models.SourceDetails{
			DisableLLMProcessing: boolPtr(true),
		},
	}

	changed := mergeExpectedSource(dst, expect)
	if !changed {
		t.Fatalf("expected merge to mark changes when disableLlmProcessing differs")
	}
	if dst.Details.DisableLLMProcessing == nil || !*dst.Details.DisableLLMProcessing {
		t.Fatalf("expected disableLlmProcessing=true after merge, got %+v", dst.Details.DisableLLMProcessing)
	}
}
