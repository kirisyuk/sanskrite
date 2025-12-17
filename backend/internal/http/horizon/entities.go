package horizon

import (
	"encoding/json"
	"net/http"

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

	// Генерируем embedding для запроса через EmbeddingProvider
	embeddingProvider := h.service.EmbeddingProvider()
	if embeddingProvider == nil {
		http.Error(w, "Embedding provider not available", http.StatusInternalServerError)
		return
	}

	embeddings, err := embeddingProvider.EmbedTexts(r.Context(), []string{req.Query})
	if err != nil {
		http.Error(w, "Failed to generate embedding", http.StatusInternalServerError)
		return
	}

	if len(embeddings) == 0 {
		http.Error(w, "Failed to generate embedding", http.StatusInternalServerError)
		return
	}

	// Выполняем семантический поиск
	fragments, err := h.service.Fragments().SearchByVectorText(r.Context(), embeddings[0], req.Limit)
	if err != nil {
		// Если векторный поиск не реализован, возвращаем ошибку
		http.Error(w, "Vector search not available: "+err.Error(), http.StatusNotImplemented)
		return
	}

	// Преобразуем в формат ответа для Live Entity Recognition
	entities := make([]map[string]interface{}, 0, len(fragments))
	for _, fragment := range fragments {
		entity := map[string]interface{}{
			"id":            fragment.ID,
			"canonicalPath": fragment.CanonicalPath,
			"name":          fragment.Labels["en"],
			"type":          fragment.Type,
			"entityPath":    fragment.EntityPath,
			"sectorPath":    fragment.SectorPath,
			"description":   fragment.Description,
		}
		if fragment.HomeLocation != nil {
			entity["location"] = fragment.HomeLocation
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
