package propagation

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// TopologyAnalyzer анализирует топологию графа для оптимизации распространения
type TopologyAnalyzer struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	lensesRepo    *repo.LensesRepository
}

// NewTopologyAnalyzer создает новый анализатор топологии
func NewTopologyAnalyzer(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	lensesRepo *repo.LensesRepository,
) *TopologyAnalyzer {
	return &TopologyAnalyzer{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		lensesRepo:    lensesRepo,
	}
}

// CriticalPath представляет критический путь от источника к цели
type CriticalPath struct {
	SourceFragmentID string
	TargetFragmentID string
	Path              []string // Последовательность fragment IDs
	TotalCriticality  float64  // Суммарная критичность пути
	TotalLatency      int      // Суммарная задержка в часах
}

// FindCriticalPathsForLens находит критические пути для Lens
func (a *TopologyAnalyzer) FindCriticalPathsForLens(ctx context.Context, lensID string) ([]*CriticalPath, error) {
	// Получаем Lens
	lens, err := a.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lens: %w", err)
	}

	if len(lens.Scope) == 0 {
		return []*CriticalPath{}, nil
	}

	// Находим все пути от источников к целям в scope
	var criticalPaths []*CriticalPath

	// Для каждого фрагмента в scope находим пути от всех источников
	for _, targetPath := range lens.Scope {
		targetFragment, err := a.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		if err != nil {
			continue
		}

		// Находим все пути к целевому фрагменту
		paths := a.findAllPathsToTarget(ctx, targetFragment.ID, make(map[string]bool), []string{}, 0, 10)
		for _, path := range paths {
			if len(path) > 0 {
				criticalPath := a.buildCriticalPath(ctx, path)
				if criticalPath != nil {
					criticalPaths = append(criticalPaths, criticalPath)
				}
			}
		}
	}

	// Сортируем по критичности (от большей к меньшей)
	criticalPaths = a.sortPathsByCriticality(criticalPaths)

	return criticalPaths, nil
}

// findAllPathsToTarget находит все пути к целевому фрагменту (рекурсивно)
func (a *TopologyAnalyzer) findAllPathsToTarget(
	ctx context.Context,
	targetID string,
	visited map[string]bool,
	currentPath []string,
	depth int,
	maxDepth int,
) [][]string {
	if depth > maxDepth {
		return [][]string{}
	}

	if visited[targetID] {
		return [][]string{} // Цикл обнаружен
	}

	visited[targetID] = true
	currentPath = append(currentPath, targetID)

	// Получаем входящие связи
	edges, err := a.edgesRepo.ListByTarget(ctx, "global", targetID, nil, 100)
	if err != nil {
		return [][]string{}
	}

	var paths [][]string

	// Если нет входящих связей, это источник - добавляем путь
	if len(edges) == 0 {
		pathCopy := make([]string, len(currentPath))
		copy(pathCopy, currentPath)
		paths = append(paths, pathCopy)
	} else {
		// Рекурсивно ищем пути от источников
		for _, edge := range edges {
			sourcePaths := a.findAllPathsToTarget(ctx, edge.SourceFragmentID, visited, currentPath, depth+1, maxDepth)
			paths = append(paths, sourcePaths...)
		}
	}

	delete(visited, targetID)
	return paths
}

// buildCriticalPath строит CriticalPath из последовательности fragment IDs
func (a *TopologyAnalyzer) buildCriticalPath(ctx context.Context, path []string) *CriticalPath {
	if len(path) < 2 {
		return nil
	}

	sourceID := path[len(path)-1]
	targetID := path[0]

	totalCriticality := 0.0
	totalLatency := 0

	// Вычисляем суммарную критичность и задержку пути
	for i := len(path) - 1; i > 0; i-- {
		sourceID := path[i]
		targetID := path[i-1]

		edge, err := a.edgesRepo.GetBySourceAndTarget(ctx, "global", sourceID, targetID)
		if err != nil {
			continue
		}

		totalCriticality += edge.Properties.Criticality
		totalLatency += edge.Properties.LatencyHours
	}

	return &CriticalPath{
		SourceFragmentID: sourceID,
		TargetFragmentID: targetID,
		Path:              path,
		TotalCriticality:  totalCriticality,
		TotalLatency:      totalLatency,
	}
}

// sortPathsByCriticality сортирует пути по критичности (от большей к меньшей)
func (a *TopologyAnalyzer) sortPathsByCriticality(paths []*CriticalPath) []*CriticalPath {
	// Простая сортировка пузырьком по TotalCriticality
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[i].TotalCriticality < paths[j].TotalCriticality {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
	return paths
}

// GetPriorityOrder определяет приоритетный порядок обработки узлов для Lens
func (a *TopologyAnalyzer) GetPriorityOrder(ctx context.Context, lensID string) ([]string, error) {
	criticalPaths, err := a.FindCriticalPathsForLens(ctx, lensID)
	if err != nil {
		return nil, err
	}

	// Строим приоритетный список узлов на основе критических путей
	priorityMap := make(map[string]int)
	for _, path := range criticalPaths {
		for i, fragmentID := range path.Path {
			// Чем ближе к источнику и чем выше критичность пути, тем выше приоритет
			priority := (len(path.Path) - i) * int(path.TotalCriticality*100)
			if existingPriority, exists := priorityMap[fragmentID]; !exists || existingPriority < priority {
				priorityMap[fragmentID] = priority
			}
		}
	}

	// Преобразуем в отсортированный список
	type priorityItem struct {
		FragmentID string
		Priority    int
	}
	var items []priorityItem
	for fragmentID, priority := range priorityMap {
		items = append(items, priorityItem{FragmentID: fragmentID, Priority: priority})
	}

	// Сортируем по приоритету
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Priority < items[j].Priority {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.FragmentID
	}

	return result, nil
}

// IsCriticalPath проверяет, является ли путь критическим для Lens
func (a *TopologyAnalyzer) IsCriticalPath(ctx context.Context, lensID string, sourceFragmentID, targetFragmentID string) (bool, error) {
	criticalPaths, err := a.FindCriticalPathsForLens(ctx, lensID)
	if err != nil {
		return false, err
	}

	for _, path := range criticalPaths {
		if path.SourceFragmentID == sourceFragmentID && path.TargetFragmentID == targetFragmentID {
			return true, nil
		}
	}

	return false, nil
}

