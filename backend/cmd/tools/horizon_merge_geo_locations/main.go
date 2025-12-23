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

// Merges historical geo fragments into strict /loc/{country}/{admin1}/{city}.
// Intended after enabling geo canonicalization rules in runtime.

type geoCanonResult struct {
	CanonicalPath string `json:"canonicalPath"`
}

func main() {
	var (
		projectID     = flag.String("project", getenvDefault("GCP_PROJECT", "sanskrite"), "GCP project id")
		apply         = flag.Bool("apply", false, "Apply changes (default: dry-run only)")
		dryRun        = flag.Bool("dry-run", false, "Force dry-run even if --apply is provided")
		limit         = flag.Int("limit", 0, "Max number of geo fragments to process (0 = no limit)")
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

	log.Printf("=== Horizon Migration: Merge GEO fragments to /loc/{country}/{admin1}/{city} ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d vertexStore=%s vertexLoc=%s ragBucket=%s",
		*projectID, *tenantID, *apply, *limit, *vertexStoreID, *vertexLoc, *ragBucket)

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init GCS client: %v", err)
	}

	vertexSearchClient := ai.NewVertexAISearchClient(*projectID, *vertexLoc, *vertexStoreID, "")
	if !vertexSearchClient.IsConfigured() {
		log.Printf("WARNING: Vertex AI Search client is not fully configured; will rely on LLM canonicalization.")
	}

	execAI := buildAITaskExecutorOrFatal()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db).
		WithStorage(gcsClient, *ragBucket).
		WithVertexSearchSync(func(ctx context.Context, tenantID string) (string, error) {
			if vertexSearchClient == nil || !vertexSearchClient.IsConfigured() {
				return "", fmt.Errorf("vertex search client not configured")
			}
			gcsPath := fmt.Sprintf("gs://%s/%s/horizon_fragments/**", *ragBucket, tenantID)
			return vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
				GCSPath:            gcsPath,
				ReconciliationMode: "INCREMENTAL",
				DataSchema:         "document",
			})
		})
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	framesRepo := horizonrepo.NewFramesRepository(db)

	candidates, err := listGeoCandidates(ctx, db, *tenantID, *limit)
	if err != nil {
		log.Fatalf("FATAL: failed to list geo candidates: %v", err)
	}
	log.Printf("Found %d geo fragments to consider", len(candidates))
	if len(candidates) == 0 {
		if *apply && *fullReconcile && vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
			log.Printf("Nothing to migrate, but apply=true and vertex-full-reconcile=true. Triggering FULL reconcile...")
			gcsPath := fmt.Sprintf("gs://%s/%s/horizon_fragments/**", *ragBucket, *tenantID)
			if op, err := vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
				GCSPath:            gcsPath,
				ReconciliationMode: "FULL",
				DataSchema:         "document",
			}); err == nil {
				log.Printf("Vertex import operation started: %s", op)
			} else {
				log.Printf("WARNING: FULL reconcile failed: %v", err)
			}
		}
		return
	}

	var (
		processed         int
		createdLoc        int
		updatedLoc        int
		deletedOld        int
		updatedEdges      int
		deletedDupEdges   int
		updatedFrames     int
		skippedAlreadyLoc int
	)

	for i, f := range candidates {
		// Skip fragments that are already in correct /loc/{country}/{admin1}/{city} format
		if strings.HasPrefix(f.CanonicalPath, "/loc/") {
			if isLoc3(f.CanonicalPath) {
				log.Printf("---- [%d/%d] SKIP (already correct /loc/): id=%s canonical=%s type=%s ----", i+1, len(candidates), f.ID, f.CanonicalPath, f.Type)
				skippedAlreadyLoc++
				continue
			}
			// Fragment has /loc/ prefix but wrong format - process it
			log.Printf("---- [%d/%d] PROCESSING (wrong /loc/ format): id=%s canonical=%s type=%s ----", i+1, len(candidates), f.ID, f.CanonicalPath, f.Type)
		} else {
			log.Printf("---- [%d/%d] PROCESSING geo fragment: id=%s canonical=%s type=%s ----", i+1, len(candidates), f.ID, f.CanonicalPath, f.Type)
		}

		locPath, err := chooseLocPath(ctx, vertexSearchClient, execAI, f)
		if err != nil {
			log.Printf("ERROR: geo canonicalization failed for %s (%s): %v", f.ID, f.CanonicalPath, err)
			continue
		}
		if !isLoc3(locPath) {
			log.Printf("ERROR: refusing non-/loc path: %s", locPath)
			continue
		}

		locFrag, err := fragmentsRepo.GetByCanonicalPath(ctx, locPath)
		if err != nil {
			newFrag := cloneGeoToLoc(f, locPath)
			if *apply {
				if _, err := fragmentsRepo.Create(ctx, newFrag); err != nil {
					log.Printf("ERROR: failed to create loc fragment %s: %v", locPath, err)
					continue
				}
				createdLoc++
				locFrag, err = fragmentsRepo.GetByCanonicalPath(ctx, locPath)
				if err != nil {
					log.Printf("ERROR: failed to refetch loc fragment %s: %v", locPath, err)
					continue
				}
			} else {
				log.Printf("[dry-run] would CREATE loc fragment: %s", locPath)
				createdLoc++
				continue
			}
		} else {
			changed := mergeGeo(locFrag, f)
			if changed {
				if *apply {
					if err := fragmentsRepo.Update(ctx, locFrag); err != nil {
						log.Printf("ERROR: failed to update loc fragment %s: %v", locFrag.CanonicalPath, err)
						continue
					}
					updatedLoc++
				} else {
					log.Printf("[dry-run] would UPDATE loc fragment: %s", locFrag.CanonicalPath)
					updatedLoc++
					continue
				}
			}
		}

		if *apply {
			u, dd, err := migrateEdges(ctx, db, edgesRepo, f.ID, locFrag.ID)
			if err != nil {
				log.Printf("ERROR: edge migration failed: %v", err)
				continue
			}
			updatedEdges += u
			deletedDupEdges += dd
			n, err := migrateFrames(ctx, db, framesRepo, f.CanonicalPath, locFrag.CanonicalPath, *tenantID)
			if err != nil {
				log.Printf("ERROR: frame migration failed: %v", err)
				continue
			}
			updatedFrames += n
			if err := fragmentsRepo.Delete(ctx, f.ID); err != nil {
				log.Printf("ERROR: failed to delete old geo fragment %s: %v", f.ID, err)
				continue
			}
			deletedOld++

			objPath := fmt.Sprintf("%s/horizon_fragments/%s.json", *tenantID, f.ID)
			if err := gcsClient.DeleteObject(ctx, *ragBucket, objPath); err != nil {
				log.Printf("WARNING: failed to delete GCS object %s/%s: %v (will be purged by FULL reconcile)", *ragBucket, objPath, err)
			}
		} else {
			log.Printf("[dry-run] would migrate edges/frames + DELETE old fragment: %s", f.CanonicalPath)
			deletedOld++
		}

		processed++
	}

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
	log.Printf("processed=%d createdLoc=%d updatedLoc=%d deletedOld=%d updatedEdges=%d deletedDupEdges=%d updatedFrames=%d skippedAlreadyLoc=%d apply=%v",
		processed, createdLoc, updatedLoc, deletedOld, updatedEdges, deletedDupEdges, updatedFrames, skippedAlreadyLoc, *apply)
}

func getenvDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func getSecret(name string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret", name, "--project", getenvDefault("GCP_PROJECT", "sanskrite"))
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
	services = append(services, ai.NewVertexAIService(apiKey, modelManager))
	if openaiKey := firstNonEmpty(getSecret("OPENAI_API_KEY"), getSecret("openai_api_key")); openaiKey != "" {
		services = append(services, ai.NewOpenAIService(openaiKey, modelManager))
	}
	if grokKey := firstNonEmpty(getSecret("GROK_API_KEY"), getSecret("grok_api_key"), getSecret("xai_api_key")); grokKey != "" {
		services = append(services, ai.NewGrokService(grokKey, modelManager))
	}
	return ai.NewAITaskExecutor(services, modelManager)
}

func listGeoCandidates(ctx context.Context, db *firestore.Client, tenantID string, limit int) ([]*horizonmodels.HorizonFragment, error) {
	q := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenantID).
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
		// Candidate set: anything in /region/* or /loc/* (we'll skip /loc in the loop),
		// and anything with type GEO_REGION even if path is /region/*.
		if strings.HasPrefix(f.CanonicalPath, "/region/") || strings.HasPrefix(f.CanonicalPath, "/loc/") {
			out = append(out, &f)
			continue
		}
	}
	return out, nil
}

func isLoc3(cp string) bool {
	cp = strings.TrimSpace(cp)
	if !strings.HasPrefix(cp, "/loc/") {
		return false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	return len(parts) == 4
}

func normalizeLegacyGeoPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "/loc/") {
		return p
	}
	if !strings.HasPrefix(p, "/region/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// parts[0] == "region"
	
	// Pattern: /region/na/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
	if len(parts) == 5 && parts[1] == "na" {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[2], parts[3], parts[4])
	}
	
	// Pattern: /region/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
	if len(parts) == 4 {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[1], parts[2], parts[3])
	}
	
	// Pattern: /region/iso-3166-1-alpha-2/{country} -> /loc/{country}/na/na
	if len(parts) == 3 && parts[1] == "iso-3166-1-alpha-2" {
		country := strings.ToLower(strings.TrimSpace(parts[2]))
		if len(country) == 2 {
			return fmt.Sprintf("/loc/%s/na/na", country)
		}
	}
	
	// Pattern: /region/country/{country} -> /loc/{country}/na/na
	if len(parts) == 3 && parts[1] == "country" {
		country := strings.ToLower(strings.TrimSpace(parts[2]))
		if len(country) == 2 {
			return fmt.Sprintf("/loc/%s/na/na", country)
		}
	}
	
	// Pattern: /region/{country} -> /loc/{country}/na/na (if looks like ISO code: 2 lowercase letters)
	if len(parts) == 2 {
		country := strings.ToLower(strings.TrimSpace(parts[1]))
		// Heuristic: if it's exactly 2 lowercase letters, treat as ISO country code
		if len(country) == 2 && strings.Trim(country, "abcdefghijklmnopqrstuvwxyz") == "" {
			return fmt.Sprintf("/loc/%s/na/na", country)
		}
	}
	
	return ""
}

func chooseLocPath(ctx context.Context, vertex *ai.VertexAISearchClient, execAI *ai.AITaskExecutor, geo *horizonmodels.HorizonFragment) (string, error) {
	// 1) direct normalization from legacy path
	if norm := normalizeLegacyGeoPath(geo.CanonicalPath); norm != "" && isLoc3(norm) {
		return norm, nil
	}

	// 2) Vertex: prefer existing /loc hit
	query := strings.TrimSpace(geo.Labels["en"])
	if query == "" {
		query = strings.TrimSpace(geo.Description)
	}
	if query == "" {
		query = geo.CanonicalPath
	}

	type candidate struct {
		CanonicalPath string  `json:"canonicalPath"`
		Score         float64 `json:"score,omitempty"`
	}
	var candidates []candidate
	if vertex != nil && vertex.IsConfigured() {
		// Use dedicated Horizon Fragments datastore if configured (store isolation; no filters).
		if ds := strings.TrimSpace(getenvDefault("VERTEX_SEARCH_HORIZON_DATA_STORE_ID", "")); ds != "" {
			vertex = vertex.WithDataStore(ds)
		}
		resp, err := vertex.Search(ctx, &ai.SearchRequest{
			Query:    query,
			PageSize: 10,
		})
		if err == nil && resp != nil {
			for _, r := range resp.Results {
				if r.Document.StructData == nil {
					continue
				}
				cp, _ := r.Document.StructData["canonicalPath"].(string)
				cp = strings.TrimSpace(cp)
				if isLoc3(cp) {
					candidates = append(candidates, candidate{CanonicalPath: cp, Score: r.RelevanceScore})
				}
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > 0 {
		return candidates[0].CanonicalPath, nil
	}

	// 3) LLM strict geo canonicalization
	prompt := buildGeoPrompt(geo)
	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         6000,
			MaxOutputSize:            1000,
		},
		Verifier: ai.NewSimpleJSONVerifier(),
	}
	res, err := execAI.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(res)
	var out geoCanonResult
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	out.CanonicalPath = strings.TrimSpace(out.CanonicalPath)
	if !isLoc3(out.CanonicalPath) {
		return "", fmt.Errorf("LLM did not return valid /loc/{country}/{admin1}/{city}: %q", out.CanonicalPath)
	}
	return out.CanonicalPath, nil
}

func buildGeoPrompt(geo *horizonmodels.HorizonFragment) string {
	hint := geo.Labels["en"]
	if strings.TrimSpace(hint) == "" {
		hint = geo.CanonicalPath
	}
	return fmt.Sprintf(`You are a GEO canonicalization agent for Horizon.

INPUT:
%q

TASK:
Return a SINGLE JSON object matching EXACTLY:
{
  "canonicalPath": "/loc/{country}/{admin1}/{city}"
}

RULES:
- canonicalPath MUST start with "/loc/" and MUST have EXACTLY 3 components after /loc:
  /loc/{country}/{admin1}/{city}
- country: ISO3166-1 alpha2 lowercase (examples: us, de, ru, fr, br, cn)
- admin1: state/province/region code or slug lowercase; use "na" if not applicable
- city: lowercase slug

Examples:
- USA / Texas / Austin: /loc/us/tx/austin
- Germany / Bavaria / Munich: /loc/de/by/munich
- France / Île-de-France / Paris: /loc/fr/idf/paris
- Russia / Moscow Oblast / Moscow: /loc/ru/mos/moscow
- Brazil / São Paulo / São Paulo: /loc/br/sp/sao-paulo

Output JSON only, no markdown, no extra text.
`, hint)
}

func cloneGeoToLoc(src *horizonmodels.HorizonFragment, locPath string) *horizonmodels.HorizonFragment {
	dst := *src
	dst.ID = ""
	dst.TenantID = "global"
	dst.CanonicalPath = locPath
	dst.Type = horizonmodels.FragmentTypeGeoRegion
	dst.Governance.Owner = "SYSTEM"
	if dst.Governance.Status == "" {
		dst.Governance.Status = string(horizonmodels.FragmentStatusActive)
	}
	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
	}
	dst.Identifiers["legacy_geo_canonical_path"] = src.CanonicalPath
	dst.Version = 0
	dst.CreatedAt = time.Time{}
	dst.UpdatedAt = time.Time{}
	dst.Metadata = horizonmodels.FragmentMetadata{}
	return &dst
}

func mergeGeo(dst *horizonmodels.HorizonFragment, src *horizonmodels.HorizonFragment) bool {
	changed := false
	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
		changed = true
	}
	if dst.Identifiers["legacy_geo_canonical_path"] == "" {
		dst.Identifiers["legacy_geo_canonical_path"] = src.CanonicalPath
		changed = true
	}
	// Labels: fill missing only
	if src.Labels != nil {
		if dst.Labels == nil {
			dst.Labels = make(map[string]string)
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
	}
	// Description
	if strings.TrimSpace(dst.Description) == "" && strings.TrimSpace(src.Description) != "" {
		dst.Description = src.Description
		changed = true
	}
	// HomeLocation
	if dst.HomeLocation == nil && src.HomeLocation != nil {
		dst.HomeLocation = src.HomeLocation
		changed = true
	}
	return changed
}

func migrateEdges(ctx context.Context, db *firestore.Client, edgesRepo *horizonrepo.DependencyEdgesRepository, oldID, newID string) (updated int, deletedDup int, err error) {
	edgesCol := db.Collection("horizon_dependency_edges")
	updated1, deleted1, err := migrateEdgesByQuery(ctx, edgesCol.Where("tenantId", "==", "global").Where("sourceFragmentId", "==", oldID), edgesRepo, func(e *horizonmodels.DependencyEdge) {
		e.SourceFragmentID = newID
	})
	if err != nil {
		return 0, 0, err
	}
	updated2, deleted2, err := migrateEdgesByQuery(ctx, edgesCol.Where("tenantId", "==", "global").Where("targetFragmentId", "==", oldID), edgesRepo, func(e *horizonmodels.DependencyEdge) {
		e.TargetFragmentID = newID
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


