package http

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// WebhooksHandler routes provider webhooks (e.g., RSS WebSub, Telegram if needed)
type WebhooksHandler struct{ logger *log.Logger }

func NewWebhooksHandler(logger *log.Logger) *WebhooksHandler { return &WebhooksHandler{logger: logger} }

func (h *WebhooksHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// For now, accept and log; provider-specific handling can be wired later
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	h.logger.Printf("webhook: %s %v", r.URL.Path, m)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
