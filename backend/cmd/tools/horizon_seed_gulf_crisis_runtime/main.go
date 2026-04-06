package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	analytical "github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	globalTenant    = "global"
	watchPackID     = "gulf-crisis-focus-pack-v1"
	watchBootstrap  = "horizon-gulf-crisis-bootstrap@system.local"
	defaultPollMins = 120
)

type sourceSpec struct {
	Key         string
	DisplayName string
	Kind        string
	URL         string
	Category    string
	FeedType    string
	SignalType  string
	PollMinutes int
	Processing  string
	Output      string
	DisableLLM  bool
	Status      string
}

type watchSpec struct {
	Label string
	Path  string
	Kind  hmodels.WatchSubjectKind
	Query string
	Modes []string
	Roles []string
	Prio  int
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), "nv-horizon-test"), "GCP project id")
	apply := flag.Bool("apply", true, "Apply changes to Firestore")
	seedSources := flag.Bool("sources", true, "Seed focused Gulf crisis sources")
	seedWatch := flag.Bool("watch", true, "Seed focused Gulf watch subjects")
	flag.Parse()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	dsRepo := corerepo.NewDataSourcesRepository(db)
	watchPacksRepo := hrepo.NewWatchPacksRepository(db)
	watchSubjectsRepo := hrepo.NewWatchSubjectsRepository(db)
	compiledWatchRepo := hrepo.NewCompiledWatchSubjectsRepository(db)
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	scopeConfigsRepo := hrepo.NewScopeAgentConfigsRepository(db)
	watchdogRepo := hrepo.NewWatchdogConfigsRepository(db)

	createdSources := 0
	updatedSources := 0
	if *seedSources {
		for _, spec := range buildSourceSpecs() {
			changed, err := upsertSource(ctx, dsRepo, spec, *apply)
			if err != nil {
				log.Fatalf("seed source %s failed: %v", spec.Key, err)
			}
			if changed == "created" {
				createdSources++
			}
			if changed == "updated" {
				updatedSources++
			}
		}
		for _, id := range deprecatedSourceIDs() {
			if err := deactivateSource(ctx, dsRepo, id, *apply); err != nil {
				log.Fatalf("deactivate source %s failed: %v", id, err)
			}
		}
	}

	createdWatch := 0
	updatedWatch := 0
	createdScopeConfigs := 0
	updatedScopeConfigs := 0
	createdWatchdogs := 0
	updatedWatchdogs := 0
	if *seedWatch {
		if *apply {
			if err := watchPacksRepo.Upsert(ctx, &hmodels.WatchPack{
				ID:          watchPackID,
				TenantID:    globalTenant,
				Name:        "Gulf Crisis Focus Pack v1",
				Description: "Focused watch coverage for Gulf strikes, ports, chokepoints and downstream operational exposure.",
				Status:      string(hmodels.WatchTargetStatusActive),
			}); err != nil {
				log.Fatalf("watch pack upsert failed: %v", err)
			}
		}
		for _, spec := range buildWatchSpecs() {
			subject, changed, err := upsertWatch(ctx, watchSubjectsRepo, compiledWatchRepo, fragmentsRepo, spec, *apply)
			if err != nil {
				log.Fatalf("seed watch %s failed: %v", spec.Path, err)
			}
			if changed == "created" {
				createdWatch++
			}
			if changed == "updated" {
				updatedWatch++
			}
			scopeChanged, watchdogChanged, err := ensureRuntime(ctx, scopeConfigsRepo, watchdogRepo, subject, *apply)
			if err != nil {
				log.Fatalf("ensure runtime %s failed: %v", spec.Path, err)
			}
			if scopeChanged == "created" {
				createdScopeConfigs++
			}
			if scopeChanged == "updated" {
				updatedScopeConfigs++
			}
			if watchdogChanged == "created" {
				createdWatchdogs++
			}
			if watchdogChanged == "updated" {
				updatedWatchdogs++
			}
		}
	}

	log.Printf(
		"gulf runtime seed complete: sources(created=%d updated=%d) watch(created=%d updated=%d) scopeConfigs(created=%d updated=%d) watchdogs(created=%d updated=%d) apply=%v",
		createdSources,
		updatedSources,
		createdWatch,
		updatedWatch,
		createdScopeConfigs,
		updatedScopeConfigs,
		createdWatchdogs,
		updatedWatchdogs,
		*apply,
	)
}

func buildSourceSpecs() []sourceSpec {
	return []sourceSpec{
		{
			Key:         "gulf-canvas-kinetic-locations",
			DisplayName: "Gulf Crisis Canvas: Kinetic Locations",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`(Iran OR "United States" OR UAE OR "Saudi Arabia" OR Qatar OR Bahrain OR Kuwait OR Oman) (missile OR drone OR strike OR attack OR interception OR blast) (Fujairah OR "Fujairah port" OR Khalifa OR "Jebel Ali" OR Doha OR Riyadh OR Dammam OR "Abu Dhabi" OR Dubai OR Manama OR Kuwait OR Muscat OR "Ras Laffan" OR "Ras Tanura" OR Hormuz)`),
			Category:    "conflict",
			FeedType:    "gulf_canvas_kinetic_locations",
			SignalType:  "risk:conflict_event",
			PollMinutes: defaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			DisableLLM:  false,
			Status:      "active",
		},
		{
			Key:         "gulf-canvas-logistics-cascade",
			DisplayName: "Gulf Crisis Canvas: Logistics Cascade",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`("Strait of Hormuz" OR Fujairah OR Khalifa OR "Jebel Ali" OR "Ras Laffan" OR "Ras Tanura" OR Dammam OR "Abu Dhabi" OR Dubai OR Doha OR Bahrain OR Kuwait OR Muscat) (port OR refinery OR LNG OR tanker OR shipping OR airspace OR airport OR logistics OR desalination) (attack OR strike OR disruption OR closure OR interception)`),
			Category:    "maritime",
			FeedType:    "gulf_canvas_logistics_cascade",
			SignalType:  "risk:logistics_disruption",
			PollMinutes: defaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			DisableLLM:  false,
			Status:      "active",
		},
		{
			Key:         "gulf-canvas-regional-strike-geography",
			DisplayName: "Gulf Crisis Canvas: Regional Strike Geography",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`("United Arab Emirates" OR UAE OR "Saudi Arabia" OR Qatar OR Bahrain OR Kuwait OR Oman) (missile OR drone OR strike OR attack OR interception OR airspace OR blast) ("Abu Dhabi" OR Dubai OR Fujairah OR "Jebel Ali" OR Khalifa OR Dammam OR Jubail OR Riyadh OR Doha OR "Ras Laffan" OR Manama OR Sitra OR "Mina al-Ahmadi" OR Kuwait OR Muscat OR Sohar OR Duqm OR Salalah)`),
			Category:    "conflict",
			FeedType:    "gulf_canvas_regional_strike_geography",
			SignalType:  "risk:regional_strike_geography",
			PollMinutes: defaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			DisableLLM:  false,
			Status:      "active",
		},
		{
			Key:         "gulf-canvas-energy-water-sites",
			DisplayName: "Gulf Crisis Canvas: Energy and Water Sites",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`("Habshan" OR Fujairah OR "Jebel Ali" OR "Ras Laffan" OR "Ras Tanura" OR "Mina al-Ahmadi" OR Bahrain OR Sitra OR Sohar OR Salalah OR Duqm) (desalination OR "potable water" OR refinery OR "gas complex" OR "oil terminal" OR pipeline OR power OR storage) (attack OR strike OR drone OR missile OR disruption OR fire OR closure)`),
			Category:    "critical_infrastructure",
			FeedType:    "gulf_canvas_energy_water_sites",
			SignalType:  "risk:critical_infrastructure_disruption",
			PollMinutes: defaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			DisableLLM:  false,
			Status:      "active",
		},
		{
			Key:         "gulf-canvas-airspace-and-urban-intercepts",
			DisplayName: "Gulf Crisis Canvas: Airspace and Urban Intercepts",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`("Abu Dhabi" OR Dubai OR Doha OR Manama OR Kuwait OR Muscat OR Dammam OR Riyadh) (airspace OR airport OR drone OR missile OR interception OR blast OR strike) (closure OR disruption OR attack OR warning)`),
			Category:    "conflict",
			FeedType:    "gulf_canvas_airspace_urban_intercepts",
			SignalType:  "risk:airspace_disruption",
			PollMinutes: defaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			DisableLLM:  false,
			Status:      "active",
		},
	}
}

func deprecatedSourceIDs() []string {
	return []string{
		buildSourceID("gulf-strikes-wire-query"),
		buildSourceID("gulf-ports-shipping-query"),
		buildSourceID("gulf-airspace-infrastructure-query"),
		buildSourceID("bbc-middle-east"),
		buildSourceID("aljazeera-all"),
		buildSourceID("hapi-conflict-context"),
		buildSourceID("nga-maritime-warnings"),
		"kauQCSBHBhOhUXiKkhUk",
		"tQd7xQ3sfe8jqSwva9DD",
	}
}

func buildWatchSpecs() []watchSpec {
	return []watchSpec{
		{Label: "Strait of Hormuz", Path: "/loc/int/strait-of-hormuz", Kind: hmodels.WatchSubjectKindExplicitTarget, Query: "Hormuz shipping, attacks, closure risk, maritime insurance and route disruption", Modes: []string{"conflict", "maritime", "energy"}, Roles: []string{"monitor", "risk"}, Prio: 98},
		{Label: "United Arab Emirates", Path: "/loc/ae", Kind: hmodels.WatchSubjectKindGeography, Query: "UAE strikes, drone intercepts, port disruption, airspace and infrastructure exposure", Modes: []string{"conflict", "transport", "energy"}, Roles: []string{"monitor", "risk"}, Prio: 95},
		{Label: "Abu Dhabi", Path: "/loc/ae/abu-dhabi/abu-dhabi", Kind: hmodels.WatchSubjectKindGeography, Query: "Abu Dhabi strikes, air defense, port and airport disruption, desalination and power exposure", Modes: []string{"conflict", "transport", "energy"}, Roles: []string{"monitor", "risk"}, Prio: 93},
		{Label: "Dubai", Path: "/loc/ae/dubai/dubai", Kind: hmodels.WatchSubjectKindGeography, Query: "Dubai strikes, Jebel Ali disruption, airport and logistics exposure under Gulf conflict", Modes: []string{"conflict", "transport", "maritime"}, Roles: []string{"monitor", "risk"}, Prio: 94},
		{Label: "Fujairah", Path: "/loc/ae/fujairah/fujairah", Kind: hmodels.WatchSubjectKindGeography, Query: "Fujairah strikes, oil terminal disruption, maritime rerouting and desalination exposure", Modes: []string{"conflict", "maritime", "energy"}, Roles: []string{"monitor", "risk"}, Prio: 97},
		{Label: "Saudi Arabia", Path: "/loc/sa", Kind: hmodels.WatchSubjectKindGeography, Query: "Saudi strikes, refinery and export route disruption, downstream Gulf exposure", Modes: []string{"conflict", "energy", "transport"}, Roles: []string{"monitor", "risk"}, Prio: 95},
		{Label: "Qatar", Path: "/loc/qa", Kind: hmodels.WatchSubjectKindGeography, Query: "Qatar drone and missile attacks, LNG and helium export disruption", Modes: []string{"conflict", "energy", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 94},
		{Label: "Bahrain", Path: "/loc/bh", Kind: hmodels.WatchSubjectKindGeography, Query: "Bahrain air defense, shipping and regional disruption", Modes: []string{"conflict", "transport"}, Roles: []string{"monitor", "risk"}, Prio: 90},
		{Label: "Kuwait", Path: "/loc/kw", Kind: hmodels.WatchSubjectKindGeography, Query: "Kuwait airspace and Gulf conflict exposure", Modes: []string{"conflict", "transport"}, Roles: []string{"monitor", "risk"}, Prio: 89},
		{Label: "Oman", Path: "/loc/om", Kind: hmodels.WatchSubjectKindGeography, Query: "Oman transit routes, Salalah and Duqm logistics under Gulf conflict pressure", Modes: []string{"conflict", "maritime", "transport"}, Roles: []string{"monitor", "risk"}, Prio: 89},
		{Label: "Iran", Path: "/loc/ir", Kind: hmodels.WatchSubjectKindGeography, Query: "Iran strikes, retaliation and cascading Gulf exposure", Modes: []string{"conflict", "energy"}, Roles: []string{"monitor", "risk"}, Prio: 92},
		{Label: "China Energy Exposure", Path: "/loc/cn", Kind: hmodels.WatchSubjectKindGeography, Query: "China energy, LNG, shipping and industrial exposure to Gulf strikes, Hormuz disruption and UAE logistics stress", Modes: []string{"energy", "maritime", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 84},
		{Label: "Khalifa Port", Path: "/fac/ae/khalifa-port", Kind: hmodels.WatchSubjectKindFragment, Query: "Khalifa port strikes, disruption and downstream port rerouting", Modes: []string{"maritime", "transport", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 93},
		{Label: "Port of Fujairah", Path: "/fac/ae/port-of-fujairah", Kind: hmodels.WatchSubjectKindFragment, Query: "Port of Fujairah strikes, oil terminal disruption, tanker rerouting and downstream infrastructure impact", Modes: []string{"maritime", "energy", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 98},
		{Label: "Jebel Ali Port", Path: "/fac/ae/jebel-ali-port", Kind: hmodels.WatchSubjectKindFragment, Query: "Jebel Ali port disruption under Gulf strikes and rerouting pressure", Modes: []string{"maritime", "transport", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 94},
		{Label: "Jebel Ali Free Zone", Path: "/fac/ae/jebel-ali-free-zone", Kind: hmodels.WatchSubjectKindFragment, Query: "Jebel Ali logistics and free zone disruption under Gulf strikes", Modes: []string{"transport", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 92},
		{Label: "Dubai Airport", Path: "/fac/ae/dubai-airport", Kind: hmodels.WatchSubjectKindFragment, Query: "Dubai airport and airspace disruption from Gulf conflict", Modes: []string{"transport", "conflict"}, Roles: []string{"monitor", "risk"}, Prio: 91},
		{Label: "Dubai International Airport", Path: "/fac/ae/dubai-international-airport", Kind: hmodels.WatchSubjectKindFragment, Query: "Dubai International Airport strikes, airspace shutdowns and downstream logistics impact", Modes: []string{"transport", "conflict", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 93},
		{Label: "Habshan Gas Complex", Path: "/fac/ae/habshan-gas-complex", Kind: hmodels.WatchSubjectKindFragment, Query: "Habshan gas complex strikes, gas flow disruption and downstream desalination and power exposure", Modes: []string{"energy", "conflict", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 96},
		{Label: "Sohar Port", Path: "/fac/om/sohar-port", Kind: hmodels.WatchSubjectKindFragment, Query: "Sohar port congestion and rerouting under Gulf conflict spillover", Modes: []string{"maritime", "transport", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 88},
		{Label: "Ras Laffan Gas Complex", Path: "/fac/qa/ras-laffan-gas-complex", Kind: hmodels.WatchSubjectKindFragment, Query: "Ras Laffan LNG and helium disruption under Gulf conflict", Modes: []string{"energy", "supply_chain", "conflict"}, Roles: []string{"monitor", "risk"}, Prio: 96},
		{Label: "Ras Tanura Refinery", Path: "/fac/sa/ras-tanura-refinery", Kind: hmodels.WatchSubjectKindFragment, Query: "Ras Tanura export and refinery disruption under Gulf conflict", Modes: []string{"energy", "transport", "conflict"}, Roles: []string{"monitor", "risk"}, Prio: 95},
	}
}

func upsertSource(ctx context.Context, repo *corerepo.DataSourcesRepository, spec sourceSpec, apply bool) (string, error) {
	existing, err := repo.FindByCanonicalSource(ctx, "", spec.URL)
	if err != nil {
		existing = nil
	}
	now := time.Now().UTC()
	pollMinutes := spec.PollMinutes
	if pollMinutes <= 0 {
		pollMinutes = defaultPollMins
	}
	scope := coremodels.SourceScopeGlobalSystem
	processing := firstNonEmpty(spec.Processing, coremodels.SourceProcessingModeHeuristic)
	output := firstNonEmpty(spec.Output, coremodels.SourceOutputModeDocumentFeed)
	disableLLM := spec.DisableLLM
	cadence := &coremodels.SourceCadencePolicy{
		BaseIntervalMinutes:         intPtr(pollMinutes),
		MinIntervalMinutes:          intPtr(pollMinutes),
		MaxIntervalMinutes:          intPtr(360),
		EmptyBackoffMultiplier:      floatPtr(2),
		ErrorBackoffMultiplier:      floatPtr(2),
		FreshnessBoostWindowMinutes: intPtr(120),
		LeaseDurationMinutes:        intPtr(90),
	}
	ds := &coremodels.DataSource{
		ID:          buildSourceID(spec.Key),
		DisplayName: spec.DisplayName,
		Kind:        strings.TrimSpace(spec.Kind),
		Status:      firstNonEmpty(spec.Status, "active"),
		OwnerUserID: watchBootstrap,
		CreatedAt:   now,
		UpdatedAt:   now,
		FirstSeen:   now,
		LastSeen:    now,
		NextPollAt:  &now,
		TrustLevel:  0.82,
		SourceGeoFacets: []string{
			"/loc/ae",
			"/loc/sa",
			"/loc/qa",
			"/loc/bh",
			"/loc/kw",
			"/loc/om",
			"/loc/ir",
			"/loc/int/strait-of-hormuz",
		},
		SourceCategoryPaths: []string{"/source/conflict/gulf"},
		Details: coremodels.SourceDetails{
			Category:                 strPtr(spec.Category),
			HorizonMonitorCategory:   strPtr(spec.Category),
			HorizonMonitorFeedType:   strPtr(spec.FeedType),
			HorizonMonitorSignalType: strPtr(spec.SignalType),
			PollIntervalMinutes:      intPtr(pollMinutes),
			SourceScope:              &scope,
			ProcessingMode:           &processing,
			OutputMode:               &output,
			CadencePolicy:            cadence,
			DisableLLMProcessing:     &disableLLM,
		},
	}
	if ds.Kind == coremodels.SourceKindRSS {
		ds.Details.RSSURL = strPtr(spec.URL)
	} else {
		mode := coremodels.SourceOutputModeOntologyFact
		ds.Details.URI = strPtr(spec.URL)
		ds.Details.HorizonMonitorIngestMode = &mode
		ds.Details.OutputMode = &mode
	}
	status := "created"
	if existing != nil {
		ds.ID = existing.ID
		ds.CreatedAt = existing.CreatedAt
		status = "updated"
	}
	if !apply {
		return status, nil
	}
	if err := repo.Upsert(ctx, ds); err != nil {
		return "", err
	}
	return status, nil
}

func deactivateSource(ctx context.Context, repo *corerepo.DataSourcesRepository, id string, apply bool) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	ds, err := repo.Get(ctx, id)
	if err != nil || ds == nil {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(ds.Status), "inactive") {
		return nil
	}
	ds.Status = "inactive"
	ds.UpdatedAt = time.Now().UTC()
	if !apply {
		return nil
	}
	return repo.Upsert(ctx, ds)
}

func upsertWatch(
	ctx context.Context,
	subjectsRepo *hrepo.WatchSubjectsRepository,
	compiledRepo *hrepo.CompiledWatchSubjectsRepository,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	spec watchSpec,
	apply bool,
) (*hmodels.WatchSubject, string, error) {
	subjectID := hrepo.BuildWatchSubjectID(globalTenant, "", "", watchPackID, watchBootstrap, spec.Kind, spec.Path)
	subject, _ := subjectsRepo.Get(ctx, subjectID)
	status := "created"
	now := time.Now().UTC()
	if subject == nil {
		subject = &hmodels.WatchSubject{
			ID:                  subjectID,
			TenantID:            globalTenant,
			PackID:              watchPackID,
			UserID:              watchBootstrap,
			SubjectKind:         spec.Kind,
			AnchorCanonicalPath: spec.Path,
			DisplayLabel:        spec.Label,
			Status:              hmodels.WatchSubjectStatusActive,
			SourceMode:          hmodels.WatchSubjectSourceHybrid,
			RuntimeMode:         hmodels.WatchSubjectRuntimeCombined,
			CreatedAt:           now,
		}
	} else {
		status = "updated"
	}
	subject.InterestTitle = spec.Label
	subject.InterestQuery = spec.Query
	subject.DomainModes = spec.Modes
	subject.Roles = spec.Roles
	subject.Priority = spec.Prio
	subject.Metadata = map[string]any{
		"focusClass": "gulf_crisis",
		"autoSeeded": true,
	}
	if frag, err := fragmentsRepo.GetByCanonicalPath(ctx, spec.Path); err == nil && frag != nil {
		subject.AnchorFragmentID = strings.TrimSpace(frag.ID)
	}
	if !apply {
		return subject, status, nil
	}
	if err := subjectsRepo.Upsert(ctx, subject); err != nil {
		return nil, "", err
	}
	if _, err := analytical.PersistCompiledWatchSubject(ctx, compiledRepo, nil, subject, nil, []string{spec.Path}); err != nil {
		return nil, "", err
	}
	return subject, status, nil
}

func googleNewsQueryRSS(query string) string {
	v := strings.TrimSpace(query)
	if v == "" {
		return ""
	}
	return "https://news.google.com/rss/search?q=" + neturl.QueryEscape(v) + "&hl=en-US&gl=US&ceid=US:en"
}

func buildSourceID(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key)))
	return "src_gulf_" + hex.EncodeToString(sum[:8])
}

func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func ensureRuntime(
	ctx context.Context,
	scopeConfigsRepo *hrepo.ScopeAgentConfigsRepository,
	watchdogRepo *hrepo.WatchdogConfigsRepository,
	subject *hmodels.WatchSubject,
	apply bool,
) (string, string, error) {
	if subject == nil {
		return "", "", nil
	}
	scopeChanged, err := ensureScopeConfig(ctx, scopeConfigsRepo, subject, apply)
	if err != nil {
		return "", "", err
	}
	watchdogChanged, err := ensureWatchdog(ctx, watchdogRepo, subject, apply)
	if err != nil {
		return "", "", err
	}
	return scopeChanged, watchdogChanged, nil
}

func ensureScopeConfig(
	ctx context.Context,
	scopeConfigsRepo *hrepo.ScopeAgentConfigsRepository,
	subject *hmodels.WatchSubject,
	apply bool,
) (string, error) {
	if scopeConfigsRepo == nil || subject == nil || strings.TrimSpace(subject.ID) == "" {
		return "", nil
	}
	existing, _ := scopeConfigsRepo.ListActiveBySubjectID(ctx, strings.TrimSpace(subject.TenantID), strings.TrimSpace(subject.ID))
	status := "created"
	if len(existing) > 0 {
		status = "updated"
	}
	cfg := &hmodels.ScopeAgentConfig{
		TenantID:           strings.TrimSpace(subject.TenantID),
		PackID:             strings.TrimSpace(subject.PackID),
		SubjectID:          strings.TrimSpace(subject.ID),
		TargetFragmentID:   strings.TrimSpace(subject.AnchorFragmentID),
		Name:               fmt.Sprintf("%s watch subject", strings.TrimSpace(subject.DisplayLabel)),
		ScopeCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		ScopeType:          scopeTypeForSubject(subject.SubjectKind),
		DomainModes:        append([]string(nil), subject.DomainModes...),
		Roles:              normalizeRoles(subject.Roles),
		Priority:           normalizedPriority(subject.Priority),
		Status:             string(hmodels.ScopeAgentStatusRunning),
		ExpansionPolicy: map[string]any{
			"focusMode":        focusModeForSubject(subject),
			"watchUniverse":    true,
			"universeSizeHint": len(subject.UniversePaths),
			"bootstrap":        "gulf_crisis_runtime",
		},
		Schedule: map[string]any{
			"debounceSeconds":    180,
			"batchWindowSeconds": 180,
		},
		LLMPolicy: hmodels.LLMPolicy{
			PrimaryModel:         "gemini-3-flash-preview",
			EscalationModel:      "gemini-3-pro-preview",
			PreferredVertexModel: "vertex-gemini-3-flash-preview",
			FallbackModels:       []string{"gemini-3.1-flash-lite-preview", "gemini-2.5-flash", "gemini-2.5-pro"},
			UseSearchGrounding:   true,
			MaxBatchSize:         8,
			BatchWindowSeconds:   180,
		},
		Metadata: map[string]any{
			"watchSubjectId": subject.ID,
			"subjectKind":    string(subject.SubjectKind),
			"interestTitle":  strings.TrimSpace(subject.InterestTitle),
			"interestQuery":  strings.TrimSpace(subject.InterestQuery),
			"queryOverrides": compactStrings(append([]string(nil), subject.QueryOverrides...)),
			"focusClass":     "gulf_crisis",
			"bootstrap":      "horizon_seed_gulf_crisis_runtime",
		},
	}
	if len(subject.PageFraming) > 0 {
		cfg.Metadata["pageFraming"] = subject.PageFraming
	}
	if !apply {
		return status, nil
	}
	if err := scopeConfigsRepo.Upsert(ctx, cfg); err != nil {
		return "", err
	}
	return status, nil
}

func ensureWatchdog(
	ctx context.Context,
	watchdogRepo *hrepo.WatchdogConfigsRepository,
	subject *hmodels.WatchSubject,
	apply bool,
) (string, error) {
	if watchdogRepo == nil || subject == nil || strings.TrimSpace(subject.AnchorFragmentID) == "" {
		return "", nil
	}
	now := time.Now().UTC()
	lastScanAt := now.Add(-2 * time.Hour)
	nextScanAt := now.Add(-1 * time.Minute)
	cfg, err := watchdogRepo.GetByTargetFragmentID(ctx, strings.TrimSpace(subject.TenantID), strings.TrimSpace(subject.AnchorFragmentID))
	status := "created"
	if err == nil && cfg != nil {
		status = "updated"
	} else {
		cfg = &hmodels.WatchdogConfig{
			TenantID:         strings.TrimSpace(subject.TenantID),
			TargetFragmentID: strings.TrimSpace(subject.AnchorFragmentID),
		}
	}
	cfg.SubjectID = strings.TrimSpace(subject.ID)
	cfg.InterestTitle = strings.TrimSpace(subject.InterestTitle)
	cfg.InterestQuery = strings.TrimSpace(subject.InterestQuery)
	cfg.QueryOverrides = compactStrings(append([]string(nil), subject.QueryOverrides...))
	cfg.PageFraming = subject.PageFraming
	cfg.ScanInterval = "PT1H"
	cfg.TriggerThreshold = 0.10
	cfg.Status = string(hmodels.WatchdogStatusRunning)
	cfg.LastScanAt = &lastScanAt
	cfg.NextScanAt = &nextScanAt
	if !apply {
		return status, nil
	}
	if status == "created" {
		_, err = watchdogRepo.Create(ctx, cfg)
	} else {
		err = watchdogRepo.Update(ctx, cfg)
	}
	if err != nil {
		return "", err
	}
	return status, nil
}

func scopeTypeForSubject(kind hmodels.WatchSubjectKind) string {
	switch kind {
	case hmodels.WatchSubjectKindLensUniverse:
		return "lens_universe"
	case hmodels.WatchSubjectKindPathOverlay:
		return "path_overlay"
	case hmodels.WatchSubjectKindFragment:
		return "fragment"
	case hmodels.WatchSubjectKindAffectedZone:
		return "affected_zone"
	case hmodels.WatchSubjectKindMarket:
		return "market"
	case hmodels.WatchSubjectKindGeography:
		return "geography"
	default:
		return "watch_subject"
	}
}

func focusModeForSubject(subject *hmodels.WatchSubject) string {
	if subject == nil {
		return "watch_subject"
	}
	switch subject.SubjectKind {
	case hmodels.WatchSubjectKindLensUniverse:
		return "lens_universe"
	case hmodels.WatchSubjectKindPathOverlay:
		return "artifact_overlay"
	case hmodels.WatchSubjectKindMarket:
		return "macro_market"
	case hmodels.WatchSubjectKindGeography:
		return "macro_country"
	default:
		return "watch_subject"
	}
}

func normalizeRoles(values []string) []string {
	if len(values) == 0 {
		return []string{"monitor"}
	}
	return compactStrings(values)
}

func normalizedPriority(v int) int {
	if v <= 0 {
		return 50
	}
	return v
}

func compactStrings(values []string) []string {
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
	return out
}
