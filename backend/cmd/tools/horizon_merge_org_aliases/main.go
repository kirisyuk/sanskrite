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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/merge"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	ah "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Merge organization alias fragments (e.g., /org/ford-motor-company -> /org/ford) using shortName,
// with LLM confirmation to avoid unsafe merges.

type orgCanonResult struct {
	LegalName     string `json:"legalName"`
	ShortName     string `json:"shortName"`
	CanonicalPath string `json:"canonicalPath"`
}

type sameEntityResult struct {
	Same       bool    `json:"sameEntity"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func slugify(s string) string {
	return taxonomy.Slugify(s)
}

func main() {
	var (
		projectID     = flag.String("project", getenvDefault("GCP_PROJECT", "prevalent-horizon-test"), "GCP project id")
		apply         = flag.Bool("apply", false, "Apply changes (default: dry-run only)")
		dryRun        = flag.Bool("dry-run", false, "Force dry-run even if --apply is provided")
		limit         = flag.Int("limit", 0, "Max number of org fragments to process (0 = no limit)")
		tenantID      = flag.String("tenant", "global", "TenantID for Horizon fragments (normally 'global')")
		vertexStoreID = flag.String("vertex-store", "", "Vertex AI Search data store ID (default from env or prod)")
		vertexLoc     = flag.String("vertex-location", "", "Vertex AI Search location (default from env or prod)")
		bucket        = flag.String("bucket", "", "GCS bucket used for Vertex indexing (defaults to RAG_DOCUMENTS_BUCKET, CORE_DOCS_BUCKET, or {project}-core-docs)")
		fullReconcile = flag.Bool("vertex-full-reconcile", true, "Run FULL Vertex AI Search import for horizon_fragments at the end (apply mode only)")
		minConfidence         = flag.Float64("min-confidence", 0.85, "Minimum confidence to merge when preferred fragment exists")
		only                  = flag.String("only", "", "Comma-separated substrings to filter source canonicalPaths to process (e.g., 'ford-motor-company,byd-company,rivian-automotive'). Empty = process all candidates")
		alsoAbbreviationMerge = flag.Bool("also-abbreviation-merge", false, "Also run abbreviation->full merge (heuristic: first-letter acronym match in same geo)")
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
	if *bucket == "" {
		*bucket = os.Getenv("RAG_DOCUMENTS_BUCKET")
	}
	if *bucket == "" {
		*bucket = os.Getenv("CORE_DOCS_BUCKET")
	}
	if *bucket == "" {
		*bucket = *projectID + "-core-docs"
	}

	log.Printf("=== Horizon Migration: Merge ORG aliases ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d bucket=%s vertexStore=%s vertexLoc=%s minConfidence=%.2f",
		*projectID, *tenantID, *apply, *limit, *bucket, *vertexStoreID, *vertexLoc, *minConfidence)

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
		WithStorage(gcsClient, *bucket).
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

	orgs, err := listOrgFragments(ctx, db, *tenantID, *limit)
	if err != nil {
		log.Fatalf("FATAL: failed to list org fragments: %v", err)
	}
	log.Printf("Found %d /org/* fragments to consider", len(orgs))
	if len(orgs) == 0 {
		return
	}

	// Build a quick lookup by canonicalPath -> fragment
	byPath := make(map[string]*horizonmodels.HorizonFragment, len(orgs))
	for _, f := range orgs {
		byPath[f.CanonicalPath] = f
	}

	var (
		processed        int
		merged           int
		createdPreferred int
		updatedPreferred int
		deletedOld       int
		updatedEdges     int
		deletedDupEdges  int
		updatedFrames    int
		skippedNoShort   int
	)

	for _, f := range orgs {
		// Optional filter
		if strings.TrimSpace(*only) != "" {
			ok := false
			for _, tok := range strings.Split(*only, ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if strings.Contains(f.CanonicalPath, tok) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}

		// Determine shortName
		// Always ask LLM for normalization to avoid treating legal names ("Ford Motor Company") as shortName.
		out, err := llmOrgNormalize(ctx, execAI, f)
		if err != nil {
			log.Printf("WARN: cannot normalize org for %s (%s): %v", f.ID, f.CanonicalPath, err)
			skippedNoShort++
			continue
		}
		shortName := strings.TrimSpace(out.ShortName)
		legalName := strings.TrimSpace(out.LegalName)
		if legalName == "" {
			legalName = strings.TrimSpace(f.Identifiers["legalName"])
		}
		if legalName == "" {
			legalName = strings.TrimSpace(f.Labels["en"])
		}

		preferredPath := strings.TrimSpace(out.CanonicalPath)
		if preferredPath == "" {
			preferredPath = "/org/" + slugify(shortName)
		}
		if preferredPath == "/org/" || preferredPath == "/org/-" {
			skippedNoShort++
			continue
		}

		if f.CanonicalPath == preferredPath {
			continue
		}

		processed++
		log.Printf("ORG alias candidate: %s -> preferred=%s (shortName=%q legalName=%q)", f.CanonicalPath, preferredPath, shortName, legalName)

		preferred, ok := byPath[preferredPath]
		if ok && preferred != nil && preferred.ID != "" {
			// Confirm with LLM that these represent same real-world entity
			same, err := llmSameEntity(ctx, execAI, preferred, f)
			if err != nil {
				log.Printf("WARN: same-entity check failed for %s vs %s: %v", preferredPath, f.CanonicalPath, err)
				continue
			}
			if !same.Same || same.Confidence < *minConfidence {
				log.Printf("SKIP: not confident to merge %s -> %s (same=%v conf=%.2f reason=%s)", f.CanonicalPath, preferredPath, same.Same, same.Confidence, same.Reason)
				continue
			}

			// Merge data into preferred (fill missing only)
			changed := mergeOrg(preferred, f, legalName, shortName)
			if *apply && changed {
				if err := fragmentsRepo.Update(ctx, preferred); err != nil {
					log.Printf("ERROR: failed to update preferred fragment %s: %v", preferred.CanonicalPath, err)
					continue
				}
				updatedPreferred++
			}
			if !*apply && changed {
				log.Printf("[dry-run] would UPDATE preferred fragment %s", preferred.CanonicalPath)
				updatedPreferred++
				continue
			}

			if *apply {
				u, dd, err := migrateEdges(ctx, db, edgesRepo, f.ID, preferred.ID)
				if err != nil {
					log.Printf("ERROR: migrate edges failed: %v", err)
					continue
				}
				updatedEdges += u
				deletedDupEdges += dd
				n, err := migrateFrames(ctx, db, framesRepo, f.CanonicalPath, preferred.CanonicalPath, *tenantID)
				if err != nil {
					log.Printf("ERROR: migrate frames failed: %v", err)
					continue
				}
				updatedFrames += n

				if err := fragmentsRepo.Delete(ctx, f.ID); err != nil {
					log.Printf("ERROR: delete old fragment failed: %v", err)
					continue
				}
				deletedOld++
				objPath := fmt.Sprintf("%s/horizon_fragments/%s.json", *tenantID, f.ID)
				_ = gcsClient.DeleteObject(ctx, *bucket, objPath) // ok if missing; FULL reconcile will purge
			} else {
				log.Printf("[dry-run] would migrate edges/frames + DELETE %s", f.CanonicalPath)
				deletedOld++
			}

			merged++
			continue
		}

		// Preferred doesn't exist. Create it by cloning f.
		newFrag := cloneOrgToPreferred(f, preferredPath, legalName, shortName)
		if *apply {
			if _, err := fragmentsRepo.Create(ctx, newFrag); err != nil {
				log.Printf("ERROR: failed to create preferred fragment %s: %v", preferredPath, err)
				continue
			}
			createdPreferred++
			pref2, err := fragmentsRepo.GetByCanonicalPath(ctx, preferredPath)
			if err != nil {
				log.Printf("ERROR: failed to refetch preferred %s: %v", preferredPath, err)
				continue
			}
			byPath[preferredPath] = pref2

			u, dd, err := migrateEdges(ctx, db, edgesRepo, f.ID, pref2.ID)
			if err != nil {
				log.Printf("ERROR: migrate edges failed: %v", err)
				continue
			}
			updatedEdges += u
			deletedDupEdges += dd
			n, err := migrateFrames(ctx, db, framesRepo, f.CanonicalPath, pref2.CanonicalPath, *tenantID)
			if err != nil {
				log.Printf("ERROR: migrate frames failed: %v", err)
				continue
			}
			updatedFrames += n

			if err := fragmentsRepo.Delete(ctx, f.ID); err != nil {
				log.Printf("ERROR: delete old fragment failed: %v", err)
				continue
			}
			deletedOld++
			objPath := fmt.Sprintf("%s/horizon_fragments/%s.json", *tenantID, f.ID)
			_ = gcsClient.DeleteObject(ctx, *bucket, objPath)
		} else {
			log.Printf("[dry-run] would CREATE preferred fragment %s and migrate edges/frames + delete %s", preferredPath, f.CanonicalPath)
			createdPreferred++
		}
	}

	if *apply && *fullReconcile && vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
		log.Printf("Triggering Vertex AI Search FULL reconcile for horizon_fragments...")
		gcsPath := fmt.Sprintf("gs://%s/%s/horizon_fragments/**", *bucket, *tenantID)
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

	if *alsoAbbreviationMerge {
		log.Printf("=== Running abbreviation->full merge ===")
		abbrevCfg := merge.RunConfig{
			ProjectID: *projectID,
			TenantID:  *tenantID,
			Apply:     *apply,
			Limit:     *limit,
			Prefixes:  []string{"/gov/", "/org/"},
		}
		abbrevResult, err := merge.RunAbbreviationMerge(ctx, db, fragmentsRepo, edgesRepo, framesRepo, abbrevCfg)
		if err != nil {
			log.Printf("WARNING: abbreviation merge failed: %v", err)
		} else {
			log.Printf("Abbreviation merge: processed=%d merged=%d updatedFull=%d deletedAbbrev=%d updatedEdges=%d",
				abbrevResult.Processed, abbrevResult.Merged, abbrevResult.UpdatedFull, abbrevResult.DeletedAbbrev, abbrevResult.UpdatedEdges)
		}
	}

	log.Printf("=== Migration Summary ===")
	log.Printf("processed=%d merged=%d createdPreferred=%d updatedPreferred=%d deletedOld=%d updatedEdges=%d deletedDupEdges=%d updatedFrames=%d skippedNoShort=%d apply=%v",
		processed, merged, createdPreferred, updatedPreferred, deletedOld, updatedEdges, deletedDupEdges, updatedFrames, skippedNoShort, *apply)
}

func getenvDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func getSecret(name string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret", name, "--project", getenvDefault("GCP_PROJECT", "prevalent-horizon-test"))
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
	services = append(services, ai.NewGeminiService(apiKey, modelManager))
	if openaiKey := firstNonEmpty(getSecret("OPENAI_API_KEY"), getSecret("openai_api_key")); openaiKey != "" {
		services = append(services, ai.NewOpenAIService(openaiKey, modelManager))
	}
	if grokKey := firstNonEmpty(getSecret("GROK_API_KEY"), getSecret("grok_api_key"), getSecret("xai_api_key")); grokKey != "" {
		services = append(services, ai.NewGrokService(grokKey, modelManager))
	}
	return ai.NewAITaskExecutor(services, modelManager)
}

func listOrgFragments(ctx context.Context, db *firestore.Client, tenantID string, limit int) ([]*horizonmodels.HorizonFragment, error) {
	// Prefix query for /org/*
	q := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenantID).
		Where("canonicalPath", ">=", "/org/").
		Where("canonicalPath", "<=", "/org/\uf8ff").
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
		out = append(out, &f)
	}

	// stable order: shorter paths first (prefer /org/ford over /org/ford-motor-company)
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].CanonicalPath) == len(out[j].CanonicalPath) {
			return out[i].CanonicalPath < out[j].CanonicalPath
		}
		return len(out[i].CanonicalPath) < len(out[j].CanonicalPath)
	})

	return out, nil
}

func llmOrgNormalize(ctx context.Context, execAI *ai.AITaskExecutor, f *horizonmodels.HorizonFragment) (*orgCanonResult, error) {
	pathCandidates := buildOrgPathCandidates(f.CanonicalPath)
	in := map[string]interface{}{
		"canonicalPath":  f.CanonicalPath,
		"pathCandidates": pathCandidates,
		"labels":         f.Labels,
		"identifiers":    f.Identifiers,
		"description":    f.Description,
	}
	inJSON, _ := json.MarshalIndent(in, "", "  ")
	prompt := fmt.Sprintf(`You are an organization name normalizer for Horizon.

INPUT (JSON):
%s

TASK:
Return ONE JSON object matching EXACTLY this schema:
{
  "legalName": "string",
  "shortName": "string",
  "canonicalPath": "string"
}

RULES:
- legalName MUST include the form of ownership when relevant (Inc., Ltd., LLC, GmbH, AG, S.A., PLC...).
- shortName MUST be the common short name / brand name used in practice (e.g., "Ford", "BYD", "Rivian") and MUST NOT include legal form (Inc., Ltd...) or generic suffixes like "Motor Company" unless it is essential for disambiguation.
- canonicalPath MUST NOT start with "/discovered/".
- If input.pathCandidates contains a shorter canonicalPath that refers to the same entity (e.g., /org/ford), you SHOULD prefer it over longer variants (e.g., /org/ford-motor-company).
- If none of the candidates are suitable, set canonicalPath to "/org/{slug(shortName)}".
- Output JSON only. No markdown. No extra text.
`, string(inJSON))

	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         6000,
			MaxOutputSize:            800,
		},
		Verifier: ai.NewSimpleJSONVerifier(),
	}

	res, err := execAI.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var out orgCanonResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	out.ShortName = strings.TrimSpace(out.ShortName)
	out.LegalName = strings.TrimSpace(out.LegalName)
	out.CanonicalPath = strings.TrimSpace(out.CanonicalPath)
	if out.ShortName == "" {
		return nil, fmt.Errorf("shortName is empty")
	}
	if !strings.HasPrefix(out.CanonicalPath, "/org/") {
		return nil, fmt.Errorf("canonicalPath must start with /org/, got %q", out.CanonicalPath)
	}
	return &out, nil
}

func buildOrgPathCandidates(canonicalPath string) []string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/org/") {
		return []string{}
	}
	slug := strings.TrimPrefix(cp, "/org/")
	slug = strings.Trim(slug, "/")
	if slug == "" {
		return []string{cp}
	}
	parts := strings.Split(slug, "-")
	// include current
	cands := []string{"/org/" + slug}
	// progressively shorter prefixes: /org/a, /org/a-b, ...
	for i := 1; i < len(parts); i++ {
		p := strings.Join(parts[:i], "-")
		if p == "" {
			continue
		}
		cands = append(cands, "/org/"+p)
	}
	// de-dup
	seen := map[string]struct{}{}
	var out []string
	for _, c := range cands {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func llmSameEntity(ctx context.Context, execAI *ai.AITaskExecutor, aFrag, bFrag *horizonmodels.HorizonFragment) (*sameEntityResult, error) {
	in := map[string]interface{}{
		"a": map[string]interface{}{
			"canonicalPath": aFrag.CanonicalPath,
			"labels":        aFrag.Labels,
			"identifiers":   aFrag.Identifiers,
			"kbRefs":        aFrag.KBRefs,
			"description":   aFrag.Description,
		},
		"b": map[string]interface{}{
			"canonicalPath": bFrag.CanonicalPath,
			"labels":        bFrag.Labels,
			"identifiers":   bFrag.Identifiers,
			"kbRefs":        bFrag.KBRefs,
			"description":   bFrag.Description,
		},
	}
	inJSON, _ := json.MarshalIndent(in, "", "  ")
	prompt := fmt.Sprintf(`You are a Horizon entity deduplication judge.

INPUT (JSON):
%s

TASK:
Return ONE JSON object matching EXACTLY this schema:
{
  "sameEntity": true|false,
  "confidence": 0.0,
  "reason": "string"
}

RULES:
- Answer true ONLY if a and b refer to the SAME real-world organization.
- Prefer identifiers (ticker, ISIN, LEI), KB refs, and strong name evidence.
- Output JSON only. No markdown. No extra text.
`, string(inJSON))

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContextDeduplication,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       9,
			MinContextWindow:         8000,
			MaxOutputSize:            800,
		},
		Verifier: ai.NewSimpleJSONVerifier(),
	}
	res, err := execAI.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var out sameEntityResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func cloneOrgToPreferred(src *horizonmodels.HorizonFragment, preferredPath, legalName, shortName string) *horizonmodels.HorizonFragment {
	dst := *src
	dst.ID = ""
	dst.TenantID = "global"
	dst.CanonicalPath = preferredPath
	dst.Class = ah.FragmentClassOrg
	dst.Governance.Owner = "SYSTEM"
	if dst.Governance.Status == "" {
		dst.Governance.Status = string(horizonmodels.FragmentStatusActive)
	}
	if dst.Labels == nil {
		dst.Labels = make(map[string]string)
	}
	if strings.TrimSpace(dst.Labels["en"]) == "" && strings.TrimSpace(legalName) != "" {
		dst.Labels["en"] = legalName
	}
	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
	}
	if dst.Identifiers["legalName"] == "" && legalName != "" {
		dst.Identifiers["legalName"] = legalName
	}
	if dst.Identifiers["shortName"] == "" && shortName != "" {
		dst.Identifiers["shortName"] = shortName
	}
	dst.Identifiers["legacy_org_canonical_path"] = src.CanonicalPath
	dst.Version = 0
	dst.CreatedAt = time.Time{}
	dst.UpdatedAt = time.Time{}
	dst.Metadata = horizonmodels.FragmentMetadata{}
	return &dst
}

func mergeOrg(dst *horizonmodels.HorizonFragment, src *horizonmodels.HorizonFragment, legalName, shortName string) bool {
	changed := false
	if dst.Labels == nil {
		dst.Labels = make(map[string]string)
		changed = true
	}
	if dst.Identifiers == nil {
		dst.Identifiers = make(map[string]string)
		changed = true
	}

	if strings.TrimSpace(dst.Identifiers["legalName"]) == "" && strings.TrimSpace(legalName) != "" {
		dst.Identifiers["legalName"] = strings.TrimSpace(legalName)
		changed = true
	}
	if strings.TrimSpace(dst.Identifiers["shortName"]) == "" && strings.TrimSpace(shortName) != "" {
		dst.Identifiers["shortName"] = strings.TrimSpace(shortName)
		changed = true
	}
	if strings.TrimSpace(dst.Labels["en"]) == "" && strings.TrimSpace(legalName) != "" {
		dst.Labels["en"] = strings.TrimSpace(legalName)
		changed = true
	}

	if dst.Identifiers["legacy_org_canonical_path"] == "" {
		dst.Identifiers["legacy_org_canonical_path"] = src.CanonicalPath
		changed = true
	}

	// Merge identifiers fill-only
	for k, v := range src.Identifiers {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Identifiers[k]) == "" {
			dst.Identifiers[k] = v
			changed = true
		}
	}

	// Labels fill-only
	for k, v := range src.Labels {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Labels[k]) == "" {
			dst.Labels[k] = v
			changed = true
		}
	}

	// Description
	if strings.TrimSpace(dst.Description) == "" && strings.TrimSpace(src.Description) != "" {
		dst.Description = src.Description
		changed = true
	}

	// Tags union
	tagSet := map[string]struct{}{}
	for _, t := range dst.Tags {
		tagSet[t] = struct{}{}
	}
	for _, t := range src.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := tagSet[t]; !ok {
			dst.Tags = append(dst.Tags, t)
			tagSet[t] = struct{}{}
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
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, edge.TenantID, edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation)
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
