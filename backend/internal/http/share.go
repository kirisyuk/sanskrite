package http

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type ShareHandler struct {
	shares *repo.ShareTokensRepository
	projs  *repo.ProjectsRepository
	inter  *repo.InteractionsRepository
	logger *log.Logger
}

func NewShareHandler(shares *repo.ShareTokensRepository, projs *repo.ProjectsRepository, inter *repo.InteractionsRepository, logger *log.Logger) *ShareHandler {
	return &ShareHandler{shares: shares, projs: projs, inter: inter, logger: logger}
}

type createShareRequest struct {
	ExpiresAt string `json:"expiresAt"` // ISO
}

type createShareResponse struct {
	ShareToken string `json:"shareToken"`
	ExpiresAt  string `json:"expiresAt"`
	Url        string `json:"url"`
}

func randomToken(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// POST /api/v1/projects/{id}/share
func (h *ShareHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Missing project id")
		return
	}
	projectID := parts[3]

	var req createShareRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	exp := time.Now().UTC().Add(24 * time.Hour)
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			exp = t
		}
	}
	tok := randomToken(48)
	st := &repo.ShareToken{ProjectID: projectID, Token: tok, ExpiresAt: exp, CreatedBy: auth.UID}
	if _, err := h.shares.Create(r.Context(), st); err != nil {
		WriteInternalError(w, "Failed to create share token")
		return
	}
	base := r.Header.Get("X-External-Base-Url")
	if base == "" {
		base = strings.TrimSuffix("https://"+r.Host, "/")
	}
	resp := createShareResponse{ShareToken: tok, ExpiresAt: exp.Format(time.RFC3339), Url: base + "/shared/" + tok}
	WriteJSON(w, http.StatusCreated, resp)
}

// DELETE /api/v1/projects/{id}/share/{token}
func (h *ShareHandler) RevokeShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Missing token")
		return
	}
	token := parts[5]
	st, id, err := h.shares.GetByToken(r.Context(), token)
	if err != nil || st == nil {
		WriteNotFound(w, "Token not found")
		return
	}
	if err := h.shares.Revoke(r.Context(), id); err != nil {
		WriteInternalError(w, "Failed to revoke token")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GET /api/v1/public/share/{token}
func (h *ShareHandler) GetPublicShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Missing token")
		return
	}
	token := parts[4]
	st, id, err := h.shares.GetByToken(r.Context(), token)
	if err != nil || st == nil {
		WriteNotFound(w, "NOT_FOUND")
		return
	}
	if !st.IsActive || time.Now().UTC().After(st.ExpiresAt) {
		WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "Token expired or revoked", nil)
		return
	}

	// Touch access
	_ = h.shares.TouchAccess(r.Context(), id)

	// Load project (minimal)
	pr, err := h.projs.Get(r.Context(), st.ProjectID)
	if err != nil || pr == nil {
		WriteNotFound(w, "PROJECT_NOT_FOUND")
		return
	}
	// Load interactions
	items, _ := h.inter.ListByProject(r.Context(), st.ProjectID)

	// Build public response
	type PubAuthor struct {
		Kind        string  `json:"kind"`
		DisplayName *string `json:"displayName,omitempty"`
	}
	type PubGraph struct {
		ChartTitle string   `json:"chartTitle"`
		ChartType  *string  `json:"chartType,omitempty"`
		Table      [][]any  `json:"table"`
		Sources    []string `json:"sources"`
	}
	type PubInteraction struct {
		ID               string                    `json:"id"`
		Kind             string                    `json:"kind"`
		Subkind          *string                   `json:"subkind,omitempty"`
		ReplyTo          *string                   `json:"replyTo,omitempty"`
		Content          *string                   `json:"content,omitempty"`
		ContentSanitized *string                   `json:"contentSanitized,omitempty"`
		GraphData        *PubGraph                 `json:"graphData,omitempty"`
		SearchQueries    []string                  `json:"searchQueries,omitempty"`
		Sources          []string                  `json:"sources,omitempty"`
		CreatedAt        time.Time                 `json:"createdAt"`
		Author           *PubAuthor                `json:"author,omitempty"`
		Metadata         *repo.InteractionMetadata `json:"metadata,omitempty"`
	}
	pub := struct {
		V     string `json:"v"`
		Share struct {
			TokenId   string    `json:"tokenId"`
			IsActive  bool      `json:"isActive"`
			ExpiresAt time.Time `json:"expiresAt"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"share"`
		Project struct {
			ID          string     `json:"id"`
			DisplayName string     `json:"displayName"`
			Description *string    `json:"description,omitempty"`
			CreatedAt   *time.Time `json:"createdAt,omitempty"`
		} `json:"project"`
		Interactions []PubInteraction `json:"interactions"`
	}{V: "1"}
	pub.Share.TokenId = id
	pub.Share.IsActive = st.IsActive
	pub.Share.ExpiresAt = st.ExpiresAt
	pub.Share.CreatedAt = st.CreatedAt
	pub.Project.ID = pr.ID
	pub.Project.DisplayName = pr.DisplayName
	pub.Project.Description = pr.Description
	pub.Project.CreatedAt = &pr.CreatedAt

	// Map interactions (sanitize)
	for _, it := range items {
		// Only 'user' | 'assistant'
		if it.Kind != "user" && it.Kind != "assistant" && it.Kind != "discovery.query" && it.Kind != "discovery.answer" {
			continue
		}
		kind := it.Kind
		if kind == "discovery.query" {
			kind = "user"
		}
		if kind == "discovery.answer" {
			kind = "assistant"
		}
		pi := PubInteraction{
			ID:               it.ID,
			Kind:             kind,
			Subkind:          it.Subkind,
			ReplyTo:          it.ReplyTo,
			Content:          &it.Content,
			ContentSanitized: it.ContentSanitized,
			SearchQueries:    it.SearchQueries,
			Sources:          it.Sources,
			CreatedAt:        it.CreatedAt,
			Metadata:         it.Metadata,
		}
		if it.Author != nil {
			pi.Author = &PubAuthor{Kind: it.Author.Kind, DisplayName: it.Author.DisplayName}
		}
		// Graph data: best-effort heuristic from sanitized JSON embedded previously (FE already persists graphData client-side)
		// We keep this empty on server side for v1; FE will infer from contentSanitized when needed.
		pub.Interactions = append(pub.Interactions, pi)
	}

	WriteJSON(w, http.StatusOK, pub)
}
