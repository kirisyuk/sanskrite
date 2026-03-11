package nlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HTTPEndpoints struct {
	EventGateURL     string
	EntityExtractURL string
	Timeout          time.Duration
	HTTPClient       *http.Client
}

type eventGateHTTPClient struct {
	baseURL string
	client  *http.Client
}

type entityExtractHTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPEventGate(baseURL string, timeout time.Duration) EventGate {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	return &eventGateHTTPClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func NewHTTPEntityExtractor(baseURL string, timeout time.Duration) EntityExtractor {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &entityExtractHTTPClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *eventGateHTTPClient) Assess(doc DocumentInput) (*ArticleAssessment, error) {
	if c == nil || c.client == nil || c.baseURL == "" {
		return nil, fmt.Errorf("event gate client is not configured")
	}
	var out ArticleAssessment
	if err := doJSONRequest(c.client, c.baseURL+"/v1/event-gate/assess", doc, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *entityExtractHTTPClient) Extract(doc DocumentInput) (*EntityExtractionResult, error) {
	if c == nil || c.client == nil || c.baseURL == "" {
		return nil, fmt.Errorf("entity extractor client is not configured")
	}
	var out EntityExtractionResult
	if err := doJSONRequest(c.client, c.baseURL+"/v1/entity-extract/extract", doc, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func doJSONRequest(client *http.Client, endpoint string, payload interface{}, out interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("nlp service %s returned status %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
