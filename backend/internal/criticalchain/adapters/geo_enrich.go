package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"google.golang.org/genproto/googleapis/type/latlng"
)

const (
	overpassURL = "https://overpass-api.de/api/interpreter"
	gleifAPI    = "https://api.gleif.org/api/v1/lei-records"
)

// GeoEnrichAdapter enriches facilities with OSM Overpass, GLEIF data.
type GeoEnrichAdapter struct {
	client *http.Client
}

// NewGeoEnrichAdapter creates a new geo enrichment adapter.
func NewGeoEnrichAdapter() *GeoEnrichAdapter {
	return &GeoEnrichAdapter{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements datasource.Adapter.
func (a *GeoEnrichAdapter) Name() string { return "geo_enrich" }

// Fetch implements datasource.Adapter. Fetches from OSM Overpass (oil/gas infrastructure).
func (a *GeoEnrichAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	// Overpass: oil/gas pipelines in Europe (bbox south,west,north,east)
	query := `[out:json][timeout:30];(way["man_made"="pipeline"](45,-10,55,20););out center 50;`
	form := url.Values{"data": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, overpassURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("[CriticalChain/GeoEnrich] Overpass error: %v", err)
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var result struct {
		Elements []struct {
			Type   string  `json:"type"`
			ID     int64   `json:"id"`
			Lat    float64 `json:"lat"`
			Lon    float64 `json:"lon"`
			Center struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[CriticalChain/GeoEnrich] parse error: %v", err)
		return nil, nil
	}
	out := make([]datasource.RawRecord, 0, len(result.Elements))
	for _, el := range result.Elements {
		lat, lon := el.Lat, el.Lon
		if lat == 0 && lon == 0 {
			lat, lon = el.Center.Lat, el.Center.Lon
		}
		m := map[string]interface{}{
			"type": el.Type,
			"id":   el.ID,
			"lat":  lat,
			"lon":  lon,
			"tags": el.Tags,
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
func (a *GeoEnrichAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, nil
	}
	idVal, _ := m["id"].(float64)
	if idVal == 0 {
		return datasource.CanonicalRecord{}, nil
	}
	lat, _ := m["lat"].(float64)
	lon, _ := m["lon"].(float64)
	tags, _ := m["tags"].(map[string]interface{})
	name := ""
	if tags != nil {
		if n, ok := tags["name"].(string); ok {
			name = n
		}
	}
	if name == "" {
		name = fmt.Sprintf("OSM %s %d", m["type"], int64(idVal))
	}
	id := fmt.Sprintf("osm_%s_%d", m["type"], int64(idVal))
	fac := &models.Facility{
		ID:            id,
		CanonicalPath: "/fac/eu/osm_" + fmt.Sprintf("%d", int64(idVal)),
		Name:          name,
		FacilityKind:  taxonomy.FacilityKindTerminal,
		FacilityRole:  taxonomy.FacilityRoleStorage,
		Country:       "EU",
		MaterialID:     "crude_oil",
		Stage:         2,
		RiskScore:     1.0,
		UpdatedAt:     time.Now(),
	}
	if lat != 0 || lon != 0 {
		fac.Coordinates = &latlng.LatLng{Latitude: lat, Longitude: lon}
	}
	return datasource.CanonicalRecord{
		Kind:    "facility",
		ID:      id,
		Payload: fac,
	}, nil
}

// Schema implements datasource.Adapter.
func (a *GeoEnrichAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "geo_enrich", RecordKind: "facility", Fields: []string{"type", "id", "lat", "lon", "tags"}}
}

// RateLimit implements datasource.Adapter.
func (a *GeoEnrichAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 10, Burst: 2}
}
