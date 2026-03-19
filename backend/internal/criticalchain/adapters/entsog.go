package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"google.golang.org/genproto/googleapis/type/latlng"
)

const entsogBase = "https://transparency.entsog.eu/api/v1"

// ENTSOGAdapter fetches European gas flows from ENTSO-G Transparency Platform (public API, no key).
type ENTSOGAdapter struct {
	client *http.Client
}

// NewENTSOGAdapter creates a new ENTSO-G adapter.
func NewENTSOGAdapter() *ENTSOGAdapter {
	return &ENTSOGAdapter{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements datasource.Adapter.
func (a *ENTSOGAdapter) Name() string { return "entsog" }

// Fetch implements datasource.Adapter.
func (a *ENTSOGAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	// Operational data: physical flows, nominations
	u, _ := url.Parse(entsogBase + "/operationaldatas")
	q := u.Query()
	q.Set("from", since.Format("2006-01-02"))
	q.Set("to", time.Now().Format("2006-01-02"))
	q.Set("limit", "100")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("[CriticalChain/ENTSOG] fetch error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var raw struct {
		OperationalDatas []map[string]interface{} `json:"operationalDatas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[CriticalChain/ENTSOG] parse error: %v", err)
		return nil, nil
	}
	out := make([]datasource.RawRecord, 0, len(raw.OperationalDatas))
	for _, r := range raw.OperationalDatas {
		out = append(out, datasource.RawRecord{
			Source:    a.Name(),
			Data:      r,
			FetchedAt: time.Now(),
		})
	}
	return out, nil
}

// Normalize implements datasource.Adapter.
func (a *ENTSOGAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	pointKey := stringVal(m, "pointKey")
	pointLabel := stringVal(m, "pointLabel")
	if pointKey == "" {
		pointKey = stringVal(m, "operatorKey")
	}
	id := fmt.Sprintf("entsog_%s_%v", pointKey, m["periodFrom"])
	fac := &models.Facility{
		ID:            id,
		CanonicalPath: "/fac/eu/entsog_" + pointKey,
		Name:          pointLabel,
		FacilityKind:  taxonomy.FacilityKindTerminal,
		FacilityRole:  taxonomy.FacilityRoleStorage,
		Country:       "EU",
		MaterialID:    "natural_gas",
		Stage:         2,
		RiskScore:     1.0,
		UpdatedAt:     time.Now(),
	}
	if lat, lon := floatVal(m, "latitude"), floatVal(m, "longitude"); lat != 0 || lon != 0 {
		fac.Coordinates = &latlng.LatLng{Latitude: lat, Longitude: lon}
	}
	return datasource.CanonicalRecord{
		Kind:    "facility",
		ID:      id,
		Payload: fac,
	}, nil
}

// Schema implements datasource.Adapter.
func (a *ENTSOGAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "entsog", RecordKind: "flow", Fields: []string{"pointKey", "pointLabel", "periodFrom"}}
}

// RateLimit implements datasource.Adapter.
func (a *ENTSOGAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 30, Burst: 5}
}
