package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type config struct {
	projectID       string
	tenantID        string
	mode            string
	anchorPath      string
	anchorQuery     string
	lensID          string
	question        string
	bucket          string
	vertexLocation  string
	nodeStoreID     string
	edgeStoreID     string
	artifactStoreID string
	factStoreID     string
	topK            int
	limitArtifacts  int
	limitEdges      int
	limitFacts      int
	limitFragments  int
	useLLM          bool
	outFile         string
}

type vertexStore struct {
	Name        string `json:"name"`
	DataStoreID string `json:"dataStoreId"`
}

type seedReport struct {
	Mode              string                `json:"mode"`
	ProjectID         string                `json:"projectId"`
	TenantID          string                `json:"tenantId"`
	AnchorPath        string                `json:"anchorPath"`
	AnchorLabel       string                `json:"anchorLabel,omitempty"`
	LensID            string                `json:"lensId,omitempty"`
	LensName          string                `json:"lensName,omitempty"`
	Stores            []vertexStore         `json:"stores,omitempty"`
	ExportedFragments int                   `json:"exportedFragments"`
	ExportedEdges     int                   `json:"exportedEdges"`
	ExportedArtifacts int                   `json:"exportedArtifacts"`
	ExportedFacts     int                   `json:"exportedFacts"`
	ImportedURIs      map[string]int        `json:"importedUris,omitempty"`
	Operations        map[string]string     `json:"operations,omitempty"`
	Warnings          []string              `json:"warnings,omitempty"`
	SelectedArtifacts []artifactSummary     `json:"selectedArtifacts,omitempty"`
	SelectedRoutes    []routeSummary        `json:"selectedRoutes,omitempty"`
}

type answerReport struct {
	Mode                string                 `json:"mode"`
	ProjectID           string                 `json:"projectId"`
	TenantID            string                 `json:"tenantId"`
	Question            string                 `json:"question"`
	AnchorPath          string                 `json:"anchorPath,omitempty"`
	AnchorLabel         string                 `json:"anchorLabel,omitempty"`
	LensID              string                 `json:"lensId,omitempty"`
	LensName            string                 `json:"lensName,omitempty"`
	QueryVariants       []string               `json:"queryVariants,omitempty"`
	VertexHits          map[string][]vertexHit `json:"vertexHits,omitempty"`
	Artifacts           []artifactSummary      `json:"artifacts,omitempty"`
	Routes              []routeSummary         `json:"routes,omitempty"`
	Products            []productSummary       `json:"products,omitempty"`
	Objects             []objectSummary        `json:"objects,omitempty"`
	Relations           []relationSummary      `json:"relations,omitempty"`
	Facts               []factSummary          `json:"facts,omitempty"`
	Gaps                []gapSummary           `json:"gaps,omitempty"`
	Narrative           string                 `json:"narrative,omitempty"`
	Warnings            []string               `json:"warnings,omitempty"`
	StructuredProvenance map[string]any        `json:"structuredProvenance,omitempty"`
}

type vertexHit struct {
	Store       string                 `json:"store"`
	ID          string                 `json:"id"`
	Title       string                 `json:"title,omitempty"`
	URI         string                 `json:"uri,omitempty"`
	Score       float64                `json:"score"`
	StructData  map[string]interface{} `json:"structData,omitempty"`
}

type graphBundle struct {
	Anchor       *hmodels.HorizonFragment
	Lens         *hmodels.Lens
	Fragments    map[string]*hmodels.HorizonFragment
	Edges        map[string]*hmodels.DependencyEdge
	Artifacts    map[string]*hmodels.MaterializedArtifact
	Facts        map[string]*coremodels.OntologyFact
	SupplyLinks  map[string]*hmodels.HorizonFragment
	Warnings     []string
}

type artifactSummary struct {
	ID            string   `json:"id"`
	CanonicalKey  string   `json:"canonicalKey"`
	DisplayName   string   `json:"displayName,omitempty"`
	Kind          string   `json:"kind"`
	PathStatus    string   `json:"pathStatus,omitempty"`
	AnchorSupplier string  `json:"anchorSupplier,omitempty"`
	AnchorCustomer string  `json:"anchorCustomer,omitempty"`
	ProductAxis   string   `json:"productAxis,omitempty"`
	NodeRefs      []string `json:"nodeRefs,omitempty"`
	GeoRefs       []string `json:"geoRefs,omitempty"`
	FlowRefs      []string `json:"flowRefs,omitempty"`
	ZoneRefs      []string `json:"zoneRefs,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
}

type routeSummary struct {
	ArtifactID           string   `json:"artifactId,omitempty"`
	ArtifactCanonicalKey string   `json:"artifactCanonicalKey,omitempty"`
	LegIndex             int      `json:"legIndex"`
	SupplierRef          string   `json:"supplierRef,omitempty"`
	CustomerRef          string   `json:"customerRef,omitempty"`
	ProductRef           string   `json:"productRef,omitempty"`
	SupplyLinkRef        string   `json:"supplyLinkRef,omitempty"`
	TransportViaRefs     []string `json:"transportViaRefs,omitempty"`
	Mode                 string   `json:"mode,omitempty"`
	VesselType           string   `json:"vesselType,omitempty"`
	FlowRefs             []string `json:"flowRefs,omitempty"`
	GeoRefs              []string `json:"geoRefs,omitempty"`
	ZoneRefs             []string `json:"zoneRefs,omitempty"`
	AffectedZoneRefs     []string `json:"affectedZoneRefs,omitempty"`
	RouteGeometrySource  string   `json:"routeGeometrySource,omitempty"`
	RouteCoordinatesN    int      `json:"routeCoordinatesN,omitempty"`
}

type productSummary struct {
	ProductRef string `json:"productRef"`
	UsedBy     int    `json:"usedBy"`
}

type objectSummary struct {
	CanonicalPath string `json:"canonicalPath"`
	Type          string `json:"type,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
}

type relationSummary struct {
	EdgeID      string  `json:"edgeId"`
	EdgeType    string  `json:"edgeType"`
	SourcePath  string  `json:"sourcePath,omitempty"`
	SourceLabel string  `json:"sourceLabel,omitempty"`
	TargetPath  string  `json:"targetPath,omitempty"`
	TargetLabel string  `json:"targetLabel,omitempty"`
	Score       float64 `json:"score,omitempty"`
}

type factSummary struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind,omitempty"`
	Type           string   `json:"type,omitempty"`
	Title          string   `json:"title,omitempty"`
	CanonicalPaths []string `json:"canonicalPaths,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
}

type gapSummary struct {
	Code    string `json:"code"`
	Details string `json:"details"`
}

func main() {
	cfg := parseFlags()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	deps, err := openDeps(ctx, cfg)
	if err != nil {
		log.Fatalf("init deps failed: %v", err)
	}
	defer deps.close()

	switch cfg.mode {
	case "seed-anchor":
		report, err := runSeedAnchor(ctx, deps, cfg)
		if err != nil {
			log.Fatalf("seed-anchor failed: %v", err)
		}
		writeOutput(cfg.outFile, report)
	case "answer":
		report, err := runAnswer(ctx, deps, cfg)
		if err != nil {
			log.Fatalf("answer failed: %v", err)
		}
		writeOutput(cfg.outFile, report)
	default:
		log.Fatalf("unsupported mode: %s", cfg.mode)
	}
}

type deps struct {
	fs          *gcp.FirestoreClient
	gcs         *gcp.GCSClient
	fragments   *hrepo.HorizonFragmentsRepository
	edges       *hrepo.DependencyEdgesRepository
	artifacts   *hrepo.MaterializedArtifactsRepository
	lenses      *hrepo.LensesRepository
	facts       *corerepo.OntologyFactsRepository
	vertex      *ai.VertexAISearchClient
	vertexModel *ai.VertexAIService
}

func (d *deps) close() {
	if d == nil || d.fs == nil {
		return
	}
	_ = d.fs.Close()
}

func openDeps(ctx context.Context, cfg config) (*deps, error) {
	fs, err := gcp.NewFirestoreClient(ctx, cfg.projectID)
	if err != nil {
		return nil, err
	}
	gcsClient, err := gcp.NewGCSClient(ctx, cfg.projectID)
	if err != nil {
		_ = fs.Close()
		return nil, err
	}
	vertexClient := ai.NewVertexAISearchClient(cfg.projectID, cfg.vertexLocation, firstNonEmpty(cfg.nodeStoreID, cfg.artifactStoreID, cfg.factStoreID), "")
	mm := ai.NewAIModelManager()
	vertexSvc, err := ai.NewVertexAIService(cfg.projectID, cfg.vertexLocation, mm)
	if err != nil {
		_ = fs.Close()
		return nil, err
	}
	return &deps{
		fs:          fs,
		gcs:         gcsClient,
		fragments:   hrepo.NewHorizonFragmentsRepository(fs.Client()),
		edges:       hrepo.NewDependencyEdgesRepository(fs.Client()),
		artifacts:   hrepo.NewMaterializedArtifactsRepository(fs.Client()),
		lenses:      hrepo.NewLensesRepository(fs.Client()),
		facts:       corerepo.NewOntologyFactsRepository(fs.Client()),
		vertex:      vertexClient,
		vertexModel: vertexSvc,
	}, nil
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.mode, "mode", "answer", "seed-anchor|answer")
	flag.StringVar(&cfg.projectID, "project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	flag.StringVar(&cfg.tenantID, "tenant", "global", "tenant id")
	flag.StringVar(&cfg.anchorPath, "anchor-path", "", "anchor canonical path")
	flag.StringVar(&cfg.anchorQuery, "anchor-query", "Diego Garcia", "anchor name query")
	flag.StringVar(&cfg.lensID, "lens-id", "", "optional lens id")
	flag.StringVar(&cfg.question, "question", "Как устроена логистика базы Diego Garcia?", "question to answer")
	flag.StringVar(&cfg.bucket, "bucket", firstNonEmpty(os.Getenv("ETIGRAM_FRAGMENTS_BUCKET"), os.Getenv("HORIZON_FRAGMENTS_BUCKET"), ""), "GCS bucket for selective export")
	flag.StringVar(&cfg.vertexLocation, "vertex-location", firstNonEmpty(os.Getenv("VERTEX_SEARCH_LOCATION"), "global"), "Vertex AI Search location")
	flag.StringVar(&cfg.nodeStoreID, "node-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"), ""), "Vertex datastore for fragments")
	flag.StringVar(&cfg.edgeStoreID, "edge-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_ETIGRAM_EDGE_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_EDGES_DATA_STORE_ID"), ""), "Vertex datastore for edges")
	flag.StringVar(&cfg.artifactStoreID, "artifact-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_HORIZON_ARTIFACTS_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"), ""), "Vertex datastore for artifacts")
	flag.StringVar(&cfg.factStoreID, "fact-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_ONTOLOGY_FACTS_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"), ""), "Vertex datastore for ontology facts")
	flag.IntVar(&cfg.topK, "top-k", 6, "top results per query/store")
	flag.IntVar(&cfg.limitArtifacts, "limit-artifacts", 24, "max artifacts")
	flag.IntVar(&cfg.limitEdges, "limit-edges", 160, "max edges")
	flag.IntVar(&cfg.limitFacts, "limit-facts", 80, "max facts")
	flag.IntVar(&cfg.limitFragments, "limit-fragments", 120, "max fragments")
	flag.BoolVar(&cfg.useLLM, "use-llm", true, "use Vertex model for final synthesis")
	flag.StringVar(&cfg.outFile, "out", "", "optional output json path")
	flag.Parse()
	if strings.TrimSpace(cfg.bucket) == "" && strings.TrimSpace(cfg.projectID) != "" {
		cfg.bucket = cfg.projectID + "-etigram-fragments"
	}
	if strings.TrimSpace(cfg.nodeStoreID) == "" {
		cfg.nodeStoreID = "etigram-fragments-store"
	}
	if strings.TrimSpace(cfg.edgeStoreID) == "" {
		cfg.edgeStoreID = "etigram-edges-store"
	}
	if strings.TrimSpace(cfg.artifactStoreID) == "" {
		cfg.artifactStoreID = cfg.nodeStoreID
	}
	return cfg
}

func runSeedAnchor(ctx context.Context, d *deps, cfg config) (*seedReport, error) {
	bundle, err := buildAnchorBundle(ctx, d, cfg)
	if err != nil {
		return nil, err
	}
	report := &seedReport{
		Mode:       cfg.mode,
		ProjectID:  cfg.projectID,
		TenantID:   cfg.tenantID,
		AnchorPath: bundle.Anchor.CanonicalPath,
		AnchorLabel: fragmentDisplayName(bundle.Anchor),
		ImportedURIs: map[string]int{},
		Operations: map[string]string{},
		Warnings: append([]string(nil), bundle.Warnings...),
		Stores: []vertexStore{
			{Name: "nodes", DataStoreID: cfg.nodeStoreID},
			{Name: "edges", DataStoreID: cfg.edgeStoreID},
			{Name: "artifacts", DataStoreID: cfg.artifactStoreID},
			{Name: "facts", DataStoreID: cfg.factStoreID},
		},
	}
	if bundle.Lens != nil {
		report.LensID = bundle.Lens.ID
		report.LensName = bundle.Lens.Name
	}
	if strings.TrimSpace(cfg.bucket) == "" {
		report.Warnings = append(report.Warnings, "bucket is empty; nothing exported")
		report.SelectedArtifacts = summarizeArtifacts(bundle.Artifacts)
		report.SelectedRoutes = summarizeRoutes(bundle.Artifacts)
		return report, nil
	}

	storeURIs := map[string][]string{
		"nodes":     {},
		"edges":     {},
		"artifacts": {},
		"facts":     {},
	}
	for _, fragment := range sortedFragments(bundle.Fragments) {
		uri, err := exportFragmentDoc(ctx, d.gcs, cfg.bucket, fragment)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("fragment export failed: %s: %v", fragment.CanonicalPath, err))
			continue
		}
		report.ExportedFragments++
		storeURIs["nodes"] = append(storeURIs["nodes"], uri)
	}
	for _, edge := range sortedEdges(bundle.Edges) {
		uri, err := exportEdgeDoc(ctx, d.gcs, cfg.bucket, d.fragments, edge)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("edge export failed: %s: %v", edge.ID, err))
			continue
		}
		report.ExportedEdges++
		storeURIs["edges"] = append(storeURIs["edges"], uri)
	}
	for _, artifact := range sortedArtifacts(bundle.Artifacts) {
		uri, err := exportArtifactDoc(ctx, d.gcs, cfg.bucket, artifact)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("artifact export failed: %s: %v", artifact.CanonicalKey, err))
			continue
		}
		report.ExportedArtifacts++
		storeURIs["artifacts"] = append(storeURIs["artifacts"], uri)
	}
	for _, fact := range sortedFacts(bundle.Facts) {
		uri, err := exportFactDoc(ctx, d.gcs, cfg.bucket, fact)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("fact export failed: %s: %v", fact.ID, err))
			continue
		}
		report.ExportedFacts++
		storeURIs["facts"] = append(storeURIs["facts"], uri)
	}

	for storeName, uris := range storeURIs {
		report.ImportedURIs[storeName] = len(uris)
	}

	ops, warns := importToVertex(ctx, d.vertex, cfg, storeURIs)
	report.Operations = ops
	report.Warnings = append(report.Warnings, warns...)
	report.SelectedArtifacts = summarizeArtifacts(bundle.Artifacts)
	report.SelectedRoutes = summarizeRoutes(bundle.Artifacts)
	return report, nil
}

func runAnswer(ctx context.Context, d *deps, cfg config) (*answerReport, error) {
	bundle, err := buildAnchorBundle(ctx, d, cfg)
	if err != nil {
		return nil, err
	}
	queryVariants := buildQueryVariants(cfg.question, bundle.Anchor)
	vertexHits, warns := searchVertex(ctx, d.vertex, cfg, queryVariants)
	bundle.Warnings = append(bundle.Warnings, warns...)

	// Vertex is the primary recall layer. Graph hydrate then expands only around the anchor bundle.
	augmentBundleFromVertexHits(ctx, d, cfg, bundle, vertexHits)

	report := &answerReport{
		Mode:         cfg.mode,
		ProjectID:    cfg.projectID,
		TenantID:     cfg.tenantID,
		Question:     cfg.question,
		AnchorPath:   bundle.Anchor.CanonicalPath,
		AnchorLabel:  fragmentDisplayName(bundle.Anchor),
		QueryVariants: queryVariants,
		VertexHits:   vertexHits,
		Artifacts:    summarizeArtifacts(bundle.Artifacts),
		Routes:       summarizeRoutes(bundle.Artifacts),
		Products:     summarizeProducts(bundle.Artifacts),
		Objects:      summarizeObjects(bundle.Fragments),
		Relations:    summarizeRelations(ctx, d.fragments, bundle.Edges),
		Facts:        summarizeFacts(bundle.Facts),
		Gaps:         detectGaps(bundle),
		Warnings:     append([]string(nil), bundle.Warnings...),
		StructuredProvenance: map[string]any{
			"fragmentCount": len(bundle.Fragments),
			"edgeCount":     len(bundle.Edges),
			"artifactCount": len(bundle.Artifacts),
			"factCount":     len(bundle.Facts),
		},
	}
	if bundle.Lens != nil {
		report.LensID = bundle.Lens.ID
		report.LensName = bundle.Lens.Name
	}
	report.Narrative = buildDeterministicNarrative(report)
	if cfg.useLLM {
		if text, err := synthesizeNarrative(ctx, d.vertexModel, cfg, report); err == nil && strings.TrimSpace(text) != "" {
			report.Narrative = strings.TrimSpace(text)
		} else if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("llm synthesis failed: %v", err))
		}
	}
	return report, nil
}

func buildAnchorBundle(ctx context.Context, d *deps, cfg config) (*graphBundle, error) {
	anchor, err := resolveAnchor(ctx, d, cfg)
	if err != nil {
		return nil, err
	}
	lens, err := resolveLens(ctx, d, cfg, anchor.CanonicalPath)
	if err != nil {
		return nil, err
	}
	bundle := &graphBundle{
		Anchor:      anchor,
		Lens:        lens,
		Fragments:   map[string]*hmodels.HorizonFragment{anchor.CanonicalPath: anchor},
		Edges:       map[string]*hmodels.DependencyEdge{},
		Artifacts:   map[string]*hmodels.MaterializedArtifact{},
		Facts:       map[string]*coremodels.OntologyFact{},
		SupplyLinks: map[string]*hmodels.HorizonFragment{},
	}

	artifacts, err := collectAnchorArtifacts(ctx, d, cfg, anchor, lens)
	if err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		bundle.Artifacts[artifact.ID] = artifact
	}

	nodeRefs := map[string]struct{}{anchor.CanonicalPath: {}}
	supplyLinkRefs := map[string]struct{}{}
	if lens != nil {
		scopePaths := workscope.LensWorkbenchScopePaths(lens)
		if resolvedScope, err := workscope.ResolveLensMapWorkScope(ctx, lens, cfg.tenantID, scopePaths, d.fragments, d.edges); err == nil {
			refs := workscope.ToBaselineRefs(resolvedScope)
			for _, cp := range refs.ScopePaths {
				if strings.TrimSpace(cp) != "" {
					nodeRefs[strings.TrimSpace(cp)] = struct{}{}
				}
			}
			if len(refs.FragmentIDs) > 0 {
				if fragmentsByID, err := d.fragments.GetMany(ctx, refs.FragmentIDs); err == nil {
					for _, fragment := range fragmentsByID {
						if fragment == nil {
							continue
						}
						bundle.Fragments[fragment.CanonicalPath] = fragment
						nodeRefs[fragment.CanonicalPath] = struct{}{}
					}
				}
			}
		} else {
			bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("workscope resolve failed: %v", err))
		}
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		for _, ref := range artifact.NodeRefs {
			if strings.TrimSpace(ref) != "" {
				nodeRefs[strings.TrimSpace(ref)] = struct{}{}
			}
		}
		if parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			for _, leg := range parsed.Legs {
				if strings.TrimSpace(leg.SupplyLinkCanonicalPath) != "" {
					supplyLinkRefs[strings.TrimSpace(leg.SupplyLinkCanonicalPath)] = struct{}{}
				}
				if strings.TrimSpace(leg.SupplierRef) != "" {
					nodeRefs[strings.TrimSpace(leg.SupplierRef)] = struct{}{}
				}
				if strings.TrimSpace(leg.CustomerRef) != "" {
					nodeRefs[strings.TrimSpace(leg.CustomerRef)] = struct{}{}
				}
				if strings.TrimSpace(leg.ProductRef) != "" {
					nodeRefs[strings.TrimSpace(leg.ProductRef)] = struct{}{}
				}
				for _, via := range leg.TransportViaRefs {
					if strings.TrimSpace(via) != "" {
						nodeRefs[strings.TrimSpace(via)] = struct{}{}
					}
				}
			}
		}
	}
	fragmentsByPath, err := d.fragments.BatchGetByCanonicalPaths(ctx, mapKeys(nodeRefs))
	if err != nil {
		return nil, err
	}
	for cp, fragment := range fragmentsByPath {
		if fragment == nil {
			continue
		}
		bundle.Fragments[cp] = fragment
	}
	if len(supplyLinkRefs) > 0 {
		if links, err := d.fragments.BatchGetSupplyLinksByCanonicalPaths(ctx, mapKeys(supplyLinkRefs)); err == nil {
			for cp, fragment := range links {
				if fragment == nil {
					continue
				}
				bundle.Fragments[cp] = fragment
				bundle.SupplyLinks[cp] = fragment
			}
		}
	}

	addEdge := func(edge *hmodels.DependencyEdge) {
		if edge == nil || strings.TrimSpace(edge.ID) == "" {
			return
		}
		if len(bundle.Edges) >= cfg.limitEdges {
			return
		}
		bundle.Edges[edge.ID] = edge
	}
	for _, fragment := range bundle.Fragments {
		if fragment == nil || len(bundle.Edges) >= cfg.limitEdges {
			continue
		}
		outgoing, _ := d.edges.ListBySource(ctx, cfg.tenantID, fragment.ID, nil, minInt(24, cfg.limitEdges))
		incoming, _ := d.edges.ListByTarget(ctx, cfg.tenantID, fragment.ID, nil, minInt(24, cfg.limitEdges))
		for _, edge := range outgoing {
			addEdge(edge)
		}
		for _, edge := range incoming {
			addEdge(edge)
		}
	}
	for _, edge := range bundle.Edges {
		if edge == nil {
			continue
		}
		if fragment, err := d.fragments.Get(ctx, strings.TrimSpace(edge.SourceFragmentID)); err == nil && fragment != nil {
			bundle.Fragments[fragment.CanonicalPath] = fragment
		}
		if fragment, err := d.fragments.Get(ctx, strings.TrimSpace(edge.TargetFragmentID)); err == nil && fragment != nil {
			bundle.Fragments[fragment.CanonicalPath] = fragment
		}
	}
	for _, fragment := range bundle.Fragments {
		if fragment == nil || len(bundle.Edges) >= cfg.limitEdges {
			continue
		}
		outgoing, _ := d.edges.ListBySource(ctx, cfg.tenantID, fragment.ID, nil, 8)
		incoming, _ := d.edges.ListByTarget(ctx, cfg.tenantID, fragment.ID, nil, 8)
		for _, edge := range outgoing {
			addEdge(edge)
		}
		for _, edge := range incoming {
			addEdge(edge)
		}
	}
	for _, cp := range mapKeys(bundle.Fragments) {
		facts, err := d.facts.ListByCanonicalPath(ctx, cfg.tenantID, cp, "", minInt(12, cfg.limitFacts))
		if err != nil {
			continue
		}
		for _, fact := range facts {
			if fact == nil || strings.TrimSpace(fact.ID) == "" {
				continue
			}
			if len(bundle.Facts) >= cfg.limitFacts {
				break
			}
			bundle.Facts[fact.ID] = fact
		}
	}
	return bundle, nil
}

func resolveAnchor(ctx context.Context, d *deps, cfg config) (*hmodels.HorizonFragment, error) {
	if strings.TrimSpace(cfg.anchorPath) != "" {
		return d.fragments.GetByCanonicalPathForTenant(ctx, cfg.tenantID, strings.TrimSpace(cfg.anchorPath))
	}
	query := strings.TrimSpace(cfg.anchorQuery)
	if query == "" {
		query = "Diego Garcia"
	}
	results, err := d.fragments.SearchPrefix(ctx, query, 20)
	if err != nil {
		return nil, err
	}
	for _, fragment := range results {
		if fragment == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(fragmentDisplayName(fragment)), query) || strings.Contains(strings.ToLower(fragmentDisplayName(fragment)), strings.ToLower(query)) {
			return fragment, nil
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("anchor not found for query %q", query)
	}
	return results[0], nil
}

func resolveLens(ctx context.Context, d *deps, cfg config, anchorPath string) (*hmodels.Lens, error) {
	if strings.TrimSpace(cfg.lensID) != "" {
		return d.lenses.Get(ctx, strings.TrimSpace(cfg.lensID))
	}
	lenses, err := d.lenses.FindByScopeAnyTenant(ctx, anchorPath, 32)
	if err == nil && len(lenses) > 0 {
		sort.Slice(lenses, func(i, j int) bool {
			return lenses[i].UpdatedAt.After(lenses[j].UpdatedAt)
		})
		return lenses[0], nil
	}
	active, err := d.lenses.ListActive(ctx, cfg.tenantID, 256)
	if err != nil || len(active) == 0 {
		return nil, nil
	}
	needleParts := compactUnique([]string{
		strings.ToLower(strings.TrimSpace(cfg.anchorQuery)),
		strings.ToLower(strings.TrimSpace(anchorPath)),
		strings.ToLower(strings.Trim(strings.TrimPrefix(anchorPath, "/"), "/")),
		lastPathToken(anchorPath),
	})
	var best *hmodels.Lens
	bestScore := 0
	for _, lens := range active {
		if lens == nil {
			continue
		}
		score := lensMatchScore(lens, needleParts)
		if score <= 0 {
			continue
		}
		if best == nil || score > bestScore || (score == bestScore && lens.UpdatedAt.After(best.UpdatedAt)) {
			best = lens
			bestScore = score
		}
	}
	return best, nil
}

func collectAnchorArtifacts(ctx context.Context, d *deps, cfg config, anchor *hmodels.HorizonFragment, lens *hmodels.Lens) ([]*hmodels.MaterializedArtifact, error) {
	artifacts := make([]*hmodels.MaterializedArtifact, 0, cfg.limitArtifacts)
	seen := map[string]struct{}{}
	push := func(items []*hmodels.MaterializedArtifact) {
		for _, artifact := range items {
			if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
				continue
			}
			if _, ok := seen[artifact.ID]; ok {
				continue
			}
			seen[artifact.ID] = struct{}{}
			artifacts = append(artifacts, artifact)
			if len(artifacts) >= cfg.limitArtifacts {
				return
			}
		}
	}
	byNode, err := d.artifacts.FindByNodeRefAnyTenant(ctx, anchor.CanonicalPath, hmodels.MaterializedKindSupplyChainPath, cfg.limitArtifacts)
	if err == nil {
		push(byNode)
	}
	byGeo, err := d.artifacts.FindByGeoRefsAnyTenant(ctx, []string{anchor.CanonicalPath}, hmodels.MaterializedKindSupplyChainPath, cfg.limitArtifacts)
	if err == nil {
		push(byGeo)
	}
	byZone, err := d.artifacts.FindByZoneRefsAnyTenant(ctx, []string{anchor.CanonicalPath}, hmodels.MaterializedKindSupplyChainPath, cfg.limitArtifacts)
	if err == nil {
		push(byZone)
	}
	byAffectedZone, err := d.artifacts.FindByAffectedZoneRefsAnyTenant(ctx, []string{anchor.CanonicalPath}, hmodels.MaterializedKindSupplyChainPath, cfg.limitArtifacts)
	if err == nil {
		push(byAffectedZone)
	}
	if lens != nil && len(artifacts) < cfg.limitArtifacts {
		byLens, err := d.artifacts.ListByLens(ctx, lens.ID, cfg.limitArtifacts*2)
		if err == nil {
			filtered := make([]*hmodels.MaterializedArtifact, 0, len(byLens))
			for _, artifact := range byLens {
				if artifact == nil {
					continue
				}
				if containsString(artifact.NodeRefs, anchor.CanonicalPath) || containsString(artifact.GeoRefs, anchor.CanonicalPath) || strings.Contains(strings.ToLower(artifact.DisplayName), strings.ToLower(fragmentDisplayName(anchor))) {
					filtered = append(filtered, artifact)
				}
			}
			push(filtered)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].UpdatedAt.After(artifacts[j].UpdatedAt)
	})
	if len(artifacts) > cfg.limitArtifacts {
		artifacts = artifacts[:cfg.limitArtifacts]
	}
	return artifacts, nil
}

func searchVertex(ctx context.Context, client *ai.VertexAISearchClient, cfg config, variants []string) (map[string][]vertexHit, []string) {
	out := map[string][]vertexHit{
		"nodes":     {},
		"edges":     {},
		"artifacts": {},
		"facts":     {},
	}
	stores := map[string]string{
		"nodes":     cfg.nodeStoreID,
		"edges":     cfg.edgeStoreID,
		"artifacts": cfg.artifactStoreID,
		"facts":     cfg.factStoreID,
	}
	var (
		mu    = make(chan struct{}, 1)
		warns []string
	)
	mu <- struct{}{}
	appendWarn := func(msg string) {
		<-mu
		warns = append(warns, msg)
		mu <- struct{}{}
	}

	var eg errgroup.Group
	for storeName, storeID := range stores {
		if strings.TrimSpace(storeID) == "" {
			continue
		}
		for _, variant := range variants {
			storeName := storeName
			storeID := storeID
			variant := variant
			eg.Go(func() error {
				resp, err := client.WithDataStore(storeID).Search(ctx, &ai.SearchRequest{
					Query:    variant,
					PageSize: cfg.topK,
				})
				if err != nil {
					appendWarn(fmt.Sprintf("vertex search failed store=%s query=%q: %v", storeName, variant, err))
					return nil
				}
				hits := make([]vertexHit, 0, len(resp.Results))
				for _, res := range resp.Results {
					hit := vertexHit{
						Store:      storeName,
						ID:         vertexDocID(res),
						Title:      strings.TrimSpace(res.Document.Title),
						URI:        strings.TrimSpace(firstNonEmpty(res.Document.URI, asString(res.Document.StructData["uri"]))),
						Score:      vertexScore(res),
						StructData: cloneMap(res.Document.StructData),
					}
					if hit.ID == "" && hit.URI == "" {
						continue
					}
					hits = append(hits, hit)
				}
				<-mu
				out[storeName] = mergeVertexHits(out[storeName], hits, cfg.topK*3)
				mu <- struct{}{}
				return nil
			})
		}
	}
	_ = eg.Wait()
	return out, warns
}

func augmentBundleFromVertexHits(ctx context.Context, d *deps, cfg config, bundle *graphBundle, hits map[string][]vertexHit) {
	for _, hit := range hits["nodes"] {
		if fragment, err := d.fragments.Get(ctx, hit.ID); err == nil && fragment != nil {
			if len(bundle.Fragments) < cfg.limitFragments {
				bundle.Fragments[fragment.CanonicalPath] = fragment
			}
		}
	}
	for _, hit := range hits["edges"] {
		if edge, err := d.edges.Get(ctx, hit.ID); err == nil && edge != nil {
			if len(bundle.Edges) < cfg.limitEdges {
				bundle.Edges[edge.ID] = edge
			}
		}
	}
	for _, hit := range hits["artifacts"] {
		if artifact, err := d.artifacts.Get(ctx, hit.ID); err == nil && artifact != nil {
			if len(bundle.Artifacts) < cfg.limitArtifacts {
				bundle.Artifacts[artifact.ID] = artifact
			}
		}
	}
	for _, hit := range hits["facts"] {
		if fact, err := d.facts.Get(ctx, hit.ID); err == nil && fact != nil {
			if len(bundle.Facts) < cfg.limitFacts {
				bundle.Facts[fact.ID] = fact
			}
		}
	}
}

func exportFragmentDoc(ctx context.Context, gcsClient *gcp.GCSClient, bucket string, fragment *hmodels.HorizonFragment) (string, error) {
	content := fragment.GenerateVectorText()
	objectPath := fmt.Sprintf("%s/test_internal_knowledge/fragments/%s.json", fragment.TenantID, fragment.ID)
	uri := fmt.Sprintf("gs://%s/%s", bucket, objectPath)
	doc := map[string]any{
		"id": fragment.ID,
		"content": map[string]any{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(content)),
		},
		"structData": map[string]any{
			"tenantId":      fragment.TenantID,
			"type":          "horizon_fragment",
			"fragmentType":  string(fragment.Type),
			"canonicalPath": fragment.CanonicalPath,
			"displayName":   fragmentDisplayName(fragment),
			"labels":        fragment.Labels,
			"uri":           uri,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	err = gcsClient.UploadBytes(ctx, bucket, objectPath, body, "application/json", map[string]string{
		"type":     "horizon_fragment",
		"tenantId": fragment.TenantID,
	})
	return uri, err
}

func exportEdgeDoc(ctx context.Context, gcsClient *gcp.GCSClient, bucket string, fragments *hrepo.HorizonFragmentsRepository, edge *hmodels.DependencyEdge) (string, error) {
	sourcePath, targetPath, sourceLabel, targetLabel := resolveEdgeEndpoints(ctx, fragments, edge)
	content := strings.TrimSpace(fmt.Sprintf("%s [%s] %s. %s -> %s.", firstNonEmpty(sourceLabel, sourcePath), edge.Type, firstNonEmpty(targetLabel, targetPath), sourcePath, targetPath))
	objectPath := fmt.Sprintf("%s/test_internal_knowledge/edges/%s.json", edge.TenantID, edge.ID)
	uri := fmt.Sprintf("gs://%s/%s", bucket, objectPath)
	doc := map[string]any{
		"id": edge.ID,
		"content": map[string]any{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(content)),
		},
		"structData": map[string]any{
			"tenantId":         edge.TenantID,
			"type":             "horizon_edge",
			"edgeType":         string(edge.Type),
			"sourceFragmentId": edge.SourceFragmentID,
			"targetFragmentId": edge.TargetFragmentID,
			"sourcePath":       sourcePath,
			"targetPath":       targetPath,
			"sourceLabel":      sourceLabel,
			"targetLabel":      targetLabel,
			"qualifiers":       edge.Qualifiers,
			"uri":              uri,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	err = gcsClient.UploadBytes(ctx, bucket, objectPath, body, "application/json", map[string]string{
		"type":     "horizon_edge",
		"tenantId": edge.TenantID,
		"edgeType": string(edge.Type),
	})
	return uri, err
}

func exportArtifactDoc(ctx context.Context, gcsClient *gcp.GCSClient, bucket string, artifact *hmodels.MaterializedArtifact) (string, error) {
	content := artifact.GenerateVectorText()
	objectPath := fmt.Sprintf("%s/test_internal_knowledge/artifacts/%s.json", artifact.TenantID, artifact.ID)
	uri := fmt.Sprintf("gs://%s/%s", bucket, objectPath)
	doc := map[string]any{
		"id": artifact.ID,
		"content": map[string]any{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(content)),
		},
		"structData": map[string]any{
			"tenantId":         artifact.TenantID,
			"type":             "supply_chain",
			"kind":             string(artifact.Kind),
			"displayName":      artifact.DisplayName,
			"canonicalKey":     artifact.CanonicalKey,
			"nodeRefs":         artifact.NodeRefs,
			"geoRefs":          artifact.GeoRefs,
			"flowRefs":         artifact.FlowRefs,
			"zoneRefs":         artifact.ZoneRefs,
			"affectedZoneRefs": artifact.AffectedZoneRefs,
			"pathStatus":       artifact.PathStatus,
			"uri":              uri,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	err = gcsClient.UploadBytes(ctx, bucket, objectPath, body, "application/json", map[string]string{
		"type":     "supply_chain",
		"tenantId": artifact.TenantID,
	})
	return uri, err
}

func exportFactDoc(ctx context.Context, gcsClient *gcp.GCSClient, bucket string, fact *coremodels.OntologyFact) (string, error) {
	vectorText := fact.VectorText
	if strings.TrimSpace(vectorText) == "" {
		vectorText = fact.GenerateVectorText()
	}
	objectPath := fmt.Sprintf("%s/test_internal_knowledge/facts/%s.json", fact.TenantID, fact.ID)
	uri := fmt.Sprintf("gs://%s/%s", bucket, objectPath)
	doc := map[string]any{
		"id": fact.ID,
		"content": map[string]any{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(vectorText)),
		},
		"structData": map[string]any{
			"tenantId":       fact.TenantID,
			"type":           "ontology_fact",
			"kind":           fact.Kind,
			"factType":       fact.Type,
			"title":          fact.Title,
			"canonicalPaths": fact.CanonicalPaths,
			"actorPaths":     fact.ActorPaths,
			"uri":            uri,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	err = gcsClient.UploadBytes(ctx, bucket, objectPath, body, "application/json", map[string]string{
		"type":     "ontology_fact",
		"tenantId": fact.TenantID,
		"kind":     fact.Kind,
	})
	return uri, err
}

func importToVertex(ctx context.Context, client *ai.VertexAISearchClient, cfg config, storeURIs map[string][]string) (map[string]string, []string) {
	out := map[string]string{}
	var warns []string
	targets := map[string]string{
		"nodes":     cfg.nodeStoreID,
		"edges":     cfg.edgeStoreID,
		"artifacts": cfg.artifactStoreID,
		"facts":     cfg.factStoreID,
	}
	for storeName, storeID := range targets {
		if strings.TrimSpace(storeID) == "" || len(storeURIs[storeName]) == 0 {
			continue
		}
		op, err := client.WithDataStore(storeID).ImportDocumentsFromURIs(ctx, compactUnique(storeURIs[storeName]), "INCREMENTAL", "document")
		if err != nil {
			warns = append(warns, fmt.Sprintf("vertex import failed store=%s: %v", storeName, err))
			continue
		}
		out[storeName] = op
	}
	return out, warns
}

func summarizeArtifacts(items map[string]*hmodels.MaterializedArtifact) []artifactSummary {
	out := make([]artifactSummary, 0, len(items))
	for _, artifact := range sortedArtifacts(items) {
		out = append(out, artifactSummary{
			ID:             strings.TrimSpace(artifact.ID),
			CanonicalKey:   strings.TrimSpace(artifact.CanonicalKey),
			DisplayName:    strings.TrimSpace(artifact.DisplayName),
			Kind:           string(artifact.Kind),
			PathStatus:     strings.TrimSpace(artifact.PathStatus),
			AnchorSupplier: strings.TrimSpace(artifact.AnchorSupplier),
			AnchorCustomer: strings.TrimSpace(artifact.AnchorCustomer),
			ProductAxis:    strings.TrimSpace(artifact.ProductAxis),
			NodeRefs:       compactUnique(artifact.NodeRefs),
			GeoRefs:        compactUnique(artifact.GeoRefs),
			FlowRefs:       compactUnique(artifact.FlowRefs),
			ZoneRefs:       compactUnique(artifact.ZoneRefs),
			UpdatedAt:      artifact.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func summarizeRoutes(items map[string]*hmodels.MaterializedArtifact) []routeSummary {
	out := make([]routeSummary, 0, 64)
	for _, artifact := range sortedArtifacts(items) {
		parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			out = append(out, routeSummary{
				ArtifactID:           artifact.ID,
				ArtifactCanonicalKey: artifact.CanonicalKey,
				LegIndex:             leg.Index,
				SupplierRef:          strings.TrimSpace(leg.SupplierRef),
				CustomerRef:          strings.TrimSpace(leg.CustomerRef),
				ProductRef:           strings.TrimSpace(leg.ProductRef),
				SupplyLinkRef:        strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				TransportViaRefs:     compactUnique(leg.TransportViaRefs),
				Mode:                 strings.TrimSpace(leg.Mode),
				VesselType:           strings.TrimSpace(leg.VesselType),
				FlowRefs:             compactUnique(leg.FlowRefs),
				GeoRefs:              compactUnique(leg.GeoRefs),
				ZoneRefs:             compactUnique(leg.ZoneRefs),
				AffectedZoneRefs:     compactUnique(leg.AffectedZoneRefs),
				RouteGeometrySource:  strings.TrimSpace(leg.RouteGeometrySource),
				RouteCoordinatesN:    len(leg.RouteCoordinates),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ArtifactCanonicalKey == out[j].ArtifactCanonicalKey {
			return out[i].LegIndex < out[j].LegIndex
		}
		return out[i].ArtifactCanonicalKey < out[j].ArtifactCanonicalKey
	})
	return out
}

func summarizeProducts(items map[string]*hmodels.MaterializedArtifact) []productSummary {
	counts := map[string]int{}
	for _, route := range summarizeRoutes(items) {
		if strings.TrimSpace(route.ProductRef) == "" {
			continue
		}
		counts[route.ProductRef]++
	}
	out := make([]productSummary, 0, len(counts))
	for ref, usedBy := range counts {
		out = append(out, productSummary{ProductRef: ref, UsedBy: usedBy})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UsedBy == out[j].UsedBy {
			return out[i].ProductRef < out[j].ProductRef
		}
		return out[i].UsedBy > out[j].UsedBy
	})
	return out
}

func summarizeObjects(items map[string]*hmodels.HorizonFragment) []objectSummary {
	out := make([]objectSummary, 0, len(items))
	for _, fragment := range sortedFragments(items) {
		out = append(out, objectSummary{
			CanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
			Type:          string(fragment.Type),
			DisplayName:   fragmentDisplayName(fragment),
		})
	}
	return out
}

func summarizeRelations(ctx context.Context, fragments *hrepo.HorizonFragmentsRepository, items map[string]*hmodels.DependencyEdge) []relationSummary {
	out := make([]relationSummary, 0, len(items))
	for _, edge := range sortedEdges(items) {
		sourcePath, targetPath, sourceLabel, targetLabel := resolveEdgeEndpoints(ctx, fragments, edge)
		out = append(out, relationSummary{
			EdgeID:      edge.ID,
			EdgeType:    string(edge.Type),
			SourcePath:  sourcePath,
			SourceLabel: sourceLabel,
			TargetPath:  targetPath,
			TargetLabel: targetLabel,
		})
	}
	return out
}

func summarizeFacts(items map[string]*coremodels.OntologyFact) []factSummary {
	out := make([]factSummary, 0, len(items))
	for _, fact := range sortedFacts(items) {
		out = append(out, factSummary{
			ID:             fact.ID,
			Kind:           strings.TrimSpace(fact.Kind),
			Type:           strings.TrimSpace(fact.Type),
			Title:          strings.TrimSpace(fact.Title),
			CanonicalPaths: compactUnique(fact.CanonicalPaths),
			UpdatedAt:      fact.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func detectGaps(bundle *graphBundle) []gapSummary {
	gaps := make([]gapSummary, 0, 6)
	if len(bundle.Artifacts) == 0 {
		gaps = append(gaps, gapSummary{Code: "missing_materialized_paths", Details: "No materialized supply-chain paths mention the anchor."})
	}
	routes := summarizeRoutes(bundle.Artifacts)
	if len(routes) == 0 {
		gaps = append(gaps, gapSummary{Code: "missing_route_legs", Details: "No route legs were found in materialized artifacts."})
	}
	missingVia := 0
	missingMode := 0
	for _, route := range routes {
		if len(route.TransportViaRefs) == 0 {
			missingVia++
		}
		if strings.TrimSpace(route.Mode) == "" {
			missingMode++
		}
	}
	if missingVia > 0 {
		gaps = append(gaps, gapSummary{Code: "route_detail_partial", Details: fmt.Sprintf("%d route legs have no transportVia refs.", missingVia)})
	}
	if missingMode > 0 {
		gaps = append(gaps, gapSummary{Code: "mode_partial", Details: fmt.Sprintf("%d route legs have no transport mode.", missingMode)})
	}
	if len(bundle.Edges) == 0 {
		gaps = append(gaps, gapSummary{Code: "missing_relations", Details: "No dependency edges were loaded around the anchor slice."})
	}
	return gaps
}

func buildDeterministicNarrative(report *answerReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Anchor: %s (%s).\n", firstNonEmpty(report.AnchorLabel, report.AnchorPath), report.AnchorPath)
	if len(report.Artifacts) > 0 {
		b.WriteString("Materialized paths:\n")
		for i, artifact := range report.Artifacts {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "- %s [%s] key=%s productAxis=%s\n", firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey), artifact.PathStatus, artifact.CanonicalKey, artifact.ProductAxis)
		}
	}
	if len(report.Routes) > 0 {
		b.WriteString("Routes:\n")
		for i, route := range report.Routes {
			if i >= 12 {
				break
			}
			fmt.Fprintf(&b, "- %s -> %s product=%s via=%s mode=%s supplyLink=%s\n",
				firstNonEmpty(route.SupplierRef, "?"),
				firstNonEmpty(route.CustomerRef, "?"),
				firstNonEmpty(route.ProductRef, "?"),
				strings.Join(route.TransportViaRefs, " -> "),
				firstNonEmpty(route.Mode, "unknown"),
				firstNonEmpty(route.SupplyLinkRef, "-"),
			)
		}
	}
	if len(report.Relations) > 0 {
		b.WriteString("Relations:\n")
		for i, rel := range report.Relations {
			if i >= 12 {
				break
			}
			fmt.Fprintf(&b, "- %s [%s] %s\n", firstNonEmpty(rel.SourcePath, rel.EdgeID), rel.EdgeType, firstNonEmpty(rel.TargetPath, "?"))
		}
	}
	if len(report.Gaps) > 0 {
		b.WriteString("Gaps:\n")
		for _, gap := range report.Gaps {
			fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
		}
	}
	return strings.TrimSpace(b.String())
}

func synthesizeNarrative(ctx context.Context, svc *ai.VertexAIService, cfg config, report *answerReport) (string, error) {
	if svc == nil {
		return "", fmt.Errorf("vertex ai service is nil")
	}
	payload, err := json.MarshalIndent(map[string]any{
		"question":  report.Question,
		"anchor":    report.AnchorPath,
		"artifacts": report.Artifacts,
		"routes":    report.Routes,
		"objects":   report.Objects,
		"relations": report.Relations,
		"facts":     report.Facts,
		"gaps":      report.Gaps,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := strings.TrimSpace(fmt.Sprintf(`
You answer only from the provided internal Horizon dossier. Do not invent missing facts.
Write in concise Russian.
Question: %s

Return:
1. Short direct answer.
2. Objects and how they are connected.
3. Products and routes if present.
4. Explicit gaps.

Internal dossier:
%s
`, report.Question, string(payload)))
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           "gemini-3-flash-preview",
			MinContextWindow:         32000,
			MaxOutputSize:            8192,
			MinCapabilityScore:       6,
			TextGenerationCapability: true,
		},
		TenantID: cfg.tenantID,
		Project:  cfg.projectID,
		UseCase:  "horizon.internal_knowledge_answer",
	}
	res, err := svc.ExecuteTask(ctx, task, "vertex-gemini-3-flash-preview")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Content), nil
}

func buildQueryVariants(question string, anchor *hmodels.HorizonFragment) []string {
	name := fragmentDisplayName(anchor)
	out := []string{
		question,
		name,
		name + " logistics",
		name + " supply chain",
		name + " routes transport",
		name + " base logistics",
	}
	if strings.TrimSpace(anchor.CanonicalPath) != "" {
		out = append(out, anchor.CanonicalPath)
	}
	return compactUnique(out)
}

func resolveEdgeEndpoints(ctx context.Context, fragments *hrepo.HorizonFragmentsRepository, edge *hmodels.DependencyEdge) (string, string, string, string) {
	sourcePath, targetPath := "", ""
	sourceLabel, targetLabel := "", ""
	if edge == nil {
		return sourcePath, targetPath, sourceLabel, targetLabel
	}
	if fragment, err := fragments.Get(ctx, strings.TrimSpace(edge.SourceFragmentID)); err == nil && fragment != nil {
		sourcePath = strings.TrimSpace(fragment.CanonicalPath)
		sourceLabel = fragmentDisplayName(fragment)
	}
	if fragment, err := fragments.Get(ctx, strings.TrimSpace(edge.TargetFragmentID)); err == nil && fragment != nil {
		targetPath = strings.TrimSpace(fragment.CanonicalPath)
		targetLabel = fragmentDisplayName(fragment)
	}
	return sourcePath, targetPath, sourceLabel, targetLabel
}

func vertexDocID(res ai.SearchResult) string {
	if id := strings.TrimSpace(strings.TrimSuffix(res.Document.ID, ".json")); id != "" {
		return id
	}
	if u := strings.TrimSpace(firstNonEmpty(res.Document.URI, asString(res.Document.StructData["uri"]))); u != "" {
		base := filepath.Base(u)
		return strings.TrimSuffix(base, ".json")
	}
	return ""
}

func vertexScore(res ai.SearchResult) float64 {
	if res.RelevanceScore > 0 {
		return res.RelevanceScore
	}
	if res.RankSignals != nil {
		return res.RankSignals.SemanticSimilarityScore
	}
	return 0
}

func mergeVertexHits(base []vertexHit, extra []vertexHit, limit int) []vertexHit {
	if limit <= 0 {
		limit = 24
	}
	best := map[string]vertexHit{}
	order := make([]string, 0, len(base)+len(extra))
	push := func(hit vertexHit) {
		key := firstNonEmpty(hit.ID, hit.URI)
		if key == "" {
			return
		}
		prev, ok := best[key]
		if !ok {
			order = append(order, key)
			best[key] = hit
			return
		}
		if hit.Score > prev.Score {
			best[key] = hit
		}
	}
	for _, hit := range base {
		push(hit)
	}
	for _, hit := range extra {
		push(hit)
	}
	out := make([]vertexHit, 0, len(best))
	for _, key := range order {
		if hit, ok := best[key]; ok {
			out = append(out, hit)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortedFragments(items map[string]*hmodels.HorizonFragment) []*hmodels.HorizonFragment {
	out := make([]*hmodels.HorizonFragment, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].CanonicalPath) < strings.TrimSpace(out[j].CanonicalPath)
	})
	return out
}

func sortedEdges(items map[string]*hmodels.DependencyEdge) []*hmodels.DependencyEdge {
	out := make([]*hmodels.DependencyEdge, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].ID) < strings.TrimSpace(out[j].ID)
	})
	return out
}

func sortedArtifacts(items map[string]*hmodels.MaterializedArtifact) []*hmodels.MaterializedArtifact {
	out := make([]*hmodels.MaterializedArtifact, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func sortedFacts(items map[string]*coremodels.OntologyFact) []*coremodels.OntologyFact {
	out := make([]*coremodels.OntologyFact, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func fragmentDisplayName(fragment *hmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(fragment.Labels["en"]), strings.TrimSpace(fragment.Labels["ru"]), strings.TrimSpace(fragment.CanonicalPath))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lastPathToken(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	return strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
}

func lensMatchScore(lens *hmodels.Lens, needles []string) int {
	if lens == nil {
		return 0
	}
	score := 0
	check := func(text string, weight int) {
		lower := strings.ToLower(strings.TrimSpace(text))
		if lower == "" {
			return
		}
		for _, needle := range needles {
			if needle == "" {
				continue
			}
			if strings.Contains(lower, needle) {
				score += weight
			}
		}
	}
	check(lens.Name, 4)
	check(lens.OriginalQuery, 5)
	for _, scope := range lens.Scope {
		check(scope, 6)
	}
	for _, dq := range lens.DomainQueries {
		check(dq.CanonicalPath, 4)
		check(dq.Title, 4)
		check(dq.Description, 4)
		for _, entity := range dq.FirstOrderEntities {
			check(entity, 3)
		}
	}
	return score
}

func compactUnique(values []string) []string {
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

func mapKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func containsString(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func writeOutput(path string, payload interface{}) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Fatalf("marshal output failed: %v", err)
	}
	if strings.TrimSpace(path) != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Fatalf("create output dir failed: %v", err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			log.Fatalf("write output failed: %v", err)
		}
	}
	fmt.Println(string(body))
}
