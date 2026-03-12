package horizon

import (
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/horizon"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonqa "github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonwatch "github.com/igorkirisyuk/sanskrite/internal/horizon/watch"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HorizonHandlers содержит все HTTP handlers для Horizon API
type HorizonHandlers struct {
	service                   *horizon.HorizonService
	lensItemsHandler          *LensItemsHandler
	genAIHandler              *httputil.GenAIHandler
	profilesHandler           *ProfilesHandler
	docsRepo                  *corerepo.DocumentsRepository
	usersRepo                 *corerepo.UsersRepository
	interactions              *corerepo.InteractionsRepository
	factsRepo                 *corerepo.OntologyFactsRepository
	exposurePathsRepo         *horizonrepo.ExposurePathsRepository
	riskJobsRepo              *horizonrepo.RiskJobsRepository
	propagationProfileLookup  *horizonprop.ProfileLookup
	signalPipeline            *horizonscout.SignalPipeline
	mergeFragmentsRepo        *horizonrepo.FragmentsMergeRepository
	materializedArtifactsRepo *horizonrepo.MaterializedArtifactsRepository
	newsByFragmentsCanvas     *discovery.NewsByFragmentsCanvas

	// Lens Q&A (Analysis view)
	qaEngine                *horizonqa.Engine
	qaContextBuilder        *horizonqa.AnswerContextBuilder
	qaSynthesizer           *horizonqa.AnswerSynthesizer
	qaSessionsRepo          *horizonrepo.QASessionsRepository
	qaSufficiencyChecker    *horizonqa.SufficiencyChecker
	qaSignalGapDetector     *horizonqa.SignalGapDetector
	qaGroundingQueryBuilder *horizonqa.GroundingQueryBuilder
	qaBestEffortFiller      *horizonqa.BestEffortFiller
	scoutingJobsRepo        *horizonrepo.ScoutingJobsRepository

	// Job-level telemetry (LLM cost tracking)
	jobTelemetryRepo *horizonrepo.JobTelemetryRepository
	watchService     *horizonwatch.Service
}

// NewHorizonHandlers создает новые handlers
func NewHorizonHandlers(service *horizon.HorizonService) *HorizonHandlers {
	return &HorizonHandlers{
		service: service,
	}
}

// WithLensItemsHandler добавляет LensItemsHandler
func (h *HorizonHandlers) WithLensItemsHandler(handler *LensItemsHandler) *HorizonHandlers {
	h.lensItemsHandler = handler
	return h
}

// WithGenAIHandler добавляет GenAIHandler для вызова LLM
func (h *HorizonHandlers) WithGenAIHandler(handler *httputil.GenAIHandler) *HorizonHandlers {
	h.genAIHandler = handler
	return h
}

// WithProfilesHandler добавляет ProfilesHandler
func (h *HorizonHandlers) WithProfilesHandler(handler *ProfilesHandler) *HorizonHandlers {
	h.profilesHandler = handler
	return h
}

// WithInteractionsRepo wires InteractionsRepository for persisting Horizon-side artifacts (e.g., graph queries).
func (h *HorizonHandlers) WithInteractionsRepo(interactions *corerepo.InteractionsRepository) *HorizonHandlers {
	h.interactions = interactions
	return h
}

// WithDocumentsRepo wires the generic DocumentsRepository (rag_documents) for Horizon-side helpers.
func (h *HorizonHandlers) WithDocumentsRepo(docs *corerepo.DocumentsRepository) *HorizonHandlers {
	h.docsRepo = docs
	return h
}

// WithUsersRepo wires Core UsersRepository for Human-in-the-Loop admin gating.
func (h *HorizonHandlers) WithUsersRepo(users *corerepo.UsersRepository) *HorizonHandlers {
	h.usersRepo = users
	return h
}

// WithOntologyFactsRepo wires core OntologyFactsRepository (ontology_facts) for debug/manual injections.
func (h *HorizonHandlers) WithOntologyFactsRepo(facts *corerepo.OntologyFactsRepository) *HorizonHandlers {
	h.factsRepo = facts
	return h
}

// WithExposurePathsRepo wires ExposurePathsRepository for strict applicability gating in events:inject.
func (h *HorizonHandlers) WithExposurePathsRepo(r *horizonrepo.ExposurePathsRepository) *HorizonHandlers {
	h.exposurePathsRepo = r
	return h
}

func (h *HorizonHandlers) WithRiskJobsRepo(r *horizonrepo.RiskJobsRepository) *HorizonHandlers {
	h.riskJobsRepo = r
	return h
}

// WithPropagationProfileLookup wires ProfileLookup for scenario/axis-based signal propagation.
func (h *HorizonHandlers) WithPropagationProfileLookup(lookup *horizonprop.ProfileLookup) *HorizonHandlers {
	h.propagationProfileLookup = lookup
	return h
}

// WithSignalPipeline wires SignalPipeline for manual 12-axis extraction (events:extract-signals).
func (h *HorizonHandlers) WithSignalPipeline(p *horizonscout.SignalPipeline) *HorizonHandlers {
	h.signalPipeline = p
	return h
}

// WithMergeFragmentsRepo wires FragmentsMergeRepository (single entry point for merge).
func (h *HorizonHandlers) WithMergeFragmentsRepo(r *horizonrepo.FragmentsMergeRepository) *HorizonHandlers {
	h.mergeFragmentsRepo = r
	return h
}

// WithMaterializedArtifactsRepo wires MaterializedArtifactsRepository for supply chains by-node endpoint.
func (h *HorizonHandlers) WithMaterializedArtifactsRepo(r *horizonrepo.MaterializedArtifactsRepository) *HorizonHandlers {
	h.materializedArtifactsRepo = r
	return h
}

// WithNewsByFragmentsCanvas wires the news-by-fragments canvas (search → facts → 12-axis → propagation).
func (h *HorizonHandlers) WithNewsByFragmentsCanvas(c *discovery.NewsByFragmentsCanvas) *HorizonHandlers {
	h.newsByFragmentsCanvas = c
	return h
}

// WithQAEngine wires the Lens Q&A engine (Decompose → Simulate → Synthesize).
func (h *HorizonHandlers) WithQAEngine(engine *horizonqa.Engine, ctxBuilder *horizonqa.AnswerContextBuilder, synthesizer *horizonqa.AnswerSynthesizer, sessionsRepo *horizonrepo.QASessionsRepository) *HorizonHandlers {
	h.qaEngine = engine
	h.qaContextBuilder = ctxBuilder
	h.qaSynthesizer = synthesizer
	h.qaSessionsRepo = sessionsRepo
	return h
}

// WithQASufficiencyChecker wires the sufficiency checker for the QA handler.
func (h *HorizonHandlers) WithQASufficiencyChecker(c *horizonqa.SufficiencyChecker) *HorizonHandlers {
	h.qaSufficiencyChecker = c
	return h
}

// WithQASignalGapDetector wires the signal gap detector for the QA handler.
func (h *HorizonHandlers) WithQASignalGapDetector(d *horizonqa.SignalGapDetector) *HorizonHandlers {
	h.qaSignalGapDetector = d
	return h
}

// WithQAGroundingQueryBuilder wires the grounding query builder for the QA handler.
func (h *HorizonHandlers) WithQAGroundingQueryBuilder(b *horizonqa.GroundingQueryBuilder) *HorizonHandlers {
	h.qaGroundingQueryBuilder = b
	return h
}

// WithQABestEffortFiller wires the best-effort signal filler for the QA handler.
func (h *HorizonHandlers) WithQABestEffortFiller(f *horizonqa.BestEffortFiller) *HorizonHandlers {
	h.qaBestEffortFiller = f
	return h
}

// WithScoutingJobsRepo wires the ScoutingJobsRepository for auto-scout in QA.
func (h *HorizonHandlers) WithScoutingJobsRepo(r *horizonrepo.ScoutingJobsRepository) *HorizonHandlers {
	h.scoutingJobsRepo = r
	return h
}

// WithJobTelemetryRepo wires the JobTelemetryRepository for per-job LLM cost tracking.
func (h *HorizonHandlers) WithJobTelemetryRepo(r *horizonrepo.JobTelemetryRepository) *HorizonHandlers {
	h.jobTelemetryRepo = r
	return h
}

func (h *HorizonHandlers) WithWatchService(svc *horizonwatch.Service) *HorizonHandlers {
	h.watchService = svc
	return h
}

// RegisterRoutes регистрирует все routes для Horizon API
func (h *HorizonHandlers) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Intent precheck (NL -> taxonomy-aligned intent)
	mux.Handle("/api/horizon/intent/resolve", authMiddleware(http.HandlerFunc(h.ResolveIntent)))

	// Lens endpoints
	mux.Handle("/api/horizon/lenses", authMiddleware(http.HandlerFunc(h.HandleLensesRoot)))
	// Lens preflight: resolve intent + recommend mode (no writes)
	mux.Handle("/api/horizon/lenses/preflight", authMiddleware(http.HandlerFunc(h.LensPreflight)))
	// Lens creation from NL query (resolve intent -> create lens)
	mux.Handle("/api/horizon/lenses/from-query", authMiddleware(http.HandlerFunc(h.CreateLensFromQuery)))
	mux.Handle("/api/horizon/lenses/", authMiddleware(http.HandlerFunc(h.HandleLensRoutes)))

	// Fragment endpoints
	mux.Handle("/api/horizon/fragments", authMiddleware(http.HandlerFunc(h.HandleFragmentsRoot)))
	mux.Handle("/api/horizon/fragments/by-path", authMiddleware(http.HandlerFunc(h.GetFragmentByPath)))
	mux.Handle("/api/horizon/fragments/", authMiddleware(http.HandlerFunc(h.HandleFragmentRoutes)))

	// Supply chains: materialized artifacts by canonical path
	mux.Handle("/api/horizon/supply-chains/by-node", authMiddleware(http.HandlerFunc(h.GetSupplyChainsByNode)))
	mux.Handle("/api/horizon/supply-chains/by-lens", authMiddleware(http.HandlerFunc(h.GetSupplyChainsByLens)))
	mux.Handle("/api/horizon/supply-chains/search", authMiddleware(http.HandlerFunc(h.SearchSupplyChains)))
	mux.Handle("/api/horizon/geo-objects", authMiddleware(http.HandlerFunc(h.ListGeoObjects)))
	mux.Handle("/api/horizon/geo-objects/by-paths", authMiddleware(http.HandlerFunc(h.ListGeoObjectsByPaths)))
	mux.Handle("/api/horizon/map-activity", authMiddleware(http.HandlerFunc(h.ListMapActivity)))

	// Fragment profiles (direct access by profile ID)
	if h.profilesHandler != nil {
		mux.Handle("/api/horizon/profiles/", authMiddleware(http.HandlerFunc(h.profilesHandler.handleProfileRoutes)))
	}

	// Rollup & Explain endpoints (Signals & Semantic Propagation)
	mux.Handle("/api/horizon/rollup/", authMiddleware(http.HandlerFunc(h.HandleRollupRoutes)))
	mux.Handle("/api/horizon/risk/concentration", authMiddleware(http.HandlerFunc(h.GetRiskConcentration)))
	mux.Handle("/api/horizon/risk/jobs", authMiddleware(http.HandlerFunc(h.HandleRiskJobsRoot)))
	mux.Handle("/api/horizon/risk/jobs/", authMiddleware(http.HandlerFunc(h.HandleRiskJobRoutes)))
	mux.Handle("/api/horizon/watch", authMiddleware(http.HandlerFunc(h.HandleWatchRoot)))
	mux.Handle("/api/horizon/watch/search", authMiddleware(http.HandlerFunc(h.SearchWatchTargets)))
	mux.Handle("/api/horizon/watch/targets", authMiddleware(http.HandlerFunc(h.HandleWatchTargetsRoot)))
	mux.Handle("/api/horizon/watch/targets/", authMiddleware(http.HandlerFunc(h.HandleWatchTargetRoutes)))
	mux.Handle("/api/horizon/watch/preferences", authMiddleware(http.HandlerFunc(h.UpdateWatchPreferences)))
	mux.Handle("/api/horizon/watch/briefs/", authMiddleware(http.HandlerFunc(h.HandleWatchBriefRoutes)))

	// AI explanations (cached markdown explanations for nodes/edges)
	mux.Handle("/api/horizon/ai-explanations/", authMiddleware(http.HandlerFunc(h.HandleAIExplanationsRoutes)))

	// DependencyEdge endpoints
	mux.Handle("/api/horizon/edges", authMiddleware(http.HandlerFunc(h.CreateEdge)))
	mux.Handle("/api/horizon/edges/", authMiddleware(http.HandlerFunc(h.HandleEdgeRoutes)))

	// Scout endpoints
	mux.Handle("/api/horizon/scout/jobs/", authMiddleware(http.HandlerFunc(h.HandleScoutRoutes)))

	// Propagation endpoints
	mux.Handle("/api/horizon/propagation/status", authMiddleware(http.HandlerFunc(h.GetPropagationStatus)))
	mux.Handle("/api/horizon/propagation/trigger", authMiddleware(http.HandlerFunc(h.TriggerPropagation)))
	mux.Handle("/api/horizon/propagation/trace", authMiddleware(http.HandlerFunc(h.TracePropagationPaths)))

	// Debug: inject an event and run SignalDescriptor pipeline (extract → seed → propagate)
	mux.Handle("/api/horizon/events:inject", authMiddleware(http.HandlerFunc(h.InjectEvent)))
	// Manual: extract 12-axis signals for existing event facts (no auto-invocation)
	mux.Handle("/api/horizon/events:extract-signals", authMiddleware(http.HandlerFunc(h.ExtractEventSignals)))
	mux.Handle("/api/horizon/canvas/news-by-fragments", authMiddleware(http.HandlerFunc(h.NewsByFragments)))
	mux.Handle("/api/horizon/ontology-facts/by-canonical-path", authMiddleware(http.HandlerFunc(h.GetFactsByCanonicalPath)))

	// Domain scenario: Encode → Propagate → Decode (policy-impact, etc.)
	mux.Handle("/api/horizon/scenarios/run", authMiddleware(http.HandlerFunc(h.RunScenario)))

	// Entity search (Live Entity Recognition)
	mux.Handle("/api/horizon/entities/search", authMiddleware(http.HandlerFunc(h.SearchEntities)))

	// Co-Pilot chat
	mux.Handle("/api/horizon/co-pilot/chat", authMiddleware(http.HandlerFunc(h.CoPilotChat)))

	// Register external feed proxies (unauthenticated)
	h.RegisterFeedRoutes(mux)
}

// HandleFragmentsRoot handles /api/horizon/fragments (list fragments via lens scope).
// GET /api/horizon/fragments?lensId={lensId}
func (h *HorizonHandlers) HandleFragmentsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.ListFragments(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleLensesRoot обрабатывает корневой путь /api/horizon/lenses
func (h *HorizonHandlers) HandleLensesRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateLens(w, r)
	case http.MethodGet:
		h.ListLenses(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleRollupRoutes handles rollup and explain requests for fragments.
// GET /api/horizon/rollup/{fragmentID}?kind=risk
// GET /api/horizon/rollup/{fragmentID}/explain?kind=risk
func (h *HorizonHandlers) HandleRollupRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/rollup/")
	if path == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	pathParts := strings.Split(path, "/")
	fragmentID := pathParts[0]
	metricKind := r.URL.Query().Get("kind")
	if metricKind == "" {
		metricKind = "risk" // Default to risk
	}

	// Check if this is an explain request
	if len(pathParts) > 1 && pathParts[1] == "explain" {
		h.GetFragmentExplain(w, r, fragmentID, metricKind)
		return
	}

	h.GetFragmentRollup(w, r, fragmentID, metricKind)
}

// HandleLensRoutes обрабатывает все запросы к /api/horizon/lenses/{id} и подпутям
func (h *HorizonHandlers) HandleLensRoutes(w http.ResponseWriter, r *http.Request) {
	// Извлекаем путь после /api/horizon/lenses/
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	if path == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Разбираем путь на части
	pathParts := strings.Split(path, "/")

	// Если есть вторая часть пути, это подпуть
	if len(pathParts) > 1 {
		subPath := pathParts[1]
		switch subPath {
		case "product-io:apply":
			h.ApplyProductIO(w, r)
			return
		case "items":
			// Delegate to LensItemsHandler
			if h.lensItemsHandler != nil {
				// Extract remaining path after /items
				if len(pathParts) > 2 {
					// /api/horizon/lenses/{lensId}/items/{itemId}
					switch r.Method {
					case http.MethodPatch:
						h.lensItemsHandler.UpdateLensItem(w, r)
						return
					case http.MethodDelete:
						h.lensItemsHandler.DeleteLensItem(w, r)
						return
					default:
						http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
						return
					}
				} else {
					// /api/horizon/lenses/{lensId}/items
					switch r.Method {
					case http.MethodGet:
						h.lensItemsHandler.ListLensItems(w, r)
						return
					case http.MethodPost:
						h.lensItemsHandler.CreateLensItem(w, r)
						return
					default:
						http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
						return
					}
				}
			}
			return
		case "assembly-status":
			h.GetAssemblyStatus(w, r)
			return
		case "cockpit":
			h.GetCockpitData(w, r)
			return
		case "simulate":
			h.SimulateWhatIf(w, r)
			return
		case "recalculate":
			h.RecalculateLens(w, r)
			return
		case "transfer":
			h.TransferLens(w, r)
			return
		case "scope-scout":
			h.ScopeScoutLensNode(w, r)
			return
		case "scope-scout-query":
			h.ScopeScoutLensQuery(w, r)
			return
		case "scope-scout-full":
			h.ScopeScoutLensFull(w, r)
			return
		case "qa":
			h.LensQA(w, r)
			return
		case "telemetry":
			h.HandleTelemetryRoutes(w, r)
			return
		case "graph-query":
			h.QueryGraph(w, r)
			return
		case "graph-intent":
			if len(pathParts) < 3 {
				http.Error(w, "Graph intent action is required", http.StatusBadRequest)
				return
			}
			action := strings.TrimSpace(pathParts[2])
			switch action {
			case "plan":
				h.PlanGraphIntent(w, r)
			case "apply":
				h.ApplyGraphIntent(w, r)
			case "dry-run":
				h.DryRunGraphIntent(w, r)
			default:
				http.Error(w, "Unknown graph-intent action", http.StatusNotFound)
			}
			return
		case "control-corridor":
			h.QueryControlCorridor(w, r)
			return
		case "artifacts":
			// Check for /artifacts/stats or /artifacts/context
			if len(pathParts) > 2 {
				switch pathParts[2] {
				case "stats":
					h.GetLensArtifactStats(w, r)
					return
				case "context":
					h.GetLensArtifactContext(w, r)
					return
				}
			}
			// /api/horizon/lenses/{lensId}/artifacts
			switch r.Method {
			case http.MethodGet:
				h.ListLensArtifacts(w, r)
			case http.MethodDelete:
				h.DeleteLensArtifacts(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		default:
			http.Error(w, "Unknown subpath", http.StatusNotFound)
			return
		}
	}

	// Если нет подпути, это операции с самим Lens
	switch r.Method {
	case http.MethodGet:
		h.GetLens(w, r)
	case http.MethodPut:
		h.UpdateLens(w, r)
	case http.MethodDelete:
		h.DeleteLens(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleFragmentRoutes обрабатывает все запросы к /api/horizon/fragments/{id} и подпутям
func (h *HorizonHandlers) HandleFragmentRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	if path == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	fragmentID := strings.TrimSpace(parts[0])
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	// Проверяем подпути
	if h.profilesHandler != nil {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			switch r.Method {
			case http.MethodGet:
				h.profilesHandler.getProfileByFragmentID(w, r, fragmentID)
				return
			case http.MethodDelete:
				h.profilesHandler.deleteProfileByFragmentID(w, r, fragmentID)
				return
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/profile/refresh") {
			h.profilesHandler.refreshProfile(w, r, fragmentID)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/profile/versions") {
			h.profilesHandler.listProfileVersions(w, r, fragmentID)
			return
		}
	}

	if strings.HasSuffix(r.URL.Path, "/ensure-entity") {
		h.EnsureFragmentEntity(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/entity") {
		h.GetFragmentEntity(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/enrich-entity") {
		h.EnrichFragmentEntity(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/product-io:extract") {
		h.ExtractProductIO(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/dependencies") {
		h.GetFragmentDependencies(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/trace") {
		h.GetFragmentTrace(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/metrics") {
		h.GetFragmentMetrics(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/provenance") {
		h.GetFragmentProvenance(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/interpretation") {
		h.GetFragmentInterpretation(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/versions") {
		h.GetFragmentVersions(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/refresh") {
		h.RefreshFragment(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/expand-materials") {
		h.ExpandProductMaterials(w, r)
		return
	}

	// Если нет подпути, это операции с самим Fragment
	switch r.Method {
	case http.MethodGet:
		h.GetFragment(w, r)
	case http.MethodDelete:
		h.DeleteFragment(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleEdgeRoutes обрабатывает все запросы к /api/horizon/edges/{id}
func (h *HorizonHandlers) HandleEdgeRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/edges/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}
	parts := strings.Split(path, "/")
	edgeID := strings.TrimSpace(parts[0])
	if edgeID == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}

	// Human-in-the-loop edge curation: POST /api/horizon/edges/{id}/hitl
	if len(parts) > 1 && strings.EqualFold(strings.TrimSpace(parts[1]), "hitl") {
		h.HumanInTheLoopEdge(w, r, edgeID)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.GetEdge(w, r)
	case http.MethodPut:
		h.UpdateEdge(w, r)
	case http.MethodDelete:
		h.DeleteEdge(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleScoutRoutes обрабатывает все запросы к /api/horizon/scout/jobs/{id}
func (h *HorizonHandlers) HandleScoutRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/scout/jobs/")
	if path == "" {
		// Список jobs
		h.ListScoutingJobs(w, r)
		return
	}

	// Получение конкретного job
	h.GetScoutingJob(w, r)
}

// GetAssemblyStatus, GetCockpitData, SimulateWhatIf реализованы в assembly.go и cockpit.go
