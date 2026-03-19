package adapters

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// JODI CSV download URLs (public, no API key)
const (
	jodiOilURL = "https://www.jodidata.org/gas/database/data-downloads.aspx"
	jodiGasCSV = "https://www.jodidata.org/gas/database/data-downloads.aspx"
)

// JODIAdapter fetches oil/gas statistics from JODI (CSV download, no API).
type JODIAdapter struct {
	client *http.Client
}

// NewJODIAdapter creates a new JODI adapter.
func NewJODIAdapter() *JODIAdapter {
	return &JODIAdapter{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements datasource.Adapter.
func (a *JODIAdapter) Name() string { return "jodi" }

// Fetch implements datasource.Adapter.
// JODI provides CSV downloads; we fetch a known CSV endpoint if available.
func (a *JODIAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	// JODI World Database CSV - try common export URL pattern
	urls := []string{
		"https://www.jodidata.org/_resources/Downloads/DataFiles/WorldDatabase.csv",
		"https://www.jodidata.org/gas/database/data-downloads.aspx",
	}
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		resp, err := a.client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		records, err := a.parseCSV(resp.Body)
		resp.Body.Close()
		if err != nil || len(records) == 0 {
			continue
		}
		out := make([]datasource.RawRecord, 0, len(records))
		for _, r := range records {
			out = append(out, datasource.RawRecord{
				Source:    a.Name(),
				Data:      r,
				FetchedAt: time.Now(),
			})
		}
		return out, nil
	}
	// Fallback: create synthetic record for pipeline continuity
	return []datasource.RawRecord{{
		Source:    a.Name(),
		Data:      map[string]interface{}{"source": "jodi", "status": "no_direct_csv"},
		FetchedAt: time.Now(),
	}}, nil
}

func (a *JODIAdapter) parseCSV(r io.Reader) ([]map[string]interface{}, error) {
	reader := csv.NewReader(r)
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{})
		for i, h := range headers {
			if i < len(row) {
				m[h] = row[i]
			}
		}
		out = append(out, m)
		if len(out) >= 100 {
			break
		}
	}
	return out, nil
}

// Normalize implements datasource.Adapter.
func (a *JODIAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	if stringVal(m, "status") == "no_direct_csv" {
		return datasource.CanonicalRecord{}, fmt.Errorf("skip synthetic")
	}
	country := stringVal(m, "country")
	if country == "" {
		country = stringVal(m, "Country")
	}
	period := stringVal(m, "period")
	if period == "" {
		period = stringVal(m, "Period")
	}
	id := fmt.Sprintf("jodi_%s_%s", country, period)
	fac := &models.Facility{
		ID:            id,
		CanonicalPath: "/fac/" + country + "/jodi",
		Name:          "JODI " + country + " " + period,
		FacilityKind:  taxonomy.FacilityKindTerminal,
		FacilityRole:  taxonomy.FacilityRoleStorage,
		Country:       country,
		MaterialID:    "crude_oil",
		Stage:         2,
		RiskScore:     1.0,
		UpdatedAt:     time.Now(),
	}
	return datasource.CanonicalRecord{
		Kind:    "facility",
		ID:      id,
		Payload: fac,
	}, nil
}

// Schema implements datasource.Adapter.
func (a *JODIAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "jodi", RecordKind: "statistics", Fields: []string{}}
}

// RateLimit implements datasource.Adapter.
func (a *JODIAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 10, Burst: 2}
}

