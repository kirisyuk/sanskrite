package ai

import (
	"context"
	"sync"
	"time"
)

// AdmissionController limits concurrent requests and token usage per provider/cascade.
// When limits are exceeded, Allow returns false so callers can return retryable errors.
type AdmissionController interface {
	Allow(ctx context.Context, provider AIProvider, tenantID, cascadeID string) (bool, time.Duration)
	RecordUsage(ctx context.Context, provider AIProvider, tenantID, cascadeID string, tokens int)
}

// admissionNoop is a no-op implementation that always allows.
type admissionNoop struct{}

func (a *admissionNoop) Allow(_ context.Context, _ AIProvider, _, _ string) (bool, time.Duration) {
	return true, 0
}

func (a *admissionNoop) RecordUsage(_ context.Context, _ AIProvider, _, _ string, _ int) {}

// NoopAdmissionController returns an admission controller that always allows.
func NoopAdmissionController() AdmissionController {
	return &admissionNoop{}
}

// InMemoryAdmissionController implements admission control with in-memory sliding-window counters.
type InMemoryAdmissionController struct {
	mu sync.Mutex

	maxTokensPerCascadeMin int
	windowDuration         time.Duration

	tokens    map[string]int64 // key: provider|tenantID|cascadeID
	windowEnd map[string]time.Time
}

// NewInMemoryAdmissionController creates an admission controller with the given limits.
// maxTokensPerCascadeMin: max tokens per (provider, tenant, cascade) per minute (0 = no limit).
func NewInMemoryAdmissionController(maxTokensPerCascadeMin int) *InMemoryAdmissionController {
	return &InMemoryAdmissionController{
		maxTokensPerCascadeMin: maxTokensPerCascadeMin,
		windowDuration:        time.Minute,
		tokens:                make(map[string]int64),
		windowEnd:             make(map[string]time.Time),
	}
}

func (a *InMemoryAdmissionController) key(provider AIProvider, tenantID, cascadeID string) string {
	t := tenantID
	if t == "" {
		t = "_"
	}
	c := cascadeID
	if c == "" {
		c = "_"
	}
	return string(provider) + "|" + t + "|" + c
}

func (a *InMemoryAdmissionController) Allow(ctx context.Context, provider AIProvider, tenantID, cascadeID string) (bool, time.Duration) {
	if ctx != nil && ctx.Err() != nil {
		return false, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now().UTC()

	if a.maxTokensPerCascadeMin > 0 {
		k := a.key(provider, tenantID, cascadeID)
		if end, ok := a.windowEnd[k]; ok && now.Before(end) {
			if a.tokens[k] >= int64(a.maxTokensPerCascadeMin) {
				return false, time.Until(end)
			}
		}
	}

	return true, 0
}

func (a *InMemoryAdmissionController) RecordUsage(ctx context.Context, provider AIProvider, tenantID, cascadeID string, tokens int) {
	if tokens <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now().UTC()

	if a.maxTokensPerCascadeMin > 0 {
		k := a.key(provider, tenantID, cascadeID)
		if end, ok := a.windowEnd[k]; !ok || now.After(end) {
			a.windowEnd[k] = now.Add(a.windowDuration)
			a.tokens[k] = 0
		}
		a.tokens[k] += int64(tokens)
	}
}
