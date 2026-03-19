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

const ofacSDNURL = "https://www.treasury.gov/ofac/downloads/sdn.xml"

// OFACSDNAdapter fetches OFAC SDN List (no API key required).
type OFACSDNAdapter struct {
	client *http.Client
}

// NewOFACSDNAdapter creates a new OFAC SDN adapter.
func NewOFACSDNAdapter() *OFACSDNAdapter {
	return &OFACSDNAdapter{
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Name implements DataSourceAdapter.
func (a *OFACSDNAdapter) Name() string { return "ofac_sdn" }

// sdnXML is a minimal struct for parsing OFAC SDN XML.
type sdnXML struct {
	XMLName xml.Name  `xml:"sdnList"`
	Entries []sdnEntry `xml:"sdnEntry"`
}

type sdnEntry struct {
	UID    string   `xml:"uid"`
	LastName string `xml:"lastName"`
	FirstName string `xml:"firstName"`
	SDNType string `xml:"sdnType"`
	Program string `xml:"programList>program"`
}

// Fetch implements datasource.Adapter.
func (a *OFACSDNAdapter) Fetch(ctx context.Context, since time.Time) ([]datasource.RawRecord, error) {
	url := os.Getenv("OFAC_SDN_URL")
	if url == "" {
		url = ofacSDNURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("[CriticalChain/OFAC] fetch error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var list sdnXML
	if err := xml.NewDecoder(resp.Body).Decode(&list); err != nil {
		log.Printf("[CriticalChain/OFAC] parse error: %v", err)
		return nil, nil
	}
	out := make([]datasource.RawRecord, 0, len(list.Entries))
	for _, e := range list.Entries {
		out = append(out, datasource.RawRecord{
			Source: a.Name(),
			Data: map[string]interface{}{
				"uid":       e.UID,
				"lastName":  e.LastName,
				"firstName": e.FirstName,
				"sdnType":   e.SDNType,
				"program":   e.Program,
			},
			FetchedAt: time.Now(),
		})
	}
	return out, nil
}

// Normalize implements datasource.Adapter. Produces sanction records for entity_sanctions.
func (a *OFACSDNAdapter) Normalize(raw datasource.RawRecord) (datasource.CanonicalRecord, error) {
	m, ok := raw.Data.(map[string]interface{})
	if !ok {
		return datasource.CanonicalRecord{}, fmt.Errorf("invalid raw record")
	}
	uid := stringVal(m, "uid")
	name := strings.TrimSpace(stringVal(m, "firstName") + " " + stringVal(m, "lastName"))
	if name == "" {
		name = uid
	}
	return datasource.CanonicalRecord{
		Kind: "sanction",
		ID:   "ofac_" + uid,
		Payload: map[string]interface{}{
			"uid":       uid,
			"name":      name,
			"source":    "OFAC",
			"sdnType":   stringVal(m, "sdnType"),
			"program":   stringVal(m, "program"),
		},
	}, nil
}


// Schema implements datasource.Adapter.
func (a *OFACSDNAdapter) Schema() datasource.SourceSchema {
	return datasource.SourceSchema{
		SourceID:   "ofac_sdn",
		RecordKind: "sanction",
		Fields:     []string{"uid", "lastName", "firstName", "sdnType", "program"},
	}
}

// RateLimit implements datasource.Adapter.
func (a *OFACSDNAdapter) RateLimit() datasource.RateLimitConfig {
	return datasource.RateLimitConfig{RequestsPerMinute: 10, Burst: 1}
}
