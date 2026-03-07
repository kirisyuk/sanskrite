package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/gorilla/websocket"
)

const aisstreamURL = "wss://stream.aisstream.io/v0/stream"
const collectDuration = 6 * time.Second

type aisstreamVesselsHandler struct {
	logger    *log.Logger
	projectID string
}

func NewAISStreamVesselsHandler(logger *log.Logger, projectID string) *aisstreamVesselsHandler {
	return &aisstreamVesselsHandler{logger: logger, projectID: projectID}
}

func (h *aisstreamVesselsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	apiKey := gcp.GetEnvOrSecret(r.Context(), h.projectID, "AISSTREAM_API_KEY", "aisstream_api_key")
	if apiKey == "" {
		http.Error(w, "vessels not configured", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	minLat, _ := strconv.ParseFloat(q.Get("minLat"), 64)
	maxLat, _ := strconv.ParseFloat(q.Get("maxLat"), 64)
	minLon, _ := strconv.ParseFloat(q.Get("minLon"), 64)
	maxLon, _ := strconv.ParseFloat(q.Get("maxLon"), 64)
	if minLat == 0 && maxLat == 0 && minLon == 0 && maxLon == 0 {
		http.Error(w, "minLat, maxLat, minLon, maxLon required", http.StatusBadRequest)
		return
	}
	if minLat < -90 || maxLat > 90 || minLon < -180 || maxLon > 180 {
		http.Error(w, "invalid bbox", http.StatusBadRequest)
		return
	}

	// AISStream expects BoundingBoxes: [[[lat1, lon1], [lat2, lon2]]]
	bbox := [][][]float64{
		{{minLat, minLon}, {maxLat, maxLon}},
	}
	subMsg := map[string]interface{}{
		"APIKey":             apiKey,
		"BoundingBoxes":      bbox,
		"FilterMessageTypes": []string{"PositionReport"},
	}
	subBytes, _ := json.Marshal(subMsg)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	conn, _, err := dialer.DialContext(ctx, aisstreamURL, nil)
	if err != nil {
		h.logger.Printf("[AISStream] dial: %v", err)
		http.Error(w, "ais stream unavailable", http.StatusBadGateway)
		return
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, subBytes); err != nil {
		h.logger.Printf("[AISStream] write subscribe: %v", err)
		http.Error(w, "subscription failed", http.StatusBadGateway)
		return
	}

	var mu sync.Mutex
	byMMSI := make(map[string]vesselPoint)
	deadline := time.Now().Add(collectDuration)

	go func() {
		for time.Now().Before(deadline) {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, buf, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var msg struct {
				MessageType string `json:"MessageType"`
				Metadata    *struct {
					MMSI     interface{} `json:"MMSI"`
					ShipName string      `json:"ShipName"`
					Lat      float64     `json:"latitude"`
					Lon      float64     `json:"longitude"`
				} `json:"MetaData"`
				Message *struct {
					PositionReport *struct {
						Latitude float64 `json:"Latitude"`
						Longitude float64 `json:"Longitude"`
						UserID   uint32  `json:"UserID"`
						Sog      float64 `json:"Sog"`
						Cog      float64 `json:"Cog"`
					} `json:"PositionReport"`
				} `json:"Message"`
			}
			if err := json.Unmarshal(buf, &msg); err != nil {
				continue
			}
			if msg.MessageType != "PositionReport" {
				if msg.MessageType == "" {
					var errMsg struct{ Error string `json:"error"` }
					_ = json.Unmarshal(buf, &errMsg)
					if errMsg.Error != "" {
						h.logger.Printf("[AISStream] api error: %s", errMsg.Error)
					}
				}
				continue
			}
			var lat, lon float64
			var mmsi string
			var name string
			var sog, cog float64
			if msg.Metadata != nil {
				lat = msg.Metadata.Lat
				lon = msg.Metadata.Lon
				switch v := msg.Metadata.MMSI.(type) {
				case float64:
					mmsi = strconv.FormatUint(uint64(v), 10)
				case string:
					mmsi = v
				default:
					mmsi = ""
				}
				name = msg.Metadata.ShipName
			}
			if msg.Message != nil && msg.Message.PositionReport != nil {
				pr := msg.Message.PositionReport
				if lat == 0 && lon == 0 {
					lat, lon = pr.Latitude, pr.Longitude
				}
				if mmsi == "" {
					mmsi = strconv.FormatUint(uint64(pr.UserID), 10)
				}
				sog, cog = pr.Sog, pr.Cog
			}
			if mmsi == "" {
				continue
			}
			mu.Lock()
			byMMSI[mmsi] = vesselPoint{Lat: lat, Lon: lon, MMSI: mmsi, Name: name, Sog: sog, Cog: cog}
			mu.Unlock()
		}
	}()

	time.Sleep(collectDuration)
	mu.Lock()
	list := make([]vesselPoint, 0, len(byMMSI))
	for _, v := range byMMSI {
		list = append(list, v)
	}
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]interface{}{"data": list})
}

type vesselPoint struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lng"`
	MMSI string  `json:"mmsi"`
	Name string  `json:"name,omitempty"`
	Sog  float64 `json:"sog,omitempty"`
	Cog  float64 `json:"cog,omitempty"`
}
