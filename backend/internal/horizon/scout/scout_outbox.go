package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

const (
	stateKeyScoutOutbox       = "scoutOutbox"
	stateKeyScoutOutboxStatus = "scoutOutboxStatus"
	stateKeyResearchDebtQueue = "researchDebtQueue"

	outboxStatusPending   = "pending"
	outboxStatusDelivered = "delivered"
	outboxStatusFailed    = "failed"

	scoutOutboxTypeWSEvent       = "ws_event"
	scoutOutboxTypePolicyAudit   = "policy_audit"
	scoutOutboxTypeAssemblyCheck = "assembly_check"
	scoutOutboxTypeResearchDebt  = "research_debt_queue"
)

const (
	scoutOutboxMaxAttempts = 8
)

type scoutOutboxIntent struct {
	Key          string                 `json:"key"`
	Type         string                 `json:"type"`
	Status       string                 `json:"status"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
	Attempts     int                    `json:"attempts"`
	MaxAttempts  int                    `json:"maxAttempts"`
	LastError    string                 `json:"lastError,omitempty"`
	CreatedAt    string                 `json:"createdAt"`
	UpdatedAt    string                 `json:"updatedAt"`
	NextAttempt  string                 `json:"nextAttemptAt,omitempty"`
	DeliveredAt  string                 `json:"deliveredAt,omitempty"`
	Retryable    bool                   `json:"retryable"`
	Idempotency  string                 `json:"idempotencyKey,omitempty"`
	JobID        string                 `json:"jobId,omitempty"`
	TenantID     string                 `json:"tenantId,omitempty"`
	PipelineHint string                 `json:"pipelineHint,omitempty"`
}

func scoutOutboxBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := 2 * time.Second
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff > 2*time.Minute {
			return 2 * time.Minute
		}
	}
	return backoff
}

func normalizeOutboxPayload(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func computeOutboxIntentKey(jobID, intentType string, payload map[string]interface{}) string {
	material := map[string]interface{}{
		"jobId": jobID,
		"type":  strings.TrimSpace(intentType),
		"p":     normalizeOutboxPayload(payload),
	}
	b, _ := json.Marshal(material)
	h := hashStringDeterministic(string(b))
	return strings.TrimSpace(jobID) + ":" + strings.TrimSpace(intentType) + ":" + h
}

func hashStringDeterministic(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	// Reuse short deterministic hash style already used in repo conventions.
	return fmt.Sprintf("%x", sumBytes([]byte(strings.TrimSpace(s))))
}

func sumBytes(b []byte) [20]byte {
	// Small local helper to avoid extra dependency churn.
	var out [20]byte
	h := uint32(2166136261)
	for _, x := range b {
		h ^= uint32(x)
		h *= 16777619
	}
	for i := 0; i < len(out); i++ {
		out[i] = byte((h >> (uint(i%4) * 8)) & 0xff)
		h = h*1103515245 + 12345
	}
	return out
}

func parseScoutOutbox(state map[string]interface{}) map[string]*scoutOutboxIntent {
	out := map[string]*scoutOutboxIntent{}
	if state == nil {
		return out
	}
	raw, ok := state[stateKeyScoutOutbox]
	if !ok || raw == nil {
		return out
	}
	decodeIntent := func(key string, rawIntent interface{}) *scoutOutboxIntent {
		if rawIntent == nil {
			return nil
		}
		switch t := rawIntent.(type) {
		case *scoutOutboxIntent:
			if t == nil {
				return nil
			}
			cp := *t
			if strings.TrimSpace(cp.Key) == "" {
				cp.Key = strings.TrimSpace(key)
			}
			if cp.Payload == nil {
				cp.Payload = map[string]interface{}{}
			}
			return &cp
		case scoutOutboxIntent:
			cp := t
			if strings.TrimSpace(cp.Key) == "" {
				cp.Key = strings.TrimSpace(key)
			}
			if cp.Payload == nil {
				cp.Payload = map[string]interface{}{}
			}
			return &cp
		case map[string]interface{}:
			b, err := json.Marshal(t)
			if err != nil {
				return nil
			}
			var it scoutOutboxIntent
			if err := json.Unmarshal(b, &it); err != nil {
				return nil
			}
			if strings.TrimSpace(it.Key) == "" {
				it.Key = strings.TrimSpace(key)
			}
			if it.Payload == nil {
				it.Payload = map[string]interface{}{}
			}
			return &it
		default:
			return nil
		}
	}
	switch t := raw.(type) {
	case map[string]*scoutOutboxIntent:
		for key, intent := range t {
			if parsed := decodeIntent(key, intent); parsed != nil {
				out[strings.TrimSpace(key)] = parsed
			}
		}
	case map[string]interface{}:
		for key, intent := range t {
			if parsed := decodeIntent(key, intent); parsed != nil {
				out[strings.TrimSpace(key)] = parsed
			}
		}
	}
	return out
}

func marshalScoutOutbox(outbox map[string]*scoutOutboxIntent) map[string]interface{} {
	serialized := map[string]interface{}{}
	for key, intent := range outbox {
		if intent == nil {
			continue
		}
		serialized[strings.TrimSpace(key)] = map[string]interface{}{
			"key":            strings.TrimSpace(intent.Key),
			"type":           strings.TrimSpace(intent.Type),
			"status":         strings.TrimSpace(intent.Status),
			"payload":        normalizeOutboxPayload(intent.Payload),
			"attempts":       float64(intent.Attempts),
			"maxAttempts":    float64(intent.MaxAttempts),
			"lastError":      strings.TrimSpace(intent.LastError),
			"createdAt":      strings.TrimSpace(intent.CreatedAt),
			"updatedAt":      strings.TrimSpace(intent.UpdatedAt),
			"nextAttemptAt":  strings.TrimSpace(intent.NextAttempt),
			"deliveredAt":    strings.TrimSpace(intent.DeliveredAt),
			"retryable":      intent.Retryable,
			"idempotencyKey": strings.TrimSpace(intent.Idempotency),
			"jobId":          strings.TrimSpace(intent.JobID),
			"tenantId":       strings.TrimSpace(intent.TenantID),
			"pipelineHint":   strings.TrimSpace(intent.PipelineHint),
		}
	}
	return serialized
}

func (e *ScoutEngine) enqueueScoutOutboxIntent(ctx context.Context, job *models.ScoutingJob, intentType string, payload map[string]interface{}) (string, error) {
	if e == nil || job == nil || e.scoutingJobsRepo == nil {
		return "", fmt.Errorf("outbox enqueue: engine/job/repo missing")
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	intentType = strings.TrimSpace(intentType)
	if intentType == "" {
		return "", fmt.Errorf("outbox enqueue: intent type required")
	}
	payload = normalizeOutboxPayload(payload)
	intentKey := computeOutboxIntentKey(strings.TrimSpace(job.ID), intentType, payload)
	if strings.TrimSpace(intentKey) == "" {
		return "", fmt.Errorf("outbox enqueue: failed to build idempotency key")
	}
	outbox := parseScoutOutbox(job.State)
	if existing, ok := outbox[intentKey]; ok && existing != nil {
		status := strings.TrimSpace(existing.Status)
		if status == outboxStatusDelivered || status == outboxStatusPending {
			return intentKey, nil
		}
	}
	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	intent := &scoutOutboxIntent{
		Key:         intentKey,
		Type:        intentType,
		Status:      outboxStatusPending,
		Payload:     payload,
		Attempts:    0,
		MaxAttempts: scoutOutboxMaxAttempts,
		CreatedAt:   nowISO,
		UpdatedAt:   nowISO,
		Retryable:   true,
		Idempotency: intentKey,
		JobID:       strings.TrimSpace(job.ID),
		TenantID:    strings.TrimSpace(job.TenantID),
	}
	outbox[intentKey] = intent
	job.State[stateKeyScoutOutbox] = marshalScoutOutbox(outbox)
	job.State[stateKeyScoutOutboxStatus] = outboxStatusPending
	if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
		stateKeyScoutOutbox:       job.State[stateKeyScoutOutbox],
		stateKeyScoutOutboxStatus: outboxStatusPending,
	}); err != nil {
		return "", fmt.Errorf("outbox enqueue persist failed: %w", err)
	}
	return intentKey, nil
}

func (e *ScoutEngine) triggerScoutOutboxDispatch(job *models.ScoutingJob) {
	if e == nil || job == nil {
		return
	}
	jobID := strings.TrimSpace(job.ID)
	if jobID == "" {
		return
	}
	e.scoutOutboxMu.Lock()
	if e.scoutOutboxInFlight == nil {
		e.scoutOutboxInFlight = map[string]struct{}{}
	}
	if _, busy := e.scoutOutboxInFlight[jobID]; busy {
		e.scoutOutboxMu.Unlock()
		return
	}
	e.scoutOutboxInFlight[jobID] = struct{}{}
	e.scoutOutboxMu.Unlock()

	go func(jobID string) {
		defer func() {
			e.scoutOutboxMu.Lock()
			delete(e.scoutOutboxInFlight, jobID)
			e.scoutOutboxMu.Unlock()
		}()
		for i := 0; i < 16; i++ {
			done, err := e.dispatchScoutOutboxForJob(context.Background(), jobID)
			if done {
				return
			}
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			time.Sleep(1 * time.Second)
		}
	}(jobID)
}

func (e *ScoutEngine) dispatchScoutOutboxForJob(ctx context.Context, jobID string) (bool, error) {
	if e == nil || e.scoutingJobsRepo == nil {
		return true, nil
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return true, nil
	}
	job, err := e.scoutingJobsRepo.Get(ctx, jobID)
	if err != nil || job == nil {
		return false, err
	}
	if job.State == nil {
		return true, nil
	}
	outbox := parseScoutOutbox(job.State)
	if len(outbox) == 0 {
		if parseStateString(job.State[stateKeyScoutOutboxStatus]) != outboxStatusDelivered {
			_ = e.scoutingJobsRepo.UpdateState(ctx, jobID, map[string]interface{}{stateKeyScoutOutboxStatus: outboxStatusDelivered})
		}
		return true, nil
	}

	keys := make([]string, 0, len(outbox))
	for key := range outbox {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	now := time.Now().UTC()
	pending := false
	for _, key := range keys {
		intent := outbox[key]
		if intent == nil {
			continue
		}
		status := strings.TrimSpace(intent.Status)
		if status == outboxStatusDelivered {
			continue
		}
		nextAttemptAt := parseStateString(intent.NextAttempt)
		if nextAttemptAt != "" {
			if t, perr := time.Parse(time.RFC3339Nano, nextAttemptAt); perr == nil && t.After(now) {
				pending = true
				continue
			}
		}
		intent.Attempts++
		intent.UpdatedAt = now.Format(time.RFC3339Nano)
		deliverErr := e.deliverScoutOutboxIntent(ctx, job, intent)
		if deliverErr == nil {
			intent.Status = outboxStatusDelivered
			intent.DeliveredAt = now.Format(time.RFC3339Nano)
			intent.LastError = ""
			intent.NextAttempt = ""
			outbox[key] = intent
			continue
		}
		intent.LastError = strings.TrimSpace(deliverErr.Error())
		if intent.Attempts >= maxInt(1, intent.MaxAttempts) {
			intent.Status = outboxStatusFailed
			intent.Retryable = false
			intent.NextAttempt = ""
			outbox[key] = intent
			continue
		}
		intent.Status = outboxStatusPending
		intent.Retryable = true
		intent.NextAttempt = now.Add(scoutOutboxBackoff(intent.Attempts)).Format(time.RFC3339Nano)
		outbox[key] = intent
		pending = true
	}

	status := outboxStatusDelivered
	for _, intent := range outbox {
		if intent == nil {
			continue
		}
		if intent.Status == outboxStatusPending {
			status = outboxStatusPending
			break
		}
		if intent.Status == outboxStatusFailed {
			status = outboxStatusFailed
		}
	}

	serialized := marshalScoutOutbox(outbox)
	job.State[stateKeyScoutOutbox] = serialized
	job.State[stateKeyScoutOutboxStatus] = status
	if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
		stateKeyScoutOutbox:       serialized,
		stateKeyScoutOutboxStatus: status,
	}); err != nil {
		return false, err
	}
	if status == outboxStatusDelivered {
		return true, nil
	}
	if status == outboxStatusFailed {
		return true, fmt.Errorf("scout outbox delivery failed permanently")
	}
	return !pending, nil
}

func (e *ScoutEngine) deliverScoutOutboxIntent(ctx context.Context, job *models.ScoutingJob, intent *scoutOutboxIntent) error {
	if e == nil || intent == nil || job == nil {
		return nil
	}
	switch strings.TrimSpace(intent.Type) {
	case scoutOutboxTypeWSEvent:
		eventType := strings.TrimSpace(parseStateString(intent.Payload["eventType"]))
		tenantID := strings.TrimSpace(parseStateString(intent.Payload["tenantId"]))
		payload := map[string]interface{}{}
		if p, ok := intent.Payload["payload"].(map[string]interface{}); ok && p != nil {
			payload = p
		}
		if eventType == "" {
			return fmt.Errorf("ws_event missing eventType")
		}
		if tenantID == "" {
			tenantID = strings.TrimSpace(job.TenantID)
		}
		if e.realtime == nil {
			return fmt.Errorf("realtime publisher not configured")
		}
		return e.realtime.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)

	case scoutOutboxTypePolicyAudit:
		e.runPolicyAudit(ctx, job)
		return nil

	case scoutOutboxTypeAssemblyCheck:
		tenantID := strings.TrimSpace(parseStateString(intent.Payload["tenantId"]))
		projectID := strings.TrimSpace(parseStateString(intent.Payload["projectId"]))
		lensID := strings.TrimSpace(parseStateString(intent.Payload["lensId"]))
		if tenantID == "" {
			tenantID = strings.TrimSpace(job.TenantID)
		}
		if projectID == "" {
			projectID = strings.TrimSpace(job.ProjectID)
		}
		if lensID == "" {
			lensID = strings.TrimSpace(job.LensID)
		}
		e.checkAndPublishAssemblyCompletion(ctx, tenantID, projectID, lensID)
		return nil

	case scoutOutboxTypeResearchDebt:
		if e.scoutingJobsRepo == nil {
			return fmt.Errorf("research debt delivery: scoutingJobsRepo is nil")
		}
		entry := normalizeOutboxPayload(intent.Payload)
		entry["queuedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		entry["intentKey"] = strings.TrimSpace(intent.Key)
		queue := parseStateSliceMap(job.State[stateKeyResearchDebtQueue])
		entryKey := strings.TrimSpace(parseStateString(entry["intentKey"]))
		if entryKey == "" {
			entryKey = strings.TrimSpace(intent.Key)
		}
		alreadyQueued := false
		for _, existing := range queue {
			if strings.TrimSpace(parseStateString(existing["intentKey"])) == entryKey {
				alreadyQueued = true
				break
			}
		}
		if !alreadyQueued {
			queue = append(queue, entry)
		}
		job.State[stateKeyResearchDebtQueue] = queue
		return e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
			stateKeyResearchDebtQueue: queue,
		})

	default:
		return fmt.Errorf("unknown scout outbox intent type %q", strings.TrimSpace(intent.Type))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func scoutOutboxIntentStatus(job *models.ScoutingJob, intentKey string) string {
	if job == nil || job.State == nil || strings.TrimSpace(intentKey) == "" {
		return ""
	}
	outbox := parseScoutOutbox(job.State)
	intent, ok := outbox[strings.TrimSpace(intentKey)]
	if !ok || intent == nil {
		return ""
	}
	return strings.TrimSpace(intent.Status)
}

func (e *ScoutEngine) waitScoutOutboxIntent(ctx context.Context, jobID, intentKey string, rounds int) error {
	if e == nil || e.scoutingJobsRepo == nil {
		return fmt.Errorf("outbox wait: engine/repo missing")
	}
	jobID = strings.TrimSpace(jobID)
	intentKey = strings.TrimSpace(intentKey)
	if jobID == "" || intentKey == "" {
		return fmt.Errorf("outbox wait: jobId and intentKey required")
	}
	if rounds < 1 {
		rounds = 1
	}
	var lastErr error
	for i := 0; i < rounds; i++ {
		_, dispatchErr := e.dispatchScoutOutboxForJob(ctx, jobID)
		if dispatchErr != nil {
			lastErr = dispatchErr
		}
		job, getErr := e.scoutingJobsRepo.Get(ctx, jobID)
		if getErr != nil {
			lastErr = getErr
		} else {
			switch scoutOutboxIntentStatus(job, intentKey) {
			case outboxStatusDelivered:
				return nil
			case outboxStatusFailed:
				return fmt.Errorf("outbox intent %s failed permanently", intentKey)
			}
		}
		if i+1 < rounds {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("outbox intent %s not delivered in %d rounds", intentKey, rounds)
}

func (e *ScoutEngine) enqueuePolicyAuditOutboxIntent(ctx context.Context, job *models.ScoutingJob, pipelineName string) error {
	if e == nil || job == nil {
		return fmt.Errorf("outbox policy audit: engine/job missing")
	}
	payload := map[string]interface{}{
		"tenantId":     strings.TrimSpace(job.TenantID),
		"projectId":    strings.TrimSpace(job.ProjectID),
		"lensId":       strings.TrimSpace(job.LensID),
		"jobId":        strings.TrimSpace(job.ID),
		"pipelineHint": strings.TrimSpace(pipelineName),
	}
	intentKey, err := e.enqueueScoutOutboxIntent(ctx, job, scoutOutboxTypePolicyAudit, payload)
	if err != nil {
		return err
	}
	if err := e.waitScoutOutboxIntent(ctx, strings.TrimSpace(job.ID), intentKey, 6); err != nil {
		return err
	}
	return nil
}

func (e *ScoutEngine) enqueueResearchDebtOutboxIntent(ctx context.Context, job *models.ScoutingJob, payload map[string]interface{}) error {
	if e == nil || job == nil {
		return fmt.Errorf("research debt enqueue: engine/job missing")
	}
	payload = normalizeOutboxPayload(payload)
	payload["tenantId"] = strings.TrimSpace(job.TenantID)
	payload["projectId"] = strings.TrimSpace(job.ProjectID)
	payload["lensId"] = strings.TrimSpace(job.LensID)
	payload["jobId"] = strings.TrimSpace(job.ID)
	if _, err := e.enqueueScoutOutboxIntent(ctx, job, scoutOutboxTypeResearchDebt, payload); err != nil {
		return err
	}
	e.triggerScoutOutboxDispatch(job)
	return nil
}

func parseStateSliceMap(raw interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0)
	switch t := raw.(type) {
	case []map[string]interface{}:
		for _, item := range t {
			if item == nil {
				continue
			}
			out = append(out, normalizeOutboxPayload(item))
		}
	case []interface{}:
		for _, item := range t {
			m, _ := item.(map[string]interface{})
			if m == nil {
				continue
			}
			out = append(out, normalizeOutboxPayload(m))
		}
	}
	return out
}
