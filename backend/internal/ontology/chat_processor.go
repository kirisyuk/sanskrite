package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"cloud.google.com/go/pubsub"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ChatOntologyProcessor асинхронно обрабатывает чат-сообщения и создает онтологические объекты
type ChatOntologyProcessor struct {
	extractor   *OntologyExtractor
	genaiClient *gen_combined.Client
	interRepo   *repo.InteractionsRepository
	logger      *log.Logger
}

// NewChatOntologyProcessor создает новый процессор чата
func NewChatOntologyProcessor(
	extractor *OntologyExtractor,
	genaiClient *gen_combined.Client,
	interRepo *repo.InteractionsRepository,
	logger *log.Logger,
) *ChatOntologyProcessor {
	return &ChatOntologyProcessor{
		extractor:   extractor,
		genaiClient: genaiClient,
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

Return JSON with entities, events, and claims. Be conservative - only extract clear, factual information.`,
		messageContent)

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"type":       map[string]interface{}{"type": "string"},
						"value":      map[string]interface{}{"type": "string"},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
					"required": []string{"type", "value", "confidence"},
				},
			},
			"events": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":   map[string]interface{}{"type": "string"},
						"dateISO": map[string]interface{}{"type": "string"},
						"context": map[string]interface{}{"type": "string"},
					},
					"required": []string{"title", "dateISO"},
				},
			},
			"claims": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text":       map[string]interface{}{"type": "string"},
						"summary":    map[string]interface{}{"type": "string"},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
					"required": []string{"text", "confidence"},
				},
			},
		},
		"required": []string{"entities", "events", "claims"},
	}

	opts := gen_combined.Options{
		ModelOverride:     "gemini_fast", // Используем быструю модель для чата
		GroundingEnabled:  false,
		SystemInstruction: "You are an expert information extractor. Extract only clear, factual entities, events, and claims from chat messages. Return structured JSON only.",
		Temperature:       0.2, // Низкая температура для точности
		MaxOutputTokens:   2048,
	}

	resp, err := p.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		return nil, fmt.Errorf("LLM extraction failed: %w", err)
	}

	// Парсим результат
	var result struct {
		Entities []ExtractedEntity `json:"entities"`
		Events   []ExtractedEvent  `json:"events"`
		Claims   []ExtractedClaim   `json:"claims"`
	}

	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &ChatExtraction{
		Entities: result.Entities,
		Events:   result.Events,
		Claims:   result.Claims,
	}, nil
}

