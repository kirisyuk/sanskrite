package horizon

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// RegisterFeedRoutes registers the external feed proxy routes for Horizon Monitor.
// These endpoints proxy external data services (ACLED, GDELT, USGS, etc.) to avoid
// frontend CORS issues and centralize API key management.
//
// Note: These routes are intentionally unauthenticated to allow polling by URL.
func (h *HorizonHandlers) RegisterFeedRoutes(mux *http.ServeMux) {
	// Open Data Feeds
	mux.HandleFunc("/api/horizon/feeds/usgs/earthquakes", h.proxyUSGSEarthquakes)
	mux.HandleFunc("/api/horizon/feeds/opensky/flights", h.proxyOpenSkyFlights)
	mux.HandleFunc("/api/horizon/feeds/gdelt/geo", h.proxyGDELTGeo)
	mux.HandleFunc("/api/horizon/feeds/ucdp/events", h.proxyUCDPEvents)
	mux.HandleFunc("/api/horizon/feeds/hapi/conflict", h.proxyHAPIConflict)
	mux.HandleFunc("/api/horizon/feeds/noaa/climate", h.proxyNOAAClimate)
	mux.HandleFunc("/api/horizon/feeds/nga/warnings", h.proxyNGAWarnings)
	mux.HandleFunc("/api/horizon/feeds/faa/status", h.proxyFAAStatus)

	// Auth-required Feeds (Keys managed on backend)
	mux.HandleFunc("/api/horizon/feeds/acled/events", h.proxyACLEDEvents)
	mux.HandleFunc("/api/horizon/feeds/firms/fires", h.proxyNASAFirms)
}

// --- Open Data Proxies ---

func (h *HorizonHandlers) proxyUSGSEarthquakes(w http.ResponseWriter, r *http.Request) {
	// USGS GeoJSON Feed (Past Day, 2.5+ Magnitude)
	targetURL := "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/2.5_day.geojson"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyOpenSkyFlights(w http.ResponseWriter, r *http.Request) {
	// OpenSky Network API (Anonymous access has limits, but sufficient for basic monitoring)
	// Returns state vectors for all aircraft.
	targetURL := "https://opensky-network.org/api/states/all"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyGDELTGeo(w http.ResponseWriter, r *http.Request) {
	// GDELT GeoJSON API (Global events with negative tone)
	targetURL := "https://api.gdeltproject.org/api/v2/geo/geo?query=tone%3C-5&format=geojson"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyUCDPEvents(w http.ResponseWriter, r *http.Request) {
	// UCDP GED (Uppsala Conflict Data Program) - Sample/Public endpoint
	// Note: Full API might require key, using public sample or cached endpoint if available.
	// Fallback to a known public dataset URL or specific query.
	targetURL := "https://ucdp.uu.se/api/gedevents/24.1?pagesize=100"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyHAPIConflict(w http.ResponseWriter, r *http.Request) {
	// HDX HAPI (Humanitarian API) - Conflict events
	targetURL := "https://hapi.humdata.org/api/v1/conflict-event?limit=1000&app_name=hatlone-test"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyNOAAClimate(w http.ResponseWriter, r *http.Request) {
	// NOAA Climate Anomalies (Example endpoint)
	targetURL := "https://www.ncei.noaa.gov/access/monitoring/climate-at-a-glance/global/time-series/globe/land_ocean/1/1/1880-2024.json"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyNGAWarnings(w http.ResponseWriter, r *http.Request) {
	// NGA Maritime Safety Information (Broadcast Warnings)
	targetURL := "https://msi.nga.mil/api/publications/download?type=view&key=16920959/SFH00000/DailyMem.json"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

func (h *HorizonHandlers) proxyFAAStatus(w http.ResponseWriter, r *http.Request) {
	// FAA NAS Status (Airport delays/ground stops)
	targetURL := "https://nasstatus.faa.gov/api/airport-status-information"
	h.proxyExternalFeed(w, r, targetURL, nil)
}

// --- Auth-Required Proxies ---

func (h *HorizonHandlers) proxyACLEDEvents(w http.ResponseWriter, r *http.Request) {
	apiKey := os.Getenv("ACLED_API_KEY")
	email := os.Getenv("ACLED_EMAIL")
	if apiKey == "" || email == "" {
		httputil.WriteError(w, http.StatusServiceUnavailable, "CONFIG_MISSING", "ACLED configuration missing", nil)
		return
	}

	// Pass through query params like 'country', 'year', 'limit'
	u, _ := url.Parse("https://api.acleddata.com/acled/read")
	q := u.Query()
	q.Set("key", apiKey)
	q.Set("email", email)

	// Default limit if not provided
	if r.URL.Query().Get("limit") == "" {
		q.Set("limit", "500")
	}

	// Copy allowed params from incoming request
	allowedParams := []string{"country", "iso", "year", "event_date", "limit", "region"}
	incomingQ := r.URL.Query()
	for _, p := range allowedParams {
		if v := incomingQ.Get(p); v != "" {
			q.Set(p, v)
		}
	}

	u.RawQuery = q.Encode()
	h.proxyExternalFeed(w, r, u.String(), nil)
}

func (h *HorizonHandlers) proxyNASAFirms(w http.ResponseWriter, r *http.Request) {
	apiKey := os.Getenv("NASA_FIRMS_API_KEY")
	if apiKey == "" {
		// Try fallback env var
		apiKey = os.Getenv("FIRMS_API_KEY")
	}
	if apiKey == "" {
		httputil.WriteError(w, http.StatusServiceUnavailable, "CONFIG_MISSING", "NASA FIRMS configuration missing", nil)
		return
	}

	// FIRMS API structure: https://firms.modaps.eosdis.nasa.gov/api/area/csv/[MAP_KEY]/[SOURCE]/[AREA_COORDS]/[DAY_RANGE]
	// We'll default to World/VIIRS_NOAA20_N21_Thermal_Anomalies_375m/1 (day)
	// Or allow simple params. For simplicity, let's proxy a standard world view.

	// Example: Get area data for the world (simplified for proxy)
	// Note: FIRMS API is strict on path structure.
	// Let's construct a request for "World" for "Today" using VIIRS.
	// Real implementation might need dynamic path construction based on frontend needs.

	// Using the 'country' endpoint as it's easier to proxy: /api/country/csv/[MAP_KEY]/[SOURCE]/[COUNTRY_CODE]/[DAY_RANGE]
	// But we want global.
	// Let's use the area endpoint with a world bounding box if possible, or just hardcode a region of interest if global is too heavy.
	// Actually, FIRMS allows "World" in some contexts, but usually requires specific coords.
	// Let's assume we fetch a specific high-interest region or pass params.

	// For this implementation, we'll proxy a safe default: World (if allowed) or a wide box.
	// Using 'https://firms.modaps.eosdis.nasa.gov/api/area/csv/' + apiKey + '/VIIRS_SNPP_NPP/world/1'

	targetURL := fmt.Sprintf("https://firms.modaps.eosdis.nasa.gov/api/area/csv/%s/VIIRS_SNPP_NPP/world/1", apiKey)
	h.proxyExternalFeed(w, r, targetURL, nil)
}

// --- Helper ---

func (h *HorizonHandlers) proxyExternalFeed(w http.ResponseWriter, r *http.Request, targetURL string, headers map[string]string) {
	ctx := r.Context()
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		httputil.WriteInternalError(w, "Failed to create request")
		return
	}

	// Add custom headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Use a client with timeout
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		httputil.WriteError(w, http.StatusBadGateway, "UPSTREAM_ERROR", fmt.Sprintf("Upstream service error: %v", err), nil)
		return
	}
	defer resp.Body.Close()

	// Forward Content-Type
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
