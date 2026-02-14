package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type horizonMonitorExport struct {
	ExportedAt        string                 `json:"exportedAt"`
	Metadata          map[string]interface{} `json:"metadata"`
	Hotspots          []wmHotspot            `json:"hotspots"`
	Waterways         []wmWaterway           `json:"waterways"`
	AptGroups         []wmAPTGroup           `json:"aptGroups"`
	ConflictZones     []wmConflictZone       `json:"conflictZones"`
	MilitaryBases     []wmMilitaryBase       `json:"militaryBases"`
	UnderseaCables    []wmUnderseaCable      `json:"underseaCables"`
	NuclearFacilities []wmNuclearFacility    `json:"nuclearFacilities"`
	EconomicCenters   []wmEconomicCenter     `json:"economicCenters"`
	Spaceports        []wmSpaceport          `json:"spaceports"`
	CriticalMinerals  []wmCriticalMineral    `json:"criticalMinerals"`
	Pipelines         []wmPipeline           `json:"pipelines"`
	Ports             []wmPort               `json:"ports"`
	IntelSources      []wmIntelSource        `json:"intelSources"`
}

type wmHotspot struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	Location    string   `json:"location"`
	Keywords    []string `json:"keywords"`
	Description string   `json:"description"`
}

type wmWaterway struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Description string  `json:"description"`
}

type wmAPTGroup struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	AKA     string  `json:"aka"`
	Sponsor string  `json:"sponsor"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type wmConflictZone struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Center      [2]float64  `json:"center"`
	Parties     []string    `json:"parties"`
	Description string      `json:"description"`
	Coords      [][]float64 `json:"coords"`
}

type wmMilitaryBase struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Type    string  `json:"type"`
	Country string  `json:"country"`
	Status  string  `json:"status"`
}

type wmLandingPoint struct {
	Country string  `json:"country"`
	City    string  `json:"city"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type wmUnderseaCable struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Points        [][]float64      `json:"points"`
	LandingPoints []wmLandingPoint `json:"landingPoints"`
	CapacityTbps  float64          `json:"capacityTbps"`
}

type wmNuclearFacility struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Operator string  `json:"operator"`
}

type wmEconomicCenter struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Country string  `json:"country"`
}

type wmSpaceport struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Country  string  `json:"country"`
	Operator string  `json:"operator"`
	Status   string  `json:"status"`
}

type wmCriticalMineral struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	Mineral      string  `json:"mineral"`
	Country      string  `json:"country"`
	Operator     string  `json:"operator"`
	Status       string  `json:"status"`
	Significance string  `json:"significance"`
}

type wmPipeline struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Status    string      `json:"status"`
	Points    [][]float64 `json:"points"`
	Capacity  string      `json:"capacity"`
	Operator  string      `json:"operator"`
	Countries []string    `json:"countries"`
}

type wmPort struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Country string  `json:"country"`
	Type    string  `json:"type"`
	Note    string  `json:"note"`
}

type wmIntelSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

const (
	wmIngestModeDocumentFeed = "document_feed"
	wmIngestModeOntologyFact = "ontology_fact"
	wmIngestModeQueryOnly    = "query_only"
)

type wmSourceSpec struct {
	DisplayName  string
	URL          string
	Kind         string
	Status       string
	Category     string
	FeedType     string
	IngestMode   string
	SignalType   string
	RequiresAuth bool
	PollMinutes  int
}

type importStats struct {
	FragmentsCreated int
	FragmentsUpdated int
	GeoLinksCreated  int
	EdgesCreated     int
	SourcesCreated   int
	SourcesUpdated   int
	SourceLinks      int
}

type importer struct {
	apply        bool
	sourceKind   string
	sourceStatus string
	ownerUserID  string
	tenantID     string
	projectID    string

	fragments *hrepo.HorizonFragmentsRepository
	edges     *hrepo.DependencyEdgesRepository
	dsRepo    *corerepo.DataSourcesRepository
	spRepo    *corerepo.SourceProjectsRepository
	taxRepo   *corerepo.HorizonTaxonomyRepository

	stats      importStats
	pathToID   map[string]string
	idToPath   map[string]string
	countryMap map[string]string
}

func newImporter(
	apply bool,
	sourceKind string,
	sourceStatus string,
	ownerUserID string,
	tenantID string,
	projectID string,
	fragments *hrepo.HorizonFragmentsRepository,
	edges *hrepo.DependencyEdgesRepository,
	dsRepo *corerepo.DataSourcesRepository,
	spRepo *corerepo.SourceProjectsRepository,
	taxRepo *corerepo.HorizonTaxonomyRepository,
) *importer {
	return &importer{
		apply:        apply,
		sourceKind:   strings.TrimSpace(sourceKind),
		sourceStatus: strings.TrimSpace(sourceStatus),
		ownerUserID:  strings.TrimSpace(ownerUserID),
		tenantID:     strings.TrimSpace(tenantID),
		projectID:    strings.TrimSpace(projectID),
		fragments:    fragments,
		edges:        edges,
		dsRepo:       dsRepo,
		spRepo:       spRepo,
		taxRepo:      taxRepo,
		pathToID:     make(map[string]string),
		idToPath:     make(map[string]string),
		countryMap: map[string]string{
			"usa":            "us",
			"u.s.a":          "us",
			"u.s.":           "us",
			"uk":             "gb",
			"u.k.":           "gb",
			"uae":            "ae",
			"china (sar)":    "cn",
			"south korea":    "kr",
			"north korea":    "kp",
			"russia":         "ru",
			"iran":           "ir",
			"saudi arabia":   "sa",
			"ivory coast":    "ci",
			"czech republic": "cz",
			"hong kong":      "hk",
			"taiwan":         "tw",
			"myanmar":        "mm",
			"germany":        "de",
			"france":         "fr",
			"united states":  "us",
			"united kingdom": "gb",
		},
	}
}

func main() {
	var (
		projectID      = flag.String("project-id", "", "GCP project ID (required)")
		inputPath      = flag.String("input", "../temp-repos/worldmonitor/data/horizon/worldmonitor-export.json", "Path to HorizonMonitor export JSON")
		apply          = flag.Bool("apply", false, "Apply changes to Firestore")
		extendTaxonomy = flag.Bool("extend-taxonomy", true, "Extend horizon_taxonomy/v1 with HorizonMonitor event/signal vocab")
		importStatic   = flag.Bool("import-static", true, "Import static HorizonMonitor objects into horizon fragments")
		importSources  = flag.Bool("import-sources", true, "Import HorizonMonitor intel RSS sources into core_datasources")
		importAPISrc   = flag.Bool("import-api-sources", true, "Import HorizonMonitor API sources into core_datasources")
		apiBaseURL     = flag.String("api-base-url", "https://horizon-monitor.app", "Base URL for HorizonMonitor API sources (used when source URL is relative)")
		sourceKind     = flag.String("source-kind", "rss", "Source kind for imported feeds: rss|horizon_monitor_feed")
		tenantID       = flag.String("tenant-id", "", "Tenant ID for source-project links (optional)")
		projectLinkID  = flag.String("project-link-id", "", "Project ID for rag_source_projects links (optional)")
		ownerUserID    = flag.String("owner-user-id", "horizon-monitor-import", "Owner user ID for created DataSource entries")
		sourceStatus   = flag.String("source-status", "active", "Status for created sources (active|paused|completed)")
		ragBucket      = flag.String("rag-bucket", "", "Optional: GCS bucket for immediate fragment export")
		vertexSync     = flag.Bool("vertex-sync", true, "Enable Vertex AI Search sync for imported fragments (requires -rag-bucket and apply mode)")
		vertexStoreID  = flag.String("vertex-store-id", "", "Vertex AI Search datastore ID (fallback: env VERTEX_SEARCH_HORIZON_DATA_STORE_ID / VERTEX_SEARCH_DATA_STORE_ID)")
		vertexLoc      = flag.String("vertex-location", "", "Vertex AI Search location (fallback: env VERTEX_SEARCH_LOCATION, default: global)")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("-project-id is required")
	}
	if *sourceKind != "rss" && *sourceKind != "horizon_monitor_feed" {
		log.Fatalf("unsupported -source-kind: %s (allowed: rss, horizon_monitor_feed)", *sourceKind)
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	var vertexBatcher *hrepo.VertexSyncBatcher
	if b := strings.TrimSpace(*ragBucket); b != "" {
		gcsClient, gerr := gcp.NewGCSClient(ctx, strings.TrimSpace(*projectID))
		if gerr != nil {
			log.Fatalf("failed to create gcs client: %v", gerr)
		}
		fragmentsRepo = fragmentsRepo.WithStorage(gcsClient, b)
		if *apply && *vertexSync {
			storeID := strings.TrimSpace(*vertexStoreID)
			if storeID == "" {
				storeID = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
			}
			if storeID == "" {
				storeID = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"))
			}
			if storeID == "" {
				log.Fatalf("vertex sync enabled but datastore id is empty; pass -vertex-store-id or set VERTEX_SEARCH_HORIZON_DATA_STORE_ID/VERTEX_SEARCH_DATA_STORE_ID")
			}
			location := strings.TrimSpace(*vertexLoc)
			if location == "" {
				location = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
			}
			if location == "" {
				location = "global"
			}

			vertexClient := ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), location, storeID, "")
			if !vertexClient.IsConfigured() {
				log.Fatalf("vertex sync enabled but vertex client is not configured (store=%s location=%s)", storeID, location)
			}

			vertexBatcher = hrepo.NewVertexSyncBatcher(
				func(ctx context.Context, uris []string) (string, error) {
					return vertexClient.ImportDocumentsFromURIs(ctx, uris, "INCREMENTAL", "document")
				},
				hrepo.DefaultVertexSyncBatcherConfig(),
			)
			fragmentsRepo = fragmentsRepo.WithVertexSyncBatcher(vertexBatcher)
		}
	}
	defer func() {
		if vertexBatcher != nil {
			vertexBatcher.Close()
			batches, docs, errs, pending := vertexBatcher.Stats()
			log.Printf("vertex sync stats: batches=%d documents=%d errors=%d pending=%d", batches, docs, errs, pending)
		}
	}()
	edgesRepo := hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(fragmentsRepo)
	dsRepo := corerepo.NewDataSourcesRepository(db)
	spRepo := corerepo.NewSourceProjectsRepository(db)
	taxRepo := corerepo.NewHorizonTaxonomyRepository(db)

	var payload *horizonMonitorExport
	if *importStatic || *importSources {
		payload, err = loadExport(*inputPath)
		if err != nil {
			log.Fatalf("failed to load export file: %v", err)
		}
	}

	imp := newImporter(
		*apply,
		*sourceKind,
		*sourceStatus,
		*ownerUserID,
		*tenantID,
		*projectLinkID,
		fragmentsRepo,
		edgesRepo,
		dsRepo,
		spRepo,
		taxRepo,
	)

	if err := imp.ensureHorizonMonitorTaxonomy(ctx, *extendTaxonomy); err != nil {
		log.Fatalf("failed to extend taxonomy: %v", err)
	}

	if *importStatic {
		if payload == nil {
			log.Fatalf("internal error: import-static requested but export payload is nil")
		}
		if err := imp.importStaticObjects(ctx, payload); err != nil {
			log.Fatalf("failed to import static objects: %v", err)
		}
	}
	if *importSources {
		if payload == nil {
			log.Fatalf("internal error: import-sources requested but export payload is nil")
		}
		if err := imp.importIntelSources(ctx, payload.IntelSources); err != nil {
			log.Fatalf("failed to import intel sources: %v", err)
		}
	}
	if *importAPISrc {
		if err := imp.importHorizonMonitorAPISources(ctx, strings.TrimSpace(*apiBaseURL)); err != nil {
			log.Fatalf("failed to import horizon monitor API sources: %v", err)
		}
	}

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	log.Printf("[%s] horizon monitor import finished: fragmentsCreated=%d fragmentsUpdated=%d geoLinksCreated=%d edgesCreated=%d sourcesCreated=%d sourcesUpdated=%d sourceLinks=%d",
		mode,
		imp.stats.FragmentsCreated,
		imp.stats.FragmentsUpdated,
		imp.stats.GeoLinksCreated,
		imp.stats.EdgesCreated,
		imp.stats.SourcesCreated,
		imp.stats.SourcesUpdated,
		imp.stats.SourceLinks,
	)
}

func loadExport(in string) (*horizonMonitorExport, error) {
	paths := []string{strings.TrimSpace(in)}
	if !filepath.IsAbs(strings.TrimSpace(in)) {
		paths = append(paths, filepath.Clean(filepath.Join("..", strings.TrimSpace(in))))
	}
	var raw []byte
	var err error
	for _, p := range paths {
		raw, err = os.ReadFile(p)
		if err == nil {
			var payload horizonMonitorExport
			if uerr := json.Unmarshal(raw, &payload); uerr != nil {
				return nil, uerr
			}
			return &payload, nil
		}
	}
	return nil, err
}

func (i *importer) importStaticObjects(ctx context.Context, p *horizonMonitorExport) error {
	for _, v := range p.Hotspots {
		if strings.TrimSpace(v.Name) == "" {
			continue
		}
		cp := taxonomy.JoinPath("event", "hotspot-"+v.Name)
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: cp,
			Type:          hmodels.FragmentTypeEvent,
			Label:         v.Name,
			Description:   v.Description,
			SourceID:      v.ID,
			Tags:          append([]string{"horizon-monitor", "hotspot"}, normalizeTags(v.Keywords)...),
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Location,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "hotspot",
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindHotspot,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				Keywords:       normalizeTags(v.Keywords),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Location, v.Lat, v.Lon, "hotspot.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.AptGroups {
		if strings.TrimSpace(v.Name) == "" {
			continue
		}
		cp := taxonomy.OrgCanonicalPath("int", v.Name)
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: cp,
			Type:          hmodels.FragmentTypeOrganization,
			Label:         v.Name,
			Description:   strings.TrimSpace(v.AKA),
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "apt-group", "cyber"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Sponsor,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "apt_group",
				"aka":                strings.TrimSpace(v.AKA),
				"sponsor":            strings.TrimSpace(v.Sponsor),
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindAPTGroup,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				AKA:            strings.TrimSpace(v.AKA),
				Sponsor:        strings.TrimSpace(v.Sponsor),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Sponsor, v.Lat, v.Lon, "apt.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.ConflictZones {
		if strings.TrimSpace(v.Name) == "" {
			continue
		}
		lat, lon := fromCenter(v.Center)
		cp := taxonomy.GeoCanonicalPath("global", "na", "conflict-"+v.Name)
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: cp,
			Type:          hmodels.FragmentTypeGeoRegion,
			Label:         v.Name,
			Description:   v.Description,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "conflict-zone"},
			Lat:           lat,
			Lon:           lon,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "conflict_zone",
				"parties":            v.Parties,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindConflictZone,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(lat, lon),
				Parties:        uniqueStrings(v.Parties),
				Polygon:        wmRoute(v.Coords),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, "", lat, lon, "conflict.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.Waterways {
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("chokepoint", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			Description:   v.Description,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "waterway", "chokepoint"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "waterway",
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindWaterway,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, "", v.Lat, v.Lon, "waterway.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.MilitaryBases {
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("military-base", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			Description:   strings.TrimSpace(v.Status),
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "military-base"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "military_base",
				"baseType":           v.Type,
				"status":             v.Status,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindMilitaryBase,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				FacilityType:   strings.TrimSpace(v.Type),
				Status:         strings.TrimSpace(v.Status),
				Countries:      uniqueStrings([]string{strings.TrimSpace(v.Country)}),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Country, v.Lat, v.Lon, "military-base.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.UnderseaCables {
		lat, lon := firstPoint(v.Points)
		country := ""
		if len(v.LandingPoints) > 0 {
			country = firstNonEmpty(v.LandingPoints[0].Country, v.LandingPoints[0].City)
		}
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("undersea-cable", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "undersea-cable"},
			Lat:           lat,
			Lon:           lon,
			Country:       country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "undersea_cable",
				"capacityTbps":       v.CapacityTbps,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindUnderseaCable,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(lat, lon),
				Route:          wmRoute(v.Points),
				LandingPoints:  wmLanding(v.LandingPoints),
				CapacityTbps:   floatPtr(v.CapacityTbps),
				Countries:      wmLandingCountries(v.LandingPoints),
			},
		})
		if err != nil {
			return err
		}
		for _, lp := range v.LandingPoints {
			if err := i.ensureGeoLink(ctx, id, lp.Country, lp.Lat, lp.Lon, "undersea-cable.landing"); err != nil {
				return err
			}
		}
	}

	for _, v := range p.NuclearFacilities {
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("nuclear", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "nuclear-facility"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Operator,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "nuclear_facility",
				"facilityType":       v.Type,
				"status":             v.Status,
				"operator":           v.Operator,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindNuclearFacility,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				FacilityType:   strings.TrimSpace(v.Type),
				Status:         strings.TrimSpace(v.Status),
				Operator:       strings.TrimSpace(v.Operator),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Operator, v.Lat, v.Lon, "nuclear.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.EconomicCenters {
		geo := i.geoToken(v.Country)
		if geo == "" {
			geo = "global"
		}
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.MarketCanonicalPath(geo, v.Name),
			Type:          hmodels.FragmentTypeMarket,
			Label:         v.Name,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "economic-center"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "economic_center",
				"marketType":         v.Type,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindEconomicCenter,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				MarketType:     strings.TrimSpace(v.Type),
				Countries:      uniqueStrings([]string{strings.TrimSpace(v.Country)}),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Country, v.Lat, v.Lon, "economic-center.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.Spaceports {
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("spaceport", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "spaceport"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "spaceport",
				"operator":           v.Operator,
				"status":             v.Status,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindSpaceport,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				Operator:       strings.TrimSpace(v.Operator),
				Status:         strings.TrimSpace(v.Status),
				Countries:      uniqueStrings([]string{strings.TrimSpace(v.Country)}),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Country, v.Lat, v.Lon, "spaceport.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.CriticalMinerals {
		mineral := taxonomy.Slugify(v.Mineral)
		if mineral == "" {
			mineral = "other"
		}
		cp := taxonomy.JoinPath("deposit", mineral, v.Name)
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: cp,
			Type:          hmodels.FragmentTypeDeposit,
			Label:         v.Name,
			Description:   v.Significance,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "critical-mineral"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "critical_mineral_project",
				"mineral":            v.Mineral,
				"operator":           v.Operator,
				"status":             v.Status,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindCriticalMineral,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				Mineral:        strings.TrimSpace(v.Mineral),
				Operator:       strings.TrimSpace(v.Operator),
				Status:         strings.TrimSpace(v.Status),
				Significance:   strings.TrimSpace(v.Significance),
				Countries:      uniqueStrings([]string{strings.TrimSpace(v.Country)}),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Country, v.Lat, v.Lon, "critical-mineral.geo"); err != nil {
			return err
		}
	}

	for _, v := range p.Pipelines {
		lat, lon := firstPoint(v.Points)
		country := ""
		if len(v.Countries) > 0 {
			country = v.Countries[0]
		}
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("pipeline", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			Description:   v.Capacity,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "pipeline"},
			Lat:           lat,
			Lon:           lon,
			Country:       country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "pipeline",
				"pipelineType":       v.Type,
				"status":             v.Status,
				"capacity":           v.Capacity,
				"operator":           v.Operator,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindPipeline,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(lat, lon),
				Route:          wmRoute(v.Points),
				PipelineType:   strings.TrimSpace(v.Type),
				Status:         strings.TrimSpace(v.Status),
				Capacity:       strings.TrimSpace(v.Capacity),
				Operator:       strings.TrimSpace(v.Operator),
				Countries:      uniqueStrings(v.Countries),
			},
		})
		if err != nil {
			return err
		}
		for _, c := range v.Countries {
			if err := i.ensureGeoLink(ctx, id, c, lat, lon, "pipeline.geo"); err != nil {
				return err
			}
		}
	}

	for _, v := range p.Ports {
		id, err := i.upsertFragment(ctx, buildFragmentSpec{
			CanonicalPath: taxonomy.FacilityCanonicalPath("port", v.Name),
			Type:          hmodels.FragmentTypeFacility,
			Label:         v.Name,
			Description:   v.Note,
			SourceID:      v.ID,
			Tags:          []string{"horizon-monitor", "port"},
			Lat:           v.Lat,
			Lon:           v.Lon,
			Country:       v.Country,
			Qualifiers: map[string]interface{}{
				"horizonMonitorKind": "port",
				"portType":           v.Type,
			},
			HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
				ObjectKind:     hmodels.HorizonMonitorKindPort,
				SourceObjectID: strings.TrimSpace(v.ID),
				Coordinates:    wmCoord(v.Lat, v.Lon),
				PortType:       strings.TrimSpace(v.Type),
				Countries:      uniqueStrings([]string{strings.TrimSpace(v.Country)}),
			},
		})
		if err != nil {
			return err
		}
		if err := i.ensureGeoLink(ctx, id, v.Country, v.Lat, v.Lon, "port.geo"); err != nil {
			return err
		}
	}

	return nil
}

func (i *importer) importIntelSources(ctx context.Context, sources []wmIntelSource) error {
	if i.dsRepo == nil {
		return nil
	}
	specs := make([]wmSourceSpec, 0, len(sources))
	for _, src := range sources {
		sourceURL := strings.TrimSpace(src.URL)
		if sourceURL == "" {
			continue
		}
		specs = append(specs, wmSourceSpec{
			DisplayName: fmt.Sprintf("WM: %s", strings.TrimSpace(src.Name)),
			URL:         sourceURL,
			Kind:        i.sourceKind,
			Status:      firstNonEmpty(i.sourceStatus, "active"),
			Category:    strings.TrimSpace(src.Type),
			FeedType:    "intel_rss",
			IngestMode:  wmIngestModeDocumentFeed,
			PollMinutes: 30,
		})
	}
	return i.importSourceSpecs(ctx, specs)
}

func (i *importer) importHorizonMonitorAPISources(ctx context.Context, apiBaseURL string) error {
	if i.dsRepo == nil {
		return nil
	}
	specs := buildHorizonMonitorAPISourceSpecs(apiBaseURL)
	return i.importSourceSpecs(ctx, specs)
}

func buildHorizonMonitorAPISourceSpecs(apiBaseURL string) []wmSourceSpec {
	base := strings.TrimSpace(apiBaseURL)
	if base == "" {
		base = "https://horizon-monitor.app"
	}
	base = strings.TrimSuffix(base, "/")
	resolve := func(raw string) string {
		v := strings.TrimSpace(raw)
		if v == "" {
			return ""
		}
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return v
		}
		if strings.HasPrefix(v, "/") {
			return base + v
		}
		return base + "/" + v
	}

	gdeltDocPath := func(query string) string {
		params := neturl.Values{}
		params.Set("query", strings.TrimSpace(query))
		params.Set("maxrecords", "25")
		params.Set("timespan", "24h")
		return "/api/gdelt-doc?" + params.Encode()
	}

	yahooPath := func(symbol string) string {
		params := neturl.Values{}
		params.Set("symbol", strings.TrimSpace(symbol))
		return "/api/yahoo-finance?" + params.Encode()
	}

	finnhubPath := func(symbols []string) string {
		params := neturl.Values{}
		params.Set("symbols", strings.Join(symbols, ","))
		return "/api/finnhub?" + params.Encode()
	}

	fredPath := func(seriesID string) string {
		params := neturl.Values{}
		params.Set("series_id", strings.TrimSpace(seriesID))
		return "/api/fred-data?" + params.Encode()
	}

	worldbankPath := func(indicator string, years int) string {
		params := neturl.Values{}
		params.Set("indicator", strings.TrimSpace(indicator))
		params.Set("years", strconv.Itoa(years))
		return "/api/worldbank?" + params.Encode()
	}

	unhcrPath := func(year int, limit int, page int) string {
		params := neturl.Values{}
		params.Set("year", strconv.Itoa(year))
		params.Set("limit", strconv.Itoa(limit))
		params.Set("page", strconv.Itoa(page))
		return "/api/unhcr-population?" + params.Encode()
	}

	specs := []wmSourceSpec{
		{
			DisplayName: "Horizon API: GDELT Geo Protests",
			URL:         resolve("/api/gdelt-geo?query=protest&format=geojson&maxrecords=250&timespan=7d"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "unrest",
			FeedType:    "gdelt_geo_protest",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:protest",
			PollMinutes: 30,
		},
		{
			DisplayName:  "Horizon API: ACLED Protests",
			URL:          resolve("/api/acled"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "unrest",
			FeedType:     "acled_protests",
			IngestMode:   wmIngestModeOntologyFact,
			SignalType:   "risk:protest",
			RequiresAuth: true,
			PollMinutes:  30,
		},
		{
			DisplayName:  "Horizon API: ACLED Armed Conflicts",
			URL:          resolve("/api/acled-conflict"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "conflict",
			FeedType:     "acled_conflict",
			IngestMode:   wmIngestModeOntologyFact,
			RequiresAuth: true,
			PollMinutes:  30,
		},
		{
			DisplayName: "Horizon API: OpenSky Military Flights",
			URL:         resolve("/api/opensky"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "military",
			FeedType:    "opensky_flights",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:military_flight",
			PollMinutes: 30,
		},
		{
			DisplayName:  "Horizon API: AIS Snapshot",
			URL:          resolve("/api/ais-snapshot?candidates=true"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "maritime",
			FeedType:     "ais_snapshot",
			IngestMode:   wmIngestModeOntologyFact,
			SignalType:   "risk:ais_disruption",
			RequiresAuth: true,
			PollMinutes:  30,
		},
		{
			DisplayName:  "Horizon API: Cloudflare Outages",
			URL:          resolve("/api/cloudflare-outages?dateRange=7d&limit=100"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "infrastructure",
			FeedType:     "cloudflare_outages",
			IngestMode:   wmIngestModeOntologyFact,
			SignalType:   "risk:internet_outage",
			RequiresAuth: true,
			PollMinutes:  30,
		},
		{
			DisplayName: "Horizon API: USGS Earthquakes",
			URL:         resolve("/api/earthquakes"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "natural",
			FeedType:    "usgs_earthquakes",
			IngestMode:  wmIngestModeOntologyFact,
			PollMinutes: 30,
		},
		{
			DisplayName:  "Horizon API: NASA FIRMS Fires",
			URL:          resolve("/api/firms-fires?days=1"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "natural",
			FeedType:     "nasa_firms_fires",
			IngestMode:   wmIngestModeOntologyFact,
			SignalType:   "risk:satellite_fire",
			RequiresAuth: true,
			PollMinutes:  30,
		},
		{
			DisplayName: "Horizon API: UCDP GED Events",
			URL:         resolve("/api/ucdp-events"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "conflict",
			FeedType:    "ucdp_ged_events",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:conflict_event",
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: HAPI Conflict Context",
			URL:         resolve("/api/hapi"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "conflict",
			FeedType:    "hapi_conflict_context",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:conflict_event",
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: Climate Anomalies",
			URL:         resolve("/api/climate-anomalies"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "natural",
			FeedType:    "climate_anomalies",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:climate_anomaly",
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: Service Status",
			URL:         resolve("/api/service-status"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "infrastructure",
			FeedType:    "service_status",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:service_outage",
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: NGA Maritime Warnings",
			URL:         resolve("/api/nga-warnings"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "maritime",
			FeedType:    "nga_warnings",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:maritime_hazard",
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: FAA Airport Status",
			URL:         resolve("/api/faa-status"),
			Kind:        "horizon_monitor_feed",
			Status:      "active",
			Category:    "transport",
			FeedType:    "faa_airport_status",
			IngestMode:  wmIngestModeOntologyFact,
			SignalType:  "risk:aviation_disruption",
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: GDELT Doc Military",
			URL:         resolve(gdeltDocPath(`(military exercise OR troop deployment OR airstrike OR "naval exercise") sourcelang:eng`)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "intel",
			FeedType:    "gdelt_doc_military",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: GDELT Doc Cyber",
			URL:         resolve(gdeltDocPath(`(cyberattack OR ransomware OR hacking OR "data breach" OR APT) sourcelang:eng`)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "intel",
			FeedType:    "gdelt_doc_cyber",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: GDELT Doc Nuclear",
			URL:         resolve(gdeltDocPath(`(nuclear OR uranium enrichment OR IAEA OR "nuclear weapon" OR plutonium) sourcelang:eng`)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "intel",
			FeedType:    "gdelt_doc_nuclear",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: GDELT Doc Sanctions",
			URL:         resolve(gdeltDocPath(`(sanctions OR embargo OR "trade war" OR tariff OR "economic pressure") sourcelang:eng`)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "intel",
			FeedType:    "gdelt_doc_sanctions",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: GDELT Doc Intelligence",
			URL:         resolve(gdeltDocPath(`(espionage OR spy OR intelligence agency OR covert OR surveillance) sourcelang:eng`)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "intel",
			FeedType:    "gdelt_doc_intelligence",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: GDELT Doc Maritime",
			URL:         resolve(gdeltDocPath(`(naval blockade OR piracy OR "strait of hormuz" OR "south china sea" OR warship) sourcelang:eng`)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "intel",
			FeedType:    "gdelt_doc_maritime",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Yahoo Finance S&P 500",
			URL:         resolve(yahooPath("^GSPC")),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "yahoo_finance_sp500",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Yahoo Finance Dow Jones",
			URL:         resolve(yahooPath("^DJI")),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "yahoo_finance_dowjones",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Yahoo Finance Nasdaq",
			URL:         resolve(yahooPath("^IXIC")),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "yahoo_finance_nasdaq",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Yahoo Finance Gold",
			URL:         resolve(yahooPath("GC=F")),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "yahoo_finance_gold",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Yahoo Finance Oil",
			URL:         resolve(yahooPath("CL=F")),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "yahoo_finance_oil",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName:  "Horizon API: Finnhub Index Basket",
			URL:          resolve(finnhubPath([]string{"SPY", "QQQ", "DIA", "IWM"})),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "market",
			FeedType:     "finnhub_indices",
			IngestMode:   wmIngestModeQueryOnly,
			RequiresAuth: true,
			PollMinutes:  30,
		},
		{
			DisplayName: "Horizon API: CoinGecko Majors",
			URL:         resolve("/api/coingecko?ids=bitcoin,ethereum,solana&vs_currencies=usd&include_24hr_change=true"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "coingecko_majors",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Polymarket Top Events",
			URL:         resolve("/api/polymarket?closed=false&order=volume&ascending=false&limit=100"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "polymarket_events",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName:  "Horizon API: FRED CPI",
			URL:          resolve(fredPath("CPIAUCSL")),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "economy",
			FeedType:     "fred_cpi",
			IngestMode:   wmIngestModeQueryOnly,
			RequiresAuth: true,
			PollMinutes:  60,
		},
		{
			DisplayName:  "Horizon API: FRED Policy Rate",
			URL:          resolve(fredPath("FEDFUNDS")),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "economy",
			FeedType:     "fred_fedfunds",
			IngestMode:   wmIngestModeQueryOnly,
			RequiresAuth: true,
			PollMinutes:  60,
		},
		{
			DisplayName:  "Horizon API: EIA Petroleum Snapshot",
			URL:          resolve("/api/eia/petroleum"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "energy",
			FeedType:     "eia_petroleum",
			IngestMode:   wmIngestModeQueryOnly,
			RequiresAuth: true,
			PollMinutes:  60,
		},
		{
			DisplayName: "Horizon API: World Bank GDP Growth",
			URL:         resolve(worldbankPath("NY.GDP.MKTP.KD.ZG", 10)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "economy",
			FeedType:    "worldbank_gdp_growth",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName: "Horizon API: Macro Signals",
			URL:         resolve("/api/macro-signals"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "macro_signals",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: ETF Flows",
			URL:         resolve("/api/etf-flows"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "etf_flows",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: Stablecoin Markets",
			URL:         resolve("/api/stablecoin-markets"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "stablecoin_markets",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 30,
		},
		{
			DisplayName: "Horizon API: Stock Index US",
			URL:         resolve("/api/stock-index?code=US"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "market",
			FeedType:    "stock_index_us",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: UCDP Country Conflicts",
			URL:         resolve("/api/ucdp"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "conflict",
			FeedType:    "ucdp_country_conflicts",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName: "Horizon API: UNHCR Population",
			URL:         resolve(unhcrPath(2025, 200, 1)),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "humanitarian",
			FeedType:    "unhcr_population",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName: "Horizon API: WorldPop Exposure Countries",
			URL:         resolve("/api/worldpop-exposure?mode=countries"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "humanitarian",
			FeedType:    "worldpop_exposure_countries",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName: "Horizon API: Arxiv AI",
			URL:         resolve("/api/arxiv?category=cs.AI&max_results=50"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "research",
			FeedType:    "arxiv_ai",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName: "Horizon API: GitHub Trending",
			URL:         resolve("/api/github-trending?language=python&since=daily"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "research",
			FeedType:    "github_trending",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName: "Horizon API: Hacker News Top",
			URL:         resolve("/api/hackernews?type=top&limit=30"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "research",
			FeedType:    "hackernews_top",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 60,
		},
		{
			DisplayName: "Horizon API: Tech Events",
			URL:         resolve("/api/tech-events"),
			Kind:        "horizon_monitor_feed",
			Status:      "paused",
			Category:    "research",
			FeedType:    "tech_events",
			IngestMode:  wmIngestModeQueryOnly,
			PollMinutes: 180,
		},
		{
			DisplayName:  "Horizon API: Risk Scores",
			URL:          resolve("/api/risk-scores"),
			Kind:         "horizon_monitor_feed",
			Status:       "paused",
			Category:     "risk",
			FeedType:     "risk_scores",
			IngestMode:   wmIngestModeQueryOnly,
			RequiresAuth: true,
			PollMinutes:  60,
		},
	}
	return specs
}

func (i *importer) importSourceSpecs(ctx context.Context, specs []wmSourceSpec) error {
	for _, spec := range specs {
		if err := i.upsertSourceSpec(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

func (i *importer) upsertSourceSpec(ctx context.Context, spec wmSourceSpec) error {
	if i.dsRepo == nil {
		return nil
	}
	sourceURL := strings.TrimSpace(spec.URL)
	if sourceURL == "" {
		return nil
	}
	kind := strings.TrimSpace(spec.Kind)
	if kind == "" {
		kind = "horizon_monitor_feed"
	}
	if kind != "rss" && kind != "horizon_monitor_feed" {
		kind = "horizon_monitor_feed"
	}
	status := strings.TrimSpace(spec.Status)
	if status == "" {
		status = firstNonEmpty(i.sourceStatus, "active")
	}
	pollMinutes := spec.PollMinutes
	if pollMinutes <= 0 {
		pollMinutes = 30
	}

	existing, err := i.dsRepo.FindByCanonicalSource(ctx, i.tenantID, sourceURL)
	if err == nil && existing != nil {
		changed := false
		if strings.TrimSpace(existing.DisplayName) == "" && strings.TrimSpace(spec.DisplayName) != "" {
			existing.DisplayName = strings.TrimSpace(spec.DisplayName)
			changed = true
		}
		if strings.TrimSpace(existing.OwnerUserID) == "" {
			existing.OwnerUserID = firstNonEmpty(i.ownerUserID, "horizon-monitor-import")
			changed = true
		}
		if strings.TrimSpace(existing.Status) != status {
			existing.Status = status
			changed = true
		}
		if strings.TrimSpace(existing.Kind) == "" {
			existing.Kind = kind
			changed = true
		} else if strings.TrimSpace(existing.Kind) != kind {
			// Keep legacy RSS sources as RSS if they already exist that way.
			if !(strings.TrimSpace(existing.Kind) == "rss" && kind == "horizon_monitor_feed") {
				existing.Kind = kind
				changed = true
			}
		}
		if kind == "rss" {
			if existing.Details.RSSURL == nil || strings.TrimSpace(*existing.Details.RSSURL) == "" {
				existing.Details.RSSURL = strPtr(sourceURL)
				changed = true
			}
		} else {
			if existing.Details.URI == nil || strings.TrimSpace(*existing.Details.URI) == "" {
				existing.Details.URI = strPtr(sourceURL)
				changed = true
			}
		}
		if strings.TrimSpace(spec.Category) != "" {
			if existing.Details.HorizonMonitorCategory == nil || strings.TrimSpace(*existing.Details.HorizonMonitorCategory) != strings.TrimSpace(spec.Category) {
				existing.Details.HorizonMonitorCategory = strPtr(spec.Category)
				changed = true
			}
		}
		if strings.TrimSpace(spec.FeedType) != "" {
			if existing.Details.HorizonMonitorFeedType == nil || strings.TrimSpace(*existing.Details.HorizonMonitorFeedType) != strings.TrimSpace(spec.FeedType) {
				existing.Details.HorizonMonitorFeedType = strPtr(spec.FeedType)
				changed = true
			}
		}
		if strings.TrimSpace(spec.IngestMode) != "" {
			if existing.Details.HorizonMonitorIngestMode == nil || strings.TrimSpace(*existing.Details.HorizonMonitorIngestMode) != strings.TrimSpace(spec.IngestMode) {
				existing.Details.HorizonMonitorIngestMode = strPtr(spec.IngestMode)
				changed = true
			}
		}
		if strings.TrimSpace(spec.SignalType) != "" {
			if existing.Details.HorizonMonitorSignalType == nil || strings.TrimSpace(*existing.Details.HorizonMonitorSignalType) != strings.TrimSpace(spec.SignalType) {
				existing.Details.HorizonMonitorSignalType = strPtr(spec.SignalType)
				changed = true
			}
		}
		if existing.Details.HorizonMonitorRequiresAuth == nil || *existing.Details.HorizonMonitorRequiresAuth != spec.RequiresAuth {
			existing.Details.HorizonMonitorRequiresAuth = boolPtr(spec.RequiresAuth)
			changed = true
		}
		if existing.Details.PollIntervalMinutes == nil || *existing.Details.PollIntervalMinutes != pollMinutes {
			existing.Details.PollIntervalMinutes = intPtr(pollMinutes)
			changed = true
		}
		if changed {
			if i.apply {
				if uerr := i.dsRepo.Upsert(ctx, existing); uerr != nil {
					return uerr
				}
			}
			i.stats.SourcesUpdated++
		}
		return i.ensureSourceProjectLink(ctx, existing.ID)
	}

	now := time.Now().UTC()
	ds := &models.DataSource{
		TenantID:    i.tenantID,
		DisplayName: firstNonEmpty(spec.DisplayName, sourceURL),
		Kind:        kind,
		Status:      status,
		OwnerUserID: firstNonEmpty(i.ownerUserID, "horizon-monitor-import"),
		CreatedAt:   now,
		UpdatedAt:   now,
		FirstSeen:   now,
		LastSeen:    now,
		TrustLevel:  horizonMonitorTrustLevel(spec.IngestMode),
		Details: models.SourceDetails{
			HorizonMonitorCategory:     strPtr(spec.Category),
			HorizonMonitorFeedType:     strPtr(spec.FeedType),
			HorizonMonitorIngestMode:   strPtr(spec.IngestMode),
			HorizonMonitorSignalType:   strPtr(spec.SignalType),
			HorizonMonitorRequiresAuth: boolPtr(spec.RequiresAuth),
			PollIntervalMinutes:        intPtr(pollMinutes),
		},
	}
	if kind == "rss" {
		ds.Details.RSSURL = strPtr(sourceURL)
	} else {
		ds.Details.URI = strPtr(sourceURL)
	}

	if !i.apply {
		i.stats.SourcesCreated++
		return nil
	}
	id, err := i.dsRepo.Create(ctx, ds)
	if err != nil {
		return err
	}
	i.stats.SourcesCreated++
	return i.ensureSourceProjectLink(ctx, id)
}

func horizonMonitorTrustLevel(mode string) float64 {
	switch strings.TrimSpace(mode) {
	case wmIngestModeOntologyFact:
		return 0.75
	case wmIngestModeQueryOnly:
		return 0.6
	default:
		return 0.7
	}
}

func (i *importer) ensureSourceProjectLink(ctx context.Context, sourceID string) error {
	if i.spRepo == nil || i.tenantID == "" || i.projectID == "" || strings.TrimSpace(sourceID) == "" {
		return nil
	}

	existing, err := i.spRepo.GetByProjectAndSource(ctx, i.tenantID, i.projectID, sourceID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	if !i.apply {
		i.stats.SourceLinks++
		return nil
	}
	_, err = i.spRepo.Create(ctx, &models.SourceProject{
		TenantID:  i.tenantID,
		ProjectID: i.projectID,
		SourceID:  sourceID,
		Status:    "active",
	})
	if err != nil {
		return err
	}
	i.stats.SourceLinks++
	return nil
}

type buildFragmentSpec struct {
	CanonicalPath  string
	Type           hmodels.FragmentType
	Label          string
	Description    string
	SourceID       string
	Tags           []string
	Lat            float64
	Lon            float64
	Country        string
	Qualifiers     map[string]interface{}
	HorizonMonitor *hmodels.HorizonMonitorFragmentData
}

func (i *importer) upsertFragment(ctx context.Context, spec buildFragmentSpec) (string, error) {
	cp := strings.TrimSpace(spec.CanonicalPath)
	if cp == "" {
		return "", nil
	}
	if id, ok := i.pathToID[cp]; ok {
		return id, nil
	}

	now := time.Now().UTC()
	incoming := &hmodels.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: cp,
		Type:          spec.Type,
		Labels: map[string]string{
			"en": strings.TrimSpace(spec.Label),
		},
		Description: strings.TrimSpace(spec.Description),
		Tags:        uniqueStrings(append([]string{"horizon-monitor"}, spec.Tags...)),
		Status:      string(hmodels.FragmentStatusActive),
		State:       map[string]hmodels.MetricValue{},
		Identifiers: map[string]string{},
		Qualifiers:  map[string]interface{}{},
		Provenance: hmodels.Provenance{
			Method:    "horizon_monitor_import",
			SourceIDs: []string{"horizon-monitor"},
		},
		Metadata: hmodels.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			LastVerifiedAt:  now,
			EpistemicStatus: hmodels.EpistemicStatusLowConfidence,
		},
		Governance: hmodels.Governance{
			Owner:  "SYSTEM",
			Status: string(hmodels.FragmentStatusActive),
		},
	}
	if strings.TrimSpace(spec.SourceID) != "" {
		incoming.Identifiers["sourceObjectId"] = strings.TrimSpace(spec.SourceID)
	}
	if strings.TrimSpace(spec.Label) != "" {
		incoming.Identifiers["shortName"] = strings.TrimSpace(spec.Label)
	}
	for k, v := range spec.Qualifiers {
		incoming.Qualifiers[k] = v
	}
	if spec.HorizonMonitor != nil {
		incoming.HorizonMonitor = cloneHorizonMonitorData(spec.HorizonMonitor)
	}
	if !isFiniteCoord(spec.Lat, spec.Lon) {
		spec.Lat = 0
		spec.Lon = 0
	}
	if isFiniteCoord(spec.Lat, spec.Lon) {
		incoming.Coordinates = &hmodels.Coordinates{Lat: spec.Lat, Lon: spec.Lon}
		incoming.HomeLocation = makeLocation(spec.Lat, spec.Lon, i.geoToken(spec.Country), strings.TrimSpace(spec.Label))
	}
	incoming.Qualifiers["horizonMonitorGeoEligible"] = hmodels.IsHorizonMonitorGeoEligibleFragment(incoming)

	existing, err := i.fragments.GetByCanonicalPath(ctx, cp)
	if err == nil && existing != nil {
		changed := mergeFragment(existing, incoming)
		i.pathToID[cp] = existing.ID
		i.idToPath[existing.ID] = cp
		if changed {
			if i.apply {
				if uerr := i.fragments.Update(ctx, existing); uerr != nil {
					return "", uerr
				}
			}
			i.stats.FragmentsUpdated++
		}
		return existing.ID, nil
	}
	if err != nil && !errors.Is(err, hrepo.ErrFragmentNotFound) {
		return "", err
	}
	if strings.TrimSpace(spec.SourceID) != "" {
		matches, ferr := i.fragments.FindByIdentifier(ctx, "sourceObjectId", strings.TrimSpace(spec.SourceID))
		if ferr != nil {
			return "", ferr
		}
		var byID *hmodels.HorizonFragment
		for _, candidate := range matches {
			if candidate == nil {
				continue
			}
			if candidate.Type == spec.Type {
				byID = candidate
				break
			}
			if byID == nil {
				byID = candidate
			}
		}
		if byID != nil {
			changed := mergeFragment(byID, incoming)
			if strings.TrimSpace(byID.CanonicalPath) != cp {
				oldPath := strings.TrimSpace(byID.CanonicalPath)
				byID.CanonicalPath = cp
				if oldPath != "" {
					i.pathToID[oldPath] = byID.ID
				}
				changed = true
			}
			i.pathToID[cp] = byID.ID
			i.idToPath[byID.ID] = cp
			if changed {
				if i.apply {
					if uerr := i.fragments.Update(ctx, byID); uerr != nil {
						return "", uerr
					}
				}
				i.stats.FragmentsUpdated++
			}
			return byID.ID, nil
		}
	}

	if !i.apply {
		dryID := "dry:" + cp
		i.pathToID[cp] = dryID
		i.idToPath[dryID] = cp
		i.stats.FragmentsCreated++
		return dryID, nil
	}

	id, err := i.fragments.Create(ctx, incoming)
	if err != nil {
		return "", err
	}
	i.pathToID[cp] = id
	i.idToPath[id] = cp
	i.stats.FragmentsCreated++
	return id, nil
}

func mergeFragment(dst, src *hmodels.HorizonFragment) bool {
	changed := false

	if dst.Labels == nil {
		dst.Labels = map[string]string{}
	}
	if strings.TrimSpace(dst.Labels["en"]) == "" && strings.TrimSpace(src.Labels["en"]) != "" {
		dst.Labels["en"] = strings.TrimSpace(src.Labels["en"])
		changed = true
	}
	if strings.TrimSpace(dst.Description) == "" && strings.TrimSpace(src.Description) != "" {
		dst.Description = strings.TrimSpace(src.Description)
		changed = true
	}

	mergedTags := uniqueStrings(append(dst.Tags, src.Tags...))
	if strings.Join(dst.Tags, "|") != strings.Join(mergedTags, "|") {
		dst.Tags = mergedTags
		changed = true
	}

	if dst.Identifiers == nil {
		dst.Identifiers = map[string]string{}
	}
	for k, v := range src.Identifiers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(dst.Identifiers[k]) == "" {
			dst.Identifiers[k] = v
			changed = true
		}
	}

	if dst.Qualifiers == nil {
		dst.Qualifiers = map[string]interface{}{}
	}
	for k, v := range src.Qualifiers {
		if _, ok := dst.Qualifiers[k]; !ok {
			dst.Qualifiers[k] = v
			changed = true
		}
	}

	if dst.HomeLocation == nil && src.HomeLocation != nil {
		dst.HomeLocation = src.HomeLocation
		changed = true
	}
	if dst.Coordinates == nil && src.Coordinates != nil {
		c := *src.Coordinates
		dst.Coordinates = &c
		changed = true
	}

	if src.HorizonMonitor != nil {
		if dst.HorizonMonitor == nil {
			dst.HorizonMonitor = cloneHorizonMonitorData(src.HorizonMonitor)
			changed = true
		} else if mergeHorizonMonitorData(dst.HorizonMonitor, src.HorizonMonitor) {
			changed = true
		}
	}

	return changed
}

func (i *importer) ensureGeoLink(ctx context.Context, fragmentID, country string, lat, lon float64, reason string) error {
	if strings.TrimSpace(fragmentID) == "" {
		return nil
	}
	geoPath := i.resolveGeoPath(country, lat, lon)
	if geoPath == "" {
		return nil
	}
	geoLabel := i.geoLabelFromPath(geoPath)
	geoID, err := i.upsertFragment(ctx, buildFragmentSpec{
		CanonicalPath: geoPath,
		Type:          hmodels.FragmentTypeGeoRegion,
		Label:         geoLabel,
		SourceID:      "",
		Tags:          []string{"geo"},
		Lat:           lat,
		Lon:           lon,
		Country:       country,
		Qualifiers: map[string]interface{}{
			"horizonMonitorKind": "geo_anchor",
		},
		HorizonMonitor: &hmodels.HorizonMonitorFragmentData{
			ObjectKind:  hmodels.HorizonMonitorKindGeoAnchor,
			Coordinates: wmCoord(lat, lon),
			Countries:   uniqueStrings([]string{strings.TrimSpace(country)}),
		},
	})
	if err != nil {
		return err
	}
	if strings.HasPrefix(fragmentID, "dry:") || strings.HasPrefix(geoID, "dry:") {
		i.stats.GeoLinksCreated++
		return nil
	}
	if err := i.ensureEdge(ctx, fragmentID, geoID, hmodels.DependencyTypeLocatedIn, reason); err != nil {
		return err
	}
	i.stats.GeoLinksCreated++
	return nil
}

func (i *importer) ensureEdge(ctx context.Context, sourceID, targetID string, depType hmodels.DependencyType, reason string) error {
	if !i.apply {
		i.stats.EdgesCreated++
		return nil
	}
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return nil
	}

	if _, err := i.edges.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, depType); err == nil {
		return nil
	} else if !errors.Is(err, hrepo.ErrEdgeNotFound) {
		return err
	}

	now := time.Now().UTC()
	_, err := i.edges.Create(ctx, &hmodels.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: sourceID,
		TargetFragmentID: targetID,
		Type:             depType,
		Properties: hmodels.EdgeProperties{
			Criticality:   0.45,
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"reason":       firstNonEmpty(reason, "horizon_monitor.geo_link"),
			"sourceSystem": "horizon-monitor",
		},
		Provenance: hmodels.Provenance{
			Method:    "horizon_monitor_import",
			SourceIDs: []string{"horizon-monitor"},
		},
		Metadata: hmodels.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
	})
	if err != nil {
		return err
	}
	i.stats.EdgesCreated++
	return nil
}

func (i *importer) ensureHorizonMonitorTaxonomy(ctx context.Context, enabled bool) error {
	if !enabled || i.taxRepo == nil {
		return nil
	}

	tax, err := i.taxRepo.Get(ctx, "v1")
	if err != nil {
		if !i.apply {
			log.Printf("[DRY-RUN] taxonomy v1 not found; would create/extend with horizon monitor vocab")
			return nil
		}
		return fmt.Errorf("load horizon taxonomy: %w", err)
	}

	changed := false
	eventTypes := []string{
		"conflict", "protest", "disaster", "diplomatic", "economic",
		"terrorism", "cyber", "health", "environmental", "military",
		"crime", "infrastructure", "tech",
	}
	for _, et := range eventTypes {
		if appendUniqueLower(&tax.AllowedEventTypes, et) {
			changed = true
		}
	}
	if tax.EventTypeSynonyms == nil {
		tax.EventTypeSynonyms = map[string]string{}
	}
	for k, v := range map[string]string{
		"cyber attack":       "cyber",
		"internet outage":    "infrastructure",
		"power outage":       "infrastructure",
		"pipeline explosion": "infrastructure",
		"civil unrest":       "protest",
		"armed escalation":   "conflict",
		"naval activity":     "military",
	} {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" || v == "" {
			continue
		}
		if cur, ok := tax.EventTypeSynonyms[k]; !ok || strings.TrimSpace(cur) != v {
			tax.EventTypeSynonyms[k] = v
			changed = true
		}
	}

	if tax.SignalTypes == nil {
		tax.SignalTypes = map[string]models.SignalTypeSpecData{}
	}
	if tax.SignalTypeSynonyms == nil {
		tax.SignalTypeSynonyms = map[string]string{}
	}
	wmSignalSpecs := map[string]models.SignalTypeSpecData{
		"risk:internet_outage":     {Kind: "risk", Label: "Internet Outage", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
		"risk:military_flight":     {Kind: "risk", Label: "Military Flight Activity", DefaultMetric: "risk_signal_strength", PropagationProfile: "upstream_amplified"},
		"risk:military_vessel":     {Kind: "risk", Label: "Military Vessel Activity", DefaultMetric: "risk_signal_strength", PropagationProfile: "upstream_amplified"},
		"risk:protest":             {Kind: "risk", Label: "Civil Protest Activity", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
		"risk:ais_disruption":      {Kind: "risk", Label: "AIS Disruption", DefaultMetric: "risk_signal_strength", PropagationProfile: "downstream_amplified"},
		"risk:satellite_fire":      {Kind: "risk", Label: "Satellite Fire Anomaly", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
		"risk:conflict_event":      {Kind: "risk", Label: "Conflict Event", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
		"risk:aviation_disruption": {Kind: "risk", Label: "Aviation Disruption", DefaultMetric: "risk_signal_strength", PropagationProfile: "downstream_amplified"},
		"risk:maritime_hazard":     {Kind: "risk", Label: "Maritime Hazard", DefaultMetric: "risk_signal_strength", PropagationProfile: "downstream_amplified"},
		"risk:climate_anomaly":     {Kind: "risk", Label: "Climate Anomaly", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
		"risk:service_outage":      {Kind: "risk", Label: "Service Outage", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
		"risk:temporal_anomaly":    {Kind: "risk", Label: "Temporal Baseline Anomaly", DefaultMetric: "risk_signal_strength", PropagationProfile: "jurisdiction_scoped"},
	}
	for key, spec := range wmSignalSpecs {
		if _, ok := tax.SignalTypes[key]; !ok {
			tax.SignalTypes[key] = spec
			changed = true
		}
	}
	for alias, canonical := range map[string]string{
		"internet_outage":     "risk:internet_outage",
		"military_flight":     "risk:military_flight",
		"military_vessel":     "risk:military_vessel",
		"protest":             "risk:protest",
		"ais_disruption":      "risk:ais_disruption",
		"satellite_fire":      "risk:satellite_fire",
		"conflict_event":      "risk:conflict_event",
		"aviation_disruption": "risk:aviation_disruption",
		"maritime_hazard":     "risk:maritime_hazard",
		"climate_anomaly":     "risk:climate_anomaly",
		"service_outage":      "risk:service_outage",
		"temporal_anomaly":    "risk:temporal_anomaly",
	} {
		if cur, ok := tax.SignalTypeSynonyms[alias]; !ok || strings.TrimSpace(cur) != canonical {
			tax.SignalTypeSynonyms[alias] = canonical
			changed = true
		}
	}

	if !changed {
		return nil
	}
	tax.UpdatedAt = time.Now().UTC()
	if !i.apply {
		log.Printf("[DRY-RUN] would update horizon_taxonomy/v1 with horizon monitor event/signal vocabulary")
		return nil
	}
	return i.taxRepo.Upsert(ctx, tax)
}

func appendUniqueLower(dst *[]string, values ...string) bool {
	if dst == nil {
		return false
	}
	seen := map[string]struct{}{}
	for _, v := range *dst {
		n := strings.ToLower(strings.TrimSpace(v))
		if n != "" {
			seen[n] = struct{}{}
		}
	}
	changed := false
	for _, v := range values {
		n := strings.ToLower(strings.TrimSpace(v))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		*dst = append(*dst, n)
		seen[n] = struct{}{}
		changed = true
	}
	return changed
}

func cloneHorizonMonitorData(in *hmodels.HorizonMonitorFragmentData) *hmodels.HorizonMonitorFragmentData {
	if in == nil {
		return nil
	}
	out := *in
	if in.Coordinates != nil {
		cp := *in.Coordinates
		out.Coordinates = &cp
	}
	if in.CapacityTbps != nil {
		v := *in.CapacityTbps
		out.CapacityTbps = &v
	}
	if in.ClassifierScore != nil {
		v := *in.ClassifierScore
		out.ClassifierScore = &v
	}
	out.Route = append([]hmodels.HorizonMonitorCoordinates(nil), in.Route...)
	out.Polygon = append([]hmodels.HorizonMonitorCoordinates(nil), in.Polygon...)
	out.Countries = append([]string(nil), in.Countries...)
	out.Parties = append([]string(nil), in.Parties...)
	out.Keywords = append([]string(nil), in.Keywords...)
	if len(in.LandingPoints) > 0 {
		out.LandingPoints = make([]hmodels.HorizonMonitorLandingPoint, 0, len(in.LandingPoints))
		for _, lp := range in.LandingPoints {
			cp := lp
			if lp.Coordinates != nil {
				c := *lp.Coordinates
				cp.Coordinates = &c
			}
			out.LandingPoints = append(out.LandingPoints, cp)
		}
	}
	return &out
}

func mergeHorizonMonitorData(dst, src *hmodels.HorizonMonitorFragmentData) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false
	if dst.ObjectKind == "" && src.ObjectKind != "" {
		dst.ObjectKind = src.ObjectKind
		changed = true
	}
	if strings.TrimSpace(dst.SourceObjectID) == "" && strings.TrimSpace(src.SourceObjectID) != "" {
		dst.SourceObjectID = strings.TrimSpace(src.SourceObjectID)
		changed = true
	}
	if dst.Coordinates == nil && src.Coordinates != nil {
		c := *src.Coordinates
		dst.Coordinates = &c
		changed = true
	}
	if len(dst.Route) == 0 && len(src.Route) > 0 {
		dst.Route = append([]hmodels.HorizonMonitorCoordinates(nil), src.Route...)
		changed = true
	}
	if len(dst.Polygon) == 0 && len(src.Polygon) > 0 {
		dst.Polygon = append([]hmodels.HorizonMonitorCoordinates(nil), src.Polygon...)
		changed = true
	}
	if len(dst.LandingPoints) == 0 && len(src.LandingPoints) > 0 {
		dst.LandingPoints = cloneHorizonMonitorData(src).LandingPoints
		changed = true
	}
	if len(dst.Countries) == 0 && len(src.Countries) > 0 {
		dst.Countries = append([]string(nil), src.Countries...)
		changed = true
	}
	if len(dst.Parties) == 0 && len(src.Parties) > 0 {
		dst.Parties = append([]string(nil), src.Parties...)
		changed = true
	}
	if len(dst.Keywords) == 0 && len(src.Keywords) > 0 {
		dst.Keywords = append([]string(nil), src.Keywords...)
		changed = true
	}
	if dst.AKA == "" && src.AKA != "" {
		dst.AKA = src.AKA
		changed = true
	}
	if dst.Sponsor == "" && src.Sponsor != "" {
		dst.Sponsor = src.Sponsor
		changed = true
	}
	if dst.FacilityType == "" && src.FacilityType != "" {
		dst.FacilityType = src.FacilityType
		changed = true
	}
	if dst.MarketType == "" && src.MarketType != "" {
		dst.MarketType = src.MarketType
		changed = true
	}
	if dst.PipelineType == "" && src.PipelineType != "" {
		dst.PipelineType = src.PipelineType
		changed = true
	}
	if dst.PortType == "" && src.PortType != "" {
		dst.PortType = src.PortType
		changed = true
	}
	if dst.Mineral == "" && src.Mineral != "" {
		dst.Mineral = src.Mineral
		changed = true
	}
	if dst.Capacity == "" && src.Capacity != "" {
		dst.Capacity = src.Capacity
		changed = true
	}
	if dst.CapacityTbps == nil && src.CapacityTbps != nil {
		v := *src.CapacityTbps
		dst.CapacityTbps = &v
		changed = true
	}
	if dst.Operator == "" && src.Operator != "" {
		dst.Operator = src.Operator
		changed = true
	}
	if dst.Status == "" && src.Status != "" {
		dst.Status = src.Status
		changed = true
	}
	if dst.Significance == "" && src.Significance != "" {
		dst.Significance = src.Significance
		changed = true
	}
	if dst.ThreatCategory == "" && src.ThreatCategory != "" {
		dst.ThreatCategory = src.ThreatCategory
		changed = true
	}
	if dst.ThreatLevel == "" && src.ThreatLevel != "" {
		dst.ThreatLevel = src.ThreatLevel
		changed = true
	}
	if dst.ClassifierScore == nil && src.ClassifierScore != nil {
		v := *src.ClassifierScore
		dst.ClassifierScore = &v
		changed = true
	}
	return changed
}

func (i *importer) resolveGeoPath(country string, lat, lon float64) string {
	if cp := i.countryCanonicalPath(country); cp != "" {
		return cp
	}
	if !isFiniteCoord(lat, lon) {
		return ""
	}
	latQ := roundCoord(lat)
	lonQ := roundCoord(lon)
	cell := fmt.Sprintf("cell-%s-%s", formatCoord(latQ), formatCoord(lonQ))
	return taxonomy.JoinPath("loc", "global", "na", cell)
}

func (i *importer) countryCanonicalPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	clean := strings.ToLower(strings.TrimSpace(removeParenthetical(s)))
	clean = strings.TrimSpace(strings.ReplaceAll(clean, "_", " "))
	if clean == "" {
		return ""
	}

	if len(clean) == 2 && regexp.MustCompile(`^[a-z]{2}$`).MatchString(clean) {
		return "/loc/" + clean + "/na/na"
	}
	if code, ok := i.countryMap[clean]; ok {
		return "/loc/" + code + "/na/na"
	}
	if cp := scout.GetCountryCanonicalPath(clean); cp != "" {
		return cp
	}
	if idx := strings.Index(clean, ","); idx > 0 {
		if cp := scout.GetCountryCanonicalPath(strings.TrimSpace(clean[:idx])); cp != "" {
			return cp
		}
	}
	return ""
}

func (i *importer) geoToken(country string) string {
	cp := i.countryCanonicalPath(country)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 4 || parts[0] != "loc" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (i *importer) geoLabelFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 4 || parts[0] != "loc" {
		return "Location"
	}
	cc := strings.TrimSpace(parts[1])
	if cc == "" {
		cc = "global"
	}
	admin := strings.TrimSpace(parts[2])
	city := strings.TrimSpace(parts[3])
	if city != "" && city != "na" {
		return strings.ReplaceAll(city, "-", " ")
	}
	if admin != "" && admin != "na" {
		return strings.ReplaceAll(admin, "-", " ")
	}
	return strings.ToUpper(cc)
}

func makeLocation(lat, lon float64, iso2 string, name string) *hmodels.Location {
	if !isFiniteCoord(lat, lon) {
		return nil
	}
	iso := fmt.Sprintf("%+.4f%+.4f/", lat, lon)
	loc := &hmodels.Location{
		Name:    strings.TrimSpace(name),
		ISO6709: &iso,
	}
	if strings.TrimSpace(iso2) != "" {
		cc := strings.ToLower(strings.TrimSpace(iso2))
		loc.ISO3166_1 = &cc
	}
	return loc
}

func wmCoord(lat, lon float64) *hmodels.HorizonMonitorCoordinates {
	if !isFiniteCoord(lat, lon) {
		return nil
	}
	return &hmodels.HorizonMonitorCoordinates{
		Lat: lat,
		Lon: lon,
	}
}

func wmRoute(points [][]float64) []hmodels.HorizonMonitorCoordinates {
	if len(points) == 0 {
		return nil
	}
	out := make([]hmodels.HorizonMonitorCoordinates, 0, len(points))
	for _, p := range points {
		if len(p) < 2 {
			continue
		}
		lat, lon := p[1], p[0]
		if !isFiniteCoord(lat, lon) {
			continue
		}
		out = append(out, hmodels.HorizonMonitorCoordinates{Lat: lat, Lon: lon})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func wmLanding(points []wmLandingPoint) []hmodels.HorizonMonitorLandingPoint {
	if len(points) == 0 {
		return nil
	}
	out := make([]hmodels.HorizonMonitorLandingPoint, 0, len(points))
	for _, p := range points {
		out = append(out, hmodels.HorizonMonitorLandingPoint{
			Country:     strings.TrimSpace(p.Country),
			City:        strings.TrimSpace(p.City),
			Coordinates: wmCoord(p.Lat, p.Lon),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func wmLandingCountries(points []wmLandingPoint) []string {
	if len(points) == 0 {
		return nil
	}
	out := make([]string, 0, len(points))
	for _, p := range points {
		out = append(out, strings.TrimSpace(p.Country))
	}
	return uniqueStrings(out)
}

func firstPoint(points [][]float64) (float64, float64) {
	if len(points) == 0 || len(points[0]) < 2 {
		return 0, 0
	}
	return points[0][1], points[0][0]
}

func fromCenter(center [2]float64) (float64, float64) {
	if len(center) < 2 {
		return 0, 0
	}
	return center[1], center[0]
}

func isFiniteCoord(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return false
	}
	return !(lat == 0 && lon == 0)
}

func roundCoord(v float64) float64 {
	return math.Round(v*10) / 10
}

func formatCoord(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	s = strings.ReplaceAll(s, ".", "p")
	s = strings.ReplaceAll(s, "-", "m")
	return s
}

func removeParenthetical(s string) string {
	idx := strings.Index(s, "(")
	if idx <= 0 {
		return s
	}
	return strings.TrimSpace(s[:idx])
}

func normalizeTags(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return uniqueStrings(out)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func intPtr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func floatPtr(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
