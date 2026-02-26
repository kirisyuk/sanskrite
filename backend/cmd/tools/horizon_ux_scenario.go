package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func getSecret(secretName string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret="+secretName, "--project=h1horizon-488615")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("Warning: Failed to fetch secret %s: %v", secretName, err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// normalizeCanonicalPath fixes MSYS/Git-Bash argument conversion ("/foo/bar" -> "C:/Program Files/Git/foo/bar").
// It also allows passing paths without a leading slash ("product/tesla-batteries").
func normalizeCanonicalPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	// If MSYS converted "/product/..." into "C:/.../product/...", strip everything before the first canonical segment.
	candidates := []string{"/org/", "/person/", "/product/", "/fac/", "/loc/", "/region/", "/event/"}
	for _, c := range candidates {
		if idx := strings.Index(s, c); idx > 0 {
			s = s[idx:]
			break
		}
	}
	// If user passed without leading slash, add it.
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

func main() {
	log.Println("=== Starting Horizon UX Test Scenario ===")
	ctx := context.Background()
	projectID := "h1horizon-488615"

	// Scenario flags (so we can test different branches without editing code)
	targetFlag := flag.String("target", "/org/tesla", "Target canonical path for the scenario (e.g., /product/tesla-batteries)")
	promptFlag := flag.String("prompt", "Analyze risks for Tesla Inc. with focus on supply chain", "User prompt for the scenario")
	focusFlag := flag.String("focus", "supply_chain", "Lens focus (informational)")
	depthFlag := flag.Int("depth", 2, "Lens depth (informational)")
	agentIDFlag := flag.String("agent-id", "", "Optional agentId for the local worker (if empty, a unique one will be generated)")
	plannerDisableVertexFlag := flag.Bool("planner-disable-vertex-search", false, "Disable Vertex AI Search usage in ScoutPlanner source filtering (use fallback filtering). Useful to ensure sources are used in tests.")
	seedWikiFlag := flag.Bool("seed-wikipedia", false, "Seed a real web DataSource (Wikipedia) for the target, to ensure Scout has content and can extract state metrics.")
	forceScoutFlag := flag.Bool("force-scout", false, "Force a fresh scout run by deleting the existing fragment (avoid freshness skip).")
	flag.Parse()

	// Чтобы тест был детерминированным и не конкурировал с боевым Scout Worker,
	// используем отдельный agentId. Боевой воркер поллит agentId="scout".
	// IMPORTANT: agentId must be unique per run, otherwise старые pending jobs могут “перехватить” воркер и увести сценарий в сторону.
	workerAgentID := strings.TrimSpace(*agentIDFlag)
	if workerAgentID == "" {
		workerAgentID = fmt.Sprintf("scout_local_%d", time.Now().UnixNano())
		log.Printf("INFO: --agent-id not provided; generated unique agentId=%q", workerAgentID)
	} else {
		log.Printf("INFO: Using provided agentId=%q", workerAgentID)
	}

	// Vertex AI Search config (OAuth via ADC; no API key required)
	vertexStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
	if vertexStoreID == "" {
		// Match production default (see scripts/deploy and backend/cmd/server/main.go expectations)
		vertexStoreID = "rag-documents-store"
		log.Printf("INFO: VERTEX_SEARCH_DATA_STORE_ID is not set locally; defaulting to %q", vertexStoreID)
	}
	vertexLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
	if vertexLocation == "" {
		vertexLocation = "global"
		log.Printf("INFO: VERTEX_SEARCH_LOCATION is not set locally; defaulting to %q", vertexLocation)
	}

	// Bucket for Vertex indexing (same pattern as server/main.go)
	ragDocsBucket := os.Getenv("RAG_DOCUMENTS_BUCKET")
	if ragDocsBucket == "" {
		ragDocsBucket = os.Getenv("CORE_DOCS_BUCKET")
	}
	if ragDocsBucket == "" {
		ragDocsBucket = projectID + "-core-docs"
	}

	// 1. Secrets & Config
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

	openaiKey := getSecret("OPENAI_API_KEY")
	if openaiKey == "" {
		openaiKey = getSecret("openai_api_key")
	}

	grokKey := getSecret("GROK_API_KEY")
	if grokKey == "" {
		grokKey = getSecret("grok_api_key")
	}
	if grokKey == "" {
		grokKey = getSecret("xai_api_key")
	}

	log.Println("Secrets loaded.")

	// 2. Init Firestore
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Firestore init failed: %v", err)
	}
	defer fsClient.Close()
	log.Println("Firestore connected.")

	// 2b. Init GCS + Vertex Search client for indexing/search over saved data
	gcsClient, err := gcp.NewGCSClient(ctx, projectID)
	if err != nil {
		log.Fatalf("GCS init failed: %v", err)
	}
	vertexSearchClient := ai.NewVertexAISearchClient(projectID, vertexLocation, vertexStoreID, "")
	if vertexStoreID == "" || !vertexSearchClient.IsConfigured() {
		log.Printf("WARNING: Vertex AI Search is not configured (VERTEX_SEARCH_DATA_STORE_ID missing or ADC unavailable). Search/indexing checks will be limited.")
	}

	// 3. Init Repos
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient)
	// Enable export -> GCS -> Vertex indexing for fragments (like prod)
	if gcsClient != nil && ragDocsBucket != "" {
		fragmentsRepo.WithStorage(gcsClient, ragDocsBucket)
		if vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
			fragmentsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
				if strings.TrimSpace(gcsURI) == "" {
					return "", nil
				}
				return vertexSearchClient.ImportDocumentsFromURIs(ctx, []string{gcsURI}, "INCREMENTAL", "document")
			})
			log.Printf("Horizon Fragments: Vertex AI Search sync enabled (bucket=%s store=%s)", ragDocsBucket, vertexStoreID)
		}
	}
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient)
	lensesRepo := horizonrepo.NewLensesRepository(fsClient)
	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient)
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	framesRepo := horizonrepo.NewFramesRepository(fsClient)
	// docsRepo := corerepo.NewCoreDocsRepository(fsClient) // Not needed for SourceExecutor
	documentsRepo := corerepo.NewDocumentsRepository(fsClient) // For crawled content
	dataSourcesRepo := corerepo.NewDataSourcesRepository(fsClient)

	// 4. Aggressive cleanup of ALL datasources to ensure pure Research Agent scenario
	/* DISABLED FOR FRESHNESS TEST
	docs, _ := fsClient.Collection("core_datasources").Documents(ctx).GetAll()
	log.Printf("Cleaning up %d existing datasources...", len(docs))
	for _, d := range docs {
		_, _ = d.Ref.Delete(ctx)
	}
	log.Println("Cleaned up ALL datasources.")
	*/

	// 4. Init AI & Agents
	modelManager := ai.NewAIModelManager()

	var services []ai.AIService

	vertexService := ai.NewGeminiService(apiKey, modelManager)
	services = append(services, vertexService)

	if openaiKey != "" {
		openaiService := ai.NewOpenAIService(openaiKey, modelManager)
		services = append(services, openaiService)
		log.Println("OpenAI Service initialized")
	} else {
		log.Println("Warning: OpenAI key not found, skipping OpenAI service")
	}

	if grokKey != "" {
		grokService := ai.NewGrokService(grokKey, modelManager)
		services = append(services, grokService)
		log.Println("Grok Service initialized")
	} else {
		log.Println("Warning: Grok key not found, skipping Grok service")
	}

	aiExecutor := ai.NewAITaskExecutor(services, modelManager)

	scoutAgent := horizonagents.NewScoutAgent(aiExecutor)
	criticAgent := horizonagents.NewCriticAgent(aiExecutor)
	resolverAgent := horizonagents.NewResolverAgent(aiExecutor)
	researchAgent := horizonagents.NewResearchAgent(aiExecutor)

	// 5. Init Engine Components
	// Use Vertex AI Search for planning/canonicalization where possible
	plannerVertex := vertexSearchClient
	if *plannerDisableVertexFlag {
		plannerVertex = nil
		log.Printf("INFO: ScoutPlanner Vertex AI Search disabled for this run (fallback filtering will be used).")
	}
	// factsRepo is not used in this UX tool scenario; keep nil.
	planner := horizonscout.NewScoutPlanner(dataSourcesRepo, nil, plannerVertex)

	wc := crawler.NewWebCrawler()
	rssProvider := prov.NewRSSProvider()
	sourceExecutor := horizonscout.NewSourceExecutor(wc, rssProvider, nil, documentsRepo)

	frameBuilder := horizonscout.NewFrameBuilder()
	workflow := horizonscout.NewScoutWorkflow(agentJobsRepo)

	scoutEngine := horizonscout.NewScoutEngine(
		planner,
		sourceExecutor,
		scoutAgent,
		criticAgent,
		resolverAgent,
		researchAgent,
		frameBuilder,
		workflow,
		fragmentsRepo,
		framesRepo,
		edgesRepo,
		scoutingJobsRepo,
		nil, // realtime disabled for script
		aiExecutor,
	)

	// Wire Propagation Trigger (Simplified Logging)
	scoutEngine.WithPropagationTrigger(func(ctx context.Context, fragmentID string) error {
		log.Printf("[PROPAGATION] Triggered for fragment: %s", fragmentID)
		return nil
	})

	// 6. Init Lens Service
	lifecycle := horizonlens.NewLifecycleManager(fragmentsRepo)
	calculator := horizonlens.NewMetricsCalculator(fragmentsRepo)
	lensService := horizonlens.NewLensService(lensesRepo, fragmentsRepo, scoutingJobsRepo, nil, lifecycle, calculator, nil)

	// --- SCENARIO EXECUTION ---

	// Clean up previous run data to ensure fresh start
	rawTarget := strings.TrimSpace(*targetFlag)
	if rawTarget == "" {
		rawTarget = "/org/tesla"
	}
	targetPath := normalizeCanonicalPath(rawTarget)

	// If rawTarget differs (e.g., MSYS conversion), clean up the accidentally created bad fragment/edges.
	if rawTarget != "" && rawTarget != targetPath {
		log.Printf("WARNING: target path was normalized: raw=%q -> canonical=%q", rawTarget, targetPath)
		if badFrag, err := fragmentsRepo.GetByCanonicalPath(ctx, rawTarget); err == nil && badFrag != nil {
			log.Printf("WARNING: Found accidental fragment with raw canonicalPath=%q (id=%s). Cleaning it up...", rawTarget, badFrag.ID)
			// delete outgoing edges
			if edges, err := edgesRepo.ListBySource(ctx, "global", badFrag.ID, nil, 1000); err == nil {
				for _, e := range edges {
					_ = edgesRepo.Delete(ctx, e.ID)
				}
			}
			// delete incoming edges
			if edges, err := edgesRepo.ListByTarget(ctx, "global", badFrag.ID, nil, 1000); err == nil {
				for _, e := range edges {
					_ = edgesRepo.Delete(ctx, e.ID)
				}
			}
			_ = fragmentsRepo.Delete(ctx, badFrag.ID)
		}
	}

	// Optional: Seed a real Wikipedia web source for this target, and link it to the project.
	// This ensures aggregatedContent != "" so ScoutAgent can actually extract fragment.state metrics.
	var seededSourceID string
	if *seedWikiFlag {
		var uri string
		switch {
		case strings.Contains(targetPath, "/person/elon-musk"):
			uri = "https://en.wikipedia.org/wiki/Elon_Musk"
		case strings.Contains(targetPath, "/org/tesla"):
			uri = "https://en.wikipedia.org/wiki/Tesla,_Inc."
		default:
			parts := strings.Split(strings.Trim(targetPath, "/"), "/")
			slug := parts[len(parts)-1]
			uri = "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(slug, "-", "_")
		}
		now := time.Now().UTC()
		ds := &agentmodels.DataSource{
			DisplayName: "seed:wikipedia:" + targetPath,
			Kind:        "web",
			Status:      "active",
			OwnerUserID: "system",
			Details: agentmodels.SourceDetails{
				URI: &uri,
			},
			ErrorCount: 0,
			TrustLevel: 0.9,
			FirstSeen:  now,
			LastSeen:   now,
		}
		id, err := dataSourcesRepo.Create(ctx, ds)
		if err != nil {
			log.Fatalf("FATAL: failed to seed wikipedia datasource: %v", err)
		}
		seededSourceID = id
		log.Printf("Seeded Wikipedia DataSource id=%s uri=%s", seededSourceID, uri)

		// Link to project so it is included in ListByProject immediately (strong signal for planner fallback).
		spRepo := corerepo.NewSourceProjectsRepository(fsClient)
		_ = spRepo.Upsert(ctx, &agentmodels.SourceProject{
			ID:         "seed:" + seededSourceID + ":ux-test-project",
			TenantID:   "global",
			ProjectID:  "ux-test-project",
			SourceID:   seededSourceID,
			Status:     "active",
			Weights:    map[string]float64{"seed": 1.0},
			SyncConfig: map[string]any{},
		})

		defer func() {
			// Best-effort cleanup to avoid polluting global datasources with test seeds.
			_ = spRepo.DeleteByProjectAndSource(ctx, "global", "ux-test-project", seededSourceID)
			_ = dataSourcesRepo.Delete(ctx, seededSourceID)
			log.Printf("Cleaned up seeded DataSource id=%s", seededSourceID)
		}()
	}

	// Optional: delete existing fragment to ensure we don't skip due to freshness.
	if *forceScoutFlag {
		log.Printf("FORCE: deleting existing fragment for %s (to avoid freshness skip)", targetPath)
		if oldFrag, err := fragmentsRepo.GetByCanonicalPath(ctx, targetPath); err == nil && oldFrag != nil && oldFrag.ID != "" {
			// Best-effort: delete edges to avoid dangling graph noise
			if edges, err := edgesRepo.ListBySource(ctx, "global", oldFrag.ID, nil, 1000); err == nil {
				for _, e := range edges {
					_ = edgesRepo.Delete(ctx, e.ID)
				}
			}
			if edges, err := edgesRepo.ListByTarget(ctx, "global", oldFrag.ID, nil, 1000); err == nil {
				for _, e := range edges {
					_ = edgesRepo.Delete(ctx, e.ID)
				}
			}
			if err := fragmentsRepo.Delete(ctx, oldFrag.ID); err != nil {
				log.Printf("Warning: failed to delete old fragment: %v", err)
			} else {
				log.Printf("Deleted existing fragment %s to force fresh scouting", oldFrag.ID)
			}
		}
	} else {
		log.Printf("Cleaning up any existing data for %s... (DISABLED FOR FRESHNESS TEST)", targetPath)
	}

	// Phase 1: The Hook (Simulated)
	userPrompt := strings.TrimSpace(*promptFlag)
	if userPrompt == "" {
		userPrompt = "Analyze risks for Tesla Inc. with focus on supply chain"
	}
	log.Printf("\n--- Phase 1: The Hook ---\nUser Prompt: %q", userPrompt)

	// Simulate "Live Entity Recognition" -> Canonical Path
	log.Printf("Recognized Entity: %s", targetPath)

	// Phase 2: Create Lens & The Sled
	log.Printf("\n--- Phase 2: The Sled (Assembly) ---")
	lens := &models.Lens{
		// HorizonFragment по архитектуре глобальные; для теста используем единый tenant,
		// чтобы не ломать кэш-хиты/поиск ребер/инкременты подписчиков.
		TenantID:  "global",
		ProjectID: "ux-test-project",
		Name:      "Tesla Supply Chain Risks (UX Test)",
		Scope:     []string{targetPath},
		ViewConfig: map[string]interface{}{
			"depth":       *depthFlag,
			"focus":       *focusFlag,
			"description": "Auto-generated lens for testing decomposition",
			"ownerId":     "tester",
			"configuration": map[string]interface{}{
				"depth": *depthFlag,
				"focus": *focusFlag,
			},
		},
		Status: "ACTIVE",
	}

	lensID, err := lensService.CreateLens(ctx, lens)
	if err != nil {
		log.Fatalf("Failed to create lens: %v", err)
	}
	log.Printf("Lens Created: %s (ID: %s)", lens.Name, lensID)

	// Force create job manually to ensure we have something to process
	// (in case LensService logic didn't trigger it or index delay hides it)
	manualJob := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  "global",
			ProjectID: "ux-test-project",
			Status:    agentmodels.AgentJobStatusPending,
			AgentID:   workerAgentID,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			State: map[string]interface{}{
				"targetCanonicalPath": targetPath,
				"lensId":              lensID,
				"currentState":        string(models.ScoutingJobStatePending),  // FSM в State
				"scoutingStrategy":    string(models.ScoutingStrategySnapshot), // ВАЖНО: иначе теряется при сохранении через AgentJob
				"recursionDepth":      0.0,
				"maxRecursionDepth":   float64(*depthFlag),
				// Dynamic metrics requested by the caller (will be canonicalized to /metric/... paths)
				"requestedMetrics": []string{
					"/metric/risk/operational_health",
					"/metric/risk/supply_chain_criticality",
					"risk/market_sentiment",
					"battery_supplier_concentration",
				},
			},
		},
		TargetCanonicalPath: targetPath,
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelBounty),
			ProjectID:   "ux-test-project",
		},
	}
	jid, err := scoutingJobsRepo.Create(ctx, manualJob)
	if err != nil {
		log.Fatalf("FATAL: Failed to create manual job: %v", err)
	}
	log.Printf("Manually created ScoutingJob: %s", jid)
	// Verify creation
	createdJob, err := agentJobsRepo.Get(ctx, jid)
	if err != nil {
		log.Fatalf("FATAL: Created job not found immediately: %v", err)
	}
	log.Printf("DEBUG: Created job verified. Status: %s, AgentID: %s", createdJob.Status, createdJob.AgentID)

	// Give Firestore a moment to index/propagate
	time.Sleep(5 * time.Second)

	// Phase 3: Worker Loop (Processing Scouting Jobs)
	log.Printf("\n--- Phase 3: Scouting & Decomposition Worker Loop ---")
	log.Println("Starting worker loop. Waiting for jobs...")

	// Для freshness-проверки нам не нужен долгий воркер: либо cache-hit сработает быстро,
	// либо мы должны быстро увидеть ошибку/отсутствие job.
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Track processed jobs to avoid infinite loop in test
	processedJobs := make(map[string]bool)
	consecutiveFailures := 0 // Circuit breaker

	for {
		select {
		case <-timeout:
			log.Println("Timeout reached. Stopping worker.")
			goto Summary
		case <-ticker.C:
			// Для детерминизма: мониторим именно job, который создали в этом прогоне.
			// Это защищает от ситуации, когда другой воркер (в облаке/в другом процессе) уже подхватил job,
			// и локальный poller больше не видит его как pending.
			if createdJob, getErr := agentJobsRepo.Get(ctx, jid); getErr == nil && createdJob != nil {
				curState, _ := createdJob.State["currentState"].(string)
				log.Printf("DEBUG: Created Job %s status=%s agentId=%s currentState=%s tenantId=%s projectId=%s",
					jid, createdJob.Status, createdJob.AgentID, curState, createdJob.TenantID, createdJob.ProjectID)

				// Если job уже завершён кем-то другим — выходим к Summary.
				if createdJob.Status == agentmodels.AgentJobStatusCompleted || createdJob.Status == agentmodels.AgentJobStatusFailed {
					goto Summary
				}
			}

			// DEBUG: Check DB content directly
			snaps, _ := fsClient.Collection("agent_jobs").Documents(ctx).GetAll()
			if len(snaps) > 0 {
				log.Printf("DEBUG: DB has %d agent_jobs. First 3:", len(snaps))
				for i, s := range snaps {
					if i >= 3 {
						break
					}
					log.Printf(" - ID: %s, Agent: %v, Status: %v", s.Ref.ID, s.Data()["agentId"], s.Data()["status"])
				}
			} else {
				log.Printf("DEBUG: DB is empty (0 agent_jobs)")
			}

			// Poll pending jobs for THIS test worker agentId (avoid conflicts with prod worker).
			agentJobs, err := agentJobsRepo.ListPendingByAgent(ctx, workerAgentID, 5)
			if err != nil {
				log.Printf("Error polling jobs: %v", err)
				continue
			}

			jobs := make([]*models.ScoutingJob, 0, len(agentJobs))
			for _, aj := range agentJobs {
				j := &models.ScoutingJob{AgentJob: *aj}
				if tp, ok := aj.State["targetCanonicalPath"].(string); ok {
					j.TargetCanonicalPath = tp
				}
				if cs, ok := aj.State["currentState"].(string); ok {
					j.CurrentState = models.ScoutingJobState(cs)
				}
				if lid, ok := aj.State["lensId"].(string); ok {
					j.LensID = lid
				}
				if strat, ok := aj.State["scoutingStrategy"].(string); ok {
					j.ScoutingStrategy = strat
				}
				jobs = append(jobs, j)
			}

			if len(jobs) == 0 {
				log.Printf("DEBUG: No pending scout jobs found for agentId=%s.", workerAgentID)
				continue
			}

			for _, job := range jobs {
				if processedJobs[job.ID] {
					continue
				}
				depth := 0
				if d, ok := job.State["recursionDepth"].(float64); ok {
					depth = int(d)
				}
				log.Printf(">>> Picked up Job: %s for %s (Depth: %v)", job.ID, job.TargetCanonicalPath, depth)

				// Mark as running (normally done by engine/workflow, but good to track here)
				processedJobs[job.ID] = true

				// Execute Sync (for test simplicity)
				// In real app this is async
				err := scoutEngine.ExecuteScoutingJob(ctx, job)
				if err != nil {
					log.Printf("XXX Job Failed: %v", err)
					consecutiveFailures++
					if consecutiveFailures >= 5 {
						log.Fatalf("Circuit breaker tripped: too many consecutive failures (%d). Stopping.", consecutiveFailures)
					}
				} else {
					log.Printf("<<< Job Completed: %s", job.TargetCanonicalPath)
					consecutiveFailures = 0
				}
			}
		}
	}

Summary:
	// Phase 4: Cockpit Data (Graph Dump)
	log.Printf("\n--- Phase 4: Resulting Graph (Cockpit View) ---")

	// Get all fragments in scope + discovered ones
	// Simplification: List all edges from source

	log.Printf("Root: %s", targetPath)
	rootFragment, err := fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
	if err != nil {
		log.Printf("Root fragment not found: %v", err)
	} else {
		printFragment(rootFragment)

		// List edges
		edges, err := edgesRepo.ListBySource(ctx, "global", rootFragment.ID, nil, 100)
		if err != nil {
			log.Printf("Failed to list edges: %v", err)
		} else {
			log.Printf("Dependencies (%d):", len(edges))
			for _, edge := range edges {
				target, _ := fragmentsRepo.Get(ctx, edge.TargetFragmentID)
				targetName := "Unknown"
				if target != nil {
					targetName = target.CanonicalPath
				}
				log.Printf("  --[%s (crit: %.2f)]--> %s", edge.Type, edge.Properties.Criticality, targetName)
				if target != nil {
					printFragment(target)
				}
			}
		}
	}

	// Phase 5: Freshness re-run (should skip expensive work)
	log.Printf("\n--- Phase 5: Freshness Re-run (should be fast, no Research) ---")
	now2 := time.Now().UTC()
	reJob := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  "global",
			ProjectID: "ux-test-project",
			Status:    agentmodels.AgentJobStatusPending,
			AgentID:   workerAgentID,
			CreatedAt: now2,
			UpdatedAt: now2,
			State: map[string]interface{}{
				"targetCanonicalPath": targetPath,
				"lensId":              lensID,
				"currentState":        string(models.ScoutingJobStatePending),
				"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
				"recursionDepth":      0.0,
				"maxRecursionDepth":   float64(*depthFlag),
			},
		},
		TargetCanonicalPath: targetPath,
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelAccess),
			ProjectID:   "ux-test-project",
		},
	}
	reJID, err := scoutingJobsRepo.Create(ctx, reJob)
	if err != nil {
		log.Fatalf("FATAL: Failed to create re-run job: %v", err)
	}
	log.Printf("Created Freshness Re-run ScoutingJob: %s", reJID)
	if j, err := agentJobsRepo.Get(ctx, reJID); err == nil {
		job := &models.ScoutingJob{AgentJob: *j, TargetCanonicalPath: targetPath, ScoutingStrategy: string(models.ScoutingStrategySnapshot)}
		start := time.Now()
		if err := scoutEngine.ExecuteScoutingJob(ctx, job); err != nil {
			log.Printf("Re-run job failed: %v", err)
		} else {
			log.Printf("Re-run completed in %v", time.Since(start))
		}
	}
}

func printFragment(f *models.HorizonFragment) {
	log.Printf("  Fragment: %s (%s)", f.CanonicalPath, f.Type)
	log.Printf("    Status: %s", f.Governance.Status)
	if len(f.State) > 0 {
		log.Printf("    Metrics:")
		for k, v := range f.State {
			log.Printf("      - %s: %.2f (conf: %.2f)", k, v.Value, v.Confidence)
		}
	}
}
