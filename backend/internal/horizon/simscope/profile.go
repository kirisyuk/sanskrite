package simscope

import (
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type Profile struct {
	Query               string
	PrimaryGeo          string
	DomainProductPaths  []string
	DomainProductTokens []string
	FocusRefs           []string
	ScopeCountries      []string
}

func BuildProfile(question string, lens *horizonmodels.Lens, refs horizonmodels.SimulationBaselineRefs, recognition map[string]any) Profile {
	profile := Profile{
		Query: strings.ToLower(strings.TrimSpace(question)),
	}
	productPaths := map[string]struct{}{}
	productTokens := map[string]struct{}{}
	domainSeedPaths := map[string]struct{}{}
	domainSeedTokens := map[string]struct{}{}
	focusRefs := map[string]float64{}
	countries := map[string]struct{}{}

	addCountry := func(path string) {
		if code := countryCodeFromPath(path); code != "" {
			countries[code] = struct{}{}
			if profile.PrimaryGeo == "" {
				profile.PrimaryGeo = code
			}
		}
	}
	addProductPath := func(path string, seed bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		tokens := productHintTokens(path)
		for _, token := range tokens {
			productTokens[token] = struct{}{}
			if seed {
				domainSeedTokens[token] = struct{}{}
			}
		}
		if strings.HasPrefix(path, "/product/") {
			productPaths[path] = struct{}{}
			if seed {
				domainSeedPaths[strings.ToLower(path)] = struct{}{}
			}
			return
		}
		if strings.HasPrefix(path, "/domain/") && strings.Contains(path, "/product/") {
			productPaths[path] = struct{}{}
			if seed {
				domainSeedPaths[strings.ToLower(path)] = struct{}{}
			}
		}
	}
	addFocusRef := func(path string, score float64) {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/") {
			return
		}
		if score > focusRefs[path] {
			focusRefs[path] = score
		}
		addCountry(path)
	}

	if lens != nil {
		for _, dq := range lens.DomainQueries {
			addProductPath(strings.TrimSpace(dq.CanonicalPath), true)
			if code := countryCodeFromPath(strings.TrimSpace(dq.CanonicalPath)); code != "" && profile.PrimaryGeo == "" {
				profile.PrimaryGeo = code
			}
		}
		for _, path := range compactStringsPreserveOrder(lens.Scope) {
			switch objectKindFromPath(path) {
			case "domain", "market":
				addFocusRef(path, 2.2)
				addProductPath(path, true)
			}
		}
	}
	seedMatcher := func(path string) bool {
		path = strings.TrimSpace(path)
		if path == "" {
			return false
		}
		if len(domainSeedPaths) == 0 && len(domainSeedTokens) == 0 {
			return true
		}
		if _, ok := domainSeedPaths[strings.ToLower(path)]; ok {
			return true
		}
		return tokenMatchInSet(path, domainSeedTokens)
	}
	scopePaths := compactStringsPreserveOrder(refs.ScopePaths)
	for _, path := range scopePaths {
		switch objectKindFromPath(path) {
		case "domain", "market":
			addProductPath(path, true)
		}
	}
	recognitionObjects := mapSlice(recognition["objectCandidates"])
	for _, raw := range recognitionObjects {
		path := strings.TrimSpace(stringValue(raw, "path"))
		kind := strings.TrimSpace(stringValue(raw, "kind"))
		if kind == "domain" || kind == "market" {
			addProductPath(path, true)
		}
	}
	for _, path := range scopePaths {
		switch objectKindFromPath(path) {
		case "domain", "market", "product":
			if objectKindFromPath(path) == "product" && !seedMatcher(path) && !pathMentionedInQuery(path, profile.Query) {
				continue
			}
			addProductPath(path, false)
		}
	}
	for _, raw := range recognitionObjects {
		path := strings.TrimSpace(stringValue(raw, "path"))
		kind := strings.TrimSpace(stringValue(raw, "kind"))
		score := floatValue(raw, "score", 0)
		sources := stringSliceAny(raw["sources"])
		if code := countryCodeFromPath(path); code != "" && profile.PrimaryGeo == "" {
			profile.PrimaryGeo = code
		}
		switch kind {
		case "domain", "market", "product":
			if kind == "product" && !seedMatcher(path) && !pathMentionedInQuery(path, profile.Query) {
				break
			}
			addProductPath(path, kind != "product")
		}
		if recognitionObjectStrong(path, kind, score, sources, productTokens, profile.Query, profile.PrimaryGeo) {
			addFocusRef(path, score)
		}
	}
	if profile.PrimaryGeo != "" {
		countries[profile.PrimaryGeo] = struct{}{}
	}
	profile.DomainProductPaths = sortedKeys(productPaths)
	profile.DomainProductTokens = sortedKeys(productTokens)
	profile.FocusRefs = sortedWeightedKeys(focusRefs)
	profile.ScopeCountries = sortedKeys(countries)
	return profile
}

func FilterRecognitionObjects(objects []map[string]any, profile Profile, limit int) []map[string]any {
	if len(objects) == 0 {
		return nil
	}
	type scored struct {
		score  float64
		object map[string]any
	}
	items := make([]scored, 0, len(objects))
	for _, object := range objects {
		if len(object) == 0 {
			continue
		}
		path := strings.TrimSpace(stringValue(object, "path"))
		kind := strings.TrimSpace(stringValue(object, "kind"))
		base := floatValue(object, "score", 0)
		sources := stringSliceAny(object["sources"])
		relevance := PathRelevanceScore(path, kind, profile)
		trusted := hasTrustedRecognitionSource(sources)
		keep := false
		switch kind {
		case "domain", "market":
			keep = relevance >= 1.4 || base >= 0.6
		case "gov", "fac", "loc":
			keep = relevance >= 0.9 || (trusted && base >= 0.9)
		case "product":
			keep = relevance >= 1.0 || productPathMatches(path, profile)
		case "org":
			keep = relevance >= 1.1 || (trusted && base >= 1.1)
		default:
			keep = relevance >= 1.0
		}
		if !keep {
			continue
		}
		items = append(items, scored{
			score:  base + relevance,
			object: object,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return strings.TrimSpace(stringValue(items[i].object, "path")) < strings.TrimSpace(stringValue(items[j].object, "path"))
		}
		return items[i].score > items[j].score
	})
	if limit <= 0 {
		limit = len(items)
	}
	out := make([]map[string]any, 0, min(limit, len(items)))
	for _, item := range items[:min(limit, len(items))] {
		out = append(out, item.object)
	}
	return out
}

func FilterArtifacts(artifacts []*horizonmodels.MaterializedArtifact, profile Profile) []*horizonmodels.MaterializedArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if ArtifactMatchesProfile(artifact, profile) {
			out = append(out, artifact)
		}
	}
	return out
}

func ArtifactMatchesProfile(artifact *horizonmodels.MaterializedArtifact, profile Profile) bool {
	if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return false
	}
	productMatch := productPathMatches(strings.TrimSpace(artifact.ProductAxis), profile)
	score := ArtifactRelevanceScore(artifact, profile)
	switch {
	case len(profile.DomainProductTokens) > 0 && productMatch:
		if profile.PrimaryGeo != "" && !artifactTouchesPrimaryGeo(artifact, profile.PrimaryGeo) && artifactStrongRefHits(artifact, profile) == 0 {
			return false
		}
		return score >= 2.4
	case len(profile.DomainProductTokens) > 0:
		return false
	default:
		return score >= 1.5
	}
}

func ArtifactRelevanceScore(artifact *horizonmodels.MaterializedArtifact, profile Profile) float64 {
	if artifact == nil {
		return 0
	}
	score := 0.0
	productAxis := strings.TrimSpace(artifact.ProductAxis)
	if productPathMatches(productAxis, profile) {
		score += 3.0
	}
	if textMatchesProductTokens(strings.Join([]string{
		strings.TrimSpace(artifact.CanonicalKey),
		strings.TrimSpace(artifact.DisplayName),
		productAxis,
	}, " | "), profile) {
		score += 0.7
	}
	refs := compactStringsPreserveOrder(append([]string{
		strings.TrimSpace(artifact.AnchorSupplier),
		strings.TrimSpace(artifact.AnchorCustomer),
	}, artifact.NodeRefs...))
	scopeCountries := makeSet(profile.ScopeCountries)
	focusSet := makeSet(profile.FocusRefs)
	for _, ref := range refs {
		if _, ok := focusSet[ref]; ok {
			score += 1.4
		}
		if code := countryCodeFromPath(ref); code != "" {
			if code == profile.PrimaryGeo {
				score += 0.45
			}
			if _, ok := scopeCountries[code]; ok {
				score += 0.2
			}
		}
	}
	if len(profile.DomainProductTokens) > 0 && !productPathMatches(productAxis, profile) && artifactStrongRefHits(artifact, profile) == 0 {
		score *= 0.25
	}
	return score
}

func PathRelevanceScore(path, kind string, profile Profile) float64 {
	path = strings.TrimSpace(path)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if path == "" || !strings.HasPrefix(path, "/") {
		return 0
	}
	score := 0.0
	focusSet := makeSet(profile.FocusRefs)
	scopeCountries := makeSet(profile.ScopeCountries)
	if _, ok := focusSet[path]; ok {
		score += 2.2
	}
	if kind == "" {
		kind = objectKindFromPath(path)
	}
	switch kind {
	case "domain", "market":
		score += 1.5
	case "product":
		score += 0.8
	}
	if code := countryCodeFromPath(path); code != "" {
		if code == profile.PrimaryGeo {
			score += 0.8
		}
		if _, ok := scopeCountries[code]; ok {
			score += 0.35
		}
	}
	if productPathMatches(path, profile) || textMatchesProductTokens(path, profile) {
		score += 1.4
	}
	if pathMentionedInQuery(path, profile.Query) {
		score += 0.8
	}
	return score
}

func artifactStrongRefHits(artifact *horizonmodels.MaterializedArtifact, profile Profile) int {
	if artifact == nil {
		return 0
	}
	focusSet := makeSet(profile.FocusRefs)
	hits := 0
	for _, ref := range artifactOperationalEndpointRefs(artifact) {
		if _, ok := focusSet[ref]; ok {
			hits++
		}
	}
	return hits
}

func artifactTouchesPrimaryGeo(artifact *horizonmodels.MaterializedArtifact, primaryGeo string) bool {
	primaryGeo = strings.ToLower(strings.TrimSpace(primaryGeo))
	if artifact == nil || primaryGeo == "" {
		return false
	}
	for _, ref := range artifactOperationalEndpointRefs(artifact) {
		if countryCodeFromPath(ref) == primaryGeo {
			return true
		}
	}
	return false
}

func artifactOperationalEndpointRefs(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	refs := compactStringsPreserveOrder([]string{
		strings.TrimSpace(artifact.AnchorSupplier),
		strings.TrimSpace(artifact.AnchorCustomer),
	})
	if len(refs) > 0 {
		return refs
	}
	legs, err := horizonmodels.GetSupplyChainPathLegs(artifact)
	if err != nil {
		return nil
	}
	refs = make([]string, 0, 4)
	for _, leg := range legs {
		refs = append(refs,
			strings.TrimSpace(leg.SupplierRef),
			strings.TrimSpace(leg.CustomerRef),
		)
	}
	return compactStringsPreserveOrder(refs)
}

func recognitionObjectStrong(path, kind string, score float64, sources []string, productTokens map[string]struct{}, query, primaryGeo string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "domain", "market":
		return true
	case "gov", "fac", "loc":
		return score >= 0.7 && (countryCodeFromPath(path) == primaryGeo || hasTrustedRecognitionSource(sources) || tokenMatchInSet(path, productTokens))
	case "product":
		return score >= 0.65
	case "org":
		return score >= 1.1 && (hasTrustedRecognitionSource(sources) || pathMentionedInQuery(path, query) || tokenMatchInSet(path, productTokens))
	default:
		return false
	}
}

func hasTrustedRecognitionSource(sources []string) bool {
	for _, source := range compactStringsPreserveOrder(sources) {
		switch strings.TrimSpace(source) {
		case "signal.actor", "signal.seed", "signal.location", "lens.scope", "lens.viewConfig", "lens.domainQuery":
			return true
		}
	}
	return false
}

func productPathMatches(path string, profile Profile) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if _, ok := makeSet(profile.DomainProductPaths)[path]; ok {
		return true
	}
	return tokenMatchInSet(path, makeSet(profile.DomainProductTokens))
}

func textMatchesProductTokens(text string, profile Profile) bool {
	return tokenMatchInSet(text, makeSet(profile.DomainProductTokens))
}

func tokenMatchInSet(text string, tokens map[string]struct{}) bool {
	text = normalizeTokenText(text)
	if text == "" || len(tokens) == 0 {
		return false
	}
	for token := range tokens {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func productHintTokens(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	out := map[string]struct{}{}
	add := func(token string) {
		token = normalizeTokenText(token)
		if token == "" {
			return
		}
		if len(token) <= 2 {
			return
		}
		out[token] = struct{}{}
		if strings.HasSuffix(token, "s") && len(token) > 4 {
			out[strings.TrimSuffix(token, "s")] = struct{}{}
		} else if len(token) > 3 {
			out[token+"s"] = struct{}{}
		}
	}
	addToyFamilyTokens := func() {
		for _, extra := range []string{
			"toy", "toys", "game", "games", "board game", "board games",
			"action figure", "action figures", "figure", "figures",
			"doll", "dolls", "construction toy", "construction toys",
			"educational toy", "educational toys", "plastic toy", "plastic toys",
			"toys and games", "children toy", "children toys", "child toy", "child toys",
		} {
			add(extra)
		}
	}
	if strings.HasPrefix(path, "/domain/") {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "product" {
				add(parts[i+1])
				if strings.Contains(strings.TrimSpace(parts[i+1]), "toy") {
					addToyFamilyTokens()
				}
			}
		}
	}
	leaf := lastPathSegment(path)
	add(leaf)
	if strings.Contains(strings.TrimSpace(leaf), "toy") {
		addToyFamilyTokens()
	}
	return sortedKeys(out)
}

func pathMentionedInQuery(path, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return false
	}
	leaf := normalizeTokenText(lastPathSegment(path))
	return leaf != "" && strings.Contains(query, leaf)
}

func lastPathSegment(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func objectKindFromPath(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "org"
	case strings.HasPrefix(path, "/fac/"):
		return "fac"
	case strings.HasPrefix(path, "/market/"):
		return "market"
	case strings.HasPrefix(path, "/product/"):
		return "product"
	case strings.HasPrefix(path, "/gov/"):
		return "gov"
	case strings.HasPrefix(path, "/loc/"):
		return "loc"
	case strings.HasPrefix(path, "/domain/"):
		return "domain"
	default:
		return ""
	}
}

func countryCodeFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "loc", "fac", "org", "market", "gov":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	case "domain":
		for i := 1; i < len(parts)-1; i++ {
			if parts[i] == "geo" && len(strings.TrimSpace(parts[i+1])) == 2 {
				return strings.ToLower(strings.TrimSpace(parts[i+1]))
			}
		}
	}
	return ""
}

func normalizeTokenText(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.NewReplacer("-", " ", "_", " ", "/", " ").Replace(raw)
	return strings.Join(strings.Fields(raw), " ")
}

func mapSlice(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(map[string]any); ok {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	switch typed := raw[key].(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func floatValue(raw map[string]any, key string, fallback float64) float64 {
	if raw == nil {
		return fallback
	}
	switch typed := raw[key].(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return fallback
	}
}

func stringSliceAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return compactStringsPreserveOrder(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, strings.TrimSpace(value))
			}
		}
		return compactStringsPreserveOrder(out)
	default:
		return nil
	}
}

func compactStringsPreserveOrder(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sortedKeys[T any](input map[string]T) []string {
	out := make([]string, 0, len(input))
	for key := range input {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedWeightedKeys(input map[string]float64) []string {
	type item struct {
		key   string
		score float64
	}
	items := make([]item, 0, len(input))
	for key, score := range input {
		items = append(items, item{key: key, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].key < items[j].key
		}
		return items[i].score > items[j].score
	})
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.key)
	}
	return out
}

func makeSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
