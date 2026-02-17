package horizon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type upsertUnitGraphRequest struct {
	DomainCanonicalPath string                 `json:"domainCanonicalPath,omitempty"`
	Edges               []models.UnitGraphEdge `json:"edges"`
	SourceJobID         string                 `json:"sourceJobId,omitempty"`
}

type unitDiveResultsListResponse struct {
	Results    []*models.UnitDiveResultArtifact `json:"results"`
	TotalCount int                              `json:"totalCount"`
}

type upsertUnitDiveResultRequest struct {
	DomainCanonicalPath string                `json:"domainCanonicalPath,omitempty"`
	RequestID           string                `json:"requestId,omitempty"`
	Result              models.UnitDiveResult `json:"result"`
	SourceJobID         string                `json:"sourceJobId,omitempty"`
}

func (h *HorizonHandlers) unitGraphRepoOrService() *horizonrepo.UnitGraphSnapshotsRepository {
	if h != nil && h.unitGraphRepo != nil {
		return h.unitGraphRepo
	}
	if h != nil && h.service != nil {
		return h.service.UnitGraphSnapshots()
	}
	return nil
}

func (h *HorizonHandlers) unitDiveRepoOrService() *horizonrepo.UnitDiveResultsRepository {
	if h != nil && h.unitDiveRepo != nil {
		return h.unitDiveRepo
	}
	if h != nil && h.service != nil {
		return h.service.UnitDiveResults()
	}
	return nil
}

func (h *HorizonHandlers) ensureLensAccess(r *http.Request) (lensID string, lens *models.Lens, errStatus int, errText string) {
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		return "", nil, http.StatusUnauthorized, "Authentication required"
	}
	lensID = strings.TrimSpace(extractIDFromPath(r, "/api/horizon/lenses/"))
	if lensID == "" {
		return "", nil, http.StatusBadRequest, "lensId required in path"
	}
	if h == nil || h.service == nil || h.service.Lens() == nil {
		return "", nil, http.StatusServiceUnavailable, "Lens service not available"
	}
	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		return "", nil, http.StatusNotFound, "Lens not found"
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		return "", nil, http.StatusForbidden, "Access denied"
	}
	return lensID, lens, 0, ""
}

// GetLensUnitGraph returns the persisted unit graph snapshot for a lens.
// GET /api/horizon/lenses/{lensId}/unit-graph?domainPath=/domain/v1/supply-chain/...
func (h *HorizonHandlers) GetLensUnitGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lensID, _, statusCode, statusText := h.ensureLensAccess(r)
	if statusCode != 0 {
		http.Error(w, statusText, statusCode)
		return
	}
	repo := h.unitGraphRepoOrService()
	if repo == nil {
		http.Error(w, "Unit graph repository not available", http.StatusServiceUnavailable)
		return
	}
	domainPath := strings.TrimSpace(r.URL.Query().Get("domainPath"))
	item, err := repo.GetByLensAndDomain(r.Context(), lensID, domainPath)
	if err != nil {
		if errors.Is(err, horizonrepo.ErrUnitGraphSnapshotNotFound) {
			http.Error(w, "Unit graph snapshot not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load unit graph snapshot", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// UpsertLensUnitGraph writes/updates a persisted unit graph snapshot for a lens.
// PUT/POST /api/horizon/lenses/{lensId}/unit-graph
func (h *HorizonHandlers) UpsertLensUnitGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lensID, lens, statusCode, statusText := h.ensureLensAccess(r)
	if statusCode != 0 {
		http.Error(w, statusText, statusCode)
		return
	}
	repo := h.unitGraphRepoOrService()
	if repo == nil {
		http.Error(w, "Unit graph repository not available", http.StatusServiceUnavailable)
		return
	}
	var req upsertUnitGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Edges == nil {
		req.Edges = []models.UnitGraphEdge{}
	}
	snapshot := &models.UnitGraphSnapshot{
		TenantID:            strings.TrimSpace(lens.TenantID),
		ProjectID:           strings.TrimSpace(lens.ProjectID),
		LensID:              lensID,
		DomainCanonicalPath: strings.TrimSpace(req.DomainCanonicalPath),
		Edges:               req.Edges,
		SourceJobID:         strings.TrimSpace(req.SourceJobID),
	}
	id, err := repo.Upsert(r.Context(), snapshot)
	if err != nil {
		http.Error(w, "Failed to save unit graph snapshot", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"lensId":   lensID,
		"edges":    len(req.Edges),
		"snapshot": snapshot,
	})
}

// ListLensUnitDiveResults returns persisted unit dive results for a lens.
// GET /api/horizon/lenses/{lensId}/unit-dive-results?limit=50
func (h *HorizonHandlers) ListLensUnitDiveResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lensID, _, statusCode, statusText := h.ensureLensAccess(r)
	if statusCode != 0 {
		http.Error(w, statusText, statusCode)
		return
	}
	repo := h.unitDiveRepoOrService()
	if repo == nil {
		http.Error(w, "Unit dive repository not available", http.StatusServiceUnavailable)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 200 {
		limit = 200
	}
	items, err := repo.ListByLens(r.Context(), lensID, limit)
	if err != nil {
		http.Error(w, "Failed to load unit dive results", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, unitDiveResultsListResponse{
		Results:    items,
		TotalCount: len(items),
	})
}

// GetLensUnitDiveResult returns a persisted unit dive result by result ID or request ID.
// GET /api/horizon/lenses/{lensId}/unit-dive-results/{resultId|requestId}
func (h *HorizonHandlers) GetLensUnitDiveResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lensID, _, statusCode, statusText := h.ensureLensAccess(r)
	if statusCode != 0 {
		http.Error(w, statusText, statusCode)
		return
	}
	repo := h.unitDiveRepoOrService()
	if repo == nil {
		http.Error(w, "Unit dive repository not available", http.StatusServiceUnavailable)
		return
	}
	escapedPath := strings.TrimSpace(r.URL.EscapedPath())
	const prefix = "/api/horizon/lenses/"
	if !strings.HasPrefix(escapedPath, prefix) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	rest := strings.TrimPrefix(escapedPath, prefix)
	_, tail, ok := strings.Cut(rest, "/unit-dive-results/")
	if !ok {
		http.Error(w, "resultId required in path", http.StatusBadRequest)
		return
	}
	tail = strings.TrimSpace(strings.Trim(tail, "/"))
	if tail == "" {
		http.Error(w, "resultId required in path", http.StatusBadRequest)
		return
	}
	token, err := url.PathUnescape(tail)
	if err != nil {
		http.Error(w, "resultId required in path", http.StatusBadRequest)
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		http.Error(w, "resultId required in path", http.StatusBadRequest)
		return
	}

	var item *models.UnitDiveResultArtifact
	if strings.HasPrefix(token, "udr_") {
		item, err = repo.Get(r.Context(), token)
		if err == nil && item != nil && strings.TrimSpace(item.LensID) != lensID {
			err = horizonrepo.ErrUnitDiveResultNotFound
		}
	} else {
		item, err = repo.GetByLensAndRequestID(r.Context(), lensID, token)
	}
	if err != nil {
		if errors.Is(err, horizonrepo.ErrUnitDiveResultNotFound) {
			http.Error(w, "Unit dive result not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load unit dive result", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// UpsertLensUnitDiveResult writes/updates a unit dive result artifact for a lens.
// POST /api/horizon/lenses/{lensId}/unit-dive-results
func (h *HorizonHandlers) UpsertLensUnitDiveResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lensID, lens, statusCode, statusText := h.ensureLensAccess(r)
	if statusCode != 0 {
		http.Error(w, statusText, statusCode)
		return
	}
	repo := h.unitDiveRepoOrService()
	if repo == nil {
		http.Error(w, "Unit dive repository not available", http.StatusServiceUnavailable)
		return
	}
	var req upsertUnitDiveResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(req.Result.RequestID)
	}
	if requestID == "" {
		http.Error(w, "requestId is required", http.StatusBadRequest)
		return
	}
	req.Result.RequestID = requestID
	artifact := &models.UnitDiveResultArtifact{
		TenantID:            strings.TrimSpace(lens.TenantID),
		ProjectID:           strings.TrimSpace(lens.ProjectID),
		LensID:              lensID,
		DomainCanonicalPath: strings.TrimSpace(req.DomainCanonicalPath),
		RequestID:           requestID,
		Result:              req.Result,
		SourceJobID:         strings.TrimSpace(req.SourceJobID),
	}
	id, err := repo.Upsert(r.Context(), artifact)
	if err != nil {
		http.Error(w, "Failed to save unit dive result", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        id,
		"lensId":    lensID,
		"requestId": requestID,
		"result":    artifact,
	})
}
