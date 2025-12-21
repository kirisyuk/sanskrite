package propagation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// PropagationEngine распространяет изменения в графе зависимостей
type PropagationEngine struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	versionsRepo  *repo.HorizonFragmentVersionsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	lensesRepo    *repo.LensesRepository
	realtime      *realtime.Publisher
	threshold     *ThresholdManager
	damping       *WaveDamping
	lazyEval      *LazyEvaluator
	
	// Трекинг активных распространений
	activePropagations map[string]*PropagationStatus
	mu                 sync.RWMutex
}

// PropagationStatus статус распространения для фрагмента
type PropagationStatus struct {
	FragmentID          string
	IsPropagating       bool
	LastPropagation     *LastPropagationInfo
	LastPropagationTime time.Time
}

// LastPropagationInfo информация о последнем распространении
type LastPropagationInfo struct {
	StartedAt          time.Time
	CompletedAt        time.Time
	NodesUpdated       int
	LensesRecalculated int
}

// NewPropagationEngine создает новый Propagation Engine
func NewPropagationEngine(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	versionsRepo *repo.HorizonFragmentVersionsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	lensesRepo *repo.LensesRepository,
	realtime *realtime.Publisher,
) *PropagationEngine {
	return &PropagationEngine{
		fragmentsRepo:      fragmentsRepo,
		versionsRepo:       versionsRepo,
		edgesRepo:          edgesRepo,
		lensesRepo:         lensesRepo,
		realtime:           realtime,
		threshold:          NewThresholdManager(),
		damping:            NewWaveDamping(),
		lazyEval:           NewLazyEvaluator(),
		activePropagations: make(map[string]*PropagationStatus),
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
	if !e.threshold.ExceedsThreshold(metricName, delta, newValue.Unit) {
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
	
	// Запускаем обработку Cold Path асинхронно (если есть задачи старше 1 минуты)
	go e.processColdPathIfReady(ctx)

	// Публикуем события
	e.publishPropagationEvents(ctx, fragmentID, affectedFragments, affectedLenses)

	// Обновляем статус распространения
	e.updatePropagationStatus(fragmentID, len(affectedFragments), len(affectedLenses), time.Since(startTime))

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

// updatePropagationStatus обновляет статус распространения для фрагмента
func (e *PropagationEngine) updatePropagationStatus(fragmentID string, nodesUpdated, lensesRecalculated int, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	status, exists := e.activePropagations[fragmentID]
	if !exists {
		status = &PropagationStatus{
			FragmentID: fragmentID,
		}
		e.activePropagations[fragmentID] = status
	}

	status.IsPropagating = false
	status.LastPropagation = &LastPropagationInfo{
		StartedAt:          now.Add(-duration),
		CompletedAt:        now,
		NodesUpdated:       nodesUpdated,
		LensesRecalculated: lensesRecalculated,
	}
	status.LastPropagationTime = now
}

// GetPropagationStatus получает статус распространения для фрагмента
func (e *PropagationEngine) GetPropagationStatus(fragmentID string) *PropagationStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status, exists := e.activePropagations[fragmentID]
	if !exists {
		return &PropagationStatus{
			FragmentID:    fragmentID,
			IsPropagating: false,
		}
	}
	return status
}

// TriggerPropagation запускает принудительное распространение для фрагмента
func (e *PropagationEngine) TriggerPropagation(ctx context.Context, fragmentID string) (*PropagationResult, error) {
	// Получаем фрагмент
	fragment, err := e.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fragment: %w", err)
	}

	// Помечаем как активное распространение
	e.mu.Lock()
	status, exists := e.activePropagations[fragmentID]
	if !exists {
		status = &PropagationStatus{
			FragmentID: fragmentID,
		}
		e.activePropagations[fragmentID] = status
	}
	status.IsPropagating = true
	e.mu.Unlock()

	// Load previous state from versions to compute real deltas.
	var prevState map[string]models.MetricValue
	if e.versionsRepo != nil {
		if vers, err := e.versionsRepo.ListByFragment(ctx, fragment.TenantID, fragment.ID, 1, 0); err == nil && len(vers) > 0 && vers[0] != nil {
			prevState = vers[0].State
		}
	}

	// Запускаем распространение для всех метрик фрагмента
	var result *PropagationResult
	for metricName, metricValue := range fragment.State {
		oldValue := models.MetricValue{}
		if prevState != nil {
			if pv, ok := prevState[metricName]; ok {
				oldValue = pv
			}
		}

		res, err := e.PropagateChange(ctx, fragmentID, metricName, oldValue, metricValue)
		if err != nil {
			// Логируем ошибку, но продолжаем
			_ = err
			continue
		}
		if result == nil {
			result = res
		} else {
			// Объединяем результаты
			result.AffectedFragments = append(result.AffectedFragments, res.AffectedFragments...)
			result.AffectedLenses = append(result.AffectedLenses, res.AffectedLenses...)
		}
	}

	// Помечаем как завершенное
	e.mu.Lock()
	if status, exists := e.activePropagations[fragmentID]; exists {
		status.IsPropagating = false
	}
	e.mu.Unlock()

	if result == nil {
		return &PropagationResult{
			AffectedFragments: []string{},
			AffectedLenses:    []string{},
			PropagationTime:   0,
			Metrics:           map[string]float64{},
		}, nil
	}

	return result, nil
}

// processColdPathIfReady обрабатывает Cold Path задачи, если они готовы
func (e *PropagationEngine) processColdPathIfReady(ctx context.Context) {
	// Обрабатываем Cold Path задачи через LazyEvaluator
	err := e.lazyEval.ProcessColdPath(ctx, func(ctx context.Context, task *ColdPathTask) error {
		// Получаем фрагмент
		fragment, err := e.fragmentsRepo.Get(ctx, task.FragmentID)
		if err != nil {
			return fmt.Errorf("failed to get fragment: %w", err)
		}

		// Получаем текущее значение метрики
		currentMetric, exists := fragment.State[task.MetricName]
		if !exists {
			// Создаем новую метрику
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
		newValue := currentMetric
		newValue.Value = currentMetric.Value + task.Delta
		
		// Определяем тип метрики и ограничиваем диапазон
		metricType := getMetricType(task.MetricName)
		newValue.Value = clampMetricValue(newValue.Value, metricType)
		
		// Обновляем метрику
		fragment.State[task.MetricName] = newValue
		
		// Сохраняем изменения
		if err := e.fragmentsRepo.Update(ctx, fragment); err != nil {
			return fmt.Errorf("failed to update fragment: %w", err)
		}

		// Находим Lens, которые используют этот фрагмент
		lenses, err := e.lensesRepo.FindByScope(ctx, "global", fragment.CanonicalPath, 100)
		if err == nil {
			for _, lens := range lenses {
				// Публикуем событие обновления Lens
				_ = e.realtime.PublishJSON(ctx, realtime.EventType("lens.updated"), "global", map[string]interface{}{
					"lensId":    lens.ID,
					"timestamp": time.Now().UTC(),
				})
			}
		}

		return nil
	})
	
	// Логируем ошибку, но не блокируем
	_ = err
}

// StartColdPathProcessor запускает фоновый процессор Cold Path задач
func (e *PropagationEngine) StartColdPathProcessor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				e.processColdPathIfReady(ctx)
			}
		}
	}()
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
		// Best-effort unit: use the current unit on the source fragment metric, if present.
		var unit *string
		if mv, ok := sourceFragment.State[metricName]; ok {
			unit = mv.Unit
		}
		// Применяем затухание волны
		dampedDelta := e.damping.ApplyDamping(delta, edge.Properties.Amplification, edge.Properties.Criticality)

		// Проверяем порог после затухания
		if !e.threshold.ExceedsThreshold(metricName, dampedDelta, unit) {
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

	// Определяем тип метрики и ограничиваем диапазон соответственно
	// Magnitude: [0.0..1.0], Directional: [-1.0..1.0]
	metricType := getMetricType(metricName)
	newValue = clampMetricValue(newValue, metricType)

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
	if !e.threshold.ExceedsThreshold(metricName, delta, nil) {
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
		if !e.threshold.ExceedsThreshold(metricName, dampedDelta, nil) {
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

