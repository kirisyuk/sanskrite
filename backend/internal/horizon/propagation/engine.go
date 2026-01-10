package propagation

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	criticalpath "github.com/igorkirisyuk/sanskrite/internal/horizon/critical_path"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

const hotPathMaxHops = 64

// PropagationEngine распространяет изменения в графе зависимостей
type PropagationEngine struct {
	fragmentsRepo        *repo.HorizonFragmentsRepository
	versionsRepo         *repo.HorizonFragmentVersionsRepository
	edgesRepo            *repo.DependencyEdgesRepository
	lensesRepo           *repo.LensesRepository
	realtime             *realtime.Publisher
	threshold            *ThresholdManager
	damping              *WaveDamping
	lazyEval             *LazyEvaluator
	policyExecutor       *policy.PolicyExecutor // Optional: for semantic propagation policies
	criticalPathDetector *criticalpath.Detector // Optional: for critical path detection

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

// WithPolicyExecutor sets the policy executor for semantic propagation.
func (e *PropagationEngine) WithPolicyExecutor(executor *policy.PolicyExecutor) *PropagationEngine {
	e.policyExecutor = executor
	return e
}

// WithCriticalPathDetector sets the critical path detector.
func (e *PropagationEngine) WithCriticalPathDetector(detector *criticalpath.Detector) *PropagationEngine {
	e.criticalPathDetector = detector
	return e
}

// PropagationResult результат распространения влияния
type PropagationResult struct {
	AffectedFragments []string           `json:"affectedFragments"`
	AffectedLenses    []string           `json:"affectedLenses"`
	PropagationPaths  [][]string         `json:"propagationPaths"` // Paths of fragment IDs showing propagation routes
	PropagationTime   time.Duration      `json:"propagationTime"`
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
	propagationPaths := make([][]string, 0) // Track propagation paths for explain trace

	// Hot Path: немедленное распространение по критическим путям
	if err := e.propagateHotPath(ctx, sourceFragment, metricName, delta, affectedFragments, affectedLenses, &propagationPaths); err != nil {
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
		PropagationPaths:  propagationPaths,
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
		// IMPORTANT:
		// - If we don't have a previous version (prevState == nil), we must NOT treat the metric as changed from 0.
		//   That would trigger massive propagation waves on first trigger, even when no "change" actually happened.
		// - If the metric didn't exist in prevState, treat it as unchanged baseline for TriggerPropagation purposes.
		oldValue := metricValue
		if prevState != nil {
			if pv, ok := prevState[metricName]; ok {
				oldValue = pv
			} else {
				oldValue = metricValue
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
			result.PropagationPaths = append(result.PropagationPaths, res.PropagationPaths...)
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
			PropagationPaths:  [][]string{},
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

		// Best-effort: notify lens updates, but only when we have tenant routing context.
		// Cold-path processor may run without rtctx; in that case, we skip realtime publishes to avoid leaking.
		rc, ok := rtctx.Get(ctx)
		tenantID := strings.TrimSpace(rc.TenantID)
		projectID := strings.TrimSpace(rc.ProjectID)
		originLensID := strings.TrimSpace(rc.LensID)
		if ok && tenantID != "" && e.lensesRepo != nil && e.realtime != nil {
			lenses, err := e.lensesRepo.FindByScope(ctx, tenantID, fragment.CanonicalPath, 100)
			if err == nil {
				for _, l := range lenses {
					payload := map[string]interface{}{
						"tenantId":  tenantID,
						"projectId": projectID,
						"lensId":    l.ID,
						"ts":        time.Now().UTC().Format(time.RFC3339Nano),
					}
					if originLensID != "" && originLensID != l.ID {
						payload["originLensId"] = originLensID
					}
					if v := strings.TrimSpace(rc.CorrelationID); v != "" {
						payload["correlationId"] = v
					}
					if v := strings.TrimSpace(rc.SessionID); v != "" {
						payload["sessionId"] = v
					}
					_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.lens.updated"), tenantID, payload)
				}
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
	propagationPaths *[][]string, // Track paths for explain trace
) error {
	if e == nil || sourceFragment == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	type waveItem struct {
		frag *models.HorizonFragment
		d    float64
		hops int
		path []string // Track path from source
	}

	// visitedMagnitude tracks the strongest (absolute) delta we've already processed for a fragment
	// during this wave, to prevent infinite loops on cyclic graphs.
	visitedMagnitude := map[string]float64{sourceFragment.ID: math.Abs(delta)}
	stack := []waveItem{{frag: sourceFragment, d: delta, hops: 0, path: []string{sourceFragment.ID}}}
	hitHopLimit := false
	cycleSkips := 0

	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		// pop (DFS)
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if it.frag == nil || strings.TrimSpace(it.frag.ID) == "" {
			continue
		}
		if it.hops >= hotPathMaxHops {
			hitHopLimit = true
			continue
		}

		// Get outgoing edges for current fragment
		edges, err := e.edgesRepo.ListBySource(ctx, "global", it.frag.ID, nil, 100)
		if err != nil {
			return fmt.Errorf("failed to list edges: %w", err)
		}

		// Best-effort unit: use the current unit on the fragment metric, if present.
		var unit *string
		if mv, ok := it.frag.State[metricName]; ok {
			unit = mv.Unit
		}

		for _, edge := range edges {
			if edge == nil {
				continue
			}
			// Guard self-edge cycles
			if strings.TrimSpace(edge.TargetFragmentID) == "" || edge.TargetFragmentID == it.frag.ID {
				continue
			}

			// Filter out inactive edges (historical or future) from real-time propagation
			// Historical edges (ValidTo < now) and future edges (ValidFrom > now) are excluded.
			// This ensures propagation only flows through currently active relationships.
			if !edge.IsActiveNow() {
				continue
			}

			// Use policy if available, otherwise fallback to default behavior
			var dampedDelta float64
			var shouldPropagate bool
			var maxDepth int = hotPathMaxHops

			if e.policyExecutor != nil {
				// Check if edge is on critical path
				isCriticalPath := false
				if e.criticalPathDetector != nil {
					targetFragment, _ := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
					if targetFragment != nil {
						isCriticalPath = e.criticalPathDetector.IsCriticalPath(edge, it.frag, targetFragment)
					}
				}

				// Get policy decision
				decision, err := e.policyExecutor.Execute(ctx, policy.PolicyInput{
					Kind: "propagation",
					Context: map[string]interface{}{
						"metricName":     metricName,
						"relationType":   string(edge.Type),
						"currentDepth":   it.hops,
						"isCriticalPath": isCriticalPath,
						"delta":          it.d,
					},
					Edge: edge,
				})

				if err == nil && decision.Action == "propagate" {
					shouldPropagate = true
					weight, _ := decision.Params["weight"].(float64)
					if weight == 0 {
						weight = 1.0
					}
					dampingOverride, _ := decision.Params["dampingOverride"].(*float64)
					if dampingOverride != nil {
						dampedDelta = it.d * weight * (*dampingOverride)
					} else {
						dampedDelta = e.damping.ApplyDamping(it.d*weight, edge.Properties.Amplification, edge.Properties.Criticality)
					}
					if md, ok := decision.Params["maxDepth"].(int); ok {
						maxDepth = md
					}
					// Apply critical path boost if applicable
					if isCriticalPath {
						if boost, ok := decision.Params["criticalPathBoost"].(float64); ok && boost > 1.0 {
							dampedDelta *= boost
						}
					}
				} else {
					shouldPropagate = false
				}
			} else {
				// Fallback to default behavior
				dampedDelta = e.damping.ApplyDamping(it.d, edge.Properties.Amplification, edge.Properties.Criticality)
				shouldPropagate = true
			}

			if !shouldPropagate {
				continue
			}

			if !e.threshold.ExceedsThreshold(metricName, dampedDelta, unit) {
				continue
			}

			// Check depth limit
			if it.hops >= maxDepth {
				continue
			}

			// Cycle guard: if we've already propagated an equal-or-stronger delta to this node in this wave, skip.
			absD := math.Abs(dampedDelta)
			if prev, ok := visitedMagnitude[edge.TargetFragmentID]; ok && prev >= absD {
				cycleSkips++
				continue
			}

			targetFragment, err := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
			if err != nil || targetFragment == nil {
				continue
			}

			if err := e.updateFragmentMetric(ctx, targetFragment, metricName, dampedDelta); err != nil {
				continue
			}

			affectedFragments[targetFragment.ID] = true
			visitedMagnitude[targetFragment.ID] = absD

			// Track propagation path
			newPath := make([]string, len(it.path)+1)
			copy(newPath, it.path)
			newPath[len(newPath)-1] = targetFragment.ID
			if propagationPaths != nil {
				*propagationPaths = append(*propagationPaths, newPath)
			}

			// Find lenses that scope this fragment (best-effort; tenant routing may vary).
			if e.lensesRepo != nil {
				lenses, err := e.lensesRepo.FindByScope(ctx, "global", targetFragment.CanonicalPath, 100)
				if err == nil {
					for _, lens := range lenses {
						if lens == nil {
							continue
						}
						affectedLenses[lens.ID] = true
					}
				}
			}

			stack = append(stack, waveItem{frag: targetFragment, d: dampedDelta, hops: it.hops + 1, path: newPath})
		}
	}

	// Best-effort diagnostics for "stuck" requests: cycles or extremely deep traversals.
	// Keep it a single line to avoid log spam.
	if hitHopLimit || cycleSkips > 0 {
		rc, ok := rtctx.Get(ctx)
		if ok {
			fmt.Printf("[PropagationEngine] hotPath diagnostics: fragmentId=%s metric=%s hitHopLimit=%v cycleSkips=%d correlationId=%s sessionId=%s\n",
				strings.TrimSpace(sourceFragment.ID),
				strings.TrimSpace(metricName),
				hitHopLimit,
				cycleSkips,
				strings.TrimSpace(rc.CorrelationID),
				strings.TrimSpace(rc.SessionID),
			)
		} else {
			fmt.Printf("[PropagationEngine] hotPath diagnostics: fragmentId=%s metric=%s hitHopLimit=%v cycleSkips=%d\n",
				strings.TrimSpace(sourceFragment.ID),
				strings.TrimSpace(metricName),
				hitHopLimit,
				cycleSkips,
			)
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
	// IMPORTANT: Even though fragments are mostly global, realtime delivery must be scoped to the
	// requesting tenant/project/lens (security + UX). We take routing context from rtctx when available.
	rc, _ := rtctx.Get(ctx)
	tenantID := strings.TrimSpace(rc.TenantID)
	projectID := strings.TrimSpace(rc.ProjectID)
	originLensID := strings.TrimSpace(rc.LensID)
	correlationID := strings.TrimSpace(rc.CorrelationID)
	sessionID := strings.TrimSpace(rc.SessionID)

	// If we don't have a tenant namespace, we cannot safely publish.
	if tenantID == "" {
		tenantID = "global"
	}

	// Публикуем событие начала распространения
	startPayload := map[string]interface{}{
		"tenantId":         tenantID,
		"projectId":        projectID,
		"lensId":           originLensID,
		"sourceFragmentId": sourceFragmentID,
		"ts":               time.Now().UTC().Format(time.RFC3339Nano),
	}
	if correlationID != "" {
		startPayload["correlationId"] = correlationID
	}
	if sessionID != "" {
		startPayload["sessionId"] = sessionID
	}
	_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.propagation.started"), tenantID, startPayload)

	// Публикуем события обновления фрагментов
	for fragmentID := range affectedFragments {
		// Granular propagation update
		nodePayload := map[string]interface{}{
			"tenantId":         tenantID,
			"projectId":        projectID,
			"lensId":           originLensID,
			"sourceFragmentId": sourceFragmentID,
			"fragmentId":       fragmentID,
			"ts":               time.Now().UTC().Format(time.RFC3339Nano),
		}
		if correlationID != "" {
			nodePayload["correlationId"] = correlationID
		}
		if sessionID != "" {
			nodePayload["sessionId"] = sessionID
		}
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.propagation.node_updated"), tenantID, nodePayload)
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.graph.node.upserted"), tenantID, map[string]interface{}{
			"tenantId":   tenantID,
			"projectId":  projectID,
			"lensId":     originLensID,
			"fragmentId": fragmentID,
			"status":     "propagated",
			"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	// Публикуем события обновления Lens
	for lensID := range affectedLenses {
		payload := map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		}
		if originLensID != "" && originLensID != lensID {
			payload["originLensId"] = originLensID
		}
		if correlationID != "" {
			payload["correlationId"] = correlationID
		}
		if sessionID != "" {
			payload["sessionId"] = sessionID
		}
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.lens.updated"), tenantID, payload)
	}

	// Propagation completed summary
	completePayload := map[string]interface{}{
		"tenantId":         tenantID,
		"projectId":        projectID,
		"lensId":           originLensID,
		"sourceFragmentId": sourceFragmentID,
		"nodesUpdated":     len(affectedFragments),
		"lensesAffected":   len(affectedLenses),
		"ts":               time.Now().UTC().Format(time.RFC3339Nano),
	}
	if correlationID != "" {
		completePayload["correlationId"] = correlationID
	}
	if sessionID != "" {
		completePayload["sessionId"] = sessionID
	}
	_ = e.realtime.PublishJSON(ctx, realtime.EventType("horizon.propagation.completed"), tenantID, completePayload)
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

	// Симулируем распространение по критическим путям (с защитой от циклов)
	visitedMagnitude := map[string]float64{sourceFragment.ID: math.Abs(delta)}
	if err := e.simulateHotPath(ctx, sourceFragment, metricName, delta, affectedFragments, affectedLenses, visitedMagnitude, 0); err != nil {
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
		PropagationPaths:  [][]string{}, // Simulate doesn't track paths
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
	visitedMagnitude map[string]float64,
	hops int,
) error {
	if e == nil || sourceFragment == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if hops >= hotPathMaxHops {
		return nil
	}
	if visitedMagnitude == nil {
		visitedMagnitude = map[string]float64{}
	}

	// Получаем исходящие связи
	edges, err := e.edgesRepo.ListBySource(ctx, "global", sourceFragment.ID, nil, 100)
	if err != nil {
		return fmt.Errorf("failed to list edges: %w", err)
	}

	// Обрабатываем каждую связь
	for _, edge := range edges {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Filter out inactive edges (historical or future) from real-time propagation
		if !edge.IsActiveNow() {
			continue
		}

		// Применяем затухание волны
		dampedDelta := e.damping.ApplyDamping(delta, edge.Properties.Amplification, edge.Properties.Criticality)

		// Проверяем порог после затухания
		if !e.threshold.ExceedsThreshold(metricName, dampedDelta, nil) {
			continue
		}

		absD := dampedDelta
		if absD < 0 {
			absD = -absD
		}
		// Защита от циклов: если мы уже приходили в target с более сильной волной — дальше не идём.
		if prev, ok := visitedMagnitude[edge.TargetFragmentID]; ok && prev >= absD {
			continue
		}

		// Получаем целевой фрагмент
		targetFragment, err := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
		if err != nil {
			continue // Пропускаем недоступные фрагменты
		}

		visitedMagnitude[targetFragment.ID] = absD

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
		if err := e.simulateHotPath(ctx, targetFragment, metricName, dampedDelta, affectedFragments, affectedLenses, visitedMagnitude, hops+1); err != nil {
			// Логируем ошибку, но продолжаем симуляцию
			_ = err
		}
	}

	return nil
}
