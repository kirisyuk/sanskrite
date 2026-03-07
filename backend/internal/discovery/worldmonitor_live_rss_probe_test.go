package discovery

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
)

type probeFeed struct {
	Name string
	URL  string
	Type string
}

var (
	reIntelBlock = regexp.MustCompile(`export\s+const\s+INTEL_SOURCES\s*:\s*Feed\[\]\s*=\s*\[(?s)(.*?)\];`)
	reIntelEntry = regexp.MustCompile(`\{\s*name:\s*'([^']+)'\s*,\s*url:\s*(?:rss|railwayRss)\('([^']+)'\)\s*(?:,\s*type:\s*'([^']+)')?\s*\}`)
)

// RUN_LIVE_RSS_PROBE=1 go test ./internal/discovery -run TestLiveRSSHeuristicProbe -v
func TestLiveRSSHeuristicProbe(t *testing.T) {
	if strings.TrimSpace(os.Getenv("RUN_LIVE_RSS_PROBE")) != "1" {
		t.Skip("set RUN_LIVE_RSS_PROBE=1 to run live RSS heuristic probe")
	}

	feeds, err := loadIntelFeedsForProbe()
	if err != nil {
		t.Fatalf("load feeds: %v", err)
	}
	if len(feeds) == 0 {
		t.Fatal("no feeds parsed from worldmonitor INTEL_SOURCES")
	}
	feedLimit := envInt("RSS_PROBE_FEEDS", 18)
	docLimit := envInt("RSS_PROBE_DOCS", 14)
	if feedLimit > len(feeds) {
		feedLimit = len(feeds)
	}
	feeds = feeds[:feedLimit]

	type feedStats struct {
		name       string
		url        string
		fetched    int
		events     int
		zones      int
		mapReady   int
		geocoded   int
		countrySet int
	}
	stats := make([]feedStats, 0, len(feeds))

	totalEvents := 0
	totalZones := 0
	totalMapReady := 0
	totalGeocoded := 0
	totalTyped := 0
	totalCountry := 0
	totalLevelHigh := 0
	totalCategoryKnown := 0

	rss := prov.NewRSSProvider()
	p := &HorizonMonitorPipeline{}

	for _, feed := range feeds {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		docs, ingestErr := rss.Ingest(ctx, "global", feed.URL)
		cancel()
		if ingestErr != nil {
			t.Logf("feed %q ingest error: %v", feed.Name, ingestErr)
			continue
		}
		if docLimit > 0 && len(docs) > docLimit {
			docs = docs[:docLimit]
		}
		fs := feedStats{name: feed.Name, url: feed.URL, fetched: len(docs)}
		for _, doc := range docs {
			event := p.buildEventFromDocument("probe", "intel_rss", feed.Type, "", feed.URL, doc)
			if event == nil {
				continue
			}
			fs.events++
			totalEvents++

			cls := classifyHorizonMonitorNewsKeyword(strings.Join([]string{event.Title, event.Summary}, " "))
			cls = normalizeClassificationWithSourceHint(cls, event)
			if cls.Level == "high" || cls.Level == "critical" {
				totalLevelHigh++
			}
			if strings.TrimSpace(cls.Category) != "" && strings.TrimSpace(cls.Category) != "general" {
				totalCategoryKnown++
			}

			c := inferAffectedZoneCandidate(event, cls)
			if c == nil {
				continue
			}
			fs.zones++
			totalZones++
			if strings.TrimSpace(c.ZoneType) != "" && strings.TrimSpace(c.ZoneSubType) != "" {
				totalTyped++
			}
			if strings.TrimSpace(c.CountryCode) != "" {
				fs.countrySet++
				totalCountry++
			}
			if c.HasCoordinates {
				fs.geocoded++
				totalGeocoded++
			}
			if c.HasCoordinates && c.RadiusKm > 0 && strings.TrimSpace(c.ZoneType) != "" && strings.TrimSpace(c.ZoneSubType) != "" {
				fs.mapReady++
				totalMapReady++
			}
		}
		stats = append(stats, fs)
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].mapReady == stats[j].mapReady {
			return stats[i].zones > stats[j].zones
		}
		return stats[i].mapReady > stats[j].mapReady
	})

	t.Logf("probe config: feeds=%d docs_per_feed=%d", feedLimit, docLimit)
	for i, st := range stats {
		if i >= 10 {
			break
		}
		t.Logf(
			"feed[%d] %s events=%d zones=%d map_ready=%d geocoded=%d country=%d url=%s",
			i+1, st.name, st.events, st.zones, st.mapReady, st.geocoded, st.countrySet, st.url,
		)
	}
	if totalEvents == 0 {
		t.Fatal("no events parsed from live RSS documents")
	}

	zoneCoverage := float64(totalZones) / float64(totalEvents)
	mapReadyCoverage := 0.0
	geoCoverage := 0.0
	typedCoverage := 0.0
	countryCoverage := 0.0
	if totalZones > 0 {
		mapReadyCoverage = float64(totalMapReady) / float64(totalZones)
		geoCoverage = float64(totalGeocoded) / float64(totalZones)
		typedCoverage = float64(totalTyped) / float64(totalZones)
		countryCoverage = float64(totalCountry) / float64(totalZones)
	}
	knownCategoryCoverage := float64(totalCategoryKnown) / float64(totalEvents)
	highCriticalCoverage := float64(totalLevelHigh) / float64(totalEvents)

	t.Logf(
		"summary events=%d zones=%d zone_coverage=%.3f map_ready=%.3f geocoded=%.3f typed=%.3f country=%.3f category_known=%.3f high_or_critical=%.3f",
		totalEvents,
		totalZones,
		zoneCoverage,
		mapReadyCoverage,
		geoCoverage,
		typedCoverage,
		countryCoverage,
		knownCategoryCoverage,
		highCriticalCoverage,
	)
}

func loadIntelFeedsForProbe() ([]probeFeed, error) {
	path := strings.TrimSpace(os.Getenv("RSS_PROBE_FEEDS_PATH"))
	if path == "" {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			return nil, os.ErrNotExist
		}
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../.."))
		path = filepath.Join(repoRoot, "temp-repos", "worldmonitor", "src", "config", "feeds.ts")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block := ""
	if m := reIntelBlock.FindSubmatch(raw); len(m) > 1 {
		block = string(m[1])
	}
	out := []probeFeed{}
	seen := map[string]struct{}{}
	matches := reIntelEntry.FindAllStringSubmatch(block, -1)
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		url := strings.TrimSpace(m[2])
		typ := strings.TrimSpace(m[3])
		if name == "" || url == "" {
			continue
		}
		key := strings.ToLower(url)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, probeFeed{Name: name, URL: url, Type: typ})
	}
	return out, nil
}

func envInt(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
