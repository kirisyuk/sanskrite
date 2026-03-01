package http

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
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// GeoNamesClient handles communication with GeoNames public API.
//
// Docs:
// - https://www.geonames.org/export/web-services.html
//
// Auth:
// - GeoNames uses a "username" parameter (no API key for basic usage).
// - Create username at https://www.geonames.org/login
type GeoNamesClient struct {
	username   string
	httpClient *http.Client
	baseURL    string
	userAgent  string
	limiter    *rate.Limiter
}

// GeoNamesStatusError is returned when GeoNames responds with {"status": {...}}.
type GeoNamesStatusError struct {
	Message string
	Value   int
}

func (e *GeoNamesStatusError) Error() string {
	if e == nil {
		return "geonames error"
	}
	if e.Value != 0 {
		return fmt.Sprintf("geonames error (code=%d): %s", e.Value, e.Message)
	}
	return fmt.Sprintf("geonames error: %s", e.Message)
}

// GeoNamesPlace represents a GeoNames feature/entity.
// We keep it minimal and add fields as needed.
type GeoNamesPlace struct {
	GeoNameID    int    `json:"geonameId"`
	Name         string `json:"name"`
	ToponymName  string `json:"toponymName,omitempty"`
	CountryCode  string `json:"countryCode,omitempty"` // ISO3166-1 alpha2 (uppercase in GeoNames)
	AdminCode1   string `json:"adminCode1,omitempty"`  // ADM1 code within country
	AdminName1   string `json:"adminName1,omitempty"`  // display name
	FeatureClass string `json:"fcl,omitempty"`         // e.g. "P", "A"
	FeatureCode  string `json:"fcode,omitempty"`       // e.g. "PCLI", "ADM1", "PPL"
	Lat          string `json:"lat,omitempty"`         // numeric string
	Lng          string `json:"lng,omitempty"`         // numeric string
	Population   int64  `json:"population,omitempty"`  // 0 if missing
}

// GeoNamesSearchResponse represents the response from /searchJSON.
type GeoNamesSearchResponse struct {
	Query             string          `json:"query"`
	TotalResultsCount int             `json:"totalResultsCount,omitempty"`
	Results           []GeoNamesPlace `json:"results"`
}

// GeoNamesHierarchyResponse represents the response from /hierarchyJSON.
type GeoNamesHierarchyResponse struct {
	GeoNameID int             `json:"geonameId,omitempty"`
	Results   []GeoNamesPlace `json:"results"`
}

type geonamesStatusEnvelope struct {
	Status *struct {
		Message string `json:"message"`
		Value   int    `json:"value"`
	} `json:"status,omitempty"`
}

// GeoNamesSearchOptions adds optional filters for GeoNames search.
// See https://www.geonames.org/export/geonames-search.html
type GeoNamesSearchOptions struct {
	// Country is ISO3166-1 alpha2 (case-insensitive).
	Country string
	// AdminCode1 is ADM1 code (e.g. "AZ" for Abu Dhabi in AE).
	AdminCode1 string
	// FeatureClass is GeoNames feature class (e.g. "A" or "P").
	FeatureClass string
	// FeatureCode is a feature code (e.g. "PCLI", "ADM1", "PPL").
	FeatureCode string
	// NameEquals enables stricter exact name matching (maps to name_equals).
	NameEquals bool
}

// NewGeoNamesClient creates a new GeoNames client.
func NewGeoNamesClient(username string) *GeoNamesClient {
	username = strings.TrimSpace(strings.Trim(username, "\"'"))
	if username == "" {
		return nil
	}
	return &GeoNamesClient{
		username: username,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		// GeoNames HTTPS endpoint uses secure.geonames.org (certificate SANs).
		// api.geonames.org may work over HTTP, but HTTPS is expected in production.
		baseURL:   "https://secure.geonames.org",
		userAgent: "sanskrite/1.0",
		limiter:   defaultGeoNamesLimiter(),
	}
}

// DefaultGeoNamesUsername is used when no env var is set (shared demo/dev username).
const DefaultGeoNamesUsername = "kirisyuk"

// GetGeoNamesClientFromEnv creates a GeoNames client from environment variables.
//
// Env:
// - GEONAMES_USERNAME (preferred)
// - HORIZON_GEONAMES_USERNAME (fallback)
// - DefaultGeoNamesUsername if both unset
func GetGeoNamesClientFromEnv() *GeoNamesClient {
	username := strings.Trim(strings.TrimSpace(os.Getenv("GEONAMES_USERNAME")), "\"'")
	if username == "" {
		username = strings.Trim(strings.TrimSpace(os.Getenv("HORIZON_GEONAMES_USERNAME")), "\"'")
	}
	if username == "" {
		username = DefaultGeoNamesUsername
	}
	return NewGeoNamesClient(username)
}

var (
	gnLimiterOnce sync.Once
	gnLimiter     *rate.Limiter
)

func defaultGeoNamesLimiter() *rate.Limiter {
	gnLimiterOnce.Do(func() {
		ms, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HORIZON_GEONAMES_RATE_LIMIT_MS")))
		if ms <= 0 {
			// Conservative default: 5 req/s (200ms interval).
			ms = 200
		}
		gnLimiter = rate.NewLimiter(rate.Every(time.Duration(ms)*time.Millisecond), 1)
	})
	return gnLimiter
}

// Search performs a text search on GeoNames.
// maxResults is clamped to [1..50].
func (c *GeoNamesClient) Search(ctx context.Context, query string, maxResults int, opts GeoNamesSearchOptions) (*GeoNamesSearchResponse, error) {
	if c == nil || strings.TrimSpace(c.username) == "" {
		return nil, fmt.Errorf("GeoNames client not configured: username missing")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 50 {
		maxResults = 50
	}

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("geonames rate limit wait failed: %w", err)
		}
	}

	u, _ := url.Parse(c.baseURL + "/searchJSON")
	q := u.Query()
	q.Set("username", c.username)
	if opts.NameEquals {
		q.Set("name_equals", query)
	} else {
		q.Set("q", query)
	}
	q.Set("maxRows", strconv.Itoa(maxResults))
	q.Set("style", "FULL")

	if cc := strings.ToUpper(strings.TrimSpace(opts.Country)); cc != "" {
		q.Set("country", cc)
	}
	if ac := strings.TrimSpace(opts.AdminCode1); ac != "" {
		q.Set("adminCode1", ac)
	}
	if fcl := strings.ToUpper(strings.TrimSpace(opts.FeatureClass)); fcl != "" {
		q.Set("featureClass", fcl)
	}
	if fcode := strings.ToUpper(strings.TrimSpace(opts.FeatureCode)); fcode != "" {
		q.Set("featureCode", fcode)
	}

	u.RawQuery = q.Encode()

	body, err := c.doGET(ctx, u.String())
	if err != nil {
		return nil, err
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read geonames response: %w", err)
	}

	// Check for error envelope first.
	var env geonamesStatusEnvelope
	_ = json.Unmarshal(raw, &env)
	if env.Status != nil {
		return nil, &GeoNamesStatusError{Message: env.Status.Message, Value: env.Status.Value}
	}

	var resp struct {
		Total int             `json:"totalResultsCount,omitempty"`
		Geo   []GeoNamesPlace `json:"geonames,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse geonames search response: %w", err)
	}

	out := &GeoNamesSearchResponse{
		Query:             query,
		TotalResultsCount: resp.Total,
		Results:           resp.Geo,
	}
	return out, nil
}

// GetPlace loads a single GeoNames record by geonameId.
func (c *GeoNamesClient) GetPlace(ctx context.Context, geonameID int) (*GeoNamesPlace, error) {
	if c == nil || strings.TrimSpace(c.username) == "" {
		return nil, fmt.Errorf("GeoNames client not configured: username missing")
	}
	if geonameID <= 0 {
		return nil, fmt.Errorf("geonameId must be positive")
	}

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("geonames rate limit wait failed: %w", err)
		}
	}

	u, _ := url.Parse(c.baseURL + "/getJSON")
	q := u.Query()
	q.Set("username", c.username)
	q.Set("geonameId", strconv.Itoa(geonameID))
	u.RawQuery = q.Encode()

	body, err := c.doGET(ctx, u.String())
	if err != nil {
		return nil, err
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read geonames response: %w", err)
	}

	var env geonamesStatusEnvelope
	_ = json.Unmarshal(raw, &env)
	if env.Status != nil {
		return nil, &GeoNamesStatusError{Message: env.Status.Message, Value: env.Status.Value}
	}

	var place GeoNamesPlace
	if err := json.Unmarshal(raw, &place); err != nil {
		return nil, fmt.Errorf("failed to parse geonames get response: %w", err)
	}
	if place.GeoNameID == 0 {
		place.GeoNameID = geonameID
	}
	return &place, nil
}

// GetCountryCode returns ISO3166-1 alpha2 country code for given lat/lon (reverse geocoding).
// Uses countryCode JSON endpoint. Returns empty string if point is in water or unknown.
func (c *GeoNamesClient) GetCountryCode(ctx context.Context, lat, lon float64) (string, error) {
	if c == nil || strings.TrimSpace(c.username) == "" {
		return "", fmt.Errorf("GeoNames client not configured: username missing")
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("geonames rate limit wait failed: %w", err)
		}
	}
	u, _ := url.Parse(c.baseURL + "/countryCodeJSON")
	q := u.Query()
	q.Set("username", c.username)
	q.Set("lat", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("lng", strconv.FormatFloat(lon, 'f', 6, 64))
	u.RawQuery = q.Encode()

	body, err := c.doGET(ctx, u.String())
	if err != nil {
		return "", err
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("failed to read geonames countryCode response: %w", err)
	}
	var env geonamesStatusEnvelope
	_ = json.Unmarshal(raw, &env)
	if env.Status != nil {
		return "", &GeoNamesStatusError{Message: env.Status.Message, Value: env.Status.Value}
	}
	var resp struct {
		CountryCode string `json:"countryCode,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("failed to parse geonames countryCode response: %w", err)
	}
	return strings.ToUpper(strings.TrimSpace(resp.CountryCode)), nil
}

// GetHierarchy loads the canonical hierarchy for a given geonameId.
// Typically returns a list including country (PCLI) and admin1 (ADM1) when available.
func (c *GeoNamesClient) GetHierarchy(ctx context.Context, geonameID int) (*GeoNamesHierarchyResponse, error) {
	if c == nil || strings.TrimSpace(c.username) == "" {
		return nil, fmt.Errorf("GeoNames client not configured: username missing")
	}
	if geonameID <= 0 {
		return nil, fmt.Errorf("geonameId must be positive")
	}

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("geonames rate limit wait failed: %w", err)
		}
	}

	u, _ := url.Parse(c.baseURL + "/hierarchyJSON")
	q := u.Query()
	q.Set("username", c.username)
	q.Set("geonameId", strconv.Itoa(geonameID))
	u.RawQuery = q.Encode()

	body, err := c.doGET(ctx, u.String())
	if err != nil {
		return nil, err
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read geonames response: %w", err)
	}

	var env geonamesStatusEnvelope
	_ = json.Unmarshal(raw, &env)
	if env.Status != nil {
		return nil, &GeoNamesStatusError{Message: env.Status.Message, Value: env.Status.Value}
	}

	var resp struct {
		GeoNameID int             `json:"geonameId,omitempty"`
		Geo       []GeoNamesPlace `json:"geonames,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse geonames hierarchy response: %w", err)
	}

	return &GeoNamesHierarchyResponse{
		GeoNameID: geonameID,
		Results:   resp.Geo,
	}, nil
}

func (c *GeoNamesClient) doGET(ctx context.Context, fullURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create geonames request: %w", err)
	}
	if strings.TrimSpace(c.userAgent) != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geonames request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to read body for diagnostics (best-effort).
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("geonames http error: %s", msg)
	}
	return resp.Body, nil
}
