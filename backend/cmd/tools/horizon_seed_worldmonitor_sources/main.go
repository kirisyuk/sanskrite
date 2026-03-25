package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	worldMonitorIngestModeOntology = "ontology_fact"
	defaultPollMinutes             = 60
	defaultDisableLLMProcessing    = true
)

var (
	reIntelSourcesBlock = regexp.MustCompile(`(?s)export\s+const\s+INTEL_SOURCES\s*:\s*Feed\[\]\s*=\s*\[(.*?)\];`)
	reIntelFeedEntry    = regexp.MustCompile(`\{\s*name:\s*'([^']+)'\s*,\s*url:\s*(?:rss|railwayRss)\('([^']+)'\)\s*(?:,\s*type:\s*'([^']+)')?\s*\}`)
)

var worldMonitorFeedNameOverrides = map[string]string{
	"csis":                  googleNewsSiteRSS("csis.org", 7),
	"middle east institute": googleNewsSiteRSS("mei.edu", 7),
	"rand":                  googleNewsSiteRSS("rand.org", 7),
	"brookings":             googleNewsSiteRSS("brookings.edu", 7),
	"carnegie":              googleNewsSiteRSS("carnegieendowment.org", 7),
	"nti":                   googleNewsSiteRSS("nti.org", 7),
}

var expandedWorldMonitorFeeds = []worldMonitorFeed{
	// Mainstream / wire (direct + Google News fallbacks)
	{Name: "BBC World", URL: "https://feeds.bbci.co.uk/news/world/rss.xml", Type: "mainstream"},
	{Name: "BBC Middle East", URL: "https://feeds.bbci.co.uk/news/world/middle_east/rss.xml", Type: "mainstream"},
	{Name: "BBC Asia", URL: "https://feeds.bbci.co.uk/news/world/asia/rss.xml", Type: "mainstream"},
	{Name: "BBC Europe", URL: "https://feeds.bbci.co.uk/news/world/europe/rss.xml", Type: "mainstream"},
	{Name: "BBC US & Canada", URL: "https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml", Type: "mainstream"},
	{Name: "Al Jazeera", URL: "https://www.aljazeera.com/xml/rss/all.xml", Type: "mainstream"},
	{Name: "NYT World", URL: "https://rss.nytimes.com/services/xml/rss/nyt/World.xml", Type: "mainstream"},
	{Name: "The Guardian World", URL: "https://www.theguardian.com/world/rss", Type: "mainstream"},
	{Name: "The Guardian Middle East", URL: "https://www.theguardian.com/world/middleeast/rss", Type: "mainstream"},
	{Name: "The Guardian Ukraine", URL: "https://www.theguardian.com/world/ukraine/rss", Type: "mainstream"},
	{Name: "France24 English", URL: "https://www.france24.com/en/rss", Type: "mainstream"},
	{Name: "France24 Middle East", URL: "https://www.france24.com/en/middle-east/rss", Type: "mainstream"},
	{Name: "AP News (site feed)", URL: googleNewsSiteRSS("apnews.com", 2), Type: "wire"},
	{Name: "Reuters (site feed)", URL: googleNewsSiteRSS("reuters.com", 3), Type: "wire"},

	// Government / institutions
	{Name: "UN News", URL: "https://news.un.org/feed/subscribe/en/news/all/rss.xml", Type: "gov"},
	{Name: "CISA Advisories", URL: "https://www.cisa.gov/cybersecurity-advisories/all.xml", Type: "cyber"},
	{Name: "US-CERT Alerts", URL: "https://www.cisa.gov/uscert/ncas/alerts.xml", Type: "cyber"},
	{Name: "DoD Releases", URL: "https://www.defense.gov/DesktopModules/ArticleCS/RSS.ashx?ContentType=1&Site=945&max=20", Type: "gov"},
	{Name: "WHO News", URL: "https://www.who.int/rss-feeds/news-english.xml", Type: "gov"},
	{Name: "UK MOD GOV.UK", URL: "https://www.gov.uk/government/organisations/ministry-of-defence.atom", Type: "gov"},
	{Name: "UK FCDO GOV.UK", URL: "https://www.gov.uk/government/organisations/foreign-commonwealth-development-office.atom", Type: "gov"},
	{Name: "State Dept (site feed)", URL: googleNewsSiteRSS("state.gov", 7), Type: "gov"},

	// Cyber + shipping + defense specialty
	{Name: "The Hacker News", URL: "https://feeds.feedburner.com/TheHackersNews", Type: "cyber"},
	{Name: "BleepingComputer", URL: "https://www.bleepingcomputer.com/feed/", Type: "cyber"},
	{Name: "SecurityWeek", URL: "https://feeds.feedburner.com/securityweek", Type: "cyber"},
	{Name: "KrebsOnSecurity", URL: "https://krebsonsecurity.com/feed/", Type: "cyber"},
	{Name: "gCaptain", URL: "https://gcaptain.com/feed/", Type: "shipping"},
	{Name: "War on the Rocks", URL: "https://warontherocks.com/feed/", Type: "defense"},
	{Name: "RealClearDefense", URL: "https://www.realcleardefense.com/index.xml", Type: "defense"},
	{Name: "USNI News", URL: "https://news.usni.org/feed", Type: "defense"},
	{Name: "Crisis Group", URL: "https://www.crisisgroup.org/rss", Type: "intl"},

	// Disaster + macro watch
	{Name: "USGS Earthquakes M4.5+", URL: "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_day.atom", Type: "disaster"},
	{Name: "ReliefWeb (site feed)", URL: googleNewsSiteRSS("reliefweb.int", 3), Type: "disaster"},
	{Name: "IMF (site feed)", URL: googleNewsSiteRSS("imf.org", 7), Type: "economic"},
	{Name: "World Bank (site feed)", URL: googleNewsSiteRSS("worldbank.org", 7), Type: "economic"},
	{Name: "Euractiv (site feed)", URL: googleNewsSiteRSS("euractiv.com", 7), Type: "intl"},
}

type worldMonitorFeed struct {
	Name string
	URL  string
	Type string
}

type taxonomyAssignment struct {
	CategoryPath string
	FormatFacet  string
	GeoFacet     string
}

type runStats struct {
	Total      int
	Created    int
	Updated    int
	Unchanged  int
	ParseError int
	SkippedBad int
}

func main() {
	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (or set GCP_PROJECT_ID)")
	feedsPath := flag.String("feeds", "", "Path to feeds.ts containing INTEL_SOURCES (required; clone HorizonMonitor/worldmonitor separately if needed)")
	apply := flag.Bool("apply", false, "Apply changes to Firestore (default: dry-run)")
	status := flag.String("status", "paused", "Source status to enforce (active|paused|completed)")
	ownerUserID := flag.String("owner-user-id", "worldmonitor-bootstrap", "Owner user ID for created sources")
	pollMinutes := flag.Int("poll-minutes", defaultPollMinutes, "Poll interval in minutes")
	includeExpanded := flag.Bool("include-expanded", true, "Include additional curated RSS sources beyond worldmonitor INTEL_SOURCES")
	verifyLive := flag.Bool("verify-live", true, "Probe feed URLs and skip non-RSS / dead endpoints")
	probeTimeoutSec := flag.Int("probe-timeout-sec", 10, "HTTP timeout in seconds for live feed probe")
	disableLLMProcessing := flag.Bool("disable-llm-processing", defaultDisableLLMProcessing, "Set sourceDetails.disableLlmProcessing for seeded sources")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		*projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT)")
	}

	normStatus := normalizeStatus(*status)
	if normStatus == "" {
		log.Fatal("--status must be active|paused|completed")
	}
	if *pollMinutes <= 0 {
		*pollMinutes = defaultPollMinutes
	}

	feedsFile := resolveFeedsPath(strings.TrimSpace(*feedsPath))
	if feedsFile == "" {
		log.Fatal("-feeds is required (path to feeds.ts with INTEL_SOURCES)")
	}
	raw, err := os.ReadFile(feedsFile)
	if err != nil {
		log.Fatalf("read feeds file: %v", err)
	}

	feeds, err := parseWorldMonitorIntelSources(string(raw))
	if err != nil {
		log.Fatalf("parse INTEL_SOURCES from %s: %v", feedsFile, err)
	}
	if *includeExpanded {
		feeds = mergeWorldMonitorFeeds(feeds, expandedWorldMonitorFeeds)
	}
	if len(feeds) == 0 {
		log.Fatalf("no INTEL_SOURCES parsed from %s", feedsFile)
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fsClient.Close()

	dsRepo := repo.NewDataSourcesRepository(fsClient.Client())
	existingByCanonical, err := loadExistingPublicSources(ctx, dsRepo)
	if err != nil {
		log.Fatalf("load existing sources: %v", err)
	}

	stats := runStats{Total: len(feeds)}
	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	log.Printf("[%s] processing %d worldmonitor INTEL_SOURCES from %s", mode, len(feeds), feedsFile)
	probeCache := map[string]bool{}
	probeTimeout := time.Duration(*probeTimeoutSec) * time.Second

	for _, feed := range feeds {
		if *verifyLive {
			url := strings.TrimSpace(feed.URL)
			ok, cached := probeCache[url]
			if !cached {
				ok = probeRSSURL(url, probeTimeout)
				probeCache[url] = ok
			}
			if !ok {
				stats.SkippedBad++
				log.Printf("skip feed=%q url=%q reason=probe_not_rss_or_unreachable", feed.Name, feed.URL)
				continue
			}
		}

		assignment, aerr := deriveTaxonomyAssignment(feed)
		if aerr != nil {
			stats.ParseError++
			log.Printf("skip feed=%q url=%q reason=%v", feed.Name, feed.URL, aerr)
			continue
		}

		now := time.Now().UTC()
		feedType := taxonomy.Slugify(feed.Name)
		if feedType == "" {
			feedType = "intel-source"
		}
		hmCategory := taxonomy.Slugify(feed.Type)
		if hmCategory == "" {
			hmCategory = "intel"
		}

		expected := &models.DataSource{
			TenantID:                  "",
			Pk:                        "global",
			DisplayName:               strings.TrimSpace(feed.Name),
			Kind:                      models.SourceKindRSS,
			Status:                    normStatus,
			SourceCategoryPaths:       []string{assignment.CategoryPath},
			PrimarySourceCategoryPath: strPtr(assignment.CategoryPath),
			SourceGeoFacets:           []string{assignment.GeoFacet},
			SourceFormatFacets:        []string{assignment.FormatFacet},
			OwnerUserID:               strings.TrimSpace(*ownerUserID),
			CreatedAt:                 now,
			UpdatedAt:                 now,
			FirstSeen:                 now,
			LastSeen:                  now,
			TrustLevel:                0.65,
			Details: models.SourceDetails{
				RSSURL:                     strPtr(feed.URL),
				Category:                   strPtr(hmCategory),
				HorizonMonitorCategory:     strPtr(hmCategory),
				HorizonMonitorFeedType:     strPtr(feedType),
				HorizonMonitorIngestMode:   strPtr(worldMonitorIngestModeOntology),
				OutputMode:                 strPtr(models.SourceOutputModeOntologyFact),
				HorizonMonitorRequiresAuth: boolPtr(false),
				PollIntervalMinutes:        intPtr(*pollMinutes),
				SourceScope:                strPtr(models.SourceScopeGlobalSystem),
				ProcessingMode:             strPtr(models.SourceProcessingModeHeuristic),
				CadencePolicy: &models.SourceCadencePolicy{
					BaseIntervalMinutes:         intPtr(*pollMinutes),
					MinIntervalMinutes:          intPtr(*pollMinutes),
					MaxIntervalMinutes:          intPtr(360),
					EmptyBackoffMultiplier:      floatPtr(2),
					ErrorBackoffMultiplier:      floatPtr(2),
					FreshnessBoostWindowMinutes: intPtr(120),
					LeaseDurationMinutes:        intPtr(90),
				},
				DisableLLMProcessing: boolPtr(*disableLLMProcessing),
			},
		}
		expected.SyncCompatForWrite()

		existing := existingByCanonical[feed.URL]
		if existing == nil {
			if *apply {
				id, cerr := dsRepo.Create(ctx, expected)
				if cerr != nil {
					log.Printf("create failed feed=%q url=%q err=%v", feed.Name, feed.URL, cerr)
					continue
				}
				log.Printf("created source id=%s kind=%s status=%s name=%q", id, expected.Kind, expected.Status, expected.DisplayName)
			} else {
				log.Printf("[dry-run] create kind=%s status=%s name=%q url=%q category=%s geo=%s format=%s", expected.Kind, expected.Status, expected.DisplayName, feed.URL, assignment.CategoryPath, assignment.GeoFacet, assignment.FormatFacet)
			}
			stats.Created++
			continue
		}

		changed := mergeExpectedSource(existing, expected)
		if !changed {
			stats.Unchanged++
			continue
		}
		if *apply {
			if uerr := dsRepo.Upsert(ctx, existing); uerr != nil {
				log.Printf("update failed id=%s name=%q err=%v", existing.ID, existing.DisplayName, uerr)
				continue
			}
			log.Printf("updated source id=%s status=%s name=%q", existing.ID, existing.Status, existing.DisplayName)
		} else {
			log.Printf("[dry-run] update id=%s name=%q status=%s", existing.ID, existing.DisplayName, existing.Status)
		}
		stats.Updated++
	}

	log.Printf("[%s] done: total=%d created=%d updated=%d unchanged=%d parseSkipped=%d probeSkipped=%d", mode, stats.Total, stats.Created, stats.Updated, stats.Unchanged, stats.ParseError, stats.SkippedBad)
}

func resolveFeedsPath(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	if filepath.IsAbs(in) {
		return filepath.Clean(in)
	}
	candidates := []string{
		filepath.Clean(in),
		filepath.Clean(filepath.Join("..", in)),
		filepath.Clean(filepath.Join("..", "..", in)),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Clean(in)
}

func parseWorldMonitorIntelSources(src string) ([]worldMonitorFeed, error) {
	blockMatch := reIntelSourcesBlock.FindStringSubmatch(src)
	if len(blockMatch) < 2 {
		return nil, errors.New("INTEL_SOURCES block not found")
	}
	block := blockMatch[1]
	matches := reIntelFeedEntry.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		return nil, errors.New("no sources parsed from INTEL_SOURCES block")
	}

	seenByURL := make(map[string]struct{}, len(matches))
	out := make([]worldMonitorFeed, 0, len(matches))
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		rawURL := strings.TrimSpace(m[2])
		typ := ""
		if len(m) > 3 {
			typ = strings.TrimSpace(m[3])
		}
		if name == "" || rawURL == "" {
			continue
		}
		normalizedURL := normalizeWorldMonitorFeedURL(worldMonitorFeed{Name: name, URL: rawURL, Type: typ})
		if _, err := neturl.ParseRequestURI(normalizedURL); err != nil {
			continue
		}
		if _, exists := seenByURL[normalizedURL]; exists {
			continue
		}
		seenByURL[normalizedURL] = struct{}{}
		out = append(out, worldMonitorFeed{Name: name, URL: normalizedURL, Type: typ})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].URL < out[j].URL
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func mergeWorldMonitorFeeds(base, extra []worldMonitorFeed) []worldMonitorFeed {
	seen := map[string]struct{}{}
	out := make([]worldMonitorFeed, 0, len(base)+len(extra))
	appendFeed := func(feed worldMonitorFeed) {
		feed.Name = strings.TrimSpace(feed.Name)
		feed.Type = strings.TrimSpace(feed.Type)
		feed.URL = normalizeWorldMonitorFeedURL(feed)
		if feed.Name == "" || strings.TrimSpace(feed.URL) == "" {
			return
		}
		key := strings.ToLower(strings.TrimSpace(feed.URL))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, feed)
	}
	for _, feed := range base {
		appendFeed(feed)
	}
	for _, feed := range extra {
		appendFeed(feed)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].URL < out[j].URL
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func normalizeWorldMonitorFeedURL(feed worldMonitorFeed) string {
	nameKey := strings.ToLower(strings.TrimSpace(feed.Name))
	if override := strings.TrimSpace(worldMonitorFeedNameOverrides[nameKey]); override != "" {
		return override
	}

	rawURL := strings.TrimSpace(feed.URL)
	if rawURL == "" {
		return ""
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed == nil {
		return rawURL
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch host {
	case "www.csis.org", "csis.org":
		return googleNewsSiteRSS("csis.org", 7)
	case "www.mei.edu", "mei.edu":
		return googleNewsSiteRSS("mei.edu", 7)
	case "www.rand.org", "rand.org":
		return googleNewsSiteRSS("rand.org", 7)
	case "www.brookings.edu", "brookings.edu":
		return googleNewsSiteRSS("brookings.edu", 7)
	case "carnegieendowment.org", "www.carnegieendowment.org":
		return googleNewsSiteRSS("carnegieendowment.org", 7)
	case "www.nti.org", "nti.org":
		return googleNewsSiteRSS("nti.org", 7)
	default:
		return rawURL
	}
}

func googleNewsSiteRSS(site string, days int) string {
	site = strings.ToLower(strings.TrimSpace(site))
	if site == "" {
		return ""
	}
	if days <= 0 {
		days = 7
	}
	q := neturl.QueryEscape(fmt.Sprintf("site:%s when:%dd", site, days))
	return "https://news.google.com/rss/search?q=" + q + "&hl=en-US&gl=US&ceid=US:en"
}

func probeRSSURL(url string, timeout time.Duration) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "sanskrite-source-seeder/1.0")
	req.Header.Set("Accept", "application/rss+xml,application/atom+xml,application/xml,text/xml;q=0.9,*/*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return false
	}
	text := strings.ToLower(string(raw))
	if strings.Contains(text, "<rss") || strings.Contains(text, "<feed") || strings.Contains(text, "<rdf:rdf") {
		return true
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(ct, "rss") || strings.Contains(ct, "atom") {
		return true
	}
	return false
}

func loadExistingPublicSources(ctx context.Context, dsRepo *repo.DataSourcesRepository) (map[string]*models.DataSource, error) {
	sources, err := dsRepo.ListByKinds(ctx, []string{models.SourceKindRSS, models.SourceKindHorizonFeed})
	if err != nil {
		return nil, err
	}
	out := make(map[string]*models.DataSource, len(sources))
	for _, ds := range sources {
		if ds == nil {
			continue
		}
		canonical := canonicalSourceURL(ds)
		if canonical == "" {
			continue
		}
		if _, exists := out[canonical]; !exists {
			out[canonical] = ds
		}
	}
	return out, nil
}

func canonicalSourceURL(ds *models.DataSource) string {
	if ds == nil {
		return ""
	}
	if ds.Details.RSSURL != nil && strings.TrimSpace(*ds.Details.RSSURL) != "" {
		return strings.TrimSpace(*ds.Details.RSSURL)
	}
	if ds.Details.URI != nil && strings.TrimSpace(*ds.Details.URI) != "" {
		return strings.TrimSpace(*ds.Details.URI)
	}
	if ds.Details.SourceURI != nil && strings.TrimSpace(*ds.Details.SourceURI) != "" {
		return strings.TrimSpace(*ds.Details.SourceURI)
	}
	return ""
}

func mergeExpectedSource(dst, expected *models.DataSource) bool {
	if dst == nil || expected == nil {
		return false
	}
	dst.SyncCompatForWrite()
	expected.SyncCompatForWrite()

	changed := false
	if strings.TrimSpace(dst.DisplayName) != strings.TrimSpace(expected.DisplayName) {
		dst.DisplayName = strings.TrimSpace(expected.DisplayName)
		changed = true
	}
	if models.NormalizeSourceKind(dst.Kind) != models.NormalizeSourceKind(expected.Kind) {
		dst.Kind = expected.Kind
		changed = true
	}
	if strings.TrimSpace(dst.Status) != strings.TrimSpace(expected.Status) {
		dst.Status = expected.Status
		changed = true
	}
	if strings.TrimSpace(dst.OwnerUserID) == "" && strings.TrimSpace(expected.OwnerUserID) != "" {
		dst.OwnerUserID = strings.TrimSpace(expected.OwnerUserID)
		changed = true
	}

	if !equalStringSlices(dst.SourceCategoryPaths, expected.SourceCategoryPaths) {
		dst.SourceCategoryPaths = append([]string{}, expected.SourceCategoryPaths...)
		changed = true
	}
	if !equalStringPtr(dst.PrimarySourceCategoryPath, expected.PrimarySourceCategoryPath) {
		dst.PrimarySourceCategoryPath = cloneStringPtr(expected.PrimarySourceCategoryPath)
		changed = true
	}
	if !equalStringSlices(dst.SourceGeoFacets, expected.SourceGeoFacets) {
		dst.SourceGeoFacets = append([]string{}, expected.SourceGeoFacets...)
		changed = true
	}
	if !equalStringSlices(dst.SourceFormatFacets, expected.SourceFormatFacets) {
		dst.SourceFormatFacets = append([]string{}, expected.SourceFormatFacets...)
		changed = true
	}

	if !equalStringPtr(dst.Details.RSSURL, expected.Details.RSSURL) {
		dst.Details.RSSURL = cloneStringPtr(expected.Details.RSSURL)
		changed = true
	}
	if !equalStringPtr(dst.Details.Category, expected.Details.Category) {
		dst.Details.Category = cloneStringPtr(expected.Details.Category)
		changed = true
	}
	if !equalStringPtr(dst.Details.HorizonMonitorCategory, expected.Details.HorizonMonitorCategory) {
		dst.Details.HorizonMonitorCategory = cloneStringPtr(expected.Details.HorizonMonitorCategory)
		changed = true
	}
	if !equalStringPtr(dst.Details.HorizonMonitorFeedType, expected.Details.HorizonMonitorFeedType) {
		dst.Details.HorizonMonitorFeedType = cloneStringPtr(expected.Details.HorizonMonitorFeedType)
		changed = true
	}
	if !equalStringPtr(dst.Details.HorizonMonitorIngestMode, expected.Details.HorizonMonitorIngestMode) {
		dst.Details.HorizonMonitorIngestMode = cloneStringPtr(expected.Details.HorizonMonitorIngestMode)
		changed = true
	}
	if !equalStringPtr(dst.Details.OutputMode, expected.Details.OutputMode) {
		dst.Details.OutputMode = cloneStringPtr(expected.Details.OutputMode)
		changed = true
	}
	if !equalBoolPtr(dst.Details.HorizonMonitorRequiresAuth, expected.Details.HorizonMonitorRequiresAuth) {
		dst.Details.HorizonMonitorRequiresAuth = cloneBoolPtr(expected.Details.HorizonMonitorRequiresAuth)
		changed = true
	}
	if !equalIntPtr(dst.Details.PollIntervalMinutes, expected.Details.PollIntervalMinutes) {
		dst.Details.PollIntervalMinutes = cloneIntPtr(expected.Details.PollIntervalMinutes)
		changed = true
	}
	if !equalStringPtr(dst.Details.SourceScope, expected.Details.SourceScope) {
		dst.Details.SourceScope = cloneStringPtr(expected.Details.SourceScope)
		changed = true
	}
	if !equalStringPtr(dst.Details.ProcessingMode, expected.Details.ProcessingMode) {
		dst.Details.ProcessingMode = cloneStringPtr(expected.Details.ProcessingMode)
		changed = true
	}
	if !equalCadencePolicy(dst.Details.CadencePolicy, expected.Details.CadencePolicy) {
		dst.Details.CadencePolicy = cloneCadencePolicy(expected.Details.CadencePolicy)
		changed = true
	}
	if !equalBoolPtr(dst.Details.DisableLLMProcessing, expected.Details.DisableLLMProcessing) {
		dst.Details.DisableLLMProcessing = cloneBoolPtr(expected.Details.DisableLLMProcessing)
		changed = true
	}

	if expected.TrustLevel > 0 && dst.TrustLevel != expected.TrustLevel {
		dst.TrustLevel = expected.TrustLevel
		changed = true
	}
	if dst.FirstSeen.IsZero() {
		dst.FirstSeen = time.Now().UTC()
		changed = true
	}

	return changed
}

func deriveTaxonomyAssignment(feed worldMonitorFeed) (taxonomyAssignment, error) {
	categoryByType := map[string]string{
		"defense":    "/source/defense/defense-industry",
		"intl":       "/source/geopolitics/diplomacy",
		"research":   "/source/science/general",
		"osint":      "/source/geopolitics/intelligence",
		"cyber":      "/source/cyber/threat-intel",
		"economic":   "/source/economy/macro",
		"gov":        "/source/geopolitics/diplomacy",
		"wire":       "/source/geopolitics/diplomacy",
		"shipping":   "/source/geopolitics/intelligence",
		"disaster":   "/source/science/general",
		"mainstream": "/source/geopolitics/diplomacy",
	}
	formatByType := map[string]string{
		"defense":    "industry-media",
		"intl":       "think-tank",
		"research":   "research",
		"osint":      "osint",
		"cyber":      "tech-media",
		"economic":   "intl-org",
		"gov":        "gov",
		"wire":       "wire",
		"shipping":   "industry-media",
		"disaster":   "intl-org",
		"mainstream": "wire",
	}

	typ := taxonomy.Slugify(feed.Type)
	rawCategory := categoryByType[typ]
	if rawCategory == "" {
		rawCategory = "/source/geopolitics/intelligence"
	}
	categoryPath, ok := taxonomy.NormalizeSourceCategoryPath(rawCategory)
	if !ok {
		return taxonomyAssignment{}, fmt.Errorf("invalid derived source category path %q", rawCategory)
	}

	rawFormat := inferFormatFacet(feed, formatByType[typ])
	formatFacet, ok := taxonomy.NormalizeSourceFormatFacet(rawFormat)
	if !ok {
		return taxonomyAssignment{}, fmt.Errorf("invalid derived source format facet %q", rawFormat)
	}

	geoFacet, ok := taxonomy.NormalizeSourceGeoFacet("global")
	if !ok {
		return taxonomyAssignment{}, errors.New("failed to normalize geo facet global")
	}

	return taxonomyAssignment{CategoryPath: categoryPath, FormatFacet: formatFacet, GeoFacet: geoFacet}, nil
}

func inferFormatFacet(feed worldMonitorFeed, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = "research"
	}
	name := strings.ToLower(strings.TrimSpace(feed.Name))
	host := ""
	if u, err := neturl.Parse(strings.TrimSpace(feed.URL)); err == nil {
		host = strings.ToLower(strings.TrimSpace(u.Hostname()))
	}

	if strings.Contains(host, ".gov") || strings.Contains(name, "state dept") {
		return "gov"
	}
	if strings.Contains(host, "reuters") || strings.Contains(host, "apnews") || strings.Contains(host, "bloomberg") {
		return "wire"
	}
	if strings.Contains(host, "fao.org") || strings.Contains(host, "un.org") || strings.Contains(host, "who.int") || strings.Contains(host, "iaea.org") {
		return "intl-org"
	}
	if strings.Contains(name, "bellingcat") {
		return "osint"
	}
	if strings.Contains(name, "csis") || strings.Contains(name, "chatham") || strings.Contains(name, "council") || strings.Contains(name, "brookings") || strings.Contains(name, "carnegie") || strings.Contains(name, "rand") {
		return "think-tank"
	}

	return fallback
}

func normalizeStatus(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "active", "paused", "completed":
		return raw
	default:
		return ""
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func equalStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return strings.TrimSpace(*a) == strings.TrimSpace(*b)
}

func equalBoolPtr(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func equalIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func equalFloatPtr(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func equalCadencePolicy(a, b *models.SourceCadencePolicy) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return equalIntPtr(a.BaseIntervalMinutes, b.BaseIntervalMinutes) &&
		equalIntPtr(a.MinIntervalMinutes, b.MinIntervalMinutes) &&
		equalIntPtr(a.MaxIntervalMinutes, b.MaxIntervalMinutes) &&
		equalFloatPtr(a.EmptyBackoffMultiplier, b.EmptyBackoffMultiplier) &&
		equalFloatPtr(a.ErrorBackoffMultiplier, b.ErrorBackoffMultiplier) &&
		equalIntPtr(a.FreshnessBoostWindowMinutes, b.FreshnessBoostWindowMinutes) &&
		equalIntPtr(a.LeaseDurationMinutes, b.LeaseDurationMinutes)
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	v := strings.TrimSpace(*in)
	return &v
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneFloatPtr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneCadencePolicy(in *models.SourceCadencePolicy) *models.SourceCadencePolicy {
	if in == nil {
		return nil
	}
	return &models.SourceCadencePolicy{
		BaseIntervalMinutes:         cloneIntPtr(in.BaseIntervalMinutes),
		MinIntervalMinutes:          cloneIntPtr(in.MinIntervalMinutes),
		MaxIntervalMinutes:          cloneIntPtr(in.MaxIntervalMinutes),
		EmptyBackoffMultiplier:      cloneFloatPtr(in.EmptyBackoffMultiplier),
		ErrorBackoffMultiplier:      cloneFloatPtr(in.ErrorBackoffMultiplier),
		FreshnessBoostWindowMinutes: cloneIntPtr(in.FreshnessBoostWindowMinutes),
		LeaseDurationMinutes:        cloneIntPtr(in.LeaseDurationMinutes),
	}
}

func strPtr(v string) *string     { return &v }
func boolPtr(v bool) *bool        { return &v }
func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
