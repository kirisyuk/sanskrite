package agents

import (
	"context"
)

// TaskAgentAdapter адаптирует TaskAgent к универсальному интерфейсу Agent
// Реализует Agent через встраивание BaseAgent методов TaskAgent
type TaskAgentAdapter struct {
	TaskAgent // Встраивание для доступа к BaseAgent методам
}

// NewTaskAgentAdapter создает новый адаптер для TaskAgent
func NewTaskAgentAdapter(taskAgent TaskAgent) *TaskAgentAdapter {
	return &TaskAgentAdapter{
		TaskAgent: taskAgent,
	}
}

// ExecuteAsTask реализует универсальный Agent интерфейс
// Просто делегирует вызов TaskAgent.ExecuteWithStream
func (a *TaskAgentAdapter) ExecuteAsTask(
	ctx context.Context,
	taskJSON []byte,
	onUpdate func(Update),
) ([]byte, error) {
	return a.ExecuteWithStream(ctx, taskJSON, onUpdate)
}

