package propagation

import (
	"context"
	"time"
)

// LazyEvaluator управляет отложенными вычислениями (Cold Path)
type LazyEvaluator struct {
	pendingTasks map[string]*ColdPathTask
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
	now := time.Now()
	for key, task := range e.pendingTasks {
		if task.Processed {
			continue
		}

		// Обрабатываем задачи старше 5 минут (или по другому критерию)
		if now.Sub(task.ScheduledAt) > 5*time.Minute {
			if err := processor(ctx, task); err != nil {
				// Логируем ошибку, но продолжаем обработку
				_ = err
			}
			task.Processed = true
			delete(e.pendingTasks, key)
		}
	}
	return nil
}

// GetPendingTasksCount возвращает количество отложенных задач
func (e *LazyEvaluator) GetPendingTasksCount() int {
	count := 0
	for _, task := range e.pendingTasks {
		if !task.Processed {
			count++
		}
	}
	return count
}

