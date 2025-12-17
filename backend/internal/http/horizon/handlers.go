package horizon

import (
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon"
)

// HorizonHandlers содержит все HTTP handlers для Horizon API
type HorizonHandlers struct {
	service *horizon.HorizonService
}

// NewHorizonHandlers создает новые handlers
func NewHorizonHandlers(service *horizon.HorizonService) *HorizonHandlers {
	return &HorizonHandlers{
		service: service,
	}
}

// RegisterRoutes регистрирует все routes для Horizon API
func (h *HorizonHandlers) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Lens endpoints
	mux.Handle("/api/horizon/lenses", authMiddleware(http.HandlerFunc(h.HandleLensesRoot)))
	mux.Handle("/api/horizon/lenses/", authMiddleware(http.HandlerFunc(h.HandleLensRoutes)))

	// Fragment endpoints
	mux.Handle("/api/horizon/fragments/by-path", authMiddleware(http.HandlerFunc(h.GetFragmentByPath)))
	mux.Handle("/api/horizon/fragments/", authMiddleware(http.HandlerFunc(h.HandleFragmentRoutes)))

	// DependencyEdge endpoints
	mux.Handle("/api/horizon/edges", authMiddleware(http.HandlerFunc(h.CreateEdge)))
	mux.Handle("/api/horizon/edges/", authMiddleware(http.HandlerFunc(h.HandleEdgeRoutes)))

	// Scout endpoints
	mux.Handle("/api/horizon/scout/jobs/", authMiddleware(http.HandlerFunc(h.HandleScoutRoutes)))

	// Propagation endpoints
	mux.Handle("/api/horizon/propagation/status", authMiddleware(http.HandlerFunc(h.GetPropagationStatus)))
	mux.Handle("/api/horizon/propagation/trigger", authMiddleware(http.HandlerFunc(h.TriggerPropagation)))

	// Entity search (Live Entity Recognition)
	mux.Handle("/api/horizon/entities/search", authMiddleware(http.HandlerFunc(h.SearchEntities)))

	// Co-Pilot chat
	mux.Handle("/api/horizon/co-pilot/chat", authMiddleware(http.HandlerFunc(h.CoPilotChat)))
}

// HandleLensesRoot обрабатывает корневой путь /api/horizon/lenses
func (h *HorizonHandlers) HandleLensesRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateLens(w, r)
	case http.MethodGet:
		h.ListLenses(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleLensRoutes обрабатывает все запросы к /api/horizon/lenses/{id} и подпутям
func (h *HorizonHandlers) HandleLensRoutes(w http.ResponseWriter, r *http.Request) {
	// Извлекаем путь после /api/horizon/lenses/
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	if path == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Разбираем путь на части
	pathParts := strings.Split(path, "/")

	// Если есть вторая часть пути, это подпуть
	if len(pathParts) > 1 {
		subPath := pathParts[1]
		switch subPath {
		case "assembly-status":
			h.GetAssemblyStatus(w, r)
			return
		case "cockpit":
			h.GetCockpitData(w, r)
			return
		case "simulate":
			h.SimulateWhatIf(w, r)
			return
		case "recalculate":
			h.RecalculateLens(w, r)
			return
		default:
			http.Error(w, "Unknown subpath", http.StatusNotFound)
			return
		}
	}

	// Если нет подпути, это операции с самим Lens
	switch r.Method {
	case http.MethodGet:
		h.GetLens(w, r)
	case http.MethodPut:
		h.UpdateLens(w, r)
	case http.MethodDelete:
		h.DeleteLens(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleFragmentRoutes обрабатывает все запросы к /api/horizon/fragments/{id} и подпутям
func (h *HorizonHandlers) HandleFragmentRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	if path == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	// Проверяем подпути
	if strings.HasSuffix(r.URL.Path, "/dependencies") {
		h.GetFragmentDependencies(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/versions") {
		h.GetFragmentVersions(w, r)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/refresh") {
		h.RefreshFragment(w, r)
		return
	}

	// Если нет подпути, это операции с самим Fragment
	switch r.Method {
	case http.MethodGet:
		h.GetFragment(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleEdgeRoutes обрабатывает все запросы к /api/horizon/edges/{id}
func (h *HorizonHandlers) HandleEdgeRoutes(w http.ResponseWriter, r *http.Request) {
	edgeID := extractIDFromPath(r, "/api/horizon/edges/")
	if edgeID == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.UpdateEdge(w, r)
	case http.MethodDelete:
		h.DeleteEdge(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleScoutRoutes обрабатывает все запросы к /api/horizon/scout/jobs/{id}
func (h *HorizonHandlers) HandleScoutRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/scout/jobs/")
	if path == "" {
		// Список jobs
		h.ListScoutingJobs(w, r)
		return
	}

	// Получение конкретного job
	h.GetScoutingJob(w, r)
}

// GetAssemblyStatus, GetCockpitData, SimulateWhatIf реализованы в assembly.go и cockpit.go

