package agents

import (
	"context"
	"fmt"
	"time"
)

// Agent интерфейс для всех агентов
type Agent interface {
	// ID возвращает уникальный идентификатор агента
	ID() string
	
	// Name возвращает полное имя агента
	Name() string
	
	// DisplayName возвращает короткое имя для UI
	DisplayName() string
	
	// Description возвращает описание агента
	Description() string
	
	// Skills возвращает список навыков агента
	Skills() []string
	
	// Type возвращает тип агента ("internal" | "external")
	Type() string
	
	// IsExternal возвращает true если агент внешний
	IsExternal() bool
	
	// TaskSchema возвращает JSON Schema для задачи
	TaskSchema() map[string]interface{}
	
	// ResultSchema возвращает JSON Schema для результата
	ResultSchema() map[string]interface{}
	
	// Execute выполняет задание без стриминга
	Execute(ctx context.Context, taskJSON []byte) ([]byte, error)
	
	// ExecuteWithStream выполняет задание со стримингом обновлений через WebSocket
	ExecuteWithStream(
		ctx context.Context,
		taskJSON []byte,
		onUpdate func(Update),
	) ([]byte, error)
}

// Update представляет обновление прогресса агента
type Update struct {
	TaskID    string                 `json:"taskId"`
	AgentID   string                 `json:"agentId"`
	AgentName string                 `json:"agentName"`
	Stage     string                 `json:"stage"`      // "searching", "analyzing", "saving", "completed", "error"
	Progress  float64                `json:"progress"`   // 0.0 - 1.0
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Error     *string                `json:"error,omitempty"`
}

// AgentContext базовый контекст для всех агентов
type AgentContext struct {
	TenantID  string `json:"tenantId"`
	ProjectID string `json:"projectId"` // ОБЯЗАТЕЛЕН
	UserID    string `json:"userId"`
}

// TaskMetadata метаданные задачи (опциональные ID и данные)
type TaskMetadata struct {
	InteractionID *string                `json:"interactionId,omitempty"`
	MessageID     *string                `json:"messageId,omitempty"`
	Custom        map[string]interface{} `json:"custom,omitempty"`
}

// AgentInfo информация об агенте для списка
type AgentInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	Type        string   `json:"type"`
	IsExternal  bool     `json:"isExternal"`
}

// generateTaskID генерирует уникальный ID задачи
func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

