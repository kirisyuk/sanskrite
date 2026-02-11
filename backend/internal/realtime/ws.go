package realtime

import (
	"fmt"
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
	// per-connection write locks (gorilla/websocket requires single concurrent writer per conn)
	connWriteMu map[*websocket.Conn]*sync.Mutex
	mu          sync.RWMutex
	logger      *log.Logger
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
		connWriteMu:   make(map[*websocket.Conn]*sync.Mutex),
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
	h.connWriteMu[c] = &sync.Mutex{}
	h.mu.Unlock()

	// simple ping loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			if err := h.writeControlWithLock(c, websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
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
		readDeadline := time.Now().Add(60 * time.Second)
		c.SetReadDeadline(readDeadline)
		c.SetPongHandler(func(string) error {
			readDeadline = time.Now().Add(60 * time.Second)
			c.SetReadDeadline(readDeadline)
			return nil
		})
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			// CRITICAL: Update read deadline on every successful read to prevent premature timeout
			// This ensures connection stays alive as long as client is sending messages
			readDeadline = time.Now().Add(60 * time.Second)
			c.SetReadDeadline(readDeadline)
			// Process subscribe messages (text frames with small JSON)
			if mt == websocket.TextMessage && len(msg) > 0 {
				// minimal parsing without extra deps
				// expect {"type":"subscribe","projectId":"..."} or {"type":"subscribe","verifiedCode":"..."}
				if bytesIndex := indexOfSubscribe(msg); bytesIndex {
					if h.logger != nil {
						h.logger.Printf("ws: detected subscribe message conn=%s tenant=%s raw=%s", h.getConnID(c), tenantID, truncateForLog(msg, 256))
					}
					if pid := extractProjectID(msg); pid != "" {
						if h.logger != nil {
							h.logger.Printf("ws: extracted projectId=%s from subscribe message conn=%s tenant=%s", pid, h.getConnID(c), tenantID)
						}
						if err := h.addProjectSubscription(tenantID, pid, c); err != nil {
							if h.logger != nil {
								h.logger.Printf("ws: addProjectSubscription error conn=%s tenant=%s project=%s raw=%s err=%v", h.getConnID(c), tenantID, pid, truncateForLog(msg, 256), err)
							}
							continue
						}
						if h.logger != nil {
							h.logger.Printf("ws: addProjectSubscription returned successfully, preparing ACK conn=%s tenant=%s project=%s", h.getConnID(c), tenantID, pid)
						}
						if h.logger != nil {
							h.logger.Printf("ws: preparing subscribe ack conn=%s tenant=%s project=%s raw=%s", h.getConnID(c), tenantID, pid, truncateForLog(msg, 256))
						}
						// send ack back using writeJSONWithRetry to avoid timeout issues
						ack := map[string]interface{}{
							"type": "ws.subscribed",
							"data": map[string]interface{}{
								"tenantId":  tenantID,
								"projectId": pid,
								"connId":    h.getConnID(c),
								"ts":        time.Now().UnixMilli(),
							},
						}
						if h.logger != nil {
							h.logger.Printf("ws: sending subscribe ack conn=%s tenant=%s project=%s", h.getConnID(c), tenantID, pid)
						}
						if err := h.writeJSONWithRetry(c, ack); err != nil {
							if h.logger != nil {
								h.logger.Printf("ws: failed to send subscribe ack after retries conn=%s tenant=%s project=%s err=%v", h.getConnID(c), tenantID, pid, err)
							}
							h.removeConn(tenantID, c)
							_ = c.Close()
							return
						}
						if h.logger != nil {
							h.logger.Printf("ws: subscribe ack delivered conn=%s tenant=%s project=%s", h.getConnID(c), tenantID, pid)
						}
					} else if vcode := extractVerifiedCode(msg); vcode != "" {
						if err := h.addVerifiedSubscription(vcode, c); err != nil {
							if h.logger != nil {
								h.logger.Printf("ws: addVerifiedSubscription error conn=%s tenant=%s code=%s raw=%s err=%v", h.getConnID(c), tenantID, vcode, truncateForLog(msg, 256), err)
							}
							continue
						}
						if h.logger != nil {
							h.logger.Printf("ws: preparing verified subscribe ack conn=%s tenant=%s code=%s raw=%s", h.getConnID(c), tenantID, vcode, truncateForLog(msg, 256))
						}
						// send ack back using writeJSONWithRetry to avoid timeout issues
						ack := map[string]interface{}{
							"type": "ws.subscribed",
							"data": map[string]interface{}{
								"verifiedCode": vcode,
								"connId":       h.getConnID(c),
								"ts":           time.Now().UnixMilli(),
							},
						}
						if h.logger != nil {
							h.logger.Printf("ws: sending verified subscribe ack conn=%s tenant=%s code=%s", h.getConnID(c), tenantID, vcode)
						}
						if err := h.writeJSONWithRetry(c, ack); err != nil {
							if h.logger != nil {
								h.logger.Printf("ws: failed to send verified subscribe ack after retries conn=%s tenant=%s code=%s err=%v", h.getConnID(c), tenantID, vcode, err)
							}
							h.removeConn(tenantID, c)
							_ = c.Close()
							return
						}
						if h.logger != nil {
							h.logger.Printf("ws: subscribe ack delivered conn=%s verifiedCode=%s", h.getConnID(c), vcode)
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
	connID := h.connIDs[c]

	// Log which projects this connection was subscribed to
	var subscribedProjects []string
	if pm, ok := h.projConns[tenantID]; ok {
		for pid, set := range pm {
			if _, hasConn := set[c]; hasConn {
				subscribedProjects = append(subscribedProjects, pid)
			}
		}
	}

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
	delete(h.connWriteMu, c)

	// Log removal for debugging
	if h.logger != nil && len(subscribedProjects) > 0 {
		h.logger.Printf("ws: removed conn=%s tenant=%s projects=%v", connID, tenantID, subscribedProjects)
	}
}

// BroadcastJSON sends payload to all clients of tenant
func (h *Hub) BroadcastJSON(tenantID string, v interface{}) {
	h.mu.RLock()
	set := h.conns[tenantID]
	connections := make([]*websocket.Conn, 0, len(set))
	for c := range set {
		connections = append(connections, c)
	}
	h.mu.RUnlock()
	if len(connections) == 0 {
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

	for _, c := range connections {
		if err := h.writeJSONWithRetry(c, v); err != nil {
			h.logger.Printf("ws: write error after retries tenant=%s conn=%s: %v", tenantID, h.getConnID(c), err)
			h.removeConn(tenantID, c)
			_ = c.Close()
		}
	}
}

// writeJSONWithRetry writes JSON to WebSocket connection with retry mechanism
func (h *Hub) writeJSONWithRetry(c *websocket.Conn, v interface{}) error {
	return h.withConnWriteLock(c, func() error {
		writeDeadline := time.Now().Add(5 * time.Second)
		if err := c.SetWriteDeadline(writeDeadline); err != nil {
			return fmt.Errorf("failed to set write deadline: %w", err)
		}
		defer func() {
			// Clear the write deadline so subsequent writes aren't impacted
			_ = c.SetWriteDeadline(time.Time{})
		}()

		// Try write with retry (up to 2 retries for transient errors)
		var writeErr error
		for attempt := 0; attempt < 3; attempt++ {
			writeErr = c.WriteJSON(v)
			if writeErr == nil {
				return nil
			}
			// Check if error is transient (timeout, temporary network issue)
			if attempt < 2 && (websocket.IsUnexpectedCloseError(writeErr, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
				strings.Contains(writeErr.Error(), "timeout") || strings.Contains(writeErr.Error(), "broken pipe")) {
				// Brief delay before retry
				time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
				// Reset deadline for retry
				if err := c.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
					return fmt.Errorf("failed to reset write deadline: %w", err)
				}
				continue
			}
			break
		}
		return writeErr
	})
}

// BroadcastJSONProject sends payload only to clients of tenant subscribed to projectId
func (h *Hub) BroadcastJSONProject(tenantID, projectID string, v interface{}) {
	// CRITICAL FIX: Get connections list atomically and verify each connection still exists before sending
	// This prevents race condition where connection is removed between getting list and sending
	h.mu.RLock()
	pm := h.projConns[tenantID]
	var connections []*websocket.Conn
	projConnsCount := 0
	if pm != nil {
		if set := pm[projectID]; set != nil {
			projConnsCount = len(set)
			// Copy connections to slice while holding lock
			for c := range set {
				// Verify connection still exists in main conns map (double-check)
				if tenantConns, ok := h.conns[tenantID]; ok {
					if _, exists := tenantConns[c]; exists {
						connections = append(connections, c)
					} else {
						// Connection removed from main map but still in projConns - cleanup needed
						if h.logger != nil {
							h.logger.Printf("ws: BroadcastJSONProject found orphaned connection in projConns tenant=%s project=%s conn=%s - will cleanup", tenantID, projectID, h.getConnIDUnsafe(c))
						}
						// Schedule cleanup (can't do it here because we hold RLock)
						go func(orphanedConn *websocket.Conn) {
							h.mu.Lock()
							defer h.mu.Unlock()
							// Double-check it's still orphaned
							if tenantConns, ok := h.conns[tenantID]; ok {
								if _, exists := tenantConns[orphanedConn]; exists {
									// Connection restored, skip cleanup
									return
								}
							}
							// Cleanup orphaned connection from projConns
							if pm, ok := h.projConns[tenantID]; ok {
								if set, ok2 := pm[projectID]; ok2 {
									delete(set, orphanedConn)
									if len(set) == 0 {
										delete(pm, projectID)
									}
									if len(pm) == 0 {
										delete(h.projConns, tenantID)
									}
									if h.logger != nil {
										h.logger.Printf("ws: cleaned up orphaned connection from projConns tenant=%s project=%s conn=%s", tenantID, projectID, h.getConnIDUnsafe(orphanedConn))
									}
								}
							}
						}(c)
					}
				}
			}
		}
	}
	// Get total tenant connections count for diagnostics
	totalTenantConns := 0
	if tenantConns, ok := h.conns[tenantID]; ok {
		totalTenantConns = len(tenantConns)
	}
	connCount := len(connections)
	h.mu.RUnlock()

	if connCount == 0 {
		// Enhanced logging for genai.stream events to diagnose missing subscriptions
		if env, ok := v.(map[string]interface{}); ok {
			if t, hasType := env["type"].(string); hasType && strings.HasPrefix(t, "genai.stream") {
				if h.logger != nil {
					h.logger.Printf("ws: BroadcastJSONProject skipped (no connections) tenant=%s project=%s type=%s projConnsCount=%d totalTenantConns=%d", tenantID, projectID, t, projConnsCount, totalTenantConns)
				}
			}
		}
		return
	}
	// Log for genai.stream and interaction events
	if env, ok := v.(map[string]interface{}); ok {
		if t, hasType := env["type"].(string); hasType {
			if strings.HasPrefix(t, "genai.stream") || t == "interaction.created" || t == "interaction.updated" {
				if h.logger != nil {
					h.logger.Printf("ws: BroadcastJSONProject sending to %d connection(s) tenant=%s project=%s type=%s", connCount, tenantID, projectID, t)
				}
			}
		}
	}
	// Send to verified connections
	for _, c := range connections {
		// Double-check connection still exists before sending (defensive)
		h.mu.RLock()
		stillExists := false
		if tenantConns, ok := h.conns[tenantID]; ok {
			_, stillExists = tenantConns[c]
		}
		connID := h.getConnIDUnsafe(c) // Get connID while lock is held
		h.mu.RUnlock()

		if !stillExists {
			if h.logger != nil {
				h.logger.Printf("ws: BroadcastJSONProject skipping removed connection tenant=%s project=%s conn=%s", tenantID, projectID, connID)
			}
			continue
		}

		// Log right before writing to connection
		if h.logger != nil {
			if env, ok := v.(map[string]interface{}); ok {
				if t, hasType := env["type"].(string); hasType && (strings.HasPrefix(t, "genai.stream") || t == "interaction.created" || t == "interaction.updated") {
					h.logger.Printf("ws: BroadcastJSONProject writing to conn=%s tenant=%s project=%s type=%s", connID, tenantID, projectID, t)
				}
			}
		}

		if err := h.writeJSONWithRetry(c, v); err != nil {
			h.logger.Printf("ws: write error after retries tenant=%s conn=%s: %v", tenantID, h.getConnID(c), err)
			h.removeConn(tenantID, c)
			_ = c.Close()
		}
	}
}

func (h *Hub) addProjectSubscription(tenantID, projectID string, c *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Log the intent to add a subscription
	if h.logger != nil {
		h.logger.Printf("ws: addProjectSubscription attempting for conn=%s tenant=%s project=%s", h.getConnIDUnsafe(c), tenantID, projectID)
	}

	// CRITICAL: Verify connection still exists in main conns map before adding to projConns
	// This prevents orphaned subscriptions when connection is removed but subscription message arrives late
	if tenantConns, ok := h.conns[tenantID]; !ok {
		if h.logger != nil {
			h.logger.Printf("ws: addProjectSubscription skipped - tenant not found tenant=%s project=%s conn=%s", tenantID, projectID, h.getConnIDUnsafe(c))
		}
		return fmt.Errorf("tenant missing for subscription")
	} else if _, exists := tenantConns[c]; !exists {
		if h.logger != nil {
			h.logger.Printf("ws: addProjectSubscription skipped - connection not found in tenant conns tenant=%s project=%s conn=%s", tenantID, projectID, h.getConnIDUnsafe(c))
		}
		return fmt.Errorf("connection missing in tenant map")
	}

	if _, ok := h.projConns[tenantID]; !ok {
		h.projConns[tenantID] = make(map[string]map[*websocket.Conn]struct{})
	}
	if _, ok := h.projConns[tenantID][projectID]; !ok {
		h.projConns[tenantID][projectID] = make(map[*websocket.Conn]struct{})
	}
	h.projConns[tenantID][projectID][c] = struct{}{}

	// Log the successful addition to projConns with current counts for diagnostics
	// CRITICAL: Don't use RLock here - we already hold Lock() from line 412
	projConnsCount := 0
	if pm, ok := h.projConns[tenantID]; ok {
		if set, ok2 := pm[projectID]; ok2 {
			projConnsCount = len(set)
		}
	}
	tenantConnsCount := 0
	if tc, ok := h.conns[tenantID]; ok {
		tenantConnsCount = len(tc)
	}
	if h.logger != nil {
		h.logger.Printf("ws: addProjectSubscription success tenant=%s project=%s conn=%s projConns=%d tenantConns=%d", tenantID, projectID, h.getConnIDUnsafe(c), projConnsCount, tenantConnsCount)
	}
	return nil
}

func (h *Hub) addVerifiedSubscription(verifiedCode string, c *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.verifiedConns[verifiedCode]; !ok {
		h.verifiedConns[verifiedCode] = make(map[*websocket.Conn]struct{})
	}
	h.verifiedConns[verifiedCode][c] = struct{}{}
	return nil
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
// CRITICAL: Only counts connections that exist in both projConns AND conns (no orphaned connections)
func (h *Hub) GetProjectConnCount(tenantID, projectID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Get tenant connections map
	tenantConns, hasTenantConns := h.conns[tenantID]
	if !hasTenantConns {
		return 0
	}

	// Count only connections that exist in both maps
	if pm, ok := h.projConns[tenantID]; ok {
		if set, ok2 := pm[projectID]; ok2 {
			count := 0
			for c := range set {
				if _, exists := tenantConns[c]; exists {
					count++
				}
			}
			return count
		}
	}
	return 0
}

func (h *Hub) getConnID(c *websocket.Conn) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.getConnIDUnsafe(c)
}

// getConnIDUnsafe gets connection ID without locking (caller must hold lock)
func (h *Hub) getConnIDUnsafe(c *websocket.Conn) string {
	if h.connIDs == nil {
		return "unknown-map-nil"
	}
	if id, ok := h.connIDs[c]; ok {
		return id
	}
	return "unknown"
}

func truncateForLog(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...(truncated)"
}

// BroadcastVerified sends payload to clients subscribed to a specific verified code
func (h *Hub) BroadcastVerified(verifiedCode string, v interface{}) {
	h.mu.RLock()
	set := h.verifiedConns[verifiedCode]
	connections := make([]*websocket.Conn, 0, len(set))
	for c := range set {
		connections = append(connections, c)
	}
	h.mu.RUnlock()
	if len(connections) == 0 {
		return
	}
	for _, c := range connections {
		if err := h.writeJSONWithRetry(c, v); err != nil {
			h.logger.Printf("ws write error (verified broadcast): %v", err)
			// Find tenant for this connection to remove it properly
			h.mu.Lock()
			var foundTID string
			for tid, connSet := range h.conns {
				if _, ok := connSet[c]; ok {
					foundTID = tid
					break
				}
			}
			h.mu.Unlock()
			if foundTID != "" {
				h.removeConn(foundTID, c)
				_ = c.Close()
			}
		}
	}
}

func (h *Hub) withConnWriteLock(c *websocket.Conn, fn func() error) error {
	if fn == nil {
		return nil
	}
	h.mu.RLock()
	wmu := h.connWriteMu[c]
	h.mu.RUnlock()
	if wmu == nil {
		return fmt.Errorf("connection write lock not found")
	}
	wmu.Lock()
	defer wmu.Unlock()
	return fn()
}

func (h *Hub) writeControlWithLock(c *websocket.Conn, messageType int, data []byte, deadline time.Time) error {
	return h.withConnWriteLock(c, func() error {
		return c.WriteControl(messageType, data, deadline)
	})
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
