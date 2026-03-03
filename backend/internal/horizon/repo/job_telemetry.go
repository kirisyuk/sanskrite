package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type ListOpts struct {
	JobType   string
	Limit     int
	StartTime *time.Time
	EndTime   *time.Time
}

type LensTelemetrySummary struct {
	TotalJobs       int
	TotalDurationMs int64
	TotalLLMCalls   int
	TotalLLMCostUSD float64
	TotalLLMTokens  int
	ByJobType       map[string]int
}

type JobTelemetryRepository struct {
	db  *firestore.Client
	col *firestore.CollectionRef
}

func NewJobTelemetryRepository(db *firestore.Client) *JobTelemetryRepository {
	if db == nil {
		return nil
	}
	return &JobTelemetryRepository{
		db:  db,
		col: db.Collection("horizon_job_telemetry"),
	}
}

// Save writes a JobTelemetry document. If no ID is set, a new document is created.
func (r *JobTelemetryRepository) Save(ctx context.Context, t *models.JobTelemetry) error {
	if t == nil {
		return ErrInvalidDocument
	}
	t.TenantID = strings.TrimSpace(t.TenantID)
	t.LensID = strings.TrimSpace(t.LensID)
	t.JobID = strings.TrimSpace(t.JobID)

	if t.ID != "" {
		_, err := r.col.Doc(t.ID).Set(ctx, t)
		return err
	}

	doc := r.col.NewDoc()
	t.ID = doc.ID
	_, err := doc.Set(ctx, t)
	return err
}

// ListByLens returns telemetry records for a lens, paginated and filterable.
func (r *JobTelemetryRepository) ListByLens(ctx context.Context, lensID string, opts ListOpts) ([]*models.JobTelemetry, error) {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return nil, ErrInvalidDocument
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	q := r.col.Where("lensId", "==", lensID)
	if strings.TrimSpace(opts.JobType) != "" {
		q = q.Where("jobType", "==", strings.TrimSpace(opts.JobType))
	}
	if opts.StartTime != nil {
		q = q.Where("startedAt", ">=", *opts.StartTime)
	}
	if opts.EndTime != nil {
		q = q.Where("startedAt", "<=", *opts.EndTime)
	}
	q = q.OrderBy("startedAt", firestore.Desc).Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listing job telemetry by lens: %w", err)
	}

	out := make([]*models.JobTelemetry, 0, len(docs))
	for _, d := range docs {
		var item models.JobTelemetry
		if err := d.DataTo(&item); err != nil {
			continue
		}
		if item.ID == "" {
			item.ID = d.Ref.ID
		}
		out = append(out, &item)
	}
	return out, nil
}

// GetByJobID returns telemetry for a specific job.
func (r *JobTelemetryRepository) GetByJobID(ctx context.Context, jobID string) (*models.JobTelemetry, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, ErrInvalidDocument
	}

	docs, err := r.col.Where("jobId", "==", jobID).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("getting job telemetry by jobId: %w", err)
	}
	if len(docs) == 0 {
		return nil, status.Errorf(codes.NotFound, "job telemetry not found for jobId %s", jobID)
	}

	var out models.JobTelemetry
	if err := docs[0].DataTo(&out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		out.ID = docs[0].Ref.ID
	}
	return &out, nil
}

// SummarizeLens returns aggregated telemetry for a lens since the given time.
func (r *JobTelemetryRepository) SummarizeLens(ctx context.Context, lensID string, since time.Time) (*LensTelemetrySummary, error) {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return nil, ErrInvalidDocument
	}

	q := r.col.Where("lensId", "==", lensID).
		Where("startedAt", ">=", since).
		OrderBy("startedAt", firestore.Desc)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("summarizing lens telemetry: %w", err)
	}

	summary := &LensTelemetrySummary{
		ByJobType: make(map[string]int),
	}

	for _, d := range docs {
		var item models.JobTelemetry
		if err := d.DataTo(&item); err != nil {
			continue
		}
		summary.TotalJobs++
		summary.TotalDurationMs += item.DurationMs
		summary.TotalLLMCalls += item.Rollup.LLMCalls
		summary.TotalLLMCostUSD += item.Rollup.LLMCostUSD
		summary.TotalLLMTokens += item.Rollup.LLMInputTokens + item.Rollup.LLMOutputTokens

		jt := strings.TrimSpace(item.JobType)
		if jt != "" {
			summary.ByJobType[jt]++
		}
	}

	return summary, nil
}
