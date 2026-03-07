package horizon

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	mapactivity "github.com/igorkirisyuk/sanskrite/internal/horizon/mapactivity"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// ListMapActivity returns heuristic map activity for static zones/flows.
// GET /api/horizon/map-activity?lensId=...&minHype=10
func (h *HorizonHandlers) ListMapActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if h.factsRepo == nil {
		http.Error(w, "Ontology facts repository is not configured", http.StatusServiceUnavailable)
		return
	}

	minHype := 0.0
	if raw := strings.TrimSpace(r.URL.Query().Get("minHype")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			minHype = v
		}
	}
	if minHype < 0 {
		minHype = 0
	}
	if minHype > 100 {
		minHype = 100
	}

	facts, err := h.factsRepo.ListByTenantAndKind(r.Context(), "global", "map_activity", 4000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snapshot := mapactivity.BuildSnapshot(facts, time.Now().UTC())

	lensID := strings.TrimSpace(r.URL.Query().Get("lensId"))
	if lensID != "" {
		countries := h.collectLensCountries(r, lensID)
		if len(countries) > 0 {
			filteredZones := make([]mapactivity.ActivityView, 0, len(snapshot.Zones))
			for _, z := range snapshot.Zones {
				if countries[strings.ToUpper(strings.TrimSpace(z.CountryISO2))] {
					filteredZones = append(filteredZones, z)
				}
			}
			snapshot.Zones = filteredZones

			filteredFlows := make([]mapactivity.ActivityView, 0, len(snapshot.Flows))
			for _, f := range snapshot.Flows {
				if flowMatchesCountries(f, countries) {
					filteredFlows = append(filteredFlows, f)
				}
			}
			snapshot.Flows = filteredFlows
		}
	}

	if minHype > 0 {
		zones := snapshot.Zones[:0]
		for _, z := range snapshot.Zones {
			if z.Hype >= minHype {
				zones = append(zones, z)
			}
		}
		snapshot.Zones = zones
		flows := snapshot.Flows[:0]
		for _, f := range snapshot.Flows {
			if f.Hype >= minHype {
				flows = append(flows, f)
			}
		}
		snapshot.Flows = flows
	}

	sort.Slice(snapshot.Zones, func(i, j int) bool {
		if snapshot.Zones[i].Hype == snapshot.Zones[j].Hype {
			return snapshot.Zones[i].Name < snapshot.Zones[j].Name
		}
		return snapshot.Zones[i].Hype > snapshot.Zones[j].Hype
	})
	sort.Slice(snapshot.Flows, func(i, j int) bool {
		if snapshot.Flows[i].Hype == snapshot.Flows[j].Hype {
			return snapshot.Flows[i].Name < snapshot.Flows[j].Name
		}
		return snapshot.Flows[i].Hype > snapshot.Flows[j].Hype
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt": snapshot.GeneratedAt,
		"schema":      snapshot.Schema,
		"lensId":      lensID,
		"minHype":     minHype,
		"zones":       snapshot.Zones,
		"flows":       snapshot.Flows,
		"totals": map[string]any{
			"zones": len(snapshot.Zones),
			"flows": len(snapshot.Flows),
		},
	})
}

func (h *HorizonHandlers) collectLensCountries(r *http.Request, lensID string) map[string]bool {
	out := map[string]bool{}
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		return out
	}
	addPathCountries := func(paths []string) {
		for _, p := range paths {
			for _, cc := range countriesFromCanonicalPath(p) {
				out[cc] = true
			}
		}
	}
	addPathCountries(lens.Scope)
	for _, arr := range lens.ResolvedQueries {
		addPathCountries(arr)
	}
	return out
}

func countriesFromCanonicalPath(path string) []string {
	path = strings.TrimSpace(strings.ToLower(path))
	if path == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	out := []string{}
	if len(parts) >= 2 && parts[0] == "loc" {
		if len(parts[1]) == 2 {
			out = append(out, strings.ToUpper(parts[1]))
		}
	}
	if len(parts) >= 2 && parts[0] == "zone" {
		for _, token := range strings.Split(parts[1], "-") {
			token = strings.TrimSpace(token)
			if len(token) == 2 {
				out = append(out, strings.ToUpper(token))
			}
		}
	}
	return out
}

func flowMatchesCountries(flow mapactivity.ActivityView, countries map[string]bool) bool {
	if len(countries) == 0 {
		return true
	}
	for _, hint := range flow.CountryHints {
		if countries[strings.ToUpper(strings.TrimSpace(hint))] {
			return true
		}
	}
	// Keep very hot global flows even if no explicit country hint.
	if flow.Hype >= 68 {
		return true
	}
	return false
}
