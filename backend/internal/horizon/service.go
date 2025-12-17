package horizon

import (
	"context"
	"fmt"

	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/watchdog"
)

// HorizonService - главный сервис модуля horizon
type HorizonService struct {
	deps HorizonServiceDependencies

	// Подсистемы (будут инициализированы на соответствующих этапах)
	scoutEngine      *scout.ScoutEngine
	propagationEngine *propagation.PropagationEngine
	watchdogService   *watchdog.WatchdogService
	lensService       *lens.LensService

	// Репозитории horizon
	fragmentsRepo     *repo.HorizonFragmentsRepository
	framesRepo        *repo.FramesRepository
	lensesRepo        *repo.LensesRepository
	edgesRepo         *repo.DependencyEdgesRepository
	watchdogRepo      *repo.WatchdogConfigsRepository
	scoutingJobsRepo *repo.ScoutingJobsRepository
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
	if deps.VertexSearchClient == nil {
		return nil, fmt.Errorf("VertexSearchClient is required")
	}
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
		deps:                deps,
		fragmentsRepo:       fragmentsRepo,
		framesRepo:          framesRepo,
		lensesRepo:          lensesRepo,
		edgesRepo:           edgesRepo,
		watchdogRepo:        watchdogRepo,
		scoutingJobsRepo:    scoutingJobsRepo,
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

// HealthCheck проверяет состояние сервиса
func (s *HorizonService) HealthCheck(ctx context.Context) error {
	// Базовая проверка доступности репозиториев
	// Более детальные проверки будут добавлены на соответствующих этапах
	return nil
}

// SearchFragments выполняет семантический поиск по фрагментам через Vertex AI Search
func (s *HorizonService) SearchFragments(ctx context.Context, query string, limit int) ([]*models.HorizonFragment, error) {
	if s.deps.VertexSearchClient == nil || !s.deps.VertexSearchClient.IsConfigured() {
		return nil, fmt.Errorf("vertex search not configured")
	}

	// Ищем документы с типом horizon_fragment
	req := &ai.SearchRequest{
		Query:    query,
		PageSize: limit,
		Filter:   `structData.type: "horizon_fragment"`,
	}

	resp, err := s.deps.VertexSearchClient.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("vertex search failed: %w", err)
	}

	var fragments []*models.HorizonFragment
	for _, res := range resp.Results {
		// Извлекаем ID фрагмента
		id := res.Document.ID
		// Если ID содержит расширение .json, убираем его
		if strings.HasSuffix(id, ".json") {
			id = strings.TrimSuffix(id, ".json")
		}

		// Пытаемся получить фрагмент из базы
		fragment, err := s.fragmentsRepo.Get(ctx, id)
		if err == nil {
			fragments = append(fragments, fragment)
		}
	}

	return fragments, nil
}
