package realtime

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Hub manages websocket clients per tenant
type Hub struct {
	upgrader websocket.Upgrader
	// tenantId -> set of connections
	conns map[string]map[*websocket.Conn]struct{}
	// tenantId -> projectId -> set of connections (only for clients that sent subscribe)
	projConns map[string]map[string]map[*websocket.Conn]struct{}
	// verifiedCode -> set of connections (for verified.stream events)
	verifiedConns map[string]map[*websocket.Conn]struct{}
	// connection ids for diagnostics
	connIDs map[*websocket.Conn]string
	mu      sync.RWMutex
	logger  *log.Logger
}

func NewHub(logger *log.Logger) *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		conns:         make(map[string]map[*websocket.Conn]struct{}),
		projConns:     make(map[string]map[string]map[*websocket.Conn]struct{}),
		verifiedConns: make(map[string]map[*websocket.Conn]struct{}),
		connIDs:       make(map[*websocket.Conn]string),
		logger:        logger,
	}
}

// ServeWS upgrades the connection and registers it to the tenant bucket
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, tenantID string) {
	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Printf("ws upgrade: %v", err)
		return
	}

	h.mu.Lock()
	if _, ok := h.conns[tenantID]; !ok {
		h.conns[tenantID] = make(map[*websocket.Conn]struct{})
	}
	h.conns[tenantID][c] = struct{}{}
	// assign connection id for diagnostics
	h.connIDs[c] = uuid.NewString()
	h.mu.Unlock()

	// simple ping loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			if err := c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
				h.removeConn(tenantID, c)
				_ = c.Close()
				return
			}
		}
	}()

	// read pump: handle simple control messages and detect close
	go func() {
		defer func() { h.removeConn(tenantID, c); _ = c.Close() }()
		c.SetReadLimit(1024)
		c.SetReadDeadline(time.Now().Add(60 * time.Second))
		c.SetPongHandler(func(string) error { c.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			// Process subscribe messages (text frames with small JSON)
			if mt == websocket.TextMessage && len(msg) > 0 {
				// minimal parsing without extra deps
				// expect {"type":"subscribe","projectId":"..."} or {"type":"subscribe","verifiedCode":"..."}
				if bytesIndex := indexOfSubscribe(msg); bytesIndex {
					if pid := extractProjectID(msg); pid != "" {
						h.addProjectSubscription(tenantID, pid, c)
						// send ack back
						ack := map[string]interface{}{
							"type": "ws.subscribed",
							"data": map[string]interface{}{
								"tenantId":  tenantID,
								"projectId": pid,
								"connId":    h.getConnID(c),
								"ts":        time.Now().UnixMilli(),
							},
						}
						_ = c.WriteJSON(ack)
						if h.logger != nil {
							h.logger.Printf("ws: subscribed conn=%s tenant=%s project=%s", h.getConnID(c), tenantID, pid)
						}
					} else if vcode := extractVerifiedCode(msg); vcode != "" {
						h.addVerifiedSubscription(vcode, c)
						// send ack back
						ack := map[string]interface{}{
							"type": "ws.subscribed",
							"data": map[string]interface{}{
								"verifiedCode": vcode,
								"connId":       h.getConnID(c),
								"ts":           time.Now().UnixMilli(),
							},
						}
						_ = c.WriteJSON(ack)
						if h.logger != nil {
							h.logger.Printf("ws: subscribed conn=%s verifiedCode=%s", h.getConnID(c), vcode)
						}
					}
				}
			}
		}
	}()
}

func (h *Hub) removeConn(tenantID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.conns[tenantID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, tenantID)
		}
	}
	if pm, ok := h.projConns[tenantID]; ok {
		for pid, set := range pm {
			delete(set, c)
			if len(set) == 0 {
				delete(pm, pid)
			}
		}
		if len(pm) == 0 {
			delete(h.projConns, tenantID)
		}
	}
	// Remove from verified subscriptions
	for vcode, set := range h.verifiedConns {
		delete(set, c)
		if len(set) == 0 {
			delete(h.verifiedConns, vcode)
		}
	}
	delete(h.connIDs, c)
}

// BroadcastJSON sends payload to all clients of tenant
func (h *Hub) BroadcastJSON(tenantID string, v interface{}) {
	h.mu.RLock()
	set := h.conns[tenantID]
	h.mu.RUnlock()
	if len(set) == 0 {
		// DEBUG: log when no clients are connected for analysis.status
		if env, ok := v.(map[string]interface{}); ok {
			if t, hasType := env["type"].(string); hasType && t == "analysis.status" {
				h.logger.Printf("ws: no clients connected for tenantId=%s (analysis.status)", tenantID)
			}
		}
		return
	}

	// DEBUG: log analysis.status broadcasts
	if env, ok := v.(map[string]interface{}); ok {
		if t, hasType := env["type"].(string); hasType && t == "analysis.status" {
			h.logger.Printf("ws: broadcasting analysis.status to %d client(s) for tenantId=%s", len(set), tenantID)
		}
	}

	for c := range set {
		if err := c.WriteJSON(v); err != nil {
			h.logger.Printf("ws write error: %v", err)
			h.removeConn(tenantID, c)
			_ = c.Close()
		}
	}
}

// BroadcastJSONProject sends payload only to clients of tenant subscribed to projectId
func (h *Hub) BroadcastJSONProject(tenantID, projectID string, v interface{}) {
	h.mu.RLock()
	pm := h.projConns[tenantID]
	var set map[*websocket.Conn]struct{}
	if pm != nil {
		set = pm[projectID]
	}
	h.mu.RUnlock()
	if len(set) == 0 {
		return
	}
	for c := range set {
		if err := c.WriteJSON(v); err != nil {
			h.logger.Printf("ws write error: %v", err)
			h.removeConn(tenantID, c)
			_ = c.Close()
		}
	}
}

func (h *Hub) addProjectSubscription(tenantID, projectID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.projConns[tenantID]; !ok {
		h.projConns[tenantID] = make(map[string]map[*websocket.Conn]struct{})
	}
	if _, ok := h.projConns[tenantID][projectID]; !ok {
		h.projConns[tenantID][projectID] = make(map[*websocket.Conn]struct{})
	}
	h.projConns[tenantID][projectID][c] = struct{}{}
}

func (h *Hub) addVerifiedSubscription(verifiedCode string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.verifiedConns[verifiedCode]; !ok {
		h.verifiedConns[verifiedCode] = make(map[*websocket.Conn]struct{})
	}
	h.verifiedConns[verifiedCode][c] = struct{}{}
}

// naive helpers to avoid json unmarshalling for tiny control frames
func indexOfSubscribe(b []byte) bool {
	// check for \"type\":\"subscribe\"
	if len(b) == 0 {
		return false
	}
	s := string(b)
	return strings.Contains(s, "\"type\":\"subscribe\"")
}

func extractProjectID(b []byte) string {
	s := string(b)
	// very small and permissive extraction
	const key = "\"projectId\""
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	tail := s[idx+len(key):]
	colon := strings.Index(tail, ":")
	if colon < 0 {
		return ""
	}
	tail = tail[colon+1:]
	// trim spaces and quotes
	tail = strings.TrimSpace(tail)
	if len(tail) == 0 {
		return ""
	}
	if tail[0] == '"' {
		tail = tail[1:]
		end := strings.IndexByte(tail, '"')
		if end >= 0 {
			return tail[:end]
		}
	}
	// fallback until comma/brace
	end := strings.IndexAny(tail, ",}")
	if end >= 0 {
		return strings.TrimSpace(tail[:end])
	}
	return strings.TrimSpace(tail)
}

func extractVerifiedCode(b []byte) string {
	s := string(b)
	const key = "\"verifiedCode\""
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	tail := s[idx+len(key):]
	colon := strings.Index(tail, ":")
	if colon < 0 {
		return ""
	}
	tail = tail[colon+1:]
	tail = strings.TrimSpace(tail)
	if len(tail) == 0 {
		return ""
	}
	if tail[0] == '"' {
		tail = tail[1:]
		end := strings.IndexByte(tail, '"')
		if end >= 0 {
			return tail[:end]
		}
	}
	end := strings.IndexAny(tail, ",}")
	if end >= 0 {
		return strings.TrimSpace(tail[:end])
	}
	return strings.TrimSpace(tail)
}

// GetProjectConnCount returns number of connections subscribed for a given tenant/project
func (h *Hub) GetProjectConnCount(tenantID, projectID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if pm, ok := h.projConns[tenantID]; ok {
		if set, ok2 := pm[projectID]; ok2 {
			return len(set)
		}
	}
	return 0
}

func (h *Hub) getConnID(c *websocket.Conn) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if id, ok := h.connIDs[c]; ok {
		return id
	}
	return ""
}

// BroadcastVerified sends payload to clients subscribed to a specific verified code
func (h *Hub) BroadcastVerified(verifiedCode string, v interface{}) {
	h.mu.RLock()
	set := h.verifiedConns[verifiedCode]
	h.mu.RUnlock()
	if len(set) == 0 {
		return
	}
	for c := range set {
		if err := c.WriteJSON(v); err != nil {
			h.logger.Printf("ws write error (verified broadcast): %v", err)
			// Find tenant for this connection to remove it properly
			h.mu.Lock()
			for tid, connSet := range h.conns {
				if _, ok := connSet[c]; ok {
					h.mu.Unlock()
					h.removeConn(tid, c)
					_ = c.Close()
					break
				}
			}
			h.mu.Unlock()
		}
	}
}

// GetConnectedClients returns the count of connected WS clients per tenantID (for debugging)
func (h *Hub) GetConnectedClients() map[string]int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	res := make(map[string]int)
	for tid, set := range h.conns {
		res[tid] = len(set)
	}
	return res
}
