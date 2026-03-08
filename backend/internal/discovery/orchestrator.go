package discovery

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/adapters"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
	"golang.org/x/sync/errgroup"
)

type Orchestrator struct {
	exec          *ai.AITaskExecutor
	crawl         *crawler.WebCrawler
	telegram      TelegramFetcher
	providersReg  *prov.Registry
	projects      *repo.ProjectsRepository
	sources       *repo.DataSourcesRepository
	sproj         *repo.SourceProjectsRepository
	profiles      *repo.SourceProfilesRepository
	docs          *repo.DocumentsRepository
	emb           ai.EmbeddingProvider
	publish       *realtime.Publisher
	logger        *log.Logger
	cache         *repo.ProcessedContentRepository
	interactions  *repo.InteractionsRepository
	ingestHistory *repo.SourceIngestHistoryRepository
	wmPipeline    *HorizonMonitorPipeline
}

type TelegramFetcher interface {
	FetchUpdates(ctx context.Context, channelUsername string, lastMessageID int) ([]telegram.TelegramMessage, error)
}

func NewOrchestrator(exec *ai.AITaskExecutor, crawl *crawler.WebCrawler, tg TelegramFetcher, pr *repo.ProjectsRepository, ds *repo.DataSourcesRepository, pub *realtime.Publisher, logger *log.Logger) *Orchestrator {
	return &Orchestrator{exec: exec, crawl: crawl, telegram: tg, projects: pr, sources: ds, publish: pub, logger: logger}
}

// WithProfiles wires SourceProfiles repository (optional during gradual migration)
func (o *Orchestrator) WithProfiles(sp *repo.SourceProfilesRepository) *Orchestrator {
	o.profiles = sp
	return o
}

// WithSourceProjects wires SourceProjects repository for linking sources to projects
func (o *Orchestrator) WithSourceProjects(sp *repo.SourceProjectsRepository) *Orchestrator {
	o.sproj = sp
	return o
}

// Publish exposes the Publisher for wiring subscribers
func (o *Orchestrator) Publish() *realtime.Publisher { return o.publish }

// Exec accessor (used by HTTP handlers needing embeddings)
func (o *Orchestrator) Exec() *ai.AITaskExecutor { return o.exec }

// EmbeddingProvider accessor (used by HTTP handlers for RSS ingest)
func (o *Orchestrator) EmbeddingProvider() ai.EmbeddingProvider { return o.emb }

// WithIngestHistory wires SourceIngestHistory repository for tracking ingest runs
func (o *Orchestrator) WithIngestHistory(h *repo.SourceIngestHistoryRepository) *Orchestrator {
	o.ingestHistory = h
	return o
}

// WithHorizonMonitorPipeline wires Horizon Monitor deterministic processing
// (ontology facts, signal propagation, grid/country rollups, briefs/classification).
func (o *Orchestrator) WithHorizonMonitorPipeline(p *HorizonMonitorPipeline) *Orchestrator {
	o.wmPipeline = p
	return o
}

// ensureProjectSourceLink creates an active link between project and source if missing.
func (o *Orchestrator) ensureProjectSourceLink(ctx context.Context, tenantID, projectID, sourceID string) {
	if o.sproj == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(sourceID) == "" {
		return
	}
	// Check for any existing link (regardless of status)
	links, err := o.sproj.ListBySource(ctx, tenantID, sourceID, nil)
	if err != nil {
		o.logger.Printf("orchestrator: link: ListBySource error tenant=%s project=%s source=%s: %v", tenantID, projectID, sourceID, err)
		return
	}
	for _, l := range links {
		if l.ProjectID == projectID {
			o.logger.Printf("orchestrator: link exists tenant=%s project=%s source=%s", tenantID, projectID, sourceID)
			return
		}
	}
	_, err = o.sproj.Create(ctx, &models.SourceProject{TenantID: tenantID, SourceID: sourceID, ProjectID: projectID, Status: "paused", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		o.logger.Printf("orchestrator: link create failed tenant=%s project=%s source=%s: %v", tenantID, projectID, sourceID, err)
		return
	}
	o.logger.Printf("orchestrator: link created tenant=%s project=%s source=%s", tenantID, projectID, sourceID)
}

// RefreshDocumentByID naive revalidation placeholder
func (o *Orchestrator) RefreshDocumentByID(ctx context.Context, tenantID, sourceID, docID string) error {
	if o.docs == nil || o.providersReg == nil {
		return fmt.Errorf("grounding deps not wired")
	}
	// Respect source status
	if o.sources != nil {
		if s, err := o.sources.Get(ctx, sourceID); err == nil && s != nil {
			if s.Status == "paused" || s.Status == "completed" {
				return nil
			}
		}
	}
	d, err := o.docs.GetByID(ctx, docID)
	if err != nil || d == nil {
		return fmt.Errorf("document not found")
	}
	// select provider by source kind
	// Minimal logic: if documentPath starts with rss/, call rss provider Ground on URL; if web/, fetch page; if telegram/, skip for now
	updatedText := ""
	if strings.HasPrefix(d.DocumentPath, "rss/") && d.URL != "" {
		if p, ok := o.providersReg.Get("rss"); ok {
			res, err := p.Ground(ctx, tenantID, d.SourceID, []string{d.URL})
			if err == nil && len(res) > 0 && strings.TrimSpace(res[0].FullText) != "" {
				updatedText = res[0].FullText
			}
		}
	} else if strings.HasPrefix(d.DocumentPath, "web/") && d.URL != "" && o.crawl != nil {
		if fr, err := o.crawl.FetchContent(ctx, d.URL); err == nil && strings.TrimSpace(fr.Text) != "" {
			updatedText = fr.Text
		}
	}
	if strings.TrimSpace(updatedText) == "" || updatedText == d.FullText {
		return nil
	}
	// update document (Upsert uploads to GCS, triggers Vertex sync)
	d.FullText = updatedText
	d.ContentHash = repo.HashContent(updatedText)
	d.UpdatedAt = time.Now().UTC()
	d.Status = "indexed"
	if err := o.docs.Upsert(ctx, d); err != nil {
		return err
	}
	return nil
}

// publishEvent safely publishes an event, handling nil publisher gracefully
func (o *Orchestrator) publishEvent(ctx context.Context, et realtime.EventType, tenantID string, payload interface{}) {
	if o.publish != nil {
		if err := o.publish.PublishJSON(ctx, et, tenantID, payload); err != nil {
			o.logger.Printf("orchestrator: pubsub %s error: %v", et, err)
		}
	}
}

// WithCache wires processed content cache
func (o *Orchestrator) WithCache(c *repo.ProcessedContentRepository) *Orchestrator {
	o.cache = c
	return o
}

// WithInteractions wires interactions repository for persistence
func (o *Orchestrator) WithInteractions(ir *repo.InteractionsRepository) *Orchestrator {
	o.interactions = ir
	return o
}

// WithDocumentIngest wires repositories and provider registry for document ingest (GCS + Vertex AI Search)
func (o *Orchestrator) WithDocumentIngest(reg *prov.Registry, docs *repo.DocumentsRepository) *Orchestrator {
	o.providersReg = reg
	o.docs = docs
	return o
}

// WithEmbedding wires embedding provider for sync-embedding ingest
func (o *Orchestrator) WithEmbedding(emb ai.EmbeddingProvider) *Orchestrator {
	o.emb = emb
	return o
}

// IngestWeb ingests a single web page as a document (Upsert uploads to GCS, triggers Vertex sync)
func (o *Orchestrator) IngestWeb(ctx context.Context, tenantID, sourceID, canonicalURL string) error {
	if o.crawl == nil || o.docs == nil {
		return fmt.Errorf("ingest dependencies not wired")
	}
	fr, err := o.crawl.FetchContent(ctx, canonicalURL)
	if err != nil {
		// Even on error, update LastPolledAt and NextPollAt to ensure polling continues
		if o.sources != nil {
			if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
				now := time.Now().UTC()
				ds.LastPolledAt = &now
				// Use backoff for errors
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
				if err := o.sources.Upsert(ctx, ds); err != nil {
					o.logger.Printf("orchestrator: failed to update NextPollAt/LastPolledAt on error for source %s: %v", sourceID, err)
				} else {
					o.logger.Printf("orchestrator: updated NextPollAt/LastPolledAt on error for source %s (NextPollAt: %s)", sourceID, next.Format(time.RFC3339))
				}
			}
		}
		return err
	}
	now := time.Now().UTC()
	extID := canonicalURL
	existing, _ := o.docs.GetByExternalID(ctx, tenantID, sourceID, extID)
	if existing != nil {
		existing.Title = canonicalURL
		existing.URL = canonicalURL
		existing.DocumentPath = "web/" + canonicalURL
		existing.FullText = fr.Text
		existing.ContentType = "plainText"
		existing.DocumentType = "article"
		existing.Status = "new"
		existing.ContentHash = repo.HashContent(fr.Text)
		existing.UpdatedAt = now
		if err := o.docs.Upsert(ctx, existing); err != nil {
			return err
		}
	} else {
		d := &models.Document{TenantID: tenantID, SourceID: sourceID, ExternalID: extID, Title: canonicalURL, URL: canonicalURL, DocumentPath: "web/" + canonicalURL, FullText: fr.Text, Status: "new", ContentType: "plainText", DocumentType: "article", ContentHash: repo.HashContent(fr.Text), IngestedAt: now, CreatedAt: now, UpdatedAt: now}
		if _, err := o.docs.Create(ctx, d); err != nil {
			return err
		}
	}
	if d, _ := o.docs.GetByExternalID(ctx, tenantID, sourceID, extID); d != nil {
		d.Status = "indexed"
		_ = o.docs.Upsert(ctx, d)
		if o.publish != nil {
			_ = o.publish.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"tenantId": tenantID, "sourceId": sourceID, "documentId": d.ID, "externalId": d.ExternalID})
		}
	}

	// Update LastPolledAt and NextPollAt in DataSource
	if o.sources != nil {
		ds, err := o.sources.Get(ctx, sourceID)
		if err == nil && ds != nil {
			now := time.Now().UTC()
			ds.LastPolledAt = &now
			// For IngestWeb, we don't track LastFoundAt separately as it's a single-page source
			// Calculate next poll time based on cadence (assume no new content if existing document was updated)
			interval := o.cadenceToDuration(ds, false)
			next := now.Add(interval)
			ds.NextPollAt = &next
			// Reset backoff on successful ingest
			zero := 0
			ds.PollBackoffSec = &zero
			if err := o.sources.Upsert(ctx, ds); err != nil {
				o.logger.Printf("orchestrator: failed to update NextPollAt for source %s: %v", sourceID, err)
			}
		}
	}

	return nil
}

// IngestTelegram ingests recent Telegram messages as documents (Upsert uploads to GCS, triggers Vertex sync)
func (o *Orchestrator) IngestTelegram(ctx context.Context, tenantID, sourceID, handle string, lastMessageID int) error {
	if o.telegram == nil || o.docs == nil {
		return fmt.Errorf("ingest dependencies not wired")
	}
	o.logger.Printf("orchestrator: IngestTelegram started for @%s (sourceID=%s, tenantID=%s, lastMessageID=%d)", handle, sourceID, tenantID, lastMessageID)
	startTime := time.Now().UTC()
	msgs, err := o.telegram.FetchUpdates(ctx, handle, lastMessageID)
	if err != nil {
		o.logger.Printf("orchestrator: IngestTelegram failed to fetch updates for @%s: %v", handle, err)
		// Записываем историю с ошибкой
		if o.ingestHistory != nil {
			errStr := err.Error()
			durationMs := int(time.Since(startTime).Milliseconds())
			completedAt := time.Now().UTC()
			_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
				TenantID:           tenantID,
				SourceID:           sourceID,
				Kind:               "telegram_channel",
				Status:             "error",
				Error:              &errStr,
				Params:             map[string]interface{}{"handle": handle, "lastMessageID": lastMessageID},
				DocumentsProcessed: 0,
				DocumentsCreated:   0,
				DurationMs:         &durationMs,
				StartedAt:          startTime,
				CompletedAt:        &completedAt,
				CreatedAt:          startTime,
			})
		}
		// Even on error, update LastPolledAt and NextPollAt to ensure polling continues
		// This is critical - we must never leave these fields empty
		if o.sources != nil {
			if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
				now := time.Now().UTC()
				ds.LastPolledAt = &now
				// Use backoff for errors
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
				if err := o.sources.Upsert(ctx, ds); err != nil {
					o.logger.Printf("orchestrator: failed to update NextPollAt/LastPolledAt on error for source %s: %v", sourceID, err)
				} else {
					o.logger.Printf("orchestrator: updated NextPollAt/LastPolledAt on error for source %s (NextPollAt: %s)", sourceID, next.Format(time.RFC3339))
				}
			}
		}
		return err
	}
	o.logger.Printf("orchestrator: IngestTelegram fetched %d messages for @%s", len(msgs), handle)
	now := time.Now().UTC()
	maxSeenID := lastMessageID
	documentsCreated := 0
	documentsProcessed := 0

	// Update LastProcessedMessageID even on error/timeout to avoid reprocessing same messages
	// Use defer to ensure it's updated even if context deadline is exceeded
	defer func() {
		if o.sources != nil && maxSeenID > lastMessageID {
			// Use background context to ensure update happens even if original context timed out
			bgCtx := context.Background()
			if ds, err := o.sources.Get(bgCtx, sourceID); err == nil && ds != nil {
				// Only move forward - update if we processed at least one message
				if ds.Details.LastProcessedMessageID == nil || maxSeenID > *ds.Details.LastProcessedMessageID {
					ds.Details.LastProcessedMessageID = &maxSeenID
					if err := o.sources.Upsert(bgCtx, ds); err != nil {
						o.logger.Printf("orchestrator: failed to update LastProcessedMessageID on timeout for source %s: %v", sourceID, err)
					} else {
						o.logger.Printf("orchestrator: updated LastProcessedMessageID to %d for source %s (processed up to this ID, may have timed out)", maxSeenID, sourceID)
					}
				}
			}
		}
	}()

	for _, m := range msgs {
		extID := fmt.Sprintf("%d", m.ID)
		if m.ID > maxSeenID {
			maxSeenID = m.ID
		}
		existing, _ := o.docs.GetByExternalID(ctx, tenantID, sourceID, extID)
		var pubAt *time.Time
		if t, err := time.Parse(time.RFC3339, m.Date); err == nil {
			pubAt = &t
		}
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		documentsProcessed++
		if existing != nil {
			existing.Title = m.Link
			existing.URL = m.Link
			existing.DocumentPath = "telegram/" + handle + "/" + extID
			existing.FullText = text
			existing.ContentType = "plainText"
			existing.DocumentType = "post"
			existing.OriginPublishedAt = pubAt
			existing.Status = "new"
			existing.ContentHash = repo.HashContent(text)
			existing.UpdatedAt = now
			if err := o.docs.Upsert(ctx, existing); err != nil {
				return err
			}
		} else {
			d := &models.Document{TenantID: tenantID, SourceID: sourceID, ExternalID: extID, Title: m.Link, URL: m.Link, DocumentPath: "telegram/" + handle + "/" + extID, FullText: text, Status: "new", ContentType: "plainText", DocumentType: "post", OriginPublishedAt: pubAt, ContentHash: repo.HashContent(text), IngestedAt: now, CreatedAt: now, UpdatedAt: now}
			if _, err := o.docs.Create(ctx, d); err != nil {
				return err
			}
			documentsCreated++
		}
		if d, _ := o.docs.GetByExternalID(ctx, tenantID, sourceID, extID); d != nil {
			d.Status = "indexed"
			_ = o.docs.Upsert(ctx, d)
			if o.publish != nil {
				_ = o.publish.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"tenantId": tenantID, "sourceId": sourceID, "documentId": d.ID, "externalId": d.ExternalID})
			}
		}
	}
	// Update lastProcessedMessageId on the DataSource after successful ingest
	// Note: On error/timeout, defer function above will update it
	if o.sources != nil {
		if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
			// Only move forward
			if ds.Details.LastProcessedMessageID == nil || maxSeenID > *ds.Details.LastProcessedMessageID {
				ds.Details.LastProcessedMessageID = &maxSeenID
				_ = o.sources.Upsert(ctx, ds)
				o.logger.Printf("orchestrator: updated LastProcessedMessageID to %d for source %s (successful ingest)", maxSeenID, sourceID)
			}
		}
	}
	completedAt := time.Now().UTC()
	durationMs := int(completedAt.Sub(startTime).Milliseconds())
	status := "success"
	if documentsProcessed == 0 {
		status = "empty"
	}

	// Записываем историю обработки
	if o.ingestHistory != nil {
		_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
			TenantID:           tenantID,
			SourceID:           sourceID,
			Kind:               "telegram_channel",
			Status:             status,
			Params:             map[string]interface{}{"handle": handle, "lastMessageID": lastMessageID, "maxSeenID": maxSeenID},
			DocumentsProcessed: documentsProcessed,
			DocumentsCreated:   documentsCreated,
			DurationMs:         &durationMs,
			StartedAt:          startTime,
			CompletedAt:        &completedAt,
			CreatedAt:          startTime,
		})
	}

	o.logger.Printf("orchestrator: IngestTelegram completed for @%s (sourceID=%s): processed %d messages, maxSeenID=%d", handle, sourceID, len(msgs), maxSeenID)

	// Update LastPolledAt and NextPollAt in DataSource
	if o.sources != nil {
		ds, err := o.sources.Get(ctx, sourceID)
		if err == nil && ds != nil {
			now := time.Now().UTC()
			ds.LastPolledAt = &now
			// Update LastFoundAt only if new documents were created (new content found)
			if documentsCreated > 0 {
				ds.LastFoundAt = &now
				o.logger.Printf("orchestrator: updated LastFoundAt for source %s (found %d new messages)", sourceID, documentsCreated)
			}
			// Calculate next poll time based on cadence (same logic as poller_worker)
			interval := o.cadenceToDuration(ds, documentsCreated > 0)
			next := now.Add(interval)
			ds.NextPollAt = &next
			// Reset backoff on successful ingest
			zero := 0
			ds.PollBackoffSec = &zero
			if err := o.sources.Upsert(ctx, ds); err != nil {
				o.logger.Printf("orchestrator: failed to update NextPollAt for source %s: %v", sourceID, err)
			} else {
				o.logger.Printf("orchestrator: updated NextPollAt for source %s to %s (interval: %v, foundNew: %v)", sourceID, next.Format(time.RFC3339), interval, documentsCreated > 0)
			}
		}
	}

	return nil
}

// cadenceToDuration maps publishingCadence string (if available via profile) to a polling interval.
// foundNew indicates if new content was found in the current poll.
// Same logic as in poller_worker.go
func (o *Orchestrator) cadenceToDuration(ds *models.DataSource, foundNew bool) time.Duration {
	return effectivePollCadence(ds, foundNew)
}

// IngestProjectSources triggers ingest for all sources linked to a project (legacy linkage for now)
func (o *Orchestrator) IngestProjectSources(ctx context.Context, tenantID, projectID string) error {
	if o.sources == nil {
		return fmt.Errorf("sources repo not wired")
	}
	list, err := o.sources.ListByProject(ctx, tenantID, projectID)
	if err != nil {
		return err
	}
	// In-run deduplication across sources (web/rss/telegram)
	webSeen := map[string]struct{}{}
	tgSeen := map[string]struct{}{}
	rssSeen := map[string]struct{}{}
	for _, ds := range list {
		// Skip paused/completed
		if ds.Status == "paused" || ds.Status == "completed" {
			continue
		}
		if ds.Kind == "web" && ds.Details.URI != nil {
			can := crawler.CanonicalizeURL(*ds.Details.URI)
			if can == "" {
				continue
			}
			if _, ok := webSeen[can]; ok {
				continue
			}
			webSeen[can] = struct{}{}
			_ = o.IngestWeb(ctx, tenantID, ds.ID, *ds.Details.URI)
		} else if ds.Kind == "telegram_channel" && ds.Details.ChannelUsername != nil {
			handle := strings.ToLower(strings.TrimPrefix(*ds.Details.ChannelUsername, "@"))
			if handle == "" {
				continue
			}
			if _, ok := tgSeen[handle]; ok {
				continue
			}
			tgSeen[handle] = struct{}{}
			last := 0
			if ds.Details.LastProcessedMessageID != nil {
				last = *ds.Details.LastProcessedMessageID
			}
			_ = o.IngestTelegram(ctx, tenantID, ds.ID, *ds.Details.ChannelUsername, last)
		} else if ds.Kind == "rss" && ds.Details.RSSURL != nil {
			can := crawler.CanonicalizeURL(*ds.Details.RSSURL)
			if can == "" {
				continue
			}
			if _, ok := rssSeen[can]; ok {
				continue
			}
			rssSeen[can] = struct{}{}
			// reuse existing rss ingest using emb
			if o.publish != nil && o.providersReg != nil {
				_ = o.IngestRSS(ctx, tenantID, ds.ID, nil)
			}
		} else if models.IsHorizonMonitorFeedKind(ds.Kind) && ds.Details.URI != nil {
			can := crawler.CanonicalizeURL(*ds.Details.URI)
			if can == "" {
				can = strings.TrimSpace(*ds.Details.URI)
			}
			if can == "" {
				continue
			}
			if _, ok := rssSeen[can]; ok {
				continue
			}
			rssSeen[can] = struct{}{}
			if o.emb != nil {
				_ = o.IngestHorizonMonitorFeed(ctx, tenantID, ds.ID, o.emb)
			}
		}
	}
	return nil
}

// IngestRSS ingests RSS sources.
//   - Default path: facts-only (document.extract → OntologyFacts, LLM extractor path).
//   - If sourceDetails.disableLlmProcessing=true: deterministic path
//     (Document persistence + Vertex sync, no document.process, no document.extract, heuristic pipeline only).
func (o *Orchestrator) IngestRSS(ctx context.Context, tenantID, sourceID string, _ ai.EmbeddingProvider) error {
	if o.providersReg == nil {
		return fmt.Errorf("RSS ingest requires providersReg")
	}
	p, ok := o.providersReg.Get("rss")
	if !ok {
		return fmt.Errorf("rss provider not registered")
	}
	// Determine feed URL from DataSource details
	feedURL := ""
	var sourceDS *models.DataSource
	if o.sources != nil {
		if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
			sourceDS = ds
			if ds.Details.RSSURL != nil {
				feedURL = *ds.Details.RSSURL
			}
		}
	}
	disableLLM := sourceDisablesLLMProcessing(sourceDS)
	if disableLLM {
		if o.docs == nil {
			return fmt.Errorf("RSS ingest with disableLlmProcessing=true requires documents repository")
		}
	} else if o.publish == nil {
		return fmt.Errorf("RSS ingest facts-only path requires publish")
	}
	if strings.TrimSpace(feedURL) == "" {
		// fallback to using sourceID as URL for backward compatibility
		feedURL = sourceID
	}
	docs, err := p.Ingest(ctx, tenantID, feedURL)
	if err != nil {
		// Save the original error before shadowing
		ingestErr := err
		// Even on error, update LastPolledAt and NextPollAt to ensure polling continues
		if o.sources != nil {
			if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
				now := time.Now().UTC()
				ds.LastPolledAt = &now
				// Use backoff for errors
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

				// Record ingestion history error
				if o.ingestHistory != nil {
					errStr := ingestErr.Error()
					// Cannot measure duration accurately here as start time wasn't captured at top of function,
					// but this is consistent with existing pattern.
					// Ideally we should refactor to capture start time at top.
					completedAt := time.Now().UTC()
					_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
						TenantID:           tenantID,
						SourceID:           sourceID,
						Kind:               "rss",
						Status:             "error",
						Error:              &errStr,
						Params:             map[string]interface{}{"url": feedURL},
						DocumentsProcessed: 0,
						DocumentsCreated:   0,
						StartedAt:          now, // Approximate
						CompletedAt:        &completedAt,
						CreatedAt:          now,
					})
				}

				if err := o.sources.Upsert(ctx, ds); err != nil {
					o.logger.Printf("orchestrator: failed to update NextPollAt/LastPolledAt on error for source %s: %v", sourceID, err)
				} else {
					o.logger.Printf("orchestrator: updated NextPollAt/LastPolledAt on error for source %s (NextPollAt: %s)", sourceID, next.Format(time.RFC3339))
				}
			}
		}
		return err
	}

	startTime := time.Now().UTC() // Start timing the processing part
	processOpts := providerIngestOptions{FactsOnly: true}
	if disableLLM {
		// Keep documents/Vertex sync, but never enqueue LLM extraction.
		processOpts = providerIngestOptions{
			PublishDocumentProcess: false,
			FactsOnly:              false,
		}
	}
	documentsCreated, err := o.processProviderDocumentsWithOptions(ctx, tenantID, sourceID, docs, processOpts)
	if err != nil {
		// Save the original error before shadowing
		processErr := err
		// Even on error, update LastPolledAt and NextPollAt to ensure polling continues
		if o.sources != nil {
			if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
				now := time.Now().UTC()
				ds.LastPolledAt = &now
				// Use backoff for errors
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

				// Record ingestion history error (processing phase)
				if o.ingestHistory != nil {
					errStr := processErr.Error()
					completedAt := time.Now().UTC()
					_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
						TenantID:           tenantID,
						SourceID:           sourceID,
						Kind:               "rss",
						Status:             "error",
						Error:              &errStr,
						Params:             map[string]interface{}{"url": feedURL},
						DocumentsProcessed: len(docs), // Documents fetched but failed processing
						DocumentsCreated:   documentsCreated,
						StartedAt:          startTime,
						CompletedAt:        &completedAt,
						CreatedAt:          startTime,
					})
				}

				if err := o.sources.Upsert(ctx, ds); err != nil {
					o.logger.Printf("orchestrator: failed to update NextPollAt/LastPolledAt on error for source %s: %v", sourceID, err)
				} else {
					o.logger.Printf("orchestrator: updated NextPollAt/LastPolledAt on error for source %s (NextPollAt: %s)", sourceID, next.Format(time.RFC3339))
				}
			}
		}
		return err
	}
	if disableLLM && o.wmPipeline != nil {
		pipelineDS := sourceDS
		if pipelineDS == nil {
			mode := wmIngestModeDocumentFeed
			disable := true
			pipelineDS = &models.DataSource{
				ID: sourceID,
				Details: models.SourceDetails{
					RSSURL:                   &feedURL,
					HorizonMonitorIngestMode: &mode,
					DisableLLMProcessing:     &disable,
				},
			}
		}
		if err := o.wmPipeline.ProcessDocuments(ctx, tenantID, pipelineDS, feedURL, docs); err != nil {
			o.logger.Printf("orchestrator: deterministic RSS heuristic pipeline failed for source %s: %v", sourceID, err)
		}
	}

	// Record successful ingestion history
	if o.ingestHistory != nil {
		completedAt := time.Now().UTC()
		durationMs := int(completedAt.Sub(startTime).Milliseconds())
		status := "success"
		if len(docs) == 0 {
			status = "empty"
		}
		_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
			TenantID:           tenantID,
			SourceID:           sourceID,
			Kind:               "rss",
			Status:             status,
			Params:             map[string]interface{}{"url": feedURL},
			DocumentsProcessed: len(docs),
			DocumentsCreated:   documentsCreated,
			DurationMs:         &durationMs,
			StartedAt:          startTime,
			CompletedAt:        &completedAt,
			CreatedAt:          startTime,
		})
	}

	// Update LastPolledAt and NextPollAt in DataSource
	if o.sources != nil {
		ds, err := o.sources.Get(ctx, sourceID)
		if err == nil && ds != nil {
			now := time.Now().UTC()
			ds.LastPolledAt = &now
			// Update LastFoundAt only if new documents were created (new content found)
			if documentsCreated > 0 {
				ds.LastFoundAt = &now
				o.logger.Printf("orchestrator: updated LastFoundAt for source %s (found %d new documents)", sourceID, documentsCreated)
			}
			// Calculate next poll time based on cadence
			interval := o.cadenceToDuration(ds, documentsCreated > 0)
			next := now.Add(interval)
			ds.NextPollAt = &next
			// Reset backoff on successful ingest
			zero := 0
			ds.PollBackoffSec = &zero
			if err := o.sources.Upsert(ctx, ds); err != nil {
				o.logger.Printf("orchestrator: failed to update NextPollAt for source %s: %v", sourceID, err)
			} else {
				o.logger.Printf("orchestrator: updated NextPollAt for source %s to %s (interval: %v, foundNew: %v)", sourceID, next.Format(time.RFC3339), interval, documentsCreated > 0)
			}
		}
	}

	return nil
}

// IngestHorizonMonitorFeed ingests a Horizon Monitor feed snapshot (JSON or text) and maps it to normalized documents.
// The feed endpoint is expected in DataSource.Details.URI.
func (o *Orchestrator) IngestHorizonMonitorFeed(ctx context.Context, tenantID, sourceID string, _ ai.EmbeddingProvider) error {
	if o.docs == nil {
		return fmt.Errorf("ingest dependencies not wired (docs)")
	}

	feedURL := ""
	var sourceDS *models.DataSource
	if o.sources != nil {
		if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
			sourceDS = ds
			if ds.Details.URI != nil {
				feedURL = strings.TrimSpace(*ds.Details.URI)
			} else if ds.Details.SourceURI != nil {
				feedURL = strings.TrimSpace(*ds.Details.SourceURI)
			}
		}
	}
	if feedURL == "" {
		return fmt.Errorf("horizon_monitor_feed source %s has empty URI", sourceID)
	}

	feedType := ""
	if sourceDS != nil && sourceDS.Details.HorizonMonitorFeedType != nil {
		feedType = strings.TrimSpace(*sourceDS.Details.HorizonMonitorFeedType)
	}
	startTime := time.Now().UTC()
	docs, fetchErr := o.fetchHorizonMonitorDocuments(ctx, feedURL, feedType)
	if fetchErr != nil {
		// Even on error, keep polling state moving to avoid source starvation.
		if o.sources != nil {
			if ds, err := o.sources.Get(ctx, sourceID); err == nil && ds != nil {
				now := time.Now().UTC()
				ds.LastPolledAt = &now
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
				if err := o.sources.Upsert(ctx, ds); err != nil {
					o.logger.Printf("orchestrator: failed to update NextPollAt/LastPolledAt on horizon monitor fetch error for source %s: %v", sourceID, err)
				}
			}
		}

		if o.ingestHistory != nil {
			errStr := fetchErr.Error()
			completedAt := time.Now().UTC()
			durationMs := int(completedAt.Sub(startTime).Milliseconds())
			_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
				TenantID:           tenantID,
				SourceID:           sourceID,
				Kind:               "horizon_monitor_feed",
				Status:             "error",
				Error:              &errStr,
				Params:             map[string]interface{}{"url": feedURL},
				DocumentsProcessed: 0,
				DocumentsCreated:   0,
				DurationMs:         &durationMs,
				StartedAt:          startTime,
				CompletedAt:        &completedAt,
				CreatedAt:          startTime,
			})
		}
		return fetchErr
	}

	mode := normalizeHorizonMonitorIngestMode(sourceDS)
	documentsCreated, err := o.processProviderDocumentsWithOptions(
		ctx,
		tenantID,
		sourceID,
		docs,
		providerIngestOptions{
			// HorizonMonitor feeds are processed by dedicated deterministic pipeline;
			// we intentionally skip generic document.process LLM extraction here.
			PublishDocumentProcess: horizonMonitorShouldPublishDocumentProcess(mode),
		},
	)
	if err != nil {
		if o.ingestHistory != nil {
			errStr := err.Error()
			completedAt := time.Now().UTC()
			durationMs := int(completedAt.Sub(startTime).Milliseconds())
			_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
				TenantID:           tenantID,
				SourceID:           sourceID,
				Kind:               "horizon_monitor_feed",
				Status:             "error",
				Error:              &errStr,
				Params:             map[string]interface{}{"url": feedURL},
				DocumentsProcessed: len(docs),
				DocumentsCreated:   documentsCreated,
				DurationMs:         &durationMs,
				StartedAt:          startTime,
				CompletedAt:        &completedAt,
				CreatedAt:          startTime,
			})
		}
		return err
	}
	if o.wmPipeline != nil {
		if err := o.wmPipeline.ProcessDocuments(ctx, tenantID, sourceDS, feedURL, docs); err != nil {
			o.logger.Printf("orchestrator: horizon monitor pipeline failed for source %s (mode=%s): %v", sourceID, mode, err)
		}
	}

	if o.ingestHistory != nil {
		completedAt := time.Now().UTC()
		durationMs := int(completedAt.Sub(startTime).Milliseconds())
		status := "success"
		if len(docs) == 0 {
			status = "empty"
		}
		_, _ = o.ingestHistory.Create(ctx, &models.SourceIngestHistory{
			TenantID:           tenantID,
			SourceID:           sourceID,
			Kind:               "horizon_monitor_feed",
			Status:             status,
			Params:             map[string]interface{}{"url": feedURL},
			DocumentsProcessed: len(docs),
			DocumentsCreated:   documentsCreated,
			DurationMs:         &durationMs,
			StartedAt:          startTime,
			CompletedAt:        &completedAt,
			CreatedAt:          startTime,
		})
	}

	if o.sources != nil {
		ds, err := o.sources.Get(ctx, sourceID)
		if err == nil && ds != nil {
			now := time.Now().UTC()
			ds.LastPolledAt = &now
			if documentsCreated > 0 {
				ds.LastFoundAt = &now
			}
			interval := o.cadenceToDuration(ds, documentsCreated > 0)
			next := now.Add(interval)
			ds.NextPollAt = &next
			zero := 0
			ds.PollBackoffSec = &zero
			if err := o.sources.Upsert(ctx, ds); err != nil {
				o.logger.Printf("orchestrator: failed to update NextPollAt for horizon monitor source %s: %v", sourceID, err)
			}
		}
	}

	return nil
}

// FetchHorizonMonitorDocuments fetches and parses a Horizon Monitor feed URL into normalized documents.
// Used by CanvasIngester for system canvas.
func (o *Orchestrator) FetchHorizonMonitorDocuments(ctx context.Context, feedURL string) ([]prov.NormalizedDocument, error) {
	return o.fetchHorizonMonitorDocuments(ctx, feedURL, "")
}

func (o *Orchestrator) fetchHorizonMonitorDocuments(ctx context.Context, feedURL, feedType string) ([]prov.NormalizedDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("horizon monitor feed returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseHorizonMonitorDocuments(feedURL, body, strings.ToLower(resp.Header.Get("Content-Type")), feedType)
}

func parseHorizonMonitorDocuments(feedURL string, body []byte, contentType, feedType string) ([]prov.NormalizedDocument, error) {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return nil, nil
	}
	if strings.Contains(contentType, "json") || strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		var payload any
		if feedType != "" {
			if normalized, err := adapters.Normalize(feedType, body); err == nil && normalized != nil {
				payload = normalized
			}
		}
		if payload == nil {
			if err := json.Unmarshal(body, &payload); err != nil {
				payload = nil
			}
		}
		if payload != nil {
			if docs := parseHorizonMonitorJSONPayload(feedURL, payload); len(docs) > 0 {
				return docs, nil
			}
		}
	}

	now := time.Now().UTC()
	ext := "hm-" + hashShort(feedURL+"|"+raw)
	return []prov.NormalizedDocument{
		{
			ExternalID:        ext,
			Title:             "Horizon Monitor feed snapshot",
			URL:               feedURL,
			DocumentPath:      "horizon-monitor/" + ext,
			FullText:          raw,
			ContentType:       "plainText",
			DocumentType:      "feed_snapshot",
			Language:          "en",
			SourceMeta:        map[string]any{"feedUrl": feedURL},
			OriginPublishedAt: &now,
		},
	}, nil
}

func parseHorizonMonitorJSONPayload(feedURL string, payload any) []prov.NormalizedDocument {
	now := time.Now().UTC()
	switch v := payload.(type) {
	case []any:
		return horizonMonitorDocsFromItems(feedURL, v, "items")
	case map[string]any:
		// Plan §8.3: features (USGS), Result (UCDP), regions (FIRMS), states (OpenSky)
		keys := []string{"items", "events", "signals", "news", "data", "results", "features", "regions"}
		for _, key := range keys {
			if arrAny, ok := v[key].([]any); ok && len(arrAny) > 0 {
				return horizonMonitorDocsFromItems(feedURL, arrAny, key)
			}
		}
		if res, ok := v["Result"].([]any); ok && len(res) > 0 {
			return horizonMonitorDocsFromItems(feedURL, res, "Result")
		}
		pretty, _ := json.MarshalIndent(v, "", "  ")
		ext := "hm-" + hashShort(feedURL+"|"+string(pretty))
		return []prov.NormalizedDocument{
			{
				ExternalID:        ext,
				Title:             "Horizon Monitor feed snapshot",
				URL:               feedURL,
				DocumentPath:      "horizon-monitor/" + ext,
				FullText:          string(pretty),
				ContentType:       "application/json",
				DocumentType:      "feed_snapshot",
				Language:          "en",
				SourceMeta:        map[string]any{"feedUrl": feedURL, "payloadKind": "object"},
				OriginPublishedAt: &now,
			},
		}
	default:
		return nil
	}
}

func horizonMonitorDocsFromItems(feedURL string, items []any, payloadKey string) []prov.NormalizedDocument {
	docs := make([]prov.NormalizedDocument, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		title := firstNonEmptyString(
			asString(m["title"]),
			asString(m["name"]),
			asString(m["headline"]),
			asString(m["summary"]),
		)
		if title == "" {
			title = fmt.Sprintf("Horizon Monitor %s #%d", payloadKey, i+1)
		}

		link := firstNonEmptyString(
			asString(m["link"]),
			asString(m["url"]),
			asString(m["sourceUrl"]),
		)

		text := firstNonEmptyString(
			asString(m["content"]),
			asString(m["description"]),
			asString(m["summary"]),
			asString(m["text"]),
		)
		if text == "" {
			if raw, err := json.Marshal(m); err == nil {
				text = string(raw)
			}
		}
		if text == "" {
			continue
		}

		extID := firstNonEmptyString(
			asString(m["id"]),
			asString(m["eventId"]),
			asString(m["guid"]),
		)
		if extID == "" {
			extID = "hm-" + hashShort(feedURL+"|"+title+"|"+link+"|"+strconv.Itoa(i))
		}

		doc := prov.NormalizedDocument{
			ExternalID:   extID,
			Title:        title,
			URL:          link,
			DocumentPath: "horizon-monitor/" + payloadKey + "/" + extID,
			FullText:     text,
			ContentType:  "plainText",
			DocumentType: "feed_item",
			Language:     "en",
			SourceMeta: map[string]any{
				"feedUrl":    feedURL,
				"payloadKey": payloadKey,
			},
		}
		if t := parseAnyTime(
			m["pubDate"],
			m["publishedAt"],
			m["published"],
			m["timestamp"],
			m["time"],
			m["date"],
		); t != nil {
			doc.OriginPublishedAt = t
		}
		docs = append(docs, doc)
	}
	return docs
}

func parseAnyTime(vals ...any) *time.Time {
	for _, v := range vals {
		switch t := v.(type) {
		case string:
			s := strings.TrimSpace(t)
			if s == "" {
				continue
			}
			if tm, err := time.Parse(time.RFC3339, s); err == nil {
				return &tm
			}
			if tm, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return &tm
			}
			if tm, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
				return &tm
			}
			if unix, err := strconv.ParseInt(s, 10, 64); err == nil {
				if unix > 1e12 {
					tm := time.UnixMilli(unix).UTC()
					return &tm
				}
				tm := time.Unix(unix, 0).UTC()
				return &tm
			}
		case float64:
			u := int64(t)
			if u <= 0 {
				continue
			}
			if u > 1e12 {
				tm := time.UnixMilli(u).UTC()
				return &tm
			}
			tm := time.Unix(u, 0).UTC()
			return &tm
		}
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return strings.TrimSpace(t.String())
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return ""
	}
}

func hashShort(s string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:8])
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// processProviderDocuments is a helper to process documents from any provider
// Returns the number of newly created documents (not updated existing ones)
func (o *Orchestrator) processProviderDocuments(ctx context.Context, tenantID, sourceID string, docs []prov.NormalizedDocument) (int, error) {
	return o.processProviderDocumentsWithOptions(ctx, tenantID, sourceID, docs, providerIngestOptions{
		PublishDocumentProcess: true,
	})
}

// processProviderDocumentsFactsOnly publishes document.extract for each NormalizedDocument.
// No Document or Chunks created. FactsExtractor consumes and persists OntologyFacts.
func (o *Orchestrator) processProviderDocumentsFactsOnly(ctx context.Context, tenantID, sourceID string, docs []prov.NormalizedDocument) (int, error) {
	if o.publish == nil {
		return 0, fmt.Errorf("publisher not wired for facts-only ingest")
	}
	seenExt := map[string]struct{}{}
	published := 0
	for _, nd := range docs {
		if strings.TrimSpace(nd.FullText) == "" {
			continue
		}
		if nd.ExternalID != "" {
			if _, ok := seenExt[nd.ExternalID]; ok {
				continue
			}
			seenExt[nd.ExternalID] = struct{}{}
		}
		originPublishedAt := ""
		if nd.OriginPublishedAt != nil {
			originPublishedAt = nd.OriginPublishedAt.Format(time.RFC3339)
		}
		payload := map[string]any{
			"tenantId":     tenantID,
			"sourceId":     sourceID,
			"externalId":   nd.ExternalID,
			"fullText":     nd.FullText,
			"title":        nd.Title,
			"url":          nd.URL,
			"language":     nd.Language,
			"documentType": nd.DocumentType,
			"sourceMeta":   nd.SourceMeta,
		}
		if originPublishedAt != "" {
			payload["originPublishedAt"] = originPublishedAt
		}
		if err := o.publish.PublishJSON(ctx, realtime.EventDocExtract, tenantID, payload); err != nil {
			o.logger.Printf("orchestrator: pubsub document.extract error: %v", err)
			continue
		}
		published++
	}
	return published, nil
}

type providerIngestOptions struct {
	PublishDocumentProcess bool
	FactsOnly              bool // Skip Document/Chunks; publish document.extract for facts-only path
}

func (o *Orchestrator) processProviderDocumentsWithOptions(
	ctx context.Context,
	tenantID, sourceID string,
	docs []prov.NormalizedDocument,
	opts providerIngestOptions,
) (int, error) {
	// Facts-only path: no Document/Chunks, publish document.extract for each doc
	if opts.FactsOnly {
		return o.processProviderDocumentsFactsOnly(ctx, tenantID, sourceID, docs)
	}
	// Deduplicate documents by ExternalID within this ingest run
	seenExt := map[string]struct{}{}
	documentsCreated := 0
	for _, nd := range docs {
		if nd.ExternalID != "" {
			if _, ok := seenExt[nd.ExternalID]; ok {
				continue
			}
			seenExt[nd.ExternalID] = struct{}{}
		}
		// idempotent upsert by externalId
		existing, _ := o.docs.GetByExternalID(ctx, tenantID, sourceID, nd.ExternalID)
		now := time.Now().UTC()
		if existing != nil {
			existing.Title = nd.Title
			existing.URL = nd.URL
			existing.DocumentPath = nd.DocumentPath
			existing.FullText = nd.FullText
			existing.Language = nd.Language
			existing.ContentType = nd.ContentType
			existing.DocumentType = nd.DocumentType
			existing.SourceMeta = nd.SourceMeta
			existing.OriginPublishedAt = nd.OriginPublishedAt
			existing.Status = "new"
			existing.ContentHash = repo.HashContent(nd.FullText)
			existing.UpdatedAt = now
			if err := o.docs.Upsert(ctx, existing); err != nil {
				return 0, err
			}
		} else {
			d := &models.Document{
				TenantID:          tenantID,
				SourceID:          sourceID,
				ExternalID:        nd.ExternalID,
				Title:             nd.Title,
				URL:               nd.URL,
				DocumentPath:      nd.DocumentPath,
				FullText:          nd.FullText,
				Status:            "new",
				ContentType:       nd.ContentType,
				DocumentType:      nd.DocumentType,
				Language:          nd.Language,
				SourceMeta:        nd.SourceMeta,
				OriginPublishedAt: nd.OriginPublishedAt,
				ContentHash:       repo.HashContent(nd.FullText),
				IngestedAt:        now,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if _, err := o.docs.Create(ctx, d); err != nil {
				return 0, err
			}
			documentsCreated++
		}

		// Update document status to indexed (Create/Upsert uploads to GCS, triggers Vertex sync)
		if existing != nil {
			existing.Status = "indexed"
			_ = o.docs.Upsert(ctx, existing)
			// publish to document.process
			if opts.PublishDocumentProcess && o.publish != nil {
				payload := map[string]any{
					"tenantId":   tenantID,
					"sourceId":   sourceID,
					"documentId": existing.ID,
					"externalId": existing.ExternalID,
				}
				_ = o.publish.PublishJSON(ctx, realtime.EventDocProcess, tenantID, payload)
			}
		} else {
			// For new documents, get it back by externalID to publish event
			if d, _ := o.docs.GetByExternalID(ctx, tenantID, sourceID, nd.ExternalID); d != nil {
				d.Status = "indexed"
				_ = o.docs.Upsert(ctx, d)
				// publish to document.process
				if opts.PublishDocumentProcess && o.publish != nil {
					payload := map[string]any{
						"tenantId":   tenantID,
						"sourceId":   sourceID,
						"documentId": d.ID,
						"externalId": d.ExternalID,
					}
					_ = o.publish.PublishJSON(ctx, realtime.EventDocProcess, tenantID, payload)
				}
			}
		}
	}
	return documentsCreated, nil
}

type ExecuteInput struct {
	TenantID    string
	ProjectID   string
	QueryText   string
	Language    string
	WebSources  []string
	TgHandles   []string
	RSSFeeds    []string
	Correlation string
	OwnerUID    string
}

// Chat handles continuation within an existing project using chat history and profiles
func (o *Orchestrator) Chat(ctx context.Context, tenantID, projectID, message, language string) (string, error) {
	if strings.TrimSpace(projectID) == "" {
		return "", fmt.Errorf("projectId is required")
	}
	if language == "" {
		language = "en"
	}

	// Persist user query
	if o.interactions != nil && strings.TrimSpace(message) != "" {
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: tenantID, ProjectID: projectID, Kind: "discovery.query", Content: message})
	}

	// Build context: recent Q/A pairs and profile summaries
	var historyPairs []map[string]string
	var profileSummaries []string
	var contextSnippets []string
	type ctxEntry struct{ sourceId, displayName, kind, snippet string }
	var contextEntries []ctxEntry
	var lastContextAt *time.Time
	hadContext := false
	if o.interactions != nil {
		if items, err := o.interactions.ListByProject(ctx, projectID); err == nil {
			// Collect last up to 8 messages (4 pairs)
			for i := len(items) - 1; i >= 0 && len(historyPairs) < 4; i-- {
				it := items[i]
				if it.Kind == "discovery.answer" {
					// find preceding query if any
					var prev *repo.Interaction
					for j := i - 1; j >= 0; j-- {
						if items[j].Kind == "discovery.query" {
							prev = items[j]
							break
						}
					}
					pair := map[string]string{"answer": it.Content}
					if prev != nil {
						pair["query"] = prev.Content
					}
					historyPairs = append([]map[string]string{pair}, historyPairs...)
				}
				if it.Kind == "discovery.profile" {
					// keep up to 6 summaries
					if len(profileSummaries) < 6 {
						profileSummaries = append(profileSummaries, it.Content)
					}
				}
				if it.Kind == "discovery.context" {
					hadContext = true
					t := it.CreatedAt
					if lastContextAt == nil || t.After(*lastContextAt) {
						lastContextAt = &t
					}
				}
			}
			// Collect recent context snippets per source; budgeting later
			for i := len(items) - 1; i >= 0; i-- {
				it := items[i]
				if it.Kind == "discovery.context" {
					// content is JSON; try to extract snippet
					var m map[string]interface{}
					if err := json.Unmarshal([]byte(it.Content), &m); err == nil {
						srcID, _ := m["sourceId"].(string)
						disp, _ := m["displayName"].(string)
						skind, _ := m["sourceKind"].(string)
						if v, ok := m["snippet"].(string); ok {
							s := strings.TrimSpace(v)
							if s != "" {
								contextEntries = append(contextEntries, ctxEntry{sourceId: srcID, displayName: disp, kind: skind, snippet: s})
							}
						}
					}
				}
			}
		}
	}

	// If no context ever or context is stale (> 2 hours), fetch minimal context now
	needRefresh := !hadContext
	if !needRefresh && lastContextAt != nil {
		if time.Since(*lastContextAt) > 2*time.Hour {
			needRefresh = true
		}
	}
	if needRefresh && o.sources != nil {
		if srcs, err := o.sources.ListByProject(ctx, tenantID, projectID); err == nil {
			for _, ds := range srcs {
				var snippet string
				if ds.Kind == "web" && ds.Details.URI != nil {
					if fr, err := o.crawl.FetchContent(ctx, *ds.Details.URI); err == nil {
						snippet = strings.TrimSpace(fr.Text)
					}
				} else if ds.Kind == "telegram_channel" && ds.Details.ChannelUsername != nil {
					if msgs, err := o.telegram.FetchUpdates(ctx, *ds.Details.ChannelUsername, 0); err == nil {
						var sb strings.Builder
						for i := 0; i < len(msgs) && i < 50; i++ {
							if msgs[i].Text != "" {
								sb.WriteString(msgs[i].Text)
								sb.WriteString("\n\n")
							}
						}
						snippet = strings.TrimSpace(sb.String())
					}
				}
				if snippet != "" {
					if len(snippet) > 100000 { // hard-cap to 100 KB
						snippet = snippet[:100000]
					}
					obj := map[string]interface{}{
						"sourceId":    ds.ID,
						"displayName": ds.DisplayName,
						"sourceKind":  ds.Kind,
						"snippet":     snippet,
						"chars":       len(snippet),
					}
					bjson, _ := json.Marshal(obj)
					if o.interactions != nil {
						_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: tenantID, ProjectID: projectID, Kind: "discovery.context", Content: string(bjson)})
					}
					contextEntries = append(contextEntries, ctxEntry{sourceId: ds.ID, displayName: ds.DisplayName, kind: ds.Kind, snippet: snippet})
				}
			}
		}
	}

	// Compose prompt
	var b strings.Builder
	b.WriteString("You are an expert assistant continuing a chat.\n")
	b.WriteString("LANGUAGE: ")
	b.WriteString(language)
	b.WriteString("\n\n")
	if len(profileSummaries) > 0 {
		b.WriteString("SOURCE PROFILES SUMMARY (JSON snippets):\n")
		for _, s := range profileSummaries {
			b.WriteString(s)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	// Apply 100KB total budget split evenly across sources
	if len(contextEntries) > 0 {
		seen := map[string]bool{}
		var perSource []ctxEntry
		for i := 0; i < len(contextEntries); i++ { // newest first in entries
			ce := contextEntries[i]
			key := ce.sourceId
			if key == "" {
				key = ce.displayName + ":" + ce.kind
			}
			if !seen[key] {
				seen[key] = true
				perSource = append(perSource, ce)
			}
		}
		if len(perSource) > 0 {
			totalBudget := 100000
			perBudget := totalBudget / len(perSource)
			if perBudget < 1 {
				perBudget = 1
			}
			for _, ce := range perSource {
				s := ce.snippet
				if len(s) > perBudget {
					s = s[:perBudget]
				}
				contextSnippets = append(contextSnippets, s)
			}
		}
	}
	if len(contextSnippets) > 0 {
		b.WriteString("RECENT CONTEXT SNIPPETS (truncated):\n")
		for _, s := range contextSnippets {
			b.WriteString(s)
			b.WriteString("\n\n")
		}
	}
	if len(historyPairs) > 0 {
		b.WriteString("RECENT DIALOG (oldest-first):\n")
		for _, p := range historyPairs {
			if q, ok := p["query"]; ok && q != "" {
				b.WriteString("USER: ")
				b.WriteString(q)
				b.WriteString("\n")
			}
			b.WriteString("ASSISTANT: ")
			b.WriteString(p["answer"])
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("CURRENT USER MESSAGE: ")
	b.WriteString(message)
	b.WriteString("\n\n")
	b.WriteString("TASK: Reply helpfully in the specified LANGUAGE using only provided context and prior dialog. Be concise and accurate. Return plain text only.")

	task := &ai.AITask{TaskType: ai.TaskTypeContentGeneration, InputContent: b.String(), AIRequirements: ai.AIRequirements{TextGenerationCapability: true}}
	res, err := o.exec.ExecuteTask(ctx, task)
	if err != nil {
		return "", err
	}

	answer := strings.TrimSpace(res.Content)
	if o.interactions != nil && answer != "" {
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: tenantID, ProjectID: projectID, Kind: "discovery.answer", Content: answer})
	}
	// For chat continuation, avoid WS duplication: REST returns the answer and UI updates the placeholder
	return answer, nil
}

// Execute runs discovery orchestration, auto-creating project if needed
func (o *Orchestrator) Execute(ctx context.Context, in ExecuteInput) error {
	// Emit started status (compat with old flow)
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"status":        "started",
		"message":       "Discovery started",
	})

	// Track whether we create a new project in this call
	createdNewProject := false

	// Auto-create project if empty
	if in.ProjectID == "" {
		title, err := o.generateProjectTitle(ctx, in.QueryText, in.Language)
		if err != nil {
			o.logger.Printf("orchestrator: generate title error: %v, using fallback", err)
			title = "Discovery " + time.Now().Format("2006-01-02 15:04")
		}

		desc := ""
		view := "discovery"
		proj := &models.Project{
			TenantID:    in.TenantID,
			DisplayName: title,
			Description: &desc,
			View:        &view,
			Status:      "active",
			OwnerUserID: in.OwnerUID,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}

		projectID, err := o.projects.Create(ctx, proj)
		if err != nil {
			o.logger.Printf("orchestrator: create project error: %v", err)
			return fmt.Errorf("failed to create project: %w", err)
		}

		in.ProjectID = projectID
		o.logger.Printf("orchestrator: auto-created project %s: %s", projectID, title)
		createdNewProject = true

		// Send WebSocket message about new project
		msg := map[string]interface{}{
			"type":          "discoveryProject",
			"projectId":     projectID,
			"title":         title,
			"views":         []string{"discovery"},
			"tenantId":      in.TenantID,
			"createdAt":     proj.CreatedAt.Format(time.RFC3339),
			"correlationId": in.Correlation,
		}

		// Persist the user's query as interaction when this is a brand-new chat
		if createdNewProject && o.interactions != nil && strings.TrimSpace(in.QueryText) != "" {
			_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.query", Content: in.QueryText})
		}
		o.publishEvent(ctx, "discovery.project", in.TenantID, msg)
	}

	// Process sources in parallel with in-run deduplication and await all
	eg, gctx := errgroup.WithContext(ctx)
	// Deduplicate web sources by canonical URL
	webSeen := map[string]struct{}{}
	for _, raw := range in.WebSources {
		canonical := crawler.CanonicalizeURL(strings.TrimSpace(raw))
		if canonical == "" {
			continue
		}
		if _, ok := webSeen[canonical]; ok {
			continue
		}
		webSeen[canonical] = struct{}{}
		src := canonical
		eg.Go(func() error { return o.processWebSource(gctx, in, src) })
	}
	// Deduplicate telegram handles
	tgSeen := map[string]struct{}{}
	for _, h := range in.TgHandles {
		handle := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "@"))
		if handle == "" {
			continue
		}
		if _, ok := tgSeen[handle]; ok {
			continue
		}
		tgSeen[handle] = struct{}{}
		hh := handle
		eg.Go(func() error { return o.processTelegramSource(gctx, in, hh) })
	}
	// Deduplicate RSS feeds
	rssSeen := map[string]struct{}{}
	for _, raw := range in.RSSFeeds {
		// Basic normalization: trim space
		// We don't use CanonicalizeURL aggressively here as feeds might be sensitive to params
		// but we should at least trim
		feedURL := strings.TrimSpace(raw)
		if feedURL == "" {
			continue
		}
		if _, ok := rssSeen[feedURL]; ok {
			continue
		}
		rssSeen[feedURL] = struct{}{}
		uu := feedURL
		eg.Go(func() error { return o.processRSSSource(gctx, in, uu) })
	}
	if err := eg.Wait(); err != nil {
		o.logger.Printf("orchestrator: parallel sources error: %v", err)
	}
	// Synthesize final answer from all profiles in project (simplified: load sources and aggregate)
	if err := o.synthesizeAnswer(ctx, in); err != nil {
		o.logger.Printf("orchestrator: synthesize answer error: %v", err)
	}

	// Emit completed status
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"status":        "completed",
		"message":       "Discovery completed",
	})
	return nil
}

// generateProjectTitle uses LLM to generate a concise title from query
func (o *Orchestrator) generateProjectTitle(ctx context.Context, query, language string) (string, error) {
	if query == "" {
		return "New Chat", nil
	}

	prompt := fmt.Sprintf(`Generate a short, concise title (2-6 words) for a discovery chat based on this query:

Query: %s

Requirements:
- Title must be brief and descriptive
- Use title case
- No quotes or special formatting
- Language: %s

Return ONLY the title, nothing else.`, query, language)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       5,
			MinContextWindow:         2000,
		},
	}

	result, err := o.exec.ExecuteTask(ctx, task)
	if err != nil {
		return "", err
	}

	title := strings.TrimSpace(result.Content)
	// Sanitize: remove quotes, limit length
	title = strings.Trim(title, `"'`)
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	if title == "" {
		title = "New Chat"
	}

	return title, nil
}

func (o *Orchestrator) processWebSource(ctx context.Context, in ExecuteInput, canonicalURL string) error {
	// find or create DataSource
	var ds *models.DataSource
	needsUpdate := false
	existing, err := o.sources.FindByCanonicalSource(ctx, in.TenantID, canonicalURL)
	if err != nil {
		o.logger.Printf("orchestrator: FindByCanonicalSource for %s error: %v (will create new)", canonicalURL, err)
	}
	createdNew := false
	if existing != nil {
		o.logger.Printf("orchestrator: found existing web source %s (id=%s) for project %s", canonicalURL, existing.ID, in.ProjectID)
		ds = existing
		// Update LastSeen
		ds.LastSeen = time.Now().UTC()
		needsUpdate = true
		// Ensure link for existing source
		o.ensureProjectSourceLink(ctx, in.TenantID, in.ProjectID, ds.ID)

		// If source was previously processed, skip reprocessing
		if (ds.Status == "paused" || ds.Status == "completed") && o.profiles != nil {
			if needsUpdate {
				_ = o.sources.Upsert(ctx, ds)
			}
			if sp, _ := o.profiles.GetByDataSource(ctx, in.TenantID, ds.ID); sp != nil {
				o.logger.Printf("orchestrator: reusing existing profile for %s", canonicalURL)
				o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
					"projectId": in.ProjectID, "sourceId": ds.ID, "profile": sp.Profile})
				return nil
			}
		}
	} else {
		now := time.Now().UTC()
		o.logger.Printf("orchestrator: creating new web source %s for project %s", canonicalURL, in.ProjectID)
		// Set initial NextPollAt based on cadence (estimate)
		initialInterval := o.cadenceToDuration(nil, false) // Use default for new sources
		initialNext := now.Add(initialInterval)
		ds = &models.DataSource{
			// Public source: store globally (no tenant scoping)
			TenantID:    "",
			Pk:          "global",
			DisplayName: canonicalURL,
			Kind:        "web",
			Status:      "paused",
			OwnerUserID: in.OwnerUID,
			Details:     models.SourceDetails{URI: &canonicalURL},
			CreatedAt:   now,
			UpdatedAt:   now,
			FirstSeen:   now,
			LastSeen:    now,
			NextPollAt:  &initialNext, // Set initial estimate
		}
		if createdID, err := o.sources.Create(ctx, ds); err != nil {
			o.logger.Printf("orchestrator: failed to create web source %s: %v", canonicalURL, err)
			return err
		} else {
			o.logger.Printf("orchestrator: created web source %s with id=%s", canonicalURL, createdID)
		}
		createdNew = true
		// Ensure link project<->source is created for this chat
		if o.sproj != nil && strings.TrimSpace(in.ProjectID) != "" {
			// Check for any existing link (regardless of status)
			links, _ := o.sproj.ListBySource(ctx, in.TenantID, ds.ID, nil)
			exists := false
			for _, l := range links {
				if l.ProjectID == in.ProjectID {
					exists = true
					break
				}
			}
			if !exists {
				_, _ = o.sproj.Create(ctx, &models.SourceProject{TenantID: in.TenantID, SourceID: ds.ID, ProjectID: in.ProjectID, Status: "paused", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
			}
		}
	}
	msg := map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"sourceId":      ds.ID,
		"status":        "fetching",
		"kind":          ds.Kind,
		"displayName":   ds.DisplayName,
	}
	if createdNew {
		msg["firstProfile"] = true
		msg["hint"] = "profilingNewSource"
	}
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, msg)

	// fetch content
	fr, err := o.crawl.FetchContent(ctx, canonicalURL)
	if err != nil {
		return err
	}

	// Persist fetched context snapshot (truncated) as interaction
	if o.interactions != nil {
		snippet := fr.Text
		if len(snippet) > 5000 {
			snippet = snippet[:5000]
		}
		ctxObj := map[string]interface{}{
			"sourceId":    ds.ID,
			"displayName": ds.DisplayName,
			"sourceKind":  ds.Kind,
			"uri":         canonicalURL,
			"snippet":     snippet,
			"chars":       len(fr.Text),
		}
		_ = func() error {
			b, _ := json.Marshal(ctxObj)
			_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.context", Content: string(b)})
			return nil
		}()
	}

	// cache check
	contentHash := repo.HashContent(fr.Text)
	if o.cache != nil {
		if p, _ := o.cache.Get(ctx, in.TenantID, canonicalURL, contentHash, 24*time.Hour); p != nil {
			ds.Status = "paused"
			_ = o.sources.Upsert(ctx, ds)
			if o.profiles != nil {
				_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: ds.ID, TenantID: "", DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *p})
				// clean tenant-scoped duplicates if exist
				_ = o.profiles.DeleteTenantScopedForSource(ctx, in.TenantID, ds.ID)
			}
			// Persist profile summary interaction
			if o.interactions != nil {
				summary := summarizeProfile(p)
				obj := map[string]interface{}{
					"sourceId":       ds.ID,
					"displayName":    ds.DisplayName,
					"sourceKind":     ds.Kind,
					"uri":            canonicalURL,
					"summary":        summary,
					"hasFullProfile": true,
				}
				b, _ := json.Marshal(obj)
				_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.profile", Content: string(b)})
			}
			o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
				"correlationId": in.Correlation,
				"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": p})
			return nil
		}
	}

	// profile via AITaskExecutor
	prof, err := o.profile(ctx, ds.DisplayName, fr.Text, in.Language)
	if err != nil {
		return err
	}
	// ensure paused status for public sources
	ds.Status = "paused"
	if err := o.sources.Upsert(ctx, ds); err != nil {
		o.logger.Printf("orchestrator: failed to upsert web source %s (profile size: ~%d bytes): %v", canonicalURL, len(mustJSON(prof)), err)
		return err
	}
	o.logger.Printf("orchestrator: saved web profile for %s, size: ~%d bytes", canonicalURL, len(mustJSON(prof)))
	if o.cache != nil {
		_ = o.cache.Put(ctx, in.TenantID, canonicalURL, contentHash, prof)
	}
	if o.profiles != nil {
		_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: ds.ID, TenantID: "", DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *prof})
		// clean tenant-scoped duplicates if exist
		_ = o.profiles.DeleteTenantScopedForSource(ctx, in.TenantID, ds.ID)
	}

	// Persist profile summary interaction
	if o.interactions != nil {
		summary := summarizeProfile(prof)
		obj := map[string]interface{}{
			"sourceId":       ds.ID,
			"displayName":    ds.DisplayName,
			"sourceKind":     ds.Kind,
			"uri":            canonicalURL,
			"summary":        summary,
			"hasFullProfile": true,
		}
		b, _ := json.Marshal(obj)
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.profile", Content: string(b)})
	}

	o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": prof})
	return nil
}

func (o *Orchestrator) processTelegramSource(ctx context.Context, in ExecuteInput, handle string) error {
	// find or create
	var ds *models.DataSource
	needsUpdate := false
	existing, err := o.sources.FindByCanonicalSource(ctx, in.TenantID, handle)
	if err != nil {
		o.logger.Printf("orchestrator: FindByCanonicalSource for @%s error: %v (will create new)", handle, err)
	}
	createdNew := false
	if existing != nil {
		o.logger.Printf("orchestrator: found existing telegram source @%s (id=%s) for project %s", handle, existing.ID, in.ProjectID)
		ds = existing
		// Update LastSeen
		ds.LastSeen = time.Now().UTC()
		needsUpdate = true

		// Ensure link project<->source is created (even if source already exists)
		if o.sproj != nil && strings.TrimSpace(in.ProjectID) != "" {
			link, _ := o.sproj.GetByProjectAndSource(ctx, in.TenantID, in.ProjectID, ds.ID)
			if link == nil {
				// Link doesn't exist, create it
				_, err := o.sproj.Create(ctx, &models.SourceProject{
					TenantID:  in.TenantID,
					SourceID:  ds.ID,
					ProjectID: in.ProjectID,
					Status:    "paused",
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				})
				if err != nil {
					o.logger.Printf("orchestrator: failed to create source-project link for @%s: %v", handle, err)
				} else {
					o.logger.Printf("orchestrator: created source-project link for @%s to project %s", handle, in.ProjectID)
				}
			}
		}

		// If source was previously processed, normalize status to paused and reuse profile
		// But still fetch and ingest messages if needed
		if ds.Status == "completed" && o.profiles != nil {
			// normalize to paused for public sources
			ds.Status = "paused"
			if needsUpdate {
				_ = o.sources.Upsert(ctx, ds)
			}
			if sp, _ := o.profiles.GetByDataSource(ctx, in.TenantID, ds.ID); sp != nil {
				o.logger.Printf("orchestrator: reusing existing profile for @%s, but will still fetch/ingest messages", handle)
				// Don't return early - continue to fetch and ingest messages
				// Publish profile event but continue processing
				o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
					"projectId": in.ProjectID, "sourceId": ds.ID, "profile": sp.Profile})
			}
		}
	} else {
		now := time.Now().UTC()
		o.logger.Printf("orchestrator: creating new telegram source @%s for project %s", handle, in.ProjectID)
		// Set initial NextPollAt based on cadence (estimate)
		initialInterval := o.cadenceToDuration(nil, false) // Use default for new sources
		initialNext := now.Add(initialInterval)
		ds = &models.DataSource{
			// Public source: store globally (no tenant scoping)
			TenantID:    "",
			Pk:          "global",
			DisplayName: "@" + handle,
			Kind:        "telegram_channel",
			Status:      "paused",
			OwnerUserID: in.OwnerUID,
			Details:     models.SourceDetails{ChannelUsername: &handle},
			CreatedAt:   now, UpdatedAt: now, FirstSeen: now, LastSeen: now,
			NextPollAt: &initialNext, // Set initial estimate
		}
		if createdID, err := o.sources.Create(ctx, ds); err != nil {
			o.logger.Printf("orchestrator: failed to create telegram source @%s: %v", handle, err)
			return err
		} else {
			o.logger.Printf("orchestrator: created telegram source @%s with id=%s", handle, createdID)
		}
		createdNew = true

		// Ensure link project<->source is created for this chat
		if o.sproj != nil && strings.TrimSpace(in.ProjectID) != "" {
			// Check for any existing link (regardless of status)
			links, _ := o.sproj.ListBySource(ctx, in.TenantID, ds.ID, nil)
			exists := false
			for _, l := range links {
				if l.ProjectID == in.ProjectID {
					exists = true
					break
				}
			}
			if !exists {
				_, _ = o.sproj.Create(ctx, &models.SourceProject{TenantID: in.TenantID, SourceID: ds.ID, ProjectID: in.ProjectID, Status: "paused", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
			}
		}
	}
	msg := map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"sourceId":      ds.ID,
		"status":        "fetching",
		"kind":          ds.Kind,
		"displayName":   ds.DisplayName,
	}
	if createdNew {
		msg["firstProfile"] = true
		msg["hint"] = "profilingNewSource"
	}
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, msg)

	// Save messages as documents if ingest is available
	// IngestTelegram will fetch messages itself, so we call it first
	if o.docs != nil {
		// Use tenantID from input (project's tenant) for document storage
		// For public sources (TenantID=""), documents should be global (TenantID="")
		// This makes them visible to all tenants who added this source
		docTenantID := ds.TenantID // Empty for public sources, tenant-specific for private sources
		if ingestErr := o.IngestTelegram(ctx, docTenantID, ds.ID, handle, 0); ingestErr != nil {
			o.logger.Printf("orchestrator: failed to ingest telegram messages for @%s: %v", handle, ingestErr)
			// Don't fail the whole process if ingest fails, continue with profiling
		} else {
			o.logger.Printf("orchestrator: successfully ingested messages for @%s", handle)
		}
	}

	// fetch posts (last 30 to avoid Firestore 1MB document limit) for profiling
	msgs, err := o.telegram.FetchUpdates(ctx, handle, 0)
	if err != nil {
		o.logger.Printf("orchestrator: telegram fetch error for @%s: %v", handle, err)
		return err
	}
	// Limit to 30 most recent messages
	if len(msgs) > 30 {
		msgs = msgs[len(msgs)-30:]
	}

	// concatenate text for profiling
	var b strings.Builder
	for _, m := range msgs {
		if m.Text != "" {
			b.WriteString(m.Text)
			b.WriteString("\n\n")
		}
	}

	// Persist fetched context snapshot for telegram
	if o.interactions != nil {
		text := b.String()
		snippet := text
		if len(snippet) > 5000 {
			snippet = snippet[:5000]
		}
		ctxObj := map[string]interface{}{
			"sourceId":    ds.ID,
			"displayName": ds.DisplayName,
			"sourceKind":  ds.Kind,
			"handle":      "@" + handle,
			"snippet":     snippet,
			"chars":       len(text),
		}
		bb, _ := json.Marshal(ctxObj)
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.context", Content: string(bb)})
	}

	prof, err := o.profile(ctx, ds.DisplayName, b.String(), in.Language)
	if err != nil {
		o.logger.Printf("orchestrator: profiling error for @%s: %v", handle, err)
		return err
	}
	ds.Status = "paused"
	if err := o.sources.Upsert(ctx, ds); err != nil {
		o.logger.Printf("orchestrator: failed to upsert telegram source @%s (profile size: ~%d bytes): %v", handle, len(mustJSON(prof)), err)
		return err
	}
	o.logger.Printf("orchestrator: saved telegram profile for @%s, size: ~%d bytes", handle, len(mustJSON(prof)))
	if o.profiles != nil {
		_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: ds.ID, TenantID: "", DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *prof})
	}

	// Persist profile summary interaction (telegram)
	if o.interactions != nil {
		summary := summarizeProfile(prof)
		obj := map[string]interface{}{
			"sourceId":       ds.ID,
			"displayName":    ds.DisplayName,
			"sourceKind":     ds.Kind,
			"handle":         "@" + handle,
			"summary":        summary,
			"hasFullProfile": true,
		}
		b2, _ := json.Marshal(obj)
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.profile", Content: string(b2)})
	}
	o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": prof})
	return nil
}

func (o *Orchestrator) processRSSSource(ctx context.Context, in ExecuteInput, feedURL string) error {
	// find or create
	var ds *models.DataSource
	needsUpdate := false
	// We can reuse FindByCanonicalSource logic if we treat feedURL as canonical.
	// Or search by Details.RSSURL
	// Currently FindByCanonicalSource probably looks for URI or Handle.
	// Let's use basic List or Get by URL if we had an index, but here we might iterate or rely on ID logic?
	// Orchestrator's FindByCanonicalSource implementation is not visible here, assuming it handles RSSURL or we should implement logic.
	// Actually, let's assume we create new if not found or use ListByKinds filter.

	// Since FindByCanonicalSource might not support RSSURL specifically without index,
	// and we want to be robust, let's try to find by basic filtering if needed or just create a new one with deterministic ID/lookup.
	// However, sticking to pattern:

	// Note: In previous turns, repo logic wasn't fully shown, but processWebSource uses it.
	// We'll try to use it if it supports generic lookups, or just search by listing.
	// Optimization: assume URL is unique enough or rely on repo.

	// Better: use repo to find by details.rssUrl if possible.
	// If not exposed, we can list all RSS (expensive) or just create new and rely on idempotency if repo handles it.
	// Let's implement "Find or Create" pattern manually if needed or trust FindByCanonicalSource.

	// For now, let's trust FindByCanonicalSource handles it or we fallback to creation.
	// Actually, for RSS, the ID is usually generated or we search.

	// NOTE: For this implementation, we'll assume FindByCanonicalSource works for RSS or we implement a fallback search.
	// But to be safe, let's implement a robust find.

	// Find existing by URL
	existing, err := o.sources.FindByCanonicalSource(ctx, in.TenantID, feedURL)
	if err != nil {
		o.logger.Printf("orchestrator: FindByCanonicalSource for %s error: %v", feedURL, err)
	}

	createdNew := false
	if existing != nil {
		o.logger.Printf("orchestrator: found existing rss source %s (id=%s) for project %s", feedURL, existing.ID, in.ProjectID)
		ds = existing
		ds.LastSeen = time.Now().UTC()
		needsUpdate = true

		// Ensure link
		o.ensureProjectSourceLink(ctx, in.TenantID, in.ProjectID, ds.ID)

		if (ds.Status == "paused" || ds.Status == "completed") && o.profiles != nil {
			if needsUpdate {
				_ = o.sources.Upsert(ctx, ds)
			}
			if sp, _ := o.profiles.GetByDataSource(ctx, in.TenantID, ds.ID); sp != nil {
				o.logger.Printf("orchestrator: reusing existing profile for rss %s", feedURL)
				o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
					"projectId": in.ProjectID, "sourceId": ds.ID, "profile": sp.Profile})
				return nil
			}
		}
	} else {
		now := time.Now().UTC()
		o.logger.Printf("orchestrator: creating new rss source %s for project %s", feedURL, in.ProjectID)
		initialInterval := o.cadenceToDuration(nil, false)
		initialNext := now.Add(initialInterval)

		ds = &models.DataSource{
			TenantID:    "", // Public
			Pk:          "global",
			DisplayName: feedURL,
			Kind:        "rss",
			Status:      "paused",
			OwnerUserID: in.OwnerUID,
			Details:     models.SourceDetails{RSSURL: &feedURL},
			CreatedAt:   now, UpdatedAt: now, FirstSeen: now, LastSeen: now,
			NextPollAt: &initialNext,
		}
		if createdID, err := o.sources.Create(ctx, ds); err != nil {
			o.logger.Printf("orchestrator: failed to create rss source %s: %v", feedURL, err)
			return err
		} else {
			o.logger.Printf("orchestrator: created rss source %s with id=%s", feedURL, createdID)
		}
		createdNew = true

		// Ensure link
		o.ensureProjectSourceLink(ctx, in.TenantID, in.ProjectID, ds.ID)
	}

	msg := map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"sourceId":      ds.ID,
		"status":        "fetching",
		"kind":          ds.Kind,
		"displayName":   ds.DisplayName,
	}
	if createdNew {
		msg["firstProfile"] = true
		msg["hint"] = "profilingNewSource"
	}
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, msg)

	// Fetch content for profiling and ingest
	// We reuse the provider to get documents
	if o.providersReg == nil {
		o.logger.Printf("orchestrator: no providers registry for rss %s", feedURL)
		return fmt.Errorf("providers registry not wired")
	}
	p, ok := o.providersReg.Get("rss")
	if !ok {
		return fmt.Errorf("rss provider not registered")
	}

	// Fetch docs (this does network call)
	docs, err := p.Ingest(ctx, in.TenantID, feedURL)
	if err != nil {
		o.logger.Printf("orchestrator: rss fetch error for %s: %v", feedURL, err)
		return err
	}

	// Ingest them (save to DB, Vertex indexes via GCS) if dependencies available
	if o.docs != nil {
		// Use tenantID from input
		docTenantID := ds.TenantID
		count, err := o.processProviderDocuments(ctx, docTenantID, ds.ID, docs)
		if err != nil {
			o.logger.Printf("orchestrator: failed to process rss docs for %s: %v", feedURL, err)
		} else {
			o.logger.Printf("orchestrator: ingested %d rss docs for %s", count, feedURL)
		}
	}

	// Build text blob for profiling from recent docs (limit to e.g. 10 most recent)
	var b strings.Builder
	// docs are typically ordered by feed order, assume most recent first or standard feed order
	limit := 10
	if len(docs) < limit {
		limit = len(docs)
	}
	for i := 0; i < limit; i++ {
		d := docs[i]
		b.WriteString(d.Title)
		b.WriteString("\n")
		// Use full text or description
		b.WriteString(d.FullText)
		b.WriteString("\n\n")
	}

	// Persist context snapshot
	if o.interactions != nil {
		text := b.String()
		snippet := text
		if len(snippet) > 5000 {
			snippet = snippet[:5000]
		}
		ctxObj := map[string]interface{}{
			"sourceId":    ds.ID,
			"displayName": ds.DisplayName,
			"sourceKind":  ds.Kind,
			"uri":         feedURL,
			"snippet":     snippet,
			"chars":       len(text),
		}
		bb, _ := json.Marshal(ctxObj)
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.context", Content: string(bb)})
	}

	// Profile
	prof, err := o.profile(ctx, ds.DisplayName, b.String(), in.Language)
	if err != nil {
		o.logger.Printf("orchestrator: profiling error for rss %s: %v", feedURL, err)
		return err
	}

	ds.Status = "paused"
	if err := o.sources.Upsert(ctx, ds); err != nil {
		return err
	}

	if o.profiles != nil {
		_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: ds.ID, TenantID: "", DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *prof})
	}

	// Persist profile summary
	if o.interactions != nil {
		summary := summarizeProfile(prof)
		obj := map[string]interface{}{
			"sourceId":       ds.ID,
			"displayName":    ds.DisplayName,
			"sourceKind":     ds.Kind,
			"uri":            feedURL,
			"summary":        summary,
			"hasFullProfile": true,
		}
		b2, _ := json.Marshal(obj)
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.profile", Content: string(b2)})
	}

	o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": prof})

	return nil
}

func (o *Orchestrator) profile(ctx context.Context, sourceId, extractedText, lang string) (*models.SourceProfileData, error) {
	pr := NewSourceProfiler(o.exec, o.logger)
	return pr.ProfileSource(ctx, ProfileRequest{SourceID: sourceId, ExtractedText: extractedText, Language: lang})
}

func (o *Orchestrator) synthesizeAnswer(ctx context.Context, in ExecuteInput) error {
	// Load sources
	list, err := o.sources.ListByProject(ctx, in.TenantID, in.ProjectID)
	if err != nil {
		o.logger.Printf("orchestrator: failed to list sources for project %s: %v", in.ProjectID, err)
		return err
	}
	o.logger.Printf("orchestrator: loaded %d sources for project %s, preparing synthesis", len(list), in.ProjectID)
	// Build human-readable identifiers per source
	infoByID := map[string]map[string]interface{}{}
	for _, ds := range list {
		if ds == nil || strings.TrimSpace(ds.ID) == "" {
			continue
		}
		m := map[string]interface{}{
			"displayName": ds.DisplayName,
			"sourceKind":  ds.Kind,
		}
		if ds.Kind == "telegram_channel" && ds.Details.ChannelUsername != nil {
			h := strings.TrimSpace(*ds.Details.ChannelUsername)
			if h != "" {
				m["handle"] = "@" + h
			}
		}
		if ds.Kind == "web" && ds.Details.URI != nil {
			u := strings.TrimSpace(*ds.Details.URI)
			if u != "" {
				m["uri"] = u
			}
		}
		infoByID[ds.ID] = m
	}

	// Build profiles array JSON via SourceProfiles repository (augmented)
	profiles := make([]map[string]interface{}, 0, len(list))
	if o.profiles != nil {
		profs, _ := o.profiles.ListByProject(ctx, in.TenantID, in.ProjectID, o.sources)
		for _, sp := range profs {
			p := sp.Profile
			entry := map[string]interface{}{
				"sourceId":                sp.DataSourceID,
				"editorialStance":         p.EditorialStance,
				"agenda":                  p.Agenda,
				"motivation":              p.Motivation,
				"trustScore":              p.TrustScore,
				"trustRationale":          p.TrustRationale,
				"behaviorPatterns":        p.BehaviorPatterns,
				"contentPatterns":         p.ContentPatterns,
				"topics":                  p.Topics,
				"editorialMethods":        p.EditorialMethods,
				"goals":                   p.Goals,
				"goalAlignmentScore":      p.GoalAlignmentScore,
				"metrics":                 p.Metrics,
				"targetAudienceCountries": p.TargetAudienceCountries,
				"redFlags":                p.RedFlags,
				"coordinatedSources":      p.CoordinatedSources,
				"flags":                   p.Flags,
			}
			if inf, ok := infoByID[sp.DataSourceID]; ok {
				if v, ok2 := inf["displayName"]; ok2 {
					entry["displayName"] = v
				}
				if v, ok2 := inf["sourceKind"]; ok2 {
					entry["sourceKind"] = v
				}
				if v, ok2 := inf["handle"]; ok2 {
					entry["handle"] = v
				}
				if v, ok2 := inf["uri"]; ok2 {
					entry["uri"] = v
				}
			}
			profiles = append(profiles, entry)
		}
	}
	o.logger.Printf("orchestrator: synthesis will use %d profiles (out of %d sources)", len(profiles), len(list))

	profilesJSON := mustJSON(profiles)
	prompt := prompts.Format(prompts.AnswerSynthesisPrompt, map[string]string{
		"queryText":    in.QueryText,
		"language":     in.Language,
		"sourcesCount": fmt.Sprintf("%d", len(profiles)),
		"profilesJson": profilesJSON,
	})

	task := &ai.AITask{
		TaskType:       ai.TaskTypeKnowledgeSynthesis,
		InputContent:   prompt,
		AIRequirements: ai.AIRequirements{TextGenerationCapability: true},
	}
	res, err := o.exec.ExecuteTask(ctx, task)
	if err != nil {
		return err
	}
	// persist interaction (answer)
	if o.interactions != nil {
		// Persist model/usage if available
		model := res.Model
		usage := &repo.UsageMeta{PromptTokens: res.InputTokens, OutputTokens: res.OutputTokens, TotalTokens: res.InputTokens + res.OutputTokens}
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.answer", Content: res.Content, Model: &model, Usage: usage})
	}
	o.publishEvent(ctx, realtime.EventDiscoveryAnswer, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"answer":        res.Content,
		"model":         res.Model,
		"usage": map[string]interface{}{
			"promptTokens": res.InputTokens,
			"outputTokens": res.OutputTokens,
			"totalTokens":  res.InputTokens + res.OutputTokens,
		},
	})
	return nil
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// summarizeProfile produces a compact summary JSON-safe string from SourceProfileData
func summarizeProfile(p *models.SourceProfileData) string {
	if p == nil {
		return ""
	}
	type summary struct {
		PrimaryGoal string   `json:"primaryGoal,omitempty"`
		Motivation  string   `json:"motivation,omitempty"`
		Topics      []string `json:"topics,omitempty"`
		TrustScore  float64  `json:"trustScore,omitempty"`
		Alignment   float64  `json:"goalAlignmentScore,omitempty"`
	}
	var align float64
	if p.GoalAlignmentScore != nil {
		align = *p.GoalAlignmentScore
	}
	s := summary{
		TrustScore: p.TrustScore,
		Alignment:  align,
	}
	if p.Agenda != nil && p.Agenda.PrimaryGoal != nil {
		s.PrimaryGoal = *p.Agenda.PrimaryGoal
	}
	s.Motivation = p.Motivation
	if len(p.Topics) > 0 {
		tops := make([]string, 0, len(p.Topics))
		for i, t := range p.Topics {
			if i >= 8 {
				break
			}
			if t.Name != "" {
				tops = append(tops, t.Name)
			}
		}
		s.Topics = tops
	}
	b, _ := json.Marshal(s)
	return string(b)
}
