package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HorizonTelemetryRecord stores aggregated telemetry counters for a time window.
// Collection: horizon_telemetry
type HorizonTelemetryRecord struct {
	TenantID      string            `firestore:"tenantId"`
	Component     string            `firestore:"component"`
	Metric        string            `firestore:"metric"`
	WindowStart   time.Time         `firestore:"windowStart"`
	WindowSizeSec int               `firestore:"windowSizeSec"`
	Dimensions    map[string]string `firestore:"dimensions,omitempty"`
	Counters      map[string]int64  `firestore:"counters"`
	UpdatedAt     time.Time         `firestore:"updatedAt"`
}

// HorizonTelemetryRepository provides a unified telemetry sink.
// It aggregates counters into time windows to keep write volume predictable.
type HorizonTelemetryRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

func NewHorizonTelemetryRepository(db *firestore.Client) *HorizonTelemetryRepository {
	if db == nil {
		return &HorizonTelemetryRepository{}
	}
	return &HorizonTelemetryRepository{
		col: db.Collection("horizon_telemetry"),
		db:  db,
	}
}

func stableDimensionsKey(dimensions map[string]string) string {
	if len(dimensions) == 0 {
		return ""
	}
	keys := make([]string, 0, len(dimensions))
	for k := range dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("|")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(dimensions[k]))
	}
	return b.String()
}

func (r *HorizonTelemetryRepository) docID(tenantID, component, metric string, windowStart time.Time, dimensions map[string]string) string {
	tenantID = strings.TrimSpace(tenantID)
	component = strings.TrimSpace(component)
	metric = strings.TrimSpace(metric)
	dimsKey := stableDimensionsKey(dimensions)
	raw := fmt.Sprintf("%s|%s|%s|%s|%s", tenantID, component, metric, windowStart.UTC().Format(time.RFC3339), dimsKey)
	sum := sha1.Sum([]byte(raw))
	h := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s_%s", tenantID, h)
}

// Increment aggregates counters for a time window.
// Example usage: component="canonical_path_resolver", metric="redirect_cache".
func (r *HorizonTelemetryRepository) Increment(
	ctx context.Context,
	tenantID string,
	component string,
	metric string,
	windowSize time.Duration,
	dimensions map[string]string,
	counters map[string]int64,
) error {
	if r == nil || r.col == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	component = strings.TrimSpace(component)
	metric = strings.TrimSpace(metric)
	if tenantID == "" || component == "" || metric == "" {
		return nil
	}
	if windowSize <= 0 {
		windowSize = time.Hour
	}
	if len(counters) == 0 {
		return nil
	}
	windowStart := time.Now().UTC().Truncate(windowSize)
	now := time.Now().UTC()

	doc := r.col.Doc(r.docID(tenantID, component, metric, windowStart, dimensions))
	record := HorizonTelemetryRecord{
		TenantID:      tenantID,
		Component:     component,
		Metric:        metric,
		WindowStart:   windowStart,
		WindowSizeSec: int(windowSize.Seconds()),
		Dimensions:    dimensions,
		Counters:      counters,
		UpdatedAt:     now,
	}

	if _, err := doc.Create(ctx, record); err == nil {
		return nil
	} else if status.Code(err) != codes.AlreadyExists {
		return err
	}

	updates := []firestore.Update{
		{Path: "updatedAt", Value: now},
	}
	for key, val := range counters {
		if key == "" || val == 0 {
			continue
		}
		updates = append(updates, firestore.Update{
			Path:  fmt.Sprintf("counters.%s", key),
			Value: firestore.Increment(val),
		})
	}
	if len(dimensions) > 0 {
		updates = append(updates, firestore.Update{Path: "dimensions", Value: dimensions})
	}
	if len(updates) == 0 {
		return nil
	}
	_, err := doc.Update(ctx, updates)
	return err
}

// List fetches recent telemetry windows for a component+metric.
// Results are ordered by windowStart descending.
func (r *HorizonTelemetryRepository) List(
	ctx context.Context,
	tenantID string,
	component string,
	metric string,
	limit int,
) ([]*HorizonTelemetryRecord, error) {
	if r == nil || r.col == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	component = strings.TrimSpace(component)
	metric = strings.TrimSpace(metric)
	if tenantID == "" || component == "" || metric == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 48
	}
	if limit > 720 {
		limit = 720
	}

	q := r.col.Where("tenantId", "==", tenantID).
		Where("component", "==", component).
		Where("metric", "==", metric).
		OrderBy("windowStart", firestore.Desc).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*HorizonTelemetryRecord, 0, len(docs))
	for _, doc := range docs {
		var rec HorizonTelemetryRecord
		if err := doc.DataTo(&rec); err != nil {
			continue
		}
		out = append(out, &rec)
	}
	return out, nil
}
