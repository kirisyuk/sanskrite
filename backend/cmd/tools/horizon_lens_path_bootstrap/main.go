package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type lensBootstrapSummary struct {
	ProjectID             string                `json:"projectId"`
	TenantID              string                `json:"tenantId"`
	LensID                string                `json:"lensId"`
	LensName              string                `json:"lensName"`
	SelectedCanonicalPath string                `json:"selectedCanonicalPath,omitempty"`
	VisibleNodeRefs       []string              `json:"visibleNodeRefs,omitempty"`
	Before                bootstrapScopeSummary `json:"before"`
	CandidatesFound       int                   `json:"candidatesFound"`
	Imported              int                   `json:"imported"`
	SkippedExisting       int                   `json:"skippedExisting"`
	CrossLensConflicts    []string              `json:"crossLensConflicts,omitempty"`
	ImportedArtifacts     []artifactImportRow   `json:"importedArtifacts,omitempty"`
	After                 bootstrapScopeSummary `json:"after"`
	Errors                []string              `json:"errors,omitempty"`
}

type bootstrapScopeSummary struct {
	VisibleNodes       int `json:"visibleNodes"`
	LensPaths          int `json:"lensPaths"`
	LensSupplyLinks    int `json:"lensSupplyLinks"`
	LensScopeArtifacts int `json:"lensScopeArtifacts"`
}

type artifactImportRow struct {
	CanonicalKey    string   `json:"canonicalKey"`
	DisplayName     string   `json:"displayName"`
	AnchorSupplier  string   `json:"anchorSupplier,omitempty"`
	AnchorCustomer  string   `json:"anchorCustomer,omitempty"`
	ProductAxis     string   `json:"productAxis,omitempty"`
	PathStressScore float64  `json:"pathStressScore,omitempty"`
	LegCount        int      `json:"legCount,omitempty"`
	NodeOverlap     int      `json:"nodeOverlap,omitempty"`
	ProductOverlap  int      `json:"productOverlap,omitempty"`
	NodeRefs        []string `json:"nodeRefs,omitempty"`
}

type lensCandidate struct {
	Lens  *horizonmodels.Lens
	Score int
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	lensID := flag.String("lens-id", "", "exact lens id")
	lensQuery := flag.String("lens-query", "asml", "lens substring when -lens-id is empty")
	maxNodes := flag.Int("max-nodes", 40, "max visible nodes to use")
	maxArtifacts := flag.Int("max-artifacts", 25, "max current lens artifacts to use when building visible scope")
	importLimit := flag.Int("import-limit", 40, "max matching global artifacts to import into the lens")
	outdir := flag.String("outdir", "", "output directory")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	client, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer client.Close()

	lensesRepo := horizonrepo.NewLensesRepository(client)
	lensArtifactsRepo := horizonrepo.NewLensArtifactsRepository(client)
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(client)

	lens, err := resolveLens(ctx, client, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), strings.TrimSpace(*lensQuery))
	if err != nil {
		log.Fatalf("resolve lens failed: %v", err)
	}
	log.Printf("using lens %s (%s)", strings.TrimSpace(lens.ID), strings.TrimSpace(lens.Name))

	visibleNodeRefs, selectedCanonicalPath, err := buildVisibleNodeScope(ctx, lens, lensArtifactsRepo, materializedRepo, *maxNodes, *maxArtifacts)
	if err != nil {
		log.Fatalf("build visible scope failed: %v", err)
	}
	log.Printf("visible scope ready: nodes=%d selected=%s", len(visibleNodeRefs), selectedCanonicalPath)

	outPath := strings.TrimSpace(*outdir)
	if outPath == "" {
		outPath = filepath.Join("tmp", fmt.Sprintf("lens_path_bootstrap_%s_%s", strings.TrimSpace(lens.ID), time.Now().UTC().Format("20060102_150405")))
	}
	if err := os.MkdirAll(outPath, 0o755); err != nil {
		log.Fatalf("mkdir failed: %v", err)
	}

	summary := lensBootstrapSummary{
		ProjectID:             strings.TrimSpace(*projectID),
		TenantID:              strings.TrimSpace(lens.TenantID),
		LensID:                strings.TrimSpace(lens.ID),
		LensName:              strings.TrimSpace(lens.Name),
		SelectedCanonicalPath: selectedCanonicalPath,
		VisibleNodeRefs:       append([]string(nil), visibleNodeRefs...),
		Before:                summarizeLensScope(ctx, lens, visibleNodeRefs, materializedRepo, lensesRepo),
	}

	log.Printf("collecting bootstrap candidates")
	candidates, err := collectBootstrapCandidates(ctx, strings.TrimSpace(lens.TenantID), visibleNodeRefs, materializedRepo)
	if err != nil {
		log.Fatalf("collect candidates failed: %v", err)
	}
	summary.CandidatesFound = len(candidates)
	log.Printf("candidates found: %d", len(candidates))
	if *importLimit > 0 && len(candidates) > *importLimit {
		candidates = candidates[:*importLimit]
	}

	log.Printf("importing up to %d artifacts into lens", len(candidates))
	importedRows, importedCount, skippedExisting, crossLensConflicts, err := importArtifactsIntoLens(ctx, lens, candidates, materializedRepo, lensesRepo)
	if err != nil {
		log.Fatalf("import artifacts failed: %v", err)
	}
	log.Printf("import finished: imported=%d skipped=%d conflicts=%d", importedCount, skippedExisting, len(crossLensConflicts))
	summary.ImportedArtifacts = importedRows
	summary.Imported = importedCount
	summary.SkippedExisting = skippedExisting
	summary.CrossLensConflicts = crossLensConflicts
	summary.After = summarizeLensScope(ctx, lens, visibleNodeRefs, materializedRepo, lensesRepo)

	writeJSON(filepath.Join(outPath, "summary.json"), summary)
	writeText(filepath.Join(outPath, "report.md"), buildReport(summary))

	fmt.Printf("Diagnostics saved to %s\n", outPath)
	fmt.Printf("Lens: %s (%s)\n", summary.LensName, summary.LensID)
	fmt.Printf("Before: paths=%d supplyLinks=%d scopeArtifacts=%d\n", summary.Before.LensPaths, summary.Before.LensSupplyLinks, summary.Before.LensScopeArtifacts)
	fmt.Printf("Candidates found: %d | imported=%d | skippedExisting=%d\n", summary.CandidatesFound, summary.Imported, summary.SkippedExisting)
	fmt.Printf("After: paths=%d supplyLinks=%d scopeArtifacts=%d\n", summary.After.LensPaths, summary.After.LensSupplyLinks, summary.After.LensScopeArtifacts)
}

func collectBootstrapCandidates(ctx context.Context, tenantID string, visibleNodeRefs []string, materializedRepo *horizonrepo.MaterializedArtifactsRepository) ([]artifactImportRow, error) {
	out := []artifactImportRow{}
	refs := compactStrings(visibleNodeRefs)
	if len(refs) == 0 || materializedRepo == nil {
		return out, nil
	}
	visibleProducts := make(map[string]struct{})
	visibleRefSet := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		visibleRefSet[ref] = struct{}{}
		if strings.HasPrefix(ref, "/product/") {
			visibleProducts[ref] = struct{}{}
		}
	}

	artifacts := []*horizonmodels.MaterializedArtifact{}
	for _, candidateTenant := range compactStrings([]string{strings.TrimSpace(tenantID), "global"}) {
		found, err := materializedRepo.FindByNodeRefsAny(ctx, candidateTenant, refs, horizonmodels.MaterializedKindSupplyChainPath, 200)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, found...)
	}

	seen := map[string]struct{}{}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		key := strings.TrimSpace(firstNonEmpty(artifact.CanonicalKey, artifact.ID))
		if key == "" {
			continue
		}
		if _, ok := seen[artifact.TenantID+"|"+key]; ok {
			continue
		}
		seen[artifact.TenantID+"|"+key] = struct{}{}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		nodeOverlap := overlapCount(artifact.NodeRefs, visibleRefSet)
		productOverlap := 0
		if _, ok := visibleProducts[strings.TrimSpace(artifact.ProductAxis)]; ok {
			productOverlap++
		}
		if nodeOverlap == 0 && productOverlap == 0 {
			continue
		}
		row := artifactImportRow{
			CanonicalKey:    strings.TrimSpace(artifact.CanonicalKey),
			DisplayName:     strings.TrimSpace(artifact.DisplayName),
			AnchorSupplier:  strings.TrimSpace(artifact.AnchorSupplier),
			AnchorCustomer:  strings.TrimSpace(artifact.AnchorCustomer),
			ProductAxis:     strings.TrimSpace(artifact.ProductAxis),
			PathStressScore: artifact.PathStressScore,
			LegCount:        artifact.LegCount,
			NodeOverlap:     nodeOverlap,
			ProductOverlap:  productOverlap,
			NodeRefs:        intersectRefs(artifact.NodeRefs, visibleRefSet),
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		leftScore := left.NodeOverlap*10 + left.ProductOverlap*5
		rightScore := right.NodeOverlap*10 + right.ProductOverlap*5
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.PathStressScore != right.PathStressScore {
			return left.PathStressScore > right.PathStressScore
		}
		return left.CanonicalKey < right.CanonicalKey
	})
	return out, nil
}

func importArtifactsIntoLens(ctx context.Context, lens *horizonmodels.Lens, candidates []artifactImportRow, materializedRepo *horizonrepo.MaterializedArtifactsRepository, lensesRepo *horizonrepo.LensesRepository) ([]artifactImportRow, int, int, []string, error) {
	if lens == nil || materializedRepo == nil || lensesRepo == nil {
		return nil, 0, 0, nil, nil
	}
	imported := make([]artifactImportRow, 0, len(candidates))
	importedCount := 0
	skippedExisting := 0
	conflicts := []string{}
	for _, row := range candidates {
		globalArtifact, err := materializedRepo.GetByCanonicalKeyAnyKind(ctx, "global", strings.TrimSpace(row.CanonicalKey))
		if err != nil || globalArtifact == nil {
			globalArtifact, err = materializedRepo.GetByCanonicalKeyAnyKind(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(row.CanonicalKey))
			if err != nil || globalArtifact == nil {
				continue
			}
		}
		existing, err := materializedRepo.GetByCanonicalKey(ctx, strings.TrimSpace(lens.TenantID), horizonmodels.MaterializedKindSupplyChainPath, strings.TrimSpace(row.CanonicalKey))
		if err == nil && existing != nil {
			if strings.TrimSpace(existing.LensID) != "" && strings.TrimSpace(existing.LensID) != strings.TrimSpace(lens.ID) {
				conflicts = append(conflicts, fmt.Sprintf("%s already belongs to lens %s", strings.TrimSpace(existing.CanonicalKey), strings.TrimSpace(existing.LensID)))
				continue
			}
			if strings.TrimSpace(existing.LensID) == strings.TrimSpace(lens.ID) {
				skippedExisting++
				if err := lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lens.ID), strings.TrimSpace(existing.CanonicalKey), scopeArtifactRefFromMaterializedArtifact(existing)); err != nil {
					return imported, importedCount, skippedExisting, conflicts, err
				}
				continue
			}
			existing.LensID = strings.TrimSpace(lens.ID)
			existing.UpdatedAt = time.Now().UTC()
			existing.UpdatedBy = "tool:horizon_lens_path_bootstrap"
			if err := materializedRepo.Update(ctx, existing); err != nil {
				return imported, importedCount, skippedExisting, conflicts, err
			}
			if err := lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lens.ID), strings.TrimSpace(existing.CanonicalKey), scopeArtifactRefFromMaterializedArtifact(existing)); err != nil {
				return imported, importedCount, skippedExisting, conflicts, err
			}
			imported = append(imported, row)
			importedCount++
			continue
		}

		clone := *globalArtifact
		clone.ID = ""
		clone.TenantID = strings.TrimSpace(lens.TenantID)
		clone.LensID = strings.TrimSpace(lens.ID)
		clone.CreatedAt = time.Time{}
		clone.UpdatedAt = time.Time{}
		clone.CreatedBy = "tool:horizon_lens_path_bootstrap"
		clone.UpdatedBy = "tool:horizon_lens_path_bootstrap"
		id, err := materializedRepo.Create(ctx, &clone)
		if err != nil {
			return imported, importedCount, skippedExisting, conflicts, err
		}
		created, err := materializedRepo.Get(ctx, id)
		if err != nil {
			return imported, importedCount, skippedExisting, conflicts, err
		}
		if err := lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lens.ID), strings.TrimSpace(created.CanonicalKey), scopeArtifactRefFromMaterializedArtifact(created)); err != nil {
			return imported, importedCount, skippedExisting, conflicts, err
		}
		imported = append(imported, row)
		importedCount++
	}
	return imported, importedCount, skippedExisting, conflicts, nil
}

func summarizeLensScope(ctx context.Context, lens *horizonmodels.Lens, visibleNodeRefs []string, materializedRepo *horizonrepo.MaterializedArtifactsRepository, lensesRepo *horizonrepo.LensesRepository) bootstrapScopeSummary {
	out := bootstrapScopeSummary{VisibleNodes: len(compactStrings(visibleNodeRefs))}
	if lens == nil || materializedRepo == nil {
		return out
	}
	artifacts, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 500)
	if err != nil {
		return out
	}
	supplyLinkSet := map[string]struct{}{}
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		out.LensPaths++
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			for _, leg := range parsed.Legs {
				ref := strings.TrimSpace(leg.SupplyLinkCanonicalPath)
				if ref != "" {
					supplyLinkSet[ref] = struct{}{}
				}
			}
		}
	}
	out.LensSupplyLinks = len(supplyLinkSet)
	out.LensScopeArtifacts = len(lens.ScopeArtifacts)
	if lensesRepo != nil {
		if freshLens, err := lensesRepo.Get(ctx, strings.TrimSpace(lens.ID)); err == nil && freshLens != nil {
			out.LensScopeArtifacts = len(freshLens.ScopeArtifacts)
		}
	}
	return out
}

func buildVisibleNodeScope(ctx context.Context, lens *horizonmodels.Lens, lensArtifactsRepo *horizonrepo.LensArtifactsRepository, materializedRepo *horizonrepo.MaterializedArtifactsRepository, maxNodes, maxArtifacts int) ([]string, string, error) {
	materialized, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 500)
	if err != nil {
		return nil, "", err
	}
	lensArtifacts, err := lensArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 2000)
	if err != nil {
		return nil, "", err
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
		if supplyPaths[i].PathStressScore == supplyPaths[j].PathStressScore {
			return supplyPaths[i].UpdatedAt.After(supplyPaths[j].UpdatedAt)
		}
		return supplyPaths[i].PathStressScore > supplyPaths[j].PathStressScore
	})
	if maxArtifacts > 0 && len(supplyPaths) > maxArtifacts {
		supplyPaths = supplyPaths[:maxArtifacts]
	}
	selected := pickSelectedCanonicalPath(lens, supplyPaths)
	return collectVisibleNodeRefs(lens, lensArtifacts, supplyPaths, selected, maxNodes), selected, nil
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
		return nil, err
	}
	candidates := make([]lensCandidate, 0, len(docs))
	needle := strings.ToLower(strings.TrimSpace(lensQuery))
	for _, doc := range docs {
		var lens horizonmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		score := scoreLens(&lens, needle)
		if score > 0 {
			candidates = append(candidates, lensCandidate{Lens: &lens, Score: score})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no lenses matched %q", lensQuery)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Lens.UpdatedAt.After(candidates[j].Lens.UpdatedAt)
	})
	return candidates[0].Lens, nil
}

func scoreLens(lens *horizonmodels.Lens, needle string) int {
	if lens == nil || needle == "" {
		return 0
	}
	score := 0
	name := strings.ToLower(strings.TrimSpace(lens.Name))
	query := strings.ToLower(strings.TrimSpace(lens.OriginalQuery))
	if strings.Contains(name, needle) {
		score += 100
	}
	if strings.Contains(query, needle) {
		score += 80
	}
	for _, ref := range lens.Scope {
		if strings.Contains(strings.ToLower(strings.TrimSpace(ref)), needle) {
			score += 30
		}
	}
	return score
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
		if ref == "" || !strings.HasPrefix(ref, "/") {
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

func scopeArtifactRefFromMaterializedArtifact(artifact *horizonmodels.MaterializedArtifact) horizonmodels.ScopeArtifactRef {
	if artifact == nil {
		return horizonmodels.ScopeArtifactRef{}
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
	ref := horizonmodels.ScopeArtifactRef{
		Kind:             string(artifact.Kind),
		DisplayName:      strings.TrimSpace(artifact.DisplayName),
		AnchorSupplier:   strings.TrimSpace(artifact.AnchorSupplier),
		AnchorCustomer:   strings.TrimSpace(artifact.AnchorCustomer),
		ProductAxis:      strings.TrimSpace(artifact.ProductAxis),
		NodeRefs:         compactStrings(artifact.NodeRefs),
		Status:           strings.TrimSpace(artifact.PathStatus),
		GeoRefs:          compactStrings(artifact.GeoRefs),
		FlowRefs:         compactStrings(artifact.FlowRefs),
		ZoneRefs:         compactStrings(artifact.ZoneRefs),
		AffectedZoneRefs: compactStrings(artifact.AffectedZoneRefs),
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
		ref.GeoRefs = summary.GeoRefs
		ref.FlowRefs = summary.FlowRefs
		ref.ZoneRefs = summary.ZoneRefs
		ref.AffectedZoneRefs = summary.AffectedZoneRefs
		ref.SpatialSemantics = summary.SpatialSemantics
		ref.LegCount = summary.LegCount
		ref.BindingConfidence = summary.BindingConfidence
		ref.PathStressScore = summary.PathStressScore
		ref.BottleneckLegIndex = summary.BottleneckLegIndex
		ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
	}
	return ref
}

func buildReport(summary lensBootstrapSummary) string {
	var b strings.Builder
	b.WriteString("# Lens Path Bootstrap Report\n\n")
	b.WriteString(fmt.Sprintf("- Lens: **%s** (`%s`)\n", summary.LensName, summary.LensID))
	b.WriteString(fmt.Sprintf("- Tenant: `%s`\n", summary.TenantID))
	if summary.SelectedCanonicalPath != "" {
		b.WriteString(fmt.Sprintf("- Selected anchor: `%s`\n", summary.SelectedCanonicalPath))
	}
	b.WriteString(fmt.Sprintf("- Visible nodes used: **%d**\n", summary.Before.VisibleNodes))
	b.WriteString(fmt.Sprintf("- Before: **%d** lens paths, **%d** lens supply links, **%d** scope artifacts\n", summary.Before.LensPaths, summary.Before.LensSupplyLinks, summary.Before.LensScopeArtifacts))
	b.WriteString(fmt.Sprintf("- Candidates found: **%d**; imported into lens: **%d**\n", summary.CandidatesFound, summary.Imported))
	b.WriteString(fmt.Sprintf("- After: **%d** lens paths, **%d** lens supply links, **%d** scope artifacts\n\n", summary.After.LensPaths, summary.After.LensSupplyLinks, summary.After.LensScopeArtifacts))

	b.WriteString("## Imported Paths\n\n")
	if len(summary.ImportedArtifacts) == 0 {
		b.WriteString("No artifacts were imported.\n\n")
	} else {
		for i, item := range summary.ImportedArtifacts {
			if i >= 12 {
				break
			}
			b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, firstNonEmpty(item.DisplayName, item.CanonicalKey)))
			b.WriteString(fmt.Sprintf("   - Product: **%s**\n", humanizeRef(item.ProductAxis)))
			b.WriteString(fmt.Sprintf("   - Supplier -> Customer: `%s` -> `%s`\n", item.AnchorSupplier, item.AnchorCustomer))
			b.WriteString(fmt.Sprintf("   - Overlap: nodes=%d, products=%d; path stress=%.2f\n", item.NodeOverlap, item.ProductOverlap, item.PathStressScore))
		}
		b.WriteString("\n")
	}

	if len(summary.CrossLensConflicts) > 0 {
		b.WriteString("## Conflicts\n\n")
		for _, item := range summary.CrossLensConflicts {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}
	if len(summary.Errors) > 0 {
		b.WriteString("## Errors\n\n")
		for _, item := range summary.Errors {
			b.WriteString("- " + item + "\n")
		}
	}
	return b.String()
}

func overlapCount(values []string, allowed map[string]struct{}) int {
	count := 0
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := allowed[value]; !ok {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		count++
	}
	return count
}

func intersectRefs(values []string, allowed map[string]struct{}) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := allowed[value]; ok {
			out = append(out, value)
		}
	}
	return compactStrings(out)
}

func compactStrings(values []string) []string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func humanizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "Unknown"
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 0 {
		return ref
	}
	return humanizeSlug(parts[len(parts)-1])
}

func humanizeSlug(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	words := strings.Fields(value)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatalf("marshal %s failed: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		log.Fatalf("write %s failed: %v", path, err)
	}
}

func writeText(path string, text string) {
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		log.Fatalf("write %s failed: %v", path, err)
	}
}
