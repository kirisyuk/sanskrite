package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrecommendations "github.com/igorkirisyuk/sanskrite/internal/horizon/recommendations"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func main() {
	var (
		projectID       = flag.String("project", "nv-horizon", "GCP project id")
		tenantID        = flag.String("tenant", "global", "tenant id")
		sourceID        = flag.String("source", "manual-worldmonitor-replay", "logical source id")
		title           = flag.String("title", "", "document title")
		summary         = flag.String("summary", "", "document summary/body")
		url             = flag.String("url", "", "source url")
		published       = flag.String("published", "", "RFC3339 or 2006-01-02 publication time")
		country         = flag.String("country", "", "ISO2 country code")
		lat             = flag.Float64("lat", 0, "latitude")
		lon             = flag.Float64("lon", 0, "longitude")
		skipPropagation = flag.Bool("skip-propagation", true, "skip signal propagation to focus on event/zone creation")
	)
	flag.Parse()

	if strings.TrimSpace(*title) == "" {
		log.Fatal("--title is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer fsClient.Close()

	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient.Client(), "v1", 10*time.Minute)
	_ = taxonomy.RefreshRuntimeTaxonomy(ctx)
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient.Client(), "v1", 20*time.Minute)
	_ = taxonomy.RefreshMarketSegmentDictionary(ctx)

	logger := log.New(os.Stdout, "[replay-worldmonitor-doc] ", log.LstdFlags|log.Lshortfile)
	factsRepo := corerepo.NewOntologyFactsRepository(fsClient.Client())
	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient.Client())
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fsClient.Client())
	lensesRepo := horizonrepo.NewLensesRepository(fsClient.Client())
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fsClient.Client())
	strategicRecommendationsRepo := horizonrepo.NewStrategicRecommendationsRepository(fsClient.Client())
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	modelManager := ai.NewAIModelManager()
	exec := newTaskExecutor(ctx, strings.TrimSpace(*projectID), modelManager)
	strategicRecommendationsService := horizonrecommendations.NewService(
		strategicRecommendationsRepo,
		fragmentsRepo,
		edgesRepo,
		scoutingJobsRepo,
		exec,
		nil,
		logger,
	)
	var profileLookup *horizonpropagation.ProfileLookup
	var propagationEngine *horizonpropagation.PropagationEngine
	if !*skipPropagation {
		profileLookup = horizonpropagation.NewProfileLookup(profilesRepo)
		if err := profileLookup.Initialize(ctx); err != nil {
			logger.Printf("profile lookup init failed, using defaults: %v", err)
		}
		propagationEngine = horizonpropagation.NewPropagationEngine(
			fragmentsRepo,
			versionsRepo,
			edgesRepo,
			lensesRepo,
			nil,
		).WithProfileLookup(profileLookup)
	}

	pipeline := discovery.NewHorizonMonitorPipeline(
		factsRepo,
		exec,
		fragmentsRepo,
		edgesRepo,
		propagationEngine,
		profileLookup,
		logger,
	).WithStrategicRecommendations(strategicRecommendationsService)

	now := time.Now().UTC()
	publishedAt := now
	if raw := strings.TrimSpace(*published); raw != "" {
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			publishedAt = ts.UTC()
		} else if ts, err := time.Parse("2006-01-02", raw); err == nil {
			publishedAt = ts.UTC()
		} else {
			log.Fatalf("invalid --published value %q: %v", raw, err)
		}
	}

	rawPayload := map[string]interface{}{
		"title":       strings.TrimSpace(*title),
		"description": strings.TrimSpace(firstNonEmpty(*summary, *title)),
		"url":         strings.TrimSpace(*url),
		"publishedAt": publishedAt.Format(time.RFC3339),
	}
	if cc := strings.ToUpper(strings.TrimSpace(*country)); cc != "" {
		rawPayload["countryCode"] = cc
	}
	if *lat != 0 || *lon != 0 {
		rawPayload["lat"] = *lat
		rawPayload["lon"] = *lon
	}
	rawJSON, err := json.Marshal(rawPayload)
	if err != nil {
		log.Fatal(err)
	}

	ds := &coremodels.DataSource{
		ID:          strings.TrimSpace(*sourceID),
		TenantID:    strings.TrimSpace(*tenantID),
		Pk:          strings.TrimSpace(*tenantID),
		DisplayName: "Manual Worldmonitor Replay",
		Kind:        "rss",
		Status:      "active",
		Details: coremodels.SourceDetails{
			OutputMode:               strPtr("ontology_fact"),
			HorizonMonitorFeedType:   strPtr("rss"),
			HorizonMonitorIngestMode: strPtr("ontology_fact"),
		},
	}

	doc := prov.NormalizedDocument{
		ExternalID:        "manual-" + shortHash(strings.Join([]string{strings.TrimSpace(*url), strings.TrimSpace(*title), publishedAt.Format(time.RFC3339)}, "|")),
		Title:             strings.TrimSpace(*title),
		URL:               strings.TrimSpace(*url),
		FullText:          string(rawJSON),
		Language:          "en",
		ContentType:       "json",
		DocumentType:      "article",
		OriginPublishedAt: &publishedAt,
	}

	if err := pipeline.ProcessDocuments(ctx, strings.TrimSpace(*tenantID), ds, strings.TrimSpace(*url), []prov.NormalizedDocument{doc}); err != nil {
		log.Fatal(err)
	}

	items, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(firstNonEmpty(*tenantID, "global")), horizonmodels.FragmentTypeAffectedZone, 500)
	if err != nil {
		log.Fatal(err)
	}
	cutoff := now.Add(-15 * time.Minute)
	found := 0
	recent := make([]*horizonmodels.HorizonFragment, 0, 12)
	for _, frag := range items {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone || frag.UpdatedAt.Before(cutoff) {
			continue
		}
		recent = append(recent, frag)
		sourceTitle := qualifierString(frag.Qualifiers, "sourceTitle")
		sourceURL := qualifierString(frag.Qualifiers, "sourceUrl")
		if strings.TrimSpace(sourceTitle) != strings.TrimSpace(*title) && strings.TrimSpace(sourceURL) != strings.TrimSpace(*url) {
			continue
		}
		found++
		fmt.Printf("zone=%s updated=%s geo=%s cond=%s hype=%0.2f coords=%s\n",
			strings.TrimSpace(frag.CanonicalPath),
			frag.UpdatedAt.UTC().Format(time.RFC3339),
			qualifierString(frag.Qualifiers, "geoPrecision"),
			qualifierString(frag.Qualifiers, "conditionType"),
			metricValue(frag.State, "zone_hype_score"),
			coordString(frag),
		)
		edgeType := horizonmodels.DependencyTypeAffects
		edges, err := edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 64)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
				continue
			}
			target, err := fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
			if err != nil || target == nil {
				continue
			}
			fmt.Printf("  affects=%s (%s)\n", strings.TrimSpace(target.CanonicalPath), target.Type)
		}
	}
	fmt.Printf("matched_zones=%d\n", found)
	pendingCount, err := strategicRecommendationsRepo.CountPendingByTenant(ctx, strings.TrimSpace(firstNonEmpty(*tenantID, "global")))
	if err != nil {
		logger.Printf("pending recommendations count failed: %v", err)
	} else {
		fmt.Printf("pending_recommendations=%d\n", pendingCount)
	}
	recommendations, err := strategicRecommendationsRepo.ListByTenant(ctx, strings.TrimSpace(firstNonEmpty(*tenantID, "global")), horizonrepo.StrategicRecommendationsListFilter{Limit: 50})
	if err != nil {
		logger.Printf("strategic recommendations list unavailable: %v", err)
	} else {
		recFound := 0
		for _, rec := range recommendations {
			if rec == nil || rec.UpdatedAt.Before(cutoff) {
				continue
			}
			if strings.TrimSpace(rec.SubjectCanonicalPath) == "" {
				continue
			}
			fmt.Printf("recommendation=%s status=%s priority=%s confidence=%.2f subject=%s missing=%s updated=%s\n",
				strings.TrimSpace(rec.ID),
				rec.Status,
				rec.Priority,
				rec.Confidence,
				strings.TrimSpace(rec.SubjectCanonicalPath),
				strings.Join(rec.ExpectedMissingTargets, ","),
				rec.UpdatedAt.UTC().Format(time.RFC3339),
			)
			recFound++
		}
		fmt.Printf("matched_recommendations=%d\n", recFound)
	}
	if found == 0 && len(recent) > 0 {
		sort.Slice(recent, func(i, j int) bool {
			return recent[i].UpdatedAt.After(recent[j].UpdatedAt)
		})
		limit := 6
		if len(recent) < limit {
			limit = len(recent)
		}
		fmt.Printf("recent_zones_window=%d\n", len(recent))
		for i := 0; i < limit; i++ {
			frag := recent[i]
			if frag == nil {
				continue
			}
			fmt.Printf("  recent=%s updated=%s cond=%s hype=%0.2f sourceTitle=%q\n",
				strings.TrimSpace(frag.CanonicalPath),
				frag.UpdatedAt.UTC().Format(time.RFC3339),
				qualifierString(frag.Qualifiers, "conditionType"),
				metricValue(frag.State, "zone_hype_score"),
				qualifierString(frag.Qualifiers, "sourceTitle"),
			)
		}
	}
}

func strPtr(v string) *string { return &v }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

func shortHash(s string) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	if s == "" {
		return "0"
	}
	var hash uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= 1099511628211
	}
	buf := make([]byte, 0, 10)
	for hash > 0 {
		buf = append(buf, alphabet[hash%36])
		hash /= 36
	}
	if len(buf) == 0 {
		return "0"
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func qualifierString(qualifiers map[string]interface{}, key string) string {
	if qualifiers == nil {
		return ""
	}
	if value, ok := qualifiers[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func metricValue(state map[string]horizonmodels.MetricValue, key string) float64 {
	if state == nil {
		return 0
	}
	if mv, ok := state[key]; ok {
		return mv.Value
	}
	return 0
}

func coordString(frag *horizonmodels.HorizonFragment) string {
	if frag == nil || frag.Coordinates == nil {
		return ""
	}
	return fmt.Sprintf("%.4f,%.4f", frag.Coordinates.Lat, frag.Coordinates.Lon)
}

func newTaskExecutor(ctx context.Context, projectID string, mm *ai.AIModelManager) *ai.AITaskExecutor {
	services := make([]ai.AIService, 0, 3)
	if apiKey := loadGeminiOrVertexAPIKey(ctx, projectID); apiKey != "" {
		_ = os.Setenv("GEMINI_API_KEY", apiKey)
		services = append(services, ai.NewGeminiService(apiKey, mm))
	}
	if openAIKey := loadOpenAIAPIKey(ctx, projectID); openAIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", openAIKey)
		services = append(services, ai.NewOpenAIService(openAIKey, mm))
	}
	if grokKey := loadGrokAPIKey(ctx, projectID); grokKey != "" {
		_ = os.Setenv("GROK_API_KEY", grokKey)
		_ = os.Setenv("XAI_API_KEY", grokKey)
		services = append(services, ai.NewGrokService(grokKey, mm))
	}
	log.Printf("replay-worldmonitor-doc: registered %d AI providers", len(services))
	return ai.NewAITaskExecutor(services, mm)
}

func loadGeminiOrVertexAPIKey(ctx context.Context, projectID string) string {
	for _, key := range []string{"GEMINI_API_KEY", "VERTEX_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	for _, name := range []string{"gemini_api_key", "GEMINI_API_KEY", "vertex_api_key", "VERTEX_API_KEY"} {
		if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func loadOpenAIAPIKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		return v
	}
	for _, name := range []string{"openai_api_key", "OPENAI_API_KEY"} {
		if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func loadGrokAPIKey(ctx context.Context, projectID string) string {
	for _, key := range []string{"GROK_API_KEY", "XAI_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	for _, name := range []string{"grok_api_key", "GROK_API_KEY", "xai_api_key", "XAI_API_KEY"} {
		if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
