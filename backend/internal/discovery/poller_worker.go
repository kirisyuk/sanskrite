package discovery

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"cloud.google.com/go/pubsub"
)

// SourcePoller consumes source.poll events and triggers provider-specific polling/ingest
type SourcePoller struct {
	sub          *pubsub.Subscription
	orchestrator *Orchestrator
	logger       *log.Logger
}

func NewSourcePoller(ps *pubsub.Client, topicName string, o *Orchestrator, logger *log.Logger) *SourcePoller {
	sub, _ := ensureSubscription(ps, topicName, topicName+".worker", logger)
	return &SourcePoller{sub: sub, orchestrator: o, logger: logger}
}

func (w *SourcePoller) Start(ctx context.Context) error {
	if w.sub == nil || w.orchestrator == nil {
		return nil
	}
	w.sub.ReceiveSettings.Synchronous = false
	w.sub.ReceiveSettings.MaxOutstandingMessages = 16
	return w.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		var payload struct {
			TenantID string `json:"tenantId"`
			SourceID string `json:"sourceId"`
			Kind     string `json:"kind"`
		}
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			w.logger.Printf("source poller: bad payload: %v", err)
			m.Ack()
			return
		}
		// Polling is best-effort and time-bound per message
		ctxP, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		switch payload.Kind {
		case "rss":
			if w.orchestrator.emb != nil {
				_ = w.orchestrator.IngestRSS(ctxP, payload.TenantID, payload.SourceID, w.orchestrator.emb)
			}
		case "telegram_channel":
			// For Telegram, we need channel handle; we can fetch DataSource to resolve it
			if w.orchestrator.sources != nil {
				if ds, err := w.orchestrator.sources.Get(ctxP, payload.SourceID); err == nil && ds != nil && ds.Details.ChannelUsername != nil {
					last := 0
					if ds.Details.LastProcessedMessageID != nil {
						last = *ds.Details.LastProcessedMessageID
					}
					_ = w.orchestrator.IngestTelegram(ctxP, payload.TenantID, payload.SourceID, *ds.Details.ChannelUsername, last)
				}
			}
		default:
			// Unsupported kinds are ignored
		}
		m.Ack()
	})
}
