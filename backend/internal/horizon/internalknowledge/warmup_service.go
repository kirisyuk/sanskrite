package internalknowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
)

type WarmupConfig struct {
	ProjectID                 string
	AnswerBucket              string
	AnswerStoreID             string
	QueueSize                 int
	UploadsPerSecond          float64
	ImportsPerMinute          int
	ProcessTimeout            time.Duration
	ImportWaitTimeout         time.Duration
	StatusWriteTimeout        time.Duration
	LeaseTTL                  time.Duration
	LeaseAcquireInterval      time.Duration
	ReconcileInterval         time.Duration
	ReconcileLimit            int
	StaleQueuedAfter          time.Duration
	StaleRunningAfter         time.Duration
	RetryFailedAfter          time.Duration
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
		cfg.ImportsPerMinute = 1
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
		cfg.ReconcileLimit = 200
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
		importLimiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(cfg.ImportsPerMinute)), 1),
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
		Reason:    strings.TrimSpace(reason),
	}
	s.pendingMu.Lock()
	if _, exists := s.pendingLensID[lensID]; exists {
		s.pendingMu.Unlock()
		return
	}
	s.pendingLensID[lensID] = struct{}{}
	s.pendingMu.Unlock()
	s.persistQueuedState(req)
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
			"status":         internalKnowledgeStateReady,
			"scopeHash":      scopeHash,
			"substrateHash":  substrateHash,
			"corpusId":       corpusID,
			"answerStore":    strings.TrimSpace(s.cfg.AnswerStoreID),
			"docCount":       len(substrate.Docs),
			"lastSkippedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			"lastReason":     firstNonEmpty(req.Reason, "assembly_ready"),
		}); err != nil {
			log.Printf("[InternalKnowledgeWarmup] failed to persist skip-ready state lens=%s err=%v", req.LensID, err)
		}
		return
	}

	if err := s.updateLensWarmState(ctx, lens, map[string]any{
		"status":         internalKnowledgeStateRunning,
		"runningAt":      time.Now().UTC().Format(time.RFC3339Nano),
		"scopeHash":      scopeHash,
		"substrateHash":  substrateHash,
		"corpusId":       corpusID,
		"answerStore":    strings.TrimSpace(s.cfg.AnswerStoreID),
		"docCount":       len(substrate.Docs),
		"projectId":      firstNonEmpty(req.ProjectID, s.cfg.ProjectID),
		"tenantId":       firstNonEmpty(req.TenantID, lens.TenantID),
		"lastReason":     firstNonEmpty(req.Reason, "assembly_ready"),
	}); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to persist running state lens=%s err=%v", req.LensID, err)
	}

	opName := ""
	if err := s.withGlobalWarmupLease(ctx, req.LensID, func() error {
		uris, err := s.exportDocs(ctx, substrate)
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
			"status":          internalKnowledgeStateRunning,
			"importOperation": strings.TrimSpace(opName),
			"lastReason":      firstNonEmpty(req.Reason, "assembly_ready"),
		}); persistErr != nil {
			log.Printf("[InternalKnowledgeWarmup] failed to persist import operation lens=%s err=%v", req.LensID, persistErr)
		}
		return waitForImport(ctx, s.vertex.WithDataStore(s.cfg.AnswerStoreID), opName, s.cfg.ImportWaitTimeout)
	}); err != nil {
		s.failLensWarmState(req, lens, err)
		return
	}

	// IMPORTANT: when lens scope expands we should stop doing full corpus rebuilds here.
	// The stored scopeHash/corpusId state is intentionally persisted so the next iteration can
	// compute the added scope delta and warm only the newly added slice of the lens substrate.
	if err := s.updateLensWarmState(ctx, lens, map[string]any{
		"status":         internalKnowledgeStateReady,
		"completedAt":    time.Now().UTC().Format(time.RFC3339Nano),
		"scopeHash":      scopeHash,
		"substrateHash":  substrateHash,
		"corpusId":       corpusID,
		"answerStore":    strings.TrimSpace(s.cfg.AnswerStoreID),
		"docCount":       len(substrate.Docs),
		"importOperation": strings.TrimSpace(opName),
		"lastError":      "",
		"lastReason":     firstNonEmpty(req.Reason, "assembly_ready"),
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
		reason, ok := s.shouldReconcileLens(&lens, now)
		if !ok {
			continue
		}
		s.EnqueueLensWarmup(context.Background(), strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ProjectID), strings.TrimSpace(lens.ID), reason)
	}
}

func (s *WarmupService) shouldReconcileLens(lens *hmodels.Lens, now time.Time) (string, bool) {
	if lens == nil || !lensAssemblyReady(lens) {
		return "", false
	}
	state := lensInternalKnowledgeState(lens)
	status := strings.TrimSpace(strings.ToLower(asString(state["status"])))
	switch status {
	case "":
		return "assembly_ready_reconcile", true
	case internalKnowledgeStateQueued:
		if ts, ok := parseRFC3339Time(state["queuedAt"]); !ok || now.Sub(ts) >= s.cfg.StaleQueuedAfter {
			return "queued_reconcile", true
		}
	case internalKnowledgeStateRunning:
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
		if err := s.gcs.UploadBytes(ctx, s.cfg.AnswerBucket, objectPath, body, "application/json", map[string]string{
			"type":     "internal_knowledge_capsule",
			"corpusId": substrate.CorpusID,
			"lensId":   strings.TrimSpace(substrate.Lens.ID),
		}); err != nil {
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
	fresh, err := s.lenses.Get(statusCtx, lens.ID)
	if err != nil || fresh == nil {
		return err
	}
	if fresh.ViewConfig == nil {
		fresh.ViewConfig = map[string]any{}
	}
	current := lensInternalKnowledgeState(fresh)
	normalizeWarmStateForTransition(current, state)
	for key, value := range state {
		current[strings.TrimSpace(key)] = value
	}
	fresh.ViewConfig[internalKnowledgeViewConfigKey] = current
	if err := s.lenses.Update(statusCtx, fresh); err != nil {
		return err
	}
	lens.ViewConfig = fresh.ViewConfig
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
		"status":      internalKnowledgeStateFailed,
		"failedAt":    time.Now().UTC().Format(time.RFC3339Nano),
		"lastError":   err.Error(),
		"lastReason":  firstNonEmpty(req.Reason, "assembly_ready"),
	})
}

func (s *WarmupService) persistQueuedState(req WarmupRequest) {
	if s == nil || s.lenses == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.StatusWriteTimeout)
	defer cancel()
	lens, err := s.lenses.Get(ctx, req.LensID)
	if err != nil || lens == nil {
		log.Printf("[InternalKnowledgeWarmup] failed to load lens for queued state lens=%s err=%v", req.LensID, err)
		return
	}
	if req.TenantID == "" {
		req.TenantID = strings.TrimSpace(lens.TenantID)
	}
	if req.ProjectID == "" {
		req.ProjectID = strings.TrimSpace(lens.ProjectID)
	}
	if err := s.updateLensWarmState(ctx, lens, map[string]any{
		"status":       internalKnowledgeStateQueued,
		"queuedAt":     time.Now().UTC().Format(time.RFC3339Nano),
		"projectId":    firstNonEmpty(req.ProjectID, s.cfg.ProjectID),
		"tenantId":     firstNonEmpty(req.TenantID, lens.TenantID),
		"answerStore":  strings.TrimSpace(s.cfg.AnswerStoreID),
		"lastReason":   firstNonEmpty(req.Reason, "assembly_ready"),
		"lastQueuedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		log.Printf("[InternalKnowledgeWarmup] failed to persist queued state lens=%s err=%v", req.LensID, err)
	}
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
		if err == nil {
			snap, getErr := lockRef.Get(ctx)
			if getErr == nil && snap.Exists() {
				var current lockDoc
				if dataErr := snap.DataTo(&current); dataErr == nil && current.Owner == lockOwner {
					acquired = true
				}
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
