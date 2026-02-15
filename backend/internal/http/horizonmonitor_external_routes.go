package http

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RegisterHorizonMonitorExternalRoutes exposes public /api/* endpoints used by HorizonMonitor sources.
// These endpoints live in our backend so we never depend on the worldmonitor/horizon-monitor hosted API.
//
// Important: these are intentionally unauthenticated (poller fetches them by URL).
func RegisterHorizonMonitorExternalRoutes(mux *http.ServeMux, logger *log.Logger) {
	if mux == nil {
		return
	}
	api := newHorizonMonitorExternalAPI(logger)
	api.register(mux)
}

type horizonMonitorExternalAPI struct {
	logger *log.Logger
	http   *http.Client

	mu    sync.Mutex
	cache map[string]hmMemCacheEntry
}

type hmMemCacheEntry struct {
	Body      []byte
	ExpiresAt time.Time
}

func newHorizonMonitorExternalAPI(logger *log.Logger) *horizonMonitorExternalAPI {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &horizonMonitorExternalAPI{
		logger: logger,
		http:   &http.Client{Timeout: 25 * time.Second},
		cache:  make(map[string]hmMemCacheEntry),
	}
}

func (a *horizonMonitorExternalAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/acled", a.handleACLEDProtests)
	mux.HandleFunc("/api/acled-conflict", a.handleACLEDConflict)
	mux.HandleFunc("/api/gdelt-geo", a.handleGDELTGeo)
	mux.HandleFunc("/api/gdelt-doc", a.handleGDELTDoc)
	mux.HandleFunc("/api/opensky", a.handleOpenSky)
	mux.HandleFunc("/api/earthquakes", a.handleUSGSEarthquakes)
	mux.HandleFunc("/api/ucdp-events", a.handleNotImplemented("ucdp-events"))
	mux.HandleFunc("/api/ucdp", a.handleNotImplemented("ucdp"))
	mux.HandleFunc("/api/hapi", a.handleNotImplemented("hapi"))
	mux.HandleFunc("/api/climate-anomalies", a.handleNotImplemented("climate-anomalies"))
	mux.HandleFunc("/api/service-status", a.handleNotImplemented("service-status"))
	mux.HandleFunc("/api/nga-warnings", a.handleNotImplemented("nga-warnings"))
	mux.HandleFunc("/api/faa-status", a.handleNotImplemented("faa-status"))

	// Query-only proxies/analytics (safe to expose; sources can remain paused until needed).
	mux.HandleFunc("/api/yahoo-finance", a.handleNotImplemented("yahoo-finance"))
	mux.HandleFunc("/api/coingecko", a.handleNotImplemented("coingecko"))
	mux.HandleFunc("/api/polymarket", a.handleNotImplemented("polymarket"))
	mux.HandleFunc("/api/fred-data", a.handleNotImplemented("fred-data"))
	mux.HandleFunc("/api/worldbank", a.handleNotImplemented("worldbank"))
	mux.HandleFunc("/api/unhcr-population", a.handleNotImplemented("unhcr-population"))
	mux.HandleFunc("/api/worldpop-exposure", a.handleNotImplemented("worldpop-exposure"))
	mux.HandleFunc("/api/github-trending", a.handleNotImplemented("github-trending"))
	mux.HandleFunc("/api/arxiv", a.handleNotImplemented("arxiv"))
	mux.HandleFunc("/api/hackernews", a.handleNotImplemented("hackernews"))
	mux.HandleFunc("/api/macro-signals", a.handleNotImplemented("macro-signals"))
	mux.HandleFunc("/api/etf-flows", a.handleNotImplemented("etf-flows"))
	mux.HandleFunc("/api/stablecoin-markets", a.handleNotImplemented("stablecoin-markets"))
	mux.HandleFunc("/api/stock-index", a.handleNotImplemented("stock-index"))
	mux.HandleFunc("/api/risk-scores", a.handleNotImplemented("risk-scores"))
	mux.HandleFunc("/api/temporal-baseline", a.handleNotImplemented("temporal-baseline"))
	mux.HandleFunc("/api/eia/petroleum", a.handleNotImplemented("eia-petroleum"))
	mux.HandleFunc("/api/tech-events", a.handleNotImplemented("tech-events"))

	// Not implemented yet (auth-heavy / websocket relay). Keep endpoint stable.
	mux.HandleFunc("/api/ais-snapshot", a.handleNotImplemented("ais-snapshot"))
	mux.HandleFunc("/api/cloudflare-outages", a.handleNotImplemented("cloudflare-outages"))
	mux.HandleFunc("/api/firms-fires", a.handleNotImplemented("firms-fires"))
	mux.HandleFunc("/api/finnhub", a.handleNotImplemented("finnhub"))
}

func (a *horizonMonitorExternalAPI) handleNotImplemented(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.writeJSON(w, http.StatusNotImplemented, map[string]any{
			"success":    false,
			"error":      "not implemented",
			"endpoint":   name,
			"items":      []any{},
			"cached_at":  time.Now().UTC().Format(time.RFC3339),
			"configured": false,
		}, 30*time.Second)
	}
}

func (a *horizonMonitorExternalAPI) allowCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func (a *horizonMonitorExternalAPI) requireGETOrOPTIONS(w http.ResponseWriter, r *http.Request) bool {
	a.allowCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func (a *horizonMonitorExternalAPI) writeJSON(w http.ResponseWriter, status int, payload any, cacheFor time.Duration) {
	a.allowCORS(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if cacheFor > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(cacheFor.Seconds())))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *horizonMonitorExternalAPI) cacheGet(key string) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache == nil {
		return nil, false
	}
	ent, ok := a.cache[key]
	if !ok {
		return nil, false
	}
	if !ent.ExpiresAt.IsZero() && time.Now().After(ent.ExpiresAt) {
		delete(a.cache, key)
		return nil, false
	}
	if len(ent.Body) == 0 {
		return nil, false
	}
	return append([]byte(nil), ent.Body...), true
}

func (a *horizonMonitorExternalAPI) cacheSet(key string, body []byte, ttl time.Duration) {
	if ttl <= 0 || len(body) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache == nil {
		a.cache = make(map[string]hmMemCacheEntry)
	}
	a.cache[key] = hmMemCacheEntry{Body: append([]byte(nil), body...), ExpiresAt: time.Now().Add(ttl)}
}

func (a *horizonMonitorExternalAPI) fetch(ctx context.Context, rawURL string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HorizonMonitor/1.0)")
	for k, v := range headers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

func hmISODate(daysAgo int) string {
	t := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
	return t.Format("2006-01-02")
}

// -----------------------------
// ACLED
// -----------------------------

func (a *horizonMonitorExternalAPI) handleACLEDProtests(w http.ResponseWriter, r *http.Request) {
	if !a.requireGETOrOPTIONS(w, r) {
		return
	}

	token := strings.TrimSpace(os.Getenv("ACLED_ACCESS_TOKEN"))
	if token == "" {
		a.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success":    false,
			"configured": false,
			"error":      "ACLED_ACCESS_TOKEN not configured",
			"items":      []any{},
			"data":       []any{},
			"cached_at":  time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}

	cacheKey := "acled:protests:v1"
	if b, ok := a.cacheGet(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	params := url.Values{}
	params.Set("event_type", "Protests")
	params.Set("event_date", hmISODate(30)+"|"+hmISODate(0))
	params.Set("event_date_where", "BETWEEN")
	params.Set("limit", "500")
	params.Set("_format", "json")
	acledURL := "https://acleddata.com/api/acled/read?" + params.Encode()

	body, _, err := a.fetch(r.Context(), acledURL, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if err != nil {
		a.writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":   false,
			"error":     "ACLED fetch failed",
			"details":   err.Error(),
			"items":     []any{},
			"data":      []any{},
			"cached_at": time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}

	items := hmACLEDExtractItems(body)
	out := map[string]any{
		"success":   true,
		"count":     len(items),
		"items":     items,
		"data":      items, // backward compatible with existing payload key list
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(out)
	a.cacheSet(cacheKey, enc, 10*time.Minute)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func (a *horizonMonitorExternalAPI) handleACLEDConflict(w http.ResponseWriter, r *http.Request) {
	if !a.requireGETOrOPTIONS(w, r) {
		return
	}

	token := strings.TrimSpace(os.Getenv("ACLED_ACCESS_TOKEN"))
	if token == "" {
		a.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success":    false,
			"configured": false,
			"error":      "ACLED_ACCESS_TOKEN not configured",
			"items":      []any{},
			"data":       []any{},
			"cached_at":  time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}

	cacheKey := "acled:conflict:v1"
	if b, ok := a.cacheGet(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	params := url.Values{}
	params.Set("event_type", "Battles|Explosions/Remote violence|Violence against civilians")
	params.Set("event_date", hmISODate(30)+"|"+hmISODate(0))
	params.Set("event_date_where", "BETWEEN")
	params.Set("limit", "500")
	params.Set("_format", "json")
	acledURL := "https://acleddata.com/api/acled/read?" + params.Encode()

	body, _, err := a.fetch(r.Context(), acledURL, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if err != nil {
		a.writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":   false,
			"error":     "ACLED fetch failed",
			"details":   err.Error(),
			"items":     []any{},
			"data":      []any{},
			"cached_at": time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}

	items := hmACLEDExtractItems(body)
	out := map[string]any{
		"success":   true,
		"count":     len(items),
		"items":     items,
		"data":      items,
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(out)
	a.cacheSet(cacheKey, enc, 10*time.Minute)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func hmACLEDExtractItems(body []byte) []any {
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []any{}
	}
	items := make([]any, 0, len(payload.Data))
	for _, e := range payload.Data {
		// Keep original ACLED fields, but clamp large fields.
		if notes, ok := e["notes"].(string); ok && len(notes) > 500 {
			e["notes"] = notes[:500]
		}
		if src, ok := e["source"].(string); ok && len(src) > 200 {
			e["source"] = src[:200]
		}
		if tags, ok := e["tags"].(string); ok && len(tags) > 200 {
			e["tags"] = tags[:200]
		}
		items = append(items, e)
	}
	return items
}

// -----------------------------
// GDELT
// -----------------------------

func (a *horizonMonitorExternalAPI) handleGDELTGeo(w http.ResponseWriter, r *http.Request) {
	if !a.requireGETOrOPTIONS(w, r) {
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if query == "" {
		query = "protest"
	}
	format := strings.TrimSpace(q.Get("format"))
	if format == "" {
		format = "geojson"
	}
	maxrecords := q.Get("maxrecords")
	if maxrecords == "" {
		maxrecords = "250"
	}
	timespan := q.Get("timespan")
	if timespan == "" {
		timespan = "7d"
	}
	gdeltURL := fmt.Sprintf("https://api.gdeltproject.org/api/v2/geo/geo?query=%s&format=%s&maxrecords=%s&timespan=%s",
		url.QueryEscape(query),
		url.QueryEscape(format),
		url.QueryEscape(maxrecords),
		url.QueryEscape(timespan),
	)

	cacheKey := "gdelt-geo:" + shortHash(gdeltURL)
	if b, ok := a.cacheGet(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	body, _, err := a.fetch(r.Context(), gdeltURL, nil)
	if err != nil {
		a.writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":   false,
			"error":     "GDELT fetch failed",
			"details":   err.Error(),
			"items":     []any{},
			"cached_at": time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}

	items := hmGDELTGeoToItems(body)
	out := map[string]any{
		"success":   true,
		"query":     query,
		"count":     len(items),
		"items":     items,
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(out)
	a.cacheSet(cacheKey, enc, 5*time.Minute)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func hmGDELTGeoToItems(body []byte) []any {
	// GDELT geo endpoint can return GeoJSON.
	var geo struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string    `json:"type"`
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &geo); err != nil {
		return []any{}
	}
	out := make([]any, 0, len(geo.Features))
	for _, f := range geo.Features {
		m := map[string]any{}
		for k, v := range f.Properties {
			m[k] = v
		}
		if len(f.Geometry.Coordinates) >= 2 {
			m["longitude"] = f.Geometry.Coordinates[0]
			m["latitude"] = f.Geometry.Coordinates[1]
		}
		// Make sure our downstream can pick up title/summary.
		if _, ok := m["title"]; !ok {
			if name, ok := m["name"].(string); ok && strings.TrimSpace(name) != "" {
				m["title"] = name
			}
		}
		if _, ok := m["publishedAt"]; !ok {
			if v, ok := m["date"].(string); ok && strings.TrimSpace(v) != "" {
				m["publishedAt"] = v
			}
		}
		out = append(out, m)
	}
	return out
}

func (a *horizonMonitorExternalAPI) handleGDELTDoc(w http.ResponseWriter, r *http.Request) {
	if !a.requireGETOrOPTIONS(w, r) {
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if len(query) < 2 {
		a.writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"error":   "query parameter required",
			"items":   []any{},
		}, 30*time.Second)
		return
	}
	maxrecords := q.Get("maxrecords")
	if maxrecords == "" {
		maxrecords = "25"
	}
	if n, err := strconv.Atoi(maxrecords); err == nil {
		if n > 25 {
			maxrecords = "25"
		} else if n < 1 {
			maxrecords = "1"
		}
	}
	timespan := strings.TrimSpace(q.Get("timespan"))
	if timespan == "" {
		timespan = "24h"
	}

	gdeltURL := "https://api.gdeltproject.org/api/v2/doc/doc"
	u, _ := url.Parse(gdeltURL)
	uq := u.Query()
	uq.Set("query", query)
	uq.Set("mode", "artlist")
	uq.Set("maxrecords", maxrecords)
	uq.Set("format", "json")
	uq.Set("sort", "date")
	uq.Set("timespan", timespan)
	u.RawQuery = uq.Encode()

	cacheKey := "gdelt-doc:" + shortHash(u.String())
	if b, ok := a.cacheGet(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	body, _, err := a.fetch(r.Context(), u.String(), nil)
	if err != nil {
		a.writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":   false,
			"error":     "GDELT fetch failed",
			"details":   err.Error(),
			"items":     []any{},
			"cached_at": time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}

	items := hmGDELTDocToItems(body)
	out := map[string]any{
		"success":   true,
		"query":     query,
		"count":     len(items),
		"items":     items,
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(out)
	a.cacheSet(cacheKey, enc, 5*time.Minute)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func hmGDELTDocToItems(body []byte) []any {
	var payload struct {
		Articles []map[string]any `json:"articles"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []any{}
	}
	out := make([]any, 0, len(payload.Articles))
	for _, a := range payload.Articles {
		m := map[string]any{}
		title, _ := a["title"].(string)
		if strings.TrimSpace(title) != "" {
			m["title"] = title
		}
		if u, _ := a["url"].(string); strings.TrimSpace(u) != "" {
			m["url"] = u
		}
		if d, _ := a["seendate"].(string); strings.TrimSpace(d) != "" {
			m["publishedAt"] = d
		}
		if img, _ := a["socialimage"].(string); strings.TrimSpace(img) != "" {
			m["image"] = img
		}
		if lang, _ := a["language"].(string); strings.TrimSpace(lang) != "" {
			m["language"] = lang
		}
		if tone, ok := a["tone"]; ok {
			m["tone"] = tone
		}
		if domain, _ := a["domain"].(string); strings.TrimSpace(domain) != "" {
			m["source"] = domain
		}
		out = append(out, m)
	}
	return out
}

// -----------------------------
// OpenSky
// -----------------------------

func (a *horizonMonitorExternalAPI) handleOpenSky(w http.ResponseWriter, r *http.Request) {
	if !a.requireGETOrOPTIONS(w, r) {
		return
	}
	q := r.URL.Query()
	params := url.Values{}
	for _, key := range []string{"lamin", "lomin", "lamax", "lomax"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			params.Set(key, v)
		}
	}
	openskyURL := "https://opensky-network.org/api/states/all"
	if enc := params.Encode(); enc != "" {
		openskyURL += "?" + enc
	}

	cacheKey := "opensky:" + shortHash(openskyURL)
	if b, ok := a.cacheGet(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=25")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	body, _, err := a.fetch(r.Context(), openskyURL, map[string]string{
		"Accept-Language": "en-US,en;q=0.9",
	})
	if err != nil {
		a.writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":   false,
			"error":     "OpenSky fetch failed",
			"details":   err.Error(),
			"items":     []any{},
			"cached_at": time.Now().UTC().Format(time.RFC3339),
		}, 10*time.Second)
		return
	}

	items := hmOpenSkyToItems(body)
	out := map[string]any{
		"success":   true,
		"count":     len(items),
		"items":     items,
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(out)
	a.cacheSet(cacheKey, enc, 30*time.Second)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=25")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func hmOpenSkyToItems(body []byte) []any {
	// OpenSky: {time: number, states: [][]any}
	var payload struct {
		Time   float64   `json:"time"`
		States [][]any   `json:"states"`
		Error  any       `json:"error"`
		Msg    string    `json:"message"`
		Now    float64   `json:"now"`
		Info   any       `json:"info"`
		Meta   any       `json:"meta"`
		Extra  struct{}  `json:"-"`
		Other  map[string]any `json:"-"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []any{}
	}
	out := make([]any, 0, len(payload.States))
	for _, s := range payload.States {
		// https://opensky-network.org/apidoc/rest.html#response
		// [0] icao24, [1] callsign, [2] origin_country, [3] time_position, [4] last_contact
		// [5] longitude, [6] latitude, [7] baro_altitude, [8] on_ground, [9] velocity
		// [10] true_track, [11] vertical_rate, [12] sensors, [13] geo_altitude, [14] squawk
		// [15] spi, [16] position_source
		m := map[string]any{}
		if len(s) > 0 {
			m["icao24"] = asStringAny(s[0])
		}
		if len(s) > 1 {
			m["callsign"] = strings.TrimSpace(asStringAny(s[1]))
			if m["callsign"] != "" {
				m["title"] = fmt.Sprintf("Flight %s", m["callsign"])
			}
		}
		if len(s) > 2 {
			m["origin_country"] = asStringAny(s[2])
		}
		if len(s) > 3 {
			m["time_position"] = s[3]
		}
		if len(s) > 4 {
			m["last_contact"] = s[4]
			if t := unixSecondsToRFC3339(s[4]); t != "" {
				m["publishedAt"] = t
			}
		}
		if len(s) > 5 {
			m["longitude"] = s[5]
		}
		if len(s) > 6 {
			m["latitude"] = s[6]
		}
		if len(s) > 7 {
			m["baro_altitude"] = s[7]
		}
		if len(s) > 8 {
			m["on_ground"] = s[8]
		}
		if len(s) > 9 {
			m["velocity"] = s[9]
		}
		if len(s) > 10 {
			m["heading"] = s[10] // our pipeline reads heading/track/true_track/course
			m["true_track"] = s[10]
		}
		if len(s) > 11 {
			m["vertical_rate"] = s[11]
		}
		if len(s) > 13 {
			m["geo_altitude"] = s[13]
			m["altitude"] = s[13]
		}
		if len(s) > 14 {
			m["squawk"] = asStringAny(s[14])
		}
		if len(s) > 16 {
			m["position_source"] = s[16]
		}
		// Pipeline coordinates extractor looks for latitude/longitude too.
		if v, ok := m["latitude"]; ok {
			m["lat"] = v
		}
		if v, ok := m["longitude"]; ok {
			m["lon"] = v
		}
		out = append(out, m)
	}
	return out
}

func asStringAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if math.Mod(t, 1) == 0 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}

func unixSecondsToRFC3339(v any) string {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return ""
		}
		return time.Unix(int64(t), 0).UTC().Format(time.RFC3339)
	case json.Number:
		if n, err := t.Int64(); err == nil && n > 0 {
			return time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// -----------------------------
// USGS Earthquakes
// -----------------------------

func (a *horizonMonitorExternalAPI) handleUSGSEarthquakes(w http.ResponseWriter, r *http.Request) {
	if !a.requireGETOrOPTIONS(w, r) {
		return
	}
	cacheKey := "usgs:eq:v1"
	if b, ok := a.cacheGet(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}
	body, _, err := a.fetch(r.Context(), "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_day.geojson", nil)
	if err != nil {
		a.writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":   false,
			"error":     "USGS fetch failed",
			"details":   err.Error(),
			"items":     []any{},
			"cached_at": time.Now().UTC().Format(time.RFC3339),
		}, 30*time.Second)
		return
	}
	items := hmUSGSEarthquakesToItems(body)
	out := map[string]any{
		"success":   true,
		"count":     len(items),
		"items":     items,
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(out)
	a.cacheSet(cacheKey, enc, 5*time.Minute)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func hmUSGSEarthquakesToItems(body []byte) []any {
	var payload struct {
		Features []struct {
			ID         string `json:"id"`
			Properties struct {
				Mag   float64 `json:"mag"`
				Place string  `json:"place"`
				Time  float64 `json:"time"`
				Title string  `json:"title"`
				URL   string  `json:"url"`
			} `json:"properties"`
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []any{}
	}
	out := make([]any, 0, len(payload.Features))
	for _, f := range payload.Features {
		m := map[string]any{
			"id":    f.ID,
			"title": f.Properties.Title,
			"url":   f.Properties.URL,
			"mag":   f.Properties.Mag,
			"place": f.Properties.Place,
		}
		if f.Properties.Time > 0 {
			t := time.UnixMilli(int64(f.Properties.Time)).UTC()
			m["publishedAt"] = t.Format(time.RFC3339)
			m["timestamp"] = t.Unix()
		}
		if len(f.Geometry.Coordinates) >= 2 {
			m["longitude"] = f.Geometry.Coordinates[0]
			m["latitude"] = f.Geometry.Coordinates[1]
			m["lon"] = f.Geometry.Coordinates[0]
			m["lat"] = f.Geometry.Coordinates[1]
		}
		if len(f.Geometry.Coordinates) >= 3 {
			m["depth_km"] = f.Geometry.Coordinates[2]
		}
		// Provide deterministic severity hint.
		switch {
		case f.Properties.Mag >= 7:
			m["severity"] = "high"
		case f.Properties.Mag >= 6:
			m["severity"] = "medium"
		default:
			m["severity"] = "low"
		}
		out = append(out, m)
	}
	return out
}

// -----------------------------
// UCDP / HAPI / Climate / ServiceStatus / NGA / FAA
// (Ported in follow-up patch to keep this file readable.)
// -----------------------------

// shortHash is local helper to avoid importing discovery package here.
func shortHash(s string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:8])
}
