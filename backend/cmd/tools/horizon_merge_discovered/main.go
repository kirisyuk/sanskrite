package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Migration tool:
// - consolidates historical "/discovered/*" Horizon fragments into stable canonical namespaces
// - rewrites edges and frames to point to canonical fragments
// - removes old discovered Firestore docs and their GCS JSON docs
// - triggers FULL reconcile in Vertex AI Search to purge deleted documents from the index

type canonicalizeResult struct {
	CanonicalPath string  `json:"canonicalPath"`
	TargetType    string  `json:"targetType,omitempty"` // ORGANIZATION|FACILITY|GEO_REGION|PRODUCT|EVENT
	Reason        string  `json:"reason,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

func main() {
	var (
		projectID     = flag.String("project", getenvDefault("GCP_PROJECT", "h1horizon-488615"), "GCP project id")
		apply         = flag.Bool("apply", false, "Apply changes (default: dry-run only)")
		dryRun        = flag.Bool("dry-run", false, "Force dry-run even if --apply is provided")
		limit         = flag.Int("limit", 0, "Max number of discovered fragments to process (0 = no limit)")
		tenantID      = flag.String("tenant", "global", "TenantID for Horizon fragments (normally 'global')")
		vertexStoreID = flag.String("vertex-store", "", "Vertex AI Search data store ID (default from env or prod)")
		vertexLoc     = flag.String("vertex-location", "", "Vertex AI Search location (default from env or prod)")
		ragBucket     = flag.String("rag-bucket", "", "GCS bucket used for Vertex indexing (default from env or prod)")
		fullReconcile = flag.Bool("vertex-full-reconcile", true, "Run FULL Vertex AI Search import for horizon_fragments at the end (apply mode only)")
	)
	flag.Parse()
	if *dryRun {
		*apply = false
	}

	ctx := context.Background()

	// Defaults consistent with production Cloud Run.
	if *vertexStoreID == "" {
		*vertexStoreID = os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	}
	if *vertexStoreID == "" {
		*vertexStoreID = "rag-documents-store"
		log.Printf(`INFO: VERTEX_SEARCH_DATA_STORE_ID not set; defaulting to %q`, *vertexStoreID)
	}

	if *vertexLoc == "" {
		*vertexLoc = os.Getenv("VERTEX_SEARCH_LOCATION")
	}
	if *vertexLoc == "" {
		*vertexLoc = "global"
		log.Printf(`INFO: VERTEX_SEARCH_LOCATION not set; defaulting to %q`, *vertexLoc)
	}

	if *ragBucket == "" {
		*ragBucket = os.Getenv("RAG_DOCUMENTS_BUCKET")
	}
	if *ragBucket == "" {
		*ragBucket = fmt.Sprintf("%s-rag-documents", *projectID)
		log.Printf(`INFO: RAG_DOCUMENTS_BUCKET not set; defaulting to %q`, *ragBucket)
	}

	log.Printf("=== Horizon Migration: Merge /discovered fragments ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d vertexStore=%s vertexLoc=%s ragBucket=%s",
		*projectID, *tenantID, *apply, *limit, *vertexStoreID, *vertexLoc, *ragBucket)

	// Firestore
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	// GCS (for removing old discovered JSON docs + re-indexing)
	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init GCS client: %v", err)
	}

	// Vertex Search client (OAuth/ADC)
	vertexSearchClient := ai.NewVertexAISearchClient(*projectID, *vertexLoc, *vertexStoreID, "")
	if !vertexSearchClient.IsConfigured() {
		log.Printf("WARNING: Vertex AI Search client is not fully configured; will rely more on LLM canonicalization.")
	}

	// AI executor for canonicalization decisions (strict JSON)
	execAI := buildAITaskExecutorOrFatal()

	// Repositories (use repo to keep GCS export + Vertex sync behavior)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db).
		WithStorage(gcsClient, *ragBucket).
		WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
			if vertexSearchClient == nil || !vertexSearchClient.IsConfigured() {
				return "", fmt.Errorf("vertex search client not configured")
			}
			if strings.TrimSpace(gcsURI) == "" {
				return "", nil
			}
			return vertexSearchClient.ImportDocumentsFromURIs(ctx, []string{gcsURI}, "INCREMENTAL", "document")
		})

	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	framesRepo := horizonrepo.NewFramesRepository(db)

	discovered, err := listDiscoveredFragments(ctx, db, *tenantID, *limit)
	if err != nil {
		log.Fatalf("FATAL: failed to list discovered fragments: %v", err)
	}
	log.Printf("Found %d fragments with canonicalPath prefix /discovered/", len(discovered))
	if len(discovered) == 0 {
		// Even if there is nothing to migrate, we may still want to purge stale docs
		// from Vertex index (e.g., if GCS objects were missing and we rely on FULL reconcile).
		if *apply && *fullReconcile && vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
			log.Printf("Nothing to migrate, but apply=true and vertex-full-reconcile=true. Triggering Vertex AI Search FULL reconcile for horizon_fragments...")
			gcsPath := fmt.Sprintf("gs://%s/%s/horizon_fragments/**", *ragBucket, *tenantID)
			op, err := vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
				GCSPath:            gcsPath,
				ReconciliationMode: "FULL",
				DataSchema:         "document",
			})
			if err != nil {
				log.Printf("WARNING: FULL reconcile failed: %v", err)
			} else {
				log.Printf("Vertex import operation started: %s", op)
			}
		}
		log.Printf("Nothing to migrate. Exiting.")
		return
	}

	var (
		processed             int
		createdCanonical      int
		updatedCanonical      int
		deletedDiscovered     int
		updatedEdges          int
		deletedDuplicateEdges int
		updatedFrames         int
	)

	for i, d := range discovered {
		log.Printf("---- [%d/%d] discovered fragment: id=%s canonical=%s type=%s ----", i+1, len(discovered), d.ID, d.CanonicalPath, d.Type)

		targetCP, targetType, err := chooseCanonicalPath(ctx, vertexSearchClient, execAI, d)
		if err != nil {
			log.Printf("ERROR: canonicalization failed for %s (%s): %v", d.ID, d.CanonicalPath, err)
			continue
		}
		if strings.HasPrefix(targetCP, "/discovered/") {
			log.Printf("ERROR: refusing to migrate into forbidden canonicalPath=%s", targetCP)
			continue
		}
		log.Printf("Chosen canonicalPath=%s targetType=%s", targetCP, targetType)

		// Ensure canonical fragment exists
		canon, err := fragmentsRepo.GetByCanonicalPath(ctx, targetCP)
		if err != nil {
			// Create canonical fragment by copying discovered data
			newFrag := cloneFragmentForCanonical(d, targetCP, targetType)
			if *apply {
				_, err := fragmentsRepo.Create(ctx, newFrag)
				if err != nil {
					log.Printf("ERROR: failed to create canonical fragment for %s: %v", targetCP, err)
					continue
				}
				createdCanonical++
				// Re-fetch to get ID
				canon, err = fragmentsRepo.GetByCanonicalPath(ctx, targetCP)
				if err != nil {
					log.Printf("ERROR: failed to re-fetch newly created canonical fragment: %v", err)
					continue
				}
			} else {
				log.Printf("[dry-run] would CREATE canonical fragment: %s", targetCP)
				createdCanonical++
				continue
			}
		} else {
			// Merge data into existing canonical
			changed := mergeFragment(canon, d)
			if changed {
				if *apply {
					if err := fragmentsRepo.Update(ctx, canon); err != nil {
						log.Printf("ERROR: failed to update canonical fragment %s (%s): %v", canon.ID, canon.CanonicalPath, err)
						continue
					}
					updatedCanonical++
				} else {
					log.Printf("[dry-run] would UPDATE canonical fragment: %s (id=%s)", canon.CanonicalPath, canon.ID)
					updatedCanonical++
					continue
				}
			} else {
				log.Printf("Canonical fragment already contains all discovered fields; no update needed.")
			}
		}

		// Migrate edges (replace discovered fragment ID with canonical ID)
		if *apply {
			u, dd, err := migrateEdges(ctx, db, edgesRepo, d.ID, canon.ID)
			if err != nil {
				log.Printf("ERROR: edge migration failed for %s -> %s: %v", d.ID, canon.ID, err)
				continue
			}
			updatedEdges += u
			deletedDuplicateEdges += dd
		}

		// Migrate frames (targetCanonicalPath references)
		if *apply {
			n, err := migrateFrames(ctx, db, framesRepo, d.CanonicalPath, canon.CanonicalPath, *tenantID)
			if err != nil {
				log.Printf("ERROR: frame migration failed for %s -> %s: %v", d.CanonicalPath, canon.CanonicalPath, err)
				continue
			}
			updatedFrames += n
		}

		// Delete discovered fragment + its GCS doc to avoid it staying in Vertex index.
		if *apply {
			if err := fragmentsRepo.Delete(ctx, d.ID); err != nil {
				log.Printf("ERROR: failed to delete discovered fragment %s: %v", d.ID, err)
				continue
			}
			deletedDiscovered++

			objPath := fmt.Sprintf("%s/horizon_fragments/%s.json", *tenantID, d.ID)
			if err := gcsClient.DeleteObject(ctx, *ragBucket, objPath); err != nil {
				log.Printf("WARNING: failed to delete GCS object %s/%s: %v (Vertex index may still contain it until FULL reconcile)", *ragBucket, objPath, err)
			}
		} else {
			log.Printf("[dry-run] would DELETE discovered fragment: %s (id=%s) and its GCS JSON", d.CanonicalPath, d.ID)
			deletedDiscovered++
		}

		processed++
	}

	// Vertex reconciliation to purge deleted docs from the index
	if *apply && *fullReconcile && vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
		log.Printf("Triggering Vertex AI Search FULL reconcile for horizon_fragments...")
		gcsPath := fmt.Sprintf("gs://%s/%s/horizon_fragments/**", *ragBucket, *tenantID)
		op, err := vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
			GCSPath:            gcsPath,
			ReconciliationMode: "FULL",
			DataSchema:         "document",
		})
		if err != nil {
			log.Printf("WARNING: FULL reconcile failed: %v", err)
		} else {
			log.Printf("Vertex import operation started: %s", op)
		}
	}

	log.Printf("=== Migration Summary ===")
	log.Printf("processed=%d createdCanonical=%d updatedCanonical=%d deletedDiscovered=%d updatedEdges=%d deletedDuplicateEdges=%d updatedFrames=%d apply=%v",
		processed, createdCanonical, updatedCanonical, deletedDiscovered, updatedEdges, deletedDuplicateEdges, updatedFrames, *apply)
}

func getenvDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func listDiscoveredFragments(ctx context.Context, db *firestore.Client, tenantID string, limit int) ([]*horizonmodels.HorizonFragment, error) {
	// Prefix query: canonicalPath in ["/discovered/", "/discovered/\uf8ff"]
	q := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenantID).
		Where("canonicalPath", ">=", "/discovered/").
		Where("canonicalPath", "<=", "/discovered/\uf8ff").
		OrderBy("canonicalPath", firestore.Asc)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var out []*horizonmodels.HorizonFragment
	for {
		if limit > 0 && len(out) >= limit {
			break
		}
		doc, err := iter.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			return nil, err
		}
		var f horizonmodels.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/discovered/") {
			out = append(out, &f)
		}
	}
	return out, nil
}

func buildAITaskExecutorOrFatal() *ai.AITaskExecutor {
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = getSecret("VERTEX_API_KEY")
	}
	if apiKey == "" {
		apiKey = getSecret("gemini_api_key")
	}
	if apiKey == "" {
		log.Fatal("FATAL: Could not find VERTEX_API_KEY or gemini_api_key in env or Secret Manager")
	}

	modelManager := ai.NewAIModelManager()

	var services []ai.AIService
	services = append(services, ai.NewGeminiService(apiKey, modelManager))

	if openaiKey := firstNonEmpty(getSecret("OPENAI_API_KEY"), getSecret("openai_api_key")); openaiKey != "" {
		services = append(services, ai.NewOpenAIService(openaiKey, modelManager))
	}
	if grokKey := firstNonEmpty(getSecret("GROK_API_KEY"), getSecret("grok_api_key"), getSecret("xai_api_key")); grokKey != "" {
		services = append(services, ai.NewGrokService(grokKey, modelManager))
	}

	return ai.NewAITaskExecutor(services, modelManager)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func getSecret(name string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret", name, "--project", getenvDefault("GCP_PROJECT", "h1horizon-488615"))
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func chooseCanonicalPath(ctx context.Context, vertex *ai.VertexAISearchClient, execAI *ai.AITaskExecutor, discovered *horizonmodels.HorizonFragment) (canonicalPath string, targetType string, err error) {
	name := discovered.Labels["en"]
	if strings.TrimSpace(name) == "" {
		// fallback: any label
		for _, v := range discovered.Labels {
			if strings.TrimSpace(v) != "" {
				name = v
				break
			}
		}
	}
	if strings.TrimSpace(name) == "" {
		name = slugFallbackFromPath(discovered.CanonicalPath)
	}

	type candidate struct {
		CanonicalPath string  `json:"canonicalPath"`
		Score         float64 `json:"score,omitempty"`
		Type          string  `json:"type,omitempty"`
	}
	var candidates []candidate
	if vertex != nil && vertex.IsConfigured() {
		// Use dedicated Horizon Fragments datastore if configured (store isolation; no filters).
		if ds := strings.TrimSpace(getenvDefault("VERTEX_SEARCH_HORIZON_DATA_STORE_ID", "")); ds != "" {
			vertex = vertex.WithDataStore(ds)
		}
		req := &ai.SearchRequest{
			Query:    name,
			PageSize: 10,
		}
		resp, vErr := vertex.Search(ctx, req)
		if vErr == nil && resp != nil {
			for _, r := range resp.Results {
				if r.Document.StructData == nil {
					continue
				}
				cp, _ := r.Document.StructData["canonicalPath"].(string)
				typ, _ := r.Document.StructData["type"].(string)
				cp = strings.TrimSpace(cp)
				if cp == "" || strings.HasPrefix(cp, "/discovered/") {
					continue
				}
				candidates = append(candidates, candidate{CanonicalPath: cp, Score: r.RelevanceScore, Type: typ})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > 10 {
		candidates = candidates[:10]
	}

	prompt := buildCanonicalizeMigrationPrompt(discovered, name, candidates)
	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         6000,
			MaxOutputSize:            1200,
		},
		Verifier: ai.NewSimpleJSONVerifier(),
	}
	res, err := execAI.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return "", "", err
	}
	b, _ := json.Marshal(res)
	var out canonicalizeResult
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", err
	}
	out.CanonicalPath = strings.TrimSpace(out.CanonicalPath)
	if out.CanonicalPath == "" {
		return "", "", fmt.Errorf("LLM returned empty canonicalPath")
	}
	if strings.HasPrefix(out.CanonicalPath, "/discovered/") {
		return "", "", fmt.Errorf("LLM returned forbidden /discovered canonicalPath: %s", out.CanonicalPath)
	}
	return out.CanonicalPath, strings.TrimSpace(out.TargetType), nil
}

func buildCanonicalizeMigrationPrompt(discovered *horizonmodels.HorizonFragment, name string, candidates interface{}) string {
	discJSON, _ := json.MarshalIndent(map[string]interface{}{
		"id":            discovered.ID,
		"canonicalPath": discovered.CanonicalPath,
		"type":          string(discovered.Type),
		"labels":        discovered.Labels,
		"description":   discovered.Description,
	}, "", "  ")
	candJSON, _ := json.MarshalIndent(candidates, "", "  ")

	return fmt.Sprintf(`You are a Horizon Canonicalization & Merge Agent.

GOAL:
We are migrating historical Horizon fragments that were stored under "/discovered/*" into stable canonical namespaces (e.g. /org/*).
You MUST choose a stable canonicalPath that does NOT start with "/discovered/".

DISCOVERED FRAGMENT (JSON):
%s

NAME HINT:
%q

VERTEX SEARCH CANDIDATES (JSON):
%s

OUTPUT:
Return ONE JSON object matching EXACTLY this schema:
{
  "canonicalPath": "string",
  "targetType": "ORGANIZATION|FACILITY|GEO_REGION|PRODUCT|EVENT",
  "confidence": 0.0,
  "reason": "string"
}

RULES:
- canonicalPath MUST NOT start with "/discovered/".
- Prefer an existing candidate canonicalPath from VERTEX SEARCH CANDIDATES if it is the same real-world entity.
- If none of the candidates are the same entity, CREATE a new stable canonicalPath in the correct namespace:
  - ORGANIZATION: /org/{lowercase-slug}
  - PRODUCT: /product/{lowercase-slug}
  - FACILITY: /fac/{lowercase-slug}
  - GEO_REGION: /region/{lowercase-slug} (or /loc/... if you can)
  - EVENT: /event/{lowercase-slug}
- Output JSON only. No markdown. No extra text.
`, string(discJSON), name, string(candJSON))
}

func slugFallbackFromPath(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 0 {
		return cp
	}
	last := parts[len(parts)-1]
	last = strings.ReplaceAll(last, "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	return last
}

func cloneFragmentForCanonical(src *horizonmodels.HorizonFragment, canonicalPath string, targetType string) *horizonmodels.HorizonFragment {
	dst := *src // shallow copy
	dst.ID = ""
	dst.CanonicalPath = canonicalPath
	dst.TenantID = "global"
	dst.Governance.Owner = "SYSTEM"
	if dst.Governance.Status == "" {
		dst.Governance.Status = string(horizonmodels.FragmentStatusActive)
	}
	if strings.TrimSpace(targetType) != "" {
		dst.Type = horizonmodels.FragmentType(strings.TrimSpace(strings.ToUpper(targetType)))
	}
	if dst.Type == "" {
		dst.Type = src.Type
	}
	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
	}
	addLegacyPathMarkers(&dst, src.CanonicalPath)
	dst.Version = 0
	dst.CreatedAt = time.Time{}
	dst.UpdatedAt = time.Time{}
	dst.Metadata = horizonmodels.FragmentMetadata{}
	return &dst
}

func mergeFragment(dst *horizonmodels.HorizonFragment, src *horizonmodels.HorizonFragment) bool {
	changed := false

	if dst.TenantID == "" {
		dst.TenantID = "global"
		changed = true
	}
	if dst.Governance.Owner != "SYSTEM" {
		dst.Governance.Owner = "SYSTEM"
		changed = true
	}

	addLegacyPathMarkers(dst, src.CanonicalPath)

	if dst.Type == "" && src.Type != "" {
		dst.Type = src.Type
		changed = true
	}

	if dst.Labels == nil && src.Labels != nil {
		dst.Labels = map[string]string{}
		changed = true
	}
	for k, v := range src.Labels {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Labels[k]) == "" {
			dst.Labels[k] = v
			changed = true
		}
	}

	if strings.TrimSpace(dst.Description) == "" && strings.TrimSpace(src.Description) != "" {
		dst.Description = src.Description
		changed = true
	}

	if strings.TrimSpace(dst.EntityPath) == "" && strings.TrimSpace(src.EntityPath) != "" {
		dst.EntityPath = src.EntityPath
		changed = true
	}
	if strings.TrimSpace(dst.SectorPath) == "" && strings.TrimSpace(src.SectorPath) != "" {
		dst.SectorPath = src.SectorPath
		changed = true
	}

	if dst.HomeLocation == nil && src.HomeLocation != nil {
		dst.HomeLocation = src.HomeLocation
		changed = true
	}

	if dst.Identifiers == nil && src.Identifiers != nil {
		dst.Identifiers = map[string]string{}
		changed = true
	}
	for k, v := range src.Identifiers {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Identifiers[k]) == "" {
			dst.Identifiers[k] = v
			changed = true
		}
	}

	dstTags := make(map[string]struct{}, len(dst.Tags))
	for _, t := range dst.Tags {
		dstTags[t] = struct{}{}
	}
	for _, t := range src.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := dstTags[t]; !ok {
			dst.Tags = append(dst.Tags, t)
			dstTags[t] = struct{}{}
			changed = true
		}
	}

	type kbKey struct{ kb, id string }
	existing := make(map[kbKey]struct{}, len(dst.KBRefs))
	for _, r := range dst.KBRefs {
		existing[kbKey{kb: r.KB, id: r.ExternalID}] = struct{}{}
	}
	for _, r := range src.KBRefs {
		k := kbKey{kb: r.KB, id: r.ExternalID}
		if k.kb == "" || k.id == "" {
			continue
		}
		if _, ok := existing[k]; !ok {
			dst.KBRefs = append(dst.KBRefs, r)
			existing[k] = struct{}{}
			changed = true
		}
	}

	if dst.State == nil && src.State != nil {
		dst.State = map[string]horizonmodels.MetricValue{}
		changed = true
	}
	for k, v := range src.State {
		if _, ok := dst.State[k]; !ok {
			dst.State[k] = v
			changed = true
		}
	}

	// Merge OrgSize: fill if target is empty/unknown ("na")
	if strings.TrimSpace(src.OrgSize) != "" {
		cur := strings.TrimSpace(strings.ToLower(dst.OrgSize))
		in := strings.TrimSpace(src.OrgSize)
		if cur == "" || cur == "na" {
			dst.OrgSize = in
			changed = true
		}
	}

	// Merge MarketSegments: union + dedup
	if len(src.MarketSegments) > 0 {
		if dst.MarketSegments == nil {
			dst.MarketSegments = make([]string, 0, len(src.MarketSegments))
		}
		seen := make(map[string]bool)
		for _, s := range dst.MarketSegments {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = true
			}
		}
		for _, s := range src.MarketSegments {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if seen[s] {
				continue
			}
			seen[s] = true
			dst.MarketSegments = append(dst.MarketSegments, s)
			changed = true
		}
	}

	return changed
}

func addLegacyPathMarkers(dst *horizonmodels.HorizonFragment, discoveredPath string) {
	discoveredPath = strings.TrimSpace(discoveredPath)
	if discoveredPath == "" {
		return
	}
	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
	}
	if dst.Identifiers["legacy_discovered_canonical_path"] == "" {
		dst.Identifiers["legacy_discovered_canonical_path"] = discoveredPath
	}
	found := false
	for _, t := range dst.Tags {
		if t == "merged_from_discovered" {
			found = true
			break
		}
	}
	if !found {
		dst.Tags = append(dst.Tags, "merged_from_discovered")
	}
}

func migrateEdges(ctx context.Context, db *firestore.Client, edgesRepo *horizonrepo.DependencyEdgesRepository, discoveredID, canonicalID string) (updated int, deletedDup int, err error) {
	edgesCol := db.Collection("horizon_dependency_edges")

	updated1, deleted1, err := migrateEdgesByQuery(ctx, edgesCol.Where("tenantId", "==", "global").Where("sourceFragmentId", "==", discoveredID), edgesRepo, func(e *horizonmodels.DependencyEdge) {
		e.SourceFragmentID = canonicalID
	})
	if err != nil {
		return 0, 0, err
	}

	updated2, deleted2, err := migrateEdgesByQuery(ctx, edgesCol.Where("tenantId", "==", "global").Where("targetFragmentId", "==", discoveredID), edgesRepo, func(e *horizonmodels.DependencyEdge) {
		e.TargetFragmentID = canonicalID
	})
	if err != nil {
		return 0, 0, err
	}

	return updated1 + updated2, deleted1 + deleted2, nil
}

func migrateEdgesByQuery(ctx context.Context, q firestore.Query, edgesRepo *horizonrepo.DependencyEdgesRepository, mutate func(e *horizonmodels.DependencyEdge)) (updated int, deletedDup int, err error) {
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range docs {
		var edge horizonmodels.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}

		origID := edge.ID
		mutate(&edge)

		if edge.SourceFragmentID == edge.TargetFragmentID {
			if err := edgesRepo.Delete(ctx, origID); err != nil {
				return updated, deletedDup, err
			}
			deletedDup++
			continue
		}

		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, edge.TenantID, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != origID {
			if err := edgesRepo.Delete(ctx, origID); err != nil {
				return updated, deletedDup, err
			}
			deletedDup++
			continue
		}

		if err := edgesRepo.Update(ctx, &edge); err != nil {
			return updated, deletedDup, err
		}
		updated++
	}
	return updated, deletedDup, nil
}

func migrateFrames(ctx context.Context, db *firestore.Client, framesRepo *horizonrepo.FramesRepository, fromCanonicalPath, toCanonicalPath, tenantID string) (int, error) {
	frames, err := framesRepo.ListByTargetCanonicalPath(ctx, tenantID, fromCanonicalPath, 1000)
	if err != nil {
		return 0, err
	}
	if len(frames) == 0 {
		return 0, nil
	}
	col := db.Collection("horizon_frames")
	updated := 0
	for _, f := range frames {
		if f == nil || f.ID == "" {
			continue
		}
		_, err := col.Doc(f.ID).Update(ctx, []firestore.Update{
			{Path: "targetCanonicalPath", Value: toCanonicalPath},
		})
		if err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
