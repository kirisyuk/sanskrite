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

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonwatch "github.com/igorkirisyuk/sanskrite/internal/horizon/watch"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type output struct {
	Lens      lensView      `json:"lens"`
	Snapshot  snapshotView  `json:"snapshot"`
	Attention attentionView `json:"attention"`
}

type lensView struct {
	ID            string            `json:"id"`
	TenantID      string            `json:"tenantId"`
	ProjectID     string            `json:"projectId"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	OriginalQuery string            `json:"originalQuery,omitempty"`
	DomainModes   []string          `json:"domainModes,omitempty"`
	ScopeCount    int               `json:"scopeCount"`
	DomainQueries []domainQueryView `json:"domainQueries,omitempty"`
}

type domainQueryView struct {
	ContractType  string `json:"contractType,omitempty"`
	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	CanonicalPath string `json:"canonicalPath,omitempty"`
}

type snapshotView struct {
	Present               bool              `json:"present"`
	BuildStatus           string            `json:"buildStatus,omitempty"`
	FreshAt               time.Time         `json:"freshAt,omitempty"`
	StaleAfterAt          time.Time         `json:"staleAfterAt,omitempty"`
	InterestTitle         string            `json:"interestTitle,omitempty"`
	InterestQuery         string            `json:"interestQuery,omitempty"`
	PageOriginalQuery     string            `json:"pageOriginalQuery,omitempty"`
	HeroTitle             string            `json:"heroTitle,omitempty"`
	HeroSummary           string            `json:"heroSummary,omitempty"`
	HeroWhyNow            string            `json:"heroWhyNow,omitempty"`
	HeroRiskPosture       string            `json:"heroRiskPosture,omitempty"`
	AnchorsCount          int               `json:"anchorsCount,omitempty"`
	ReachCount            int               `json:"reachCount,omitempty"`
	ProductsCount         int               `json:"productsCount,omitempty"`
	SupplyPathsCount      int               `json:"supplyPathsCount,omitempty"`
	RisksCount            int               `json:"risksCount,omitempty"`
	AffectedCount         int               `json:"affectedCount,omitempty"`
	DirectAffectedCount   int               `json:"directAffectedCount,omitempty"`
	IndirectAffectedCount int               `json:"indirectAffectedCount,omitempty"`
	DashboardsCount       int               `json:"dashboardsCount,omitempty"`
	MapFragmentsCount     int               `json:"mapFragmentsCount,omitempty"`
	SignalsCount          int               `json:"signalsCount,omitempty"`
	ScenariosCount        int               `json:"scenariosCount,omitempty"`
	EvidenceCount         int               `json:"evidenceCount,omitempty"`
	TopRisks              []string          `json:"topRisks,omitempty"`
	TopProducts           []string          `json:"topProducts,omitempty"`
	TopDirect             []string          `json:"topDirect,omitempty"`
	TopIndirect           []string          `json:"topIndirect,omitempty"`
	TopAffected           []string          `json:"topAffected,omitempty"`
	TopSignals            []string          `json:"topSignals,omitempty"`
	TopScenarios          []string          `json:"topScenarios,omitempty"`
	MapTitles             []string          `json:"mapTitles,omitempty"`
	EvidenceTitles        []string          `json:"evidenceTitles,omitempty"`
	SectionEnrichments    []string          `json:"sectionEnrichments,omitempty"`
	SectionSummaries      map[string]string `json:"sectionSummaries,omitempty"`
}

type attentionView struct {
	Present      bool                `json:"present"`
	GeneratedAt  time.Time           `json:"generatedAt,omitempty"`
	Subjects     []attentionSubject  `json:"subjects,omitempty"`
	Feed         []attentionFeedItem `json:"feed,omitempty"`
	SubjectCount int                 `json:"subjectCount,omitempty"`
	FeedCount    int                 `json:"feedCount,omitempty"`
}

type attentionSubject struct {
	Label               string     `json:"label"`
	AnchorCanonicalPath string     `json:"anchorCanonicalPath"`
	Status              string     `json:"status"`
	DomainModes         []string   `json:"domainModes,omitempty"`
	CoverageGaps        []string   `json:"coverageGaps,omitempty"`
	LastRunUpdatedAt    *time.Time `json:"lastRunUpdatedAt,omitempty"`
	LastTriggerReason   string     `json:"lastTriggerReason,omitempty"`
}

type attentionFeedItem struct {
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	lensID := flag.String("lens-id", "", "lens id to inspect")
	freshness := flag.Duration("freshness", 30*24*time.Hour, "freshness window when auto-selecting latest lens")
	subjectLimit := flag.Int("subject-limit", 6, "max subjects to print")
	feedLimit := flag.Int("feed-limit", 8, "max feed items to print")
	refresh := flag.Bool("refresh", false, "build and persist the lens watch page before inspecting it")
	enrichLLM := flag.Bool("enrich-llm", false, "enable LLM section enrichment during refresh")
	timeHorizon := flag.String("time-horizon", "7d", "time horizon for refresh")
	userID := flag.String("user-id", "horizon_inspect_lens_page", "user id recorded on refresh")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	lensesRepo := horizonrepo.NewLensesRepository(db)
	snapshotsRepo := horizonrepo.NewWatchPageSnapshotsRepository(db)
	historyRepo := horizonrepo.NewWatchPageHistoryRepository(db)
	agentJobsRepo := coreRepo.NewAgentJobsRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(db)
	watchService := horizonwatch.NewService(
		horizonrepo.NewWatchTargetsRepository(db),
		horizonrepo.NewWatchBriefsRepository(db),
		horizonrepo.NewWatchFeedEntriesRepository(db),
		horizonrepo.NewWatchPreferencesRepository(db),
		fragmentsRepo,
		horizonrepo.NewRiskJobsRepository(db),
		horizonrepo.NewAgentProposalsRepository(db),
		horizonrepo.NewArbitrationDecisionsRepository(db),
		coreRepo.NewOntologyFactsRepository(db),
	).WithLensesRepository(lensesRepo).
		WithPagePersistence(snapshotsRepo, historyRepo).
		WithMaterializedArtifactsRepository(materializedRepo).
		WithEdgesRepository(edgesRepo)
	watchRuntimeService := horizonwatchruntime.NewService(
		horizonrepo.NewWatchSubjectsRepository(db),
		horizonrepo.NewWatchTriggersRepository(db),
		horizonrepo.NewWatchTargetsRepository(db),
		horizonrepo.NewWatchdogConfigsRepository(db),
		horizonrepo.NewScopeAgentConfigsRepository(db),
		lensesRepo,
		horizonrepo.NewLensArtifactsRepository(db),
		fragmentsRepo,
		edgesRepo,
	).WithMaterializedArtifactsRepository(materializedRepo).
		WithMacroAgentPoliciesRepository(horizonrepo.NewMacroAgentPoliciesRepository(db)).
		WithAgentProposalsRepository(horizonrepo.NewAgentProposalsRepository(db)).
		WithArbitrationDecisionsRepository(horizonrepo.NewArbitrationDecisionsRepository(db)).
		WithWatchBriefsRepository(horizonrepo.NewWatchBriefsRepository(db)).
		WithWatchFeedEntriesRepository(horizonrepo.NewWatchFeedEntriesRepository(db)).
		WithRiskJobsRepository(horizonrepo.NewRiskJobsRepository(db)).
		WithScoutingJobsRepository(horizonrepo.NewScoutingJobsRepository(agentJobsRepo))
	watchService = watchService.WithLensAttentionProvider(func(ctx context.Context, tenantID, lensID string, limit int) (*horizonwatchruntime.LensAttentionView, error) {
		return watchRuntimeService.LensAttention(ctx, tenantID, lensID, limit)
	})
	if *enrichLLM {
		if enricher, err := buildPageEnricher(ctx, strings.TrimSpace(*projectID)); err == nil && enricher != nil {
			watchService = watchService.WithPageEnricher(enricher)
		} else if err != nil {
			log.Fatalf("init page enricher: %v", err)
		}
	}

	chosenLensID := strings.TrimSpace(*lensID)
	if chosenLensID == "" {
		chosenLensID, err = latestLensID(ctx, db, strings.TrimSpace(*tenantID), *freshness)
		if err != nil {
			log.Fatalf("select latest lens: %v", err)
		}
	}
	lens, err := lensesRepo.Get(ctx, chosenLensID)
	if err != nil {
		log.Fatalf("load lens: %v", err)
	}
	if *refresh {
		if _, err := watchService.RefreshLensPageSnapshot(ctx, strings.TrimSpace(firstNonEmpty(*tenantID, lens.TenantID)), strings.TrimSpace(*userID), chosenLensID, strings.TrimSpace(*timeHorizon)); err != nil {
			log.Fatalf("refresh lens page: %v", err)
		}
	}

	out := output{
		Lens: buildLensView(lens),
	}
	if snap, err := snapshotsRepo.GetByLens(ctx, strings.TrimSpace(firstNonEmpty(*tenantID, lens.TenantID)), chosenLensID); err == nil && snap != nil {
		out.Snapshot = buildSnapshotView(snap)
	} else {
		out.Snapshot = snapshotView{Present: false}
	}
	if attention, err := watchRuntimeService.LensAttention(ctx, strings.TrimSpace(firstNonEmpty(*tenantID, lens.TenantID)), chosenLensID, maxInt(*feedLimit, 12)); err == nil && attention != nil {
		out.Attention = buildAttentionView(attention, *subjectLimit, *feedLimit)
	} else {
		out.Attention = attentionView{Present: false}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatal(err)
	}
}

func latestLensID(ctx context.Context, db *firestore.Client, tenantID string, freshness time.Duration) (string, error) {
	if db == nil {
		return "", fmt.Errorf("firestore client is required")
	}
	q := db.Collection("horizon_lenses").Query
	if tenantID = strings.TrimSpace(tenantID); tenantID != "" && !strings.EqualFold(tenantID, "all") {
		q = q.Where("tenantId", "==", tenantID)
	}
	if freshness > 0 {
		q = q.Where("updatedAt", ">=", time.Now().UTC().Add(-freshness))
	}
	q = q.OrderBy("updatedAt", firestore.Desc).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		if tenantID != "" && !strings.EqualFold(tenantID, "all") && strings.Contains(strings.ToLower(err.Error()), "requires an index") {
			fallback := db.Collection("horizon_lenses").Query
			if freshness > 0 {
				fallback = fallback.Where("updatedAt", ">=", time.Now().UTC().Add(-freshness))
			}
			fallback = fallback.OrderBy("updatedAt", firestore.Desc).Limit(50)
			docs, err = fallback.Documents(ctx).GetAll()
			if err != nil {
				return "", err
			}
			for _, doc := range docs {
				var lens horizonmodels.Lens
				if derr := doc.DataTo(&lens); derr != nil {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(lens.TenantID), tenantID) {
					return doc.Ref.ID, nil
				}
			}
			return "", fmt.Errorf("no recent lens found for tenant %q in fallback window", tenantID)
		}
		return "", err
	}
	if len(docs) == 0 {
		return "", fmt.Errorf("no lens found")
	}
	return docs[0].Ref.ID, nil
}

func buildLensView(lens *horizonmodels.Lens) lensView {
	out := lensView{}
	if lens == nil {
		return out
	}
	out.ID = strings.TrimSpace(lens.ID)
	out.TenantID = strings.TrimSpace(lens.TenantID)
	out.ProjectID = strings.TrimSpace(lens.ProjectID)
	out.Name = strings.TrimSpace(lens.Name)
	out.Status = strings.TrimSpace(lens.Status)
	out.UpdatedAt = lens.UpdatedAt
	out.OriginalQuery = strings.TrimSpace(firstNonEmpty(lens.OriginalQuery, viewConfigString(lens.ViewConfig, "originalQuery")))
	out.DomainModes = append([]string(nil), lens.DomainModes...)
	out.ScopeCount = len(lens.Scope)
	for _, query := range horizonmodels.NormalizeLensDomainQueries(firstNonEmptyDomainQueries(lens)) {
		out.DomainQueries = append(out.DomainQueries, domainQueryView{
			ContractType:  strings.TrimSpace(query.ContractType),
			Title:         strings.TrimSpace(query.Title),
			Description:   strings.TrimSpace(query.Description),
			CanonicalPath: strings.TrimSpace(query.CanonicalPath),
		})
	}
	return out
}

func buildSnapshotView(snap *horizonmodels.WatchPageSnapshot) snapshotView {
	if snap == nil {
		return snapshotView{Present: false}
	}
	out := snapshotView{
		Present:               true,
		BuildStatus:           string(snap.BuildStatus),
		FreshAt:               snap.FreshAt,
		StaleAfterAt:          snap.StaleAfterAt,
		InterestTitle:         strings.TrimSpace(snap.InterestTitle),
		InterestQuery:         strings.TrimSpace(snap.InterestQuery),
		PageOriginalQuery:     strings.TrimSpace(anyString(snap.PageFraming["originalQuery"])),
		HeroTitle:             strings.TrimSpace(snap.Hero.Title),
		HeroSummary:           strings.TrimSpace(snap.Hero.Summary),
		HeroWhyNow:            strings.TrimSpace(snap.Hero.WhyNow),
		HeroRiskPosture:       strings.TrimSpace(snap.Hero.RiskPosture),
		AnchorsCount:          len(snap.Anchors),
		ReachCount:            len(snap.Reach),
		ProductsCount:         len(snap.Products),
		SupplyPathsCount:      len(snap.SupplyPaths),
		RisksCount:            len(snap.WhatIsAtRisk),
		AffectedCount:         len(snap.Affected),
		DirectAffectedCount:   len(snap.DirectlyAffected),
		IndirectAffectedCount: len(snap.IndirectlyAffected),
		DashboardsCount:       len(snap.Dashboards),
		MapFragmentsCount:     len(snap.MapFragments),
		SignalsCount:          len(snap.Signals),
		ScenariosCount:        len(snap.Scenarios),
		EvidenceCount:         len(snap.Evidence),
		TopRisks:              firstRiskTitles(snap.WhatIsAtRisk, 4),
		TopProducts:           firstScopeLabels(snap.Products, 5),
		TopDirect:             firstAffectedLabels(snap.DirectlyAffected, 5),
		TopIndirect:           firstAffectedLabels(snap.IndirectlyAffected, 5),
		TopAffected:           firstAffectedLabels(snap.Affected, 5),
		TopSignals:            firstSignalTitles(snap.Signals, 5),
		TopScenarios:          firstScenarioTitles(snap.Scenarios, 4),
		MapTitles:             firstMapTitles(snap.MapFragments, 4),
		EvidenceTitles:        firstEvidenceTitles(snap.Evidence, 5),
		SectionEnrichments:    firstEnrichmentKeys(snap.SectionEnrichments, 8),
		SectionSummaries:      firstEnrichmentSummaries(snap.SectionEnrichments),
	}
	return out
}

func buildAttentionView(attention *horizonwatchruntime.LensAttentionView, subjectLimit, feedLimit int) attentionView {
	if attention == nil {
		return attentionView{Present: false}
	}
	out := attentionView{
		Present:      true,
		GeneratedAt:  attention.GeneratedAt,
		SubjectCount: len(attention.Subjects),
		FeedCount:    len(attention.Feed),
	}
	for i, subject := range attention.Subjects {
		if subject == nil || (subjectLimit > 0 && i >= subjectLimit) {
			break
		}
		item := attentionSubject{
			Label:               strings.TrimSpace(subject.DisplayLabel),
			AnchorCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
			Status:              strings.TrimSpace(subject.Status),
			DomainModes:         append([]string(nil), subject.DomainModes...),
			CoverageGaps:        append([]string(nil), subject.CoverageGaps...),
		}
		if subject.LastRun != nil {
			item.LastRunUpdatedAt = &subject.LastRun.UpdatedAt
		}
		if subject.LastTrigger != nil {
			item.LastTriggerReason = strings.TrimSpace(subject.LastTrigger.TriggerReason)
		}
		out.Subjects = append(out.Subjects, item)
	}
	for i, item := range attention.Feed {
		if item == nil || (feedLimit > 0 && i >= feedLimit) {
			break
		}
		out.Feed = append(out.Feed, attentionFeedItem{
			Kind:      strings.TrimSpace(item.Kind),
			Title:     strings.TrimSpace(item.Title),
			Summary:   strings.TrimSpace(item.Summary),
			CreatedAt: item.CreatedAt,
		})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyDomainQueries(lens *horizonmodels.Lens) []horizonmodels.LensDomainQuery {
	if lens == nil {
		return nil
	}
	if len(lens.DomainQueries) > 0 {
		return lens.DomainQueries
	}
	if lens.ViewConfig == nil {
		return nil
	}
	return horizonmodels.ParseLensDomainQueries(lens.ViewConfig["domains"])
}

func viewConfigString(viewConfig map[string]interface{}, key string) string {
	if len(viewConfig) == 0 {
		return ""
	}
	if raw, ok := viewConfig[key].(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}

func anyString(raw any) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstRiskTitles(items []horizonmodels.WatchPageRiskItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		if title := strings.TrimSpace(item.Title); title != "" {
			out = append(out, title)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstScopeLabels(items []horizonmodels.WatchPageScopeItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		title := strings.TrimSpace(firstNonEmpty(item.Label, item.CanonicalPath))
		if title == "" {
			continue
		}
		out = append(out, title)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return compactUnique(out)
}

func firstAffectedLabels(items []horizonmodels.WatchPageAffectedItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		if label := strings.TrimSpace(firstNonEmpty(item.Label, item.CanonicalPath)); label != "" {
			out = append(out, label)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstSignalTitles(items []horizonmodels.WatchPageSignalItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		if title := strings.TrimSpace(item.Title); title != "" {
			out = append(out, title)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstScenarioTitles(items []horizonmodels.WatchPageScenarioItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		if title := strings.TrimSpace(item.Title); title != "" {
			out = append(out, title)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstMapTitles(items []horizonmodels.WatchPageMapFragment, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		if title := strings.TrimSpace(item.Title); title != "" {
			out = append(out, title)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstEvidenceTitles(items []horizonmodels.WatchPageEvidenceItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		if title := strings.TrimSpace(item.Title); title != "" {
			out = append(out, title)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstEnrichmentSummaries(in map[string]horizonmodels.WatchPageSectionEnrichment) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := map[string]string{}
	for _, key := range keys {
		content := strings.TrimSpace(in[key].Content)
		if content == "" {
			continue
		}
		out[key] = content
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstEnrichmentKeys(items map[string]horizonmodels.WatchPageSectionEnrichment, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, minInt(len(items), limit))
	for key := range items {
		out = append(out, key)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func compactUnique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildPageEnricher(ctx context.Context, projectID string) (horizonwatch.PageSectionEnricher, error) {
	projectID = strings.TrimSpace(projectID)
	modelManager := ai.NewAIModelManager()
	aiServices := []ai.AIService{}
	vertexLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
	if projectID != "" {
		if vertexService, err := ai.NewVertexAIService(projectID, vertexLocation, modelManager); err == nil {
			if ds := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, projectID, "VERTEX_SEARCH_DATA_STORE_ID", "vertex_search_data_store_id")); ds != "" {
				vertexService = vertexService.WithDataStore(ds)
			}
			aiServices = append(aiServices, vertexService)
		}
	}
	if geminiKey := strings.TrimSpace(firstNonEmpty(os.Getenv("GEMINI_API_KEY"), gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key"))); geminiKey != "" {
		aiServices = append(aiServices, ai.NewGeminiService(geminiKey, modelManager))
	}
	if openAIKey := strings.TrimSpace(firstNonEmpty(os.Getenv("OPENAI_API_KEY"), gcp.GetEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "openai_api_key"))); openAIKey != "" {
		aiServices = append(aiServices, ai.NewOpenAIService(openAIKey, modelManager))
	}
	if len(aiServices) == 0 {
		return nil, fmt.Errorf("no AI services available for LLM enrichment")
	}
	handler := httphandlers.NewGenAIHandler().
		WithModelManager(modelManager).
		WithAIExecutor(ai.NewAITaskExecutor(aiServices, modelManager))

	return func(ctx context.Context, input horizonwatch.PageEnrichmentInput) (*horizonwatch.PageEnrichmentOutput, error) {
		if strings.TrimSpace(input.HeroTitle) == "" && len(input.RiskItems) == 0 {
			return nil, nil
		}
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"heroTitle":         map[string]any{"type": "string"},
				"heroSummary":       map[string]any{"type": "string"},
				"heroWhyNow":        map[string]any{"type": "string"},
				"scopeSummary":      map[string]any{"type": "string"},
				"productSummary":    map[string]any{"type": "string"},
				"supplyPathSummary": map[string]any{"type": "string"},
				"riskSummary":       map[string]any{"type": "string"},
				"affectedSummary":   map[string]any{"type": "string"},
				"directSummary":     map[string]any{"type": "string"},
				"indirectSummary":   map[string]any{"type": "string"},
				"signalSummary":     map[string]any{"type": "string"},
				"scenarioSummary":   map[string]any{"type": "string"},
				"dashboardCaptions": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"mapCaptions": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
				},
			},
			"required": []string{"heroTitle", "heroSummary", "heroWhyNow", "scopeSummary", "productSummary", "supplyPathSummary", "riskSummary", "affectedSummary", "directSummary", "indirectSummary", "signalSummary", "scenarioSummary", "dashboardCaptions", "mapCaptions"},
		}
		payload, _ := json.Marshal(map[string]any{
			"lensId":            input.LensID,
			"lensName":          input.LensName,
			"timeHorizon":       input.TimeHorizon,
			"interestTitle":     input.InterestTitle,
			"interestQuery":     input.InterestQuery,
			"queryOverrides":    input.QueryOverrides,
			"heroTitle":         input.HeroTitle,
			"heroSummary":       input.HeroSummary,
			"anchorItems":       input.AnchorItems,
			"reachItems":        input.ReachItems,
			"productItems":      input.ProductItems,
			"supplyPathItems":   input.SupplyPathItems,
			"riskItems":         input.RiskItems,
			"affectedItems":     input.AffectedItems,
			"directItems":       input.DirectItems,
			"indirectItems":     input.IndirectItems,
			"signalItems":       input.SignalItems,
			"scenarioItems":     input.ScenarioItems,
			"anchorDetails":     input.AnchorDetails,
			"reachDetails":      input.ReachDetails,
			"productDetails":    input.ProductDetails,
			"supplyPathDetails": input.SupplyPathDetails,
			"riskDetails":       input.RiskDetails,
			"affectedDetails":   input.AffectedDetails,
			"directDetails":     input.DirectDetails,
			"indirectDetails":   input.IndirectDetails,
			"signalDetails":     input.SignalDetails,
			"scenarioDetails":   input.ScenarioDetails,
			"evidenceDetails":   input.EvidenceDetails,
			"dashboards":        input.Dashboards,
			"mapFragments":      input.MapFragments,
			"evidenceTitles":    input.EvidenceTitles,
		})
		req := gen_combined.GenAIRequest{
			SystemInstruction: "You are assembling a concise external teaser page for a geopolitical and supply-chain intelligence lens. Interpret only lens-specific aggregates derived from the lens scope, expanded graph reach, and user query. Keep the language clear, direct, non-technical, and evidence-aware. Do not invent facts beyond the provided data. Product summaries must explain affected products, their consuming entities, and transformed or downstream products. Direct summaries must explain who is hit first and why. Indirect summaries must explain second-order exposure. Signal summaries must aggregate events or structural signals and their origin. Scenario summaries must describe plausible operational paths forward implied by the current graph.",
			Prompt: fmt.Sprintf(
				"Return exactly one JSON object that enriches this persisted Horizon Watch page. Keep heroSummary and heroWhyNow short, dashboard captions to one sentence, and map captions to one sentence each. Use the detailed section objects, not just the item titles. Preserve the difference between direct and indirect effects, and treat global nodes as relevant only when they are reached from this lens.\n\nStructured input:\n%s",
				string(payload),
			),
			ProjectID:         strings.TrimSpace(input.ProjectID),
			Temperature:       0.2,
			MaxOutputTokens:   2200,
			StrictJSONEnabled: true,
			JSONSchema:        schema,
		}
		resp, err := handler.GenerateStructuredViaTaskExecutor(ctx, req, 5)
		if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
			return nil, err
		}
		var parsed struct {
			HeroTitle         string            `json:"heroTitle"`
			HeroSummary       string            `json:"heroSummary"`
			HeroWhyNow        string            `json:"heroWhyNow"`
			ScopeSummary      string            `json:"scopeSummary"`
			ProductSummary    string            `json:"productSummary"`
			SupplyPathSummary string            `json:"supplyPathSummary"`
			RiskSummary       string            `json:"riskSummary"`
			AffectedSummary   string            `json:"affectedSummary"`
			DirectSummary     string            `json:"directSummary"`
			IndirectSummary   string            `json:"indirectSummary"`
			SignalSummary     string            `json:"signalSummary"`
			ScenarioSummary   string            `json:"scenarioSummary"`
			DashboardCaptions map[string]string `json:"dashboardCaptions"`
			MapCaptions       map[string]string `json:"mapCaptions"`
		}
		if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
			return nil, err
		}
		return &horizonwatch.PageEnrichmentOutput{
			Model:             strings.TrimSpace(resp.Model),
			HeroTitle:         strings.TrimSpace(parsed.HeroTitle),
			HeroSummary:       strings.TrimSpace(parsed.HeroSummary),
			HeroWhyNow:        strings.TrimSpace(parsed.HeroWhyNow),
			ScopeSummary:      strings.TrimSpace(parsed.ScopeSummary),
			ProductSummary:    strings.TrimSpace(parsed.ProductSummary),
			SupplyPathSummary: strings.TrimSpace(parsed.SupplyPathSummary),
			RiskSummary:       strings.TrimSpace(parsed.RiskSummary),
			AffectedSummary:   strings.TrimSpace(parsed.AffectedSummary),
			DirectSummary:     strings.TrimSpace(parsed.DirectSummary),
			IndirectSummary:   strings.TrimSpace(parsed.IndirectSummary),
			SignalSummary:     strings.TrimSpace(parsed.SignalSummary),
			ScenarioSummary:   strings.TrimSpace(parsed.ScenarioSummary),
			DashboardCaptions: parsed.DashboardCaptions,
			MapCaptions:       parsed.MapCaptions,
			GeneratedAt:       time.Now().UTC(),
		}, nil
	}, nil
}
