package propagation

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// PropagationEngine распространяет изменения в графе зависимостей
type PropagationEngine struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	lensesRepo    *repo.LensesRepository
	realtime      *realtime.Publisher
	threshold     *ThresholdManager
	damping       *WaveDamping
	lazyEval      *LazyEvaluator
}

// NewPropagationEngine создает новый Propagation Engine
func NewPropagationEngine(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	lensesRepo *repo.LensesRepository,
	realtime *realtime.Publisher,
) *PropagationEngine {
	return &PropagationEngine{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		lensesRepo:    lensesRepo,
		realtime:      realtime,
		threshold:     NewThresholdManager(),
		damping:       NewWaveDamping(),
		lazyEval:      NewLazyEvaluator(),
	}
}

// PropagationResult результат распространения влияния
type PropagationResult struct {
	AffectedFragments []string          `json:"affectedFragments"`
	AffectedLenses    []string          `json:"affectedLenses"`
	PropagationTime   time.Duration     `json:"propagationTime"`
	Metrics           map[string]float64 `json:"metrics"`
}

// PropagateChange распространяет изменение метрики в графе
func (e *PropagationEngine) PropagateChange(ctx context.Context, fragmentID string, metricName string, oldValue, newValue models.MetricValue) (*PropagationResult, error) {
	startTime := time.Now()

	// Проверяем порог значимости
	delta := newValue.Value - oldValue.Value
	if !e.threshold.ExceedsThreshold(metricName, delta) {
		// Изменение незначительно, пропускаем распространение
		return &PropagationResult{
			AffectedFragments: []string{},
			AffectedLenses:    []string{},
			PropagationTime:   time.Since(startTime),
			Metrics:           map[string]float64{"skipped": 1},
		}, nil
	}

	// Получаем исходный фрагмент
	sourceFragment, err := e.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source fragment: %w", err)
	}

	// Запускаем волну распространения
	affectedFragments := make(map[string]bool)
	affectedLenses := make(map[string]bool)

	// Hot Path: немедленное распространение по критическим путям
	if err := e.propagateHotPath(ctx, sourceFragment, metricName, delta, affectedFragments, affectedLenses); err != nil {
		return nil, fmt.Errorf("hot path propagation failed: %w", err)
	}

	// Cold Path: отложенное распространение для некритических путей
	e.lazyEval.ScheduleColdPath(ctx, fragmentID, metricName, delta)

	// Публикуем события
	e.publishPropagationEvents(ctx, fragmentID, affectedFragments, affectedLenses)

	// Преобразуем в слайсы
	affectedFragmentsList := make([]string, 0, len(affectedFragments))
	for id := range affectedFragments {
		affectedFragmentsList = append(affectedFragmentsList, id)
	}

	affectedLensesList := make([]string, 0, len(affectedLenses))
	for id := range affectedLenses {
		affectedLensesList = append(affectedLensesList, id)
	}

	return &PropagationResult{
		AffectedFragments: affectedFragmentsList,
		AffectedLenses:    affectedLensesList,
		PropagationTime:   time.Since(startTime),
		Metrics: map[string]float64{
			"hotPathFragments": float64(len(affectedFragments)),
			"affectedLenses":   float64(len(affectedLenses)),
		},
	}, nil
}

// propagateHotPath распространяет влияние по критическим путям (немедленно)
func (e *PropagationEngine) propagateHotPath(
	ctx context.Context,
	sourceFragment *models.HorizonFragment,
	metricName string,
	delta float64,
	affectedFragments map[string]bool,
	affectedLenses map[string]bool,
) error {
	// Получаем исходящие связи
	edges, err := e.edgesRepo.ListBySource(ctx, "global", sourceFragment.ID, nil, 100)
	if err != nil {
		return fmt.Errorf("failed to list edges: %w", err)
	}

	// Обрабатываем каждую связь
	for _, edge := range edges {
		// Применяем затухание волны
		dampedDelta := e.damping.ApplyDamping(delta, edge.Properties.Amplification, edge.Properties.Criticality)

		// Проверяем порог после затухания
		if !e.threshold.ExceedsThreshold(metricName, dampedDelta) {
			continue
		}

		// Получаем целевой фрагмент
		targetFragment, err := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
		if err != nil {
			continue // Пропускаем недоступные фрагменты
		}

		// Обновляем метрику в целевом фрагменте
		if err := e.updateFragmentMetric(ctx, targetFragment, metricName, dampedDelta); err != nil {
			continue
		}

		affectedFragments[targetFragment.ID] = true

		// Находим Lens, которые используют этот фрагмент
		lenses, err := e.lensesRepo.FindByScope(ctx, "global", targetFragment.CanonicalPath, 100)
		if err == nil {
			for _, lens := range lenses {
				affectedLenses[lens.ID] = true
			}
		}

		// Рекурсивно распространяем дальше
		if err := e.propagateHotPath(ctx, targetFragment, metricName, dampedDelta, affectedFragments, affectedLenses); err != nil {
			// Логируем ошибку, но продолжаем распространение
			_ = err
		}
	}

	return nil
}

// updateFragmentMetric обновляет метрику во фрагменте
func (e *PropagationEngine) updateFragmentMetric(ctx context.Context, fragment *models.HorizonFragment, metricName string, delta float64) error {
	// Получаем текущее значение метрики
	currentMetric, exists := fragment.State[metricName]
	if !exists {
		// Создаем новую метрику с базовым значением
		currentMetric = models.MetricValue{
			Value:      0.0,
			Confidence: 0.5,
			ObservedAt: time.Now(),
			Provenance: models.Provenance{
				Method: "propagated",
			},
		}
	}

	// Применяем изменение
	newValue := currentMetric.Value + delta

	// Ограничиваем диапазон в зависимости от типа метрики
	// Magnitude: [0.0..1.0], Directional: [-1.0..1.0]
	// Упрощенная проверка: если метрика начинается с определенных префиксов
	if newValue < 0.0 {
		newValue = 0.0
	}
	if newValue > 1.0 {
		newValue = 1.0
	}

	// Обновляем метрику
	currentMetric.Value = newValue
	currentMetric.ObservedAt = time.Now()
	fragment.State[metricName] = currentMetric

	// Сохраняем изменения
	return e.fragmentsRepo.Update(ctx, fragment)
}

// publishPropagationEvents публикует события распространения
func (e *PropagationEngine) publishPropagationEvents(ctx context.Context, sourceFragmentID string, affectedFragments map[string]bool, affectedLenses map[string]bool) {
	tenantID := "global" // HorizonFragment всегда глобальные

	// Публикуем событие начала распространения
	_ = e.realtime.PublishJSON(ctx, realtime.EventType("propagation.started"), tenantID, map[string]interface{}{
		"sourceFragmentId": sourceFragmentID,
		"timestamp":        time.Now().UTC(),
	})

	// Публикуем события обновления фрагментов
	for fragmentID := range affectedFragments {
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("fragment.updated"), tenantID, map[string]interface{}{
			"fragmentId": fragmentID,
			"timestamp":  time.Now().UTC(),
		})
	}

	// Публикуем события обновления Lens
	for lensID := range affectedLenses {
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("lens.updated"), tenantID, map[string]interface{}{
			"lensId":    lensID,
			"timestamp": time.Now().UTC(),
		})
	}
}

// SimulatePropagation выполняет симуляцию распространения без сохранения изменений
func (e *PropagationEngine) SimulatePropagation(ctx context.Context, fragmentID string, metricName string, oldValue, newValue models.MetricValue) (*PropagationResult, error) {
	startTime := time.Now()

	// Проверяем порог значимости
	delta := newValue.Value - oldValue.Value
	if !e.threshold.ExceedsThreshold(metricName, delta) {
		return &PropagationResult{
			AffectedFragments: []string{},
			AffectedLenses:    []string{},
			PropagationTime:   time.Since(startTime),
			Metrics:           map[string]float64{"skipped": 1},
		}, nil
	}

	// Получаем исходный фрагмент
	sourceFragment, err := e.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source fragment: %w", err)
	}

	// Запускаем симуляцию распространения (без сохранения изменений)
	affectedFragments := make(map[string]bool)
	affectedLenses := make(map[string]bool)

	// Симулируем распространение по критическим путям
	if err := e.simulateHotPath(ctx, sourceFragment, metricName, delta, affectedFragments, affectedLenses); err != nil {
		return nil, fmt.Errorf("hot path simulation failed: %w", err)
	}

	// Преобразуем в слайсы
	affectedFragmentsList := make([]string, 0, len(affectedFragments))
	for id := range affectedFragments {
		affectedFragmentsList = append(affectedFragmentsList, id)
	}

	affectedLensesList := make([]string, 0, len(affectedLenses))
	for id := range affectedLenses {
		affectedLensesList = append(affectedLensesList, id)
	}

	return &PropagationResult{
		AffectedFragments: affectedFragmentsList,
		AffectedLenses:    affectedLensesList,
		PropagationTime:   time.Since(startTime),
		Metrics: map[string]float64{
			"simulatedFragments": float64(len(affectedFragments)),
			"affectedLenses":     float64(len(affectedLenses)),
		},
	}, nil
}

// simulateHotPath симулирует распространение по критическим путям без сохранения изменений
func (e *PropagationEngine) simulateHotPath(
	ctx context.Context,
	sourceFragment *models.HorizonFragment,
	metricName string,
	delta float64,
	affectedFragments map[string]bool,
	affectedLenses map[string]bool,
) error {
	// Получаем исходящие связи
	edges, err := e.edgesRepo.ListBySource(ctx, "global", sourceFragment.ID, nil, 100)
	if err != nil {
		return fmt.Errorf("failed to list edges: %w", err)
	}

	// Обрабатываем каждую связь
	for _, edge := range edges {
		// Применяем затухание волны
		dampedDelta := e.damping.ApplyDamping(delta, edge.Properties.Amplification, edge.Properties.Criticality)

		// Проверяем порог после затухания
		if !e.threshold.ExceedsThreshold(metricName, dampedDelta) {
			continue
		}

		// Получаем целевой фрагмент
		targetFragment, err := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
		if err != nil {
			continue // Пропускаем недоступные фрагменты
		}

		// Симулируем обновление метрики (без сохранения)
		affectedFragments[targetFragment.ID] = true

		// Находим Lens, которые используют этот фрагмент
		lenses, err := e.lensesRepo.FindByScope(ctx, "global", targetFragment.CanonicalPath, 100)
		if err == nil {
			for _, lens := range lenses {
				affectedLenses[lens.ID] = true
			}
		}

		// Рекурсивно симулируем дальше
		if err := e.simulateHotPath(ctx, targetFragment, metricName, dampedDelta, affectedFragments, affectedLenses); err != nil {
			// Логируем ошибку, но продолжаем симуляцию
			_ = err
		}
	}

	return nil
}

