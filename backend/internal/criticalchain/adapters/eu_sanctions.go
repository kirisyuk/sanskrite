package adapters

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
)

// EU Consolidated List - XML from EC FSF (webgate.ec.europa.eu)
const euSanctionsBase = "https://webgate.ec.europa.eu/fsd/fsf/public/files/xmlFullSanctionsList_1_1/content"

// EUSanctionsAdapter fetches EU Consolidated List (public, no key).
type EUSanctionsAdapter struct {
	client *http.Client
}

// NewEUSanctionsAdapter creates a new EU sanctions adapter.
func NewEUSanctionsAdapter() *EUSanctionsAdapter {
	return &EUSanctionsAdapter{
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Name implements datasource.Adapter.
func (a *EUSanctionsAdapter) Name() string { return "eu_sanctions" }

// Fetch implements datasource.Adapter.
func (a *EUSanctionsAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	url := euSanctionsBase
	if token := os.Getenv("EU_SANCTIONS_TOKEN"); token != "" {
		url += "?token=" + token
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CriticalChain/1.0")
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("[CriticalChain/EUSanctions] fetch error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[CriticalChain/EUSanctions] status %d", resp.StatusCode)
		return nil, nil
	}
	// Try FSF v1.1 structure (sanctionEntity with logicalId, firstName, lastName)
	var list1 struct {
		XMLName xml.Name `xml:"sanctions"`
		Entries []struct {
			LogicalID   string `xml:"logicalId"`
			FirstName   string `xml:"firstName"`
			LastName    string `xml:"lastName"`
			WholeName   string `xml:"wholeName"`
			EntityType  string `xml:"entityType"`
		} `xml:"sanctionEntity"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&list1); err != nil {
		log.Printf("[CriticalChain/EUSanctions] parse error: %v", err)
		return nil, nil
	}
	out := make([]datasource.RawRecord, 0, len(list1.Entries))
	for _, e := range list1.Entries {
		out = append(out, datasource.RawRecord{
			Source: a.Name(),
			Data: map[string]interface{}{
				"logicalId":  e.LogicalID,
				"firstName":  e.FirstName,
				"lastName":   e.LastName,
				"wholeName":  e.WholeName,
				"entityType": e.EntityType,
			},
			FetchedAt: time.Now(),
		})
	}
	return out, nil
}

// Normalize implements datasource.Adapter.
func (a *EUSanctionsAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	id := "eu_" + stringVal(m, "logicalId")
	name := strings.TrimSpace(stringVal(m, "firstName") + " " + stringVal(m, "lastName"))
	if name == "" {
		name = stringVal(m, "wholeName")
	}
	if name == "" {
		name = stringVal(m, "logicalId")
	}
	return datasource.CanonicalRecord{
		Kind: "sanction",
		ID:  id,
		Payload: map[string]interface{}{
			"logicalId":  stringVal(m, "logicalId"),
			"name":      name,
			"source":    "EU",
			"entityType": stringVal(m, "entityType"),
		},
	}, nil
}

// Schema implements datasource.Adapter.
func (a *EUSanctionsAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{SourceID: "eu_sanctions", RecordKind: "sanction", Fields: []string{"logicalId", "firstName", "lastName"}}
}

// RateLimit implements datasource.Adapter.
func (a *EUSanctionsAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 5, Burst: 1}
}
