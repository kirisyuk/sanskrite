package horizon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

const horizonAIExplanationsSourceID = "horizon:ai_explanations"

func explainDocID(tenantID, targetType, targetID, lang string) string {
	// Deterministic, short Firestore doc ID. Avoid relying on composite indexes.
	s := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(targetType) + "|" + strings.TrimSpace(targetID) + "|" + strings.TrimSpace(lang)
	sum := sha256.Sum256([]byte(s))
	return "hzn_ai_" + hex.EncodeToString(sum[:])
}

// HandleAIExplanationsRoutes
// - GET /api/horizon/ai-explanations/edge/{edgeId}?lang=en
// - PUT /api/horizon/ai-explanations/edge/{edgeId}?lang=en
func (h *HorizonHandlers) HandleAIExplanationsRoutes(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		httputil.WriteInternalError(w, "Horizon not configured")
		return
	}
	if h.docsRepo == nil {
		httputil.WriteInternalError(w, "Documents repository is not configured")
		return
	}

	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		httputil.WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.TrimPrefix(strings.Trim(r.URL.Path, "/"), "api/horizon/ai-explanations")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}
	if len(parts) < 2 {
		httputil.WriteBadRequest(w, "INVALID_PATH", "Expected /ai-explanations/{targetType}/{targetId}")
		return
	}

	targetType := strings.TrimSpace(parts[0])
	targetID := strings.TrimSpace(parts[1])
	if targetType == "" || targetID == "" {
		httputil.WriteBadRequest(w, "INVALID_PATH", "targetType and targetId are required")
		return
	}
	// For now we only expose edge caching (requested), but keep the path flexible.
	if targetType != "edge" && targetType != "node" {
		httputil.WriteBadRequest(w, "INVALID_TARGET", "targetType must be 'edge' or 'node'")
		return
	}

	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = "en"
	}

	docID := explainDocID(authInfo.TenantID, targetType, targetID, lang)

	switch r.Method {
	case http.MethodGet:
		doc, err := h.docsRepo.GetByID(r.Context(), docID)
		if err != nil {
			// Translate Firestore not found into HTTP 404.
			if status.Code(err) == codes.NotFound {
				httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "AI explanation not found", nil)
				return
			}
			httputil.WriteInternalError(w, "Failed to load AI explanation")
			return
		}
		// Defensive: enforce tenant match
		if doc != nil && doc.TenantID != authInfo.TenantID {
			httputil.WriteError(w, http.StatusForbidden, "FORBIDDEN", "Access denied", nil)
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"targetType": targetType,
			"targetId":   targetID,
			"language":   strings.TrimSpace(doc.Language),
			"markdown":   doc.FullText,
			"updatedAt":  doc.UpdatedAt.UTC().Format(time.RFC3339),
			"docId":      doc.ID,
		})
		return

	case http.MethodPut:
		var body struct {
			Markdown  string         `json:"markdown"`
			Language  *string        `json:"language,omitempty"`
			Model     *string        `json:"model,omitempty"`
			LensID    *string        `json:"lensId,omitempty"`
			EdgeType  *string        `json:"edgeType,omitempty"`
			SourceID  *string        `json:"sourceFragmentId,omitempty"`
			TargetID  *string        `json:"targetFragmentId,omitempty"`
			ExtraAttr map[string]any `json:"attrs,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
			return
		}
		md := strings.TrimSpace(body.Markdown)
		if md == "" {
			httputil.WriteBadRequest(w, "VALIDATION_ERROR", "markdown is required")
			return
		}
		docLang := lang
		if body.Language != nil && strings.TrimSpace(*body.Language) != "" {
			docLang = strings.TrimSpace(*body.Language)
		}

		externalID := targetType + ":" + targetID + ":" + docLang
		title := "Horizon AI explanation (" + targetType + ")"

		attrs := map[string]any{
			"targetType": targetType,
			"targetId":   targetID,
			"lang":       docLang,
		}
		if body.Model != nil && strings.TrimSpace(*body.Model) != "" {
			attrs["model"] = strings.TrimSpace(*body.Model)
		}
		if body.LensID != nil && strings.TrimSpace(*body.LensID) != "" {
			attrs["lensId"] = strings.TrimSpace(*body.LensID)
		}
		if body.EdgeType != nil && strings.TrimSpace(*body.EdgeType) != "" {
			attrs["edgeType"] = strings.TrimSpace(*body.EdgeType)
		}
		if body.SourceID != nil && strings.TrimSpace(*body.SourceID) != "" {
			attrs["sourceFragmentId"] = strings.TrimSpace(*body.SourceID)
		}
		if body.TargetID != nil && strings.TrimSpace(*body.TargetID) != "" {
			attrs["targetFragmentId"] = strings.TrimSpace(*body.TargetID)
		}
		if body.ExtraAttr != nil {
			for k, v := range body.ExtraAttr {
				if strings.TrimSpace(k) == "" {
					continue
				}
				attrs[k] = v
			}
		}

		now := time.Now().UTC()
		doc := &models.Document{
			ID:           docID,
			TenantID:     authInfo.TenantID,
			SourceID:     horizonAIExplanationsSourceID,
			ExternalID:   externalID,
			Title:        title,
			DocumentPath: "/horizon/ai-explanations/" + targetType + "/" + targetID,
			FullText:     md,
			ContentType:  "text/markdown",
			DocumentType: "horizon_ai_explanation",
			Language:     docLang,
			Status:       "new",
			ContentHash:  repo.HashContent(md),
			IngestedAt:   now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Attrs:        attrs,
		}
		if err := h.docsRepo.Upsert(r.Context(), doc); err != nil {
			httputil.WriteInternalError(w, "Failed to save AI explanation")
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"targetType": targetType,
			"targetId":   targetID,
			"language":   docLang,
			"docId":      doc.ID,
		})
		return

	default:
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
}


