package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
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
	defaultFeedsPath               = "../temp-repos/worldmonitor/src/config/feeds.ts"
	worldMonitorIngestModeDocument = "document_feed"
	defaultPollMinutes             = 30
)

var (
	reIntelSourcesBlock = regexp.MustCompile(`(?s)export\s+const\s+INTEL_SOURCES\s*:\s*Feed\[\]\s*=\s*\[(.*?)\];`)
	reIntelFeedEntry    = regexp.MustCompile(`\{\s*name:\s*'([^']+)'\s*,\s*url:\s*(?:rss|railwayRss)\('([^']+)'\)\s*(?:,\s*type:\s*'([^']+)')?\s*\}`)
)

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
}

func main() {
	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (or set GCP_PROJECT_ID)")
	feedsPath := flag.String("feeds", defaultFeedsPath, "Path to worldmonitor feeds.ts")
	apply := flag.Bool("apply", false, "Apply changes to Firestore (default: dry-run)")
	status := flag.String("status", "paused", "Source status to enforce (active|paused|completed)")
	ownerUserID := flag.String("owner-user-id", "worldmonitor-bootstrap", "Owner user ID for created sources")
	pollMinutes := flag.Int("poll-minutes", defaultPollMinutes, "Poll interval in minutes")
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
	raw, err := os.ReadFile(feedsFile)
	if err != nil {
		log.Fatalf("read feeds file: %v", err)
	}

	feeds, err := parseWorldMonitorIntelSources(string(raw))
	if err != nil {
		log.Fatalf("parse INTEL_SOURCES from %s: %v", feedsFile, err)
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

	for _, feed := range feeds {
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
				HorizonMonitorIngestMode:   strPtr(worldMonitorIngestModeDocument),
				HorizonMonitorRequiresAuth: boolPtr(false),
				PollIntervalMinutes:        intPtr(*pollMinutes),
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

	log.Printf("[%s] done: total=%d created=%d updated=%d unchanged=%d skipped=%d", mode, stats.Total, stats.Created, stats.Updated, stats.Unchanged, stats.ParseError)
}

func resolveFeedsPath(in string) string {
	if strings.TrimSpace(in) == "" {
		in = defaultFeedsPath
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
		if _, err := neturl.ParseRequestURI(rawURL); err != nil {
			continue
		}
		if _, exists := seenByURL[rawURL]; exists {
			continue
		}
		seenByURL[rawURL] = struct{}{}
		out = append(out, worldMonitorFeed{Name: name, URL: rawURL, Type: typ})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].URL < out[j].URL
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
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
	if !equalBoolPtr(dst.Details.HorizonMonitorRequiresAuth, expected.Details.HorizonMonitorRequiresAuth) {
		dst.Details.HorizonMonitorRequiresAuth = cloneBoolPtr(expected.Details.HorizonMonitorRequiresAuth)
		changed = true
	}
	if !equalIntPtr(dst.Details.PollIntervalMinutes, expected.Details.PollIntervalMinutes) {
		dst.Details.PollIntervalMinutes = cloneIntPtr(expected.Details.PollIntervalMinutes)
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
		"defense":  "/source/defense/defense-industry",
		"intl":     "/source/geopolitics/diplomacy",
		"research": "/source/science/general",
		"osint":    "/source/geopolitics/intelligence",
		"cyber":    "/source/cyber/threat-intel",
		"economic": "/source/economy/macro",
	}
	formatByType := map[string]string{
		"defense":  "industry-media",
		"intl":     "think-tank",
		"research": "research",
		"osint":    "osint",
		"cyber":    "tech-media",
		"economic": "intl-org",
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

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }
func intPtr(v int) *int       { return &v }
