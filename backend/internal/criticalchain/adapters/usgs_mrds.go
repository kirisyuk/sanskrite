package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"google.golang.org/genproto/googleapis/type/latlng"
)

// MRDS commodity codes -> our material_id (https://mrdata.usgs.gov/mrds/commodity-codes.html)
var mrdsCommodityToMaterial = map[string]string{
	"CO":   "cobalt",
	"LI":   "lithium",
	"REE":  "ree",
	"HE":   "helium",
	"SIL":  "silicon",
	"SIL_F": "silicon",
	"GAS":  "natural_gas",
	"OIL":  "crude_oil",
	"OIL_G": "crude_oil",
	"OIL_R": "crude_oil",
	"OIL_SA": "crude_oil",
	"OIL_SH": "crude_oil",
	"NI":   "cobalt", // nickel-cobalt deposits
	"NI_L": "cobalt",
	"NI_R": "cobalt",
	"NI_S": "cobalt",
}

const usgsMRDSBase = "https://mrdata.usgs.gov"

// USGSMRDSAdapter fetches minerals and facilities from USGS Mineral Resources Data System.
type USGSMRDSAdapter struct {
	client *http.Client
}

// NewUSGSMRDSAdapter creates a new USGS MRDS adapter.
func NewUSGSMRDSAdapter() *USGSMRDSAdapter {
	return &USGSMRDSAdapter{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements DataSourceAdapter.
func (a *USGSMRDSAdapter) Name() string { return "usgs_mrds" }

// Fetch implements datasource.Adapter. Fetches MRDS records (no API key required).
func (a *USGSMRDSAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	u := usgsMRDSBase + "/mrds/search-bbox.php?minlon=-180&maxlon=180&minlat=-90&maxlat=90&fmt=2"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("[CriticalChain/USGS] fetch error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var raw []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[CriticalChain/USGS] parse error: %v", err)
		return nil, nil
	}
	return a.wrapRecords(raw), nil
}

func (a *USGSMRDSAdapter) wrapRecords(raw []map[string]interface{}) []datasource.RawRecord {
	out := make([]datasource.RawRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, datasource.RawRecord{
			Source:    a.Name(),
			Data:      r,
			FetchedAt: time.Now(),
		})
	}
	return out
}

// Normalize implements datasource.Adapter.
func (a *USGSMRDSAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record type")
	}
	id := stringPtr(m, "dep_id")
	if id == "" {
		id = stringPtr(m, "dep_id")
	}
	if id == "" {
		id = fmt.Sprintf("usgs_%v", m["record_id"])
	}
	name := stringPtr(m, "dep_name")
	if name == "" {
		name = stringPtr(m, "dep_id")
	}
	country := stringPtr(m, "country")
	if len(country) > 3 {
		country = country[:3]
	}
	materialID := mrdsCommodityToMaterialID(m)
	if materialID == "" {
		materialID = "lithium" // fallback for unknown commodities
	}
	fac := &models.Facility{
		ID:            id,
		CanonicalPath: "/fac/" + country + "/" + slugify(name),
		Name:          name,
		FacilityKind:  taxonomy.FacilityKindMine,
		FacilityRole:  taxonomy.FacilityRoleExtraction,
		Country:       country,
		MaterialID:    materialID,
		Stage:         0,
		RiskScore:     1.0,
		UpdatedAt:     time.Now(),
	}
	if lat, lon := floatPtr(m, "lat"), floatPtr(m, "lon"); lat != 0 || lon != 0 {
		fac.Coordinates = &latlng.LatLng{Latitude: lat, Longitude: lon}
	}
	return datasource.CanonicalRecord{
		Kind:    "facility",
		ID:      id,
		Payload: fac,
	}, nil
}

// Schema implements datasource.Adapter.
func (a *USGSMRDSAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{
		SourceID:   "usgs_mrds",
		RecordKind: "facility",
		Fields:     []string{"dep_id", "dep_name", "lat", "lon", "country", "commodity"},
	}
}

// RateLimit implements datasource.Adapter.
func (a *USGSMRDSAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 60, Burst: 10}
}

func mrdsCommodityToMaterialID(m map[string]interface{}) string {
	try := func(keys ...string) string {
		for _, k := range keys {
			raw := stringPtr(m, k)
			if raw == "" {
				continue
			}
			// MRDS can return "CO", "CO, CU", "CO;CU" etc
			parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
			for _, p := range parts {
				code := strings.TrimSpace(strings.ToUpper(p))
				if code == "" {
					continue
				}
				if mat, ok := mrdsCommodityToMaterial[code]; ok {
					return mat
				}
			}
		}
		return ""
	}
	return try("commodity", "commod1", "commod", "commodity1")
}

func stringPtr(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func floatPtr(m map[string]interface{}, k string) float64 {
	if v, ok := m[k]; ok && v != nil {
		switch x := v.(type) {
		case float64:
			return x
		case float32:
			return float64(x)
		case int:
			return float64(x)
		}
	}
	return 0
}

func slugify(s string) string {
	var b []byte
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b = append(b, byte(r))
		} else if r >= 'A' && r <= 'Z' {
			b = append(b, byte(r+32))
		} else if r == ' ' || r == '-' || r == '_' {
			if len(b) > 0 && b[len(b)-1] != '_' {
				b = append(b, '_')
			}
		}
	}
	if len(b) == 0 {
		return "unknown"
	}
	return string(b)
}
