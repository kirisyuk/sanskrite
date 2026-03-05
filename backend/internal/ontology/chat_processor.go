package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

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
			TenantID      string `json:"tenantId"`
			ProjectID     string `json:"projectId"`
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

Return JSON with entities, events, claims, and (optionally) metricUpdates. Be conservative - only extract clear, factual information.

IMPORTANT (Horizon canonical taxonomy):
- When you can normalize a location, provide countryAlpha2 and optional admin1/city for /loc/{country}[/{level1}[/{level2}]].
- countryAlpha2 MUST be ISO3166-1 alpha2 lowercase (e.g., us, cn, de).
- admin1/city SHOULD be lowercase slugs/codes when known; omit unknown trailing levels.
- Country-only locations MUST be represented as: countryAlpha2=<code>.

IMPORTANT (intangible events):
- Include intangible events such as partnerships, alliances/coalitions, union actions, incorporations (new legal entities), policy/regulatory changes, sanctions/licensing, investigations, and dissolutions as events when clearly stated.

IMPORTANT (eventType):
- If you output eventType, it MUST be one of: attack, cyberattack, sanction, blockade, policy_change, protest, strike, election, warning, announcement, incident, other.
- If you cannot map confidently, use "other".

IMPORTANT (spherePaths):
- For each extracted event/claim, set spherePaths to 1..N canonical taxonomy paths under /sphere/...
- Examples:
  - /sphere/geopolitics
  - /sphere/social
  - /sphere/social/information
  - /sphere/security/cyber
  - /sphere/technology/digital
  - /sphere/logistics
  - /sphere/logistics/maritime
  - /sphere/energy
  - /sphere/finance
- If unsure, omit spherePaths.

Metric rules:
- Prefer metricName from this vocabulary when possible (but not required):
  org_headcount, org_revenue, org_operating_margin, org_net_margin, org_cash, org_debt, org_market_cap, org_esg_risk_score,
  capacity_units_per_day, capacity_utilization, downtime_hours, output_volume, lead_time_days, inventory_days,
  price, market_share, demand_index, defect_rate, on_time_delivery_rate,
  shipment_delay_days, freight_cost, port_congestion_index,
  incident_severity, incident_duration_hours, impact_confidence, status_code, sanctions_risk, supply_risk
- If not mappable, create snake_case with prefix org_/fac_/product_/logistics_/risk_/incident_
- Units allowed: usd, eur, rub, units, units_per_day, days, hours, ratio, index_point, code
- IMPORTANT: use ratio (0..1) for shares/rates; do NOT output percent.

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
      "eventType": "string (optional; e.g., attack|cyberattack|sanction|blockade|policy_change|protest|strike|election|warning|announcement|incident|other)",
      "dateISO": "YYYY-MM-DD or RFC3339",
      "context": "string (optional)",
      "summary": "string (optional)",
      "location": {
        "name": "place name (optional)",
        "address": "full address for geocoding (optional)",
        "iso6709": "string (optional)",
        "countryAlpha2": "string (ISO3166-1 alpha2 lowercase, optional)",
        "admin1": "string (lowercase code/slug, optional)",
        "city": "string (lowercase slug, optional)"
      },
      "spherePaths": ["string (optional, canonical /sphere/... taxonomy paths)"],
      "participants": [{"entityName": "string", "role": "agent|target|mediator|mentioned|beneficiary|affected|other"}]
    }
  ],
  "claims": [
    {
      "text": "string",
      "summary": "string (optional)",
      "confidence": 0.0-1.0,
      "evidenceUrls": ["string"],
      "subjectEntityName": "string (optional)",
      "spherePaths": ["string (optional, canonical /sphere/... taxonomy paths)"]
    }
  ],
  "metricUpdates": [
    {
      "metricName": "string",
      "value": 0.0,
      "unit": "string",
      "confidence": 0.0-1.0,
      "observedAt": "YYYY-MM-DD or RFC3339 (optional)",
      "subjectEntityName": "string (optional)"
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
		Entities      []ExtractedEntity       `json:"entities"`
		Events        []ExtractedEvent        `json:"events"`
		Claims        []ExtractedClaim        `json:"claims"`
		MetricUpdates []ExtractedMetricUpdate `json:"metricUpdates"`
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
				if summary, ok := evMap["summary"].(string); ok {
					result.Events[i].Summary = &summary
				}
				if locAny, ok := evMap["location"].(map[string]interface{}); ok {
					var loc ExtractedLocation
					if nm, ok := locAny["name"].(string); ok {
						nm = strings.TrimSpace(nm)
						if nm != "" {
							loc.Name = &nm
						}
					}
					if addr, ok := locAny["address"].(string); ok {
						addr = strings.TrimSpace(addr)
						if addr != "" {
							loc.Address = &addr
						}
					}
					if iso, ok := locAny["iso6709"].(string); ok {
						iso = strings.TrimSpace(iso)
						if iso != "" {
							loc.ISO6709 = &iso
						}
					}
					if cc, ok := locAny["countryAlpha2"].(string); ok {
						cc = strings.ToLower(strings.TrimSpace(cc))
						if cc != "" {
							loc.CountryAlpha2 = &cc
						}
					}
					if a1, ok := locAny["admin1"].(string); ok {
						a1 = strings.ToLower(strings.TrimSpace(a1))
						if a1 != "" {
							loc.Admin1 = &a1
						}
					}
					if ct, ok := locAny["city"].(string); ok {
						ct = strings.ToLower(strings.TrimSpace(ct))
						if ct != "" {
							loc.City = &ct
						}
					}
					if loc.Name != nil || loc.Address != nil || loc.ISO6709 != nil || loc.CountryAlpha2 != nil || loc.Admin1 != nil || loc.City != nil {
						result.Events[i].Location = &loc
					}
				}
				if partsAny, ok := evMap["participants"].([]interface{}); ok {
					parts := make([]ExtractedEventParticipant, 0, len(partsAny))
					for _, pAny := range partsAny {
						pMap, ok := pAny.(map[string]interface{})
						if !ok {
							continue
						}
						var p ExtractedEventParticipant
						if en, ok := pMap["entityName"].(string); ok {
							p.EntityName = strings.TrimSpace(en)
						}
						if rl, ok := pMap["role"].(string); ok {
							p.Role = strings.TrimSpace(rl)
						}
						if p.EntityName != "" {
							parts = append(parts, p)
						}
					}
					if len(parts) > 0 {
						result.Events[i].Participants = parts
					}
				}
				if spAny, ok := evMap["spherePaths"].([]interface{}); ok {
					sps := make([]string, 0, len(spAny))
					for _, it := range spAny {
						if s, ok := it.(string); ok {
							s = strings.TrimSpace(s)
							if s != "" {
								sps = append(sps, s)
							}
						}
					}
					if len(sps) > 0 {
						result.Events[i].SpherePaths = sps
					}
				}
				if et, ok := evMap["eventType"].(string); ok {
					et = strings.TrimSpace(et)
					if et != "" {
						result.Events[i].EventType = et
					}
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
				if su, ok := cMap["subjectEntityName"].(string); ok {
					su = strings.TrimSpace(su)
					if su != "" {
						result.Claims[i].SubjectEntityName = &su
					}
				}
				if evUrls, ok := cMap["evidenceUrls"].([]interface{}); ok {
					urls := make([]string, 0, len(evUrls))
					for _, u := range evUrls {
						if s, ok := u.(string); ok {
							s = strings.TrimSpace(s)
							if s != "" {
								urls = append(urls, s)
							}
						}
					}
					if len(urls) > 0 {
						result.Claims[i].EvidenceURLs = urls
					}
				}
				if spAny, ok := cMap["spherePaths"].([]interface{}); ok {
					sps := make([]string, 0, len(spAny))
					for _, it := range spAny {
						if s, ok := it.(string); ok {
							s = strings.TrimSpace(s)
							if s != "" {
								sps = append(sps, s)
							}
						}
					}
					if len(sps) > 0 {
						result.Claims[i].SpherePaths = sps
					}
				}
			}
		}
	}

	if mus, ok := resultMap["metricUpdates"].([]interface{}); ok {
		result.MetricUpdates = make([]ExtractedMetricUpdate, 0, len(mus))
		for _, it := range mus {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			var mu ExtractedMetricUpdate
			if mn, ok := m["metricName"].(string); ok {
				mn = strings.TrimSpace(mn)
				mu.MetricName = mn
			}
			if v, ok := m["value"].(float64); ok {
				mu.Value = v
			}
			if u, ok := m["unit"].(string); ok {
				u = strings.TrimSpace(u)
				if u != "" {
					mu.Unit = &u
				}
			}
			if c, ok := m["confidence"].(float64); ok {
				mu.Confidence = c
			}
			if obs, ok := m["observedAt"].(string); ok {
				obs = strings.TrimSpace(obs)
				if obs != "" {
					mu.ObservedAt = &obs
				}
			}
			if sub, ok := m["subjectEntityName"].(string); ok {
				sub = strings.TrimSpace(sub)
				if sub != "" {
					mu.SubjectEntityName = &sub
				}
			}
			if mu.MetricName != "" {
				result.MetricUpdates = append(result.MetricUpdates, mu)
			}
		}
	}

	return &ChatExtraction{
		Entities: result.Entities,
		Events:   result.Events,
		Claims:   result.Claims,
		Metrics:  result.MetricUpdates,
	}, nil
}
