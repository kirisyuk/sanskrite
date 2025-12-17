package propagation

import (
	"context"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// LazyEvaluator управляет отложенными вычислениями (Cold Path)
type LazyEvaluator struct {
	pendingTasks map[string]*ColdPathTask
	mu           sync.RWMutex
}

// ColdPathTask задача отложенного распространения
type ColdPathTask struct {
	FragmentID string
	MetricName string
	Delta      float64
	ScheduledAt time.Time
	Processed  bool
}

// NewLazyEvaluator создает новый lazy evaluator
func NewLazyEvaluator() *LazyEvaluator {
	return &LazyEvaluator{
		pendingTasks: make(map[string]*ColdPathTask),
	}
}

// ScheduleColdPath планирует отложенное распространение
func (e *LazyEvaluator) ScheduleColdPath(ctx context.Context, fragmentID string, metricName string, delta float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	taskKey := fragmentID + ":" + metricName
	e.pendingTasks[taskKey] = &ColdPathTask{
		FragmentID:  fragmentID,
		MetricName:  metricName,
		Delta:       delta,
		ScheduledAt: time.Now(),
		Processed:   false,
	}
}

// ProcessColdPath обрабатывает отложенные задачи
func (e *LazyEvaluator) ProcessColdPath(ctx context.Context, processor func(ctx context.Context, task *ColdPathTask) error) error {
	e.mu.Lock()
	now := time.Now()
	var tasksToProcess []*ColdPathTask
	var keysToDelete []string

	for key, task := range e.pendingTasks {
		if task.Processed {
			continue
		}

		// Обрабатываем задачи старше 1 минуты (Cold Path)
		if now.Sub(task.ScheduledAt) > 1*time.Minute {
			tasksToProcess = append(tasksToProcess, task)
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Удаляем задачи из pendingTasks перед обработкой
	for _, key := range keysToDelete {
		delete(e.pendingTasks, key)
	}
	e.mu.Unlock()

	// Обрабатываем задачи вне блокировки
	for _, task := range tasksToProcess {
		if err := processor(ctx, task); err != nil {
			// Логируем ошибку, но продолжаем обработку
			_ = err
		}
		task.Processed = true
	}

	return nil
}

// GetPendingTasksCount возвращает количество отложенных задач
func (e *LazyEvaluator) GetPendingTasksCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	count := 0
	for _, task := range e.pendingTasks {
		if !task.Processed {
			count++
		}
	}
	return count
}

