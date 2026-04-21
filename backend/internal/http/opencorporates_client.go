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

// OpenCorporatesClient handles communication with OpenCorporates API.
//
// Docs: https://api.opencorporates.com/documentation/API-Reference
// Auth: API token via OPEN_CORPORATES_API_TOKEN (required; 401 without it).
const openCorporatesBaseURL = "https://api.opencorporates.com/v0.4"

// OpenCorporatesClient is the HTTP client for OpenCorporates API.
type OpenCorporatesClient struct {
	apiToken  string
	httpClient *http.Client
	userAgent string
}

// OpenCorporatesCompany is a company from search results.
type OpenCorporatesCompany struct {
	CompanyNumber   string `json:"company_number"`
	JurisdictionCode string `json:"jurisdiction_code"`
	Name            string `json:"name"`
	CompanyType     string `json:"company_type,omitempty"`
	CurrentStatus   string `json:"current_status,omitempty"`
	Inactive        bool   `json:"inactive"`
	OpenCorporatesURL string `json:"opencorporates_url,omitempty"`
}

// OpenCorporatesSearchResponse is the response from companies/search.
type OpenCorporatesSearchResponse struct {
	Results struct {
		Companies []struct {
			Company OpenCorporatesCompany `json:"company"`
		} `json:"companies"`
		Page       int `json:"page"`
		PerPage    int `json:"per_page"`
		TotalCount int `json:"total_count"`
	} `json:"results"`
}

// NewOpenCorporatesClient creates a new OpenCorporates client.
func NewOpenCorporatesClient(apiToken string) *OpenCorporatesClient {
	return &OpenCorporatesClient{
		apiToken: strings.TrimSpace(apiToken),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		userAgent: "Horizon/1.0",
	}
}

// GetOpenCorporatesClientFromEnv creates client from environment.
//
// Env: OPEN_CORPORATES_API_TOKEN (required; API returns 401 without it as of 2024).
func GetOpenCorporatesClientFromEnv() *OpenCorporatesClient {
	token := strings.Trim(strings.TrimSpace(os.Getenv("OPEN_CORPORATES_API_TOKEN")), "\"'")
	return NewOpenCorporatesClient(token)
}

// IsConfigured returns true if the client has a valid API token.
// OpenCorporates API requires a token; requests without it return 401 Unauthorized.
func (c *OpenCorporatesClient) IsConfigured() bool {
	return c != nil && strings.TrimSpace(c.apiToken) != ""
}

// SearchCompanies searches for companies by name.
// Returns up to perPage results (max 30 default, 100 max).
func (c *OpenCorporatesClient) SearchCompanies(ctx context.Context, query string, perPage int) (*OpenCorporatesSearchResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("OpenCorporates client not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if perPage <= 0 {
		perPage = 10
	}
	if perPage > 30 {
		perPage = 30
	}

	u, err := url.Parse(openCorporatesBaseURL + "/companies/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("per_page", fmt.Sprintf("%d", perPage))
	q.Set("order", "score") // Prefer best matches
	q.Set("inactive", "false") // Prefer active companies
	if c.apiToken != "" {
		q.Set("api_token", c.apiToken)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenCorporates API error: status=%d body=%s", resp.StatusCode, string(body))
	}

	var out OpenCorporatesSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode OpenCorporates response: %w", err)
	}
	return &out, nil
}

// CompanyID returns the canonical ID for a company (jurisdiction_code/company_number).
func (co *OpenCorporatesCompany) CompanyID() string {
	jc := strings.TrimSpace(co.JurisdictionCode)
	cn := strings.TrimSpace(co.CompanyNumber)
	if jc == "" || cn == "" {
		return ""
	}
	return jc + "/" + cn
}
