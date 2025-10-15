package discovery

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
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

// PublishDue polls datasources and enqueues source.poll for those with NextPollAt due
func (w *SourcePoller) PublishDue(ctx context.Context) error {
	if w.orchestrator == nil || w.orchestrator.publish == nil || w.orchestrator.sources == nil {
		return nil
	}
	kinds := []string{"rss", "telegram_channel"}
	list, err := w.orchestrator.sources.ListActiveByKinds(ctx, kinds)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, ds := range list {
		if ds.Status != "active" {
			continue
		}
		if ds.NextPollAt != nil && now.Before(*ds.NextPollAt) {
			continue
		}
		payload := map[string]any{"tenantId": ds.TenantID, "sourceId": ds.ID, "kind": ds.Kind}
		_ = w.orchestrator.publish.PublishJSON(ctx, realtime.EventSourcePoll, ds.TenantID, payload)
	}
	return nil
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
		// Fetch datasource to enforce status/nextPollAt
		dsRepo := w.orchestrator.sources
		if dsRepo == nil {
			m.Ack()
			return
		}
		ds, err := dsRepo.Get(ctx, payload.SourceID)
		if err != nil || ds == nil {
			m.Ack()
			return
		}
		// Skip paused/completed
		if ds.Status == "paused" || ds.Status == "completed" {
			m.Ack()
			return
		}
		// Respect NextPollAt if set in the future
		if ds.NextPollAt != nil && time.Now().Before(*ds.NextPollAt) {
			m.Ack()
			return
		}

		// Polling is best-effort and time-bound per message
		ctxP, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		var pollErr error
		switch payload.Kind {
		case "rss":
			if w.orchestrator.emb != nil {
				pollErr = w.orchestrator.IngestRSS(ctxP, payload.TenantID, payload.SourceID, w.orchestrator.emb)
			}
		case "telegram_channel":
			// For Telegram, we need channel handle; we can fetch DataSource to resolve it
			if w.orchestrator.sources != nil {
				if dsTg, err := w.orchestrator.sources.Get(ctxP, payload.SourceID); err == nil && dsTg != nil && dsTg.Details.ChannelUsername != nil {
					last := 0
					if dsTg.Details.LastProcessedMessageID != nil {
						last = *dsTg.Details.LastProcessedMessageID
					}
					pollErr = w.orchestrator.IngestTelegram(ctxP, payload.TenantID, payload.SourceID, *dsTg.Details.ChannelUsername, last)
				}
			}
		default:
			// Unsupported kinds are ignored
		}
		// Update lastPolledAt and compute nextPollAt/backoff
		now := time.Now().UTC()
		ds.LastPolledAt = &now
		// Basic backoff: if error, double backoff up to 1h; else reset and schedule by cadence
		if pollErr != nil {
			b := 60
			if ds.PollBackoffSec != nil && *ds.PollBackoffSec > 0 {
				b = *ds.PollBackoffSec * 2
				if b > 3600 {
					b = 3600
				}
			}
			ds.PollBackoffSec = &b
			next := now.Add(time.Duration(b) * time.Second)
			ds.NextPollAt = &next
		} else {
			// derive from publishingCadence (simple heuristic)
			interval := cadenceToDuration(ds)
			next := now.Add(interval)
			ds.NextPollAt = &next
			zero := 0
			ds.PollBackoffSec = &zero
		}
		_ = dsRepo.Upsert(ctx, ds)
		m.Ack()
	})
}

// cadenceToDuration maps publishingCadence string (if available via profile) to a polling interval.
func cadenceToDuration(ds *models.DataSource) time.Duration {
	// Default 30 minutes
	def := 30 * time.Minute
	// Try to infer from profile attached to this datasource via orchestrator.profiles
	// Minimal implementation: look up profile and parse behaviorPatterns.publishingCadence keywords
	// Since poller doesn't have direct access to profiles repo, fall back to LastFoundAt heuristics
	if ds.LastFoundAt != nil {
		age := time.Since(*ds.LastFoundAt)
		switch {
		case age < 15*time.Minute:
			return 10 * time.Minute
		case age < 2*time.Hour:
			return 30 * time.Minute
		case age < 24*time.Hour:
			return 2 * time.Hour
		default:
			return 6 * time.Hour
		}
	}
	return def
}
