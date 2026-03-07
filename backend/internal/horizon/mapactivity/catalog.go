package mapactivity

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"unicode"
)

//go:embed data/map_activity_index.json
var embeddedCatalogJSON []byte

type rawCatalog struct {
	Schema string        `json:"schema"`
	Zones  []rawZoneItem `json:"zones"`
	Flows  []rawFlowItem `json:"flows"`
}

type rawZoneItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	CountryISO2 string   `json:"countryIso2"`
	CountryName string   `json:"countryName"`
	ZoneType    string   `json:"zoneType"`
	ZoneSubType string   `json:"zoneSubType"`
	Tags        []string `json:"tags"`
}

type rawFlowItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	FlowMode    string   `json:"flowMode"`
	FlowSubType string   `json:"flowSubType"`
	Tags        []string `json:"tags"`
}

type ZoneDefinition struct {
	ID          string
	Name        string
	CountryISO2 string
	CountryName string
	ZoneType    string
	ZoneSubType string
	Tags        []string
	Keywords    []string
}

type FlowDefinition struct {
	ID           string
	Name         string
	FlowMode     string
	FlowSubType  string
	Tags         []string
	Keywords     []string
	CountryHints []string
}

type Catalog struct {
	Schema     string
	Zones      []ZoneDefinition
	Flows      []FlowDefinition
	ZoneByID   map[string]ZoneDefinition
	FlowByID   map[string]FlowDefinition
	countryMap map[string]string // normalized country name token -> iso2
	countryLex map[string]string // normalized country alias phrase -> iso2
}

var (
	catalogOnce sync.Once
	catalogInst *Catalog
)

func LoadCatalog() *Catalog {
	catalogOnce.Do(func() {
		catalogInst = buildCatalog()
	})
	return catalogInst
}

func buildCatalog() *Catalog {
	out := &Catalog{
		Schema:   "map_activity_index.v1",
		ZoneByID: make(map[string]ZoneDefinition),
		FlowByID: make(map[string]FlowDefinition),
	}
	if len(embeddedCatalogJSON) == 0 {
		return out
	}
	var raw rawCatalog
	if err := json.Unmarshal(embeddedCatalogJSON, &raw); err != nil {
		return out
	}
	if strings.TrimSpace(raw.Schema) != "" {
		out.Schema = strings.TrimSpace(raw.Schema)
	}

	countryNameToISO := make(map[string]string)
	countryLex := make(map[string]string)
	for _, z := range raw.Zones {
		zone := ZoneDefinition{
			ID:          strings.TrimSpace(z.ID),
			Name:        strings.TrimSpace(z.Name),
			CountryISO2: strings.ToUpper(strings.TrimSpace(z.CountryISO2)),
			CountryName: strings.TrimSpace(z.CountryName),
			ZoneType:    strings.TrimSpace(z.ZoneType),
			ZoneSubType: strings.TrimSpace(z.ZoneSubType),
			Tags:        normalizeStrings(z.Tags),
		}
		if zone.ID == "" {
			continue
		}
		zone.Keywords = buildKeywords(zone.Name, zone.ZoneSubType, zone.ZoneType, zone.CountryName, zone.CountryISO2, strings.Join(zone.Tags, " "), zone.ID)
		out.Zones = append(out.Zones, zone)
		out.ZoneByID[zone.ID] = zone

		if zone.CountryISO2 != "" {
			countryNameToISO[strings.ToLower(zone.CountryISO2)] = zone.CountryISO2
		}
		if zone.CountryName != "" {
			phrase := normalizePhrase(zone.CountryName)
			if phrase != "" && zone.CountryISO2 != "" {
				countryLex[phrase] = zone.CountryISO2
			}
			for _, t := range splitTokens(zone.CountryName) {
				if len(t) >= 3 {
					countryNameToISO[t] = zone.CountryISO2
				}
			}
			countryNameToISO[normalizePhrase(zone.CountryName)] = zone.CountryISO2
		}
	}
	out.countryMap = countryNameToISO
	for alias, iso := range defaultCountryAliases {
		alias = normalizePhrase(alias)
		iso = strings.ToUpper(strings.TrimSpace(iso))
		if alias == "" || iso == "" {
			continue
		}
		countryLex[alias] = iso
	}
	out.countryLex = countryLex

	for _, f := range raw.Flows {
		flow := FlowDefinition{
			ID:          strings.TrimSpace(f.ID),
			Name:        strings.TrimSpace(f.Name),
			FlowMode:    strings.TrimSpace(f.FlowMode),
			FlowSubType: strings.TrimSpace(f.FlowSubType),
			Tags:        normalizeStrings(f.Tags),
		}
		if flow.ID == "" {
			continue
		}
		flow.Keywords = buildKeywords(flow.Name, flow.FlowSubType, flow.FlowMode, strings.Join(flow.Tags, " "), flow.ID)
		flow.CountryHints = inferCountryHints(flow, countryNameToISO)
		out.Flows = append(out.Flows, flow)
		out.FlowByID[flow.ID] = flow
	}
	return out
}

var defaultCountryAliases = map[string]string{
	"u s":          "US",
	"u s a":        "US",
	"usa":          "US",
	"american":     "US",
	"u k":          "GB",
	"uk":           "GB",
	"britain":      "GB",
	"british":      "GB",
	"uae":          "AE",
	"emirati":      "AE",
	"iranian":      "IR",
	"israeli":      "IL",
	"russian":      "RU",
	"ukrainian":    "UA",
	"chinese":      "CN",
	"taiwanese":    "TW",
	"south korean": "KR",
	"north korean": "KP",
	"saudi":        "SA",
	"qatari":       "QA",
	"yemeni":       "YE",
	"omani":        "OM",
	"afghan":       "AF",
	"pakistani":    "PK",
	"turkish":      "TR",
	"egyptian":     "EG",
	"syrian":       "SY",
	"jordanian":    "JO",
	"iraqi":        "IQ",
	"lebanese":     "LB",
	"moroccan":     "MA",
	"algerian":     "DZ",
	"tunisian":     "TN",
	"libyan":       "LY",
	"nigerian":     "NG",
	"ethiopian":    "ET",
	"kenyan":       "KE",
	"sudanese":     "SD",
	"indian":       "IN",
	"japanese":     "JP",
	"australian":   "AU",
	"new zealand":  "NZ",
	"french":       "FR",
	"german":       "DE",
	"italian":      "IT",
	"spanish":      "ES",
	"portuguese":   "PT",
	"dutch":        "NL",
	"belgian":      "BE",
	"norwegian":    "NO",
	"swedish":      "SE",
	"finnish":      "FI",
	"danish":       "DK",
	"polish":       "PL",
	"romanian":     "RO",
	"bulgarian":    "BG",
	"georgian":     "GE",
	"azerbaijani":  "AZ",
	"armenian":     "AM",
	"kazakh":       "KZ",
	"uzbek":        "UZ",
	"turkmen":      "TM",
	"kyrgyz":       "KG",
	"tajik":        "TJ",
	"mexican":      "MX",
	"canadian":     "CA",
	"brazilian":    "BR",
	"argentine":    "AR",
	"colombian":    "CO",
	"peruvian":     "PE",
	"chilean":      "CL",
	"ecuadorian":   "EC",
	"venezuelan":   "VE",
}

func (c *Catalog) inferCountryCodes(text string) []string {
	if c == nil || len(c.countryLex) == 0 {
		return nil
	}
	text = normalizePhrase(text)
	if text == "" {
		return nil
	}
	padded := " " + text + " "
	scores := map[string]int{}
	for alias, iso := range c.countryLex {
		if alias == "" || iso == "" {
			continue
		}
		if strings.Contains(alias, " ") {
			if strings.Contains(text, alias) {
				scores[iso] += 2
			}
			continue
		}
		needle := " " + alias + " "
		if strings.Contains(padded, needle) {
			scores[iso]++
		}
	}
	if len(scores) == 0 {
		return nil
	}
	type ccScore struct {
		CC    string
		Score int
	}
	ordered := make([]ccScore, 0, len(scores))
	for cc, score := range scores {
		ordered = append(ordered, ccScore{CC: cc, Score: score})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Score == ordered[j].Score {
			return ordered[i].CC < ordered[j].CC
		}
		return ordered[i].Score > ordered[j].Score
	})
	limit := 4
	if len(ordered) < limit {
		limit = len(ordered)
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, ordered[i].CC)
	}
	return out
}

func inferCountryHints(flow FlowDefinition, countryMap map[string]string) []string {
	text := normalizePhrase(strings.Join([]string{flow.ID, flow.Name, flow.FlowSubType, strings.Join(flow.Tags, " ")}, " "))
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	for token, iso := range countryMap {
		if iso == "" || token == "" {
			continue
		}
		if strings.Contains(text, token) {
			seen[iso] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for iso := range seen {
		out = append(out, iso)
	}
	return out
}

func normalizeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = normalizePhrase(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func buildKeywords(parts ...string) []string {
	stop := map[string]struct{}{
		"and": {}, "the": {}, "zone": {}, "corridor": {}, "flow": {}, "mainline": {},
		"with": {}, "from": {}, "into": {}, "for": {}, "north": {}, "south": {}, "east": {}, "west": {},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 24)
	for _, p := range parts {
		n := normalizePhrase(p)
		if n == "" {
			continue
		}
		if len(n) >= 4 {
			if _, ok := seen[n]; !ok {
				seen[n] = struct{}{}
				out = append(out, n)
			}
		}
		for _, tok := range splitTokens(n) {
			if len(tok) < 3 {
				continue
			}
			if _, bad := stop[tok]; bad {
				continue
			}
			if _, ok := seen[tok]; ok {
				continue
			}
			seen[tok] = struct{}{}
			out = append(out, tok)
		}
	}
	return out
}

func splitTokens(s string) []string {
	s = normalizePhrase(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizePhrase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}
