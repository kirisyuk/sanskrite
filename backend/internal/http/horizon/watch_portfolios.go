package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type watchPortfolioEntryRequest struct {
	ID                    string         `json:"id,omitempty"`
	EntryType             string         `json:"entryType,omitempty"`
	Label                 string         `json:"label"`
	CanonicalPath         string         `json:"canonicalPath,omitempty"`
	SubjectID             string         `json:"subjectId,omitempty"`
	SourceTenantID        string         `json:"sourceTenantId,omitempty"`
	SourceLensID          string         `json:"sourceLensId,omitempty"`
	RelatedCanonicalPaths []string       `json:"relatedCanonicalPaths,omitempty"`
	AutoEnsureFocus       bool           `json:"autoEnsureFocus"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type watchPortfolioRequest struct {
	ID          string                       `json:"id,omitempty"`
	Name        string                       `json:"name"`
	Kind        string                       `json:"kind,omitempty"`
	Description string                       `json:"description,omitempty"`
	Status      string                       `json:"status,omitempty"`
	Entries     []watchPortfolioEntryRequest `json:"entries,omitempty"`
	Metadata    map[string]any               `json:"metadata,omitempty"`
}

func (h *HorizonHandlers) HandleWatchPortfoliosRoot(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.watchRuntime == nil {
		http.Error(w, "watch runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		portfolios, lerr := h.watchRuntime.ListPortfolios(r.Context(), authInfo.TenantID, 100)
		if lerr != nil {
			http.Error(w, fmt.Sprintf("failed to list portfolios: %v", lerr), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"portfolios": portfolios})
	case http.MethodPost:
		var req watchPortfolioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		portfolio := &horizonmodels.WatchPortfolio{
			ID:          strings.TrimSpace(req.ID),
			TenantID:    strings.TrimSpace(authInfo.TenantID),
			UserID:      strings.TrimSpace(authInfo.UID),
			Name:        strings.TrimSpace(req.Name),
			Kind:        strings.TrimSpace(req.Kind),
			Description: strings.TrimSpace(req.Description),
			Status:      strings.TrimSpace(req.Status),
			Metadata:    cloneAnyMap(req.Metadata),
			Entries:     make([]horizonmodels.WatchPortfolioEntry, 0, len(req.Entries)),
		}
		for _, item := range req.Entries {
			portfolio.Entries = append(portfolio.Entries, horizonmodels.WatchPortfolioEntry{
				ID:                    strings.TrimSpace(item.ID),
				EntryType:             strings.TrimSpace(item.EntryType),
				Label:                 strings.TrimSpace(item.Label),
				CanonicalPath:         strings.TrimSpace(item.CanonicalPath),
				SubjectID:             strings.TrimSpace(item.SubjectID),
				SourceTenantID:        strings.TrimSpace(item.SourceTenantID),
				SourceLensID:          strings.TrimSpace(item.SourceLensID),
				RelatedCanonicalPaths: append([]string(nil), item.RelatedCanonicalPaths...),
				AutoEnsureFocus:       item.AutoEnsureFocus,
				Metadata:              cloneAnyMap(item.Metadata),
			})
		}
		if err := h.watchRuntime.UpsertPortfolio(r.Context(), portfolio); err != nil {
			http.Error(w, fmt.Sprintf("failed to upsert portfolio: %v", err), http.StatusInternalServerError)
			return
		}
		view, verr := h.watchRuntime.PortfolioView(r.Context(), portfolio.ID)
		if verr != nil {
			http.Error(w, fmt.Sprintf("failed to build portfolio view: %v", verr), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, view)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) HandleWatchPortfolioDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchRuntime == nil {
		http.Error(w, "watch runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	portfolioID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/horizon/watch/portfolios/"))
	if portfolioID == "" {
		http.Error(w, "portfolio id is required", http.StatusBadRequest)
		return
	}
	if strings.HasSuffix(portfolioID, "/digest") {
		portfolioID = strings.TrimSpace(strings.TrimSuffix(portfolioID, "/digest"))
		view, verr := h.watchRuntime.PortfolioView(r.Context(), portfolioID)
		if verr != nil {
			http.Error(w, fmt.Sprintf("failed to build portfolio view: %v", verr), http.StatusInternalServerError)
			return
		}
		if view == nil || view.Portfolio == nil || strings.TrimSpace(view.Portfolio.TenantID) != strings.TrimSpace(authInfo.TenantID) {
			http.Error(w, "portfolio not found", http.StatusNotFound)
			return
		}
		digest, err := h.watchRuntime.BuildPortfolioMonitoringDigest(r.Context(), portfolioID, strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("persist")), "true") || strings.TrimSpace(r.URL.Query().Get("persist")) == "1")
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to build portfolio digest: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, digest)
		return
	}
	view, err := h.watchRuntime.PortfolioView(r.Context(), portfolioID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build portfolio view: %v", err), http.StatusInternalServerError)
		return
	}
	if view == nil || view.Portfolio == nil || strings.TrimSpace(view.Portfolio.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "portfolio not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
