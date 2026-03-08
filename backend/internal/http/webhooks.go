package http

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/igorkirisyuk/sanskrite/internal/discovery"
)

// WebhooksHandler routes provider webhooks (e.g., RSS WebSub, Telegram if needed)
type WebhooksHandler struct {
	logger *log.Logger
	poller *discovery.SourcePoller
}

func NewWebhooksHandler(logger *log.Logger) *WebhooksHandler { return &WebhooksHandler{logger: logger} }

// WithPoller wires poller for due publishing (optional)
func (h *WebhooksHandler) WithPoller(p *discovery.SourcePoller) *WebhooksHandler {
	h.poller = p
	return h
}

func (h *WebhooksHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// For now, accept and log; provider-specific handling can be wired later
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	h.logger.Printf("webhook: %s %v", r.URL.Path, m)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// CoordinateSourcesHandler triggers source coordination (for Cloud Scheduler -> HTTP best practice)
func (h *WebhooksHandler) CoordinateSourcesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET/POST is allowed", nil)
		return
	}
	if h.poller == nil {
		WriteInternalError(w, "Poller not configured")
		return
	}
	if err := h.poller.CoordinateDue(r.Context()); err != nil {
		h.logger.Printf("coordinate-sources error: %v", err)
		WriteInternalError(w, "Failed to publish due sources")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
