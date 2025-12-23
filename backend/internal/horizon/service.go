package horizon

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/watchdog"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// HorizonService - главный сервис модуля horizon
type HorizonService struct {
	deps HorizonServiceDependencies

	// Подсистемы (будут инициализированы на соответствующих этапах)
	scoutEngine       *scout.ScoutEngine
	propagationEngine *propagation.PropagationEngine
	watchdogService   *watchdog.WatchdogService
	lensService       *lens.LensService

	// Репозитории horizon
	fragmentsRepo        *repo.HorizonFragmentsRepository
	framesRepo           *repo.FramesRepository
	lensesRepo           *repo.LensesRepository
	edgesRepo            *repo.DependencyEdgesRepository
	watchdogRepo         *repo.WatchdogConfigsRepository
	scoutingJobsRepo     *repo.ScoutingJobsRepository
	fragmentVersionsRepo *repo.HorizonFragmentVersionsRepository
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

// RealtimePublisher exposes the underlying realtime publisher for HTTP handlers that need to emit
// events directly (best-effort). Prefer emitting from domain services when possible.
func (s *HorizonService) RealtimePublisher() *realtime.Publisher {
	if s == nil {
		return nil
	}
	return s.deps.RealtimePublisher
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
				if strings.HasSuffix(id, ".json") {
					id = strings.TrimSuffix(id, ".json")
				}
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
