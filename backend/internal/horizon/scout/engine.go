package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto/sha1"
	"encoding/hex"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	redis "github.com/redis/go-redis/v9"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func isLegacyOrgPath(cp string) bool {
	cp = strings.TrimSpace(cp)
	if !strings.HasPrefix(cp, "/org/") {
		return false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// Legacy: /org/{slug} (2 segments total)
	return len(parts) == 2 && parts[0] == "org" && strings.TrimSpace(parts[1]) != ""
}

func isFlatProductPath(cp string) (string, bool) {
	cp = strings.TrimSpace(cp)
	if !strings.HasPrefix(cp, "/product/") {
		return "", false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 2 && parts[0] == "product" && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[1]), true
	}
	return "", false
}

func isUpperAcronym(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 || len(s) > 8 {
		return false
	}
	hasLetter := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			// ok
		default:
			return false
		}
	}
	return hasLetter
}

func shouldRequestProductCanonicalization(frag *models.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	slug, flat := isFlatProductPath(frag.CanonicalPath)
	if !flat {
		return false
	}
	label := strings.TrimSpace(frag.Labels["en"])
	short := strings.TrimSpace(frag.Identifiers["shortName"])
	if isUpperAcronym(label) || isUpperAcronym(short) {
		return true
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return false
	}
	// Any flat /product/{slug} should be canonicalized into taxonomy.
	return true
}

// mergeHomeLocation fills dst's empty fields from src (in-place). Used when merging extracted geo into existing fragment.
func mergeHomeLocation(dst, src *models.Location) {
	if dst == nil || src == nil {
		return
	}
	if strings.TrimSpace(dst.Name) == "" && strings.TrimSpace(src.Name) != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if (dst.ISO3166_1 == nil || strings.TrimSpace(*dst.ISO3166_1) == "") &&
		src.ISO3166_1 != nil && strings.TrimSpace(*src.ISO3166_1) != "" {
		v := strings.TrimSpace(*src.ISO3166_1)
		dst.ISO3166_1 = &v
	}
	if (dst.ISO3166_2 == nil || strings.TrimSpace(*dst.ISO3166_2) == "") &&
		src.ISO3166_2 != nil && strings.TrimSpace(*src.ISO3166_2) != "" {
		v := strings.TrimSpace(*src.ISO3166_2)
		dst.ISO3166_2 = &v
	}
	if (dst.ISO6709 == nil || strings.TrimSpace(*dst.ISO6709) == "") &&
		src.ISO6709 != nil && strings.TrimSpace(*src.ISO6709) != "" {
		v := strings.TrimSpace(*src.ISO6709)
		dst.ISO6709 = &v
	}
}

func appendMissingInfoUnique(list []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return list
	}
	for _, existing := range list {
		if strings.TrimSpace(existing) == item {
			return list
		}
	}
	return append(list, item)
}

const (
	// RuntimeContractContextEnvelope:
	// bounded context budget for extraction/research/validation prompts.
	maxAggregatedContentChars   = 48000
	maxSourceContentBlockChars  = 9000
	maxCriticSourceContentChars = 18000
	maxResearchRawExcerptChars  = 7000
	// RuntimeContractMissingInfoRelevance:
	// upper bound for critic-generated missingInfo questions per entity.
	defaultMissingInfoLimit = 12
	personMissingInfoLimit  = 8
	// RuntimeContractMetricsRefresh:
	// recompute metrics only when missing, stale, or low-confidence.
	metricsMaxAge              = 30 * 24 * time.Hour
	metricsMinConfidence       = 0.60
	metricsFutureSkewTolerance = 24 * time.Hour
)

func truncateWithHeadTail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 32 {
		return text[:limit]
	}
	head := (limit * 2) / 3
	tail := limit - head - len("\n...[truncated]...\n")
	if tail < 0 {
		tail = 0
	}
	if head > len(text) {
		head = len(text)
	}
	if tail > len(text)-head {
		tail = len(text) - head
	}
	return text[:head] + "\n...[truncated]...\n" + text[len(text)-tail:]
}

func appendAggregatedContentBlock(current string, block string, maxChars int) string {
	block = strings.TrimSpace(block)
	if block == "" {
		return strings.TrimSpace(current)
	}
	if strings.Contains(current, block) {
		return strings.TrimSpace(current)
	}
	if strings.TrimSpace(current) == "" {
		return truncateWithHeadTail(block, maxChars)
	}
	merged := strings.TrimSpace(current) + "\n\n" + block
	return truncateWithHeadTail(merged, maxChars)
}

func buildSourceContentBlock(result *ExecuteDataSourceResult) string {
	if result == nil {
		return ""
	}
	content := truncateWithHeadTail(result.Content, maxSourceContentBlockChars)
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("SOURCE CONTENT")
	if s := strings.TrimSpace(result.SourceType); s != "" {
		b.WriteString(" [" + s + "]")
	}
	if u := strings.TrimSpace(result.SourceURL); u != "" {
		b.WriteString(": " + u)
	}
	b.WriteString("\n")
	b.WriteString(content)
	return b.String()
}

func buildResearchContextBlock(rawContent string, relations []agents.DiscoveredRelation, chains []agents.SupplyChain, evidence *ai.GroundingEvidence) string {
	var b strings.Builder
	b.WriteString("RESEARCH ENRICHMENT CONTEXT:\n")

	if len(relations) > 0 {
		b.WriteString("Relations discovered:\n")
		for i, rel := range relations {
			if i >= 20 {
				b.WriteString("- ...\n")
				break
			}
			relType := strings.TrimSpace(rel.RelationType)
			target := strings.TrimSpace(rel.TargetCanonicalPath)
			if target == "" {
				target = strings.TrimSpace(rel.TargetName)
			}
			if relType == "" && target == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("- %s -> %s\n", relType, target))
		}
	}

	if len(chains) > 0 {
		b.WriteString("Supply chains discovered:\n")
		for i, chain := range chains {
			if i >= 10 {
				b.WriteString("- ...\n")
				break
			}
			nodes := make([]string, 0, len(chain.Nodes))
			for _, node := range chain.Nodes {
				path := strings.TrimSpace(node.CanonicalPath)
				if path == "" {
					path = strings.TrimSpace(node.Name)
				}
				if path != "" {
					nodes = append(nodes, path)
				}
			}
			if len(nodes) == 0 {
				continue
			}
			b.WriteString("- " + strings.Join(nodes, " -> ") + "\n")
		}
	}

	if evidence != nil && len(evidence.WebSources) > 0 {
		b.WriteString("Grounded web sources:\n")
		for i, src := range evidence.WebSources {
			if i >= 8 {
				b.WriteString("- ...\n")
				break
			}
			url := strings.TrimSpace(src.URL)
			title := strings.TrimSpace(src.Title)
			switch {
			case title != "" && url != "":
				b.WriteString(fmt.Sprintf("- %s (%s)\n", title, url))
			case url != "":
				b.WriteString("- " + url + "\n")
			case title != "":
				b.WriteString("- " + title + "\n")
			}
		}
	}

	raw := strings.TrimSpace(rawContent)
	if raw != "" {
		b.WriteString("Research raw excerpt:\n")
		b.WriteString(truncateWithHeadTail(raw, maxResearchRawExcerptChars))
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func criticSourceContent(content string) string {
	return truncateWithHeadTail(content, maxCriticSourceContentChars)
}

func missingInfoLimitForFragment(fragment *models.HorizonFragment) int {
	if fragment == nil {
		return defaultMissingInfoLimit
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	if fragment.Type == models.FragmentTypePerson || strings.HasPrefix(cp, "/person/") {
		return personMissingInfoLimit
	}
	return defaultMissingInfoLimit
}

func metricObservedAt(mv models.MetricValue) time.Time {
	if !mv.ObservedAt.IsZero() {
		return mv.ObservedAt
	}
	if mv.Provenance.ObservedAt != nil && !mv.Provenance.ObservedAt.IsZero() {
		return *mv.Provenance.ObservedAt
	}
	return time.Time{}
}

// RuntimeContractMetricsRefresh:
// evaluate whether metric recomputation is required for required metric paths.
func evaluateMetricsRefreshContractReasons(fragment *models.HorizonFragment, requiredPaths []string, now time.Time) []string {
	if fragment == nil {
		return []string{"fragment_nil"}
	}
	if fragment.State == nil {
		return []string{"state_missing"}
	}
	reasons := make([]string, 0)
	for _, path := range requiredPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		mv, ok := fragment.State[path]
		if !ok {
			reasons = append(reasons, "missing:"+path)
			continue
		}
		if mv.Confidence < metricsMinConfidence {
			reasons = append(reasons, "low_confidence:"+path)
			continue
		}
		observedAt := metricObservedAt(mv)
		if observedAt.IsZero() {
			reasons = append(reasons, "stale:"+path)
			continue
		}
		age := now.Sub(observedAt)
		if age > metricsMaxAge || observedAt.After(now.Add(metricsFutureSkewTolerance)) {
			reasons = append(reasons, "stale:"+path)
			continue
		}
	}
	return reasons
}

// recordDeterministicRedirect records a redirect that becomes immediately stable.
// Use for authoritative pipeline decisions (dedup, org path upgrade, scope rewrite)
// where the system made a definitive canonical path change.
func (e *ScoutEngine) recordDeterministicRedirect(ctx context.Context, fromPath string, toPath string, reason string) {
	if e == nil || e.canonicalPathResolver == nil {
		return
	}
	fromPath = strings.TrimSpace(fromPath)
	toPath = strings.TrimSpace(toPath)
	if fromPath == "" || toPath == "" || fromPath == toPath {
		return
	}
	fragType := InferFragmentTypeFromCanonicalPath(toPath)
	if fragType == "" {
		fragType = InferFragmentTypeFromCanonicalPath(fromPath)
	}
	_, _, _ = e.canonicalPathResolver.RecordDeterministicRedirect(ctx, fromPath, toPath, fragType, reason)
}

func hasLegalFormSuffix(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	legalForms := []string{"inc", "llc", "jsc", "pjsc", "ltd", "corp", "plc", "gmbh", "sarl", "srl", "ag", "sa", "spa", "as", "ab", "bv", "nv"}
	for _, lf := range legalForms {
		if strings.HasSuffix(slug, "-"+lf) {
			return true
		}
	}
	return false
}

func shouldRequestOrgCanonicalization(frag *models.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	cp := strings.TrimSpace(frag.CanonicalPath)
	if !strings.HasPrefix(cp, "/org/") {
		return false
	}
	// If jurisdiction is unknown (/org/na/...) or path is legacy (/org/{slug}),
	// we should ask missingInfo to resolve country/jurisdiction.
	if strings.HasPrefix(cp, "/org/na/") || isLegacyOrgPath(cp) {
		return true
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return true
	}
	slug := parts[len(parts)-1]
	return !hasLegalFormSuffix(slug)
}

func buildOrgCanonicalizationMissingInfo(fragment *models.HorizonFragment, job *models.ScoutingJob, relations []agents.DiscoveredRelation) string {
	if fragment == nil {
		return ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	label := strings.TrimSpace(fragment.Labels["en"])
	short := strings.TrimSpace(fragment.Identifiers["shortName"])
	legal := strings.TrimSpace(fragment.Identifiers["legalName"])
	desc := strings.TrimSpace(fragment.Description)
	originalQuery := ""
	if job != nil && job.State != nil {
		if v, ok := job.State["originalQuery"].(string); ok {
			originalQuery = strings.TrimSpace(v)
		}
	}
	relContext := relationContextSnippet(relations, 6)

	var sb strings.Builder
	if strings.HasPrefix(cp, "/org/na/") || isLegacyOrgPath(cp) {
		sb.WriteString("Resolve the country of origin / primary jurisdiction (ISO2) for this organization and update its canonicalPath.")
		sb.WriteString(" Current canonicalPath is jurisdiction-unknown (/org/na/...) or legacy (/org/{slug}), which causes cross-country collisions.")
		sb.WriteString(" Target format: /org/{iso2}/{slug}-{legal_form} (use legalName to pick legal_form suffix when known).")
	} else {
		sb.WriteString("Confirm the legal name, jurisdiction (ISO2), and canonical org path for this organization.")
		sb.WriteString(" Ensure the /org/{iso2}/{slug}-{legal_form} suffix is correct (e.g., -llc, -jsc, -ltd).")
	}
	sb.WriteString(" If legal form or country is unclear, resolve it using sources.")

	// Optional hint: if we can infer jurisdiction from existing fragment fields or label, include it.
	hintISO2 := ""
	if v := inferOrgISO2(fragment); v != "" {
		hintISO2 = v
	} else {
		for _, cand := range []string{legal, label, short} {
			if cand == "" {
				continue
			}
			if v := inferISO2FromLabel(cand, fragment); v != "" {
				hintISO2 = v
				break
			}
		}
	}
	sb.WriteString(" Context:")
	if cp != "" {
		sb.WriteString(" canonicalPath=" + cp + ";")
	}
	if label != "" {
		sb.WriteString(" label=" + label + ";")
	}
	if short != "" {
		sb.WriteString(" shortName=" + short + ";")
	}
	if legal != "" {
		sb.WriteString(" legalName=" + legal + ";")
	}
	if desc != "" {
		sb.WriteString(" description=" + trace.Trunc(desc, 160) + ";")
	}
	if originalQuery != "" {
		sb.WriteString(" originalQuery=" + trace.Trunc(originalQuery, 140) + ";")
	}
	if relContext != "" {
		sb.WriteString(" relations=[" + relContext + "];")
	}
	if hintISO2 != "" {
		sb.WriteString(" inferredIso2Hint=" + hintISO2 + ";")
	}
	return sb.String()
}

func buildProductCanonicalizationMissingInfo(fragment *models.HorizonFragment, job *models.ScoutingJob, relations []agents.DiscoveredRelation) string {
	if fragment == nil {
		return ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	label := strings.TrimSpace(fragment.Labels["en"])
	short := strings.TrimSpace(fragment.Identifiers["shortName"])
	desc := strings.TrimSpace(fragment.Description)
	aliasPath := ""
	originalQuery := ""
	if job != nil && job.State != nil {
		if v, ok := job.State["aliasCanonicalPath"].(string); ok {
			aliasPath = strings.TrimSpace(v)
		}
		if v, ok := job.State["originalQuery"].(string); ok {
			originalQuery = strings.TrimSpace(v)
		}
	}
	relContext := relationContextSnippet(relations, 6)

	var sb strings.Builder
	sb.WriteString("Resolve canonical product taxonomy for this product and disambiguate any acronym/short slug.")
	sb.WriteString(" Provide a precise product name and canonical path using /product/{category}/{type}/{stage}/{slug}.")
	sb.WriteString(" For services, use category=service and map service domain/subdomain into {type}/{stage}.")
	sb.WriteString(" If the current path is flat or acronym-like, expand it to the full product name and update fragment.canonicalPath plus labels/identifiers.")
	sb.WriteString(" Examples: rice -> /product/raw/agricultural/extraction/rice; salt -> /product/raw/mineral/extraction/salt; swad (Indian savory snack) -> /product/finished/agricultural/processing/swad; processed/finished goods -> /product/finished/mechanical/assembly/{slug}.")
	sb.WriteString(" Context:")
	if cp != "" {
		sb.WriteString(" canonicalPath=" + cp + ";")
	}
	if aliasPath != "" && aliasPath != cp {
		sb.WriteString(" aliasPath=" + aliasPath + ";")
	}
	if label != "" {
		sb.WriteString(" label=" + label + ";")
	}
	if short != "" {
		sb.WriteString(" shortName=" + short + ";")
	}
	if desc != "" {
		sb.WriteString(" description=" + trace.Trunc(desc, 160) + ";")
	}
	if originalQuery != "" {
		sb.WriteString(" originalQuery=" + trace.Trunc(originalQuery, 140) + ";")
	}
	if relContext != "" {
		sb.WriteString(" relations=[" + relContext + "];")
	}
	return sb.String()
}

func relationContextSnippet(relations []agents.DiscoveredRelation, limit int) string {
	if len(relations) == 0 || limit <= 0 {
		return ""
	}
	out := make([]string, 0, limit)
	for _, rel := range relations {
		if len(out) >= limit {
			break
		}
		target := strings.TrimSpace(rel.TargetCanonicalPath)
		if target == "" {
			target = strings.TrimSpace(rel.TargetName)
		}
		edge := strings.TrimSpace(rel.RelationType)
		if edge == "" {
			edge = "RELATED_TO"
		}
		if target != "" {
			out = append(out, edge+" -> "+target)
		}
	}
	return strings.Join(out, "; ")
}

func relationNeedsOrgCanonicalization(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/org/") {
		return false
	}
	if strings.HasPrefix(path, "/org/na/") || isLegacyOrgPath(path) {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return true
	}
	slug := parts[len(parts)-1]
	return !hasLegalFormSuffix(slug)
}

func relationNeedsGovCanonicalization(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/org/") {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return false
	}
	slug := parts[len(parts)-1]
	return looksLikeGovLabel(slug)
}

func relationNeedsProductCanonicalization(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/product/") {
		return false
	}
	if _, flat := isFlatProductPath(path); flat {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) < 2
}

func relationNeedsGeoDisambiguation(path string) bool {
	path = strings.TrimSpace(strings.ToLower(path))
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "/loc/") {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		// /loc/{country}/{admin1}/{city}
		if len(parts) >= 4 {
			return parts[2] == "na" || parts[3] == "na"
		}
		return true
	}
	if strings.HasPrefix(path, "/market/") {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		// /market/{geo}/{...}; "na" here is ambiguous (North America bucket vs unknown marker in noisy outputs).
		if len(parts) >= 2 {
			return parts[1] == "na"
		}
	}
	return false
}

func relationNeedsJurisdictionDisambiguation(path string) bool {
	path = strings.TrimSpace(strings.ToLower(path))
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "/org/na/") || strings.HasPrefix(path, "/gov/na/") {
		return true
	}
	return false
}

func buildRelationMissingInfo(relations []agents.DiscoveredRelation, limit int) []string {
	if len(relations) == 0 || limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := map[string]struct{}{}

	for _, rel := range relations {
		if len(out) >= limit {
			break
		}
		path := strings.TrimSpace(rel.TargetCanonicalPath)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}

		switch {
		case relationNeedsJurisdictionDisambiguation(path):
			out = append(out, fmt.Sprintf("Resolve jurisdiction for related entity %s and replace unknown country segment with explicit ISO2 in canonicalPath when possible. Keep fail-open behavior, but enrich legal name and jurisdiction evidence.", path))
			seen[path] = struct{}{}
		case relationNeedsGovCanonicalization(path):
			out = append(out, fmt.Sprintf("Confirm if related entity %s is a government body. If so, use /gov/{iso2}/{slug} canonical path and ensure jurisdiction is correct.", path))
			seen[path] = struct{}{}
		case relationNeedsOrgCanonicalization(path):
			out = append(out, fmt.Sprintf("Confirm the legal name, jurisdiction (ISO2), and canonical org path for related entity %s. Ensure the /org/{iso2}/{slug}-{legal_form} suffix is correct.", path))
			seen[path] = struct{}{}
		case relationNeedsProductCanonicalization(path):
			out = append(out, fmt.Sprintf("Resolve canonical product taxonomy for related product %s. Use /product/{category}/{type}/{stage}/{slug}; for services, use category=service and map service domain/subdomain into {type}/{stage}.", path))
			seen[path] = struct{}{}
		case relationNeedsGeoDisambiguation(path):
			out = append(out, fmt.Sprintf("Clarify geo scope for related entity %s and enrich display title. Keep canonicalPath best-effort, but resolve explicit country/region/city names and disambiguate placeholder-like segments.", path))
			seen[path] = struct{}{}
		}
	}

	return out
}

func filterDiscoveredRelationsByDomainModes(in []agents.DiscoveredRelation, domainModes []string) []agents.DiscoveredRelation {
	if len(in) == 0 {
		return nil
	}
	allowlist := EdgeAllowlistByDomainModes(domainModes)
	if allowlist == nil {
		return in
	}
	out := make([]agents.DiscoveredRelation, 0, len(in))
	for _, rel := range in {
		normalized := strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
		if normalized == "" {
			continue
		}
		if !AllowlistContains(allowlist, models.DependencyType(normalized)) {
			continue
		}
		rel.RelationType = normalized
		out = append(out, rel)
	}
	return out
}

func supplyChainsEnabled(targetCanonicalPath string, targetType models.FragmentType, _ []string, _ []string) bool {
	if targetType == models.FragmentTypePerson {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(targetCanonicalPath)), "/person/") {
		return false
	}
	// Supply-chain discovery is part of the core scouting pipeline.
	// Keep this enabled regardless of specialty/domain flags.
	return true
}

type facilityQualifierSemantic string

const (
	facilityQualifierSemanticNone        facilityQualifierSemantic = ""
	facilityQualifierSemanticRoute       facilityQualifierSemantic = "route"
	facilityQualifierSemanticSupply      facilityQualifierSemantic = "supply"
	facilityQualifierSemanticProcurement facilityQualifierSemantic = "procurement"
)

func mapModelFacilitySemantic(sem models.FacilitySemantic) facilityQualifierSemantic {
	switch sem {
	case models.FacilitySemanticRoute:
		return facilityQualifierSemanticRoute
	case models.FacilitySemanticSupply:
		return facilityQualifierSemanticSupply
	case models.FacilitySemanticProcurement:
		return facilityQualifierSemanticProcurement
	default:
		return facilityQualifierSemanticNone
	}
}

func hasTruthyQualifierValue(raw interface{}) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.TrimSpace(v) != ""
	case []string:
		return len(v) > 0
	case []interface{}:
		return len(v) > 0
	case map[string]interface{}:
		return len(v) > 0
	default:
		return false
	}
}

func hasTruthyRouteQualifier(raw interface{}) bool {
	return hasTruthyQualifierValue(raw)
}

func containsRouteHint(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, token := range []string{
		"route",
		"corridor",
		"pipeline",
		"shipping-lane",
		"logistics-lane",
		"transport-lane",
		"trade-lane",
		"canal",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func containsProcurementHint(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, token := range []string{
		"procurement",
		"sourcing",
		"purchasing",
		"tender",
		"vendor",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func detectFacilityQualifierSemanticFromFragment(frag *models.HorizonFragment) facilityQualifierSemantic {
	return mapModelFacilitySemantic(models.DetectFacilitySemanticFromFragment(frag))
}

func isFacilityTarget(targetCanonicalPath string, targetType models.FragmentType, candidates ...*models.HorizonFragment) bool {
	cp := strings.TrimSpace(strings.ToLower(targetCanonicalPath))
	if strings.HasPrefix(cp, "/fac/") || targetType == models.FragmentTypeFacility {
		return true
	}
	for _, c := range candidates {
		if c == nil {
			continue
		}
		fcp := strings.TrimSpace(strings.ToLower(c.CanonicalPath))
		if c.Type == models.FragmentTypeFacility || strings.HasPrefix(fcp, "/fac/") {
			return true
		}
	}
	return false
}

func detectFacilityQualifierSemanticForTarget(targetCanonicalPath string, targetType models.FragmentType, candidates ...*models.HorizonFragment) facilityQualifierSemantic {
	if !isFacilityTarget(targetCanonicalPath, targetType, candidates...) {
		return facilityQualifierSemanticNone
	}
	if sem := mapModelFacilitySemantic(models.DetectFacilitySemantic(targetCanonicalPath, nil, nil, nil)); sem != facilityQualifierSemanticNone {
		return sem
	}
	for _, c := range candidates {
		if sem := detectFacilityQualifierSemanticFromFragment(c); sem != facilityQualifierSemanticNone {
			return sem
		}
	}
	return facilityQualifierSemanticNone
}

func shouldSkipSameFacilityQualifierRecursion(sourceFragment *models.HorizonFragment, targetCanonicalPath string, targetType models.FragmentType, candidates ...*models.HorizonFragment) bool {
	sourceKind := detectFacilityQualifierSemanticFromFragment(sourceFragment)
	if sourceKind == facilityQualifierSemanticNone {
		return false
	}
	targetKind := detectFacilityQualifierSemanticForTarget(targetCanonicalPath, targetType, candidates...)
	return targetKind != facilityQualifierSemanticNone && targetKind == sourceKind
}

func detectFacilityQualifierSemanticFromPath(path string) facilityQualifierSemantic {
	return mapModelFacilitySemantic(models.DetectFacilitySemantic(path, nil, nil, nil))
}

func resolveFacilityQualifierSemantic(sourceFragment *models.HorizonFragment, fallbackTargetPath string) facilityQualifierSemantic {
	if k := detectFacilityQualifierSemanticFromFragment(sourceFragment); k != facilityQualifierSemanticNone {
		return k
	}
	return detectFacilityQualifierSemanticFromPath(fallbackTargetPath)
}

func firstObjectQualifierRaw(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	if frag.Identifiers != nil {
		for _, key := range []string{"objectQualifier", "object_qualifier", "qualifier", "businessQualifier", "business_qualifier"} {
			if v, ok := frag.Identifiers[key]; ok {
				s := strings.TrimSpace(v)
				if s != "" {
					return s
				}
			}
		}
	}
	if frag.Qualifiers != nil {
		for _, key := range []string{"objectQualifier", "object_qualifier", "qualifier", "businessQualifier", "business_qualifier"} {
			if v, ok := frag.Qualifiers[key]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
	}
	return ""
}

func inferObjectQualifierForFragment(frag *models.HorizonFragment, fallbackTargetPath string) models.ObjectQualifier {
	if frag == nil {
		return models.ObjectQualifierNone
	}
	cp := strings.TrimSpace(strings.ToLower(frag.CanonicalPath))
	if cp == "" {
		cp = strings.TrimSpace(strings.ToLower(fallbackTargetPath))
	}
	switch {
	case strings.HasPrefix(cp, "/fac/"):
		switch detectFacilityQualifierSemanticForTarget(cp, models.FragmentTypeFacility, frag) {
		case facilityQualifierSemanticRoute:
			return models.ObjectQualifierRoute
		case facilityQualifierSemanticSupply:
			return models.ObjectQualifierSupply
		case facilityQualifierSemanticProcurement:
			return models.ObjectQualifierProcurement
		}
	case strings.HasPrefix(cp, "/gov/"):
		if containsProcurementHint(cp) {
			return models.ObjectQualifierProcurer
		}
		if strings.Contains(cp, "regulator") || strings.Contains(cp, "authority") || strings.Contains(cp, "commission") {
			return models.ObjectQualifierRegulator
		}
	case strings.HasPrefix(cp, "/org/"):
		if strings.Contains(cp, "holding") {
			return models.ObjectQualifierHolding
		}
		if strings.Contains(cp, "distribution") || strings.Contains(cp, "distributor") {
			return models.ObjectQualifierDistributor
		}
	case strings.HasPrefix(cp, "/market/"):
		if strings.Contains(cp, "regulated") {
			return models.ObjectQualifierRegulated
		}
	case strings.HasPrefix(cp, "/deposit/"):
		if strings.Contains(cp, "greenfield") {
			return models.ObjectQualifierGreenfield
		}
		if strings.Contains(cp, "brownfield") {
			return models.ObjectQualifierBrownfield
		}
	}
	return models.ObjectQualifierNone
}

func normalizeAndSetFragmentObjectQualifier(frag *models.HorizonFragment, fallbackTargetPath string) models.ObjectQualifier {
	if frag == nil {
		return models.ObjectQualifierNone
	}
	raw := firstObjectQualifierRaw(frag)
	q := models.NormalizeObjectQualifier(raw)
	if q == models.ObjectQualifierNone {
		q = inferObjectQualifierForFragment(frag, fallbackTargetPath)
	}
	if !models.IsAllowedObjectQualifierForType(frag.Type, frag.CanonicalPath, q) {
		q = inferObjectQualifierForFragment(frag, fallbackTargetPath)
		if !models.IsAllowedObjectQualifierForType(frag.Type, frag.CanonicalPath, q) {
			q = models.ObjectQualifierNone
		}
	}
	if frag.Identifiers == nil {
		frag.Identifiers = map[string]string{}
	}
	if q == models.ObjectQualifierNone {
		delete(frag.Identifiers, "objectQualifier")
		return q
	}
	frag.Identifiers["objectQualifier"] = string(q)
	return q
}

func parseRelationTargetType(raw string) models.FragmentType {
	raw = strings.TrimSpace(strings.ToUpper(raw))
	if raw == "" {
		return ""
	}
	return models.FragmentType(raw)
}

func targetPathHasAnyPrefix(path string, prefixes ...string) bool {
	path = strings.TrimSpace(strings.ToLower(path))
	if path == "" {
		return false
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func isFacilityQualifierBusinessTargetType(kind facilityQualifierSemantic, targetPath string, targetType models.FragmentType) bool {
	if kind == facilityQualifierSemanticNone {
		return true
	}
	if targetType == models.FragmentTypePerson || targetType == models.FragmentTypeCommunity || targetType == models.FragmentTypeMediaChannel || targetType == models.FragmentTypeEvent {
		return false
	}
	if targetPathHasAnyPrefix(targetPath, "/person/", "/community/", "/media/", "/event/", "/sphere/", "/domain/", "/decision/", "/mechanism/") {
		return false
	}

	// Keep qualifier lanes business-first: geo + flow + actors/products/markets.
	if targetType == models.FragmentTypeOrganization ||
		targetType == models.FragmentTypeFacility ||
		targetType == models.FragmentTypeProduct ||
		targetType == models.FragmentTypeGeoRegion ||
		targetType == models.FragmentTypeMarket ||
		targetType == models.FragmentTypeDeposit ||
		targetType == models.FragmentTypeProgram ||
		targetType == models.FragmentTypeDomain ||
		targetType == "" {
		return true
	}
	if targetPathHasAnyPrefix(targetPath, "/org/", "/fac/", "/product/", "/loc/", "/market/", "/deposit/", "/gov/", "/program/") {
		return true
	}
	return false
}

func isFacilityQualifierAllowedRelationType(kind facilityQualifierSemantic, relType string) bool {
	if kind == facilityQualifierSemanticNone {
		return true
	}
	rt := strings.TrimSpace(relType)
	if rt == "" {
		return true
	}
	switch kind {
	case facilityQualifierSemanticRoute:
		switch rt {
		case string(models.DependencyTypeDependsOn),
			string(models.DependencyTypeSuppliesTo),
			string(models.DependencyTypeLocatedIn),
			string(models.DependencyTypeOperatesIn),
			string(models.DependencyTypeProduces),
			string(models.DependencyTypeConsumes),
			string(models.DependencyTypeTransformsInto),
			string(models.DependencyTypeExtractsFrom),
			string(models.DependencyTypeParticipatesIn),
			string(models.DependencyTypeRegulates),
			string(models.DependencyTypeControlsAccess),
			string(models.DependencyTypeImposesCondition):
			return true
		default:
			return false
		}
	case facilityQualifierSemanticSupply, facilityQualifierSemanticProcurement:
		switch rt {
		case string(models.DependencyTypeDependsOn),
			string(models.DependencyTypeSuppliesTo),
			string(models.DependencyTypeProduces),
			string(models.DependencyTypeConsumes),
			string(models.DependencyTypeTransformsInto),
			string(models.DependencyTypeExtractsFrom),
			string(models.DependencyTypeLocatedIn),
			string(models.DependencyTypeOperatesIn),
			string(models.DependencyTypeParticipatesIn),
			string(models.DependencyTypePartnersWith),
			string(models.DependencyTypeRegulates),
			string(models.DependencyTypeControlsAccess),
			string(models.DependencyTypeImposesCondition):
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func isFacilityQualifierBusinessRelation(kind facilityQualifierSemantic, relType string, targetPath string, targetType models.FragmentType) bool {
	if !isFacilityQualifierBusinessTargetType(kind, targetPath, targetType) {
		return false
	}
	return isFacilityQualifierAllowedRelationType(kind, relType)
}

func filterDiscoveredRelationsByFacilityQualifier(in []agents.DiscoveredRelation, sourceFragment *models.HorizonFragment, fallbackTargetPath string) []agents.DiscoveredRelation {
	if len(in) == 0 {
		return nil
	}
	kind := resolveFacilityQualifierSemantic(sourceFragment, fallbackTargetPath)
	if kind == facilityQualifierSemanticNone {
		return in
	}
	out := make([]agents.DiscoveredRelation, 0, len(in))
	for _, rel := range in {
		targetType := parseRelationTargetType(rel.TargetType)
		if !isFacilityQualifierBusinessRelation(kind, rel.RelationType, rel.TargetCanonicalPath, targetType) {
			continue
		}
		out = append(out, rel)
	}
	return out
}

func containsAnyToken(text string, tokens []string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, token := range tokens {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func filterMissingInfoByFacilityQualifier(in []string, kind facilityQualifierSemantic) []string {
	if len(in) == 0 {
		return nil
	}
	if kind == facilityQualifierSemanticNone {
		return in
	}
	// Human-biography prompts are out-of-scope for fac(route/supply/procurement) business scouting.
	bannedTokens := []string{
		"spouse", "children", "parents", "siblings",
		"relative", "family", "classmates", "friends",
		"biography", "personal life", "birth date",
	}
	out := make([]string, 0, len(in))
	for _, q := range in {
		if containsAnyToken(q, bannedTokens) {
			continue
		}
		out = appendMissingInfoUnique(out, q)
	}
	return out
}

func filterMissingInfoByFragmentRelevance(fragment *models.HorizonFragment, in []string) []string {
	if len(in) == 0 {
		return nil
	}
	if fragment == nil {
		out := make([]string, 0, len(in))
		for _, q := range in {
			out = appendMissingInfoUnique(out, q)
		}
		return out
	}

	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	isPerson := fragment.Type == models.FragmentTypePerson || strings.HasPrefix(cp, "/person/")
	isMarket := fragment.Type == models.FragmentTypeMarket || strings.HasPrefix(cp, "/market/")
	isFacility := fragment.Type == models.FragmentTypeFacility || strings.HasPrefix(cp, "/fac/")
	isDeposit := fragment.Type == models.FragmentTypeDeposit || strings.HasPrefix(cp, "/deposit/")
	isCommunity := fragment.Type == models.FragmentTypeCommunity || strings.HasPrefix(cp, "/community/")

	personOnlyTokens := []string{
		"spouse", "children", "parents", "siblings", "relative", "close associates", "family",
	}
	marketOnlyTokens := []string{
		"total addressable market", "/metric/market/tam_usd", "cagr", "cr4", "hhi", "marketdomain", "marketvertical", "sub-markets", "top players in <market>",
	}
	lifecycleOnlyTokens := []string{
		"lifecycle stage", "operational status", "expected start date", "commissioning", "decommissioning",
	}
	supplyOnlyTokens := []string{
		"supply chain", "upstream", "downstream", "suppliers", "customers", "transport chokepoints",
	}

	out := make([]string, 0, len(in))
	for _, q := range in {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if !isPerson && containsAnyToken(q, personOnlyTokens) {
			continue
		}
		if !isMarket && containsAnyToken(q, marketOnlyTokens) {
			continue
		}
		if !isFacility && !isDeposit && containsAnyToken(q, lifecycleOnlyTokens) {
			continue
		}
		if (isPerson || isCommunity) && containsAnyToken(q, supplyOnlyTokens) {
			continue
		}
		out = appendMissingInfoUnique(out, q)
	}
	return out
}

// RuntimeContractMissingInfoRelevance:
// cap and sanitize critic questions to a bounded, entity-relevant set.
func limitMissingInfoQuestions(in []string, maxItems int) []string {
	if len(in) == 0 {
		return nil
	}
	if maxItems <= 0 {
		maxItems = defaultMissingInfoLimit
	}
	out := make([]string, 0, len(in))
	for _, q := range in {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		q = truncateWithHeadTail(q, 420)
		out = appendMissingInfoUnique(out, q)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func isRouteFacilityFragment(frag *models.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	cp := strings.TrimSpace(strings.ToLower(frag.CanonicalPath))
	if frag.Type != models.FragmentTypeFacility && !strings.HasPrefix(cp, "/fac/") {
		return false
	}
	if frag.Qualifiers != nil {
		for _, key := range []string{"route", "route_path", "routePath", "route_waypoints", "routeWaypoints"} {
			if hasTruthyRouteQualifier(frag.Qualifiers[key]) {
				return true
			}
		}
	}
	if frag.Identifiers != nil {
		for _, key := range []string{"facilityType", "routeType"} {
			if containsRouteHint(frag.Identifiers[key]) {
				return true
			}
		}
	}
	return containsRouteHint(cp)
}

func buildRouteFacilityMissingInfo(targetCanonicalPath string) []string {
	t := strings.TrimSpace(targetCanonicalPath)
	if t == "" {
		t = "this route facility"
	}
	return []string{
		fmt.Sprintf("What regions/countries/cities does %s traverse or connect? Return canonical /loc/... paths and LOCATED_IN/OPERATES_IN links for each major segment.", t),
		fmt.Sprintf("What is the ordered geo route for %s? Provide waypoint sequence with latitude/longitude and route facilities (ports/terminals/pipelines/warehouses). Prefer Google Maps geocoding when coordinates are missing.", t),
		fmt.Sprintf("Which logistics operators, carriers, suppliers, and major customers are business-critical for %s? Return DEPENDS_ON/SUPPLIES_TO edges with facet=logistics|supply and transmission qualifiers.", t),
	}
}

func buildSupplyFacilityMissingInfo(targetCanonicalPath string) []string {
	t := strings.TrimSpace(targetCanonicalPath)
	if t == "" {
		t = "this supply facility"
	}
	return []string{
		fmt.Sprintf("Who are the direct upstream suppliers for %s (companies + input products/materials)? Return DEPENDS_ON/SUPPLIES_TO + PRODUCES/CONSUMES continuity.", t),
		fmt.Sprintf("Who are the direct downstream customers/recipients for %s and what outputs flow to them? Include product/facility paths and confidence.", t),
		fmt.Sprintf("Which logistics operators and transport facilities are critical for %s (ports, terminals, rail hubs, warehouses)? Add transport_via and facet=logistics qualifiers where known.", t),
		fmt.Sprintf("What are the key sourcing/serving regions for %s? Return /loc/... paths and LOCATED_IN/OPERATES_IN links for major flow segments.", t),
	}
}

func buildProcurementFacilityMissingInfo(targetCanonicalPath string) []string {
	t := strings.TrimSpace(targetCanonicalPath)
	if t == "" {
		t = "this procurement facility"
	}
	return []string{
		fmt.Sprintf("Who are the key vendors/suppliers contracted by %s and what categories/products do they cover? Return supplier organization/product links.", t),
		fmt.Sprintf("What are the primary procurement channels and logistics partners for %s (distributors, freight forwarders, carriers, procurement service providers)?", t),
		fmt.Sprintf("Which sourcing regions and alternative suppliers exist for %s to reduce concentration risk? Return /loc/... + supplier paths.", t),
		fmt.Sprintf("What are the critical contract/tender dependencies for %s (single-source risks, framework agreements, procurement bottlenecks) with evidence?", t),
	}
}

func buildFacilityQualifierMissingInfo(targetCanonicalPath string, kind facilityQualifierSemantic) []string {
	switch kind {
	case facilityQualifierSemanticRoute:
		return buildRouteFacilityMissingInfo(targetCanonicalPath)
	case facilityQualifierSemanticSupply:
		return buildSupplyFacilityMissingInfo(targetCanonicalPath)
	case facilityQualifierSemanticProcurement:
		return buildProcurementFacilityMissingInfo(targetCanonicalPath)
	default:
		return nil
	}
}

func (e *ScoutEngine) applyDimensionWavesFromAppliedFacts(
	ctx context.Context,
	target *models.HorizonFragment,
	facts []*coreModels.OntologyFact,
	appliedFactIDs []string,
) error {
	if e == nil || target == nil || target.ID == "" || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return nil
	}
	if len(facts) == 0 || len(appliedFactIDs) == 0 {
		return nil
	}

	applied := map[string]struct{}{}
	for _, id := range appliedFactIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			applied[id] = struct{}{}
		}
	}
	if len(applied) == 0 {
		return nil
	}

	now := time.Now().UTC()
	dimensionFragmentIDsToPropagate := map[string]struct{}{}

	ensureFragment := func(canonicalPath string, ftype models.FragmentType, label string, entityPath string, sectorPath string) (*models.HorizonFragment, error) {
		canonicalPath = strings.TrimSpace(canonicalPath)
		if canonicalPath == "" || canonicalPath == "/" {
			return nil, fmt.Errorf("empty canonicalPath")
		}
		// Best-effort: normalize/strictify canonicalPath early so we don't create duplicates like /loc/uae vs /loc/ae/na/na.
		if e.canonicalPathResolver != nil && ftype != "" {
			if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, canonicalPath, ftype); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				canonicalPath = strings.TrimSpace(resolved.CanonicalPath)
				if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
					return resolved.ExistingFragment, nil
				}
			}
		}
		ex, err := e.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err == nil && ex != nil && ex.ID != "" {
			// Keep GEO dimension labels human-readable even for existing taxonomy stubs.
			if ftype == models.FragmentTypeGeoRegion {
				if ex.Labels == nil {
					ex.Labels = map[string]string{}
				}
				if shouldRefreshDisplayLabel(ex.Labels["en"], canonicalPath) {
					if next := bestEffortDisplayLabel("", canonicalPath); strings.TrimSpace(next) != "" {
						ex.Labels["en"] = next
						_ = e.fragmentsRepo.Update(ctx, ex)
					}
				}
			}
			return ex, nil
		}
		frag := &models.HorizonFragment{
			TenantID:      "global",
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
			CreatedBy:     "system",
			UpdatedBy:     "system",
			Labels:        map[string]string{"en": strings.TrimSpace(label)},
			Status:        "active",
			CanonicalPath: canonicalPath,
			Type:          ftype,
			EntityPath:    entityPath,
			SectorPath:    sectorPath,
			State:         map[string]models.MetricValue{},
			Provenance:    models.Provenance{Method: "taxonomy_dimension_seed", SourceIDs: []string{}, SourceRefs: []models.SourceRef{}},
			Metadata:      models.FragmentMetadata{CreatedAt: now, LastUpdatedAt: now, LastVerifiedAt: now, LastFactsSeenAt: now, LastFactsAppliedAt: now, EpistemicStatus: models.EpistemicStatusVerified},
			Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
		}
		if e.preCreateValidator != nil {
			if pre, perr := e.preCreateValidator.ValidateAndProcess(ctx, frag); perr == nil && pre != nil {
				if pre.ReplacementFrag != nil && strings.TrimSpace(pre.ReplacementFrag.ID) != "" {
					return pre.ReplacementFrag, nil
				}
				// Keep fail-open behavior: block only for truly critical path issues.
				if !pre.IsValid && strings.TrimSpace(pre.BlockReason) != "" {
					reason := strings.TrimSpace(pre.BlockReason)
					if strings.Contains(reason, "/discovered/") || strings.Contains(strings.ToLower(reason), "canonical path is required") {
						return nil, fmt.Errorf("dimension seed blocked: %s", reason)
					}
				}
			}
		}
		// Validation may rewrite canonicalPath; re-check after normalization to avoid duplicate create.
		canonicalPath = strings.TrimSpace(frag.CanonicalPath)
		if canonicalPath == "" || canonicalPath == "/" {
			return nil, fmt.Errorf("empty canonicalPath after normalization")
		}
		if ex2, exErr := e.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath); exErr == nil && ex2 != nil && ex2.ID != "" {
			return ex2, nil
		}
		id, err := e.fragmentsRepo.Create(ctx, frag)
		if err != nil {
			return nil, err
		}
		frag.ID = id
		// Add to session graph for duplicate prevention within job
		if sg := SessionGraphFromContext(ctx); sg != nil {
			sg.Add(frag)
		}
		return frag, nil
	}

	applyMetricUpdates := func(fragment *models.HorizonFragment, fact *coreModels.OntologyFact) bool {
		if fragment == nil || fact == nil || len(fact.MetricUpdates) == 0 {
			return false
		}
		if fragment.State == nil {
			fragment.State = map[string]models.MetricValue{}
		}
		dedupStringsLocal := func(in []string) []string {
			seen := map[string]struct{}{}
			out := make([]string, 0, len(in))
			for _, s := range in {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if _, ok := seen[s]; ok {
					continue
				}
				seen[s] = struct{}{}
				out = append(out, s)
			}
			return out
		}
		dedupHorizonRefsLocal := func(in []coreModels.SourceRef) []models.SourceRef {
			seen := map[string]struct{}{}
			out := make([]models.SourceRef, 0, len(in))
			for _, r := range in {
				sid := strings.TrimSpace(r.SourceID)
				st := strings.TrimSpace(r.SourceType)
				if sid == "" && st == "" {
					continue
				}
				key := st + "|" + sid
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, models.SourceRef{SourceID: sid, SourceType: st})
			}
			return out
		}
		changed := false
		for _, mu := range fact.MetricUpdates {
			name := strings.TrimSpace(mu.MetricName)
			if name == "" {
				continue
			}
			obs := now
			if mu.ObservedAt != nil {
				obs = mu.ObservedAt.UTC()
			}
			conf := mu.Confidence
			if conf <= 0 {
				conf = fact.Confidence
			}
			prev, ok := fragment.State[name]
			next := models.MetricValue{
				Value:      mu.Value,
				Unit:       mu.Unit,
				Confidence: conf,
				ObservedAt: obs,
				Provenance: models.Provenance{
					SourceIDs:  dedupStringsLocal(fact.SourceIDs),
					SourceRefs: dedupHorizonRefsLocal(fact.SourceRefs),
					ObservedAt: &obs,
					Method:     "dimension_fact_fastpath",
				},
			}
			if !ok || prev.Value != next.Value || (prev.Unit == nil) != (next.Unit == nil) {
				fragment.State[name] = next
				changed = true
			}
		}
		return changed
	}

	ensureInfluenceEdge := func(sourceID, targetID string, confidence float64, qualifier map[string]interface{}) {
		if sourceID == "" || targetID == "" || sourceID == targetID {
			return
		}
		edgeType := models.DependencyTypeInfluences
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, edgeType); err == nil {
			return
		}
		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sourceID,
			TargetFragmentID: targetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality: func(x float64) float64 {
					if x < 0 {
						return 0
					}
					if x > 1 {
						return 1
					}
					return x
				}(confidence),
				Amplification: 1.0,
				LatencyHours:  1,
			},
			Qualifiers: qualifier,
			Provenance: models.Provenance{
				Method: "dimension_edge_from_fact",
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     now,
				LastUpdatedAt: now,
			},
		}
		if _, err := e.edgesRepo.Create(ctx, edge); err != nil {
			// Fail-open: dimension edges are not critical for core graph materialization, but must not be silent.
			fmt.Printf("[ScoutEngine] Failed to create dimension edge: src=%s dst=%s type=%s err=%v\n",
				sourceID, targetID, string(edgeType), err)
		}
	}

	for _, f := range facts {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		if _, ok := applied[strings.TrimSpace(f.ID)]; !ok {
			continue
		}

		// 1) sphere dimensions
		if len(f.SpherePaths) > 0 {
			for _, sp := range f.SpherePaths {
				sp = strings.TrimSpace(sp)
				if sp == "" {
					continue
				}
				sfrag, err := ensureFragment(sp, models.FragmentTypeSphere, sp, "/Influence/Sphere", "/Influence")
				if err != nil || sfrag == nil {
					if err != nil {
						fmt.Printf("[ScoutEngine] Failed to ensure sphere fragment %s: %v\n", sp, err)
					}
					continue
				}
				ensureInfluenceEdge(sfrag.ID, target.ID, f.Confidence, map[string]interface{}{"dimension": "sphere", "path": sp, "factId": f.ID})
				// Metric updates are optional; sphere linkage should exist even without metrics.
				if len(f.MetricUpdates) > 0 && applyMetricUpdates(sfrag, f) {
					if err := e.fragmentsRepo.Update(ctx, sfrag); err != nil {
						fmt.Printf("[ScoutEngine] Failed to update sphere dimension fragment after metric updates: id=%s path=%s err=%v\n",
							strings.TrimSpace(sfrag.ID), strings.TrimSpace(sfrag.CanonicalPath), err)
					}
					dimensionFragmentIDsToPropagate[sfrag.ID] = struct{}{}
				}
			}
		}

		// 2) geo dimension (locationPath)
		if f.LocationPath != nil {
			lp := strings.TrimSpace(*f.LocationPath)
			if lp != "" {
				gfrag, err := ensureFragment(lp, models.FragmentTypeGeoRegion, "", "/Geo/Region", "/Geography")
				if err == nil && gfrag != nil {
					ensureInfluenceEdge(gfrag.ID, target.ID, f.Confidence, map[string]interface{}{"dimension": "geo", "path": lp, "factId": f.ID})
					// Metric updates are optional; geo linkage should exist even without metrics.
					if len(f.MetricUpdates) > 0 && applyMetricUpdates(gfrag, f) {
						if err := e.fragmentsRepo.Update(ctx, gfrag); err != nil {
							fmt.Printf("[ScoutEngine] Failed to update geo dimension fragment after metric updates: id=%s path=%s err=%v\n",
								strings.TrimSpace(gfrag.ID), strings.TrimSpace(gfrag.CanonicalPath), err)
						}
						dimensionFragmentIDsToPropagate[gfrag.ID] = struct{}{}
					}
				}
			}
		}
	}

	// Trigger propagation for dimension fragments (non-blocking).
	if e.propagationTrigger != nil {
		for fid := range dimensionFragmentIDsToPropagate {
			fid := fid
			runAsyncSafe("dimension_fragment_propagation", func() {
				propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				if err := e.propagationTrigger(propCtx, fid); err != nil {
					fmt.Printf("[ScoutEngine] propagationTrigger failed for dimension fragment: fragmentId=%s err=%v\n", fid, err)
				}
			})
		}
	}

	return nil
}

// ScoutEngine основной движок Scout для сбора и верификации данных
type ScoutEngine struct {
	planner                 *ScoutPlanner
	executor                *SourceExecutor
	scoutAgent              *agents.ScoutAgent
	criticAgent             *agents.CriticAgent
	resolverAgent           *agents.ResolverAgent
	researchAgent           *agents.ResearchAgent // New
	frameBuilder            *FrameBuilder
	workflow                *ScoutWorkflow
	fragmentsRepo           *repo.HorizonFragmentsRepository
	framesRepo              *repo.FramesRepository
	edgesRepo               *repo.DependencyEdgesRepository
	scoutingJobsRepo        *repo.ScoutingJobsRepository
	cascadeVisitedRepo      *repo.CascadeVisitedRepository
	cascadeEntityRegistry   *repo.CascadeEntityRegistryRepository
	lensesRepo              *repo.LensesRepository // Optional: for updating lens ViewConfig on assembly completion
	exposurePathsRepo       *repo.ExposurePathsRepository
	realtime                *realtime.Publisher
	taxonomyClassifier      *TaxonomyClassifier
	aiExecutor              *ai.AITaskExecutor
	wikidataClient          coreontology.WikidataClientInterface  // Optional: shared for enrichment/dedup (no search by default)
	geonamesClient          coreontology.GeoNamesClientInterface  // Optional: deterministic geo normalization/enrichment
	geocodingClient         coreontology.GeocodingClientInterface // Optional: address-to-coordinates for org/facility/gov
	openCorporatesClient    henrich.OpenCorporatesClientInterface // Optional: org enrichment with registry IDs
	spendLedger             *repo.HorizonSpendLedger
	budgetPolicy            BudgetPolicy
	propagationTrigger      func(ctx context.Context, fragmentID string) error
	signalPropagationEngine *propagation.PropagationEngine
	signalProfileLookup     *propagation.ProfileLookup
	signalSynthesizer       *signals.Synthesizer              // Optional: for signal synthesis from facts
	factsRepo               *corerepo.OntologyFactsRepository // Optional: for saving synthesized signals
	factEmitter             *coreontology.FactEmitter         // Optional: unified fact emission (write + feed)
	geoHierarchyBuilder     *GeoHierarchyBuilder              // Auto-creates geo hierarchy when cities are created
	orgDeduplicator         *OrgDeduplicator                  // Deduplicates organizations by Wikidata/shortName
	personDeduplicator      *PersonDeduplicator               // Deduplicates persons by name/phonetic match
	preCreateValidator      *PreCreateValidator               // Validates fragments before creation
	orgHierarchyBuilder     *OrgHierarchyBuilder              // Auto-creates org parent hierarchy
	marketHierarchyBuilder  *MarketHierarchyBuilder           // Auto-creates market hierarchy (segment-first when possible)
	marketGeoHierarchy      *MarketGeoHierarchyBuilder        // Auto-creates market geo parent (regional -> global)
	govHierarchyBuilder     *GovHierarchyBuilder              // Best-effort gov type inference + admin hierarchy helpers
	productDeduplicator     *ProductDeduplicator              // Deduplicates products by Wikidata/CAS/formula
	productHierarchyBuilder *ProductHierarchyBuilder          // Auto-creates product value chain hierarchy
	validationPolicy        *policy.PolicyExecutor            // Optional: unified validation policy for edges
	canonizationPolicy      *policy.PolicyExecutor            // Optional: unified canonization policy for paths/types
	canonicalPathResolver   *CanonicalPathResolver            // Unified canonicalization and deduplication service
	missingInfoStatusRepo   *repo.MissingInfoStatusRepository // Tracks missingInfo attempts to prevent infinite loops
	groundingResolver       *GroundingEvidenceResolver        // Resolves Vertex AI grounding evidence → real fragments from DB
	telemetryRepo           *repo.HorizonTelemetryRepository  // Optional: grounding/risk counters

	// Policy enforcement (final validation step)
	policyChecker          *policy.PolicyChecker              // Optional: validates job output before completion
	enforcementExecutor    *policy.EnforcementExecutor        // Optional: executes auto-fixes for policy violations
	enforcementJobsRepo    *repo.EnforcementJobsRepository    // Optional: persists enforcement jobs
	policyCheckResultsRepo *repo.PolicyCheckResultsRepository // Optional: persists policy check results
	policyWSPublisher      *policy.PolicyWSPublisher          // Optional: publishes policy check results via WebSocket
	policyPersistQueueMu   sync.Mutex
	policyPersistQueue     map[string]*policyPersistTask
	policyPersistDLQ       map[string]*policyPersistTask
	policyPersistWorkerOn  bool

	// Lens artifacts for Phase 2 Strategic Overlay
	lensArtifactsRepo *repo.LensArtifactsRepository         // Optional: accumulates finalized fragments/edges per lens
	domainAnswersRepo *repo.DomainAnswerSnapshotsRepository // Optional: persisted DomainAnswerSnapshot artifacts

	// Supply chain path materialization (Plan mode)
	supplyChainPathBuilder *pathbuilder.SupplyChainPathBuilder // Optional: MaterializePath from parsed chains

	// Cross-job early dedup cache for edge candidates (short-lived, best-effort).
	edgeRecentDedupMu         sync.Mutex
	edgeRecentDedup           map[string]time.Time
	edgeRecentDedupTTL        time.Duration
	edgeRecentDedupMaxEntries int

	// Shared Redis client for cross-job caching (skeleton context, edge dedup).
	redisClient *redis.Client
}

// bestEffortDeferredScopeAdd adds canonicalPath to lens.Scope if under limit (max 5 per lens).
// Used when Scout creates/updates a fragment for any lens job (not just deferred-scope).
// Path changes during pipeline (org upgrade, dedup, etc.) are handled by bestEffortRewriteLensScope.
func (e *ScoutEngine) bestEffortDeferredScopeAdd(ctx context.Context, lensID, canonicalPath string) {
	if e == nil || e.lensesRepo == nil || lensID == "" || strings.TrimSpace(canonicalPath) == "" {
		return
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	res, err := e.lensesRepo.AddScopeCanonicalPathAutoLimited(ctx, lensID, canonicalPath, models.MaxAutoScopeAddsPerLens)
	if err != nil {
		trace.Log(ctx, "scope", "scope.deferred_add.failed", map[string]any{
			"lensId": lensID,
			"path":   canonicalPath,
			"error":  err.Error(),
		})
		return
	}
	if res.LimitReached {
		trace.Log(ctx, "scope", "scope.deferred_add.skipped_limit", map[string]any{
			"lensId": lensID,
			"path":   canonicalPath,
			"count":  res.DeferredScopeAddCount,
		})
		return
	}
	if res.AlreadyInScope {
		trace.Log(ctx, "scope", "scope.deferred_add.skipped_exists", map[string]any{
			"lensId": lensID,
			"path":   canonicalPath,
		})
		return
	}
	trace.Log(ctx, "scope", "scope.deferred_add.ok", map[string]any{
		"lensId": lensID,
		"path":   canonicalPath,
		"count":  res.DeferredScopeAddCount,
	})
}

// bestEffortRemoveOrphanScopePaths removes scope paths that have no corresponding fragment.
func (e *ScoutEngine) bestEffortRemoveOrphanScopePaths(ctx context.Context, lensID string) {
	if e == nil || e.lensesRepo == nil || e.fragmentsRepo == nil || lensID == "" {
		return
	}
	lens, err := e.lensesRepo.Get(ctx, lensID)
	if err != nil || lens == nil || len(lens.Scope) == 0 {
		return
	}
	var toRemove []string
	for _, p := range lens.Scope {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "/query/") || strings.HasPrefix(p, "/domain/") {
			continue
		}
		resolvedCP := p
		if e.canonicalPathResolver != nil {
			ft := InferFragmentTypeFromCanonicalPath(p)
			if rr, rerr := e.canonicalPathResolver.Resolve(ctx, p, ft); rerr == nil && rr != nil && strings.TrimSpace(rr.CanonicalPath) != "" {
				resolvedCP = strings.TrimSpace(rr.CanonicalPath)
				if rr.ExistingFragment != nil {
					continue // has fragment
				}
			}
		}
		f, ferr := e.fragmentsRepo.GetByCanonicalPath(ctx, resolvedCP)
		if ferr != nil || f == nil || strings.TrimSpace(f.ID) == "" {
			toRemove = append(toRemove, p)
		}
	}
	if len(toRemove) == 0 {
		return
	}
	if err := e.lensesRepo.RemoveScopeCanonicalPaths(ctx, lensID, toRemove); err != nil {
		trace.Log(ctx, "scope", "scope.orphan_cleanup.failed", map[string]any{
			"lensId":   lensID,
			"removedN": len(toRemove),
			"paths":    toRemove,
			"error":    err.Error(),
		})
		return
	}
	trace.Log(ctx, "scope", "scope.orphan_cleanup.ok", map[string]any{
		"lensId":   lensID,
		"removedN": len(toRemove),
		"paths":    toRemove,
	})
}

// bestEffortRewriteLensScope replaces alias canonical paths in lens.Scope with their canonical equivalents.
// This helps the UI/graph loader, which relies on lens.Scope, not on transient job state.
//
// IMPORTANT: The primary lens is updated first, but errors are best-effort (no pipeline stop).
// Other lenses are updated best-effort as well.
func (e *ScoutEngine) bestEffortRewriteLensScope(ctx context.Context, tenantID, lensID, fromPath, toPath string) {
	if e == nil || e.lensesRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	fromPath = strings.TrimSpace(fromPath)
	toPath = strings.TrimSpace(toPath)
	if fromPath == "" || toPath == "" || fromPath == toPath {
		return
	}

	trace.Log(ctx, "scope", "scope.rewrite.start", map[string]any{
		"tenantId": tenantID,
		"lensId":   strings.TrimSpace(lensID),
		"fromPath": fromPath,
		"toPath":   toPath,
		"hint":     "best_effort_scope_rewrite",
	})

	// Helper to rewrite a single lens scope.
	// Returns true if a rewrite happened.
	rewriteLens := func(lens *models.Lens) bool {
		if lens == nil {
			return false
		}
		changed := false
		replaced := map[string]string{}
		resolvedCache := map[string]string{}
		resolvePath := func(p string) string {
			p = strings.TrimSpace(p)
			if p == "" {
				return ""
			}
			if v, ok := resolvedCache[p]; ok {
				return v
			}
			if e.canonicalPathResolver == nil || strings.HasPrefix(p, "/query/") {
				resolvedCache[p] = p
				return p
			}
			ft := InferFragmentTypeFromCanonicalPath(p)
			if ft == "" {
				ft = models.FragmentTypeOrganization
			}
			if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, p, ft); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				cp := strings.TrimSpace(resolved.CanonicalPath)
				resolvedCache[p] = cp
				return cp
			}
			resolvedCache[p] = p
			return p
		}
		for i, s := range lens.Scope {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if s == fromPath {
				lens.Scope[i] = toPath
				changed = true
				replaced[s] = toPath
			}
		}
		// If direct match didn't happen, try resolving aliases to the new canonical path.
		if !changed && e.canonicalPathResolver != nil {
			for i, s := range lens.Scope {
				s = strings.TrimSpace(s)
				if s == "" || s == toPath || strings.HasPrefix(s, "/query/") {
					continue
				}
				if resolvePath(s) == toPath {
					lens.Scope[i] = toPath
					changed = true
					replaced[s] = toPath
				}
			}
		}
		if !changed {
			return false
		}

		// Best-effort: keep ScopeFragments cache consistent.
		if lens.ScopeFragments != nil && len(replaced) > 0 {
			for old := range replaced {
				if fid, ok := lens.ScopeFragments[old]; ok {
					delete(lens.ScopeFragments, old)
					// Only set if not already present.
					if _, ok2 := lens.ScopeFragments[toPath]; !ok2 && strings.TrimSpace(fid) != "" {
						lens.ScopeFragments[toPath] = fid
					}
				}
			}
		}

		// Best-effort: keep ResolvedQueries cache consistent.
		// This cache stores results of QueryAddress resolution (e.g., /query/sector:/Tech/** -> [paths]).
		// If any resolved path matches fromPath, update it to toPath.
		if lens.ResolvedQueries != nil {
			for queryAddr, resolvedPaths := range lens.ResolvedQueries {
				updated := false
				for i, p := range resolvedPaths {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					if p == fromPath || replaced[p] != "" || resolvePath(p) == toPath {
						resolvedPaths[i] = toPath
						updated = true
					}
				}
				if updated {
					// De-dupe after rewrite.
					seen := map[string]struct{}{}
					out := make([]string, 0, len(resolvedPaths))
					for _, p := range resolvedPaths {
						p = strings.TrimSpace(p)
						if p == "" {
							continue
						}
						if _, ok := seen[p]; ok {
							continue
						}
						seen[p] = struct{}{}
						out = append(out, p)
					}
					lens.ResolvedQueries[queryAddr] = out
				}
			}
		}

		// De-dupe scope after rewrite.
		seen := map[string]struct{}{}
		out := make([]string, 0, len(lens.Scope))
		for _, s := range lens.Scope {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		lens.Scope = out
		return true
	}

	// Track which lenses we've already updated to avoid duplicates.
	updatedLensIDs := map[string]struct{}{}

	// Always record a deterministic redirect so that future scope processing can resolve aliases.
	// This is critical: even if the lens update below fails, the redirect is persisted
	// and the canonical path resolver can resolve it on next Resolve() call.
	e.recordDeterministicRedirect(ctx, fromPath, toPath, "scope_rewrite")

	// 1) Fast path: update the specific lens (lensID) first for immediate UI feedback.
	//    Uses retry to make the scope rewrite reliable (not truly "best-effort" for the primary lens).
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		trace.Log(ctx, "scope", "scope.rewrite.primary_lens.missing_lens_id", map[string]any{
			"tenantId": tenantID,
			"fromPath": fromPath,
			"toPath":   toPath,
		})
	} else {
		const maxRetries = 3
		for attempt := 0; attempt < maxRetries; attempt++ {
			lens, err := e.lensesRepo.Get(ctx, lensID)
			if err != nil || lens == nil {
				trace.Log(ctx, "scope", "scope.rewrite.primary_lens.get_failed", map[string]any{
					"tenantId": tenantID,
					"lensId":   lensID,
					"fromPath": fromPath,
					"toPath":   toPath,
					"error":    fmt.Sprint(err),
				})
				break
			}
			// Tenant safety: never cross-write.
			if tenantID != "" && strings.TrimSpace(lens.TenantID) != "" && strings.TrimSpace(lens.TenantID) != tenantID {
				trace.Log(ctx, "scope", "scope.rewrite.primary_lens.tenant_mismatch", map[string]any{
					"tenantId":     tenantID,
					"lensId":       lensID,
					"lensTenantId": strings.TrimSpace(lens.TenantID),
					"fromPath":     fromPath,
					"toPath":       toPath,
				})
				break
			}
			if !rewriteLens(lens) {
				trace.Log(ctx, "scope", "scope.rewrite.primary_lens.noop", map[string]any{
					"tenantId": tenantID,
					"lensId":   lensID,
					"fromPath": fromPath,
					"toPath":   toPath,
				})
				break // nothing to rewrite (already done or path not found)
			}
			if err := e.lensesRepo.Update(ctx, lens); err != nil {
				// Retry on conflict (concurrent write); give up on other errors.
				if attempt < maxRetries-1 {
					fmt.Printf("[ScoutEngine] bestEffortRewriteLensScope retry %d/%d: lensId=%s from=%s to=%s err=%v\n",
						attempt+1, maxRetries, lensID, fromPath, toPath, err)
					continue
				}
				fmt.Printf("[ScoutEngine] bestEffortRewriteLensScope FAILED after %d retries: lensId=%s from=%s to=%s err=%v\n",
					maxRetries, lensID, fromPath, toPath, err)
				trace.Log(ctx, "scope", "scope.rewrite.primary_lens.update_failed", map[string]any{
					"tenantId": tenantID,
					"lensId":   lensID,
					"fromPath": fromPath,
					"toPath":   toPath,
					"error":    err.Error(),
					"retries":  maxRetries,
				})
			} else {
				// Keep LensArtifacts canonical paths consistent with lens scope rewrites.
				if e.lensArtifactsRepo != nil {
					_ = e.lensArtifactsRepo.UpdateCanonicalPath(ctx, lens.ID, fromPath, toPath)
				}
				trace.Log(ctx, "scope", "scope.rewrite.primary_lens.ok", map[string]any{
					"tenantId": tenantID,
					"lensId":   lensID,
					"fromPath": fromPath,
					"toPath":   toPath,
				})
			}
			break
		}
		updatedLensIDs[lensID] = struct{}{}
	}

	// 2) Find and update ALL other lenses that contain the old path (up to 200).
	// This ensures canonical path changes propagate to all affected lenses, not just the job's lens.
	lenses, err := e.lensesRepo.FindByScope(ctx, tenantID, fromPath, 200)
	if err != nil || len(lenses) == 0 {
		return
	}
	for _, lens := range lenses {
		if lens == nil || strings.TrimSpace(lens.ID) == "" {
			continue
		}
		// Skip if already updated above.
		if _, ok := updatedLensIDs[lens.ID]; ok {
			continue
		}
		if rewriteLens(lens) {
			_ = e.lensesRepo.Update(ctx, lens)
			// Keep LensArtifacts canonical paths consistent with lens scope rewrites.
			if e.lensArtifactsRepo != nil {
				_ = e.lensArtifactsRepo.UpdateCanonicalPath(ctx, lens.ID, fromPath, toPath)
			}
		}
	}

	trace.Log(ctx, "scope", "scope.rewrite.done", map[string]any{
		"tenantId":       tenantID,
		"primaryLensId":  lensID,
		"fromPath":       fromPath,
		"toPath":         toPath,
		"updatedLensesN": len(updatedLensIDs),
	})
}

// WithPropagationTrigger устанавливает триггер для запуска распространения изменений
func (e *ScoutEngine) WithPropagationTrigger(trigger func(ctx context.Context, fragmentID string) error) *ScoutEngine {
	e.propagationTrigger = trigger
	return e
}

// WithSignalPropagation wires shared signal propagation components.
// This avoids ad-hoc defaults-only engines during Scout fast-path processing.
func (e *ScoutEngine) WithSignalPropagation(engine *propagation.PropagationEngine, lookup *propagation.ProfileLookup) *ScoutEngine {
	e.signalPropagationEngine = engine
	e.signalProfileLookup = lookup
	return e
}

// WithSpendLedger wires spend attribution ledger used for budget enforcement (optional).
func (e *ScoutEngine) WithSpendLedger(ledger *repo.HorizonSpendLedger) *ScoutEngine {
	e.spendLedger = ledger
	return e
}

// WithCascadeVisitedRepo wires cascade-level visited set storage (optional).
// Used to prevent cycles / avalanche fanout inside a single cascade (especially supply_chain mode).
func (e *ScoutEngine) WithCascadeVisitedRepo(r *repo.CascadeVisitedRepository) *ScoutEngine {
	e.cascadeVisitedRepo = r
	return e
}

// WithCascadeEntityRegistry wires cascade-level identity registry (optional).
// Used to remember which entities were already resolved/created inside one cascade,
// even when canonical paths differ across child jobs.
func (e *ScoutEngine) WithCascadeEntityRegistry(r *repo.CascadeEntityRegistryRepository) *ScoutEngine {
	e.cascadeEntityRegistry = r
	return e
}

func (e *ScoutEngine) WithBudgetPolicy(policy BudgetPolicy) *ScoutEngine {
	e.budgetPolicy = policy
	return e
}

// WithFactEmitter wires a shared fact emitter for producing ontology facts from Scout results.
func (e *ScoutEngine) WithFactEmitter(emitter *coreontology.FactEmitter) *ScoutEngine {
	e.factEmitter = emitter
	return e
}

// WithValidationPolicy sets the validation policy executor for edge validation.
func (e *ScoutEngine) WithValidationPolicy(executor *policy.PolicyExecutor) *ScoutEngine {
	e.validationPolicy = executor
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithValidationPolicy(executor)
	}
	return e
}

// WithTelemetryRepo wires unified telemetry sink for policy KPI counters.
func (e *ScoutEngine) WithTelemetryRepo(telemetryRepo *repo.HorizonTelemetryRepository) *ScoutEngine {
	e.telemetryRepo = telemetryRepo
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithTelemetryRepo(telemetryRepo)
	}
	if e.edgesRepo != nil {
		e.edgesRepo.WithTelemetryRepo(telemetryRepo)
	}
	return e
}

func (e *ScoutEngine) emitGroundingCounter(ctx context.Context, tenantID, metric string, dimensions map[string]string, counters map[string]int64) {
	if e == nil || e.telemetryRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if err := e.telemetryRepo.Increment(ctx, tenantID, "grounding", strings.TrimSpace(metric), time.Hour, dimensions, counters); err != nil {
		logGroundingWarnf("telemetry increment failed: tenantId=%s metric=%s err=%v", tenantID, strings.TrimSpace(metric), err)
	}
}

func (e *ScoutEngine) emitGroundingErrorTelemetry(ctx context.Context, job *models.ScoutingJob, component, phase, reasonCode, errorClass string, bestEffort bool) {
	tenantID := ""
	if job != nil {
		tenantID = strings.TrimSpace(job.TenantID)
	}
	e.emitGroundingCounter(ctx, tenantID, "horizon_grounding_errors_total", map[string]string{
		"component":   strings.TrimSpace(component),
		"phase":       strings.TrimSpace(phase),
		"error_class": strings.TrimSpace(errorClass),
		"reason_code": strings.TrimSpace(reasonCode),
	}, map[string]int64{"count": 1})
	if bestEffort {
		e.emitGroundingCounter(ctx, tenantID, "horizon_grounding_best_effort_total", map[string]string{
			"component":   strings.TrimSpace(component),
			"phase":       strings.TrimSpace(phase),
			"reason_code": strings.TrimSpace(reasonCode),
		}, map[string]int64{"count": 1})
	}
}

// WithCanonizationPolicy sets the canonization policy executor for path/type normalization.
func (e *ScoutEngine) WithCanonizationPolicy(executor *policy.PolicyExecutor) *ScoutEngine {
	e.canonizationPolicy = executor
	return e
}

// WithCanonicalPathResolver sets the canonical path resolver for early canonicalization and deduplication.
func (e *ScoutEngine) WithCanonicalPathResolver(resolver *CanonicalPathResolver) *ScoutEngine {
	e.canonicalPathResolver = resolver
	return e
}

// CanonicalPathResolver returns the resolver wired into this engine (may be nil).
func (e *ScoutEngine) CanonicalPathResolver() *CanonicalPathResolver {
	if e == nil {
		return nil
	}
	return e.canonicalPathResolver
}

// WithLensesRepository sets the lenses repository for updating lens ViewConfig on assembly completion.
func (e *ScoutEngine) WithLensesRepository(lensesRepo *repo.LensesRepository) *ScoutEngine {
	e.lensesRepo = lensesRepo
	return e
}

// WithExposurePathsRepository sets the exposure paths repo for strict applicability gating.
func (e *ScoutEngine) WithExposurePathsRepository(exposurePathsRepo *repo.ExposurePathsRepository) *ScoutEngine {
	e.exposurePathsRepo = exposurePathsRepo
	return e
}

// OrgDeduplicator returns the OrgDeduplicator instance used by this engine.
// This allows other subsystems (e.g. CanonicalPathResolver wired in main) to reuse the same dedup logic.
func (e *ScoutEngine) OrgDeduplicator() *OrgDeduplicator {
	if e == nil {
		return nil
	}
	return e.orgDeduplicator
}

// NewScoutEngine создает новый Scout Engine
func NewScoutEngine(
	planner *ScoutPlanner,
	executor *SourceExecutor,
	scoutAgent *agents.ScoutAgent,
	criticAgent *agents.CriticAgent,
	resolverAgent *agents.ResolverAgent,
	researchAgent *agents.ResearchAgent, // New
	frameBuilder *FrameBuilder,
	workflow *ScoutWorkflow,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	framesRepo *repo.FramesRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	scoutingJobsRepo *repo.ScoutingJobsRepository,
	realtime *realtime.Publisher,
	aiExecutor *ai.AITaskExecutor,
) *ScoutEngine {
	geoBuilder := NewGeoHierarchyBuilder(fragmentsRepo, edgesRepo)
	orgDedup := NewOrgDeduplicator(fragmentsRepo, nil)       // WikidataClient set via WithWikidataClient
	personDedup := NewPersonDeduplicator(fragmentsRepo, nil) // WikidataClient set via WithWikidataClient
	orgHierarchy := NewOrgHierarchyBuilder(fragmentsRepo, edgesRepo)
	marketHierarchy := NewMarketHierarchyBuilder(fragmentsRepo, edgesRepo)
	marketGeoHierarchy := NewMarketGeoHierarchyBuilder(fragmentsRepo, edgesRepo)
	govHierarchy := NewGovHierarchyBuilder(fragmentsRepo, edgesRepo)
	productDedup := NewProductDeduplicator(fragmentsRepo, nil)                    // WikidataClient set via WithWikidataClient
	productHierarchy := NewProductHierarchyBuilder(fragmentsRepo, edgesRepo, nil) // WikidataClient set via WithWikidataClient

	engine := &ScoutEngine{
		planner:                   planner,
		executor:                  executor,
		scoutAgent:                scoutAgent,
		criticAgent:               criticAgent,
		resolverAgent:             resolverAgent,
		researchAgent:             researchAgent,
		frameBuilder:              frameBuilder,
		workflow:                  workflow,
		fragmentsRepo:             fragmentsRepo,
		framesRepo:                framesRepo,
		edgesRepo:                 edgesRepo,
		scoutingJobsRepo:          scoutingJobsRepo,
		cascadeVisitedRepo:        nil,
		cascadeEntityRegistry:     nil,
		realtime:                  realtime,
		taxonomyClassifier:        NewTaxonomyClassifier(aiExecutor),
		aiExecutor:                aiExecutor,
		spendLedger:               nil,
		budgetPolicy:              BudgetPolicyFromEnv(),
		geoHierarchyBuilder:       geoBuilder,
		orgDeduplicator:           orgDedup,
		personDeduplicator:        personDedup,
		orgHierarchyBuilder:       orgHierarchy,
		marketHierarchyBuilder:    marketHierarchy,
		marketGeoHierarchy:        marketGeoHierarchy,
		govHierarchyBuilder:       govHierarchy,
		productDeduplicator:       productDedup,
		productHierarchyBuilder:   productHierarchy,
		policyPersistQueue:        make(map[string]*policyPersistTask),
		policyPersistDLQ:          make(map[string]*policyPersistTask),
		edgeRecentDedup:           make(map[string]time.Time),
		edgeRecentDedupTTL:        10 * time.Minute,
		edgeRecentDedupMaxEntries: 20000,
	}
	engine.preCreateValidator = NewPreCreateValidator(geoBuilder, orgDedup, productDedup).WithPersonDedup(personDedup)
	engine.groundingResolver = NewGroundingEvidenceResolver(fragmentsRepo)
	return engine
}

// WithWikidataClient sets the Wikidata client for organization, person, and product deduplication
func (e *ScoutEngine) WithWikidataClient(client coreontology.WikidataClientInterface) *ScoutEngine {
	e.wikidataClient = client
	if e.orgDeduplicator != nil {
		e.orgDeduplicator.wikidataClient = client
	} else {
		e.orgDeduplicator = NewOrgDeduplicator(e.fragmentsRepo, client)
	}
	if e.personDeduplicator != nil {
		e.personDeduplicator.wikidataClient = client
	} else {
		e.personDeduplicator = NewPersonDeduplicator(e.fragmentsRepo, client)
	}
	if e.productDeduplicator != nil {
		e.productDeduplicator.wikidataClient = client
		if e.productDeduplicator.extractor != nil {
			e.productDeduplicator.extractor.wikidataClient = client
		}
	} else {
		e.productDeduplicator = NewProductDeduplicator(e.fragmentsRepo, client)
	}
	if e.productHierarchyBuilder != nil {
		e.productHierarchyBuilder.wikidataClient = client
		if e.productHierarchyBuilder.extractor != nil {
			e.productHierarchyBuilder.extractor.wikidataClient = client
		}
		// Keep producer/org dedup in sync as well (used for Wikidata manufacturer enrichment).
		if e.productHierarchyBuilder.orgDeduplicator != nil {
			e.productHierarchyBuilder.orgDeduplicator.wikidataClient = client
		}
	} else {
		e.productHierarchyBuilder = NewProductHierarchyBuilder(e.fragmentsRepo, e.edgesRepo, client)
	}
	// Also update canonicalPathResolver if it exists
	if e.canonicalPathResolver != nil && e.canonicalPathResolver.orgDeduplicator != nil {
		e.canonicalPathResolver.orgDeduplicator.wikidataClient = client
	}
	// Also update PreCreateValidator for conservative enrichment (QID-known only).
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithWikidataClient(client)
	}
	return e
}

// WithGeoNamesClient sets the GeoNames client for deterministic geo operations.
func (e *ScoutEngine) WithGeoNamesClient(client coreontology.GeoNamesClientInterface) *ScoutEngine {
	e.geonamesClient = client
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithGeoNamesClient(client)
	}
	return e
}

// WithGeocodingClient sets the geocoding client for address-to-coordinates (org/facility/gov).
func (e *ScoutEngine) WithGeocodingClient(client coreontology.GeocodingClientInterface) *ScoutEngine {
	e.geocodingClient = client
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithGeocodingClient(client)
	}
	return e
}

// WithOpenCorporatesClient sets the OpenCorporates client for organization enrichment (registry IDs).
func (e *ScoutEngine) WithOpenCorporatesClient(client henrich.OpenCorporatesClientInterface) *ScoutEngine {
	e.openCorporatesClient = client
	return e
}

// WithPolicyChecker sets the policy checker for final validation of job outputs.
func (e *ScoutEngine) WithPolicyChecker(checker *policy.PolicyChecker) *ScoutEngine {
	e.policyChecker = checker
	return e
}

// WithEnforcementExecutor sets the enforcement executor for auto-fixing policy violations.
func (e *ScoutEngine) WithEnforcementExecutor(executor *policy.EnforcementExecutor) *ScoutEngine {
	e.enforcementExecutor = executor
	return e
}

// WithEnforcementJobsRepo sets the repository for persisting enforcement jobs.
func (e *ScoutEngine) WithEnforcementJobsRepo(repo *repo.EnforcementJobsRepository) *ScoutEngine {
	e.enforcementJobsRepo = repo
	return e
}

// WithPolicyCheckResultsRepo sets the repository for persisting policy check results.
func (e *ScoutEngine) WithPolicyCheckResultsRepo(repo *repo.PolicyCheckResultsRepository) *ScoutEngine {
	e.policyCheckResultsRepo = repo
	return e
}

// WithPolicyWSPublisher sets the WebSocket publisher for policy check results.
func (e *ScoutEngine) WithPolicyWSPublisher(publisher *policy.PolicyWSPublisher) *ScoutEngine {
	e.policyWSPublisher = publisher
	return e
}

// WithLensArtifactsRepo sets the repository for accumulating finalized fragments/edges per lens.
// Used by Phase 2 Strategic Overlay to build decision layer from lens job outputs.
func (e *ScoutEngine) WithLensArtifactsRepo(repo *repo.LensArtifactsRepository) *ScoutEngine {
	e.lensArtifactsRepo = repo
	return e
}

// WithDomainAnswerSnapshotsRepo sets repository for Domain Answer snapshots.
func (e *ScoutEngine) WithDomainAnswerSnapshotsRepo(repo *repo.DomainAnswerSnapshotsRepository) *ScoutEngine {
	e.domainAnswersRepo = repo
	return e
}

// WithSupplyChainPathBuilder sets the path builder for materializing supply chain paths from parsed chains (Plan mode).
func (e *ScoutEngine) WithSupplyChainPathBuilder(b *pathbuilder.SupplyChainPathBuilder) *ScoutEngine {
	e.supplyChainPathBuilder = b
	return e
}

// WithRedisClient sets the shared Redis client for cross-job caching (skeleton context, edge dedup).
func (e *ScoutEngine) WithRedisClient(client *redis.Client) *ScoutEngine {
	e.redisClient = client
	return e
}

// persistLensArtifactsFromJobState persists finalized fragments and edges created/modified by this job
// into the lens-owned artifacts collection.
//
// IMPORTANT:
// - We persist ONLY objects tracked in job.State by the tracking policy (created/modified IDs).
// - We do NOT persist during mid-job execution, to avoid leaking partial outputs from failed/rejected jobs.
func (e *ScoutEngine) persistLensArtifactsFromJobState(ctx context.Context, job *models.ScoutingJob) {
	if e == nil || e.lensArtifactsRepo == nil || e.fragmentsRepo == nil || e.edgesRepo == nil || job == nil {
		return
	}
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" {
		return
	}
	if job.State == nil {
		return
	}
	tenantID := strings.TrimSpace(job.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	// 1) Fragments
	fragIDs := make([]string, 0, 64)
	fragSeen := map[string]struct{}{}
	for _, id := range append(
		e.extractStringSlice(job.State, "createdFragmentIds"),
		e.extractStringSlice(job.State, "modifiedFragmentIds")...,
	) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := fragSeen[id]; ok {
			continue
		}
		fragSeen[id] = struct{}{}
		fragIDs = append(fragIDs, id)
	}

	fragsByID, _ := e.fragmentsRepo.GetMany(ctx, fragIDs)
	for _, fid := range fragIDs {
		f := fragsByID[strings.TrimSpace(fid)]
		if f == nil || strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.CanonicalPath) == "" {
			continue
		}
		displayLabel := strings.TrimSpace(f.Labels["en"])
		if displayLabel == "" {
			displayLabel = strings.TrimSpace(f.CanonicalPath)
		}
		artifact := models.NewFragmentArtifact(
			tenantID,
			lensID,
			strings.TrimSpace(f.ID),
			strings.TrimSpace(f.CanonicalPath),
			string(f.Type),
			strings.TrimSpace(job.ID),
			displayLabel,
		)
		if _, err := e.lensArtifactsRepo.UpsertFragment(ctx, artifact); err != nil {
			fmt.Printf("[ScoutEngine] persistLensArtifacts: UpsertFragment failed lensId=%s path=%s err=%v\n",
				lensID, strings.TrimSpace(f.CanonicalPath), err)
		}
	}

	// 2) Edges
	edgeIDs := make([]string, 0, 128)
	edgeSeen := map[string]struct{}{}
	for _, id := range append(
		e.extractStringSlice(job.State, "createdEdgeIds"),
		e.extractStringSlice(job.State, "modifiedEdgeIds")...,
	) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := edgeSeen[id]; ok {
			continue
		}
		edgeSeen[id] = struct{}{}
		edgeIDs = append(edgeIDs, id)
	}

	// Fetch edges and collect endpoint fragment IDs for batched fragment reads.
	edges := make([]*models.DependencyEdge, 0, len(edgeIDs))
	endpointIDs := make([]string, 0, len(edgeIDs)*2)
	for _, eid := range edgeIDs {
		ed, err := e.edgesRepo.Get(ctx, strings.TrimSpace(eid))
		if err != nil || ed == nil {
			continue
		}
		edges = append(edges, ed)
		if strings.TrimSpace(ed.SourceFragmentID) != "" {
			endpointIDs = append(endpointIDs, strings.TrimSpace(ed.SourceFragmentID))
		}
		if strings.TrimSpace(ed.TargetFragmentID) != "" {
			endpointIDs = append(endpointIDs, strings.TrimSpace(ed.TargetFragmentID))
		}
	}
	endpointsByID, _ := e.fragmentsRepo.GetMany(ctx, endpointIDs)

	for _, ed := range edges {
		if ed == nil || strings.TrimSpace(ed.ID) == "" {
			continue
		}
		srcPath := ""
		tgtPath := ""
		if src := endpointsByID[strings.TrimSpace(ed.SourceFragmentID)]; src != nil {
			srcPath = strings.TrimSpace(src.CanonicalPath)
		}
		if tgt := endpointsByID[strings.TrimSpace(ed.TargetFragmentID)]; tgt != nil {
			tgtPath = strings.TrimSpace(tgt.CanonicalPath)
		}

		artifact := models.NewEdgeArtifact(
			tenantID,
			lensID,
			strings.TrimSpace(ed.ID),
			string(ed.Type),
			srcPath,
			tgtPath,
			strings.TrimSpace(ed.SourceFragmentID),
			strings.TrimSpace(ed.TargetFragmentID),
			strings.TrimSpace(job.ID),
		)
		if _, err := e.lensArtifactsRepo.UpsertEdge(ctx, artifact); err != nil {
			fmt.Printf("[ScoutEngine] persistLensArtifacts: UpsertEdge failed lensId=%s edgeId=%s err=%v\n",
				lensID, strings.TrimSpace(ed.ID), err)
		}
	}
}

func (e *ScoutEngine) publish(ctx context.Context, et realtime.EventType, data map[string]interface{}) {
	if e == nil || e.realtime == nil {
		return
	}
	rc, ok := rtctx.Get(ctx)
	tenantID := ""
	if ok {
		tenantID = strings.TrimSpace(rc.TenantID)
		if v := strings.TrimSpace(rc.ProjectID); v != "" {
			data["projectId"] = v
		}
		if v := strings.TrimSpace(rc.LensID); v != "" {
			data["lensId"] = v
		}
		if v := strings.TrimSpace(rc.CorrelationID); v != "" {
			data["correlationId"] = v
		}
		if v := strings.TrimSpace(rc.SessionID); v != "" {
			data["sessionId"] = v
		}
	}
	if tenantID == "" {
		if v, ok2 := data["tenantId"].(string); ok2 {
			tenantID = strings.TrimSpace(v)
		}
	}
	if tenantID == "" {
		return
	}
	data["tenantId"] = tenantID
	data["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	_ = e.realtime.PublishJSON(ctx, et, tenantID, data)
}

func (e *ScoutEngine) attachRealtimeContext(ctx context.Context, job *models.ScoutingJob, tenantID, projectID, lensID string) context.Context {
	correlationID := ""
	sessionID := ""
	if job != nil && job.State != nil {
		if v, ok := job.State["correlationId"].(string); ok {
			correlationID = strings.TrimSpace(v)
		}
		if v, ok := job.State["sessionId"].(string); ok {
			sessionID = strings.TrimSpace(v)
		}
	}
	return rtctx.With(ctx, rtctx.Context{
		TenantID:      strings.TrimSpace(tenantID),
		ProjectID:     strings.TrimSpace(projectID),
		LensID:        strings.TrimSpace(lensID),
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})
}

func normalizePipelinePassName(raw string) (pipeline.PassName, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(pipeline.PassBaseEnrichment), "base-enrichment", "base", "phase1", "domain_expansion", "domain-expansion":
		return pipeline.PassBaseEnrichment, true
	case string(pipeline.PassStrategicProjection), "strategic-projection", "strategic", "phase2", "domain_overlay", "domain-overlay", "phase2_overlay", "phase2-overlay":
		return pipeline.PassStrategicProjection, true
	default:
		return "", false
	}
}

func (e *ScoutEngine) resolvePipelinePassForJob(ctx context.Context, job *models.ScoutingJob, effectiveCanonicalPath string) (pipeline.PassName, bool) {
	if job == nil {
		return "", false
	}
	if !strings.HasPrefix(strings.TrimSpace(effectiveCanonicalPath), "/domain/") {
		return "", false
	}
	if job.State != nil {
		if raw := parseStateString(job.State[pipeline.StateKeyPipelinePass]); raw != "" {
			if pass, ok := normalizePipelinePassName(raw); ok {
				return pass, true
			}
			trace.Log(ctx, "scout_exec", "scout.pipeline_pass.unknown", map[string]any{
				"tenantId":      strings.TrimSpace(job.TenantID),
				"projectId":     strings.TrimSpace(job.ProjectID),
				"lensId":        strings.TrimSpace(job.LensID),
				"jobId":         strings.TrimSpace(job.ID),
				"canonicalPath": strings.TrimSpace(effectiveCanonicalPath),
				"pipelinePass":  strings.TrimSpace(raw),
			})
		}
		if parseStateBool(job.State["domainOverlayMode"], false) || parseStateBool(job.State["phase2Overlay"], false) {
			return pipeline.PassStrategicProjection, true
		}
	}
	// Domain jobs default to base enrichment pass.
	return pipeline.PassBaseEnrichment, true
}

func isTerminalScoutJob(job *models.ScoutingJob) bool {
	if job == nil {
		return true
	}
	switch job.CurrentState {
	case models.ScoutingJobStateVerified, models.ScoutingJobStateFailed, models.ScoutingJobStateRejected:
		return true
	}
	switch job.Status {
	case coreModels.AgentJobStatusCompleted, coreModels.AgentJobStatusFailed, coreModels.AgentJobStatusPartial:
		return true
	}
	return false
}

func (e *ScoutEngine) finalizePipelinePassIfNeeded(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	passName pipeline.PassName,
) error {
	if e == nil || job == nil || isTerminalScoutJob(job) {
		return nil
	}
	hint := "pipeline_pass_" + strings.TrimSpace(string(passName))
	if strings.TrimSpace(string(passName)) == "" {
		hint = "pipeline_pass"
	}
	if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, hint); err != nil {
		return err
	}
	e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": strings.TrimSpace(effectiveCanonicalPath),
		"confidence":    1.0,
		"hint":          hint,
	})
	e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
	return nil
}

func (e *ScoutEngine) runPipelinePass(
	ctx context.Context,
	job *models.ScoutingJob,
	passName pipeline.PassName,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	if e == nil || job == nil {
		return nil
	}
	if strings.TrimSpace(string(passName)) == "" {
		passName = pipeline.PassBaseEnrichment
	}
	trace.Log(ctx, "scout_exec", "scout.pipeline_pass.start", map[string]any{
		"tenantId":      strings.TrimSpace(job.TenantID),
		"projectId":     strings.TrimSpace(job.ProjectID),
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         strings.TrimSpace(job.ID),
		"canonicalPath": strings.TrimSpace(effectiveCanonicalPath),
		"pass":          strings.TrimSpace(string(passName)),
	})
	runner := pipeline.DefaultRunner()
	if err := runner.Run(ctx, e, passName, pipeline.PassInput{
		Job:                    job,
		EffectiveCanonicalPath: effectiveCanonicalPath,
		ExistingFragment:       existingFragment,
	}); err != nil {
		_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "pipeline_pass_failed")
		return fmt.Errorf("pipeline pass %q failed: %w", strings.TrimSpace(string(passName)), err)
	}
	if err := e.finalizePipelinePassIfNeeded(ctx, job, effectiveCanonicalPath, passName); err != nil {
		_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "pipeline_pass_finalize_failed")
		return fmt.Errorf("pipeline pass %q finalize failed: %w", strings.TrimSpace(string(passName)), err)
	}
	trace.Log(ctx, "scout_exec", "scout.pipeline_pass.completed", map[string]any{
		"tenantId":      strings.TrimSpace(job.TenantID),
		"projectId":     strings.TrimSpace(job.ProjectID),
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         strings.TrimSpace(job.ID),
		"canonicalPath": strings.TrimSpace(effectiveCanonicalPath),
		"pass":          strings.TrimSpace(string(passName)),
	})
	return nil
}

func (e *ScoutEngine) transition(ctx context.Context, job *models.ScoutingJob, newState models.ScoutingJobState, progress float64, hint string) error {
	if e == nil || e.workflow == nil || job == nil {
		return nil
	}
	if err := e.workflow.TransitionState(ctx, job.ID, newState); err != nil {
		return err
	}
	// Update local job snapshot (best-effort)
	job.CurrentState = newState
	// IMPORTANT: keep AgentJob.Status in sync with the transitioned state.
	// Otherwise subsequent best-effort Update() calls from ScoutEngine (which persist job.State, attempts, etc.)
	// can overwrite the terminal status back to "pending"/"running", causing CreateDedup() to incorrectly
	// treat old jobs as active forever and to reuse jobId (created=false).
	switch newState {
	case models.ScoutingJobStateVerified:
		job.Status = coreModels.AgentJobStatusCompleted
	case models.ScoutingJobStateRejected, models.ScoutingJobStateFailed:
		job.Status = coreModels.AgentJobStatusFailed
		// Orphan cleanup: remove scope paths with no fragment when job fails.
		if lensID := strings.TrimSpace(job.LensID); lensID != "" && e.lensesRepo != nil && e.fragmentsRepo != nil {
			e.bestEffortRemoveOrphanScopePaths(ctx, lensID)
		}
	default:
		// Any non-terminal scout state counts as running from AgentJob perspective.
		// (pending is only used at creation time).
		job.Status = coreModels.AgentJobStatusRunning
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State["currentState"] = string(newState)
	evt := map[string]interface{}{
		"tenantId":  job.TenantID,
		"projectId": job.ProjectID,
		"lensId":    strings.TrimSpace(job.LensID),
		"jobId":     job.ID,
		"state":     string(newState),
		"progress":  progress,
	}
	if hint != "" {
		evt["hint"] = hint
	}
	if strings.TrimSpace(job.TargetCanonicalPath) != "" {
		evt["targetCanonicalPath"] = strings.TrimSpace(job.TargetCanonicalPath)
	}
	e.publish(ctx, realtime.EventType("horizon.scout.job.state"), evt)
	return nil
}

func (e *ScoutEngine) logLine(ctx context.Context, job *models.ScoutingJob, level string, line string) {
	if e == nil || job == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if level == "" {
		level = "info"
	}
	e.publish(ctx, realtime.EventType("horizon.scout.job.log"), map[string]interface{}{
		"tenantId":            job.TenantID,
		"projectId":           job.ProjectID,
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               job.ID,
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"level":               level,
		"line":                line,
	})
}

// ExecuteScoutingJob выполняет полный цикл ScoutingJob
func (e *ScoutEngine) ExecuteScoutingJob(ctx context.Context, job *models.ScoutingJob) error {
	if job == nil {
		return fmt.Errorf("job is nil")
	}
	policyTracker := tracking.NewTracker()
	ctx = tracking.WithTracker(ctx, policyTracker)

	// SESSION GRAPH: In-memory cache for fragments discovered/created during this job.
	// Solves: Vertex indexing delay, duplicate creation, path update tracking.
	sessionGraph := NewSessionGraph()
	ctx = WithSessionGraph(ctx, sessionGraph)

	trace.Log(ctx, "scout_exec", "scout.job.start", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"status":              strings.TrimSpace(string(job.Status)),
		"currentState":        string(job.CurrentState),
		"sourceIdsN":          len(job.SourceIDs),
	})

	// IDEMPOTENCY CHECK: Skip already-completed jobs (handles Cloud Tasks retries gracefully).
	// This prevents duplicate work when Cloud Tasks retries due to timeout/transient errors.
	if job.CurrentState == models.ScoutingJobStateVerified ||
		job.CurrentState == models.ScoutingJobStateFailed ||
		job.Status == coreModels.AgentJobStatusCompleted ||
		job.Status == coreModels.AgentJobStatusFailed ||
		job.Status == coreModels.AgentJobStatusPartial {
		trace.Log(ctx, "scout_exec", "scout.job.idempotent_skip", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"status":              strings.TrimSpace(string(job.Status)),
			"currentState":        string(job.CurrentState),
		})
		fmt.Printf("[ScoutEngine] Job %s already terminal (CurrentState=%s, Status=%s). Skipping (idempotent).\n",
			job.ID, job.CurrentState, job.Status)
		return nil // Success: already processed
	}

	// Strict Domain Plan Contract: domain jobs without valid execution plan are not executed.
	// Also short-circuit budget-exhausted roots into deterministic partial terminal state.
	{
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		targetCP := strings.TrimSpace(job.TargetCanonicalPath)
		lensID := strings.TrimSpace(job.LensID)
		shouldStop := false
		stopHint := ""
		stopOutcome := ""
		stopReason := map[string]interface{}{}
		if strings.HasPrefix(targetCP, "/domain/") {
			if _, err := ValidateDomainExecutionPlanState(job.State); err != nil {
				shouldStop = true
				stopHint = "invalid_execution_plan"
				stopOutcome = TerminalOutcomeFailed
				stopReason["error"] = err.Error()
			}
		}
		if !shouldStop && parseStateBool(job.State["budgetExhausted"], false) && parseStateBool(job.State["stopOnBudgetExhaustion"], false) {
			shouldStop = true
			stopHint = "budget_exhausted_partial"
			stopOutcome = TerminalOutcomeBudgetExhausted
			stopReason["budgetExhaustedPartial"] = true
		}
		if shouldStop {
			job.State["terminalOutcome"] = stopOutcome
			job.State["terminalOutcomeReasons"] = stopReason
			job.State["terminalizedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			if stopOutcome == TerminalOutcomeFailed {
				job.Status = coreModels.AgentJobStatusFailed
				job.CurrentState = models.ScoutingJobStateFailed
			} else {
				job.Status = coreModels.AgentJobStatusPartial
				job.CurrentState = models.ScoutingJobStateVerified
			}
			_ = e.scoutingJobsRepo.Update(ctx, job)
			e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
				"tenantId":        job.TenantID,
				"projectId":       job.ProjectID,
				"lensId":          lensID,
				"jobId":           job.ID,
				"canonicalPath":   targetCP,
				"terminalOutcome": stopOutcome,
				"reasonMap":       stopReason,
				"hint":            stopHint,
			})
			e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, lensID)
			return nil
		}
	}

	// CIRCUIT BREAKER: prevent infinite retarget/alias loops within a single job.
	// If the same canonicalPath is processed repeatedly (e.g., due to alias/dedup oscillation),
	// hard-stop this branch to avoid runaway Cloud Tasks retries and repeated research.
	{
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		cp := strings.TrimSpace(job.TargetCanonicalPath)
		if cp != "" {
			// Keep per-path attempt counters inside job.State to survive Cloud Tasks retries.
			// Shape: state["canonicalPathAttempts"] = map[string]int
			attempts := map[string]int{}
			if raw, ok := job.State["canonicalPathAttempts"]; ok {
				switch t := raw.(type) {
				case map[string]int:
					attempts = t
				case map[string]interface{}:
					for k, v := range t {
						k = strings.TrimSpace(k)
						if k == "" {
							continue
						}
						// Firestore may decode ints as float64/int64/string.
						n := 0
						switch vv := v.(type) {
						case int:
							n = vv
						case int32:
							n = int(vv)
						case int64:
							n = int(vv)
						case float64:
							n = int(vv)
						case float32:
							n = int(vv)
						case string:
							n = parseStateInt(vv, 0)
						default:
							n = parseStateInt(fmt.Sprintf("%v", vv), 0)
						}
						attempts[k] = n
					}
				}
			}
			attempts[cp] = attempts[cp] + 1
			job.State["canonicalPathAttempts"] = attempts
			if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"canonicalPathAttempts": attempts}); err != nil {
				fmt.Printf("[ScoutEngine] Failed to persist circuit breaker state (best-effort): jobId=%s target=%s err=%v\n",
					strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			}
			trace.Log(ctx, "scout_exec", "scout.job.circuit_breaker.tick", map[string]any{
				"tenantId":      strings.TrimSpace(job.TenantID),
				"projectId":     strings.TrimSpace(job.ProjectID),
				"lensId":        strings.TrimSpace(job.LensID),
				"jobId":         strings.TrimSpace(job.ID),
				"canonicalPath": cp,
				"attempts":      attempts[cp],
			})

			if attempts[cp] >= 5 {
				trace.Log(ctx, "scout_exec", "scout.job.circuit_breaker.trip", map[string]any{
					"tenantId":      strings.TrimSpace(job.TenantID),
					"projectId":     strings.TrimSpace(job.ProjectID),
					"lensId":        strings.TrimSpace(job.LensID),
					"jobId":         strings.TrimSpace(job.ID),
					"canonicalPath": cp,
					"attempts":      attempts[cp],
				})
				fmt.Printf("[ScoutEngine] Circuit breaker tripped: job=%s canonicalPath=%s attempts=%d. Hard-stopping.\n",
					strings.TrimSpace(job.ID), cp, attempts[cp])
				if err := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "circuit_breaker"); err != nil {
					fmt.Printf("[ScoutEngine] Failed to transition job to FAILED on circuit breaker (best-effort): jobId=%s target=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
				}
				e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
					"tenantId":      job.TenantID,
					"projectId":     job.ProjectID,
					"lensId":        strings.TrimSpace(job.LensID),
					"jobId":         job.ID,
					"canonicalPath": cp,
					"hint":          "circuit_breaker",
				})
				// Check if all jobs are completed
				e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
				return nil // ACK: stop Cloud Tasks retries for this branch
			}
		}
	}

	tenantID := job.TenantID
	projectID := job.ProjectID

	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	// Optional task focus modules (specialties) requested by UI/lens.
	// These are stored in job.State to keep schemas stable.
	specialties := []string(nil)
	if job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}

	// Supply chain context (optional): pass already-discovered chain nodes/edges + gaps
	// to keep LLM scouting incremental and business-meaningful.
	chainContextForPrompt := ""
	if job.State != nil {
		if v, ok := job.State["chainContext"]; ok {
			if cc, _ := ParseChainContext(v); cc != nil {
				chainContextForPrompt = cc.ToPromptContext()
			}
		}
	}

	// Attach telemetry context so LLM usage metrics get attributed even if individual agents
	// don't fill task.TenantID/UserID fields.
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" && job.State != nil {
		if v, ok := job.State["lensId"].(string); ok {
			lensID = strings.TrimSpace(v)
		}
	}
	// Ensure assembly completion is checked when job reaches a terminal state,
	// regardless of which code path caused the failure. Without this, a single
	// rejected/failed job can hang the entire cascade forever.
	defer func() {
		if isTerminalScoutJob(job) && lensID != "" {
			e.checkAndPublishAssemblyCompletion(context.Background(), tenantID, projectID, lensID)
		}
	}()
	cascadeID := ""
	if job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); ok {
			cascadeID = strings.TrimSpace(v)
		}
	}
	if cascadeID == "" {
		cascadeID = strings.TrimSpace(job.ID)
	}
	// Best-effort: persist cascadeRootJobId back into job state once.
	if job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); !ok || strings.TrimSpace(v) == "" {
			job.State["cascadeRootJobId"] = cascadeID
			_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"cascadeRootJobId": cascadeID})
		}
	}
	ctx = ai.WithUsageContext(ctx, tenantID, "SYSTEM", projectID, "horizon.scout", lensID, cascadeID)
	ctx = e.attachRealtimeContext(ctx, job, tenantID, projectID, lensID)
	trace.Log(ctx, "scout_exec", "scout.job.context", map[string]any{
		"tenantId":            strings.TrimSpace(tenantID),
		"projectId":           strings.TrimSpace(projectID),
		"lensId":              strings.TrimSpace(lensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"cascadeRootJobId":    cascadeID,
	})

	// EARLY CANONICALIZATION AND DEDUPLICATION: Resolve canonical path before any processing
	// This ensures we operate on canonicalized and deduplicated paths throughout the pipeline
	var resolvedPath *ResolveResult
	var effectiveCanonicalPath string
	var orgLegacyPath string
	var orgCanonicalizedPath string
	if e.canonicalPathResolver != nil {
		fragmentType := InferFragmentTypeFromCanonicalPath(job.TargetCanonicalPath)

		resolved, err := e.canonicalPathResolver.Resolve(ctx, job.TargetCanonicalPath, fragmentType)
		if err == nil && resolved != nil {
			resolvedPath = resolved
			effectiveCanonicalPath = resolved.CanonicalPath

			// If path was changed (canonicalized or deduplicated), update job
			if effectiveCanonicalPath != strings.TrimSpace(job.TargetCanonicalPath) {
				oldPath := strings.TrimSpace(job.TargetCanonicalPath)
				fmt.Printf("[ScoutEngine] Early canonicalization/dedup: %s -> %s\n", oldPath, effectiveCanonicalPath)
				trace.Log(ctx, "scout_exec", "scout.early_canonicalization", map[string]any{
					"tenantId":           strings.TrimSpace(job.TenantID),
					"projectId":          strings.TrimSpace(job.ProjectID),
					"lensId":             strings.TrimSpace(lensID),
					"jobId":              strings.TrimSpace(job.ID),
					"fromCanonicalPath":  oldPath,
					"toCanonicalPath":    effectiveCanonicalPath,
					"foundExisting":      resolved.ExistingFragment != nil,
					"normalizationSteps": resolved.NormalizationSteps,
				})

				// Update job with canonicalized path
				job.TargetCanonicalPath = effectiveCanonicalPath
				if job.State == nil {
					job.State = make(map[string]interface{})
				}
				if _, ok := job.State["aliasCanonicalPath"]; !ok && oldPath != "" {
					job.State["aliasCanonicalPath"] = oldPath
				}
				job.State["targetCanonicalPath"] = effectiveCanonicalPath
				_ = e.scoutingJobsRepo.Update(ctx, job)
				// Update lens scope so UI/graph doesn't keep alias paths.
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldPath, effectiveCanonicalPath)
			}
		} else {
			// Fallback to original path if resolution fails
			effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
		}
	} else {
		effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
	}

	// Domain mode: derive from intent-resolved domains (contract types), path, lens scope/viewConfig, or specialties.
	// Used by Scout extraction/decomposition and by composable Research/Critic.
	e.ensureJobDomainMode(ctx, job, effectiveCanonicalPath)
	domainModesForScout := getJobDomainModes(job)
	domainModesForComposable := domainModesForScout
	if !policy.ScoutComposablePipelineEnabled() {
		domainModesForComposable = nil
	}
	// FRESHNESS CHECK: Проверяем, есть ли свежие данные
	// Используем уже канонизированный и дедуплицированный путь
	// Это предотвращает повторные дорогие поиски, если данные недавно обновлялись (волны/затухание)
	var sinceFacts *time.Time
	var existingFragment *models.HorizonFragment

	// If resolver found an existing fragment, use it directly
	if resolvedPath != nil && resolvedPath.ExistingFragment != nil {
		existingFragment = resolvedPath.ExistingFragment
	} else {
		// Otherwise check by canonical path
		if existing, err := e.fragmentsRepo.GetByCanonicalPath(ctx, effectiveCanonicalPath); err == nil && existing != nil {
			existingFragment = existing
		}
	}

	// Domain jobs are executed by explicit pipeline passes (base_enrichment/strategic_projection).
	if passName, ok := e.resolvePipelinePassForJob(ctx, job, effectiveCanonicalPath); ok {
		if err := e.runPipelinePass(ctx, job, passName, effectiveCanonicalPath, existingFragment); err != nil {
			return err
		}
		return nil
	}

	if existingFragment != nil {
		freshnessThreshold := 1 * time.Hour // Для теста. В проде может быть конфигурируемо.
		isFresh := !existingFragment.Metadata.LastVerifiedAt.IsZero() && time.Since(existingFragment.Metadata.LastVerifiedAt) < freshnessThreshold
		// Watermark for facts: prefer dedicated LastFactsSeenAt, fall back to LastVerifiedAt.
		if !existingFragment.Metadata.LastFactsSeenAt.IsZero() {
			sinceFacts = &existingFragment.Metadata.LastFactsSeenAt
		} else if !existingFragment.Metadata.LastVerifiedAt.IsZero() {
			sinceFacts = &existingFragment.Metadata.LastVerifiedAt
		}
		trace.Log(ctx, "scout_exec", "scout.freshness.checked", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"fragmentId":          strings.TrimSpace(existingFragment.ID),
			"isFresh":             isFresh,
			"epistemicStatus":     string(existingFragment.Metadata.EpistemicStatus),
			"lastVerifiedAt": func() string {
				if existingFragment.Metadata.LastVerifiedAt.IsZero() {
					return ""
				}
				return existingFragment.Metadata.LastVerifiedAt.UTC().Format(time.RFC3339Nano)
			}(),
			"lastFactsSeenAt": func() string {
				if existingFragment.Metadata.LastFactsSeenAt.IsZero() {
					return ""
				}
				return existingFragment.Metadata.LastFactsSeenAt.UTC().Format(time.RFC3339Nano)
			}(),
		})

		// FAST PATH: if we have fresh facts and they strongly match the fragment, apply deterministically without LLM.
		var facts []*coreModels.OntologyFact
		if e.planner != nil && e.planner.factsRepo != nil && sinceFacts != nil {
			var err error
			facts, err = e.planner.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, effectiveCanonicalPath, sinceFacts.UTC(), 200)
			if err == nil && len(facts) > 0 {
				trace.Log(ctx, "scout_exec", "scout.fast_path.facts_found", map[string]any{
					"tenantId":            strings.TrimSpace(job.TenantID),
					"projectId":           strings.TrimSpace(job.ProjectID),
					"lensId":              strings.TrimSpace(lensID),
					"jobId":               strings.TrimSpace(job.ID),
					"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
					"factsN":              len(facts),
				})
				applier := &FactApplier{vertex: e.planner.vertexSearchClient}
				// Thresholds are conservative; can be tuned later or moved into policy doc.
				applyRes, err := applier.TryApplyFactsFastPath(ctx, e.fragmentsRepo, existingFragment, facts, 0.70 /*relevance*/, 0.80 /*fact confidence*/)
				if err == nil && applyRes != nil && applyRes.Applied {
					trace.Log(ctx, "scout_exec", "scout.fast_path.applied", map[string]any{
						"tenantId":            strings.TrimSpace(job.TenantID),
						"projectId":           strings.TrimSpace(job.ProjectID),
						"lensId":              strings.TrimSpace(lensID),
						"jobId":               strings.TrimSpace(job.ID),
						"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
						"appliedFacts":        applyRes.AppliedFacts,
						"maxRelevance":        applyRes.MaxRelevance,
					})
					fmt.Printf("[ScoutEngine] Fast-path applied facts for %s: appliedFacts=%d maxRelevance=%.3f\n",
						job.TargetCanonicalPath, applyRes.AppliedFacts, applyRes.MaxRelevance)

					// NOTE: Signal creation (event → 12-axis signals → propagation) is a separate Signaling Pipeline.
					// Scout only populates the graph (facts, fragments, edges). Signaling will be built separately.

					// Dimension propagation: for applied facts, map sphere/location to dimension fragments and trigger waves.
					if err := e.applyDimensionWavesFromAppliedFacts(ctx, existingFragment, facts, applyRes.AppliedFactIDs); err != nil {
						fmt.Printf("[ScoutEngine] applyDimensionWavesFromAppliedFacts failed (best-effort): jobId=%s fragmentId=%s target=%s err=%v\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(existingFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
					}
					// Trigger propagation for deterministic updates as well (non-blocking).
					if e.propagationTrigger != nil {
						fid := existingFragment.ID
						runAsyncSafe("deterministic_metric_propagation", func() {
							propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
							defer cancel()
							if err := e.propagationTrigger(propCtx, fid); err != nil {
								fmt.Printf("Propagation trigger failed for fragment %s: %v\n", fid, err)
							}
						})
					}
					if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "fast_path_applied_facts"); err != nil {
						fmt.Printf("[ScoutEngine] Failed to transition job to VERIFIED on fast-path (best-effort): jobId=%s target=%s err=%v\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
					}
					// Emit completion + node upsert for UI.
					e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
						"tenantId":        job.TenantID,
						"projectId":       projectID,
						"lensId":          lensID,
						"jobId":           job.ID,
						"fragmentId":      existingFragment.ID,
						"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
						"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
						"confidence":      1.0,
					})
					// Check if all jobs are completed
					e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, projectID, lensID)
					e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
						"tenantId":        job.TenantID,
						"projectId":       projectID,
						"lensId":          lensID,
						"fragmentId":      existingFragment.ID,
						"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
						"status":          "verified",
						"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
						"version":         existingFragment.Version,
					})
					return nil
				}
			}
		}

		// If fragment is fresh AND there are no new facts since watermark, skip expensive scouting.
		// IMPORTANT: do NOT skip Watchdog-driven jobs (they carry explicit SourceIDs and are triggered by content changes).
		if isFresh && len(facts) == 0 && len(job.SourceIDs) == 0 {
			fmt.Printf("[ScoutEngine] Data for %s is fresh (verified %v ago) and no new facts. Skipping active scouting.\n",
				job.TargetCanonicalPath, time.Since(existingFragment.Metadata.LastVerifiedAt))
			_ = e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "fresh_no_new_facts")
			e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
				"tenantId":        job.TenantID,
				"projectId":       projectID,
				"lensId":          lensID,
				"jobId":           job.ID,
				"fragmentId":      existingFragment.ID,
				"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
				"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
				"confidence":      1.0,
			})
			// Check if all jobs are completed
			e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, projectID, lensID)
			return nil
		}
	}

	// PLANNING: Планирование источников
	if err := e.transition(ctx, job, models.ScoutingJobStatePlanning, 0.1, "planning"); err != nil {
		return fmt.Errorf("failed to transition to PLANNING: %w", err)
	}
	e.logLine(ctx, job, "info", fmt.Sprintf("> planning sources for %s", strings.TrimSpace(job.TargetCanonicalPath)))

	var sourcePlan *SourcePlan

	// ОПТИМИЗАЦИЯ: Если job имеет конкретные sourceIDs (от Watchdog), используем их напрямую.
	// Это избегает дорогого PLANNING (Vertex AI Search + Firestore queries) для Watchdog-driven jobs.
	// Watchdog уже обнаружил изменения в конкретных источниках, нет смысла переопределять их.
	if len(job.SourceIDs) > 0 {
		fmt.Printf("[ScoutEngine] Using explicit sourceIDs from job (Watchdog-driven): %d sources\n", len(job.SourceIDs))
		e.logLine(ctx, job, "info", fmt.Sprintf("> using explicit sourceIDs (%d) (watchdog-driven)", len(job.SourceIDs)))
		sources := make([]*coreModels.DataSource, 0, len(job.SourceIDs))
		for _, sourceID := range job.SourceIDs {
			if e.planner != nil && e.planner.dataSourcesRepo != nil {
				if source, err := e.planner.dataSourcesRepo.Get(ctx, sourceID); err == nil && source != nil {
					sources = append(sources, source)
				}
			}
		}
		sourcePlan = &SourcePlan{
			DataSourceIDs: job.SourceIDs,
			Strategy:      job.ScoutingStrategy,
			Sources:       sources,
		}
	} else {
		// Полное планирование только если sourceIDs не указаны (Lens-driven jobs)
		fmt.Printf("[ScoutEngine] Running full planning for %s (Lens-driven)\n", job.TargetCanonicalPath)
		e.logLine(ctx, job, "info", "> running full planning (lens-driven)")
		var err error
		// Optional focus query from Lens/ViewConfig (stored in job.State).
		searchQuery := ""
		if job.State != nil {
			if v, ok := job.State["focus"].(string); ok && strings.TrimSpace(v) != "" {
				searchQuery = strings.TrimSpace(v)
			} else if v, ok := job.State["searchQuery"].(string); ok && strings.TrimSpace(v) != "" {
				// Legacy/experimental alias.
				searchQuery = strings.TrimSpace(v)
			}
			// Optional free-form keywords (not necessarily canonical paths).
			if v, ok := job.State["keywords"]; ok {
				switch t := v.(type) {
				case []string:
					for _, s := range t {
						s = strings.TrimSpace(s)
						if s == "" {
							continue
						}
						if searchQuery != "" {
							searchQuery += " "
						}
						searchQuery += s
					}
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							s = strings.TrimSpace(s)
							if s == "" {
								continue
							}
							if searchQuery != "" {
								searchQuery += " "
							}
							searchQuery += s
						}
					}
				case string:
					s := strings.TrimSpace(t)
					if s != "" {
						if searchQuery != "" {
							searchQuery += " "
						}
						searchQuery += s
					}
				}
			}
			// If caller also provided spherePaths, treat them as additional query tokens.
			if v, ok := job.State["spherePaths"]; ok {
				switch t := v.(type) {
				case []string:
					for _, s := range t {
						s = strings.TrimSpace(s)
						if s != "" {
							if searchQuery != "" {
								searchQuery += " "
							}
							searchQuery += s
						}
					}
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							s = strings.TrimSpace(s)
							if s != "" {
								if searchQuery != "" {
									searchQuery += " "
								}
								searchQuery += s
							}
						}
					}
				case string:
					s := strings.TrimSpace(t)
					if s != "" {
						if searchQuery != "" {
							searchQuery += " "
						}
						searchQuery += s
					}
				}
			}
		}
		sourcePlan, err = e.planner.PlanSources(ctx, tenantID, projectID, job.TargetCanonicalPath, models.ScoutingStrategy(job.ScoutingStrategy), sinceFacts, searchQuery)
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "planning_failed")
			return fmt.Errorf("failed to plan sources: %w", err)
		}
	}

	// EXECUTION: Выполнение запросов к источникам
	if err := e.transition(ctx, job, models.ScoutingJobStateExecution, 0.25, "execution"); err != nil {
		return fmt.Errorf("failed to transition to EXECUTION: %w", err)
	}
	e.logLine(ctx, job, "info", "> executing sources")
	trace.Log(ctx, "scout_exec", "scout.execution.start", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(lensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"plannedSourcesN": func() int {
			if sourcePlan == nil {
				return 0
			}
			return len(sourcePlan.Sources)
		}(),
		"strategy": strings.TrimSpace(job.ScoutingStrategy),
	})

	var aggregatedContent string
	var allSourceIDs []string
	var allDocumentIDs []string
	var sourceResults []*ExecuteDataSourceResult
	seenSourceContentHashes := map[string]struct{}{}

	// Получаем источники из плана
	for _, source := range sourcePlan.Sources {
		if source != nil {
			e.logLine(ctx, job, "info", fmt.Sprintf("> fetching source %s (%s)", strings.TrimSpace(source.ID), strings.TrimSpace(source.Kind)))
		}
		result, err := e.executor.ExecuteDataSource(ctx, source)
		if err != nil {
			// Логируем ошибку, но продолжаем с другими источниками
			fmt.Printf("[ScoutEngine] Failed to execute data source (id=%s, kind=%s): %v\n", source.ID, source.Kind, err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> source failed %s (%s): %v", strings.TrimSpace(source.ID), strings.TrimSpace(source.Kind), err))
			continue
		}
		if strings.TrimSpace(result.Content) != "" {
			contentHash := trace.HashText(strings.TrimSpace(result.Content))
			if contentHash != "" {
				if _, seen := seenSourceContentHashes[contentHash]; !seen {
					seenSourceContentHashes[contentHash] = struct{}{}
					if block := buildSourceContentBlock(result); strings.TrimSpace(block) != "" {
						aggregatedContent = appendAggregatedContentBlock(aggregatedContent, block, maxAggregatedContentChars)
						sourceResults = append(sourceResults, result)
					}
				}
			} else {
				if block := buildSourceContentBlock(result); strings.TrimSpace(block) != "" {
					aggregatedContent = appendAggregatedContentBlock(aggregatedContent, block, maxAggregatedContentChars)
					sourceResults = append(sourceResults, result)
				}
			}
		}
		allSourceIDs = append(allSourceIDs, result.SourceIDs...)
		allDocumentIDs = append(allDocumentIDs, result.DocumentIDs...)
	}
	trace.Log(ctx, "scout_exec", "scout.execution.done", map[string]any{
		"tenantId":              strings.TrimSpace(job.TenantID),
		"projectId":             strings.TrimSpace(job.ProjectID),
		"lensId":                strings.TrimSpace(lensID),
		"jobId":                 strings.TrimSpace(job.ID),
		"targetCanonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
		"aggregatedContentLen":  len(aggregatedContent),
		"aggregatedContentSha1": trace.HashText(aggregatedContent),
		"sourceResultsN":        len(sourceResults),
		"sourceIdsN":            len(allSourceIDs),
		"documentIdsN":          len(allDocumentIDs),
	})

	// Hybrid internal-content fallback:
	// If source/doc aggregation is empty, try deterministic graph-backed context
	// (fragments + edges + facts) before switching to full web research.
	if strings.TrimSpace(aggregatedContent) == "" {
		if graphCtx := e.buildGraphBackedInternalContext(ctx, tenantID, job.TargetCanonicalPath); strings.TrimSpace(graphCtx) != "" {
			aggregatedContent = graphCtx
			e.logLine(ctx, job, "info", "> no source content; using graph-backed internal context")
			trace.Log(ctx, "scout_exec", "scout.internal_context.graph_fallback", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(lensID),
				"jobId":               strings.TrimSpace(job.ID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"contextLen":          len(aggregatedContent),
				"contextSha1":         trace.HashText(aggregatedContent),
			})
		}
	}

	var scoutResult *agents.ScoutResult
	var extractedFragment *models.HorizonFragment
	var validationResult *agents.ValidationResult
	var groundingEvidence *ai.GroundingEvidence

	if aggregatedContent == "" {
		fmt.Printf("[ScoutEngine] No internal content for %s. Initiating full research.\n", job.TargetCanonicalPath)
		e.logLine(ctx, job, "info", "> no internal content; initiating full research")
		trace.Log(ctx, "scout_research", "scout.no_internal_content", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"plannedSourcesN": func() int {
				if sourcePlan == nil {
					return 0
				}
				return len(sourcePlan.Sources)
			}(),
		})

		// Choose a sensible default type from canonicalPath prefix (no LLM needed for this deterministic mapping).
		defaultType := models.FragmentTypeOrganization
		switch {
		case strings.HasPrefix(job.TargetCanonicalPath, "/domain/"):
			defaultType = models.FragmentTypeDomain
		case strings.HasPrefix(job.TargetCanonicalPath, "/product/"):
			defaultType = models.FragmentTypeProduct
		case strings.HasPrefix(job.TargetCanonicalPath, "/program/"):
			defaultType = models.FragmentTypeProgram
		case strings.HasPrefix(job.TargetCanonicalPath, "/market/"):
			defaultType = models.FragmentTypeMarket
		case strings.HasPrefix(job.TargetCanonicalPath, "/fac/"):
			defaultType = models.FragmentTypeFacility
		case strings.HasPrefix(job.TargetCanonicalPath, "/loc/") || strings.HasPrefix(job.TargetCanonicalPath, "/region/"):
			defaultType = models.FragmentTypeGeoRegion
		case strings.HasPrefix(job.TargetCanonicalPath, "/event/"):
			defaultType = models.FragmentTypeEvent
		case strings.HasPrefix(job.TargetCanonicalPath, "/gov/"):
			// Government bodies are modeled as ORGANIZATION fragments, but use /gov/{iso2}/{slug} paths to avoid collisions.
			defaultType = models.FragmentTypeOrganization
		case strings.HasPrefix(job.TargetCanonicalPath, "/org/"):
			defaultType = models.FragmentTypeOrganization
		case strings.HasPrefix(job.TargetCanonicalPath, "/person/"):
			defaultType = models.FragmentTypePerson
		case strings.HasPrefix(job.TargetCanonicalPath, "/media/"):
			defaultType = models.FragmentTypeMediaChannel
		}

		// Создаем базовый фрагмент
		extractedFragment = &models.HorizonFragment{
			CanonicalPath: job.TargetCanonicalPath,
			TenantID:      tenantID,
			Type:          defaultType,
			State:         make(map[string]models.MetricValue),
			Governance: models.Governance{
				Owner:  "SYSTEM",
				Status: "ACTIVE",
			},
			Metadata: models.FragmentMetadata{
				CreatedAt:       time.Now(),
				LastUpdatedAt:   time.Now(),
				EpistemicStatus: models.EpistemicStatusLowConfidence,
			},
		}

		scoutResult = &agents.ScoutResult{
			Fragment:  *extractedFragment,
			Relations: []agents.DiscoveredRelation{},
		}

		// Пытаемся определить таксономию даже без контента
		entityPath, sectorPath, err := e.taxonomyClassifier.ClassifyTaxonomy(ctx, job.TargetCanonicalPath)
		if err == nil {
			extractedFragment.EntityPath = entityPath
			extractedFragment.SectorPath = sectorPath
		}

		// Формируем результат валидации, требующий полного исследования
		validationResult = &agents.ValidationResult{
			IsValid:    true,
			Confidence: 0.1,
			MissingInfo: []string{
				fmt.Sprintf("Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships.", job.TargetCanonicalPath),
			},
		}
		// MARKET: When internal sources are empty, we must explicitly seed market sizing/concentration questions.
		// Critic is not run in this branch, so without these prompts market metrics are often never populated.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/market/") || defaultType == models.FragmentTypeMarket {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("What is the market domain and vertical for %s? Return marketDomain (finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate) and marketVertical (specific vertical) with sources.", job.TargetCanonicalPath),
				fmt.Sprintf("What is the TAM (total addressable market) in USD for %s (latest available year)? Provide value (number), year, and sources.", job.TargetCanonicalPath),
				fmt.Sprintf("What is the market CAGR percent for %s (provide period, e.g. 2024-2029)? Provide percent and sources.", job.TargetCanonicalPath),
				fmt.Sprintf("What is the market concentration for %s? Provide CR4 (top-4 share percent) and/or HHI (0..10000) with sources.", job.TargetCanonicalPath),
				fmt.Sprintf("Who are the key players in %s and who dominates? Provide top players and (if available) market shares.", job.TargetCanonicalPath),
			)
		}
		// Always request ORG/FACILITY classifiers and governance facts when internal sources are empty.
		// Critic is not run in this branch, so we seed missingInfo explicitly for grounded research.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/org/") ||
			strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/") ||
			strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/fac/") {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Who provides external assurance to %s? Auditor, certifier, or regulator. Use ASSURES(assurance_type:audit|certification|regulatory|inspection). Provide assurer and source.", job.TargetCanonicalPath),
				fmt.Sprintf("[HIGH PRIORITY] What industry/sector does %s operate in? Return marketSegments as array of paths /segment/{domain}/{vertical}/{geo}. Domains: finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate|public-sector|professional-services. For government use public-sector domain. Geo: ISO2 lowercase or regional bucket (global, mena, emea, apac, latam, na, gcc, eu, asean, nordics, benelux, anz, cis, ssa, usmca, nafta). Example: /segment/technology/semiconductors/tw", job.TargetCanonicalPath),
				fmt.Sprintf("[HIGH PRIORITY] What is the size of %s? Find employee count or revenue to determine orgSize: sme (<500 emp or <$100M rev), le (500-5000 emp or $100M-$1B), enterprise (5000-50000 emp or $1B-$10B), mega (>50000 emp or >$10B). For government: federal G20 ministries=enterprise/mega, regional=le/enterprise. Use 'na' only if truly unknown.", job.TargetCanonicalPath),
			)
		}
		// If a gov path uses /gov/na/... we MUST resolve jurisdiction to avoid cross-country collisions.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/na/") {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Which country/jurisdiction does %s belong to? Replace /gov/na/... with /gov/{iso2}/... based on evidence.", job.TargetCanonicalPath),
			)
		}
		// If an org path is legacy (/org/{slug}) or uses /org/na/... we should try to resolve the country of origin.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/org/na/") || isLegacyOrgPath(strings.TrimSpace(job.TargetCanonicalPath)) {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("What is the country of origin / primary jurisdiction for %s? Prefer /org/{iso2}/{slug} canonical path. If unknown, keep /org/na/...", job.TargetCanonicalPath),
			)
		}
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/person/") {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Who are the direct public relatives (spouse/children/parents/siblings) of %s? Output RELATIVE_OF links with sources.", job.TargetCanonicalPath),
				fmt.Sprintf("Who are the publicly known close associates of %s (business partners/friends/classmates)? Output RELATED_TO links with evidence and sources.", job.TargetCanonicalPath),
			)
		}
		// Add specialty-specific research prompts when internal sources are empty (Critic is not run in this branch).
		if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyManagement) {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Who are the executive leaders (CEO, CFO, COO, CTO/CIO, CMO, CHRO, President/Managing Director, Chair) of %s? Provide names and roles with sources.", job.TargetCanonicalPath),
			)
		}
		if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyProductsAndServices) {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("What products and services does %s produce/offer? Provide a canonical list of outputs suitable for /product/... paths.", job.TargetCanonicalPath),
				fmt.Sprintf("What key inputs/materials/components does %s consume? Provide a canonical list of inputs suitable for /product/... paths.", job.TargetCanonicalPath),
			)
			// For product nodes, also request upstream/downstream transformations.
			if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/product/") {
				validationResult.MissingInfo = append(validationResult.MissingInfo,
					fmt.Sprintf("What are the upstream materials and downstream derived products for %s (produced from / used to produce)? Return TRANSFORMS_INTO relationships between products.", job.TargetCanonicalPath),
				)
			}
		}
	} else {
		// EXTRACTION: Извлечение данных через Scout Agent
		if err := e.transition(ctx, job, models.ScoutingJobStateExtraction, 0.5, "extraction"); err != nil {
			return fmt.Errorf("failed to transition to EXTRACTION: %w", err)
		}
		e.logLine(ctx, job, "info", "> extracting structured data")

		// Phase 0 (structural extraction): extract fragment identity and basic relations (LOCATED_IN, OPERATES_IN; optional PRODUCES/CONSUMES).
		// Определяем entityPath и sectorPath из targetCanonicalPath через LLM
		entityPath, sectorPath, err := e.taxonomyClassifier.ClassifyTaxonomy(ctx, job.TargetCanonicalPath)
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "taxonomy_failed")
			return fmt.Errorf("failed to classify taxonomy: %w", err)
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("> taxonomy classified: entityPath=%s sectorPath=%s", entityPath, sectorPath))

		scoutResult, groundingEvidence, err = e.scoutAgent.ExtractFragmentDataWithGrounding(ctx, aggregatedContent, job.TargetCanonicalPath, entityPath, sectorPath, specialties, chainContextForPrompt, domainModesForScout)
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "extraction_failed")
			return fmt.Errorf("failed to extract fragment data: %w", err)
		}
		if scoutResult != nil {
			scoutResult.Relations = filterDiscoveredRelationsByDomainModes(scoutResult.Relations, domainModesForScout)
			scoutResult.Relations = filterDiscoveredRelationsByFacilityQualifier(scoutResult.Relations, &scoutResult.Fragment, job.TargetCanonicalPath)
			if !supplyChainsEnabled(job.TargetCanonicalPath, scoutResult.Fragment.Type, domainModesForScout, specialties) {
				scoutResult.SupplyChains = nil
			}
		}
		if internalFirstGroundingEnabled() {
			if groundingEvidence != nil {
				logGroundingInfof("scout extraction grounding: tenantId=%s projectId=%s lensId=%s jobId=%s target=%s datastore=%v(%d) web=%v(%d)",
					strings.TrimSpace(job.TenantID),
					strings.TrimSpace(job.ProjectID),
					strings.TrimSpace(job.LensID),
					strings.TrimSpace(job.ID),
					strings.TrimSpace(job.TargetCanonicalPath),
					groundingEvidence.HasDatastoreResults,
					len(groundingEvidence.DatastoreFragmentIDs),
					groundingEvidence.HasWebResults,
					len(groundingEvidence.WebSources),
				)
				applyGroundingEvidenceToJobState(job, groundingEvidence, allSourceIDs, allDocumentIDs)
				if job.State != nil {
					if originMode := parseStateString(job.State[stateKeyOriginMode]); strings.TrimSpace(originMode) != "" {
						e.emitGroundingCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_grounding_origin_mode_total", map[string]string{
							"mode": strings.TrimSpace(originMode),
						}, map[string]int64{"count": 1})
					}
				}
			} else if requireGroundingProvenanceEnabled() {
				logGroundingWarnf("missing grounding evidence on extraction: jobId=%s target=%s", strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath))
			}
		}
		extractedFragment = &scoutResult.Fragment
		normalizeAndSetFragmentObjectQualifier(extractedFragment, job.TargetCanonicalPath)

		// Phase 2 (domain validation / critic): first validation — CriticPipeline when domain mode set, else monolithic prompt.
		// VALIDATION: Валидация через Critic Agent
		if err := e.transition(ctx, job, models.ScoutingJobStateValidation, 0.7, "validation"); err != nil {
			return fmt.Errorf("failed to transition to VALIDATION: %w", err)
		}
		e.logLine(ctx, job, "info", "> critic validating extracted data")

		// Provide Critic with a compact view of the existing hierarchy skeleton for MARKET/GOV targets,
		// so it can explicitly request "build up/down" actions as missingInfo without blocking the pipeline.
		graphContext := ""
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/market/") || strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/") {
			graphContext = e.getSkeletonContextCached(ctx, job.TargetCanonicalPath)
		}
		// First validation pass: use rule-based quick validation for non-general modes.
		// This avoids an LLM call; missingInfo is generated deterministically from structural gaps.
		validationResult = agents.RuleBasedQuickValidation(extractedFragment, scoutResult.Relations, scoutResult.SupplyChains, domainModesForComposable)
		if validationResult == nil {
			// General mode or unknown: fall through to LLM-based critic.
			validationResult, err = e.criticAgent.ValidateWithGraphContext(ctx, extractedFragment, scoutResult.Relations, scoutResult.SupplyChains, criticSourceContent(aggregatedContent), specialties, graphContext, domainModesForComposable)
		}
		if err != nil {
			// Логируем ошибку, но не останавливаем процесс (fail-open с низким confidence)
			fmt.Printf("[ScoutEngine] critic validation error (fail-open): jobId=%s target=%s err=%v\n",
				strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> critic validation error (fail-open): %v", err))
			validationResult = &agents.ValidationResult{
				IsValid:    true,
				Confidence: 0.5,
			}
		}
	}

	if extractedFragment != nil {
		normalizeAndSetFragmentObjectQualifier(extractedFragment, job.TargetCanonicalPath)
	}

	// PRODUCT CANONICALIZATION MISSING INFO (best-effort):
	// If product path is flat/short or acronym-like, request explicit disambiguation and canonicalization.
	if validationResult != nil && extractedFragment != nil && shouldRequestProductCanonicalization(extractedFragment) {
		var rels []agents.DiscoveredRelation
		if scoutResult != nil {
			rels = scoutResult.Relations
		}
		question := buildProductCanonicalizationMissingInfo(extractedFragment, job, rels)
		validationResult.MissingInfo = appendMissingInfoUnique(validationResult.MissingInfo, question)
	}

	// ORG CANONICALIZATION MISSING INFO (best-effort):
	// If org path lacks legal form or jurisdiction clarity, request explicit canonicalization.
	if validationResult != nil && extractedFragment != nil && shouldRequestOrgCanonicalization(extractedFragment) {
		var rels []agents.DiscoveredRelation
		if scoutResult != nil {
			rels = scoutResult.Relations
		}
		question := buildOrgCanonicalizationMissingInfo(extractedFragment, job, rels)
		validationResult.MissingInfo = appendMissingInfoUnique(validationResult.MissingInfo, question)
	}

	// RELATED ENTITIES CANONICALIZATION (best-effort):
	// If relations include org/product targets with non-canonical paths, request clarification.
	if validationResult != nil && scoutResult != nil {
		for _, q := range buildRelationMissingInfo(scoutResult.Relations, 6) {
			validationResult.MissingInfo = appendMissingInfoUnique(validationResult.MissingInfo, q)
		}
	}
	if validationResult != nil {
		beforeMissingInfo := len(validationResult.MissingInfo)
		facilityQualifier := resolveFacilityQualifierSemantic(extractedFragment, job.TargetCanonicalPath)
		if facilityQualifier == facilityQualifierSemanticNone {
			facilityQualifier = resolveFacilityQualifierSemantic(existingFragment, job.TargetCanonicalPath)
		}
		for _, q := range buildFacilityQualifierMissingInfo(job.TargetCanonicalPath, facilityQualifier) {
			validationResult.MissingInfo = appendMissingInfoUnique(validationResult.MissingInfo, q)
		}
		validationResult.MissingInfo = filterMissingInfoByFacilityQualifier(validationResult.MissingInfo, facilityQualifier)
		validationResult.MissingInfo = filterMissingInfoByFragmentRelevance(extractedFragment, validationResult.MissingInfo)
		validationResult.MissingInfo = limitMissingInfoQuestions(validationResult.MissingInfo, missingInfoLimitForFragment(extractedFragment))
		if afterMissingInfo := len(validationResult.MissingInfo); afterMissingInfo != beforeMissingInfo {
			e.logLine(ctx, job, "info", fmt.Sprintf("> missingInfo normalized (%s): %d -> %d", RuntimeContractMissingInfoRelevance, beforeMissingInfo, afterMissingInfo))
		}
	}

	// MARKET METRICS NORMALIZATION (best-effort):
	// Research currently may emit legacy keys like "market_tam_usd". We canonicalize them into /metric/market/... so:
	// - Critic can reliably require/check them
	// - later systems can aggregate/query them consistently
	// - they are not "lost" due to inconsistent key naming.
	if extractedFragment != nil && (extractedFragment.Type == models.FragmentTypeMarket || strings.HasPrefix(strings.TrimSpace(extractedFragment.CanonicalPath), "/market/")) {
		normalizeMarketMetricKeys(extractedFragment)
	}

	researchSucceeded := false
	// RESEARCH: Дообогащение данных через Research Agent
	if len(validationResult.MissingInfo) > 0 {
		// Filter out missingInfo items that are exhausted or in cooldown to prevent infinite loops.
		// This uses the horizon_missing_info_status collection to track attempts per question.
		if e.missingInfoStatusRepo != nil {
			searchable, filteredCount, err := e.missingInfoStatusRepo.FilterSearchable(ctx, extractedFragment.ID, validationResult.MissingInfo)
			if err != nil {
				// Fail-open: keep missingInfo list, but log loudly so issues are visible.
				fmt.Printf("[ScoutEngine] missingInfo filter error (fail-open): jobId=%s fragmentId=%s target=%s err=%v\n",
					strings.TrimSpace(job.ID), strings.TrimSpace(extractedFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
				e.logLine(ctx, job, "warn", fmt.Sprintf("> missingInfo filter error (fail-open): %v", err))
			} else {
				if filteredCount > 0 {
					fmt.Printf("[ScoutEngine] Filtered %d exhausted/cooldown missingInfo items for fragment %s\n", filteredCount, extractedFragment.ID)
					e.logLine(ctx, job, "info", fmt.Sprintf("> filtered %d exhausted/cooldown missingInfo items", filteredCount))
				}
				validationResult.MissingInfo = searchable
			}
		}
		// Check if there are still items to research after filtering
		if len(validationResult.MissingInfo) == 0 {
			goto SKIP_RESEARCH
		}
		// RuntimeContractBudgetAdaptiveFanout:
		// do not hard-stop on budget overflow; reduce in-flight research workload adaptively.
		researchMissingInfo := validationResult.MissingInfo
		if scale, snap, ok := e.computeRuntimeBudgetAdaptation(ctx, tenantID, lensID, cascadeID); ok && scale < 1.0 {
			before := len(researchMissingInfo)
			capN := scaleCapByBudget(before, 1, scale)
			if capN < before {
				researchMissingInfo = researchMissingInfo[:capN]
				e.logLine(ctx, job, "info", fmt.Sprintf("> research budget adapted (%s): missingInfo %d -> %d (%s)",
					RuntimeContractBudgetAdaptiveFanout, before, capN, snap.summary()))
			}
		}
		fmt.Printf("[ScoutEngine] Starting research for job %s, missing info: %v\n", job.ID, researchMissingInfo)
		e.logLine(ctx, job, "info", fmt.Sprintf("> research started (missing=%d)", len(researchMissingInfo)))
		researchCtx := ai.WithUsageContext(ctx, tenantID, "SYSTEM", projectID, "horizon.research", lensID, cascadeID)
		// Pass focus signals to grounded research (best-effort).
		focus := ""
		var spherePaths []string
		var keywords []string
		if job.State != nil {
			if v, ok := job.State["focus"].(string); ok {
				focus = strings.TrimSpace(v)
			}
			if v, ok := job.State["spherePaths"]; ok {
				switch t := v.(type) {
				case []string:
					spherePaths = append(spherePaths, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							spherePaths = append(spherePaths, s)
						}
					}
				case string:
					parts := strings.Split(t, ",")
					for _, p := range parts {
						spherePaths = append(spherePaths, p)
					}
				}
			}
			if v, ok := job.State["keywords"]; ok {
				switch t := v.(type) {
				case []string:
					keywords = append(keywords, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							keywords = append(keywords, s)
						}
					}
				case string:
					parts := strings.Split(t, ",")
					for _, p := range parts {
						keywords = append(keywords, p)
					}
				}
			}
		}
		// Extract originalQuery and decompositionHints for MARKET-aware research
		originalQuery := ""
		var decompositionHints []agents.DecompositionHint
		if job.State != nil {
			if v, ok := job.State["originalQuery"].(string); ok {
				originalQuery = strings.TrimSpace(v)
			}
			if v, ok := job.State["decompositionHints"]; ok && v != nil {
				// Parse decompositionHints (may come as []interface{} from JSON)
				if dhBytes, err := json.Marshal(v); err == nil {
					if err := json.Unmarshal(dhBytes, &decompositionHints); err != nil {
						fmt.Printf("[ScoutEngine] Failed to parse decompositionHints (best-effort): jobId=%s target=%s err=%v\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
					}
				}
			}
		}
		trace.Log(ctx, "scout_research", "scout.research.start", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"missingN":            len(researchMissingInfo),
			"missingTop":          trace.StrSlice(researchMissingInfo, 6),
			"focus":               trace.Trunc(focus, 140),
			"spherePathsTop":      trace.StrSlice(spherePaths, 10),
			"keywordsTop":         trace.StrSlice(keywords, 10),
			"originalQueryLen":    len(originalQuery),
			"decompositionHintsN": len(decompositionHints),
		})
		// Collect existing entities from session graph for duplicate prevention
		var existingEntities []string
		if sg := SessionGraphFromContext(ctx); sg != nil {
			for _, frag := range sg.AllFragments() {
				if frag == nil {
					continue
				}
				path := strings.TrimSpace(frag.CanonicalPath)
				if path == "" {
					continue
				}
				label := ""
				if frag.Labels != nil {
					label = strings.TrimSpace(frag.Labels["en"])
				}
				if label == "" && frag.Identifiers != nil {
					label = strings.TrimSpace(frag.Identifiers["shortName"])
				}
				if label == "" {
					existingEntities = append(existingEntities, path)
				} else {
					existingEntities = append(existingEntities, fmt.Sprintf("%s | %s", path, label))
				}
			}
		}
		// Phase 1 (domain expansion / research): ResearchPipeline when domain mode set, else EnrichFragmentWithGrounding; allowlist applied when appending relations.
		enrichedFragment, newRelations, newSupplyChains, researchGroundingEvidence, researchRawContent, err := e.researchAgent.EnrichFragmentWithGroundingWithDomainModes(researchCtx, extractedFragment, researchMissingInfo, focus, spherePaths, keywords, specialties, chainContextForPrompt, originalQuery, decompositionHints, existingEntities, domainModesForComposable)
		if err == nil {
			researchSucceeded = true
			groundingEvidence = mergeGroundingEvidence(groundingEvidence, researchGroundingEvidence)
			newRelations = filterDiscoveredRelationsByFacilityQualifier(newRelations, extractedFragment, job.TargetCanonicalPath)
			// Apply allowlist by domain mode: only persist relations (and supply chains) allowed for this job's mode(s).
			if len(newRelations) > 0 || len(newSupplyChains) > 0 {
				allowlist := EdgeAllowlistByDomainModes(domainModesForScout)
				if allowlist != nil {
					filtered := make([]agents.DiscoveredRelation, 0, len(newRelations))
					for _, r := range newRelations {
						normalized := strings.TrimSpace(taxonomy.NormalizeRelationType(r.RelationType))
						if normalized == "" {
							continue
						}
						if AllowlistContains(allowlist, models.DependencyType(normalized)) {
							r.RelationType = normalized
							filtered = append(filtered, r)
						}
					}
					newRelations = filtered
					// Supply chains are gated separately via supplyChainsEnabled().
				}
			}
			fragTypeForSupplyChain := models.FragmentType("")
			if extractedFragment != nil {
				fragTypeForSupplyChain = extractedFragment.Type
			}
			if !supplyChainsEnabled(job.TargetCanonicalPath, fragTypeForSupplyChain, domainModesForScout, specialties) {
				newSupplyChains = nil
			}
			if researchContext := buildResearchContextBlock(researchRawContent, newRelations, newSupplyChains, researchGroundingEvidence); strings.TrimSpace(researchContext) != "" {
				aggregatedContent = appendAggregatedContentBlock(aggregatedContent, researchContext, maxAggregatedContentChars)
			}
			if internalFirstGroundingEnabled() {
				if researchGroundingEvidence != nil {
					logGroundingInfof("research grounding: tenantId=%s projectId=%s lensId=%s jobId=%s target=%s datastore=%v(%d) web=%v(%d)",
						strings.TrimSpace(job.TenantID),
						strings.TrimSpace(job.ProjectID),
						strings.TrimSpace(job.LensID),
						strings.TrimSpace(job.ID),
						strings.TrimSpace(job.TargetCanonicalPath),
						researchGroundingEvidence.HasDatastoreResults,
						len(researchGroundingEvidence.DatastoreFragmentIDs),
						researchGroundingEvidence.HasWebResults,
						len(researchGroundingEvidence.WebSources),
					)
					applyGroundingEvidenceToJobState(job, groundingEvidence, allSourceIDs, allDocumentIDs)
					if job.State != nil {
						if originMode := parseStateString(job.State[stateKeyOriginMode]); strings.TrimSpace(originMode) != "" {
							e.emitGroundingCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_grounding_origin_mode_total", map[string]string{
								"mode": strings.TrimSpace(originMode),
							}, map[string]int64{"count": 1})
						}
					}
				} else if requireGroundingProvenanceEnabled() {
					logGroundingWarnf("missing grounding evidence on research: jobId=%s target=%s", strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath))
				}
			}
			// Capture canonicalPath drift introduced by research (aliasing).
			oldCP := ""
			if extractedFragment != nil {
				oldCP = strings.TrimSpace(extractedFragment.CanonicalPath)
			}
			newCP := ""
			if enrichedFragment != nil {
				newCP = strings.TrimSpace(enrichedFragment.CanonicalPath)
			}
			extractedFragment = enrichedFragment
			normalizeAndSetFragmentObjectQualifier(extractedFragment, job.TargetCanonicalPath)
			// Best-effort: if research upgraded canonicalPath (e.g., /product/{slug} -> /product/{cat}/{type}/{stage}/{slug}),
			// propagate the rewrite into job state + lens scope so UI and in-flight execution follow the canonical path.
			if oldCP != "" && newCP != "" && oldCP != newCP {
				if job.State == nil {
					job.State = make(map[string]interface{})
				}
				if _, ok := job.State["aliasCanonicalPath"]; !ok && strings.TrimSpace(oldCP) != "" {
					job.State["aliasCanonicalPath"] = strings.TrimSpace(oldCP)
				}
				// Keep a stable "effective target" in state (used by dedup keys and downstream tooling).
				job.State["targetCanonicalPath"] = strings.TrimSpace(newCP)
				// If the job target equals the old canonical path, upgrade it so subsequent steps align.
				if strings.TrimSpace(job.TargetCanonicalPath) == strings.TrimSpace(oldCP) {
					job.TargetCanonicalPath = strings.TrimSpace(newCP)
				}
				_ = e.scoutingJobsRepo.Update(ctx, job)
				// Update lens scope so UI/graph doesn't keep alias paths.
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldCP, newCP)
				e.recordDeterministicRedirect(ctx, oldCP, newCP, "scout_research_upgrade")
				e.logLine(ctx, job, "info", fmt.Sprintf("> canonicalized canonicalPath: %s -> %s", oldCP, newCP))
			}
			if len(newRelations) > 0 {
				scoutResult.Relations = append(scoutResult.Relations, newRelations...)
				fmt.Printf("[ScoutEngine] Research found %d new relations.\n", len(newRelations))
				e.logLine(ctx, job, "info", fmt.Sprintf("> research found %d new relations", len(newRelations)))
			}
			// Append supply chains from research (if supply_chain mode enabled)
			if len(newSupplyChains) > 0 {
				scoutResult.SupplyChains = append(scoutResult.SupplyChains, newSupplyChains...)
				fmt.Printf("[ScoutEngine] Research found %d new supply chains.\n", len(newSupplyChains))
				e.logLine(ctx, job, "info", fmt.Sprintf("> research found %d new supply chains", len(newSupplyChains)))
			}
			// Обновляем LastVerifiedAt
			extractedFragment.Metadata.LastVerifiedAt = time.Now()
			trace.Log(ctx, "scout_research", "scout.research.ok", map[string]any{
				"tenantId":                    strings.TrimSpace(job.TenantID),
				"projectId":                   strings.TrimSpace(job.ProjectID),
				"lensId":                      strings.TrimSpace(lensID),
				"jobId":                       strings.TrimSpace(job.ID),
				"targetCanonicalPath":         strings.TrimSpace(job.TargetCanonicalPath),
				"fragmentCanonicalPathBefore": oldCP,
				"fragmentCanonicalPathAfter":  newCP,
				"newRelationsN":               len(newRelations),
				"newSupplyChainsN":            len(newSupplyChains),
				"aggregatedContentLen":        len(aggregatedContent),
				"aggregatedContentSha1":       trace.HashText(aggregatedContent),
				"relationsTopCanonicalPaths": func() []string {
					out := make([]string, 0, len(newRelations))
					for _, r := range newRelations {
						out = append(out, strings.TrimSpace(r.TargetCanonicalPath))
					}
					return trace.StrSlice(out, 12)
				}(),
			})
			// Record successful research attempts for resolved questions.
			// A question is considered resolved if new relations/data were found.
			if e.missingInfoStatusRepo != nil {
				foundSomething := len(newRelations) > 0 || len(newSupplyChains) > 0
				for _, q := range researchMissingInfo {
					if recErr := e.missingInfoStatusRepo.RecordAttempt(ctx, extractedFragment.ID, q, foundSomething, 0); recErr != nil {
						fmt.Printf("[ScoutEngine] missingInfo RecordAttempt failed: jobId=%s fragmentId=%s target=%s found=%v err=%v q=%q\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(extractedFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), foundSomething, recErr, trace.Trunc(q, 160))
						e.logLine(ctx, job, "warn", fmt.Sprintf("> missingInfo RecordAttempt failed: %v", recErr))
					}
				}
			}
		} else {
			fmt.Printf("[ScoutEngine] Research failed: %v\n", err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> research failed: %v", err))
			trace.Log(ctx, "scout_research", "scout.research.error", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(lensID),
				"jobId":               strings.TrimSpace(job.ID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"error":               err.Error(),
			})
			// Record failed research attempts (increment counter, apply cooldown).
			if e.missingInfoStatusRepo != nil {
				for _, q := range researchMissingInfo {
					if recErr := e.missingInfoStatusRepo.RecordAttempt(ctx, extractedFragment.ID, q, false, 0); recErr != nil {
						fmt.Printf("[ScoutEngine] missingInfo RecordAttempt failed: jobId=%s fragmentId=%s target=%s found=%v err=%v q=%q\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(extractedFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), false, recErr, trace.Trunc(q, 160))
						e.logLine(ctx, job, "warn", fmt.Sprintf("> missingInfo RecordAttempt failed: %v", recErr))
					}
				}
			}
		}
	}
SKIP_RESEARCH:

	// RE-VALIDATION: run Critic once after Research (best-effort).
	if researchSucceeded && extractedFragment != nil && validationResult != nil {
		e.logLine(ctx, job, "info", "> critic re-validating enriched data")
		graphContext := ""
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/market/") || strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/") {
			graphContext = e.getSkeletonContextCached(ctx, job.TargetCanonicalPath)
		}
		var rels []agents.DiscoveredRelation
		var chains []agents.SupplyChain
		if scoutResult != nil {
			rels = scoutResult.Relations
			chains = scoutResult.SupplyChains
		}
		// Phase 2 (domain validation / critic): re-validation after research — CriticPipeline when domain mode set.
		revalidationResult, err := e.criticAgent.ValidateWithGraphContext(ctx, extractedFragment, rels, chains, criticSourceContent(aggregatedContent), specialties, graphContext, domainModesForComposable)
		if err != nil {
			fmt.Printf("[ScoutEngine] critic re-validation error (fail-open): jobId=%s target=%s err=%v\n",
				strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> critic re-validation error (fail-open): %v", err))
		} else {
			validationResult = revalidationResult
		}
	}

	// RESOLUTION: Разрешение конфликтов если необходимо
	if validationResult != nil && validationResult.HasConflicts {
		if err := e.transition(ctx, job, models.ScoutingJobStateResolution, 0.85, "resolution"); err != nil {
			return fmt.Errorf("failed to transition to RESOLUTION: %w", err)
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("> resolver resolving %d conflicts", len(validationResult.Conflicts)))

		resolverCtx, resolverCancel := context.WithTimeout(ctx, 3*time.Minute)
		defer resolverCancel()
		resolutionResult, err := e.resolverAgent.Resolve(resolverCtx, agents.ResolutionRequest{
			ScoutData:       extractedFragment,
			CriticArguments: validationResult.Conflicts,
			SourceContent:   aggregatedContent,
		})
		if err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> resolver failed: %v", err))
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "resolution_failed")
			return fmt.Errorf("failed to resolve conflicts: %w", err)
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("> resolver verdict: %s (confidence=%.2f)", strings.TrimSpace(resolutionResult.Verdict), resolutionResult.Confidence))

		switch resolutionResult.Verdict {
		case "Rejected":
			_ = e.transition(ctx, job, models.ScoutingJobStateRejected, 1.0, "rejected")
			return fmt.Errorf("data rejected by resolver: %s", resolutionResult.Reasoning)
		case "Low_Confidence":
			if resolutionResult.ResolvedData != nil {
				if resolvedJSON, ok := resolutionResult.ResolvedData.(map[string]interface{}); ok {
					if resolvedState, ok := resolvedJSON["state"].(map[string]interface{}); ok {
						for metricName, metricValue := range extractedFragment.State {
							if resolvedMetricValue, ok := resolvedState[metricName].(map[string]interface{}); ok {
								if value, ok := resolvedMetricValue["value"].(float64); ok {
									metricValue.Value = value
									extractedFragment.State[metricName] = metricValue
								}
							}
						}
					}
				}
			}
		case "Verified":
			// Данные подтверждены, продолжаем
		}
	}

	// METRICS: Ensure standardized risk metrics are present in fragment.State.
	// Upstream extraction/research can yield facts but may omit normalized risk scores,
	// which would make Lens metrics aggregation and propagation ineffective.
	if extractedFragment.State == nil {
		extractedFragment.State = make(map[string]models.MetricValue)
	}
	{
		sourceURLs := make([]string, 0, len(sourceResults))
		for _, r := range sourceResults {
			if r != nil && strings.TrimSpace(r.SourceURL) != "" {
				sourceURLs = append(sourceURLs, strings.TrimSpace(r.SourceURL))
			}
		}
		// Optional dynamic metrics explicitly requested by caller (job.State["requestedMetrics"]).
		// Can be either []string or []any of strings; each entry can be:
		// - canonical metric path: "/metric/risk/..."
		// - domain path: "risk/..." or "/risk/..."
		// - bare name: "battery_supplier_concentration" (will be canonicalized)
		var requested []string
		if job.State != nil {
			if v, ok := job.State["requestedMetrics"]; ok {
				switch t := v.(type) {
				case []string:
					requested = append(requested, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
							requested = append(requested, s)
						}
					}
				case string:
					// Allow comma-separated convenience
					parts := strings.Split(t, ",")
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							requested = append(requested, p)
						}
					}
				}
			}
		}

		requiredMetricPaths := agents.RequiredMetricPathsForType(extractedFragment.Type, requested)
		refreshReasons := evaluateMetricsRefreshContractReasons(extractedFragment, requiredMetricPaths, time.Now().UTC())
		if len(refreshReasons) == 0 {
			e.logLine(ctx, job, "info", fmt.Sprintf("> metrics skipped (%s: fresh/complete): required=%d", RuntimeContractMetricsRefresh, len(requiredMetricPaths)))
		} else {
			var metricRelations []agents.DiscoveredRelation
			if scoutResult != nil {
				metricRelations = scoutResult.Relations
			}
			metricsAgent := agents.NewMetricsAgent(e.aiExecutor)
			e.logLine(ctx, job, "info", fmt.Sprintf("> computing risk metrics (%s; reasons=%d)", RuntimeContractMetricsRefresh, len(refreshReasons)))
			metricsCtx, metricsCancel := context.WithTimeout(ctx, 2*time.Minute)
			defer metricsCancel()
			state, err := metricsAgent.ComputeState(metricsCtx, extractedFragment, sourceURLs, aggregatedContent, metricRelations, requested)
			if err != nil {
				fmt.Printf("[ScoutEngine] MetricsAgent failed (keeping existing state, len=%d): %v\n", len(extractedFragment.State), err)
				e.logLine(ctx, job, "warn", fmt.Sprintf("> metrics failed (keeping existing): %v", err))
			} else if len(state) > 0 {
				// IMPORTANT: Do NOT replace the entire state map.
				// Research (especially for MARKET) can populate absolute market metrics (TAM/CAGR/HHI/CR4) which must be preserved.
				for k, v := range state {
					extractedFragment.State[k] = v
				}
				e.logLine(ctx, job, "info", fmt.Sprintf("> metrics computed: %d entries", len(state)))
			}
		}
	}

	// CANONICAL RETARGET:
	// If Research (or any upstream step) produced a different canonicalPath than the job started with,
	// we must continue work on THAT entity. Otherwise we end up verifying/storing data under one path
	// while repeatedly scouting another alias (infinite "no internal content" loop).
	// Note: effectiveCanonicalPath is already set from early canonicalization above
	if effectiveCanonicalPath == "" {
		effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
	}
	if extractedFragment != nil {
		if cp := strings.TrimSpace(extractedFragment.CanonicalPath); cp != "" {
			effectiveCanonicalPath = cp
		}
	}
	if effectiveCanonicalPath != "" && strings.TrimSpace(job.TargetCanonicalPath) != effectiveCanonicalPath {
		old := strings.TrimSpace(job.TargetCanonicalPath)
		trace.Log(ctx, "scout_persist", "scout.retarget.start", map[string]any{
			"tenantId":          strings.TrimSpace(job.TenantID),
			"projectId":         strings.TrimSpace(job.ProjectID),
			"lensId":            strings.TrimSpace(lensID),
			"jobId":             strings.TrimSpace(job.ID),
			"fromCanonicalPath": old,
			"toCanonicalPath":   effectiveCanonicalPath,
			"hint":              "using_canonicalPath_from_research_or_dedup",
		})
		fmt.Printf("[ScoutEngine] Retargeting job %s: %s -> %s (using canonicalPath from research/dedup)\n",
			strings.TrimSpace(job.ID), old, effectiveCanonicalPath)
		// Persist into job + state so Cloud Tasks retries execute the correct target as well.
		job.TargetCanonicalPath = effectiveCanonicalPath
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		job.State["targetCanonicalPath"] = effectiveCanonicalPath
		// Keep original path for later anti-dedup / alias cleanup (best-effort).
		if _, ok := job.State["aliasCanonicalPath"]; !ok && old != "" {
			job.State["aliasCanonicalPath"] = old
		}
		if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
			fmt.Printf("[ScoutEngine] Failed to persist job retarget (best-effort): jobId=%s from=%s to=%s err=%v\n",
				strings.TrimSpace(job.ID), old, effectiveCanonicalPath, err)
		}
		// Update lens scope so UI/graph doesn't keep alias paths.
		e.bestEffortRewriteLensScope(ctx, tenantID, lensID, old, effectiveCanonicalPath)
		e.recordDeterministicRedirect(ctx, old, effectiveCanonicalPath, "scout_retarget")
		trace.Log(ctx, "scout_persist", "scout.retarget.done", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		})
	}

	// ORG LEGACY CANONICALIZATION (self-correction):
	// Upgrade /org/{slug} or /org/na/{slug} to geo-scoped /org/{iso2}/{slug}-{legal_form} when possible.
	var orgUpgrade *orgPathUpgrade
	if extractedFragment != nil {
		orgUpgrade = e.maybeUpgradeLegacyOrgPath(ctx, extractedFragment, effectiveCanonicalPath)
	}
	if orgUpgrade == nil && existingFragment != nil {
		orgUpgrade = e.maybeUpgradeLegacyOrgPath(ctx, existingFragment, effectiveCanonicalPath)
	}
	if orgUpgrade != nil && e.fragmentsRepo != nil {
		if existing, err := e.fragmentsRepo.GetByCanonicalPath(ctx, orgUpgrade.NewPath); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
			if existingFragment == nil || existing.ID != existingFragment.ID {
				fmt.Printf("[ScoutEngine] Skipping org path upgrade due to existing fragment: old=%s new=%s existingId=%s\n",
					orgUpgrade.OldPath, orgUpgrade.NewPath, existing.ID)
				orgUpgrade = nil
			}
		}
	}
	if orgUpgrade != nil {
		old := strings.TrimSpace(orgUpgrade.OldPath)
		effectiveCanonicalPath = strings.TrimSpace(orgUpgrade.NewPath)
		job.TargetCanonicalPath = effectiveCanonicalPath
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		if _, ok := job.State["aliasCanonicalPath"]; !ok && old != "" {
			job.State["aliasCanonicalPath"] = old
		}
		job.State["targetCanonicalPath"] = effectiveCanonicalPath
		job.State["orgLegacyCanonicalPath"] = old
		job.State["orgCanonicalPathUpgradedTo"] = effectiveCanonicalPath
		if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
			fmt.Printf("[ScoutEngine] Failed to persist org path upgrade (best-effort): jobId=%s from=%s to=%s err=%v\n",
				strings.TrimSpace(job.ID), old, effectiveCanonicalPath, err)
		}
		e.bestEffortRewriteLensScope(ctx, tenantID, lensID, old, effectiveCanonicalPath)
		e.recordDeterministicRedirect(ctx, old, effectiveCanonicalPath, "org_path_upgrade")
		if extractedFragment != nil {
			extractedFragment.CanonicalPath = effectiveCanonicalPath
			// Keep semantic parts in sync so downstream canonizers can apply selective fixes.
			if parts, err := taxonomy.ParseCanonicalPathParts(effectiveCanonicalPath); err == nil && parts != nil {
				extractedFragment.CanonicalParts = parts
			}
			recordLegacyOrgPath(extractedFragment, old)
		}
		if existingFragment != nil {
			// Best-effort: also keep parts consistent on the existing fragment snapshot.
			if parts, err := taxonomy.ParseCanonicalPathParts(effectiveCanonicalPath); err == nil && parts != nil {
				existingFragment.CanonicalParts = parts
			}
			recordLegacyOrgPath(existingFragment, old)
		}
		orgLegacyPath = old
		orgCanonicalizedPath = effectiveCanonicalPath
		e.logLine(ctx, job, "info", fmt.Sprintf("> canonicalized org path: %s -> %s", old, effectiveCanonicalPath))
	}

	// VERIFIED: Создание Frame и обновление HorizonFragment
	if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "verified"); err != nil {
		return fmt.Errorf("failed to transition to VERIFIED: %w", err)
	}
	trace.Log(ctx, "scout_persist", "scout.persist.start", map[string]any{
		"tenantId":               strings.TrimSpace(job.TenantID),
		"projectId":              strings.TrimSpace(job.ProjectID),
		"lensId":                 strings.TrimSpace(lensID),
		"jobId":                  strings.TrimSpace(job.ID),
		"effectiveCanonicalPath": effectiveCanonicalPath,
		"confidence":             validationResult.Confidence,
		"relationsN": func() int {
			if scoutResult == nil {
				return 0
			}
			return len(scoutResult.Relations)
		}(),
	})

	// Извлекаем Evidence из результатов источников через LLM
	evidenceExtractor := NewEvidenceExtractorLLM(e.aiExecutor)
	evidence, err := ExtractEvidence(ctx, evidenceExtractor, sourceResults, extractedFragment.State)
	if err != nil {
		// Логируем ошибку, но продолжаем без Evidence
		fmt.Printf("[ScoutEngine] Failed to extract evidence (jobID=%s, fragmentPath=%s): %v\n", job.ID, job.TargetCanonicalPath, err)
		evidence = []models.Evidence{}
	}

	// Создаем Frame
	frame := e.frameBuilder.BuildFrame(
		tenantID,
		projectID,
		job.ID,
		effectiveCanonicalPath,
		extractedFragment.State,
		evidence,
		models.ClaimProvenance{
			SourceIDs:     allSourceIDs,
			DataSourceIDs: allSourceIDs,
			DocumentIDs:   allDocumentIDs,
			Method:        models.ExtractionMethodNLP,
			ExtractedAt:   time.Now(),
		},
		extractedFragment.KBRefs,
		validationResult.Confidence,
		allSourceIDs,
		allDocumentIDs,
	)

	frameID, err := e.framesRepo.Create(ctx, frame)
	if err != nil {
		return fmt.Errorf("failed to create frame: %w", err)
	}
	frame.ID = frameID

	// Обновляем или создаем HorizonFragment
	// Note: existingFragment may already be set from early canonicalization above
	if existingFragment == nil {
		var getErr error
		existingFragment, getErr = e.fragmentsRepo.GetByCanonicalPath(ctx, effectiveCanonicalPath)
		if getErr != nil {
			err = getErr
		}
	}
	var fragmentID string

	if err != nil || existingFragment == nil {
		// Создаем новый фрагмент
		extractedFragment.CanonicalPath = effectiveCanonicalPath
		extractedFragment.TenantID = "global"
		// Ensure we have a human-readable label even when upstream LLM produced slug-like lowercase or left labels empty.
		// This only affects display fields and does not change canonicalPath/type.
		if extractedFragment.Labels == nil {
			extractedFragment.Labels = map[string]string{}
		}
		if strings.TrimSpace(extractedFragment.Labels["en"]) == "" {
			seed := strings.TrimSpace(extractedFragment.Identifiers["legalName"])
			if seed == "" {
				seed = strings.TrimSpace(extractedFragment.Identifiers["shortName"])
			}
			extractedFragment.Labels["en"] = bestEffortDisplayLabel(seed, effectiveCanonicalPath)
		} else {
			extractedFragment.Labels["en"] = bestEffortDisplayLabel(extractedFragment.Labels["en"], effectiveCanonicalPath)
		}
		extractedFragment.Metadata.SourceFrameID = frameID
		extractedFragment.Metadata.EpistemicStatus = models.EpistemicStatusVerified
		if validationResult.Confidence < 0.7 {
			extractedFragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}
		if orgLegacyPath != "" {
			recordLegacyOrgPath(extractedFragment, orgLegacyPath)
		}

		// Market deduplication via canonical path resolver (tag-based).
		if e.canonicalPathResolver != nil && extractedFragment.Type == models.FragmentTypeMarket && len(extractedFragment.Tags) > 0 {
			meta := map[string]string{
				"tags": strings.Join(extractedFragment.Tags, ","),
			}
			if resolved, rerr := e.canonicalPathResolver.ResolveWithMetadata(ctx, extractedFragment.CanonicalPath, extractedFragment.Type, meta); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				if strings.TrimSpace(resolved.CanonicalPath) != strings.TrimSpace(extractedFragment.CanonicalPath) {
					old := strings.TrimSpace(extractedFragment.CanonicalPath)
					extractedFragment.CanonicalPath = strings.TrimSpace(resolved.CanonicalPath)
					if effectiveCanonicalPath != extractedFragment.CanonicalPath {
						effectiveCanonicalPath = extractedFragment.CanonicalPath
						job.TargetCanonicalPath = effectiveCanonicalPath
						if job.State == nil {
							job.State = map[string]interface{}{}
						}
						job.State["targetCanonicalPath"] = effectiveCanonicalPath
						if _, ok := job.State["aliasCanonicalPath"]; !ok && old != "" {
							job.State["aliasCanonicalPath"] = old
						}
						fmt.Printf("[ScoutEngine] Retargeting job %s to market dedup canonicalPath: %s -> %s\n",
							strings.TrimSpace(job.ID), old, effectiveCanonicalPath)
						_ = e.scoutingJobsRepo.Update(ctx, job)
						// Update lens scope so UI/graph doesn't keep alias paths.
						e.bestEffortRewriteLensScope(ctx, tenantID, lensID, old, effectiveCanonicalPath)
						e.recordDeterministicRedirect(ctx, old, effectiveCanonicalPath, "market_dedup")
					}
				}
			}
		}

		// Pre-create validation: check policies and deduplication
		usedExistingDuplicate := false
		if e.preCreateValidator != nil {
			preValidation, valErr := e.preCreateValidator.ValidateAndProcess(ctx, extractedFragment)
			if valErr != nil {
				fmt.Printf("[ScoutEngine] Warning: pre-create validation error: %v\n", valErr)
			} else if preValidation != nil {
				// Log warnings
				for _, w := range preValidation.Warnings {
					fmt.Printf("[ScoutEngine] Pre-create warning: %s\n", w)
				}
				// If pre-create validation normalized the canonicalPath (e.g., /loc/uae -> /loc/ae/na/na),
				// retarget the job so all subsequent steps operate on the canonical path.
				if strings.TrimSpace(extractedFragment.CanonicalPath) != "" && extractedFragment.CanonicalPath != effectiveCanonicalPath {
					old := effectiveCanonicalPath
					effectiveCanonicalPath = strings.TrimSpace(extractedFragment.CanonicalPath)
					job.TargetCanonicalPath = effectiveCanonicalPath
					if job.State == nil {
						job.State = map[string]interface{}{}
					}
					job.State["targetCanonicalPath"] = effectiveCanonicalPath
					if _, ok := job.State["aliasCanonicalPath"]; !ok && strings.TrimSpace(old) != "" {
						job.State["aliasCanonicalPath"] = strings.TrimSpace(old)
					}
					fmt.Printf("[ScoutEngine] Retargeting job %s to normalized canonicalPath: %s -> %s\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(old), effectiveCanonicalPath)
					_ = e.scoutingJobsRepo.Update(ctx, job)
					// Update lens scope so UI/graph doesn't keep alias paths.
					e.bestEffortRewriteLensScope(ctx, tenantID, lensID, old, effectiveCanonicalPath)
					e.recordDeterministicRedirect(ctx, old, effectiveCanonicalPath, "precreate_normalization")
				}
				// Only block on critical issues (empty path/type, /discovered/ paths)
				// Warnings don't block creation - they're logged for post-factum fixes
				if !preValidation.IsValid && preValidation.BlockReason != "" {
					// Only block on critical cases that cannot be fixed post-factum
					if strings.Contains(preValidation.BlockReason, "/discovered/") ||
						strings.Contains(preValidation.BlockReason, "canonical path is required") ||
						strings.Contains(preValidation.BlockReason, "fragment type is required") {
						return fmt.Errorf("fragment creation blocked: %s", preValidation.BlockReason)
					}
					// For other cases, log warning but continue
					fmt.Printf("[ScoutEngine] WARNING (non-blocking): %s\n", preValidation.BlockReason)
				}
				// Check if we should use an existing fragment instead (deduplication)
				if preValidation.ReplacementFrag != nil && preValidation.ReplacementFrag.ID != "" {
					fmt.Printf("[ScoutEngine] Using existing fragment %s instead of creating new\n", preValidation.ReplacementFrag.ID)
					existingFragment = preValidation.ReplacementFrag
					fragmentID = existingFragment.ID
					usedExistingDuplicate = true

					// Merge OrgSize/MarketSegments from extracted into existing (fill-only)
					if strings.TrimSpace(extractedFragment.OrgSize) != "" {
						cur := strings.TrimSpace(strings.ToLower(existingFragment.OrgSize))
						in := strings.TrimSpace(extractedFragment.OrgSize)
						if cur == "" || cur == "na" {
							existingFragment.OrgSize = in
						}
					}
					if len(extractedFragment.MarketSegments) > 0 {
						if existingFragment.MarketSegments == nil {
							existingFragment.MarketSegments = make([]string, 0, len(extractedFragment.MarketSegments))
						}
						seen := make(map[string]bool)
						for _, s := range existingFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s != "" {
								seen[s] = true
							}
						}
						for _, s := range extractedFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s == "" {
								continue
							}
							if seen[s] {
								continue
							}
							seen[s] = true
							existingFragment.MarketSegments = append(existingFragment.MarketSegments, s)
						}
					}
					if len(extractedFragment.Tags) > 0 {
						if existingFragment.Tags == nil {
							existingFragment.Tags = make([]string, 0, len(extractedFragment.Tags))
						}
						seen := make(map[string]bool)
						for _, t := range existingFragment.Tags {
							t = strings.TrimSpace(t)
							if t != "" {
								seen[t] = true
							}
						}
						for _, t := range extractedFragment.Tags {
							t = strings.TrimSpace(t)
							if t == "" {
								continue
							}
							if seen[t] {
								continue
							}
							seen[t] = true
							existingFragment.Tags = append(existingFragment.Tags, t)
						}
						if existingFragment.Type == models.FragmentTypeMarket {
							existingFragment.Tags = taxonomy.NormalizeMarketTags(existingFragment.Tags)
						}
					}

					// Also merge State/Metadata from extracted fragment so new scouting is effective
					if len(extractedFragment.State) > 0 {
						if existingFragment.State == nil {
							existingFragment.State = make(map[string]models.MetricValue)
						}
						for k, v := range extractedFragment.State {
							existingFragment.State[k] = v
						}
					}
					// Merge HomeLocation so geocoding chain can run
					if extractedFragment.HomeLocation != nil {
						if existingFragment.HomeLocation == nil {
							existingFragment.HomeLocation = extractedFragment.HomeLocation
						} else {
							mergeHomeLocation(existingFragment.HomeLocation, extractedFragment.HomeLocation)
						}
					}
					existingFragment.Metadata.LastUpdatedAt = time.Now()
					existingFragment.Metadata.LastVerifiedAt = time.Now()
					existingFragment.Metadata.SourceFrameID = frameID

					// Best-effort: save the merged OrgSize/MarketSegments back to Firestore
					if uerr := e.fragmentsRepo.Update(ctx, existingFragment); uerr != nil {
						fmt.Printf("[ScoutEngine] Warning: failed to update fragment after dedup merge: %v\n", uerr)
					} else if sg := SessionGraphFromContext(ctx); sg != nil {
						sg.Add(existingFragment)
					}

					// If dedup selected a replacement fragment, we must retarget the job to it as well.
					// Otherwise subsequent steps (events, decomposition, retries) will keep using the alias.
					if existingFragment != nil {
						if cp := strings.TrimSpace(existingFragment.CanonicalPath); cp != "" && cp != effectiveCanonicalPath {
							old := effectiveCanonicalPath
							effectiveCanonicalPath = cp
							fmt.Printf("[ScoutEngine] Retargeting job %s to dedup replacement canonicalPath: %s -> %s\n",
								strings.TrimSpace(job.ID), old, effectiveCanonicalPath)
							job.TargetCanonicalPath = effectiveCanonicalPath
							if job.State == nil {
								job.State = map[string]interface{}{}
							}
							job.State["targetCanonicalPath"] = effectiveCanonicalPath
							if _, ok := job.State["aliasCanonicalPath"]; !ok && strings.TrimSpace(old) != "" {
								job.State["aliasCanonicalPath"] = strings.TrimSpace(old)
							}
							_ = e.scoutingJobsRepo.Update(ctx, job)
							// Update lens scope so UI/graph doesn't keep alias paths.
							e.bestEffortRewriteLensScope(ctx, tenantID, lensID, strings.TrimSpace(old), effectiveCanonicalPath)
						}
					}
				}
			}
		}

		// Only create if we didn't find a duplicate
		if !usedExistingDuplicate {
			// Race guard: another worker/job might have created this fragment after our first GetByCanonicalPath.
			// Firestore doesn't enforce uniqueness on canonicalPath, so re-check right before Create.
			if cp := strings.TrimSpace(extractedFragment.CanonicalPath); cp != "" {
				if reread, rerr := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); rerr == nil && reread != nil && strings.TrimSpace(reread.ID) != "" {
					usedExistingDuplicate = true
					existingFragment = reread
					fragmentID = reread.ID

					// Best-effort: apply the newly extracted state onto the existing fragment.
					// This keeps the current scouting run effective even when creation raced.
					existingFragment.State = extractedFragment.State
					// Merge HomeLocation so geocoding chain can run
					if extractedFragment.HomeLocation != nil {
						if existingFragment.HomeLocation == nil {
							existingFragment.HomeLocation = extractedFragment.HomeLocation
						} else {
							mergeHomeLocation(existingFragment.HomeLocation, extractedFragment.HomeLocation)
						}
					}
					existingFragment.Metadata.LastUpdatedAt = time.Now()
					existingFragment.Metadata.LastVerifiedAt = time.Now()
					existingFragment.Metadata.SourceFrameID = frameID
					existingFragment.Metadata.EpistemicStatus = extractedFragment.Metadata.EpistemicStatus

					// Merge OrgSize: fill if existing is empty/unknown ("na")
					if strings.TrimSpace(extractedFragment.OrgSize) != "" {
						cur := strings.TrimSpace(strings.ToLower(existingFragment.OrgSize))
						in := strings.TrimSpace(extractedFragment.OrgSize)
						if cur == "" || cur == "na" {
							existingFragment.OrgSize = in
						}
					}

					// Merge MarketSegments: union + dedup
					if len(extractedFragment.MarketSegments) > 0 {
						if existingFragment.MarketSegments == nil {
							existingFragment.MarketSegments = make([]string, 0, len(extractedFragment.MarketSegments))
						}
						seen := make(map[string]bool)
						for _, s := range existingFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s != "" {
								seen[s] = true
							}
						}
						for _, s := range extractedFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s == "" {
								continue
							}
							if seen[s] {
								continue
							}
							seen[s] = true
							existingFragment.MarketSegments = append(existingFragment.MarketSegments, s)
						}
					}

					// Normalize display label again (safe; no canonical changes).
					if existingFragment.Labels == nil {
						existingFragment.Labels = map[string]string{}
					}
					if strings.TrimSpace(existingFragment.Labels["en"]) == "" {
						seed := strings.TrimSpace(extractedFragment.Labels["en"])
						if seed == "" {
							seed = strings.TrimSpace(extractedFragment.Identifiers["legalName"])
						}
						if seed == "" {
							seed = strings.TrimSpace(extractedFragment.Identifiers["shortName"])
						}
						existingFragment.Labels["en"] = bestEffortDisplayLabel(seed, cp)
					} else {
						existingFragment.Labels["en"] = bestEffortDisplayLabel(existingFragment.Labels["en"], cp)
					}

					e.enrichFragmentParallel(ctx, existingFragment)

					if uerr := e.fragmentsRepo.Update(ctx, existingFragment); uerr != nil {
						return fmt.Errorf("failed to update fragment after race-guard hit: %w", uerr)
					} else if sg := SessionGraphFromContext(ctx); sg != nil {
						sg.Add(existingFragment)
					}
				}
			}
			if usedExistingDuplicate {
				// Skip Create path; we've adopted an existing fragment.
			} else {
				fragmentID, err = e.fragmentsRepo.Create(ctx, extractedFragment)
				if err != nil {
					return fmt.Errorf("failed to create fragment: %w", err)
				}
				extractedFragment.ID = fragmentID
				existingFragment = extractedFragment

				// Add to session graph for duplicate prevention within job
				if sg := SessionGraphFromContext(ctx); sg != nil {
					sg.Add(extractedFragment)
				}

				// Segment-first linking: materialize derived market nodes from org/fac marketSegments (best-effort).
				e.ensureSegmentDerivedMarketLinks(ctx, existingFragment, "scout_job", job.ID)
			}
			// Segment-first linking also applies when we adopted an existing fragment (dedup path).
			if usedExistingDuplicate {
				e.ensureSegmentDerivedMarketLinks(ctx, existingFragment, "scout_job", job.ID)
			}
		}
	} else {
		// Обновляем существующий фрагмент
		if strings.TrimSpace(effectiveCanonicalPath) != "" && strings.TrimSpace(existingFragment.CanonicalPath) != strings.TrimSpace(effectiveCanonicalPath) {
			legacy := strings.TrimSpace(existingFragment.CanonicalPath)
			existingFragment.CanonicalPath = strings.TrimSpace(effectiveCanonicalPath)
			// Keep semantic parts in sync so downstream canonizers/fixers operate on the updated entity consistently.
			if parts, err := taxonomy.ParseCanonicalPathParts(existingFragment.CanonicalPath); err == nil && parts != nil {
				existingFragment.CanonicalParts = parts
			}
			// If the old canonical path was in the lens scope, rewrite it so all subsequent
			// agents/UI work with the updated canonical path within this job.
			if legacy != "" && strings.TrimSpace(existingFragment.CanonicalPath) != "" && legacy != strings.TrimSpace(existingFragment.CanonicalPath) {
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, legacy, strings.TrimSpace(existingFragment.CanonicalPath))
				// Track path change in session graph
				if sg := SessionGraphFromContext(ctx); sg != nil {
					sg.RecordPathChange(legacy, strings.TrimSpace(existingFragment.CanonicalPath))
				}
			}
			if legacy != "" {
				recordLegacyOrgPath(existingFragment, legacy)
			}
		}
		existingFragment.State = extractedFragment.State

		// Merge HomeLocation: fill when extracted has geo data (enables geocoding chain)
		if extractedFragment.HomeLocation != nil {
			if existingFragment.HomeLocation == nil {
				existingFragment.HomeLocation = extractedFragment.HomeLocation
			} else {
				// Fill missing fields from extracted (prefer extracted when existing is empty)
				mergeHomeLocation(existingFragment.HomeLocation, extractedFragment.HomeLocation)
			}
		}

		// Merge OrgSize: fill if existing is empty/unknown ("na")
		if strings.TrimSpace(extractedFragment.OrgSize) != "" {
			cur := strings.TrimSpace(strings.ToLower(existingFragment.OrgSize))
			in := strings.TrimSpace(extractedFragment.OrgSize)
			if cur == "" || cur == "na" {
				existingFragment.OrgSize = in
			}
		}

		// Merge MarketSegments: union + dedup
		if len(extractedFragment.MarketSegments) > 0 {
			if existingFragment.MarketSegments == nil {
				existingFragment.MarketSegments = make([]string, 0, len(extractedFragment.MarketSegments))
			}
			seen := make(map[string]bool)
			for _, s := range existingFragment.MarketSegments {
				s = strings.TrimSpace(s)
				if s != "" {
					seen[s] = true
				}
			}
			for _, s := range extractedFragment.MarketSegments {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if seen[s] {
					continue
				}
				seen[s] = true
				existingFragment.MarketSegments = append(existingFragment.MarketSegments, s)
			}
		}

		// Best-effort: normalize display label on update as well (helps fix older lowercase-only stubs over time).
		if existingFragment.Labels == nil {
			existingFragment.Labels = map[string]string{}
		}
		if strings.TrimSpace(existingFragment.Labels["en"]) == "" {
			seed := strings.TrimSpace(extractedFragment.Labels["en"])
			if seed == "" {
				seed = strings.TrimSpace(extractedFragment.Identifiers["legalName"])
			}
			if seed == "" {
				seed = strings.TrimSpace(extractedFragment.Identifiers["shortName"])
			}
			existingFragment.Labels["en"] = bestEffortDisplayLabel(seed, effectiveCanonicalPath)
		} else {
			existingFragment.Labels["en"] = bestEffortDisplayLabel(existingFragment.Labels["en"], effectiveCanonicalPath)
		}
		existingFragment.Metadata.LastUpdatedAt = time.Now()
		existingFragment.Metadata.LastVerifiedAt = time.Now()
		existingFragment.Metadata.SourceFrameID = frameID
		existingFragment.Metadata.EpistemicStatus = models.EpistemicStatusVerified
		if validationResult.Confidence < 0.7 {
			existingFragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}

		e.enrichFragmentParallel(ctx, existingFragment)
		if internalFirstGroundingEnabled() {
			var resolutionStats *models.GroundingResolutionStats
			if job.State != nil {
				if raw, ok := job.State[stateKeyResolutionStats]; ok {
					if m, ok := raw.(map[string]interface{}); ok && m != nil {
						resolutionStats = &models.GroundingResolutionStats{
							RelationsResolvedByGrounding: parseStateInt(m["relationsResolvedByGrounding"], 0),
							RelationsCreatedNew:          parseStateInt(m["relationsCreatedNew"], 0),
							DedupRedirectsByGrounding:    parseStateInt(m["dedupRedirectsByGrounding"], 0),
						}
					}
				}
			}
			applyGroundingProvenance(&existingFragment.Provenance, groundingEvidence, allSourceIDs, allDocumentIDs, resolutionStats)
			if requireGroundingProvenanceEnabled() && strings.TrimSpace(existingFragment.Provenance.OriginMode) == "" {
				logGroundingWarnf("missing fragment provenance originMode after grounding: jobId=%s fragmentId=%s target=%s",
					strings.TrimSpace(job.ID), strings.TrimSpace(existingFragment.ID), strings.TrimSpace(job.TargetCanonicalPath))
			}
		}

		if err := e.fragmentsRepo.Update(ctx, existingFragment); err != nil {
			return fmt.Errorf("failed to update fragment: %w", err)
		}
		if sg := SessionGraphFromContext(ctx); sg != nil {
			sg.Add(existingFragment)
		}
		fragmentID = existingFragment.ID

		// Segment-first linking: materialize derived market nodes from org/fac marketSegments (best-effort).
		e.ensureSegmentDerivedMarketLinks(ctx, existingFragment, "scout_job", job.ID)
	}

	// Auto-enrich fragment with external hierarchy (Wikidata/Geo/Org)
	// This runs on both create and update to ensure best-effort enrichment even for existing fragments.
	if existingFragment != nil {
		e.enrichFragmentHierarchy(ctx, existingFragment, job.ID)
		// OpenCorporates runs inside enrichFragmentParallel (above); no separate call needed.
		if e.enrichRouteFacilityGeoContext(ctx, existingFragment, scoutResult) {
			_ = e.fragmentsRepo.Update(ctx, existingFragment)
		}
	}

	if orgLegacyPath != "" && orgCanonicalizedPath != "" && existingFragment != nil {
		e.runLegacyOrgEdgeCleanup(ctx, job, existingFragment, orgLegacyPath, orgCanonicalizedPath)
	}

	// Intent-based sphere linking: ensure sphere fragments and INFLUENCES edges exist for this target
	// when the Lens requested spherePaths. This is intentionally independent of ontology facts/metrics
	// so the UI can group/cluster entities by sphere even in early/partial graphs.
	if err := e.applyIntentSphereLinks(ctx, job, existingFragment); err != nil {
		// Best effort: do not fail job completion for linkage issues.
		fmt.Printf("[ScoutEngine] Failed to apply intent sphere links for %s: %v\n", job.TargetCanonicalPath, err)
	}

	// Ontology-first intent decomposition: propose reusable relations/facts even when local corpus is empty.
	// This seeds exploration for the requested spheres and makes future similar queries faster.
	if err := e.seedOntologyFromIntent(ctx, job, existingFragment); err != nil {
		fmt.Printf("[ScoutEngine] Intent decomposition failed for %s: %v\n", job.TargetCanonicalPath, err)
	}

	// Add to scope when Scout creates/updates fragment (max 5 per lens).
	// Trigger: object creation by pipeline, not just deferredScopeAdd or cache hit.
	// Path changes during pipeline (org upgrade, dedup, etc.) are handled by bestEffortRewriteLensScope.
	if lensID != "" && e.lensesRepo != nil && e.fragmentsRepo != nil {
		effectiveCP := strings.TrimSpace(existingFragment.CanonicalPath)
		if effectiveCP != "" {
			e.bestEffortDeferredScopeAdd(ctx, lensID, effectiveCP)
		}
		e.bestEffortRemoveOrphanScopePaths(ctx, lensID)
	}

	// Realtime: job completed + node materialized/updated.
	e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
		"tenantId":            tenantID,
		"projectId":           projectID,
		"lensId":              lensID,
		"jobId":               job.ID,
		"fragmentId":          fragmentID,
		"canonicalPath":       strings.TrimSpace(job.TargetCanonicalPath),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"epistemicStatus":     string(existingFragment.Metadata.EpistemicStatus),
		"confidence":          validationResult.Confidence,
	})

	// Check if all scout jobs for this lens are completed and publish final assembly.ready event
	e.checkAndPublishAssemblyCompletion(ctx, tenantID, projectID, lensID)

	e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
		"tenantId":      tenantID,
		"projectId":     projectID,
		"lensId":        lensID,
		"fragmentId":    fragmentID,
		"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"status": func() string {
			if existingFragment.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence {
				return "low_confidence"
			}
			return "verified"
		}(),
		"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
		"version":         existingFragment.Version,
	})

	// DECOMPOSITION: Обработка найденных связей (рекурсивный скаутинг)
	if len(scoutResult.Relations) > 0 {
		var resolvedGrounding *ResolvedGroundingContext
		if internalFirstGroundingEnabled() && e.groundingResolver != nil && groundingEvidence != nil {
			resolvedGrounding = e.groundingResolver.Resolve(ctx, groundingEvidence)
			if resolvedGrounding != nil && !resolvedGrounding.IsEmpty() {
				logGroundingInfof("grounding resolver context: jobId=%s target=%s fragments=%d",
					strings.TrimSpace(job.ID),
					strings.TrimSpace(job.TargetCanonicalPath),
					resolvedGrounding.Size(),
				)
			} else if requireGroundingProvenanceEnabled() {
				logGroundingWarnf("grounding resolver returned empty context: jobId=%s target=%s", strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath))
			}
		}
		// Phase 3 (decomposition): materialize edges and schedule child jobs (child jobs inherit domainMode/domainModes/domains from job state).
		// Используем existingFragment, так как он содержит актуальный ID после сохранения.
		var err error
		if resolvedGrounding != nil && !resolvedGrounding.IsEmpty() {
			err = e.processDiscoveredRelationsWithGrounding(ctx, job, existingFragment, scoutResult.Relations, resolvedGrounding)
		} else {
			err = e.processDiscoveredRelations(ctx, job, existingFragment, scoutResult.Relations)
		}
		if err != nil {
			logGroundingErrorWithContext(job, "scout", "materialization.decomposition", "decomposition_failed", "materialization_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "materialization.decomposition", "decomposition_failed", "materialization_error", true)
		}
	}
	if internalFirstGroundingEnabled() && job.State != nil {
		if raw, ok := job.State[stateKeyResolutionStats]; ok {
			if m, ok := raw.(map[string]interface{}); ok && m != nil {
				trace.Log(ctx, "scout_grounding", "scout.grounding.stats", map[string]any{
					"tenantId":                     strings.TrimSpace(job.TenantID),
					"projectId":                    strings.TrimSpace(job.ProjectID),
					"lensId":                       strings.TrimSpace(job.LensID),
					"jobId":                        strings.TrimSpace(job.ID),
					"targetCanonicalPath":          strings.TrimSpace(job.TargetCanonicalPath),
					"originMode":                   parseStateString(job.State[stateKeyOriginMode]),
					"relationsResolvedByGrounding": parseStateInt(m["relationsResolvedByGrounding"], 0),
					"relationsCreatedNew":          parseStateInt(m["relationsCreatedNew"], 0),
					"dedupRedirectsByGrounding":    parseStateInt(m["dedupRedirectsByGrounding"], 0),
				})
			}
		}
	}

	// SUPPLY CHAIN: Parse and persist supply chain fragments and edges
	if len(scoutResult.SupplyChains) > 0 {
		if err := e.processSupplyChains(ctx, job, existingFragment, scoutResult.SupplyChains); err != nil {
			fmt.Printf("[ScoutEngine] Supply chain processing failed: %v\n", err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> supply chain processing failed: %v", err))
		}
	}

	// POLICY ENFORCEMENT: Run final validation on all created/modified entities
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State = policyTracker.ApplyToState(job.State)
	// Persist finalized job outputs into LensArtifacts (Phase 2 input).
	e.persistLensArtifactsFromJobState(ctx, job)
	e.runPolicyAudit(ctx, job)

	// Запускаем распространение изменений в графе
	if e.propagationTrigger != nil {
		rc, _ := rtctx.Get(ctx)
		// Запускаем в отдельной горутине, чтобы не блокировать завершение задачи
		fid := fragmentID
		runAsyncSafe("scout_job_fragment_propagation", func() {
			// Используем фоновый контекст с таймаутом
			propBase := context.Background()
			if strings.TrimSpace(rc.TenantID) != "" || strings.TrimSpace(rc.ProjectID) != "" || strings.TrimSpace(rc.LensID) != "" || strings.TrimSpace(rc.CorrelationID) != "" || strings.TrimSpace(rc.SessionID) != "" {
				propBase = rtctx.With(propBase, rc)
			}
			propCtx, cancel := context.WithTimeout(propBase, 5*time.Minute)
			defer cancel()
			if err := e.propagationTrigger(propCtx, fid); err != nil {
				// Логируем ошибку (в реальном коде нужен logger)
				fmt.Printf("Propagation trigger failed for fragment %s: %v\n", fid, err)
			}
		})
	}

	return nil
}

func (e *ScoutEngine) seedOntologyFromIntent(ctx context.Context, job *models.ScoutingJob, sourceFragment *models.HorizonFragment) error {
	if e == nil || job == nil || sourceFragment == nil || strings.TrimSpace(sourceFragment.ID) == "" {
		return nil
	}
	if e.aiExecutor == nil {
		return nil
	}
	if job.State == nil {
		return nil
	}
	// Only run on root jobs to avoid exponential growth; recursion inherits intent for grounded scouting.
	if v, ok := job.State["recursionDepth"].(float64); ok && int(v) > 0 {
		return nil
	}
	if v, ok := job.State["intentDecompositionDone"].(bool); ok && v {
		return nil
	}

	focus := ""
	if v, ok := job.State["focus"].(string); ok {
		focus = strings.TrimSpace(v)
	}
	// keywords (optional)
	keywords := []string{}
	if v, ok := job.State["keywords"]; ok {
		switch t := v.(type) {
		case []string:
			keywords = append(keywords, t...)
		case []interface{}:
			for _, it := range t {
				if s, ok2 := it.(string); ok2 {
					keywords = append(keywords, s)
				}
			}
		case string:
			for _, p := range strings.Split(t, ",") {
				keywords = append(keywords, p)
			}
		}
	}
	// spherePaths (optional)
	spherePaths := []string{}
	if v, ok := job.State["spherePaths"]; ok {
		switch t := v.(type) {
		case []string:
			spherePaths = append(spherePaths, t...)
		case []interface{}:
			for _, it := range t {
				if s, ok2 := it.(string); ok2 {
					spherePaths = append(spherePaths, s)
				}
			}
		case string:
			for _, p := range strings.Split(t, ",") {
				spherePaths = append(spherePaths, p)
			}
		}
	}
	spherePaths = taxonomy.NormalizeSpherePaths(spherePaths)

	// Extract originalQuery and decompositionHints for MARKET-aware decomposition
	originalQuery := ""
	var decompositionHints []agents.DecompositionHint
	if v, ok := job.State["originalQuery"].(string); ok {
		originalQuery = strings.TrimSpace(v)
	}
	if v, ok := job.State["decompositionHints"]; ok && v != nil {
		if dhBytes, err := json.Marshal(v); err == nil {
			if err := json.Unmarshal(dhBytes, &decompositionHints); err != nil {
				fmt.Printf("[ScoutEngine] Failed to parse decompositionHints for intent seeding (best-effort): jobId=%s target=%s err=%v\n",
					strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			}
		}
	}

	// Check if this is an abstract anchor (MARKET/PROGRAM)
	isAbstract := strings.HasPrefix(job.TargetCanonicalPath, "/market/") ||
		strings.HasPrefix(job.TargetCanonicalPath, "/program/")

	// Guardrail: do NOT run intent decomposition unless we have any intent signal.
	// This is important to avoid changing behavior for ad-hoc "Scout it" / RefreshFragment calls
	// that may target MARKET/PROGRAM nodes without any user intent context.
	hasIntent := focus != "" || len(keywords) > 0 || len(spherePaths) > 0 || originalQuery != "" || len(decompositionHints) > 0
	if !hasIntent {
		return nil
	}

	agent := agents.NewIntentDecomposerAgent(e.aiExecutor)
	opts := agents.DecomposeOptions{
		IsAbstractAnchor:   isAbstract,
		DecompositionHints: decompositionHints,
		OriginalQuery:      originalQuery,
	}
	rels, decompGroundingEvidence, err := agent.DecomposeWithGrounding(ctx, strings.TrimSpace(job.TargetCanonicalPath), focus, spherePaths, keywords, opts)
	if err != nil {
		return err
	}
	if internalFirstGroundingEnabled() && decompGroundingEvidence != nil {
		logGroundingInfof("intent_decomposer grounding evidence: jobId=%s target=%s hasDatastore=%t hasWeb=%t fragmentIDs=%d",
			strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath),
			decompGroundingEvidence.HasDatastoreResults, decompGroundingEvidence.HasWebResults,
			len(decompGroundingEvidence.DatastoreFragmentIDs))
		applyGroundingEvidenceToJobState(job, decompGroundingEvidence, nil, nil)
	}
	if len(rels) == 0 {
		job.State["intentDecompositionDone"] = true
		if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"intentDecompositionDone": true}); err != nil {
			fmt.Printf("[ScoutEngine] Failed to persist intentDecompositionDone (best-effort): jobId=%s target=%s err=%v\n",
				strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
		}
		return nil
	}

	// Persist reusable ontology facts (kind=relation) so future queries can reuse them (and Vertex can index them).
	if e.factEmitter != nil || (e.planner != nil && e.planner.factsRepo != nil) {
		now := time.Now().UTC()
		for _, r := range rels {
			targetCP := strings.TrimSpace(r.TargetCanonicalPath)
			if targetCP == "" {
				continue
			}
			rt := strings.TrimSpace(taxonomy.NormalizeRelationType(r.RelationType))
			title := fmt.Sprintf("%s %s %s", strings.TrimSpace(job.TargetCanonicalPath), rt, targetCP)
			f := &coreModels.OntologyFact{
				BaseOntologyFields: coreModels.BaseOntologyFields{
					TenantID:       strings.TrimSpace(job.TenantID),
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
					CreatedBy:      "system",
					UpdatedBy:      "system",
					Labels:         map[string]string{"en": title},
					Status:         "active",
					Tags:           []string{"hypothesis", "intent", "relation"},
					CanonicalPaths: []string{strings.TrimSpace(job.TargetCanonicalPath), targetCP},
					ActorPaths:     []string{strings.TrimSpace(job.TargetCanonicalPath), targetCP},
					SpherePaths:    spherePaths,
					VectorText:     "",
					LocationPath:   nil,
					Description:    "",
				},
				Kind:       "relation",
				Type:       rt,
				Title:      title,
				Summary:    strings.TrimSpace(r.Reason),
				ObservedAt: &now,
				SourceRefs: []coreModels.SourceRef{{SourceID: strings.TrimSpace(job.ID), SourceType: coreModels.SourceTypeAgentJob}},
				Confidence: func() float64 {
					c := r.Confidence
					if c <= 0 || c > 0.65 {
						return 0.35
					}
					return c
				}(),
			}
			// Add correlation/trace link when available (so facts can be traced back to LLM/tool telemetry).
			if rc, ok := rtctx.Get(ctx); ok {
				if v := strings.TrimSpace(rc.CorrelationID); v != "" {
					f.SourceRefs = append(f.SourceRefs, coreModels.SourceRef{SourceID: v, SourceType: coreModels.SourceTypeAITrace})
				}
			}
			f.VectorText = f.GenerateVectorText()
			if e.factEmitter != nil {
				rc, _ := rtctx.Get(ctx)
				if _, err := e.factEmitter.Emit(ctx, f, coreontology.EmitFactOptions{
					TenantID:         strings.TrimSpace(job.TenantID),
					ProjectID:        strings.TrimSpace(job.ProjectID),
					LensID:           strings.TrimSpace(job.LensID),
					SessionID:        strings.TrimSpace(rc.SessionID),
					CorrelationID:    strings.TrimSpace(rc.CorrelationID),
					PublishFeedEvent: true,
					Actor:            "system",
				}); err != nil { // best effort
					fmt.Printf("[ScoutEngine] Failed to emit intent relation fact (best-effort): jobId=%s target=%s type=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), strings.TrimSpace(r.RelationType), err)
				}
			} else if e.planner != nil && e.planner.factsRepo != nil {
				if _, err := e.planner.factsRepo.Create(ctx, f); err != nil { // best effort
					fmt.Printf("[ScoutEngine] Failed to persist intent relation fact (best-effort): jobId=%s target=%s type=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), strings.TrimSpace(r.RelationType), err)
				}
			}
		}
	}

	// Materialize hypothesis edges/fragments and schedule recursive scouting.
	if err := e.processHypothesisRelations(ctx, job, sourceFragment, rels); err != nil {
		return err
	}

	job.State["intentDecompositionDone"] = true
	if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"intentDecompositionDone": true}); err != nil {
		fmt.Printf("[ScoutEngine] Failed to persist intentDecompositionDone (best-effort): jobId=%s target=%s err=%v\n",
			strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
	}
	return nil
}

// applyIntentSphereLinks ensures the graph contains sphere dimension nodes and edges requested by Lens/job intent.
// This is a lightweight linkage (not an extracted fact) and should be treated as a UI/analysis hint.
func (e *ScoutEngine) applyIntentSphereLinks(ctx context.Context, job *models.ScoutingJob, target *models.HorizonFragment) error {
	if e == nil || job == nil || target == nil || target.ID == "" {
		return nil
	}
	if job.State == nil {
		return nil
	}
	raw, ok := job.State["spherePaths"]
	if !ok {
		return nil
	}

	// Parse to []string (accept []string, []any, comma-separated string).
	paths := []string{}
	switch t := raw.(type) {
	case []string:
		paths = append(paths, t...)
	case []interface{}:
		for _, it := range t {
			if s, ok := it.(string); ok {
				paths = append(paths, s)
			}
		}
	case string:
		for _, p := range strings.Split(t, ",") {
			paths = append(paths, p)
		}
	}

	now := time.Now().UTC()
	for _, sp := range paths {
		sp = strings.TrimSpace(sp)
		if sp == "" {
			continue
		}

		// Ensure sphere fragment exists.
		sfrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, sp)
		if err != nil || sfrag == nil {
			sfrag = &models.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: sp,
				Type:          models.FragmentTypeSphere,
				Status:        "active",
				Labels:        map[string]string{"en": sp},
				EntityPath:    "/Influence/Sphere",
				SectorPath:    "/Influence",
				State:         make(map[string]models.MetricValue),
				Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
				Provenance: models.Provenance{
					Method:    "lens_intent_sphere_link",
					SourceIDs: []string{strings.TrimSpace(job.ID)},
				},
				Metadata: models.FragmentMetadata{
					CreatedAt:     now,
					LastUpdatedAt: now,
				},
			}
			id, cerr := e.fragmentsRepo.Create(ctx, sfrag)
			if cerr != nil {
				// If create races or fails for other reasons, re-read once.
				if reread, rerr := e.fragmentsRepo.GetByCanonicalPath(ctx, sp); rerr == nil && reread != nil {
					sfrag = reread
				} else {
					return cerr
				}
			} else {
				sfrag.ID = id
				// Add to session graph for duplicate prevention within job
				if sg := SessionGraphFromContext(ctx); sg != nil {
					sg.Add(sfrag)
				}
			}
		}

		if sfrag.ID == "" || sfrag.ID == target.ID {
			continue
		}

		// Ensure target fragment carries this sphere coordinate in its indexPaths (best-effort).
		// This enables deterministic "map by sphere" without requiring ontology facts or LLM.
		hasSphere := false
		for _, ip := range target.IndexPaths {
			if ip == sp {
				hasSphere = true
				break
			}
		}
		if !hasSphere && e.fragmentsRepo != nil {
			if err := e.fragmentsRepo.UpdateIndexPaths(ctx, target.ID, []string{sp}); err != nil { // best effort
				fmt.Printf("[ScoutEngine] Failed to UpdateIndexPaths (best-effort): fragmentId=%s target=%s indexPath=%s err=%v\n",
					strings.TrimSpace(target.ID), strings.TrimSpace(target.CanonicalPath), sp, err)
			}
			// Keep local copy consistent for subsequent iterations in this job.
			target.IndexPaths = models.MergeIndexPaths(target, []string{sp})
		}

		edgeType := models.DependencyTypeInfluences
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sfrag.ID, target.ID, edgeType); err == nil {
			continue
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sfrag.ID,
			TargetFragmentID: target.ID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   0.6,
				Amplification: 1.0,
				LatencyHours:  1,
			},
			Qualifiers: map[string]interface{}{
				"dimension": "sphere",
				"path":      sp,
				"source":    "lens_intent",
				"lensId":    strings.TrimSpace(job.LensID),
				"cascadeRootJobId": func() string {
					if v, ok := job.State["cascadeRootJobId"].(string); ok {
						return strings.TrimSpace(v)
					}
					return ""
				}(),
			},
			Provenance: models.Provenance{
				Method:    "lens_intent_sphere_link",
				SourceIDs: []string{strings.TrimSpace(job.ID)},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     now,
				LastUpdatedAt: now,
			},
		}
		if id, err := e.edgesRepo.Create(ctx, edge); err == nil {
			edge.ID = id
			sourcePath := ""
			targetPath := ""
			sourceLabel := ""
			targetLabel := ""
			if e.fragmentsRepo != nil {
				if frag, ferr := e.fragmentsRepo.Get(ctx, edge.SourceFragmentID); ferr == nil && frag != nil {
					sourcePath = strings.TrimSpace(frag.CanonicalPath)
					sourceLabel = strings.TrimSpace(frag.Labels["en"])
				}
				if frag, ferr := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID); ferr == nil && frag != nil {
					targetPath = strings.TrimSpace(frag.CanonicalPath)
					targetLabel = strings.TrimSpace(frag.Labels["en"])
				}
			}
			// Realtime: edge materialized/updated for UI graph.
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":            job.TenantID,
				"projectId":           job.ProjectID,
				"lensId":              strings.TrimSpace(job.LensID),
				"edgeId":              edge.ID,
				"sourceFragmentId":    edge.SourceFragmentID,
				"targetFragmentId":    edge.TargetFragmentID,
				"sourceCanonicalPath": sourcePath,
				"targetCanonicalPath": targetPath,
				"sourceLabel":         sourceLabel,
				"targetLabel":         targetLabel,
				"type":                string(edge.Type),
				"properties":          edge.Properties,
				"qualifiers":          edge.Qualifiers,
			})
		}
	}

	return nil
}

func normalizeTerminalOutcome(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case TerminalOutcomeCompleted:
		return TerminalOutcomeCompleted
	case TerminalOutcomePartialCompleted:
		return TerminalOutcomePartialCompleted
	case TerminalOutcomeFailed:
		return TerminalOutcomeFailed
	case TerminalOutcomeBudgetExhausted:
		return TerminalOutcomeBudgetExhausted
	case TerminalOutcomeCompletedWithPolicyPersistError:
		return TerminalOutcomeCompletedWithPolicyPersistError
	default:
		return ""
	}
}

func scoutJobHasBudgetExhaustion(job *models.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	if parseStateBool(job.State["budgetExhausted"], false) {
		return true
	}
	if parseStateBool(job.State["budgetExhaustedPartial"], false) {
		return true
	}
	if normalizeTerminalOutcome(parseStateString(job.State["terminalOutcome"])) == TerminalOutcomeBudgetExhausted {
		return true
	}
	return false
}

func scoutJobHasPolicyPersistError(job *models.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	if parseStateBool(job.State["policyPersistError"], false) {
		return true
	}
	return normalizeTerminalOutcome(parseStateString(job.State["terminalOutcome"])) == TerminalOutcomeCompletedWithPolicyPersistError
}

func isRootLensScoutJob(job *models.ScoutingJob) bool {
	if job == nil {
		return false
	}
	if job.State == nil {
		return true
	}
	return strings.TrimSpace(parseStateString(job.State["parentJobId"])) == ""
}

func (e *ScoutEngine) markRootJobsTerminalOutcome(
	ctx context.Context,
	jobs []*models.ScoutingJob,
	outcome string,
	reasons map[string]interface{},
	completedAt string,
) {
	if e == nil || e.scoutingJobsRepo == nil || len(jobs) == 0 {
		return
	}
	outcome = normalizeTerminalOutcome(outcome)
	if outcome == "" {
		return
	}
	for _, job := range jobs {
		if !isRootLensScoutJob(job) {
			continue
		}
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		job.State["terminalOutcome"] = outcome
		job.State["terminalOutcomeReasons"] = reasons
		job.State["terminalizedAt"] = completedAt
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
			"terminalOutcome":        outcome,
			"terminalOutcomeReasons": reasons,
			"terminalizedAt":         completedAt,
		})
	}
}

// checkAndPublishAssemblyCompletion checks if all scout jobs for a lens are completed
// and publishes horizon.assembly.ready with deterministic terminal outcome.
func (e *ScoutEngine) checkAndPublishAssemblyCompletion(ctx context.Context, tenantID, projectID, lensID string) {
	if e == nil || e.scoutingJobsRepo == nil || lensID == "" {
		return
	}

	jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, tenantID, projectID, lensID, 1000)
	if err != nil || len(jobs) == 0 {
		return
	}

	var totalJobs, rootJobs, completedJobs, partialJobs, failedJobs, pendingJobs, runningJobs int
	var budgetExhaustedJobs, policyPersistErrorJobs int
	var scEdgesCreated, scEdgeDupes, scEdgesReversed int
	var policyViolationsTotal, policyAutoFixableTotal int
	for _, job := range jobs {
		if job == nil {
			continue
		}
		totalJobs++
		if isRootLensScoutJob(job) {
			rootJobs++
		}
		if scoutJobHasBudgetExhaustion(job) {
			budgetExhaustedJobs++
		}
		if scoutJobHasPolicyPersistError(job) {
			policyPersistErrorJobs++
		}
		if job.State != nil {
			if sc, ok := job.State["supplyChainSummary"].(map[string]interface{}); ok {
				scEdgesCreated += parseStateInt(sc["createdEdges"], 0)
				scEdgeDupes += parseStateInt(sc["edgeDupes"], 0)
				scEdgesReversed += parseStateInt(sc["edgeReversed"], 0)
			}
			if ps, ok := job.State["policySummary"].(map[string]interface{}); ok {
				policyViolationsTotal += parseStateInt(ps["violationsTotal"], 0)
				policyAutoFixableTotal += parseStateInt(ps["autoFixable"], 0)
			}
		}
		switch job.Status {
		case coreModels.AgentJobStatusCompleted:
			completedJobs++
		case coreModels.AgentJobStatusPartial:
			partialJobs++
		case coreModels.AgentJobStatusFailed:
			failedJobs++
		case coreModels.AgentJobStatusPending:
			pendingJobs++
		case coreModels.AgentJobStatusRunning:
			runningJobs++
		default:
			switch job.CurrentState {
			case models.ScoutingJobStateVerified:
				completedJobs++
			case models.ScoutingJobStateFailed, models.ScoutingJobStateRejected:
				failedJobs++
			case models.ScoutingJobStatePending:
				pendingJobs++
			default:
				runningJobs++
			}
		}
	}
	if totalJobs == 0 {
		return
	}

	doneJobs := completedJobs + partialJobs + failedJobs
	progress := float64(doneJobs) / float64(totalJobs)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	terminal := pendingJobs == 0 && runningJobs == 0
	terminalOutcome := ""
	if terminal {
		switch {
		case budgetExhaustedJobs > 0:
			terminalOutcome = TerminalOutcomeBudgetExhausted
		case completedJobs == 0 && partialJobs == 0 && failedJobs > 0:
			terminalOutcome = TerminalOutcomeFailed
		case failedJobs > 0 || partialJobs > 0 || policyPersistErrorJobs > 0:
			terminalOutcome = TerminalOutcomePartialCompleted
		default:
			terminalOutcome = TerminalOutcomeCompleted
		}
		progress = 1.0
	}
	fanOutPerRoot := 0.0
	if rootJobs > 0 {
		fanOutPerRoot = float64(totalJobs) / float64(rootJobs)
	}
	dupeCreateRatio := 0.0
	reverseRate := 0.0
	if scEdgesCreated > 0 {
		dupeCreateRatio = float64(scEdgeDupes) / float64(scEdgesCreated)
		reverseRate = float64(scEdgesReversed) / float64(scEdgesCreated)
	}
	kpiGuardrailTriggered := false
	kpiGuardReasons := []string{}
	if scEdgesCreated >= 10 && dupeCreateRatio > 0.2 {
		kpiGuardrailTriggered = true
		kpiGuardReasons = append(kpiGuardReasons, "dupe_create_ratio_exceeded")
	}
	if scEdgesCreated >= 10 && reverseRate > 0.25 {
		kpiGuardrailTriggered = true
		kpiGuardReasons = append(kpiGuardReasons, "reverse_rate_exceeded")
	}
	if rootJobs > 0 && fanOutPerRoot > 12.0 {
		kpiGuardrailTriggered = true
		kpiGuardReasons = append(kpiGuardReasons, "fanout_per_root_exceeded")
	}
	if terminal && terminalOutcome == TerminalOutcomeCompleted && kpiGuardrailTriggered {
		terminalOutcome = TerminalOutcomePartialCompleted
	}

	reasonMap := map[string]interface{}{
		"jobsTotal":              totalJobs,
		"jobsRoot":               rootJobs,
		"jobsCompleted":          completedJobs,
		"jobsPartial":            partialJobs,
		"jobsFailed":             failedJobs,
		"jobsPending":            pendingJobs,
		"jobsRunning":            runningJobs,
		"jobsBudgetExhausted":    budgetExhaustedJobs,
		"jobsPolicyPersistError": policyPersistErrorJobs,
		"kpi": map[string]interface{}{
			"fanOutPerRoot":          fanOutPerRoot,
			"supplyChainEdges":       scEdgesCreated,
			"supplyChainEdgeDupes":   scEdgeDupes,
			"supplyChainEdgeReverse": scEdgesReversed,
			"dupeCreateRatio":        dupeCreateRatio,
			"reverseRate":            reverseRate,
			"policyViolationsTotal":  policyViolationsTotal,
			"policyAutoFixableTotal": policyAutoFixableTotal,
		},
		"kpiGuardrailTriggered": kpiGuardrailTriggered,
		"kpiGuardrailReasons":   kpiGuardReasons,
	}
	if q := e.policyPersistQueueSnapshot(); q != nil {
		reasonMap["policyPersistQueue"] = q
	}

	if terminal {
		nowISO := time.Now().UTC().Format(time.RFC3339Nano)
		e.publish(ctx, realtime.EventType("horizon.assembly.progress"), map[string]interface{}{
			"tenantId":        tenantID,
			"projectId":       projectID,
			"lensId":          lensID,
			"phase":           "ready",
			"progress":        progress,
			"terminalOutcome": terminalOutcome,
			"reasonMap":       reasonMap,
			"counters": map[string]interface{}{
				"jobsTotal":   totalJobs,
				"jobsDone":    doneJobs,
				"jobsFailed":  failedJobs,
				"jobsRunning": 0,
			},
		})

		e.runIntraGraphInference(ctx, tenantID, projectID, lensID)

		e.publish(ctx, realtime.EventType("horizon.assembly.ready"), map[string]interface{}{
			"tenantId":        tenantID,
			"projectId":       projectID,
			"lensId":          lensID,
			"phase":           "ready",
			"progress":        progress,
			"terminalOutcome": terminalOutcome,
			"reasonMap":       reasonMap,
			"counters": map[string]interface{}{
				"jobsTotal":   totalJobs,
				"jobsDone":    doneJobs,
				"jobsFailed":  failedJobs,
				"jobsPartial": partialJobs,
			},
		})

		e.markRootJobsTerminalOutcome(ctx, jobs, terminalOutcome, reasonMap, nowISO)

		if e.lensesRepo != nil {
			lens, getErr := e.lensesRepo.Get(ctx, lensID)
			if getErr == nil && lens != nil {
				if lens.ViewConfig == nil {
					lens.ViewConfig = make(map[string]interface{})
				}
				lens.ViewConfig["assemblyStatus"] = map[string]interface{}{
					"phase":           "ready",
					"progress":        progress,
					"terminalOutcome": terminalOutcome,
					"reasonMap":       reasonMap,
					"completedAt":     nowISO,
					"counters": map[string]interface{}{
						"jobsTotal":   totalJobs,
						"jobsDone":    doneJobs,
						"jobsFailed":  failedJobs,
						"jobsPartial": partialJobs,
					},
				}
				_ = e.lensesRepo.Update(ctx, lens)
			}
		}
		return
	}

	e.publish(ctx, realtime.EventType("horizon.assembly.progress"), map[string]interface{}{
		"tenantId":  tenantID,
		"projectId": projectID,
		"lensId":    lensID,
		"phase":     "scouting",
		"progress":  progress,
		"counters": map[string]interface{}{
			"jobsTotal":   totalJobs,
			"jobsDone":    doneJobs,
			"jobsFailed":  failedJobs,
			"jobsPartial": partialJobs,
			"jobsRunning": runningJobs,
			"jobsPending": pendingJobs,
		},
	})
}

// enrichFragmentHierarchy best-effort builds external hierarchies (Geo, Org, Product/Wikidata) for a fragment.
// It is safe to call on existing fragments (idempotent / non-destructive).
func (e *ScoutEngine) enrichFragmentHierarchy(ctx context.Context, fragment *models.HorizonFragment, jobID string) {
	if fragment == nil || fragment.ID == "" {
		return
	}

	// Auto-create geo hierarchy for GEO_REGION fragments
	if fragment.Type == models.FragmentTypeGeoRegion && e.geoHierarchyBuilder != nil {
		if strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			if err := e.geoHierarchyBuilder.EnsureAndLinkGeoHierarchy(ctx, fragment.ID, fragment.CanonicalPath, "scout_job", jobID); err != nil {
				fmt.Printf("[ScoutEngine] Warning: failed to build geo hierarchy for %s: %v\n", fragment.CanonicalPath, err)
			}
		}
	}

	// Auto-create org hierarchy for ORGANIZATION fragments (infer parent from name)
	if fragment.Type == models.FragmentTypeOrganization && e.orgHierarchyBuilder != nil {
		if err := e.orgHierarchyBuilder.EnsureOrgHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build org hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Auto-create product hierarchy for PRODUCT fragments (build value chain from Wikidata)
	if fragment.Type == models.FragmentTypeProduct && e.productHierarchyBuilder != nil {
		if err := e.productHierarchyBuilder.EnsureProductHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build product hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Auto-create market hierarchy for MARKET fragments (segment-first when possible; heuristic fallback)
	if fragment.Type == models.FragmentTypeMarket && e.marketHierarchyBuilder != nil {
		if err := e.marketHierarchyBuilder.EnsureMarketHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build market hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}
	// Auto-create geo market parent for regional MARKET fragments: /market/{geo}/X -> /market/global/X
	if fragment.Type == models.FragmentTypeMarket && e.marketGeoHierarchy != nil {
		if err := e.marketGeoHierarchy.EnsureMarketGeoHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build market geo hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Best-effort gov type inference for /gov/... fragments (stored as ORGANIZATION)
	if strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/gov/") && e.govHierarchyBuilder != nil {
		if err := e.govHierarchyBuilder.EnsureGovHierarchy(ctx, fragment.ID, fragment); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to infer gov hierarchy fields for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Sphere and resolution inference (best-effort, only when empty)
	if len(fragment.SpherePaths) == 0 {
		qualifiers := make(map[string]interface{})
		for k, v := range fragment.Qualifiers {
			qualifiers[k] = v
		}
		if len(fragment.MarketSegmentNormDomains) > 0 {
			qualifiers["marketSegmentNormDomains"] = fragment.MarketSegmentNormDomains
		}
		if fragment.MarketDomain != "" {
			qualifiers["marketDomain"] = fragment.MarketDomain
		}
		if len(fragment.IndustryPaths) > 0 {
			qualifiers["industryPaths"] = fragment.IndustryPaths
		}
		if fragment.CommunityType != "" {
			qualifiers["communityType"] = fragment.CommunityType
		}
		if fragment.GeoFeatureType != "" {
			qualifiers["geoFeatureType"] = fragment.GeoFeatureType
		}
		if inferred := InferSpherePaths(fragment.CanonicalPath, fragment.Type, fragment.GovType, qualifiers); len(inferred) > 0 {
			fragment.SpherePaths = inferred
		}
	}
	if fragment.ResolutionHint == "" {
		parentPath := fragment.ParentPath
		if parentPath == "" && fragment.ParentGovPath != "" {
			parentPath = fragment.ParentGovPath
		}
		if parentPath == "" && fragment.ParentMarketPath != "" {
			parentPath = fragment.ParentMarketPath
		}
		if h := InferResolutionHint(fragment.Type, fragment.GovType, fragment.CanonicalPath, parentPath); h != "" {
			fragment.ResolutionHint = h
		}
	}
}

// enrichFragmentParallel runs Wikidata and OpenCorporates concurrently (Group 1),
// then geo enrichment sequentially (Group 2 — depends on Wikidata results for address data).
func (e *ScoutEngine) enrichFragmentParallel(ctx context.Context, frag *models.HorizonFragment) {
	if frag == nil {
		return
	}
	var wg sync.WaitGroup

	// Group 1: independent external lookups.
	wg.Add(2)
	go func() {
		defer wg.Done()
		if e.wikidataClient != nil {
			_, _ = henrich.EnrichFragmentFromWikidataIfPresent(ctx, e.wikidataClient, frag, henrich.ConservativeWikidataOptions{
				Force:     false,
				Overwrite: true,
			})
		}
	}()
	go func() {
		defer wg.Done()
		if henrich.OpenCorporatesEnabled() && e.openCorporatesClient != nil && frag.Type == models.FragmentTypeOrganization {
			henrich.AttachOpenCorporatesIDIfMissing(ctx, e.openCorporatesClient, frag)
		}
	}()
	wg.Wait()

	// Group 2: geo chain (sequential — may depend on Wikidata-provided address data).
	if e.geonamesClient != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/loc/") {
		_, _ = henrich.EnrichCoordinatesFromGeoNames(ctx, e.geonamesClient, frag)
	}
	if e.geocodingClient != nil {
		_, _ = henrich.EnrichCoordinatesFromGeocoding(ctx, e.geocodingClient, frag)
	}
	if !models.HasRealGeoCoordinates(frag.Coordinates) {
		_ = e.tryPropagateCoordinatesFromParent(ctx, frag)
	}
}

// tryPropagateCoordinatesFromParent applies parent-org coordinates when direct geocoding didn't produce a result.
// Uses explicit parent pointers only (deterministic and cheap for scout-time updates).
func (e *ScoutEngine) tryPropagateCoordinatesFromParent(ctx context.Context, fragment *models.HorizonFragment) bool {
	if e == nil || fragment == nil || e.fragmentsRepo == nil {
		return false
	}
	if models.HasRealGeoCoordinates(fragment.Coordinates) {
		return false
	}
	seen := map[string]struct{}{}
	candidates := []string{
		strings.TrimSpace(fragment.ParentPath),
		strings.TrimSpace(fragment.AffiliatedOrgPath),
		strings.TrimSpace(fragment.Identifiers["parentOrgPath"]),
		strings.TrimSpace(fragment.Identifiers["orgPath"]),
		strings.TrimSpace(fragment.Identifiers["operatorPath"]),
		strings.TrimSpace(fragment.Identifiers["ownerPath"]),
	}
	for _, cp := range candidates {
		if cp == "" || !strings.HasPrefix(cp, "/org/") {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		parent, err := e.fragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err != nil || parent == nil {
			continue
		}
		if henrich.EnrichCoordinatesFromParent(fragment, parent) {
			return true
		}
	}
	return false
}

// =============================================================================
// POLICY ENFORCEMENT
// =============================================================================

// runPolicyCheck runs the policy checker on job output (non-blocking).
// If violations are found, it creates enforcement jobs for auto-fixable issues
// and publishes WebSocket messages for real-time UI updates.
func (e *ScoutEngine) runPolicyCheck(_ context.Context, job *models.ScoutingJob) {
	if e == nil || job == nil {
		return
	}

	// Collect created/modified fragment and edge IDs from job state (for PolicyChecker).
	createdFragmentIDs := e.extractStringSlice(job.State, "createdFragmentIds")
	modifiedFragmentIDs := e.extractStringSlice(job.State, "modifiedFragmentIds")
	createdEdgeIDs := e.extractStringSlice(job.State, "createdEdgeIds")
	modifiedEdgeIDs := e.extractStringSlice(job.State, "modifiedEdgeIds")

	shouldRunPolicy := e.policyChecker != nil &&
		(len(createdFragmentIDs) > 0 || len(modifiedFragmentIDs) > 0 || len(createdEdgeIDs) > 0 || len(modifiedEdgeIDs) > 0)

	if !shouldRunPolicy {
		return
	}

	// Run in background to not block job completion.
	runAsyncSafe("policy_check", func() {
		// Policy checks may involve multiple LLM calls (duplicate checks + fallbacks) and must not
		// be prematurely cancelled, otherwise we get "context deadline exceeded" across all providers
		// and fail to persist policy check results.
		policyCtx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		policy.LogPolicyEvent("policy_checker_policy", policy.PolicyLogStageInput, map[string]interface{}{
			"trace_id":         "na",
			"tenant_id":        job.TenantID,
			"job_id":           job.ID,
			"action":           "policy_audit_start",
			"policy_status":    "na",
			"policy_reason":    "",
			"policy_penalty":   0,
			"decision_path":    "policy_audit",
			"confidence":       0,
			"contract_enabled": policy.UnifiedPolicyContractEnabled(),
			"shadow_mode":      policy.UnifiedPolicyShadowModeEnabled(),
		})

		durationMs := int64(0)
		var result *policy.PolicyCheckResult
		if shouldRunPolicy {
			startTime := time.Now()
			r, err := e.policyChecker.CheckJobOutput(
				policyCtx,
				job.TenantID,
				job.ID,
				createdFragmentIDs,
				modifiedFragmentIDs,
				createdEdgeIDs,
				modifiedEdgeIDs,
			)
			durationMs = time.Since(startTime).Milliseconds()
			if err != nil {
				policy.LogPolicyEvent("policy_checker_policy", policy.PolicyLogStageError, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      job.TenantID,
					"job_id":         job.ID,
					"action":         "check_job_output",
					"policy_status":  "na",
					"policy_reason":  err.Error(),
					"policy_penalty": 0,
					"decision_path":  "policy_audit",
					"error_class":    "policy_checker_error",
					"confidence":     0,
				})
			} else {
				result = r
			}
		}

		// If policy result exists, report it + optionally run enforcement.
		if result != nil {
			if result.TotalViolations == 0 {
				policy.LogPolicyEvent("policy_checker_policy", policy.PolicyLogStageResult, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      job.TenantID,
					"job_id":         job.ID,
					"action":         "allow",
					"policy_status":  "clean",
					"policy_reason":  "no_violations",
					"policy_penalty": 0,
					"decision_path":  "policy_audit",
					"confidence":     1,
				})
			} else {
				policy.LogPolicyEvent("policy_checker_policy", policy.PolicyLogStageDecision, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      job.TenantID,
					"job_id":         job.ID,
					"action":         "quarantine_soft",
					"policy_status":  "penalized",
					"policy_reason":  "violations_found",
					"policy_penalty": 0,
					"decision_path":  "policy_audit",
					"confidence":     1,
					"violations":     result.TotalViolations,
					"auto_fixable":   result.AutoFixableCount,
				})
			}

			// Create enforcement jobs for auto-fixable violations
			if result.AutoFixableCount > 0 && e.enforcementExecutor != nil {
				enforcementCtx, enforcementCancel := context.WithTimeout(context.Background(), 45*time.Second)
				enforcementJobs, err := e.enforcementExecutor.CreateEnforcementJobs(enforcementCtx, job.TenantID, job.ID, result.Violations)
				enforcementCancel()
				if err != nil {
					policy.LogPolicyEvent("enforcement_policy", policy.PolicyLogStageError, map[string]interface{}{
						"trace_id":       "na",
						"tenant_id":      job.TenantID,
						"job_id":         job.ID,
						"action":         "create_enforcement_jobs",
						"policy_status":  "na",
						"policy_reason":  err.Error(),
						"policy_penalty": 0,
						"decision_path":  "policy_audit",
						"error_class":    "enforcement_create_error",
						"confidence":     0,
					})
				} else if len(enforcementJobs) > 0 {
					// Store enforcement job IDs in result
					for _, ej := range enforcementJobs {
						result.EnforcementJobIDs = append(result.EnforcementJobIDs, ej.ID)
					}
					policy.LogPolicyEvent("enforcement_policy", policy.PolicyLogStageApply, map[string]interface{}{
						"trace_id":       "na",
						"tenant_id":      job.TenantID,
						"job_id":         job.ID,
						"action":         "create_enforcement_jobs",
						"policy_status":  "na",
						"policy_reason":  "enforcement_jobs_created",
						"policy_penalty": 0,
						"decision_path":  "policy_audit",
						"confidence":     1,
						"jobs_count":     len(enforcementJobs),
					})

					// Optionally execute auto-fixes immediately.
					if e.shouldAutoExecuteEnforcement() {
						execCtx, execCancel := context.WithTimeout(context.Background(), 2*time.Minute)
						e.executeEnforcementJobs(execCtx, job)
						execCancel()
					}
				}
			}
		}

		// Persist policy check results (best-effort) and publish realtime output.
		if result != nil {
			if e.policyCheckResultsRepo != nil {
				e.enqueuePolicyPersistTask(job, result)
				policy.LogPolicyEvent("policy_checker_policy", policy.PolicyLogStageResult, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      job.TenantID,
					"job_id":         job.ID,
					"action":         "persist_policy_check_result_enqueued",
					"policy_status":  "queued",
					"policy_reason":  "idempotent_retry_queue",
					"policy_penalty": 0,
					"decision_path":  "policy_audit",
					"confidence":     1,
					"queue_snapshot": e.formatPolicyPersistQueueSnapshot(),
				})
			}
			if job != nil {
				if job.State == nil {
					job.State = map[string]interface{}{}
				}
				job.State["policySummary"] = map[string]interface{}{
					"violationsTotal": float64(result.TotalViolations),
					"autoFixable":     float64(result.AutoFixableCount),
				}
			}
		}

		// Publish policy check result via realtime (frontend expects horizon.policy.*)
		// IMPORTANT: front-end shows only 1-3 sample items; ensure critical violations are surfaced first.
		if result == nil {
			return
		}
		violationsForSample := make([]policy.PolicyViolation, 0, len(result.Violations))
		violationsForSample = append(violationsForSample, result.Violations...)
		sort.SliceStable(violationsForSample, func(i, j int) bool {
			si := violationsForSample[i].Severity
			sj := violationsForSample[j].Severity
			score := func(s policy.PolicyViolationSeverity) int {
				switch s {
				case policy.SeverityCritical:
					return 3
				case policy.SeverityWarning:
					return 2
				case policy.SeverityInfo:
					return 1
				default:
					return 0
				}
			}
			ai := score(si)
			aj := score(sj)
			if ai != aj {
				return ai > aj
			}
			// Within same severity, prefer auto-fixable first for user usefulness.
			if violationsForSample[i].AutoFixable != violationsForSample[j].AutoFixable {
				return violationsForSample[i].AutoFixable
			}
			return false
		})
		// Best-effort: include a small amount of human context in the sample (paths/labels, edge endpoints).
		fragCache := map[string]struct {
			path  string
			label string
		}{}
		resolveFrag := func(id string) (path, label string) {
			id = strings.TrimSpace(id)
			if id == "" || e.fragmentsRepo == nil {
				return "", ""
			}
			if v, ok := fragCache[id]; ok {
				return v.path, v.label
			}
			frag, err := e.fragmentsRepo.Get(policyCtx, id)
			if err != nil || frag == nil {
				fragCache[id] = struct {
					path  string
					label string
				}{}
				return "", ""
			}
			lbl := ""
			if frag.Labels != nil {
				lbl = strings.TrimSpace(frag.Labels["en"])
			}
			out := struct {
				path  string
				label string
			}{path: strings.TrimSpace(frag.CanonicalPath), label: lbl}
			fragCache[id] = out
			return out.path, out.label
		}

		violationsSample := make([]map[string]interface{}, 0, 5)
		for i, v := range violationsForSample {
			if i >= 5 {
				break
			}
			fragPath, fragLabel := resolveFrag(v.FragmentID)
			edgeType := ""
			sourcePath := ""
			targetPath := ""
			if strings.TrimSpace(v.EdgeID) != "" && e.edgesRepo != nil {
				if edge, err := e.edgesRepo.Get(policyCtx, strings.TrimSpace(v.EdgeID)); err == nil && edge != nil {
					edgeType = string(edge.Type)
					sourcePath, _ = resolveFrag(edge.SourceFragmentID)
					targetPath, _ = resolveFrag(edge.TargetFragmentID)
				}
			}
			violationsSample = append(violationsSample, map[string]interface{}{
				"id":            v.ID,
				"type":          v.Type,
				"severity":      v.Severity,
				"description":   v.Description,
				"fragmentId":    v.FragmentID,
				"fragmentPath":  fragPath,
				"fragmentLabel": fragLabel,
				"edgeId":        v.EdgeID,
				"edgeType":      edgeType,
				"sourcePath":    sourcePath,
				"targetPath":    targetPath,
				"actualValue":   v.ActualValue,
				"expectedValue": v.ExpectedValue,
				"autoFixable":   v.AutoFixable,
			})
		}
		// Also include the main job target for a nicer header on the frontend.
		subjectPath := strings.TrimSpace(job.TargetCanonicalPath)
		subjectLabel := ""
		if subjectPath != "" && e.fragmentsRepo != nil {
			if frag, err := e.fragmentsRepo.GetByCanonicalPath(policyCtx, subjectPath); err == nil && frag != nil {
				if frag.Labels != nil {
					subjectLabel = strings.TrimSpace(frag.Labels["en"])
				}
				if strings.TrimSpace(frag.CanonicalPath) != "" {
					subjectPath = strings.TrimSpace(frag.CanonicalPath)
				}
			}
		}
		payload := policy.NewWSPolicyCheckComplete(result, durationMs)
		policyEvent := map[string]interface{}{
			"type":                 payload.Type,
			"tenantId":             payload.TenantID,
			"jobId":                payload.JobID,
			"violationsFound":      payload.ViolationsFound,
			"criticalCount":        payload.CriticalCount,
			"warningCount":         payload.WarningCount,
			"infoCount":            payload.InfoCount,
			"enforcementJobIds":    payload.EnforcementJobIDs,
			"autoFixableCount":     payload.AutoFixableCount,
			"checkDurationMs":      payload.CheckDurationMs,
			"timestamp":            payload.Timestamp,
			"violationsSample":     violationsSample,
			"subjectCanonicalPath": subjectPath,
			"subjectLabel":         subjectLabel,
		}
		if v := strings.TrimSpace(job.ProjectID); v != "" {
			policyEvent["projectId"] = v
		}
		if v := strings.TrimSpace(job.LensID); v != "" {
			policyEvent["lensId"] = v
		}
		e.publish(policyCtx, realtime.EventType("horizon.policy.check_complete"), policyEvent)

		// Optional legacy publisher (non-pubsub, custom topic)
		if e.policyWSPublisher != nil {
			_ = e.policyWSPublisher.PublishPolicyCheckComplete(policyCtx, result, durationMs)
		}
	})
}

// shouldAutoExecuteEnforcement returns true if enforcement jobs should be executed immediately.
// This can be controlled via configuration (default: false for safety).
func (e *ScoutEngine) shouldAutoExecuteEnforcement() bool {
	enabled := strings.TrimSpace(os.Getenv("ENABLE_POLICY_AUTO_ENFORCEMENT"))
	if enabled == "" {
		enabled = "1"
	}
	return enabled == "1" || strings.EqualFold(enabled, "true") || strings.EqualFold(enabled, "yes")
}

// executeEnforcementJobs executes all pending enforcement jobs for a parent job.
func (e *ScoutEngine) executeEnforcementJobs(ctx context.Context, job *models.ScoutingJob) {
	if e.enforcementExecutor == nil || e.enforcementJobsRepo == nil || job == nil {
		return
	}

	startTime := time.Now()
	jobs, err := e.enforcementJobsRepo.ListByParentJob(ctx, job.ID, 1000)
	if err != nil {
		fmt.Printf("[ScoutEngine] Enforcement batch load failed for %s: %v\n", job.ID, err)
		return
	}

	completed := 0
	failed := 0
	skipped := 0

	for _, ej := range jobs {
		if ej == nil || ej.Status != policy.EnforcementStatusPending {
			continue
		}
		if !isAutoExecutableTask(ej.Task.Type) {
			skipped++
			continue
		}

		// Publish "in_progress" before execution for UI feedback.
		inProgress := *ej
		inProgress.Status = policy.EnforcementStatusInProgress
		e.publishEnforcementProgress(ctx, &inProgress, job.ProjectID, job.LensID)

		if _, err := e.enforcementExecutor.ExecuteJob(ctx, ej); err != nil {
			fmt.Printf("[ScoutEngine] Enforcement job failed: parentJob=%s jobId=%s task=%s err=%v\n",
				job.ID, ej.ID, ej.Task.Type, err)
			failed++
		} else if ej.Result != nil && ej.Result.Success {
			completed++
		} else {
			failed++
		}

		e.publishEnforcementProgress(ctx, ej, job.ProjectID, job.LensID)
	}

	durationMs := time.Since(startTime).Milliseconds()
	fmt.Printf("[ScoutEngine] Enforcement batch completed for %s: %d completed, %d failed, %d skipped in %dms\n",
		job.ID, completed, failed, skipped, durationMs)

	if noPendingEnforcementJobs(jobs) {
		e.publishEnforcementBatchComplete(ctx, job, jobs, durationMs)
	}
}

func isAutoExecutableTask(taskType policy.EnforcementTaskType) bool {
	switch taskType {
	case policy.TaskMergeFragments:
		// Auto-merge is allowed only when PolicyChecker marked it auto-fixable (high confidence / strong ID match).
		return true
	case policy.TaskFixPath,
		policy.TaskResolveGeoNames,
		policy.TaskFixLegalForm,
		policy.TaskFixISO2,
		policy.TaskReverseEdge,
		policy.TaskCanonicalizeEdge,
		policy.TaskNormalizeSymmetric:
		return true
	default:
		return false
	}
}

func noPendingEnforcementJobs(jobs []*policy.PolicyEnforcementJob) bool {
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if job.Status == policy.EnforcementStatusPending || job.Status == policy.EnforcementStatusInProgress {
			return false
		}
	}
	return true
}

func (e *ScoutEngine) publishEnforcementProgress(ctx context.Context, job *policy.PolicyEnforcementJob, projectID, lensID string) {
	if e == nil || job == nil {
		return
	}
	msg := policy.NewWSEnforcementProgress(job)
	// Best-effort: enrich message with target object context for UI readability.
	targetPath := ""
	targetLabel := ""
	if e.fragmentsRepo != nil && strings.TrimSpace(msg.TargetID) != "" {
		if frag, err := e.fragmentsRepo.Get(ctx, strings.TrimSpace(msg.TargetID)); err == nil && frag != nil {
			targetPath = strings.TrimSpace(frag.CanonicalPath)
			if frag.Labels != nil {
				targetLabel = strings.TrimSpace(frag.Labels["en"])
			}
		}
	}
	isBadLabel := func(s string) bool {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			return true
		}
		// Avoid "ma", "na", etc. Prefer canonicalPath for such nodes.
		if len(s) <= 3 {
			return true
		}
		if s == "na" || s == "n/a" {
			return true
		}
		return false
	}
	if isBadLabel(targetLabel) {
		targetLabel = ""
	}
	// For edge tasks, we can optionally add edge endpoints later; keep it simple for now.
	if strings.TrimSpace(msg.Message) == "" {
		// Make in-progress messages informative.
		if strings.TrimSpace(targetLabel) != "" && strings.TrimSpace(targetPath) != "" {
			msg.Message = fmt.Sprintf("%s — %s (%s)", strings.TrimSpace(msg.TaskType), targetLabel, targetPath)
		} else if strings.TrimSpace(targetPath) != "" {
			msg.Message = fmt.Sprintf("%s — %s", strings.TrimSpace(msg.TaskType), targetPath)
		}
	}
	payload := map[string]interface{}{
		"type":          msg.Type,
		"jobId":         msg.JobID,
		"parentJobId":   msg.ParentJobID,
		"tenantId":      msg.TenantID,
		"status":        msg.Status,
		"violationType": msg.ViolationType,
		"taskType":      msg.TaskType,
		"targetId":      msg.TargetID,
		"progress":      msg.Progress,
		"message":       msg.Message,
		"timestamp":     msg.Timestamp,
	}
	if targetPath != "" {
		payload["targetCanonicalPath"] = targetPath
	}
	if targetLabel != "" {
		payload["targetLabel"] = targetLabel
	}
	if msg.Success != nil {
		payload["success"] = *msg.Success
	}
	if len(msg.AffectedIDs) > 0 {
		payload["affectedIds"] = msg.AffectedIDs
	}
	if msg.Error != "" {
		payload["error"] = msg.Error
	}
	if v := strings.TrimSpace(projectID); v != "" {
		payload["projectId"] = v
	}
	if v := strings.TrimSpace(lensID); v != "" {
		payload["lensId"] = v
	}
	e.publish(ctx, realtime.EventType("horizon.policy.enforcement_progress"), payload)
}

func (e *ScoutEngine) publishEnforcementBatchComplete(ctx context.Context, job *models.ScoutingJob, jobs []*policy.PolicyEnforcementJob, durationMs int64) {
	if e == nil || job == nil {
		return
	}
	msg := policy.NewWSEnforcementBatchComplete(job.ID, job.TenantID, jobs, durationMs)
	// Best-effort: include a small, user-friendly list of what was fixed/failed.
	type jobItem struct {
		Status              string `json:"status"`
		TaskType            string `json:"taskType"`
		ViolationType       string `json:"violationType,omitempty"`
		TargetID            string `json:"targetId,omitempty"`
		TargetCanonicalPath string `json:"targetCanonicalPath,omitempty"`
		TargetLabel         string `json:"targetLabel,omitempty"`
		Success             *bool  `json:"success,omitempty"`
		Error               string `json:"error,omitempty"`
		Details             string `json:"details,omitempty"`
	}
	jobsSample := make([]jobItem, 0, 8)
	fragCache := map[string]struct {
		path  string
		label string
	}{}
	resolveFrag := func(id string) (string, string) {
		id = strings.TrimSpace(id)
		if id == "" || e.fragmentsRepo == nil {
			return "", ""
		}
		if v, ok := fragCache[id]; ok {
			return v.path, v.label
		}
		frag, err := e.fragmentsRepo.Get(ctx, id)
		if err != nil || frag == nil {
			fragCache[id] = struct {
				path  string
				label string
			}{}
			return "", ""
		}
		lbl := ""
		if frag.Labels != nil {
			lbl = strings.TrimSpace(frag.Labels["en"])
		}
		out := struct {
			path  string
			label string
		}{path: strings.TrimSpace(frag.CanonicalPath), label: lbl}
		fragCache[id] = out
		return out.path, out.label
	}
	for _, j := range jobs {
		if j == nil {
			continue
		}
		// Prioritize failed and completed jobs for the sample.
		if j.Status != policy.EnforcementStatusFailed && j.Status != policy.EnforcementStatusCompleted {
			continue
		}
		item := jobItem{
			Status:   string(j.Status),
			TaskType: string(j.Task.Type),
			TargetID: j.Task.TargetID,
			ViolationType: func() string {
				if j.Violation != nil {
					return string(j.Violation.Type)
				}
				return ""
			}(),
		}
		item.TargetCanonicalPath, item.TargetLabel = resolveFrag(j.Task.TargetID)
		if j.Result != nil {
			item.Success = &j.Result.Success
			item.Error = strings.TrimSpace(j.Result.Error)
			item.Details = strings.TrimSpace(j.Result.Details)
		}
		jobsSample = append(jobsSample, item)
		if len(jobsSample) >= 8 {
			break
		}
	}
	payload := map[string]interface{}{
		"type":               msg.Type,
		"parentJobId":        msg.ParentJobID,
		"tenantId":           msg.TenantID,
		"totalJobs":          msg.TotalJobs,
		"completed":          msg.Completed,
		"failed":             msg.Failed,
		"skipped":            msg.Skipped,
		"durationMs":         msg.DurationMs,
		"timestamp":          msg.Timestamp,
		"fragmentsMerged":    msg.FragmentsMerged,
		"pathsFixed":         msg.PathsFixed,
		"edgesReversed":      msg.EdgesReversed,
		"edgesCanonicalized": msg.EdgesCanonicalized,
		"edgesDeleted":       msg.EdgesDeleted,
		"jobsSample":         jobsSample,
	}
	if v := strings.TrimSpace(job.ProjectID); v != "" {
		payload["projectId"] = v
	}
	if v := strings.TrimSpace(job.LensID); v != "" {
		payload["lensId"] = v
	}
	e.publish(ctx, realtime.EventType("horizon.policy.enforcement_batch_complete"), payload)
}

// getSkeletonContextCached returns skeleton context with 3-level cache: SessionGraph (L0) → Redis (L1) → DB build.
func (e *ScoutEngine) getSkeletonContextCached(ctx context.Context, canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return ""
	}
	sg := SessionGraphFromContext(ctx)
	if sg != nil {
		if cached, ok := sg.GetSkeletonContext(cp); ok {
			return cached
		}
	}
	if e.redisClient != nil {
		rctx, cancel := context.WithTimeout(ctx, 70*time.Millisecond)
		val, err := e.redisClient.Get(rctx, "h:skel:"+engineSha1Hex(cp)).Result()
		cancel()
		if err == nil && val != "" {
			if sg != nil {
				sg.SetSkeletonContext(cp, val)
			}
			return val
		}
	}
	result := e.buildGraphSkeletonContext(ctx, cp)
	if sg != nil {
		sg.SetSkeletonContext(cp, result)
	}
	if e.redisClient != nil && result != "" {
		rctx, cancel := context.WithTimeout(ctx, 70*time.Millisecond)
		_ = e.redisClient.Set(rctx, "h:skel:"+engineSha1Hex(cp), result, 30*time.Minute).Err()
		cancel()
	}
	return result
}

func engineSha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// extractStringSlice extracts a string slice from job state.
func (e *ScoutEngine) extractStringSlice(state map[string]interface{}, key string) []string {
	if state == nil {
		return nil
	}
	v, ok := state[key]
	if !ok || v == nil {
		return nil
	}

	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		result := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
