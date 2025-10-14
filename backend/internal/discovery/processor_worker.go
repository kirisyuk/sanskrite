package discovery

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"time"

	"strings"

	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DocumentProcessor consumes document.process events and updates document status
type DocumentProcessor struct {
	sub      *pubsub.Subscription
	docsRepo *repo.DocumentsRepository
	logger   *log.Logger
	exec     *ai.AITaskExecutor
	rules    *repo.RulesRepository
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

func NewDocumentProcessor(ps *pubsub.Client, topicName string, docs *repo.DocumentsRepository, rules *repo.RulesRepository, exec *ai.AITaskExecutor, logger *log.Logger) *DocumentProcessor {
	sub, _ := ensureSubscription(ps, topicName, topicName+".worker", logger)
	return &DocumentProcessor{sub: sub, docsRepo: docs, rules: rules, exec: exec, logger: logger}
}

func (w *DocumentProcessor) Start(ctx context.Context) error {
	if w.sub == nil {
		return nil
	}
	w.sub.ReceiveSettings.Synchronous = false
	w.sub.ReceiveSettings.MaxOutstandingMessages = 16
	return w.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
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
				task := &ai.AITask{TaskType: ai.TaskTypeSummarization, InputContent: "Summarize in 1-2 sentences:\n\n" + d.FullText, AIRequirements: ai.AIRequirements{TextGenerationCapability: true, MinCapabilityScore: 5, MaxOutputSize: 256}}
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
			// rules-based categorization
			if cats := w.applyRules(ctx, payload.TenantID, payload.SourceID, d.FullText); len(cats) > 0 {
				d.Categories = cats
			} else {
				d.Categories = []map[string]any{{"label": "general", "confidence": 0.5, "method": "rules", "at": time.Now().UTC().Format(time.RFC3339)}}
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
		for _, r := range rs {
			all = append(all, r)
		}
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
