package realtime

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Hub manages websocket clients per tenant
type Hub struct {
	upgrader websocket.Upgrader
	// tenantId -> set of connections
	conns  map[string]map[*websocket.Conn]struct{}
	mu     sync.RWMutex
	logger *log.Logger
}

func NewHub(logger *log.Logger) *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		conns:  make(map[string]map[*websocket.Conn]struct{}),
		logger: logger,
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

	// read pump to detect close
	go func() {
		defer func() { h.removeConn(tenantID, c); _ = c.Close() }()
		c.SetReadLimit(1024)
		c.SetReadDeadline(time.Now().Add(60 * time.Second))
		c.SetPongHandler(func(string) error { c.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
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
}

// BroadcastJSON sends payload to all clients of tenant
func (h *Hub) BroadcastJSON(tenantID string, v interface{}) {
	h.mu.RLock()
	set := h.conns[tenantID]
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



