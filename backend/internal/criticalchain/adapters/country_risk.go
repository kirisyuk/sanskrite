package adapters

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
)

const (
	stateDeptAPI = "https://cadataapi.state.gov/api/TravelAdvisories"
	fcdoAtom     = "https://www.gov.uk/foreign-travel-advice.atom"
)

var stateDeptLevelPattern = regexp.MustCompile(`(?i)Level\s+(\d)`)

// CountryRiskAdapter fetches country risk from State Dept, FCDO (public APIs, no keys).
type CountryRiskAdapter struct {
	client *http.Client
}

// NewCountryRiskAdapter creates a new country risk adapter.
func NewCountryRiskAdapter() *CountryRiskAdapter {
	return &CountryRiskAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name implements datasource.Adapter.
func (a *CountryRiskAdapter) Name() string { return "country_risk" }

// Fetch implements datasource.Adapter.
func (a *CountryRiskAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	var out []datasource.RawRecord

	// US State Department Travel Advisories
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stateDeptAPI, nil)
	if err == nil {
		resp, err := a.client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			var adv []struct {
				Title    string   `json:"Title"`
				Category []string `json:"Category"`
			}
			if decodeJSON(resp.Body, &adv) == nil {
				for _, r := range adv {
					countryCode := ""
					if len(r.Category) > 0 {
						countryCode = strings.TrimSpace(r.Category[0])
					}
					if countryCode == "" {
						continue
					}
					out = append(out, datasource.RawRecord{
						Source: "state_dept",
						Data: map[string]interface{}{
							"country": countryCode,
							"level":   extractStateDeptLevel(r.Title),
							"title":   r.Title,
						},
						FetchedAt: time.Now(),
					})
				}
			}
			resp.Body.Close()
		}
	}

	// FCDO Atom feed
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, fcdoAtom, nil)
	if err == nil {
		resp, err := a.client.Do(req2)
		if err == nil && resp.StatusCode == http.StatusOK {
			var feed struct {
				XMLName xml.Name `xml:"feed"`
				Entries []struct {
					Title   string `xml:"title"`
					ID      string `xml:"id"`
					Updated string `xml:"updated"`
					Links   []struct {
						Rel  string `xml:"rel,attr"`
						Href string `xml:"href,attr"`
					} `xml:"link"`
				} `xml:"entry"`
			}
			if xml.NewDecoder(resp.Body).Decode(&feed) == nil {
				for _, e := range feed.Entries {
					link := e.ID
					for _, l := range e.Links {
						if l.Rel == "alternate" && strings.TrimSpace(l.Href) != "" {
							link = l.Href
							break
						}
					}
					cc := extractCountryCode(link)
					if cc != "" {
						out = append(out, datasource.RawRecord{
							Source: "fcdo",
							Data: map[string]interface{}{
								"country": cc,
								"title":   e.Title,
							},
							FetchedAt: time.Now(),
						})
					}
				}
			}
			resp.Body.Close()
		}
	}

	return out, nil
}

func decodeJSON(body io.Reader, target interface{}) error {
	decoder := json.NewDecoder(body)
	return decoder.Decode(target)
}

func extractStateDeptLevel(title string) int {
	matches := stateDeptLevelPattern.FindStringSubmatch(title)
	if len(matches) < 2 {
		return 2
	}
	switch matches[1] {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	default:
		return 2
	}
}

func extractCountryCode(link string) string {
	// Link format: https://www.gov.uk/foreign-travel-advice/country-name
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	if len(parts) >= 1 {
		last := parts[len(parts)-1]
		if idx := strings.Index(last, "#"); idx >= 0 {
			last = last[:idx]
		}
		return last
	}
	return ""
}

// normalizeCountryCode maps slugs/names to ISO 3166-1 alpha-2 for choropleth.
func normalizeCountryCode(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	if len(s) == 2 || len(s) == 3 {
		if iso, ok := alpha3ToISO2[s]; ok {
			return iso
		}
		return s
	}
	// FCDO slugs and common names -> ISO2
	slugToISO := map[string]string{
		"FRANCE": "FR", "UK": "GB", "SPAIN": "ES", "ITALY": "IT", "GERMANY": "DE",
		"RUSSIA": "RU", "CHINA": "CN", "JAPAN": "JP", "INDIA": "IN", "BRAZIL": "BR",
		"MEXICO": "MX", "CANADA": "CA", "AUSTRALIA": "AU", "TURKEY": "TR", "IRAN": "IR",
		"IRAQ": "IQ", "SYRIA": "SY", "UKRAINE": "UA", "EGYPT": "EG", "SAUDI-ARABIA": "SA",
		"SOUTH-AFRICA": "ZA", "NIGERIA": "NG", "KENYA": "KE", "THAILAND": "TH",
		"VIETNAM": "VN", "INDONESIA": "ID", "MALAYSIA": "MY", "PHILIPPINES": "PH",
		"SOUTH-KOREA": "KR", "NORTH-KOREA": "KP", "PAKISTAN": "PK", "AFGHANISTAN": "AF",
		"LEBANON": "LB", "JORDAN": "JO", "ISRAEL": "IL", "PALESTINE": "PS",
		"YEMEN": "YE", "LIBYA": "LY", "SUDAN": "SD", "ETHIOPIA": "ET", "SOMALIA": "SO",
		"DEMOCRATIC-REPUBLIC-OF-THE-CONGO": "CD", "MALI": "ML", "NIGER": "NE",
		"BURKINA-FASO": "BF", "CHAD": "TD", "CENTRAL-AFRICAN-REPUBLIC": "CF",
		"MYANMAR": "MM", "BANGLADESH": "BD", "SRI-LANKA": "LK", "NEPAL": "NP",
		"COLOMBIA": "CO", "VENEZUELA": "VE", "PERU": "PE", "ECUADOR": "EC",
		"BOLIVIA": "BO", "ARGENTINA": "AR", "CHILE": "CL", "ZIMBABWE": "ZW",
		"MADAGASCAR": "MG", "MOZAMBIQUE": "MZ", "TANZANIA": "TZ", "UGANDA": "UG",
	}
	if iso, ok := slugToISO[s]; ok {
		return iso
	}
	// Try first 2 chars as fallback
	if len(s) >= 2 {
		return s[:2]
	}
	return s
}

var alpha3ToISO2 = map[string]string{
	"ARE": "AE",
	"AUS": "AU",
	"BRA": "BR",
	"CAN": "CA",
	"CHE": "CH",
	"CHL": "CL",
	"CHN": "CN",
	"DEU": "DE",
	"ESP": "ES",
	"FRA": "FR",
	"GBR": "GB",
	"IND": "IN",
	"IRN": "IR",
	"IRQ": "IQ",
	"ITA": "IT",
	"JPN": "JP",
	"KAZ": "KZ",
	"KOR": "KR",
	"MEX": "MX",
	"NOR": "NO",
	"QAT": "QA",
	"RUS": "RU",
	"SAU": "SA",
	"TUR": "TR",
	"TWN": "TW",
	"UKR": "UA",
	"USA": "US",
}

// Normalize implements datasource.Adapter. Produces conflict_zone records for country risk.
func (a *CountryRiskAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	country := stringVal(m, "country")
	if country == "" {
		return datasource.CanonicalRecord{}, fmt.Errorf("missing country")
	}
	level := int(floatVal(m, "level"))
	if level == 0 {
		level = 2
	}
	severity := models.SeverityMedium
	if level >= 4 {
		severity = models.SeverityCritical
	} else if level >= 3 {
		severity = models.SeverityHigh
	} else if level <= 1 {
		severity = models.SeverityLow
	}
	source := raw.Source
	source = strings.TrimSpace(source)
	if source == "" {
		source = "country_risk"
	}
	countryNorm := normalizeCountryCode(country)
	if countryNorm == "" {
		countryNorm = country
	}
	id := fmt.Sprintf("country_risk_%s_%s", source, countryNorm)
	cz := &models.ConflictZone{
		ID:        id,
		Name:      country + " - " + stringVal(m, "title"),
		Country:   countryNorm,
		Severity:  severity,
		Type:      models.ConflictTypePolitical,
		Source:    source,
		UpdatedAt: time.Now(),
	}
	return datasource.CanonicalRecord{
		Kind:    "conflict_zone",
		ID:      id,
		Payload: cz,
	}, nil
}

// Schema implements datasource.Adapter.
func (a *CountryRiskAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "country_risk", RecordKind: "conflict_zone", Fields: []string{"country", "level"}}
}

// RateLimit implements datasource.Adapter.
func (a *CountryRiskAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 60, Burst: 10}
}
