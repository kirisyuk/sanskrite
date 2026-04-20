package pathquery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type cacheKeyPayload struct {
	Tenant         string   `json:"tenant"`
	Project        string   `json:"project"`
	FromKind       int      `json:"fromKind"`
	FromValue      string   `json:"fromValue"`
	ToPresent      bool     `json:"toPresent"`
	ToKind         int      `json:"toKind"`
	ToValue        string   `json:"toValue"`
	ProductAxis    string   `json:"productAxis"`
	MaxHops        int      `json:"maxHops"`
	RelationKinds  []string `json:"relationKinds"`
	LensID         string   `json:"lensId"`
	Scope          string   `json:"scope"`
	AsOfKey        string   `json:"asOfKey"`
	IncludeStress  bool     `json:"includeStress"`
	IncludeGeo     bool     `json:"includeGeo"`
}

func asOfCacheKey(t time.Time) string {
	if t.IsZero() {
		return "live"
	}
	return t.UTC().Format("2006-01-02")
}

func sortedRelationKeyNames(rels []ah.RelationName) []string {
	if len(rels) == 0 {
		return nil
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, string(r))
	}
	sort.Strings(out)
	return out
}

func computeCacheKey(q Query, fromKind EndpointKind, fromVal string, toPresent bool, toKind EndpointKind, toVal string, maxHops int, rels []ah.RelationName) string {
	payload := cacheKeyPayload{
		Tenant:        strings.TrimSpace(q.Tenant),
		Project:       strings.TrimSpace(q.Project),
		FromKind:      int(fromKind),
		FromValue:     fromVal,
		ToPresent:     toPresent,
		ToKind:        int(toKind),
		ToValue:       toVal,
		ProductAxis:   strings.TrimSpace(q.ProductAxis),
		MaxHops:       maxHops,
		RelationKinds: sortedRelationKeyNames(rels),
		LensID:        strings.TrimSpace(q.Context.LensID),
		Scope:         strings.TrimSpace(q.Context.Scope),
		AsOfKey:       asOfCacheKey(q.Context.AsOf),
		IncludeStress: q.IncludeStress,
		IncludeGeo:    q.IncludeGeo,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(fmt.Sprintf("%+v", payload))
	}
	sum := sha256.Sum256(raw)
	return "pq_" + hex.EncodeToString(sum[:])
}
