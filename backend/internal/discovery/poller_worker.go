package discovery

import (
	"context"
	"encoding/json"
	"fmt"
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
// It checks all sources of supported kinds and filters by:
// 1. Source has at least one active project link (project-specific activation)
// 2. NextPollAt is due or not set
// Note: Source global status is not checked here - activation is controlled via project links
func (w *SourcePoller) PublishDue(ctx context.Context) error {
	if w.orchestrator == nil || w.orchestrator.publish == nil || w.orchestrator.sources == nil {
		return nil
	}
	kinds := []string{
		models.SourceKindRSS,
		models.SourceKindTelegramChannel,
		models.SourceKindHorizonFeed,
		models.SourceKindWorldMonitorLegacy, // legacy alias
	}
	// List all sources of supported kinds (regardless of global status)
	// We'll filter by active project links instead
	list, err := w.orchestrator.sources.ListByKinds(ctx, kinds)
	if err != nil {
		return err
	}
	now := time.Now()
	w.logger.Printf("source poller: checking %d sources for polling", len(list))
	for _, ds := range list {
		// Skip if NextPollAt is set and not yet due, UNLESS LastPolledAt is never set (first poll)
		// This ensures sources are polled at least once even if NextPollAt was set initially
		if ds.NextPollAt != nil && now.Before(*ds.NextPollAt) {
			// If LastPolledAt is never set, we should poll anyway (first poll)
			if ds.LastPolledAt == nil {
				w.logger.Printf("source poller: source %s has NextPollAt in future but LastPolledAt is never set - polling anyway (first poll)", ds.ID)
			} else {
				w.logger.Printf("source poller: skipping source %s - NextPollAt is in the future (%s)", ds.ID, ds.NextPollAt.Format(time.RFC3339))
				continue
			}
		}
		// Check if source has at least one active project link
		// For public sources (TenantID=""), we need to check all tenants
		if !w.hasActiveProjects(ctx, ds.TenantID, ds.ID) {
			w.logger.Printf("source poller: skipping source %s - no active projects", ds.ID)
			// Update NextPollAt to avoid "Overdue" status and immediate retry
			// We'll check again after the standard cadence
			interval := cadenceToDuration(ds)
			next := now.Add(interval)
			ds.NextPollAt = &next
			// We don't update LastPolledAt because we didn't actually poll content
			if err := w.orchestrator.sources.Upsert(ctx, ds); err != nil {
				w.logger.Printf("source poller: failed to update NextPollAt for skipped source %s: %v", ds.ID, err)
			}
			continue
		}

		// Check for auto-paused sources (Status="paused" AND ErrorCount >= 10)
		// We should skip them here to avoid spamming the worker, but we must update NextPollAt
		if ds.Status == "paused" && ds.ErrorCount >= 10 {
			// Allow manual override if we want to retry auto-paused sources occasionally
			// For now, strictly skip as per original logic, but update schedule
			w.logger.Printf("source poller: skipping auto-paused source %s (errorCount: %d)", ds.ID, ds.ErrorCount)
			// Check again much later (e.g., 6 hours) or standard cadence
			interval := cadenceToDuration(ds)
			next := now.Add(interval)
			ds.NextPollAt = &next
			if err := w.orchestrator.sources.Upsert(ctx, ds); err != nil {
				w.logger.Printf("source poller: failed to update NextPollAt for auto-paused source %s: %v", ds.ID, err)
			}
			continue
		}

		// If NextPollAt is not set, set an initial estimate based on cadence
		// This ensures sources always have a NextPollAt value
		if ds.NextPollAt == nil {
			interval := cadenceToDuration(ds)
			initialNext := now.Add(interval)
			ds.NextPollAt = &initialNext
			ds.LastPolledAt = nil // Will be set after first poll
			if err := w.orchestrator.sources.Upsert(ctx, ds); err != nil {
				w.logger.Printf("source poller: failed to set initial NextPollAt for source %s: %v", ds.ID, err)
			} else {
				w.logger.Printf("source poller: set initial NextPollAt for source %s to %s", ds.ID, initialNext.Format(time.RFC3339))
			}
		}
		w.logger.Printf("source poller: publishing poll event for source %s (kind: %s)", ds.ID, ds.Kind)
		// For public sources, we need to determine tenantID from active project links
		tenantID := ds.TenantID
		if tenantID == "" {
			// Public source - get tenantID from first active project link
			if w.orchestrator != nil && w.orchestrator.sproj != nil {
				activeStatus := "active"
				links, err := w.orchestrator.sproj.ListBySource(ctx, "", ds.ID, &activeStatus)
				if err == nil && len(links) > 0 {
					tenantID = links[0].TenantID
				}
			}
		}
		payload := map[string]any{"tenantId": tenantID, "sourceId": ds.ID, "kind": ds.Kind}
		_ = w.orchestrator.publish.PublishJSON(ctx, realtime.EventSourcePoll, tenantID, payload)
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
		// Log receipt for debugging
		// w.logger.Printf("source poller: received message %s", string(m.Data))

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

		// Log processing start
		w.logger.Printf("source poller: processing message for source %s (kind: %s)", payload.SourceID, payload.Kind)

		// Fetch datasource to enforce nextPollAt
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
		// Skip if explicitly completed
		if ds.Status == "completed" {
			m.Ack()
			return
		}
		// Skip auto-paused sources (but allow manual "paused" status to be processed if they have active projects)
		// Auto-paused sources have Status="paused" AND ErrorCount >= 10
		// Manual paused sources (Status="paused" with ErrorCount < 10) can still be processed if they have active project links
		// Sources with Status="active" are always allowed (if they have active projects)
		if ds.Status == "paused" && ds.ErrorCount >= 10 {
			// Check if we should retry anyway based on backoff schedule
			// If NextPollAt is due, we should retry even if auto-paused, to allow recovery
			// But if NextPollAt is not set or in future, we skip
			if ds.NextPollAt != nil && time.Now().Before(*ds.NextPollAt) {
				w.logger.Printf("source poller: skipping auto-paused source %s (errorCount: %d, requires manual reactivation)", payload.SourceID, ds.ErrorCount)
				m.Ack()
				return
			}
			// If due, we'll try again below, hoping error clears.
			// If it fails again, ErrorCount increments and backoff increases.
			w.logger.Printf("source poller: retrying auto-paused source %s (errorCount: %d)", payload.SourceID, ds.ErrorCount)
		}
		// Allow sources with Status="active" or Status="paused" (with ErrorCount < 10) to continue
		// They will be checked for active projects below
		// Determine tenantID for processing (critical for public sources to track metrics under project tenant)
		// For public sources (ds.TenantID == ""), we need to get tenantID from active project links
		// This ensures usage metrics are tracked under the tenant that activated the source
		processingTenantID := payload.TenantID
		if processingTenantID == "" && ds.TenantID == "" {
			// Public source - get tenantID from first active project link
			if w.orchestrator != nil && w.orchestrator.sproj != nil {
				activeStatus := "active"
				links, err := w.orchestrator.sproj.ListBySource(ctx, "", payload.SourceID, &activeStatus)
				if err == nil && len(links) > 0 {
					processingTenantID = links[0].TenantID
					w.logger.Printf("source poller: determined tenantID=%s for public source %s from active project", processingTenantID, payload.SourceID)
				}
			}
		}

		// Check if source has at least one active project in rag_source_projects
		// This is the primary activation mechanism - sources are activated per-project
		if !w.hasActiveProjects(ctx, payload.TenantID, payload.SourceID) {
			w.logger.Printf("source poller: skipping source %s - no active projects (status=%s, errorCount=%d)", payload.SourceID, ds.Status, ds.ErrorCount)
			m.Ack()
			return
		}

		// For public sources, we must have a tenantID to track metrics correctly
		// If we still don't have one, skip processing to avoid losing metrics
		if processingTenantID == "" && ds.TenantID == "" {
			w.logger.Printf("source poller: skipping public source %s - cannot determine tenantID for metrics tracking (no active projects found)", payload.SourceID)
			m.Ack()
			return
		}
		// Log that we're processing a paused source (for debugging)
		if ds.Status == "paused" {
			w.logger.Printf("source poller: processing paused source %s (errorCount=%d, has active projects)", payload.SourceID, ds.ErrorCount)
		}
		// Respect NextPollAt if set in the future
		if ds.NextPollAt != nil && time.Now().Before(*ds.NextPollAt) {
			m.Ack()
			return
		}

		// Polling is best-effort and time-bound per message
		// Increased timeout to 45 minutes to allow processing large RSS feeds or multiple Telegram messages
		// Cloud Run timeout must be set correspondingly (e.g. 3600s)
		ctxP, cancel := context.WithTimeout(ctx, 45*time.Minute)
		defer cancel()

		// Track if ingest was attempted and succeeded
		ingestAttempted := false
		var pollErr error

		switch payload.Kind {
		case "rss":
			if w.orchestrator.publish != nil && w.orchestrator.providersReg != nil {
				ingestAttempted = true
				pollErr = w.orchestrator.IngestRSS(ctxP, processingTenantID, payload.SourceID, nil)
			} else {
				w.logger.Printf("source poller: skipping RSS ingest for source %s - missing dependencies (publish=%v, providersReg=%v)",
					payload.SourceID, w.orchestrator.publish != nil, w.orchestrator.providersReg != nil)
				pollErr = fmt.Errorf("ingest dependencies not wired")
			}
		case "telegram_channel":
			// For Telegram, we need channel handle; we can fetch DataSource to resolve it
			if w.orchestrator.sources != nil {
				if dsTg, err := w.orchestrator.sources.Get(ctxP, payload.SourceID); err == nil && dsTg != nil && dsTg.Details.ChannelUsername != nil {
					// Check if all dependencies are available
					if w.orchestrator.telegram != nil && w.orchestrator.docs != nil {
						ingestAttempted = true
						last := 0
						if dsTg.Details.LastProcessedMessageID != nil {
							last = *dsTg.Details.LastProcessedMessageID
						}
						pollErr = w.orchestrator.IngestTelegram(ctxP, processingTenantID, payload.SourceID, *dsTg.Details.ChannelUsername, last)
					} else {
						w.logger.Printf("source poller: skipping Telegram ingest for source %s - missing dependencies (telegram=%v, docs=%v)",
							payload.SourceID, w.orchestrator.telegram != nil, w.orchestrator.docs != nil)
						pollErr = fmt.Errorf("ingest dependencies not wired")
					}
				} else {
					w.logger.Printf("source poller: failed to get Telegram source %s or missing channel username", payload.SourceID)
					pollErr = fmt.Errorf("source not found or invalid")
				}
			} else {
				pollErr = fmt.Errorf("sources repository not available")
			}
		case models.SourceKindHorizonFeed, models.SourceKindWorldMonitorLegacy:
			if w.orchestrator.docs != nil {
				ingestAttempted = true
				pollErr = w.orchestrator.IngestHorizonMonitorFeed(ctxP, processingTenantID, payload.SourceID, w.orchestrator.emb)
			} else {
				w.logger.Printf("source poller: skipping horizon_monitor_feed ingest for source %s - missing dependencies (docs=%v)",
					payload.SourceID, w.orchestrator.docs != nil)
				pollErr = fmt.Errorf("ingest dependencies not wired")
			}
		default:
			// Unsupported kinds are ignored
			w.logger.Printf("source poller: unsupported source kind: %s", payload.Kind)
			pollErr = fmt.Errorf("unsupported kind: %s", payload.Kind)
		}

		// ALWAYS update LastPolledAt and NextPollAt, regardless of ingest result
		// This ensures these fields are never empty and polling continues
		now := time.Now().UTC()

		// If ingest was attempted and succeeded, orchestrator should have updated NextPollAt
		// Re-fetch to get latest state (orchestrator may have updated it)
		if ingestAttempted && pollErr == nil {
			if updatedDs, err := dsRepo.Get(ctx, payload.SourceID); err == nil && updatedDs != nil {
				ds = updatedDs
				// Reset error count on successful ingest
				ds.ErrorCount = 0
				// Ensure LastPolledAt is set (orchestrator should have set it, but double-check)
				if ds.LastPolledAt == nil {
					ds.LastPolledAt = &now
				}
				// If orchestrator didn't update NextPollAt, set it here
				if ds.NextPollAt == nil {
					interval := cadenceToDuration(ds)
					next := now.Add(interval)
					ds.NextPollAt = &next
					zero := 0
					ds.PollBackoffSec = &zero
					w.logger.Printf("source poller: orchestrator didn't update NextPollAt, setting it to %s", next.Format(time.RFC3339))
				}
			} else {
				// Re-fetch failed, use current ds and update
				ds.LastPolledAt = &now
				ds.ErrorCount = 0 // Reset on success
				interval := cadenceToDuration(ds)
				next := now.Add(interval)
				ds.NextPollAt = &next
				zero := 0
				ds.PollBackoffSec = &zero
			}
		} else {
			// Ingest failed or wasn't attempted - always update LastPolledAt and NextPollAt
			ds.LastPolledAt = &now
			// Ingest failed or wasn't attempted - set NextPollAt with backoff or cadence
			if pollErr != nil {
				w.logger.Printf("source poller: ingest error for source %s: %v", payload.SourceID, pollErr)
				// Increment error count
				ds.ErrorCount++

				// Auto-pause source after 10 consecutive errors to prevent infinite retries
				const maxConsecutiveErrors = 10
				if ds.ErrorCount >= maxConsecutiveErrors {
					if ds.Status != "paused" && ds.Status != "error" {
						oldStatus := ds.Status
						ds.Status = "paused"
						w.logger.Printf("source poller: auto-pausing source %s after %d consecutive errors (status changed from %s to paused)", payload.SourceID, ds.ErrorCount, oldStatus)
					}
				}

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
				w.logger.Printf("source poller: set NextPollAt with backoff for source %s to %s (backoff: %ds, errorCount: %d)", payload.SourceID, next.Format(time.RFC3339), b, ds.ErrorCount)
			} else {
				// No error but ingest wasn't attempted - use cadence
				interval := cadenceToDuration(ds)
				next := now.Add(interval)
				ds.NextPollAt = &next
				zero := 0
				ds.PollBackoffSec = &zero
				w.logger.Printf("source poller: set NextPollAt for source %s to %s (interval: %v, no ingest attempted)", payload.SourceID, next.Format(time.RFC3339), interval)
			}
		}

		// Always update LastPolledAt (set above) and NextPollAt
		if err := dsRepo.Upsert(ctx, ds); err != nil {
			w.logger.Printf("source poller: failed to update NextPollAt/LastPolledAt for source %s: %v", payload.SourceID, err)
		} else {
			w.logger.Printf("source poller: successfully updated NextPollAt=%s and LastPolledAt=%s for source %s",
				ds.NextPollAt.Format(time.RFC3339), ds.LastPolledAt.Format(time.RFC3339), payload.SourceID)
		}
		m.Ack()
	})
}

// hasActiveProjects checks if the source has at least one active project in rag_source_projects
// For public sources (tenantID=""), it checks all tenants
func (w *SourcePoller) hasActiveProjects(ctx context.Context, tenantID, sourceID string) bool {
	if w.orchestrator == nil || w.orchestrator.sproj == nil {
		// If SourceProjectsRepository is not available, allow processing for backward compatibility
		return true
	}
	activeStatus := "active"
	var links []*models.SourceProject
	var err error

	if tenantID == "" {
		// Public source - check all tenants by passing empty tenantID
		// ListBySource with empty tenantID should return links for all tenants
		links, err = w.orchestrator.sproj.ListBySource(ctx, "", sourceID, &activeStatus)
	} else {
		// Tenant-scoped source - check only this tenant
		links, err = w.orchestrator.sproj.ListBySource(ctx, tenantID, sourceID, &activeStatus)
	}

	if err != nil {
		w.logger.Printf("source poller: failed to check active projects for source %s (tenantID=%s): %v", sourceID, tenantID, err)
		// On error, allow processing to avoid blocking (fail-open)
		return true
	}
	return len(links) > 0
}

// cadenceToDuration maps publishingCadence string (if available via profile) to a polling interval.
// In poller_worker, we don't know if new content was found, so we rely on LastFoundAt heuristics.
func cadenceToDuration(ds *models.DataSource) time.Duration {
	if ds != nil && ds.Details.PollIntervalMinutes != nil && *ds.Details.PollIntervalMinutes > 0 {
		return time.Duration(*ds.Details.PollIntervalMinutes) * time.Minute
	}
	if ds != nil && models.IsHorizonMonitorFeedKind(ds.Kind) {
		return 30 * time.Minute
	}
	// Default 30 minutes
	def := 30 * time.Minute
	// Try to infer from profile attached to this datasource via orchestrator.profiles
	// Minimal implementation: look up profile and parse behaviorPatterns.publishingCadence keywords
	// Since poller doesn't have direct access to profiles repo, fall back to LastFoundAt heuristics
	if ds != nil && ds.LastFoundAt != nil {
		age := time.Since(*ds.LastFoundAt)
		switch {
		case age < 15*time.Minute:
			// Recently found content - check more frequently
			return 10 * time.Minute
		case age < 2*time.Hour:
			// Found content within last 2 hours - moderate frequency
			return 30 * time.Minute
		case age < 24*time.Hour:
			// Found content within last day - check less frequently
			return 2 * time.Hour
		default:
			// No new content for more than 24 hours - check rarely
			return 6 * time.Hour
		}
	}
	// No LastFoundAt set - use default (likely first poll or no content ever found)
	return def
}
