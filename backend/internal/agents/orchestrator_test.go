package agents

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// TestAgentOrchestrator_BasicFlow тестирует базовый flow создания и выполнения job
func TestAgentOrchestrator_BasicFlow(t *testing.T) {
	// Этот тест требует подключения к Firestore и другим сервисам
	// Запускать только в интеграционном окружении
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test")
	}

	// TODO: Инициализировать зависимости
	// - Firestore client
	// - Repositories
	// - Capability registry с исполнителями
	// - Publisher для WebSocket

	t.Log("TestAgentOrchestrator_BasicFlow: TODO - implement integration test")
}

// TestCapabilityRegistry_RegisterAndGet тестирует регистрацию и получение capabilities
func TestCapabilityRegistry_RegisterAndGet(t *testing.T) {
	registry := NewCapabilityRegistry()

	// Создать mock executor
	mockExecutor := &mockCapabilityExecutor{
		capability: CapabilityDocumentRead,
	}

	// Зарегистрировать
	registry.Register(mockExecutor)

	// Получить
	executor, err := registry.Get(CapabilityDocumentRead)
	if err != nil {
		t.Fatalf("Failed to get executor: %v", err)
	}

	if executor.Capability() != CapabilityDocumentRead {
		t.Errorf("Expected capability %s, got %s", CapabilityDocumentRead, executor.Capability())
	}

	// Попытка получить несуществующий
	_, err = registry.Get(CapabilityWebSearch)
	if err == nil {
		t.Error("Expected error for non-existent capability")
	}
}

// mockCapabilityExecutor mock исполнитель для тестов
type mockCapabilityExecutor struct {
	capability Capability
}

func (m *mockCapabilityExecutor) Capability() Capability {
	return m.capability
}

func (m *mockCapabilityExecutor) ValidateInput(input map[string]interface{}) error {
	return nil
}

func (m *mockCapabilityExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	return &CapabilityResult{
		Success: true,
		Output:  map[string]interface{}{"mock": "result"},
	}, nil
}

// TestAgentStep_Dependencies тестирует логику зависимостей между шагами
func TestAgentStep_Dependencies(t *testing.T) {
	steps := []models.AgentStep{
		{
			ID:         "step1",
			Capability: "document_read",
			Status:     models.AgentStepStatusPending,
			DependsOn:  []string{},
		},
		{
			ID:         "step2",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			DependsOn:  []string{"step1"},
		},
		{
			ID:         "step3",
			Capability: "document_write",
			Status:     models.AgentStepStatusPending,
			DependsOn:  []string{"step2"},
		},
	}

	stepMap := make(map[string]*models.AgentStep)
	for i := range steps {
		stepMap[steps[i].ID] = &steps[i]
	}

	orchestrator := &AgentOrchestrator{
		logger: log.New(os.Stdout, "[TEST] ", log.LstdFlags),
	}

	// Найти готовые шаги (должен быть только step1)
	ready := orchestrator.findReadySteps(steps, stepMap)
	if len(ready) != 1 || ready[0] != "step1" {
		t.Errorf("Expected [step1], got %v", ready)
	}

	// Пометить step1 как completed
	steps[0].Status = models.AgentStepStatusCompleted

	// Теперь step2 должен быть готов
	ready = orchestrator.findReadySteps(steps, stepMap)
	if len(ready) != 1 || ready[0] != "step2" {
		t.Errorf("Expected [step2], got %v", ready)
	}
}

// TestCapability_IsCritical тестирует определение критичности capabilities
func TestCapability_IsCritical(t *testing.T) {
	tests := []struct {
		capability Capability
		expected   bool
	}{
		{CapabilityDocumentRead, true},
		{CapabilityDocumentWrite, true},
		{CapabilityLLMGenerate, false},
		{CapabilityWebSearch, false},
		{CapabilityDataSearch, false},
	}

	for _, tt := range tests {
		if tt.capability.IsCritical() != tt.expected {
			t.Errorf("Capability %s: expected IsCritical=%v, got %v",
				tt.capability, tt.expected, tt.capability.IsCritical())
		}
	}
}

