package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Google Static Maps (satellite) — same API key as Geocoding when both are enabled on the project.
// https://developers.google.com/maps/documentation/maps-static/overview
var googleStaticMapBaseURL = "https://maps.googleapis.com/maps/api/staticmap"

const googleStaticMapMaxBodyBytes = 8 << 20 // 8 MiB safety cap

// FetchGoogleStaticSatelliteMap requests a centered satellite basemap image.
// width/height are logical pixels (each 1–640); scale is 1 or 2 (retina).
// Returns image bytes and Content-Type from the upstream response.
func FetchGoogleStaticSatelliteMap(ctx context.Context, apiKey string, lat, lon float64, zoom, width, height, scale int) ([]byte, string, error) {
	key := strings.Trim(strings.TrimSpace(apiKey), "\"'")
	if key == "" {
		return nil, "", fmt.Errorf("GOOGLE_MAPS_API_KEY is not set")
	}
	if zoom < 0 {
		zoom = 0
	}
	if zoom > 22 {
		zoom = 22
	}
	if width < 1 {
		width = 1
	}
	if width > 640 {
		width = 640
	}
	if height < 1 {
		height = 1
	}
	if height > 640 {
		height = 640
	}
	if scale != 1 && scale != 2 {
		scale = 1
	}

	params := url.Values{}
	params.Set("center", fmt.Sprintf("%f,%f", lat, lon))
	params.Set("zoom", strconv.Itoa(zoom))
	params.Set("size", fmt.Sprintf("%dx%d", width, height))
	params.Set("scale", strconv.Itoa(scale))
	params.Set("maptype", "satellite")
	params.Set("format", "png")
	params.Set("key", key)

	reqURL := googleStaticMapBaseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("static map request: %w", err)
	}

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("static map http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, googleStaticMapMaxBodyBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("static map read: %w", err)
	}
	if len(body) > googleStaticMapMaxBodyBytes {
		return nil, "", fmt.Errorf("static map response too large")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("static map http status %d: %s", resp.StatusCode, truncateErrBody(body))
	}

	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if ct == "" {
		ct = "image/png"
	}
	if !strings.HasPrefix(strings.ToLower(ct), "image/") {
		return nil, "", fmt.Errorf("static map unexpected content-type %q: %s", ct, truncateErrBody(body))
	}
	return body, ct, nil
}

func truncateErrBody(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// GoogleMapsAPIKeyFromEnv returns the trimmed Maps Platform API key from the environment.
func GoogleMapsAPIKeyFromEnv() string {
	return strings.Trim(strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")), "\"'")
}
