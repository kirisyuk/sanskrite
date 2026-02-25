package geocoding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

const defaultNominatimBaseURL = "https://nominatim.openstreetmap.org"

// NominatimClient is a geocoding client backed by OpenStreetMap Nominatim.
// Free, no API key required. Public instance: 1 req/s policy (strict).
// Self-hosted (NOMINATIM_BASE_URL != default): no rate limit by default.
type NominatimClient struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string
	limiter    *rate.Limiter // nil = no limit (self-hosted)
}

// NominatimOptionsFromEnv builds a NominatimClient from environment.
//
// Env:
//   - NOMINATIM_BASE_URL (default: https://nominatim.openstreetmap.org)
//   - NOMINATIM_USER_AGENT (required by Nominatim ToS for public instance, default: "sanskrite/1.0")
//   - NOMINATIM_RATE_LIMIT (requests per second; default: 1 for public, 0=unlimited for self-hosted)
func NominatimOptionsFromEnv() *NominatimClient {
	base := strings.TrimSpace(os.Getenv("NOMINATIM_BASE_URL"))
	if base == "" {
		base = defaultNominatimBaseURL
	}
	base = strings.TrimRight(base, "/")
	ua := strings.TrimSpace(os.Getenv("NOMINATIM_USER_AGENT"))
	if ua == "" {
		ua = "sanskrite/1.0"
	}
	var lim *rate.Limiter
	if rps := strings.TrimSpace(os.Getenv("NOMINATIM_RATE_LIMIT")); rps != "" {
		// Explicit env override
		var n float64
		if _, err := fmt.Sscanf(rps, "%f", &n); err == nil && n > 0 {
			lim = rate.NewLimiter(rate.Limit(n), 1)
		}
	} else if base == defaultNominatimBaseURL {
		// Public instance: strict 1 req/s (Nominatim Usage Policy)
		lim = rate.NewLimiter(rate.Every(time.Second), 1)
	}
	return &NominatimClient{
		baseURL:   base,
		userAgent: ua,
		limiter:   lim,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type nominatimSearchResult struct {
	PlaceID     int64  `json:"place_id"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
	OSMType     string `json:"osm_type"`
	OSMID       int64  `json:"osm_id"`
}

// Geocode implements ontology.GeocodingClientInterface.
func (c *NominatimClient) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	if c == nil {
		return nil, nil
	}
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil, nil
	}

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("nominatim rate limit: %w", err)
		}
	}

	params := url.Values{}
	params.Set("q", addr)
	params.Set("format", "json")
	params.Set("limit", "1")
	params.Set("addressdetails", "0")

	reqURL := c.baseURL + "/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("nominatim request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nominatim http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		// 429: rate limited. Retry-After may be set; wait and retry once.
		retryAfter := 2 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if sec, err := parseRetryAfter(ra); err == nil && sec > 0 {
				retryAfter = time.Duration(sec) * time.Second
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("nominatim: rate limited (429), context cancelled")
		case <-time.After(retryAfter):
			// Retry once
			req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
			req2.Header.Set("User-Agent", c.userAgent)
			req2.Header.Set("Accept", "application/json")
			resp2, err2 := c.httpClient.Do(req2)
			if err2 != nil {
				return nil, fmt.Errorf("nominatim retry after 429: %w", err2)
			}
			defer resp2.Body.Close()
			if resp2.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("nominatim: status %d (after 429 retry)", resp2.StatusCode)
			}
			return parseNominatimBody(resp2.Body)
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nominatim: status %d", resp.StatusCode)
	}
	return parseNominatimBody(resp.Body)
}

func parseNominatimBody(r io.Reader) (*ontology.GeocodingResult, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("nominatim read: %w", err)
	}
	var results []nominatimSearchResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("nominatim decode: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	r0 := results[0]
	lat, lon, parseErr := parseLatLon(r0.Lat, r0.Lon)
	if parseErr != nil {
		return nil, fmt.Errorf("nominatim coords: %w", parseErr)
	}
	return &ontology.GeocodingResult{
		Lat:              lat,
		Lon:              lon,
		FormattedAddress: r0.DisplayName,
		PlaceID:          fmt.Sprintf("osm:%s:%d", r0.OSMType, r0.OSMID),
	}, nil
}

func parseRetryAfter(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	return strconv.Atoi(s)
}

func parseLatLon(latStr, lonStr string) (float64, float64, error) {
	var lat, lon float64
	if _, err := fmt.Sscanf(latStr, "%f", &lat); err != nil {
		return 0, 0, fmt.Errorf("parse lat %q: %w", latStr, err)
	}
	if _, err := fmt.Sscanf(lonStr, "%f", &lon); err != nil {
		return 0, 0, fmt.Errorf("parse lon %q: %w", lonStr, err)
	}
	return lat, lon, nil
}
