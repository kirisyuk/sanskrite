package horizon

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetGeoContextImage serves a satellite basemap PNG for explicit lat/lon (Google Static Maps).
// GET /api/horizon/geo/context-image?lat=&lon=&zoom=&width=&height=&scale=
// Query defaults: zoom=14, width=640, height=640, scale=2
func (h *HorizonHandlers) GetGeoContextImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lat, lon, err := parseLatLonQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !models.IsRealGeoCoordinate(lat, lon) {
		http.Error(w, "invalid or placeholder coordinates", http.StatusBadRequest)
		return
	}

	zoom, width, height, scale := parseStaticMapOptions(r)
	h.writeStaticSatelliteImage(w, r, lat, lon, zoom, width, height, scale)
}

// GetFragmentContextImage serves a satellite basemap centered on the fragment's resolved coordinates.
// GET /api/horizon/fragments/{id}/context-image?zoom=&width=&height=&scale=
func (h *HorizonHandlers) GetFragmentContextImage(w http.ResponseWriter, r *http.Request, fragmentID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(fragmentID) == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil || fragment == nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	lat, lon, ok := horizonrepo.ResolveFragmentCoordinates(fragment)
	if !ok || !models.IsRealGeoCoordinate(lat, lon) {
		http.Error(w, "fragment has no usable coordinates", http.StatusBadRequest)
		return
	}

	zoom, width, height, scale := parseStaticMapOptions(r)
	h.writeStaticSatelliteImage(w, r, lat, lon, zoom, width, height, scale)
}

func parseLatLonQuery(r *http.Request) (float64, float64, error) {
	q := r.URL.Query()
	latStr := strings.TrimSpace(q.Get("lat"))
	lonStr := strings.TrimSpace(q.Get("lon"))
	if latStr == "" || lonStr == "" {
		return 0, 0, fmt.Errorf("lat and lon query parameters are required")
	}
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lat")
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lon")
	}
	return lat, lon, nil
}

func parseStaticMapOptions(r *http.Request) (zoom, width, height, scale int) {
	q := r.URL.Query()
	zoom = intQuery(q, "zoom", 14)
	width = intQuery(q, "width", 640)
	height = intQuery(q, "height", 640)
	scale = intQuery(q, "scale", 2)
	return zoom, width, height, scale
}

func intQuery(q url.Values, key string, def int) int {
	v := strings.TrimSpace(q.Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (h *HorizonHandlers) writeStaticSatelliteImage(w http.ResponseWriter, r *http.Request, lat, lon float64, zoom, width, height, scale int) {
	key := httputil.GoogleMapsAPIKeyFromEnv()
	if key == "" {
		http.Error(w, "GOOGLE_MAPS_API_KEY is not configured", http.StatusServiceUnavailable)
		return
	}

	img, contentType, err := httputil.FetchGoogleStaticSatelliteMap(r.Context(), key, lat, lon, zoom, width, height, scale)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Geo-Context-Image-Provider", "google-static-satellite")
	w.Header().Set("X-Geo-Context-Image-Center", fmt.Sprintf("%f,%f", lat, lon))
	w.Header().Set("X-Geo-Context-Image-Zoom", strconv.Itoa(zoom))
	w.Header().Set("X-Geo-Context-Image-Attribution", "Imagery © Google / Map data © Google (display per Google Maps Platform Terms of Service)")
	_, _ = w.Write(img)
}
