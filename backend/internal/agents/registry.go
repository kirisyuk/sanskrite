package agents

import (
	"context"
	"log"
	"sync"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// Registry управляет регистрацией и выполнением агентов
// Использует универсальный Agent интерфейс для поддержки всех типов агентов
type Registry struct {
	agents      map[string]Agent // Универсальный интерфейс Agent
	mu          sync.RWMutex
	executor    *ai.AITaskExecutor
	genaiClient *gen_combined.Client
	publisher   *realtime.Publisher
	logger      *log.Logger
}

// NewRegistry создает новый реестр агентов
func NewRegistry(executor *ai.AITaskExecutor, genaiClient *gen_combined.Client, publisher *realtime.Publisher, logger *log.Logger) *Registry {
	r := &Registry{
		agents:      make(map[string]Agent),
		executor:    executor,
		genaiClient: genaiClient,
		publisher:   publisher,
		logger:      logger,
	}

	// Регистрируем внутренние агенты через адаптеры (usage metrics будут добавлены позже через WithUsageMetrics)
	r.RegisterTaskAgent(NewEntitiesAgent(genaiClient, logger))
	r.RegisterTaskAgent(NewDataAgent(genaiClient, logger))

	return r
}

// WithUsageMetrics sets usage metrics repository and model manager for all registered agents
func (r *Registry) WithUsageMetrics(repo interface {
	CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int) (string, error)
}, modelMgr interface {
	CalculateProcessingCost(modelName string, inputTokens int, outputTokens int) (float64, error)
}) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Update all registered agents with usage metrics
	// Проверяем адаптеры для доступа к исходным агентам
	for id, agent := range r.agents {
		// Проверяем TaskAgentAdapter
		if taskAdapter, ok := agent.(*TaskAgentAdapter); ok {
			if entitiesAgent, ok := taskAdapter.TaskAgent.(*EntitiesAgent); ok {
				entitiesAgent.WithUsageMetrics(repo, modelMgr)
			} else if dataAgent, ok := taskAdapter.TaskAgent.(*DataAgent); ok {
				dataAgent.WithUsageMetrics(repo, modelMgr)
			}
		}
		_ = id // avoid unused variable
	}

	return r
}

// Register регистрирует агента напрямую (если уже реализует Agent интерфейс)
func (r *Registry) Register(agent Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.ID()] = agent
}

// RegisterTaskAgent регистрирует TaskAgent через адаптер
// Создает TaskAgentAdapter и регистрирует его как универсальный Agent
func (r *Registry) RegisterTaskAgent(taskAgent TaskAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	adapter := NewTaskAgentAdapter(taskAgent)
	r.agents[adapter.ID()] = adapter
	if r.logger != nil {
		r.logger.Printf("[Registry] Registered TaskAgent: %s", taskAgent.Name())
	}
}

// RegisterDomainAgent регистрирует DomainAgent через адаптер
// Создает DomainAgentAdapter и регистрирует его как универсальный Agent
func (r *Registry) RegisterDomainAgent(domainAgent DomainAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	adapter := NewDomainAgentAdapter(domainAgent, r.logger)
	r.agents[adapter.ID()] = adapter
	if r.logger != nil {
		r.logger.Printf("[Registry] Registered DomainAgent: %s (domain: %s)", domainAgent.Name(), domainAgent.Domain())
	}
}

// Get возвращает агента по ID
func (r *Registry) Get(id string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[id]
	return agent, ok
}

// List возвращает список всех зарегистрированных агентов
func (r *Registry) List() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []AgentInfo
	for _, agent := range r.agents {
		list = append(list, AgentInfo{
			ID:          agent.ID(),
			Name:        agent.Name(),
			DisplayName: agent.DisplayName(),
			Description: agent.Description(),
			Skills:      agent.Skills(),
			Type:        agent.Type(),
			IsExternal:  agent.IsExternal(),
		})
	}
	return list
}

// ExecuteAgentWithStream выполняет агента со стримингом обновлений через WebSocket
func (r *Registry) ExecuteAgentWithStream(
	ctx context.Context,
	agentID string,
	taskJSON []byte,
	tenantID string,
	projectID string,
) ([]byte, error) {
	// Получаем агента
	agent, ok := r.Get(agentID)
	if !ok {
		return nil, ErrAgentNotFound
	}

	// Callback для отправки обновлений через WebSocket
	onUpdate := func(update Update) {
		if r.publisher != nil {
			err := r.publisher.PublishJSON(ctx, realtime.EventAgentUpdate, tenantID, map[string]interface{}{
				"taskId":    update.TaskID,
				"agentId":   update.AgentID,
				"agentName": update.AgentName,
				"projectId": projectID,
				"stage":     update.Stage,
				"progress":  update.Progress,
				"message":   update.Message,
				"data":      update.Data,
				"timestamp": update.Timestamp.UnixMilli(),
				"error":     update.Error,
			})
			if err != nil {
				if r.logger != nil {
					r.logger.Printf("[Registry] Failed to publish agent update: %v (taskId=%s, agentId=%s, stage=%s)", err, update.TaskID, update.AgentID, update.Stage)
				}
			} else if r.logger != nil {
				r.logger.Printf("[Registry] Published agent update: taskId=%s, agentId=%s, stage=%s, progress=%.2f", update.TaskID, update.AgentID, update.Stage, update.Progress)
			}
		} else if r.logger != nil {
			r.logger.Printf("[Registry] Publisher is nil, cannot send agent update (taskId=%s)", update.TaskID)
		}
	}

	// Выполняем агента через универсальный интерфейс
	return agent.ExecuteAsTask(ctx, taskJSON, onUpdate)
}

// Errors
var (
	ErrAgentNotFound = &AgentError{Message: "agent not found"}
)

type AgentError struct {
	Message string
}

func (e *AgentError) Error() string {
	return e.Message
}
