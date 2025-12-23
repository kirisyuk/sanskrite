package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonwatchdog "github.com/igorkirisyuk/sanskrite/internal/horizon/watchdog"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Tool goal:
// - Create/ensure: core user + core horizon project for a tenant (email-based tenant).
// - Create/ensure: a global HorizonFragment bound to that project (CreatedByProjectID).
// - Create: a Lens via LensService (so lifecycle + subscribers + jobs are created).
// - Create/ensure: a RUNNING WatchdogConfig due now for that fragment (facts-driven mode).
// - Insert: an ontology_fact (tenant=global) linked to the fragment canonicalPath and a project datasource.
// - Call real backend watchdog cron endpoint (/api/v1/admin/horizon/watchdog/run) using ADMIN_JOB_TOKEN from Secret Manager.
// - Verify: at least one scout AgentJob exists (pending/running/completed) for the created project/canonicalPath.
func main() {
	ctx := context.Background()

	gcpProject := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID, fallback 'sanskrite')")
	region := flag.String("region", "us-central1", "Cloud Run region for sanskrite-api (default us-central1)")
	tenantID := flag.String("tenant", "kirisyuk@gmail.com", "tenantId (email). This creates core user+project under this tenant.")
	email := flag.String("email", "kirisyuk@gmail.com", "user email (defaults to tenant)")
	apiURL := flag.String("api-url", "", "Cloud Run base URL (if empty, auto-detect via gcloud)")
	canonicalPath := flag.String("canonical-path", "", "target Horizon fragment canonicalPath (default auto-generated)")
	wait := flag.Bool("wait", true, "wait until scout job completes (default true)")
	waitTimeout := flag.Duration("wait-timeout", 20*time.Minute, "max wait time for scout job completion")
	pollEvery := flag.Duration("poll-every", 3*time.Second, "poll interval while waiting")
	validate := flag.Bool("validate", true, "run post-validation after scout job completion")
	strictValidate := flag.Bool("strict-validate", false, "exit non-zero if validation finds no evidence of changes")
	flag.Parse()

	if *gcpProject == "" {
		*gcpProject = "sanskrite"
	}
	if *email == "" {
		*email = *tenantID
	}
	if strings.TrimSpace(*tenantID) == "" {
		log.Fatal("--tenant is required")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *gcpProject)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	usersRepo := repo.NewUsersRepository(db)
	projectsRepo := repo.NewProjectsRepository(db)
	dataSourcesRepo := repo.NewDataSourcesRepository(db)
	sourceProjectsRepo := repo.NewSourceProjectsRepository(db)

	ontologyFactsRepo := repo.NewOntologyFactsRepository(db)

	hFragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	hLensesRepo := horizonrepo.NewLensesRepository(db)
	hWatchdogRepo := horizonrepo.NewWatchdogConfigsRepository(db)
	hFramesRepo := horizonrepo.NewFramesRepository(db)
	hEdgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	agentJobsRepo := repo.NewAgentJobsRepository(db)
	hScoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)

	// --- 1) Ensure core user (tenant-scoped)
	u := &models.User{
		ID:       *email, // use email as stable ID for bootstrap
		TenantID: *tenantID,
		Email:    *email,
	}
	if err := usersRepo.Upsert(ctx, u); err != nil {
		log.Fatalf("failed to upsert core user: %v", err)
	}

	// --- 2) Create core Horizon project (tenant-scoped)
	pt := models.ProjectTypeHorizon
	project := &models.Project{
		TenantID:    *tenantID,
		DisplayName: fmt.Sprintf("Horizon (bootstrap) — %s", *tenantID),
		Status:      "active",
		OwnerUserID: u.ID,
		ProjectType: &pt,
	}
	projectID, err := projectsRepo.Create(ctx, project)
	if err != nil {
		log.Fatalf("failed to create core project: %v", err)
	}
	log.Printf("Created project: tenant=%s projectId=%s", *tenantID, projectID)

	// --- 3) Create a minimal datasource + link to project (tenant-scoped)
	uri := "https://en.wikipedia.org/wiki/Red_Sea_crisis"
	ds := &models.DataSource{
		TenantID:    *tenantID,
		DisplayName: "Bootstrap Source (Wikipedia: Red Sea crisis)",
		Kind:        "web",
		Status:      "active",
		OwnerUserID: u.ID,
		Details: models.SourceDetails{
			URI: &uri,
		},
	}
	dsID, err := dataSourcesRepo.Create(ctx, ds)
	if err != nil {
		log.Fatalf("failed to create datasource: %v", err)
	}
	sp := &models.SourceProject{
		TenantID:   *tenantID,
		SourceID:   dsID,
		ProjectID:  projectID,
		Status:     "active",
		Weights:    map[string]float64{},
		SyncConfig: map[string]any{},
	}
	if _, err := sourceProjectsRepo.Create(ctx, sp); err != nil {
		log.Fatalf("failed to create source->project link: %v", err)
	}
	log.Printf("Created datasource+link: tenant=%s dsId=%s projectId=%s", *tenantID, dsID, projectID)

	// --- 4) Create or get global HorizonFragment for canonicalPath
	targetPath := strings.TrimSpace(*canonicalPath)
	targetPath = normalizeCanonicalPathArg(targetPath)
	if targetPath == "" {
		slug := strings.NewReplacer("@", "-", ".", "-", "+", "-").Replace(strings.ToLower(strings.TrimSpace(*tenantID)))
		targetPath = fmt.Sprintf("/org/bootstrap-%s", slug)
	}
	var fragment *horizonmodels.HorizonFragment
	if existing, err := hFragmentsRepo.GetByCanonicalPath(ctx, targetPath); err == nil && existing != nil {
		fragment = existing
		log.Printf("Using existing fragment: id=%s canonicalPath=%s", fragment.ID, fragment.CanonicalPath)
	} else {
		now := time.Now().UTC()
		createdByProjectID := projectID
		fragType, entityPath, sectorPath, label := inferFragmentDefaults(targetPath, *tenantID)
		fragment = &horizonmodels.HorizonFragment{
			TenantID:      "global",
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
			CreatedBy:     "horizon_bootstrap_lens",
			UpdatedBy:     "horizon_bootstrap_lens",
			Labels:        map[string]string{"en": label},
			Description:   "Bootstrap fragment for end-to-end Horizon flow (lens + watchdog + facts).",
			Tags:          []string{"bootstrap", "horizon"},
			Status:        "active",
			CanonicalPath: targetPath,
			Type:          fragType,
			EntityPath:    entityPath,
			SectorPath:    sectorPath,
			State:         map[string]horizonmodels.MetricValue{},
			Provenance: horizonmodels.Provenance{
				SourceIDs: []string{dsID},
				Method:    "curated",
				ObservedAt: func() *time.Time {
					t := now
					return &t
				}(),
			},
			Metadata: horizonmodels.FragmentMetadata{
				CreatedAt:          now,
				LastUpdatedAt:      now,
				LastVerifiedAt:     now,
				LastFactsSeenAt:    time.Time{},
				LastFactsAppliedAt: time.Time{},
				SourceFrameID:      "",
				EpistemicStatus:    horizonmodels.EpistemicStatusVerified,
			},
			Governance: horizonmodels.Governance{
				Owner:              "SYSTEM",
				ActiveSubscribers:  0,
				Status:             string(horizonmodels.FragmentStatusDormant),
				CreatedByProjectID: &createdByProjectID,
			},
		}
		fragmentID, err := hFragmentsRepo.Create(ctx, fragment)
		if err != nil {
			log.Fatalf("failed to create horizon fragment: %v", err)
		}
		fragment.ID = fragmentID
		log.Printf("Created fragment: id=%s canonicalPath=%s createdByProjectId=%s", fragment.ID, fragment.CanonicalPath, projectID)
	}

	// Baseline snapshot for post-validation
	baseline := snapshotBaseline(ctx, db, fragment, projectID)

	// --- 5) Create Lens via LensService (so it flips fragment ACTIVE and syncs watchdog lifecycle hook)
	lifecycle := horizonlens.NewLifecycleManager(hFragmentsRepo)
	calculator := horizonlens.NewMetricsCalculator(hFragmentsRepo)
	lensService := horizonlens.NewLensService(hLensesRepo, hFragmentsRepo, hScoutingJobsRepo, ontologyFactsRepo, lifecycle, calculator, nil)

	// Wire hook exactly like server does, but with a minimal watchdog service (we only use UpdateConfigLifecycle).
	scheduler := horizonwatchdog.NewScheduler()
	minWatchdog := horizonwatchdog.NewWatchdogService(hWatchdogRepo, hFragmentsRepo, nil, scheduler, nil)
	lensService.SetWatchdogLifecycleHook(func(ctx context.Context, fragmentID string) {
		_ = minWatchdog.UpdateConfigLifecycle(ctx, fragmentID)
	})

	now := time.Now().UTC()
	lens := &horizonmodels.Lens{
		TenantID:    *tenantID,
		ProjectID:   projectID,
		Name:        fmt.Sprintf("Bootstrap Lens — %s", *tenantID),
		Scope:       []string{fragment.CanonicalPath},
		ViewConfig:  map[string]interface{}{"requestedMetrics": []string{"risk", "geopolitical_risk"}, "maxRecursionDepth": 1},
		AlertConfig: map[string]interface{}{},
		Status:      string(horizonmodels.LensStatusActive),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	lensID, err := lensService.CreateLens(ctx, lens)
	if err != nil {
		log.Fatalf("failed to create lens: %v", err)
	}
	log.Printf("Created lens: id=%s scope=%s", lensID, fragment.CanonicalPath)

	// --- 6) Ensure watchdog config exists and is scheduled now (facts-driven mode).
	cfg, err := hWatchdogRepo.GetByTargetFragmentID(ctx, "global", fragment.ID)
	if err != nil {
		// UpdateConfigLifecycle should have created one if ACTIVE; but ensure anyway.
		nextScanAt := now.Add(-1 * time.Minute)
		lastScanAt := now.Add(-2 * time.Hour)
		cfg = &horizonmodels.WatchdogConfig{
			TenantID:          "global",
			TargetFragmentID:  fragment.ID,
			ScanInterval:      "PT5M",
			SourceIDs:         nil,
			UseProjectSources: false,
			TriggerThreshold:  0.5,
			Status:            string(horizonmodels.WatchdogStatusRunning),
			LastScanAt:        &lastScanAt,
			NextScanAt:        &nextScanAt,
		}
		cfgID, err := hWatchdogRepo.Create(ctx, cfg)
		if err != nil {
			log.Fatalf("failed to create watchdog config: %v", err)
		}
		cfg.ID = cfgID
		log.Printf("Created watchdog config: id=%s", cfg.ID)
	} else {
		cfg.Status = string(horizonmodels.WatchdogStatusRunning)
		cfg.ScanInterval = "PT5M"
		cfg.TriggerThreshold = 0.5
		// Ensure it is due now
		next := now.Add(-1 * time.Minute)
		cfg.NextScanAt = &next
		if cfg.LastScanAt == nil {
			last := now.Add(-2 * time.Hour)
			cfg.LastScanAt = &last
		}
		if err := hWatchdogRepo.Update(ctx, cfg); err != nil {
			log.Fatalf("failed to update watchdog config: %v", err)
		}
		log.Printf("Updated watchdog config: id=%s nextScanAt=%s", cfg.ID, cfg.NextScanAt.UTC().Format(time.RFC3339))
	}

	// --- 7) Insert ontology fact (tenant=global) so facts-driven watchdog will trigger.
	factNow := time.Now().UTC()
	fact := &models.OntologyFact{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  "global",
			Version:   1,
			CreatedAt: factNow,
			UpdatedAt: factNow,
			CreatedBy: "horizon_bootstrap_lens",
			UpdatedBy: "horizon_bootstrap_lens",
			Labels:    map[string]string{"en": "Bootstrap fact (watchdog trigger)"},
			Status:    "active",
			CanonicalPaths: []string{
				fragment.CanonicalPath,
			},
			ActorPaths: []string{
				fragment.CanonicalPath,
			},
		},
		Kind:       "event",
		Type:       "bootstrap_trigger",
		Title:      fmt.Sprintf("Bootstrap trigger for %s", fragment.CanonicalPath),
		Summary:    "Created by horizon_bootstrap_lens tool to validate watchdog->facts->scout wiring on real backend.",
		Confidence: 0.99,
		ObservedAt: ptrTime(factNow),
		SourceIDs:  []string{dsID},
		SourceRefs: []models.SourceRef{{SourceID: dsID, SourceType: "dataSource"}},
	}
	fact.VectorText = fact.GenerateVectorText()
	factID, err := ontologyFactsRepo.Create(ctx, fact)
	if err != nil {
		log.Fatalf("failed to create ontology_fact: %v", err)
	}
	log.Printf("Created ontology_fact: id=%s canonicalPath=%s sourceId=%s", factID, fragment.CanonicalPath, dsID)

	// --- 8) Call real backend watchdog cron endpoint.
	baseURL := strings.TrimSpace(*apiURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(runCmd("gcloud", "run", "services", "describe", "sanskrite-api", "--region", *region, "--format=value(status.url)"))
	}
	if baseURL == "" {
		log.Fatal("cannot resolve Cloud Run API URL (pass --api-url or ensure gcloud run services describe works)")
	}
	token := strings.TrimSpace(runCmd("gcloud", "secrets", "versions", "access", "latest", "--secret=admin_job_token"))
	if token == "" {
		log.Fatal("failed to read admin_job_token from Secret Manager")
	}

	cronURL := strings.TrimRight(baseURL, "/") + "/api/v1/admin/horizon/watchdog/run?limit=50"
	respBody, statusCode, err := postWithJobToken(ctx, cronURL, token, nil)
	if err != nil {
		log.Fatalf("watchdog cron call failed: %v", err)
	}
	log.Printf("Called watchdog cron: status=%d url=%s", statusCode, cronURL)
	if len(respBody) > 0 {
		log.Printf("watchdog response: %s", strings.TrimSpace(respBody))
	}

	// --- 9) Find scout job created by watchdog and optionally wait for completion.
	jobID := findScoutJobID(ctx, db, projectID, fragment.CanonicalPath, 60*time.Second, *pollEvery)
	if jobID == "" {
		log.Printf("WARNING: did not observe scout job creation for projectId=%s canonicalPath=%s", projectID, fragment.CanonicalPath)
	} else {
		log.Printf("Observed scout job: id=%s (now=%s)", jobID, time.Now().UTC().Format(time.RFC3339))
		if *wait {
			finalStatus, err := waitForAgentJobTerminal(ctx, db, jobID, *waitTimeout, *pollEvery)
			if err != nil {
				log.Fatalf("scout job did not reach terminal state: %v", err)
			}
			log.Printf("Scout job terminal: id=%s status=%s", jobID, finalStatus)
		}
	}

	if *validate {
		report := postValidate(ctx, db, hFramesRepo, hEdgesRepo, fragment, projectID, baseline)
		log.Printf("Post-validate summary: fragmentUpdated=%v framesDelta=%d outEdgesDelta=%d inEdgesDelta=%d projectFragmentsDelta=%d",
			report.FragmentUpdated, report.FramesDelta, report.OutEdgesDelta, report.InEdgesDelta, report.ProjectFragmentsDelta)
		if *strictValidate && !report.HasEvidenceOfWork() {
			log.Fatalf("strict validation failed: no evidence of changes (enable logs or re-run with longer wait-timeout)")
		}
	}

	log.Printf("DONE. Lens=%s Project=%s Tenant=%s Fragment=%s WatchdogConfig=%s Fact=%s", lensID, projectID, *tenantID, fragment.CanonicalPath, cfg.ID, factID)
}

func inferFragmentDefaults(canonicalPath string, tenantID string) (horizonmodels.FragmentType, string, string, string) {
	cp := strings.TrimSpace(canonicalPath)
	switch {
	case strings.HasPrefix(cp, "/program/"):
		return horizonmodels.FragmentTypeProgram, "/program", "/sector/unknown", fmt.Sprintf("Bootstrap Program (%s)", tenantID)
	case strings.HasPrefix(cp, "/market/"):
		return horizonmodels.FragmentTypeMarket, "/market", "/sector/unknown", fmt.Sprintf("Bootstrap Market (%s)", tenantID)
	case strings.HasPrefix(cp, "/product/"):
		return horizonmodels.FragmentTypeProduct, "/product", "/sector/unknown", fmt.Sprintf("Bootstrap Product (%s)", tenantID)
	case strings.HasPrefix(cp, "/fac/"):
		return horizonmodels.FragmentTypeFacility, "/Infrastructure/Facility", "/sector/unknown", fmt.Sprintf("Bootstrap Facility (%s)", tenantID)
	case strings.HasPrefix(cp, "/loc/"):
		return horizonmodels.FragmentTypeGeoRegion, "/Geo/Region", "/Geography", fmt.Sprintf("Bootstrap Location (%s)", tenantID)
	case strings.HasPrefix(cp, "/sphere/"):
		return horizonmodels.FragmentTypeSphere, "/Influence/Sphere", "/Influence", fmt.Sprintf("Bootstrap Sphere (%s)", tenantID)
	case strings.HasPrefix(cp, "/person/"):
		return horizonmodels.FragmentTypePerson, "/Person/Individual", "/sector/unknown", fmt.Sprintf("Bootstrap Person (%s)", tenantID)
	case strings.HasPrefix(cp, "/event/"):
		return horizonmodels.FragmentTypeEvent, "/Event", "/sector/unknown", fmt.Sprintf("Bootstrap Event (%s)", tenantID)
	default:
		return horizonmodels.FragmentTypeOrganization, "/org", "/sector/unknown", fmt.Sprintf("Bootstrap Org (%s)", tenantID)
	}
}

// normalizeCanonicalPathArg mitigates MSYS/Git-Bash path conversion on Windows where
// "/program/..." may become "C:/Program Files/Git/program/..." (or similar).
// It extracts the first recognized Horizon canonical namespace suffix.
func normalizeCanonicalPathArg(in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return ""
	}
	// Normalize slashes
	s = strings.ReplaceAll(s, "\\", "/")

	// If already canonical, keep as-is.
	if strings.HasPrefix(s, "/") {
		return s
	}

	// Attempt to extract the canonical suffix from a converted absolute path.
	candidates := []string{
		"/org/",
		"/fac/",
		"/loc/",
		"/product/",
		"/program/",
		"/market/",
		"/sphere/",
		"/event/",
		"/person/",
	}
	for _, pref := range candidates {
		if idx := strings.Index(s, pref); idx >= 0 {
			return s[idx:]
		}
	}
	return s
}

func ptrTime(t time.Time) *time.Time { return &t }

func runCmd(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func postWithJobToken(ctx context.Context, url, token string, payload any) (string, int, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return "", 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("X-Job-Token", token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(b), resp.StatusCode, fmt.Errorf("non-2xx response: %s", resp.Status)
	}
	return string(b), resp.StatusCode, nil
}

type agentJobLite struct {
	ID        string                 `firestore:"id"`
	AgentID   string                 `firestore:"agentId"`
	ProjectID string                 `firestore:"projectId"`
	Status    string                 `firestore:"status"`
	State     map[string]interface{} `firestore:"state"`
	CreatedAt time.Time              `firestore:"createdAt"`
	UpdatedAt time.Time              `firestore:"updatedAt"`
}

func findScoutJobID(ctx context.Context, db *firestore.Client, projectID, canonicalPath string, timeout time.Duration, pollEvery time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		docs, err := db.Collection("agent_jobs").
			Where("projectId", "==", projectID).
			Limit(200).
			Documents(ctx).GetAll()
		if err != nil {
			time.Sleep(pollEvery)
			continue
		}
		for _, d := range docs {
			var aj agentJobLite
			if err := d.DataTo(&aj); err != nil {
				continue
			}
			aj.ID = d.Ref.ID
			if strings.TrimSpace(aj.AgentID) != "scout" {
				continue
			}
			if aj.State != nil {
				if tp, ok := aj.State["targetCanonicalPath"].(string); ok && strings.TrimSpace(tp) == canonicalPath {
					return aj.ID
				}
			}
		}
		time.Sleep(pollEvery)
	}
	return ""
}

func waitForAgentJobTerminal(ctx context.Context, db *firestore.Client, jobID string, timeout time.Duration, pollEvery time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	lastStatus := ""
	for time.Now().Before(deadline) {
		snap, err := db.Collection("agent_jobs").Doc(jobID).Get(ctx)
		if err != nil {
			time.Sleep(pollEvery)
			continue
		}
		var aj agentJobLite
		if err := snap.DataTo(&aj); err != nil {
			time.Sleep(pollEvery)
			continue
		}
		aj.ID = jobID
		status := strings.TrimSpace(strings.ToLower(aj.Status))
		if status != lastStatus {
			log.Printf("Scout job status changed: id=%s %s -> %s", jobID, lastStatus, status)
			lastStatus = status
		}
		switch status {
		case "completed", "failed", "partial":
			return status, nil
		default:
			time.Sleep(pollEvery)
		}
	}
	return "", fmt.Errorf("timeout waiting for job %s (lastStatus=%s)", jobID, lastStatus)
}

type baselineSnapshot struct {
	AtUTC             time.Time
	FragmentUpdatedAt time.Time
	MetaLastUpdatedAt time.Time
	FramesCount       int
	OutEdgesCount     int
	InEdgesCount      int
	ProjectFragments  int
}

func snapshotBaseline(ctx context.Context, db *firestore.Client, fragment *horizonmodels.HorizonFragment, projectID string) baselineSnapshot {
	now := time.Now().UTC()
	out := baselineSnapshot{AtUTC: now}
	if fragment != nil {
		out.FragmentUpdatedAt = fragment.UpdatedAt
		out.MetaLastUpdatedAt = fragment.Metadata.LastUpdatedAt
	}
	out.FramesCount = countFramesByTargetCanonicalPath(ctx, db, "global", fragment.CanonicalPath, 50)
	out.OutEdgesCount = countEdgesBySource(ctx, db, "global", fragment.ID, 200)
	out.InEdgesCount = countEdgesByTarget(ctx, db, "global", fragment.ID, 200)
	out.ProjectFragments = countProjectFragments(ctx, db, projectID, 200)
	return out
}

type validationReport struct {
	FragmentUpdated       bool
	FramesDelta           int
	OutEdgesDelta         int
	InEdgesDelta          int
	ProjectFragmentsDelta int
}

func (r validationReport) HasEvidenceOfWork() bool {
	if r.FragmentUpdated {
		return true
	}
	if r.FramesDelta > 0 || r.OutEdgesDelta > 0 || r.InEdgesDelta > 0 || r.ProjectFragmentsDelta > 0 {
		return true
	}
	return false
}

func postValidate(
	ctx context.Context,
	db *firestore.Client,
	framesRepo *horizonrepo.FramesRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
	fragment *horizonmodels.HorizonFragment,
	projectID string,
	base baselineSnapshot,
) validationReport {
	_ = framesRepo
	_ = edgesRepo

	// Re-fetch fragment to avoid stale data.
	refreshed := fragment
	if fragment != nil && fragment.ID != "" {
		if snap, err := db.Collection("horizon_fragments").Doc(fragment.ID).Get(ctx); err == nil && snap.Exists() {
			var f horizonmodels.HorizonFragment
			if err := snap.DataTo(&f); err == nil {
				f.ID = fragment.ID
				refreshed = &f
			}
		}
	}

	report := validationReport{}
	if refreshed != nil {
		// Any update after baseline snapshot time is considered evidence.
		if refreshed.UpdatedAt.After(base.AtUTC) || refreshed.Metadata.LastUpdatedAt.After(base.AtUTC) {
			report.FragmentUpdated = true
		}
	}

	framesNow := countFramesByTargetCanonicalPath(ctx, db, "global", fragment.CanonicalPath, 50)
	outEdgesNow := countEdgesBySource(ctx, db, "global", fragment.ID, 200)
	inEdgesNow := countEdgesByTarget(ctx, db, "global", fragment.ID, 200)
	projectFragmentsNow := countProjectFragments(ctx, db, projectID, 200)

	report.FramesDelta = framesNow - base.FramesCount
	report.OutEdgesDelta = outEdgesNow - base.OutEdgesCount
	report.InEdgesDelta = inEdgesNow - base.InEdgesCount
	report.ProjectFragmentsDelta = projectFragmentsNow - base.ProjectFragments

	// Extra helpful logs
	if refreshed != nil {
		log.Printf("Post-validate fragment: id=%s canonicalPath=%s governance.status=%s subscribers=%d updatedAt=%s meta.lastUpdatedAt=%s",
			refreshed.ID, refreshed.CanonicalPath, refreshed.Governance.Status, refreshed.Governance.ActiveSubscribers,
			refreshed.UpdatedAt.UTC().Format(time.RFC3339), refreshed.Metadata.LastUpdatedAt.UTC().Format(time.RFC3339))
	}
	log.Printf("Post-validate counts: frames=%d (base=%d) outEdges=%d (base=%d) inEdges=%d (base=%d) projectFragments=%d (base=%d)",
		framesNow, base.FramesCount, outEdgesNow, base.OutEdgesCount, inEdgesNow, base.InEdgesCount, projectFragmentsNow, base.ProjectFragments)

	return report
}

func countFramesByTargetCanonicalPath(ctx context.Context, db *firestore.Client, tenantID, canonicalPath string, limit int) int {
	if canonicalPath == "" {
		return 0
	}
	q := db.Collection("horizon_frames").
		Where("tenantId", "==", tenantID).
		Where("targetCanonicalPath", "==", canonicalPath).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0
	}
	return len(docs)
}

func countEdgesBySource(ctx context.Context, db *firestore.Client, tenantID, sourceFragmentID string, limit int) int {
	if sourceFragmentID == "" {
		return 0
	}
	q := db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", tenantID).
		Where("sourceFragmentId", "==", sourceFragmentID).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0
	}
	return len(docs)
}

func countEdgesByTarget(ctx context.Context, db *firestore.Client, tenantID, targetFragmentID string, limit int) int {
	if targetFragmentID == "" {
		return 0
	}
	q := db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", tenantID).
		Where("targetFragmentId", "==", targetFragmentID).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0
	}
	return len(docs)
}

func countProjectFragments(ctx context.Context, db *firestore.Client, projectID string, limit int) int {
	if projectID == "" {
		return 0
	}
	q := db.Collection("horizon_fragments").
		Where("governance.createdByProjectId", "==", projectID).
		Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0
	}
	return len(docs)
}
