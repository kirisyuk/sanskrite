package discovery

import (
	"context"
	"encoding/json"
	"log"

	"cloud.google.com/go/pubsub"
)

// GroundingProcessor consumes ground.refresh events and revalidates documents
// from the aggregated operations topic.
type GroundingProcessor struct {
	sub          *pubsub.Subscription
	orchestrator *Orchestrator
	logger       *log.Logger
}

func NewGroundingProcessor(ps *pubsub.Client, topicName string, o *Orchestrator, logger *log.Logger) *GroundingProcessor {
	sub, _ := ensureSubscription(ps, topicName, topicName+".worker", logger)
	return &GroundingProcessor{sub: sub, orchestrator: o, logger: logger}
}

func (w *GroundingProcessor) Start(ctx context.Context) error {
	if w.sub == nil || w.orchestrator == nil {
		return nil
	}
	w.sub.ReceiveSettings.Synchronous = false
	w.sub.ReceiveSettings.MaxOutstandingMessages = 16
	return w.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		// Only handle ground.refresh when subscribed to aggregated topics.
		if m.Attributes["eventType"] != "ground.refresh" {
			m.Ack()
			return
		}
		var payload struct {
			TenantID   string `json:"tenantId"`
			SourceID   string `json:"sourceId"`
			DocumentID string `json:"documentId"`
		}
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			w.logger.Printf("ground worker: bad payload: %v", err)
			m.Ack()
			return
		}
		// best-effort refresh
		if err := w.orchestrator.RefreshDocumentByID(ctx, payload.TenantID, payload.SourceID, payload.DocumentID); err != nil {
			w.logger.Printf("ground worker: refresh error: %v", err)
		}
		m.Ack()
	})
}
