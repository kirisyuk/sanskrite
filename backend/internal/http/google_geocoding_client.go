package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GoogleGeocodingClient handles communication with Google Maps Geocoding API.
//
// Docs: https://developers.google.com/maps/documentation/geocoding
// Auth: API key via GOOGLE_MAPS_API_KEY (or google_maps_api_key secret)
const geocodingBaseURL = "https://maps.googleapis.com/maps/api/geocode/json"

// GeocodingResult represents a single geocoded result.
type GeocodingResult struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// FormattedAddress is the human-readable address returned by the API.
	FormattedAddress string `json:"formattedAddress,omitempty"`
	// PlaceID can be used for caching/deduplication.
	PlaceID string `json:"placeId,omitempty"`
}

type googleGeocodingAPIResponse struct {
	Results []struct {
		PlaceID          string `json:"place_id"`
		FormattedAddress string `json:"formatted_address"`
		Geometry         struct {
			Location struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"location"`
		} `json:"geometry"`
	} `json:"results"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// GoogleGeocodingClient is the HTTP client for Google Geocoding API.
type GoogleGeocodingClient struct {
	apiKey     string
	httpClient *http.Client
}

// NewGoogleGeocodingClient creates a new Geocoding API client.
func NewGoogleGeocodingClient(apiKey string) *GoogleGeocodingClient {
	key := strings.TrimSpace(apiKey)
	key = strings.Trim(key, "\"'")
	return &GoogleGeocodingClient{
		apiKey: key,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetGoogleGeocodingClientFromEnv creates a client from environment.
// Env: GOOGLE_MAPS_API_KEY
func GetGoogleGeocodingClientFromEnv() *GoogleGeocodingClient {
	key := strings.Trim(strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")), "\"'")
	if key == "" {
		return nil
	}
	return NewGoogleGeocodingClient(key)
}

// Geocode converts an address string to latitude/longitude.
// Returns the first (best) result. Use components for structured queries.
func (c *GoogleGeocodingClient) Geocode(ctx context.Context, address string) (*GeocodingResult, error) {
	if c == nil || strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("google geocoding client not configured")
	}
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil, fmt.Errorf("address cannot be empty")
	}

	params := url.Values{}
	params.Set("address", addr)
	params.Set("key", c.apiKey)

	reqURL := geocodingBaseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("geocoding request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocoding http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("geocoding read: %w", err)
	}

	var apiResp googleGeocodingAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("geocoding decode: %w", err)
	}

	switch apiResp.Status {
	case "OK":
		if len(apiResp.Results) == 0 {
			return nil, fmt.Errorf("geocoding: no results for address %q", addr)
		}
		r := apiResp.Results[0]
		return &GeocodingResult{
			Lat:              r.Geometry.Location.Lat,
			Lon:              r.Geometry.Location.Lng,
			FormattedAddress: r.FormattedAddress,
			PlaceID:          r.PlaceID,
		}, nil
	case "ZERO_RESULTS":
		return nil, nil // Not an error; address not found
	case "OVER_QUERY_LIMIT":
		return nil, fmt.Errorf("geocoding: over query limit")
	case "REQUEST_DENIED":
		return nil, fmt.Errorf("geocoding: request denied: %s", apiResp.ErrorMessage)
	case "INVALID_REQUEST":
		return nil, fmt.Errorf("geocoding: invalid request: %s", apiResp.ErrorMessage)
	default:
		return nil, fmt.Errorf("geocoding: status=%s: %s", apiResp.Status, apiResp.ErrorMessage)
	}
}

// GeocodeWithComponents performs component-based geocoding (e.g. street, city, country).
// Components are joined into a single address string for the API.
func (c *GoogleGeocodingClient) GeocodeWithComponents(ctx context.Context, components map[string]string) (*GeocodingResult, error) {
	if len(components) == 0 {
		return nil, fmt.Errorf("components cannot be empty")
	}
	// Build address from components in a stable order
	parts := make([]string, 0, len(components))
	for _, k := range []string{"street", "street_address", "address", "locality", "city", "admin1", "region", "postal_code", "country"} {
		if v := strings.TrimSpace(components[k]); v != "" {
			parts = append(parts, v)
		}
	}
	// Add any remaining keys not in the standard order
	seen := map[string]bool{}
	for _, k := range []string{"street", "street_address", "address", "locality", "city", "admin1", "region", "postal_code", "country"} {
		seen[k] = true
	}
	for k, v := range components {
		if !seen[k] && strings.TrimSpace(v) != "" {
			parts = append(parts, strings.TrimSpace(v))
		}
	}
	addr := strings.Join(parts, ", ")
	if addr == "" {
		return nil, fmt.Errorf("no non-empty components")
	}
	return c.Geocode(ctx, addr)
}

// IsConfigured returns true if the client has a valid API key.
func (c *GoogleGeocodingClient) IsConfigured() bool {
	return c != nil && strings.TrimSpace(c.apiKey) != ""
}
