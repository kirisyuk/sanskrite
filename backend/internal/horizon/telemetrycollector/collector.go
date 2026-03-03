package telemetrycollector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type contextKey struct{}

// Collector aggregates telemetry events (LLM calls, external API calls,
// Firestore ops) for a single job/request lifecycle. It is goroutine-safe.
type Collector struct {
	mu sync.Mutex

	TenantID  string
	ProjectID string
	LensID    string
	JobID     string
	JobType   string
	Trigger   string
	StartedAt time.Time

	groups   []GroupEntry
	llmCalls []LLMCallRecord
	apiCalls []ExternalAPICallRecord
	fsOps    []FirestoreOpRecord
}

// GroupEntry tracks a named telemetry phase.
type GroupEntry struct {
	Name      string
	StartedAt time.Time
	EndedAt   time.Time
}

// LLMCallRecord captures a single LLM provider invocation.
type LLMCallRecord struct {
	Group        string
	Provider     string
	Model        string
	ModelAlias   string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	DurationMs   int
	Cacheable    bool
	Timestamp    time.Time
}

// ExternalAPICallRecord captures a call to an external service (Wikidata, GeoNames, etc.)
type ExternalAPICallRecord struct {
	Group      string
	Name       string
	Provider   string
	DurationMs int
	CacheHit   bool
	Timestamp  time.Time
}

// FirestoreOpRecord captures a Firestore database operation.
type FirestoreOpRecord struct {
	Group      string
	Collection string
	Op         string
	Count      int
	Timestamp  time.Time
}

// New creates a Collector for the given job context.
func New(tenantID, projectID, lensID, jobID, jobType, trigger string) *Collector {
	return &Collector{
		TenantID:  tenantID,
		ProjectID: projectID,
		LensID:    lensID,
		JobID:     jobID,
		JobType:   jobType,
		Trigger:   trigger,
		StartedAt: time.Now(),
	}
}

// WithCollector stores the collector in the context.
func WithCollector(ctx context.Context, c *Collector) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// CollectorFromContext retrieves the collector from the context, or nil.
func CollectorFromContext(ctx context.Context) *Collector {
	if c, ok := ctx.Value(contextKey{}).(*Collector); ok {
		return c
	}
	return nil
}

func (c *Collector) currentGroup() string {
	if len(c.groups) == 0 {
		return ""
	}
	last := c.groups[len(c.groups)-1]
	if !last.EndedAt.IsZero() {
		return ""
	}
	return last.Name
}

// BeginGroup starts a named telemetry group (e.g. "planning", "research").
func (c *Collector) BeginGroup(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groups = append(c.groups, GroupEntry{Name: name, StartedAt: time.Now()})
}

// EndGroup closes the current group.
func (c *Collector) EndGroup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.groups) - 1; i >= 0; i-- {
		if c.groups[i].EndedAt.IsZero() {
			c.groups[i].EndedAt = time.Now()
			return
		}
	}
}

// RecordLLMCall records a single LLM provider call.
func (c *Collector) RecordLLMCall(provider, model, modelAlias string, inputTokens, outputTokens int, costUSD float64, durationMs int, cacheable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.llmCalls = append(c.llmCalls, LLMCallRecord{
		Group:        c.currentGroup(),
		Provider:     provider,
		Model:        model,
		ModelAlias:   modelAlias,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostUSD:      costUSD,
		DurationMs:   durationMs,
		Cacheable:    cacheable,
		Timestamp:    time.Now(),
	})
}

// RecordExternalAPICall records a call to an external API service.
func (c *Collector) RecordExternalAPICall(name, provider string, durationMs int, cacheHit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiCalls = append(c.apiCalls, ExternalAPICallRecord{
		Group:      c.currentGroup(),
		Name:       name,
		Provider:   provider,
		DurationMs: durationMs,
		CacheHit:   cacheHit,
		Timestamp:  time.Now(),
	})
}

// RecordFirestoreOp records a Firestore database operation.
func (c *Collector) RecordFirestoreOp(collection, op string, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fsOps = append(c.fsOps, FirestoreOpRecord{
		Group:      c.currentGroup(),
		Collection: collection,
		Op:         op,
		Count:      count,
		Timestamp:  time.Now(),
	})
}

// LLMCalls returns a copy of all recorded LLM calls.
func (c *Collector) LLMCalls() []LLMCallRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]LLMCallRecord, len(c.llmCalls))
	copy(out, c.llmCalls)
	return out
}

// APICalls returns a copy of all recorded external API calls.
func (c *Collector) APICalls() []ExternalAPICallRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ExternalAPICallRecord, len(c.apiCalls))
	copy(out, c.apiCalls)
	return out
}

// FirestoreOps returns a copy of all recorded Firestore operations.
func (c *Collector) FirestoreOps() []FirestoreOpRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]FirestoreOpRecord, len(c.fsOps))
	copy(out, c.fsOps)
	return out
}

// Groups returns a copy of all group entries.
func (c *Collector) Groups() []GroupEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]GroupEntry, len(c.groups))
	copy(out, c.groups)
	return out
}

// TotalLLMCost returns the sum of CostUSD across all recorded LLM calls.
func (c *Collector) TotalLLMCost() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total float64
	for _, r := range c.llmCalls {
		total += r.CostUSD
	}
	return total
}

// TotalTokens returns the sum of input and output tokens across all LLM calls.
func (c *Collector) TotalTokens() (input, output int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.llmCalls {
		input += r.InputTokens
		output += r.OutputTokens
	}
	return
}

// Flush builds a JobTelemetry from collected data and saves it via the provided function.
// status should be "completed", "failed", etc.
func (c *Collector) Flush(ctx context.Context, status string, saveFunc func(context.Context, *models.JobTelemetry) error) error {
	c.mu.Lock()
	groups := make([]GroupEntry, len(c.groups))
	copy(groups, c.groups)
	llmCalls := make([]LLMCallRecord, len(c.llmCalls))
	copy(llmCalls, c.llmCalls)
	apiCalls := make([]ExternalAPICallRecord, len(c.apiCalls))
	copy(apiCalls, c.apiCalls)
	fsOps := make([]FirestoreOpRecord, len(c.fsOps))
	copy(fsOps, c.fsOps)
	startedAt := c.StartedAt
	tenantID := c.TenantID
	projectID := c.ProjectID
	lensID := c.LensID
	jobID := c.JobID
	jobType := c.JobType
	trigger := c.Trigger
	c.mu.Unlock()

	now := time.Now()

	// Index calls by group name for fast lookup.
	groupNames := make(map[string]bool, len(groups))
	for _, g := range groups {
		groupNames[g.Name] = true
	}

	// If there are calls with an empty Group, create an "ungrouped" entry.
	hasUngrouped := false
	for _, r := range llmCalls {
		if r.Group == "" {
			hasUngrouped = true
			break
		}
	}
	if !hasUngrouped {
		for _, r := range apiCalls {
			if r.Group == "" {
				hasUngrouped = true
				break
			}
		}
	}
	if !hasUngrouped {
		for _, r := range fsOps {
			if r.Group == "" {
				hasUngrouped = true
				break
			}
		}
	}
	if hasUngrouped {
		groups = append(groups, GroupEntry{Name: "", StartedAt: startedAt, EndedAt: now})
	}

	telGroups := make([]models.TelemetryGroup, 0, len(groups))
	for _, g := range groups {
		gName := g.Name
		displayName := gName
		if displayName == "" {
			displayName = "ungrouped"
		}

		var durMs int64
		if !g.EndedAt.IsZero() {
			durMs = g.EndedAt.Sub(g.StartedAt).Milliseconds()
		} else {
			durMs = now.Sub(g.StartedAt).Milliseconds()
		}

		ops := buildGroupOperations(gName, llmCalls, apiCalls, fsOps)

		telGroups = append(telGroups, models.TelemetryGroup{
			Name:       displayName,
			StartedAt:  g.StartedAt,
			DurationMs: durMs,
			Operations: ops,
		})
	}

	rollup := buildRollup(now, startedAt, llmCalls, apiCalls, fsOps)

	completedAt := now
	tel := models.JobTelemetry{
		TenantID:    tenantID,
		ProjectID:   projectID,
		LensID:      lensID,
		JobID:       jobID,
		JobType:     jobType,
		Trigger:     trigger,
		Status:      status,
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
		DurationMs:  now.Sub(startedAt).Milliseconds(),
		Groups:      telGroups,
		Rollup:      rollup,
	}

	return saveFunc(ctx, &tel)
}

func buildGroupOperations(groupName string, llmCalls []LLMCallRecord, apiCalls []ExternalAPICallRecord, fsOps []FirestoreOpRecord) []models.TelemetryOperation {
	type llmKey struct{ provider, model string }
	llmAgg := make(map[llmKey]*models.TelemetryOperation)
	for _, r := range llmCalls {
		if r.Group != groupName {
			continue
		}
		k := llmKey{r.Provider, r.Model}
		op, ok := llmAgg[k]
		if !ok {
			op = &models.TelemetryOperation{
				Kind:       "llm",
				Name:       fmt.Sprintf("%s/%s", r.Provider, r.Model),
				Provider:   r.Provider,
				Model:      r.Model,
				ModelAlias: r.ModelAlias,
				Cacheable:  r.Cacheable,
			}
			llmAgg[k] = op
		}
		op.Count++
		op.DurationMs += int64(r.DurationMs)
		op.InputTokens += r.InputTokens
		op.OutputTokens += r.OutputTokens
		op.TotalTokens += r.InputTokens + r.OutputTokens
		op.CostUSD += r.CostUSD
		if r.Cacheable {
			op.Cacheable = true
		}
	}

	type apiKey struct{ name string }
	apiAgg := make(map[apiKey]*models.TelemetryOperation)
	for _, r := range apiCalls {
		if r.Group != groupName {
			continue
		}
		k := apiKey{r.Name}
		op, ok := apiAgg[k]
		if !ok {
			op = &models.TelemetryOperation{
				Kind:     "external_api",
				Name:     r.Name,
				Provider: r.Provider,
			}
			apiAgg[k] = op
		}
		op.Count++
		op.APICalls++
		op.DurationMs += int64(r.DurationMs)
		if r.CacheHit {
			op.CacheHits++
		}
	}

	type fsKey struct{ collection, op string }
	fsAgg := make(map[fsKey]*models.TelemetryOperation)
	for _, r := range fsOps {
		if r.Group != groupName {
			continue
		}
		k := fsKey{r.Collection, r.Op}
		op, ok := fsAgg[k]
		if !ok {
			op = &models.TelemetryOperation{
				Kind: "firestore",
				Name: fmt.Sprintf("%s/%s", r.Collection, r.Op),
			}
			fsAgg[k] = op
		}
		op.Count += r.Count
	}

	ops := make([]models.TelemetryOperation, 0, len(llmAgg)+len(apiAgg)+len(fsAgg))
	for _, op := range llmAgg {
		ops = append(ops, *op)
	}
	for _, op := range apiAgg {
		ops = append(ops, *op)
	}
	for _, op := range fsAgg {
		ops = append(ops, *op)
	}
	return ops
}

func buildRollup(now, startedAt time.Time, llmCalls []LLMCallRecord, apiCalls []ExternalAPICallRecord, fsOps []FirestoreOpRecord) models.TelemetryRollup {
	rollup := models.TelemetryRollup{
		TotalDurationMs: now.Sub(startedAt).Milliseconds(),
		LLMCalls:        len(llmCalls),
		ExternalAPICalls: len(apiCalls),
	}
	for _, r := range llmCalls {
		rollup.LLMInputTokens += r.InputTokens
		rollup.LLMOutputTokens += r.OutputTokens
		rollup.LLMCostUSD += r.CostUSD
	}
	for _, r := range fsOps {
		switch r.Op {
		case "read", "get", "list", "query":
			rollup.FirestoreReads += r.Count
		default:
			rollup.FirestoreWrites += r.Count
		}
	}
	return rollup
}
