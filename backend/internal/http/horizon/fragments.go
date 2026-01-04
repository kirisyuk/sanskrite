package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ListFragments получает список HorizonFragment через Lens scope
func (h *HorizonHandlers) ListFragments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Получаем lensID из query параметра
	lensID := r.URL.Query().Get("lensId")
	if lensID == "" {
		http.Error(w, "lensId query parameter is required", http.StatusBadRequest)
		return
	}

	// Получаем Lens для определения scope
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ к Lens
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем все HorizonFragment из scope
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), lens.Scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"fragments": fragments,
		"total":     len(fragments),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFragment получает HorizonFragment по ID
func (h *HorizonHandlers) GetFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем ID из пути: /api/horizon/fragments/{id}
	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Ensure entity mapping on access (best-effort)
	_, _, _, _ = h.service.EnsureEntityForFragmentObject(r.Context(), fragment)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fragment)
}

// GetFragmentByPath получает HorizonFragment по canonicalPath
func (h *HorizonHandlers) GetFragmentByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	canonicalPath := r.URL.Query().Get("path")
	if canonicalPath == "" {
		http.Error(w, "path query parameter is required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().GetByCanonicalPath(r.Context(), canonicalPath)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Ensure entity mapping on access (best-effort)
	_, _, _, _ = h.service.EnsureEntityForFragmentObject(r.Context(), fragment)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fragment)
}

// EnsureFragmentEntity triggers create/link/enrich for a fragment's entity mapping.
// POST /api/horizon/fragments/{id}/ensure-entity
func (h *HorizonHandlers) EnsureFragmentEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/ensure-entity"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	entity, created, enriched, err := h.service.EnsureEntityForFragment(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entity == nil {
		// Not supported / no-op
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"fragmentId": fragmentID,
			"entity":     nil,
			"created":    false,
			"enriched":   false,
			"status":     "noop",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"fragmentId": fragmentID,
		"entityId":   entity.ID,
		"entity":     entity,
		"created":    created,
		"enriched":   enriched,
		"status":     "ok",
	})
}

// GetFragmentEntity returns the entity mapped to a fragment (ensuring it exists by default).
// GET /api/horizon/fragments/{id}/entity
func (h *HorizonHandlers) GetFragmentEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/entity"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	// Ensure on access (default behavior)
	entity, _, _, err := h.service.EnsureEntityForFragment(r.Context(), fragmentID)
	if err != nil {
		log.Printf("[GetFragmentEntity] EnsureEntityForFragment failed: fragmentId=%s err=%v", fragmentID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entity == nil {
		http.Error(w, "Entity not available for this fragment type", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entity)
}

// EnrichFragmentEntity forces or triggers Wikidata enrichment for the entity linked to a fragment.
// POST /api/horizon/fragments/{id}/enrich-entity?force=1
func (h *HorizonHandlers) EnrichFragmentEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/enrich-entity"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force")
	forceBool := force == "1" || strings.EqualFold(force, "true")

	entity, err := h.service.EnrichEntityForFragment(r.Context(), fragmentID, forceBool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"fragmentId": fragmentID,
		"entityId":   entity.ID,
		"force":      forceBool,
		"entity":     entity,
	})
}

// RefreshFragment создает ScoutingJob для принудительного обновления HorizonFragment
func (h *HorizonHandlers) RefreshFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	if fragmentID == "" {
		http.Error(w, "Fragment ID required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Strategy    string   `json:"strategy"`
		MaxCost     float64  `json:"maxCost"`
		Specialties []string `json:"specialties,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Определяем стратегию
	strategy := models.ScoutingStrategySnapshot
	if req.Strategy != "" {
		strategy = models.ScoutingStrategy(req.Strategy)
	}

	// Создаем ScoutingJob для обновления
	scoutEngine := h.service.Scout()
	if scoutEngine == nil {
		http.Error(w, "Scout Engine not available", http.StatusServiceUnavailable)
		return
	}

	// Создаем ScoutingJob через репозиторий
	scoutingJob := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  fragment.TenantID,
			ProjectID: "", // Обновление не привязано к проекту
			Status:    "pending",
			AgentID:   "scout",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			State: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"currentState":        string(models.ScoutingJobStatePending),
			},
		},
		TargetCanonicalPath: fragment.CanonicalPath,
		ScoutingStrategy:    string(strategy),
		CurrentState:        models.ScoutingJobStatePending,
	}
	// Optional: pass specialties into job state (normalized).
	if specs := taxonomy.NormalizeSpecialties(req.Specialties); len(specs) > 0 {
		if scoutingJob.State == nil {
			scoutingJob.State = map[string]interface{}{}
		}
		scoutingJob.State["specialties"] = specs
	}

	jobID, err := h.service.ScoutingJobs().Create(r.Context(), scoutingJob)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create scouting job: %v", err), http.StatusInternalServerError)
		return
	}

	// Запускаем выполнение ScoutingJob асинхронно
	go func() {
		ctx := context.Background()
		scoutingJob.ID = jobID
		if err := scoutEngine.ExecuteScoutingJob(ctx, scoutingJob); err != nil {
			// Логируем ошибку, но не блокируем ответ
			_ = err
		}
	}()

	response := map[string]interface{}{
		"scoutingJobId":       jobID,
		"status":              "pending",
		"estimatedCompletion": nil,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

// ExpandProductMaterials expands upstream and downstream materials for a product fragment.
// POST /api/horizon/fragments/{id}/expand-materials
// Body (optional): {"upstreamDepth": 3, "downstreamDepth": 3, "includeProducers": true}
// Response: {"productId": "...", "upstreamCount": N, "downstreamCount": N, "producersCount": N, "duration": "..."}
func (h *HorizonHandlers) ExpandProductMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Extract fragment ID from path: /api/horizon/fragments/{id}/expand-materials
	fragmentIDOrPath := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentIDOrPath = strings.TrimSuffix(fragmentIDOrPath, "/expand-materials")
	if fragmentIDOrPath == "" {
		http.Error(w, "Fragment ID or canonical path required", http.StatusBadRequest)
		return
	}

	// Get Scout Engine
	scoutEngine := h.service.Scout()
	if scoutEngine == nil {
		http.Error(w, "Scout Engine not available", http.StatusServiceUnavailable)
		return
	}

	// Parse optional request body for options
	var reqBody struct {
		UpstreamDepth    int   `json:"upstreamDepth"`
		DownstreamDepth  int   `json:"downstreamDepth"`
		IncludeProducers *bool `json:"includeProducers"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			// Ignore parse errors, use defaults
			_ = err
		}
	}

	// Build options
	opts := scout.ExpandProductMaterialsOptions{
		UpstreamDepth:    3,
		DownstreamDepth:  3,
		IncludeProducers: true,
	}
	if reqBody.UpstreamDepth > 0 {
		opts.UpstreamDepth = reqBody.UpstreamDepth
	}
	if reqBody.DownstreamDepth > 0 {
		opts.DownstreamDepth = reqBody.DownstreamDepth
	}
	if reqBody.IncludeProducers != nil {
		opts.IncludeProducers = *reqBody.IncludeProducers
	}

	// Create a context with timeout for the synchronous operation
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Call the expand method
	result, err := scoutEngine.ExpandProductMaterials(ctx, fragmentIDOrPath, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to expand materials: %v", err), http.StatusBadRequest)
		return
	}

	// Build response
	response := map[string]interface{}{
		"productId":       result.ProductID,
		"productPath":     result.ProductPath,
		"upstreamCount":   result.UpstreamCount,
		"downstreamCount": result.DownstreamCount,
		"producersCount":  result.ProducersCount,
		"edgesCount":      result.EdgesCount,
		"duration":        result.Duration.String(),
		"status":          "completed",
	}
	if result.WikidataID != "" {
		response["wikidataId"] = result.WikidataID
	}
	if len(result.Errors) > 0 {
		response["warnings"] = result.Errors
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFragmentVersions получает историю версий HorizonFragment
func (h *HorizonHandlers) GetFragmentVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем fragmentID из пути: /api/horizon/fragments/{id}/versions
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 2 || pathParts[1] != "versions" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	fragmentID := pathParts[0]

	// Проверяем существование фрагмента
	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// Получаем версии через репозиторий
	versions, err := h.service.FragmentVersions().ListByFragment(r.Context(), fragment.TenantID, fragmentID, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get versions: %v", err), http.StatusInternalServerError)
		return
	}

	// Преобразуем в формат ответа
	versionResponses := make([]map[string]interface{}, 0, len(versions))
	for _, v := range versions {
		versionResponses = append(versionResponses, map[string]interface{}{
			"versionId":     v.VersionID,
			"state":         v.State,
			"sourceFrameId": v.SourceFrameID,
			"timestamp":     v.Timestamp,
		})
	}

	response := map[string]interface{}{
		"versions": versionResponses,
		"total":    len(versions),
		"limit":    limit,
		"offset":   offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
