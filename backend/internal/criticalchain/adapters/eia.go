package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const eiaBase = "https://api.eia.gov/v2"

// EIA v2 response structure
type eiaResponse struct {
	Response struct {
		Data []struct {
			Period string `json:"period"`
			Value  string `json:"value"`
			Area   string `json:"area-name,omitempty"`
		} `json:"data"`
	} `json:"response"`
}

// EIAAdapter fetches oil and gas data from EIA API (EIA_API_KEY env or Secret Manager eia_api_key).
type EIAAdapter struct {
	client *http.Client
	apiKey string
}

// NewEIAAdapter creates a new EIA adapter.
func NewEIAAdapter() *EIAAdapter {
	projectID := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	apiKey := gcp.GetEnvOrSecret(context.Background(), projectID, "EIA_API_KEY", "eia_api_key")
	if apiKey == "" {
		log.Printf("[CriticalChain/EIA] EIA_API_KEY not set; adapter will return empty")
	}
	return &EIAAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
		apiKey: apiKey,
	}
}

// Name implements datasource.Adapter.
func (a *EIAAdapter) Name() string { return "eia" }

// Fetch implements datasource.Adapter.
func (a *EIAAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	if a.apiKey == "" {
		return nil, nil
	}
	// EIA petroleum stocks and production
	endpoints := []string{
		"/petroleum/sum/snd/data/",
		"/natural-gas/ng/data/",
	}
	var out []datasource.RawRecord
	for _, e := range endpoints {
		u, _ := url.Parse(eiaBase + e)
		q := u.Query()
		q.Set("api_key", a.apiKey)
		q.Set("frequency", "monthly")
		q.Set("data[0]", "value")
		q.Set("length", "12")
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			continue
		}
		resp, err := a.client.Do(req)
		if err != nil {
			log.Printf("[CriticalChain/EIA] fetch %s error: %v", e, err)
			continue
		}
		var body eiaResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		for _, d := range body.Response.Data {
			out = append(out, datasource.RawRecord{
				Source:    a.Name(),
				Data:      map[string]interface{}{"period": d.Period, "value": d.Value, "area": d.Area, "endpoint": e},
				FetchedAt: time.Now(),
			})
		}
	}
	return out, nil
}

// Normalize implements datasource.Adapter.
func (a *EIAAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	period := stringVal(m, "period")
	endpoint := stringVal(m, "endpoint")
	matID := "crude_oil"
	if strings.Contains(endpoint, "natural-gas") {
		matID = "natural_gas"
	}
	id := fmt.Sprintf("eia_%s_%s", matID, period)
	fac := &models.Facility{
		ID:            id,
		CanonicalPath: "/fac/us/eia_" + matID,
		Name:          "EIA " + matID + " " + period,
		FacilityKind:  taxonomy.FacilityKindTerminal,
		FacilityRole:  taxonomy.FacilityRoleStorage,
		Country:       "USA",
		MaterialID:    matID,
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
func (a *EIAAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{
		SourceID:   "eia",
		RecordKind: "facility",
		Fields:     []string{"period", "value", "area"},
	}
}

// RateLimit implements datasource.Adapter.
func (a *EIAAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 60, Burst: 5}
}
