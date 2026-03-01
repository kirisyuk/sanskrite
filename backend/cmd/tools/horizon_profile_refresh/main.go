package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/profile"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tasks"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")

	// Execution mode
	fragmentType = flag.String("type", "", "Fragment type to refresh: ORGANIZATION, MARKET, PRODUCT, GOV (or 'all' for all enabled types)")
	fragmentID   = flag.String("fragment-id", "", "Single fragment ID to refresh (on-demand mode)")

	// Policy overrides
	topPercent   = flag.Float64("top-percent", 0, "Override top percent (e.g., 0.10 for 10%)")
	maxPerRun    = flag.Int("max-per-run", 0, "Override max fragments per run")
	minEdgeCount = flag.Int("min-edge-count", 0, "Override minimum edge count")

	// Output
	outPath = flag.String("out", "", "Output JSONL file for results (default: stdout)")

	// Seed defaults
	seedPolicies = flag.Bool("seed-policies", false, "Seed default refresh policies to Firestore and exit")

	// Safety gate for scheduled runs
	enableScheduled = flag.Bool("enable-scheduled", false, "Enable scheduled runs (default: false)")
)

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()

	// Initialize Firestore
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer db.Close()

	// Initialize repositories
	fragmentsRepo := repo.NewHorizonFragmentsRepository(db)
	edgesRepo := repo.NewDependencyEdgesRepository(db)
	profilesRepo := repo.NewFragmentProfilesRepository(db)
	policiesRepo := repo.NewFragmentProfileRefreshPoliciesRepository(db)

	// Handle seed-policies mode
	if *seedPolicies {
		log.Println("Seeding default refresh policies...")
		if err := policiesRepo.SeedDefaults(ctx); err != nil {
			log.Fatalf("Failed to seed policies: %v", err)
		}
		log.Println("Default policies seeded successfully")
		return
	}

	// Initialize AI executor
	mm := ai.NewAIModelManager()
	services := buildAIServices(mm)
	exec := ai.NewAITaskExecutor(services, mm)

	// Initialize generator and scheduler
	generator := profile.NewGenerator(exec, fragmentsRepo, edgesRepo, profilesRepo)
	tasksDispatcher := buildProfileTasksDispatcher(ctx, *projectID)
	scheduler := profile.NewScheduler(generator, fragmentsRepo, edgesRepo, profilesRepo, policiesRepo, tasksDispatcher)

	// Output sink
	out := os.Stdout
	if strings.TrimSpace(*outPath) != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("Failed to create output file: %v", err)
		}
		defer f.Close()
		out = f
	}

	writeResult := func(result interface{}) {
		b, _ := json.Marshal(result)
		_, _ = out.Write(append(b, '\n'))
	}

	// Single fragment mode
	if strings.TrimSpace(*fragmentID) != "" {
		log.Printf("Refreshing single fragment: %s", *fragmentID)
		result, err := scheduler.RefreshSingle(ctx, strings.TrimSpace(*fragmentID))
		if err != nil {
			log.Fatalf("Failed to refresh fragment: %v", err)
		}
		writeResult(map[string]interface{}{
			"mode":       "single",
			"fragmentId": *fragmentID,
			"created":    result.Created,
			"profileId":  result.Profile.ID,
			"durationMs": result.DurationMs,
		})
		log.Printf("Profile generated: %s", result.Profile.ID)
		return
	}

	// Scheduled refresh mode
	if strings.TrimSpace(*fragmentType) == "" {
		log.Fatal("Either --type or --fragment-id is required")
	}

	if !*enableScheduled {
		log.Fatal("Scheduled runs are disabled by default. Pass --enable-scheduled to proceed.")
	}

	ft := strings.ToUpper(strings.TrimSpace(*fragmentType))

	// Apply policy overrides if specified
	if *topPercent > 0 || *maxPerRun > 0 || *minEdgeCount > 0 {
		applyPolicyOverrides(ctx, policiesRepo, models.FragmentType(ft))
	}

	startTime := time.Now()

	if ft == "ALL" {
		// Run all types
		log.Println("Running refresh for all enabled fragment types...")
		results, err := scheduler.RunAllTypes(ctx)
		if err != nil {
			log.Fatalf("Failed to run all types: %v", err)
		}

		totalCreated := 0
		totalFailed := 0
		for fragType, result := range results {
			writeResult(map[string]interface{}{
				"mode":            "scheduled",
				"fragmentType":    fragType,
				"candidatesFound": result.CandidatesFound,
				"profilesCreated": result.ProfilesCreated,
				"profilesSkipped": result.ProfilesSkipped,
				"profilesFailed":  result.ProfilesFailed,
				"durationMs":      result.DurationMs,
				"errors":          result.Errors,
			})
			totalCreated += result.ProfilesCreated
			totalFailed += result.ProfilesFailed
		}

		log.Printf("Completed all types: totalCreated=%d totalFailed=%d duration=%s",
			totalCreated, totalFailed, time.Since(startTime))
	} else {
		// Run single type
		fragType := models.FragmentType(ft)
		log.Printf("Running refresh for type: %s", fragType)

		result, err := scheduler.RunRefresh(ctx, fragType)
		if err != nil {
			log.Fatalf("Failed to run refresh: %v", err)
		}

		writeResult(map[string]interface{}{
			"mode":            "scheduled",
			"fragmentType":    fragType,
			"candidatesFound": result.CandidatesFound,
			"profilesCreated": result.ProfilesCreated,
			"profilesSkipped": result.ProfilesSkipped,
			"profilesFailed":  result.ProfilesFailed,
			"durationMs":      result.DurationMs,
			"errors":          result.Errors,
		})

		log.Printf("Completed type %s: created=%d skipped=%d failed=%d duration=%s",
			fragType, result.ProfilesCreated, result.ProfilesSkipped, result.ProfilesFailed, time.Since(startTime))
	}
}

func buildProfileTasksDispatcher(ctx context.Context, projectID string) *tasks.ProfileCloudTasksDispatcher {
	enabled := strings.TrimSpace(os.Getenv("ENABLE_CLOUD_TASKS_PROFILE"))
	if enabled == "" {
		enabled = "1"
	}
	if enabled != "1" && !strings.EqualFold(enabled, "true") {
		return nil
	}

	location := strings.TrimSpace(os.Getenv("CLOUD_TASKS_LOCATION"))
	if location == "" {
		location = "us-central1"
	}
	queue := strings.TrimSpace(os.Getenv("CLOUD_TASKS_PROFILE_QUEUE"))
	if queue == "" {
		queue = "etigram-profile-refresh"
	}
	baseURL := strings.TrimSpace(os.Getenv("CLOUD_TASKS_TARGET_BASE_URL"))
	saEmail := strings.TrimSpace(os.Getenv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT"))
	aud := strings.TrimSpace(os.Getenv("CLOUD_TASKS_OIDC_AUDIENCE"))
	if baseURL == "" || saEmail == "" {
		return nil
	}
	targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/tasks/profile/refresh"
	if aud == "" {
		aud = strings.TrimRight(baseURL, "/")
	}

	d, err := tasks.NewProfileCloudTasksDispatcher(ctx, strings.TrimSpace(projectID), location, queue, targetURL, saEmail, aud)
	if err != nil {
		log.Printf("WARNING: Cloud Tasks (Profile) disabled due to init error: %v", err)
		return nil
	}
	log.Printf("Cloud Tasks (Profile) enabled: queue=%s location=%s target=%s", queue, location, targetURL)
	return d
}

func applyPolicyOverrides(ctx context.Context, policiesRepo *repo.FragmentProfileRefreshPoliciesRepository, fragmentType models.FragmentType) {
	policy, err := policiesRepo.GetByType(ctx, fragmentType)
	if err != nil {
		log.Printf("Warning: could not get policy for overrides: %v", err)
		return
	}

	updated := false
	if *topPercent > 0 {
		policy.TopPercent = *topPercent
		updated = true
	}
	if *maxPerRun > 0 {
		policy.MaxPerRun = *maxPerRun
		updated = true
	}
	if *minEdgeCount > 0 {
		policy.MinEdgeCount = *minEdgeCount
		updated = true
	}

	if updated {
		if err := policiesRepo.Upsert(ctx, policy); err != nil {
			log.Printf("Warning: could not save policy overrides: %v", err)
		} else {
			log.Printf("Applied policy overrides for %s: topPercent=%.2f maxPerRun=%d minEdgeCount=%d",
				fragmentType, policy.TopPercent, policy.MaxPerRun, policy.MinEdgeCount)
		}
	}
}

func buildAIServices(mm *ai.AIModelManager) []ai.AIService {
	services := make([]ai.AIService, 0)

	// Try Vertex AI / Gemini
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey != "" {
		services = append(services, ai.NewGeminiService(apiKey, mm))
	}

	// Try Grok (optional)
	grokKey := os.Getenv("GROK_API_KEY")
	if grokKey != "" {
		services = append(services, ai.NewGrokService(grokKey, mm))
	}

	if len(services) == 0 {
		log.Println("WARNING: No AI services configured. Set VERTEX_API_KEY or GEMINI_API_KEY.")
	}

	return services
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `horizon_profile_refresh - Generate and refresh fragment profiles

Usage:
  horizon_profile_refresh --project=PROJECT_ID [options]

Modes:
  Scheduled refresh (default):
    --type=ORGANIZATION    Refresh profiles for a specific fragment type
    --type=all             Refresh all enabled fragment types

  On-demand refresh:
    --fragment-id=ID       Refresh a single fragment's profile

Examples:
  # Seed default refresh policies
  horizon_profile_refresh --project=my-project --seed-policies

  # Refresh top 10%% of ORGANIZATION fragments
  horizon_profile_refresh --project=my-project --type=ORGANIZATION --enable-scheduled

  # Refresh with custom parameters
  horizon_profile_refresh --project=my-project --type=MARKET --top-percent=0.05 --max-per-run=100 --enable-scheduled

  # Refresh a single fragment on demand
  horizon_profile_refresh --project=my-project --fragment-id=abc123

  # Refresh all types and output to file
  horizon_profile_refresh --project=my-project --type=all --out=results.jsonl --enable-scheduled

Options:
`)
		flag.PrintDefaults()
	}
}
