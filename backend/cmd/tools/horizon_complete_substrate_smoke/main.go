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

type completionEventRecord struct {
	Type    string         `json:"type"`
	Tenant  string         `json:"tenantId"`
	Payload map[string]any `json:"payload,omitempty"`
	At      string         `json:"at"`
}

type completionSummary struct {
	ProjectID             string                                    `json:"projectId"`
	TenantID              string                                    `json:"tenantId"`
	LensID                string                                    `json:"lensId"`
	LensName              string                                    `json:"lensName"`
	SelectedCanonicalPath string                                    `json:"selectedCanonicalPath,omitempty"`
	Request               horizonhttp.CompleteSubstrateRequest      `json:"request"`
	Debug                 *horizonhttp.CompleteSubstrateDebugResult `json:"debug,omitempty"`
	Completion            map[string]any                            `json:"completion,omitempty"`
	Errors                []string                                  `json:"errors,omitempty"`
}

type lensCandidate struct {
	Lens  *horizonmodels.Lens
	Score int
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "all", "tenant id or all")
	lensID := flag.String("lens-id", "", "exact lens id")
	lensQuery := flag.String("lens-query", "asml", "substring used to auto-select lens when -lens-id is empty")
	listOnly := flag.Bool("list", false, "list active lenses and exit")
	maxNodes := flag.Int("max-nodes", 40, "max visible nodes")
	maxArtifacts := flag.Int("max-artifacts", 25, "max visible artifacts")
	maxCompletions := flag.Int("max-completions", 80, "max completions")
	useLLM := flag.Bool("use-llm", true, "use grounded LLM completion")
	autoBootstrap := flag.Bool("bootstrap", true, "bootstrap missing route substrate before completion")
	bootstrapMinPaths := flag.Int("bootstrap-min-paths", 8, "bootstrap if lens has fewer than this many supply paths")
	applyChanges := flag.Bool("apply", true, "apply completions after debug")
	outdir := flag.String("outdir", "", "directory for saved diagnostics")
	flag.Parse()

	logger := log.New(os.Stdout, "[complete-substrate-smoke] ", log.LstdFlags)
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	if *listOnly {
		if err := listActiveLenses(ctx, db, strings.TrimSpace(*tenantID)); err != nil {
			log.Fatalf("list lenses failed: %v", err)
		}
		return
	}

	lens, err := resolveLens(ctx, db, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), strings.TrimSpace(*lensQuery))
	if err != nil {
		log.Fatalf("resolve lens failed: %v", err)
	}
	logger.Printf("Using lens %s (%s) tenant=%s", strings.TrimSpace(lens.ID), strings.TrimSpace(lens.Name), strings.TrimSpace(lens.TenantID))

	lensesRepo := horizonrepo.NewLensesRepository(db)
	lensArtifactsRepo := horizonrepo.NewLensArtifactsRepository(db)
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)

	if *autoBootstrap {
		if err := maybeBootstrapLens(ctx, logger, strings.TrimSpace(*projectID), lens, materializedRepo, *bootstrapMinPaths); err != nil {
			logger.Printf("bootstrap warning: %v", err)
		}
	}

	scopeRequest, selectedCanonicalPath, err := buildCompleteRequest(ctx, lens, lensArtifactsRepo, materializedRepo, *maxNodes, *maxArtifacts, *maxCompletions, *useLLM)
	if err != nil {
		log.Fatalf("build request failed: %v", err)
	}

	outPath := strings.TrimSpace(*outdir)
	if outPath == "" {
		outPath = filepath.Join("tmp", fmt.Sprintf("complete_substrate_smoke_%s_%s", strings.TrimSpace(lens.ID), time.Now().UTC().Format("20060102_150405")))
	}
	if err := os.MkdirAll(outPath, 0o755); err != nil {
		log.Fatalf("create outdir failed: %v", err)
	}

	modelManager := ai.NewAIModelManager()
	aiServices := buildAIServices(ctx, strings.TrimSpace(*projectID), modelManager, logger)
	if len(aiServices) == 0 {
		log.Fatal("no AI services configured")
	}
	exec := ai.NewAITaskExecutor(aiServices, modelManager).
		WithUsageMetrics(coreRepo.NewUsageMetricsRepository(db)).
		WithCircuitBreaker(ai.NewCircuitBreaker()).
		WithAdmissionController(ai.NoopAdmissionController())

	var published []completionEventRecord
	publisher := func(ctx context.Context, eventType string, tenant string, payload interface{}) error {
		record := completionEventRecord{
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

	canvas := horizonhttp.NewCompleteSubstrateCanvas(horizonhttp.CompleteSubstrateCanvasConfig{
		Exec:             exec,
		FragmentsRepo:    fragmentsRepo,
		EdgesRepo:        edgesRepo,
		MaterializedRepo: materializedRepo,
		LensesRepo:       lensesRepo,
		Publisher:        publisher,
		Logger:           logger,
	})

	debugResult, err := canvas.DebugComplete(ctx, strings.TrimSpace(lens.TenantID), lens, scopeRequest)
	if err != nil {
		writeJSON(filepath.Join(outPath, "request.json"), scopeRequest)
		log.Fatalf("debug complete-substrate failed: %v", err)
	}
	logger.Printf("Debug completion produced %d gaps and %d completions", len(debugResult.Gaps), len(debugResult.Completions))
	writeJSON(filepath.Join(outPath, "request.json"), scopeRequest)
	writeJSON(filepath.Join(outPath, "debug.json"), debugResult)

	var completionResp *horizonhttp.CompleteSubstrateResponse
	if *applyChanges {
		jobID := "complete_substrate_smoke_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		completionResp, err = canvas.Run(ctx, strings.TrimSpace(lens.TenantID), lens, scopeRequest, jobID)
		if err != nil {
			writeJSON(filepath.Join(outPath, "events.json"), published)
			log.Fatalf("apply complete-substrate failed: %v", err)
		}
		logger.Printf("Apply completed for job %s", jobID)
	}

	writeJSON(filepath.Join(outPath, "events.json"), published)
	summary := completionSummary{
		ProjectID:             strings.TrimSpace(*projectID),
		TenantID:              strings.TrimSpace(lens.TenantID),
		LensID:                strings.TrimSpace(lens.ID),
		LensName:              strings.TrimSpace(lens.Name),
		SelectedCanonicalPath: selectedCanonicalPath,
		Request:               scopeRequest,
		Debug:                 debugResult,
	}
	if completionResp != nil {
		summary.Completion = map[string]any{
			"jobId":         completionResp.JobID,
			"acceptedScope": completionResp.AcceptedScope,
			"startedAt":     completionResp.StartedAt,
		}
	}
	writeJSON(filepath.Join(outPath, "summary.json"), summary)
	fmt.Printf("\nDiagnostics saved to %s\n", outPath)
}

func buildCompleteRequest(
	ctx context.Context,
	lens *horizonmodels.Lens,
	lensArtifactsRepo *horizonrepo.LensArtifactsRepository,
	materializedRepo *horizonrepo.MaterializedArtifactsRepository,
	maxNodes, maxArtifacts, maxCompletions int,
	useLLM bool,
) (horizonhttp.CompleteSubstrateRequest, string, error) {
	materialized, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 500)
	if err != nil {
		return horizonhttp.CompleteSubstrateRequest{}, "", err
	}
	lensArtifacts, err := lensArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 2000)
	if err != nil {
		return horizonhttp.CompleteSubstrateRequest{}, "", err
	}

	supplyPaths := make([]*horizonmodels.MaterializedArtifact, 0, len(materialized))
	for _, artifact := range materialized {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		supplyPaths = append(supplyPaths, artifact)
	}
	sort.Slice(supplyPaths, func(i, j int) bool {
		if supplyPaths[i].PathStressScore != supplyPaths[j].PathStressScore {
			return supplyPaths[i].PathStressScore > supplyPaths[j].PathStressScore
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
	useLLMPtr := useLLM

	return horizonhttp.CompleteSubstrateRequest{
		ScopeMode:             "visible_set",
		SelectedCanonicalPath: selectedCanonicalPath,
		VisibleNodeRefs:       nodeRefs,
		VisibleArtifactKeys:   artifactKeys,
		VisibleSupplyLinkRefs: supplyLinkRefs,
		VisibleRelations:      relations,
		ClientViewID:          "horizon_complete_substrate_smoke",
		MaxNodes:              maxNodes,
		MaxArtifacts:          maxArtifacts,
		MaxCompletions:        maxCompletions,
		AsOfDate:              time.Now().UTC().Format("2006-01-02"),
		UseLLM:                &useLLMPtr,
	}, selectedCanonicalPath, nil
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
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/tools/horizon_lens_path_bootstrap",
		"-project", strings.TrimSpace(projectID),
		"-tenant", strings.TrimSpace(lens.TenantID),
		"-lens-id", strings.TrimSpace(lens.ID),
		"-repair=false",
		"-import-limit", "80",
	)
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
				os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID"),
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
		score := 0
		name := strings.ToLower(strings.TrimSpace(lens.Name))
		queryText := strings.ToLower(strings.TrimSpace(lens.OriginalQuery))
		project := strings.ToLower(strings.TrimSpace(lens.ProjectID))
		if strings.Contains(name, needle) {
			score += 100
		}
		if strings.Contains(queryText, needle) {
			score += 80
		}
		if strings.Contains(project, needle) {
			score += 40
		}
		for _, ref := range lens.Scope {
			if strings.Contains(strings.ToLower(strings.TrimSpace(ref)), needle) {
				score += 20
			}
		}
		if score > 0 {
			candidates = append(candidates, lensCandidate{Lens: &lens, Score: score})
		}
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
	return candidates[0].Lens, nil
}

func listActiveLenses(ctx context.Context, db *firestore.Client, tenantID string) error {
	query := db.Collection("horizon_lenses").Where("status", "==", string(horizonmodels.LensStatusActive)).Limit(500)
	if !strings.EqualFold(strings.TrimSpace(tenantID), "all") && strings.TrimSpace(tenantID) != "" {
		query = query.Where("tenantId", "==", tenantID)
	}
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, doc := range docs {
		var lens horizonmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		fmt.Printf("%s | tenant=%s | %s | updated=%s\n", lens.ID, lens.TenantID, lens.Name, lens.UpdatedAt.Format(time.RFC3339))
	}
	return nil
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
		weight := 10 + int(artifact.PathStressScore*100)
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
		if artifact == nil {
			continue
		}
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
		for _, ref := range artifact.MatchedRefs {
			push(ref)
		}
		push(artifact.AnchorSupplier)
		push(artifact.AnchorCustomer)
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, edge := range parsed.Edges {
			push(edge.SupplyLinkCanonicalPath)
			push(edge.SupplyLinkID)
		}
		for _, leg := range parsed.Legs {
			push(leg.SupplyLinkCanonicalPath)
			push(leg.SupplyLinkID)
		}
	}
	return out
}

func collectVisibleRelations(lensArtifacts []*horizonmodels.LensArtifact, supplyPaths []*horizonmodels.MaterializedArtifact) []horizonhttp.IdentifyStressVisibleRelation {
	seen := map[string]struct{}{}
	out := make([]horizonhttp.IdentifyStressVisibleRelation, 0)
	push := func(rel horizonhttp.IdentifyStressVisibleRelation) {
		key := strings.Join([]string{
			strings.TrimSpace(rel.EdgeID),
			strings.TrimSpace(rel.SourceCanonicalPath),
			strings.TrimSpace(rel.TargetCanonicalPath),
			strings.TrimSpace(rel.RelationType),
			strings.TrimSpace(rel.RelationRole),
		}, "|")
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
			RelationFamily:      "lens_artifact",
			RelationRole:        "lens_artifact",
			BusinessMeaning:     strings.TrimSpace(artifact.DisplayLabel),
		})
	}
	for _, artifact := range supplyPaths {
		if artifact == nil {
			continue
		}
		for _, ref := range artifact.NodeRefs {
			if ref == "" || strings.TrimSpace(artifact.AnchorCustomer) == "" || ref == strings.TrimSpace(artifact.AnchorCustomer) {
				continue
			}
			push(horizonhttp.IdentifyStressVisibleRelation{
				SourceCanonicalPath: ref,
				TargetCanonicalPath: strings.TrimSpace(artifact.AnchorCustomer),
				RelationType:        "SUPPLY_PATH_CONTEXT",
				RelationFamily:      "supply_path",
				RelationRole:        "path",
				BusinessMeaning:     strings.TrimSpace(artifact.DisplayName),
				Criticality:         artifact.PathStressScore,
			})
		}
	}
	return out
}

func toMap(v interface{}) map[string]any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"_raw": fmt.Sprintf("%v", v)}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"_raw": string(b)}
	}
	return out
}

func writeJSON(path string, v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Printf("writeJSON %s marshal failed: %v", path, err)
		return
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		log.Printf("writeJSON %s failed: %v", path, err)
	}
}

func backendRoot() string {
	cwd, err := os.Getwd()
	if err == nil {
		if strings.HasSuffix(cwd, "/backend") {
			return cwd
		}
		if strings.HasSuffix(cwd, "/backend/cmd/tools/horizon_complete_substrate_smoke") {
			return filepath.Clean(filepath.Join(cwd, "..", "..", ".."))
		}
	}
	return filepath.Clean(filepath.Join(filepath.Dir(os.Args[0]), "..", "..", ".."))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
