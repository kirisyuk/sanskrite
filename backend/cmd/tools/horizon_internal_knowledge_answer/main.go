package main

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
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
	answerStoreID   string
	answerBucket    string
	corpusID        string
	waitImport      bool
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
	CorpusID            string                 `json:"corpusId,omitempty"`
	AnswerStoreID       string                 `json:"answerStoreId,omitempty"`
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

type prepareReport struct {
	Mode              string               `json:"mode"`
	ProjectID         string               `json:"projectId"`
	TenantID          string               `json:"tenantId"`
	CorpusID          string               `json:"corpusId"`
	AnswerStoreID     string               `json:"answerStoreId,omitempty"`
	LensID            string               `json:"lensId,omitempty"`
	LensName          string               `json:"lensName,omitempty"`
	Question          string               `json:"question,omitempty"`
	ScopePaths        []string             `json:"scopePaths,omitempty"`
	ExportedDocs      int                  `json:"exportedDocs"`
	ImportedURIs      int                  `json:"importedUris"`
	ImportOperation   string               `json:"importOperation,omitempty"`
	Warnings          []string             `json:"warnings,omitempty"`
	PreparedDocs      []preparedDocSummary `json:"preparedDocs,omitempty"`
	SelectedArtifacts []artifactSummary    `json:"selectedArtifacts,omitempty"`
	SelectedRoutes    []routeSummary       `json:"selectedRoutes,omitempty"`
}

type preparedDocSummary struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Kind    string   `json:"kind"`
	URI     string   `json:"uri,omitempty"`
	Refs    []string `json:"refs,omitempty"`
	Updated string   `json:"updated,omitempty"`
}

type preparedEvidenceDoc struct {
	ID             string
	Kind           string
	Title          string
	URI            string
	Content        string
	CanonicalPaths []string
}

type preparedCorpusDoc struct {
	ID         string
	Title      string
	Kind       string
	Content    string
	StructData map[string]any
	URI        string
}

type preparedSubstrate struct {
	CorpusID   string
	Lens       *hmodels.Lens
	Question   string
	ScopePaths []string
	Fragments  map[string]*hmodels.HorizonFragment
	Artifacts  map[string]*hmodels.MaterializedArtifact
	Facts      map[string]*coremodels.OntologyFact
	Routes     []routeSummary
	Objects    []objectSummary
	Relations  []relationSummary
	Products   []productSummary
	Gaps       []gapSummary
	Docs       []preparedCorpusDoc
}

type corridorSummary struct {
	ID               string   `json:"id"`
	SupplierRef      string   `json:"supplierRef,omitempty"`
	CustomerRef      string   `json:"customerRef,omitempty"`
	ProductRef       string   `json:"productRef,omitempty"`
	TransportViaRefs []string `json:"transportViaRefs,omitempty"`
	Modes            []string `json:"modes,omitempty"`
	ArtifactKeys     []string `json:"artifactKeys,omitempty"`
	RouteCount       int      `json:"routeCount"`
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
	case "prepare":
		report, err := runPrepare(ctx, deps, cfg)
		if err != nil {
			log.Fatalf("prepare failed: %v", err)
		}
		writeOutput(cfg.outFile, report)
	case "seed-anchor":
		report, err := runSeedAnchor(ctx, deps, cfg)
		if err != nil {
			log.Fatalf("seed-anchor failed: %v", err)
		}
		writeOutput(cfg.outFile, report)
	case "answer":
		report, err := runPreparedAnswer(ctx, deps, cfg)
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
	lensArtifacts *hrepo.LensArtifactsRepository
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
		lensArtifacts: hrepo.NewLensArtifactsRepository(fs.Client()),
		lenses:      hrepo.NewLensesRepository(fs.Client()),
		facts:       corerepo.NewOntologyFactsRepository(fs.Client()),
		vertex:      vertexClient,
		vertexModel: vertexSvc,
	}, nil
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.mode, "mode", "answer", "prepare|seed-anchor|answer")
	flag.StringVar(&cfg.projectID, "project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	flag.StringVar(&cfg.tenantID, "tenant", "global", "tenant id")
	flag.StringVar(&cfg.anchorPath, "anchor-path", "", "anchor canonical path")
	flag.StringVar(&cfg.anchorQuery, "anchor-query", "", "anchor name query")
	flag.StringVar(&cfg.lensID, "lens-id", "", "optional lens id")
	flag.StringVar(&cfg.question, "question", "", "question to answer")
	flag.StringVar(&cfg.bucket, "bucket", firstNonEmpty(os.Getenv("NV_HORIZON_FRAGMENTS_BUCKET"), os.Getenv("HORIZON_FRAGMENTS_BUCKET"), ""), "GCS bucket for selective export")
	flag.StringVar(&cfg.vertexLocation, "vertex-location", firstNonEmpty(os.Getenv("VERTEX_SEARCH_LOCATION"), "global"), "Vertex AI Search location")
	flag.StringVar(&cfg.nodeStoreID, "node-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"), ""), "Vertex datastore for fragments")
	flag.StringVar(&cfg.edgeStoreID, "edge-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_NV_HORIZON_EDGE_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_EDGES_DATA_STORE_ID"), ""), "Vertex datastore for edges")
	flag.StringVar(&cfg.artifactStoreID, "artifact-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_HORIZON_ARTIFACTS_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"), ""), "Vertex datastore for artifacts")
	flag.StringVar(&cfg.factStoreID, "fact-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_ONTOLOGY_FACTS_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"), ""), "Vertex datastore for ontology facts")
	flag.StringVar(&cfg.answerStoreID, "answer-store", strings.TrimSpace(os.Getenv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID")), "Vertex datastore for prepared answer corpus; if empty, a corpus-scoped test store is derived automatically")
	flag.StringVar(&cfg.answerBucket, "answer-bucket", firstNonEmpty(os.Getenv("RAG_DOCUMENTS_BUCKET"), ""), "GCS bucket for prepared answer corpus docs")
	flag.StringVar(&cfg.corpusID, "corpus-id", "", "prepared corpus id override")
	flag.BoolVar(&cfg.waitImport, "wait-import", true, "wait for Vertex import completion")
	flag.IntVar(&cfg.topK, "top-k", 6, "top results per query/store")
	flag.IntVar(&cfg.limitArtifacts, "limit-artifacts", 24, "max artifacts")
	flag.IntVar(&cfg.limitEdges, "limit-edges", 160, "max edges")
	flag.IntVar(&cfg.limitFacts, "limit-facts", 80, "max facts")
	flag.IntVar(&cfg.limitFragments, "limit-fragments", 120, "max fragments")
	flag.BoolVar(&cfg.useLLM, "use-llm", true, "use Vertex model for final synthesis")
	flag.StringVar(&cfg.outFile, "out", "", "optional output json path")
	flag.Parse()
	if strings.TrimSpace(cfg.bucket) == "" && strings.TrimSpace(cfg.projectID) != "" {
		cfg.bucket = cfg.projectID + "-nv-horizon-fragments"
	}
	if strings.TrimSpace(cfg.answerBucket) == "" && strings.TrimSpace(cfg.projectID) != "" {
		cfg.answerBucket = cfg.projectID + "-rag-documents"
	}
	if strings.TrimSpace(cfg.nodeStoreID) == "" {
		cfg.nodeStoreID = "nv-horizon-fragments-store"
	}
	if strings.TrimSpace(cfg.edgeStoreID) == "" {
		cfg.edgeStoreID = "nv-horizon-edges-store"
	}
	if strings.TrimSpace(cfg.artifactStoreID) == "" {
		cfg.artifactStoreID = cfg.nodeStoreID
	}
	return cfg
}

func runPrepare(ctx context.Context, d *deps, cfg config) (*prepareReport, error) {
	if strings.TrimSpace(cfg.lensID) == "" {
		return nil, fmt.Errorf("--lens-id is required for prepare")
	}
	lens, err := resolveLens(ctx, d, cfg, "")
	if err != nil {
		return nil, err
	}
	if lens == nil {
		return nil, fmt.Errorf("lens not found")
	}
	substrate, err := buildPreparedSubstrate(ctx, d, cfg, lens)
	if err != nil {
		return nil, err
	}
	report := &prepareReport{
		Mode:              cfg.mode,
		ProjectID:         cfg.projectID,
		TenantID:          cfg.tenantID,
		CorpusID:          substrate.CorpusID,
		AnswerStoreID:     resolvePreparedStoreID(cfg, substrate.CorpusID),
		LensID:            lens.ID,
		LensName:          lens.Name,
		Question:          substrate.Question,
		ScopePaths:        append([]string(nil), substrate.ScopePaths...),
		PreparedDocs:      make([]preparedDocSummary, 0, len(substrate.Docs)),
		Warnings:          []string{},
		SelectedArtifacts: summarizeArtifacts(substrate.Artifacts),
		SelectedRoutes:    append([]routeSummary(nil), substrate.Routes...),
	}
	if strings.TrimSpace(cfg.answerBucket) == "" {
		report.Warnings = append(report.Warnings, "answer bucket is empty; prepared docs were not exported")
		return report, nil
	}
	stageDir, err := os.MkdirTemp("", "horizon-prepared-corpus-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageDir)
	uris := make([]string, 0, len(substrate.Docs))
	for i := range substrate.Docs {
		uri, err := stagePreparedDoc(stageDir, cfg.answerBucket, substrate.CorpusID, &substrate.Docs[i])
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("export failed doc=%s: %v", substrate.Docs[i].ID, err))
			continue
		}
		substrate.Docs[i].URI = uri
		uris = append(uris, uri)
		report.ExportedDocs++
		report.PreparedDocs = append(report.PreparedDocs, preparedDocSummary{
			ID:      substrate.Docs[i].ID,
			Title:   substrate.Docs[i].Title,
			Kind:    substrate.Docs[i].Kind,
			URI:     uri,
			Refs:    compactUnique(stringSlice(substituteRefs(substrate.Docs[i].StructData["canonicalPaths"]))),
			Updated: time.Now().UTC().Format(time.RFC3339),
		})
	}
	report.ImportedURIs = len(uris)
	if len(uris) == 0 {
		report.Warnings = append(report.Warnings, "no prepared docs exported")
		return report, nil
	}
	if err := uploadPreparedCorpusDir(ctx, cfg.answerBucket, stageDir, substrate.CorpusID); err != nil {
		return nil, err
	}
	storeID := resolvePreparedStoreID(cfg, substrate.CorpusID)
	storeClient := d.vertex.WithDataStore(storeID)
	if _, err := storeClient.EnsureDataStore(ctx, resolvePreparedStoreDisplayName(lens, substrate.CorpusID)); err != nil {
		return nil, fmt.Errorf("ensure prepared datastore failed: %w", err)
	}
	op, err := storeClient.ImportDocumentsFromURIs(ctx, compactUnique(uris), "INCREMENTAL", "document")
	if err != nil {
		return nil, fmt.Errorf("vertex import failed: %w", err)
	}
	report.ImportOperation = op
	if cfg.waitImport {
		if err := waitForVertexImport(ctx, storeClient, op, 6*time.Minute); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("vertex import wait failed: %v", err))
		}
	}
	return report, nil
}

func runPreparedAnswer(ctx context.Context, d *deps, cfg config) (*answerReport, error) {
	question := strings.TrimSpace(cfg.question)
	if question == "" {
		return nil, fmt.Errorf("--question is required")
	}
	cfg.answerStoreID = resolvePreparedStoreID(cfg, cfg.corpusID)
	queryVariants := buildPreparedQueryVariants(question, cfg.corpusID)
	hits, warns := searchPreparedCorpus(ctx, d.vertex.WithDataStore(cfg.answerStoreID), cfg, queryVariants)
	hits = rerankPreparedHits(question, hits, maxInt(cfg.topK*6, 24))
	evidenceDocs, evidenceWarns := loadPreparedEvidenceDocs(ctx, d.gcs, hits, 10)
	warns = append(warns, evidenceWarns...)
	text, evidence, err := synthesizePreparedAnswer(ctx, d.vertexModel.WithDataStore(cfg.answerStoreID), cfg, question, hits, evidenceDocs)
	if err != nil {
		return nil, err
	}
	report := &answerReport{
		Mode:          cfg.mode,
		ProjectID:     cfg.projectID,
		TenantID:      cfg.tenantID,
		CorpusID:      strings.TrimSpace(cfg.corpusID),
		AnswerStoreID: cfg.answerStoreID,
		Question:      question,
		QueryVariants: queryVariants,
		VertexHits: map[string][]vertexHit{
			"prepared": hits,
		},
		Narrative: text,
		Warnings:  append([]string(nil), warns...),
		StructuredProvenance: map[string]any{
			"answerStore":      cfg.answerStoreID,
			"vertexHitCount":   len(hits),
			"grounding":        groundingEvidenceMap(evidence),
			"retrievedCorpusIds": uniqueStructValues(hits, "corpusId"),
			"retrievedKinds":   uniqueStructValues(hits, "kind"),
		},
	}
	return report, nil
}

func buildPreparedSubstrate(ctx context.Context, d *deps, cfg config, lens *hmodels.Lens) (*preparedSubstrate, error) {
	if lens == nil {
		return nil, fmt.Errorf("lens is nil")
	}
	scopePaths := preparedScopePaths(lens)
	artifacts, err := d.artifacts.ListByLens(ctx, strings.TrimSpace(lens.ID), maxInt(cfg.limitArtifacts*16, 256))
	if err != nil {
		return nil, err
	}
	lensArtifacts, err := d.lensArtifacts.ListByLens(ctx, strings.TrimSpace(lens.ID), 2000)
	if err != nil {
		return nil, err
	}
	artifactMap := make(map[string]*hmodels.MaterializedArtifact, len(artifacts))
	pathRefs := make(map[string]struct{}, len(scopePaths)+128)
	for _, path := range scopePaths {
		if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, hmodels.DomainScopePrefix) && !strings.HasPrefix(path, hmodels.QueryScopePrefix) && !strings.HasPrefix(path, hmodels.ArtifactScopePrefix) {
			pathRefs[path] = struct{}{}
		}
	}
	for _, lensArtifact := range lensArtifacts {
		if lensArtifact == nil {
			continue
		}
		if path := strings.TrimSpace(lensArtifact.CanonicalPath); path != "" {
			pathRefs[path] = struct{}{}
		}
		if path := strings.TrimSpace(lensArtifact.SourcePath); path != "" {
			pathRefs[path] = struct{}{}
		}
		if path := strings.TrimSpace(lensArtifact.TargetPath); path != "" {
			pathRefs[path] = struct{}{}
		}
	}
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
			continue
		}
		artifactMap[artifact.ID] = artifact
		for _, ref := range compactUnique(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			if strings.TrimSpace(ref) != "" {
				pathRefs[strings.TrimSpace(ref)] = struct{}{}
			}
		}
		parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			for _, ref := range compactUnique(append([]string{leg.SupplierRef, leg.CustomerRef, leg.ProductRef, leg.SupplyLinkCanonicalPath}, leg.TransportViaRefs...)) {
				if strings.TrimSpace(ref) != "" {
					pathRefs[strings.TrimSpace(ref)] = struct{}{}
				}
			}
		}
	}
	fragments, err := loadFragmentsByCanonicalPathsReadOnly(ctx, d, mapKeys(pathRefs))
	if err != nil {
		return nil, err
	}
	routes := summarizeRoutes(artifactMap)
	relations := summarizeLensArtifactRelations(lensArtifacts)
	objectScores := objectScoresForPreparedScope(mapKeys(pathRefs), routes, relations, artifactMap)
	objects := summarizePreparedObjects(fragments, objectScores, cfg.limitFragments)
	products := summarizeProducts(artifactMap)
	gaps := detectPreparedGaps(artifactMap, relations)
	substrate := &preparedSubstrate{
		CorpusID:   resolveCorpusID(cfg, lens),
		Lens:       lens,
		Question:   resolvePreparedQuestion(cfg, lens),
		ScopePaths: scopePaths,
		Fragments:  fragments,
		Artifacts:  artifactMap,
		Facts:      map[string]*coremodels.OntologyFact{},
		Routes:     routes,
		Objects:    objects,
		Relations:  relations,
		Products:   products,
		Gaps:       gaps,
	}
	substrate.Docs = buildPreparedCorpusDocs(substrate)
	return substrate, nil
}

func preparedScopePaths(lens *hmodels.Lens) []string {
	paths := workscope.LensWorkbenchScopePaths(lens)
	filtered := make([]string, 0, len(paths))
	for _, path := range compactUnique(paths) {
		switch {
		case strings.HasPrefix(path, hmodels.DomainScopePrefix):
			continue
		case strings.HasPrefix(path, hmodels.QueryScopePrefix):
			continue
		case strings.HasPrefix(path, hmodels.ArtifactScopePrefix):
			continue
		case strings.HasPrefix(path, "/"):
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func resolvePreparedStoreID(cfg config, corpusID string) string {
	if storeID := strings.TrimSpace(cfg.answerStoreID); storeID != "" {
		return storeID
	}
	corpusID = strings.TrimSpace(corpusID)
	if corpusID == "" {
		return "test-internal-knowledge-store"
	}
	slug := shortSlug(corpusID)
	if len(slug) > 38 {
		slug = slug[:38]
	}
	return fmt.Sprintf("ik-%s-%s", slug, shortHash(corpusID))
}

func resolvePreparedStoreDisplayName(lens *hmodels.Lens, corpusID string) string {
	base := strings.TrimSpace(corpusID)
	if lens != nil && strings.TrimSpace(lens.Name) != "" {
		base = strings.TrimSpace(lens.Name)
	}
	if base == "" {
		base = "Internal Knowledge"
	}
	return firstNonEmpty(strings.TrimSpace(base)+" Internal Knowledge Store", "Internal Knowledge Store")
}

func resolveCorpusID(cfg config, lens *hmodels.Lens) string {
	if strings.TrimSpace(cfg.corpusID) != "" {
		return strings.TrimSpace(cfg.corpusID)
	}
	if lens == nil {
		return "internal-knowledge-" + shortHash(strings.TrimSpace(cfg.question))
	}
	base := strings.ToLower(strings.TrimSpace(lens.Name))
	base = strings.NewReplacer(" ", "-", "/", "-", "_", "-", ":", "-", ",", "-", ".", "-", "(", "", ")", "").Replace(base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "lens"
	}
	return base + "-" + shortHash(strings.TrimSpace(lens.ID))
}

func resolvePreparedQuestion(cfg config, lens *hmodels.Lens) string {
	if strings.TrimSpace(cfg.question) != "" {
		return strings.TrimSpace(cfg.question)
	}
	if lens != nil {
		if strings.TrimSpace(lens.OriginalQuery) != "" {
			return strings.TrimSpace(lens.OriginalQuery)
		}
		if strings.TrimSpace(lens.Name) != "" {
			return strings.TrimSpace(lens.Name)
		}
	}
	return "internal knowledge answer"
}

func summarizeLensArtifactRelations(items []*hmodels.LensArtifact) []relationSummary {
	type key struct {
		source string
		edge   string
		target string
	}
	best := map[key]relationSummary{}
	for _, item := range items {
		if item == nil || item.Type != hmodels.LensArtifactTypeEdge {
			continue
		}
		k := key{
			source: strings.TrimSpace(item.SourcePath),
			edge:   strings.TrimSpace(item.EdgeType),
			target: strings.TrimSpace(item.TargetPath),
		}
		prev := best[k]
		prev.EdgeID = firstNonEmpty(prev.EdgeID, strings.TrimSpace(item.EdgeID))
		prev.EdgeType = k.edge
		prev.SourcePath = k.source
		prev.TargetPath = k.target
		prev.SourceLabel = firstNonEmpty(prev.SourceLabel, strings.TrimSpace(item.SourcePath))
		prev.TargetLabel = firstNonEmpty(prev.TargetLabel, strings.TrimSpace(item.TargetPath))
		prev.Score++
		best[k] = prev
	}
	out := make([]relationSummary, 0, len(best))
	for _, relation := range best {
		out = append(out, relation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].EdgeType == out[j].EdgeType {
				if out[i].SourcePath == out[j].SourcePath {
					return out[i].TargetPath < out[j].TargetPath
				}
				return out[i].SourcePath < out[j].SourcePath
			}
			return out[i].EdgeType < out[j].EdgeType
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func objectScoresForPreparedScope(scopePaths []string, routes []routeSummary, relations []relationSummary, artifacts map[string]*hmodels.MaterializedArtifact) map[string]int {
	scores := map[string]int{}
	for _, path := range compactUnique(scopePaths) {
		if strings.TrimSpace(path) != "" {
			scores[strings.TrimSpace(path)] += 10
		}
	}
	for _, route := range routes {
		for _, path := range compactUnique(append([]string{route.SupplierRef, route.CustomerRef, route.ProductRef, route.SupplyLinkRef}, append(route.TransportViaRefs, append(route.GeoRefs, append(route.ZoneRefs, route.AffectedZoneRefs...)...)...)...)) {
			if strings.TrimSpace(path) != "" {
				scores[strings.TrimSpace(path)] += 14
			}
		}
	}
	for _, relation := range relations {
		if strings.TrimSpace(relation.SourcePath) != "" {
			scores[strings.TrimSpace(relation.SourcePath)] += 6
		}
		if strings.TrimSpace(relation.TargetPath) != "" {
			scores[strings.TrimSpace(relation.TargetPath)] += 6
		}
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		for _, path := range compactUnique(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			if strings.TrimSpace(path) != "" {
				scores[strings.TrimSpace(path)] += 2
			}
		}
	}
	return scores
}

func summarizePreparedObjects(fragments map[string]*hmodels.HorizonFragment, scores map[string]int, limit int) []objectSummary {
	type ranked struct {
		path  string
		score int
	}
	rows := make([]ranked, 0, len(fragments))
	for path := range fragments {
		rows = append(rows, ranked{path: path, score: scores[path]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score == rows[j].score {
			return rows[i].path < rows[j].path
		}
		return rows[i].score > rows[j].score
	})
	if limit <= 0 {
		limit = 120
	}
	out := make([]objectSummary, 0, minInt(limit, len(rows)))
	for _, row := range rows {
		fragment := fragments[row.path]
		if fragment == nil {
			continue
		}
		out = append(out, objectSummary{
			CanonicalPath: row.path,
			Type:          string(fragment.Type),
			DisplayName:   fragmentDisplayName(fragment),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func detectPreparedGaps(artifacts map[string]*hmodels.MaterializedArtifact, relations []relationSummary) []gapSummary {
	bundle := &graphBundle{
		Artifacts: artifacts,
		Edges:     map[string]*hmodels.DependencyEdge{},
	}
	for _, relation := range relations {
		bundle.Edges[firstNonEmpty(relation.EdgeID, relation.EdgeType+"|"+relation.SourcePath+"|"+relation.TargetPath)] = &hmodels.DependencyEdge{ID: relation.EdgeID}
	}
	return detectGaps(bundle)
}

func buildPreparedCorpusDocs(sub *preparedSubstrate) []preparedCorpusDoc {
	if sub == nil {
		return nil
	}
	docs := make([]preparedCorpusDoc, 0, 256)
	corridors := summarizeCorridors(sub.Routes)
	hints := preparedQuestionHints(sub)
	addDoc := func(id, title, kind, content string, refs []string, extra map[string]any) {
		if strings.TrimSpace(content) == "" {
			return
		}
		structData := map[string]any{
			"type":           "internal_knowledge_capsule",
			"kind":           kind,
			"title":          title,
			"corpusId":       sub.CorpusID,
			"lensId":         firstNonEmpty(sub.Lens.ID),
			"lensName":       firstNonEmpty(sub.Lens.Name),
			"question":       sub.Question,
			"questionHints":  hints,
			"canonicalPaths": compactUnique(refs),
		}
		for k, v := range extra {
			structData[k] = v
		}
		docs = append(docs, preparedCorpusDoc{
			ID:         stableDocID(sub.CorpusID, kind, id),
			Title:      title,
			Kind:       kind,
			Content:    strings.TrimSpace(content),
			StructData: structData,
		})
	}
	addDoc("overview", firstNonEmpty(sub.Lens.Name, sub.Question), "overview", buildPreparedOverviewDoc(sub, corridors), collectOverviewRefs(sub), map[string]any{
		"routeCount":    len(sub.Routes),
		"productCount":  len(sub.Products),
		"objectCount":   len(sub.Objects),
		"relationCount": len(sub.Relations),
		"gapCount":      len(sub.Gaps),
	})
	addDoc("dossier", firstNonEmpty(sub.Lens.Name, sub.Question)+" dossier", "dossier", buildPreparedDossierDoc(sub, corridors), collectOverviewRefs(sub), map[string]any{
		"routeCount":    len(sub.Routes),
		"productCount":  len(sub.Products),
		"objectCount":   len(sub.Objects),
		"relationCount": len(sub.Relations),
		"gapCount":      len(sub.Gaps),
	})
	addDoc("products", firstNonEmpty(sub.Lens.Name, sub.Question)+" products", "products", buildPreparedProductsDoc(sub), topProductRefs(sub.Products, 40), map[string]any{
		"productCount": len(sub.Products),
	})
	addDoc("gaps", firstNonEmpty(sub.Lens.Name, sub.Question)+" gaps", "gaps", buildPreparedGapDoc(sub), collectOverviewRefs(sub), map[string]any{
		"gapCount": len(sub.Gaps),
	})
	for i, corridor := range corridors {
		if i >= 24 {
			break
		}
		addDoc(corridor.ID, buildCorridorTitle(corridor), "corridor", buildCorridorDoc(corridor), compactUnique(append([]string{corridor.SupplierRef, corridor.CustomerRef, corridor.ProductRef}, corridor.TransportViaRefs...)), map[string]any{
			"routeCount": corridor.RouteCount,
			"modes":      corridor.Modes,
		})
	}
	for i, relation := range sub.Relations {
		if i >= 24 {
			break
		}
		addDoc(firstNonEmpty(relation.EdgeID, fmt.Sprintf("%s-%s-%s", relation.EdgeType, relation.SourcePath, relation.TargetPath)), buildRelationTitle(relation), "relation", buildRelationDoc(relation), compactUnique([]string{relation.SourcePath, relation.TargetPath}), map[string]any{
			"edgeType": relation.EdgeType,
			"score":    relation.Score,
		})
	}
	for i, object := range sub.Objects {
		if i >= 24 {
			break
		}
		fragment := sub.Fragments[object.CanonicalPath]
		addDoc(object.CanonicalPath, firstNonEmpty(object.DisplayName, object.CanonicalPath), "object", buildObjectDoc(fragment), []string{object.CanonicalPath}, map[string]any{
			"fragmentType": object.Type,
		})
	}
	for i, artifact := range sortedArtifacts(sub.Artifacts) {
		if i >= 16 {
			break
		}
		addDoc(firstNonEmpty(artifact.CanonicalKey, artifact.ID), firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey), "route_artifact", buildRouteArtifactDoc(artifact), compactUnique(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...)), map[string]any{
			"artifactKind":   string(artifact.Kind),
			"pathStatus":     artifact.PathStatus,
			"artifactKey":    artifact.CanonicalKey,
			"anchorSupplier": artifact.AnchorSupplier,
			"anchorCustomer": artifact.AnchorCustomer,
			"productAxis":    artifact.ProductAxis,
		})
	}
	return docs
}

func summarizeCorridors(routes []routeSummary) []corridorSummary {
	type key struct {
		supplier string
		customer string
		product  string
	}
	type agg struct {
		corridorSummary
		viaSet      map[string]struct{}
		modeSet     map[string]struct{}
		artifactSet map[string]struct{}
	}
	grouped := map[key]*agg{}
	for _, route := range routes {
		k := key{
			supplier: strings.TrimSpace(route.SupplierRef),
			customer: strings.TrimSpace(route.CustomerRef),
			product:  strings.TrimSpace(route.ProductRef),
		}
		item := grouped[k]
		if item == nil {
			item = &agg{
				corridorSummary: corridorSummary{
					ID:          fmt.Sprintf("%s|%s|%s", k.supplier, k.customer, k.product),
					SupplierRef: k.supplier,
					CustomerRef: k.customer,
					ProductRef:  k.product,
				},
				viaSet:      map[string]struct{}{},
				modeSet:     map[string]struct{}{},
				artifactSet: map[string]struct{}{},
			}
			grouped[k] = item
		}
		item.RouteCount++
		for _, via := range route.TransportViaRefs {
			if via = strings.TrimSpace(via); via != "" {
				item.viaSet[via] = struct{}{}
			}
		}
		if mode := strings.TrimSpace(route.Mode); mode != "" {
			item.modeSet[mode] = struct{}{}
		}
		if key := strings.TrimSpace(route.ArtifactCanonicalKey); key != "" {
			item.artifactSet[key] = struct{}{}
		}
	}
	out := make([]corridorSummary, 0, len(grouped))
	for _, item := range grouped {
		item.TransportViaRefs = sortedSetKeys(item.viaSet)
		item.Modes = sortedSetKeys(item.modeSet)
		item.ArtifactKeys = sortedSetKeys(item.artifactSet)
		out = append(out, item.corridorSummary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RouteCount == out[j].RouteCount {
			if out[i].SupplierRef == out[j].SupplierRef {
				if out[i].CustomerRef == out[j].CustomerRef {
					return out[i].ProductRef < out[j].ProductRef
				}
				return out[i].CustomerRef < out[j].CustomerRef
			}
			return out[i].SupplierRef < out[j].SupplierRef
		}
		return out[i].RouteCount > out[j].RouteCount
	})
	return out
}

func buildPreparedOverviewDoc(sub *preparedSubstrate, corridors []corridorSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n", sub.Question)
	if sub.Lens != nil {
		fmt.Fprintf(&b, "Lens: %s\n", firstNonEmpty(sub.Lens.Name, sub.Lens.ID))
		if strings.TrimSpace(sub.Lens.OriginalQuery) != "" {
			fmt.Fprintf(&b, "Original framing: %s\n", strings.TrimSpace(sub.Lens.OriginalQuery))
		}
	}
	if len(sub.ScopePaths) > 0 {
		b.WriteString("Scope paths:\n")
		for i, path := range sub.ScopePaths {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s\n", path)
		}
	}
	if len(sub.Objects) > 0 {
		b.WriteString("Key objects:\n")
		for i, object := range sub.Objects {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s (%s) path=%s\n", firstNonEmpty(object.DisplayName, object.CanonicalPath), object.Type, object.CanonicalPath)
		}
	}
	if len(sub.Products) > 0 {
		b.WriteString("Products:\n")
		for i, product := range sub.Products {
			if i >= 20 {
				break
			}
			fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", product.ProductRef, product.UsedBy)
		}
	}
	if len(corridors) > 0 {
		b.WriteString("Logistics corridors:\n")
		for i, corridor := range corridors {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s -> %s product=%s routes=%d via=%s modes=%s\n",
				firstNonEmpty(corridor.SupplierRef, "?"),
				firstNonEmpty(corridor.CustomerRef, "?"),
				firstNonEmpty(corridor.ProductRef, "?"),
				corridor.RouteCount,
				strings.Join(corridor.TransportViaRefs, " | "),
				strings.Join(corridor.Modes, ", "),
			)
		}
	}
	if len(sub.Relations) > 0 {
		b.WriteString("Graph relations:\n")
		for i, relation := range sub.Relations {
			if i >= 40 {
				break
			}
			fmt.Fprintf(&b, "- %s [%s] %s evidence=%0.0f\n", firstNonEmpty(relation.SourcePath, "?"), relation.EdgeType, firstNonEmpty(relation.TargetPath, "?"), relation.Score)
		}
	}
	if len(sub.Gaps) > 0 {
		b.WriteString("Known gaps:\n")
		for _, gap := range sub.Gaps {
			fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
		}
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedProductsDoc(sub *preparedSubstrate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Products for question: %s\n", sub.Question)
	for i, product := range sub.Products {
		if i >= 40 {
			break
		}
		fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", product.ProductRef, product.UsedBy)
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedDossierDoc(sub *preparedSubstrate, corridors []corridorSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Answer dossier for question: %s\n", sub.Question)
	if sub.Lens != nil {
		fmt.Fprintf(&b, "Scope: %s\n", firstNonEmpty(sub.Lens.Name, sub.Lens.ID))
	}
	b.WriteString("Objects:\n")
	for i, object := range sub.Objects {
		if i >= 12 {
			break
		}
		fmt.Fprintf(&b, "- %s [%s] path=%s\n", firstNonEmpty(object.DisplayName, object.CanonicalPath), firstNonEmpty(object.Type, "UNKNOWN"), object.CanonicalPath)
	}
	b.WriteString("Connections:\n")
	for i, relation := range sub.Relations {
		if i >= 16 {
			break
		}
		fmt.Fprintf(&b, "- %s [%s] %s\n", firstNonEmpty(relation.SourcePath, "?"), relation.EdgeType, firstNonEmpty(relation.TargetPath, "?"))
	}
	b.WriteString("Routes:\n")
	for i, corridor := range corridors {
		if i >= 16 {
			break
		}
		fmt.Fprintf(&b, "- %s -> %s product=%s via=%s modes=%s routes=%d\n",
			firstNonEmpty(corridor.SupplierRef, "?"),
			firstNonEmpty(corridor.CustomerRef, "?"),
			firstNonEmpty(corridor.ProductRef, "?"),
			strings.Join(corridor.TransportViaRefs, " | "),
			strings.Join(corridor.Modes, ", "),
			corridor.RouteCount,
		)
	}
	if len(sub.Products) > 0 {
		b.WriteString("Products:\n")
		for i, product := range sub.Products {
			if i >= 12 {
				break
			}
			fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", product.ProductRef, product.UsedBy)
		}
	}
	if len(sub.Gaps) > 0 {
		b.WriteString("Gaps:\n")
		for _, gap := range sub.Gaps {
			fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
		}
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedGapDoc(sub *preparedSubstrate) string {
	if len(sub.Gaps) == 0 {
		return "No critical internal-knowledge gaps were detected for this prepared corpus."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Known gaps for question: %s\n", sub.Question)
	for _, gap := range sub.Gaps {
		fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
	}
	return strings.TrimSpace(b.String())
}

func buildCorridorTitle(c corridorSummary) string {
	return fmt.Sprintf("Corridor %s -> %s product %s", firstNonEmpty(c.SupplierRef, "?"), firstNonEmpty(c.CustomerRef, "?"), firstNonEmpty(c.ProductRef, "?"))
}

func buildCorridorDoc(c corridorSummary) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Supplier: %s\nCustomer: %s\nProduct: %s\nRoute count: %d\nTransport via: %s\nModes: %s\nMaterialized artifacts: %s",
		firstNonEmpty(c.SupplierRef, "?"),
		firstNonEmpty(c.CustomerRef, "?"),
		firstNonEmpty(c.ProductRef, "?"),
		c.RouteCount,
		strings.Join(c.TransportViaRefs, " | "),
		strings.Join(c.Modes, ", "),
		strings.Join(c.ArtifactKeys, ", "),
	))
}

func buildRelationTitle(r relationSummary) string {
	return fmt.Sprintf("Relation %s %s %s", firstNonEmpty(r.SourcePath, "?"), r.EdgeType, firstNonEmpty(r.TargetPath, "?"))
}

func buildRelationDoc(r relationSummary) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Source: %s\nRelation: %s\nTarget: %s\nEvidence count: %0.0f",
		firstNonEmpty(r.SourcePath, "?"),
		r.EdgeType,
		firstNonEmpty(r.TargetPath, "?"),
		r.Score,
	))
}

func buildObjectDoc(fragment *hmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Object: %s\n", fragmentDisplayName(fragment))
	fmt.Fprintf(&b, "Canonical path: %s\n", fragment.CanonicalPath)
	fmt.Fprintf(&b, "Type: %s\n", fragment.Type)
	if strings.TrimSpace(fragment.Description) != "" {
		fmt.Fprintf(&b, "Description: %s\n", strings.TrimSpace(fragment.Description))
	}
	if len(fragment.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(compactUnique(fragment.Tags), ", "))
	}
	if len(fragment.IndustryPaths) > 0 {
		fmt.Fprintf(&b, "Industry paths: %s\n", strings.Join(compactUnique(fragment.IndustryPaths), ", "))
	}
	if len(fragment.MarketSegments) > 0 {
		fmt.Fprintf(&b, "Market segments: %s\n", strings.Join(compactUnique(fragment.MarketSegments), ", "))
	}
	return strings.TrimSpace(b.String())
}

func buildRouteArtifactDoc(artifact *hmodels.MaterializedArtifact) string {
	if artifact == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Materialized route artifact: %s\n", firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey))
	fmt.Fprintf(&b, "Canonical key: %s\n", artifact.CanonicalKey)
	fmt.Fprintf(&b, "Kind: %s\n", artifact.Kind)
	fmt.Fprintf(&b, "Path status: %s\n", artifact.PathStatus)
	if strings.TrimSpace(artifact.ProductAxis) != "" {
		fmt.Fprintf(&b, "Product axis: %s\n", artifact.ProductAxis)
	}
	parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err == nil && parsed != nil {
		b.WriteString("Legs:\n")
		for _, leg := range parsed.Legs {
			fmt.Fprintf(&b, "- leg=%d %s -> %s product=%s via=%s mode=%s supplyLink=%s\n",
				leg.Index,
				firstNonEmpty(leg.SupplierRef, "?"),
				firstNonEmpty(leg.CustomerRef, "?"),
				firstNonEmpty(leg.ProductRef, "?"),
				strings.Join(compactUnique(leg.TransportViaRefs), " | "),
				firstNonEmpty(leg.Mode, "unknown"),
				firstNonEmpty(leg.SupplyLinkCanonicalPath, "-"),
			)
		}
	}
	return strings.TrimSpace(b.String())
}

func collectOverviewRefs(sub *preparedSubstrate) []string {
	refs := make([]string, 0, 128)
	for i, object := range sub.Objects {
		if i >= 60 {
			break
		}
		refs = append(refs, object.CanonicalPath)
	}
	return compactUnique(refs)
}

func topProductRefs(products []productSummary, limit int) []string {
	out := make([]string, 0, minInt(limit, len(products)))
	for i, product := range products {
		if i >= limit {
			break
		}
		if strings.TrimSpace(product.ProductRef) != "" {
			out = append(out, strings.TrimSpace(product.ProductRef))
		}
	}
	return out
}

func preparedQuestionHints(sub *preparedSubstrate) []string {
	hints := []string{sub.Question}
	if sub.Lens != nil {
		hints = append(hints, sub.Lens.Name, sub.Lens.OriginalQuery)
		for _, query := range sub.Lens.DomainQueries {
			hints = append(hints, query.Title, query.Description, query.CanonicalPath)
		}
	}
	for _, path := range sub.ScopePaths {
		if len(hints) >= 32 {
			break
		}
		hints = append(hints, path)
	}
	return compactUnique(hints)
}

func stagePreparedDoc(rootDir, bucket, corpusID string, doc *preparedCorpusDoc) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("prepared doc is nil")
	}
	objectPath := fmt.Sprintf("global/test_internal_knowledge/corpora/%s/%s.json", corpusID, doc.ID)
	uri := fmt.Sprintf("gs://%s/%s", bucket, objectPath)
	structData := cloneMap(doc.StructData)
	if structData == nil {
		structData = map[string]any{}
	}
	structData["uri"] = uri
	structData["documentId"] = doc.ID
	body, err := json.Marshal(map[string]any{
		"id": doc.ID,
		"content": map[string]any{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(doc.Content)),
		},
		"structData": structData,
	})
	if err != nil {
		return "", err
	}
	localPath := filepath.Join(rootDir, filepath.FromSlash(objectPath))
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(localPath, body, 0o644); err != nil {
		return "", err
	}
	return uri, nil
}

func uploadPreparedCorpusDir(ctx context.Context, bucket, rootDir, corpusID string) error {
	sourceDir := filepath.Join(rootDir, "global", "test_internal_knowledge", "corpora", corpusID)
	target := fmt.Sprintf("gs://%s/global/test_internal_knowledge/corpora/", bucket)
	cmd := exec.CommandContext(ctx, "gcloud", "storage", "cp", "--recursive", sourceDir, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gcloud storage cp failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func waitForVertexImport(ctx context.Context, client *ai.VertexAISearchClient, operation string, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("vertex client is nil")
	}
	deadline := time.Now().Add(timeout)
	readinessHits := 0
	for time.Now().Before(deadline) {
		status, err := client.CheckOperationStatus(ctx, operation)
		if err != nil {
			return err
		}
		if status.Done {
			if status.Error != nil {
				return fmt.Errorf("vertex operation failed: %d %s", status.Error.Code, status.Error.Message)
			}
			return nil
		}
		if docs, err := client.ListDocuments(ctx, 1); err == nil && len(docs.Documents) > 0 {
			readinessHits++
			if readinessHits >= 2 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("vertex import timeout after %s", timeout)
}

func searchPreparedCorpus(ctx context.Context, client *ai.VertexAISearchClient, cfg config, variants []string) ([]vertexHit, []string) {
	if client == nil {
		return nil, []string{"vertex client is nil"}
	}
	hits := []vertexHit{}
	warns := []string{}
	for _, variant := range compactUnique(variants) {
		resp, err := client.Search(ctx, &ai.SearchRequest{
			Query:    variant,
			PageSize: maxInt(cfg.topK*4, 24),
			SpellCorrectionSpec: &ai.SpellCorrectionSpec{
				Mode: "AUTO",
			},
		})
		if err != nil {
			warns = append(warns, fmt.Sprintf("vertex prepared search failed query=%q: %v", variant, err))
			continue
		}
		current := make([]vertexHit, 0, len(resp.Results))
		for _, res := range resp.Results {
			structData := cloneMap(res.Document.StructData)
			if asString(structData["type"]) != "internal_knowledge_capsule" {
				continue
			}
			if strings.TrimSpace(cfg.corpusID) != "" && asString(structData["corpusId"]) != strings.TrimSpace(cfg.corpusID) {
				continue
			}
			current = append(current, vertexHit{
				Store:      "prepared",
				ID:         vertexDocID(res),
				Title:      strings.TrimSpace(res.Document.Title),
				URI:        strings.TrimSpace(firstNonEmpty(res.Document.URI, asString(res.Document.StructData["uri"]))),
				Score:      vertexScore(res),
				StructData: structData,
			})
		}
		hits = mergeVertexHits(hits, current, cfg.topK*6)
	}
	return hits, warns
}

func rerankPreparedHits(question string, hits []vertexHit, limit int) []vertexHit {
	if len(hits) == 0 {
		return nil
	}
	queryTokens := normalizedTokenSet(question)
	logisticsQuery := strings.Contains(strings.ToLower(question), "supply") || strings.Contains(strings.ToLower(question), "logistics") || strings.Contains(strings.ToLower(question), "route")
	type scored struct {
		hit   vertexHit
		score float64
	}
	scoredHits := make([]scored, 0, len(hits))
	for _, hit := range hits {
		score := hit.Score * 100
		kind := asString(hit.StructData["kind"])
		score += preparedKindWeight(kind, logisticsQuery)
		score += 4.0 * tokenOverlapScore(queryTokens, normalizedTokenSet(firstNonEmpty(hit.Title, asString(hit.StructData["title"]))))
		score += 2.0 * tokenOverlapScore(queryTokens, normalizedTokenSet(strings.Join(stringSlice(hit.StructData["canonicalPaths"]), " ")))
		score += 1.5 * tokenOverlapScore(queryTokens, normalizedTokenSet(strings.Join(stringSlice(hit.StructData["questionHints"]), " ")))
		if strings.TrimSpace(asString(hit.StructData["productAxis"])) != "" && tokenOverlapScore(queryTokens, normalizedTokenSet(asString(hit.StructData["productAxis"]))) > 0 {
			score += 6
		}
		if strings.Contains(strings.ToLower(question), "spare") || strings.Contains(strings.ToLower(question), "service") {
			joined := strings.ToLower(strings.Join(append(stringSlice(hit.StructData["canonicalPaths"]), asString(hit.StructData["productAxis"])), " "))
			if strings.Contains(joined, "spare") || strings.Contains(joined, "service") {
				score += 12
			}
		}
		scoredHits = append(scoredHits, scored{hit: hit, score: score})
	}
	sort.Slice(scoredHits, func(i, j int) bool {
		if scoredHits[i].score == scoredHits[j].score {
			return scoredHits[i].hit.ID < scoredHits[j].hit.ID
		}
		return scoredHits[i].score > scoredHits[j].score
	})
	perKindCap := map[string]int{
		"overview":       1,
		"products":       1,
		"gaps":           1,
		"corridor":       maxInt(limit/3, 4),
		"route_artifact": maxInt(limit/3, 4),
		"relation":       maxInt(limit/4, 3),
		"object":         maxInt(limit/4, 3),
	}
	selected := make([]vertexHit, 0, minInt(limit, len(scoredHits)))
	seen := map[string]struct{}{}
	perKind := map[string]int{}
	for _, item := range scoredHits {
		if _, ok := seen[item.hit.ID]; ok {
			continue
		}
		kind := asString(item.hit.StructData["kind"])
		if cap, ok := perKindCap[kind]; ok && perKind[kind] >= cap {
			continue
		}
		seen[item.hit.ID] = struct{}{}
		perKind[kind]++
		selected = append(selected, item.hit)
		if len(selected) >= limit {
			break
		}
	}
	if len(selected) == 0 {
		return hits
	}
	return selected
}

func preparedKindWeight(kind string, logisticsQuery bool) float64 {
	switch strings.TrimSpace(kind) {
	case "dossier":
		return 22
	case "overview":
		return 14
	case "corridor":
		if logisticsQuery {
			return 18
		}
		return 10
	case "route_artifact":
		if logisticsQuery {
			return 16
		}
		return 9
	case "relation":
		return 8
	case "object":
		return 7
	case "products":
		return 11
	case "gaps":
		return 5
	default:
		return 0
	}
}

func tokenOverlapScore(queryTokens, docTokens map[string]struct{}) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	var overlap float64
	for token := range queryTokens {
		if _, ok := docTokens[token]; ok {
			overlap++
		}
	}
	return overlap
}

func normalizedTokenSet(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, token := range textTokens(strings.ToLower(strings.TrimSpace(text))) {
		token = strings.TrimSpace(token)
		if len(token) < 3 {
			continue
		}
		set[token] = struct{}{}
	}
	return set
}

func buildPreparedQueryVariants(question string, corpusID string) []string {
	base := compactUnique([]string{
		question,
		strings.Join(textTokens(question), " "),
		strings.TrimSpace(strings.NewReplacer("?", "", ",", "", ".", "", ":", "").Replace(question)),
	})
	base = append(base, corpusQueryVariants(corpusID)...)
	joined := strings.ToLower(strings.Join(base, " "))
	if strings.Contains(joined, "omani") || strings.Contains(joined, "oman") {
		base = append(base,
			"oman retail wholesale supply chain",
			"oman retail logistics routes products",
			"oman retail and wholesale supply chain",
		)
	}
	if strings.Contains(joined, "retail") {
		base = append(base, "retail wholesale logistics distribution")
	}
	if strings.Contains(joined, "supply") || strings.Contains(joined, "logistics") {
		base = append(base, "products routes suppliers customers transport via")
	}
	return compactUnique(base)
}

func corpusQueryVariants(corpusID string) []string {
	corpusID = strings.TrimSpace(strings.ToLower(corpusID))
	if corpusID == "" {
		return nil
	}
	parts := strings.Split(corpusID, "-")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(part) == 10 && isHexLike(part) {
			continue
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		return nil
	}
	base := strings.Join(cleaned, " ")
	out := []string{base}
	if strings.Contains(base, "supply chain") {
		out = append(out, strings.ReplaceAll(base, "supply chain", "logistics"))
		out = append(out, base+" logistics")
	}
	if len(cleaned) > 2 {
		out = append(out, strings.Join(cleaned[:minInt(4, len(cleaned))], " "))
	}
	return compactUnique(out)
}

func isHexLike(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func synthesizePreparedAnswer(ctx context.Context, svc *ai.VertexAIService, cfg config, question string, hits []vertexHit, evidenceDocs []preparedEvidenceDoc) (string, *ai.GroundingEvidence, error) {
	if svc == nil {
		return "", nil, fmt.Errorf("vertex ai service is nil")
	}
	hitTitles := make([]string, 0, len(hits))
	for i, hit := range hits {
		if i >= 10 {
			break
		}
		hitTitles = append(hitTitles, firstNonEmpty(hit.Title, hit.ID))
	}
	evidenceBlock := renderPreparedEvidenceDocs(evidenceDocs)
	prompt := strings.TrimSpace(fmt.Sprintf(`
You have access to an internal knowledge corpus built from the system graph, materialized supply-chain routes, relations, and prepared logistics capsules.
Use ONLY internal datastore grounding. Do not use web search. Do not invent missing facts.
Answer in the same language as the user's question.

Question: %s

Likely relevant internal documents:
%s

Prepared evidence pack:
%s

Return:
1. A direct answer.
2. Objects and how they are connected.
3. Products and routes.
4. Explicit gaps in internal knowledge.
Be concrete. Prefer canonical objects, explicit corridors, explicit products, and explicit route legs that are present in the evidence pack.
Do not claim a gap if the evidence pack already contains the route, relation, or object.
`, question, strings.Join(hitTitles, "\n"), evidenceBlock))
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
		UseCase:  "horizon.internal_knowledge_answer.prepared",
	}
	res, err := svc.ExecuteTask(ctx, task, "vertex-gemini-3-flash-preview")
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(res.Content), res.GroundingEvidence, nil
}

func loadPreparedEvidenceDocs(ctx context.Context, gcsClient *gcp.GCSClient, hits []vertexHit, limit int) ([]preparedEvidenceDoc, []string) {
	if gcsClient == nil || len(hits) == 0 || limit <= 0 {
		return nil, nil
	}
	out := make([]preparedEvidenceDoc, 0, minInt(limit, len(hits)))
	warns := []string{}
	for _, hit := range hits {
		if len(out) >= limit {
			break
		}
		uri := strings.TrimSpace(firstNonEmpty(hit.URI, asString(hit.StructData["uri"])))
		bucket, object, ok := parseGCSURI(uri)
		if !ok {
			continue
		}
		body, err := gcsClient.ReadObject(ctx, bucket, object)
		if err != nil {
			warns = append(warns, fmt.Sprintf("read evidence doc failed uri=%s: %v", uri, err))
			continue
		}
		doc, err := decodePreparedEvidenceDoc(body)
		if err != nil {
			warns = append(warns, fmt.Sprintf("decode evidence doc failed uri=%s: %v", uri, err))
			continue
		}
		doc.URI = uri
		doc.ID = firstNonEmpty(doc.ID, hit.ID)
		doc.Kind = firstNonEmpty(doc.Kind, asString(hit.StructData["kind"]))
		doc.Title = firstNonEmpty(doc.Title, asString(hit.StructData["title"]), hit.Title, hit.ID)
		if len(doc.CanonicalPaths) == 0 {
			doc.CanonicalPaths = compactUnique(stringSlice(hit.StructData["canonicalPaths"]))
		}
		out = append(out, doc)
	}
	return out, warns
}

func decodePreparedEvidenceDoc(body []byte) (preparedEvidenceDoc, error) {
	var payload struct {
		ID         string         `json:"id"`
		StructData map[string]any `json:"structData"`
		Content    struct {
			RawBytes string `json:"rawBytes"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return preparedEvidenceDoc{}, err
	}
	contentBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Content.RawBytes))
	if err != nil {
		return preparedEvidenceDoc{}, err
	}
	return preparedEvidenceDoc{
		ID:             strings.TrimSpace(payload.ID),
		Kind:           asString(payload.StructData["kind"]),
		Title:          asString(payload.StructData["title"]),
		Content:        strings.TrimSpace(string(contentBytes)),
		CanonicalPaths: compactUnique(stringSlice(payload.StructData["canonicalPaths"])),
	}, nil
}

func renderPreparedEvidenceDocs(docs []preparedEvidenceDoc) string {
	if len(docs) == 0 {
		return "No hydrated evidence documents were available."
	}
	var b strings.Builder
	for i, doc := range docs {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&b, "[Doc %d] kind=%s title=%s id=%s\n", i+1, firstNonEmpty(doc.Kind, "unknown"), firstNonEmpty(doc.Title, doc.ID), doc.ID)
		if len(doc.CanonicalPaths) > 0 {
			fmt.Fprintf(&b, "refs=%s\n", strings.Join(doc.CanonicalPaths, ", "))
		}
		if doc.URI != "" {
			fmt.Fprintf(&b, "uri=%s\n", doc.URI)
		}
		fmt.Fprintf(&b, "%s\n\n", doc.Content)
	}
	return strings.TrimSpace(b.String())
}

func parseGCSURI(uri string) (bucket string, object string, ok bool) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "gs://") {
		return "", "", false
	}
	rest := strings.TrimPrefix(uri, "gs://")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func stableDocID(corpusID, kind, seed string) string {
	return fmt.Sprintf("%s-%s-%s", shortSlug(kind), shortSlug(seed), shortHash(corpusID+"|"+kind+"|"+seed))
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])[:10]
}

func shortSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if !lastDash {
				b.WriteRune(r)
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s = strings.Trim(b.String(), "-_")
	if s == "" {
		return "item"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

func groundingEvidenceMap(evidence *ai.GroundingEvidence) map[string]any {
	if evidence == nil {
		return nil
	}
	return map[string]any{
		"hasDatastoreResults":  evidence.HasDatastoreResults,
		"datastoreFragmentIDs": append([]string(nil), evidence.DatastoreFragmentIDs...),
		"datastoreUris":        append([]string(nil), evidence.DatastoreURIs...),
		"hasWebResults":        evidence.HasWebResults,
	}
}

func uniqueStructValues(hits []vertexHit, key string) []string {
	out := make([]string, 0, len(hits))
	seen := map[string]struct{}{}
	for _, hit := range hits {
		value := asString(hit.StructData[key])
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedSetKeys(items map[string]struct{}) []string {
	out := make([]string, 0, len(items))
	for key := range items {
		if strings.TrimSpace(key) != "" {
			out = append(out, strings.TrimSpace(key))
		}
	}
	sort.Strings(out)
	return out
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func substituteRefs(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
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
	question := resolveQuestion(cfg, bundle)
	queryVariants := buildQueryVariants(question, bundle.Anchor, bundle.Lens)
	vertexHits := map[string][]vertexHit{
		"nodes":     {},
		"edges":     {},
		"artifacts": {},
		"facts":     {},
	}
	if shouldUseVertexRecall(bundle) {
		var warns []string
		vertexHits, warns = searchVertex(ctx, d.vertex, cfg, queryVariants)
		bundle.Warnings = append(bundle.Warnings, warns...)

		// Vertex is a recall layer for gaps, not a mandatory blocker when the lens already exposes a usable graph dossier.
		augmentBundleFromVertexHits(ctx, d, cfg, bundle, vertexHits)
	} else {
		bundle.Warnings = append(bundle.Warnings, "vertex recall skipped: graph dossier already sufficient")
	}

	report := &answerReport{
		Mode:         cfg.mode,
		ProjectID:    cfg.projectID,
		TenantID:     cfg.tenantID,
		Question:     question,
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
	lens, err := resolveLens(ctx, d, cfg, "")
	if err != nil {
		return nil, err
	}
	anchor, err := resolveAnchor(ctx, d, cfg, lens)
	if err != nil {
		return nil, err
	}
	if lens == nil {
		lens, err = resolveLens(ctx, d, cfg, anchor.CanonicalPath)
		if err != nil {
			return nil, err
		}
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
	fragmentsByPath, err := loadFragmentsByCanonicalPathsReadOnly(ctx, d, mapKeys(nodeRefs))
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
		if links, err := loadFragmentsByCanonicalPathsReadOnly(ctx, d, mapKeys(supplyLinkRefs)); err == nil {
			for cp, fragment := range links {
				if fragment == nil || fragment.Type != hmodels.FragmentTypeSupplyLink {
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

func resolveAnchor(ctx context.Context, d *deps, cfg config, lens *hmodels.Lens) (*hmodels.HorizonFragment, error) {
	if strings.TrimSpace(cfg.anchorPath) != "" {
		fragmentsByPath, err := loadFragmentsByCanonicalPathsReadOnly(ctx, d, []string{strings.TrimSpace(cfg.anchorPath)})
		if err != nil {
			return nil, err
		}
		if fragment := fragmentsByPath[strings.TrimSpace(cfg.anchorPath)]; fragment != nil {
			return fragment, nil
		}
		return nil, fmt.Errorf("anchor not found for path %q", strings.TrimSpace(cfg.anchorPath))
	}
	if lens != nil {
		if anchor := resolveAnchorFromLens(ctx, d, lens, cfg.question); anchor != nil {
			return anchor, nil
		}
	}
	query := strings.TrimSpace(cfg.anchorQuery)
	if query == "" {
		return nil, fmt.Errorf("anchor is empty; pass --anchor-path/--anchor-query or provide --lens-id with fragment-backed scope")
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
	scopeArtifactKeys := map[string]struct{}{}
	if lens != nil {
		for _, entry := range lens.Scope {
			key := strings.TrimSpace(hlens.ArtifactKeyFromScopeEntry(entry))
			if key != "" {
				scopeArtifactKeys[key] = struct{}{}
			}
		}
		for key := range lens.ScopeArtifacts {
			key = strings.TrimSpace(key)
			if key != "" {
				scopeArtifactKeys[key] = struct{}{}
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
		byLens, err := d.artifacts.ListByLens(ctx, lens.ID, maxInt(cfg.limitArtifacts*8, 256))
		if err == nil {
			filtered := make([]*hmodels.MaterializedArtifact, 0, len(byLens))
			for _, artifact := range byLens {
				if artifact == nil {
					continue
				}
				if _, ok := scopeArtifactKeys[strings.TrimSpace(artifact.CanonicalKey)]; ok {
					filtered = append(filtered, artifact)
					continue
				}
				if containsString(artifact.NodeRefs, anchor.CanonicalPath) || containsString(artifact.GeoRefs, anchor.CanonicalPath) || strings.Contains(strings.ToLower(artifact.DisplayName), strings.ToLower(fragmentDisplayName(anchor))) {
					filtered = append(filtered, artifact)
				}
			}
			push(filtered)
			if len(artifacts) < cfg.limitArtifacts {
				ranked := rankArtifactsForLens(byLens, scopeArtifactKeys, lens, anchor, cfg.question)
				push(ranked)
			}
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

func resolveQuestion(cfg config, bundle *graphBundle) string {
	if s := strings.TrimSpace(cfg.question); s != "" {
		return s
	}
	if bundle != nil && bundle.Lens != nil {
		if s := strings.TrimSpace(bundle.Lens.OriginalQuery); s != "" {
			return s
		}
		if s := strings.TrimSpace(bundle.Lens.Name); s != "" {
			return s
		}
	}
	if bundle != nil && bundle.Anchor != nil {
		return fragmentDisplayName(bundle.Anchor) + " supply chain"
	}
	return "internal knowledge answer"
}

func resolveAnchorFromLens(ctx context.Context, d *deps, lens *hmodels.Lens, question string) *hmodels.HorizonFragment {
	if lens == nil {
		return nil
	}
	candidates := lensAnchorCandidatePaths(lens)
	if len(candidates) == 0 {
		return nil
	}
	fragmentsByPath, err := loadFragmentsByCanonicalPathsReadOnly(ctx, d, candidates)
	if err != nil || len(fragmentsByPath) == 0 {
		return nil
	}
	scopeSet := make(map[string]struct{}, len(lens.Scope))
	for _, entry := range lens.Scope {
		if p := strings.TrimSpace(entry); strings.HasPrefix(p, "/") && !strings.HasPrefix(p, hmodels.DomainScopePrefix) && !strings.HasPrefix(p, hmodels.ArtifactScopePrefix) && !strings.HasPrefix(p, hmodels.QueryScopePrefix) {
			scopeSet[p] = struct{}{}
		}
	}
	questionTokens := textTokens(strings.TrimSpace(question + " " + lens.OriginalQuery + " " + lens.Name))
	var (
		best      *hmodels.HorizonFragment
		bestScore = -1
	)
	for _, cp := range candidates {
		fragment := fragmentsByPath[cp]
		if fragment == nil {
			continue
		}
		score := 0
		if _, ok := scopeSet[cp]; ok {
			score += 40
		}
		switch {
		case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/geo/"):
			score += 120
		case strings.HasPrefix(cp, "/market/"):
			score += 90
		case strings.HasPrefix(cp, "/fac/"):
			score += 50
		case strings.HasPrefix(cp, "/org/"):
			score += 45
		}
		for _, token := range questionTokens {
			if strings.Contains(strings.ToLower(cp), token) || strings.Contains(strings.ToLower(fragmentDisplayName(fragment)), token) {
				score += 6
			}
		}
		if best == nil || score > bestScore {
			best = fragment
			bestScore = score
		}
	}
	return best
}

func lensAnchorCandidatePaths(lens *hmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	paths := workscope.LensWorkbenchScopePaths(lens)
	filtered := make([]string, 0, len(paths))
	for _, path := range compactUnique(paths) {
		switch {
		case strings.HasPrefix(path, hmodels.DomainScopePrefix):
			continue
		case strings.HasPrefix(path, hmodels.ArtifactScopePrefix):
			continue
		case strings.HasPrefix(path, hmodels.QueryScopePrefix):
			continue
		case strings.HasPrefix(path, "/"):
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func rankArtifactsForLens(items []*hmodels.MaterializedArtifact, scopeArtifactKeys map[string]struct{}, lens *hmodels.Lens, anchor *hmodels.HorizonFragment, question string) []*hmodels.MaterializedArtifact {
	if len(items) == 0 {
		return nil
	}
	candidatePaths := map[string]struct{}{}
	for _, path := range lensAnchorCandidatePaths(lens) {
		candidatePaths[path] = struct{}{}
	}
	questionTokens := textTokens(question)
	type scored struct {
		item  *hmodels.MaterializedArtifact
		score int
	}
	scoredItems := make([]scored, 0, len(items))
	for _, artifact := range items {
		if artifact == nil {
			continue
		}
		score := 0
		if _, ok := scopeArtifactKeys[strings.TrimSpace(artifact.CanonicalKey)]; ok {
			score += 1000
		}
		if anchor != nil {
			if containsString(artifact.NodeRefs, anchor.CanonicalPath) || containsString(artifact.GeoRefs, anchor.CanonicalPath) || containsString(artifact.ZoneRefs, anchor.CanonicalPath) || containsString(artifact.AffectedZoneRefs, anchor.CanonicalPath) {
				score += 180
			}
		}
		score += 12 * overlapCount(candidatePaths, artifact.NodeRefs)
		score += 8 * overlapCount(candidatePaths, artifact.GeoRefs)
		score += 6 * overlapCount(candidatePaths, artifact.ZoneRefs)
		score += 6 * overlapCount(candidatePaths, artifact.AffectedZoneRefs)
		text := strings.ToLower(strings.TrimSpace(artifact.DisplayName + " " + artifact.CanonicalKey + " " + artifact.ProductAxis))
		for _, token := range questionTokens {
			if strings.Contains(text, token) {
				score += 5
			}
		}
		scoredItems = append(scoredItems, scored{item: artifact, score: score})
	}
	sort.Slice(scoredItems, func(i, j int) bool {
		if scoredItems[i].score == scoredItems[j].score {
			return scoredItems[i].item.UpdatedAt.After(scoredItems[j].item.UpdatedAt)
		}
		return scoredItems[i].score > scoredItems[j].score
	})
	out := make([]*hmodels.MaterializedArtifact, 0, len(scoredItems))
	for _, item := range scoredItems {
		out = append(out, item.item)
	}
	return out
}

func overlapCount(set map[string]struct{}, values []string) int {
	if len(set) == 0 || len(values) == 0 {
		return 0
	}
	n := 0
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		if _, ok := set[value]; ok {
			n++
		}
	}
	return n
}

func textTokens(s string) []string {
	fields := strings.Fields(strings.ToLower(strings.NewReplacer(
		"?", " ",
		",", " ",
		".", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
		"-", " ",
		"_", " ",
	).Replace(s)))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		out = append(out, field)
	}
	return compactUnique(out)
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

func shouldUseVertexRecall(bundle *graphBundle) bool {
	if bundle == nil {
		return true
	}
	routes := summarizeRoutes(bundle.Artifacts)
	switch {
	case len(bundle.Artifacts) >= 6 && len(routes) >= 6 && len(bundle.Edges) >= 24:
		return false
	case len(bundle.Artifacts) >= 10 && len(bundle.Edges) >= 12:
		return false
	default:
		return true
	}
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

func buildQueryVariants(question string, anchor *hmodels.HorizonFragment, lens *hmodels.Lens) []string {
	name := fragmentDisplayName(anchor)
	out := []string{
		question,
		name,
		name + " logistics",
		name + " supply chain",
		name + " routes transport",
		name + " import distribution retail wholesale",
	}
	if lens != nil {
		out = append(out,
			strings.TrimSpace(lens.Name),
			strings.TrimSpace(lens.OriginalQuery),
		)
		for _, query := range lens.DomainQueries {
			out = append(out,
				strings.TrimSpace(query.Title),
				strings.TrimSpace(query.Description),
				strings.TrimSpace(query.CanonicalPath),
			)
		}
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func loadFragmentsByCanonicalPathsReadOnly(ctx context.Context, d *deps, canonicalPaths []string) (map[string]*hmodels.HorizonFragment, error) {
	if d == nil || d.fs == nil || d.fs.Client() == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	paths := compactUnique(canonicalPaths)
	if len(paths) == 0 {
		return map[string]*hmodels.HorizonFragment{}, nil
	}
	out := make(map[string]*hmodels.HorizonFragment, len(paths))
	const chunkSize = 30
	col := d.fs.Client().Collection("horizon_fragments")
	for i := 0; i < len(paths); i += chunkSize {
		end := i + chunkSize
		if end > len(paths) {
			end = len(paths)
		}
		chunk := paths[i:end]
		docs, err := col.
			Where("tenantId", "==", "global").
			Where("canonicalPath", "in", chunk).
			Limit(len(chunk) * 3).
			Documents(ctx).
			GetAll()
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			if doc == nil || !doc.Exists() {
				continue
			}
			var fragment hmodels.HorizonFragment
			if err := doc.DataTo(&fragment); err != nil {
				continue
			}
			cp := strings.TrimSpace(fragment.CanonicalPath)
			if cp == "" {
				continue
			}
			if current, ok := out[cp]; !ok || preferFragment(fragment, *current) {
				out[cp] = &fragment
			}
		}
	}
	return out, nil
}

func preferFragment(candidate, current hmodels.HorizonFragment) bool {
	if candidate.Status == "active" && current.Status != "active" {
		return true
	}
	if candidate.Status != "active" && current.Status == "active" {
		return false
	}
	if candidate.UpdatedAt.After(current.UpdatedAt) {
		return true
	}
	if candidate.UpdatedAt.Equal(current.UpdatedAt) && strings.TrimSpace(candidate.ID) < strings.TrimSpace(current.ID) {
		return true
	}
	return false
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
