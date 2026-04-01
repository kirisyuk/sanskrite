package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

type recoveredSetSummary struct {
	Count   int      `json:"count"`
	Samples []string `json:"samples,omitempty"`
}

type overlapSummary struct {
	BaseCount            int      `json:"baseCount"`
	RecoveredCount       int      `json:"recoveredCount"`
	MatchedCount         int      `json:"matchedCount"`
	MissingCount         int      `json:"missingCount"`
	RecoveredOnlyCount   int      `json:"recoveredOnlyCount"`
	CoveragePct          float64  `json:"coveragePct"`
	MissingSamples       []string `json:"missingSamples,omitempty"`
	RecoveredOnlySamples []string `json:"recoveredOnlySamples,omitempty"`
	MatchedSamples       []string `json:"matchedSamples,omitempty"`
}

type jobRecoverySummary struct {
	JobID             string   `json:"jobId"`
	ArtifactFragments int      `json:"artifactFragments"`
	ArtifactEdges     int      `json:"artifactEdges"`
	RecoveredSamples  []string `json:"recoveredSamples,omitempty"`
}

type report struct {
	ProjectID string `json:"projectId"`
	Lens      struct {
		ID             string   `json:"id"`
		TenantID       string   `json:"tenantId"`
		ProjectID      string   `json:"projectId"`
		Name           string   `json:"name"`
		Status         string   `json:"status"`
		CreatedAt      string   `json:"createdAt"`
		UpdatedAt      string   `json:"updatedAt"`
		ScopeCount     int      `json:"scopeCount"`
		Scope          []string `json:"scope,omitempty"`
		Workbench      []string `json:"workbenchScope,omitempty"`
		ScopeArtifacts int      `json:"scopeArtifacts"`
	} `json:"lens"`
	Recovered struct {
		LensArtifactFragments    recoveredSetSummary `json:"lensArtifactFragments"`
		MaterializedNodeRefs     recoveredSetSummary `json:"materializedNodeRefs"`
		ScopeArtifactNodeRefs    recoveredSetSummary `json:"scopeArtifactNodeRefs"`
		JobTrackedFragmentPaths  recoveredSetSummary `json:"jobTrackedFragmentPaths"`
		JobChainContextNodes     recoveredSetSummary `json:"jobChainContextNodes"`
		JobWorkingProductRefs    recoveredSetSummary `json:"jobWorkingProductRefs"`
		UnionRecoveredObjectRefs recoveredSetSummary `json:"unionRecoveredObjectRefs"`
	} `json:"recovered"`
	Overlap struct {
		RawScopeObjects  overlapSummary `json:"rawScopeObjects"`
		WorkbenchObjects overlapSummary `json:"workbenchObjects"`
	} `json:"overlap"`
	Jobs struct {
		Total   int                  `json:"total"`
		Summary []jobRecoverySummary `json:"summary,omitempty"`
	} `json:"jobs"`
	Analysis struct {
		Assessment string   `json:"assessment"`
		Signals    []string `json:"signals,omitempty"`
		Caveats    []string `json:"caveats,omitempty"`
	} `json:"analysis"`
	GeneratedAt string `json:"generatedAt"`
}

type sourceBuckets struct {
	lensArtifactFragments   map[string]struct{}
	materializedNodeRefs    map[string]struct{}
	scopeArtifactNodeRefs   map[string]struct{}
	jobTrackedFragmentPaths map[string]struct{}
	jobChainContextNodes    map[string]struct{}
	jobWorkingProductRefs   map[string]struct{}
}

type lensLite struct {
	ID        string    `firestore:"id"`
	TenantID  string    `firestore:"tenantId"`
	ProjectID string    `firestore:"projectId"`
	Name      string    `firestore:"name"`
	Status    string    `firestore:"status"`
	CreatedAt time.Time `firestore:"createdAt"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")), "nv-horizon"), "GCP project id")
	lensID := flag.String("lens", "", "optional lens id; when omitted, uses latest created lens")
	tenantID := flag.String("tenant", "", "optional tenant filter when selecting latest lens")
	status := flag.String("status", "", "optional status filter when selecting latest lens")
	limitJobs := flag.Int("jobs", 200, "max scout jobs to inspect for the lens")
	limitArtifacts := flag.Int("artifacts", 2000, "max lens/materialized artifacts to inspect")
	sampleSize := flag.Int("samples", 20, "sample size for lists")
	jsonOut := flag.Bool("json", true, "emit JSON")
	verbose := flag.Bool("verbose", false, "log progress to stderr")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		fmt.Fprintln(os.Stderr, "--project is required")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()
	db := fsClient.Client()
	logf := func(format string, args ...interface{}) {
		if *verbose {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}

	lensesRepo := repo.NewLensesRepository(db)
	lensArtifactsRepo := repo.NewLensArtifactsRepository(db)
	materializedRepo := repo.NewMaterializedArtifactsRepository(db)

	var lens *models.Lens
	if strings.TrimSpace(*lensID) != "" {
		logf("load lens by id=%s", strings.TrimSpace(*lensID))
		lens, err = lensesRepo.Get(ctx, strings.TrimSpace(*lensID))
		if err != nil {
			fmt.Fprintf(os.Stderr, "load lens failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		logf("find latest lens project=%s tenant=%s status=%s", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), strings.TrimSpace(*status))
		lens, err = findLatestLens(ctx, db, strings.TrimSpace(*tenantID), strings.TrimSpace(*status))
		if err != nil {
			fmt.Fprintf(os.Stderr, "find latest lens failed: %v\n", err)
			os.Exit(1)
		}
		if lens == nil {
			fmt.Fprintln(os.Stderr, "no lens found")
			os.Exit(1)
		}
	}
	logf("lens selected id=%s name=%q project=%s tenant=%s", strings.TrimSpace(lens.ID), strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ProjectID), strings.TrimSpace(lens.TenantID))

	out := report{
		ProjectID:   strings.TrimSpace(*projectID),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	out.Lens.ID = strings.TrimSpace(lens.ID)
	out.Lens.TenantID = strings.TrimSpace(lens.TenantID)
	out.Lens.ProjectID = strings.TrimSpace(lens.ProjectID)
	out.Lens.Name = strings.TrimSpace(lens.Name)
	out.Lens.Status = strings.TrimSpace(lens.Status)
	out.Lens.CreatedAt = formatTime(lens.CreatedAt)
	out.Lens.UpdatedAt = formatTime(lens.UpdatedAt)
	out.Lens.ScopeCount = len(lens.Scope)
	out.Lens.Scope = append([]string(nil), lens.Scope...)
	out.Lens.Workbench = workscope.LensWorkbenchScopePaths(lens)
	out.Lens.ScopeArtifacts = len(lens.ScopeArtifacts)

	rawScopeObjects := filterObjectRefs(lens.Scope)
	workbenchObjects := filterObjectRefs(out.Lens.Workbench)

	buckets := sourceBuckets{
		lensArtifactFragments:   make(map[string]struct{}),
		materializedNodeRefs:    make(map[string]struct{}),
		scopeArtifactNodeRefs:   make(map[string]struct{}),
		jobTrackedFragmentPaths: make(map[string]struct{}),
		jobChainContextNodes:    make(map[string]struct{}),
		jobWorkingProductRefs:   make(map[string]struct{}),
	}

	for _, ref := range collectScopeArtifactNodeRefs(lens) {
		buckets.scopeArtifactNodeRefs[ref] = struct{}{}
	}

	lensArtifacts, err := lensArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), *limitArtifacts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list lens artifacts failed: %v\n", err)
		os.Exit(1)
	}
	logf("lens artifacts loaded n=%d", len(lensArtifacts))
	for _, art := range lensArtifacts {
		if art == nil || art.Type != models.LensArtifactTypeFragment {
			continue
		}
		if ref := normalizeObjectRef(art.CanonicalPath); ref != "" {
			buckets.lensArtifactFragments[ref] = struct{}{}
		}
	}

	materialized, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), *limitArtifacts)
	if err == nil {
		logf("materialized artifacts loaded n=%d", len(materialized))
		for _, art := range materialized {
			if art == nil {
				continue
			}
			for _, ref := range art.NodeRefs {
				if norm := normalizeObjectRef(ref); norm != "" {
					buckets.materializedNodeRefs[norm] = struct{}{}
				}
			}
		}
	}

	jobSummaries := summarizeJobsFromArtifacts(lensArtifacts, *sampleSize/2, *limitJobs)
	logf("job summaries built n=%d", len(jobSummaries))

	unionRecovered := unionSets(
		mapToSliceKeys(buckets.lensArtifactFragments),
		mapToSliceKeys(buckets.materializedNodeRefs),
		mapToSliceKeys(buckets.scopeArtifactNodeRefs),
		mapToSliceKeys(buckets.jobTrackedFragmentPaths),
		mapToSliceKeys(buckets.jobChainContextNodes),
		mapToSliceKeys(buckets.jobWorkingProductRefs),
	)

	out.Recovered.LensArtifactFragments = summarizeSet(buckets.lensArtifactFragments, *sampleSize)
	out.Recovered.MaterializedNodeRefs = summarizeSet(buckets.materializedNodeRefs, *sampleSize)
	out.Recovered.ScopeArtifactNodeRefs = summarizeSet(buckets.scopeArtifactNodeRefs, *sampleSize)
	out.Recovered.JobTrackedFragmentPaths = summarizeSet(buckets.jobTrackedFragmentPaths, *sampleSize)
	out.Recovered.JobChainContextNodes = summarizeSet(buckets.jobChainContextNodes, *sampleSize)
	out.Recovered.JobWorkingProductRefs = summarizeSet(buckets.jobWorkingProductRefs, *sampleSize)
	out.Recovered.UnionRecoveredObjectRefs = recoveredSetSummary{
		Count:   len(unionRecovered),
		Samples: sampleStrings(unionRecovered, *sampleSize),
	}

	out.Overlap.RawScopeObjects = buildOverlapSummary(rawScopeObjects, unionRecovered, *sampleSize)
	out.Overlap.WorkbenchObjects = buildOverlapSummary(workbenchObjects, unionRecovered, *sampleSize)
	out.Jobs.Total = len(jobSummaries)
	out.Jobs.Summary = trimJobSummaries(jobSummaries, 25)
	out.Analysis = buildAnalysis(out)
	logf("report ready assessment=%s recovered=%d", out.Analysis.Assessment, out.Recovered.UnionRecoveredObjectRefs.Count)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "encode report failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	printTextReport(out)
}

func findLatestLens(ctx context.Context, db *firestore.Client, tenantID, status string) (*models.Lens, error) {
	q := db.Collection("horizon_lenses").OrderBy("createdAt", firestore.Desc).Limit(100)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		var lite lensLite
		if err := doc.DataTo(&lite); err != nil {
			continue
		}
		lite.ID = doc.Ref.ID
		if tenantID != "" && strings.TrimSpace(lite.TenantID) != tenantID {
			continue
		}
		if status != "" && !strings.EqualFold(strings.TrimSpace(lite.Status), status) {
			continue
		}
		var lens models.Lens
		if err := doc.DataTo(&lens); err == nil {
			if strings.TrimSpace(lens.ID) == "" {
				lens.ID = doc.Ref.ID
			}
			return &lens, nil
		}
	}
	return nil, nil
}

func buildAnalysis(r report) struct {
	Assessment string   `json:"assessment"`
	Signals    []string `json:"signals,omitempty"`
	Caveats    []string `json:"caveats,omitempty"`
} {
	assessment := "weak"
	coverage := r.Overlap.WorkbenchObjects.CoveragePct
	switch {
	case coverage >= 85:
		assessment = "strong"
	case coverage >= 60:
		assessment = "useful_approximation"
	case coverage >= 30:
		assessment = "partial"
	}

	signals := []string{
		fmt.Sprintf("workbench scope coverage %.1f%% (%d/%d)", coverage, r.Overlap.WorkbenchObjects.MatchedCount, r.Overlap.WorkbenchObjects.BaseCount),
		fmt.Sprintf("recovered union %d object refs", r.Recovered.UnionRecoveredObjectRefs.Count),
		fmt.Sprintf("lens artifacts contributed %d fragment refs", r.Recovered.LensArtifactFragments.Count),
		fmt.Sprintf("chain context contributed %d refs", r.Recovered.JobChainContextNodes.Count),
		fmt.Sprintf("working product scopes contributed %d refs", r.Recovered.JobWorkingProductRefs.Count),
	}

	caveats := []string{
		"union contains only persisted or job-state-tracked traces; purely transient LLM hypotheses are unrecoverable",
		"lens artifacts mostly cover finalized successful job outputs, not every intermediate exploration step",
		"scope comparison uses object-like refs only and intentionally ignores /query, /domain, /artifact, /exposure entries",
	}

	return struct {
		Assessment string   `json:"assessment"`
		Signals    []string `json:"signals,omitempty"`
		Caveats    []string `json:"caveats,omitempty"`
	}{
		Assessment: assessment,
		Signals:    signals,
		Caveats:    caveats,
	}
}

func buildOverlapSummary(base []string, recovered []string, samples int) overlapSummary {
	baseSet := make(map[string]struct{}, len(base))
	recoveredSet := make(map[string]struct{}, len(recovered))
	for _, item := range base {
		if item = normalizeObjectRef(item); item != "" {
			baseSet[item] = struct{}{}
		}
	}
	for _, item := range recovered {
		if item = normalizeObjectRef(item); item != "" {
			recoveredSet[item] = struct{}{}
		}
	}

	matched := make([]string, 0)
	missing := make([]string, 0)
	recoveredOnly := make([]string, 0)
	for item := range baseSet {
		if _, ok := recoveredSet[item]; ok {
			matched = append(matched, item)
		} else {
			missing = append(missing, item)
		}
	}
	for item := range recoveredSet {
		if _, ok := baseSet[item]; !ok {
			recoveredOnly = append(recoveredOnly, item)
		}
	}
	sort.Strings(matched)
	sort.Strings(missing)
	sort.Strings(recoveredOnly)

	coverage := 0.0
	if len(baseSet) > 0 {
		coverage = 100 * float64(len(matched)) / float64(len(baseSet))
	}
	return overlapSummary{
		BaseCount:            len(baseSet),
		RecoveredCount:       len(recoveredSet),
		MatchedCount:         len(matched),
		MissingCount:         len(missing),
		RecoveredOnlyCount:   len(recoveredOnly),
		CoveragePct:          coverage,
		MissingSamples:       sampleStrings(missing, samples),
		RecoveredOnlySamples: sampleStrings(recoveredOnly, samples),
		MatchedSamples:       sampleStrings(matched, minInt(samples, 10)),
	}
}

func summarizeSet(set map[string]struct{}, samples int) recoveredSetSummary {
	values := mapToSliceKeys(set)
	sort.Strings(values)
	return recoveredSetSummary{
		Count:   len(values),
		Samples: sampleStrings(values, samples),
	}
}

func loadWorkingProductScope(ctx context.Context, repo *repo.WorkingProductScopesRepository, job *models.ScoutingJob) *models.WorkingProductScope {
	if job == nil {
		return nil
	}
	if repo != nil {
		if scope, err := repo.GetByJobID(ctx, strings.TrimSpace(job.ID)); err == nil && scope != nil {
			return scope
		}
	}
	return loadWorkingProductScopeFromJobState(job.State)
}

func loadWorkingProductScopeFromJobState(state map[string]interface{}) *models.WorkingProductScope {
	if len(state) == 0 {
		return nil
	}
	raw, ok := state["workingProductScope"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case models.WorkingProductScope:
		scope := v
		return &scope
	case *models.WorkingProductScope:
		if v == nil {
			return nil
		}
		scope := *v
		return &scope
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var scope models.WorkingProductScope
		if err := json.Unmarshal(data, &scope); err != nil {
			return nil
		}
		return &scope
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var scope models.WorkingProductScope
		if err := json.Unmarshal(data, &scope); err != nil {
			return nil
		}
		return &scope
	}
}

func collectWorkingProductRefs(scope *models.WorkingProductScope) []string {
	if scope == nil {
		return nil
	}
	refs := make([]string, 0, len(scope.CanonicalProducts)*2)
	for _, entry := range scope.CanonicalProducts {
		switch entry.Status {
		case models.WorkingProductStatusRejected:
			continue
		}
		if ref := normalizeObjectRef(entry.ProductRef); ref != "" {
			refs = append(refs, ref)
		}
		if ref := normalizeObjectRef(entry.PrimaryOperationalRef); ref != "" {
			refs = append(refs, ref)
		}
	}
	return uniqueStrings(refs)
}

func collectScopeArtifactNodeRefs(lens *models.Lens) []string {
	if lens == nil || len(lens.ScopeArtifacts) == 0 {
		return nil
	}
	out := make([]string, 0, 64)
	for _, ref := range lens.ScopeArtifacts {
		out = append(out, ref.NodeRefs...)
	}
	return filterObjectRefs(out)
}

func collectSourceJobIDs(artifacts []*models.LensArtifact, limit int) []string {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]string, 0, len(artifacts))
	seen := make(map[string]struct{}, len(artifacts))
	for _, art := range artifacts {
		if art == nil {
			continue
		}
		jobID := strings.TrimSpace(art.SourceJobID)
		if jobID == "" {
			continue
		}
		if _, ok := seen[jobID]; ok {
			continue
		}
		seen[jobID] = struct{}{}
		out = append(out, jobID)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func summarizeJobsFromArtifacts(artifacts []*models.LensArtifact, sampleSize, limit int) []jobRecoverySummary {
	if len(artifacts) == 0 {
		return nil
	}
	type agg struct {
		fragmentCount int
		edgeCount     int
		paths         []string
	}
	byJob := make(map[string]*agg)
	order := make([]string, 0, len(artifacts))
	for _, art := range artifacts {
		if art == nil {
			continue
		}
		jobID := strings.TrimSpace(art.SourceJobID)
		if jobID == "" {
			continue
		}
		item := byJob[jobID]
		if item == nil {
			item = &agg{}
			byJob[jobID] = item
			order = append(order, jobID)
		}
		switch art.Type {
		case models.LensArtifactTypeFragment:
			item.fragmentCount++
			if ref := normalizeObjectRef(art.CanonicalPath); ref != "" {
				item.paths = append(item.paths, ref)
			}
		case models.LensArtifactTypeEdge:
			item.edgeCount++
		}
	}
	out := make([]jobRecoverySummary, 0, len(order))
	for _, jobID := range order {
		item := byJob[jobID]
		if item == nil {
			continue
		}
		out = append(out, jobRecoverySummary{
			JobID:             jobID,
			ArtifactFragments: item.fragmentCount,
			ArtifactEdges:     item.edgeCount,
			RecoveredSamples:  sampleStrings(uniqueStrings(item.paths), sampleSize),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ArtifactFragments == out[j].ArtifactFragments {
			return out[i].JobID < out[j].JobID
		}
		return out[i].ArtifactFragments > out[j].ArtifactFragments
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func chainContextNodes(ctx *scout.ChainContext) []string {
	if ctx == nil {
		return nil
	}
	out := append([]string(nil), ctx.DiscoveredNodes...)
	for _, edge := range ctx.DiscoveredEdges {
		out = append(out, edge.Source, edge.Target)
	}
	return filterObjectRefs(out)
}

func filterObjectRefs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if ref := normalizeObjectRef(value); ref != "" {
			out = append(out, ref)
		}
	}
	return uniqueStrings(out)
}

func normalizeObjectRef(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || !strings.HasPrefix(value, "/") {
		return ""
	}
	switch {
	case strings.HasPrefix(value, "/query/"),
		strings.HasPrefix(value, "/domain/"),
		strings.HasPrefix(value, "/artifact/"),
		strings.HasPrefix(value, "/exposure/"):
		return ""
	default:
		return value
	}
}

func extractStringSlice(state map[string]interface{}, key string) []string {
	if len(state) == 0 {
		return nil
	}
	raw, ok := state[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return uniqueStrings(typed)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := asString(item); value != "" {
				out = append(out, value)
			}
		}
		return uniqueStrings(out)
	default:
		return nil
	}
}

func mapToSliceKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func unionSets(parts ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, part := range parts {
		for _, value := range part {
			value = normalizeObjectRef(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
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
	sort.Strings(out)
	return out
}

func countResolvedTrackedFragments(ids []string, resolved map[string]*models.HorizonFragment) int {
	count := 0
	for _, id := range ids {
		if resolved[strings.TrimSpace(id)] != nil {
			count++
		}
	}
	return count
}

func sampleStrings(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func trimJobSummaries(items []jobRecoverySummary, limit int) []jobRecoverySummary {
	if len(items) <= limit {
		return items
	}
	return append([]jobRecoverySummary(nil), items[:limit]...)
}

func printTextReport(r report) {
	fmt.Printf("lens %s %q created=%s updated=%s\n", r.Lens.ID, r.Lens.Name, r.Lens.CreatedAt, r.Lens.UpdatedAt)
	fmt.Printf("recovered union=%d workbenchCoverage=%.1f%% rawScopeCoverage=%.1f%% jobs=%d\n",
		r.Recovered.UnionRecoveredObjectRefs.Count,
		r.Overlap.WorkbenchObjects.CoveragePct,
		r.Overlap.RawScopeObjects.CoveragePct,
		r.Jobs.Total,
	)
	fmt.Printf("assessment=%s\n", r.Analysis.Assessment)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func asString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
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
