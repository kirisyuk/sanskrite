package discovery

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/extraction"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// FactsExtractor consumes document.extract events and persists OntologyFacts.
// Facts-only path: no Document, no Chunks. Storage is GCS/Vertex via OntologyFactsRepository.
type FactsExtractor struct {
	sub      *pubsub.Subscription
	extractor *extraction.NormalizedDocumentExtractor
	logger   *log.Logger
}

// documentExtractPayload matches the JSON published by orchestrator for document.extract
type documentExtractPayload struct {
	TenantID           string         `json:"tenantId"`
	SourceID           string         `json:"sourceId"`
	ExternalID         string         `json:"externalId"`
	FullText           string         `json:"fullText"`
	Title              string         `json:"title"`
	URL                string         `json:"url"`
	Language           string         `json:"language"`
	DocumentType       string         `json:"documentType"`
	SourceMeta         map[string]any `json:"sourceMeta,omitempty"`
	OriginPublishedAt  *string        `json:"originPublishedAt,omitempty"` // RFC3339
}

func ensureFactsExtractorSubscription(ps *pubsub.Client, topicName, subID string, logger *log.Logger) (*pubsub.Subscription, error) {
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
	if logger != nil {
		logger.Printf("facts extractor: created subscription %s", subID)
	}
	return created, nil
}

func NewFactsExtractor(ps *pubsub.Client, topicName string, extractor *extraction.NormalizedDocumentExtractor, logger *log.Logger) *FactsExtractor {
	subID := topicName + ".facts-extractor.worker"
	sub, _ := ensureFactsExtractorSubscription(ps, topicName, subID, logger)
	return &FactsExtractor{sub: sub, extractor: extractor, logger: logger}
}

func (w *FactsExtractor) Start(ctx context.Context) error {
	if w.sub == nil {
		if w.logger != nil {
			w.logger.Printf("facts extractor: subscription is nil, skipping start")
		}
		return nil
	}
	if w.logger != nil {
		w.logger.Printf("facts extractor: starting subscription %s", w.sub.ID())
	}
	w.sub.ReceiveSettings.Synchronous = false
	w.sub.ReceiveSettings.MaxOutstandingMessages = 16
	return w.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		eventType := m.Attributes["eventType"]
		if eventType != string(realtime.EventDocExtract) {
			m.Ack()
			return
		}
		var payload documentExtractPayload
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			if w.logger != nil {
				w.logger.Printf("facts extractor: bad payload: %v", err)
			}
			m.Ack()
			return
		}
		if strings.TrimSpace(payload.FullText) == "" {
			m.Ack()
			return
		}
		nd := &prov.NormalizedDocument{
			ExternalID:   payload.ExternalID,
			Title:        payload.Title,
			URL:          payload.URL,
			FullText:     payload.FullText,
			Language:     payload.Language,
			DocumentType: payload.DocumentType,
			SourceMeta:   payload.SourceMeta,
		}
		if payload.OriginPublishedAt != nil && *payload.OriginPublishedAt != "" {
			if t, err := time.Parse(time.RFC3339, *payload.OriginPublishedAt); err == nil {
				nd.OriginPublishedAt = &t
			}
		}
		if err := w.extractor.ExtractAndPersist(ctx, payload.TenantID, payload.SourceID, nd); err != nil {
			if w.logger != nil {
				w.logger.Printf("facts extractor: extract failed tenant=%s source=%s: %v", payload.TenantID, payload.SourceID, err)
			}
			m.Nack()
			return
		}
		m.Ack()
	})
}
