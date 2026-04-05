package internalknowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/time/rate"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

const (
	internalKnowledgeViewConfigKey = "internalKnowledge"
	internalKnowledgeStateQueued   = "queued"
	internalKnowledgeStateRunning  = "running"
	internalKnowledgeStateReady    = "ready"
	internalKnowledgeStateFailed   = "failed"
	internalKnowledgeRefreshPrefix = "refresh:"
	pendingImportReason            = "pending_import_continue"
)

var internalKnowledgeTemperatureOrder = []string{
	PreparedTemperatureHot,
	PreparedTemperatureWarm,
	PreparedTemperatureFull,
}

type WarmupConfig struct {
	ProjectID            string
	AnswerBucket         string
	AnswerStoreID        string
	QueueSize            int
	UploadsPerSecond     float64
	ImportsPerMinute     int
	ProcessTimeout       time.Duration
	ImportWaitTimeout    time.Duration
	StatusWriteTimeout   time.Duration
	LeaseTTL             time.Duration
	LeaseAcquireInterval time.Duration
	ReconcileInterval    time.Duration
	ReconcileLimit       int
	StaleQueuedAfter     time.Duration
	StaleRunningAfter    time.Duration
	RetryFailedAfter     time.Duration
}

type WarmupRequest struct {
	TenantID  string
	ProjectID string
	LensID    string
	Reason    string
}

type WarmupService struct {
	cfg           WarmupConfig
	lenses        *hrepo.LensesRepository
	builder       *Builder
	gcs           *gcp.GCSClient
	vertex        *ai.VertexAISearchClient
	db            *firestore.Client
	queue         chan WarmupRequest
	pendingMu     sync.Mutex
	pendingLensID map[string]struct{}
	uploadLimiter *rate.Limiter
	importLimiter *rate.Limiter
}

func NewWarmupService(
	cfg WarmupConfig,
	lenses *hrepo.LensesRepository,
	builder *Builder,
	gcsClient *gcp.GCSClient,
	vertexClient *ai.VertexAISearchClient,
	firestoreClient *firestore.Client,
) *WarmupService {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 64
	}
	if cfg.UploadsPerSecond <= 0 {
		cfg.UploadsPerSecond = 5
	}
	if cfg.ImportsPerMinute <= 0 {
		cfg.ImportsPerMinute = 6
	}
	if cfg.ProcessTimeout <= 0 {
		cfg.ProcessTimeout = 45 * time.Minute
	}
	if cfg.ImportWaitTimeout <= 0 {
		cfg.ImportWaitTimeout = 15 * time.Minute
	}
	if cfg.StatusWriteTimeout <= 0 {
		cfg.StatusWriteTimeout = 15 * time.Second
	}
	if cfg.ProcessTimeout < cfg.ImportWaitTimeout+15*time.Minute {
		cfg.ProcessTimeout = cfg.ImportWaitTimeout + 15*time.Minute
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 45 * time.Minute
	}
	if cfg.LeaseAcquireInterval <= 0 {
		cfg.LeaseAcquireInterval = 15 * time.Second
	}
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = 2 * time.Minute
	}
	if cfg.ReconcileLimit <= 0 {
		cfg.ReconcileLimit = 64
	}
	if cfg.StaleQueuedAfter <= 0 {
		cfg.StaleQueuedAfter = 5 * time.Minute
	}
	if cfg.StaleRunningAfter <= 0 {
		cfg.StaleRunningAfter = 45 * time.Minute
	}
	if cfg.RetryFailedAfter <= 0 {
		cfg.RetryFailedAfter = 30 * time.Minute
	}
	svc := &WarmupService{
		cfg:           cfg,
		lenses:        lenses,
		builder:       builder,
		gcs:           gcsClient,
		vertex:        vertexClient,
		db:            firestoreClient,
		queue:         make(chan WarmupRequest, cfg.QueueSize),
		pendingLensID: map[string]struct{}{},
		uploadLimiter: rate.NewLimiter(rate.Limit(cfg.UploadsPerSecond), maxInt(1, int(cfg.UploadsPerSecond))),
		importLimiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(cfg.ImportsPerMinute)), maxInt(1, cfg.ImportsPerMinute)),
	}
	go svc.run()
	go svc.reconcileLoop()
	return svc
}

func (s *WarmupService) EnqueueLensWarmup(ctx context.Context, tenantID, projectID, lensID, reason string) {
	if s == nil || s.lenses == nil || s.builder == nil || s.gcs == nil || s.vertex == nil {
		return
	}
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return
	}
	req := WarmupRequest{
		TenantID:  strings.TrimSpace(tenantID),
		ProjectID: strings.TrimSpace(projectID),
		LensID:    lensID,
		Reason:    normalizeWarmupReason(reason),
	}
	s.pendingMu.Lock()
	if _, exists := s.pendingLensID[lensID]; exists {
		s.pendingMu.Unlock()
		return
	}
	s.pendingLensID[lensID] = struct{}{}
	s.pendingMu.Unlock()
	if !warmupIsReconcileReason(req.Reason) {
		s.persistQueuedState(req)
	}
	select {
	case s.queue <- req:
	default:
		go s.enqueueWhenReady(req)
	}
}

func (s *WarmupService) enqueueWhenReady(req WarmupRequest) {
	if s == nil {
		return
	}
	s.queue <- req
}

func (s *WarmupService) run() {
	for req := range s.queue {
		s.process(req)
		s.pendingMu.Lock()
		delete(s.pendingLensID, req.LensID)
		s.pendingMu.Unlock()
	}
}

func (s *WarmupService) process(req WarmupRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ProcessTimeout)
	defer cancel()

	lens, err := s.lenses.Get(ctx, req.LensID)
	if err != nil || lens == nil {
		log.Printf("[InternalKnowledgeWarmup] lens lookup failed lens=%s err=%v", req.LensID, err)
		return
	}
	if req.TenantID == "" {
		req.TenantID = strings.TrimSpace(lens.TenantID)
	}
	if req.ProjectID == "" {
		req.ProjectID = strings.TrimSpace(lens.ProjectID)
	}

	substrate, err := s.builder.BuildForLens(ctx, lens, "")
	if err != nil {
		s.failLensWarmState(req, lens, err)
		return
	}
	scopeHash := PreparedScopeHash(substrate)
	substrateHash := PreparedSubstrateHash(substrate)
	corpusID := strings.TrimSpace(substrate.CorpusID)
	currentState := lensInternalKnowledgeState(lens)
	if strings.EqualFold(strings.TrimSpace(asString(currentState["substrateHash"])), substrateHash) &&
		strings.EqualFold(strings.TrimSpace(asString(currentState["status"])), internalKnowledgeStateReady) {
		if err := s.updateLensWarmState(ctx, lens, map[string]any{
			"status":        internalKnowledgeStateReady,
			"scopeHash":     scopeHash,
			"substrateHash": substrateHash,
			"corpusId":      corpusID,
			"answerStore":   strings.TrimSpace(s.cfg.AnswerStoreID),
			"docCount":      len(substrate.Docs),
			"lastSkippedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"lastReason":    firstNonEmpty(req.Reason, "assembly_ready"),
		}); err != nil {
			log.Printf("[InternalKnowledgeWarmup] failed to persist skip-ready state lens=%s err=%v", req.LensID, err)
		}
		return
	}

	if err := s.updateLensWarmState(ctx, lens, map[string]any{
		"runningAt":     time.Now().UTC().Format(time.RFC3339Nano),
		"scopeHash":     scopeHash,
		"substrateHash": substrateHash,
		"corpusId":      corpusID,
		"answerStore":   strings.TrimSpace(s.cfg.AnswerStoreID),
		"docCount":      len(substrate.Docs),
		"projectId":     firstNonEmpty(req.ProjectID, s.cfg.ProjectID),
		"tenantId":      firstNonEmpty(req.TenantID, lens.TenantID),
		"lastReason":    warmupReasonCore(firstNonEmpty(req.Reason, "assembly_ready")),
	}); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to persist running state lens=%s err=%v", req.LensID, err)
	}
	if !warmupPreservesReadyState(req.Reason) {
		if err := s.updateLensWarmState(ctx, lens, map[string]any{"status": internalKnowledgeStateRunning}); err != nil {
			log.Printf("[InternalKnowledgeWarmup] failed to persist running status lens=%s err=%v", req.LensID, err)
		}
	} else {
		_ = s.updateLensWarmState(ctx, lens, map[string]any{
			"refreshRunningAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	opName := ""
	if err := s.withGlobalWarmupLease(ctx, req.LensID, func() error {
		for _, temperature := range internalKnowledgeTemperatureOrder {
			stageHash := PreparedSubstrateHashForTemperature(substrate, temperature)
			if stageHash == "" {
				continue
			}
			if lensTemperatureReady(currentState, temperature) && strings.EqualFold(strings.TrimSpace(asString(currentState[temperature+"Hash"])), stageHash) {
				continue
			}
			// Import only the docs introduced at this temperature. Earlier stages are
			// already persisted, so re-importing hot+warm+full cumulatively just makes
			// the slowest stage even slower.
			docs := filterPreparedDocsAtTemperature(substrate.Docs, temperature)
			if len(docs) == 0 {
				continue
			}
			stageSubstrate := *substrate
			stageSubstrate.Docs = docs
			uris, err := s.exportDocs(ctx, &stageSubstrate)
			if err != nil {
				return err
			}
			if waitErr := s.importLimiter.Wait(ctx); waitErr != nil {
				return waitErr
			}
			opName, err = s.vertex.WithDataStore(s.cfg.AnswerStoreID).ImportDocumentsFromURIs(ctx, compactUnique(uris), "INCREMENTAL", "document")
			if err != nil {
				return err
			}
			if persistErr := s.updateLensWarmState(ctx, lens, map[string]any{
				"importOperation":       strings.TrimSpace(opName),
				"lastReason":            warmupReasonCore(firstNonEmpty(req.Reason, "assembly_ready")),
				"temperatureLevel":      strings.TrimSpace(temperature),
				"availableTemperatures": append([]string{}, append(lensAvailableTemperatures(currentState), temperature)...),
			}); persistErr != nil {
				log.Printf("[InternalKnowledgeWarmup] failed to persist stage-running state lens=%s stage=%s err=%v", req.LensID, temperature, persistErr)
			}
			if !warmupPreservesReadyState(req.Reason) {
				_ = s.updateLensWarmState(ctx, lens, map[string]any{"status": internalKnowledgeStateRunning})
			}
			if err := waitForImport(ctx, s.vertex.WithDataStore(s.cfg.AnswerStoreID), opName, s.cfg.ImportWaitTimeout); err != nil {
				if shouldDeferImportCompletion(err) {
					s.deferPendingImport(ctx, req, lens, currentState, temperature, opName, stageHash, len(stageSubstrate.Docs), err)
					return nil
				}
				return err
			}
			currentState[temperature+"Hash"] = stageHash
			currentState[temperature+"ReadyAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			currentState["availableTemperatures"] = compactUnique(append(lensAvailableTemperatures(currentState), temperature))
			currentState["temperatureLevel"] = strings.TrimSpace(temperature)
			stageState := map[string]any{
				"temperatureLevel":      strings.TrimSpace(temperature),
				"availableTemperatures": compactUnique(append(lensAvailableTemperatures(currentState), temperature)),
				temperature + "Hash":    stageHash,
				temperature + "ReadyAt": time.Now().UTC().Format(time.RFC3339Nano),
				"docCount":              len(stageSubstrate.Docs),
				"corpusId":              corpusID,
				"answerStore":           strings.TrimSpace(s.cfg.AnswerStoreID),
			}
			if !warmupPreservesReadyState(req.Reason) || strings.EqualFold(strings.TrimSpace(temperature), PreparedTemperatureFull) {
				stageState["status"] = stageStatusForTemperature(temperature)
			}
			if persistErr := s.updateLensWarmState(ctx, lens, stageState); persistErr != nil {
				log.Printf("[InternalKnowledgeWarmup] failed to persist stage-ready state lens=%s stage=%s err=%v", req.LensID, temperature, persistErr)
			}
		}
		return nil
	}); err != nil {
		s.failLensWarmState(req, lens, err)
		return
	}

	// IMPORTANT: when lens scope expands we should stop doing full corpus rebuilds here.
	// The stored scopeHash/corpusId state is intentionally persisted so the next iteration can
	// compute the added scope delta and warm only the newly added slice of the lens substrate.
	if err := s.updateLensWarmState(ctx, lens, map[string]any{
		"status":                internalKnowledgeStateReady,
		"completedAt":           time.Now().UTC().Format(time.RFC3339Nano),
		"scopeHash":             scopeHash,
		"substrateHash":         substrateHash,
		"corpusId":              corpusID,
		"answerStore":           strings.TrimSpace(s.cfg.AnswerStoreID),
		"docCount":              len(substrate.Docs),
		"temperatureLevel":      PreparedTemperatureFull,
		"availableTemperatures": []string{PreparedTemperatureHot, PreparedTemperatureWarm, PreparedTemperatureFull},
		"importOperation":       strings.TrimSpace(opName),
		"lastError":             "",
		"lastReason":            warmupReasonCore(firstNonEmpty(req.Reason, "assembly_ready")),
	}); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to persist ready state lens=%s err=%v", req.LensID, err)
	}
	log.Printf("[InternalKnowledgeWarmup] lens=%s corpus=%s docs=%d ready", req.LensID, corpusID, len(substrate.Docs))
}

func (s *WarmupService) reconcileLoop() {
	if s == nil || s.db == nil {
		return
	}
	ticker := time.NewTicker(s.cfg.ReconcileInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.reconcileRecentLenses()
	}
}

func (s *WarmupService) reconcileRecentLenses() {
	if s == nil || s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	docs, err := s.db.Collection("horizon_lenses").
		Where("status", "==", string(hmodels.LensStatusActive)).
		Select("id", "tenantId", "projectId", "status", "updatedAt", "viewConfig.assemblyStatus", "viewConfig.internalKnowledge").
		OrderBy("updatedAt", firestore.Desc).
		Limit(s.cfg.ReconcileLimit).
		Documents(ctx).
		GetAll()
	if err != nil {
		log.Printf("[InternalKnowledgeWarmup] reconcile list failed: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, doc := range docs {
		var lens hmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if s.normalizeReconcileState(&lens) {
			continue
		}
		if s.tryAdvancePendingImport(&lens) {
			continue
		}
		reason, ok := s.shouldReconcileLens(&lens, now)
		if !ok {
			continue
		}
		s.EnqueueLensWarmup(context.Background(), strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ProjectID), strings.TrimSpace(lens.ID), reason)
	}
}

func (s *WarmupService) shouldReconcileLens(lens *hmodels.Lens, now time.Time) (string, bool) {
	if lens == nil || !lensWarmupEligible(lens) {
		return "", false
	}
	state := lensInternalKnowledgeState(lens)
	status := strings.TrimSpace(strings.ToLower(asString(state["status"])))
	switch status {
	case "":
		return "assembly_ready_reconcile", true
	case internalKnowledgeStateQueued:
		if lensTemperatureReady(state, PreparedTemperatureFull) && warmupIsReconcileReason(asString(state["lastReason"])) {
			return "", false
		}
		if ts, ok := parseRFC3339Time(state["queuedAt"]); !ok || now.Sub(ts) >= s.cfg.StaleQueuedAfter {
			return "queued_reconcile", true
		}
	case internalKnowledgeStateRunning:
		if lensTemperatureReady(state, PreparedTemperatureFull) && warmupIsReconcileReason(asString(state["lastReason"])) {
			return "", false
		}
		if ts, ok := parseRFC3339Time(state["runningAt"]); !ok || now.Sub(ts) >= s.cfg.StaleRunningAfter {
			return "stale_running_reconcile", true
		}
	case internalKnowledgeStateFailed:
		if ts, ok := parseRFC3339Time(state["failedAt"]); !ok || now.Sub(ts) >= s.cfg.RetryFailedAfter {
			return "failed_reconcile_retry", true
		}
	}
	return "", false
}

func (s *WarmupService) normalizeReconcileState(lens *hmodels.Lens) bool {
	if s == nil || s.lenses == nil || lens == nil {
		return false
	}
	state := lensInternalKnowledgeState(lens)
	if len(state) == 0 {
		return false
	}
	status := strings.TrimSpace(strings.ToLower(asString(state["status"])))
	if status != internalKnowledgeStateQueued && status != internalKnowledgeStateRunning {
		return false
	}
	if !warmupIsReconcileReason(asString(state["lastReason"])) {
		return false
	}
	if !lensTemperatureReady(state, PreparedTemperatureFull) {
		return false
	}
	delete(state, "queuedAt")
	delete(state, "runningAt")
	delete(state, "refreshQueuedAt")
	delete(state, "refreshRunningAt")
	state["status"] = internalKnowledgeStateReady
	state["lastReason"] = "reconcile_state_normalized"
	state["completedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.StatusWriteTimeout)
	defer cancel()
	if err := s.lenses.UpdateInternalKnowledgeState(ctx, lens.ID, state); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to normalize reconcile state lens=%s err=%v", lens.ID, err)
		return false
	}
	if lens.ViewConfig == nil {
		lens.ViewConfig = map[string]any{}
	}
	lens.ViewConfig[internalKnowledgeViewConfigKey] = state
	log.Printf("[InternalKnowledgeWarmup] normalized stale reconcile state lens=%s to ready", lens.ID)
	return true
}

func (s *WarmupService) exportDocs(ctx context.Context, substrate *PreparedSubstrate) ([]string, error) {
	if substrate == nil {
		return nil, fmt.Errorf("substrate is nil")
	}
	uris := make([]string, 0, len(substrate.Docs))
	for _, doc := range substrate.Docs {
		if err := s.uploadLimiter.Wait(ctx); err != nil {
			return nil, err
		}
		objectPath := fmt.Sprintf("global/internal_knowledge/corpora/%s/%s.json", substrate.CorpusID, doc.ID)
		gcsURI := fmt.Sprintf("gs://%s/%s", s.cfg.AnswerBucket, objectPath)
		payload := map[string]any{
			"id": doc.ID,
			"content": map[string]any{
				"mimeType": "text/plain",
				"rawBytes": base64.StdEncoding.EncodeToString([]byte(doc.Content)),
			},
			"structData": doc.StructData,
		}
		payload["structData"].(map[string]any)["uri"] = gcsURI
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if err := s.uploadDocWithRetry(ctx, substrate, objectPath, body); err != nil {
			return nil, err
		}
		uris = append(uris, gcsURI)
	}
	return uris, nil
}

func (s *WarmupService) updateLensWarmState(ctx context.Context, lens *hmodels.Lens, state map[string]any) error {
	if s == nil || s.lenses == nil || lens == nil {
		return nil
	}
	statusCtx, cancel := context.WithTimeout(ctx, s.cfg.StatusWriteTimeout)
	defer cancel()
	if lens.ViewConfig == nil {
		lens.ViewConfig = map[string]any{}
	}
	current := lensInternalKnowledgeState(lens)
	normalizeWarmStateForTransition(current, state)
	for key, value := range state {
		current[strings.TrimSpace(key)] = value
	}
	if err := s.lenses.UpdateInternalKnowledgeState(statusCtx, lens.ID, current); err != nil {
		return err
	}
	lens.ViewConfig[internalKnowledgeViewConfigKey] = current
	return nil
}

func (s *WarmupService) failLensWarmState(req WarmupRequest, lens *hmodels.Lens, err error) {
	if err == nil {
		return
	}
	log.Printf("[InternalKnowledgeWarmup] lens=%s failed: %v", req.LensID, err)
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.StatusWriteTimeout)
	defer cancel()
	_ = s.updateLensWarmState(ctx, lens, map[string]any{
		"failedAt":   time.Now().UTC().Format(time.RFC3339Nano),
		"lastError":  err.Error(),
		"lastReason": warmupReasonCore(firstNonEmpty(req.Reason, "assembly_ready")),
	})
	if !warmupPreservesReadyState(req.Reason) {
		_ = s.updateLensWarmState(ctx, lens, map[string]any{
			"status": internalKnowledgeStateFailed,
		})
	}
}

func (s *WarmupService) deferPendingImport(ctx context.Context, req WarmupRequest, lens *hmodels.Lens, currentState map[string]any, temperature, operation, stageHash string, docCount int, err error) {
	if s == nil || lens == nil {
		return
	}
	log.Printf("[InternalKnowledgeWarmup] lens=%s stage=%s awaiting import completion op=%s err=%v", req.LensID, temperature, operation, err)
	available := compactUnique(append(lensAvailableTemperatures(currentState), temperature))
	patch := map[string]any{
		"status":                internalKnowledgeStateRunning,
		"importOperation":       strings.TrimSpace(operation),
		"temperatureLevel":      strings.TrimSpace(temperature),
		"availableTemperatures": available,
		"lastError":             err.Error(),
		"lastReason":            warmupReasonCore(firstNonEmpty(req.Reason, "assembly_ready")),
		"pendingStageHash":      strings.TrimSpace(stageHash),
		"pendingStageDocCount":  docCount,
		"pendingImportSince":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.updateLensWarmState(ctx, lens, patch); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to persist pending import state lens=%s stage=%s err=%v", req.LensID, temperature, err)
	}
}

func (s *WarmupService) persistQueuedState(req WarmupRequest) {
	if s == nil || s.lenses == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.StatusWriteTimeout)
	defer cancel()
	patch := map[string]any{
		"answerStore":        strings.TrimSpace(s.cfg.AnswerStoreID),
		"lastReason":         warmupReasonCore(firstNonEmpty(req.Reason, "assembly_ready")),
		"lastQueuedAt":       time.Now().UTC().Format(time.RFC3339Nano),
		"desiredTemperature": desiredTemperatureForReason(req.Reason),
	}
	if !warmupPreservesReadyState(req.Reason) {
		patch["status"] = internalKnowledgeStateQueued
		patch["queuedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	} else {
		patch["refreshQueuedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if tenantID := strings.TrimSpace(req.TenantID); tenantID != "" {
		patch["tenantId"] = tenantID
	}
	if projectID := firstNonEmpty(req.ProjectID, s.cfg.ProjectID); projectID != "" {
		patch["projectId"] = projectID
	}
	if err := s.lenses.PatchInternalKnowledgeStateFields(ctx, req.LensID, patch); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to persist queued state lens=%s err=%v", req.LensID, err)
	}
}

func (s *WarmupService) uploadDocWithRetry(ctx context.Context, substrate *PreparedSubstrate, objectPath string, body []byte) error {
	if s == nil || s.gcs == nil || substrate == nil {
		return fmt.Errorf("warmup upload is not configured")
	}
	metadata := map[string]string{
		"type":     "internal_knowledge_capsule",
		"corpusId": substrate.CorpusID,
		"lensId":   strings.TrimSpace(substrate.Lens.ID),
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		if err := s.gcs.UploadBytes(ctx, s.cfg.AnswerBucket, objectPath, body, "application/json", metadata); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func waitForImport(ctx context.Context, client *ai.VertexAISearchClient, operation string, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("vertex client is nil")
	}
	if strings.TrimSpace(operation) == "" {
		return fmt.Errorf("vertex import operation is empty")
	}
	if timeout <= 0 {
		timeout = 6 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := client.CheckOperationStatus(ctx, operation)
		if err == nil && status != nil && status.Done {
			if status.Error != nil && status.Error.Code != 0 {
				return fmt.Errorf("vertex operation failed: %d %s", status.Error.Code, status.Error.Message)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(4 * time.Second):
		}
	}
	return fmt.Errorf("vertex import timeout after %s", timeout)
}

func shouldDeferImportCompletion(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "vertex import timeout")
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(typed), "%d", &n)
		return n
	default:
		return 0
	}
}

func filterPreparedDocsAtTemperature(docs []PreparedCorpusDoc, temperature string) []PreparedCorpusDoc {
	if len(docs) == 0 {
		return nil
	}
	targetRank := preparedTemperatureRank(temperature)
	if targetRank == 0 {
		return nil
	}
	out := make([]PreparedCorpusDoc, 0, len(docs))
	for _, doc := range docs {
		if preparedTemperatureRank(doc.Temperature) == targetRank {
			out = append(out, doc)
		}
	}
	return out
}

func (s *WarmupService) tryAdvancePendingImport(lens *hmodels.Lens) bool {
	if s == nil || s.vertex == nil || s.lenses == nil || lens == nil {
		return false
	}
	state := lensInternalKnowledgeState(lens)
	if len(state) == 0 {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(asString(state["status"])))
	if status != internalKnowledgeStateRunning && status != internalKnowledgeStateQueued {
		return false
	}
	operation := strings.TrimSpace(asString(state["importOperation"]))
	temperature := strings.TrimSpace(asString(state["temperatureLevel"]))
	stageHash := strings.TrimSpace(asString(state["pendingStageHash"]))
	if operation == "" || temperature == "" || stageHash == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	checker := s.vertex.WithDataStore(strings.TrimSpace(s.cfg.AnswerStoreID))
	statusResp, err := checker.CheckOperationStatus(ctx, operation)
	if err != nil || statusResp == nil || !statusResp.Done {
		return false
	}
	if statusResp.Error != nil && statusResp.Error.Code != 0 {
		_ = s.updateLensWarmState(ctx, lens, map[string]any{
			"status":    internalKnowledgeStateFailed,
			"failedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			"lastError": fmt.Sprintf("vertex operation failed: %d %s", statusResp.Error.Code, statusResp.Error.Message),
		})
		return true
	}
	docCount := anyInt(state["pendingStageDocCount"])
	available := compactUnique(append(lensAvailableTemperatures(state), temperature))
	readyAt := time.Now().UTC().Format(time.RFC3339Nano)
	stageState := map[string]any{
		"temperatureLevel":      strings.TrimSpace(temperature),
		"availableTemperatures": available,
		temperature + "Hash":    stageHash,
		temperature + "ReadyAt": readyAt,
		"docCount":              docCount,
		"lastError":             "",
		"pendingStageHash":      "",
		"pendingStageDocCount":  0,
		"pendingImportSince":    "",
	}
	if strings.EqualFold(strings.TrimSpace(temperature), PreparedTemperatureFull) {
		stageState["status"] = internalKnowledgeStateReady
		stageState["completedAt"] = readyAt
	} else {
		stageState["status"] = internalKnowledgeStateRunning
		stageState["runningAt"] = readyAt
	}
	if err := s.updateLensWarmState(ctx, lens, stageState); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to finalize pending import lens=%s stage=%s err=%v", lens.ID, temperature, err)
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(temperature), PreparedTemperatureFull) {
		s.EnqueueLensWarmup(context.Background(), strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ProjectID), strings.TrimSpace(lens.ID), pendingImportReason)
	}
	log.Printf("[InternalKnowledgeWarmup] finalized pending import lens=%s stage=%s op=%s", lens.ID, temperature, operation)
	return true
}

func (s *WarmupService) withGlobalWarmupLease(ctx context.Context, lensID string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if s == nil || s.db == nil {
		return fn()
	}
	lockOwner := fmt.Sprintf("internal-knowledge-%s-%d", strings.TrimSpace(lensID), time.Now().UnixNano())
	lockRef := s.db.Collection("horizon_runtime_locks").Doc("internal_knowledge_warmup")
	type lockDoc struct {
		Owner     string    `firestore:"owner"`
		ExpiresAt time.Time `firestore:"expiresAt"`
		UpdatedAt time.Time `firestore:"updatedAt"`
		LensID    string    `firestore:"lensId,omitempty"`
	}

	for {
		now := time.Now().UTC()
		acquired := false
		err := s.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			snap, err := tx.Get(lockRef)
			if err == nil && snap.Exists() {
				var current lockDoc
				if err := snap.DataTo(&current); err == nil {
					if !current.ExpiresAt.IsZero() && current.ExpiresAt.After(now) {
						return nil
					}
				}
			}
			return tx.Set(lockRef, lockDoc{
				Owner:     lockOwner,
				ExpiresAt: now.Add(s.cfg.LeaseTTL),
				UpdatedAt: now,
				LensID:    strings.TrimSpace(lensID),
			})
		})
		if err != nil {
			return err
		}
		snap, getErr := lockRef.Get(ctx)
		if getErr == nil && snap.Exists() {
			var current lockDoc
			if dataErr := snap.DataTo(&current); dataErr == nil && current.Owner == lockOwner {
				acquired = true
			}
		}
		if acquired {
			defer func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = s.db.RunTransaction(releaseCtx, func(ctx context.Context, tx *firestore.Transaction) error {
					snap, err := tx.Get(lockRef)
					if err != nil || !snap.Exists() {
						return nil
					}
					var current lockDoc
					if err := snap.DataTo(&current); err != nil {
						return nil
					}
					if current.Owner != lockOwner {
						return nil
					}
					current.ExpiresAt = time.Now().UTC()
					current.UpdatedAt = time.Now().UTC()
					return tx.Set(lockRef, current)
				})
			}()
			return fn()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cfg.LeaseAcquireInterval):
		}
	}
}

func lensInternalKnowledgeState(lens *hmodels.Lens) map[string]any {
	if lens == nil || lens.ViewConfig == nil {
		return map[string]any{}
	}
	raw, ok := lens.ViewConfig[internalKnowledgeViewConfigKey]
	if !ok || raw == nil {
		return map[string]any{}
	}
	if value, ok := raw.(map[string]any); ok && value != nil {
		return value
	}
	if value, ok := raw.(map[string]interface{}); ok && value != nil {
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = v
		}
		return out
	}
	return map[string]any{}
}

func lensAssemblyReady(lens *hmodels.Lens) bool {
	if lens == nil || lens.ViewConfig == nil {
		return false
	}
	raw, ok := lens.ViewConfig["assemblyStatus"]
	if !ok || raw == nil {
		return false
	}
	state, ok := raw.(map[string]any)
	if !ok {
		if typed, ok2 := raw.(map[string]interface{}); ok2 {
			state = make(map[string]any, len(typed))
			for k, v := range typed {
				state[k] = v
			}
		}
	}
	return strings.EqualFold(strings.TrimSpace(asString(state["phase"])), "ready")
}

func lensWarmupEligible(lens *hmodels.Lens) bool {
	if lensAssemblyReady(lens) {
		return true
	}
	state := lensInternalKnowledgeState(lens)
	if len(state) == 0 {
		return false
	}
	if strings.TrimSpace(asString(state["status"])) != "" {
		return true
	}
	if strings.TrimSpace(asString(state["corpusId"])) != "" {
		return true
	}
	if len(lensAvailableTemperatures(state)) > 0 {
		return true
	}
	for _, temperature := range internalKnowledgeTemperatureOrder {
		if _, ok := parseRFC3339Time(state[temperature+"ReadyAt"]); ok {
			return true
		}
	}
	return false
}

func normalizeWarmStateForTransition(current map[string]any, incoming map[string]any) {
	if current == nil || incoming == nil {
		return
	}
	status := strings.TrimSpace(strings.ToLower(asString(incoming["status"])))
	clearKeys := func(keys ...string) {
		for _, key := range keys {
			delete(current, key)
		}
	}
	switch status {
	case internalKnowledgeStateQueued:
		clearKeys("runningAt", "completedAt", "failedAt", "lastError", "importOperation")
	case internalKnowledgeStateRunning:
		clearKeys("completedAt", "failedAt", "lastError")
	case internalKnowledgeStateReady:
		clearKeys("failedAt")
	case internalKnowledgeStateFailed:
		clearKeys("completedAt")
	}
}

func lensAvailableTemperatures(state map[string]any) []string {
	raw, ok := state["availableTemperatures"]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return compactUnique(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(asString(item)); value != "" {
				out = append(out, value)
			}
		}
		return compactUnique(out)
	default:
		return nil
	}
}

func lensTemperatureReady(state map[string]any, temperature string) bool {
	temperature = strings.TrimSpace(strings.ToLower(temperature))
	if temperature == "" {
		return false
	}
	for _, available := range lensAvailableTemperatures(state) {
		if preparedTemperatureRank(available) >= preparedTemperatureRank(temperature) {
			return true
		}
	}
	_, ok := parseRFC3339Time(state[temperature+"ReadyAt"])
	return ok
}

func stageStatusForTemperature(temperature string) string {
	if strings.EqualFold(strings.TrimSpace(temperature), PreparedTemperatureFull) {
		return internalKnowledgeStateReady
	}
	return internalKnowledgeStateRunning
}

func desiredTemperatureForReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(warmupReasonCore(reason)))
	switch {
	case strings.Contains(reason, "risk"), strings.Contains(reason, "weak"), strings.Contains(reason, "bottleneck"), strings.Contains(reason, "watch"), strings.Contains(reason, "fragment"):
		return PreparedTemperatureHot
	case strings.Contains(reason, "supply"), strings.Contains(reason, "route"), strings.Contains(reason, "supplier"):
		return PreparedTemperatureWarm
	default:
		return PreparedTemperatureFull
	}
}

func normalizeWarmupReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	if warmupPreservesReadyState(reason) {
		return internalKnowledgeRefreshPrefix + warmupReasonCore(reason)
	}
	return reason
}

func warmupPreservesReadyState(reason string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(reason)), internalKnowledgeRefreshPrefix)
}

func warmupReasonCore(reason string) string {
	reason = strings.TrimSpace(reason)
	if strings.HasPrefix(strings.ToLower(reason), internalKnowledgeRefreshPrefix) {
		return strings.TrimSpace(reason[len(internalKnowledgeRefreshPrefix):])
	}
	return reason
}

func warmupIsReconcileReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(warmupReasonCore(reason)))
	return strings.Contains(reason, "reconcile")
}

func parseRFC3339Time(value any) (time.Time, bool) {
	raw := strings.TrimSpace(asString(value))
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
