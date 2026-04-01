package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/google/uuid"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonhttp "github.com/igorkirisyuk/sanskrite/internal/http/horizon"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type eventRecord struct {
	Type    string         `json:"type"`
	Tenant  string         `json:"tenantId"`
	Payload map[string]any `json:"payload,omitempty"`
	At      string         `json:"at"`
}

type smokeSummary struct {
	ProjectID             string                                   `json:"projectId"`
	TenantID              string                                   `json:"tenantId"`
	LensID                string                                   `json:"lensId"`
	LensName              string                                   `json:"lensName"`
	SelectedCanonicalPath string                                   `json:"selectedCanonicalPath,omitempty"`
	Request               horizonhttp.IdentifyStressesRequest      `json:"request"`
	Debug                 *horizonhttp.IdentifyStressesDebugResult `json:"debug,omitempty"`
	Completion            map[string]any                           `json:"completion,omitempty"`
	ArtifactUpdates       []artifactUpdateSummary                  `json:"artifactUpdates,omitempty"`
	TopMechanisms         []mechanismCount                         `json:"topMechanisms,omitempty"`
	Errors                []string                                 `json:"errors,omitempty"`
}

type artifactUpdateSummary struct {
	ID               string   `json:"id,omitempty"`
	CanonicalKey     string   `json:"canonicalKey,omitempty"`
	DisplayName      string   `json:"displayName,omitempty"`
	PathStressScore  float64  `json:"pathStressScore,omitempty"`
	BindingScore     float64  `json:"bindingScore,omitempty"`
	LegCount         int      `json:"legCount,omitempty"`
	PrimaryMechanism string   `json:"primaryMechanism,omitempty"`
	StressScore      float64  `json:"stressScore,omitempty"`
	BottleneckLeg    int      `json:"bottleneckLegIndex,omitempty"`
	Flags            []string `json:"flags,omitempty"`
	DominantFactors  []string `json:"dominantFactors,omitempty"`
}

type mechanismCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type lensCandidate struct {
	Lens  *horizonmodels.Lens
	Score int
	Why   []string
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	lensID := flag.String("lens-id", "", "exact lens id (optional)")
	lensQuery := flag.String("lens-query", "australia", "substring used to auto-select lens when -lens-id is empty")
	listOnly := flag.Bool("list", false, "list active lenses and exit")
	maxNodes := flag.Int("max-nodes", 40, "max visible nodes")
	maxArtifacts := flag.Int("max-artifacts", 25, "max visible artifacts")
	maxFindings := flag.Int("max-findings", 120, "max findings")
	autoBootstrap := flag.Bool("bootstrap", true, "bootstrap missing route substrate before identify")
	bootstrapMinPaths := flag.Int("bootstrap-min-paths", 8, "bootstrap if lens has fewer than this many supply paths")
	applyChanges := flag.Bool("apply", true, "apply findings after debug normalization")
	outdir := flag.String("outdir", "", "directory for saved diagnostics (default: tmp/identify_stresses_smoke_<timestamp>)")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	logger := log.New(os.Stdout, "[identify-smoke] ", log.LstdFlags)

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	lensesRepo := horizonrepo.NewLensesRepository(db)
	if *listOnly {
		if err := listActiveLenses(ctx, db, strings.TrimSpace(*tenantID)); err != nil {
			log.Fatalf("list lenses failed: %v", err)
		}
		return
	}
	lensArtifactsRepo := horizonrepo.NewLensArtifactsRepository(db)
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)

	lens, err := resolveLens(ctx, db, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), strings.TrimSpace(*lensQuery))
	if err != nil {
		log.Fatalf("resolve lens failed: %v", err)
	}
	logger.Printf("Using lens %s (%s)", strings.TrimSpace(lens.ID), strings.TrimSpace(lens.Name))
	if *autoBootstrap {
		if err := maybeBootstrapLens(ctx, logger, strings.TrimSpace(*projectID), lens, materializedRepo, *bootstrapMinPaths); err != nil {
			logger.Printf("bootstrap warning: %v", err)
		}
	}

	scopeRequest, selectedCanonicalPath, err := buildIdentifyRequest(ctx, lens, lensArtifactsRepo, materializedRepo, *maxNodes, *maxArtifacts, *maxFindings)
	if err != nil {
		log.Fatalf("build request failed: %v", err)
	}

	outPath := strings.TrimSpace(*outdir)
	if outPath == "" {
		outPath = filepath.Join("tmp", fmt.Sprintf("identify_stresses_smoke_%s_%s", strings.TrimSpace(lens.ID), time.Now().UTC().Format("20060102_150405")))
	}
	if err := os.MkdirAll(outPath, 0o755); err != nil {
		log.Fatalf("create outdir failed: %v", err)
	}

	modelManager := ai.NewAIModelManager()
	aiServices := buildAIServices(ctx, strings.TrimSpace(*projectID), modelManager, logger)
	if len(aiServices) == 0 {
		log.Fatal("no AI services configured; need Gemini API key and/or Vertex AI access")
	}
	exec := ai.NewAITaskExecutor(aiServices, modelManager).
		WithUsageMetrics(coreRepo.NewUsageMetricsRepository(db)).
		WithCircuitBreaker(ai.NewCircuitBreaker()).
		WithAdmissionController(ai.NoopAdmissionController())

	var published []eventRecord
	publisher := func(ctx context.Context, eventType string, tenant string, payload interface{}) error {
		record := eventRecord{
			Type:   strings.TrimSpace(eventType),
			Tenant: strings.TrimSpace(tenant),
			At:     time.Now().UTC().Format(time.RFC3339Nano),
		}
		if payload != nil {
			record.Payload = toMap(payload)
		}
		published = append(published, record)
		return nil
	}

	canvas := horizonhttp.NewIdentifyStressesCanvas(horizonhttp.IdentifyStressesCanvasConfig{
		Exec:                   exec,
		FactsRepo:              coreRepo.NewOntologyFactsRepository(db),
		DataSourcesRepo:        coreRepo.NewDataSourcesRepository(db),
		FragmentsRepo:          fragmentsRepo,
		EdgesRepo:              edgesRepo,
		MaterializedRepo:       materializedRepo,
		LensesRepo:             lensesRepo,
		CapabilityProfilesRepo: horizonrepo.NewCapabilityProfilesRepository(db),
		CapabilityObsRepo:      horizonrepo.NewCapabilityObservationsRepository(db),
		SupplyLinkObsRepo:      horizonrepo.NewSupplyLinkObservationsRepository(db),
		RouteObsRepo:           horizonrepo.NewRouteObservationsRepository(db),
		DerivedRiskRepo:        horizonrepo.NewDerivedRiskProfilesRepository(db),
		Publisher:              publisher,
		Logger:                 logger,
	})

	debugResult, err := canvas.DebugIdentify(ctx, strings.TrimSpace(lens.TenantID), lens, scopeRequest)
	if err != nil {
		writeJSON(filepath.Join(outPath, "request.json"), scopeRequest)
		log.Fatalf("debug identify failed: %v", err)
	}
	logger.Printf("Debug normalize produced %d findings", len(debugResult.Findings))
	writeJSON(filepath.Join(outPath, "request.json"), scopeRequest)
	writeJSON(filepath.Join(outPath, "debug.json"), debugResult)

	jobID := "identify_smoke_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if *applyChanges {
		if err := canvas.Run(ctx, strings.TrimSpace(lens.TenantID), lens, scopeRequest, jobID); err != nil {
			writeJSON(filepath.Join(outPath, "events.json"), published)
			log.Fatalf("apply identify failed: %v", err)
		}
		logger.Printf("Apply completed for job %s", jobID)
	}

	writeJSON(filepath.Join(outPath, "events.json"), published)
	summary := buildSummary(strings.TrimSpace(*projectID), strings.TrimSpace(lens.TenantID), lens, selectedCanonicalPath, scopeRequest, debugResult, published)
	writeJSON(filepath.Join(outPath, "summary.json"), summary)
	writeText(filepath.Join(outPath, "report.md"), buildBusinessReport(summary))

	fmt.Printf("\nDiagnostics saved to %s\n", outPath)
	printSummary(summary)
}

func maybeBootstrapLens(ctx context.Context, logger *log.Logger, projectID string, lens *horizonmodels.Lens, materializedRepo *horizonrepo.MaterializedArtifactsRepository, minPaths int) error {
	if lens == nil || materializedRepo == nil || minPaths <= 0 {
		return nil
	}
	artifacts, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 500)
	if err != nil {
		return err
	}
	pathCount := 0
	for _, artifact := range artifacts {
		if artifact != nil && artifact.Kind == horizonmodels.MaterializedKindSupplyChainPath {
			pathCount++
		}
	}
	if pathCount >= minPaths {
		return nil
	}
	logger.Printf("Lens has only %d supply paths, running bootstrap prepass", pathCount)
	args := []string{
		"run", "./cmd/tools/horizon_lens_path_bootstrap",
		"-project", strings.TrimSpace(projectID),
		"-tenant", strings.TrimSpace(lens.TenantID),
		"-lens-id", strings.TrimSpace(lens.ID),
		"-repair=false",
		"-import-limit", "80",
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = backendRoot()
	output, err := cmd.CombinedOutput()
	logger.Printf("bootstrap output:\n%s", strings.TrimSpace(string(output)))
	if err != nil {
		return fmt.Errorf("bootstrap prepass failed: %w", err)
	}
	return nil
}

func buildAIServices(ctx context.Context, projectID string, modelManager *ai.AIModelManager, logger *log.Logger) []ai.AIService {
	services := []ai.AIService{}
	if strings.TrimSpace(projectID) != "" {
		vertexLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
		vertexService, err := ai.NewVertexAIService(projectID, vertexLocation, modelManager)
		if err != nil {
			logger.Printf("Vertex AI init skipped: %v", err)
		} else {
			if ds := strings.TrimSpace(firstNonEmpty(
				os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"),
				os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"),
			)); ds != "" {
				vertexService = vertexService.WithDataStore(ds)
			}
			services = append(services, vertexService)
		}
	}
	geminiKey := strings.TrimSpace(firstNonEmpty(
		os.Getenv("GEMINI_API_KEY"),
		gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key"),
	))
	if geminiKey != "" {
		services = append(services, ai.NewGeminiService(geminiKey, modelManager))
	}
	return services
}

func resolveLens(ctx context.Context, db *firestore.Client, tenantID, explicitLensID, lensQuery string) (*horizonmodels.Lens, error) {
	lensesRepo := horizonrepo.NewLensesRepository(db)
	if explicitLensID != "" {
		return lensesRepo.Get(ctx, explicitLensID)
	}

	query := db.Collection("horizon_lenses").Where("status", "==", string(horizonmodels.LensStatusActive)).Limit(500)
	if !strings.EqualFold(strings.TrimSpace(tenantID), "all") && strings.TrimSpace(tenantID) != "" {
		query = query.Where("tenantId", "==", tenantID)
	}
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("list lenses: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no active lenses found for tenant %q", tenantID)
	}

	needle := strings.ToLower(strings.TrimSpace(lensQuery))
	candidates := make([]lensCandidate, 0, len(docs))
	for _, doc := range docs {
		var lens horizonmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		score, why := scoreLens(&lens, needle)
		if score <= 0 {
			continue
		}
		candidates = append(candidates, lensCandidate{Lens: &lens, Score: score, Why: why})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no lenses matched query %q", lensQuery)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Lens.UpdatedAt.After(candidates[j].Lens.UpdatedAt)
	})
	if len(candidates) > 1 && candidates[0].Score == candidates[1].Score {
		return nil, fmt.Errorf("lens query %q is ambiguous: %s (%d), %s (%d)", lensQuery, candidates[0].Lens.Name, candidates[0].Score, candidates[1].Lens.Name, candidates[1].Score)
	}
	return candidates[0].Lens, nil
}

func listActiveLenses(ctx context.Context, db *firestore.Client, tenantID string) error {
	query := db.Collection("horizon_lenses").
		Where("status", "==", string(horizonmodels.LensStatusActive)).
		Limit(500)
	if !strings.EqualFold(strings.TrimSpace(tenantID), "all") && strings.TrimSpace(tenantID) != "" {
		query = query.Where("tenantId", "==", tenantID)
	}
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	type row struct {
		ID          string
		TenantID    string
		Name        string
		ProjectID   string
		UpdatedAt   time.Time
		ScopeCount  int
		ScopeSample []string
	}
	rows := make([]row, 0, len(docs))
	for _, doc := range docs {
		var lens horizonmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		sample := make([]string, 0, 3)
		for _, ref := range lens.Scope {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			sample = append(sample, ref)
			if len(sample) >= 3 {
				break
			}
		}
		rows = append(rows, row{
			ID:          strings.TrimSpace(lens.ID),
			TenantID:    strings.TrimSpace(lens.TenantID),
			Name:        strings.TrimSpace(lens.Name),
			ProjectID:   strings.TrimSpace(lens.ProjectID),
			UpdatedAt:   lens.UpdatedAt,
			ScopeCount:  len(lens.Scope),
			ScopeSample: sample,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt.After(rows[j].UpdatedAt) })
	for _, row := range rows {
		fmt.Printf("%s | tenant=%s | %s | project=%s | updated=%s | scope=%d | sample=%s\n",
			row.ID,
			firstNonEmpty(row.TenantID, tenantID),
			row.Name,
			row.ProjectID,
			row.UpdatedAt.Format(time.RFC3339),
			row.ScopeCount,
			strings.Join(row.ScopeSample, ", "),
		)
	}
	return nil
}

func scoreLens(lens *horizonmodels.Lens, needle string) (int, []string) {
	if lens == nil || needle == "" {
		return 0, nil
	}
	score := 0
	var why []string
	add := func(points int, reason string) {
		score += points
		why = append(why, reason)
	}
	name := strings.ToLower(strings.TrimSpace(lens.Name))
	query := strings.ToLower(strings.TrimSpace(lens.OriginalQuery))
	project := strings.ToLower(strings.TrimSpace(lens.ProjectID))
	if strings.Contains(name, needle) {
		add(100, "name")
	}
	if strings.Contains(query, needle) {
		add(80, "originalQuery")
	}
	if strings.Contains(project, needle) {
		add(40, "projectId")
	}
	for _, ref := range lens.Scope {
		refLower := strings.ToLower(strings.TrimSpace(ref))
		switch {
		case refLower == "/loc/au":
			add(90, "scope:/loc/au")
		case strings.Contains(refLower, needle):
			add(30, "scope")
		}
	}
	for _, artifact := range lens.ScopeArtifacts {
		text := strings.ToLower(strings.Join([]string{
			artifact.DisplayName,
			artifact.AnchorSupplier,
			artifact.AnchorCustomer,
			artifact.ProductAxis,
		}, " "))
		if strings.Contains(text, needle) {
			add(20, "scopeArtifacts")
		}
	}
	return score, why
}

func buildIdentifyRequest(
	ctx context.Context,
	lens *horizonmodels.Lens,
	lensArtifactsRepo *horizonrepo.LensArtifactsRepository,
	materializedRepo *horizonrepo.MaterializedArtifactsRepository,
	maxNodes, maxArtifacts, maxFindings int,
) (horizonhttp.IdentifyStressesRequest, string, error) {
	materialized, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 500)
	if err != nil {
		return horizonhttp.IdentifyStressesRequest{}, "", err
	}
	lensArtifacts, err := lensArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 2000)
	if err != nil {
		return horizonhttp.IdentifyStressesRequest{}, "", err
	}

	supplyPaths := make([]*horizonmodels.MaterializedArtifact, 0, len(materialized))
	for _, artifact := range materialized {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		enrichArtifactSummary(artifact)
		supplyPaths = append(supplyPaths, artifact)
	}
	sort.Slice(supplyPaths, func(i, j int) bool {
		leftScore := supplyPathStressScore(supplyPaths[i])
		rightScore := supplyPathStressScore(supplyPaths[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return supplyPaths[i].UpdatedAt.After(supplyPaths[j].UpdatedAt)
	})
	if len(supplyPaths) > maxArtifacts {
		supplyPaths = supplyPaths[:maxArtifacts]
	}

	selectedCanonicalPath := pickSelectedCanonicalPath(lens, supplyPaths)
	nodeRefs := collectVisibleNodeRefs(lens, lensArtifacts, supplyPaths, selectedCanonicalPath, maxNodes)
	artifactKeys := collectArtifactKeys(supplyPaths)
	supplyLinkRefs := collectArtifactSupplyLinkRefs(supplyPaths)
	relations := collectVisibleRelations(lensArtifacts, supplyPaths)

	return horizonhttp.IdentifyStressesRequest{
		ScopeMode:             "visible_set",
		SelectedCanonicalPath: selectedCanonicalPath,
		VisibleNodeRefs:       nodeRefs,
		VisibleArtifactKeys:   artifactKeys,
		VisibleSupplyLinkRefs: supplyLinkRefs,
		VisibleRelations:      relations,
		ClientViewID:          "horizon_identify_stresses_smoke",
		MaxNodes:              maxNodes,
		MaxArtifacts:          maxArtifacts,
		MaxFindings:           maxFindings,
	}, selectedCanonicalPath, nil
}

func pickSelectedCanonicalPath(lens *horizonmodels.Lens, supplyPaths []*horizonmodels.MaterializedArtifact) string {
	candidates := map[string]int{}
	push := func(ref string, weight int) {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "/artifact/") || strings.HasPrefix(ref, "/query/") || strings.HasPrefix(ref, "/domain/") || strings.HasPrefix(ref, "/supply-link/") {
			return
		}
		candidates[ref] += weight
	}
	for _, ref := range lens.Scope {
		push(ref, 100)
	}
	for _, artifact := range supplyPaths {
		if artifact == nil {
			continue
		}
		push(artifact.AnchorSupplier, 60)
		push(artifact.AnchorCustomer, 60)
		for _, ref := range artifact.NodeRefs {
			push(ref, 12)
		}
	}
	type row struct {
		ref   string
		score int
	}
	rows := make([]row, 0, len(candidates))
	for ref, score := range candidates {
		rows = append(rows, row{ref: ref, score: score})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].ref < rows[j].ref
	})
	if len(rows) == 0 {
		return ""
	}
	return rows[0].ref
}

func collectVisibleNodeRefs(lens *horizonmodels.Lens, lensArtifacts []*horizonmodels.LensArtifact, supplyPaths []*horizonmodels.MaterializedArtifact, selected string, limit int) []string {
	counts := map[string]int{}
	push := func(ref string, weight int) {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "/artifact/") || strings.HasPrefix(ref, "/query/") || strings.HasPrefix(ref, "/domain/") {
			return
		}
		counts[ref] += weight
	}
	push(selected, 1000)
	for _, ref := range lens.Scope {
		push(ref, 50)
	}
	for _, artifact := range lensArtifacts {
		if artifact == nil {
			continue
		}
		if artifact.Type == horizonmodels.LensArtifactTypeFragment {
			push(artifact.CanonicalPath, 35)
		}
		if artifact.Type == horizonmodels.LensArtifactTypeEdge {
			push(artifact.SourcePath, 20)
			push(artifact.TargetPath, 20)
		}
	}
	for _, artifact := range supplyPaths {
		if artifact == nil {
			continue
		}
		weight := 10 + int(supplyPathStressScore(artifact)*100)
		for _, ref := range artifact.NodeRefs {
			push(ref, weight)
		}
	}
	type row struct {
		Ref   string
		Score int
	}
	rows := make([]row, 0, len(counts))
	for ref, score := range counts {
		rows = append(rows, row{Ref: ref, Score: score})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Ref < rows[j].Ref
	})
	out := make([]string, 0, minInt(limit, len(rows)))
	for _, row := range rows {
		out = append(out, row.Ref)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func collectArtifactKeys(artifacts []*horizonmodels.MaterializedArtifact) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		key := strings.TrimSpace(firstNonEmpty(artifact.CanonicalKey, artifact.ID))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func collectArtifactSupplyLinkRefs(artifacts []*horizonmodels.MaterializedArtifact) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	push := func(value string) {
		ref := strings.TrimSpace(value)
		if ref == "" || !strings.HasPrefix(ref, "/supply-link/") {
			return
		}
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		for _, ref := range artifact.NodeRefs {
			push(ref)
		}
		for _, ref := range artifact.MatchedRefs {
			push(ref)
		}
		if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			for _, node := range parsed.Nodes {
				push(node.Ref)
			}
			for _, edge := range parsed.Edges {
				push(edge.SupplyLinkCanonicalPath)
				push(edge.SupplyLinkID)
				push(stringValue(edge.Qualifiers, "supplyLinkCanonicalPath"))
			}
			for _, leg := range parsed.Legs {
				push(leg.SupplyLinkCanonicalPath)
				push(leg.SupplyLinkID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func collectVisibleRelations(lensArtifacts []*horizonmodels.LensArtifact, supplyPaths []*horizonmodels.MaterializedArtifact) []horizonhttp.IdentifyStressVisibleRelation {
	seen := map[string]struct{}{}
	out := make([]horizonhttp.IdentifyStressVisibleRelation, 0, len(lensArtifacts))
	push := func(rel horizonhttp.IdentifyStressVisibleRelation) {
		key := strings.Join([]string{
			strings.TrimSpace(rel.EdgeID),
			strings.TrimSpace(rel.SourceCanonicalPath),
			strings.TrimSpace(rel.TargetCanonicalPath),
			strings.TrimSpace(rel.RelationType),
			strings.TrimSpace(rel.RelationRole),
		}, "|")
		if key == "||||" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, rel)
	}

	for _, artifact := range lensArtifacts {
		if artifact == nil || artifact.Type != horizonmodels.LensArtifactTypeEdge {
			continue
		}
		push(horizonhttp.IdentifyStressVisibleRelation{
			EdgeID:              strings.TrimSpace(artifact.EdgeID),
			SourceCanonicalPath: strings.TrimSpace(artifact.SourcePath),
			TargetCanonicalPath: strings.TrimSpace(artifact.TargetPath),
			RelationType:        strings.TrimSpace(artifact.EdgeType),
			RelationFamily:      inferRelationFamily(strings.TrimSpace(artifact.EdgeType)),
			RelationRole:        "lens_edge",
		})
	}
	for _, artifact := range supplyPaths {
		if artifact == nil {
			continue
		}
		if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			for _, leg := range parsed.Legs {
				push(horizonhttp.IdentifyStressVisibleRelation{
					EdgeID:              strings.TrimSpace(firstNonEmpty(leg.SupplyLinkID, fmt.Sprintf("%s#leg:%d", artifact.CanonicalKey, leg.Index))),
					SourceCanonicalPath: strings.TrimSpace(leg.SupplierRef),
					TargetCanonicalPath: strings.TrimSpace(leg.CustomerRef),
					RelationType:        "SUPPLY_PATH_LEG",
					RelationFamily:      "supply_path",
					RelationRole:        "path_leg",
					BusinessMeaning:     strings.TrimSpace(artifact.DisplayName),
					Criticality:         legStressScore(leg),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join([]string{out[i].RelationRole, out[i].RelationType, out[i].SourceCanonicalPath, out[i].TargetCanonicalPath}, "|") <
			strings.Join([]string{out[j].RelationRole, out[j].RelationType, out[j].SourceCanonicalPath, out[j].TargetCanonicalPath}, "|")
	})
	return out
}

func inferRelationFamily(edgeType string) string {
	edgeType = strings.ToUpper(strings.TrimSpace(edgeType))
	switch edgeType {
	case "SUPPLIES_TO", "SUPPLY_LINK", "PRODUCES", "DELIVERS_TO":
		return "supply_chain"
	case "DEPENDS_ON", "RELIES_ON", "USES":
		return "dependency"
	default:
		return "graph"
	}
}

func buildSummary(projectID, tenantID string, lens *horizonmodels.Lens, selected string, req horizonhttp.IdentifyStressesRequest, debug *horizonhttp.IdentifyStressesDebugResult, events []eventRecord) smokeSummary {
	summary := smokeSummary{
		ProjectID:             projectID,
		TenantID:              tenantID,
		LensID:                strings.TrimSpace(lens.ID),
		LensName:              strings.TrimSpace(lens.Name),
		SelectedCanonicalPath: selected,
		Request:               req,
		Debug:                 debug,
	}
	mechanisms := map[string]int{}
	artifactByKey := map[string]artifactUpdateSummary{}
	for _, event := range events {
		switch event.Type {
		case "horizon.identify_stresses.completed":
			summary.Completion = event.Payload
			if errs := stringSliceAny(event.Payload["errors"]); len(errs) > 0 {
				summary.Errors = append(summary.Errors, errs...)
			}
		case "horizon.identify_stresses.failed":
			summary.Errors = append(summary.Errors, strings.TrimSpace(stringValue(event.Payload, "error")))
		case "horizon.identify_stresses.artifact_upserted":
			artifact := summarizeArtifactEvent(event.Payload)
			key := firstNonEmpty(artifact.CanonicalKey, artifact.ID)
			if key != "" {
				artifactByKey[key] = artifact
			}
			if artifact.PrimaryMechanism != "" {
				mechanisms[artifact.PrimaryMechanism]++
			}
		}
	}
	for _, artifact := range artifactByKey {
		summary.ArtifactUpdates = append(summary.ArtifactUpdates, artifact)
	}
	sort.Slice(summary.ArtifactUpdates, func(i, j int) bool {
		if summary.ArtifactUpdates[i].PathStressScore != summary.ArtifactUpdates[j].PathStressScore {
			return summary.ArtifactUpdates[i].PathStressScore > summary.ArtifactUpdates[j].PathStressScore
		}
		return summary.ArtifactUpdates[i].DisplayName < summary.ArtifactUpdates[j].DisplayName
	})
	for key, count := range mechanisms {
		summary.TopMechanisms = append(summary.TopMechanisms, mechanismCount{Key: key, Count: count})
	}
	sort.Slice(summary.TopMechanisms, func(i, j int) bool {
		if summary.TopMechanisms[i].Count != summary.TopMechanisms[j].Count {
			return summary.TopMechanisms[i].Count > summary.TopMechanisms[j].Count
		}
		return summary.TopMechanisms[i].Key < summary.TopMechanisms[j].Key
	})
	return summary
}

func summarizeArtifactEvent(payload map[string]any) artifactUpdateSummary {
	artifact := artifactUpdateSummary{
		ID:              strings.TrimSpace(stringValue(payload, "id")),
		CanonicalKey:    strings.TrimSpace(stringValue(payload, "canonicalKey")),
		DisplayName:     strings.TrimSpace(stringValue(payload, "displayName")),
		PathStressScore: floatValue(payload["pathStressScore"]),
		BindingScore:    floatValue(payload["bindingScore"]),
		LegCount:        int(floatValue(payload["legCount"])),
	}
	overlayMap := toMap(payload["riskOverlay"])
	if len(overlayMap) > 0 {
		artifact.PrimaryMechanism = strings.TrimSpace(stringValue(overlayMap, "primaryMechanism"))
		artifact.StressScore = floatValue(overlayMap["stressScore"])
		artifact.BottleneckLeg = int(floatValue(overlayMap["bottleneckLegIndex"]))
		artifact.Flags = stringSliceAny(overlayMap["flags"])
		artifact.DominantFactors = collectOverlayDominantFactors(overlayMap)
	}
	if artifact.StressScore == 0 {
		if parsed, err := horizonmodels.ParseSupplyChainPathPayload(toMap(payload["payload"])); err == nil && parsed != nil && parsed.Stress != nil {
			artifact.StressScore = parsed.Stress.PathScore
			artifact.BottleneckLeg = parsed.Stress.BottleneckLegIndex
			artifact.DominantFactors = append([]string(nil), parsed.Stress.DominantFactors...)
		}
	}
	return artifact
}

func collectOverlayDominantFactors(overlay map[string]any) []string {
	mechanismsRaw, ok := overlay["mechanisms"].([]any)
	if !ok || len(mechanismsRaw) == 0 {
		return nil
	}
	out := make([]string, 0, len(mechanismsRaw))
	for _, raw := range mechanismsRaw {
		m := toMap(raw)
		key := strings.TrimSpace(stringValue(m, "key"))
		if key != "" {
			out = append(out, key)
		}
	}
	return out
}

func enrichArtifactSummary(artifact *horizonmodels.MaterializedArtifact) {
	if artifact == nil {
		return
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		artifact.LegCount = len(parsed.Legs)
		artifact.BindingScore = averageLegBinding(parsed.Legs)
		if parsed.Stress != nil {
			artifact.PathStressScore = parsed.Stress.PathScore
			artifact.BottleneckLeg = parsed.Stress.BottleneckLegIndex
		}
	}
}

func supplyPathStressScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	if artifact.PathStressScore > 0 {
		return artifact.PathStressScore
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil && parsed.Stress != nil {
		return parsed.Stress.PathScore
	}
	return 0
}

func legStressScore(leg horizonmodels.SupplyChainPathLeg) float64 {
	if leg.Stress == nil {
		return 0
	}
	return leg.Stress.StressScore
}

func averageLegBinding(legs []horizonmodels.SupplyChainPathLeg) float64 {
	if len(legs) == 0 {
		return 0
	}
	sum := 0.0
	count := 0
	for _, leg := range legs {
		if leg.BindingConfidence <= 0 {
			continue
		}
		sum += leg.BindingConfidence
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func writeText(path, contents string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(contents), 0o644)
}

func writeJSON(path string, value any) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func buildBusinessReport(summary smokeSummary) string {
	var b strings.Builder
	b.WriteString("# Identify Stresses Report\n\n")
	b.WriteString(fmt.Sprintf("- Lens: **%s** (`%s`)\n", summary.LensName, summary.LensID))
	b.WriteString(fmt.Sprintf("- Tenant: `%s`\n", summary.TenantID))
	if summary.SelectedCanonicalPath != "" {
		b.WriteString(fmt.Sprintf("- Selected scope anchor: `%s`\n", summary.SelectedCanonicalPath))
	}
	if summary.Debug != nil {
		b.WriteString(fmt.Sprintf("- Scope accepted: **%d nodes**, **%d supply paths**, **%d supply links**, **%d relations**\n",
			summary.Debug.ScopeSummary.AcceptedNodes,
			summary.Debug.ScopeSummary.AcceptedArtifacts,
			summary.Debug.ScopeSummary.AcceptedSupplyLinks,
			summary.Debug.ScopeSummary.AcceptedRelations,
		))
		b.WriteString(fmt.Sprintf("- Findings normalized: **%d**\n", len(summary.Debug.Findings)))
	}
	if summary.Completion != nil {
		b.WriteString(fmt.Sprintf("- Apply result: **%s**; findings applied=%d, paths updated=%d, supply links updated=%d, events persisted=%d\n",
			firstNonEmpty(stringValue(summary.Completion, "status"), "unknown"),
			int(floatValue(summary.Completion["findingsApplied"])),
			int(floatValue(summary.Completion["artifactsUpdated"])),
			int(floatValue(summary.Completion["supplyLinksUpdated"])),
			int(floatValue(summary.Completion["eventsPersisted"])),
		))
	}
	b.WriteString("\n## Executive View\n\n")
	routeScopedFindings := make([]horizonhttp.StressFinding, 0)
	if summary.Debug != nil {
		for _, finding := range summary.Debug.Findings {
			if strings.TrimSpace(finding.ArtifactKey) != "" || strings.TrimSpace(finding.SupplyLinkRef) != "" {
				routeScopedFindings = append(routeScopedFindings, finding)
			}
		}
	}
	if len(summary.ArtifactUpdates) == 0 {
		if len(routeScopedFindings) == 0 {
			b.WriteString("No visible shipment/path overlays were updated in this run.\n\n")
		} else {
			b.WriteString("### Priority Shipments Implicated By Current Findings\n\n")
			for i, finding := range routeScopedFindings {
				if i >= 8 {
					break
				}
				b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, firstNonEmpty(humanizeRef(finding.ProductRef), humanizeRef(firstRef(finding.TargetRefs)), humanizeKey(finding.StressorType))))
				b.WriteString(fmt.Sprintf("   - Target: **%s**; stressor: **%s**; mechanism: **%s**; severity: **%s**\n",
					humanizeKey(firstNonEmpty(finding.TargetType, "unknown")),
					humanizeKey(finding.StressorType),
					humanizeKey(finding.Mechanism),
					strings.ToUpper(firstNonEmpty(finding.Severity, "unknown")),
				))
				if finding.ArtifactKey != "" {
					b.WriteString(fmt.Sprintf("   - Path artifact: `%s`\n", finding.ArtifactKey))
				}
				if finding.SupplyLinkRef != "" {
					b.WriteString(fmt.Sprintf("   - Supply link: `%s`\n", finding.SupplyLinkRef))
				}
				if finding.Summary != "" {
					b.WriteString(fmt.Sprintf("   - Why it matters: %s\n", strings.TrimSpace(finding.Summary)))
				}
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("### Shipments and Routes Under Stress\n\n")
		for i, artifact := range summary.ArtifactUpdates {
			if i >= 8 {
				break
			}
			b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey, artifact.ID)))
			b.WriteString(fmt.Sprintf("   - Path stress: **%.2f**; mechanism: **%s**; bottleneck leg: **%d**\n",
				artifact.PathStressScore,
				humanizeKey(firstNonEmpty(artifact.PrimaryMechanism, "unknown")),
				artifact.BottleneckLeg,
			))
			if len(artifact.DominantFactors) > 0 {
				b.WriteString(fmt.Sprintf("   - Dominant factors: %s\n", joinHumanizedList(artifact.DominantFactors)))
			}
			if len(artifact.Flags) > 0 {
				b.WriteString(fmt.Sprintf("   - Flags: %s\n", joinHumanizedList(artifact.Flags)))
			}
		}
		b.WriteString("\n")
	}

	if summary.Debug != nil && len(summary.Debug.Findings) > 0 {
		driverBuckets := bucketFindingsByDriver(summary.Debug.Findings)
		b.WriteString("### What Is Driving Risk\n\n")
		if len(driverBuckets) == 0 {
			b.WriteString("No clear driver categories extracted.\n\n")
		} else {
			keys := make([]string, 0, len(driverBuckets))
			for key := range driverBuckets {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				findings := driverBuckets[key]
				b.WriteString(fmt.Sprintf("- **%s**: %d finding(s)\n", humanizeKey(key), len(findings)))
			}
			b.WriteString("\n")
		}

		b.WriteString("## Detailed Findings\n\n")
		for i, finding := range summary.Debug.Findings {
			if i >= 12 {
				break
			}
			b.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, findingHeadline(finding)))
			b.WriteString(fmt.Sprintf("- Target type: **%s**\n", humanizeKey(finding.TargetType)))
			if len(finding.TargetRefs) > 0 {
				b.WriteString(fmt.Sprintf("- Target refs: %s\n", joinRefs(finding.TargetRefs, 5)))
			}
			if finding.SupplyLinkRef != "" {
				b.WriteString(fmt.Sprintf("- Supply link: `%s`\n", finding.SupplyLinkRef))
			}
			if finding.ArtifactKey != "" {
				b.WriteString(fmt.Sprintf("- Path artifact: `%s`\n", finding.ArtifactKey))
			}
			if finding.ProductRef != "" {
				b.WriteString(fmt.Sprintf("- Product: **%s** (`%s`)\n", humanizeRef(finding.ProductRef), finding.ProductRef))
			}
			b.WriteString(fmt.Sprintf("- Stressor: **%s**; mechanism: **%s**; severity: **%s**; confidence: **%.2f**\n",
				humanizeKey(finding.StressorType),
				humanizeKey(finding.Mechanism),
				strings.ToUpper(firstNonEmpty(finding.Severity, "unknown")),
				finding.Confidence,
			))
			if finding.EffectiveAt != "" || finding.TimeHorizon != "" {
				b.WriteString(fmt.Sprintf("- Timing: effective=`%s`, horizon=`%s`\n",
					firstNonEmpty(finding.EffectiveAt, "unknown"),
					firstNonEmpty(finding.TimeHorizon, "unknown"),
				))
			}
			if finding.Summary != "" {
				b.WriteString(fmt.Sprintf("- Why it matters: %s\n", strings.TrimSpace(finding.Summary)))
			}
			if len(finding.MetricObservations) > 0 {
				b.WriteString("- Metric impacts:\n")
				for _, metric := range finding.MetricObservations[:minInt(4, len(finding.MetricObservations))] {
					b.WriteString(fmt.Sprintf("  - %s = %s %s (%s)\n",
						humanizeKey(metric.MetricKey),
						formatMetricValue(metric.Value),
						strings.TrimSpace(metric.Unit),
						firstNonEmpty(metric.Description, metric.Scope, "impact"),
					))
				}
			}
			if linked := linkedArtifactLines(finding, summary.ArtifactUpdates); len(linked) > 0 {
				b.WriteString("- Visible shipments affected now:\n")
				for _, line := range linked {
					b.WriteString("  - " + line + "\n")
				}
			}
			if len(finding.Evidence) > 0 {
				b.WriteString("- Evidence:\n")
				for _, evidence := range finding.Evidence[:minInt(3, len(finding.Evidence))] {
					title := firstNonEmpty(evidence.Title, evidence.Domain, evidence.URL)
					b.WriteString(fmt.Sprintf("  - [%s](%s)\n", title, evidence.URL))
				}
			}
			b.WriteString("\n")
		}
	}

	if len(summary.Errors) > 0 {
		b.WriteString("## Errors\n\n")
		for _, err := range summary.Errors {
			if strings.TrimSpace(err) == "" {
				continue
			}
			b.WriteString("- " + err + "\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

func bucketFindingsByDriver(findings []horizonhttp.StressFinding) map[string][]horizonhttp.StressFinding {
	out := make(map[string][]horizonhttp.StressFinding)
	for _, finding := range findings {
		key := classifyDriver(finding)
		out[key] = append(out[key], finding)
	}
	return out
}

func classifyDriver(finding horizonhttp.StressFinding) string {
	switch strings.TrimSpace(finding.StressorType) {
	case "sanctions", "export_controls", "regulatory_block", "customs_restrictions", "insurance_finance_restriction":
		return "government_and_regulation"
	case "security_conflict", "natural_hazard", "labor_action", "cyber_operational_disruption":
		return "disruption_and_shocks"
	case "route_disruption", "port_or_border_congestion", "carrier_or_service_interruption":
		return "routes_and_logistics"
	case "supplier_outage", "capacity_drop":
		return "supplier_and_capacity"
	case "inventory_shortfall":
		return "inventory_and_supply_balance"
	default:
		return "other"
	}
}

func findingHeadline(finding horizonhttp.StressFinding) string {
	product := humanizeRef(finding.ProductRef)
	target := humanizeRef(firstNonEmpty(firstRef(finding.TargetRefs), finding.SupplyLinkRef, finding.ArtifactKey))
	parts := []string{humanizeKey(finding.StressorType)}
	if product != "" {
		parts = append(parts, "on "+product)
	}
	if target != "" {
		parts = append(parts, "affecting "+target)
	}
	return strings.Join(parts, " ")
}

func linkedArtifactLines(finding horizonhttp.StressFinding, artifacts []artifactUpdateSummary) []string {
	lines := make([]string, 0, 4)
	for _, artifact := range artifacts {
		if !findingTouchesArtifact(finding, artifact) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (stress %.2f, %s)", firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey, artifact.ID), artifact.PathStressScore, humanizeKey(firstNonEmpty(artifact.PrimaryMechanism, "unknown"))))
		if len(lines) >= 4 {
			break
		}
	}
	return lines
}

func findingTouchesArtifact(finding horizonhttp.StressFinding, artifact artifactUpdateSummary) bool {
	artifactKey := strings.TrimSpace(artifact.CanonicalKey)
	if finding.ArtifactKey != "" && strings.TrimSpace(finding.ArtifactKey) == artifactKey {
		return true
	}
	if finding.SupplyLinkRef != "" && artifactKey != "" && strings.Contains(artifactKey, slugTail(finding.SupplyLinkRef)) {
		return true
	}
	if finding.ProductRef != "" && artifactKey != "" && strings.Contains(artifactKey, slugTail(finding.ProductRef)) {
		return true
	}
	for _, ref := range finding.TargetRefs {
		if artifactKey != "" && strings.Contains(artifactKey, slugTail(ref)) {
			return true
		}
	}
	return false
}

func slugTail(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func joinRefs(refs []string, limit int) string {
	out := make([]string, 0, minInt(limit, len(refs)))
	for i, ref := range refs {
		if i >= limit {
			break
		}
		out = append(out, fmt.Sprintf("`%s`", ref))
	}
	return strings.Join(out, ", ")
}

func joinHumanizedList(values []string) string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := humanizeKey(strings.Trim(value, "[] "))
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return strings.Join(out, ", ")
}

func humanizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	tail := parts[len(parts)-1]
	return humanizeKey(tail)
}

func humanizeKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, "[]")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "/", " ")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	for i, field := range fields {
		fields[i] = strings.ToUpper(field[:1]) + field[1:]
	}
	return strings.Join(fields, " ")
}

func firstRef(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	return strings.TrimSpace(refs[0])
}

func formatMetricValue(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%.2f", value)
}

func printSummary(summary smokeSummary) {
	fmt.Printf("\nLens: %s (%s)\n", summary.LensName, summary.LensID)
	if summary.Debug != nil {
		fmt.Printf("Scope: nodes=%d artifacts=%d supplyLinks=%d relations=%d findings=%d\n",
			summary.Debug.ScopeSummary.AcceptedNodes,
			summary.Debug.ScopeSummary.AcceptedArtifacts,
			summary.Debug.ScopeSummary.AcceptedSupplyLinks,
			summary.Debug.ScopeSummary.AcceptedRelations,
			len(summary.Debug.Findings),
		)
	}
	if summary.Completion != nil {
		fmt.Printf("Completion: status=%s findingsApplied=%d artifactsUpdated=%d supplyLinksUpdated=%d eventsPersisted=%d zones=%d\n",
			stringValue(summary.Completion, "status"),
			int(floatValue(summary.Completion["findingsApplied"])),
			int(floatValue(summary.Completion["artifactsUpdated"])),
			int(floatValue(summary.Completion["supplyLinksUpdated"])),
			int(floatValue(summary.Completion["eventsPersisted"])),
			int(floatValue(summary.Completion["affectedZonesCreated"])),
		)
	}
	if len(summary.Errors) > 0 {
		fmt.Println("Errors:")
		for _, err := range summary.Errors {
			if strings.TrimSpace(err) != "" {
				fmt.Printf("  - %s\n", err)
			}
		}
	}
	if len(summary.TopMechanisms) > 0 {
		fmt.Println("Top mechanisms:")
		for i, item := range summary.TopMechanisms {
			if i >= 6 {
				break
			}
			fmt.Printf("  - %s: %d\n", item.Key, item.Count)
		}
	}
	if len(summary.ArtifactUpdates) > 0 {
		fmt.Println("Top updated paths:")
		for i, item := range summary.ArtifactUpdates {
			if i >= 8 {
				break
			}
			fmt.Printf("  - %.2f %s [%s] bottleneck=%d factors=%s\n",
				item.PathStressScore,
				firstNonEmpty(item.DisplayName, item.CanonicalKey, item.ID),
				firstNonEmpty(item.PrimaryMechanism, "unknown"),
				item.BottleneckLeg,
				strings.Join(item.DominantFactors, ", "),
			)
		}
	}
}

func toMap(raw any) map[string]any {
	switch v := raw.(type) {
	case map[string]any:
		return v
	case nil:
		return nil
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		out := map[string]any{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func floatValue(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		var n json.Number = json.Number(strings.TrimSpace(v))
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func stringValue(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

func stringSliceAny(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func backendRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	if strings.HasSuffix(wd, string(filepath.Separator)+"backend") {
		return wd
	}
	return filepath.Join(wd, "backend")
}
