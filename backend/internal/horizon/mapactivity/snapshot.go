package mapactivity

import (
	"sort"
	"strings"
	"time"

	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

type ActivityView struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	CountryISO2   string   `json:"countryIso2,omitempty"`
	CountryName   string   `json:"countryName,omitempty"`
	ZoneType      string   `json:"zoneType,omitempty"`
	FlowMode      string   `json:"flowMode,omitempty"`
	CountryHints  []string `json:"countryHints,omitempty"`
	Score         float64  `json:"score"`
	Hype          float64  `json:"hype"`
	HypeIndicator int      `json:"hypeIndicator"`
	Status        string   `json:"status"`
	Hits1h        float64  `json:"hits1h"`
	Hits24h       float64  `json:"hits24h"`
	TotalMentions float64  `json:"totalMentions"`
	LastBoost     float64  `json:"lastBoost"`
	LastMatchedAt string   `json:"lastMatchedAt,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
	LastTitle     string   `json:"lastTitle,omitempty"`
}

type Snapshot struct {
	GeneratedAt string         `json:"generatedAt"`
	Schema      string         `json:"schema"`
	Zones       []ActivityView `json:"zones"`
	Flows       []ActivityView `json:"flows"`
}

func BuildSnapshot(facts []*coremodels.OntologyFact, now time.Time) Snapshot {
	catalog := LoadCatalog()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := Snapshot{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Schema:      strings.TrimSpace(catalog.Schema),
		Zones:       []ActivityView{},
		Flows:       []ActivityView{},
	}
	for _, f := range facts {
		if f == nil || strings.TrimSpace(f.Kind) != mapActivityFactKind {
			continue
		}
		kind := ""
		t := strings.TrimSpace(strings.ToLower(f.Type))
		switch t {
		case "zone_activity":
			kind = "zone"
		case "flow_activity":
			kind = "flow"
		}
		if kind == "" {
			continue
		}
		itemID := tagValue(f.Tags, "map_item_id:")
		if itemID == "" {
			continue
		}
		st := parsePersistedState(f)
		st.applyDecay(now)
		view := ActivityView{
			ID:            itemID,
			Kind:          kind,
			Score:         round2(clamp(st.Score, 0, 100)),
			Hype:          round2(clamp(st.Score, 0, 100)),
			HypeIndicator: hypeIndicator(st.Score),
			Status:        activityStatus(st.Score),
			Hits1h:        round2(st.Hits1h),
			Hits24h:       round2(st.Hits24h),
			TotalMentions: round2(st.TotalMentions),
			LastBoost:     round2(st.LastBoost),
			LastTitle:     strings.TrimSpace(f.Description),
		}
		if !st.LastMatchedAt.IsZero() {
			view.LastMatchedAt = st.LastMatchedAt.UTC().Format(time.RFC3339)
		}
		if !f.UpdatedAt.IsZero() {
			view.UpdatedAt = f.UpdatedAt.UTC().Format(time.RFC3339)
		}
		if kind == "zone" {
			if z, ok := catalog.ZoneByID[itemID]; ok {
				view.Name = z.Name
				view.CountryISO2 = z.CountryISO2
				view.CountryName = z.CountryName
				view.ZoneType = z.ZoneType
			} else {
				view.Name = strings.TrimSpace(f.Title)
			}
			out.Zones = append(out.Zones, view)
			continue
		}
		if fl, ok := catalog.FlowByID[itemID]; ok {
			view.Name = fl.Name
			view.FlowMode = fl.FlowMode
			view.CountryHints = append([]string(nil), fl.CountryHints...)
		} else {
			view.Name = strings.TrimSpace(f.Title)
		}
		out.Flows = append(out.Flows, view)
	}

	sort.Slice(out.Zones, func(i, j int) bool {
		if out.Zones[i].Score == out.Zones[j].Score {
			return out.Zones[i].Name < out.Zones[j].Name
		}
		return out.Zones[i].Score > out.Zones[j].Score
	})
	sort.Slice(out.Flows, func(i, j int) bool {
		if out.Flows[i].Score == out.Flows[j].Score {
			return out.Flows[i].Name < out.Flows[j].Name
		}
		return out.Flows[i].Score > out.Flows[j].Score
	})
	return out
}

func tagValue(tags []string, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(t, prefix))
		}
	}
	return ""
}

func activityStatus(score float64) string {
	switch {
	case score >= 72:
		return "hot"
	case score >= 48:
		return "active"
	case score >= 26:
		return "watch"
	case score >= 10:
		return "cooling"
	default:
		return "inactive"
	}
}

func hypeIndicator(score float64) int {
	switch {
	case score >= 80:
		return 5
	case score >= 60:
		return 4
	case score >= 40:
		return 3
	case score >= 20:
		return 2
	case score >= 8:
		return 1
	default:
		return 0
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
