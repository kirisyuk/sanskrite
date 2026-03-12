package horizon

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonrisk "github.com/igorkirisyuk/sanskrite/internal/horizon/risk"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scopeagent"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonwatch "github.com/igorkirisyuk/sanskrite/internal/horizon/watch"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/watchdog"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HorizonService - главный сервис модуля horizon
type HorizonService struct {
	deps HorizonServiceDependencies

	// Подсистемы (будут инициализированы на соответствующих этапах)
	scoutEngine       *scout.ScoutEngine
	propagationEngine *propagation.PropagationEngine
	watchdogService   *watchdog.WatchdogService
	lensService       *lens.LensService
	appetiteResolver  *horizonrisk.AppetiteResolver
	concentration     *horizonrisk.ConcentrationAnalyzer
	scopeAgentService *scopeagent.Service
	watchService      *horizonwatch.Service

	// Репозитории horizon
	fragmentsRepo        *repo.HorizonFragmentsRepository
	framesRepo           *repo.FramesRepository
	lensesRepo           *repo.LensesRepository
	edgesRepo            *repo.DependencyEdgesRepository
	watchdogRepo         *repo.WatchdogConfigsRepository
	scoutingJobsRepo     *repo.ScoutingJobsRepository
	fragmentVersionsRepo *repo.HorizonFragmentVersionsRepository
	lensArtifactsRepo    *repo.LensArtifactsRepository // Phase 2 Strategic Overlay artifacts
	domainAnswersRepo    *repo.DomainAnswerSnapshotsRepository
	ontologyFactsRepo    *corerepo.OntologyFactsRepository
	scoutExecutionReady  bool
	scoutExecutionReason string
}

// NewHorizonService создает новый экземпляр HorizonService с валидацией зависимостей
func NewHorizonService(
	deps HorizonServiceDependencies,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	framesRepo *repo.FramesRepository,
	lensesRepo *repo.LensesRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	watchdogRepo *repo.WatchdogConfigsRepository,
	scoutingJobsRepo *repo.ScoutingJobsRepository,
	fragmentVersionsRepo *repo.HorizonFragmentVersionsRepository,
) (*HorizonService, error) {
	// Валидация обязательных зависимостей
	if deps.AIExecutor == nil {
		return nil, fmt.Errorf("AIExecutor is required")
	}
	if deps.EmbeddingProvider == nil {
		return nil, fmt.Errorf("EmbeddingProvider is required")
	}
	// VertexSearchClient is optional: ScoutPlanner and SearchFragments can fall back to Firestore/doc heuristics.
	// In production, it SHOULD be configured for quality/recall, but we must not hard-fail Horizon initialization.
	if deps.AgentJobsRepo == nil {
		return nil, fmt.Errorf("AgentJobsRepo is required")
	}
	if deps.RealtimePublisher == nil {
		return nil, fmt.Errorf("RealtimePublisher is required")
	}
	if deps.DataSourcesRepo == nil {
		return nil, fmt.Errorf("DataSourcesRepo is required")
	}
	if deps.DocumentsRepo == nil {
		return nil, fmt.Errorf("DocumentsRepo is required")
	}
	if deps.Crawler == nil {
		return nil, fmt.Errorf("Crawler is required")
	}
	if deps.Logger == nil {
		return nil, fmt.Errorf("Logger is required")
	}

	// Валидация репозиториев
	if fragmentsRepo == nil {
		return nil, fmt.Errorf("HorizonFragmentsRepository is required")
	}
	if framesRepo == nil {
		return nil, fmt.Errorf("FramesRepository is required")
	}
	if lensesRepo == nil {
		return nil, fmt.Errorf("LensesRepository is required")
	}
	if edgesRepo == nil {
		return nil, fmt.Errorf("DependencyEdgesRepository is required")
	}
	if watchdogRepo == nil {
		return nil, fmt.Errorf("WatchdogConfigsRepository is required")
	}
	if scoutingJobsRepo == nil {
		return nil, fmt.Errorf("ScoutingJobsRepository is required")
	}
	if fragmentVersionsRepo == nil {
		return nil, fmt.Errorf("HorizonFragmentVersionsRepository is required")
	}

	return &HorizonService{
		deps:                 deps,
		fragmentsRepo:        fragmentsRepo,
		framesRepo:           framesRepo,
		lensesRepo:           lensesRepo,
		edgesRepo:            edgesRepo,
		watchdogRepo:         watchdogRepo,
		scoutingJobsRepo:     scoutingJobsRepo,
		fragmentVersionsRepo: fragmentVersionsRepo,
		// Подсистемы будут инициализированы на соответствующих этапах
	}, nil
}

// SetScoutEngine устанавливает Scout Engine (вызывается на этапе 2)
func (s *HorizonService) SetScoutEngine(scoutEngine *scout.ScoutEngine) {
	s.scoutEngine = scoutEngine
}

// SetPropagationEngine устанавливает Propagation Engine (вызывается на этапе 4)
func (s *HorizonService) SetPropagationEngine(propagationEngine *propagation.PropagationEngine) {
	s.propagationEngine = propagationEngine
}

// SetWatchdogService устанавливает Watchdog Service (вызывается на этапе 5)
func (s *HorizonService) SetWatchdogService(watchdogService *watchdog.WatchdogService) {
	s.watchdogService = watchdogService
}

// SetLensService устанавливает Lens Service (вызывается на этапе 3)
func (s *HorizonService) SetLensService(lensService *lens.LensService) {
	s.lensService = lensService
}

func (s *HorizonService) SetScopeAgentService(scopeAgentService *scopeagent.Service) {
	s.scopeAgentService = scopeAgentService
}

func (s *HorizonService) SetWatchService(watchService *horizonwatch.Service) {
	s.watchService = watchService
}

// Scout возвращает Scout Engine
func (s *HorizonService) Scout() *scout.ScoutEngine {
	return s.scoutEngine
}

// Propagation возвращает Propagation Engine
func (s *HorizonService) Propagation() *propagation.PropagationEngine {
	return s.propagationEngine
}

// Watchdog возвращает Watchdog Service
func (s *HorizonService) Watchdog() *watchdog.WatchdogService {
	return s.watchdogService
}

// Lens возвращает Lens Service
func (s *HorizonService) Lens() *lens.LensService {
	return s.lensService
}

func (s *HorizonService) ScopeAgents() *scopeagent.Service {
	return s.scopeAgentService
}

func (s *HorizonService) Watch() *horizonwatch.Service {
	return s.watchService
}

// Fragments возвращает HorizonFragmentsRepository
func (s *HorizonService) Fragments() *repo.HorizonFragmentsRepository {
	return s.fragmentsRepo
}

// EmbeddingProvider возвращает EmbeddingProvider из зависимостей
func (s *HorizonService) EmbeddingProvider() ai.EmbeddingProvider {
	return s.deps.EmbeddingProvider
}

// AIExecutor возвращает AIExecutor из зависимостей
func (s *HorizonService) AIExecutor() *ai.AITaskExecutor {
	return s.deps.AIExecutor
}

// ScoutingJobs возвращает ScoutingJobsRepository
func (s *HorizonService) ScoutingJobs() *repo.ScoutingJobsRepository {
	return s.scoutingJobsRepo
}

// Edges возвращает DependencyEdgesRepository
func (s *HorizonService) Edges() *repo.DependencyEdgesRepository {
	return s.edgesRepo
}

// FragmentVersions возвращает HorizonFragmentVersionsRepository
func (s *HorizonService) FragmentVersions() *repo.HorizonFragmentVersionsRepository {
	return s.fragmentVersionsRepo
}

// LensArtifacts возвращает LensArtifactsRepository for Phase 2 Strategic Overlay
func (s *HorizonService) LensArtifacts() *repo.LensArtifactsRepository {
	return s.lensArtifactsRepo
}

// SetLensArtifactsRepo устанавливает LensArtifactsRepository (optional, for Phase 2 Strategic Overlay)
func (s *HorizonService) SetLensArtifactsRepo(repo *repo.LensArtifactsRepository) {
	s.lensArtifactsRepo = repo
}

// DomainAnswerSnapshots returns DomainAnswerSnapshotsRepository.
func (s *HorizonService) DomainAnswerSnapshots() *repo.DomainAnswerSnapshotsRepository {
	return s.domainAnswersRepo
}

// SetDomainAnswerSnapshotsRepo wires DomainAnswerSnapshotsRepository.
func (s *HorizonService) SetDomainAnswerSnapshotsRepo(repo *repo.DomainAnswerSnapshotsRepository) {
	s.domainAnswersRepo = repo
}

// SetOntologyFactsRepository wires ontology_facts access for semantics-aware rollups.
func (s *HorizonService) SetOntologyFactsRepository(factsRepo *corerepo.OntologyFactsRepository) {
	s.ontologyFactsRepo = factsRepo
}

func (s *HorizonService) SetRiskServices(appetite *horizonrisk.AppetiteResolver, concentration *horizonrisk.ConcentrationAnalyzer) {
	s.appetiteResolver = appetite
	s.concentration = concentration
}

func (s *HorizonService) GetConcentrationSnapshot(ctx context.Context, tenantID, polarity string, limit int) (*horizonrisk.ConcentrationSnapshot, error) {
	if s == nil || s.concentration == nil {
		return &horizonrisk.ConcentrationSnapshot{Polarity: polarity}, nil
	}
	return s.concentration.Snapshot(ctx, tenantID, polarity, limit)
}

// RealtimePublisher exposes the underlying realtime publisher for HTTP handlers that need to emit
// events directly (best-effort). Prefer emitting from domain services when possible.
func (s *HorizonService) RealtimePublisher() *realtime.Publisher {
	if s == nil {
		return nil
	}
	return s.deps.RealtimePublisher
}

// SetScoutExecutionRuntime stores runtime readiness for scout execution (Cloud Tasks or fallback worker).
// It is set during server bootstrap and read by HTTP handlers to fail fast when pipeline execution is unavailable.
func (s *HorizonService) SetScoutExecutionRuntime(ready bool, reason string) {
	if s == nil {
		return
	}
	s.scoutExecutionReady = ready
	s.scoutExecutionReason = strings.TrimSpace(reason)
}

// ScoutExecutionRuntime returns whether scout execution is currently available and the status reason code.
func (s *HorizonService) ScoutExecutionRuntime() (bool, string) {
	if s == nil {
		return false, "horizon_service_unavailable"
	}
	return s.scoutExecutionReady, strings.TrimSpace(s.scoutExecutionReason)
}

// HealthCheck проверяет состояние сервиса
func (s *HorizonService) HealthCheck(ctx context.Context) error {
	// Базовая проверка доступности репозиториев
	// Более детальные проверки будут добавлены на соответствующих этапах
	return nil
}

// SearchFragments выполняет семантический поиск по фрагментам через Vertex AI Search
func (s *HorizonService) SearchFragments(ctx context.Context, query string, limit int) ([]*models.HorizonFragment, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// 1) Vertex AI Search (semantic). Best-effort: if it fails, we fall back to Firestore prefix search.
	var vertexFragments []*models.HorizonFragment
	var vertexErr error

	// Horizon fragments are searched in a dedicated Vertex AI Search Data Store.
	// IMPORTANT: Filtering by structData.* is not supported in our current "document" schema datastore,
	// so we avoid filters and instead rely on store isolation.
	if s.deps.VertexSearchClient != nil && s.deps.VertexSearchClient.IsConfigured() {
		vertexClient := s.deps.VertexSearchClient
		if ds := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID")); ds != "" {
			vertexClient = vertexClient.WithDataStore(ds)
		}

		req := &ai.SearchRequest{
			Query:    q,
			PageSize: limit,
		}

		resp, err := vertexClient.Search(ctx, req)
		if err != nil {
			vertexErr = fmt.Errorf("vertex search failed: %w", err)
		} else {
			log.Printf("[SearchFragments] Vertex search returned %d results", len(resp.Results))

			for i, res := range resp.Results {
				// Extract fragment ID
				id := res.Document.ID
				id = strings.TrimSuffix(id, ".json")
				log.Printf("[SearchFragments] Result %d: ID=%s, URI=%s", i, id, res.Document.URI)

				fragment, err := s.fragmentsRepo.Get(ctx, id)
				if err != nil {
					log.Printf("[SearchFragments] Failed to get fragment %s: %v", id, err)
					continue
				}
				vertexFragments = append(vertexFragments, fragment)
			}

			log.Printf("[SearchFragments] Successfully loaded %d fragments from %d search results", len(vertexFragments), len(resp.Results))
		}
	}

	// 2) Firestore prefix search (typeahead). Best-effort: used as supplement + fallback.
	var fsFragments []*models.HorizonFragment
	var fsErr error
	if s.fragmentsRepo != nil {
		// Prefix search is only useful for short UI queries; keep it cheap.
		if len([]rune(q)) >= 2 {
			fsFragments, fsErr = s.fragmentsRepo.SearchPrefix(ctx, q, limit)
		}
	}
	// Firestore may require a composite index for prefix queries; treat missing-index as best-effort
	// so UI typeahead doesn't hard-fail with 500.
	if fsErr != nil {
		msg := strings.ToLower(strings.TrimSpace(fsErr.Error()))
		if strings.Contains(msg, "requires an index") || strings.Contains(msg, "failedprecondition") {
			log.Printf("[SearchFragments] Firestore prefix search missing index (ignored): %v", fsErr)
			fsErr = nil
			fsFragments = nil
		}
	}

	// If both sources failed / unavailable, return a useful error.
	if len(vertexFragments) == 0 && len(fsFragments) == 0 {
		if vertexErr != nil && fsErr != nil {
			return nil, fmt.Errorf("search failed (vertex: %v; firestore: %v)", vertexErr, fsErr)
		}
		if vertexErr != nil {
			return nil, vertexErr
		}
		if fsErr != nil {
			return nil, fsErr
		}
		return nil, nil
	}

	// Merge + dedupe (prefer Vertex ordering, then append Firestore).
	seen := map[string]struct{}{}
	out := make([]*models.HorizonFragment, 0, limit*2)
	add := func(f *models.HorizonFragment) {
		if f == nil {
			return
		}
		key := strings.TrimSpace(f.CanonicalPath)
		if key == "" {
			key = strings.TrimSpace(f.ID)
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	for _, f := range vertexFragments {
		add(f)
		if len(out) >= limit {
			return out, nil
		}
	}
	for _, f := range fsFragments {
		add(f)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// SearchDomainFragments performs best-effort search for DOMAIN fragments.
// It uses the same underlying search pipeline as SearchFragments (Vertex preferred, Firestore prefix fallback),
// but filters results to Type==DOMAIN.
func (s *HorizonService) SearchDomainFragments(ctx context.Context, query string, limit int) ([]*models.HorizonFragment, error) {
	all, err := s.SearchFragments(ctx, query, limit*3)
	if err != nil {
		return nil, err
	}
	out := make([]*models.HorizonFragment, 0, limit)
	seen := map[string]struct{}{}
	for _, f := range all {
		if f == nil {
			continue
		}
		if strings.TrimSpace(string(f.Type)) != string(models.FragmentTypeDomain) {
			continue
		}
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp == "" {
			cp = strings.TrimSpace(f.ID)
		}
		if cp == "" {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, f)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// FindDomainByCanonicalPathBestEffort returns an existing DOMAIN fragment for the given canonicalPath.
// It first checks Firestore directly (fast exact lookup) and then falls back to a best-effort Vertex search
// (which may return candidates by similarity, so we validate exact canonicalPath match after loading).
func (s *HorizonService) FindDomainByCanonicalPathBestEffort(ctx context.Context, canonicalPath string) (*models.HorizonFragment, error) {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return nil, nil
	}
	// 1) Firestore exact lookup (best).
	if s != nil && s.fragmentsRepo != nil {
		if frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, cp); err == nil && frag != nil {
			if strings.TrimSpace(string(frag.Type)) == string(models.FragmentTypeDomain) {
				return frag, nil
			}
			// Not a domain; treat as no-match.
			return nil, nil
		}
	}
	// 2) Vertex-assisted best-effort search (fallback).
	cands, err := s.SearchDomainFragments(ctx, cp, 10)
	if err != nil {
		return nil, err
	}
	for _, c := range cands {
		if c == nil {
			continue
		}
		if strings.TrimSpace(c.CanonicalPath) == cp && strings.TrimSpace(string(c.Type)) == string(models.FragmentTypeDomain) {
			return c, nil
		}
	}
	return nil, nil
}

// GetFragmentRollup computes aggregated rollup for a fragment and metric kind.
// Uses RollupService if available, otherwise returns basic fragment state metrics.
func (s *HorizonService) GetFragmentRollup(ctx context.Context, fragmentID, metricKind string) (*FragmentRollupResponse, error) {
	fragment, err := s.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, fmt.Errorf("fragment not found: %w", err)
	}
	kind := strings.ToLower(strings.TrimSpace(metricKind))
	if kind == "" {
		kind = "risk"
	}
	if kind != "risk" && kind != "opportunity" {
		return nil, fmt.Errorf("invalid metric kind: %s", metricKind)
	}

	// Build basic response from fragment state
	response := &FragmentRollupResponse{
		FragmentID:    fragmentID,
		CanonicalPath: fragment.CanonicalPath,
		MetricKind:    kind,
	}
	var primaryMetric models.MetricValue
	hasPrimaryMetric := false

	// Extract canonical signal metrics from fragment state.
	prefix := "/metric/" + kind + "/"
	type contribAgg struct {
		Amplitude float64
		Hops      int
		Path      []string
	}
	topContrib := map[string]*contribAgg{}
	pathStrength := map[string]float64{}

	for name, value := range fragment.State {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if response.Metrics == nil {
			response.Metrics = make(map[string]float64)
		}
		response.Metrics[name] = value.Value
		// Use highest value as primary
		if value.Value > response.Value {
			response.Value = value.Value
			response.Confidence = value.Confidence
			response.MetricName = name
			primaryMetric = value
			hasPrimaryMetric = true
		}

		for _, c := range value.Provenance.Contributions {
			sid := strings.TrimSpace(c.SignalID)
			if sid == "" {
				continue
			}
			if ex, ok := topContrib[sid]; !ok || c.Amplitude > ex.Amplitude {
				topContrib[sid] = &contribAgg{
					Amplitude: c.Amplitude,
					Hops:      c.Hops,
					Path:      append([]string(nil), c.Path...),
				}
			}
			pathKey := strings.Join(c.Path, " -> ")
			if pathKey != "" {
				pathStrength[pathKey] += c.Amplitude
			}
		}
	}
	if len(topContrib) > 0 {
		scored := make([]signalContributorScore, 0, len(topContrib))
		for sid, agg := range topContrib {
			scored = append(scored, signalContributorScore{
				SignalID: sid,
				Score:    agg.Amplitude,
				Hops:     agg.Hops,
				Path:     agg.Path,
			})
		}
		sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
		semantic := s.loadSignalSemanticsFromContributors(ctx, scored)
		if semantic.SampleCount > 0 {
			response.SignalSemanticSamples = semantic.SampleCount
			response.SignalSemanticLikelihood = semantic.Likelihood
			response.SignalSemanticImpact = semantic.Impact
			response.SignalSemanticModality = semantic.Modality
			response.SignalSemanticEscalation = semantic.Escalation
		}
		if len(scored) > 5 {
			scored = scored[:5]
		}
		explain := &ExplainTrace{
			TopContributors: make([]ExplainContributor, 0, len(scored)),
		}
		for _, c := range scored {
			explain.TopContributors = append(explain.TopContributors, ExplainContributor{
				SignalID:     c.SignalID,
				SignalType:   kind,
				SourcePath:   fragment.CanonicalPath,
				Contribution: c.Score,
				Reasoning:    fmt.Sprintf("propagated_hops=%d", c.Hops),
			})
		}
		if len(pathStrength) > 0 {
			type scoredPath struct {
				Path  string
				Score float64
			}
			paths := make([]scoredPath, 0, len(pathStrength))
			for p, v := range pathStrength {
				paths = append(paths, scoredPath{Path: p, Score: v})
			}
			sort.Slice(paths, func(i, j int) bool { return paths[i].Score > paths[j].Score })
			if len(paths) > 3 {
				paths = paths[:3]
			}
			explain.PropagationPath = make([]string, 0, len(paths))
			for _, p := range paths {
				explain.PropagationPath = append(explain.PropagationPath, p.Path)
			}
		}
		explain.RulesApplied = []string{"canonical_signal_rollup_v1"}
		response.ExplainTrace = explain
	}
	if s.appetiteResolver != nil {
		if profile, neighbors, err := s.appetiteResolver.ResolveForFragment(ctx, fragment, kind); err == nil && profile != nil {
			response.AppetiteScope = strings.TrimSpace(profile.ScopePath)
			baseThreshold := profile.AggregateThreshold
			response.AppetiteThreshold = baseThreshold
			response.AppetiteBreached = response.Value >= baseThreshold
			response.AppetiteNeighborScopes = neighbors
			if axis := axisFromCanonicalMetricPath(response.MetricName); axis != "" {
				if thr, ok := profile.AxisThresholds[axis]; ok {
					baseThreshold = thr
					response.AppetiteThreshold = thr
					response.AppetiteBreached = response.Value >= thr
				}
			}

			// Tail-event appetite mode: lower tolerance for low-probability/high-impact downside.
			// Deterministic by design to keep alert semantics stable.
			if kind == "risk" && hasPrimaryMetric {
				tail := assessTailScenarioMode(response, primaryMetric, signalSemanticTailContext{
					Likelihood: response.SignalSemanticLikelihood,
					Impact:     response.SignalSemanticImpact,
					Modality:   response.SignalSemanticModality,
					Escalation: response.SignalSemanticEscalation,
					Samples:    response.SignalSemanticSamples,
				})
				response.TailScenarioMode = tail.Enabled
				response.TailLikelihood = tail.Likelihood
				response.TailImpact = tail.Impact
				response.TailModality = tail.Modality
				response.TailEscalation = tail.Escalation
				response.TailReason = tail.Reason
				response.AppetiteBaseThreshold = baseThreshold
				if tail.Enabled {
					adjusted := math.Max(0.35, baseThreshold*0.75)
					// Extra strictness for extreme downside tails.
					if tail.Impact >= 0.90 && tail.Likelihood <= 0.45 {
						adjusted = math.Max(0.30, baseThreshold*0.70)
					}
					response.TailAdjustedThreshold = adjusted
					response.AppetiteThreshold = adjusted
					response.AppetiteBreached = response.Value >= adjusted
				}
			}
		}
	}

	return response, nil
}

func axisFromCanonicalMetricPath(metricPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(metricPath), "/"), "/")
	if len(parts) < 4 || parts[0] != "metric" {
		return ""
	}
	return parts[2] + "/" + parts[3]
}

// FragmentRollupResponse is the API response for fragment rollups.
type FragmentRollupResponse struct {
	FragmentID               string             `json:"fragmentId"`
	CanonicalPath            string             `json:"canonicalPath"`
	MetricKind               string             `json:"metricKind"`
	MetricName               string             `json:"metricName,omitempty"`
	Value                    float64            `json:"value"`
	Confidence               float64            `json:"confidence"`
	Metrics                  map[string]float64 `json:"metrics,omitempty"`
	ExplainTrace             *ExplainTrace      `json:"explainTrace,omitempty"`
	AppetiteScope            string             `json:"appetiteScope,omitempty"`
	AppetiteThreshold        float64            `json:"appetiteThreshold,omitempty"`
	AppetiteBreached         bool               `json:"appetiteBreached,omitempty"`
	AppetiteNeighborScopes   []string           `json:"appetiteNeighborScopes,omitempty"`
	AppetiteBaseThreshold    float64            `json:"appetiteBaseThreshold,omitempty"`
	TailScenarioMode         bool               `json:"tailScenarioMode,omitempty"`
	TailLikelihood           float64            `json:"tailLikelihood,omitempty"`
	TailImpact               float64            `json:"tailImpact,omitempty"`
	TailAdjustedThreshold    float64            `json:"tailAdjustedThreshold,omitempty"`
	TailModality             string             `json:"tailModality,omitempty"`
	TailEscalation           string             `json:"tailEscalation,omitempty"`
	TailReason               string             `json:"tailReason,omitempty"`
	SignalSemanticSamples    int                `json:"signalSemanticSamples,omitempty"`
	SignalSemanticLikelihood float64            `json:"signalSemanticLikelihood,omitempty"`
	SignalSemanticImpact     float64            `json:"signalSemanticImpact,omitempty"`
	SignalSemanticModality   string             `json:"signalSemanticModality,omitempty"`
	SignalSemanticEscalation string             `json:"signalSemanticEscalation,omitempty"`
}

// ExplainTrace explains how a rollup was computed.
type ExplainTrace struct {
	TopContributors []ExplainContributor `json:"topContributors"`
	PropagationPath []string             `json:"propagationPath"`
	RulesApplied    []string             `json:"rulesApplied"`
}

// ExplainContributor represents a contributing signal.
type ExplainContributor struct {
	SignalID     string  `json:"signalId"`
	SignalType   string  `json:"signalType"`
	SourcePath   string  `json:"sourcePath"`
	Contribution float64 `json:"contribution"`
	Reasoning    string  `json:"reasoning"`
}

type tailScenarioAssessment struct {
	Enabled    bool
	Likelihood float64
	Impact     float64
	Modality   string
	Escalation string
	Reason     string
}

type signalContributorScore struct {
	SignalID string
	Score    float64
	Hops     int
	Path     []string
}

type signalSemanticTailContext struct {
	Likelihood float64
	Impact     float64
	Modality   string
	Escalation string
	Samples    int
}

func assessTailScenarioMode(response *FragmentRollupResponse, primaryMetric models.MetricValue, semantics signalSemanticTailContext) tailScenarioAssessment {
	if response == nil {
		return tailScenarioAssessment{}
	}
	axis := axisFromCanonicalMetricPath(response.MetricName)
	if !isTailSensitiveAxis(axis) {
		return tailScenarioAssessment{
			Enabled:    false,
			Likelihood: clamp01(primaryMetric.Confidence),
			Impact:     clamp01(response.Value),
			Modality:   normalizeSignalModality(semantics.Modality),
			Escalation: normalizeEscalationLevel(semantics.Escalation),
			Reason:     "axis_not_tail_sensitive",
		}
	}

	likelihood := clamp01(primaryMetric.Confidence)
	impact := clamp01(response.Value)
	modality := normalizeSignalModality(semantics.Modality)
	escalation := normalizeEscalationLevel(semantics.Escalation)

	if semantics.Samples > 0 {
		likelihood = clamp01(semantics.Likelihood)
		impact = math.Max(impact, clamp01(semantics.Impact))
		if modality == "" {
			modality = models.SignalModalityPotential
		}
		if escalation == "" {
			escalation = models.EscalationLevelNone
		}
	}

	if impact < 0.05 && response.ExplainTrace != nil {
		for _, c := range response.ExplainTrace.TopContributors {
			if c.Contribution > impact {
				impact = clamp01(c.Contribution)
			}
		}
	}

	// Tail mode criteria:
	// - canonical low-prob/high-impact
	// - catastrophic impact regardless of confidence
	// - systemic spread hint (multiple strong contributors)
	if impact >= 0.85 && likelihood <= 0.60 {
		return tailScenarioAssessment{
			Enabled:    true,
			Likelihood: likelihood,
			Impact:     impact,
			Modality:   modality,
			Escalation: escalation,
			Reason:     "low_probability_high_impact",
		}
	}
	if impact >= 0.92 {
		return tailScenarioAssessment{
			Enabled:    true,
			Likelihood: likelihood,
			Impact:     impact,
			Modality:   modality,
			Escalation: escalation,
			Reason:     "catastrophic_impact_guardrail",
		}
	}
	if (escalation == models.EscalationLevelWarning || escalation == models.EscalationLevelCritical) && impact >= 0.75 {
		return tailScenarioAssessment{
			Enabled:    true,
			Likelihood: likelihood,
			Impact:     impact,
			Modality:   modality,
			Escalation: escalation,
			Reason:     "escalation_and_high_impact",
		}
	}
	if response.ExplainTrace != nil {
		strong := 0
		for _, c := range response.ExplainTrace.TopContributors {
			if c.Contribution >= 0.70 {
				strong++
			}
		}
		if strong >= 3 && impact >= 0.70 {
			return tailScenarioAssessment{
				Enabled:    true,
				Likelihood: likelihood,
				Impact:     impact,
				Modality:   modality,
				Escalation: escalation,
				Reason:     "systemic_multi_path_pressure",
			}
		}
	}
	return tailScenarioAssessment{
		Enabled:    false,
		Likelihood: likelihood,
		Impact:     impact,
		Modality:   modality,
		Escalation: escalation,
		Reason:     "normal_mode",
	}
}

type signalSemanticAggregate struct {
	SampleCount int
	Likelihood  float64
	Impact      float64
	Modality    string
	Escalation  string
}

func (s *HorizonService) loadSignalSemanticsFromContributors(ctx context.Context, scored []signalContributorScore) signalSemanticAggregate {
	if s == nil || s.ontologyFactsRepo == nil || len(scored) == 0 {
		return signalSemanticAggregate{}
	}
	const maxSignals = 24
	totalWeight := 0.0
	likelihoodWeighted := 0.0
	impactWeighted := 0.0
	modality := ""
	escalation := ""
	samples := 0
	seen := map[string]struct{}{}

	for i := 0; i < len(scored) && i < maxSignals; i++ {
		sid := strings.TrimSpace(scored[i].SignalID)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue
		}
		seen[sid] = struct{}{}
		f, err := s.ontologyFactsRepo.Get(ctx, sid)
		if err != nil || f == nil {
			continue
		}
		sem := parseSignalSemanticsFromFact(f)
		if !sem.Ready {
			continue
		}
		weight := scored[i].Score
		if weight <= 0 {
			weight = 0.1
		}
		totalWeight += weight
		likelihoodWeighted += sem.Likelihood * weight
		impactWeighted += sem.Impact * weight
		modality = strongerSignalModality(modality, sem.Modality)
		escalation = strongerEscalationLevel(escalation, sem.Escalation)
		samples++
	}
	if samples == 0 || totalWeight <= 0 {
		return signalSemanticAggregate{}
	}
	return signalSemanticAggregate{
		SampleCount: samples,
		Likelihood:  clamp01(likelihoodWeighted / totalWeight),
		Impact:      clamp01(impactWeighted / totalWeight),
		Modality:    normalizeSignalModality(modality),
		Escalation:  normalizeEscalationLevel(escalation),
	}
}

type signalSemantics struct {
	Ready      bool
	Likelihood float64
	Impact     float64
	Modality   string
	Escalation string
}

func parseSignalSemanticsFromFact(f *coremodels.OntologyFact) signalSemantics {
	if f == nil || strings.TrimSpace(f.Kind) != "signal" {
		return signalSemantics{}
	}
	summaryKV := parseSummaryKeyValues(strings.TrimSpace(f.Summary))
	likelihood := findSemanticFloat(f.Tags, "signal_likelihood:", summaryKV, "likelihood", clamp01(f.Confidence))
	impact := findSemanticFloat(f.Tags, "signal_impact:", summaryKV, "impact", 0)
	if impact <= 0 {
		impact = findSemanticFloat(f.Tags, "signal_amplitude:", summaryKV, "amplitude", 0)
	}
	modality := strings.TrimSpace(findSemanticString(f.Tags, "signal_modality:", summaryKV, "modality"))
	escalation := strings.TrimSpace(findSemanticString(f.Tags, "signal_escalation:", summaryKV, "escalation"))

	modality = normalizeSignalModality(modality)
	if modality == "" {
		modality = models.SignalModalityPotential
	}
	escalation = normalizeEscalationLevel(escalation)
	if escalation == "" {
		escalation = models.EscalationLevelNone
	}

	return signalSemantics{
		Ready:      true,
		Likelihood: clamp01(likelihood),
		Impact:     clamp01(impact),
		Modality:   modality,
		Escalation: escalation,
	}
}

func parseSummaryKeyValues(summary string) map[string]string {
	if summary == "" {
		return nil
	}
	parts := strings.Split(summary, ";")
	out := make(map[string]string, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		i := strings.Index(p, ":")
		if i <= 0 || i >= len(p)-1 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(p[:i]))
		v := strings.TrimSpace(p[i+1:])
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func findSemanticFloat(tags []string, tagPrefix string, summaryKV map[string]string, summaryKey string, fallback float64) float64 {
	if raw, ok := findTagValue(tags, tagPrefix); ok {
		return parseFloatClamped(raw, fallback)
	}
	if summaryKV != nil {
		if raw, ok := summaryKV[strings.ToLower(strings.TrimSpace(summaryKey))]; ok {
			return parseFloatClamped(raw, fallback)
		}
	}
	return clamp01(fallback)
}

func findSemanticString(tags []string, tagPrefix string, summaryKV map[string]string, summaryKey string) string {
	if raw, ok := findTagValue(tags, tagPrefix); ok {
		return strings.TrimSpace(raw)
	}
	if summaryKV != nil {
		if raw, ok := summaryKV[strings.ToLower(strings.TrimSpace(summaryKey))]; ok {
			return strings.TrimSpace(raw)
		}
	}
	return ""
}

func findTagValue(tags []string, prefix string) (string, bool) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return "", false
	}
	for _, t := range tags {
		tt := strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(tt), prefix) {
			return strings.TrimSpace(tt[len(prefix):]), true
		}
	}
	return "", false
}

func parseFloatClamped(raw string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return clamp01(fallback)
	}
	return clamp01(v)
}

func normalizeSignalModality(modality string) string {
	return models.NormalizeSignalModality(strings.TrimSpace(modality))
}

func strongerSignalModality(a, b string) string {
	return models.StrongerSignalModality(a, b)
}

func normalizeEscalationLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	if models.IsValidEscalationLevel(level) {
		if level == "" {
			return models.EscalationLevelNone
		}
		return level
	}
	return models.EscalationLevelNone
}

func strongerEscalationLevel(a, b string) string {
	rank := func(v string) int {
		switch normalizeEscalationLevel(v) {
		case models.EscalationLevelWatch:
			return 1
		case models.EscalationLevelWarning:
			return 2
		case models.EscalationLevelCritical:
			return 3
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return normalizeEscalationLevel(b)
	}
	return normalizeEscalationLevel(a)
}

func isTailSensitiveAxis(axis string) bool {
	switch strings.TrimSpace(axis) {
	case models.AxisGeoConflict,
		models.AxisLogisticsCapacity,
		models.AxisLogisticsLatency,
		models.AxisPhysicalFlowSupply,
		models.AxisOperationalContinuity,
		models.AxisFinancialSettlement,
		models.AxisLegalAccess:
		return true
	default:
		return false
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
