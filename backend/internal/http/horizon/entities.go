package horizon

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// SearchEntities выполняет Live Entity Recognition через семантический поиск
func (h *HorizonHandlers) SearchEntities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Limit > 50 {
		req.Limit = 50
	}

	// Выполняем семантический поиск через Vertex AI Search
	fragments, err := h.service.SearchFragments(r.Context(), req.Query, req.Limit)
	if err != nil {
		log.Printf("[SearchEntities] Search failed: %v", err)
		http.Error(w, "Search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Преобразуем в формат ответа для Live Entity Recognition
	entities := make([]map[string]interface{}, 0, len(fragments))
	for _, fragment := range fragments {
		// Извлекаем имя: предпочитаем Labels["en"], fallback на Identifiers или canonicalPath
		name := ""
		if fragment.Labels != nil {
			name = fragment.Labels["en"]
		}
		if name == "" && fragment.Identifiers != nil {
			if shortName := fragment.Identifiers["shortName"]; shortName != "" {
				name = shortName
			} else if legalName := fragment.Identifiers["legalName"]; legalName != "" {
				name = legalName
			}
		}
		if name == "" {
			// Fallback: извлекаем имя из canonicalPath
			parts := strings.Split(fragment.CanonicalPath, "/")
			if len(parts) > 0 {
				lastPart := parts[len(parts)-1]
				name = strings.Title(strings.ReplaceAll(lastPart, "-", " "))
			}
		}

		// Извлекаем location и sector из fragment
		location := ""
		if fragment.HomeLocation != nil {
			if fragment.HomeLocation.ISO3166_1 != nil {
				location = *fragment.HomeLocation.ISO3166_1
			}
		}

		sector := ""
		if fragment.SectorPath != "" {
			parts := strings.Split(strings.Trim(fragment.SectorPath, "/"), "/")
			if len(parts) > 0 {
				sector = parts[len(parts)-1]
			}
		}

		entity := map[string]interface{}{
			"id":            fragment.ID,
			"canonicalPath": fragment.CanonicalPath,
			"name":          name,
			"type":          string(fragment.Type),
			"entityPath":    fragment.EntityPath,
			"sectorPath":    fragment.SectorPath,
			"description":   fragment.Description,
		}
		if location != "" {
			entity["location"] = location
		}
		if sector != "" {
			entity["sector"] = sector
		}
		entities = append(entities, entity)
	}

	response := map[string]interface{}{
		"entities": entities,
		"total":    len(entities),
		"query":    req.Query,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
