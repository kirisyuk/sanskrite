package criticalchain

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	ccsignal "github.com/igorkirisyuk/sanskrite/internal/criticalchainview/signal"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchainview"
)

// Handlers provides HTTP handlers for CriticalChain API.
type Handlers struct {
	svc *criticalchainview.Service
}

// NewHandlers creates new CriticalChain handlers.
func NewHandlers(svc *criticalchainview.Service) *Handlers {
	return &Handlers{svc: svc}
}

// RegisterRoutes registers CriticalChain routes.
// Demo endpoints (materials, facilities, routes, conflict-zones, graph, signals) are public.
// Admin pipeline run returns 410 Gone (ingestion moved to taxonomy migrate / scout path).
func (h *Handlers) RegisterRoutes(mux *http.ServeMux, _ func(http.Handler) http.Handler, jobAuth func(http.Handler) http.Handler) {
	// Public demo endpoints (read-only)
	mux.Handle("/api/v1/criticalchain/materials", http.HandlerFunc(h.handleMaterialsRoot))
	mux.Handle("/api/v1/criticalchain/materials/", http.HandlerFunc(h.handleMaterialsByID))
	mux.Handle("/api/v1/criticalchain/facilities", http.HandlerFunc(h.handleFacilitiesRoot))
	mux.Handle("/api/v1/criticalchain/facilities/", http.HandlerFunc(h.handleFacilitiesByID))
	mux.Handle("/api/v1/criticalchain/routes", http.HandlerFunc(h.handleRoutes))
	mux.Handle("/api/v1/criticalchain/conflict-zones", http.HandlerFunc(h.handleConflictZones))
	mux.Handle("/api/v1/criticalchain/graph/", http.HandlerFunc(h.handleGraph))
	mux.Handle("/api/v1/criticalchain/signals", http.HandlerFunc(h.handleSignals))
	mux.Handle("/api/v1/criticalchain/country-risk", http.HandlerFunc(h.handleCountryRisk))
	mux.Handle("/api/v1/criticalchain/chokepoints", http.HandlerFunc(h.handleChokepoints))
	// Admin: legacy pipeline endpoint (removed)
	mux.Handle("/api/v1/criticalchain/admin/pipeline/run", jobAuth(http.HandlerFunc(h.handlePipelineRun)))
}

func (h *Handlers) handleMaterialsRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := h.svc.ListMaterials(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*criticalchainview.Material{}
	}
	writeJSON(w, list)
}

func (h *Handlers) handleMaterialsByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/criticalchain/materials/")
	if id == "" {
		http.Error(w, "material id required", http.StatusBadRequest)
		return
	}
	m, err := h.svc.GetMaterial(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, m)
}

func (h *Handlers) handleFacilitiesRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	opts := &criticalchainview.ListFacilitiesOpts{}
	if q := r.URL.Query().Get("material"); q != "" {
		opts.MaterialID = q
	}
	if q := r.URL.Query().Get("country"); q != "" {
		opts.Country = q
	}
	if q := r.URL.Query().Get("region"); q != "" && opts.Country == "" {
		opts.Country = q
	}
	if q := r.URL.Query().Get("riskMin"); q != "" {
		if v, err := strconv.ParseFloat(q, 64); err == nil {
			opts.MinRiskScore = v
		}
	}
	list, err := h.svc.ListFacilities(r.Context(), opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*criticalchainview.Facility{}
	}
	writeJSON(w, list)
}

func (h *Handlers) handleFacilitiesByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/criticalchain/facilities/")
	if id == "" {
		http.Error(w, "facility id required", http.StatusBadRequest)
		return
	}
	f, err := h.svc.GetFacility(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, f)
}

func (h *Handlers) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := h.svc.ListRoutes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*criticalchainview.Route{}
	}
	writeJSON(w, list)
}

func (h *Handlers) handleConflictZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := h.svc.ListConflictZones(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*criticalchainview.ConflictZone{}
	}
	writeJSON(w, list)
}

func (h *Handlers) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/criticalchain/graph/")
	materialID := strings.Trim(path, "/")
	if materialID == "" {
		http.Error(w, "material id required", http.StatusBadRequest)
		return
	}
	graph, err := h.svc.GetGraph(r.Context(), materialID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if graph.Facilities == nil {
		graph.Facilities = []*criticalchainview.Facility{}
	}
	if graph.ProcessingSteps == nil {
		graph.ProcessingSteps = []*criticalchainview.ProcessingStep{}
	}
	writeJSON(w, graph)
}

func (h *Handlers) handleSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	signals, err := h.svc.GetSignals(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if signals == nil {
		signals = []ccsignal.FusedSignal{}
	}
	writeJSON(w, map[string]interface{}{"regions": signals})
}

func (h *Handlers) handleCountryRisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	risk, err := h.svc.GetCountryRisk(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if risk == nil {
		risk = map[string]float64{}
	}
	writeJSON(w, risk)
}

func (h *Handlers) handleChokepoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	materialID := r.URL.Query().Get("material")
	list, err := h.svc.ListChokepoints(r.Context(), materialID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []*criticalchainview.Chokepoint{}
	}
	writeJSON(w, list)
}

func (h *Handlers) handlePipelineRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "gone",
		"message": "CriticalChain pipeline ingestion was removed; use backend/cmd/taxonomy_migrate (see docs/archive/taxonomy-migration-2026/MIGRATION_SPEC_P0_P14.md §6).",
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
