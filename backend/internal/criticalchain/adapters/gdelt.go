package adapters

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

// GDELT public dataset: gdelt-bq.gdeltv2.events
const gdeltProject = "gdelt-bq"
const gdeltDataset = "gdeltv2"
const gdeltTable = "events"

// GDELTAdapter fetches events from GDELT BigQuery (public dataset).
type GDELTAdapter struct {
	client *bigquery.Client
}

// NewGDELTAdapter creates a new GDELT adapter. Uses GCP project from env for query billing.
func NewGDELTAdapter() *GDELTAdapter {
	projectID := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if projectID == "" {
		return &GDELTAdapter{}
	}
	client, err := bigquery.NewClient(context.Background(), projectID)
	if err != nil {
		log.Printf("[CriticalChain/GDELT] BigQuery client init failed: %v", err)
		return &GDELTAdapter{}
	}
	return &GDELTAdapter{client: client}
}

// NewGDELTAdapterWithClient creates adapter with an existing BigQuery client.
func NewGDELTAdapterWithClient(bq *gcp.BigQueryClient) *GDELTAdapter {
	if bq == nil {
		return &GDELTAdapter{}
	}
	return &GDELTAdapter{client: bq.Client()}
}

// Name implements datasource.Adapter.
func (a *GDELTAdapter) Name() string { return "gdelt" }

// Fetch implements datasource.Adapter.
func (a *GDELTAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	if a.client == nil {
		return nil, nil
	}
	// Query GDELT events for recent days
	day := since.Format("20060102")
	query := fmt.Sprintf(`
		SELECT 
			GLOBALEVENTID, SQLDATE, Actor1Code, Actor1Name, Actor2Code, Actor2Name,
			EventCode, GoldsteinScale, NumMentions, NumSources, ActionGeo_CountryCode
		FROM `+"`%s.%s.%s`"+`
		WHERE SQLDATE >= @since
		LIMIT 500
	`, gdeltProject, gdeltDataset, gdeltTable)

	q := a.client.Query(query)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "since", Value: day},
	}
	q.Location = "US"

	iter, err := q.Read(ctx)
	if err != nil {
		log.Printf("[CriticalChain/GDELT] query error: %v", err)
		return nil, nil
	}
	var out []datasource.RawRecord
	for {
		var row map[string]bigquery.Value
		if err := iter.Next(&row); err != nil {
			break
		}
		m := make(map[string]interface{})
		for k, v := range row {
			if v != nil {
				m[k] = v
			}
		}
		out = append(out, datasource.RawRecord{
			Source:    a.Name(),
			Data:      m,
			FetchedAt: time.Now(),
		})
	}
	return out, nil
}

// Normalize implements datasource.Adapter.
func (a *GDELTAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, nil
	}
	id, _ := m["GLOBALEVENTID"].(int64)
	if id == 0 {
		return datasource.CanonicalRecord{}, nil
	}
	return datasource.CanonicalRecord{
		Kind: "event",
		ID:   fmt.Sprintf("gdelt_%d", id),
		Payload: map[string]interface{}{
			"globalEventId":   id,
			"sqlDate":         m["SQLDATE"],
			"actor1Code":      m["Actor1Code"],
			"actor1Name":      m["Actor1Name"],
			"actor2Code":      m["Actor2Code"],
			"actor2Name":      m["Actor2Name"],
			"eventCode":       m["EventCode"],
			"goldsteinScale":  m["GoldsteinScale"],
			"numMentions":     m["NumMentions"],
			"countryCode":     m["ActionGeo_CountryCode"],
		},
	}, nil
}

// Schema implements datasource.Adapter.
func (a *GDELTAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "gdelt", RecordKind: "event", Fields: []string{"GLOBALEVENTID", "SQLDATE", "Actor1Name", "EventCode"}}
}

// RateLimit implements datasource.Adapter.
func (a *GDELTAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 10, Burst: 2}
}
