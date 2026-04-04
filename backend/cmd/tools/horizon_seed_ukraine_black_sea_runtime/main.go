package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
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
	ukrGlobalTenant    = "global"
	ukrWatchPackID     = "ukraine-black-sea-focus-pack-v1"
	ukrWatchBootstrap  = "horizon-ukraine-black-sea-bootstrap@system.local"
	ukrDefaultPollMins = 120
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
	seedSources := flag.Bool("sources", true, "Seed focused Ukraine/Black Sea sources")
	seedWatch := flag.Bool("watch", true, "Seed focused Ukraine/Black Sea watch subjects")
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
	}

	createdWatch := 0
	updatedWatch := 0
	if *seedWatch {
		if *apply {
			if err := watchPacksRepo.Upsert(ctx, &hmodels.WatchPack{
				ID:          ukrWatchPackID,
				TenantID:    ukrGlobalTenant,
				Name:        "Ukraine Black Sea Focus Pack v1",
				Description: "Focused watch coverage for Ukraine, south Russia, Black Sea ports, missile and drone strikes, and downstream logistics and industrial exposure.",
				Status:      string(hmodels.WatchTargetStatusActive),
			}); err != nil {
				log.Fatalf("watch pack upsert failed: %v", err)
			}
		}
		for _, spec := range buildWatchSpecs() {
			changed, err := upsertWatch(ctx, watchSubjectsRepo, compiledWatchRepo, fragmentsRepo, spec, *apply)
			if err != nil {
				log.Fatalf("seed watch %s failed: %v", spec.Path, err)
			}
			if changed == "created" {
				createdWatch++
			}
			if changed == "updated" {
				updatedWatch++
			}
		}
	}

	log.Printf("ukraine runtime seed complete: sources(created=%d updated=%d) watch(created=%d updated=%d) apply=%v",
		createdSources, updatedSources, createdWatch, updatedWatch, *apply)
}

func buildSourceSpecs() []sourceSpec {
	return []sourceSpec{
		{
			Key:         "ukraine-canvas-black-sea-strike-geography",
			DisplayName: "Ukraine Black Sea Canvas: Strike Geography",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`(Ukraine OR Russia OR Crimea OR "Black Sea") (drone OR missile OR strike OR attack OR blast OR interception) (Odesa OR Odessa OR Chornomorsk OR Pivdennyi OR Izmail OR Reni OR Mykolaiv OR Nikolaev OR Kherson OR Sevastopol OR Kerch OR Novorossiysk OR Tuapse OR Taganrog OR Rostov OR Krasnodar)`),
			Category:    "conflict",
			FeedType:    "ukraine_black_sea_strike_geography",
			SignalType:  "risk:regional_strike_geography",
			PollMinutes: ukrDefaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			Status:      "active",
		},
		{
			Key:         "ukraine-canvas-black-sea-ports-and-grain",
			DisplayName: "Ukraine Black Sea Canvas: Ports and Grain Corridor",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`("Black Sea" OR Odesa OR Odessa OR Chornomorsk OR Pivdennyi OR Izmail OR Reni OR Novorossiysk) (port OR terminal OR grain OR export OR shipping OR corridor OR Danube) (attack OR disruption OR closure OR drone OR missile OR security)`),
			Category:    "maritime",
			FeedType:    "ukraine_black_sea_ports_grain",
			SignalType:  "risk:logistics_disruption",
			PollMinutes: ukrDefaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			Status:      "active",
		},
		{
			Key:         "ukraine-canvas-south-russia-refinery-port-stress",
			DisplayName: "Ukraine Black Sea Canvas: South Russia Refinery and Port Stress",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`(Novorossiysk OR Tuapse OR Taganrog OR Rostov OR Krasnodar OR Crimea OR Sevastopol OR Kerch) (refinery OR oil terminal OR port OR customs OR rail OR fuel depot OR logistics) (drone OR missile OR strike OR fire OR disruption OR closure)`),
			Category:    "critical_infrastructure",
			FeedType:    "ukraine_south_russia_refinery_port_stress",
			SignalType:  "risk:critical_infrastructure_disruption",
			PollMinutes: ukrDefaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			Status:      "active",
		},
		{
			Key:         "ukraine-canvas-crimea-and-kerch-military-logistics",
			DisplayName: "Ukraine Black Sea Canvas: Crimea and Kerch Military Logistics",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`(Crimea OR Sevastopol OR Kerch OR "Kerch bridge") (drone OR missile OR strike OR blast OR military OR air defense OR ferry OR bridge) (logistics OR port OR naval OR transport OR disruption)`),
			Category:    "conflict",
			FeedType:    "ukraine_crimea_kerch_military_logistics",
			SignalType:  "risk:military_logistics_disruption",
			PollMinutes: ukrDefaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			Status:      "active",
		},
		{
			Key:         "ukraine-canvas-odesa-industrial-inputs",
			DisplayName: "Ukraine Black Sea Canvas: Odesa Industrial Inputs",
			Kind:        coremodels.SourceKindRSS,
			URL:         googleNewsQueryRSS(`(Odesa OR Odessa OR Mykolaiv OR Kherson OR Izmail OR Reni) (port OR neon OR ammonia OR grain OR container OR industrial OR logistics) (attack OR strike OR disruption OR export OR shutdown OR damage)`),
			Category:    "supply_chain",
			FeedType:    "ukraine_odesa_industrial_inputs",
			SignalType:  "risk:supply_chain_disruption",
			PollMinutes: ukrDefaultPollMins,
			Processing:  coremodels.SourceProcessingModeCanvasLLM,
			Output:      coremodels.SourceOutputModeOntologyFact,
			Status:      "active",
		},
	}
}

func buildWatchSpecs() []watchSpec {
	return []watchSpec{
		{Label: "Ukraine", Path: "/loc/ua", Kind: hmodels.WatchSubjectKindGeography, Query: "Ukraine south and Black Sea strikes, port disruption, drone and missile attacks, industrial and export exposure", Modes: []string{"conflict", "maritime", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 97},
		{Label: "Russian Federation", Path: "/loc/ru", Kind: hmodels.WatchSubjectKindGeography, Query: "South Russia strikes, refinery and port disruption, Black Sea logistics and export stress", Modes: []string{"conflict", "energy", "transport"}, Roles: []string{"monitor", "risk"}, Prio: 92},
		{Label: "Port of Odesa", Path: "/fac/ua/port-of-odessa", Kind: hmodels.WatchSubjectKindFragment, Query: "Port of Odesa strikes, shipping disruption, grain exports and Black Sea cascade effects", Modes: []string{"maritime", "transport", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 98},
		{Label: "Odesa Neon Plant", Path: "/fac/ua/odesa-neon-plant", Kind: hmodels.WatchSubjectKindFragment, Query: "Odesa neon and industrial gas disruption under strikes on southern Ukraine logistics and industry", Modes: []string{"conflict", "industry", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 91},
		{Label: "Novorossiysk Seaport", Path: "/fac/ru/novorossiysk-seaport", Kind: hmodels.WatchSubjectKindFragment, Query: "Novorossiysk seaport disruption, Black Sea rerouting, customs and refinery logistics exposure", Modes: []string{"maritime", "energy", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 97},
		{Label: "Novorossiysk Seaport Terminal", Path: "/fac/ru/novorossiysk-seaport-terminal", Kind: hmodels.WatchSubjectKindFragment, Query: "Novorossiysk terminal strikes and downstream logistics, customs and oilfield equipment impacts", Modes: []string{"maritime", "transport", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 94},
		{Label: "Rostov Customs Terminal", Path: "/fac/ru/rostov-customs-terminal", Kind: hmodels.WatchSubjectKindFragment, Query: "Rostov customs and logistics disruption connected to Black Sea and south Russia strikes", Modes: []string{"transport", "supply_chain", "conflict"}, Roles: []string{"monitor", "risk"}, Prio: 90},
		{Label: "China Energy Exposure", Path: "/loc/cn", Kind: hmodels.WatchSubjectKindGeography, Query: "China exposure to Black Sea and south Russia port, grain, oil and shipping disruptions from Ukraine conflict", Modes: []string{"energy", "maritime", "supply_chain"}, Roles: []string{"monitor", "risk"}, Prio: 82},
		{Label: "Iran", Path: "/loc/ir", Kind: hmodels.WatchSubjectKindGeography, Query: "Iran exposure and logistics linkages to Black Sea and south Russia disruptions, port and corridor stress", Modes: []string{"maritime", "supply_chain", "conflict"}, Roles: []string{"monitor", "risk"}, Prio: 78},
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
		pollMinutes = ukrDefaultPollMins
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
		OwnerUserID: ukrWatchBootstrap,
		CreatedAt:   now,
		UpdatedAt:   now,
		FirstSeen:   now,
		LastSeen:    now,
		TrustLevel:  0.82,
		SourceGeoFacets: []string{
			"/loc/ua",
			"/loc/ru",
			"/fac/ua/port-of-odessa",
			"/fac/ru/novorossiysk-seaport",
			"/fac/ru/rostov-customs-terminal",
		},
		SourceCategoryPaths: []string{"/source/conflict/ukraine-black-sea"},
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

func upsertWatch(
	ctx context.Context,
	subjectsRepo *hrepo.WatchSubjectsRepository,
	compiledRepo *hrepo.CompiledWatchSubjectsRepository,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	spec watchSpec,
	apply bool,
) (string, error) {
	subjectID := hrepo.BuildWatchSubjectID(ukrGlobalTenant, "", "", ukrWatchPackID, ukrWatchBootstrap, spec.Kind, spec.Path)
	subject, _ := subjectsRepo.Get(ctx, subjectID)
	status := "created"
	now := time.Now().UTC()
	if subject == nil {
		subject = &hmodels.WatchSubject{
			ID:                  subjectID,
			TenantID:            ukrGlobalTenant,
			PackID:              ukrWatchPackID,
			UserID:              ukrWatchBootstrap,
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
		"focusClass": "ukraine_black_sea",
		"autoSeeded": true,
	}
	if frag, err := fragmentsRepo.GetByCanonicalPath(ctx, spec.Path); err == nil && frag != nil {
		subject.AnchorFragmentID = strings.TrimSpace(frag.ID)
	}
	if !apply {
		return status, nil
	}
	if err := subjectsRepo.Upsert(ctx, subject); err != nil {
		return "", err
	}
	if _, err := analytical.PersistCompiledWatchSubject(ctx, compiledRepo, nil, subject, nil, []string{spec.Path}); err != nil {
		return "", err
	}
	return status, nil
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
	return "src_ukr_" + hex.EncodeToString(sum[:8])
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
