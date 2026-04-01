package scout

import (
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type geoResolveAttempt struct {
	Query            string
	Provider         string
	Outcome          string
	Reason           string
	ResolvedISO2     string
	FormattedAddress string
	Score            int
}

func recordGeoResolveTrace(fragment *models.HorizonFragment, phase string, attempts []geoResolveAttempt, accepted *geoResolveAttempt) {
	if fragment == nil {
		return
	}
	if fragment.Qualifiers == nil {
		fragment.Qualifiers = map[string]interface{}{}
	}
	serializedAttempts := make([]map[string]interface{}, 0, minInt(len(attempts), 8))
	for i, attempt := range attempts {
		if i >= 8 {
			break
		}
		row := map[string]interface{}{
			"query":   truncateGeoTraceValue(attempt.Query, 120),
			"outcome": truncateGeoTraceValue(attempt.Outcome, 32),
			"reason":  truncateGeoTraceValue(attempt.Reason, 120),
		}
		if provider := strings.TrimSpace(attempt.Provider); provider != "" {
			row["provider"] = truncateGeoTraceValue(provider, 32)
		}
		if iso2 := strings.TrimSpace(attempt.ResolvedISO2); iso2 != "" {
			row["country"] = strings.ToUpper(iso2)
		}
		if formatted := strings.TrimSpace(attempt.FormattedAddress); formatted != "" {
			row["formattedAddress"] = truncateGeoTraceValue(formatted, 140)
		}
		if attempt.Score > 0 {
			row["score"] = attempt.Score
		}
		serializedAttempts = append(serializedAttempts, row)
	}
	trace := map[string]interface{}{
		"phase":    strings.TrimSpace(phase),
		"at":       time.Now().UTC().Format(time.RFC3339),
		"attempts": serializedAttempts,
	}
	if accepted != nil {
		trace["accepted"] = map[string]interface{}{
			"query":   truncateGeoTraceValue(accepted.Query, 120),
			"reason":  truncateGeoTraceValue(accepted.Reason, 120),
			"score":   accepted.Score,
			"country": strings.ToUpper(strings.TrimSpace(accepted.ResolvedISO2)),
		}
		if provider := strings.TrimSpace(accepted.Provider); provider != "" {
			trace["accepted"].(map[string]interface{})["provider"] = truncateGeoTraceValue(provider, 32)
		}
		if formatted := strings.TrimSpace(accepted.FormattedAddress); formatted != "" {
			trace["accepted"].(map[string]interface{})["formattedAddress"] = truncateGeoTraceValue(formatted, 140)
		}
	}
	fragment.Qualifiers["geoResolveTrace"] = trace
}

func truncateGeoTraceValue(raw string, limit int) string {
	v := strings.TrimSpace(raw)
	if limit <= 0 || len(v) <= limit {
		return v
	}
	return strings.TrimSpace(v[:limit-3]) + "..."
}

func isWeakGeoResolveQuery(query string) bool {
	head := strings.TrimSpace(strings.Split(strings.TrimSpace(query), ",")[0])
	if head == "" {
		return true
	}
	tokens := strings.Fields(normalizeGeoPhrase(head))
	if len(tokens) == 0 {
		return true
	}
	signal := 0
	for _, token := range tokens {
		switch {
		case len(token) <= 2:
			continue
		case len(token) <= 3 && isAllASCIIUpperish(head):
			continue
		case isWeakGeoResolveToken(token):
			continue
		default:
			signal++
		}
	}
	if signal == 0 {
		return true
	}
	if len(tokens) == 1 && signal == 1 && len(tokens[0]) <= 4 {
		return true
	}
	return false
}

func isWeakGeoResolveToken(token string) bool {
	switch strings.TrimSpace(strings.ToLower(token)) {
	case "plant", "factory", "facility", "facilities", "field", "launch", "power", "campus", "center", "centre",
		"station", "terminal", "site", "network", "group", "company", "industries", "industry", "service", "services",
		"ministry", "department", "authority", "bureau", "office", "national", "people", "peoples", "defence", "defense",
		"production", "processing", "manufacturing", "general", "pdf", "hub", "hubs", "chemical", "chemicals",
		"showroom", "warehouse", "warehousing", "depot", "storage", "logistics", "distribution", "retail",
		"consumer", "industrial", "automotive", "personal", "care", "commercial", "operations", "sales",
		"border", "crossing", "checkpoint", "post", "supermarket", "hypermarket", "store", "shop", "outlet":
		return true
	default:
		return false
	}
}

func isAllASCIIUpperish(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	seenLetter := false
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			seenLetter = true
		case r >= 'a' && r <= 'z':
			return false
		}
	}
	return seenLetter
}

func geoResolveScoreReason(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "; ")
}

func formatGeoResolveErr(err error) string {
	if err == nil {
		return ""
	}
	return truncateGeoTraceValue(fmt.Sprintf("%v", err), 120)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
