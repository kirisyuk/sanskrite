package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ChatOntologyProcessor асинхронно обрабатывает чат-сообщения и создает онтологические объекты
type ChatOntologyProcessor struct {
	extractor   *OntologyExtractor
	genaiClient *gen_combined.Client
	executor    *ai.AITaskExecutor
	interRepo   *repo.InteractionsRepository
	logger      *log.Logger
}

// NewChatOntologyProcessor создает новый процессор чата
func NewChatOntologyProcessor(
	extractor *OntologyExtractor,
	genaiClient *gen_combined.Client,
	executor *ai.AITaskExecutor,
	interRepo *repo.InteractionsRepository,
	logger *log.Logger,
) *ChatOntologyProcessor {
	return &ChatOntologyProcessor{
		extractor:   extractor,
		genaiClient: genaiClient,
		executor:    executor,
		interRepo:   interRepo,
		logger:      logger,
	}
}

// Start запускает Pub/Sub подписчик для обработки чат-сообщений
func (p *ChatOntologyProcessor) Start(ctx context.Context, sub *pubsub.Subscription) error {
	if sub == nil {
		return nil
	}

	sub.ReceiveSettings.Synchronous = false
	sub.ReceiveSettings.MaxOutstandingMessages = 16

	return sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		var payload struct {
			TenantID     string `json:"tenantId"`
			ProjectID    string `json:"projectId"`
			InteractionID string `json:"interactionId"`
		}

		if err := json.Unmarshal(m.Data, &payload); err != nil {
			if p.logger != nil {
				p.logger.Printf("[ChatOntologyProcessor] Bad payload: %v", err)
			}
			m.Ack()
			return
		}

		// Получаем Interaction
		interaction, err := p.interRepo.Get(ctx, payload.InteractionID)
		if err != nil || interaction == nil {
			if p.logger != nil {
				p.logger.Printf("[ChatOntologyProcessor] Interaction not found: %s", payload.InteractionID)
			}
			m.Ack()
			return
		}

		// Обрабатываем только user и assistant сообщения
		if interaction.Kind != "user" && interaction.Kind != "assistant" {
			m.Ack()
			return
		}

		// Извлекаем онтологические объекты асинхронно
		go func() {
			if err := p.ProcessInteraction(context.Background(), payload.TenantID, payload.ProjectID, interaction); err != nil {
				if p.logger != nil {
					p.logger.Printf("[ChatOntologyProcessor] Failed to process interaction %s: %v", interaction.ID, err)
				}
			}
		}()

		m.Ack()
	})
}

// ProcessInteraction обрабатывает Interaction и создает онтологические объекты
func (p *ChatOntologyProcessor) ProcessInteraction(
	ctx context.Context,
	tenantID, projectID string,
	interaction *repo.Interaction,
) error {
	if interaction.Content == "" {
		return nil
	}

	// Извлекаем онтологические объекты через LLM
	extraction, err := p.extractFromChatMessage(ctx, interaction.Content)
	if err != nil {
		return fmt.Errorf("failed to extract from chat: %w", err)
	}

	if len(extraction.Entities) == 0 && len(extraction.Events) == 0 && len(extraction.Claims) == 0 {
		// Нет данных для извлечения
		return nil
	}

	// Преобразуем в онтологические объекты
	chatExtraction := &ChatExtraction{
		Entities: extraction.Entities,
		Events:   extraction.Events,
		Claims:   extraction.Claims,
	}

	if err := p.extractor.ExtractFromChat(ctx, tenantID, projectID, interaction, chatExtraction); err != nil {
		return fmt.Errorf("failed to extract ontology: %w", err)
	}

	if p.logger != nil {
		p.logger.Printf("[ChatOntologyProcessor] Processed interaction %s: %d entities, %d events, %d claims",
			interaction.ID, len(extraction.Entities), len(extraction.Events), len(extraction.Claims))
	}

	return nil
}

// extractFromChatMessage извлекает онтологические объекты из сообщения через LLM
func (p *ChatOntologyProcessor) extractFromChatMessage(
	ctx context.Context,
	messageContent string,
) (*ChatExtraction, error) {
	// Создаем промпт для извлечения
	prompt := fmt.Sprintf(`Extract entities, events, and claims from the following chat message.

MESSAGE:
%s

Return JSON with entities, events, and claims. Be conservative - only extract clear, factual information.

Schema:
{
  "entities": [
    {
      "type": "string",
      "value": "string",
      "confidence": 0.0-1.0
    }
  ],
  "events": [
    {
      "title": "string",
      "dateISO": "string",
      "context": "string (optional)"
    }
  ],
  "claims": [
    {
      "text": "string",
      "summary": "string (optional)",
      "confidence": 0.0-1.0
    }
  ]
}`,
		messageContent)

	// Используем AITaskExecutor с верификатором для проверки JSON десериализации
	validateFunc := func(data map[string]interface{}) error {
		return nil
	}
	verifier := ai.NewGenericVerifier(validateFunc)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt + "\n\nReturn JSON strictly matching the schema. Do not include markdown code fences.",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            2048,
			MinContextWindow:         4000,
		},
		Verifier: verifier,
	}

	parsed, err := p.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM extraction failed: %w", err)
	}

	// Преобразуем результат
	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	var result struct {
		Entities []ExtractedEntity `json:"entities"`
		Events   []ExtractedEvent  `json:"events"`
		Claims   []ExtractedClaim   `json:"claims"`
	}

	// Преобразуем map в структуру
	if entities, ok := resultMap["entities"].([]interface{}); ok {
		result.Entities = make([]ExtractedEntity, len(entities))
		for i, e := range entities {
			if eMap, ok := e.(map[string]interface{}); ok {
				if t, ok := eMap["type"].(string); ok {
					result.Entities[i].Type = t
				}
				if v, ok := eMap["value"].(string); ok {
					result.Entities[i].Value = v
				}
				if conf, ok := eMap["confidence"].(float64); ok {
					result.Entities[i].Confidence = conf
				}
			}
		}
	}
	if events, ok := resultMap["events"].([]interface{}); ok {
		result.Events = make([]ExtractedEvent, len(events))
		for i, ev := range events {
			if evMap, ok := ev.(map[string]interface{}); ok {
				if title, ok := evMap["title"].(string); ok {
					result.Events[i].Title = title
				}
				if dateISO, ok := evMap["dateISO"].(string); ok {
					result.Events[i].DateISO = dateISO
				}
				if ctx, ok := evMap["context"].(string); ok {
					result.Events[i].Context = &ctx
				}
			}
		}
	}
	if claims, ok := resultMap["claims"].([]interface{}); ok {
		result.Claims = make([]ExtractedClaim, len(claims))
		for i, c := range claims {
			if cMap, ok := c.(map[string]interface{}); ok {
				if text, ok := cMap["text"].(string); ok {
					result.Claims[i].Text = text
				}
				if summary, ok := cMap["summary"].(string); ok {
					result.Claims[i].Summary = &summary
				}
				if conf, ok := cMap["confidence"].(float64); ok {
					result.Claims[i].Confidence = conf
				}
			}
		}
	}

	return &ChatExtraction{
		Entities: result.Entities,
		Events:   result.Events,
		Claims:   result.Claims,
	}, nil
}

