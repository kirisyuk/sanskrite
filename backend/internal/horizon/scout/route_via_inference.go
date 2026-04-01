package scout

import (
	"regexp"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

type routeHintAlias struct {
	Ref                  string
	Phrase               string
	RequiresRouteContext bool
}

type routeHintMatch struct {
	Ref       string
	TextIndex int
	Position  int
	Strength  int
}

var canonicalFacilityPathPattern = regexp.MustCompile(`/fac/[a-z0-9/_-]+`)

var knownRouteHintAliases = []routeHintAlias{
	{Ref: "/fac/de/port-of-hamburg", Phrase: "port of hamburg"},
	{Ref: "/fac/gb/port-of-felixstowe", Phrase: "port of felixstowe"},
	{Ref: "/fac/nl/port-of-rotterdam", Phrase: "port of rotterdam"},
	{Ref: "/fac/fr/port-of-marseille", Phrase: "port of marseille"},
	{Ref: "/fac/it/port-of-genoa", Phrase: "port of genoa"},
	{Ref: "/fac/sg/port-of-singapore", Phrase: "port of singapore"},
	{Ref: "/fac/id/port-of-belawan", Phrase: "port of belawan"},
	{Ref: "/fac/id/port-of-tanjung-priok", Phrase: "port of tanjung priok"},
	{Ref: "/fac/vn/port-of-ho-chi-minh-city", Phrase: "port of ho chi minh city"},
	{Ref: "/fac/jp/port-of-yokohama", Phrase: "port of yokohama"},
	{Ref: "/fac/ae/jebel-ali-port", Phrase: "jebel ali port"},
	{Ref: "/fac/ae/jebel-ali-port", Phrase: "jebel ali", RequiresRouteContext: true},
	{Ref: "/fac/om/sohar-port", Phrase: "port of sohar"},
	{Ref: "/fac/om/sohar-port", Phrase: "sohar port"},
	{Ref: "/fac/om/sohar-port", Phrase: "sohar", RequiresRouteContext: true},
	{Ref: "/fac/eg/suez-terminal", Phrase: "suez canal"},
	{Ref: "/fac/eg/suez-terminal", Phrase: "suez", RequiresRouteContext: true},
	{Ref: "/fac/sea/strait-of-hormuz-terminal", Phrase: "strait of hormuz"},
	{Ref: "/fac/sea/strait-of-hormuz-terminal", Phrase: "hormuz", RequiresRouteContext: true},
	{Ref: "/fac/int/strait-of-malacca", Phrase: "strait of malacca"},
	{Ref: "/fac/int/strait-of-malacca", Phrase: "malacca strait"},
	{Ref: "/fac/int/strait-of-malacca", Phrase: "malacca", RequiresRouteContext: true},
	{Ref: "/fac/sea/bab-el-mandeb", Phrase: "bab el mandeb"},
	{Ref: "/fac/sea/bab-el-mandeb", Phrase: "bab al mandab"},
	{Ref: "/fac/sea/bab-el-mandeb", Phrase: "bab-el-mandeb"},
	{Ref: "/fac/sea/bab-el-mandeb", Phrase: "bab-el-mandab"},
}

func inferTransportViaRefsFromEvidence(explicitRefs []string, texts ...string) []string {
	out := filterFacilityRefs(explicitRefs)
	if len(texts) == 0 {
		return models.NormalizeTransportViaRefs(out)
	}

	bestByRef := map[string]routeHintMatch{}
	for textIndex, raw := range texts {
		normalized := normalizeRouteHintText(raw)
		if normalized == "" {
			continue
		}
		routeContext := hasRouteContext(normalized)
		for _, ref := range extractCanonicalFacilityRefs(raw) {
			recordRouteHintMatch(bestByRef, routeHintMatch{
				Ref:       ref,
				TextIndex: textIndex,
				Position:  canonicalFacilityRefPosition(raw, ref),
				Strength:  len(ref),
			})
		}
		for _, alias := range knownRouteHintAliases {
			phrase := " " + alias.Phrase + " "
			idx := strings.Index(normalized, phrase)
			if idx < 0 {
				continue
			}
			if alias.RequiresRouteContext && !routeContext {
				continue
			}
			recordRouteHintMatch(bestByRef, routeHintMatch{
				Ref:       alias.Ref,
				TextIndex: textIndex,
				Position:  idx,
				Strength:  len(alias.Phrase),
			})
		}
	}

	matches := make([]routeHintMatch, 0, len(bestByRef))
	for _, match := range bestByRef {
		matches = append(matches, match)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].TextIndex != matches[j].TextIndex {
			return matches[i].TextIndex < matches[j].TextIndex
		}
		if matches[i].Position != matches[j].Position {
			return matches[i].Position < matches[j].Position
		}
		if matches[i].Strength != matches[j].Strength {
			return matches[i].Strength > matches[j].Strength
		}
		return matches[i].Ref < matches[j].Ref
	})
	for _, match := range matches {
		out = append(out, match.Ref)
	}
	return mergeOrderedRouteEvidenceRefs(filterFacilityRefs(explicitRefs), matches)
}

func InferTransportViaRefsFromEvidence(explicitRefs []string, texts ...string) []string {
	return inferTransportViaRefsFromEvidence(explicitRefs, texts...)
}

func collectSegmentRouteEvidence(chain agents.SupplyChain, supplierRef, productRef, customerRef string) ([]string, []string) {
	explicitRefs := filterFacilityRefs(chain.TransportVia)
	texts := make([]string, 0, 1+len(chain.RouteObservations)*4)
	if reason := strings.TrimSpace(chain.Reason); reason != "" {
		texts = append(texts, reason)
	}
	for _, obs := range chain.RouteObservations {
		if !routeObservationMatchesSegment(obs, supplierRef, productRef, customerRef) {
			continue
		}
		explicitRefs = append(explicitRefs, filterFacilityRefs(obs.TransportVia)...)
		texts = append(texts, collectLooseStringEvidence(obs.Qualifiers)...)
	}
	return models.NormalizeTransportViaRefs(explicitRefs), compactNonEmptyStrings(texts)
}

func normalizeRouteObservationViaRefs(obs agents.RouteObservation) []string {
	texts := collectLooseStringEvidence(obs.Qualifiers)
	return inferTransportViaRefsFromEvidence(normalizeSupplyChainPathList(obs.TransportVia), texts...)
}

func routeObservationMatchesSegment(obs agents.RouteObservation, supplierRef, productRef, customerRef string) bool {
	matches := func(observed, expected string) bool {
		observed = normalizeSupplyChainCanonicalPath(observed)
		expected = normalizeSupplyChainCanonicalPath(expected)
		return observed == "" || expected == "" || observed == expected
	}
	return matches(obs.SupplierRef, supplierRef) &&
		matches(obs.ProductRef, productRef) &&
		matches(obs.CustomerRef, customerRef)
}

func collectLooseStringEvidence(raw map[string]interface{}) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	var visit func(value interface{})
	visit = func(value interface{}) {
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				out = append(out, trimmed)
			}
		case []string:
			for _, item := range typed {
				visit(item)
			}
		case []interface{}:
			for _, item := range typed {
				visit(item)
			}
		case map[string]interface{}:
			for _, item := range typed {
				visit(item)
			}
		}
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		visit(raw[key])
	}
	return compactNonEmptyStrings(out)
}

func mergeOrderedRouteEvidenceRefs(explicitRefs []string, matches []routeHintMatch) []string {
	explicitRefs = models.NormalizeTransportViaRefs(explicitRefs)
	if len(matches) == 0 {
		return explicitRefs
	}

	matchOrder := make(map[string]int, len(matches))
	orderedMatches := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, ok := matchOrder[match.Ref]; ok {
			continue
		}
		matchOrder[match.Ref] = len(orderedMatches)
		orderedMatches = append(orderedMatches, match.Ref)
	}

	out := append([]string(nil), explicitRefs...)
	sort.SliceStable(out, func(i, j int) bool {
		leftIdx, leftOK := matchOrder[out[i]]
		rightIdx, rightOK := matchOrder[out[j]]
		if leftOK && rightOK {
			return leftIdx < rightIdx
		}
		return false
	})

	seen := make(map[string]struct{}, len(out)+len(orderedMatches))
	merged := make([]string, 0, len(out)+len(orderedMatches))
	for _, ref := range out {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		merged = append(merged, ref)
	}
	for _, ref := range orderedMatches {
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		merged = append(merged, ref)
	}
	return models.NormalizeTransportViaRefs(merged)
}

func CollectLooseStringEvidence(raw map[string]interface{}) []string {
	return collectLooseStringEvidence(raw)
}

func filterFacilityRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "/fac/") {
			continue
		}
		out = append(out, ref)
	}
	return models.NormalizeTransportViaRefs(out)
}

func compactNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func normalizeRouteHintText(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\n", " ",
		"\t", " ",
		",", " ",
		".", " ",
		";", " ",
		":", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		"+", " ",
		"-", " ",
		"_", " ",
		"/", " ",
	)
	return " " + strings.Join(strings.Fields(replacer.Replace(raw)), " ") + " "
}

func hasRouteContext(normalized string) bool {
	for _, token := range []string{
		" via ",
		" sea ",
		" vessel ",
		" maritime ",
		" shipping ",
		" route ",
		" corridor ",
		" canal ",
		" strait ",
		" port ",
		" terminal ",
		" gulf ",
		" ocean ",
	} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func extractCanonicalFacilityRefs(raw string) []string {
	matches := canonicalFacilityPathPattern.FindAllString(strings.ToLower(raw), -1)
	if len(matches) == 0 {
		return nil
	}
	return models.NormalizeTransportViaRefs(matches)
}

func canonicalFacilityRefPosition(raw, ref string) int {
	idx := strings.Index(strings.ToLower(raw), strings.ToLower(ref))
	if idx >= 0 {
		return idx
	}
	return 0
}

func recordRouteHintMatch(dst map[string]routeHintMatch, candidate routeHintMatch) {
	if existing, ok := dst[candidate.Ref]; ok {
		if candidate.TextIndex > existing.TextIndex {
			return
		}
		if candidate.TextIndex == existing.TextIndex {
			if candidate.Position > existing.Position {
				return
			}
			if candidate.Position == existing.Position && candidate.Strength <= existing.Strength {
				return
			}
		}
	}
	dst[candidate.Ref] = candidate
}
