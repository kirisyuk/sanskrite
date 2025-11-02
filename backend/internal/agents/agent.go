package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// extractJSON извлекает JSON из текста (убирает markdown, лишний текст)
// Улучшенная версия, которая правильно обрабатывает строки и экранирование
func extractJSON(content string) string {
	content = strings.TrimSpace(content)

	// Убираем markdown code blocks
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	// Найти начало JSON (первая { или [)
	start := -1
	for i := 0; i < len(content); i++ {
		if content[i] == '{' || content[i] == '[' {
			start = i
			break
		}
	}
	if start == -1 {
		return content
	}

	// Найти конец JSON, правильно обрабатывая строки и экранирование
	inStr := false
	esc := false
	depthObj := 0
	depthArr := 0
	end := start

	for i := start; i < len(content); i++ {
		ch := content[i]
		
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		
		if ch == '"' {
			inStr = true
			continue
		}
		
		switch ch {
		case '{':
			depthObj++
		case '}':
			if depthObj > 0 {
				depthObj--
				if depthObj == 0 && depthArr == 0 {
					end = i
					break
				}
			}
		case '[':
			depthArr++
		case ']':
			if depthArr > 0 {
				depthArr--
				if depthObj == 0 && depthArr == 0 {
					end = i
					break
				}
			}
		}
	}

	if end > start {
		jsonStr := content[start : end+1]
		
		// Исправляем неэкранированные переносы строк внутри строковых значений
		// Это критично важно для корректного парсинга
		result := make([]byte, 0, len(jsonStr))
		inStr := false
		esc := false
		
		for i := 0; i < len(jsonStr); i++ {
			ch := jsonStr[i]
			
			if inStr {
				if esc {
					esc = false
					result = append(result, ch)
					continue
				}
				if ch == '\\' {
					esc = true
					result = append(result, ch)
					continue
				}
				if ch == '"' {
					inStr = false
					result = append(result, ch)
					continue
				}
				// Если внутри строки найден неэкранированный перенос строки - экранируем его
				if ch == '\n' {
					result = append(result, '\\', 'n')
					continue
				}
				if ch == '\r' {
					result = append(result, '\\', 'r')
					continue
				}
				if ch == '\t' {
					result = append(result, '\\', 't')
					continue
				}
				result = append(result, ch)
				continue
			}
			
			if ch == '"' {
				inStr = true
			}
			result = append(result, ch)
		}
		
		return string(result)
	}

	return content
}

// formatSchemaAsText форматирует JSON Schema в читаемый текст для промпта
func formatSchemaAsText(schema map[string]interface{}) string {
	// Простое форматирование схемы для промпта
	// Преобразуем схему в JSON и форматируем для читаемости
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		// Если не удалось сериализовать, возвращаем базовое описание
		return "JSON object matching the provided schema"
	}
	return string(schemaJSON)
}

