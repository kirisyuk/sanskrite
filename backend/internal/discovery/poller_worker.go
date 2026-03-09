package discovery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SourcePoller coordinates due sources and executes per-source ingestion.
// Coordination uses source_runtime_state as mutable scheduler state.
type SourcePoller struct {
	orchestrator    *Orchestrator
	logger          *log.Logger
	runtime         *repo.SourceRuntimeStatesRepository
	tasksDispatcher SourceTaskDispatcher
}

type coordinationStats struct {
	scanned  int
	skipped  int
	enqueued int
}

var errStaleSourceLease = errors.New("stale source lease")

func NewSourcePoller(o *Orchestrator, logger *log.Logger) *SourcePoller {
	return &SourcePoller{orchestrator: o, logger: logger}
}

func (w *SourcePoller) WithRuntimeStates(runtime *repo.SourceRuntimeStatesRepository) *SourcePoller {
	w.runtime = runtime
	return w
}

func (w *SourcePoller) WithTaskDispatcher(dispatcher SourceTaskDispatcher) *SourcePoller {
	w.tasksDispatcher = dispatcher
	return w
}

// CoordinateDue coordinates due sources and dispatches individual execution tasks.
func (w *SourcePoller) CoordinateDue(ctx context.Context) error {
	stats, err := w.coordinateDue(ctx)
	if err == nil && w.logger != nil {
		w.logger.Printf("source coordinator: scanned=%d skipped=%d enqueued=%d", stats.scanned, stats.skipped, stats.enqueued)
	}
	return err
}

func (w *SourcePoller) coordinateDue(ctx context.Context) (coordinationStats, error) {
	stats := coordinationStats{}
	if w.orchestrator == nil || w.orchestrator.sources == nil {
		return stats, nil
	}
	now := time.Now().UTC()
	kinds := []string{
		models.SourceKindRSS,
		models.SourceKindTelegramChannel,
		models.SourceKindHorizonFeed,
		models.SourceKindWorldMonitorLegacy,
	}
	list, err := w.orchestrator.sources.ListByKinds(ctx, kinds)
	if err != nil {
		return stats, err
	}
	for _, ds := range list {
		stats.scanned++
		enqueued, skipped, err := w.coordinateSource(ctx, ds, now)
		if err != nil {
			return stats, err
		}
		if enqueued {
			stats.enqueued++
		}
		if skipped {
			stats.skipped++
		}
	}
	return stats, nil
}

func (w *SourcePoller) coordinateSource(ctx context.Context, ds *models.DataSource, now time.Time) (enqueued bool, skipped bool, err error) {
	if ds == nil {
		return false, true, nil
	}
	if ds.Status == "paused" {
		resumed, err := w.resumeAutoPausedSource(ctx, ds, now)
		if err != nil {
			return false, false, err
		}
		if !resumed {
			status := "skipped_paused"
			if models.IsAutoPaused(ds) {
				status = "skipped_auto_paused"
			}
			return false, w.holdPausedSource(ctx, ds, status), nil
		}
	}
	if models.IsGlobalSystemSource(ds) && models.UsesCanvasLLMProcessing(ds) {
		return false, w.skipSource(ctx, ds, now, "skipped_canvas_lane", nil), nil
	}
	if ds.Status == "completed" {
		return false, w.skipSource(ctx, ds, now, "skipped_completed", nil), nil
	}

	systemGlobalSource := w.isSystemGlobalHeuristicSource(ds)
	if !systemGlobalSource && !w.hasActiveProjects(ctx, ds.TenantID, ds.ID) {
		return false, w.skipSource(ctx, ds, now, "skipped_no_active_projects", nil), nil
	}

	leaseOwner := ""
	payloadTenantID := ds.TenantID
	if payloadTenantID == "" && systemGlobalSource {
		payloadTenantID = "global"
	}
	claimed, err := w.claimSourceLease(ctx, ds, now, &leaseOwner)
	if err != nil {
		return false, false, err
	}
	if !claimed {
		return false, false, nil
	}

	payload := SourceTaskPayload{
		SourceID:   ds.ID,
		TenantID:   payloadTenantID,
		Kind:       ds.Kind,
		LeaseOwner: leaseOwner,
	}
	if err := w.dispatchSource(ctx, payload); err != nil {
		w.releaseSourceLease(ctx, ds, now, leaseOwner, "enqueue_error", err)
		return false, false, err
	}
	if w.runtime != nil {
		_, _ = w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
			ensureRuntimeStateBase(state, ds)
			if state.LeaseOwner == nil || *state.LeaseOwner != leaseOwner {
				return nil
			}
			state.LastEnqueuedAt = timePtr(now)
			state.LastStatus = stringPtr("queued")
			state.LastError = nil
			state.Status = ds.Status
			return nil
		})
	}
	return true, false, nil
}

func (w *SourcePoller) RunSourcePoll(ctx context.Context, payload SourceTaskPayload) error {
	payload.SourceID = trim(payload.SourceID)
	payload.Kind = models.NormalizeSourceKind(trim(payload.Kind))
	payload.TenantID = trim(payload.TenantID)
	payload.LeaseOwner = trim(payload.LeaseOwner)
	if payload.SourceID == "" {
		return fmt.Errorf("sourceId is required")
	}

	dsRepo := w.orchestrator.sources
	if dsRepo == nil {
		return nil
	}
	ds, err := dsRepo.Get(ctx, payload.SourceID)
	if err != nil || ds == nil {
		return err
	}

	if payload.Kind == "" {
		payload.Kind = ds.Kind
	}
	if err := w.validateLease(ctx, ds, payload.LeaseOwner); err != nil {
		if errors.Is(err, errStaleSourceLease) {
			return nil
		}
		return err
	}
	if ds.Status == "completed" {
		return nil
	}

	systemGlobalSource := w.isSystemGlobalHeuristicSource(ds)
	processingTenantID := payload.TenantID
	if processingTenantID == "" && ds.TenantID == "" {
		if systemGlobalSource {
			processingTenantID = "global"
		} else if tenantID := w.resolveTenantIDForPublicSource(ctx, payload.SourceID); tenantID != "" {
			processingTenantID = tenantID
		}
	}
	if !systemGlobalSource && !w.hasActiveProjects(ctx, payload.TenantID, payload.SourceID) {
		w.finalizeSkipRun(ctx, ds, payload.LeaseOwner, "skipped_no_active_projects")
		return nil
	}
	if !systemGlobalSource && processingTenantID == "" && ds.TenantID == "" {
		w.finalizeSkipRun(ctx, ds, payload.LeaseOwner, "skipped_missing_processing_tenant")
		return nil
	}

	preLastFoundAt := ds.LastFoundAt
	ctxP, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()

	ingestAttempted := false
	var pollErr error

	switch payload.Kind {
	case models.SourceKindRSS:
		if w.orchestrator.publish != nil && w.orchestrator.providersReg != nil {
			ingestAttempted = true
			pollErr = w.orchestrator.IngestRSS(ctxP, processingTenantID, payload.SourceID, nil)
		} else {
			pollErr = fmt.Errorf("ingest dependencies not wired")
		}
	case models.SourceKindTelegramChannel:
		if w.orchestrator.sources != nil {
			if dsTg, err := w.orchestrator.sources.Get(ctxP, payload.SourceID); err == nil && dsTg != nil && dsTg.Details.ChannelUsername != nil {
				if w.orchestrator.telegram != nil && w.orchestrator.docs != nil {
					ingestAttempted = true
					last := 0
					if dsTg.Details.LastProcessedMessageID != nil {
						last = *dsTg.Details.LastProcessedMessageID
					}
					pollErr = w.orchestrator.IngestTelegram(ctxP, processingTenantID, payload.SourceID, *dsTg.Details.ChannelUsername, last)
				} else {
					pollErr = fmt.Errorf("ingest dependencies not wired")
				}
			} else {
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
			pollErr = fmt.Errorf("ingest dependencies not wired")
		}
	default:
		pollErr = fmt.Errorf("unsupported kind: %s", payload.Kind)
	}

	updatedDs := ds
	if refreshed, rerr := dsRepo.Get(ctx, payload.SourceID); rerr == nil && refreshed != nil {
		updatedDs = refreshed
	}
	processed, created, status, foundNew := w.latestIngestOutcome(ctx, processingTenantID, payload.SourceID, preLastFoundAt, updatedDs, ingestAttempted, pollErr)
	if err := w.finalizeRun(ctx, updatedDs, payload.LeaseOwner, sourceRunOutcome{
		foundNew:           foundNew,
		hadError:           pollErr != nil,
		documentsProcessed: processed,
		documentsCreated:   created,
	}, status, pollErr); err != nil {
		return err
	}
	return pollErr
}

func (w *SourcePoller) latestIngestOutcome(ctx context.Context, tenantID, sourceID string, previousLastFoundAt *time.Time, ds *models.DataSource, ingestAttempted bool, pollErr error) (documentsProcessed, documentsCreated int, status string, foundNew bool) {
	status = "success"
	if pollErr != nil {
		status = "error"
	}
	if !ingestAttempted {
		status = "skipped"
	}

	foundNew = lastFoundAdvanced(previousLastFoundAt, ds)
	if w.orchestrator == nil || w.orchestrator.ingestHistory == nil {
		return documentsProcessed, documentsCreated, status, foundNew
	}
	history, err := w.orchestrator.ingestHistory.ListBySource(ctx, tenantID, sourceID, 1)
	if err != nil || len(history) == 0 || history[0] == nil {
		if foundNew && status == "success" {
			return 1, 1, status, true
		}
		return documentsProcessed, documentsCreated, status, foundNew
	}
	last := history[0]
	if trim(last.SourceID) != sourceID {
		return documentsProcessed, documentsCreated, status, foundNew
	}
	documentsProcessed = last.DocumentsProcessed
	documentsCreated = last.DocumentsCreated
	if trim(last.Status) != "" {
		status = trim(last.Status)
	}
	if documentsCreated > 0 {
		foundNew = true
	}
	return documentsProcessed, documentsCreated, status, foundNew
}

func (w *SourcePoller) finalizeSkipRun(ctx context.Context, ds *models.DataSource, leaseOwner, status string) {
	_ = w.finalizeRun(ctx, ds, leaseOwner, sourceRunOutcome{}, status, nil)
}

func (w *SourcePoller) finalizeRun(ctx context.Context, ds *models.DataSource, leaseOwner string, outcome sourceRunOutcome, status string, runErr error) error {
	now := time.Now().UTC()
	if w.runtime != nil {
		state, err := w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
			ensureRuntimeStateBase(state, ds)
			if leaseOwner != "" && state.LeaseOwner != nil && *state.LeaseOwner != leaseOwner {
				return nil
			}

			state.LastPolledAt = timePtr(now)
			state.LastStatus = stringPtr(status)
			state.LeaseOwner = nil
			state.LeaseUntil = nil
			state.TotalRuns++
			state.TotalDocumentsProcessed += outcome.documentsProcessed
			state.TotalDocumentsCreated += outcome.documentsCreated

			if runErr != nil {
				state.ConsecutiveErrors++
				state.LastErrorAt = timePtr(now)
				state.LastError = stringPtr(runErr.Error())
				backoff := int(computeNextInterval(ds, state, outcome).Seconds())
				state.PollBackoffSec = intPtr(backoff)
				if shouldAutoPauseSource(ds, state) {
					state.Status = "paused"
					state.PauseReason = stringPtr(models.SourcePauseReasonAutoErrorThreshold)
					state.PausedAt = timePtr(now)
					state.PausedUntil = computeAutoPauseUntil(ds, now)
					state.LastStatus = stringPtr("auto_paused")
					if state.PausedUntil != nil {
						state.NextPollAt = cloneTimePtr(state.PausedUntil)
					}
					return nil
				}
			} else {
				state.ConsecutiveErrors = 0
				state.LastSuccessAt = timePtr(now)
				state.LastError = nil
				state.LastErrorAt = nil
				zero := 0
				state.PollBackoffSec = &zero
				if state.Status == "paused" && state.PauseReason != nil && *state.PauseReason == models.SourcePauseReasonAutoErrorThreshold {
					state.Status = "active"
					state.PauseReason = nil
					state.PausedAt = nil
					state.PausedUntil = nil
				}
			}

			if outcome.foundNew {
				state.LastFoundAt = timePtr(now)
				state.ConsecutiveEmptyPolls = 0
			} else if !outcome.hadError && outcome.documentsProcessed == 0 && outcome.documentsCreated == 0 {
				state.ConsecutiveEmptyPolls++
			} else if !outcome.hadError {
				state.ConsecutiveEmptyPolls = 0
			}

			lastYield := float64(outcome.documentsCreated)
			state.LastYield = floatPtr(lastYield)
			if state.AvgYield == nil {
				state.AvgYield = floatPtr(lastYield)
			} else {
				avg := (*state.AvgYield * 0.7) + (lastYield * 0.3)
				state.AvgYield = floatPtr(avg)
			}

			next := now.Add(computeNextInterval(ds, state, outcome))
			state.NextPollAt = &next
			return nil
		})
		if err != nil {
			return err
		}
		if state != nil {
			w.syncLegacySchedulingSnapshot(ctx, ds, state)
		}
		return nil
	}

	next := now.Add(computeNextInterval(ds, nil, outcome))
	ds.LastPolledAt = &now
	ds.NextPollAt = &next
	if outcome.foundNew {
		ds.LastFoundAt = &now
	}
	if runErr != nil {
		ds.ErrorCount++
	} else {
		ds.ErrorCount = 0
	}
	return w.orchestrator.sources.Upsert(ctx, ds)
}

func (w *SourcePoller) holdPausedSource(ctx context.Context, ds *models.DataSource, status string) bool {
	if ds == nil {
		return true
	}
	if w.runtime != nil {
		state, stateErr := w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
			ensureRuntimeStateBase(state, ds)
			state.LastStatus = stringPtr(status)
			state.LastError = nil
			state.LeaseOwner = nil
			state.LeaseUntil = nil
			state.Status = ds.Status
			state.PauseReason = cloneStringPtr(ds.PauseReason)
			state.PausedAt = cloneTimePtr(ds.PausedAt)
			state.PausedUntil = cloneTimePtr(ds.PausedUntil)
			if state.PausedUntil != nil {
				state.NextPollAt = cloneTimePtr(state.PausedUntil)
			}
			return nil
		})
		if stateErr == nil && state != nil {
			w.syncLegacySchedulingSnapshot(ctx, ds, state)
		}
		return true
	}
	_ = w.orchestrator.sources.Upsert(ctx, ds)
	return true
}

func (w *SourcePoller) skipSource(ctx context.Context, ds *models.DataSource, now time.Time, status string, err error) bool {
	if w.runtime != nil {
		state, stateErr := w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
			ensureRuntimeStateBase(state, ds)
			state.LastStatus = stringPtr(status)
			if err != nil {
				state.LastError = stringPtr(err.Error())
			} else {
				state.LastError = nil
			}
			state.LeaseOwner = nil
			state.LeaseUntil = nil
			state.Status = ds.Status
			state.PauseReason = cloneStringPtr(ds.PauseReason)
			state.PausedAt = cloneTimePtr(ds.PausedAt)
			state.PausedUntil = cloneTimePtr(ds.PausedUntil)
			next := now.Add(computeNextInterval(ds, state, sourceRunOutcome{}))
			state.NextPollAt = &next
			return nil
		})
		if stateErr == nil && state != nil {
			w.syncLegacySchedulingSnapshot(ctx, ds, state)
		}
		return true
	}
	next := now.Add(cadenceToDuration(ds))
	ds.NextPollAt = &next
	_ = w.orchestrator.sources.Upsert(ctx, ds)
	return true
}

func (w *SourcePoller) claimSourceLease(ctx context.Context, ds *models.DataSource, now time.Time, leaseOwnerOut *string) (bool, error) {
	if w.runtime == nil {
		return true, nil
	}
	claimed := false
	_, err := w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
		ensureRuntimeStateBase(state, ds)
		if state.LeaseUntil != nil && now.Before(*state.LeaseUntil) {
			return nil
		}
		if state.NextPollAt != nil && state.LastPolledAt != nil && now.Before(*state.NextPollAt) {
			return nil
		}
		leaseOwner := fmt.Sprintf("source:%s:%d", ds.ID, now.UnixNano())
		leaseUntil := now.Add(resolveSourceCadencePolicy(ds).leaseDuration)
		state.LeaseOwner = &leaseOwner
		state.LeaseUntil = &leaseUntil
		state.LastCoordinatedAt = timePtr(now)
		state.LastStatus = stringPtr("leased")
		state.Status = ds.Status
		*leaseOwnerOut = leaseOwner
		claimed = true
		return nil
	})
	return claimed, err
}

func (w *SourcePoller) releaseSourceLease(ctx context.Context, ds *models.DataSource, now time.Time, leaseOwner, status string, err error) {
	if w.runtime == nil {
		return
	}
	state, stateErr := w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
		ensureRuntimeStateBase(state, ds)
		if leaseOwner != "" && state.LeaseOwner != nil && *state.LeaseOwner != leaseOwner {
			return nil
		}
		state.LeaseOwner = nil
		state.LeaseUntil = nil
		state.LastStatus = stringPtr(status)
		state.Status = ds.Status
		if err != nil {
			state.LastError = stringPtr(err.Error())
		}
		next := now.Add(computeNextInterval(ds, state, sourceRunOutcome{}))
		state.NextPollAt = &next
		return nil
	})
	if stateErr == nil && state != nil {
		w.syncLegacySchedulingSnapshot(ctx, ds, state)
	}
}

func (w *SourcePoller) dispatchSource(ctx context.Context, payload SourceTaskPayload) error {
	if w.tasksDispatcher != nil {
		return w.tasksDispatcher.EnqueueSourcePoll(ctx, payload)
	}
	return fmt.Errorf("no source task dispatcher configured")
}

func (w *SourcePoller) validateLease(ctx context.Context, ds *models.DataSource, leaseOwner string) error {
	if leaseOwner == "" || w.runtime == nil {
		return nil
	}
	state, err := w.runtime.Get(ctx, ds.ID)
	if err != nil {
		return errStaleSourceLease
	}
	if state.LeaseOwner == nil || *state.LeaseOwner != leaseOwner {
		return errStaleSourceLease
	}
	return nil
}

func (w *SourcePoller) syncLegacySchedulingSnapshot(ctx context.Context, ds *models.DataSource, state *models.SourceRuntimeState) {
	if ds == nil || state == nil || w.orchestrator == nil || w.orchestrator.sources == nil {
		return
	}
	ds.LastPolledAt = state.LastPolledAt
	ds.LastFoundAt = state.LastFoundAt
	ds.NextPollAt = state.NextPollAt
	ds.PollBackoffSec = state.PollBackoffSec
	ds.ErrorCount = state.ConsecutiveErrors
	if trim(state.Status) != "" {
		ds.Status = state.Status
	}
	ds.PauseReason = cloneStringPtr(state.PauseReason)
	ds.PausedAt = cloneTimePtr(state.PausedAt)
	ds.PausedUntil = cloneTimePtr(state.PausedUntil)
	_ = w.orchestrator.sources.Upsert(ctx, ds)
}

func ensureRuntimeStateBase(state *models.SourceRuntimeState, ds *models.DataSource) {
	if state == nil || ds == nil {
		return
	}
	now := time.Now().UTC()
	if state.ID == "" {
		state.ID = ds.ID
	}
	if state.SourceID == "" {
		state.SourceID = ds.ID
	}
	if state.TenantID == "" {
		state.TenantID = ds.TenantID
	}
	if state.Kind == "" {
		state.Kind = ds.Kind
	}
	if state.Scope == "" {
		state.Scope = models.SourceScopeValue(ds)
	}
	if state.ProcessingMode == "" {
		state.ProcessingMode = models.SourceProcessingModeValue(ds)
	}
	if state.OutputMode == "" {
		state.OutputMode = models.SourceOutputModeValue(ds)
	}
	if state.Status == "" {
		state.Status = ds.Status
	}
	if state.LastPolledAt == nil {
		state.LastPolledAt = ds.LastPolledAt
	}
	if state.LastFoundAt == nil {
		state.LastFoundAt = ds.LastFoundAt
	}
	if state.NextPollAt == nil {
		state.NextPollAt = ds.NextPollAt
	}
	if state.PollBackoffSec == nil {
		state.PollBackoffSec = ds.PollBackoffSec
	}
	if state.PauseReason == nil {
		state.PauseReason = cloneStringPtr(ds.PauseReason)
	}
	if state.PausedAt == nil {
		state.PausedAt = cloneTimePtr(ds.PausedAt)
	}
	if state.PausedUntil == nil {
		state.PausedUntil = cloneTimePtr(ds.PausedUntil)
	}
	if state.ConsecutiveErrors == 0 && ds.ErrorCount > 0 {
		state.ConsecutiveErrors = ds.ErrorCount
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
}

func (w *SourcePoller) resumeAutoPausedSource(ctx context.Context, ds *models.DataSource, now time.Time) (bool, error) {
	if ds == nil {
		return false, nil
	}
	if ds.Status != "paused" {
		return false, nil
	}
	if !models.IsAutoPaused(ds) {
		return false, nil
	}
	if ds.PausedUntil != nil && now.Before(*ds.PausedUntil) {
		return false, nil
	}
	if w.runtime != nil {
		state, err := w.runtime.Mutate(ctx, ds.ID, func(state *models.SourceRuntimeState) error {
			ensureRuntimeStateBase(state, ds)
			state.Status = "active"
			state.PauseReason = nil
			state.PausedAt = nil
			state.PausedUntil = nil
			state.ConsecutiveErrors = 0
			state.LastError = nil
			zero := 0
			state.PollBackoffSec = &zero
			state.LastStatus = stringPtr("auto_resumed")
			state.NextPollAt = timePtr(now)
			return nil
		})
		if err != nil {
			return false, err
		}
		if state != nil {
			w.syncLegacySchedulingSnapshot(ctx, ds, state)
		}
	}
	ds.Status = "active"
	ds.PauseReason = nil
	ds.PausedAt = nil
	ds.PausedUntil = nil
	ds.ErrorCount = 0
	zero := 0
	ds.PollBackoffSec = &zero
	ds.NextPollAt = timePtr(now)
	return true, nil
}

func lastFoundAdvanced(previous *time.Time, ds *models.DataSource) bool {
	if ds == nil || ds.LastFoundAt == nil {
		return false
	}
	if previous == nil {
		return true
	}
	return ds.LastFoundAt.After(*previous)
}

func (w *SourcePoller) resolveTenantIDForPublicSource(ctx context.Context, sourceID string) string {
	if w.orchestrator == nil || w.orchestrator.sproj == nil {
		return ""
	}
	activeStatus := "active"
	links, err := w.orchestrator.sproj.ListBySource(ctx, "", sourceID, &activeStatus)
	if err != nil || len(links) == 0 {
		return ""
	}
	return trim(links[0].TenantID)
}

// hasActiveProjects checks if the source has at least one active project in rag_source_projects.
func (w *SourcePoller) hasActiveProjects(ctx context.Context, tenantID, sourceID string) bool {
	if w.orchestrator == nil || w.orchestrator.sproj == nil {
		return true
	}
	activeStatus := "active"
	var links []*models.SourceProject
	var err error

	if tenantID == "" {
		links, err = w.orchestrator.sproj.ListBySource(ctx, "", sourceID, &activeStatus)
	} else {
		links, err = w.orchestrator.sproj.ListBySource(ctx, tenantID, sourceID, &activeStatus)
	}
	if err != nil {
		if w.logger != nil {
			w.logger.Printf("source poller: failed to check active projects for source %s (tenantID=%s): %v", sourceID, tenantID, err)
		}
		return true
	}
	return len(links) > 0
}

func (w *SourcePoller) isSystemGlobalHeuristicSource(ds *models.DataSource) bool {
	return models.IsGlobalSystemSource(ds) && models.UsesHeuristicProcessing(ds)
}

func cadenceToDuration(ds *models.DataSource) time.Duration {
	return effectivePollCadence(ds, false)
}

func trim(v string) string {
	return strings.TrimSpace(v)
}

func stringPtr(v string) *string {
	v = trim(v)
	if v == "" {
		return nil
	}
	return &v
}

func timePtr(v time.Time) *time.Time {
	out := v.UTC()
	return &out
}

func cloneTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	out := v.UTC()
	return &out
}

func cloneStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	s := trim(*v)
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}
