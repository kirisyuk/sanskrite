package repo

import (
	"context"
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
func (r *UsageMetricsRepository) CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int) (string, error) {
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
		Kind:         "included",
	}
	return r.Create(ctx, metric)
}

// ListByUser returns usage metrics for a user within a time range
func (r *UsageMetricsRepository) ListByUser(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) ([]*UsageMetric, error) {
	q := r.col.Where("tenantId", "==", tenantID).
		Where("userId", "==", userID).
		Where("createdAt", ">=", startTime).
		Where("createdAt", "<=", endTime).
		OrderBy("createdAt", firestore.Desc)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var metrics []*UsageMetric
	for _, d := range docs {
		var m UsageMetric
		if err := d.DataTo(&m); err != nil {
			continue
		}
		metrics = append(metrics, &m)
	}
	return metrics, nil
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
	metrics, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}

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
	metrics, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}

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
	metrics, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}

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

// GetAverageTiming estimates average response time in seconds for a given model and token count
// Uses historical data from the last 30 days to calculate average duration per token
// Filters out outliers using IQR (Interquartile Range) method to avoid skewing from extreme values
// Returns estimated time in seconds, or nil if not enough data is available
func (r *UsageMetricsRepository) GetAverageTiming(ctx context.Context, tenantID, userID, provider, model string, estimatedOutputTokens int) (*float64, error) {
	// Look at last 30 days of data
	endTime := time.Now().UTC()
	startTime := endTime.AddDate(0, 0, -30)

	metrics, err := r.ListByUser(ctx, tenantID, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// Filter metrics for this specific model
	// Note: DurationMs == nil means the metric was recorded before timing was added, so we skip it
	var relevantMetrics []*UsageMetric
	for _, m := range metrics {
		if m.Provider == provider && m.Model == model && m.DurationMs != nil && m.OutputTokens > 0 {
			relevantMetrics = append(relevantMetrics, m)
		}
	}

	if len(relevantMetrics) == 0 {
		return nil, nil // Not enough data
	}

	// Calculate duration per token for each metric
	type metricRate struct {
		durationMs   int64
		outputTokens int64
		msPerToken   float64
	}
	rates := make([]metricRate, 0, len(relevantMetrics))
	for _, m := range relevantMetrics {
		msPerToken := float64(*m.DurationMs) / float64(m.OutputTokens)
		rates = append(rates, metricRate{
			durationMs:   int64(*m.DurationMs),
			outputTokens: int64(m.OutputTokens),
			msPerToken:   msPerToken,
		})
	}

	// Filter outliers using IQR method
	// Sort by msPerToken to find quartiles
	sortedRates := make([]float64, len(rates))
	for i, r := range rates {
		sortedRates[i] = r.msPerToken
	}

	// Simple insertion sort (good enough for small datasets)
	for i := 1; i < len(sortedRates); i++ {
		key := sortedRates[i]
		j := i - 1
		for j >= 0 && sortedRates[j] > key {
			sortedRates[j+1] = sortedRates[j]
			j--
		}
		sortedRates[j+1] = key
	}

	// Calculate quartiles
	var q1, q3 float64
	n := len(sortedRates)
	if n < 4 {
		// Too few samples, use all data
		q1 = sortedRates[0]
		q3 = sortedRates[n-1]
	} else {
		// Q1: 25th percentile, Q3: 75th percentile
		q1Idx := n / 4
		q3Idx := (3 * n) / 4
		q1 = sortedRates[q1Idx]
		q3 = sortedRates[q3Idx]
	}

	// Calculate IQR and bounds
	iqr := q3 - q1
	lowerBound := q1 - 1.5*iqr
	upperBound := q3 + 1.5*iqr

	// Also add hard limits: reject anything over 120 seconds total duration or under 10ms
	// This prevents extreme outliers from skewing results
	const maxDurationMs = 120000 // 120 seconds
	const minDurationMs = 10     // 10ms

	// Filter rates, keeping only those within bounds
	var filteredRates []metricRate
	for _, r := range rates {
		if r.msPerToken >= lowerBound && r.msPerToken <= upperBound &&
			r.durationMs >= minDurationMs && r.durationMs <= maxDurationMs {
			filteredRates = append(filteredRates, r)
		}
	}

	// If filtering removed too many samples, use original data (but still apply hard limits)
	if len(filteredRates) < len(rates)/2 {
		filteredRates = filteredRates[:0]
		for _, r := range rates {
			if r.durationMs >= minDurationMs && r.durationMs <= maxDurationMs {
				filteredRates = append(filteredRates, r)
			}
		}
	}

	if len(filteredRates) == 0 {
		return nil, nil // All data filtered out as outliers
	}

	// Calculate average duration per output token from filtered data
	var totalDurationMs int64
	var totalOutputTokens int64
	for _, r := range filteredRates {
		totalDurationMs += r.durationMs
		totalOutputTokens += r.outputTokens
	}

	if totalOutputTokens == 0 {
		return nil, nil
	}

	// Average milliseconds per output token
	avgMsPerToken := float64(totalDurationMs) / float64(totalOutputTokens)

	// Estimate duration for requested token count
	estimatedMs := avgMsPerToken * float64(estimatedOutputTokens)

	// Convert to seconds and round to 1 decimal place
	estimatedSeconds := estimatedMs / 1000.0
	// Round to 1 decimal place
	estimatedSeconds = float64(int(estimatedSeconds*10+0.5)) / 10.0

	return &estimatedSeconds, nil
}
