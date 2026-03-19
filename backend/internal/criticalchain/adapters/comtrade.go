package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
)

// UN Comtrade API (free tier) - https://comtradeplus.un.org/
const comtradeBase = "https://comtradeplus.un.org/api/get"

// ComtradeAdapter fetches trade flows from UN Comtrade (public free tier, optional key for higher limits).
type ComtradeAdapter struct {
	client *http.Client
	apiKey string
}

// NewComtradeAdapter creates a new Comtrade adapter.
func NewComtradeAdapter() *ComtradeAdapter {
	apiKey := os.Getenv("UN_COMTRADE_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("COMTRADE_SUBSCRIPTION_KEY")
	}
	return &ComtradeAdapter{
		client: &http.Client{Timeout: 60 * time.Second},
		apiKey: apiKey,
	}
}

// Name implements datasource.Adapter.
func (a *ComtradeAdapter) Name() string { return "comtrade" }

// Fetch implements datasource.Adapter.
func (a *ComtradeAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	// Comtrade+ API - bulk download may require key; basic queries work without
	u, _ := url.Parse(comtradeBase + "/bulk/C/A/840/0/0/0")
	q := u.Query()
	q.Set("maxRecords", "100")
	if a.apiKey != "" {
		q.Set("subscription-key", a.apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("[CriticalChain/Comtrade] fetch error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var raw []map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		raw = []map[string]interface{}{}
	}
	out := make([]datasource.RawRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, datasource.RawRecord{
			Source:    a.Name(),
			Data:      r,
			FetchedAt: time.Now(),
		})
	}
	return out, nil
}

// Normalize implements datasource.Adapter.
func (a *ComtradeAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	id := fmt.Sprintf("comtrade_%v_%v_%v", m["period"], m["reporterCode"], m["partnerCode"])
	return datasource.CanonicalRecord{
		Kind:    "trade",
		ID:      id,
		Payload: m,
	}, nil
}

// Schema implements datasource.Adapter.
func (a *ComtradeAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "comtrade", RecordKind: "trade", Fields: []string{"period", "reporterCode", "partnerCode", "cmdCode"}}
}

// RateLimit implements datasource.Adapter.
func (a *ComtradeAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 30, Burst: 5}
}
