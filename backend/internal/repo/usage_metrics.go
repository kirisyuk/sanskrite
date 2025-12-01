package repo

import (
	"context"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// UsageMetric represents a single GenAI API call usage record
type UsageMetric struct {
	ID            string  `json:"id" firestore:"id"`
	TenantID      string  `json:"tenantId" firestore:"tenantId"`
	UserID        string  `json:"userId" firestore:"userId"`
	ProjectID     *string `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	InteractionID *string `json:"interactionId,omitempty" firestore:"interactionId,omitempty"`

	// Model information (actual model name, not alias)
	Provider   string  `json:"provider" firestore:"provider"`                         // gemini, openai, anthropic, grok, deepseek
	Model      string  `json:"model" firestore:"model"`                               // actual model name like "gemini-2.0-flash-exp", "gpt-4o", "claude-3-5-sonnet-20241022"
	ModelAlias *string `json:"modelAlias,omitempty" firestore:"modelAlias,omitempty"` // alias used (e.g., "gemini_fast")

	// Token usage
	InputTokens  int `json:"inputTokens" firestore:"inputTokens"`
	OutputTokens int `json:"outputTokens" firestore:"outputTokens"`
	TotalTokens  int `json:"totalTokens" firestore:"totalTokens"`

	// Cost information
	CostUSD float64 `json:"costUSD" firestore:"costUSD"`

	// Timing information (optional, for backward compatibility)
	DurationMs *int `json:"durationMs,omitempty" firestore:"durationMs,omitempty"` // Execution time in milliseconds

	// Metadata
	TaskType *string `json:"taskType,omitempty" firestore:"taskType,omitempty"` // e.g., "discovery.answer", "eval.stance", "genai.generate"
	UseCase  *string `json:"useCase,omitempty" firestore:"useCase,omitempty"`   // Business purpose: "chat", "bias_analysis", "frame_decomposition", "source_processing", etc.
	Kind     string  `json:"kind" firestore:"kind"`                             // "included" or "on_demand"

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
}

// UsageMetricsRepository handles usage metrics storage and retrieval
type UsageMetricsRepository struct {
	col *firestore.CollectionRef
}

// NewUsageMetricsRepository creates a new UsageMetricsRepository
func NewUsageMetricsRepository(db *firestore.Client) *UsageMetricsRepository {
	return &UsageMetricsRepository{col: db.Collection("usage_metrics")}
}

// Create records a new usage metric
func (r *UsageMetricsRepository) Create(ctx context.Context, metric *UsageMetric) (string, error) {
	doc := r.col.NewDoc()
	metric.ID = doc.ID
	metric.CreatedAt = time.Now().UTC()
	_, err := doc.Set(ctx, metric)
	if err != nil {
		return "", err
	}
	return doc.ID, nil
}

// CreateMetric is a convenience method that creates a usage metric from individual parameters
// This allows AITaskExecutor to save metrics without importing the UsageMetric struct
// durationMs is optional (can be nil for backward compatibility)
// useCase is optional (can be nil for backward compatibility)
func (r *UsageMetricsRepository) CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int, useCase *string) (string, error) {
	metric := &UsageMetric{
		TenantID:     tenantID,
		UserID:       userID,
		ProjectID:    projectID,
		Provider:     provider,
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
		CostUSD:      cost,
		DurationMs:   durationMs,
		TaskType:     taskType,
		UseCase:      useCase,
		Kind:         "included",
	}
	return r.Create(ctx, metric)
}

// ListByUser returns usage metrics for a user within a time range
// limit: maximum number of records to return (default: 0 = all)
// offset: number of records to skip (default: 0)
func (r *UsageMetricsRepository) ListByUser(ctx context.Context, tenantID, userID string, startTime, endTime time.Time, limit, offset int) ([]*UsageMetric, int, error) {
	q := r.col.Where("tenantId", "==", tenantID).
		Where("userId", "==", userID).
		Where("createdAt", ">=", startTime).
		Where("createdAt", "<=", endTime).
		OrderBy("createdAt", firestore.Desc)

	// Get total count first (for pagination info)
	var totalCount int
	var docs []*firestore.DocumentSnapshot

	if limit > 0 {
		allDocs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, 0, err
		}
		totalCount = len(allDocs)

		// Apply pagination
		if offset > 0 {
			q = q.Offset(offset)
		}
		q = q.Limit(limit)

		docs, err = q.Documents(ctx).GetAll()
		if err != nil {
			return nil, 0, err
		}
	} else {
		// If limit is 0, we need to get all docs anyway, so count them
		allDocs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, 0, err
		}
		totalCount = len(allDocs)
		// Use the same docs for results
		docs = allDocs
	}

	var metrics []*UsageMetric
	for _, d := range docs {
		var m UsageMetric
		if err := d.DataTo(&m); err != nil {
			log.Printf("[ListByUser] Failed to parse document %s: %v", d.Ref.ID, err)
			continue
		}
		// Set ID from document reference
		m.ID = d.Ref.ID
		metrics = append(metrics, &m)
	}
	log.Printf("[ListByUser] Loaded %d metrics for user %s (tenant %s) from %v to %v", len(metrics), userID, tenantID, startTime, endTime)
	return metrics, totalCount, nil
}

// ModelAggregate represents aggregated usage for a model
type ModelAggregate struct {
	Count        int     `json:"count"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUSD"`
}

// AggregateByModel returns aggregated usage grouped by model
func (r *UsageMetricsRepository) AggregateByModel(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) (map[string]ModelAggregate, error) {
	metrics, _, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime, 0, 0)
	if err != nil {
		return nil, err
	}
	return r.AggregateByModelFromMetrics(ctx, metrics)
}

// AggregateByModelFromMetrics aggregates usage from already loaded metrics
func (r *UsageMetricsRepository) AggregateByModelFromMetrics(ctx context.Context, metrics []*UsageMetric) (map[string]ModelAggregate, error) {
	agg := make(map[string]ModelAggregate)

	for _, m := range metrics {
		key := m.Provider + ":" + m.Model
		val := agg[key]
		val.Count++
		val.InputTokens += m.InputTokens
		val.OutputTokens += m.OutputTokens
		val.TotalTokens += m.TotalTokens
		val.CostUSD += m.CostUSD
		agg[key] = val
	}

	return agg, nil
}

// DailyTotal represents aggregated usage for a single day
type DailyTotal struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUSD"`
	Count        int     `json:"count"`
}

// isPremiumModel determines if a model is considered "premium" (quality or balanced for Claude)
// Premium models are: _quality for any provider, or _balanced for Claude only
func isPremiumModel(provider string, modelAlias *string) bool {
	if modelAlias == nil {
		return false
	}
	alias := *modelAlias
	// Check for _quality suffix (any provider)
	if strings.HasSuffix(alias, "_quality") {
		return true
	}
	// Check for _balanced suffix (Claude only)
	if provider == "anthropic" && strings.HasSuffix(alias, "_balanced") {
		return true
	}
	return false
}

// GetDailyTotals returns daily totals for a user
func (r *UsageMetricsRepository) GetDailyTotals(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) (map[string]DailyTotal, error) {
	metrics, _, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime, 0, 0)
	if err != nil {
		return nil, err
	}
	return r.GetDailyTotalsFromMetrics(ctx, metrics)
}

// GetDailyTotalsFromMetrics calculates daily totals from already loaded metrics
func (r *UsageMetricsRepository) GetDailyTotalsFromMetrics(ctx context.Context, metrics []*UsageMetric) (map[string]DailyTotal, error) {
	daily := make(map[string]DailyTotal)

	for _, m := range metrics {
		dateKey := m.CreatedAt.Format("2006-01-02")
		val := daily[dateKey]
		val.Count++
		val.InputTokens += m.InputTokens
		val.OutputTokens += m.OutputTokens
		val.TotalTokens += m.TotalTokens
		val.CostUSD += m.CostUSD
		daily[dateKey] = val
	}

	return daily, nil
}

// TokenCategoryTotals represents token totals split by premium vs regular models
type TokenCategoryTotals struct {
	PremiumTokens TokenTotals `json:"premiumTokens"`
	RegularTokens TokenTotals `json:"regularTokens"`
}

// TokenTotals represents token usage totals
type TokenTotals struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUSD"`
	Count        int     `json:"count"`
}

// GetTokenCategoryTotals returns token totals split by premium vs regular models
func (r *UsageMetricsRepository) GetTokenCategoryTotals(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) (*TokenCategoryTotals, error) {
	metrics, _, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime, 0, 0)
	if err != nil {
		return nil, err
	}
	return r.GetTokenCategoryTotalsFromMetrics(ctx, metrics)
}

// GetTokenCategoryTotalsFromMetrics calculates token category totals from already loaded metrics
func (r *UsageMetricsRepository) GetTokenCategoryTotalsFromMetrics(ctx context.Context, metrics []*UsageMetric) (*TokenCategoryTotals, error) {
	var premium, regular TokenTotals

	for _, m := range metrics {
		if isPremiumModel(m.Provider, m.ModelAlias) {
			premium.Count++
			premium.InputTokens += m.InputTokens
			premium.OutputTokens += m.OutputTokens
			premium.TotalTokens += m.TotalTokens
			premium.CostUSD += m.CostUSD
		} else {
			regular.Count++
			regular.InputTokens += m.InputTokens
			regular.OutputTokens += m.OutputTokens
			regular.TotalTokens += m.TotalTokens
			regular.CostUSD += m.CostUSD
		}
	}

	return &TokenCategoryTotals{
		PremiumTokens: premium,
		RegularTokens: regular,
	}, nil
}

// UsageAggregates contains all aggregated usage data computed in a single pass
type UsageAggregates struct {
	ByModel         map[string]ModelAggregate `json:"byModel"`
	DailyTotals     map[string]DailyTotal     `json:"dailyTotals"`
	TokenCategories *TokenCategoryTotals      `json:"tokenCategories"`
	Totals          *DailyTotal               `json:"totals"`
}

// GetAllAggregates loads metrics once and computes all aggregations in a single pass
// This is much more efficient than calling separate aggregation methods
func (r *UsageMetricsRepository) GetAllAggregates(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) (*UsageAggregates, error) {
	// Load metrics once
	metrics, _, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime, 0, 0)
	if err != nil {
		return nil, err
	}

	// Initialize aggregation structures
	byModel := make(map[string]ModelAggregate)
	dailyTotals := make(map[string]DailyTotal)
	var premium, regular TokenTotals
	var totals DailyTotal

	// Single pass through all metrics to compute all aggregations
	log.Printf("[GetAllAggregates] Processing %d metrics for user %s", len(metrics), userID)
	for _, m := range metrics {
		// 1. Aggregate by model (provider:model)
		modelKey := m.Provider + ":" + m.Model
		modelAgg := byModel[modelKey]
		modelAgg.Count++
		modelAgg.InputTokens += m.InputTokens
		modelAgg.OutputTokens += m.OutputTokens
		modelAgg.TotalTokens += m.TotalTokens
		modelAgg.CostUSD += m.CostUSD
		byModel[modelKey] = modelAgg

		// 2. Aggregate by day
		dateKey := m.CreatedAt.Format("2006-01-02")
		daily := dailyTotals[dateKey]
		daily.Count++
		daily.InputTokens += m.InputTokens
		daily.OutputTokens += m.OutputTokens
		daily.TotalTokens += m.TotalTokens
		daily.CostUSD += m.CostUSD
		dailyTotals[dateKey] = daily

		// 3. Aggregate by token category (premium vs regular)
		if isPremiumModel(m.Provider, m.ModelAlias) {
			premium.Count++
			premium.InputTokens += m.InputTokens
			premium.OutputTokens += m.OutputTokens
			premium.TotalTokens += m.TotalTokens
			premium.CostUSD += m.CostUSD
		} else {
			regular.Count++
			regular.InputTokens += m.InputTokens
			regular.OutputTokens += m.OutputTokens
			regular.TotalTokens += m.TotalTokens
			regular.CostUSD += m.CostUSD
		}

		// 4. Aggregate totals
		totals.Count++
		totals.InputTokens += m.InputTokens
		totals.OutputTokens += m.OutputTokens
		totals.TotalTokens += m.TotalTokens
		totals.CostUSD += m.CostUSD
	}

	log.Printf("[GetAllAggregates] Aggregation complete: byModel=%d entries, dailyTotals=%d entries, totals.count=%d", len(byModel), len(dailyTotals), totals.Count)
	if len(byModel) > 0 {
		for key, agg := range byModel {
			log.Printf("[GetAllAggregates] Model %s: count=%d, tokens=%d, cost=%.6f", key, agg.Count, agg.TotalTokens, agg.CostUSD)
		}
	}

	return &UsageAggregates{
		ByModel:         byModel,
		DailyTotals:     dailyTotals,
		TokenCategories: &TokenCategoryTotals{PremiumTokens: premium, RegularTokens: regular},
		Totals:          &totals,
	}, nil
}

// ListByUserPaginated returns paginated usage metrics with total count
// Returns events for the requested page and the total count of all matching events
// limit must be at least 200 for efficient grouping
func (r *UsageMetricsRepository) ListByUserPaginated(ctx context.Context, tenantID, userID string, startTime, endTime time.Time, page, limit int) ([]*UsageMetric, int, error) {
	if limit < 200 {
		limit = 200 // Minimum limit for efficient grouping
	}
	if page < 1 {
		page = 1
	}

	log.Printf("[ListByUserPaginated] Request: page=%d, limit=%d, user=%s, tenant=%s, startTime=%v, endTime=%v", page, limit, userID, tenantID, startTime, endTime)

	// Load all matching metrics (needed for accurate total count)
	// This is acceptable because we need them for aggregations anyway
	allMetrics, totalCount, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime, 0, 0)
	if err != nil {
		log.Printf("[ListByUserPaginated] Error loading metrics: %v", err)
		return nil, 0, err
	}

	log.Printf("[ListByUserPaginated] Loaded %d total metrics", totalCount)

	// Calculate pagination
	offset := (page - 1) * limit
	if offset >= totalCount {
		log.Printf("[ListByUserPaginated] Offset %d >= totalCount %d, returning empty slice", offset, totalCount)
		return []*UsageMetric{}, totalCount, nil
	}

	end := offset + limit
	if end > totalCount {
		end = totalCount
	}

	log.Printf("[ListByUserPaginated] Returning slice [%d:%d] (%d events)", offset, end, end-offset)

	// Return paginated slice
	return allMetrics[offset:end], totalCount, nil
}

// GetAverageTiming calculates average response time in seconds for a given model
// Uses historical data from the last 30 days
// Filters out outliers using IQR (Interquartile Range) method to avoid skewing from extreme values
// Returns average response time in seconds, or nil if not enough data is available
// Simply returns the average duration without considering token count
func (r *UsageMetricsRepository) GetAverageTiming(ctx context.Context, tenantID, userID, provider, model string) (*float64, error) {
	// Look at last 30 days of data
	endTime := time.Now().UTC()
	startTime := endTime.AddDate(0, 0, -30)

	metrics, _, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime, 0, 0)
	if err != nil {
		return nil, err
	}

	// Filter metrics for this specific model
	// Note: DurationMs == nil means the metric was recorded before timing was added, so we skip it
	var relevantMetrics []*UsageMetric
	for _, m := range metrics {
		if m.Provider == provider && m.Model == model && m.DurationMs != nil {
			relevantMetrics = append(relevantMetrics, m)
		}
	}

	if len(relevantMetrics) == 0 {
		return nil, nil // Not enough data
	}

	// Extract durations for outlier filtering
	durations := make([]float64, 0, len(relevantMetrics))
	for _, m := range relevantMetrics {
		durationMs := float64(*m.DurationMs)
		durations = append(durations, durationMs)
	}

	// Filter outliers using IQR method
	// Sort durations to find quartiles
	sortedDurations := make([]float64, len(durations))
	copy(sortedDurations, durations)

	// Simple insertion sort (good enough for small datasets)
	for i := 1; i < len(sortedDurations); i++ {
		key := sortedDurations[i]
		j := i - 1
		for j >= 0 && sortedDurations[j] > key {
			sortedDurations[j+1] = sortedDurations[j]
			j--
		}
		sortedDurations[j+1] = key
	}

	// Calculate quartiles
	var q1, q3 float64
	n := len(sortedDurations)
	if n < 4 {
		// Too few samples, use all data
		q1 = sortedDurations[0]
		q3 = sortedDurations[n-1]
	} else {
		// Q1: 25th percentile, Q3: 75th percentile
		q1Idx := n / 4
		q3Idx := (3 * n) / 4
		q1 = sortedDurations[q1Idx]
		q3 = sortedDurations[q3Idx]
	}

	// Calculate IQR and bounds
	iqr := q3 - q1
	lowerBound := q1 - 1.5*iqr
	upperBound := q3 + 1.5*iqr

	// Also add hard limits: reject anything over 120 seconds total duration or under 10ms
	// This prevents extreme outliers from skewing results
	const maxDurationMs = 120000 // 120 seconds
	const minDurationMs = 10     // 10ms

	// Filter durations, keeping only those within bounds
	var filteredDurations []float64
	for _, d := range durations {
		if d >= lowerBound && d <= upperBound &&
			d >= minDurationMs && d <= maxDurationMs {
			filteredDurations = append(filteredDurations, d)
		}
	}

	// If filtering removed too many samples, use original data (but still apply hard limits)
	if len(filteredDurations) < len(durations)/2 {
		filteredDurations = filteredDurations[:0]
		for _, d := range durations {
			if d >= minDurationMs && d <= maxDurationMs {
				filteredDurations = append(filteredDurations, d)
			}
		}
	}

	if len(filteredDurations) == 0 {
		return nil, nil // All data filtered out as outliers
	}

	// Calculate average duration from filtered data
	var totalDurationMs float64
	for _, d := range filteredDurations {
		totalDurationMs += d
	}

	// Average milliseconds
	avgMs := totalDurationMs / float64(len(filteredDurations))

	// Convert to seconds and round to 1 decimal place
	avgSeconds := avgMs / 1000.0
	// Round to 1 decimal place
	avgSeconds = float64(int(avgSeconds*10+0.5)) / 10.0

	return &avgSeconds, nil
}
