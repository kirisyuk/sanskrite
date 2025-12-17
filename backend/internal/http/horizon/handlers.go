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
	mux.Handle("/api/horizon/lenses", authMiddleware(http.HandlerFunc(h.CreateLens)))
	mux.Handle("/api/horizon/lenses/", authMiddleware(http.HandlerFunc(h.HandleLensRoutes)))

	// Fragment endpoints
	mux.Handle("/api/horizon/fragments", authMiddleware(http.HandlerFunc(h.ListFragments)))
	mux.Handle("/api/horizon/fragments/", authMiddleware(http.HandlerFunc(h.GetFragment)))

	// Entity search (Live Entity Recognition)
	mux.Handle("/api/horizon/entities/search", authMiddleware(http.HandlerFunc(h.SearchEntities)))

	// Co-Pilot chat
	mux.Handle("/api/horizon/co-pilot/chat", authMiddleware(http.HandlerFunc(h.CoPilotChat)))
}

// HandleLensRoutes обрабатывает все запросы к /api/horizon/lenses/{id} и подпутям
func (h *HorizonHandlers) HandleLensRoutes(w http.ResponseWriter, r *http.Request) {
	// Извлекаем путь после /api/horizon/lenses/
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	if path == "" {
		// Это должно быть обработано CreateLens, но на всякий случай
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

// GetAssemblyStatus, GetCockpitData, SimulateWhatIf реализованы в assembly.go и cockpit.go

