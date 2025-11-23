package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"time"

	"strings"

	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DocumentProcessor consumes document.process events and updates document status
type DocumentProcessor struct {
	sub      *pubsub.Subscription
	docsRepo *repo.DocumentsRepository
	logger   *log.Logger
	exec     *ai.AITaskExecutor
	rules    *repo.RulesRepository
	extractor *ontology.OntologyExtractor
}

func ensureSubscription(ps *pubsub.Client, topicName, subID string, logger *log.Logger) (*pubsub.Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sub := ps.Subscription(subID)
	exists, err := sub.Exists(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return sub, nil
	}
	t := ps.Topic(topicName)
	if ok, _ := t.Exists(ctx); !ok {
		return nil, nil
	}
	created, err := ps.CreateSubscription(ctx, subID, pubsub.SubscriptionConfig{Topic: t})
	if err != nil {
		return nil, err
	}
	logger.Printf("document processor: created subscription %s", subID)
	return created, nil
}

func NewDocumentProcessor(ps *pubsub.Client, topicName string, docs *repo.DocumentsRepository, rules *repo.RulesRepository, exec *ai.AITaskExecutor, extractor *ontology.OntologyExtractor, logger *log.Logger) *DocumentProcessor {
	// Используем имя подписки с суффиксом .document-processor.worker для агрегированного топика
	subID := topicName + ".document-processor.worker"
	sub, _ := ensureSubscription(ps, topicName, subID, logger)
	return &DocumentProcessor{sub: sub, docsRepo: docs, rules: rules, exec: exec, extractor: extractor, logger: logger}
}

func (w *DocumentProcessor) Start(ctx context.Context) error {
	if w.sub == nil {
		if w.logger != nil {
			w.logger.Printf("doc processor: subscription is nil, skipping start")
		}
		return nil
	}
	if w.logger != nil {
		w.logger.Printf("doc processor: starting subscription %s", w.sub.ID())
	}
	w.sub.ReceiveSettings.Synchronous = false
	w.sub.ReceiveSettings.MaxOutstandingMessages = 16
	return w.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		// Фильтруем только события document.process из агрегированного топика
		eventType := m.Attributes["eventType"]
		if eventType != "document.process" {
			m.Ack()
			return
		}
		var payload struct {
			TenantID   string `json:"tenantId"`
			SourceID   string `json:"sourceId"`
			DocumentID string `json:"documentId"`
			ExternalID string `json:"externalId"`
		}
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			w.logger.Printf("doc processor: bad payload: %v", err)
			m.Ack()
			return
		}
		// set processing
		if d, err := w.docsRepo.GetByExternalID(ctx, payload.TenantID, payload.SourceID, payload.ExternalID); err == nil && d != nil {
			d.Status = "processing"
			_ = w.docsRepo.Upsert(ctx, d)
			// summarization via AI (best effort)
			summary := ""
			if w.exec != nil && d.FullText != "" {
				task := &ai.AITask{
					TaskType:     ai.TaskTypeSummarization,
					InputContent: "Summarize in 1-2 sentences:\n\n" + d.FullText,
					TenantID:     payload.TenantID, // Передаем TenantID для записи метрик
					UserID:       "system",         // Системные задачи ingest используют "system" как UserID
					AIRequirements: ai.AIRequirements{
						TextGenerationCapability: true,
						MinCapabilityScore:       5,
						MaxOutputSize:            256,
					},
				}
				if res, err := w.exec.ExecuteTask(ctx, task); err == nil {
					summary = res.Content
				}
			}
			if d.Attrs == nil {
				d.Attrs = map[string]any{}
			}
			if summary != "" {
				d.Attrs["summary"] = summary
			}

			// LLM-based extraction with strict JSON schema + verification
			type extCategory struct {
				Label      string  `json:"label"`
				Confidence float64 `json:"confidence"`
			}
			type extTopic struct {
				Name  string  `json:"name"`
				Share float64 `json:"share"`
			}
			type extEntity struct {
				Type       string  `json:"type"`
				Value      string  `json:"value"`
				Confidence float64 `json:"confidence"`
			}
			type extOutlink struct {
				URL  string `json:"url"`
				Kind string `json:"kind"`
			}
			type extEvent struct {
				Title   string  `json:"title"`
				DateISO string  `json:"dateISO"`
				Context *string `json:"context,omitempty"`
			}
			type extClaim struct {
				Text         string   `json:"text"`
				Summary      *string  `json:"summary,omitempty"`
				Confidence   float64  `json:"confidence"`
				EvidenceURLs []string `json:"evidenceUrls,omitempty"`
			}
			type docExtraction struct {
				Language     string        `json:"language"`
				Sentiment    float64       `json:"sentiment"`
				Categories   []extCategory `json:"categories"`
				Topics       []extTopic    `json:"topics"`
				Entities     []extEntity   `json:"entities"`
				KeyPoints    []string      `json:"keyPoints"`
				DocumentType string        `json:"documentType"`
				Outlinks     []extOutlink  `json:"outlinks"`
				Events       []extEvent    `json:"events"`
				Claims       []extClaim    `json:"claims"`
			}

			validate := func(x docExtraction) error {
				// language: ISO 639-1, 2 letters
				if len(strings.TrimSpace(x.Language)) != 2 {
					return fmt.Errorf("language must be ISO 639-1 code")
				}
				// sentiment [-1..1]
				if x.Sentiment < -1 || x.Sentiment > 1 {
					return fmt.Errorf("sentiment out of range [-1..1]")
				}
				// categories: 1-2 words, confidence [0..1]
				if len(x.Categories) == 0 {
					return fmt.Errorf("at least one category required")
				}
				for _, c := range x.Categories {
					wds := strings.Fields(c.Label)
					if len(wds) == 0 || len(wds) > 2 {
						return fmt.Errorf("category label must be 1-2 words")
					}
					if c.Confidence < 0 || c.Confidence > 1 {
						return fmt.Errorf("category confidence out of range")
					}
				}
				// topics shares in [0..1]
				for _, t := range x.Topics {
					if t.Name == "" || t.Share < 0 || t.Share > 1 {
						return fmt.Errorf("invalid topic entry")
					}
				}
				// entities
				for _, e := range x.Entities {
					if e.Type == "" || e.Value == "" || e.Confidence < 0 || e.Confidence > 1 {
						return fmt.Errorf("invalid entity entry")
					}
				}
				// documentType
				enums := map[string]bool{"article": true, "post": true, "report": true, "announcement": true, "interview": true, "transcript": true, "other": true}
				if !enums[strings.ToLower(strings.TrimSpace(x.DocumentType))] {
					x.DocumentType = "other"
				}
				// outlinks
				for _, ol := range x.Outlinks {
					if strings.TrimSpace(ol.URL) == "" {
						return fmt.Errorf("outlink url empty")
					}
				}
				// events: dateISO non-empty
				for _, ev := range x.Events {
					if ev.Title == "" || ev.DateISO == "" {
						return fmt.Errorf("invalid event entry")
					}
				}
				// claims
				for _, cl := range x.Claims {
					if cl.Text == "" || cl.Confidence < 0 || cl.Confidence > 1 {
						return fmt.Errorf("invalid claim entry")
					}
				}
				return nil
			}

			if w.exec != nil && d.FullText != "" {
				// Limit text size to avoid huge prompts
				text := d.FullText
				if len(text) > 120000 {
					text = text[:120000]
				}
				lang := d.Language
				if strings.TrimSpace(lang) == "" {
					lang = "en"
				}
				prompt := prompts.Format(prompts.DocumentExtractionPrompt, map[string]string{
					"language": strings.ToLower(lang),
					"text":     text,
				})

				ver := ai.NewGenericVerifier(validate)
				t := &ai.AITask{
					TaskType:     ai.TaskTypeContentGeneration,
					InputContent: prompt,
					TenantID:     payload.TenantID, // Передаем TenantID для записи метрик
					UserID:       "system",        // Системные задачи ingest используют "system" как UserID
					AIRequirements: ai.AIRequirements{
						TextGenerationCapability: true,
						MinCapabilityScore:       6,
						MaxOutputSize:            4000,
					},
					Verifier: ver,
				}
				if parsed, err := w.exec.ExecuteStructuredTask(ctx, t); err == nil {
					ex, ok := parsed.(docExtraction)
					if ok {
						// Apply to document
						d.Language = strings.ToLower(strings.TrimSpace(ex.Language))
						d.DocumentType = strings.ToLower(strings.TrimSpace(ex.DocumentType))
						// categories
						cats := make([]map[string]any, 0, len(ex.Categories))
						now := time.Now().UTC().Format(time.RFC3339)
						for _, c := range ex.Categories {
							cats = append(cats, map[string]any{"label": c.Label, "confidence": clamp01(c.Confidence), "method": "llm", "at": now})
						}
						d.Categories = cats
						// attrs
						d.Attrs["sentiment"] = clampRange(ex.Sentiment, -1, 1)
						d.Attrs["keyPoints"] = ex.KeyPoints
						// topics
						var topics []map[string]any
						for _, t := range ex.Topics {
							topics = append(topics, map[string]any{"name": t.Name, "share": clamp01(t.Share)})
						}
						d.Attrs["topics"] = topics
						// outlinks (оставляем для UI)
						var outs []map[string]any
						for _, o := range ex.Outlinks {
							outs = append(outs, map[string]any{"url": o.URL, "kind": strings.ToLower(o.Kind)})
						}
						d.Attrs["outlinks"] = outs

						// Извлекаем онтологические объекты через OntologyExtractor
						if w.extractor != nil {
							extraction := &ontology.DocumentExtraction{
								Entities: make([]ontology.ExtractedEntity, 0, len(ex.Entities)),
								Events:   make([]ontology.ExtractedEvent, 0, len(ex.Events)),
								Claims:   make([]ontology.ExtractedClaim, 0, len(ex.Claims)),
							}

							// Преобразуем entities
							for _, e := range ex.Entities {
								extraction.Entities = append(extraction.Entities, ontology.ExtractedEntity{
									Type:       e.Type,
									Value:      e.Value,
									Confidence: clamp01(e.Confidence),
								})
							}

							// Преобразуем events
						for _, ev := range ex.Events {
								extraction.Events = append(extraction.Events, ontology.ExtractedEvent{
									Title:   ev.Title,
									DateISO: ev.DateISO,
									Context: ev.Context,
								})
						}

							// Преобразуем claims
						for _, c := range ex.Claims {
								extraction.Claims = append(extraction.Claims, ontology.ExtractedClaim{
									Text:         c.Text,
									Summary:      c.Summary,
									Confidence:   clamp01(c.Confidence),
									EvidenceURLs: c.EvidenceURLs,
								})
							}

							// Извлекаем в онтологическую модель
							if err := w.extractor.ExtractFromDocument(ctx, payload.TenantID, d, extraction); err != nil {
								if w.logger != nil {
									w.logger.Printf("doc processor: failed to extract ontology: %v", err)
								}
							}
						}
					}
				}
			}
			// fallback to rules if structured extraction failed or returned zero categories
			if len(d.Categories) == 0 {
				if cats := w.applyRules(ctx, payload.TenantID, payload.SourceID, d.FullText); len(cats) > 0 {
					d.Categories = cats
				} else {
					d.Categories = []map[string]any{{"label": "general", "confidence": 0.5, "method": "rules", "at": time.Now().UTC().Format(time.RFC3339)}}
				}
			}
			d.Status = "processed"
			_ = w.docsRepo.Upsert(ctx, d)
		}
		m.Ack()
	})
}

func (w *DocumentProcessor) applyRules(ctx context.Context, tenantID, sourceID, text string) []map[string]any {
	if w.rules == nil || text == "" {
		return nil
	}
	// evaluate three scopes: global, tenant, source
	var all []*models.Rule
	collect := func(scope map[string]any) {
		rs, err := w.rules.ListByScope(ctx, tenantID, scope)
		if err != nil {
			return
		}
		all = append(all, rs...)
	}
	collect(map[string]any{"level": "global"})
	collect(map[string]any{"level": "tenant", "id": tenantID})
	collect(map[string]any{"level": "source", "id": sourceID})
	if len(all) == 0 {
		return nil
	}
	lower := strings.ToLower(text)
	var cats []map[string]any
	now := time.Now().UTC().Format(time.RFC3339)
	for _, ru := range all {
		label := ""
		if ru.Config != nil {
			if v, ok := ru.Config["label"].(string); ok {
				label = v
			}
		}
		if label == "" {
			continue
		}
		if ruleMatches(ru, lower) {
			cats = append(cats, map[string]any{"label": label, "confidence": 0.7, "method": "rules", "at": now})
		}
	}
	return cats
}

func ruleMatches(r *models.Rule, text string) bool {
	if r == nil || r.Config == nil {
		return false
	}
	kw, ok := r.Config["keywords"]
	if !ok {
		// allow regex-based rules when keywords missing
		if patAny, ok2 := r.Config["regex"]; ok2 {
			if ps, ok3 := patAny.(string); ok3 && ps != "" {
				if re, err := regexp.Compile(ps); err == nil {
					return re.MatchString(text)
				}
			}
		}
		return false
	}
	arr, ok := kw.([]interface{})
	if !ok {
		return false
	}
	for _, it := range arr {
		s, ok := it.(string)
		if !ok || s == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// clamp01 limits a float to [0,1]
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clampRange limits a float to [minVal,maxVal]
func clampRange(v float64, minVal float64, maxVal float64) float64 {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
