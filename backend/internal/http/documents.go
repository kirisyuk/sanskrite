package http

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DocumentsHandler handles document-related operations
type DocumentsHandler struct {
	docsRepo     *repo.DocumentsRepository
	entitiesRepo *repo.EntityRepository
	eventsRepo   *repo.EventRepository
	claimsRepo   *repo.ClaimRepository
	logger       *log.Logger
}

func NewDocumentsHandler(docs *repo.DocumentsRepository, entities *repo.EntityRepository, events *repo.EventRepository, claims *repo.ClaimRepository, logger *log.Logger) *DocumentsHandler {
	return &DocumentsHandler{
		docsRepo:     docs,
		entitiesRepo: entities,
		eventsRepo:   events,
		claimsRepo:   claims,
		logger:       logger,
	}
}

// GetSourceArtifacts handles GET /api/v1/sources/{sourceId}/artifacts?sourceType={type}
// Returns all ontology artifacts (entities, events, claims) created from the source
func (h *DocumentsHandler) GetSourceArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Source ID is required")
		return
	}
	sourceID := parts[3]

	// Parse query parameters
	sourceType := r.URL.Query().Get("sourceType") // опционально: document, telegram_channel, rss, web, chat, etc.
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Fetch artifacts
	ctx := r.Context()
	tenantID := auth.TenantID

	var entities []interface{}
	if h.entitiesRepo != nil {
		if ents, err := h.entitiesRepo.ListBySource(ctx, tenantID, sourceID, sourceType, limit); err == nil {
			for _, e := range ents {
				entities = append(entities, e)
			}
		} else {
			h.logger.Printf("GetSourceArtifacts: failed to list entities: %v", err)
		}
	}

	var events []interface{}
	if h.eventsRepo != nil {
		if evs, err := h.eventsRepo.ListBySource(ctx, tenantID, sourceID, sourceType, limit); err == nil {
			for _, e := range evs {
				events = append(events, e)
			}
		} else {
			h.logger.Printf("GetSourceArtifacts: failed to list events: %v", err)
		}
	}

	var claims []interface{}
	if h.claimsRepo != nil {
		if cls, err := h.claimsRepo.ListBySource(ctx, tenantID, sourceID, sourceType, limit); err == nil {
			for _, c := range cls {
				claims = append(claims, c)
			}
		} else {
			h.logger.Printf("GetSourceArtifacts: failed to list claims: %v", err)
		}
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sourceId":   sourceID,
		"sourceType": sourceType,
		"entities":   entities,
		"events":     events,
		"claims":     claims,
		"counts": map[string]int{
			"entities": len(entities),
			"events":   len(events),
			"claims":   len(claims),
		},
	})
}

