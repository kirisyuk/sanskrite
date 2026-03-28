package scout

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/regstress"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplysemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func parseOptionalISODate(s *string) *time.Time {
	if s == nil {
		return nil
	}
	raw := strings.TrimSpace(*s)
	if raw == "" {
		return nil
	}
	// Preferred: YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		tt := t.UTC()
		return &tt
	}
	// Accept RFC3339 variants as a fallback.
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		tt := t.UTC()
		return &tt
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		tt := t.UTC()
		return &tt
	}
	return nil
}

func mergeQualifiers(dst map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	if dst == nil {
		dst = map[string]interface{}{}
	}
	if len(extra) == 0 {
		return dst
	}
	for k, v := range extra {
		kk := strings.TrimSpace(k)
		if kk == "" {
			continue
		}
		// Do not overwrite existing keys (baseQualifiers/raw debug fields win).
		if _, exists := dst[kk]; exists {
			continue
		}
		dst[kk] = v
	}
	return dst
}

func isDomainScaffoldRelationType(rt string) bool {
	rt = strings.TrimSpace(taxonomy.NormalizeRelationType(rt))
	return IsDomainScaffoldRelationType(models.DependencyType(rt))
}

func canonicalDomainAnchorRelationType(rawType, targetPath string) string {
	rt := strings.TrimSpace(taxonomy.NormalizeRelationType(rawType))
	if rt == "" {
		rt = string(models.DependencyTypeFocusesOn)
	}
	if isDomainScaffoldRelationType(rt) {
		return rt
	}
	// Domain -> MECHANISM/DECISION_POINT must use strategic domain-scaffold relation.
	// Fallback to FOCUSES_ON here caused systematic quarantine (direction contract mismatch).
	targetType := InferFragmentTypeFromCanonicalPath(strings.TrimSpace(targetPath))
	switch targetType {
	case models.FragmentTypeMechanism, models.FragmentTypeDecisionPoint:
		return string(models.DependencyTypeSurfaces)
	default:
		return string(models.DependencyTypeFocusesOn)
	}
}

func sanitizeDomainResearchRelations(domainCanonicalPath, contractType string, rels []agents.DiscoveredRelation) []agents.DiscoveredRelation {
	if len(rels) == 0 {
		return nil
	}
	domainCanonicalPath = strings.TrimSpace(domainCanonicalPath)
	modes := taxonomy.NormalizeDomainModes(domain.DomainModesForContractTypes([]string{strings.TrimSpace(contractType)}))
	allowlist := EdgeAllowlistByDomainModes(modes)

	out := make([]agents.DiscoveredRelation, 0, len(rels))
	for _, rel := range rels {
		targetPath := strings.TrimSpace(rel.TargetCanonicalPath)
		if targetPath == "" {
			continue
		}
		rt := strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
		if rt == "" {
			rt = string(models.DependencyTypeFocusesOn)
		}

		sourcePath := strings.TrimSpace(rel.SourceCanonicalPath)
		if sourcePath == "" {
			sourcePath = domainCanonicalPath
		}

		// Domain contracts are projection anchors and should not have incoming edges from non-domain sources.
		// Canonicalize legacy/non-strict outputs into domain->entity scaffold relation.
		if strings.HasPrefix(targetPath, "/domain/") && !strings.HasPrefix(sourcePath, "/domain/") {
			rel.SourceCanonicalPath = targetPath
			rel.TargetCanonicalPath = sourcePath
			sourcePath = rel.SourceCanonicalPath
			targetPath = rel.TargetCanonicalPath
		}

		if strings.HasPrefix(sourcePath, "/domain/") {
			// Domain anchor edges must remain in domain-scaffold relation space.
			rt = canonicalDomainAnchorRelationType(rt, targetPath)
		} else {
			// For non-domain source relations (explicit source from research), enforce domain-mode allowlist.
			if allowlist != nil && !AllowlistContains(allowlist, models.DependencyType(rt)) && !isDomainScaffoldRelationType(rt) {
				continue
			}
		}

		rel.RelationType = rt
		out = append(out, rel)
	}
	return out
}

// extractRadiusKmFromQualifiers extracts zone radius (km) from relation qualifiers.
// Supports "radiusKm" and "radius_km" keys. Returns (value, true) when valid and > 0.
func extractRadiusKmFromQualifiers(q map[string]interface{}) (float64, bool) {
	if q == nil {
		return 0, false
	}
	for _, key := range []string{"radiusKm", "radius_km"} {
		if raw, ok := q[key]; ok && raw != nil {
			if v, ok := parseNumericQualifier(raw); ok && v > 0 {
				return v, true
			}
		}
	}
	return 0, false
}

func supplyChainSubstitutabilityHeuristic(edgeType models.DependencyType, srcPath, dstPath string) float64 {
	_ = srcPath
	switch edgeType {
	case models.DependencyTypeSuppliesTo:
		if strings.HasPrefix(strings.TrimSpace(dstPath), "/product/") {
			return 0.24
		}
		return 0.40
	case models.DependencyTypeTransformsInto, models.DependencyTypeAssemblesInto, models.DependencyTypeUsedIn:
		return 0.44
	case models.DependencyTypeExtractsFrom:
		return 0.36
	default:
		return 0.52
	}
}

func parseStateInt(v interface{}, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if i, err := strconv.Atoi(t); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int(f)
		}
		return def
	default:
		return def
	}
}

func parseStateBool(v interface{}, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			return def
		}
		if t == "true" || t == "1" || t == "yes" {
			return true
		}
		if t == "false" || t == "0" || t == "no" {
			return false
		}
		return def
	case int:
		return t != 0
	case int32:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	case float32:
		return t != 0
	default:
		return def
	}
}

func hasDomainsInState(state map[string]interface{}) bool {
	if state == nil {
		return false
	}
	v, ok := state["domains"]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case []interface{}:
		return len(t) > 0
	case []map[string]interface{}:
		return len(t) > 0
	case []string:
		return len(t) > 0
	}
	return false
}

func normalizeStrategicMechanismPath(path, title, description string) (string, bool) {
	cp := strings.TrimSpace(path)
	if cp == "" {
		slug := taxonomy.Slugify(strings.TrimSpace(title))
		if slug == "" {
			return "", false
		}
		cat := inferMechanismCategoryFromText(title, description)
		return taxonomy.JoinPath("mechanism", cat, slug), true
	}
	if !strings.HasPrefix(cp, "/mechanism/") {
		return cp, false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 3 {
		slug := taxonomy.Slugify(strings.TrimSpace(title))
		if slug == "" {
			slug = lastPathSlug(cp)
		}
		if slug == "" {
			return cp, false
		}
		cat := inferMechanismCategoryFromText(title, description)
		fixed := taxonomy.JoinPath("mechanism", cat, slug)
		return fixed, fixed != cp
	}

	cat := strings.ToLower(strings.TrimSpace(parts[1]))
	slug := taxonomy.Slugify(strings.TrimSpace(parts[2]))
	if slug == "" {
		slug = taxonomy.Slugify(strings.TrimSpace(title))
	}
	if slug == "" {
		return cp, false
	}

	if !isValidMechanismCategory(cat) {
		cat = inferMechanismCategoryFromText(title, description)
	}
	fixed := taxonomy.JoinPath("mechanism", cat, slug)
	return fixed, fixed != cp
}

func isValidMechanismCategory(cat string) bool {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "regulatory", "economic", "technological", "contractual", "political":
		return true
	default:
		return false
	}
}

func inferMechanismCategoryFromText(title, description string) string {
	text := strings.ToLower(strings.TrimSpace(title + " " + description))
	switch {
	case strings.Contains(text, "license"), strings.Contains(text, "regulat"), strings.Contains(text, "sanction"), strings.Contains(text, "permit"), strings.Contains(text, "compliance"), strings.Contains(text, "approval"), strings.Contains(text, "export control"):
		return "regulatory"
	case strings.Contains(text, "subsid"), strings.Contains(text, "pricing"), strings.Contains(text, "capacity"), strings.Contains(text, "allocation"), strings.Contains(text, "tariff"), strings.Contains(text, "tax"), strings.Contains(text, "incentive"), strings.Contains(text, "funding"):
		return "economic"
	case strings.Contains(text, "patent"), strings.Contains(text, "standard"), strings.Contains(text, "ip "), strings.Contains(text, "intellectual property"), strings.Contains(text, "technology"), strings.Contains(text, "proprietary"):
		return "technological"
	case strings.Contains(text, "contract"), strings.Contains(text, "agreement"), strings.Contains(text, "exclusiv"), strings.Contains(text, "offtake"), strings.Contains(text, "joint venture"):
		return "contractual"
	case strings.Contains(text, "diplomat"), strings.Contains(text, "alliance"), strings.Contains(text, "geopolit"), strings.Contains(text, "treaty"), strings.Contains(text, "politic"):
		return "political"
	default:
		return "regulatory"
	}
}

func containsUpperLetter(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// bestEffortTitleCase converts "riyadh" -> "Riyadh", "four points sheraton" -> "Four Points Sheraton".
// It is intentionally conservative: it only title-cases when the string has no uppercase letters.
func bestEffortTitleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if containsUpperLetter(s) {
		return s
	}
	// Humanize mild slug artifacts if present.
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	for i, w := range words {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func isPlaceholderToken(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	switch l {
	case "", "na", "n/a", "none", "null", "unknown", "tbd":
		return true
	default:
		return false
	}
}

func bestEffortLabelFromCanonicalPath(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(parts[i])
		if seg == "" || isPlaceholderToken(seg) {
			continue
		}
		return seg
	}
	return ""
}

// marketGeoLabel returns a human-readable label for the geo segment of a market path.
// e.g. "iq" → "Iraq", "global" → "Global", "gcc" → "GCC"
func marketGeoLabel(geoSlug string) string {
	geo := strings.ToLower(strings.TrimSpace(geoSlug))
	if geo == "" {
		return ""
	}
	if n := countryNameByISO2(geo); n != "" {
		return n
	}
	switch geo {
	case "global", "int":
		return "Global"
	case "gcc":
		return "GCC"
	case "eu":
		return "EU"
	case "asean":
		return "ASEAN"
	case "opec":
		return "OPEC"
	case "brics":
		return "BRICS"
	case "eaeu":
		return "EAEU"
	default:
		return bestEffortTitleCase(strings.ReplaceAll(geo, "-", " "))
	}
}

// marketDisplayLabel produces a deterministic, informative title for a market from its canonical path.
// Template: "{Country} {Leaf Segment} Market" for all levels.
// L1 /market/{geo}/{domain}                         → "Iraq Agriculture Market"
// L2 /market/{geo}/{domain}/{vertical}              → "Iraq Crop Production Market"
// L3+ /market/{geo}/{domain}/{vertical}/{sub}/...   → "Iraq Rice Production Market"
func marketDisplayLabel(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || !strings.HasPrefix(cp, "/market/") {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	geo := marketGeoLabel(parts[1])
	if geo == "" {
		geo = bestEffortTitleCase(parts[1])
	}
	leaf := parts[len(parts)-1]
	leafLabel := bestEffortTitleCase(strings.ReplaceAll(leaf, "-", " "))
	return fmt.Sprintf("%s %s Market", geo, leafLabel)
}

// isSubstantiveMarketLabel returns true if the LLM-provided label is meaningful enough
// to keep instead of replacing with the structured template.
// A label is substantive if it is non-empty, contains a space (not a bare slug),
// and doesn't just repeat the last path segment.
func isSubstantiveMarketLabel(label, canonicalPath string) bool {
	if label == "" {
		return false
	}
	if !strings.Contains(label, " ") {
		return false
	}
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	lastSeg := ""
	if len(parts) > 0 {
		lastSeg = strings.ReplaceAll(parts[len(parts)-1], "-", " ")
	}
	if strings.EqualFold(label, lastSeg) {
		return false
	}
	return true
}

func hasUnknownGeoSegments(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	if !strings.HasPrefix(p, "/loc/") {
		return false
	}
	_, level1, level2, ok := taxonomy.ParseLocCanonicalPath(p)
	if !ok {
		return true
	}
	return level1 == "" || level2 == ""
}

func hasUnknownJurisdiction(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(p, "/org/na/") || strings.HasPrefix(p, "/gov/na/")
}

func shouldSkipRecursivePlaceholderTarget(path string) bool {
	return hasUnknownGeoSegments(path) || hasUnknownJurisdiction(path)
}

func isSoftDeniedRelationForRecurse(sourceType, targetType models.FragmentType, _ string, targetPath, relationType string) bool {
	rt := strings.TrimSpace(relationType)
	if rt == "" {
		return false
	}
	if shouldSkipRecursivePlaceholderTarget(targetPath) {
		return true
	}
	if sourceType == models.FragmentTypeGeoRegion {
		switch rt {
		case string(models.DependencyTypeOperatesIn), string(models.DependencyTypeParticipatesIn):
			if targetType == models.FragmentTypeMarket || strings.HasPrefix(strings.TrimSpace(targetPath), "/market/") {
				return true
			}
		}
	}
	return false
}

func bestEffortDisplayLabel(label string, canonicalPath string) string {
	label = strings.TrimSpace(label)
	if strings.HasPrefix(canonicalPath, "/market/") {
		if candidate := marketDisplayLabel(canonicalPath); candidate != "" {
			if !isSubstantiveMarketLabel(label, canonicalPath) {
				return candidate
			}
		}
	}
	if strings.HasPrefix(canonicalPath, "/loc/") {
		cc, level1, level2, ok := taxonomy.ParseLocCanonicalPath(canonicalPath)
		if ok && cc != "" && level1 == "" && level2 == "" {
			if label == "" || len(label) <= 2 || strings.EqualFold(label, cc) {
				if n := countryNameByISO2(cc); n != "" {
					return n
				}
			}
		}
	}
	if label == "" {
		label = bestEffortLabelFromCanonicalPath(canonicalPath)
	}
	return bestEffortTitleCase(label)
}

func isPlaceholderDisplayLabel(label string) bool {
	return isPlaceholderToken(label)
}

func shouldRefreshDisplayLabel(current string, canonicalPath string) bool {
	cur := strings.TrimSpace(current)
	cp := strings.TrimSpace(canonicalPath)
	if cur == "" {
		return true
	}
	if isPlaceholderDisplayLabel(cur) {
		return true
	}
	if strings.HasPrefix(cur, "/") || strings.EqualFold(cur, cp) {
		return true
	}
	if strings.HasPrefix(cp, "/loc/") {
		lcur := strings.ToLower(cur)
		cc, level1, level2, ok := taxonomy.ParseLocCanonicalPath(cp)
		if ok && level1 == "" && level2 == "" && (lcur == "na" || lcur == cc) {
			return true
		}
	}
	return false
}

func buildDomainTitle(contractType string, canonicalPath string, facets map[string][]string) string {
	ct := strings.ToLower(strings.TrimSpace(contractType))
	base := ""

	// For "other" contract type, extract custom slug from canonical path.
	if ct == "other" {
		slug := extractOtherSlugFromDomainPath(canonicalPath)
		if slug != "" {
			base = bestEffortTitleCase(strings.ReplaceAll(slug, "-", " "))
		} else {
			base = "Other"
		}
	} else {
		switch ct {
		// Supply chain / industry
		case "supply-chain":
			base = "Supply chain"
		case "value-chain":
			base = "Value chain"
		case "logistics-flow":
			base = "Logistics flow"
		case "trade-flow":
			base = "Trade flow"
		case "sourcing-map":
			base = "Sourcing map"
		case "procurement-map":
			base = "Procurement map"

		// Risk / geopolitics
		case "risk-assessment":
			base = "Risk assessment"
		case "geopolitical-risk":
			base = "Geopolitical risk"
		case "bilateral-relations":
			base = "Bilateral relations"
		case "sanctions-exposure":
			base = "Sanctions exposure"
		case "conflict-dynamics":
			base = "Conflict dynamics"

		// Markets / opportunity
		case "market-opportunity":
			base = "Market opportunity"
		case "competitive-landscape":
			base = "Competitive landscape"
		case "market-entry":
			base = "Market entry"
		case "market-sizing":
			base = "Market sizing"
		case "demand-forecast":
			base = "Demand forecast"
		case "pricing-dynamics":
			base = "Pricing dynamics"

		// Investment / finance
		case "investment-thesis":
			base = "Investment thesis"
		case "funding-landscape":
			base = "Funding landscape"
		case "exit-strategy":
			base = "Exit strategy"
		case "valuation-drivers":
			base = "Valuation drivers"
		case "portfolio-exposure":
			base = "Portfolio exposure"

		// Technology / innovation
		case "technology-landscape":
			base = "Technology landscape"
		case "innovation-map":
			base = "Innovation map"
		case "patent-landscape":
			base = "Patent landscape"
		case "rd-landscape":
			base = "R&D landscape"
		case "adoption-curve":
			base = "Adoption curve"
		case "disruption-analysis":
			base = "Disruption analysis"

		// Ecosystem / stakeholder
		case "ecosystem-mapping":
			base = "Ecosystem mapping"
		case "stakeholder-mapping":
			base = "Stakeholder mapping"
		case "partnership-landscape":
			base = "Partnership landscape"
		case "influence-network":
			base = "Influence network"
		case "power-dynamics":
			base = "Power dynamics"

		// Policy / regulation
		case "policy-impact":
			base = "Policy impact"
		case "regulatory-impact":
			base = "Regulatory impact"
		case "policy-and-regulation":
			base = "Policy and regulation"
		case "compliance-landscape":
			base = "Compliance landscape"
		case "security-and-compliance":
			base = "Security and compliance"
		case "licensing-requirements":
			base = "Licensing requirements"
		case "esg-landscape":
			base = "ESG landscape"

		// Scenarios / trends
		case "scenario-analysis":
			base = "Scenario analysis"
		case "macro-trends":
			base = "Macro trends"
		case "megatrends":
			base = "Megatrends"
		case "future-outlook":
			base = "Future outlook"

		// Industry analysis
		case "industry-analysis":
			base = "Industry analysis"

		default:
			base = bestEffortTitleCase(strings.ReplaceAll(ct, "-", " "))
		}
	}

	if strings.TrimSpace(base) == "" {
		base = "Domain"
	}

	// Prefer geo facet as the primary human hint (stable order).
	gs := []string(nil)
	if facets != nil {
		if v, ok := facets["geo"]; ok && len(v) > 0 {
			gs = append([]string(nil), v...)
		}
	}
	if len(gs) > 0 {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(gs))
		for _, g := range gs {
			g = strings.TrimSpace(strings.ToLower(g))
			if len(g) != 2 {
				continue
			}
			if _, ok := seen[g]; ok {
				continue
			}
			seen[g] = struct{}{}
			out = append(out, strings.ToUpper(g))
		}
		sort.Strings(out)
		if len(out) > 0 {
			return fmt.Sprintf("%s — %s", base, strings.Join(out, " · "))
		}
	}

	return base
}

// extractOtherSlugFromDomainPath extracts custom slug from /domain/v1/other/{slug}/... paths.
func extractOtherSlugFromDomainPath(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/domain/v1/other/") {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// ["domain", "v1", "other", slug, ...]
	if len(parts) < 4 {
		return ""
	}
	return strings.TrimSpace(parts[3])
}

// copyDomainModeStateToChild copies domainMode, domainModes, and domains from parent state into child state
// so child jobs run the same domain phases (Phase 1 Research / Phase 2 Critic by mode).
func copyDomainModeStateToChild(parentState map[string]interface{}, childState map[string]interface{}) {
	if parentState == nil || childState == nil {
		return
	}
	for _, key := range []string{
		"domainMode", "domainModes", "domains",
		ExecutionPlanStateKey,
		"maxRootJobs", "maxRecursionDepth", "maxChildrenPerJob", "maxCascadeJobs", "maxSupplyChainQueries",
		"budgetCeilingUSD", "budgetCeilingOutputTokens", "stopOnBudgetExhaustion",
		"supplyChainMode", "supplyChainPhase", "supplyChainPhases", "supplyChainExpandEnabled", "supplyChainExpandMaxNodes", "supplyChainGraphBuildMode",
		"planModeResearchDone", "supplyChainPlanMode",
	} {
		if v, ok := parentState[key]; ok && v != nil {
			childState[key] = v
		}
	}
	// Derive planModeResearchDone from any plan mode when not already set.
	if _, ok := childState["planModeResearchDone"]; !ok {
		if parseStateBool(parentState["supplyChainPlanMode"], false) {
			childState["planModeResearchDone"] = true
		} else if dr, ok := parentState["domainResearch"].(map[string]interface{}); ok {
			if parseStateBool(dr["planMode"], false) {
				childState["planModeResearchDone"] = true
			}
		}
	}
}

func (e *ScoutEngine) estimateCascadeRemainingCapacity(
	ctx context.Context,
	job *models.ScoutingJob,
	lensID string,
	cascadeID string,
	maxCascadeJobs int,
) (remaining int, used int) {
	if maxCascadeJobs <= 0 {
		return 0, 0
	}
	if e == nil || e.scoutingJobsRepo == nil || job == nil {
		return maxCascadeJobs, 0
	}
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		lensID = strings.TrimSpace(job.LensID)
	}
	if lensID == "" && job.State != nil {
		lensID = parseStateString(job.State["lensId"])
	}
	cascadeID = strings.TrimSpace(cascadeID)
	if cascadeID == "" && job.State != nil {
		cascadeID = parseStateString(job.State["cascadeRootJobId"])
	}
	if cascadeID == "" {
		cascadeID = strings.TrimSpace(job.ID)
	}
	if strings.TrimSpace(job.TenantID) == "" || strings.TrimSpace(job.ProjectID) == "" || lensID == "" || cascadeID == "" {
		return maxCascadeJobs, 0
	}

	queryLimit := maxCascadeJobs + 256
	if queryLimit < 400 {
		queryLimit = 400
	}
	if queryLimit > 5000 {
		queryLimit = 5000
	}

	jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, queryLimit)
	if err != nil {
		return maxCascadeJobs, 0
	}
	for _, j := range jobs {
		if j == nil {
			continue
		}
		rootID := ""
		if j.State != nil {
			rootID = parseStateString(j.State["cascadeRootJobId"])
		}
		if strings.TrimSpace(rootID) == "" {
			rootID = strings.TrimSpace(j.ID)
		}
		if strings.TrimSpace(rootID) != cascadeID {
			continue
		}
		used++
	}
	remaining = maxCascadeJobs - used
	if remaining < 0 {
		remaining = 0
	}
	return remaining, used
}

// getJobDomainModes returns domain mode(s) from job state for composable Critic/Research.
func getJobDomainModes(job *models.ScoutingJob) []string {
	if job == nil || job.State == nil {
		return nil
	}
	if v, ok := job.State["domainModes"]; ok && v != nil {
		return parseStateStringSlice(v)
	}
	if v, ok := job.State["domainMode"]; ok && v != nil {
		s := parseStateString(v)
		if s != "" {
			return []string{s}
		}
	}
	return nil
}

func parseStateString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func parseStateStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		// comma-separated convenience
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
	}
}

type overlayGraphContext struct {
	Nodes []overlayGraphContextNode `json:"nodes"`
	Edges []overlayGraphContextEdge `json:"edges,omitempty"`
}

type overlayGraphContextNode struct {
	CanonicalPath string `json:"canonicalPath"`
	Type          string `json:"type"`
	Label         string `json:"label,omitempty"`
}

type overlayGraphContextEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

func (e *ScoutEngine) buildLensGraphContext(ctx context.Context, lens *models.Lens, maxNodes, maxEdges int) string {
	if e == nil || lens == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return ""
	}
	if maxNodes <= 0 {
		maxNodes = 40
	}
	if maxEdges <= 0 {
		maxEdges = 120
	}

	// 1) Direct fragment-backed scope nodes.
	// Non-fragment scope entries (/query, /domain, /artifact) are handled elsewhere.
	directPaths := make([]string, 0, len(lens.Scope))
	seenPath := map[string]struct{}{}
	for _, p := range lens.Scope {
		p = strings.TrimSpace(p)
		if p == "" || models.IsNonFragmentScopeEntry(p) {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(p); err != nil {
			continue
		}
		if _, ok := seenPath[p]; ok {
			continue
		}
		seenPath[p] = struct{}{}
		directPaths = append(directPaths, p)
		if len(directPaths) >= maxNodes {
			break
		}
	}
	if len(directPaths) == 0 {
		return ""
	}

	directFragsRaw, err := e.fragmentsRepo.ListByCanonicalPaths(ctx, directPaths)
	if err != nil || len(directFragsRaw) == 0 {
		return ""
	}

	// Keep direct node order aligned to lens.Scope (ListByCanonicalPaths doesn't guarantee ordering).
	fragByPath := make(map[string]*models.HorizonFragment, len(directFragsRaw))
	for _, f := range directFragsRaw {
		if f == nil {
			continue
		}
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := fragByPath[cp]; ok {
			continue
		}
		fragByPath[cp] = f
	}
	directFrags := make([]*models.HorizonFragment, 0, len(directPaths))
	for _, cp := range directPaths {
		if f := fragByPath[strings.TrimSpace(cp)]; f != nil {
			directFrags = append(directFrags, f)
		}
	}
	if len(directFrags) == 0 {
		return ""
	}

	nodeByID := make(map[string]*models.HorizonFragment, len(directFrags))
	nodes := make([]overlayGraphContextNode, 0, maxNodes)
	for _, f := range directFrags {
		if f == nil || strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.CanonicalPath) == "" {
			continue
		}
		if _, ok := nodeByID[f.ID]; ok {
			continue
		}
		nodeByID[f.ID] = f
		label := strings.TrimSpace(f.Labels["en"])
		if label == "" {
			label = strings.TrimSpace(f.CanonicalPath)
		}
		nodes = append(nodes, overlayGraphContextNode{
			CanonicalPath: strings.TrimSpace(f.CanonicalPath),
			Type:          string(f.Type),
			Label:         label,
		})
	}
	if len(nodes) == 0 {
		return ""
	}

	// 2) Expand with 1st-order neighbors (only via "strong" edges) until we hit maxNodes.
	// This provides more useful context when LensArtifacts are empty / unavailable.
	strongTypes := make(map[models.DependencyType]struct{})
	for _, spec := range models.DefaultAxisSpecs() {
		for _, t := range spec.StrongEdgeTypes {
			tt := models.DependencyType(strings.TrimSpace(t))
			if tt != "" {
				strongTypes[tt] = struct{}{}
			}
		}
	}
	edgeScanLimit := 60 // per direct node (out + in), sorted by criticality on backend
	edgesPool := make([]*models.DependencyEdge, 0, len(directFrags)*8)
	edgePoolSeen := map[string]struct{}{}

	neighborsSeen := map[string]struct{}{}
	for id := range nodeByID {
		neighborsSeen[id] = struct{}{}
	}

	for _, f := range directFrags {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}

		outEdges, _ := e.edgesRepo.ListBySource(ctx, "global", f.ID, nil, edgeScanLimit)
		inEdges, _ := e.edgesRepo.ListByTarget(ctx, "global", f.ID, nil, edgeScanLimit)
		all := append(outEdges, inEdges...)

		for _, ed := range all {
			if ed == nil {
				continue
			}

			// Keep a pool for later edge selection (no extra reads).
			pk := fmt.Sprintf("%s|%s|%s", ed.SourceFragmentID, ed.TargetFragmentID, ed.Type)
			if _, ok := edgePoolSeen[pk]; !ok {
				edgePoolSeen[pk] = struct{}{}
				edgesPool = append(edgesPool, ed)
			}

			if len(nodeByID) >= maxNodes {
				continue
			}
			if _, ok := strongTypes[ed.Type]; !ok {
				continue
			}

			otherID := ""
			if ed.SourceFragmentID == f.ID {
				otherID = ed.TargetFragmentID
			} else if ed.TargetFragmentID == f.ID {
				otherID = ed.SourceFragmentID
			}
			otherID = strings.TrimSpace(otherID)
			if otherID == "" {
				continue
			}
			if _, ok := neighborsSeen[otherID]; ok {
				continue
			}

			nf, nerr := e.fragmentsRepo.Get(ctx, otherID)
			if nerr != nil || nf == nil {
				continue
			}
			if strings.TrimSpace(nf.ID) == "" || strings.TrimSpace(nf.CanonicalPath) == "" {
				continue
			}
			cp := strings.TrimSpace(nf.CanonicalPath)
			if strings.HasPrefix(cp, "/domain/") {
				// Avoid dragging other domains into the fallback context.
				continue
			}
			if taxonomy.ValidateCanonicalPath(cp) != nil {
				continue
			}

			neighborsSeen[otherID] = struct{}{}
			nodeByID[otherID] = nf

			label := strings.TrimSpace(nf.Labels["en"])
			if label == "" {
				label = cp
			}
			nodes = append(nodes, overlayGraphContextNode{
				CanonicalPath: cp,
				Type:          string(nf.Type),
				Label:         label,
			})
			if len(nodeByID) >= maxNodes {
				break
			}
		}
		if len(nodeByID) >= maxNodes {
			break
		}
	}

	// 3) Build edge list from pooled direct-node edges (and only those that connect included nodes).
	outEdgeSeen := map[string]struct{}{}
	edges := make([]overlayGraphContextEdge, 0, maxEdges)
	for _, ed := range edgesPool {
		if ed == nil {
			continue
		}
		src := nodeByID[ed.SourceFragmentID]
		tgt := nodeByID[ed.TargetFragmentID]
		if src == nil || tgt == nil {
			continue
		}
		key := fmt.Sprintf("%s|%s|%s", ed.SourceFragmentID, ed.TargetFragmentID, ed.Type)
		if _, ok := outEdgeSeen[key]; ok {
			continue
		}
		outEdgeSeen[key] = struct{}{}
		edges = append(edges, overlayGraphContextEdge{
			Source: strings.TrimSpace(src.CanonicalPath),
			Target: strings.TrimSpace(tgt.CanonicalPath),
			Type:   string(ed.Type),
		})
		if len(edges) >= maxEdges {
			break
		}
	}

	out := overlayGraphContext{
		Nodes: nodes,
		Edges: edges,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// buildLensArtifactsContext builds graph context from LensArtifacts instead of lens.Scope.
// This is the preferred method for Phase 2 Strategic Overlay as it uses finalized
// entities and edges from jobs, providing richer context than just scope paths.
//
// Strategy:
// 1. Fetch UNPROCESSED artifacts first (incremental work).
// 2. Fetch recent PROCESSED artifacts to provide context (anchors).
// 3. Merge and dedup.
func (e *ScoutEngine) buildLensArtifactsContext(ctx context.Context, lensID string, maxNodes, maxEdges int) string {
	if e == nil || e.lensArtifactsRepo == nil || lensID == "" {
		return ""
	}
	if maxNodes <= 0 {
		maxNodes = 50
	}
	if maxEdges <= 0 {
		maxEdges = 150
	}

	// 1. Fetch Unprocessed (priority)
	unprocessed, err := e.lensArtifactsRepo.ListUnprocessedForStrategic(ctx, lensID, maxNodes)
	if err != nil {
		unprocessed = []*models.LensArtifact{}
	}

	// 2. Fetch Recent Processed (context)
	// We want enough context to connect the new nodes to existing graph structure.
	// Fetch slightly more to account for overlap.
	contextLimit := maxNodes
	if len(unprocessed) > 0 {
		contextLimit = maxNodes - len(unprocessed)
		if contextLimit < 10 {
			contextLimit = 10 // Minimum context floor
		}
	}
	recent, err := e.lensArtifactsRepo.ListByLens(ctx, lensID, contextLimit)
	if err != nil {
		recent = []*models.LensArtifact{}
	}

	// 3. Merge
	merged := make([]*models.LensArtifact, 0, len(unprocessed)+len(recent))
	seen := make(map[string]struct{})

	add := func(list []*models.LensArtifact) {
		for _, a := range list {
			if _, ok := seen[a.ID]; !ok {
				seen[a.ID] = struct{}{}
				merged = append(merged, a)
			}
		}
	}
	add(unprocessed)
	add(recent)

	if len(merged) == 0 {
		return ""
	}

	// Build context using LensArtifactContext (simplified IDs for LLM)
	artContext := models.NewLensArtifactContext(merged)
	if artContext == nil || (len(artContext.Fragments) == 0 && len(artContext.Edges) == 0) {
		return ""
	}

	// Convert to overlayGraphContext format
	nodes := make([]overlayGraphContextNode, 0, len(artContext.Fragments))
	for _, f := range artContext.Fragments {
		nodes = append(nodes, overlayGraphContextNode{
			CanonicalPath: f.Path,
			Type:          f.Type,
			Label:         f.Label,
		})
	}

	edges := make([]overlayGraphContextEdge, 0, len(artContext.Edges))
	fragShortToPath := make(map[string]string)
	for _, f := range artContext.Fragments {
		fragShortToPath[f.ShortID] = f.Path
	}
	for _, ed := range artContext.Edges {
		srcPath := fragShortToPath[ed.SourceID]
		tgtPath := fragShortToPath[ed.TargetID]
		if srcPath == "" {
			srcPath = ed.SourceID
		}
		if tgtPath == "" {
			tgtPath = ed.TargetID
		}
		edges = append(edges, overlayGraphContextEdge{
			Source: srcPath,
			Target: tgtPath,
			Type:   ed.Type,
		})
	}

	out := overlayGraphContext{
		Nodes: nodes,
		Edges: edges,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func (e *ScoutEngine) getOrCreateFragmentByPath(
	ctx context.Context,
	canonicalPath string,
	fragType models.FragmentType,
	label string,
	description string,
	provenanceMethod string,
	sourceID string,
) (*models.HorizonFragment, error) {
	if e == nil || e.fragmentsRepo == nil {
		return nil, fmt.Errorf("fragments repo not available")
	}
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return nil, fmt.Errorf("canonical path is empty")
	}
	if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
		return nil, err
	}
	if frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); err == nil && frag != nil {
		updated := false
		if frag.Labels == nil {
			frag.Labels = map[string]string{}
		}
		candidateLabel := bestEffortDisplayLabel(label, cp)
		if shouldRefreshDisplayLabel(frag.Labels["en"], cp) && strings.TrimSpace(candidateLabel) != "" {
			frag.Labels["en"] = strings.TrimSpace(candidateLabel)
			updated = true
		}
		if strings.TrimSpace(description) != "" && strings.TrimSpace(frag.Description) == "" {
			frag.Description = strings.TrimSpace(description)
			updated = true
		}
		if updated {
			_ = e.fragmentsRepo.Update(ctx, frag)
		}
		e.ensureOrgGeoFallbackLink(ctx, frag, provenanceMethod, sourceID)
		return frag, nil
	}
	if fragType == "" {
		fragType = InferFragmentTypeFromCanonicalPath(cp)
	}
	label = bestEffortDisplayLabel(label, cp)
	now := time.Now().UTC()
	newFrag := &models.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: cp,
		Type:          fragType,
		Status:        string(models.FragmentStatusActive),
		State:         map[string]models.MetricValue{},
		Labels:        map[string]string{"en": strings.TrimSpace(label)},
		Description:   strings.TrimSpace(description),
		Governance:    models.Governance{Owner: "SYSTEM", Status: "ACTIVE"},
		Provenance: models.Provenance{
			Method:    provenanceMethod,
			SourceIDs: []string{strings.TrimSpace(sourceID)},
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:      now,
			LastUpdatedAt:  now,
			LastVerifiedAt: time.Time{},
		},
	}
	if _, err := e.fragmentsRepo.Create(ctx, newFrag); err != nil {
		return nil, err
	}
	e.ensureOrgGeoFallbackLink(ctx, newFrag, provenanceMethod, sourceID)
	return newFrag, nil
}

// ensureOrgGeoFallbackLink guarantees a minimum geo anchoring edge for country-scoped organizations
// and government bodies (/gov/{geo}/{slug}, modeled as ORGANIZATION).
// Rule:
// - if fragment is /org/{geo}/{slug} or /gov/{geo}/{slug}, and geo != "na"
// - and organization has no outgoing LOCATED_IN/OPERATES_IN geo edges yet
// then create LOCATED_IN -> /loc/{geo} (best effort).
// Special-case:
// - /org/int/... maps to /loc/global as the international bucket.
func (e *ScoutEngine) ensureOrgGeoFallbackLink(ctx context.Context, frag *models.HorizonFragment, provenanceMethod, sourceID string) {
	if e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil || frag == nil {
		return
	}
	if frag.Type != models.FragmentTypeOrganization {
		return
	}
	orgID := strings.TrimSpace(frag.ID)
	if orgID == "" {
		if loaded, err := e.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(frag.CanonicalPath)); err == nil && loaded != nil {
			orgID = strings.TrimSpace(loaded.ID)
			frag = loaded
		}
	}
	if orgID == "" {
		return
	}

	parts := strings.Split(strings.Trim(strings.TrimSpace(frag.CanonicalPath), "/"), "/")
	if len(parts) < 3 {
		return
	}
	ns := strings.ToLower(strings.TrimSpace(parts[0]))
	if ns != "org" && ns != "gov" {
		return
	}
	if ns == "org" && len(parts) < 3 {
		return // legacy /org/{slug} has no explicit geo token
	}
	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	if geo == "" || geo == "na" {
		return
	}

	// If any geo edge already exists, do not add fallback.
	if e.orgHasAnyGeoLocationEdge(ctx, orgID) {
		return
	}

	geoToken := geo
	if geoToken == "int" {
		geoToken = "global"
	}
	if !taxonomy.IsRegionalGeoToken(geoToken) && !taxonomy.IsCompositeGeoToken(geoToken) && len(geoToken) != 2 {
		return
	}
	targetPath := fmt.Sprintf("/loc/%s", geoToken)
	if err := taxonomy.ValidateCanonicalPath(targetPath); err != nil {
		return
	}

	geoFrag, err := e.getOrCreateFragmentByPath(ctx, targetPath, models.FragmentTypeGeoRegion, bestEffortGeoLabel(geoToken), "", "org_geo_fallback", sourceID)
	if err != nil || geoFrag == nil || strings.TrimSpace(geoFrag.ID) == "" {
		return
	}
	geoID := strings.TrimSpace(geoFrag.ID)

	if existing, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", orgID, geoID, models.DependencyTypeLocatedIn); err == nil && existing != nil {
		return
	}
	now := time.Now().UTC()
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: orgID,
		TargetFragmentID: geoID,
		Type:             models.DependencyTypeLocatedIn,
		Properties: models.EdgeProperties{
			Criticality:   0.45,
			Amplification: 1.0,
		},
		Qualifiers: map[string]interface{}{
			"source": "org_path_geo_fallback",
		},
		Provenance: models.Provenance{
			Method:    firstNonEmpty(strings.TrimSpace(provenanceMethod), "org_geo_fallback"),
			SourceIDs: []string{strings.TrimSpace(sourceID)},
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
	}
	_, _ = e.edgesRepo.Create(ctx, edge)
}

func (e *ScoutEngine) ensureFacilityGeoFallbackLink(ctx context.Context, frag *models.HorizonFragment, provenanceMethod, sourceID string) {
	if e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil || frag == nil {
		return
	}
	if frag.Type != models.FragmentTypeFacility {
		return
	}
	facilityID := strings.TrimSpace(frag.ID)
	if facilityID == "" {
		if loaded, err := e.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(frag.CanonicalPath)); err == nil && loaded != nil {
			facilityID = strings.TrimSpace(loaded.ID)
			frag = loaded
		}
	}
	if facilityID == "" {
		return
	}
	if e.fragmentHasAnyGeoLocationEdge(ctx, facilityID) {
		return
	}

	geoToken := relationTargetCountryGeoToken(frag)
	if geoToken == "" || geoToken == "na" {
		return
	}
	targetPath := fmt.Sprintf("/loc/%s", geoToken)
	if err := taxonomy.ValidateCanonicalPath(targetPath); err != nil {
		return
	}

	geoFrag, err := e.getOrCreateFragmentByPath(ctx, targetPath, models.FragmentTypeGeoRegion, bestEffortGeoLabel(geoToken), "", "facility_geo_fallback", sourceID)
	if err != nil || geoFrag == nil || strings.TrimSpace(geoFrag.ID) == "" {
		return
	}
	geoID := strings.TrimSpace(geoFrag.ID)

	if existing, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", facilityID, geoID, models.DependencyTypeLocatedIn); err == nil && existing != nil {
		return
	}
	now := time.Now().UTC()
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: facilityID,
		TargetFragmentID: geoID,
		Type:             models.DependencyTypeLocatedIn,
		Properties: models.EdgeProperties{
			Criticality:   0.45,
			Amplification: 1.0,
		},
		Qualifiers: map[string]interface{}{
			"source": "facility_path_geo_fallback",
		},
		Provenance: models.Provenance{
			Method:    firstNonEmpty(strings.TrimSpace(provenanceMethod), "facility_geo_fallback"),
			SourceIDs: []string{strings.TrimSpace(sourceID)},
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
	}
	_, _ = e.edgesRepo.Create(ctx, edge)
}

func (e *ScoutEngine) orgHasAnyGeoLocationEdge(ctx context.Context, orgID string) bool {
	return e.fragmentHasAnyGeoLocationEdge(ctx, orgID)
}

func (e *ScoutEngine) fragmentHasAnyGeoLocationEdge(ctx context.Context, fragmentID string) bool {
	if e == nil || e.edgesRepo == nil || e.fragmentsRepo == nil || strings.TrimSpace(fragmentID) == "" {
		return false
	}
	types := []models.DependencyType{
		models.DependencyTypeLocatedIn,
		models.DependencyTypeOperatesIn,
	}
	for _, t := range types {
		tt := t
		edges, err := e.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(fragmentID), &tt, 5)
		if err != nil || len(edges) == 0 {
			continue
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
				continue
			}
			target, gerr := e.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
			if gerr != nil || target == nil {
				continue
			}
			if target.Type == models.FragmentTypeGeoRegion || strings.HasPrefix(strings.TrimSpace(target.CanonicalPath), "/loc/") {
				return true
			}
		}
	}
	return false
}

func relationTargetCountryGeoToken(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if iso2 := normalizeISO2(*fragment.HomeLocation.ISO3166_1); iso2 != "" {
			return iso2
		}
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "fac", "community", "program", "market":
		token := strings.TrimSpace(parts[1])
		if token == "int" {
			return "global"
		}
		if normalizeISO2(token) != "" || taxonomy.IsRegionalGeoToken(token) || taxonomy.IsCompositeGeoToken(token) {
			return token
		}
	case "loc":
		cc, _, _, ok := taxonomy.ParseLocCanonicalPath(cp)
		if !ok {
			return ""
		}
		if iso2 := normalizeISO2(cc); iso2 != "" {
			return iso2
		}
	}
	return ""
}

func bestEffortAttachCountryISOFromPath(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	iso2 := relationTargetCountryGeoToken(fragment)
	if normalizeISO2(iso2) == "" {
		return false
	}
	upper := strings.ToUpper(iso2)
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &models.Location{ISO3166_1: &upper}
		return true
	}
	if fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) == "" {
		fragment.HomeLocation.ISO3166_1 = &upper
		return true
	}
	return false
}

func applyRelationTargetGeocodingResult(fragment *models.HorizonFragment, result *coreontology.GeocodingResult) bool {
	if fragment == nil || result == nil {
		return false
	}
	if !models.IsValidCoordinates(result.Lat, result.Lon) || !models.IsRealGeoCoordinate(result.Lat, result.Lon) {
		return false
	}
	changed := false
	if !models.HasRealGeoCoordinates(fragment.Coordinates) ||
		fragment.Coordinates.Lat != result.Lat || fragment.Coordinates.Lon != result.Lon {
		fragment.Coordinates = &models.Coordinates{Lat: result.Lat, Lon: result.Lon}
		changed = true
	}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &models.Location{}
		changed = true
	}
	iso6709 := fmt.Sprintf("%+.6f%+.6f/", result.Lat, result.Lon)
	if fragment.HomeLocation.ISO6709 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO6709) == "" {
		fragment.HomeLocation.ISO6709 = &iso6709
		changed = true
	}
	if fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) == "" {
		if cc := strings.TrimSpace(inferCountryISO2FromFormattedAddress(result.FormattedAddress)); cc != "" {
			fragment.HomeLocation.ISO3166_1 = &cc
			changed = true
		}
	}
	return changed
}

func (e *ScoutEngine) bestEffortGeocodeRelationTargetCandidates(ctx context.Context, fragment, parent *models.HorizonFragment) bool {
	if e == nil || e.geocodingClient == nil || fragment == nil || models.HasRealGeoCoordinates(fragment.Coordinates) {
		return false
	}
	candidates := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(values ...string) {
		for _, raw := range values {
			addr := strings.TrimSpace(raw)
			if addr == "" {
				continue
			}
			key := strings.ToLower(addr)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, addr)
		}
	}
	add(henrich.BuildAddressFromFragment(fragment))
	if fragment.Type == models.FragmentTypeFacility {
		add(henrich.BuildFacilityAddressCandidates(fragment, parent)...)
	}
	add(henrich.BuildLastSegmentAddressCandidates(fragment)...)

	for _, candidate := range candidates {
		result, err := e.geocodingClient.Geocode(ctx, candidate)
		if err != nil || result == nil {
			continue
		}
		if applyRelationTargetGeocodingResult(fragment, result) {
			return true
		}
	}
	return false
}

func (e *ScoutEngine) bestEffortEnrichRelationTargetGeo(ctx context.Context, fragment, parent *models.HorizonFragment, provenanceMethod, sourceID string) {
	if e == nil || fragment == nil {
		return
	}
	updated := false
	if bestEffortAttachCountryISOFromPath(fragment) {
		updated = true
	}
	if fragment.Type == models.FragmentTypeFacility && parent != nil && henrich.EnrichCoordinatesFromParent(fragment, parent) {
		updated = true
	}
	if !models.HasRealGeoCoordinates(fragment.Coordinates) && e.geonamesClient != nil {
		if changed, err := henrich.EnrichCoordinatesFromGeoNames(ctx, e.geonamesClient, fragment); err == nil && changed {
			updated = true
		}
	}
	if !models.HasRealGeoCoordinates(fragment.Coordinates) {
		if fragment.Type == models.FragmentTypeFacility {
			if result, err := RevalidateFacilityGeo(ctx, fragment, e.geocodingClient); err == nil && result.Applied {
				updated = true
			}
		} else if fragment.Type == models.FragmentTypeOrganization {
			if result, err := RevalidateOrgGeo(ctx, fragment, e.geocodingClient); err == nil && result.Applied {
				updated = true
			}
		} else if e.bestEffortGeocodeRelationTargetCandidates(ctx, fragment, parent) {
			updated = true
		}
	}
	if updated && e.fragmentsRepo != nil && strings.TrimSpace(fragment.ID) != "" {
		if err := e.fragmentsRepo.Update(ctx, fragment); err != nil {
			log.Printf("[ScoutEngine] WARN relation target geo update failed path=%s id=%s err=%v",
				strings.TrimSpace(fragment.CanonicalPath),
				strings.TrimSpace(fragment.ID),
				err,
			)
		}
	}
	e.ensureOrgGeoFallbackLink(ctx, fragment, provenanceMethod, sourceID)
	e.ensureFacilityGeoFallbackLink(ctx, fragment, provenanceMethod, sourceID)
}

func bestEffortGeoLabel(token string) string {
	t := strings.TrimSpace(strings.ToLower(token))
	if t == "" {
		return "Location"
	}
	if t == "global" {
		return "Global"
	}
	if len(t) == 2 {
		return strings.ToUpper(t)
	}
	s := strings.ReplaceAll(t, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Location"
	}
	return strings.Title(strings.TrimSpace(s))
}

func normalizeEdgeConfidence(in float64) float64 {
	// LLMs sometimes violate the contract and return 0..100 or other ranges.
	// We normalize best-effort to keep graph materialization robust.
	c := in
	if c > 1.0 && c <= 100.0 {
		c = c / 100.0
	}
	if c < 0 {
		return 0
	}
	if c > 1.0 {
		return 1.0
	}
	return c
}

// processDiscoveredRelations обрабатывает найденные связи и запускает декомпозицию
func (e *ScoutEngine) processDiscoveredRelations(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
) error {
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "scout_discovery", map[string]interface{}{"source": "scout"}, nil)
}

// Keep for optional grounded-relation flows even when current call graph doesn't invoke it directly.
var _ = (*ScoutEngine).processDiscoveredRelationsWithGrounding

// processDiscoveredRelationsWithGrounding is like processDiscoveredRelations but also accepts
// a ResolvedGroundingContext from Vertex AI Search. When a relation target matches a grounded
// fragment, the system uses the DB entity directly (canonical path, identifiers) instead of
// creating a new fragment. This eliminates duplicate creation at the source.
func (e *ScoutEngine) processDiscoveredRelationsWithGrounding(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
	groundingCtx *ResolvedGroundingContext,
) error {
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "scout_discovery", map[string]interface{}{"source": "scout"}, groundingCtx)
}

// processHypothesisRelations materializes relations produced from intent decomposition.
// These are hypotheses (not grounded claims), so we mark provenance/qualifiers accordingly.
func (e *ScoutEngine) processHypothesisRelations(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
) error {
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "intent_decomposition", map[string]interface{}{"source": "intent_decomposition", "hypothesis": true}, nil)
}

type domainContractForExpansion = pipeline.DomainContract

func normalizeDomainPathForMatch(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return ""
	}
	if strings.HasPrefix(cp, "/domain/") {
		return NormalizeRuntimeDomainCanonicalPath(cp)
	}
	return cp
}

func domainFacetSignature(contractType string, facets map[string][]string) string {
	ct := strings.ToLower(strings.TrimSpace(contractType))
	if ct == "industry-analysis" {
		parts := []string{"ct=" + ct}
		if geos, ok := facets["geo"]; ok && len(geos) > 0 {
			geoSeen := map[string]struct{}{}
			geoVals := make([]string, 0, len(geos))
			for _, geo := range geos {
				geo = strings.ToLower(strings.TrimSpace(geo))
				if geo == "" {
					continue
				}
				if _, ok := geoSeen[geo]; ok {
					continue
				}
				geoSeen[geo] = struct{}{}
				geoVals = append(geoVals, geo)
			}
			sort.Strings(geoVals)
			for _, geo := range geoVals {
				parts = append(parts, "geo="+geo)
			}
		}
		if chain, ok := facets["industry"]; ok && len(chain) > 0 {
			for _, item := range chain {
				item = strings.ToLower(strings.TrimSpace(item))
				if item == "" {
					continue
				}
				parts = append(parts, "industry="+item)
			}
		}
		keys := make([]string, 0, len(facets))
		for k := range facets {
			kk := strings.ToLower(strings.TrimSpace(k))
			if kk == "" || kk == "geo" || kk == "industry" {
				continue
			}
			keys = append(keys, kk)
		}
		sort.Strings(keys)
		for _, k := range keys {
			seen := map[string]struct{}{}
			vals := make([]string, 0, len(facets[k]))
			for _, v := range facets[k] {
				vv := strings.ToLower(strings.TrimSpace(v))
				if vv == "" {
					continue
				}
				if _, ok := seen[vv]; ok {
					continue
				}
				seen[vv] = struct{}{}
				vals = append(vals, vv)
			}
			sort.Strings(vals)
			parts = append(parts, k+"="+strings.Join(vals, ","))
		}
		return strings.Join(parts, "|")
	}
	keys := make([]string, 0, len(facets))
	for k := range facets {
		kk := strings.ToLower(strings.TrimSpace(k))
		if kk != "" {
			keys = append(keys, kk)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	parts = append(parts, "ct="+ct)
	for _, k := range keys {
		seen := map[string]struct{}{}
		vals := make([]string, 0, len(facets[k]))
		for _, v := range facets[k] {
			vv := strings.ToLower(strings.TrimSpace(v))
			if vv == "" {
				continue
			}
			if _, ok := seen[vv]; ok {
				continue
			}
			seen[vv] = struct{}{}
			vals = append(vals, vv)
		}
		sort.Strings(vals)
		parts = append(parts, k+"="+strings.Join(vals, ","))
	}
	return strings.Join(parts, "|")
}

func domainContractQualityScore(c *domainContractForExpansion) int {
	if c == nil {
		return 0
	}
	score := 0
	if strings.TrimSpace(c.Title) != "" {
		score += 10
	}
	if strings.TrimSpace(c.Description) != "" {
		score += 6
	}
	score += len(c.Facets)
	score += len(c.FirstOrderEntities)
	score += len(c.RelatedDomains)
	score += len(c.AffectedDomains)
	return score
}

func bestEffortDomainTitleFromJobState(job *models.ScoutingJob) string {
	if job == nil || job.State == nil {
		return ""
	}
	for _, key := range []string{"focus", "originalQuery", "query"} {
		raw := parseStateString(job.State[key])
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		words := strings.Fields(raw)
		if len(words) > 10 {
			words = words[:10]
		}
		raw = strings.TrimSpace(strings.Join(words, " "))
		if len(raw) < 4 {
			continue
		}
		return bestEffortTitleCase(raw)
	}
	return ""
}

// getSupplyChainExecutionPlanFromJobState extracts SupplyChainExecutionPlan for the matching domain.
// Used for Plan mode when contractType is supply-chain group.
func getSupplyChainExecutionPlanFromJobState(job *models.ScoutingJob, domainCanonicalPath string) *agents.SupplyChainExecutionPlan {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State["domains"]
	if !ok || raw == nil {
		return nil
	}
	cpNorm := normalizeDomainPathForMatch(strings.TrimSpace(domainCanonicalPath))
	matchPaths := map[string]struct{}{cpNorm: {}}
	cpCT, cpFacets := parseDomainPathFacets(cpNorm)
	cpSig := domainFacetSignature(cpCT, cpFacets)
	var bySignature *agents.SupplyChainExecutionPlan
	if st := parseStateString(job.State["targetCanonicalPath"]); st != "" {
		matchPaths[normalizeDomainPathForMatch(st)] = struct{}{}
	}
	if st := parseStateString(job.State["aliasCanonicalPath"]); st != "" {
		matchPaths[normalizeDomainPathForMatch(st)] = struct{}{}
	}
	getPlanFromMap := func(m map[string]interface{}) *agents.SupplyChainExecutionPlan {
		if m == nil {
			return nil
		}
		if p := decodeSupplyChainExecutionPlan(m["supplyChainExecutionPlan"]); p != nil {
			return p
		}
		return decodeSupplyChainExecutionPlan(m["SupplyChainExecutionPlan"])
	}
	getCanonicalPathFromMap := func(m map[string]interface{}) string {
		if m == nil {
			return ""
		}
		cp := parseStateString(m["canonicalPath"])
		if cp == "" {
			cp = parseStateString(m["CanonicalPath"])
		}
		return strings.TrimSpace(cp)
	}
	tryMatch := func(cp string, plan *agents.SupplyChainExecutionPlan) *agents.SupplyChainExecutionPlan {
		if plan == nil || len(plan.Queries) == 0 {
			return nil
		}
		cn := normalizeDomainPathForMatch(strings.TrimSpace(cp))
		if _, ok := matchPaths[cn]; ok {
			return plan
		}
		ct, facets := parseDomainPathFacets(cn)
		if cpSig != "" && domainFacetSignature(ct, facets) == cpSig {
			if bySignature == nil || len(plan.Queries) > len(bySignature.Queries) {
				bySignature = plan
			}
		}
		return nil
	}
	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			if found := tryMatch(d.CanonicalPath, d.SupplyChainExecutionPlan); found != nil {
				return found
			}
		}
	case []map[string]interface{}:
		for _, m := range t {
			if found := tryMatch(getCanonicalPathFromMap(m), getPlanFromMap(m)); found != nil {
				return found
			}
		}
	case []interface{}:
		for _, it := range t {
			if d, ok := it.(agents.DomainResolution); ok {
				if found := tryMatch(d.CanonicalPath, d.SupplyChainExecutionPlan); found != nil {
					return found
				}
				continue
			}
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			if found := tryMatch(getCanonicalPathFromMap(m), getPlanFromMap(m)); found != nil {
				return found
			}
		}
	}
	return bySignature
}

// expandSupplyChainQueriesWithOrgs expands product/market-anchored queries to org/fac-anchored.
// Discovers BOTH org and fac via IntentDecomposer recursively (max 5 hops). Organization anchors
// remain /org|/gov paths (no synthetic /fac/*-general-production fallback).
func (e *ScoutEngine) expandSupplyChainQueriesWithOrgs(ctx context.Context, job *models.ScoutingJob, queries []agents.SupplyChainQuery) []agents.SupplyChainQuery {
	if e == nil || e.aiExecutor == nil || job == nil || len(queries) == 0 {
		return queries
	}
	needsExpansion := func(q *agents.SupplyChainQuery) bool {
		if len(q.Anchors) > 0 {
			return false // already has anchors
		}
		anchor := strings.TrimSpace(q.Anchor)
		if anchor == "" {
			return false
		}
		return strings.EqualFold(strings.TrimSpace(q.AnchorType), "product") ||
			strings.HasPrefix(anchor, "/market/") ||
			strings.HasPrefix(anchor, "/product/")
	}
	maxHops := SupplyChainMaxTraversalDepth
	if maxHops < 1 || maxHops > 5 {
		maxHops = 5
	}
	if job.State != nil {
		if n := parseStateInt(job.State["supplyChainFastFirstMaxAnchorHops"], 0); n > 0 && n <= 5 {
			maxHops = n
		}
	}
	maxAnchors := 12
	if job.State != nil {
		if n := parseStateInt(job.State["maxSupplyChainAnchors"], 0); n > 0 && n <= 24 {
			maxAnchors = n
		}
		if n := parseStateInt(job.State["supplyChainFastFirstMaxAnchors"], 0); n > 0 && n <= 24 {
			maxAnchors = n
		}
	}
	out := make([]agents.SupplyChainQuery, 0, len(queries))
	for i := range queries {
		q := &queries[i]
		if !needsExpansion(q) {
			e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainExpansion] query[%d] skip: already has %d anchors", i, len(q.Anchors)))
			out = append(out, *q)
			continue
		}
		focus := ""
		if v, ok := job.State["focus"].(string); ok {
			focus = strings.TrimSpace(v)
		}
		var keywords, spherePaths []string
		if v := job.State["keywords"]; v != nil {
			keywords = parseStateStringSlice(v)
		}
		if v := job.State["spherePaths"]; v != nil {
			spherePaths = parseStateStringSlice(v)
		}
		originalQuery := ""
		if v, ok := job.State["originalQuery"].(string); ok {
			originalQuery = strings.TrimSpace(v)
		}
		var decompositionHints []agents.DecompositionHint
		if v := job.State["decompositionHints"]; v != nil {
			if b, err := json.Marshal(v); err == nil {
				_ = json.Unmarshal(b, &decompositionHints)
			}
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainExpansion] query[%d] expanding anchor=%q (product/market→org+fac discovery)", i, q.Anchor))
		anchors := e.discoverOrgAndFacAnchorsRecursive(ctx, job, q.Anchor, focus, spherePaths, keywords, originalQuery, decompositionHints, maxHops, maxAnchors)
		if len(anchors) == 0 {
			e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainExpansion] query[%d] discovery failed for anchor=%q, keeping original", i, q.Anchor))
			out = append(out, *q)
			continue
		}
		expanded := *q
		expanded.Anchors = anchors
		expanded.AnchorType = "org"
		expanded.Anchor = ""
		out = append(out, expanded)
		e.logLine(ctx, job, "info", fmt.Sprintf("> supply chain query expanded product/market→org+fac: %d anchors (e.g. %s)", len(anchors), anchors[0]))
	}
	return out
}

// discoverOrgAndFacAnchorsRecursive discovers org/fac/deposit anchors via IntentDecomposer.
// Recursion: round 0 = decompose product/market; rounds 1..maxHops-1 = decompose each org to find facilities.
// Returns normalized anchors without synthetic fallback nodes.
func (e *ScoutEngine) discoverOrgAndFacAnchorsRecursive(
	ctx context.Context,
	job *models.ScoutingJob,
	initialAnchor string,
	focus string,
	spherePaths, keywords []string,
	originalQuery string,
	decompositionHints []agents.DecompositionHint,
	maxHops, maxAnchors int,
) []string {
	agent := agents.NewIntentDecomposerAgent(e.aiExecutor)
	opts := agents.DecomposeOptions{
		IsAbstractAnchor:   strings.HasPrefix(initialAnchor, "/market/") || strings.HasPrefix(initialAnchor, "/program/"),
		DecompositionHints: decompositionHints,
		OriginalQuery:      originalQuery,
		DomainModes:        []string{taxonomy.DomainModeSupplyChain}, // always supply-chain context
	}

	seen := make(map[string]struct{})
	addAnchor := func(cp string) bool {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			return false
		}
		key := strings.ToLower(cp)
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		return true
	}

	// Normalize path to effective anchor without org->fac fallback conversion.
	toEffectiveAnchor := func(cp string) string {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			return ""
		}
		if strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/deposit/") || strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") {
			return cp
		}
		return ""
	}

	// Round 0: decompose initial (product/market)
	rels, err := agent.DecomposeWithOptions(ctx, initialAnchor, focus, spherePaths, keywords, opts)
	if err != nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainDiscovery] IntentDecomposer round=0 anchor=%q failed: %v", initialAnchor, err))
		return nil
	}
	if len(rels) == 0 {
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainDiscovery] IntentDecomposer round=0 anchor=%q → 0 relations, no expansion", initialAnchor))
		return nil
	}
	sample := ""
	if len(rels) > 0 {
		r0 := rels[0]
		sample = fmt.Sprintf("%s (%s→%s conf=%.2f)", r0.TargetCanonicalPath, r0.RelationType, r0.TargetType, r0.Confidence)
	}
	if len(rels) > 1 {
		sample += fmt.Sprintf(" +%d more", len(rels)-1)
	}
	e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainDiscovery] IntentDecomposer round=0 anchor=%q → relations=%d sample=%s", initialAnchor, len(rels), sample))
	type queueItem struct {
		path  string
		round int
	}
	queue := make([]queueItem, 0, 32)
	decomposed := make(map[string]struct{}) // avoid re-decomposing same org
	anchors := make([]string, 0, maxAnchors)
	for _, r := range rels {
		cp := strings.TrimSpace(r.TargetCanonicalPath)
		if cp == "" {
			continue
		}
		if strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/deposit/") {
			eff := toEffectiveAnchor(cp)
			if eff != "" && addAnchor(eff) {
				anchors = append(anchors, eff)
				if len(anchors) >= maxAnchors {
					return anchors
				}
			}
			continue
		}
		if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") {
			eff := toEffectiveAnchor(cp)
			if eff != "" && addAnchor(eff) {
				anchors = append(anchors, eff)
				if len(anchors) >= maxAnchors {
					return anchors
				}
			}
			if maxHops > 1 {
				key := strings.ToLower(cp)
				if _, done := decomposed[key]; !done {
					decomposed[key] = struct{}{}
					queue = append(queue, queueItem{path: cp, round: 1})
				}
			}
		}
	}
	fromOrg, fromFac := 0, 0
	for _, eff := range anchors {
		if strings.HasPrefix(eff, "/org/") || strings.HasPrefix(eff, "/gov/") {
			fromOrg++
		} else {
			fromFac++
		}
	}
	e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainDiscovery] round=0 decided anchors=%d (from_org=%d from_fac=%d): %v", len(anchors), fromOrg, fromFac, anchors))

	// Rounds 1..maxHops-1: decompose orgs to discover facilities
	for len(queue) > 0 && len(anchors) < maxAnchors {
		cur := queue[0]
		queue = queue[1:]
		if cur.round >= maxHops {
			continue
		}
		rels2, err2 := agent.DecomposeWithOptions(ctx, cur.path, focus, spherePaths, keywords, opts)
		if err2 != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainDiscovery] IntentDecomposer round=%d org=%q failed: %v", cur.round, cur.path, err2))
			continue
		}
		if len(rels2) == 0 {
			continue
		}
		prevN := len(anchors)
		for _, r := range rels2 {
			cp := strings.TrimSpace(r.TargetCanonicalPath)
			if cp == "" {
				continue
			}
			if strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/deposit/") {
				eff := toEffectiveAnchor(cp)
				if eff != "" && addAnchor(eff) {
					anchors = append(anchors, eff)
					if len(anchors) >= maxAnchors {
						return anchors
					}
				}
				continue
			}
			if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") {
				eff := toEffectiveAnchor(cp)
				if eff != "" && addAnchor(eff) {
					anchors = append(anchors, eff)
					if len(anchors) >= maxAnchors {
						return anchors
					}
				}
				if cur.round+1 < maxHops {
					key := strings.ToLower(cp)
					if _, done := decomposed[key]; !done {
						decomposed[key] = struct{}{}
						queue = append(queue, queueItem{path: cp, round: cur.round + 1})
					}
				}
			}
		}
		newN := len(anchors) - prevN
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainDiscovery] IntentDecomposer round=%d org=%q → relations=%d new_anchors=%d total=%d", cur.round, cur.path, len(rels2), newN, len(anchors)))
	}
	return anchors
}

func decodeSupplyChainExecutionPlan(raw interface{}) *agents.SupplyChainExecutionPlan {
	if raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case *agents.SupplyChainExecutionPlan:
		if t != nil && len(t.Queries) > 0 {
			t.GraphBuildMode = normalizeSupplyChainGraphBuildModeState(t.GraphBuildMode)
			return t
		}
		return nil
	case agents.SupplyChainExecutionPlan:
		if len(t.Queries) == 0 {
			return nil
		}
		cp := t
		cp.GraphBuildMode = normalizeSupplyChainGraphBuildModeState(cp.GraphBuildMode)
		return &cp
	}
	b, err := json.Marshal(raw)
	if err != nil || len(b) == 0 {
		return nil
	}
	var plan agents.SupplyChainExecutionPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return nil
	}
	if len(plan.Queries) == 0 {
		return nil
	}
	plan.GraphBuildMode = normalizeSupplyChainGraphBuildModeState(plan.GraphBuildMode)
	return &plan
}

func normalizeSupplyChainGraphBuildModeState(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case agents.SupplyChainGraphBuildModeHotPathOnly:
		return agents.SupplyChainGraphBuildModeHotPathOnly
	case "", agents.SupplyChainGraphBuildModeHotPathPlusGraph:
		return agents.SupplyChainGraphBuildModeHotPathPlusGraph
	default:
		return agents.SupplyChainGraphBuildModeHotPathPlusGraph
	}
}

func supplyChainGraphBuildMode(plan *agents.SupplyChainExecutionPlan, job *models.ScoutingJob) string {
	if plan != nil {
		if mode := normalizeSupplyChainGraphBuildModeState(plan.GraphBuildMode); mode != "" {
			return mode
		}
	}
	if job != nil && job.State != nil {
		if mode := normalizeSupplyChainGraphBuildModeState(parseStateString(job.State["supplyChainGraphBuildMode"])); mode != "" {
			return mode
		}
	}
	return agents.SupplyChainGraphBuildModeHotPathPlusGraph
}

// getRegulatoryImpactPlanFromJobState extracts RegulatoryImpactExecutionPlan for the matching domain.
func getRegulatoryImpactPlanFromJobState(job *models.ScoutingJob, domainCanonicalPath string) *agents.RegulatoryImpactExecutionPlan {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State["domains"]
	if !ok || raw == nil {
		return nil
	}
	cpNorm := normalizeDomainPathForMatch(strings.TrimSpace(domainCanonicalPath))
	decodePlan := func(v interface{}) *agents.RegulatoryImpactExecutionPlan {
		if v == nil {
			return nil
		}
		if p, ok := v.(*agents.RegulatoryImpactExecutionPlan); ok && p != nil && len(p.Queries) > 0 {
			return p
		}
		if p, ok := v.(agents.RegulatoryImpactExecutionPlan); ok && len(p.Queries) > 0 {
			return &p
		}
		b, err := json.Marshal(v)
		if err != nil || len(b) == 0 {
			return nil
		}
		var plan agents.RegulatoryImpactExecutionPlan
		if err := json.Unmarshal(b, &plan); err != nil || len(plan.Queries) == 0 {
			return nil
		}
		return &plan
	}
	tryFromMap := func(m map[string]interface{}) *agents.RegulatoryImpactExecutionPlan {
		if p := decodePlan(m["regulatoryImpactExecutionPlan"]); p != nil {
			return p
		}
		return decodePlan(m["RegulatoryImpactExecutionPlan"])
	}
	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			if normalizeDomainPathForMatch(strings.TrimSpace(d.CanonicalPath)) == cpNorm {
				return d.RegulatoryImpactExecutionPlan
			}
		}
	case []map[string]interface{}:
		for _, m := range t {
			cp := parseStateString(m["canonicalPath"])
			if cp == "" {
				cp = parseStateString(m["CanonicalPath"])
			}
			if normalizeDomainPathForMatch(strings.TrimSpace(cp)) == cpNorm {
				return tryFromMap(m)
			}
		}
	case []interface{}:
		for _, it := range t {
			if d, ok := it.(agents.DomainResolution); ok {
				if normalizeDomainPathForMatch(strings.TrimSpace(d.CanonicalPath)) == cpNorm {
					return d.RegulatoryImpactExecutionPlan
				}
				continue
			}
			if m, ok := it.(map[string]interface{}); ok {
				cp := parseStateString(m["canonicalPath"])
				if cp == "" {
					cp = parseStateString(m["CanonicalPath"])
				}
				if normalizeDomainPathForMatch(strings.TrimSpace(cp)) == cpNorm {
					return tryFromMap(m)
				}
			}
		}
	}
	return nil
}

// getGovernancePlanFromJobState extracts GovernanceExecutionPlan for the matching domain.
func getGovernancePlanFromJobState(job *models.ScoutingJob, domainCanonicalPath string) *agents.GovernanceExecutionPlan {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State["domains"]
	if !ok || raw == nil {
		return nil
	}
	cpNorm := normalizeDomainPathForMatch(strings.TrimSpace(domainCanonicalPath))
	decodePlan := func(v interface{}) *agents.GovernanceExecutionPlan {
		if v == nil {
			return nil
		}
		if p, ok := v.(*agents.GovernanceExecutionPlan); ok && p != nil && len(p.Queries) > 0 {
			return p
		}
		if p, ok := v.(agents.GovernanceExecutionPlan); ok && len(p.Queries) > 0 {
			return &p
		}
		b, err := json.Marshal(v)
		if err != nil || len(b) == 0 {
			return nil
		}
		var plan agents.GovernanceExecutionPlan
		if err := json.Unmarshal(b, &plan); err != nil || len(plan.Queries) == 0 {
			return nil
		}
		return &plan
	}
	tryFromMap := func(m map[string]interface{}) *agents.GovernanceExecutionPlan {
		if p := decodePlan(m["governanceExecutionPlan"]); p != nil {
			return p
		}
		return decodePlan(m["GovernanceExecutionPlan"])
	}
	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			if normalizeDomainPathForMatch(strings.TrimSpace(d.CanonicalPath)) == cpNorm {
				return d.GovernanceExecutionPlan
			}
		}
	case []map[string]interface{}:
		for _, m := range t {
			cp := parseStateString(m["canonicalPath"])
			if cp == "" {
				cp = parseStateString(m["CanonicalPath"])
			}
			if normalizeDomainPathForMatch(strings.TrimSpace(cp)) == cpNorm {
				return tryFromMap(m)
			}
		}
	case []interface{}:
		for _, it := range t {
			if d, ok := it.(agents.DomainResolution); ok {
				if normalizeDomainPathForMatch(strings.TrimSpace(d.CanonicalPath)) == cpNorm {
					return d.GovernanceExecutionPlan
				}
				continue
			}
			if m, ok := it.(map[string]interface{}); ok {
				cp := parseStateString(m["canonicalPath"])
				if cp == "" {
					cp = parseStateString(m["CanonicalPath"])
				}
				if normalizeDomainPathForMatch(strings.TrimSpace(cp)) == cpNorm {
					return tryFromMap(m)
				}
			}
		}
	}
	return nil
}

// getEcosystemPlanFromJobState extracts EcosystemExecutionPlan for the matching domain.
func getEcosystemPlanFromJobState(job *models.ScoutingJob, domainCanonicalPath string) *agents.EcosystemExecutionPlan {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State["domains"]
	if !ok || raw == nil {
		return nil
	}
	cpNorm := normalizeDomainPathForMatch(strings.TrimSpace(domainCanonicalPath))
	decodePlan := func(v interface{}) *agents.EcosystemExecutionPlan {
		if v == nil {
			return nil
		}
		if p, ok := v.(*agents.EcosystemExecutionPlan); ok && p != nil && len(p.Queries) > 0 {
			return p
		}
		if p, ok := v.(agents.EcosystemExecutionPlan); ok && len(p.Queries) > 0 {
			return &p
		}
		b, err := json.Marshal(v)
		if err != nil || len(b) == 0 {
			return nil
		}
		var plan agents.EcosystemExecutionPlan
		if err := json.Unmarshal(b, &plan); err != nil || len(plan.Queries) == 0 {
			return nil
		}
		return &plan
	}
	tryFromMap := func(m map[string]interface{}) *agents.EcosystemExecutionPlan {
		if p := decodePlan(m["ecosystemExecutionPlan"]); p != nil {
			return p
		}
		return decodePlan(m["EcosystemExecutionPlan"])
	}
	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			if normalizeDomainPathForMatch(strings.TrimSpace(d.CanonicalPath)) == cpNorm {
				return d.EcosystemExecutionPlan
			}
		}
	case []map[string]interface{}:
		for _, m := range t {
			cp := parseStateString(m["canonicalPath"])
			if cp == "" {
				cp = parseStateString(m["CanonicalPath"])
			}
			if normalizeDomainPathForMatch(strings.TrimSpace(cp)) == cpNorm {
				return tryFromMap(m)
			}
		}
	case []interface{}:
		for _, it := range t {
			if d, ok := it.(agents.DomainResolution); ok {
				if normalizeDomainPathForMatch(strings.TrimSpace(d.CanonicalPath)) == cpNorm {
					return d.EcosystemExecutionPlan
				}
				continue
			}
			if m, ok := it.(map[string]interface{}); ok {
				cp := parseStateString(m["canonicalPath"])
				if cp == "" {
					cp = parseStateString(m["CanonicalPath"])
				}
				if normalizeDomainPathForMatch(strings.TrimSpace(cp)) == cpNorm {
					return tryFromMap(m)
				}
			}
		}
	}
	return nil
}

func (e *ScoutEngine) bestEffortFindDomainContract(job *models.ScoutingJob, domainCanonicalPath string) *domainContractForExpansion {
	cp := strings.TrimSpace(domainCanonicalPath)
	if job == nil || job.State == nil || cp == "" {
		return nil
	}
	cpNorm := normalizeDomainPathForMatch(cp)
	cpCT, cpFacets := parseDomainPathFacets(cpNorm)
	cpSig := domainFacetSignature(cpCT, cpFacets)
	matchPaths := map[string]struct{}{cpNorm: {}}
	if st := parseStateString(job.State["targetCanonicalPath"]); st != "" {
		matchPaths[normalizeDomainPathForMatch(st)] = struct{}{}
	}
	if st := parseStateString(job.State["aliasCanonicalPath"]); st != "" {
		matchPaths[normalizeDomainPathForMatch(st)] = struct{}{}
	}

	raw, ok := job.State["domains"]
	if !ok || raw == nil {
		return nil
	}
	var bySignature *domainContractForExpansion

	// NOTE: job.State may contain:
	// - []interface{} (when loaded from Firestore/JSON)
	// - []agents.DomainResolution (when created in-process from lens.ViewConfig)
	// - []map[string]interface{} (various runtime encodings)

	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			c := &domainContractForExpansion{
				CanonicalPath:      strings.TrimSpace(d.CanonicalPath),
				ContractType:       strings.TrimSpace(d.ContractType),
				Title:              strings.TrimSpace(d.Title),
				Description:        strings.TrimSpace(d.Description),
				Facets:             d.Facets,
				FirstOrderEntities: append([]string(nil), d.FirstOrderEntities...),
				RelatedDomains:     append([]string(nil), d.RelatedDomains...),
				AffectedDomains:    append([]string(nil), d.AffectedDomains...),
			}
			cn := normalizeDomainPathForMatch(c.CanonicalPath)
			if _, ok := matchPaths[cn]; ok {
				return c
			}
			ct, facets := parseDomainPathFacets(cn)
			if cpSig != "" && domainFacetSignature(ct, facets) == cpSig {
				if bySignature == nil || domainContractQualityScore(c) > domainContractQualityScore(bySignature) {
					bySignature = c
				}
			}
		}
		return bySignature
	case []map[string]interface{}:
		list := make([]interface{}, 0, len(t))
		for _, m := range t {
			list = append(list, m)
		}
		raw = list
	}

	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 {
		return nil
	}
	getStringField := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok {
				if v := strings.TrimSpace(s); v != "" {
					return v
				}
			}
		}
		return ""
	}
	getMapField := func(m map[string]interface{}, keys ...string) map[string]interface{} {
		for _, k := range keys {
			if v, ok := m[k].(map[string]interface{}); ok && v != nil {
				return v
			}
		}
		return nil
	}
	for _, it := range list {
		m, ok := it.(map[string]interface{})
		if !ok || m == nil {
			continue
		}
		v := getStringField(m, "canonicalPath", "CanonicalPath")
		out := &domainContractForExpansion{
			CanonicalPath: strings.TrimSpace(v),
		}
		out.ContractType = getStringField(m, "contractType", "ContractType")
		out.Title = getStringField(m, "title", "Title")
		out.Description = getStringField(m, "description", "Description")
		// Facets: map[string][]string
		if fv := getMapField(m, "facets", "Facets"); fv != nil {
			fm := map[string][]string{}
			for k, vv := range fv {
				kk := strings.TrimSpace(k)
				if kk == "" {
					continue
				}
				fm[kk] = parseStateStringSlice(vv)
			}
			out.Facets = fm
		}
		out.FirstOrderEntities = parseStateStringSlice(m["firstOrderEntities"])
		if len(out.FirstOrderEntities) == 0 {
			out.FirstOrderEntities = parseStateStringSlice(m["FirstOrderEntities"])
		}
		out.RelatedDomains = parseStateStringSlice(m["relatedDomains"])
		if len(out.RelatedDomains) == 0 {
			out.RelatedDomains = parseStateStringSlice(m["RelatedDomains"])
		}
		out.AffectedDomains = parseStateStringSlice(m["affectedDomains"])
		if len(out.AffectedDomains) == 0 {
			out.AffectedDomains = parseStateStringSlice(m["AffectedDomains"])
		}
		cn := normalizeDomainPathForMatch(out.CanonicalPath)
		if _, ok := matchPaths[cn]; ok {
			return out
		}
		ct, facets := parseDomainPathFacets(cn)
		if cpSig != "" && domainFacetSignature(ct, facets) == cpSig {
			if bySignature == nil || domainContractQualityScore(out) > domainContractQualityScore(bySignature) {
				bySignature = out
			}
		}
	}
	return bySignature
}

func parseDomainPathFacets(domainCP string) (contractType string, facets map[string][]string) {
	domainCP = strings.TrimSpace(domainCP)
	if !strings.HasPrefix(domainCP, "/domain/v1/") {
		return "", nil
	}
	parts := strings.Split(strings.Trim(domainCP, "/"), "/")
	// ["domain","v1",contractType,(k,v)*]
	if len(parts) < 3 {
		return "", nil
	}
	contractType = strings.ToLower(strings.TrimSpace(parts[2]))
	rest := parts[3:]
	if len(rest)%2 != 0 {
		// invalid shape; ignore
		return contractType, nil
	}
	facets = map[string][]string{}
	for i := 0; i+1 < len(rest); i += 2 {
		k := strings.ToLower(strings.TrimSpace(rest[i]))
		v := strings.ToLower(strings.TrimSpace(rest[i+1]))
		if k == "" || v == "" {
			continue
		}
		// For geo facets, split composite tokens (ae-sa) into individual ISO2 codes so
		// domain research + deterministic first-order expansion can work with per-country scope.
		if k == "geo" && strings.Contains(v, "-") {
			parts := strings.Split(v, "-")
			for _, p := range parts {
				p = strings.ToLower(strings.TrimSpace(p))
				if len(p) == 2 {
					facets[k] = append(facets[k], p)
				}
			}
			continue
		}
		facets[k] = append(facets[k], v)
	}
	return contractType, facets
}

func contractTypesFromDomainsRaw(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	out := make([]string, 0, 8)
	add := func(v string) {
		v = strings.TrimSpace(strings.ToLower(v))
		if v != "" {
			out = append(out, v)
		}
	}
	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			add(d.ContractType)
		}
	case []models.LensDomainQuery:
		for _, d := range t {
			add(d.ContractType)
		}
	case []map[string]interface{}:
		for _, m := range t {
			add(getStringField(m, "contractType", "ContractType"))
		}
	case []interface{}:
		for _, it := range t {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			add(getStringField(m, "contractType", "ContractType"))
		}
	}
	return out
}

func contractTypesFromLensScope(scope []string) []string {
	out := make([]string, 0, 4)
	for _, p := range scope {
		ct, _ := parseDomainPathFacets(strings.TrimSpace(p))
		ct = strings.TrimSpace(strings.ToLower(ct))
		if ct != "" {
			out = append(out, ct)
		}
	}
	return out
}

func writeJobDomainModesState(job *models.ScoutingJob, modes []string) {
	if job == nil {
		return
	}
	modes = taxonomy.NormalizeDomainModes(modes)
	if len(modes) == 1 {
		job.State["domainMode"] = modes[0]
		delete(job.State, "domainModes")
		return
	}
	job.State["domainModes"] = modes
	delete(job.State, "domainMode")
}

func ensureNativeGovernanceModeForPath(modes []string, canonicalPath string) []string {
	modes = taxonomy.NormalizeDomainModes(modes)
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	if !strings.HasPrefix(cp, "/org/") && !strings.HasPrefix(cp, "/gov/") {
		return modes
	}
	if taxonomy.HasDomainMode(modes, taxonomy.DomainModeGovernance) {
		return modes
	}
	if len(modes) == 0 {
		modes = []string{taxonomy.DomainModeGeneral}
	}
	modes = append(modes, taxonomy.DomainModeGovernance)
	return taxonomy.NormalizeDomainModes(modes)
}

// ensureJobDomainMode computes domain mode(s) from path, domains in job/lens state, and specialties fallback,
// then writes job.State["domainMode"] or job.State["domainModes"].
// Call once at the start of job execution, after effectiveCanonicalPath is known.
// If domainModes/domainMode already set (e.g. from lens at job creation), do not overwrite.
func (e *ScoutEngine) ensureJobDomainMode(ctx context.Context, job *models.ScoutingJob, effectiveCanonicalPath string) {
	if job == nil {
		return
	}
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	path := strings.TrimSpace(effectiveCanonicalPath)
	if path == "" {
		path = strings.TrimSpace(job.TargetCanonicalPath)
	}

	// Prefer pre-seeded domain modes from lens/job payload, but still enforce native governance for /org|/gov.
	if v, ok := job.State["domainModes"]; ok && v != nil {
		modes := parseStateStringSlice(v)
		modes = ensureNativeGovernanceModeForPath(modes, path)
		if len(modes) > 0 {
			writeJobDomainModesState(job, modes)
			return
		}
	}
	if v, ok := job.State["domainMode"]; ok && v != nil {
		if s := parseStateString(v); s != "" {
			modes := ensureNativeGovernanceModeForPath([]string{s}, path)
			if len(modes) > 0 {
				writeJobDomainModesState(job, modes)
				return
			}
		}
	}

	var contractTypes []string
	// 1) From path if /domain/v1/
	if strings.HasPrefix(path, "/domain/v1/") {
		ct, _ := parseDomainPathFacets(path)
		if ct != "" {
			contractTypes = append(contractTypes, ct)
		}
	}
	// 2) From job.State["domains"]
	if len(contractTypes) == 0 && job.State != nil {
		if raw, ok := job.State["domains"]; ok {
			contractTypes = append(contractTypes, contractTypesFromDomainsRaw(raw)...)
		}
	}

	// 3) From lens when job state lacks domains (e.g., reused jobs, old payloads).
	// Prefer lens.DomainModes (new); else lens.ViewConfig["domains"] → contractTypes; else lens.Scope (legacy /domain/ paths).
	var lensModes []string
	if len(contractTypes) == 0 && e != nil && e.lensesRepo != nil {
		lensID := strings.TrimSpace(job.LensID)
		if lensID == "" && job.State != nil {
			lensID = parseStateString(job.State["lensId"])
		}
		if lensID != "" && strings.TrimSpace(job.TenantID) != "" {
			if lens, err := e.lensesRepo.Get(ctx, lensID); err == nil && lens != nil {
				if strings.TrimSpace(lens.TenantID) == strings.TrimSpace(job.TenantID) {
					if len(lens.DomainModes) > 0 {
						lensModes = taxonomy.NormalizeDomainModes(lens.DomainModes)
					} else if len(lens.DomainQueries) > 0 {
						contractTypes = append(contractTypes, contractTypesFromDomainsRaw(lens.DomainQueries)...)
					} else if lens.ViewConfig != nil {
						if raw, ok := lens.ViewConfig["domains"]; ok {
							contractTypes = append(contractTypes, contractTypesFromDomainsRaw(raw)...)
						}
					}
					if len(contractTypes) == 0 && len(lensModes) == 0 {
						contractTypes = append(contractTypes, contractTypesFromLensScope(lens.Scope)...)
					}
				}
			}
		}
	}

	// 4) From specialties fallback
	if len(contractTypes) == 0 && job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			specs := parseStateStringSlice(v)
			specs = taxonomy.NormalizeSpecialties(specs)
			for _, s := range specs {
				s = strings.TrimSpace(strings.ToLower(s))
				switch s {
				case taxonomy.SpecialtySupplyChain:
					contractTypes = append(contractTypes, "supply-chain")
				case taxonomy.SpecialtyEquity:
					contractTypes = append(contractTypes, "equity")
				case taxonomy.SpecialtyManagement:
					contractTypes = append(contractTypes, "ownership-structure")
				case taxonomy.SpecialtyProductsAndServices:
					contractTypes = append(contractTypes, "supply-chain")
				}
			}
		}
	}

	var modes []string
	if len(lensModes) > 0 {
		modes = lensModes
	} else if len(contractTypes) > 0 {
		modes = domain.DomainModesForContractTypes(contractTypes)
	}
	modes = taxonomy.NormalizeDomainModes(modes)
	if len(modes) == 0 {
		modes = []string{taxonomy.DomainModeGeneral}
	}
	modes = ensureNativeGovernanceModeForPath(modes, path)
	writeJobDomainModesState(job, modes)
}

func getStringField(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func (e *ScoutEngine) executeDomainExpansionJob(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	if e == nil || job == nil {
		return nil
	}
	domainCP := strings.TrimSpace(effectiveCanonicalPath)
	if domainCP == "" {
		domainCP = strings.TrimSpace(job.TargetCanonicalPath)
	}
	domainCP = NormalizeRuntimeDomainCanonicalPath(domainCP)
	if domainCP == "" || !strings.HasPrefix(domainCP, "/domain/") {
		return nil
	}
	pipelineRuntimeName := pipelineRuntimeDomainBase
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stagePlanning, true, "domain_base_planning_done")
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageExtraction, true, "domain_base_extraction_done")
	e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageCritic, true, "domain_base_critic_skip")
	e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageRevalidation, true, "domain_base_revalidation_skip")

	// Early detection: supply chain plan mode skips grounding, overlays, facet relations, artifact builder.
	earlyContract := e.bestEffortFindDomainContract(job, domainCP)
	earlyContractType := ""
	if earlyContract != nil {
		earlyContractType = strings.TrimSpace(earlyContract.ContractType)
	}
	if earlyContractType == "" {
		earlyContractType, _ = parseDomainPathFacets(domainCP)
	}
	earlySupplyChainPlan := getSupplyChainExecutionPlanFromJobState(job, domainCP)
	isSupplyChainFastPath := agents.IsSupplyChainContractType(earlyContractType) && earlySupplyChainPlan != nil && len(earlySupplyChainPlan.Queries) > 0

	internalFirstContext := ""
	domainFastPathApplied := false
	if internalFirstGroundingEnabled() && !isSupplyChainFastPath {
		if e.planner != nil && e.executor != nil {
			sourcePlan, err := e.planner.PlanSources(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(job.ProjectID), domainCP, models.ScoutingStrategySnapshot, nil, "")
			if err != nil {
				logGroundingWarnf("domain expansion PlanSources failed: jobId=%s target=%s err=%v", strings.TrimSpace(job.ID), strings.TrimSpace(domainCP), err)
			} else if sourcePlan != nil {
				for _, src := range sourcePlan.Sources {
					if src == nil {
						continue
					}
					res, execErr := e.executor.ExecuteDataSource(ctx, src)
					if execErr != nil {
						logGroundingWarnf("domain expansion source execution failed: jobId=%s sourceId=%s kind=%s err=%v",
							strings.TrimSpace(job.ID), strings.TrimSpace(src.ID), strings.TrimSpace(src.Kind), execErr)
						continue
					}
					if s := strings.TrimSpace(res.Content); s != "" {
						internalFirstContext += s + "\n\n"
					}
				}
			}
		}
		if gctx := e.buildGraphBackedInternalContext(ctx, strings.TrimSpace(job.TenantID), domainCP); strings.TrimSpace(gctx) != "" {
			if internalFirstContext != "" {
				internalFirstContext += "\n\n"
			}
			internalFirstContext += gctx
		}
		// Reuse deterministic facts fast-path before any grounded AI calls.
		if e.planner != nil && e.planner.factsRepo != nil && e.fragmentsRepo != nil {
			domainFragment := existingFragment
			if domainFragment == nil || strings.TrimSpace(domainFragment.ID) == "" {
				if existingByPath, err := e.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(domainCP)); err == nil && existingByPath != nil {
					domainFragment = existingByPath
					existingFragment = existingByPath
				}
			}
			if domainFragment != nil && strings.TrimSpace(domainFragment.ID) != "" {
				facts, err := e.planner.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(domainCP), time.Time{}, 200)
				if err != nil {
					logGroundingWarnf("domain expansion facts fast-path failed: jobId=%s target=%s err=%v",
						strings.TrimSpace(job.ID), strings.TrimSpace(domainCP), err)
				} else if len(facts) > 0 {
					applier := &FactApplier{vertex: e.planner.vertexSearchClient}
					applyRes, applyErr := applier.TryApplyFactsFastPath(ctx, e.fragmentsRepo, domainFragment, facts, 0.70, 0.80)
					if applyErr != nil {
						logGroundingErrorWithContext(job, "scout", "domain_expansion.fast_path", "domain_fast_path_apply_failed", "fact_fast_path_error", true, applyErr)
						e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.fast_path", "domain_fast_path_apply_failed", "fact_fast_path_error", true)
					} else if applyRes != nil && applyRes.Applied {
						domainFastPathApplied = true
						logGroundingInfof("domain expansion fast-path applied: jobId=%s target=%s appliedFacts=%d maxRelevance=%.3f",
							strings.TrimSpace(job.ID), strings.TrimSpace(domainCP), applyRes.AppliedFacts, applyRes.MaxRelevance)
					}
				}
			}
		}
	}

	inlineDomainContractRun := false
	if job.State != nil {
		if v, ok := job.State["inlineDomainContractRun"]; ok {
			inlineDomainContractRun = parseStateBool(v, false)
		}
	}
	if !inlineDomainContractRun {
		if err := e.transition(ctx, job, models.ScoutingJobStateExecution, 0.4, "domain_expansion"); err != nil {
			logGroundingErrorWithContext(job, "scout", "domain_expansion.transition_execution", "job_transition_failed", "state_transition_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.transition_execution", "job_transition_failed", "state_transition_error", true)
		}
	}

	contract := e.bestEffortFindDomainContract(job, domainCP)
	title := ""
	desc := ""
	contractType := ""
	firstOrder := []string(nil)
	related := []string(nil)
	affected := []string(nil)
	// Optional per-entity relation hints produced by LLM domain research.
	type relHint struct {
		rel    string
		conf   float64
		reason string
	}
	researchRelByPath := map[string]relHint{}
	if contract != nil {
		title = strings.TrimSpace(contract.Title)
		desc = strings.TrimSpace(contract.Description)
		contractType = strings.TrimSpace(contract.ContractType)
		firstOrder = append([]string(nil), contract.FirstOrderEntities...)
		related = append([]string(nil), contract.RelatedDomains...)
		affected = append([]string(nil), contract.AffectedDomains...)
	}
	// If we couldn't find contract in job.State, parse facets from canonicalPath so domain research
	// can still do its core job (expand meaning into entities).
	parsedCT, parsedFacets := parseDomainPathFacets(domainCP)
	if contractType == "" {
		contractType = strings.TrimSpace(parsedCT)
	}

	// Prefer intent-derived human title from job state over generic contractType auto-title.
	facetsForTitle := parsedFacets
	if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
		facetsForTitle = contract.Facets
	}
	autoTitle := buildDomainTitle(contractType, domainCP, facetsForTitle)
	if focusTitle := bestEffortDomainTitleFromJobState(job); focusTitle != "" {
		if strings.TrimSpace(title) == "" || strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(autoTitle)) {
			title = focusTitle
		}
	}
	// Final fallback: deterministic contractType/facets title.
	if title == "" {
		title = autoTitle
	}

	if desc == "" {
		desc = "Executable knowledge contract for exploration."
	}

	// Deterministic fallback: if intent didn't provide firstOrderEntities, derive a minimal set from facets.
	if len(firstOrder) == 0 && contract != nil && len(contract.Facets) > 0 {
		getFacet := func(key string) []string {
			if contract.Facets == nil {
				return nil
			}
			if v, ok := contract.Facets[key]; ok {
				return v
			}
			// case-insensitive fallback
			for k, v := range contract.Facets {
				if strings.EqualFold(strings.TrimSpace(k), key) {
					return v
				}
			}
			return nil
		}
		geoToken := "global"
		if v := getFacet("geo"); len(v) > 0 {
			g := strings.TrimSpace(strings.ToLower(v[0]))
			if g == "int" {
				geoToken = "global"
			} else if gg := taxonomy.Slugify(g); gg != "" {
				geoToken = gg
			}
		}
		switch strings.TrimSpace(strings.ToLower(contractType)) {
		case "supply-chain", "market-opportunity":
			// Minimal deterministic anchors for supply/market-oriented contracts.
			// - If material present: /market/{geo}/materials/{material}
			if mats := getFacet("material"); len(mats) > 0 {
				mat := taxonomy.Slugify(mats[0])
				if mat != "" {
					mp := "/market/" + geoToken + "/materials/" + mat
					if err := taxonomy.ValidateCanonicalPath(mp); err == nil {
						firstOrder = append(firstOrder, mp)
					}
				}
			}
		}
	}

	// LLM-based domain research: reuse standard ResearchAgent to keep parity with non-domain research.
	// IntentResolver is NOT required to fully expand domains; this step is responsible for that.
	// Plan mode: when supply-chain contractType + SupplyChainExecutionPlan with queries, use holistic full-query strategy instead of fragment-centric ResearchPipeline.
	var domainResearchRelations []agents.DiscoveredRelation
	var domainResearchChains []agents.SupplyChain
	var domainGroundingEvidence *ai.GroundingEvidence
	researchStageActive := false
	supplyChainRefineContinuation := agents.IsSupplyChainContractType(contractType) && isSupplyChainRefineContinuationJob(job)
	if domainFastPathApplied {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageResearch, true, "domain_base_research_skip_fast_path")
	} else if supplyChainRefineContinuation {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageResearch, true, "domain_base_research_skip_supply_chain_refine_continuation")
	} else {
		e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageResearch, true, "domain_base_research")
		researchStageActive = true
	}
	supplyChainPlan := getSupplyChainExecutionPlanFromJobState(job, domainCP)
	usePlanMode := agents.IsSupplyChainContractType(contractType) && supplyChainPlan != nil && len(supplyChainPlan.Queries) > 0
	graphBuildMode := supplyChainGraphBuildMode(supplyChainPlan, job)
	hotPathOnlySupplyChain := usePlanMode && graphBuildMode == agents.SupplyChainGraphBuildModeHotPathOnly
	fastFirstSupplyChain := usePlanMode && (hotPathOnlySupplyChain || isFastFirstSupplyChainJob(job)) && !supplyChainRefineContinuation
	if usePlanMode && e.researchAgent != nil && !domainFastPathApplied && !supplyChainRefineContinuation {
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		if supplyChainPlan != nil {
			supplyChainPlan.GraphBuildMode = graphBuildMode
		}
		job.State["supplyChainGraphBuildMode"] = graphBuildMode
		job.State["supplyChainIncrementalMaterialized"] = false
		if fastFirstSupplyChain {
			job.State["supplyChainFastFirstMaxAnchors"] = 6
			job.State["supplyChainFastFirstMaxAnchorHops"] = 1
			job.State["supplyChainSkipRecursiveStep4"] = true
			job.State["supplyChainStopAfterFirstIncremental"] = true
			job.State["supplyChainFastFirstMaxOrgsPerDepth"] = 8
			job.State["supplyChainFastFirstMaxFacsPerDepth"] = 18
		}
		queries := supplyChainPlan.Queries
		if job != nil && HasExecutionPlanContractState(job.State) {
			planContract := ExecutionPlanContractFromState(job.State)
			if planContract.MaxSupplyChainQueries > 0 && len(queries) > planContract.MaxSupplyChainQueries {
				queries = queries[:planContract.MaxSupplyChainQueries]
				e.logLine(ctx, job, "info", fmt.Sprintf("> supply chain plan capped (%s): %d", RuntimeContractExecutionPlan, len(queries)))
			}
		}
		if job != nil {
			seedProducts := make([]string, 0, 8)
			for _, query := range queries {
				seedProducts = append(seedProducts, query.ProductFilter...)
			}
			ensureWorkingProductScopeLoaded(ctx, e, job, dedupStrings(seedProducts), "query_seed", domainCP)
		}
		// Expand product/market anchors to org/fac list: first query should go FROM organizations or facilities.
		queriesBefore := len(queries)
		queries = e.expandSupplyChainQueriesWithOrgs(ctx, job, queries)
		if fastFirstSupplyChain {
			queries = limitSupplyChainQueriesForFastFirst(queries, 3)
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainPlan] queries before=%d after_expansion=%d", queriesBefore, len(queries)))
		hotPathSatisfied := false
		allowGraphCompletion := !hotPathOnlySupplyChain
		if fastFirstSupplyChain {
			hotPathChains := e.runSupplyChainFastFirstHotPath(ctx, job, queries)
			if len(hotPathChains) > 0 {
				domainResearchChains = append(domainResearchChains, hotPathChains...)
				if e.materializeSupplyChainHotPathChains(ctx, job, hotPathChains) {
					scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainHotPath] materialized chains=%d", len(hotPathChains)))
				}
				if hasUsefulSupplyChainHotPathMaterialization(job, hotPathChains) {
					scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainHotPath] early stop after useful hot-path materialization chains=%d", len(hotPathChains)))
					hotPathSatisfied = true
				}
			}
		}
		if allowGraphCompletion && !hotPathSatisfied {
			// Collect anchors by direction: downstream (producer→consumer) and upstream (consumer→supplier).
			defaultDirection := "down"
			if strings.TrimSpace(job.LensID) != "" {
				defaultDirection = "both"
			}
			downOrgs, downFacs, upOrgs, upFacs := collectAnchorOrgsFromQueriesByDirection(queries, defaultDirection)
			if fastFirstSupplyChain {
				downOrgs = capSupplyChainFastFirstAnchors(downOrgs, 4)
				downFacs = capSupplyChainFastFirstAnchors(downFacs, 3)
				upOrgs = capSupplyChainFastFirstAnchors(upOrgs, 4)
				upFacs = capSupplyChainFastFirstAnchors(upFacs, 3)
			}
			downFilters, upFilters := collectSupplyChainFiltersByDirection(queries, defaultDirection)
			scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainPlan] anchors: down orgs=%d facs=%d, up orgs=%d facs=%d", len(downOrgs), len(downFacs), len(upOrgs), len(upFacs)))
			unwindMaxDepth := 2
			if strings.HasPrefix(strings.TrimSpace(domainCP), "/domain/v1/supply-chain/") {
				if _, facets := parseDomainPathFacets(domainCP); facets != nil {
					hasGeo := len(facets["geo"]) > 0 && strings.TrimSpace(facets["geo"][0]) != ""
					hasProduct := len(facets["product"]) > 0 && strings.TrimSpace(facets["product"][0]) != ""
					if hasGeo && hasProduct {
						// Tight contract (geo + product) should stay focused and avoid global fanout.
						unwindMaxDepth = 1
					}
				}
			}
			if job != nil && job.State != nil {
				if v := parseStateInt(job.State["supplyChainUnwindMaxDepth"], 0); v > 0 && v < 6 {
					unwindMaxDepth = v
				}
			}
			if fastFirstSupplyChain {
				unwindMaxDepth = 1
				scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainPlan] fast-first caps enabled: queries=%d down(org=%d fac=%d) up(org=%d fac=%d)", len(queries), len(downOrgs), len(downFacs), len(upOrgs), len(upFacs)))
			} else if job != nil && job.State != nil {
				delete(job.State, "supplyChainFastFirstMaxAnchors")
				delete(job.State, "supplyChainFastFirstMaxAnchorHops")
				delete(job.State, "supplyChainSkipRecursiveStep4")
				delete(job.State, "supplyChainStopAfterFirstIncremental")
				delete(job.State, "supplyChainFastFirstMaxOrgsPerDepth")
				delete(job.State, "supplyChainFastFirstMaxFacsPerDepth")
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainPlan] unwind maxDepth=%d", unwindMaxDepth))
			if len(downOrgs) > 0 || len(downFacs) > 0 {
				chains := e.runSupplyChainRecursiveUnwinding(ctx, job, downOrgs, downFacs, unwindMaxDepth, downFilters.Product, downFilters.Geo)
				domainResearchChains = append(domainResearchChains, chains...)
			}
			if len(upOrgs) > 0 || len(upFacs) > 0 {
				upChains := e.runSupplyChainUpstreamUnwinding(ctx, job, upOrgs, upFacs, unwindMaxDepth, upFilters.Product, upFilters.Geo)
				domainResearchChains = append(domainResearchChains, upChains...)
			}
		}
		domainResearchChains = dedupSupplyChainChains(domainResearchChains)
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		domainResearch := map[string]interface{}{
			"planMode":               true,
			"queriesN":               len(queries),
			"supplyChainsN":          len(domainResearchChains),
			"graphBuildMode":         graphBuildMode,
			"graphCompletionSkipped": hotPathOnlySupplyChain,
		}
		job.State["domainResearch"] = domainResearch
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
			"domainResearch":            domainResearch,
			"supplyChainGraphBuildMode": graphBuildMode,
		})
	}

	// Regulatory Impact plan-mode: holistic impact graph queries.
	regulatoryPlan := getRegulatoryImpactPlanFromJobState(job, domainCP)
	useRegPlanMode := !usePlanMode && agents.IsPolicyImpactContractType(contractType) && regulatoryPlan != nil && len(regulatoryPlan.Queries) > 0
	if useRegPlanMode && e.researchAgent != nil && !domainFastPathApplied {
		for _, q := range regulatoryPlan.Queries {
			rels, err := e.researchAgent.EnrichRegulatoryImpactFull(ctx, q)
			if err != nil {
				e.logLine(ctx, job, "warn", fmt.Sprintf("> regulatory impact plan query failed (instrument=%s): %v", strings.TrimSpace(q.Instrument), err))
				continue
			}
			domainResearchRelations = append(domainResearchRelations, rels...)
		}
		autoRefine := len(domainResearchRelations) > 0 && regulatoryGraphNeedsAutoRefine(domainResearchRelations, domainCP)
		refineFlag := false
		if job.State != nil {
			refineFlag = parseStateBool(job.State["regulatoryImpactRefinePass"], false)
		}
		if len(domainResearchRelations) > 0 && (autoRefine || refineFlag) {
			extra, rerr := e.researchAgent.EnrichRegulatoryImpactRefinement(ctx, domainCP, domainResearchRelations)
			if rerr != nil {
				e.logLine(ctx, job, "warn", fmt.Sprintf("> regulatory impact refinement failed: %v", rerr))
			} else if len(extra) > 0 {
				domainResearchRelations = append(domainResearchRelations, extra...)
			}
		}
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		domainResearch := map[string]interface{}{
			"planMode":   true,
			"planType":   "regulatoryImpact",
			"queriesN":   len(regulatoryPlan.Queries),
			"relationsN": len(domainResearchRelations),
		}
		job.State["domainResearch"] = domainResearch
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainResearch": domainResearch})
	}

	// Governance plan-mode: holistic ownership/control graph queries.
	govPlan := getGovernancePlanFromJobState(job, domainCP)
	useGovPlanMode := !usePlanMode && !useRegPlanMode && agents.IsGovernanceContractType(contractType) && govPlan != nil && len(govPlan.Queries) > 0
	if useGovPlanMode && e.researchAgent != nil && !domainFastPathApplied {
		for _, q := range govPlan.Queries {
			rels, err := e.researchAgent.EnrichGovernanceFullForContract(ctx, q, contractType)
			if err != nil {
				e.logLine(ctx, job, "warn", fmt.Sprintf("> governance plan query failed (target=%s): %v", strings.TrimSpace(q.Target), err))
				continue
			}
			domainResearchRelations = append(domainResearchRelations, rels...)
		}
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		domainResearch := map[string]interface{}{
			"planMode":   true,
			"planType":   "governance",
			"queriesN":   len(govPlan.Queries),
			"relationsN": len(domainResearchRelations),
		}
		job.State["domainResearch"] = domainResearch
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainResearch": domainResearch})
	}

	// Ecosystem plan-mode: holistic multi-actor ecosystem mapping queries.
	ecoPlan := getEcosystemPlanFromJobState(job, domainCP)
	useEcoPlanMode := !usePlanMode && !useRegPlanMode && !useGovPlanMode && agents.IsEcosystemContractType(contractType) && ecoPlan != nil && len(ecoPlan.Queries) > 0
	if useEcoPlanMode && e.researchAgent != nil && !domainFastPathApplied {
		for _, q := range ecoPlan.Queries {
			rels, err := e.researchAgent.EnrichEcosystemFull(ctx, q)
			if err != nil {
				e.logLine(ctx, job, "warn", fmt.Sprintf("> ecosystem plan query failed (focus=%s): %v", strings.TrimSpace(q.Focus), err))
				continue
			}
			domainResearchRelations = append(domainResearchRelations, rels...)
		}
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		domainResearch := map[string]interface{}{
			"planMode":   true,
			"planType":   "ecosystem",
			"queriesN":   len(ecoPlan.Queries),
			"relationsN": len(domainResearchRelations),
		}
		job.State["domainResearch"] = domainResearch
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainResearch": domainResearch})
	}

	// Affected-zone holistic mode: single query across condition types (no execution plan needed).
	geoToken := ""
	if g := parsedFacets["geo"]; len(g) > 0 {
		geoToken = strings.Join(g, "-")
	}
	useAffectedZoneMode := !usePlanMode && !useRegPlanMode && !useGovPlanMode && !useEcoPlanMode && agents.IsAffectedZoneContractType(contractType) && e.researchAgent != nil && !domainFastPathApplied
	if useAffectedZoneMode {
		log.Printf("[ScoutEngine] affected-zone: holistic mode triggered jobId=%s target=%s geo=%s", strings.TrimSpace(job.ID), domainCP, geoToken)
		aq := agents.AffectedZoneQuery{Geo: geoToken, Title: title}
		if job.State != nil {
			if v, ok := job.State["originalQuery"].(string); ok {
				aq.Reason = strings.TrimSpace(v)
			}
		}
		rels, err := e.researchAgent.EnrichAffectedZoneFull(ctx, aq)
		if err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> affected-zone holistic query failed (geo=%s): %v", geoToken, err))
			log.Printf("[ScoutEngine] affected-zone: holistic query FAILED jobId=%s geo=%s err=%v", strings.TrimSpace(job.ID), geoToken, err)
		} else {
			zonePaths := make([]string, 0, len(rels)*2)
			seen := make(map[string]struct{})
			for _, r := range rels {
				for _, p := range []string{strings.TrimSpace(r.TargetCanonicalPath), strings.TrimSpace(r.SourceCanonicalPath)} {
					if p != "" && strings.HasPrefix(p, "/zone/") {
						if _, ok := seen[p]; !ok {
							seen[p] = struct{}{}
							zonePaths = append(zonePaths, p)
						}
					}
				}
			}
			domainResearchRelations = append(domainResearchRelations, rels...)
			sample := zonePaths
			if len(sample) > 5 {
				sample = sample[:5]
			}
			log.Printf("[ScoutEngine] affected-zone: holistic query OK jobId=%s geo=%s relationsN=%d zonePathsN=%d sample=%v", strings.TrimSpace(job.ID), geoToken, len(rels), len(zonePaths), sample)
		}
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		domainResearch := map[string]interface{}{
			"planMode":   true,
			"planType":   "affectedZone",
			"relationsN": len(domainResearchRelations),
		}
		job.State["domainResearch"] = domainResearch
		_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainResearch": domainResearch})
	}

	anyPlanMode := usePlanMode || useRegPlanMode || useGovPlanMode || useEcoPlanMode || useAffectedZoneMode
	if !anyPlanMode && e.researchAgent != nil && !domainFastPathApplied {
		originalQuery := ""
		if job.State != nil {
			if v, ok := job.State["originalQuery"].(string); ok {
				originalQuery = strings.TrimSpace(v)
			}
		}
		e.publish(ctx, realtime.EventType("horizon.domain.research.started"), map[string]interface{}{
			"tenantId":      job.TenantID,
			"projectId":     job.ProjectID,
			"lensId":        strings.TrimSpace(job.LensID),
			"jobId":         job.ID,
			"canonicalPath": domainCP,
			"humanReadable": map[string]interface{}{
				"title":       "Domain research started",
				"description": fmt.Sprintf("%s • %s", title, domainCP),
				"entityName":  title,
			},
		})

		// Extract focus signals from job.State (same approach as normal research).
		focus := ""
		var spherePaths []string
		var keywords []string
		var specialties []string
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
					for _, p := range strings.Split(t, ",") {
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
					for _, p := range strings.Split(t, ",") {
						keywords = append(keywords, p)
					}
				}
			}
			if v, ok := job.State["specialties"]; ok {
				specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
			}
		}

		// Domain fragment for research prompt context (same agent, same pipeline).
		domainFragment := &models.HorizonFragment{
			TenantID:      strings.TrimSpace(job.TenantID),
			CanonicalPath: domainCP,
			Type:          models.FragmentTypeDomain,
			Labels:        map[string]string{"en": title},
			Description:   desc,
			Status:        "active",
		}
		domainMissing := []string{
			fmt.Sprintf(
				"Expand this DOMAIN contract into 5-10 first-order entities and relations. Use DOMAIN relation types (%s) and valid canonical paths only. ContractType=%s; Facets=%v; Title=%s.",
				DomainScaffoldRelationTypesString(),
				strings.TrimSpace(contractType),
				parsedFacets,
				title,
			),
		}
		if strings.TrimSpace(strings.ToLower(contractType)) == "affected-zone" {
			domainMissing = append(domainMissing, "For affected-zone: output AFFECTED_ZONE targets with /zone/{geo}/{condition_type}/{slug}. slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas, northern-syria). NEVER use \"na\" as slug — use region name or condition-specific name (e.g. myanmar-crisis-area). condition_type: military_conflict, natural_disaster, political_tension, humanitarian_crisis, earthquake_affected, flood_zone. geo from facets or query context. ZONE = geographic area; EVENT = discrete occurrence (TRIGGERED_BY links zone→event). Request radius_km ONLY when you have reliable, evidence-based data; be conservative and verifiable—avoid overly broad interpretations.")
		}
		// Collect existing entities from session graph for duplicate prevention.
		// Domain jobs never populate session graph before this step, so fall back to lens scope.
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
		if len(existingEntities) == 0 && job != nil && strings.TrimSpace(job.LensID) != "" && e.lensesRepo != nil {
			if lens, lerr := e.lensesRepo.Get(ctx, strings.TrimSpace(job.LensID)); lerr == nil && lens != nil {
				for _, p := range lens.Scope {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					existingEntities = append(existingEntities, p)
				}
			}
		}
		enrichedFragment, newRelations, newChains, evidence, _, err := e.researchAgent.EnrichFragmentWithGrounding(ctx, domainFragment, domainMissing, focus, spherePaths, keywords, specialties, internalFirstContext, originalQuery, nil, existingEntities)
		_ = enrichedFragment
		if err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> domain research failed: %v", err))
		} else {
			newRelations = sanitizeDomainResearchRelations(domainCP, contractType, newRelations)
			domainGroundingEvidence = mergeGroundingEvidence(domainGroundingEvidence, evidence)
			if internalFirstGroundingEnabled() && evidence != nil {
				applyGroundingEvidenceToJobState(job, domainGroundingEvidence, nil, nil)
				logGroundingInfof("domain base research grounding: jobId=%s target=%s datastore=%v(%d) web=%v(%d)",
					strings.TrimSpace(job.ID),
					strings.TrimSpace(domainCP),
					evidence.HasDatastoreResults,
					len(evidence.DatastoreFragmentIDs),
					evidence.HasWebResults,
					len(evidence.WebSources),
				)
			}
			domainResearchRelations = append(domainResearchRelations, newRelations...)
			domainResearchChains = append(domainResearchChains, newChains...)
			for _, rel := range newRelations {
				cp := strings.TrimSpace(rel.TargetCanonicalPath)
				if cp == "" {
					continue
				}
				rt := strings.TrimSpace(rel.RelationType)
				if rt == "" {
					rt = string(models.DependencyTypeFocusesOn)
				}
				rt = strings.TrimSpace(taxonomy.NormalizeRelationType(rt))
				switch rt {
				case string(models.DependencyTypeFocusesOn),
					string(models.DependencyTypeInvolvesActor),
					string(models.DependencyTypeInGeoScope),
					string(models.DependencyTypeAbout),
					string(models.DependencyTypeDependsOn),
					string(models.DependencyTypeAffects),
					string(models.DependencyTypeSurfaces):
					// ok
				default:
					rt = canonicalDomainAnchorRelationType(rt, cp)
				}
				researchRelByPath[cp] = relHint{
					rel:    rt,
					conf:   normalizeEdgeConfidence(rel.Confidence),
					reason: strings.TrimSpace(rel.Reason),
				}
				firstOrder = append(firstOrder, cp)
			}
			if job.State == nil {
				job.State = map[string]interface{}{}
			}
			domainResearch := map[string]interface{}{
				"relationsN":    len(newRelations),
				"supplyChainsN": len(newChains),
			}
			job.State["domainResearch"] = domainResearch
			if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainResearch": domainResearch}); err != nil {
				logGroundingErrorWithContext(job, "scout", "domain_expansion.persist_research_state", "job_state_update_failed", "repo_update_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.persist_research_state", "job_state_update_failed", "repo_update_error", true)
			}
		}

		e.publish(ctx, realtime.EventType("horizon.domain.research.completed"), map[string]interface{}{
			"tenantId":      job.TenantID,
			"projectId":     job.ProjectID,
			"lensId":        strings.TrimSpace(job.LensID),
			"jobId":         job.ID,
			"canonicalPath": domainCP,
			"humanReadable": map[string]interface{}{
				"title":       "Domain research complete",
				"description": fmt.Sprintf("%s • %d entities", title, len(firstOrder)),
				"entityName":  title,
			},
		})
	}
	if researchStageActive {
		e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageResearch, true, "domain_base_research_done")
	}

	// Normalize/validate referenced canonical paths (fail-open, best-effort).
	filterValidCanonicalPaths := func(in []string) []string {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(in))
		for _, p := range in {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(p); err != nil {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
		return out
	}
	firstOrder = filterValidCanonicalPaths(firstOrder)
	related = filterValidCanonicalPaths(related)
	affected = filterValidCanonicalPaths(affected)

	// Hard cap: domain first-order entities should be small at startup (graph UX).
	// Keep deterministic order (already deduped by filterValidCanonicalPaths).
	if len(firstOrder) > 10 {
		firstOrder = firstOrder[:10]
	}

	// NOTE: Do not auto-extend lens scope with domain expansion results.
	// The lens scope should remain anchored to the initial domains & anchors to avoid graph sprawl.

	// Best-effort: publish WS start event (UI log feed).
	e.publish(ctx, realtime.EventType("horizon.domain.expansion.started"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"humanReadable": map[string]interface{}{
			"title":       "Domain expansion started",
			"description": fmt.Sprintf("%s • %s", title, domainCP),
			"entityName":  title,
		},
	})
	// New event namespace (preferred): base enrichment pass.
	e.publish(ctx, realtime.EventType("horizon.domain.base_enrichment.started"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"humanReadable": map[string]interface{}{
			"title":       "Base enrichment started",
			"description": fmt.Sprintf("%s • %s", title, domainCP),
			"entityName":  title,
		},
	})

	// Create/update DOMAIN fragment (contract node).
	frag := existingFragment
	if frag == nil {
		frag = &models.HorizonFragment{
			CanonicalPath: domainCP,
			Type:          models.FragmentTypeDomain,
			Status:        string(models.FragmentStatusActive),
			State:         map[string]models.MetricValue{},
			Governance: models.Governance{
				Owner:  "SYSTEM",
				Status: "ACTIVE",
			},
		}
	}
	// Ensure governance owner is always set (fragment.Validate requires SYSTEM).
	if strings.TrimSpace(frag.Governance.Owner) == "" {
		frag.Governance.Owner = "SYSTEM"
	}
	if strings.TrimSpace(frag.Governance.Status) == "" {
		frag.Governance.Status = "ACTIVE"
	}
	if frag.Labels == nil {
		frag.Labels = map[string]string{}
	}
	// Domain labels from intent resolution should always win over stale/fallback labels.
	// Unlike entity fragments, domain titles are deterministic (contractType + facets) and
	// the intent-resolved title carries richer context (e.g., full country names vs geo codes).
	if strings.TrimSpace(title) != "" {
		frag.Labels["en"] = title
	}
	if desc != "" {
		frag.Description = desc
	}
	frag.Type = models.FragmentTypeDomain
	if frag.Identifiers == nil {
		frag.Identifiers = map[string]string{}
	}
	if contractType != "" {
		frag.Identifiers["domainContractType"] = contractType
	}
	if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
		if b, err := json.Marshal(contract.Facets); err == nil && len(b) > 0 && len(b) < 8000 {
			frag.Identifiers["domainFacetsJson"] = string(b)
		}
	}

	var fragmentID string
	e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_fragment_upsert")
	if existingFragment == nil || strings.TrimSpace(existingFragment.ID) == "" {
		id, err := e.fragmentsRepo.Create(ctx, frag)
		if err != nil {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_fragment_create_failed", true, err)
			if !inlineDomainContractRun {
				if trErr := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_fragment_create_failed"); trErr != nil {
					logGroundingErrorWithContext(job, "scout", "domain_expansion.transition_failed", "job_transition_failed", "state_transition_error", true, trErr)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.transition_failed", "job_transition_failed", "state_transition_error", true)
				}
			}
			return fmt.Errorf("domain fragment create failed: %w", err)
		}
		fragmentID = id
		// Check if canonical path was changed during creation (e.g., by bestEffortAssembleCanonicalPathFromParts).
		if strings.TrimSpace(frag.CanonicalPath) != "" && strings.TrimSpace(frag.CanonicalPath) != strings.TrimSpace(domainCP) {
			oldPath := domainCP
			domainCP = strings.TrimSpace(frag.CanonicalPath)
			// Best-effort: update lens scope if path changed.
			tenantID := strings.TrimSpace(job.TenantID)
			lensID := strings.TrimSpace(job.LensID)
			if tenantID != "" && lensID != "" {
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldPath, domainCP)
			}
		}
	} else {
		if err := e.fragmentsRepo.Update(ctx, frag); err != nil {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_fragment_update_failed", true, err)
			if !inlineDomainContractRun {
				if trErr := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_fragment_update_failed"); trErr != nil {
					logGroundingErrorWithContext(job, "scout", "domain_expansion.transition_failed", "job_transition_failed", "state_transition_error", true, trErr)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.transition_failed", "job_transition_failed", "state_transition_error", true)
				}
			}
			return fmt.Errorf("domain fragment update failed: %w", err)
		}
		fragmentID = existingFragment.ID
		// Check if canonical path was changed during update.
		if strings.TrimSpace(frag.CanonicalPath) != "" && strings.TrimSpace(frag.CanonicalPath) != strings.TrimSpace(domainCP) {
			oldPath := domainCP
			domainCP = strings.TrimSpace(frag.CanonicalPath)
			// Best-effort: update lens scope if path changed.
			tenantID := strings.TrimSpace(job.TenantID)
			lensID := strings.TrimSpace(job.LensID)
			if tenantID != "" && lensID != "" {
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldPath, domainCP)
			}
		}
	}
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_fragment_upsert_done")

	// Emit node upsert to UI (use updated domainCP if path changed).
	e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
		"tenantId":        job.TenantID,
		"projectId":       job.ProjectID,
		"lensId":          strings.TrimSpace(job.LensID),
		"fragmentId":      fragmentID,
		"canonicalPath":   domainCP,
		"type":            string(models.FragmentTypeDomain),
		"label":           title,
		"status":          "verified",
		"epistemicStatus": string(frag.Metadata.EpistemicStatus),
		"version":         frag.Version,
	})

	// Synthesize relations for decomposition.
	rels := make([]agents.DiscoveredRelation, 0, len(firstOrder)+len(related)+len(affected)+len(domainResearchRelations))

	// Facet-driven relations (deterministic; best-effort). Skipped for supply chain fast-path.
	if !isSupplyChainFastPath && contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
		// geo facet may have multiple tokens (e.g., bilateral relations)
		if gs, ok := contract.Facets["geo"]; ok && len(gs) > 0 {
			for _, g := range gs {
				g = strings.TrimSpace(strings.ToLower(g))
				if g == "" || g == "global" || g == "int" {
					continue
				}
				emitLoc := func(cc string) {
					cc = strings.TrimSpace(strings.ToLower(cc))
					if len(cc) != 2 {
						return
					}
					loc := "/loc/" + cc
					if err := taxonomy.ValidateCanonicalPath(loc); err == nil {
						rels = append(rels, agents.DiscoveredRelation{
							TargetName:          bestEffortDisplayLabel("", loc),
							TargetType:          string(models.FragmentTypeGeoRegion),
							TargetCanonicalPath: loc,
							RelationType:        string(models.DependencyTypeInGeoScope),
							Reason:              "domain_expansion:facet_geo",
							Confidence:          0.95,
						})
					}
				}
				// Country-level only (deterministic). Support composite tokens like "ae-sa".
				if len(g) == 2 {
					emitLoc(g)
					continue
				}
				if strings.Contains(g, "-") {
					for _, part := range strings.Split(g, "-") {
						emitLoc(part)
					}
				}
			}
		}
		addAboutProducts := func(key string) {
			ps, ok := contract.Facets[key]
			if !ok || len(ps) == 0 {
				return
			}
			for _, raw := range ps {
				lr := strings.TrimSpace(strings.ToLower(raw))
				if lr == "" || lr == "na" || lr == "n/a" || lr == "unknown" || lr == "none" {
					continue
				}
				slug := taxonomy.Slugify(raw)
				if slug == "" || slug == "na" {
					continue
				}
				cp := BuildProductCanonicalPath("", "", "", slug)
				if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
					continue
				}
				rels = append(rels, agents.DiscoveredRelation{
					TargetName:          bestEffortDisplayLabel("", cp),
					TargetType:          string(models.FragmentTypeProduct),
					TargetCanonicalPath: cp,
					RelationType:        string(models.DependencyTypeAbout),
					Reason:              "domain_expansion:facet_" + key,
					Confidence:          0.85,
				})
			}
		}
		for _, k := range []string{"material", "application", "asset", "project", "subject", "topic"} {
			addAboutProducts(k)
		}
	}

	for _, p := range firstOrder {
		ft := InferFragmentTypeFromCanonicalPath(p)
		rt := string(models.DependencyTypeFocusesOn)
		conf := 0.90
		reason := "domain_expansion:first_order_entity"
		if h, ok := researchRelByPath[strings.TrimSpace(p)]; ok {
			if strings.TrimSpace(h.rel) != "" {
				rt = strings.TrimSpace(h.rel)
			}
			if h.conf > 0 {
				conf = h.conf
			}
			if strings.TrimSpace(h.reason) != "" {
				reason = "domain_research:" + strings.TrimSpace(h.reason)
			} else {
				reason = "domain_research:first_order_entity"
			}
		}
		rels = append(rels, agents.DiscoveredRelation{
			TargetName:          bestEffortDisplayLabel("", p),
			TargetType:          string(ft),
			TargetCanonicalPath: p,
			RelationType:        rt,
			Reason:              reason,
			Confidence:          conf,
		})
	}
	for _, p := range related {
		rels = append(rels, agents.DiscoveredRelation{
			TargetName:          bestEffortDisplayLabel("", p),
			TargetType:          string(models.FragmentTypeDomain),
			TargetCanonicalPath: p,
			RelationType:        string(models.DependencyTypeRelatedDomain),
			Reason:              "domain_expansion:related_domain",
			Confidence:          0.50, // keep edge, but avoid recursion breadth
		})
	}
	for _, p := range affected {
		rels = append(rels, agents.DiscoveredRelation{
			TargetName:          bestEffortDisplayLabel("", p),
			TargetType:          string(models.FragmentTypeDomain),
			TargetCanonicalPath: p,
			RelationType:        string(models.DependencyTypeAffects),
			Reason:              "domain_expansion:affected_domain",
			Confidence:          0.50, // keep edge, but avoid recursion breadth
		})
	}
	if len(domainResearchRelations) > 0 {
		// Affected-zone holistic research intentionally bypasses domain-mode relation filtering
		// in ResearchAgent; do not re-apply contract-derived allowlists here.
		if !agents.IsAffectedZoneContractType(contractType) {
			domainResearchRelations = sanitizeDomainResearchRelations(domainCP, contractType, domainResearchRelations)
		}
		domainResearchRelations = dedupeDiscoveredRelations(domainResearchRelations)
		if agents.IsPolicyImpactContractType(contractType) && len(domainResearchRelations) > 0 {
			domainResearchRelations = RegulatoryImpactStrictValidator{}.ValidateRelations(domainResearchRelations)
			cov := ValidateRegulatoryImpactLayerCoverage(domainResearchRelations)
			domainResearchRelations = ApplyRegulatoryLayerOrphanPenalty(domainResearchRelations, cov)
			suff := ComputeRegulatoryGraphSufficiency(domainResearchRelations)
			if job.State == nil {
				job.State = map[string]interface{}{}
			}
			job.State["regulatoryGraphSufficiency"] = map[string]interface{}{
				"tier":        string(suff.Tier),
				"gaps":        suff.Gaps,
				"quantScore":  suff.QuantScore,
				"temporalPct": suff.TemporalPct,
			}
			if tiss := ValidateRegulatoryTemporalConsistency(domainResearchRelations); len(tiss) > 0 {
				msgs := make([]string, 0, len(tiss))
				for _, ti := range tiss {
					msgs = append(msgs, ti.Message)
				}
				job.State["regulatoryTemporalIssues"] = msgs
			}
			mergeWorkingRegulatoryScopeFromRelations(job, domainResearchRelations)
			domainResearchRelations = applyPolicyImpactCriticToRelations(ctx, e, job, frag, domainCP, domainResearchRelations)
			persist := map[string]interface{}{
				"regulatoryGraphSufficiency":   job.State["regulatoryGraphSufficiency"],
				stateKeyWorkingRegulatoryScope: job.State[stateKeyWorkingRegulatoryScope],
			}
			if v, ok := job.State["regulatoryTemporalIssues"]; ok {
				persist["regulatoryTemporalIssues"] = v
			}
			if v, ok := job.State["regulatoryImpactCriticMissingInfo"]; ok {
				persist["regulatoryImpactCriticMissingInfo"] = v
			}
			_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, persist)
		}
		rels = append(rels, domainResearchRelations...)
		zoneN := 0
		for _, r := range domainResearchRelations {
			if strings.HasPrefix(strings.TrimSpace(r.TargetCanonicalPath), "/zone/") || strings.HasPrefix(strings.TrimSpace(r.SourceCanonicalPath), "/zone/") {
				zoneN++
			}
		}
		if zoneN > 0 {
			log.Printf("[ScoutEngine] affected-zone: domain expansion rels include zone relations jobId=%s relsTotal=%d zoneRelatedN=%d", strings.TrimSpace(job.ID), len(domainResearchRelations), zoneN)
		}
	}

	// Persist tracker state into job.State before policy check.
	if tr := tracking.FromContext(ctx); tr != nil {
		job.State = tr.ApplyToState(job.State)
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	// Persist finalized job outputs into LensArtifacts (Phase 2 input).
	e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageArtifactPersistence, true, "artifact_persistence")
	e.persistLensArtifactsFromJobState(ctx, job)
	domainExpansion := map[string]interface{}{
		"canonicalPath":       domainCP,
		"title":               title,
		"firstOrderEntitiesN": len(firstOrder),
		"relatedDomainsN":     len(related),
		"affectedDomainsN":    len(affected),
		"relationsEmittedN":   len(rels),
	}
	job.State["domainExpansion"] = domainExpansion
	if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainExpansion": domainExpansion}); err != nil {
		logGroundingErrorWithContext(job, "scout", "domain_expansion.persist_state", "job_state_update_failed", "repo_update_error", true, err)
		e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.persist_state", "job_state_update_failed", "repo_update_error", true)
		recordPipelineIssue(job, "domain_expansion_state_persist", err.Error())
	}
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageArtifactPersistence, true, "artifact_persistence_done")

	// Decomposition: schedule first-order entity scouts.
	if len(rels) > 0 {
		e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainRelationMaterialize, false, "domain_relation_materialization")
		var resolvedGrounding *ResolvedGroundingContext
		if internalFirstGroundingEnabled() && e.groundingResolver != nil {
			ev := mergeGroundingEvidence(domainGroundingEvidence, groundingEvidenceFromJobState(job))
			if ev != nil {
				resolvedGrounding = e.groundingResolver.Resolve(ctx, ev)
			}
		}
		if resolvedGrounding != nil && !resolvedGrounding.IsEmpty() {
			if err := e.processDiscoveredRelationsWithGrounding(ctx, job, frag, rels, resolvedGrounding); err != nil {
				logGroundingErrorWithContext(job, "scout", "domain_expansion.relations_grounded", "relations_materialization_failed", "materialization_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.relations_grounded", "relations_materialization_failed", "materialization_error", true)
				e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainRelationMaterialize, false, "domain_relation_materialization_failed", true, err)
				recordPipelineIssue(job, "domain_relation_materialization", err.Error())
			} else {
				e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainRelationMaterialize, false, "domain_relation_materialization_done")
			}
		} else {
			if err := e.processDiscoveredRelations(ctx, job, frag, rels); err != nil {
				logGroundingErrorWithContext(job, "scout", "domain_expansion.relations_fallback", "relations_materialization_failed", "materialization_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.relations_fallback", "relations_materialization_failed", "materialization_error", true)
				e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainRelationMaterialize, false, "domain_relation_materialization_failed", true, err)
				recordPipelineIssue(job, "domain_relation_materialization", err.Error())
			} else {
				e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainRelationMaterialize, false, "domain_relation_materialization_done")
			}
		}
	} else {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageDomainRelationMaterialize, false, "domain_relation_materialization_skip_no_relations")
	}
	refineContinuation := isSupplyChainRefineContinuationJob(job)
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	if fastFirstSupplyChain && !refineContinuation {
		job.State["supplyChainSkipFamilyCoResearch"] = true
		job.State["supplyChainSkipChildScheduling"] = true
	} else {
		delete(job.State, "supplyChainSkipFamilyCoResearch")
		delete(job.State, "supplyChainSkipChildScheduling")
	}
	if len(domainResearchChains) > 0 {
		e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainSupplyChains, false, "domain_supply_chain_materialization")
		if parseStateBool(job.State["supplyChainIncrementalMaterialized"], false) {
			e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainSupplyChains, false, "domain_supply_chain_materialization_done_incremental")
		} else {
			if err := e.processSupplyChains(ctx, job, frag, domainResearchChains); err != nil {
				logGroundingErrorWithContext(job, "scout", "domain_expansion.supply_chains", "supply_chain_processing_failed", "materialization_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.supply_chains", "supply_chain_processing_failed", "materialization_error", true)
				e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainSupplyChains, false, "domain_supply_chain_materialization_failed", true, err)
				recordPipelineIssue(job, "domain_supply_chain_materialization", err.Error())
			} else {
				e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainSupplyChains, false, "domain_supply_chain_materialization_done")
			}
		}
		// Expand phase (optional): 1-hop for low-confidence nodes, alternatives check. Config: job.State["supplyChainExpandEnabled"], ["supplyChainExpandMaxNodes"].
		if usePlanMode && !hotPathOnlySupplyChain && (!fastFirstSupplyChain || refineContinuation) {
			e.runSupplyChainExpandPhase(ctx, job, frag, domainResearchChains, supplyChainPlan)
		}
		// Stage 2 Overlay queries: skipped for supply chain fast-path (pure org→fac→product→fac→org).
		if usePlanMode && !hotPathOnlySupplyChain && (!fastFirstSupplyChain || refineContinuation) && !isSupplyChainFastPath && supplyChainPlan != nil && len(supplyChainPlan.OverlayQueries) > 0 {
			e.runSupplyChainStage2Overlays(ctx, job, frag, domainResearchChains, supplyChainPlan.OverlayQueries)
		}
	} else {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageDomainSupplyChains, false, "domain_supply_chain_materialization_skip_no_chains")
	}

	if agents.IsSupplyChainContractType(contractType) {
		switch {
		case fastFirstSupplyChain && !refineContinuation:
			if err := e.runSupplyChainFastAssembly(ctx, job, frag, domainCP, supplyChainPlan); err != nil {
				recordPipelineIssue(job, "supply_chain_fast_assembly", err.Error())
				return err
			}
		case hotPathOnlySupplyChain:
			e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageEntitySupplyChainRefine, true, "supply_chain_refine_skip_hot_path_only")
		default:
			if err := e.runSupplyChainAssemblyRefine(ctx, job, frag, domainCP, supplyChainPlan); err != nil {
				recordPipelineIssue(job, "supply_chain_refine", err.Error())
				return err
			}
		}
	}

	// Domain artifact materialization: build relations graph for governance/regulatory/ecosystem. Skipped for supply chain fast-path.
	if !isSupplyChainFastPath && e.domainArtifactBuilder != nil {
		domainModes := taxonomy.NormalizeDomainModes(getJobDomainModes(job))
		for _, dm := range domainModes {
			if dm == taxonomy.DomainModeGovernance || dm == taxonomy.DomainModePolicyImpact || dm == taxonomy.DomainModeEcosystem {
				rootPath := ""
				if dm == taxonomy.DomainModePolicyImpact {
					rootPath = policyImpactMaterializationRoot(domainCP, firstOrder, getRegulatoryImpactPlanFromJobState(job, domainCP), rels)
				}
				if rootPath == "" {
					if frag != nil {
						rootPath = strings.TrimSpace(frag.CanonicalPath)
					}
				}
				if rootPath == "" || strings.HasPrefix(rootPath, "/domain/") {
					for _, fo := range firstOrder {
						if p := strings.TrimSpace(fo); p != "" && !strings.HasPrefix(p, "/domain/") {
							rootPath = p
							break
						}
					}
				}
				if rootPath != "" && !strings.HasPrefix(rootPath, "/domain/") {
					policyImpactArtifactStage := dm == taxonomy.DomainModePolicyImpact
					if policyImpactArtifactStage {
						e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainPolicyImpactArtifact, false, "domain_policy_impact_artifact")
					}
					artifact, matErr := e.domainArtifactBuilder.MaterializeRelationsGraph(
						ctx,
						strings.TrimSpace(job.TenantID),
						strings.TrimSpace(job.ProjectID),
						strings.TrimSpace(job.LensID),
						rootPath, dm, contractType, title,
					)
					if policyImpactArtifactStage {
						switch {
						case matErr != nil:
							fmt.Printf("[DomainArtifact] materialization failed: jobId=%s mode=%s root=%s err=%v\n",
								strings.TrimSpace(job.ID), dm, rootPath, matErr)
							e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainPolicyImpactArtifact, false, "domain_policy_impact_artifact_failed", true, matErr)
						case artifact == nil:
							e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageDomainPolicyImpactArtifact, false, "domain_policy_impact_artifact_skip_empty_graph")
						default:
							e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainPolicyImpactArtifact, false, "domain_policy_impact_artifact_done")
							fmt.Printf("[DomainArtifact] materialized: jobId=%s mode=%s kind=%s key=%s nodes=%d\n",
								strings.TrimSpace(job.ID), dm, string(artifact.Kind), artifact.CanonicalKey, len(artifact.NodeRefs))
							piRels := domainResearchRelations
							if len(piRels) == 0 {
								piRels = filterPolicyImpactRelations(rels)
							}
							if len(piRels) > 0 {
								paths := regstress.BuildPathsFromRelations(piRels, domainCP, 5)
								if err := e.domainArtifactBuilder.UpsertRegulatoryTransmissionArtifact(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(job.ProjectID), strings.TrimSpace(job.LensID), rootPath, paths); err != nil {
									e.logLine(ctx, job, "warn", fmt.Sprintf("> regulatory transmission artifact: %v", err))
								}
								if err := e.domainArtifactBuilder.UpsertRegulatoryScenarioArtifact(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(job.ProjectID), strings.TrimSpace(job.LensID), rootPath, paths, regulatoryWhatIfScenariosFromJob(job)); err != nil {
									e.logLine(ctx, job, "warn", fmt.Sprintf("> regulatory scenario artifact: %v", err))
								}
								if err := e.domainArtifactBuilder.UpsertRegulatoryTimelineArtifact(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(job.ProjectID), strings.TrimSpace(job.LensID), rootPath, piRels); err != nil {
									e.logLine(ctx, job, "warn", fmt.Sprintf("> regulatory timeline artifact: %v", err))
								}
								if err := e.domainArtifactBuilder.UpsertRegulatoryCascadeArtifact(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(job.ProjectID), strings.TrimSpace(job.LensID), rootPath, regulatoryCascadeDirectAffectedPaths(piRels)); err != nil {
									e.logLine(ctx, job, "warn", fmt.Sprintf("> regulatory cascade artifact: %v", err))
								}
							}
						}
					} else if matErr != nil {
						fmt.Printf("[DomainArtifact] materialization failed: jobId=%s mode=%s root=%s err=%v\n",
							strings.TrimSpace(job.ID), dm, rootPath, matErr)
					} else if artifact != nil {
						fmt.Printf("[DomainArtifact] materialized: jobId=%s mode=%s kind=%s key=%s nodes=%d\n",
							strings.TrimSpace(job.ID), dm, string(artifact.Kind), artifact.CanonicalKey, len(artifact.NodeRefs))
					}
					if artifact != nil {
						if lensID := strings.TrimSpace(job.LensID); lensID != "" && artifact.CanonicalKey != "" {
							scopePath := fmt.Sprintf("/artifact/%s", artifact.CanonicalKey)
							e.bestEffortDeferredScopeAddForJob(ctx, job, lensID, scopePath)
						}
						if dm == taxonomy.DomainModeGovernance {
							recordGovernanceArtifactState(job, artifact, contractType)
						}
					}
				} else if dm == taxonomy.DomainModePolicyImpact {
					e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageDomainPolicyImpactArtifact, false, "domain_policy_impact_artifact_skip_no_root")
				}
			}
		}
	}

	// Policy audit + optional enforcement (best-effort). Skipped for supply chain fast-path.
	if isSupplyChainFastPath {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_supply_chain_fast_path_skip")
	} else if !inlineDomainContractRun {
		e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_dispatch")
		if err := e.enqueuePolicyAuditOutboxIntent(ctx, job, pipelineRuntimeName); err != nil {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_dispatch_failed", true, err)
			recordPipelineIssue(job, "domain_policy_audit_dispatch", err.Error())
		} else {
			e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_dispatched")
		}
	} else {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_inline_skip")
	}

	// Best-effort propagation trigger for the domain node. Supply chain fast-path skips unless chains were materialized (domain metrics may still need propagation).
	shouldTriggerDomainPropagation := e.propagationTrigger != nil && (!isSupplyChainFastPath || len(domainResearchChains) > 0)
	if shouldTriggerDomainPropagation {
		rc, _ := rtctx.Get(ctx)
		fid := fragmentID
		runAsyncSafe("domain_node_propagation", func() {
			propBase := context.Background()
			if strings.TrimSpace(rc.TenantID) != "" || strings.TrimSpace(rc.ProjectID) != "" || strings.TrimSpace(rc.LensID) != "" || strings.TrimSpace(rc.CorrelationID) != "" || strings.TrimSpace(rc.SessionID) != "" {
				propBase = rtctx.With(propBase, rc)
			}
			propCtx, cancel := context.WithTimeout(propBase, 2*time.Minute)
			defer cancel()
			if err := e.propagationTrigger(propCtx, fid); err != nil {
				logGroundingErrorWithContext(job, "scout", "domain_expansion.propagation", "propagation_trigger_failed", "propagation_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.propagation", "propagation_trigger_failed", "propagation_error", true)
			}
		})
	}

	e.publish(ctx, realtime.EventType("horizon.domain.expansion.completed"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"counters": map[string]interface{}{
			"firstOrderEntities": len(firstOrder),
			"relatedDomains":     len(related),
			"affectedDomains":    len(affected),
			"relations":          len(rels),
		},
		"humanReadable": map[string]interface{}{
			"title":       "Domain expansion complete",
			"description": fmt.Sprintf("%s • %d entities", title, len(firstOrder)),
			"entityName":  title,
		},
	})
	// New event namespace (preferred): base enrichment pass.
	e.publish(ctx, realtime.EventType("horizon.domain.base_enrichment.completed"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"counters": map[string]interface{}{
			"firstOrderEntities": len(firstOrder),
			"relatedDomains":     len(related),
			"affectedDomains":    len(affected),
			"relations":          len(rels),
		},
		"humanReadable": map[string]interface{}{
			"title":       "Base enrichment complete",
			"description": fmt.Sprintf("%s • %d entities", title, len(firstOrder)),
			"entityName":  title,
		},
	})

	// Phase 2: Strategic Layer Overlay — automatically triggered after Phase 1 completes.
	// Only run if there are entities/relations to analyze and the domain benefits from strategic analysis.
	skipAutoStrategicOverlay := false
	if job.State != nil {
		if v, ok := job.State["skipAutoStrategicOverlay"]; ok {
			skipAutoStrategicOverlay = parseStateBool(v, false)
		}
	}
	if !skipAutoStrategicOverlay && e.shouldRunStrategicOverlay(contractType, len(firstOrder), len(rels), collectAllEntityPaths(firstOrder, rels)) {
		e.triggerPhase2StrategicOverlay(ctx, job, domainCP)
	}

	return nil
}

func (e *ScoutEngine) executeDomainStrategicOverlayJob(
	ctx context.Context,
	job *models.ScoutingJob,
	effectiveCanonicalPath string,
	existingFragment *models.HorizonFragment,
) error {
	if e == nil || job == nil {
		return nil
	}
	domainCP := strings.TrimSpace(effectiveCanonicalPath)
	if domainCP == "" {
		domainCP = strings.TrimSpace(job.TargetCanonicalPath)
	}
	domainCP = NormalizeRuntimeDomainCanonicalPath(domainCP)
	if domainCP == "" || !strings.HasPrefix(domainCP, "/domain/") {
		return nil
	}
	pipelineRuntimeName := pipelineRuntimeDomainLayer
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stagePlanning, true, "domain_overlay_planning_done")

	inlineDomainContractRun := false
	if job.State != nil {
		if v, ok := job.State["inlineDomainContractRun"]; ok {
			inlineDomainContractRun = parseStateBool(v, false)
		}
	}
	if !inlineDomainContractRun {
		if err := e.transition(ctx, job, models.ScoutingJobStateExecution, 0.4, "domain_overlay"); err != nil {
			logGroundingErrorWithContext(job, "scout", "domain_overlay.transition_execution", "job_transition_failed", "state_transition_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.transition_execution", "job_transition_failed", "state_transition_error", true)
		}
	}

	contract := e.bestEffortFindDomainContract(job, domainCP)
	title := ""
	desc := ""
	contractType := ""
	if contract != nil {
		title = strings.TrimSpace(contract.Title)
		desc = strings.TrimSpace(contract.Description)
		contractType = strings.TrimSpace(contract.ContractType)
	}
	parsedCT, parsedFacets := parseDomainPathFacets(domainCP)
	if contractType == "" {
		contractType = strings.TrimSpace(parsedCT)
	}
	facetsForTitle := parsedFacets
	if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
		facetsForTitle = contract.Facets
	}
	autoTitle := buildDomainTitle(contractType, domainCP, facetsForTitle)
	if focusTitle := bestEffortDomainTitleFromJobState(job); focusTitle != "" {
		if strings.TrimSpace(title) == "" || strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(autoTitle)) {
			title = focusTitle
		}
	}
	if title == "" {
		title = autoTitle
	}
	if desc == "" {
		desc = "Executable knowledge contract for strategic overlay."
	}

	// Create/update DOMAIN fragment (contract node).
	frag := existingFragment
	if frag == nil {
		frag = &models.HorizonFragment{
			CanonicalPath: domainCP,
			Type:          models.FragmentTypeDomain,
			Status:        string(models.FragmentStatusActive),
			State:         map[string]models.MetricValue{},
			Governance: models.Governance{
				Owner:  "SYSTEM",
				Status: "ACTIVE",
			},
		}
	}
	if strings.TrimSpace(frag.Governance.Owner) == "" {
		frag.Governance.Owner = "SYSTEM"
	}
	if strings.TrimSpace(frag.Governance.Status) == "" {
		frag.Governance.Status = "ACTIVE"
	}
	if frag.Labels == nil {
		frag.Labels = map[string]string{}
	}
	if strings.TrimSpace(title) != "" {
		frag.Labels["en"] = title
	}
	if strings.TrimSpace(desc) != "" {
		frag.Description = desc
	}
	frag.Type = models.FragmentTypeDomain
	if frag.Identifiers == nil {
		frag.Identifiers = map[string]string{}
	}
	if contractType != "" {
		frag.Identifiers["domainContractType"] = contractType
	}
	if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
		if b, err := json.Marshal(contract.Facets); err == nil && len(b) > 0 && len(b) < 8000 {
			frag.Identifiers["domainFacetsJson"] = string(b)
		}
	}

	var fragmentID string
	e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_fragment_upsert")
	if existingFragment == nil || strings.TrimSpace(existingFragment.ID) == "" {
		id, err := e.fragmentsRepo.Create(ctx, frag)
		if err != nil {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_overlay_fragment_create_failed", true, err)
			if !inlineDomainContractRun {
				if trErr := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_fragment_create_failed"); trErr != nil {
					logGroundingErrorWithContext(job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true, trErr)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true)
				}
			}
			return fmt.Errorf("domain fragment create failed: %w", err)
		}
		fragmentID = id
		if strings.TrimSpace(frag.CanonicalPath) != "" && strings.TrimSpace(frag.CanonicalPath) != strings.TrimSpace(domainCP) {
			oldPath := domainCP
			domainCP = strings.TrimSpace(frag.CanonicalPath)
			tenantID := strings.TrimSpace(job.TenantID)
			lensID := strings.TrimSpace(job.LensID)
			if tenantID != "" && lensID != "" {
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldPath, domainCP)
			}
		}
	} else {
		if err := e.fragmentsRepo.Update(ctx, frag); err != nil {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_overlay_fragment_update_failed", true, err)
			if !inlineDomainContractRun {
				if trErr := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_fragment_update_failed"); trErr != nil {
					logGroundingErrorWithContext(job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true, trErr)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true)
				}
			}
			return fmt.Errorf("domain fragment update failed: %w", err)
		}
		fragmentID = existingFragment.ID
		if strings.TrimSpace(frag.CanonicalPath) != "" && strings.TrimSpace(frag.CanonicalPath) != strings.TrimSpace(domainCP) {
			oldPath := domainCP
			domainCP = strings.TrimSpace(frag.CanonicalPath)
			tenantID := strings.TrimSpace(job.TenantID)
			lensID := strings.TrimSpace(job.LensID)
			if tenantID != "" && lensID != "" {
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldPath, domainCP)
			}
		}
	}
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainFragmentUpsert, true, "domain_fragment_upsert_done")

	e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
		"tenantId":        job.TenantID,
		"projectId":       job.ProjectID,
		"lensId":          strings.TrimSpace(job.LensID),
		"fragmentId":      fragmentID,
		"canonicalPath":   domainCP,
		"type":            string(models.FragmentTypeDomain),
		"label":           title,
		"status":          "verified",
		"epistemicStatus": string(frag.Metadata.EpistemicStatus),
		"version":         frag.Version,
	})

	e.publish(ctx, realtime.EventType("horizon.domain.overlay.started"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"humanReadable": map[string]interface{}{
			"title":       "Strategic overlay started",
			"description": fmt.Sprintf("%s • %s", title, domainCP),
			"entityName":  title,
		},
	})
	// New event namespace (preferred): strategic projection pass.
	e.publish(ctx, realtime.EventType("horizon.domain.strategic_projection.started"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"humanReadable": map[string]interface{}{
			"title":       "Strategic projection started",
			"description": fmt.Sprintf("%s • %s", title, domainCP),
			"entityName":  title,
		},
	})

	internalFirstOverlayContext := ""
	if internalFirstGroundingEnabled() {
		if e.planner != nil && e.executor != nil {
			sourcePlan, err := e.planner.PlanSources(ctx, strings.TrimSpace(job.TenantID), strings.TrimSpace(job.ProjectID), domainCP, models.ScoutingStrategySnapshot, nil, "")
			if err != nil {
				logGroundingWarnf("domain overlay PlanSources failed: jobId=%s target=%s err=%v", strings.TrimSpace(job.ID), strings.TrimSpace(domainCP), err)
			} else if sourcePlan != nil {
				for _, src := range sourcePlan.Sources {
					if src == nil {
						continue
					}
					res, execErr := e.executor.ExecuteDataSource(ctx, src)
					if execErr != nil {
						logGroundingWarnf("domain overlay source execution failed: jobId=%s sourceId=%s kind=%s err=%v",
							strings.TrimSpace(job.ID), strings.TrimSpace(src.ID), strings.TrimSpace(src.Kind), execErr)
						continue
					}
					if s := strings.TrimSpace(res.Content); s != "" {
						internalFirstOverlayContext += s + "\n\n"
					}
				}
			}
		}
		if gctx := e.buildGraphBackedInternalContext(ctx, strings.TrimSpace(job.TenantID), domainCP); strings.TrimSpace(gctx) != "" {
			if internalFirstOverlayContext != "" {
				internalFirstOverlayContext += "\n\n"
			}
			internalFirstOverlayContext += gctx
		}
	}

	// Build graph context for strategic overlay.
	// Prefer LensArtifacts (finalized entities from jobs) over lens.Scope (just paths).
	var graphContext string
	lensID := strings.TrimSpace(job.LensID)
	if e.lensArtifactsRepo != nil && lensID != "" {
		graphContext = e.buildLensArtifactsContext(ctx, lensID, 50, 150)
	}
	// Fallback to lens.Scope if LensArtifacts is empty or not available
	if graphContext == "" {
		var lens *models.Lens
		if e.lensesRepo != nil {
			if l, err := e.lensesRepo.Get(ctx, lensID); err == nil {
				lens = l
			}
		}
		graphContext = e.buildLensGraphContext(ctx, lens, 40, 120)
	}
	if strings.TrimSpace(internalFirstOverlayContext) != "" {
		if strings.TrimSpace(graphContext) == "" {
			graphContext = strings.TrimSpace(internalFirstOverlayContext)
		} else {
			graphContext = strings.TrimSpace(graphContext) + "\n\nINTERNAL-FIRST CONTEXT:\n" + strings.TrimSpace(internalFirstOverlayContext)
		}
	}

	focus := ""
	keywords := []string(nil)
	if job.State != nil {
		if v, ok := job.State["focus"].(string); ok {
			focus = strings.TrimSpace(v)
		}
		if v, ok := job.State["keywords"]; ok {
			keywords = parseStateStringSlice(v)
		}
	}

	overlayAgent := agents.NewStrategicOverlayAgent(e.aiExecutor)
	e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainOverlayExtraction, true, "domain_overlay_extraction")
	overlayResult, overlayEvidence, err := overlayAgent.ExtractWithGrounding(ctx, frag, graphContext, focus, keywords)
	if err != nil {
		e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainOverlayExtraction, true, "domain_overlay_extraction_failed", true, err)
		if !inlineDomainContractRun {
			if trErr := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_failed"); trErr != nil {
				logGroundingErrorWithContext(job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true, trErr)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true)
			}
		}
		return err
	}
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainOverlayExtraction, true, "domain_overlay_extraction_done")
	if internalFirstGroundingEnabled() && overlayEvidence != nil {
		applyGroundingEvidenceToJobState(job, overlayEvidence, nil, nil)
		logGroundingInfof("domain overlay extraction grounding: jobId=%s target=%s datastore=%v(%d) web=%v(%d)",
			strings.TrimSpace(job.ID),
			strings.TrimSpace(domainCP),
			overlayEvidence.HasDatastoreResults,
			len(overlayEvidence.DatastoreFragmentIDs),
			overlayEvidence.HasWebResults,
			len(overlayEvidence.WebSources),
		)
	}

	decisionCount := 0
	mechanismCount := 0
	edgesEmitted := 0
	overlayScopePaths := make([]string, 0, 32)
	overlayFragments := map[string]*models.HorizonFragment{}

	for _, dp := range overlayResult.DecisionPoints {
		cp := strings.TrimSpace(dp.CanonicalPath)
		if cp == "" || taxonomy.ValidateCanonicalPath(cp) != nil {
			continue
		}
		f, err := e.getOrCreateFragmentByPath(ctx, cp, models.FragmentTypeDecisionPoint, dp.Title, dp.Description, "strategic_overlay", job.ID)
		if err == nil && f != nil {
			if f.Type != models.FragmentTypeDecisionPoint {
				f.Type = models.FragmentTypeDecisionPoint
				if err := e.fragmentsRepo.Update(ctx, f); err != nil {
					logGroundingErrorWithContext(job, "scout", "domain_overlay.update_fragment_type", "fragment_update_failed", "repo_update_error", true, err)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.update_fragment_type", "fragment_update_failed", "repo_update_error", true)
				}
			}
			decisionCount++
			overlayScopePaths = append(overlayScopePaths, cp)
			overlayFragments[cp] = f

			// Realtime: emit node upsert for newly surfaced strategic entities.
			if strings.TrimSpace(f.ID) != "" {
				label := strings.TrimSpace("")
				if f.Labels != nil {
					label = strings.TrimSpace(f.Labels["en"])
				}
				if label == "" {
					label = strings.TrimSpace(dp.Title)
				}
				e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
					"tenantId":        job.TenantID,
					"projectId":       job.ProjectID,
					"lensId":          strings.TrimSpace(job.LensID),
					"fragmentId":      strings.TrimSpace(f.ID),
					"canonicalPath":   cp,
					"type":            string(models.FragmentTypeDecisionPoint),
					"label":           label,
					"status":          "verified",
					"epistemicStatus": string(f.Metadata.EpistemicStatus),
					"version":         f.Version,
				})
			}
		}
	}
	for _, mech := range overlayResult.Mechanisms {
		cp := strings.TrimSpace(mech.CanonicalPath)
		if fixed, changed := normalizeStrategicMechanismPath(cp, mech.Title, mech.Description); changed {
			cp = fixed
		}
		if cp == "" || taxonomy.ValidateCanonicalPath(cp) != nil {
			continue
		}
		f, err := e.getOrCreateFragmentByPath(ctx, cp, models.FragmentTypeMechanism, mech.Title, mech.Description, "strategic_overlay", job.ID)
		if err == nil && f != nil {
			if f.Type != models.FragmentTypeMechanism {
				f.Type = models.FragmentTypeMechanism
				if err := e.fragmentsRepo.Update(ctx, f); err != nil {
					logGroundingErrorWithContext(job, "scout", "domain_overlay.update_fragment_type", "fragment_update_failed", "repo_update_error", true, err)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.update_fragment_type", "fragment_update_failed", "repo_update_error", true)
				}
			}
			mechanismCount++
			overlayScopePaths = append(overlayScopePaths, cp)
			overlayFragments[cp] = f

			// Realtime: emit node upsert for newly surfaced strategic entities.
			if strings.TrimSpace(f.ID) != "" {
				label := strings.TrimSpace("")
				if f.Labels != nil {
					label = strings.TrimSpace(f.Labels["en"])
				}
				if label == "" {
					label = strings.TrimSpace(mech.Title)
				}
				e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
					"tenantId":        job.TenantID,
					"projectId":       job.ProjectID,
					"lensId":          strings.TrimSpace(job.LensID),
					"fragmentId":      strings.TrimSpace(f.ID),
					"canonicalPath":   cp,
					"type":            string(models.FragmentTypeMechanism),
					"label":           label,
					"status":          "verified",
					"epistemicStatus": string(f.Metadata.EpistemicStatus),
					"version":         f.Version,
				})
			}
		}
	}

	// Create SURFACES edges from domain to each decision point and mechanism.
	// This anchors strategic entities to the domain that surfaced them.
	surfacesCount := 0
	for _, path := range overlayScopePaths {
		targetFrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err != nil || targetFrag == nil {
			continue
		}
		// Check if edge already exists
		existing, _ := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", fragmentID, targetFrag.ID, models.DependencyTypeSurfaces)
		if existing != nil {
			continue
		}
		surfaceEdge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: fragmentID,
			TargetFragmentID: targetFrag.ID,
			Type:             models.DependencyTypeSurfaces,
			Properties: models.EdgeProperties{
				Criticality:   0.8,
				Amplification: 1.0,
			},
			Provenance: models.Provenance{
				SourceIDs: []string{job.ID},
				Method:    "strategic_overlay",
			},
			Metadata: models.EdgeMetadata{
				SourceFrameID: job.ID,
				CreatedAt:     time.Now(),
				LastUpdatedAt: time.Now(),
			},
		}
		if edgeID, err := e.edgesRepo.Create(ctx, surfaceEdge); err == nil {
			surfaceEdge.ID = edgeID
			surfacesCount++
			// Realtime: update graph edges (unified payload with all sources)
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":            job.TenantID,
				"projectId":           job.ProjectID,
				"lensId":              strings.TrimSpace(job.LensID),
				"edgeId":              surfaceEdge.ID,
				"sourceFragmentId":    surfaceEdge.SourceFragmentID,
				"targetFragmentId":    surfaceEdge.TargetFragmentID,
				"sourceCanonicalPath": domainCP,
				"targetCanonicalPath": path,
				"sourceLabel":         strings.TrimSpace(frag.Labels["en"]),
				"targetLabel":         strings.TrimSpace(targetFrag.Labels["en"]),
				"type":                string(surfaceEdge.Type),
				"properties":          surfaceEdge.Properties,
				"qualifiers":          surfaceEdge.Qualifiers,
			})
			e.publish(ctx, realtime.EventType("horizon.edge.created"), map[string]interface{}{
				"tenantId":   job.TenantID,
				"projectId":  job.ProjectID,
				"lensId":     strings.TrimSpace(job.LensID),
				"jobId":      job.ID,
				"edgeType":   "SURFACES",
				"sourcePath": domainCP,
				"targetPath": path,
				"humanReadable": map[string]interface{}{
					"title":       "Link Surfaces created",
					"description": fmt.Sprintf("%s → %s", domainCP, path),
				},
			})
		}
	}

	// IMPORTANT:
	// Do NOT append strategic overlay nodes (DECISION_POINT / MECHANISM) to lens.Scope.
	// Scope is meant to stay anchored on domains/subdomains (and user-selected anchors), otherwise the UI becomes noisy.
	// The Cockpit loader should include strategic nodes via graph expansion (neighbors), not by inflating scope.
	e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageCritic, true, "domain_overlay_critic_skip")
	e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageResearch, true, "domain_overlay_research_skip")
	e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageRevalidation, true, "domain_overlay_revalidation_skip")

	relationsBySource := map[string][]agents.DiscoveredRelation{}
	sourceTypeByPath := map[string]models.FragmentType{}
	for _, ed := range overlayResult.Edges {
		src := strings.TrimSpace(ed.SourceCanonicalPath)
		tgt := strings.TrimSpace(ed.TargetCanonicalPath)
		if src == "" || tgt == "" {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(src); err != nil {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(tgt); err != nil {
			continue
		}
		relType := strings.TrimSpace(taxonomy.NormalizeRelationType(ed.RelationType))
		if relType == "" {
			continue
		}
		// Strategic overlay guardrails: fix or drop invalid endpoint combinations early.
		if relType == string(models.DependencyTypeUsesMechanism) {
			if !strings.HasPrefix(tgt, "/mechanism/") {
				// If LLM linked to a decision point, reinterpret as GATEKEEPS_DECISION.
				if strings.HasPrefix(tgt, "/decision/") {
					relType = string(models.DependencyTypeGatekeepsDecision)
				} else {
					continue
				}
			}
		}
		if relType == string(models.DependencyTypeGatekeepsDecision) {
			if !strings.HasPrefix(tgt, "/decision/") {
				continue
			}
		}
		if relType == string(models.DependencyTypeAppliesTo) {
			if strings.HasPrefix(tgt, "/decision/") || strings.HasPrefix(tgt, "/mechanism/") {
				continue
			}
		}
		targetType := strings.TrimSpace(ed.TargetType)
		if targetType == "" {
			targetType = string(InferFragmentTypeFromCanonicalPath(tgt))
		}
		if st := strings.TrimSpace(ed.SourceType); st != "" {
			if ft := e.normalizeFragmentType(ctx, st); ft != "" {
				sourceTypeByPath[src] = ft
			}
		} else if _, ok := sourceTypeByPath[src]; !ok {
			sourceTypeByPath[src] = InferFragmentTypeFromCanonicalPath(src)
		}
		rel := claimFromStrategicEdge(
			src,
			strings.TrimSpace(ed.SourceType),
			tgt,
			targetType,
			relType,
			strings.TrimSpace(ed.Reason),
			ed.Confidence,
			ed.Qualifiers,
		)
		relationsBySource[src] = append(relationsBySource[src], rel)
	}
	if len(relationsBySource) > 0 {
		e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageDomainOverlayRelations, false, "domain_overlay_relation_materialization")
		overlayRelationErr := false
		for sourcePath, rels := range relationsBySource {
			ft := sourceTypeByPath[sourcePath]
			sourceFrag, err := e.getOrCreateFragmentByPath(ctx, sourcePath, ft, "", "", "strategic_overlay", job.ID)
			if err != nil || sourceFrag == nil {
				overlayRelationErr = true
				if err != nil {
					recordPipelineIssue(job, "domain_overlay_relation_materialization", err.Error())
				}
				continue
			}
			if err := e.processRelationsInternal(ctx, job, sourceFrag, rels, "strategic_overlay", map[string]interface{}{"source": "strategic_overlay", "domainOverlay": true}, nil); err == nil {
				edgesEmitted += len(rels)
				continue
			} else {
				overlayRelationErr = true
				recordPipelineIssue(job, "domain_overlay_relation_materialization", err.Error())
			}
		}
		if overlayRelationErr {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stageDomainOverlayRelations, false, "domain_overlay_relation_materialization_failed", true, fmt.Errorf("overlay relation materialization had failures"))
		} else {
			e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageDomainOverlayRelations, false, "domain_overlay_relation_materialization_done")
		}
	} else {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stageDomainOverlayRelations, false, "domain_overlay_relation_materialization_skip_no_relations")
	}

	// Phase 2 critic + research enrichment (best-effort).
	if e.criticAgent != nil && e.researchAgent != nil && len(overlayFragments) > 0 {
		enrichedCount, addedEdges := e.runStrategicOverlayCritic(ctx, job, graphContext, overlayFragments, relationsBySource)
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		job.State["domainOverlayCritic"] = map[string]interface{}{
			"enriched":   enrichedCount,
			"edgesAdded": addedEdges,
		}
	}

	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	// Persist tracker state into job.State before policy check / artifact persistence.
	if tr := tracking.FromContext(ctx); tr != nil {
		job.State = tr.ApplyToState(job.State)
	}
	// Persist finalized job outputs into LensArtifacts (Phase 2 input).
	e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stageArtifactPersistence, true, "artifact_persistence")
	e.persistLensArtifactsFromJobState(ctx, job)
	domainOverlay := map[string]interface{}{
		"canonicalPath":    domainCP,
		"title":            title,
		"decisions":        decisionCount,
		"mechanisms":       mechanismCount,
		"surfacesEdges":    surfacesCount,
		"relationsEmitted": edgesEmitted,
	}
	job.State["domainOverlay"] = domainOverlay
	if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{"domainOverlay": domainOverlay}); err != nil {
		logGroundingErrorWithContext(job, "scout", "domain_overlay.persist_state", "job_state_update_failed", "repo_update_error", true, err)
		e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.persist_state", "job_state_update_failed", "repo_update_error", true)
		recordPipelineIssue(job, "domain_overlay_state_persist", err.Error())
	}

	// Mark all lens artifacts as processed for strategic overlay
	if e.lensArtifactsRepo != nil && lensID != "" {
		if arts, err := e.lensArtifactsRepo.ListUnprocessedForStrategic(ctx, lensID, 2000); err == nil && len(arts) > 0 {
			artIDs := make([]string, 0, len(arts))
			for _, a := range arts {
				if a != nil && strings.TrimSpace(a.ID) != "" {
					artIDs = append(artIDs, strings.TrimSpace(a.ID))
				}
			}
			if len(artIDs) > 0 {
				if err := e.lensArtifactsRepo.MarkProcessedForStrategic(ctx, artIDs); err != nil {
					logGroundingErrorWithContext(job, "scout", "domain_overlay.mark_artifacts_processed", "artifact_mark_processed_failed", "repo_update_error", true, err)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.mark_artifacts_processed", "artifact_mark_processed_failed", "repo_update_error", true)
					recordPipelineIssue(job, "domain_overlay_mark_artifacts_processed", err.Error())
				}
			}
		}
	}
	e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stageArtifactPersistence, true, "artifact_persistence_done")
	if !inlineDomainContractRun {
		e.pipelineStageBegin(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_dispatch")
		if err := e.enqueuePolicyAuditOutboxIntent(ctx, job, pipelineRuntimeName); err != nil {
			e.pipelineStageFail(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_dispatch_failed", true, err)
			recordPipelineIssue(job, "domain_overlay_policy_audit_dispatch", err.Error())
		} else {
			e.pipelineStageComplete(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_dispatched")
		}
	} else {
		e.pipelineStageSkip(ctx, job, pipelineRuntimeName, stagePolicyAudit, true, "policy_audit_inline_skip")
	}
	e.publish(ctx, realtime.EventType("horizon.domain.overlay.completed"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"counters": map[string]interface{}{
			"decisionPoints": decisionCount,
			"mechanisms":     mechanismCount,
			"relations":      edgesEmitted,
		},
		"humanReadable": map[string]interface{}{
			"title":       "Strategic overlay complete",
			"description": fmt.Sprintf("%s • %d decisions", title, decisionCount),
			"entityName":  title,
		},
	})
	// New event namespace (preferred): strategic projection pass.
	e.publish(ctx, realtime.EventType("horizon.domain.strategic_projection.completed"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": domainCP,
		"counters": map[string]interface{}{
			"decisionPoints": decisionCount,
			"mechanisms":     mechanismCount,
			"relations":      edgesEmitted,
		},
		"humanReadable": map[string]interface{}{
			"title":       "Strategic projection complete",
			"description": fmt.Sprintf("%s • %d decisions", title, decisionCount),
			"entityName":  title,
		},
	})
	return nil
}

func (e *ScoutEngine) processRelationsInternal(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
	provenanceMethod string,
	baseQualifiers map[string]interface{},
	groundingCtx *ResolvedGroundingContext,
) error {
	if len(relations) == 0 {
		e.logLine(ctx, job, "info", "> relation_child_scheduling_skip: no relations from extraction+research")
		return nil
	}

	// PostProcessingPolicy: check if this job is allowed to schedule relation children.
	relPolicy := NewPostProcessingPolicy(job)
	allowRelationChildren := relPolicy.ShouldRun(StepRelationChildScheduling)
	if !allowRelationChildren {
		e.logLine(ctx, job, "info", fmt.Sprintf("> relation_child_scheduling_disabled: policy (recursionDepth=%d maxRecursionDepth=%d hasDomainExpansionPeer=%v)",
			relPolicy.RecursionDepth, relPolicy.MaxRecursionDepth, relPolicy.HasDomainExpansionPeer))
	}

	// Guardrails for recursive scouting:
	// - Depth is controlled by recursionDepth/maxRecursionDepth.
	// - Breadth is capped per parent job to avoid runaway fanout (LLM can emit many relations).
	// - Domain-derived jobs use lower cap to control cost explosion.
	// - We deduplicate targets within this job execution to avoid enqueueing the same target multiple times.
	hasExecutionPlan := job != nil && HasExecutionPlanContractState(job.State)
	executionPlan := ExecutionPlanContract{}
	if hasExecutionPlan {
		executionPlan = ExecutionPlanContractFromState(job.State)
	}
	maxChildren := 25
	if job != nil && job.State != nil {
		if v, ok := job.State["maxChildrenPerJob"]; ok {
			maxChildren = parseStateInt(v, maxChildren)
		}
		// Domain lens jobs: reduce fanout to control Vertex cost.
		if hasDomainsInState(job.State) && maxChildren > 6 {
			maxChildren = 6
		}
	}
	if hasExecutionPlan && executionPlan.MaxChildrenPerJob > 0 && (maxChildren == 0 || executionPlan.MaxChildrenPerJob < maxChildren) {
		maxChildren = executionPlan.MaxChildrenPerJob
	}
	if maxChildren < 0 {
		maxChildren = 0
	}
	budgetPressure := 0.0
	budgetFreezeDepth := false
	if e != nil && job != nil {
		lensIDForBudget := strings.TrimSpace(job.LensID)
		if lensIDForBudget == "" && job.State != nil {
			if v, ok := job.State["lensId"]; ok {
				lensIDForBudget = parseStateString(v)
			}
		}
		cascadeIDForBudget := ""
		if job.State != nil {
			if v, ok := job.State["cascadeRootJobId"].(string); ok {
				cascadeIDForBudget = strings.TrimSpace(v)
			}
		}
		if cascadeIDForBudget == "" {
			cascadeIDForBudget = strings.TrimSpace(job.ID)
		}
		if scale, snap, ok := e.computeRuntimeBudgetAdaptation(ctx, job.TenantID, lensIDForBudget, cascadeIDForBudget); ok {
			budgetPressure = snap.MaxPressure
			adaptedCap := scaleCapByBudget(maxChildren, 1, scale)
			if adaptedCap < maxChildren {
				e.logLine(ctx, job, "info", fmt.Sprintf("> fanout budget adapted (%s): maxChildren %d -> %d (%s)",
					RuntimeContractBudgetAdaptiveFanout, maxChildren, adaptedCap, snap.summary()))
				maxChildren = adaptedCap
			}
			budgetFreezeDepth = shouldFreezeDepthByBudgetPressure(budgetPressure)
		}
	}
	scheduledChildren := map[string]struct{}{}
	scheduledIdentityChildren := map[string]struct{}{}
	hasScheduledIdentity := func(k string) bool {
		_, ok := scheduledIdentityChildren[k]
		return ok
	}
	scheduledCount := 0

	// Best-effort: prefetch active (pending/running) scout jobs for this project/lens,
	// so we can avoid enqueueing duplicate child jobs for the same canonicalPath.
	activeJobByPath := map[string]string{}     // canonicalPath -> jobId
	activeJobByIdentity := map[string]string{} // targetIdentityKey -> jobId
	if e != nil && e.scoutingJobsRepo != nil && job != nil && strings.TrimSpace(job.TenantID) != "" && strings.TrimSpace(job.ProjectID) != "" {
		lensID := strings.TrimSpace(job.LensID)
		if lensID == "" && job.State != nil {
			if v, ok := job.State["lensId"]; ok {
				lensID = parseStateString(v)
			}
		}
		if jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, 300); err == nil {
			for _, j := range jobs {
				if j == nil {
					continue
				}
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				if p == "" {
					continue
				}
				if _, ok := activeJobByPath[p]; !ok {
					activeJobByPath[p] = j.ID
				}
				if j.State != nil {
					if v, ok := j.State["targetIdentityKey"].(string); ok {
						idk := strings.TrimSpace(v)
						if idk != "" {
							if _, exists := activeJobByIdentity[idk]; !exists {
								activeJobByIdentity[idk] = j.ID
							}
						}
					}
				}
			}
		}
	}

	// Edge-level guardrails:
	// - Deduplicate edges within this job run to reduce Firestore reads/writes.
	// - Normalize symmetric edges so A<->B is represented consistently.
	seenEdgeKeys := map[string]struct{}{}

	// Domain modes + specialties (optional): recurse strategy should be domain-driven first.
	specialties := []string(nil)
	if job != nil && job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}
	domainModes := taxonomy.NormalizeDomainModes(getJobDomainModes(job))
	isSupplyChainMode := taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain)
	if !isSupplyChainMode {
		isSupplyChainMode = taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain)
	}
	isGovernanceMode := taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGovernance)
	if !isGovernanceMode {
		isGovernanceMode = taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyEquity) || taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyManagement)
	}
	supplyChainPhase := ""
	if job != nil && job.State != nil {
		supplyChainPhase = strings.TrimSpace(strings.ToLower(parseStateString(job.State["supplyChainPhase"])))
	}
	if isSupplyChainMode && supplyChainPhase != "targeted_enrich" {
		before := len(relations)
		sourcePath := ""
		if sourceFragment != nil {
			sourcePath = strings.TrimSpace(sourceFragment.CanonicalPath)
		}
		relations, droppedTypes := filterDiscoveredRelationsByDomainModesWithDomainAnchorAndLog(sourceFragment, relations, []string{taxonomy.DomainModeSupplyChain})
		after := len(relations)
		if after != before {
			e.logLine(ctx, job, "info", fmt.Sprintf("> phase-A relation filter (%s): %d -> %d source=%q droppedTypes=%v",
				RuntimeContractExecutionPlan, before, after, sourcePath, droppedTypes))
		}
	}
	relationsResolvedByGrounding := 0
	relationsCreatedNew := 0
	dedupRedirectsByGrounding := 0
	dedupFalsePositiveSuspected := 0
	groundingReasonCounts := map[string]int64{}
	cascadeID := ""
	if job != nil && job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
			cascadeID = strings.TrimSpace(v)
		}
	}
	if cascadeID == "" && job != nil {
		cascadeID = strings.TrimSpace(job.ID)
	}
	lensIDForCascade := ""
	if job != nil {
		lensIDForCascade = strings.TrimSpace(job.LensID)
		if lensIDForCascade == "" && job.State != nil {
			lensIDForCascade = parseStateString(job.State["lensId"])
		}
	}
	cascadeRemaining := 0
	cascadeUsed := 0
	cascadeLimitActive := hasExecutionPlan && executionPlan.MaxCascadeJobs > 0
	if cascadeLimitActive {
		cascadeRemaining, cascadeUsed = e.estimateCascadeRemainingCapacity(ctx, job, lensIDForCascade, cascadeID, executionPlan.MaxCascadeJobs)
		if cascadeRemaining <= 0 {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> execution plan cascade cap reached (%s): used=%d limit=%d",
				RuntimeContractExecutionPlan,
				cascadeUsed, executionPlan.MaxCascadeJobs))
		}
	}
	sourceFacilityQualifier := resolveFacilityQualifierSemantic(sourceFragment, "")

	// Relation-level guardrails:
	// - Do not recurse on COMPETES_WITH (explodes breadth and is rarely needed for local intent).
	// - Do not recurse on INFLUENCES (spheres and vague influences create huge expansions).
	// - Require a moderate confidence for recursion to avoid "broad hallucinated" graphs.
	// - In supply_chain mode: ONLY recurse on supply-chain relevant edge types.
	shouldRecurse := func(relType string, conf float64, targetPath string, targetType models.FragmentType) bool {
		rt := strings.TrimSpace(relType)
		if rt == "" {
			return false
		}
		if !isFacilityQualifierBusinessRelation(sourceFacilityQualifier, rt, targetPath, targetType) {
			return false
		}
		if sourceFragment != nil && isSoftDeniedRelationForRecurse(sourceFragment.Type, targetType, sourceFragment.CanonicalPath, targetPath, rt) {
			return false
		}
		if conf > 0 && conf < 0.55 {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(targetPath), "/sphere/") || targetType == models.FragmentTypeSphere {
			return false
		}

		// Domain-mode strict filtering: only recurse on edges allowed by the active mode.
		if isSupplyChainMode {
			allow := EdgeAllowlistByDomainMode("supply_chain")
			if !AllowlistContains(allow, models.DependencyType(rt)) {
				return false
			}
			return true
		}
		if isGovernanceMode {
			allow := EdgeAllowlistByDomainMode("governance")
			if !AllowlistContains(allow, models.DependencyType(rt)) {
				return false
			}
			return true
		}

		// General mode: exclude known noisy edge types
		switch rt {
		case string(models.DependencyTypeCompetesWith),
			string(models.DependencyTypeInfluences),
			// OSINT-lite / social edges can explode recursion breadth; keep nodes/edges but don't auto-expand.
			string(models.DependencyTypeRelatedTo),
			string(models.DependencyTypeRelativeOf),
			// Auditors/assurers are governance facts; not useful to recursively expand by default.
			string(models.DependencyTypeAssures),
			string(models.DependencyTypeAuditedBy):
			return false
		default:
			return true
		}
	}

	// NOTE: specialties already read above for shouldRecurse filtering
	if job != nil && job.State != nil {
		// Skip duplicate read - already done above
		if v, ok := job.State["specialties"]; ok && len(specialties) == 0 {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}
	enableManagement := taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyManagement)
	runtimeGroundingEvidence := groundingEvidenceFromJobState(job)

	// Enable edge batch buffering: edges are accumulated during the loop and
	// flushed in a single Firestore batch write after all relations are processed.
	edgeBuf := NewEdgeBatchBuffer()
	batchCtx := WithEdgeBatchBuffer(ctx, edgeBuf)

	// Batch pre-resolve canonical paths (Phase 2 LLM optimization).
	batchCanonResults := e.resolveCanonicalPathsBatch(ctx, relations)

	for relIdx, rel := range relations {
		if rel.TargetName == "" {
			continue
		}

		// Normalize relation type early (code-defined allowlist) to avoid creating edges with unsupported types.
		// IMPORTANT: preserve the raw relation type for debugging taxonomy drift / LLM outputs.
		rawRelationType := strings.TrimSpace(rel.RelationType)
		rel.RelationType = strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
		normalizedRelationType := strings.TrimSpace(rel.RelationType)
		if rawRelationType != "" && normalizedRelationType != "" && rawRelationType != normalizedRelationType {
			logGroundingInfof("component=scout phase=relations.normalize_type jobId=%s targetCanonicalPath=%s reasonCode=relation_type_normalized raw=%q normalized=%q",
				strings.TrimSpace(job.ID),
				strings.TrimSpace(job.TargetCanonicalPath),
				rawRelationType,
				normalizedRelationType,
			)
		}
		targetTypeHint := e.normalizeFragmentType(ctx, rel.TargetType)
		targetType := targetTypeHint

		// 1. Determine target canonical path (use batch result if available, fall back to per-relation)
		var targetPath string
		if relIdx < len(batchCanonResults) && batchCanonResults[relIdx].Resolved && batchCanonResults[relIdx].Result != "" {
			targetPath = batchCanonResults[relIdx].Result
		} else {
			var err error
			targetPath, err = e.resolveCanonicalPath(ctx, rel)
			if err != nil {
				logGroundingErrorWithContext(job, "scout", "relations.resolve_canonical_path", "canonicalization_failed", "canonicalization_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.resolve_canonical_path", "canonicalization_failed", "canonicalization_error", true)
				continue
			}
		}
		// Align targetType with canonical path prefix (auto-fix mismatches).
		if inferred := InferFragmentTypeFromCanonicalPath(targetPath); inferred != "" && inferred != targetType {
			targetType = inferred
		}
		// Early strictification for GEO paths to avoid creating legacy non-strict nodes
		// like /loc/{cc}/{admin1}. Reuse shared canonicalization helper.
		if targetType == models.FragmentTypeGeoRegion {
			if norm := normalizeGeoPathToStrict(targetPath); norm != "" && norm != targetPath {
				targetPath = norm
			}
		}
		targetIdentityKey := buildRelationTargetIdentityKey(rel, targetPath, targetType)

		// 2. Find or Create Target Fragment
		//
		// GROUNDING-AWARE ENTITY RESOLUTION (Phase 2):
		// Before looking up by canonical path, check if this entity was grounded by Vertex AI Search.
		// If the LLM's grounding metadata referenced a fragment from our datastore, we use
		// the DB entity directly (with its real canonical path, identifiers, labels).
		// This eliminates a major source of duplicates: the LLM may generate a slightly different
		// canonical path than what's in the DB, but the grounding evidence contains the exact fragment ID.
		var targetFragment *models.HorizonFragment
		var targetID string

		groundingMatchReason := ""
		if groundingCtx != nil && !groundingCtx.IsEmpty() {
			identifiers := map[string]string{}
			if rel.TargetLegalName != "" {
				identifiers["legalName"] = rel.TargetLegalName
			}
			if rel.TargetShortName != "" {
				identifiers["shortName"] = rel.TargetShortName
			}
			grounded, matchReason := groundingCtx.FindBestMatch(targetPath, rel.TargetName, identifiers)
			groundingMatchReason = string(matchReason)
			if strings.TrimSpace(groundingMatchReason) != "" {
				groundingReasonCounts[strings.TrimSpace(groundingMatchReason)]++
			}
			if grounded != nil {
				// Entity EXISTS in our database — use it directly.
				targetFragment = grounded
				targetID = grounded.ID
				relationsResolvedByGrounding++
				groundedCP := strings.TrimSpace(grounded.CanonicalPath)
				if groundedCP != "" && groundedCP != targetPath {
					// The DB canonical path differs from LLM-generated one → record redirect.
					logGroundingInfof("component=scout phase=relations.grounding_match jobId=%s reasonCode=%s llmPath=%q dbPath=%q fragmentId=%s",
						strings.TrimSpace(job.ID),
						strings.TrimSpace(groundingMatchReason),
						targetPath,
						groundedCP,
						strings.TrimSpace(grounded.ID),
					)
					e.recordDeterministicRedirect(ctx, targetPath, groundedCP, groundingMatchReason)
					dedupRedirectsByGrounding++
					if groundingMatchReason == string(GroundingMatchLabel) {
						dedupFalsePositiveSuspected++
					}
					targetPath = groundedCP
				} else {
					logGroundingInfof("component=scout phase=relations.grounding_match jobId=%s reasonCode=%s path=%q fragmentId=%s",
						strings.TrimSpace(job.ID),
						strings.TrimSpace(groundingMatchReason),
						targetPath,
						strings.TrimSpace(grounded.ID),
					)
				}
			}
		}
		targetIdentityKey = buildRelationTargetIdentityKey(rel, targetPath, targetType)

		// Cascade-level identity memory: if this identity was already resolved in this cascade,
		// reuse that fragment/path even when current relation emits a different canonical path.
		if targetFragment == nil && e != nil && e.cascadeEntityRegistry != nil && job != nil && targetIdentityKey != "" && cascadeID != "" {
			rec, rerr := e.cascadeEntityRegistry.Touch(
				ctx,
				job.TenantID,
				job.ProjectID,
				strings.TrimSpace(job.LensID),
				cascadeID,
				targetIdentityKey,
				targetPath,
				job.ID,
			)
			if rerr == nil && rec != nil {
				boundID := strings.TrimSpace(rec.FragmentID)
				boundPath := strings.TrimSpace(rec.CanonicalPath)
				if boundID != "" {
					if existing, gerr := e.fragmentsRepo.Get(ctx, boundID); gerr == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
						targetFragment = existing
						targetID = existing.ID
						if cp := strings.TrimSpace(existing.CanonicalPath); cp != "" && cp != targetPath {
							e.recordDeterministicRedirect(ctx, targetPath, cp, "cascade_identity_registry")
							targetPath = cp
						}
					}
				} else if boundPath != "" && boundPath != targetPath {
					if existing, gerr := e.fragmentsRepo.GetByCanonicalPath(ctx, boundPath); gerr == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
						targetFragment = existing
						targetID = existing.ID
						e.recordDeterministicRedirect(ctx, targetPath, boundPath, "cascade_identity_registry")
						targetPath = boundPath
					}
				}
			}
		}

		if targetFragment == nil {
			existingByPath, getErr := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
			if getErr != nil {
				if !errors.Is(getErr, horizonrepo.ErrFragmentNotFound) {
					logGroundingErrorWithContext(job, "scout", "relations.lookup_target_fragment", "target_lookup_failed", "repo_read_error", true, getErr)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.lookup_target_fragment", "target_lookup_failed", "repo_read_error", true)
				}
			}
			targetFragment = existingByPath
		}

		if targetFragment == nil {
			// Resolve path through canonicalPathResolver to reuse existing fragment or canonical form (reduces duplicates).
			if e.canonicalPathResolver != nil {
				meta := map[string]string{}
				if v := strings.TrimSpace(rel.TargetShortName); v != "" {
					meta["shortName"] = v
				}
				if v := strings.TrimSpace(rel.TargetLegalName); v != "" {
					meta["legalName"] = v
				}
				if v := strings.TrimSpace(rel.TargetName); v != "" {
					meta["labelEN"] = v
					if _, ok := meta["shortName"]; !ok {
						meta["shortName"] = v
					}
				}
				var (
					resolved *ResolveResult
					rerr     error
				)
				if len(meta) > 0 {
					resolved, rerr = e.canonicalPathResolver.ResolveWithMetadata(ctx, targetPath, targetType, meta)
				} else {
					resolved, rerr = e.canonicalPathResolver.Resolve(ctx, targetPath, targetType)
				}
				if rerr == nil && resolved != nil {
					if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
						targetFragment = resolved.ExistingFragment
						targetID = targetFragment.ID
					} else if strings.TrimSpace(resolved.CanonicalPath) != "" && strings.TrimSpace(resolved.CanonicalPath) != targetPath {
						targetPath = strings.TrimSpace(resolved.CanonicalPath)
						if existingByPath2, gerr := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath); gerr == nil && existingByPath2 != nil && strings.TrimSpace(existingByPath2.ID) != "" {
							targetFragment = existingByPath2
							targetID = existingByPath2.ID
						}
					}
				}
			}
		}
		if targetFragment == nil {
			// Fragment doesn't exist -> Create minimal fragment (low confidence) and schedule scouting to fill it.
			// We do NOT use /discovered paths; canonicalPath must already be stable here.
			ft := e.normalizeFragmentType(ctx, rel.TargetType)
			if ft == "" {
				ft = InferFragmentTypeFromCanonicalPath(targetPath)
			}
			if ft == "" {
				ft = models.FragmentTypeOrganization
			}
			targetType = ft
			now := time.Now().UTC()
			legalName := rel.TargetName
			if rel.TargetLegalName != "" {
				legalName = rel.TargetLegalName
			}
			labelEN := bestEffortDisplayLabel(legalName, targetPath)
			newFragment := &models.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: targetPath,
				Type:          ft,
				Status:        "active",
				State:         make(map[string]models.MetricValue),
				Labels:        map[string]string{"en": labelEN},
				Identifiers: map[string]string{
					"legalName": legalName,
				},
				Governance:  models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
				Description: fmt.Sprintf("Discovered via relation from %s", sourceFragment.CanonicalPath),
				Provenance: models.Provenance{
					Method:    provenanceMethod,
					SourceIDs: []string{job.ID},
				},
				Metadata: models.FragmentMetadata{
					CreatedAt:       now,
					LastUpdatedAt:   now,
					LastVerifiedAt:  time.Time{}, // IMPORTANT: keep zero so freshness check does not skip scouting
					EpistemicStatus: models.EpistemicStatusLowConfidence,
				},
			}
			applyGroundingProvenance(&newFragment.Provenance, runtimeGroundingEvidence, nil, nil, nil)
			if rel.TargetShortName != "" {
				newFragment.Identifiers["shortName"] = rel.TargetShortName
			}
			// AFFECTED_ZONE: extract radiusKm from relation qualifiers when creating zone fragment
			if ft == models.FragmentTypeAffectedZone && rel.Qualifiers != nil {
				if v, ok := extractRadiusKmFromQualifiers(rel.Qualifiers); ok && v > 0 {
					newFragment.RadiusKm = v
				}
			}

			// Pre-create validation: check policies and deduplication (same as in processScoutResult)
			usedExistingDuplicate := false
			if e.preCreateValidator != nil {
				preValidation, valErr := e.preCreateValidator.ValidateAndProcess(ctx, newFragment)
				if valErr != nil {
					logGroundingErrorWithContext(job, "scout", "relations.precreate_validate", "precreate_validation_failed", "validation_error", true, valErr)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.precreate_validate", "precreate_validation_failed", "validation_error", true)
				} else if preValidation != nil {
					// Log warnings
					for _, w := range preValidation.Warnings {
						logGroundingWarnf("component=scout phase=relations.precreate_validate jobId=%s targetCanonicalPath=%s reasonCode=precreate_warning warning=%q",
							strings.TrimSpace(job.ID),
							strings.TrimSpace(targetPath),
							strings.TrimSpace(w),
						)
					}
					// Check if creation should be blocked
					if !preValidation.IsValid {
						logGroundingWarnf("component=scout phase=relations.precreate_validate jobId=%s targetCanonicalPath=%s reasonCode=precreate_blocked blockReason=%q",
							strings.TrimSpace(job.ID),
							strings.TrimSpace(targetPath),
							strings.TrimSpace(preValidation.BlockReason),
						)
						continue
					}
					// Check if we should use an existing fragment instead (deduplication)
					if preValidation.ReplacementFrag != nil && preValidation.ReplacementFrag.ID != "" {
						logGroundingInfof("component=scout phase=relations.precreate_validate jobId=%s targetCanonicalPath=%s reasonCode=precreate_replaced replacementFragmentId=%s",
							strings.TrimSpace(job.ID),
							strings.TrimSpace(targetPath),
							strings.TrimSpace(preValidation.ReplacementFrag.ID),
						)
						targetFragment = preValidation.ReplacementFrag
						targetID = targetFragment.ID
						usedExistingDuplicate = true
					}
				}
			}

			if !usedExistingDuplicate {
				// Race guard: another worker/job might have created the fragment after our first GetByCanonicalPath.
				if reread, rerr := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath); rerr == nil && reread != nil && strings.TrimSpace(reread.ID) != "" {
					targetID = reread.ID
					targetFragment = reread
					usedExistingDuplicate = true
				}
			}

			if !usedExistingDuplicate {
				id, err := e.fragmentsRepo.Create(ctx, newFragment)
				if err != nil {
					logGroundingErrorWithContext(job, "scout", "relations.create_target_fragment", "fragment_create_failed", "repo_write_error", true, err)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.create_target_fragment", "fragment_create_failed", "repo_write_error", true)
					continue
				}
				targetID = id
				targetFragment = newFragment
				targetFragment.ID = id
				if strings.HasPrefix(strings.TrimSpace(targetPath), "/zone/") {
					log.Printf("[ScoutEngine] zone: fragment CREATED jobId=%s path=%s fragmentId=%s", strings.TrimSpace(job.ID), targetPath, id)
				}
				relationsCreatedNew++
			}
			if targetFragment != nil {
				e.bestEffortEnrichRelationTargetGeo(ctx, targetFragment, sourceFragment, provenanceMethod, job.ID)
			}

			// Create Recursive Scouting Job (Decomposition)
			// Проверяем глубину рекурсии, чтобы не уйти в бесконечность.
			// Читаем текущую глубину/лимит из job.State (это позволяет прод-ядру работать так же,
			// как тестовый сценарий).
			currentDepth := 0
			if job != nil && job.State != nil {
				// Firestore may decode numbers as int64; do not assume float64.
				currentDepth = parseStateInt(job.State["recursionDepth"], 0)
			}

			maxDepth := 2
			if job != nil && job.State != nil {
				if v, ok := job.State["maxRecursionDepth"]; ok {
					maxDepth = parseStateInt(v, 2)
				}
			}
			if maxDepth < 0 {
				maxDepth = 0
			}

			// RuntimeContractBudgetAdaptiveFanout:
			// keep pipeline progress under budget pressure; freeze depth only in severe overrun.
			if budgetFreezeDepth {
				maxDepth = currentDepth
			}

			// Ограничиваем глубину; skip if policy disallows relation child scheduling.
			if allowRelationChildren && currentDepth < maxDepth {
				if cascadeLimitActive && cascadeRemaining <= 0 {
					continue
				}
				// Breadth cap + per-parent dedup (in-memory).
				effectiveCap := maxChildren
				// Depth-aware cap: keep the graph tight for shallow (depth=2) lenses.
				if currentDepth <= 0 && effectiveCap > 12 {
					effectiveCap = 12
				} else if currentDepth >= 1 && effectiveCap > 6 {
					effectiveCap = 6
				}
				if effectiveCap == 0 || scheduledCount >= effectiveCap {
					// skip recursive expansion
				} else if _, ok := scheduledChildren[targetPath]; ok {
					// already scheduled from another relation in the same job
				} else if targetIdentityKey != "" && hasScheduledIdentity(targetIdentityKey) {
					// already scheduled by identity from another relation in the same job
					continue
				} else if !shouldRecurse(rel.RelationType, rel.Confidence, targetPath, targetType) {
					// relation type / confidence not eligible for recursion
				} else {
					// Check if we should skip creating scouting job
					shouldSkipJob := false

					// CRITICAL: Skip self-reference (prevents infinite loops)
					if sourceFragment != nil && strings.TrimSpace(targetPath) == strings.TrimSpace(sourceFragment.CanonicalPath) {
						fmt.Printf("[ScoutEngine] Skipping self-reference job: %s -> %s. Circular dependency avoided.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}
					if !shouldSkipJob && shouldSkipSameFacilityQualifierRecursion(sourceFragment, targetPath, targetType, targetFragment) {
						fmt.Printf("[ScoutEngine] Skipping same-qualifier FAC recursion: %s -> %s.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}

					// CRITICAL: Skip if target fragment is already verified (regardless of edge existence)
					// This prevents re-scouting entities that were just processed
					if !shouldSkipJob && targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
						fmt.Printf("[ScoutEngine] Target fragment already verified: %s (status=%s). Skipping scouting job.\n",
							targetPath, targetFragment.Metadata.EpistemicStatus)
						shouldSkipJob = true
					}

					// Check if edge already exists and target fragment is verified
					// This prevents duplicate scouting jobs for already-processed entities
					if !shouldSkipJob {
						edgeType := models.DependencyType(rel.RelationType)
						if e.edgesRepo != nil && sourceFragment != nil && sourceFragment.ID != "" && targetID != "" {
							if existingEdge, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragment.ID, targetID, edgeType); err == nil && existingEdge != nil {
								// Edge already exists, check if target fragment is verified
								if targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
									// Fragment is verified, skip creating new scouting job
									fmt.Printf("[ScoutEngine] Edge already exists and target fragment is verified: %s -> %s (%s). Skipping scouting job.\n",
										sourceFragment.CanonicalPath, targetPath, rel.RelationType)
									shouldSkipJob = true
								}
							}
						}
					}

					// Check if there's already an active scouting job for this canonicalPath (pending/running).
					// Uses a prefetched snapshot to avoid per-relation queries.
					if !shouldSkipJob {
						if existingJobID, ok := activeJobByPath[strings.TrimSpace(targetPath)]; ok && strings.TrimSpace(existingJobID) != "" {
							fmt.Printf("[ScoutEngine] Active scouting job already exists for %s (jobId=%s). Skipping duplicate.\n",
								targetPath, existingJobID)
							scheduledChildren[targetPath] = struct{}{}
							shouldSkipJob = true
						}
					}
					if !shouldSkipJob && targetIdentityKey != "" {
						if existingJobID, ok := activeJobByIdentity[targetIdentityKey]; ok && strings.TrimSpace(existingJobID) != "" {
							fmt.Printf("[ScoutEngine] Active scouting job already exists for identity %s (jobId=%s). Skipping duplicate for path=%s.\n",
								targetIdentityKey, existingJobID, targetPath)
							scheduledChildren[targetPath] = struct{}{}
							scheduledIdentityChildren[targetIdentityKey] = struct{}{}
							shouldSkipJob = true
						}
					}

					if shouldSkipJob {
						continue // Skip to next relation
					}

					// Cascade visited set: prevent enqueueing same (targetPath, relationType) multiple times.
					// For relations: relationHash distinguishes different relations to same target.
					// For supply chain: relationHash empty.
					relationHash := ""
					if sourceFragment != nil {
						relationHash = horizonrepo.RelationHashForDedup(sourceFragment.CanonicalPath, targetPath, rel.RelationType)
					}
					if (isSupplyChainMode || cascadeID != "") && e != nil && e.cascadeVisitedRepo != nil {
						already, vErr := e.cascadeVisitedRepo.TryMarkVisitedWithRelation(
							ctx,
							job.TenantID,
							job.ProjectID,
							cascadeID,
							targetPath,
							relationHash,
							job.ID,
							currentDepth+1,
						)
						if vErr != nil {
							logGroundingErrorWithContext(job, "scout", "relations.supply_chain_visited", "visited_mark_failed", "repo_update_error", true, vErr)
							e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.supply_chain_visited", "visited_mark_failed", "repo_update_error", true)
						} else if already {
							msg := fmt.Sprintf("[SupplyChainVisited] SKIP alreadyVisited tenant=%s cascade=%s target=%s depth=%d",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), currentDepth+1)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "info", msg)
							continue
						}
					}

					scheduledChildren[targetPath] = struct{}{}
					if targetIdentityKey != "" {
						scheduledIdentityChildren[targetIdentityKey] = struct{}{}
					}
					scheduledCount++

					agentID := job.AgentID
					if agentID == "" {
						agentID = "scout"
					}
					now := time.Now().UTC()
					childState := map[string]interface{}{
						"targetCanonicalPath": targetPath,
						"currentState":        string(models.ScoutingJobStatePending),
						"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
						"recursionDepth":      float64(currentDepth + 1),
						"maxRecursionDepth":   float64(maxDepth),
						"parentJobId":         job.ID,
					}
					if targetIdentityKey != "" {
						childState["targetIdentityKey"] = targetIdentityKey
					}
					// Propagate lens id (critical for assembly tracking, budgets, and UI filtering).
					if job != nil {
						if strings.TrimSpace(job.LensID) != "" {
							childState["lensId"] = strings.TrimSpace(job.LensID)
						} else if job.State != nil {
							if v, ok := job.State["lensId"]; ok {
								if s := parseStateString(v); s != "" {
									childState["lensId"] = s
								}
							}
						}
						// Propagate optional breadth cap downstream
						if maxChildren > 0 {
							childState["maxChildrenPerJob"] = float64(maxChildren)
						}
					}
					// Propagate cascade root id for budget attribution.
					if job != nil && job.State != nil {
						if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
							childState["cascadeRootJobId"] = strings.TrimSpace(v)
						} else {
							childState["cascadeRootJobId"] = job.ID
						}
					} else {
						childState["cascadeRootJobId"] = job.ID
					}
					if relationHash != "" {
						childState["relationHash"] = relationHash
					}
					// Propagate requestedMetrics, if present (ensures identical dynamic metrics behavior across prod/test).
					if job != nil && job.State != nil {
						if rm, ok := job.State["requestedMetrics"]; ok {
							childState["requestedMetrics"] = rm
						}
						// Propagate specialties (optional task focus modules).
						if v, ok := job.State["specialties"]; ok {
							childState["specialties"] = v
						}
						// Propagate supply chain context for continuous chain building.
						// This includes discovered chain fragments so far, enabling agents to extend them.
						if v, ok := job.State["chainContext"]; ok {
							childState["chainContext"] = v
						}
						// Propagate intent steering signals so recursive scouting stays aligned with the Lens focus.
						// These may be non-canonical, but are safe to pass through job state.
						if v, ok := job.State["focus"]; ok {
							childState["focus"] = v
						}
						if v, ok := job.State["keywords"]; ok {
							childState["keywords"] = v
						}
						if v, ok := job.State["spherePaths"]; ok {
							childState["spherePaths"] = v
						}
						// Propagate originalQuery + decompositionHints so grounded research stays aligned
						// even deep in recursion (especially important for MARKET/PROGRAM anchors).
						if v, ok := job.State["originalQuery"]; ok {
							childState["originalQuery"] = v
						}
						if v, ok := job.State["decompositionHints"]; ok {
							childState["decompositionHints"] = v
						}
						if v, ok := job.State["relationChildSchedulingOptIn"]; ok && parseStateBool(v, false) {
							childState["relationChildSchedulingOptIn"] = true
						}
						copyDomainModeStateToChild(job.State, childState)
					}
					scoutingJob := &models.ScoutingJob{
						AgentJob: agentmodels.AgentJob{
							TenantID:  job.TenantID,
							ProjectID: job.ProjectID,
							Status:    agentmodels.AgentJobStatusPending,
							AgentID:   agentID, // inherit agentId to allow deterministic test workers and consistent routing
							CreatedAt: now,
							UpdatedAt: now,
							State:     childState,
						},
						LensID:              strings.TrimSpace(job.LensID),
						TargetCanonicalPath: targetPath,
						ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
						BillingContext: models.BillingContext{
							ChargeModel: string(models.ChargeModelBounty),
							ProjectID:   job.ProjectID,
						},
					}
					if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
						logGroundingErrorWithContext(job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true, err)
						e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true)
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if (isSupplyChainMode || cascadeID != "") && e != nil && e.cascadeVisitedRepo != nil {
							if delErr := e.cascadeVisitedRepo.DeleteVisitedWithRelation(ctx, job.TenantID, cascadeID, targetPath, relationHash); delErr != nil {
								logGroundingErrorWithContext(job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true, delErr)
								e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true)
							}
						}
					} else {
						if cascadeLimitActive && cascadeRemaining > 0 {
							cascadeRemaining--
						}
						fmt.Printf("Triggered recursive scouting for: %s\n", targetPath)
					}
				}
			}

		} else {
			targetID = targetFragment.ID
			if targetFragment.Type != "" {
				targetType = targetFragment.Type
			}

			// If fragment exists but is not verified (or never verified), schedule scouting to enrich it.
			// This is what makes the graph "expand" beyond already-known nodes (e.g., Panasonic) when needed.
			currentDepth := 0
			if job != nil && job.State != nil {
				currentDepth = parseStateInt(job.State["recursionDepth"], 0)
			}
			shouldScout := targetFragment.Metadata.LastVerifiedAt.IsZero() || targetFragment.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence
			maxDepth := 2
			if job != nil && job.State != nil {
				if v, ok := job.State["maxRecursionDepth"]; ok {
					maxDepth = parseStateInt(v, 2)
				}
			}
			if maxDepth < 0 {
				maxDepth = 0
			}

			// RuntimeContractBudgetAdaptiveFanout:
			// keep pipeline progress under budget pressure; freeze depth only in severe overrun.
			if budgetFreezeDepth {
				maxDepth = currentDepth
			}

			if allowRelationChildren && shouldScout && currentDepth < maxDepth {
				if cascadeLimitActive && cascadeRemaining <= 0 {
					continue
				}
				// Breadth cap + per-parent dedup (in-memory).
				effectiveCap := maxChildren
				if currentDepth <= 0 && effectiveCap > 12 {
					effectiveCap = 12
				} else if currentDepth >= 1 && effectiveCap > 6 {
					effectiveCap = 6
				}
				if effectiveCap == 0 || scheduledCount >= effectiveCap {
					// skip recursive expansion
				} else if _, ok := scheduledChildren[targetPath]; ok {
					// already scheduled from another relation in the same job
				} else if targetIdentityKey != "" && hasScheduledIdentity(targetIdentityKey) {
					continue
				} else if !shouldRecurse(rel.RelationType, rel.Confidence, targetPath, targetType) {
					// relation type / confidence not eligible for recursion
				} else {
					// Check if we should skip creating scouting job
					shouldSkipJob := false

					// CRITICAL: Skip self-reference (prevents infinite loops)
					if sourceFragment != nil && strings.TrimSpace(targetPath) == strings.TrimSpace(sourceFragment.CanonicalPath) {
						fmt.Printf("[ScoutEngine] Skipping self-reference job: %s -> %s. Circular dependency avoided.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}
					if !shouldSkipJob && shouldSkipSameFacilityQualifierRecursion(sourceFragment, targetPath, targetType, targetFragment) {
						fmt.Printf("[ScoutEngine] Skipping same-qualifier FAC recursion: %s -> %s.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}

					// CRITICAL: Skip if target fragment is already verified (regardless of edge existence)
					// This prevents re-scouting entities that were just processed
					if !shouldSkipJob && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
						fmt.Printf("[ScoutEngine] Target fragment already verified: %s (status=%s). Skipping scouting job.\n",
							targetPath, targetFragment.Metadata.EpistemicStatus)
						shouldSkipJob = true
					}

					// Check if edge already exists and target fragment is verified
					if !shouldSkipJob {
						edgeType := models.DependencyType(rel.RelationType)
						if e.edgesRepo != nil && sourceFragment != nil && sourceFragment.ID != "" && targetFragment.ID != "" {
							if existingEdge, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragment.ID, targetFragment.ID, edgeType); err == nil && existingEdge != nil {
								if targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
									fmt.Printf("[ScoutEngine] Edge already exists and target fragment is verified: %s -> %s (%s). Skipping scouting job.\n",
										sourceFragment.CanonicalPath, targetPath, rel.RelationType)
									shouldSkipJob = true
								}
							}
						}
					}

					// Check if there's already an active scouting job for this canonicalPath (pending/running).
					if !shouldSkipJob {
						if existingJobID, ok := activeJobByPath[strings.TrimSpace(targetPath)]; ok && strings.TrimSpace(existingJobID) != "" {
							fmt.Printf("[ScoutEngine] Active scouting job already exists for %s (jobId=%s). Skipping duplicate.\n",
								targetPath, existingJobID)
							scheduledChildren[targetPath] = struct{}{}
							shouldSkipJob = true
						}
					}
					if !shouldSkipJob && targetIdentityKey != "" {
						if existingJobID, ok := activeJobByIdentity[targetIdentityKey]; ok && strings.TrimSpace(existingJobID) != "" {
							scheduledChildren[targetPath] = struct{}{}
							scheduledIdentityChildren[targetIdentityKey] = struct{}{}
							shouldSkipJob = true
						}
					}

					if shouldSkipJob {
						continue // Skip to next relation
					}

					scheduledChildren[targetPath] = struct{}{}
					if targetIdentityKey != "" {
						scheduledIdentityChildren[targetIdentityKey] = struct{}{}
					}
					scheduledCount++

					agentID := job.AgentID
					if agentID == "" {
						agentID = "scout"
					}
					now := time.Now().UTC()
					childState := map[string]interface{}{
						"targetCanonicalPath": targetPath,
						"currentState":        string(models.ScoutingJobStatePending),
						"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
						"recursionDepth":      float64(currentDepth + 1),
						"maxRecursionDepth":   float64(maxDepth),
						"parentJobId":         job.ID,
					}
					if targetIdentityKey != "" {
						childState["targetIdentityKey"] = targetIdentityKey
					}
					if job != nil {
						if strings.TrimSpace(job.LensID) != "" {
							childState["lensId"] = strings.TrimSpace(job.LensID)
						} else if job.State != nil {
							if v, ok := job.State["lensId"]; ok {
								if s := parseStateString(v); s != "" {
									childState["lensId"] = s
								}
							}
						}
						if maxChildren > 0 {
							childState["maxChildrenPerJob"] = float64(maxChildren)
						}
					}
					// Propagate cascade root id for budget attribution.
					if job != nil && job.State != nil {
						if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
							childState["cascadeRootJobId"] = strings.TrimSpace(v)
						} else {
							childState["cascadeRootJobId"] = job.ID
						}
					} else {
						childState["cascadeRootJobId"] = job.ID
					}
					relationHash2 := ""
					if sourceFragment != nil {
						relationHash2 = horizonrepo.RelationHashForDedup(sourceFragment.CanonicalPath, targetPath, rel.RelationType)
					}
					if relationHash2 != "" {
						childState["relationHash"] = relationHash2
					}
					if job != nil && job.State != nil {
						if rm, ok := job.State["requestedMetrics"]; ok {
							childState["requestedMetrics"] = rm
						}
						if v, ok := job.State["specialties"]; ok {
							childState["specialties"] = v
						}
						if v, ok := job.State["focus"]; ok {
							childState["focus"] = v
						}
						if v, ok := job.State["keywords"]; ok {
							childState["keywords"] = v
						}
						if v, ok := job.State["spherePaths"]; ok {
							childState["spherePaths"] = v
						}
						if v, ok := job.State["relationChildSchedulingOptIn"]; ok && parseStateBool(v, false) {
							childState["relationChildSchedulingOptIn"] = true
						}
						copyDomainModeStateToChild(job.State, childState)
					}
					scoutingJob := &models.ScoutingJob{
						AgentJob: agentmodels.AgentJob{
							TenantID:  job.TenantID,
							ProjectID: job.ProjectID,
							Status:    agentmodels.AgentJobStatusPending,
							AgentID:   agentID,
							CreatedAt: now,
							UpdatedAt: now,
							State:     childState,
						},
						LensID:              strings.TrimSpace(job.LensID),
						TargetCanonicalPath: targetPath,
						ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
						BillingContext: models.BillingContext{
							ChargeModel: string(models.ChargeModelBounty),
							ProjectID:   job.ProjectID,
						},
					}
					if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err == nil {
						if cascadeLimitActive && cascadeRemaining > 0 {
							cascadeRemaining--
						}
						fmt.Printf("Triggered recursive scouting (existing fragment) for: %s\n", targetPath)
					} else {
						logGroundingErrorWithContext(job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true, err)
						e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true)
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if (isSupplyChainMode || cascadeID != "") && e != nil && e.cascadeVisitedRepo != nil {
							if delErr := e.cascadeVisitedRepo.DeleteVisitedWithRelation(ctx, job.TenantID, cascadeID, targetPath, relationHash2); delErr != nil {
								logGroundingErrorWithContext(job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true, delErr)
								e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true)
							}
						}
					}
				}
			}
		}

		if targetIdentityKey == "" && targetFragment != nil {
			targetIdentityKey = buildFragmentIdentityKey(targetFragment)
		}
		if targetFragment != nil && targetIdentityKey != "" && e != nil && e.cascadeEntityRegistry != nil && job != nil && cascadeID != "" {
			if rec, berr := e.cascadeEntityRegistry.BindFragment(
				ctx,
				job.TenantID,
				job.ProjectID,
				strings.TrimSpace(job.LensID),
				cascadeID,
				targetIdentityKey,
				targetFragment.ID,
				strings.TrimSpace(targetFragment.CanonicalPath),
				job.ID,
			); berr == nil && rec != nil {
				if bound := strings.TrimSpace(rec.CanonicalPath); bound != "" && bound != strings.TrimSpace(targetPath) {
					e.recordDeterministicRedirect(ctx, targetPath, bound, "cascade_identity_registry_bind")
					targetPath = bound
				}
			}
		}

		relationAllowedForScopeAdd := true

		// Pre-materialization supply-chain type guard: reject out-of-contract types before persistence.
		// Catches any relation type that slipped through phase-A filter (e.g. from canonicalization, strategic overlay).
		if isSupplyChainMode {
			srcPath := ""
			if sourceFragment != nil {
				srcPath = strings.TrimSpace(sourceFragment.CanonicalPath)
			}
			if explicitSrc := strings.TrimSpace(rel.SourceCanonicalPath); explicitSrc != "" {
				srcPath = explicitSrc
			}
			allowed := IsAllowedSupplyChainRuntimeRelation(srcPath, targetPath, models.DependencyType(normalizedRelationType))
			if !allowed {
				relationAllowedForScopeAdd = false
				e.logLine(ctx, job, "info", fmt.Sprintf("> pre_materialization_supply_chain_guard: dropped type=%q source=%q target=%q reason=out_of_contract",
					normalizedRelationType, srcPath, targetPath))
				continue
			}
		}

		// Best-effort: add key entities to lens scope so they appear in frontend list.
		// This must happen after contract gates, otherwise out-of-contract relations can leak nodes into scope.
		if targetFragment != nil && lensIDForCascade != "" &&
			shouldAutoAddRelationTargetToScope(targetPath, targetType, relationAllowedForScopeAdd) {
			e.bestEffortDeferredScopeAddForJob(ctx, job, lensIDForCascade, strings.TrimSpace(targetPath))
		}

		// 3. Materialize edge (centralized path).
		// NOTE: This is the unified edge materializer used for both base enrichment and strategic projection.
		// The legacy inline materialization block below is kept temporarily for safe diffing/migration,
		// but is bypassed via continue.
		if matErr := e.materializeRelationEdge(
			batchCtx,
			job,
			sourceFragment,
			targetFragment,
			targetID,
			targetType,
			targetPath,
			rel,
			rawRelationType,
			normalizedRelationType,
			provenanceMethod,
			baseQualifiers,
			enableManagement,
			seenEdgeKeys,
		); matErr != nil {
			logGroundingErrorWithContext(job, "scout", "relations.materialize_edge", "edge_materialization_failed", "materialization_error", true, matErr)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.materialize_edge", "edge_materialization_failed", "materialization_error", true)
		}
		continue

		/* LEGACY INLINE EDGE MATERIALIZER (disabled; kept for reference during migration)

		// 3. Create Dependency Edge
		// Normalize and validate relation types against code-defined taxonomy contracts.
		edgeType := models.DependencyType(strings.TrimSpace(rel.RelationType))
		edgeSourceID := sourceFragment.ID
		edgeTargetID := targetID

		// === EXPLICIT SOURCE HANDLING (Phase 2 strategic relations) ===
		// If SourceCanonicalPath is set, the edge should originate from a different entity
		// than the default sourceFragment. This is used for strategic edges like GATEKEEPS_DECISION
		// where the semantic direction requires an ORG/GOV as source, not the DECISION_POINT.
		explicitSourceFragment := sourceFragment
		if explicitSourcePath := strings.TrimSpace(rel.SourceCanonicalPath); explicitSourcePath != "" {
			// Fast-path: if the explicit source matches the current source fragment, don't do extra lookups.
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == explicitSourcePath {
				// Keep edgeSourceID as sourceFragment.ID
			} else if e.fragmentsRepo == nil {
				// No repo available; fall back to default source.
			} else {
			// Find or create the explicit source fragment
			if existingSrc, err := e.fragmentsRepo.GetByCanonicalPath(ctx, explicitSourcePath); err == nil && existingSrc != nil {
				explicitSourceFragment = existingSrc
				edgeSourceID = existingSrc.ID
				fmt.Printf("[ScoutEngine] Using explicit source for edge: %s -> %s [%s]\n", explicitSourcePath, targetPath, edgeType)
			} else {
				// Create minimal fragment for explicit source
				srcType := e.normalizeFragmentType(ctx, rel.SourceType)
				if srcType == "" {
					srcType = InferFragmentTypeFromCanonicalPath(explicitSourcePath)
				}
				if srcType == "" {
					srcType = models.FragmentTypeOrganization
				}
				now := time.Now().UTC()
				srcName := rel.SourceName
				if srcName == "" {
					// Extract name from path as fallback
					parts := strings.Split(strings.Trim(explicitSourcePath, "/"), "/")
					if len(parts) > 0 {
						srcName = parts[len(parts)-1]
					}
				}
				newSrcFrag := &models.HorizonFragment{
					TenantID:      "global",
					CanonicalPath: explicitSourcePath,
					Type:          srcType,
					Labels:        map[string]string{"en": srcName},
					Status:        "active",
					Metadata: models.FragmentMetadata{
						CreatedAt:       now,
						LastUpdatedAt:   now,
						SourceFrameID:   provenanceMethod,
						EpistemicStatus: models.EpistemicStatusLowConfidence,
					},
					Provenance: models.Provenance{
						SourceIDs: []string{"explicit_source_" + provenanceMethod},
						Method:    provenanceMethod,
					},
				}
				// AFFECTED_ZONE: extract radiusKm from relation qualifiers when creating zone as explicit source
				if srcType == models.FragmentTypeAffectedZone && rel.Qualifiers != nil {
					if v, ok := extractRadiusKmFromQualifiers(rel.Qualifiers); ok && v > 0 {
						newSrcFrag.RadiusKm = v
					}
				}
					if createdID, createErr := e.fragmentsRepo.Create(ctx, newSrcFrag); createErr == nil {
						edgeSourceID = createdID
						explicitSourceFragment = newSrcFrag
						fmt.Printf("[ScoutEngine] Created explicit source fragment: %s (%s)\n", explicitSourcePath, srcType)
					} else {
						logGroundingErrorWithContext(job, "scout", "relations.explicit_source_create", "explicit_source_create_failed", "repo_write_error", true, createErr)
						e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.explicit_source_create", "explicit_source_create_failed", "repo_write_error", true)
						// Fall back to default source
						explicitSourceFragment = sourceFragment
						edgeSourceID = sourceFragment.ID
				}
			}
			}
			// When explicit source is used and target is THIS FRAGMENT, swap target to sourceFragment
			if strings.TrimSpace(rel.TargetCanonicalPath) == strings.TrimSpace(sourceFragment.CanonicalPath) {
				edgeTargetID = sourceFragment.ID
				targetType = sourceFragment.Type
			}
		}

		// Enforce directional semantics for common relations (best-effort, heuristic).
		// This prevents obviously inverted edges like "/loc/astana SUPPLIES_TO /org/crrc".
		edgeSourceID, edgeTargetID, edgeType, directionChanged := normalizeRelationEdge(explicitSourceFragment, edgeSourceID, edgeTargetID, edgeType, targetType, enableManagement)
		switch edgeType {
		case "SUBSIDIARY_OF":
			// Canonical representation: OWNS from parent -> subsidiary.
			edgeType = models.DependencyTypeOwns
			edgeSourceID, edgeTargetID = targetID, sourceFragment.ID
			directionChanged = true
		}

		// Policy: self-loop prevention (reject).
		if IsSelfLoop(edgeSourceID, edgeTargetID) {
			fmt.Printf("Skipping self-edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}

		// Policy: symmetric edge normalization (avoid A->B and B->A duplicates).
		edgeSourceID, edgeTargetID, edgeType, symmetricSwapped := NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
		if symmetricSwapped {
			directionChanged = true
		}

		// Resolve *actual* endpoint types for validation (must match edgeSourceID/edgeTargetID after swaps).
		// Rely on local fragments when possible to avoid extra Firestore reads.
		resolveTypeByID := func(id string) models.FragmentType {
			id = strings.TrimSpace(id)
			if id == "" {
				return ""
			}
			resolveFromPath := func(path string, current models.FragmentType) models.FragmentType {
				inferred := InferFragmentTypeFromCanonicalPath(strings.TrimSpace(path))
				if inferred == "" {
					return current
				}
				if current == "" || current != inferred {
					return inferred
				}
				return current
			}
			// Check explicit source fragment first (Phase 2 strategic relations)
			if explicitSourceFragment != nil && strings.TrimSpace(explicitSourceFragment.ID) == id {
				return resolveFromPath(explicitSourceFragment.CanonicalPath, explicitSourceFragment.Type)
			}
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
				return resolveFromPath(sourceFragment.CanonicalPath, sourceFragment.Type)
			}
			if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
				return resolveFromPath(targetFragment.CanonicalPath, targetFragment.Type)
			}
			// Fallback: best-effort fetch (rare; only when endpoints differ from source/target fragments).
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
					return resolveFromPath(f.CanonicalPath, f.Type)
				}
			}
			return ""
		}
		// Resolve *actual* endpoint canonical paths (needed for /gov vs /org semantics).
		// NOTE: Gov bodies are modeled as ORGANIZATION fragments, so type-only validation can't distinguish them.
		resolvePathByID := func(id string) string {
			id = strings.TrimSpace(id)
			if id == "" {
				return ""
			}
			// Check explicit source fragment first (Phase 2 strategic relations)
			if explicitSourceFragment != nil && strings.TrimSpace(explicitSourceFragment.ID) == id {
				return strings.TrimSpace(explicitSourceFragment.CanonicalPath)
			}
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
				return strings.TrimSpace(sourceFragment.CanonicalPath)
			}
			if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
				return strings.TrimSpace(targetFragment.CanonicalPath)
			}
			// Fallback: best-effort fetch (rare; only when endpoints differ from source/target fragments).
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
					return strings.TrimSpace(f.CanonicalPath)
				}
			}
			return ""
		}
		srcTypeActual := resolveTypeByID(edgeSourceID)
		dstTypeActual := resolveTypeByID(edgeTargetID)

		// Unified canonicalization via policy (includes REGULATES /gov swap, transition fallback, unknown type fallback).
		srcPathActual := resolvePathByID(edgeSourceID)
		dstPathActual := resolvePathByID(edgeTargetID)
		canonFromEdgeType := ""
		canonReason := ""
		relConfidence := rel.Confidence
		policyStatus := models.PolicyStatusClean
		policyReason := ""
		policyPenalty := 0.0
		policyEdgeModality := ""
		canon := policy.ApplyEdgeCanonicalizationPolicy(policy.EdgeCanonicalizationInput{
			EdgeType:          edgeType,
			SourceType:        srcTypeActual,
			TargetType:        dstTypeActual,
			SourcePath:        srcPathActual,
			TargetPath:        dstPathActual,
			RawRelationType:   rawRelationType,
			Reason:            strings.TrimSpace(rel.Reason),
			Qualifiers:        rel.Qualifiers,
			OwnershipEvidence: models.HasOwnershipEvidence(rel.Reason, rel.Qualifiers, rel.Metrics),
		})
		if canon.Changed {
			if canon.OriginalType != "" {
				canonFromEdgeType = string(canon.OriginalType)
			} else {
				canonFromEdgeType = string(edgeType)
			}
			canonReason = canon.Reason
			edgeType = canon.EdgeType
			directionChanged = true
			if canon.ShouldSwap {
				edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
				srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
				srcPathActual, dstPathActual = dstPathActual, srcPathActual
			}
			edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
		}
			if canon.PolicyPenalty > 0 {
				relConfidence = relConfidence * canon.ConfidenceMult
			if relConfidence < 0 {
				relConfidence = 0
			}
			if canon.PolicyPenalty > policyPenalty {
				policyPenalty = canon.PolicyPenalty
			}
			policyStatus = policy.MergePolicyStatus(policyStatus, canon.PolicyStatus)
			if strings.TrimSpace(canon.PolicyReason) != "" {
				policyReason = strings.TrimSpace(canon.PolicyReason)
			}
			if canon.PolicyReason == "transition_unclassified" {
				policy.LogPolicyEvent("edge_canonicalization_policy", policy.PolicyLogStageFallback, map[string]interface{}{
					"trace_id":      "na",
					"tenant_id":     job.TenantID,
					"job_id":        job.ID,
					"edge_id":       "",
					"source_type":   srcTypeActual,
					"target_type":   dstTypeActual,
					"relation_type": edgeType,
					"action":        policy.ActionAllowWithPenalty,
					"policy_status": policyStatus,
					"policy_reason": policyReason,
					"policy_penalty": policyPenalty,
					"decision_path": "runtime_scout_relations",
					"confidence":    canon.ConfidenceMult,
					})
				}
			}
			effectiveQualifiers, _ := withDeterministicDirectionalRoleQualifiers(edgeType, srcTypeActual, dstTypeActual, rel.Qualifiers)
			// Policy: direction validation and reversal (enforced for known edge types).
			// Use unified ValidationPolicy if available, otherwise fallback to hardcoded rules
			if e.validationPolicy != nil && sourceFragment != nil {
			logPrefix := "[ScoutEngine:processRelations]"
			policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageInput, map[string]interface{}{
				"trace_id":      "na",
				"tenant_id":     job.TenantID,
				"job_id":        job.ID,
				"source_type":   srcTypeActual,
				"target_type":   dstTypeActual,
				"relation_type": edgeType,
				"action":        "validate_edge",
				"policy_status": "na",
				"policy_reason": "",
				"policy_penalty": 0,
				"decision_path": "runtime",
				"confidence":    0,
			})

			decision, err := e.validationPolicy.Execute(ctx, policy.PolicyInput{
				Kind: "edge",
				Context: map[string]interface{}{
					"validationKind":   "edge",
					"sourceFragmentID": edgeSourceID,
					"targetFragmentID": edgeTargetID,
					"sourceType":       string(srcTypeActual),
					"targetType":       string(dstTypeActual),
					"sourceCanonicalPath": srcPathActual,
						"targetCanonicalPath": dstPathActual,
						"relationType":     string(edgeType),
						"reason":           strings.TrimSpace(rel.Reason),
						"qualifiers":       effectiveQualifiers,
					},
				// IMPORTANT: do not pass Fragment here, or ValidationPolicy will overwrite sourceType from it,
				// which can be incorrect after edge normalization swaps.
				Fragment: nil,
			})

				if err != nil {
					logGroundingErrorWithContext(job, "scout", "relations.validation_policy", "validation_policy_failed", "policy_execution_error", true, err)
					e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.validation_policy", "validation_policy_failed", "policy_execution_error", true)
				} else {
				decisionReason := ""
				if reason, _ := decision.Params["reason"].(string); strings.TrimSpace(reason) != "" {
					decisionReason = strings.TrimSpace(reason)
				}
				policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageDecision, map[string]interface{}{
					"trace_id":      "na",
					"tenant_id":     job.TenantID,
					"job_id":        job.ID,
					"edge_id":       "",
					"source_type":   srcTypeActual,
					"target_type":   dstTypeActual,
					"relation_type": edgeType,
					"action":        decision.Action,
					"policy_status": policyStatus,
					"policy_reason": decisionReason,
					"policy_penalty": policyPenalty,
					"decision_path": decision.DecisionPath,
					"confidence":    decision.Confidence,
				})
				if modality, _ := decision.Params["edgeModality"].(string); strings.TrimSpace(modality) != "" {
					policyEdgeModality = strings.TrimSpace(modality)
				}

				policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
					Reverse: func() {
						edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
						srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
						srcPathActual, dstPathActual = dstPathActual, srcPathActual
						directionChanged = true
						edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
					},
					RewriteType: func(newType string, _ string) {
						newEdgeType := models.DependencyType(strings.TrimSpace(newType))
						if newEdgeType == "" || newEdgeType == edgeType {
							return
						}
						canonFromEdgeType = string(edgeType)
						edgeType = newEdgeType
						directionChanged = true
						edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
					},
					ApplyPenalty: func(penalty float64, _ string) {
						relConfidence = relConfidence * (1.0 - penalty)
						if relConfidence < 0 {
							relConfidence = 0
						}
					},
					SetPolicyStatus: func(status, reason string, penalty float64) {
						policyStatus = policy.MergePolicyStatus(policyStatus, status)
						if strings.TrimSpace(reason) != "" {
							policyReason = strings.TrimSpace(reason)
						}
						if penalty > policyPenalty {
							policyPenalty = penalty
						}
					},
				})
			}
		} else if sourceFragment != nil {
			// Policy-level direction validation (replaces direct edgesemantics call).
			st := srcTypeActual
			tt := dstTypeActual
			if st == "" {
				st = sourceFragment.Type
			}
			if tt == "" {
				tt = targetType
			}
			valid, shouldReverse, dirStatus, dirReason, dirPenalty := policy.ApplyEdgeDirectionValidation(edgeType, st, tt)
			if !valid {
				if shouldReverse {
					edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
					srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
					srcPathActual, dstPathActual = dstPathActual, srcPathActual
					directionChanged = true
					edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
				} else {
					policyStatus = policy.MergePolicyStatus(policyStatus, dirStatus)
					policyReason = dirReason
					if dirPenalty > policyPenalty {
						policyPenalty = dirPenalty
					}
					relConfidence = relConfidence * (1.0 - dirPenalty)
				}
			}
		}

		// Fail-closed for invalid edge semantics: quarantined edges must not be persisted.
		if policyStatus == models.PolicyStatusQuarantined {
			msg := fmt.Sprintf("[ScoutEngine] Skipping quarantined edge source=%s target=%s type=%s reason=%s",
				strings.TrimSpace(srcPathActual),
				strings.TrimSpace(dstPathActual),
				string(edgeType),
				strings.TrimSpace(policyReason),
			)
			fmt.Printf("%s\n", msg)
			e.logLine(ctx, job, "warn", msg)
			continue
		}

		// In-memory dedup (per job run), after all normalization.
		ek := EdgeKey(edgeSourceID, edgeTargetID, edgeType)
		if e.seenEdgeDedupRecently(ek) {
			fmt.Printf("Skipping duplicate edge (cross-job recent cache): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}
		if _, ok := seenEdgeKeys[ek]; ok {
			fmt.Printf("Skipping duplicate edge (in-memory): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			e.rememberEdgeDedupRecently(ek)
			continue
		}
		seenEdgeKeys[ek] = struct{}{}

		// Firestore dedup check (cross-run).
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edgeSourceID, edgeTargetID, edgeType); err == nil {
			fmt.Printf("Skipping duplicate edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			e.rememberEdgeDedupRecently(ek)
			continue
		}

		// Resolve actual canonical paths after all normalizations (sourceID/targetID may have been swapped).
		resolvedSourcePath := ""
		resolvedTargetPath := ""
		if edgeSourceID == sourceFragment.ID {
			resolvedSourcePath = sourceFragment.CanonicalPath
		} else if targetFragment != nil && edgeSourceID == targetFragment.ID {
			resolvedSourcePath = targetFragment.CanonicalPath
		} else if e.fragmentsRepo != nil {
			if f, _ := e.fragmentsRepo.Get(ctx, edgeSourceID); f != nil {
				resolvedSourcePath = f.CanonicalPath
			}
		}
		if edgeTargetID == targetFragment.ID && targetFragment != nil {
			resolvedTargetPath = targetFragment.CanonicalPath
		} else if edgeTargetID == sourceFragment.ID {
			resolvedTargetPath = sourceFragment.CanonicalPath
		} else if e.fragmentsRepo != nil {
			if f, _ := e.fragmentsRepo.Get(ctx, edgeTargetID); f != nil {
				resolvedTargetPath = f.CanonicalPath
			}
		}
		// Fallback to original paths if resolution failed
		if resolvedSourcePath == "" {
			resolvedSourcePath = sourceFragment.CanonicalPath
		}
		if resolvedTargetPath == "" {
			resolvedTargetPath = targetPath
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: edgeSourceID,
			TargetFragmentID: edgeTargetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   normalizeEdgeConfidence(relConfidence),
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Qualifiers: map[string]interface{}{
				"reason":                 strings.TrimSpace(rel.Reason),
				"rawRelationType":        rawRelationType,
				"normalizedRelationType": normalizedRelationType,
				"rawConfidence":          relConfidence,
				"targetName":             strings.TrimSpace(rel.TargetName),
				"targetCanonicalPath":    strings.TrimSpace(resolvedTargetPath),
				"sourceCanonicalPath":    strings.TrimSpace(resolvedSourcePath),
				"groundingMatchReason":   strings.TrimSpace(groundingMatchReason),
			},
			Provenance: models.Provenance{
				Method:    provenanceMethod,
				SourceIDs: []string{job.ID},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     time.Now(),
				LastUpdatedAt: time.Now(),
			},
		}
		applyGroundingProvenance(&edge.Provenance, runtimeGroundingEvidence, nil, nil, nil)
		if canonFromEdgeType != "" {
			edge.Qualifiers["canonicalized"] = true
			edge.Qualifiers["canonicalizedFromEdgeType"] = canonFromEdgeType
			edge.Qualifiers["canonicalizationReason"] = canonReason
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
			if policyStatus == models.PolicyStatusClean {
				policyStatus = models.PolicyStatusAutoFixed
			}
			if policyReason == "" {
				policyReason = strings.TrimSpace(canonReason)
			}
		}
		// Temporal validity (real-world effective time) — fail-open:
		// If parsing fails, keep edge without dates rather than dropping it.
		edge.ValidFrom = parseOptionalISODate(rel.ValidFrom)
		edge.ValidTo = parseOptionalISODate(rel.ValidTo)

		// Edge metrics (quantitative attributes) — keep even if not allowlisted (fail-open).
		if len(rel.Metrics) > 0 {
			edge.Metrics = rel.Metrics
		}

		if len(baseQualifiers) > 0 {
			for k, v := range baseQualifiers {
				if _, exists := edge.Qualifiers[k]; !exists {
					edge.Qualifiers[k] = v
				}
			}
		}
		// Merge structured qualifiers from LLM (edgeModality, eventId, round, etc.) without overwriting debug/base fields.
			if len(effectiveQualifiers) > 0 {
				edge.Qualifiers = mergeQualifiers(edge.Qualifiers, effectiveQualifiers)
			}
		if strings.TrimSpace(policyEdgeModality) != "" {
			if _, exists := edge.Qualifiers["edgeModality"]; !exists {
				edge.Qualifiers["edgeModality"] = policyEdgeModality
			}
		}
		if directionChanged {
			edge.Qualifiers["normalizedDirection"] = true
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
			edge.Qualifiers["normalizedSourceFragmentId"] = edge.SourceFragmentID
			edge.Qualifiers["normalizedTargetFragmentId"] = edge.TargetFragmentID
		}
		if canon.GovRegulatesSwap {
			edge.Qualifiers["canonicalizedGovRegulatesSwap"] = true
			edge.Qualifiers["govRegulatesSwapReason"] = canon.GovRegulatesSwapReason
		}
		edge.SetPolicyStatus(policyStatus, policyReason, policyPenalty)
		if canonFromEdgeType != "" {
			edge.SetPolicyOriginalType(models.DependencyType(canonFromEdgeType))
		}
		applyBestEffortEdgeEnrichment(edge, srcPath, dstPath)

		// Check if edge already exists to prevent duplicates (idempotency)
		if e.edgesRepo != nil {
			if existing, _ := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edge.SourceFragmentID, edge.TargetFragmentID, edge.Type); existing != nil {
				// Edge exists: update properties/qualifiers instead of creating new
				// Or skip if identical. For now, we skip to be safe and simple.
				// In future, we might want to merge confidence or update timestamps.
				e.rememberEdgeDedupRecently(EdgeKey(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type))
				continue
			}
		}

		// Используем Create, предполагая что репо обработает дубликаты или мы просто создаем новые
		// В реальной системе нужен UpsertEdge
		if id, err := e.edgesRepo.Create(ctx, edge); err != nil {
			// Historically this was swallowed, which can lead to "0 edges" while nodes exist.
			// Log loudly to make root cause visible (validation, Firestore indexes, permissions, etc.).
			msg := fmt.Sprintf("Failed to create edge %s -> %s (%s) confidence=%.4f rawConfidence=%.4f rawType=%q: %v",
				strings.TrimSpace(sourceFragment.CanonicalPath),
				strings.TrimSpace(targetPath),
				string(edgeType),
				edge.Properties.Criticality,
				relConfidence,
				strings.TrimSpace(rel.RelationType),
				err,
			)
			fmt.Printf("[ScoutEngine] %s\n", msg)
			e.logLine(ctx, job, "warn", msg)
		} else {
			edge.ID = id
			e.rememberEdgeDedupRecently(EdgeKey(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type))
			fmt.Printf("Created edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			// Realtime: edge materialized/updated for UI graph.
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":            job.TenantID,
				"projectId":           job.ProjectID,
				"lensId":              strings.TrimSpace(job.LensID),
				"edgeId":              edge.ID,
				"sourceFragmentId":    edge.SourceFragmentID,
				"targetFragmentId":    edge.TargetFragmentID,
				"sourceCanonicalPath": strings.TrimSpace(sourceFragment.CanonicalPath),
				"targetCanonicalPath": strings.TrimSpace(targetPath),
				"sourceLabel":         strings.TrimSpace(sourceFragment.Labels["en"]),
				"targetLabel":         strings.TrimSpace(firstNonEmpty(rel.TargetShortName, rel.TargetName, rel.TargetLegalName)),
				"type":                string(edge.Type),
				"properties":          edge.Properties,
				"qualifiers":          edge.Qualifiers,
			})
		}
		*/
	}
	// Flush buffered edges in a single batch write.
	if edgeBuf.Len() > 0 {
		if flushErr := edgeBuf.Flush(ctx, e); flushErr != nil {
			fmt.Printf("[ScoutEngine] edge batch flush error: %v\n", flushErr)
			if job != nil {
				if job.State == nil {
					job.State = map[string]interface{}{}
				}
				job.State["edgeBatchFlushError"] = true
				job.State["edgeBatchFlushErrorMessage"] = strings.TrimSpace(flushErr.Error())
				recordPipelineIssue(job, "edge_batch_flush", flushErr.Error())
				e.persistPipelineRuntimeState(ctx, job)
				if e.scoutingJobsRepo != nil {
					_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
						"edgeBatchFlushError":        true,
						"edgeBatchFlushErrorMessage": strings.TrimSpace(flushErr.Error()),
						stateKeyPipelineIssues:       job.State[stateKeyPipelineIssues],
					})
				}
			}
		}
	}
	if job != nil {
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		prevResolved := 0
		prevCreated := 0
		prevRedirects := 0
		if raw, ok := job.State[stateKeyResolutionStats]; ok {
			if m, ok := raw.(map[string]interface{}); ok && m != nil {
				prevResolved = parseStateInt(m["relationsResolvedByGrounding"], 0)
				prevCreated = parseStateInt(m["relationsCreatedNew"], 0)
				prevRedirects = parseStateInt(m["dedupRedirectsByGrounding"], 0)
			}
		}
		job.State[stateKeyGroundingContractVer] = groundingContractVersionV1
		job.State[stateKeyResolutionStats] = map[string]interface{}{
			"relationsResolvedByGrounding": prevResolved + relationsResolvedByGrounding,
			"relationsCreatedNew":          prevCreated + relationsCreatedNew,
			"dedupRedirectsByGrounding":    prevRedirects + dedupRedirectsByGrounding,
		}
		if groundingCtx != nil {
			originMode := groundingOriginModeFromEvidence(groundingCtx.HasDatastoreResults, groundingCtx.HasWebResults)
			if originMode != "" {
				job.State[stateKeyOriginMode] = originMode
			}
		}
		if strings.TrimSpace(job.TenantID) != "" || e != nil {
			for reason, n := range groundingReasonCounts {
				if strings.TrimSpace(reason) == "" || n <= 0 {
					continue
				}
				e.emitGroundingCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_grounding_relations_resolved_total", map[string]string{
					"reason": strings.TrimSpace(reason),
				}, map[string]int64{"count": n})
			}
			if relationsCreatedNew > 0 {
				origin := parseStateString(job.State[stateKeyOriginMode])
				if strings.TrimSpace(origin) == "" {
					origin = "unknown"
				}
				e.emitGroundingCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_grounding_new_fragments_total", map[string]string{
					"origin": strings.TrimSpace(origin),
				}, map[string]int64{"count": int64(relationsCreatedNew)})
			}
			if dedupFalsePositiveSuspected > 0 {
				e.emitGroundingCounter(ctx, strings.TrimSpace(job.TenantID), "horizon_dedup_false_positive_suspected_total", nil, map[string]int64{
					"count": int64(dedupFalsePositiveSuspected),
				})
			}
		}
	}
	if len(relations) > 0 && scheduledCount == 0 && allowRelationChildren {
		e.logLine(ctx, job, "info", fmt.Sprintf("> relation_child_scheduling_summary: %d relations processed, 0 child jobs scheduled (filtered by shouldRecurse/shouldScout/visited/cap)",
			len(relations)))
	}
	return nil
}

// runStrategicOverlayCritic runs Critic/Research over strategic overlay nodes (decision points, mechanisms).
// It uses missingInfo to trigger best-effort enrichment in Phase 2 without changing global policy rules.
func (e *ScoutEngine) runStrategicOverlayCritic(
	ctx context.Context,
	job *models.ScoutingJob,
	graphContext string,
	fragments map[string]*models.HorizonFragment,
	relationsBySource map[string][]agents.DiscoveredRelation,
) (int, int) {
	if e == nil || job == nil || e.criticAgent == nil || e.researchAgent == nil {
		return 0, 0
	}
	tenantID := strings.TrimSpace(job.TenantID)
	projectID := strings.TrimSpace(job.ProjectID)
	lensID := strings.TrimSpace(job.LensID)
	cascadeID := ""
	if job.State != nil {
		if v, ok := job.State["cascadeId"].(string); ok {
			cascadeID = strings.TrimSpace(v)
		}
	}

	enriched := 0
	addedEdges := 0

	for _, frag := range fragments {
		if frag == nil {
			continue
		}
		if frag.Type != models.FragmentTypeDecisionPoint && frag.Type != models.FragmentTypeMechanism {
			continue
		}
		rel := relationsBySource[strings.TrimSpace(frag.CanonicalPath)]
		// Use dedicated Phase 2 strategic validation (focused prompt, no org/fac/person rules).
		validation, err := e.criticAgent.ValidateStrategicOverlay(ctx, frag, rel, graphContext)
		if err != nil || validation == nil {
			continue
		}
		if len(validation.MissingInfo) == 0 {
			continue
		}
		missing := validation.MissingInfo
		// Filter out exhausted/cooldown missingInfo items (reuse global repo).
		if e.missingInfoStatusRepo != nil && strings.TrimSpace(frag.ID) != "" {
			searchable, _, err := e.missingInfoStatusRepo.FilterSearchable(ctx, strings.TrimSpace(frag.ID), missing)
			if err == nil && len(searchable) > 0 {
				missing = searchable
			}
		}
		if len(missing) == 0 {
			continue
		}

		// Run grounded research to enrich strategic fragment.
		researchCtx := ai.WithUsageContext(ctx, tenantID, "SYSTEM", projectID, "horizon.strategic.research", lensID, cascadeID)
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
		enrichedFrag, newRelations, _, evidence, _, err := e.researchAgent.EnrichFragmentWithGrounding(
			researchCtx,
			frag,
			missing,
			"",
			nil,
			nil,
			[]string{"strategic"},
			"",
			"",
			nil,
			existingEntities,
		)
		if err != nil {
			continue
		}
		if internalFirstGroundingEnabled() && evidence != nil {
			applyGroundingEvidenceToJobState(job, evidence, nil, nil)
			logGroundingInfof("strategic research grounding: jobId=%s fragmentId=%s target=%s datastore=%v(%d) web=%v(%d)",
				strings.TrimSpace(job.ID),
				strings.TrimSpace(frag.ID),
				strings.TrimSpace(frag.CanonicalPath),
				evidence.HasDatastoreResults,
				len(evidence.DatastoreFragmentIDs),
				evidence.HasWebResults,
				len(evidence.WebSources),
			)
		}

		// Sanitize edges from Research Agent (same rules as Overlay Agent).
		// Research might hallucinate USES_MECHANISM -> DECISION_POINT or other invalid paths.
		sanitizedRelations := make([]agents.DiscoveredRelation, 0, len(newRelations))
		for _, rel := range newRelations {
			targetPath := strings.TrimSpace(rel.TargetCanonicalPath)
			rt := rel.RelationType

			// FIX: USES_MECHANISM -> DECISION_POINT is invalid policy.
			// If actor controls decision, it must be GATEKEEPS_DECISION.
			if rt == "USES_MECHANISM" && strings.HasPrefix(targetPath, "/decision/") {
				rel.RelationType = "GATEKEEPS_DECISION"
				rt = "GATEKEEPS_DECISION"
			}

			// DROP: USES_MECHANISM must target /mechanism/...
			if rt == "USES_MECHANISM" && !strings.HasPrefix(targetPath, "/mechanism/") {
				continue
			}
			// DROP: APPLIES_TO must target entities, not other mechanisms (usually)
			if rt == "APPLIES_TO" && strings.HasPrefix(targetPath, "/mechanism/") {
				continue
			}

			sanitizedRelations = append(sanitizedRelations, rel)
		}
		newRelations = sanitizedRelations

		if enrichedFrag != nil && strings.TrimSpace(enrichedFrag.ID) != "" {
			_ = e.fragmentsRepo.Update(ctx, enrichedFrag)
		}
		if len(newRelations) > 0 && enrichedFrag != nil {
			if err := e.processRelationsInternal(ctx, job, enrichedFrag, newRelations, "strategic_overlay", map[string]interface{}{"source": "strategic_overlay", "domainOverlay": true}, nil); err == nil {
				addedEdges += len(newRelations)
			}
		}
		enriched++

		if e.missingInfoStatusRepo != nil && strings.TrimSpace(frag.ID) != "" {
			foundSomething := len(newRelations) > 0
			for _, q := range missing {
				_ = e.missingInfoStatusRepo.RecordAttempt(ctx, strings.TrimSpace(frag.ID), q, foundSomething, 0, "")
			}
		}
	}

	return enriched, addedEdges
}

// NOTE: legacy sanitizePathName removed.

// normalizeRelationEdge enforces directional semantics for common relation types.
// It returns possibly swapped source/target IDs and/or adjusted edge type.
// qualifiers may contain source_role/target_role; when both endpoints are ORG and roles
// indicate inversion (e.g. source=owned_entity, target=owner for OWNS), direction is swapped.
// This is intentionally conservative: if unsure, it leaves the relation as-is.
func normalizeRelationEdge(sourceFrag *models.HorizonFragment, sourceID, targetID string, edgeType models.DependencyType, targetType models.FragmentType, enableManagement bool, qualifiers map[string]interface{}) (string, string, models.DependencyType, bool) {
	if sourceFrag == nil {
		return sourceID, targetID, edgeType, false
	}
	srcType := sourceFrag.Type
	isGeo := func(t models.FragmentType) bool { return t == models.FragmentTypeGeoRegion }
	isOrg := func(t models.FragmentType) bool { return t == models.FragmentTypeOrganization }
	isFac := func(t models.FragmentType) bool { return t == models.FragmentTypeFacility }
	isProd := func(t models.FragmentType) bool { return t == models.FragmentTypeProduct }
	isPerson := func(t models.FragmentType) bool { return t == models.FragmentTypePerson }
	isCommunity := func(t models.FragmentType) bool { return t == models.FragmentTypeCommunity }

	changed := false

	// Role-based direction correction for ORG->ORG edges when LLM outputs inverted direction.
	// E.g. PIF owns Noon, but LLM outputs Noon->PIF with source_role=owned_entity, target_role=owner.
	if isOrg(srcType) && isOrg(targetType) && isRoleInvertedForDirection(edgeType, qualifiers) {
		sourceID, targetID = targetID, sourceID
		changed = true
	}

	switch edgeType {
	case models.DependencyTypeOperatesIn:
		// "X operates in Y": X is typically ORG/FAC, Y is GEO.
		if isGeo(srcType) && (isOrg(targetType) || isFac(targetType)) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeLocatedIn:
		// "X located in Y": X is non-geo, Y is GEO. Geo->Geo can be used for containment.
		if isGeo(srcType) && !isGeo(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeRegulates:
		// GEO->GEO is almost certainly containment, not regulation.
		if isGeo(srcType) && isGeo(targetType) {
			edgeType = models.DependencyTypeLocatedIn
			changed = true
		}
		// GEO->ORG/FAC likely means the org/fac operates in the geo.
		if isGeo(srcType) && (isOrg(targetType) || isFac(targetType)) {
			sourceID, targetID = targetID, sourceID
			edgeType = models.DependencyTypeOperatesIn
			changed = true
		}
	case models.DependencyTypeOwns:
		// Ownership is never "X OWNS a person" in our graph semantics.
		// If target is a PERSON and source is an ORG, assume inversion and swap.
		if isOrg(srcType) && isPerson(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
			break
		}
		// If it's clearly inverted, swap (e.g., FAC owns ORG / PRODUCT owns ORG).
		if !isOrg(srcType) && !isPerson(srcType) && !isCommunity(srcType) &&
			(isOrg(targetType) || isPerson(targetType)) && !isGeo(srcType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeProduces:
		// "X produces Y": X is ORG/FAC, Y is PRODUCT.
		if isProd(srcType) && !isProd(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeSuppliesTo:
		// "X supplies to Y": X is ORG (supplier), Y can be ORG/FAC/PRODUCT/GEO (recipient).
		// If GEO -> ORG/FAC/PRODUCT, it's almost certainly inverted.
		if isGeo(srcType) && (isOrg(targetType) || isFac(targetType) || isProd(targetType)) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeManages, models.DependencyTypeEmployedBy, models.DependencyTypeBoardMemberOf, models.DependencyTypeFounded:
		// Leadership/people relationships are directional and typically originate from PERSON -> ORG/COMMUNITY.
		// FOUNDED: Person founds org/community, never the reverse. Always swap COMMUNITY/ORG -> PERSON.
		if edgeType == models.DependencyTypeFounded &&
			(srcType == models.FragmentTypeCommunity || isOrg(srcType)) && isPerson(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		} else if enableManagement && isOrg(srcType) && isPerson(targetType) {
			// When scouting an ORGANIZATION, LLM output may attach PERSON targets with these types.
			sourceID, targetID = targetID, sourceID
			changed = true
		}

	// === STRATEGIC EDGE TYPE NORMALIZATION (Phase 2) ===
	// These edge types have strict directional semantics that must be enforced.

	case models.DependencyTypeGatekeepsDecision:
		// GATEKEEPS_DECISION: source MUST be ORG/GOV/COMMUNITY, target MUST be DECISION_POINT
		// If source is DECISION_POINT, the edge is inverted and must be swapped.
		isDecisionPoint := func(t models.FragmentType) bool { return t == models.FragmentTypeDecisionPoint }
		if isDecisionPoint(srcType) && (isOrg(targetType) || targetType == models.FragmentTypeCommunity) {
			// Inverted: DECISION_POINT → ORG [GATEKEEPS_DECISION] should be ORG → DECISION_POINT
			sourceID, targetID = targetID, sourceID
			changed = true
		}
		// Also catch MECHANISM as source (completely invalid)
		isMechanism := func(t models.FragmentType) bool { return t == models.FragmentTypeMechanism }
		if isMechanism(srcType) {
			// MECHANISM cannot gatekeep decisions. This is likely a semantic error.
			// Swap and hope target is a decision point.
			sourceID, targetID = targetID, sourceID
			changed = true
		}

	case models.DependencyTypeUsesMechanism:
		// USES_MECHANISM: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY, target MUST be MECHANISM
		// If source is MECHANISM and target is something else, swap.
		isMechanism := func(t models.FragmentType) bool { return t == models.FragmentTypeMechanism }
		if isMechanism(srcType) && !isMechanism(targetType) {
			// Inverted: MECHANISM → DECISION_POINT [USES_MECHANISM] should be DECISION_POINT → MECHANISM
			sourceID, targetID = targetID, sourceID
			changed = true
		}

	case models.DependencyTypeAppliesTo:
		// APPLIES_TO: source MUST be MECHANISM, target MUST be entity (ORG/FACILITY/PRODUCT/MARKET)
		// If source is NOT MECHANISM and target IS MECHANISM, swap.
		isMechanism := func(t models.FragmentType) bool { return t == models.FragmentTypeMechanism }
		if !isMechanism(srcType) && isMechanism(targetType) {
			// Inverted: ORG → MECHANISM [APPLIES_TO] should be MECHANISM → ORG
			sourceID, targetID = targetID, sourceID
			changed = true
		}

	case models.DependencyTypeControlsAccess, models.DependencyTypeImposesCondition:
		// CONTROLS_ACCESS / IMPOSES_CONDITION: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY
		// Target MUST be entity (ORG/FACILITY/PRODUCT/MARKET/PROGRAM)
		// If source is a "controlled" entity type and target is a controller type, swap.
		isController := func(t models.FragmentType) bool {
			return t == models.FragmentTypeDecisionPoint || isOrg(t) || t == models.FragmentTypeCommunity
		}
		isControlled := func(t models.FragmentType) bool {
			return isFac(t) || isProd(t) || t == models.FragmentTypeMarket || t == models.FragmentTypeProgram
		}
		if isControlled(srcType) && isController(targetType) {
			// Inverted: FACILITY → ORG [CONTROLS_ACCESS] should be ORG → FACILITY
			sourceID, targetID = targetID, sourceID
			changed = true
		}

	case models.DependencyTypeAdministers:
		// ADMINISTERS: source MUST be ORGANIZATION/GEO_REGION. FACILITY and DEPOSIT cannot administer.
		isDeposit := func(t models.FragmentType) bool { return t == models.FragmentTypeDeposit }
		tgtValid := isOrg(targetType) || targetType == models.FragmentTypeProgram || targetType == models.FragmentTypeGeoRegion
		if (isFac(srcType) || isDeposit(srcType)) && tgtValid {
			sourceID, targetID = targetID, sourceID
			changed = true
		}

	case models.DependencyTypeControls:
		// CONTROLS: source MUST be ORG/PERSON/COMMUNITY. PRODUCT cannot control.
		if isProd(srcType) && (isOrg(targetType) || isFac(targetType)) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	}

	return sourceID, targetID, edgeType, changed
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// canonicalizeQualifierFacilityPaths resolves at_facility and transport_via paths through the resolver.
// Qualifier objects are fragments; same canonicalization and deduplication rules apply.
func (e *ScoutEngine) canonicalizeQualifierFacilityPaths(ctx context.Context, qualifiers map[string]interface{}, pathRewrites map[string]string) map[string]interface{} {
	if qualifiers == nil {
		return qualifiers
	}
	out := make(map[string]interface{}, len(qualifiers))
	for k, v := range qualifiers {
		out[k] = v
	}
	resolvePath := func(p string) string {
		p = strings.TrimSpace(p)
		if p == "" || !strings.HasPrefix(p, "/fac/") {
			return p
		}
		if rp, ok := pathRewrites[p]; ok && strings.TrimSpace(rp) != "" {
			return strings.TrimSpace(rp)
		}
		if e.canonicalPathResolver != nil {
			if resolved, err := e.canonicalPathResolver.Resolve(ctx, p, models.FragmentTypeFacility); err == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				return strings.TrimSpace(resolved.CanonicalPath)
			}
		}
		return p
	}
	if v := out[models.QualifierAtFacility]; v != nil {
		if s, ok := v.(string); ok {
			out[models.QualifierAtFacility] = resolvePath(s)
		}
	}
	if v := out[models.QualifierTransportVia]; v != nil {
		switch arr := v.(type) {
		case []interface{}:
			canon := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					if r := resolvePath(s); r != "" {
						canon = append(canon, r)
					}
				}
			}
			out[models.QualifierTransportVia] = canon
		case []string:
			canon := make([]string, 0, len(arr))
			for _, s := range arr {
				if r := resolvePath(s); r != "" {
					canon = append(canon, r)
				}
			}
			out[models.QualifierTransportVia] = canon
		}
	}
	return out
}

func routeCoordKey(lat, lon float64) string {
	return fmt.Sprintf("%.5f,%.5f", lat, lon)
}

func locCountryToken(path string) string {
	p := strings.TrimSpace(strings.ToLower(path))
	if !strings.HasPrefix(p, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}
	cc := strings.TrimSpace(parts[1])
	if cc == "" || cc == "na" {
		return ""
	}
	if len(cc) == 2 || taxonomy.IsRegionalGeoToken(cc) || taxonomy.IsCompositeGeoToken(cc) {
		return cc
	}
	return ""
}

func (e *ScoutEngine) enrichRouteFacilityGeoContext(ctx context.Context, fragment *models.HorizonFragment, scoutResult *agents.ScoutResult) bool {
	if e == nil || fragment == nil || e.fragmentsRepo == nil {
		return false
	}
	if !isRouteFacilityFragment(fragment) {
		return false
	}

	changed := false
	if e.geocodingClient != nil {
		if applied, _ := henrich.EnrichCoordinatesFromGeocoding(ctx, e.geocodingClient, fragment); applied {
			changed = true
		}
	}

	transportRefs := make([]string, 0, 16)
	regionPaths := make([]string, 0, 16)
	if fragment.Qualifiers != nil {
		for _, key := range []string{"route", "route_path", "routePath", "route_waypoints", "routeWaypoints"} {
			transportRefs = append(transportRefs, parseStateStringSlice(fragment.Qualifiers[key])...)
		}
		transportRefs = append(transportRefs, parseStateStringSlice(fragment.Qualifiers[models.QualifierTransportVia])...)
	}
	if scoutResult != nil {
		fragCP := strings.TrimSpace(fragment.CanonicalPath)
		for _, rel := range scoutResult.Relations {
			rt := strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
			src := strings.TrimSpace(rel.SourceCanonicalPath)
			fromFragment := src == "" || src == fragCP
			if !fromFragment {
				continue
			}
			targetPath := strings.TrimSpace(rel.TargetCanonicalPath)
			if (rt == string(models.DependencyTypeLocatedIn) || rt == string(models.DependencyTypeOperatesIn)) &&
				strings.HasPrefix(targetPath, "/loc/") {
				regionPaths = append(regionPaths, targetPath)
			}
			if rel.Qualifiers != nil {
				transportRefs = append(transportRefs, parseStateStringSlice(rel.Qualifiers[models.QualifierTransportVia])...)
			}
		}
		for _, ch := range scoutResult.SupplyChains {
			anchored := false
			for _, n := range ch.Nodes {
				if strings.TrimSpace(n.CanonicalPath) == fragCP {
					anchored = true
					break
				}
			}
			if anchored {
				transportRefs = append(transportRefs, ch.TransportVia...)
			}
		}
	}
	transportRefs = appendUniqueTrim(nil, transportRefs...)
	regionPaths = appendUniqueTrim(nil, regionPaths...)

	for _, ref := range transportRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "/loc/") {
			regionPaths = append(regionPaths, ref)
		}
		if strings.HasPrefix(ref, "/fac/") || strings.HasPrefix(ref, "/loc/") {
			if waypoint, err := e.fragmentsRepo.GetByCanonicalPath(ctx, ref); err == nil && waypoint != nil {
				if e.geocodingClient != nil {
					if applied, _ := henrich.EnrichCoordinatesFromGeocoding(ctx, e.geocodingClient, waypoint); applied {
						waypoint.Metadata.LastUpdatedAt = time.Now().UTC()
						_ = e.fragmentsRepo.Update(ctx, waypoint)
					}
				}
				if waypoint.HomeLocation != nil && waypoint.HomeLocation.ISO3166_1 != nil {
					cc := strings.ToLower(strings.TrimSpace(*waypoint.HomeLocation.ISO3166_1))
					if cc != "" && cc != "na" {
						regionPaths = append(regionPaths, "/loc/"+cc)
					}
				}
			}
			continue
		}
	}

	regionPaths = appendUniqueTrim(nil, regionPaths...)
	sort.Strings(regionPaths)
	countries := make([]string, 0, len(regionPaths))
	for _, rp := range regionPaths {
		if cc := locCountryToken(rp); cc != "" {
			countries = append(countries, cc)
		}
	}
	countries = appendUniqueTrim(nil, countries...)
	sort.Strings(countries)

	wm := fragment.EffectiveHorizonMonitor()
	if wm == nil {
		fragment.HorizonMonitor = &models.HorizonMonitorFragmentData{}
		wm = fragment.HorizonMonitor
		changed = true
	}
	resolvedRoute := pathbuilder.ResolveSupplyChainRouteGeometryDetailed(ctx, e.fragmentsRepo, transportRefs)
	if len(resolvedRoute.Coordinates) >= 2 && strings.TrimSpace(resolvedRoute.Source) != "fragmentary_waypoint_sequence" {
		routeCoords := make([]models.HorizonMonitorCoordinates, 0, len(resolvedRoute.Coordinates))
		for _, c := range resolvedRoute.Coordinates {
			routeCoords = append(routeCoords, models.HorizonMonitorCoordinates{Lat: c.Lat, Lon: c.Lon})
		}
		nextRouteKeys := make([]string, 0, len(routeCoords))
		prevRouteKeys := make([]string, 0, len(wm.Route))
		for _, c := range routeCoords {
			nextRouteKeys = append(nextRouteKeys, routeCoordKey(c.Lat, c.Lon))
		}
		for _, c := range wm.Route {
			prevRouteKeys = append(prevRouteKeys, routeCoordKey(c.Lat, c.Lon))
		}
		if !slicesEqual(nextRouteKeys, prevRouteKeys) {
			wm.Route = routeCoords
			changed = true
		}
	}
	prevCountries := appendUniqueTrim(nil, wm.Countries...)
	sort.Strings(prevCountries)
	if len(countries) > 0 && !slicesEqual(countries, prevCountries) {
		wm.Countries = countries
		changed = true
	}
	if len(regionPaths) > 0 {
		if fragment.Qualifiers == nil {
			fragment.Qualifiers = map[string]interface{}{}
		}
		prev := appendUniqueTrim(nil, parseStateStringSlice(fragment.Qualifiers["route_region_paths"])...)
		sort.Strings(prev)
		if !slicesEqual(regionPaths, prev) {
			fragment.Qualifiers["route_region_paths"] = regionPaths
			changed = true
		}
	}
	if len(resolvedRoute.Coordinates) > 0 && strings.TrimSpace(resolvedRoute.Source) != "fragmentary_waypoint_sequence" && !models.HasRealGeoCoordinates(fragment.Coordinates) {
		fragment.Coordinates = &models.Coordinates{Lat: resolvedRoute.Coordinates[0].Lat, Lon: resolvedRoute.Coordinates[0].Lon}
		changed = true
	}
	if changed {
		fragment.Metadata.LastUpdatedAt = time.Now().UTC()
		fragment.SyncHorizonMonitorAliases()
	}
	return changed
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func extractCanonicalPathsFromHint(hint string) []string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return nil
	}
	tokens := strings.FieldsFunc(hint, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case ',', ';', ':', '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'', '`':
			return true
		default:
			return false
		}
	})
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(strings.Trim(tok, ".,!?"))
		if tok == "" || !strings.HasPrefix(tok, "/") {
			continue
		}
		if strings.Count(tok, "/") < 2 {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

func mergeUniqueStrings(base []string, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, s := range append(base, extra...) {
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

func equalInterfaceMaps(a, b map[string]interface{}) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

func shouldReplaceMetricValue(existing models.MetricValue, incoming models.MetricValue) bool {
	if incoming.Confidence > existing.Confidence+0.05 {
		return true
	}
	if existing.Value == 0 && incoming.Value != 0 {
		return true
	}
	var oldUnit, newUnit string
	if existing.Unit != nil {
		oldUnit = strings.TrimSpace(*existing.Unit)
	}
	if incoming.Unit != nil {
		newUnit = strings.TrimSpace(*incoming.Unit)
	}
	if oldUnit == "" && newUnit != "" {
		return true
	}
	if !incoming.ObservedAt.IsZero() && incoming.ObservedAt.After(existing.ObservedAt) && incoming.Confidence >= existing.Confidence {
		return true
	}
	return false
}

func mergeSupplyLinkMetricState(dst map[string]models.MetricValue, incoming map[string]models.MetricValue) bool {
	if len(incoming) == 0 {
		return false
	}
	changed := false
	for key, mv := range incoming {
		cur, ok := dst[key]
		if !ok {
			dst[key] = mv
			changed = true
			continue
		}
		if shouldReplaceMetricValue(cur, mv) {
			dst[key] = mv
			changed = true
		}
	}
	return changed
}

func mergeSupplyLinkQualifiers(existing, incoming map[string]interface{}) map[string]interface{} {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(existing)+len(incoming))
	for k, v := range existing {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	for k, v := range incoming {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if k == "routeVariants" {
			mergedVariants := models.MergeSupplyLinkRouteVariants(
				models.ExtractSupplyLinkRouteVariants(out, nil, "", 0),
				models.ExtractSupplyLinkRouteVariants(map[string]interface{}{"routeVariants": v}, nil, "", 0),
			)
			if len(mergedVariants) > 0 {
				out[k] = mergedVariants
				continue
			}
		}
		if k == "flow_edge_contract" {
			// Keep prior context and append new warnings/gaps when possible.
			if oldMap, okOld := out[k].(map[string]interface{}); okOld {
				if newMap, okNew := v.(map[string]interface{}); okNew {
					merged := make(map[string]interface{}, len(oldMap)+len(newMap))
					for mk, mv := range oldMap {
						merged[mk] = mv
					}
					for mk, mv := range newMap {
						switch mk {
						case "parseWarnings", "validationGaps":
							merged[mk] = mergeUniqueStrings(parseStateStringSlice(merged[mk]), parseStateStringSlice(mv))
						default:
							merged[mk] = mv
						}
					}
					out[k] = merged
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

func isSupplyChainPlanMode(state map[string]interface{}) bool {
	if state == nil {
		return false
	}
	if parseStateBool(state["supplyChainPlanMode"], false) {
		return true
	}
	if raw := state["domainResearch"]; raw != nil {
		if m, ok := raw.(map[string]interface{}); ok {
			return parseStateBool(m["planMode"], false)
		}
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			var m map[string]interface{}
			if json.Unmarshal([]byte(s), &m) == nil {
				return parseStateBool(m["planMode"], false)
			}
		}
	}
	return false
}

func supplyLinkValidityFromState(state map[string]models.MetricValue) (*time.Time, *time.Time) {
	if len(state) == 0 {
		return nil, nil
	}
	return parseSupplyLinkMetricTime(state["start"]), parseSupplyLinkMetricTime(state["end"])
}

func parseSupplyLinkMetricTime(metric models.MetricValue) *time.Time {
	if metric.Unit == nil {
		return nil
	}
	raw := strings.TrimSpace(*metric.Unit)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

type supplyLinkMaterializationRef struct {
	ID            string
	CanonicalPath string
	TenantID      string
}

// buildSegmentsFromParsed converts SupplyLinkSegments from a ParsedChainResult into pathbuilder.PathSegment slice,
// applying pathRewrites for canonical path consistency and attaching the materialized supply-link reference.
func buildSegmentsFromParsed(parsed *ParsedChainResult, pathRewrites map[string]string, segmentRefs map[string]supplyLinkMaterializationRef) []pathbuilder.PathSegment {
	if parsed == nil {
		return nil
	}
	segmentCandidates := parsed.SupplyLinkSegments
	if len(segmentCandidates) == 0 {
		segmentCandidates = deriveSupplyLinkSegmentsFromEdges(parsed.Edges)
	}
	dedupedSegments := normalizeAndDedupSupplyLinkSegments(segmentCandidates)
	segments := make([]pathbuilder.PathSegment, 0, len(dedupedSegments))
	for _, seg := range dedupedSegments {
		supplier := strings.TrimSpace(seg.SupplierRef)
		product := strings.TrimSpace(seg.ProductRef)
		customer := strings.TrimSpace(seg.CustomerRef)
		if rp, ok := pathRewrites[supplier]; ok && rp != "" {
			supplier = strings.TrimSpace(rp)
		}
		if rp, ok := pathRewrites[product]; ok && rp != "" {
			product = strings.TrimSpace(rp)
		}
		if rp, ok := pathRewrites[customer]; ok && rp != "" {
			customer = strings.TrimSpace(rp)
		}
		if supplier == "" || product == "" || customer == "" {
			continue
		}
		linkRef := segmentRefs[buildSupplyLinkSegmentLookupKey(supplier, product, customer, seg.TransportViaRefs)]
		viaRefs := make([]string, 0, len(seg.TransportViaRefs))
		for _, via := range seg.TransportViaRefs {
			via = strings.TrimSpace(via)
			if rp, ok := pathRewrites[via]; ok && rp != "" {
				via = strings.TrimSpace(rp)
			}
			if via != "" {
				viaRefs = append(viaRefs, via)
			}
		}
		var supFac, custFac *string
		if seg.SupplierFacilityRef != "" {
			sup := strings.TrimSpace(seg.SupplierFacilityRef)
			if rp, ok := pathRewrites[sup]; ok && rp != "" {
				sup = strings.TrimSpace(rp)
			}
			if sup != "" {
				supFac = &sup
			}
		}
		if seg.CustomerFacilityRef != "" {
			cust := strings.TrimSpace(seg.CustomerFacilityRef)
			if rp, ok := pathRewrites[cust]; ok && rp != "" {
				cust = strings.TrimSpace(rp)
			}
			if cust != "" {
				custFac = &cust
			}
		}
		segments = append(segments, pathbuilder.PathSegment{
			SupplyLinkID:            strings.TrimSpace(linkRef.ID),
			SupplyLinkCanonicalPath: strings.TrimSpace(linkRef.CanonicalPath),
			RouteVariantKey:         models.BuildSupplyLinkRouteVariantKey(viaRefs),
			SupplyLinkTenantID:      strings.TrimSpace(linkRef.TenantID),
			SupplierRef:             supplier,
			ProductRef:              product,
			CustomerRef:             customer,
			SupplierFacilityRef:     supFac,
			CustomerFacilityRef:     custFac,
			TransportViaRefs:        viaRefs,
			SegmentClass:            seg.SegmentClass,
			Confidence:              seg.Confidence,
			TenantID:                "global",
			IsTenantSpecific:        false,
			Metrics:                 seg.State,
		})
	}
	return segments
}

func buildSupplyLinkSegmentLookupKey(supplier, product, customer string, viaRefs []string) string {
	return strings.Join([]string{
		strings.TrimSpace(supplier),
		strings.TrimSpace(product),
		strings.TrimSpace(customer),
		models.BuildSupplyLinkRouteVariantKey(viaRefs),
	}, "|")
}

// enqueueEdgeTriggeredPropagation checks if the endpoints of a newly created edge
// have accumulated signals. If so, it triggers async propagation from those endpoints
// so signals flow along the new edge.
func (e *ScoutEngine) enqueueEdgeTriggeredPropagation(ctx context.Context, edge *models.DependencyEdge, job *models.ScoutingJob) {
	if e.propagationTrigger == nil || edge == nil {
		return
	}
	if e.fragmentsRepo == nil {
		return
	}

	srcFrag, srcErr := e.fragmentsRepo.Get(ctx, edge.SourceFragmentID)
	tgtFrag, tgtErr := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)

	signals := propagation.CollectEdgeTriggeredSignals(ctx, edge, srcFrag, tgtFrag)
	signals = propagation.DeduplicateEdgeTriggeredSignals(signals)
	if len(signals) == 0 {
		return
	}

	// Collect unique fragment IDs that need re-propagation.
	fragIDs := make(map[string]struct{}, 2)
	for _, s := range signals {
		fragIDs[s.FragmentID] = struct{}{}
	}

	for fid := range fragIDs {
		fid := fid
		runAsyncSafe("edge_triggered_propagation", func() {
			propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := e.propagationTrigger(propCtx, fid); err != nil {
				fmt.Printf("[EdgeTriggeredPropagation] trigger failed fragmentId=%s edgeId=%s err=%v\n", fid, edge.ID, err)
			}
		})
	}

	if srcErr == nil && tgtErr == nil {
		sourceName := strings.TrimSpace(firstNonEmpty(
			srcFrag.Labels["en"],
			srcFrag.Labels["name"],
			srcFrag.CanonicalPath,
		))
		targetName := strings.TrimSpace(firstNonEmpty(
			tgtFrag.Labels["en"],
			tgtFrag.Labels["name"],
			tgtFrag.CanonicalPath,
		))
		title := "I queued downstream propagation"
		desc := fmt.Sprintf("%d signal(s) across the new link", len(signals))
		if sourceName != "" && targetName != "" {
			desc = fmt.Sprintf("%d signal(s) from %s to %s", len(signals), sourceName, targetName)
		}
		if len(fragIDs) > 0 {
			desc = fmt.Sprintf("%s • %d affected fragment(s) added to the queue", desc, len(fragIDs))
		}
		e.logLineHuman(ctx, job, "info",
			fmt.Sprintf("[EdgeTriggeredPropagation] I queued downstream propagation from a new link (%d signal(s), %d affected fragment(s))",
				len(signals), len(fragIDs)),
			title,
			desc,
		)
	}
}

func buildSupplyChainArtifactCanonicalKey(job *models.ScoutingJob, anchorPath string, chainIndex int, segments []pathbuilder.PathSegment) string {
	jobID := "unknown-job"
	if job != nil && strings.TrimSpace(job.ID) != "" {
		jobID = strings.TrimSpace(job.ID)
	}
	anchorToken := strings.Trim(strings.ReplaceAll(strings.TrimSpace(anchorPath), "/", "_"), "_")
	if anchorToken == "" {
		anchorToken = "unknown-anchor"
	}
	chainHash := hashSupplyChainSegments(segments)
	if chainHash == "" {
		chainHash = fmt.Sprintf("chain-%d", chainIndex)
	}
	return fmt.Sprintf("/supply-chain/global/%s/%s/%s", jobID, anchorToken, chainHash)
}

func hashSupplyChainSegments(segments []pathbuilder.PathSegment) string {
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(strings.TrimSpace(seg.SupplierRef))
		b.WriteString(">")
		b.WriteString(strings.TrimSpace(seg.ProductRef))
		b.WriteString(">")
		b.WriteString(strings.TrimSpace(seg.CustomerRef))
		b.WriteString("|")
		if seg.SupplierFacilityRef != nil {
			b.WriteString(strings.TrimSpace(*seg.SupplierFacilityRef))
		}
		b.WriteString("|")
		if seg.CustomerFacilityRef != nil {
			b.WriteString(strings.TrimSpace(*seg.CustomerFacilityRef))
		}
		b.WriteString("|")
		if len(seg.TransportViaRefs) > 0 {
			b.WriteString(strings.Join(seg.TransportViaRefs, ","))
		}
		if len(seg.Metrics) > 0 {
			keys := make([]string, 0, len(seg.Metrics))
			for k := range seg.Metrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				mv := seg.Metrics[k]
				unit := ""
				if mv.Unit != nil {
					unit = strings.TrimSpace(*mv.Unit)
				}
				b.WriteString(fmt.Sprintf("|%s=%.6f@%s#%.3f", k, mv.Value, unit, mv.Confidence))
			}
		}
		b.WriteString(";")
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

func buildSupplyChainJoinValidationPairs(segments []pathbuilder.PathSegment) []agents.SupplyChainJoinValidationPair {
	if len(segments) < 2 {
		return nil
	}
	pairs := make([]agents.SupplyChainJoinValidationPair, 0, len(segments)-1)
	for idx := 0; idx < len(segments)-1; idx++ {
		left := segments[idx]
		right := segments[idx+1]
		pairs = append(pairs, agents.SupplyChainJoinValidationPair{
			JoinIndex:         idx,
			LeftLegIndex:      idx,
			RightLegIndex:     idx + 1,
			LeftSupplierRef:   strings.TrimSpace(left.SupplierRef),
			LeftProductRef:    strings.TrimSpace(left.ProductRef),
			LeftCustomerRef:   strings.TrimSpace(left.CustomerRef),
			RightSupplierRef:  strings.TrimSpace(right.SupplierRef),
			RightProductRef:   strings.TrimSpace(right.ProductRef),
			RightCustomerRef:  strings.TrimSpace(right.CustomerRef),
			LeftTransportVia:  append([]string(nil), left.TransportViaRefs...),
			RightTransportVia: append([]string(nil), right.TransportViaRefs...),
		})
	}
	return pairs
}

func buildSupplyChainJoinAllowedRefs(chain agents.SupplyChain, segments []pathbuilder.PathSegment, graphContext string) []string {
	refs := make([]string, 0, len(chain.Nodes)+len(segments)*4)
	for _, node := range chain.Nodes {
		if ref := strings.TrimSpace(node.CanonicalPath); ref != "" {
			refs = append(refs, ref)
		}
	}
	for _, segment := range segments {
		for _, ref := range []string{
			segment.SupplierRef,
			segment.ProductRef,
			segment.CustomerRef,
		} {
			if ref = strings.TrimSpace(ref); ref != "" {
				refs = append(refs, ref)
			}
		}
		if segment.SupplierFacilityRef != nil && strings.TrimSpace(*segment.SupplierFacilityRef) != "" {
			refs = append(refs, strings.TrimSpace(*segment.SupplierFacilityRef))
		}
		if segment.CustomerFacilityRef != nil && strings.TrimSpace(*segment.CustomerFacilityRef) != "" {
			refs = append(refs, strings.TrimSpace(*segment.CustomerFacilityRef))
		}
		for _, via := range segment.TransportViaRefs {
			if via = strings.TrimSpace(via); via != "" {
				refs = append(refs, via)
			}
		}
	}
	for _, line := range strings.Split(graphContext, "\n") {
		start := 0
		for {
			openIdx := strings.Index(line[start:], "(")
			if openIdx < 0 {
				break
			}
			openIdx += start
			closeIdx := strings.Index(line[openIdx:], ")")
			if closeIdx < 0 {
				break
			}
			closeIdx += openIdx
			candidate := strings.TrimSpace(line[openIdx+1 : closeIdx])
			if strings.HasPrefix(candidate, "/") {
				refs = append(refs, candidate)
			}
			start = closeIdx + 1
		}
	}
	return uniqueSortedStrings(refs)
}

func mapJoinValidationAssessmentToModel(hint agents.SupplyChainJoinValidationAssessment) models.SupplyChainJoinAssessment {
	status := models.SupplyChainSufficiencyHeuristic
	switch strings.TrimSpace(hint.Status) {
	case models.SupplyChainSufficiencyValidated:
		status = models.SupplyChainSufficiencyValidated
	case models.SupplyChainSufficiencyForensicGap:
		status = models.SupplyChainSufficiencyForensicGap
	}
	entityReuse := models.SupplyChainJoinEntityReuseOnly
	if strings.TrimSpace(hint.Status) == "unresolved" {
		entityReuse = models.SupplyChainJoinEntityReuseUnknown
	}
	if len(hint.PolicyViolations) > 0 {
		entityReuse = models.SupplyChainJoinEntityReuseBlocked
		status = models.SupplyChainSufficiencyForensicGap
	}
	reasons := make([]string, 0, 1+len(hint.Gaps)+len(hint.PolicyViolations))
	if reason := strings.TrimSpace(hint.Reason); reason != "" {
		reasons = append(reasons, reason)
	}
	reasons = append(reasons, hint.Gaps...)
	reasons = append(reasons, hint.PolicyViolations...)
	productContinuity := strings.TrimSpace(hint.ProductRelation)
	if productContinuity == "" {
		productContinuity = "unknown"
	}
	return models.SupplyChainJoinAssessment{
		Index:             hint.JoinIndex,
		LeftLegIndex:      hint.LeftLegIndex,
		RightLegIndex:     hint.RightLegIndex,
		ProductContinuity: productContinuity,
		EntityReuse:       entityReuse,
		Status:            status,
		Reasons:           uniqueSortedStrings(reasons),
	}
}

func (e *ScoutEngine) buildSupplyChainJoinValidationHints(
	ctx context.Context,
	chain agents.SupplyChain,
	segments []pathbuilder.PathSegment,
	graphContext string,
) ([]models.SupplyChainJoinAssessment, []string, error) {
	if e == nil || e.criticAgent == nil || len(segments) < 2 {
		return nil, nil, nil
	}
	input := agents.SupplyChainJoinValidationInput{
		ChainRef:         strings.TrimSpace(chain.Reason),
		ChainReason:      strings.TrimSpace(chain.Reason),
		GraphContext:     strings.TrimSpace(graphContext),
		AllowedReuseRefs: buildSupplyChainJoinAllowedRefs(chain, segments, graphContext),
		Pairs:            buildSupplyChainJoinValidationPairs(segments),
	}
	if input.ChainRef == "" && len(input.Pairs) > 0 {
		input.ChainRef = fmt.Sprintf("segments:%d", len(segments))
	}
	out, err := e.criticAgent.ValidateSupplyChainJoins(ctx, input)
	if err != nil || out == nil {
		return nil, nil, err
	}
	hints := make([]models.SupplyChainJoinAssessment, 0, len(out.Assessments))
	gaps := make([]string, 0)
	for _, assessment := range out.Assessments {
		modelHint := mapJoinValidationAssessmentToModel(assessment)
		hints = append(hints, modelHint)
		gaps = append(gaps, assessment.Gaps...)
		gaps = append(gaps, assessment.PolicyViolations...)
	}
	return hints, uniqueSortedStrings(gaps), nil
}

// buildBatchedSupplyChainExpandMissingInfo builds a single MissingInfo prompt for multiple nodes of the same type.
// This allows one LLM call instead of N separate calls.
func buildBatchedSupplyChainExpandMissingInfo(nodeType string, paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	pathList := strings.Join(paths, ", ")
	switch nodeType {
	case "org":
		return []string{fmt.Sprintf("For EACH of these organizations: [%s]\nFind ALL child organizations (subsidiaries, controlled, partners) and ALL own facilities. One org → many child orgs + many facs. Return results per org. Format: JSON.", pathList)}
	case "fac":
		return []string{fmt.Sprintf("For EACH of these facilities: [%s]\nFind ALL products/services that EACH facility creates directly. One fac → many products. Do NOT list products of related or parent companies. Return results per facility. Format: JSON.", pathList)}
	case "product":
		return []string{fmt.Sprintf("For EACH of these products: [%s]\nFind ALL org-recipients (orgs that receive each product via their facilities). One product → many recipients. Return results per product. Format: JSON.", pathList)}
	default:
		return []string{fmt.Sprintf("For EACH of these nodes: [%s]\nBuild org→fac→product→fac→org trees (1-to-many at each level). Return results per node. Format: JSON.", pathList)}
	}
}

// runSupplyChainExpandPhase runs optional expand (1-hop, alternatives check) after processSupplyChains in Plan mode.
// Depth: 1-hop expand only when currentDepth < maxRecursionDepth (research depth set at job start).
// Expand criteria: confidence < 0.6, OR node in gapHints, OR node matches productFilter (plan 5.1).
// Config: job.State["supplyChainExpandEnabled"] (bool), job.State["supplyChainExpandMaxNodes"] (int, default 5).
func (e *ScoutEngine) runSupplyChainExpandPhase(ctx context.Context, job *models.ScoutingJob, frag *models.HorizonFragment, chains []agents.SupplyChain, supplyChainPlan *agents.SupplyChainExecutionPlan) {
	if job == nil || job.State == nil {
		return
	}
	prevPhase := parseStateString(job.State["supplyChainPhase"])
	job.State["supplyChainPhase"] = "targeted_enrich"
	job.State["contextScope"] = map[string]interface{}{
		"layer":    "optional_enrich",
		"contract": RuntimeContractContextScope,
	}
	defer func() {
		if prevPhase == "" {
			job.State["supplyChainPhase"] = "chain_core"
		} else {
			job.State["supplyChainPhase"] = prevPhase
		}
	}()
	if !parseStateBool(job.State["supplyChainExpandEnabled"], false) {
		return
	}
	if normalizeSupplyChainGraphBuildModeState(parseStateString(job.State["supplyChainGraphBuildMode"])) == agents.SupplyChainGraphBuildModeHotPathOnly {
		return
	}
	// Research depth (maxRecursionDepth) is set at job start; expand only if we have room for 1 more hop.
	currentDepth := parseStateInt(job.State["recursionDepth"], 0)
	maxDepth := parseStateInt(job.State["maxRecursionDepth"], 2)
	if maxDepth < 0 {
		maxDepth = 0
	}
	if currentDepth >= maxDepth {
		return
	}
	maxNodes := parseStateInt(job.State["supplyChainExpandMaxNodes"], 5)
	if maxNodes <= 0 {
		maxNodes = 5
	}
	// gapHints from chainContext (updated by processSupplyChains)
	gapHints := make(map[string]struct{})
	if job.State != nil {
		if cc, _ := ParseChainContext(job.State["chainContext"]); cc != nil {
			for _, g := range cc.GapHints {
				g = strings.TrimSpace(g)
				if g != "" {
					if strings.HasPrefix(g, "/") {
						gapHints[g] = struct{}{}
					}
					for _, cp := range extractCanonicalPathsFromHint(g) {
						gapHints[cp] = struct{}{}
					}
				}
			}
		}
	}
	// productFilter from plan queries
	productFilterSet := make(map[string]struct{})
	if supplyChainPlan != nil {
		for _, q := range supplyChainPlan.Queries {
			for _, pf := range q.ProductFilter {
				pf = strings.TrimSpace(strings.ToLower(pf))
				if pf != "" {
					productFilterSet[pf] = struct{}{}
				}
			}
		}
	}
	// nodeMatchesProductFilter: product path contains filter slug
	nodeMatchesProductFilter := func(cp string) bool {
		if len(productFilterSet) == 0 {
			return false
		}
		cpLower := strings.ToLower(cp)
		for pf := range productFilterSet {
			if strings.Contains(cpLower, pf) {
				return true
			}
		}
		return false
	}
	// Collect nodes for 1-hop expand: confidence < 0.6 OR in gapHints OR matches productFilter (plan 5.1)
	var expandCandidates []string
	seen := make(map[string]struct{})
	for _, ch := range chains {
		for _, n := range ch.Nodes {
			cp := strings.TrimSpace(n.CanonicalPath)
			if cp == "" || strings.HasPrefix(cp, "/domain/") {
				continue
			}
			if _, ok := seen[cp]; ok {
				continue
			}
			conf := ch.Confidence
			_, inGap := gapHints[cp]
			shouldExpand := conf < 0.6 || inGap || nodeMatchesProductFilter(cp)
			if shouldExpand {
				seen[cp] = struct{}{}
				expandCandidates = append(expandCandidates, cp)
				if len(expandCandidates) >= maxNodes {
					break
				}
			}
		}
		if len(expandCandidates) >= maxNodes {
			break
		}
	}
	if len(expandCandidates) > 0 && e.researchAgent != nil && e.fragmentsRepo != nil {
		chainContextStr := ""
		existingEntities := make([]string, 0)
		if cc, _ := ParseChainContext(job.State["chainContext"]); cc != nil {
			chainContextStr = cc.ToPromptContext()
			existingEntities = cc.DiscoveredNodes
			if len(existingEntities) > 50 {
				existingEntities = existingEntities[:50]
			}
		}
		// Group candidates by node type for batched LLM calls (1 call per type instead of N calls).
		groups := map[string][]string{"org": {}, "fac": {}, "product": {}, "other": {}}
		for _, cp := range expandCandidates {
			switch {
			case strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/"):
				groups["org"] = append(groups["org"], cp)
			case strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/deposit/"):
				groups["fac"] = append(groups["fac"], cp)
			case strings.HasPrefix(cp, "/product/"):
				groups["product"] = append(groups["product"], cp)
			default:
				groups["other"] = append(groups["other"], cp)
			}
		}
		var allChains []agents.SupplyChain
		var mu sync.Mutex
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(3)
		for nodeType, paths := range groups {
			if len(paths) == 0 {
				continue
			}
			nodeType := nodeType
			paths := paths
			g.Go(func() error {
				// Use first fragment as representative for the batch prompt context.
				var repFrag *models.HorizonFragment
				for _, cp := range paths {
					if f, err := e.fragmentsRepo.GetByCanonicalPath(gctx, cp); err == nil && f != nil {
						repFrag = f
						break
					}
				}
				if repFrag == nil {
					repFrag = &models.HorizonFragment{CanonicalPath: paths[0]}
				}
				missingInfo := buildBatchedSupplyChainExpandMissingInfo(nodeType, paths)
				input := agents.ResearchStrategyInput{
					Fragment:         repFrag,
					MissingInfo:      missingInfo,
					ChainContext:     chainContextStr,
					ExistingEntities: existingEntities,
				}
				domainModes := []string{taxonomy.DomainModeSupplyChain}
				if _, _, newChains, _, _, err := e.researchAgent.ResearchPipeline().Enrich(gctx, input, domainModes); err == nil && len(newChains) > 0 {
					mu.Lock()
					allChains = append(allChains, newChains...)
					mu.Unlock()
				}
				return nil
			})
		}
		_ = g.Wait()
		if len(allChains) > 0 {
			_ = e.processSupplyChains(ctx, job, frag, allChains)
		}
	}
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	job.State["supplyChainExpandPhase"] = "completed"
}

func limitSupplyChainQueriesForFastFirst(queries []agents.SupplyChainQuery, limit int) []agents.SupplyChainQuery {
	if limit <= 0 || len(queries) <= limit {
		return queries
	}
	out := make([]agents.SupplyChainQuery, 0, limit)
	for _, query := range queries {
		if len(out) >= limit {
			break
		}
		out = append(out, query)
	}
	return out
}

func capSupplyChainFastFirstAnchors(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return values
	}
	out := make([]string, 0, min(limit, len(values)))
	seen := make(map[string]struct{}, len(values))
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
		if len(out) >= limit {
			break
		}
	}
	return out
}

func supplyChainFastFirstIntLimit(job *models.ScoutingJob, key string, fallback int) int {
	if fallback <= 0 || job == nil || job.State == nil {
		return fallback
	}
	if v := parseStateInt(job.State[key], 0); v > 0 && v < fallback {
		return v
	}
	return fallback
}

func shouldStopSupplyChainAfterFirstIncremental(job *models.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	if !parseStateBool(job.State["supplyChainStopAfterFirstIncremental"], false) {
		return false
	}
	return parseStateBool(job.State["supplyChainIncrementalMaterialized"], false)
}

func supplyChainFastFirstHotPathQueries(queries []agents.SupplyChainQuery, limit int) []agents.SupplyChainQuery {
	if len(queries) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 2
	}
	capN := len(queries)
	if capN > limit {
		capN = limit
	}
	out := make([]agents.SupplyChainQuery, 0, capN)
	for _, query := range queries {
		q := query
		if q.Depth < 3 {
			q.Depth = 3
		}
		if q.Depth > 4 {
			q.Depth = 4
		}
		out = append(out, q)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func dedupSupplyChainChains(chains []agents.SupplyChain) []agents.SupplyChain {
	if len(chains) == 0 {
		return nil
	}
	out := make([]agents.SupplyChain, 0, len(chains))
	indexByKey := make(map[string]int, len(chains))
	for _, chain := range chains {
		if len(chain.Nodes) == 0 {
			continue
		}
		parts := make([]string, 0, len(chain.Nodes))
		for _, node := range chain.Nodes {
			cp := strings.TrimSpace(node.CanonicalPath)
			if cp == "" {
				continue
			}
			parts = append(parts, cp)
		}
		if len(parts) == 0 {
			continue
		}
		key := strings.Join(parts, "|")
		if idx, ok := indexByKey[key]; ok {
			out[idx] = mergeSupplyChainChain(out[idx], chain)
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, chain)
	}
	return out
}

func mergeSupplyChainObservationOverlays(base []agents.SupplyChain, overlays []agents.SupplyChain) []agents.SupplyChain {
	if len(base) == 0 {
		return dedupSupplyChainChains(overlays)
	}
	if len(overlays) == 0 {
		return dedupSupplyChainChains(base)
	}
	merged := append([]agents.SupplyChain{}, base...)
	merged = append(merged, overlays...)
	return dedupSupplyChainChains(merged)
}

func applyScopedObservationBundleToSupplyChains(chains []agents.SupplyChain, bundle *agents.SupplyChainScopedObservations) []agents.SupplyChain {
	if len(chains) == 0 || bundle == nil {
		return chains
	}
	out := append([]agents.SupplyChain{}, chains...)
	for _, candidate := range bundle.ProductCandidates {
		idx := 0
		out[idx].ProductCandidates = mergeProductCandidates(out[idx].ProductCandidates, []agents.ProductCandidate{candidate})
	}
	for _, obs := range bundle.CarrierProductObservations {
		idx := findSupplyChainForCarrierProductObservation(out, obs)
		out[idx].CarrierProductObservations = mergeCarrierProductObservations(out[idx].CarrierProductObservations, []agents.CarrierProductObservation{obs})
	}
	for _, obs := range bundle.LinkObservations {
		idx := findSupplyChainForLinkObservation(out, obs)
		out[idx].LinkObservations = mergeLinkObservations(out[idx].LinkObservations, []agents.SupplyLinkObservation{obs})
	}
	for _, obs := range bundle.RouteObservations {
		idx := findSupplyChainForRouteObservation(out, obs)
		out[idx].RouteObservations = mergeRouteObservations(out[idx].RouteObservations, []agents.RouteObservation{obs})
	}
	return out
}

func findSupplyChainForCarrierProductObservation(chains []agents.SupplyChain, obs agents.CarrierProductObservation) int {
	productRef := strings.TrimSpace(obs.ProductRef)
	carrierRef := strings.TrimSpace(obs.CarrierRef)
	for idx, chain := range chains {
		if supplyChainContainsProduct(chain, productRef) && supplyChainContainsActor(chain, carrierRef) {
			return idx
		}
	}
	for idx, chain := range chains {
		if supplyChainContainsProduct(chain, productRef) {
			return idx
		}
	}
	return 0
}

func findSupplyChainForLinkObservation(chains []agents.SupplyChain, obs agents.SupplyLinkObservation) int {
	productRef := strings.TrimSpace(obs.ProductRef)
	supplierRef := strings.TrimSpace(obs.SupplierRef)
	customerRef := strings.TrimSpace(obs.CustomerRef)
	for idx, chain := range chains {
		if supplyChainContainsProduct(chain, productRef) && supplyChainContainsActor(chain, supplierRef) && supplyChainContainsActor(chain, customerRef) {
			return idx
		}
	}
	for idx, chain := range chains {
		if supplyChainContainsProduct(chain, productRef) {
			return idx
		}
	}
	return 0
}

func findSupplyChainForRouteObservation(chains []agents.SupplyChain, obs agents.RouteObservation) int {
	productRef := strings.TrimSpace(obs.ProductRef)
	for idx, chain := range chains {
		if productRef != "" && supplyChainContainsProduct(chain, productRef) {
			if len(obs.TransportVia) == 0 || supplyChainSharesTransportVia(chain, obs.TransportVia) {
				return idx
			}
		}
	}
	for idx, chain := range chains {
		if supplyChainSharesTransportVia(chain, obs.TransportVia) {
			return idx
		}
	}
	return 0
}

func supplyChainContainsProduct(chain agents.SupplyChain, productRef string) bool {
	productRef = strings.TrimSpace(productRef)
	if productRef == "" {
		return false
	}
	for _, node := range chain.Nodes {
		if strings.TrimSpace(node.CanonicalPath) == productRef {
			return true
		}
	}
	return false
}

func supplyChainContainsActor(chain agents.SupplyChain, actorRef string) bool {
	actorRef = strings.TrimSpace(actorRef)
	if actorRef == "" {
		return false
	}
	for _, node := range chain.Nodes {
		if strings.TrimSpace(node.CanonicalPath) == actorRef {
			return true
		}
	}
	return false
}

func supplyChainSharesTransportVia(chain agents.SupplyChain, via []string) bool {
	if len(via) == 0 || len(chain.TransportVia) == 0 {
		return false
	}
	chainVia := make(map[string]struct{}, len(chain.TransportVia))
	for _, ref := range chain.TransportVia {
		value := strings.TrimSpace(ref)
		if value != "" {
			chainVia[strings.ToLower(value)] = struct{}{}
		}
	}
	for _, ref := range via {
		if _, ok := chainVia[strings.ToLower(strings.TrimSpace(ref))]; ok {
			return true
		}
	}
	return false
}

func mergeSupplyChainChain(base, overlay agents.SupplyChain) agents.SupplyChain {
	if len(base.Nodes) == 0 {
		base.Nodes = overlay.Nodes
	}
	if strings.TrimSpace(base.Reason) == "" {
		base.Reason = overlay.Reason
	}
	if overlay.Confidence > base.Confidence {
		base.Confidence = overlay.Confidence
	}
	base.TransportVia = mergeStringSlices(base.TransportVia, overlay.TransportVia)
	base.ProductCandidates = mergeProductCandidates(base.ProductCandidates, overlay.ProductCandidates)
	base.CarrierProductObservations = mergeCarrierProductObservations(base.CarrierProductObservations, overlay.CarrierProductObservations)
	base.LinkObservations = mergeLinkObservations(base.LinkObservations, overlay.LinkObservations)
	base.RouteObservations = mergeRouteObservations(base.RouteObservations, overlay.RouteObservations)
	return base
}

func mergeStringSlices(left, right []string) []string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(left)+len(right))
	out := make([]string, 0, len(left)+len(right))
	for _, raw := range append(append([]string{}, left...), right...) {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeProductCandidates(left, right []agents.ProductCandidate) []agents.ProductCandidate {
	if len(left) == 0 {
		return append([]agents.ProductCandidate(nil), right...)
	}
	out := append([]agents.ProductCandidate{}, left...)
	seen := make(map[string]int, len(out))
	for idx, candidate := range out {
		key := strings.ToLower(strings.TrimSpace(candidate.CanonicalPath))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(candidate.Name))
		}
		if key != "" {
			seen[key] = idx
		}
	}
	for _, candidate := range right {
		key := strings.ToLower(strings.TrimSpace(candidate.CanonicalPath))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(candidate.Name))
		}
		if key == "" {
			continue
		}
		if idx, ok := seen[key]; ok {
			if candidate.Confidence > out[idx].Confidence {
				out[idx] = candidate
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, candidate)
	}
	return out
}

func mergeCarrierProductObservations(left, right []agents.CarrierProductObservation) []agents.CarrierProductObservation {
	if len(left) == 0 {
		return append([]agents.CarrierProductObservation(nil), right...)
	}
	out := append([]agents.CarrierProductObservation{}, left...)
	seen := make(map[string]int, len(out))
	for idx, obs := range out {
		seen[strings.ToLower(strings.TrimSpace(obs.CarrierRef)+"|"+strings.TrimSpace(obs.ProductRef))] = idx
	}
	for _, obs := range right {
		key := strings.ToLower(strings.TrimSpace(obs.CarrierRef) + "|" + strings.TrimSpace(obs.ProductRef))
		if strings.TrimSpace(key) == "|" {
			continue
		}
		if idx, ok := seen[key]; ok {
			out[idx] = mergeCarrierProductObservation(out[idx], obs)
			continue
		}
		seen[key] = len(out)
		out = append(out, obs)
	}
	return out
}

func mergeCarrierProductObservation(base, overlay agents.CarrierProductObservation) agents.CarrierProductObservation {
	if overlay.Confidence > base.Confidence {
		base.Confidence = overlay.Confidence
	}
	if strings.TrimSpace(base.ObservedAt) == "" {
		base.ObservedAt = overlay.ObservedAt
	}
	base.Metrics = mergeLooseObservationMaps(base.Metrics, overlay.Metrics)
	base.Qualifiers = mergeLooseObservationMaps(base.Qualifiers, overlay.Qualifiers)
	return base
}

func mergeLinkObservations(left, right []agents.SupplyLinkObservation) []agents.SupplyLinkObservation {
	if len(left) == 0 {
		return append([]agents.SupplyLinkObservation(nil), right...)
	}
	out := append([]agents.SupplyLinkObservation{}, left...)
	seen := make(map[string]int, len(out))
	for idx, obs := range out {
		seen[strings.ToLower(strings.TrimSpace(obs.SupplierRef)+"|"+strings.TrimSpace(obs.ProductRef)+"|"+strings.TrimSpace(obs.CustomerRef))] = idx
	}
	for _, obs := range right {
		key := strings.ToLower(strings.TrimSpace(obs.SupplierRef) + "|" + strings.TrimSpace(obs.ProductRef) + "|" + strings.TrimSpace(obs.CustomerRef))
		if strings.TrimSpace(key) == "||" {
			continue
		}
		if idx, ok := seen[key]; ok {
			out[idx] = mergeLinkObservation(out[idx], obs)
			continue
		}
		seen[key] = len(out)
		out = append(out, obs)
	}
	return out
}

func mergeLinkObservation(base, overlay agents.SupplyLinkObservation) agents.SupplyLinkObservation {
	if overlay.Confidence > base.Confidence {
		base.Confidence = overlay.Confidence
	}
	if strings.TrimSpace(base.ObservedAt) == "" {
		base.ObservedAt = overlay.ObservedAt
	}
	base.Metrics = mergeLooseObservationMaps(base.Metrics, overlay.Metrics)
	base.Qualifiers = mergeLooseObservationMaps(base.Qualifiers, overlay.Qualifiers)
	return base
}

func mergeRouteObservations(left, right []agents.RouteObservation) []agents.RouteObservation {
	if len(left) == 0 {
		return append([]agents.RouteObservation(nil), right...)
	}
	out := append([]agents.RouteObservation{}, left...)
	seen := make(map[string]int, len(out))
	for idx, obs := range out {
		seen[routeObservationMergeKey(obs)] = idx
	}
	for _, obs := range right {
		key := routeObservationMergeKey(obs)
		if strings.TrimSpace(key) == "" {
			continue
		}
		if idx, ok := seen[key]; ok {
			out[idx] = mergeRouteObservation(out[idx], obs)
			continue
		}
		seen[key] = len(out)
		out = append(out, obs)
	}
	return out
}

func mergeRouteObservation(base, overlay agents.RouteObservation) agents.RouteObservation {
	if overlay.Confidence > base.Confidence {
		base.Confidence = overlay.Confidence
	}
	if strings.TrimSpace(base.ObservedAt) == "" {
		base.ObservedAt = overlay.ObservedAt
	}
	if strings.TrimSpace(base.VesselType) == "" {
		base.VesselType = overlay.VesselType
	}
	base.TransportVia = mergeStringSlices(base.TransportVia, overlay.TransportVia)
	base.Qualifiers = mergeLooseObservationMaps(base.Qualifiers, overlay.Qualifiers)
	return base
}

func routeObservationMergeKey(obs agents.RouteObservation) string {
	via := mergeStringSlices(nil, obs.TransportVia)
	return strings.ToLower(
		strings.TrimSpace(obs.SupplierRef) + "|" +
			strings.TrimSpace(obs.ProductRef) + "|" +
			strings.TrimSpace(obs.CustomerRef) + "|" +
			strings.Join(via, ",") + "|" +
			strings.TrimSpace(obs.VesselType),
	)
}

func mergeLooseObservationMaps(left, right map[string]interface{}) map[string]interface{} {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(left)+len(right))
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		if existing, ok := out[key]; ok && existing != nil {
			existingMap, existingIsMap := existing.(map[string]interface{})
			valueMap, valueIsMap := value.(map[string]interface{})
			if existingIsMap && valueIsMap {
				out[key] = mergeLooseObservationMaps(existingMap, valueMap)
			}
			continue
		}
		out[key] = value
	}
	return out
}

func parseSupplyChainCounter(raw interface{}, key string) int {
	m, ok := raw.(map[string]interface{})
	if !ok || m == nil {
		return 0
	}
	return parseStateInt(m[key], 0)
}

func hasUsefulSupplyChainHotPathMaterialization(job *models.ScoutingJob, chains []agents.SupplyChain) bool {
	if job == nil || job.State == nil {
		return len(chains) >= 4
	}
	materializationLinks := parseSupplyChainCounter(job.State["supplyChainMaterialization"], "materializedSupplyLinks")
	materializationPaths := parseSupplyChainCounter(job.State["supplyChainMaterialization"], "materializedPaths")
	discoveredNodes := parseSupplyChainCounter(job.State["supplyChainSummary"], "discoveredNodes")
	discoveredEdges := parseSupplyChainCounter(job.State["supplyChainSummary"], "discoveredEdges")
	return materializationLinks >= 2 ||
		materializationPaths >= 2 ||
		len(chains) >= 4 ||
		(discoveredNodes >= 8 && discoveredEdges >= 8)
}

func (e *ScoutEngine) materializeSupplyChainHotPathChains(ctx context.Context, job *models.ScoutingJob, chains []agents.SupplyChain) bool {
	if e == nil || job == nil || len(chains) == 0 {
		return false
	}
	chains = dedupSupplyChainChains(chains)
	if len(chains) == 0 {
		return false
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	prevStage := parseStateString(job.State["supplyChainMaterializationStage"])
	job.State["supplyChainMaterializationStage"] = "hot_path_full_query"
	if err := e.processSupplyChains(ctx, job, nil, chains); err != nil {
		if strings.TrimSpace(prevStage) == "" {
			delete(job.State, "supplyChainMaterializationStage")
		} else {
			job.State["supplyChainMaterializationStage"] = prevStage
		}
		scLog(ctx, e, job, "warn", fmt.Sprintf("[SupplyChainHotPath] incremental materialization failed chains=%d: %v", len(chains), err))
		return false
	}
	job.State["supplyChainIncrementalMaterialized"] = true
	job.State["supplyChainHotPathChainsN"] = len(chains)
	if strings.TrimSpace(prevStage) == "" {
		delete(job.State, "supplyChainMaterializationStage")
	} else {
		job.State["supplyChainMaterializationStage"] = prevStage
	}
	e.publish(ctx, realtime.EventType("horizon.supply_chain.hot_path.materialized"), map[string]interface{}{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         job.ID,
		"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"chainsN":       len(chains),
		"stage":         "hot_path_full_query",
	})
	e.publishDirectedExplorationMaterialized(ctx, job, "hot_path_full_query", directedExplorationDirection(job), chains)
	return true
}

func (e *ScoutEngine) runSupplyChainFastFirstHotPath(ctx context.Context, job *models.ScoutingJob, queries []agents.SupplyChainQuery) []agents.SupplyChain {
	if e == nil || e.researchAgent == nil || len(queries) == 0 {
		return nil
	}
	_, domainFacets := parseDomainPathFacets(strings.TrimSpace(firstNonEmpty(
		strings.TrimSpace(job.TargetCanonicalPath),
		strings.TrimSpace(parseStateString(job.State["targetCanonicalPath"])),
	)))
	targetGeoFilters := []string(nil)
	if domainFacets != nil {
		targetGeoFilters = dedupStrings(domainFacets["geo"])
		if len(targetGeoFilters) > 8 {
			targetGeoFilters = targetGeoFilters[:8]
		}
	}
	maxQueries := 2
	if job != nil {
		maxQueries = supplyChainFastFirstIntLimit(job, "supplyChainFastFirstMaxHotQueries", maxQueries)
	}
	if directedExplorationHotPathOnly(job) && maxQueries > 1 {
		maxQueries = 1
	}
	hotQueries := supplyChainFastFirstHotPathQueries(queries, maxQueries)
	if len(hotQueries) == 0 {
		return nil
	}
	chains := make([]agents.SupplyChain, 0, 32)
	for idx, query := range hotQueries {
		query = applyDirectedExplorationQueryAugment(query, job)
		prePlan, preErr := e.researchAgent.EnrichSupplyChainPreHotPath(ctx, query)
		if preErr != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SupplyChainPreHotPath] query[%d] failed: %v", idx, preErr))
		} else if prePlan != nil {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainPreHotPath] query[%d] strategy=%s confidence=%.2f derivedProducts=%d focus=%d",
				idx, strings.TrimSpace(prePlan.Strategy), prePlan.Confidence, len(prePlan.DerivedProductFilters), len(prePlan.SearchFocus)))
		}
		queryVariants := deriveSupplyChainHotPathVariants(query, prePlan)
		directedHotPathOnly := directedExplorationHotPathOnly(job)
		var accumulatedForQuery []agents.SupplyChain
		for variantIdx, variant := range queryVariants {
			if directedHotPathOnly {
				if len(accumulatedForQuery) > 0 && variantIdx > 0 {
					break
				}
				if variantIdx > 1 {
					break
				}
			}
			variant = applyDirectedExplorationQueryAugment(variant, job)
			queryChains, err := e.researchAgent.EnrichSupplyChainHotPath(ctx, variant, prePlan)
			if err != nil {
				scLog(ctx, e, job, "warn", fmt.Sprintf("[SupplyChainHotPath] query[%d] variant[%d] failed: %v", idx, variantIdx, err))
				continue
			}
			queryChains = filterSupplyChainsByPlanFilters(queryChains, variant.ProductFilter, variant.GeoFilter)
			queryChains = filterSupplyChainsByAnchorScope(queryChains, variant, prePlan, targetGeoFilters)
			if len(queryChains) > 0 && directedExplorationWantsCapabilityPass(job) {
				if capabilityBundle, capErr := e.researchAgent.EnrichSupplyChainHotPathCapabilities(ctx, variant, queryChains, prePlan); capErr != nil {
					scLog(ctx, e, job, "warn", fmt.Sprintf("[SupplyChainHotPathCapability] query[%d] variant[%d] failed: %v", idx, variantIdx, capErr))
				} else if capabilityBundle != nil {
					queryChains = applyScopedObservationBundleToSupplyChains(queryChains, capabilityBundle)
					scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainHotPathCapability] query[%d] variant[%d] carrierObs=%d linkObs=%d routeObs=%d candidates=%d",
						idx,
						variantIdx,
						len(capabilityBundle.CarrierProductObservations),
						len(capabilityBundle.LinkObservations),
						len(capabilityBundle.RouteObservations),
						len(capabilityBundle.ProductCandidates),
					))
				}
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SupplyChainHotPath] query[%d] variant[%d] chains=%d anchor=%s anchors=%d products=%d",
				idx, variantIdx, len(queryChains), strings.TrimSpace(variant.Anchor), len(variant.Anchors), len(variant.ProductFilter)))
			accumulatedForQuery = append(accumulatedForQuery, queryChains...)
			accumulatedForQuery = dedupSupplyChainChains(accumulatedForQuery)
			if len(accumulatedForQuery) >= 10 || len(chains)+len(accumulatedForQuery) >= 20 {
				break
			}
		}
		chains = append(chains, accumulatedForQuery...)
		if len(chains) >= 20 {
			break
		}
	}
	chains = dedupSupplyChainChains(chains)
	if len(chains) > 20 {
		chains = chains[:20]
	}
	return chains
}

func deriveSupplyChainHotPathVariants(query agents.SupplyChainQuery, prePlan *agents.SupplyChainPreHotPathPlan) []agents.SupplyChainQuery {
	var out []agents.SupplyChainQuery
	out = append(out, buildAnchorFocusedSupplyChainHotPathQueries(query, prePlan)...)
	focused := buildFocusedSupplyChainHotPathQuery(query, prePlan)
	if focused != nil {
		out = append(out, *focused)
	}
	out = append(out, query)
	return dedupSupplyChainHotPathVariants(out)
}

func buildAnchorFocusedSupplyChainHotPathQueries(query agents.SupplyChainQuery, prePlan *agents.SupplyChainPreHotPathPlan) []agents.SupplyChainQuery {
	if prePlan == nil || len(prePlan.CandidateAnchorTerms) == 0 {
		return nil
	}
	anchorType := strings.ToLower(strings.TrimSpace(query.AnchorType))
	if anchorType != "" && anchorType != "org" && anchorType != "facility" && anchorType != "fac" {
		return nil
	}
	original := strings.TrimSpace(query.Anchor)
	seen := map[string]struct{}{}
	if original != "" {
		seen[strings.ToLower(original)] = struct{}{}
	}
	out := make([]agents.SupplyChainQuery, 0, 3)
	for _, raw := range prePlan.CandidateAnchorTerms {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		variant := query
		variant.Anchor = trimmed
		variant.Anchors = nil
		reason := strings.TrimSpace(variant.Reason)
		if reason == "" {
			reason = "pre_hot_path_anchor_focus"
		} else {
			reason += "; pre_hot_path_anchor_focus"
		}
		if strategy := strings.TrimSpace(prePlan.Strategy); strategy != "" {
			reason += ":" + strategy
		}
		variant.Reason = reason
		out = append(out, variant)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func buildFocusedSupplyChainHotPathQuery(query agents.SupplyChainQuery, prePlan *agents.SupplyChainPreHotPathPlan) *agents.SupplyChainQuery {
	if prePlan == nil {
		return nil
	}
	existing := make(map[string]struct{}, len(query.ProductFilter))
	for _, raw := range query.ProductFilter {
		if key := strings.TrimSpace(strings.ToLower(raw)); key != "" {
			existing[key] = struct{}{}
		}
	}
	candidates := append([]string(nil), prePlan.DerivedProductFilters...)
	if len(candidates) == 0 {
		candidates = append(candidates, prePlan.CandidateProductFamilies...)
	}
	filters := make([]string, 0, len(candidates))
	novel := false
	for _, raw := range candidates {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, ok := existing[strings.ToLower(trimmed)]; !ok {
			novel = true
		}
		filters = append(filters, trimmed)
		if len(filters) >= 3 {
			break
		}
	}
	if len(filters) == 0 || !novel {
		return nil
	}
	focused := query
	focused.ProductFilter = filters
	reason := strings.TrimSpace(focused.Reason)
	if reason == "" {
		reason = "pre_hot_path_focus"
	} else {
		reason += "; pre_hot_path_focus"
	}
	if strategy := strings.TrimSpace(prePlan.Strategy); strategy != "" {
		reason += ":" + strategy
	}
	focused.Reason = reason
	return &focused
}

func dedupSupplyChainHotPathVariants(in []agents.SupplyChainQuery) []agents.SupplyChainQuery {
	if len(in) == 0 {
		return nil
	}
	out := make([]agents.SupplyChainQuery, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, q := range in {
		key := strings.ToLower(strings.TrimSpace(q.Anchor) + "|" + strings.Join(dedupStrings(q.ProductFilter), "|") + "|" + strings.Join(dedupStrings(q.GeoFilter), "|") + "|" + strings.TrimSpace(q.Direction))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, q)
	}
	return out
}

// runSupplyChainStage2Overlays runs overlay queries (regulatory, market, decision_points) over parsed chains.
// Anchors are extracted from chain nodes (org, market paths). Results: DependencyEdges (REGULATES, INFLUENCES).
func (e *ScoutEngine) runSupplyChainStage2Overlays(
	ctx context.Context,
	job *models.ScoutingJob,
	frag *models.HorizonFragment,
	chains []agents.SupplyChain,
	overlayQueries []agents.SupplyChainOverlayQuery,
) {
	if job == nil || len(chains) == 0 || len(overlayQueries) == 0 {
		return
	}
	// Extract anchors from chain nodes (org, market paths)
	anchors := make(map[string]struct{})
	for _, ch := range chains {
		for _, n := range ch.Nodes {
			cp := strings.TrimSpace(n.CanonicalPath)
			if cp == "" {
				continue
			}
			if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") || strings.HasPrefix(cp, "/market/") || strings.HasPrefix(cp, "/fac/") {
				anchors[cp] = struct{}{}
			}
		}
	}
	anchorList := make([]string, 0, len(anchors))
	for a := range anchors {
		anchorList = append(anchorList, a)
	}
	for _, oq := range overlayQueries {
		oqType := strings.TrimSpace(strings.ToLower(oq.Type))
		oqAnchors := oq.Anchors
		if len(oqAnchors) == 0 {
			oqAnchors = anchorList
		}
		if len(oqAnchors) == 0 {
			continue
		}
		switch oqType {
		case "regulatory":
			e.runStage2OverlayLLM(ctx, job, frag, "regulatory", oqAnchors, chains,
				"Identify regulatory relationships: which gov/agency entities REGULATE these anchors (markets, orgs)? Return edges: sourceRef (regulator), targetRef (regulated), type=REGULATES, reason.")
		case "market":
			e.runStage2OverlayLLM(ctx, job, frag, "market", oqAnchors, chains,
				"Identify market participation: which orgs PARTICIPATE_IN these markets? Return edges: sourceRef (org), targetRef (market), type=PARTICIPATES_IN, reason.")
		case "decision_points":
			e.runStage2OverlayLLM(ctx, job, frag, "decision_points", oqAnchors, chains,
				"Identify decision points: who decides, which mechanisms apply? Return edges: sourceRef (org/gov), targetRef (market/org), type=REGULATES|INFLUENCES|IMPOSES_CONDITION, reason.")
		default:
			e.logLine(ctx, job, "info", fmt.Sprintf("[Stage2Overlay] unknown type=%q anchors=%d", oq.Type, len(oqAnchors)))
		}
	}
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	job.State["supplyChainStage2Overlays"] = "completed"
}

// extractChainNodePaths returns unique canonical paths from chain nodes (for prompt context).
func extractChainNodePaths(chains []agents.SupplyChain) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, ch := range chains {
		for _, n := range ch.Nodes {
			cp := strings.TrimSpace(n.CanonicalPath)
			if cp != "" && !strings.HasPrefix(cp, "/domain/") {
				if _, ok := seen[cp]; !ok {
					seen[cp] = struct{}{}
					out = append(out, cp)
				}
			}
		}
	}
	return out
}

// runStage2OverlayLLM runs one overlay LLM call and persists DependencyEdges (REGULATES, INFLUENCES, PARTICIPATES_IN).
func (e *ScoutEngine) runStage2OverlayLLM(
	ctx context.Context,
	job *models.ScoutingJob,
	frag *models.HorizonFragment,
	overlayType string,
	anchors []string,
	chains []agents.SupplyChain,
	instruction string,
) {
	if e.aiExecutor == nil || e.edgesRepo == nil || e.fragmentsRepo == nil {
		e.logLine(ctx, job, "info", fmt.Sprintf("[Stage2Overlay] %s skipped (no aiExecutor/edgesRepo)", overlayType))
		return
	}
	anchorsJSON, _ := json.Marshal(anchors)
	anchorCtx := ""
	if frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
		anchorCtx = fmt.Sprintf("\nSupply chain anchor: %s\n", strings.TrimSpace(frag.CanonicalPath))
	}
	chainNodes := extractChainNodePaths(chains)
	chainCtx := ""
	if len(chainNodes) > 0 {
		chainCtx = fmt.Sprintf("\nSupply chain nodes (context): %s\n", strings.Join(chainNodes, ", "))
	}
	prompt := fmt.Sprintf(`%s%s%s

Anchors (canonical paths): %s

Return ONLY valid JSON: { "edges": [ { "sourceRef": "/org/... or /gov/...", "targetRef": "/org/... or /market/...", "type": "REGULATES|INFLUENCES|PARTICIPATES_IN|IMPOSES_CONDITION", "reason": "short evidence" } ] }
Use canonical paths. type must be one of: REGULATES, INFLUENCES, PARTICIPATES_IN, IMPOSES_CONDITION.`, instruction, anchorCtx, chainCtx, string(anchorsJSON))
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         8000,
			MaxOutputSize:            4000,
		},
		Verifier: verifier,
	}
	result, _, err := e.aiExecutor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil || result == nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("[Stage2Overlay] %s LLM failed: %v", overlayType, err))
		return
	}
	jsonData, _ := json.Marshal(result)
	var parsed struct {
		Edges []struct {
			SourceRef string `json:"sourceRef"`
			TargetRef string `json:"targetRef"`
			Type      string `json:"type"`
			Reason    string `json:"reason"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("[Stage2Overlay] %s parse failed: %v", overlayType, err))
		return
	}
	created := 0
	for _, ed := range parsed.Edges {
		srcRef := strings.TrimSpace(ed.SourceRef)
		tgtRef := strings.TrimSpace(ed.TargetRef)
		typ := strings.TrimSpace(strings.ToUpper(ed.Type))
		if srcRef == "" || tgtRef == "" || typ == "" {
			continue
		}
		if typ != "REGULATES" && typ != "INFLUENCES" && typ != "PARTICIPATES_IN" && typ != "IMPOSES_CONDITION" {
			continue
		}
		srcFrag, _ := e.fragmentsRepo.GetByCanonicalPath(ctx, srcRef)
		tgtFrag, _ := e.fragmentsRepo.GetByCanonicalPath(ctx, tgtRef)
		if srcFrag == nil || tgtFrag == nil || srcFrag.ID == "" || tgtFrag.ID == "" {
			continue
		}
		edgeType := models.DependencyType(typ)
		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: srcFrag.ID,
			TargetFragmentID: tgtFrag.ID,
			Type:             edgeType,
			Properties:       models.EdgeProperties{Criticality: 0.6, Amplification: 1.0},
			Provenance:       models.Provenance{Method: "scout_stage2_overlay", SourceIDs: []string{job.ID}},
			Qualifiers:       map[string]interface{}{"overlay_type": overlayType, "reason": ed.Reason},
		}
		if err := edge.Validate(); err != nil {
			continue
		}
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", srcFrag.ID, tgtFrag.ID, edgeType); err == nil {
			continue
		}
		if _, err := e.edgesRepo.Create(ctx, edge); err != nil {
			continue
		}
		created++
	}
	e.logLine(ctx, job, "info", fmt.Sprintf("[Stage2Overlay] %s edges=%d", overlayType, created))
}

// processSupplyChains parses supply chain output from agents and persists fragments/edges.
func (e *ScoutEngine) processSupplyChains(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	chains []agents.SupplyChain,
) error {
	if len(chains) == 0 {
		return nil
	}
	if job != nil {
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		if strings.TrimSpace(parseStateString(job.State["supplyChainPhase"])) == "" {
			job.State["supplyChainPhase"] = "chain_core"
		}
		job.State["contextScope"] = map[string]interface{}{
			"layer":    "chain_core",
			"contract": RuntimeContractContextScope,
		}
	}
	if job != nil {
		scope := mergeWorkingProductScopeFromChains(job, chains, "chain_discovery")
		persistWorkingProductScope(ctx, e, job, scope)
		applyWorkingProductScopeProductDictionary(ctx, e, job)
	}
	persistScopedObservationsFromChains(ctx, e, job, chains)

	parser := NewSupplyChainParser()
	if e.canonicalPathResolver != nil {
		parser = parser.WithProductDictionary(e.canonicalPathResolver.productDictionary)
	}
	sourceCanonicalPath := ""
	if sourceFragment != nil {
		sourceCanonicalPath = strings.TrimSpace(sourceFragment.CanonicalPath)
	}
	result, err := parser.ParseSupplyChains(chains, sourceCanonicalPath)
	if err != nil {
		msg := fmt.Sprintf("[SupplyChainParse] failed to parse supplyChains: %v", err)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "warn", msg)
		return fmt.Errorf("failed to parse supply chains: %w", err)
	}
	result.SupplyLinkSegments = normalizeAndDedupSupplyLinkSegments(result.SupplyLinkSegments)
	result.Edges = dedupeChainEdges(append(result.Edges, buildOperationalEdgesFromSegments(result.SupplyLinkSegments)...))

	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" && job.State != nil {
		lensID = parseStateString(job.State["lensId"])
	}

	{
		msg := fmt.Sprintf("[SupplyChainParse] chains=%d nodes=%d edges=%d fragments=%d stats=%+v",
			len(chains), result.Stats.NodesTotal, len(result.Edges), len(result.Fragments), result.Stats)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
		trace.Log(ctx, "SupplyChain", "SupplyChain.parse.ok", map[string]any{
			"tenantId":    strings.TrimSpace(job.TenantID),
			"projectId":   strings.TrimSpace(job.ProjectID),
			"lensId":      lensID,
			"jobId":       strings.TrimSpace(job.ID),
			"target":      strings.TrimSpace(job.TargetCanonicalPath),
			"chainsN":     len(chains),
			"fragmentsN":  len(result.Fragments),
			"edgesN":      len(result.Edges),
			"parserStats": result.Stats,
			"chainContextLen": func() int {
				if job.State == nil {
					return 0
				}
				s, _ := job.State["chainContext"].(string)
				return len(s)
			}(),
		})
	}

	// Map canonical path -> fragment ID for edge creation
	pathToID := make(map[string]string)
	pathToFragment := make(map[string]*models.HorizonFragment)
	if sourceFragment != nil && sourceFragment.ID != "" {
		pathToID[sourceFragment.CanonicalPath] = sourceFragment.ID
		pathToFragment[sourceFragment.CanonicalPath] = sourceFragment
	}
	// Track canonical-path rewrites within this supply-chain run so edge endpoints remain consistent
	// even when we normalize (legacy geo -> /loc, /loc country aliases, product taxonomy upgrades, etc.).
	pathRewrites := map[string]string{} // originalPath -> canonicalPath

	// Chain reuse counters (populated in materialization loop, used in summary).
	chainReuseSkipped := 0
	chainReuseMerged := 0
	chainReuseScopeAdded := 0
	var familyBatches map[string]*supplyChainFamilyBatch

	parsedChains := make([]*ParsedChainResult, len(chains))
	for idx, chain := range chains {
		singleParsed, perr := parser.ParseSupplyChains([]agents.SupplyChain{chain}, sourceCanonicalPath)
		if perr != nil || singleParsed == nil {
			continue
		}
		singleParsed.SupplyLinkSegments = normalizeAndDedupSupplyLinkSegments(singleParsed.SupplyLinkSegments)
		singleParsed.Edges = dedupeChainEdges(append(singleParsed.Edges, buildOperationalEdgesFromSegments(singleParsed.SupplyLinkSegments)...))
		parsedChains[idx] = singleParsed
	}

	// Persist fragments discovered in supply chains
	createdFrags := 0
	updatedFrags := 0
	createErrors := 0
	previewLimit := 20
	type preparedSupplyChainFragment struct {
		fragment *models.HorizonFragment
	}
	preparedFragments := make([]preparedSupplyChainFragment, 0, len(result.Fragments))
	fragmentCanonicalPaths := make([]string, 0, len(result.Fragments))
	for canonicalPath, frag := range result.Fragments {
		if frag == nil {
			continue
		}
		origPath := strings.TrimSpace(canonicalPath)
		effectivePath := origPath
		if strings.TrimSpace(frag.CanonicalPath) != "" {
			effectivePath = strings.TrimSpace(frag.CanonicalPath)
		}
		if effectivePath == "" {
			continue
		}

		// Skip the source fragment itself
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) != "" && effectivePath == strings.TrimSpace(sourceFragment.CanonicalPath) {
			continue
		}

		// IMPORTANT: Horizon fragments are global-scoped (except SUPPLY_LINK).
		// If tenantId != global/empty, fragment.Validate() will fail and nothing will be created.
		frag.TenantID = "global"
		frag.Status = "active"
		frag.Governance.Owner = "SYSTEM"
		frag.Governance.Status = string(models.FragmentStatusActive)
		// Ensure Type is set (best-effort) before canonicalization/dedup.
		if frag.Type == "" {
			frag.Type = InferFragmentTypeFromCanonicalPath(effectivePath)
		}
		// Early canonicalization + dedup (best-effort): normalize canonicalPath and reuse existing fragments when found.
		if e.canonicalPathResolver != nil {
			if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, effectivePath, frag.Type); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				if strings.TrimSpace(resolved.CanonicalPath) != strings.TrimSpace(effectivePath) {
					pathRewrites[effectivePath] = strings.TrimSpace(resolved.CanonicalPath)
					if origPath != "" {
						pathRewrites[origPath] = strings.TrimSpace(resolved.CanonicalPath)
					}
					effectivePath = strings.TrimSpace(resolved.CanonicalPath)
				}
				if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
					pathToID[effectivePath] = strings.TrimSpace(resolved.ExistingFragment.ID)
					pathToFragment[effectivePath] = resolved.ExistingFragment
					if job != nil {
						e.setSupplyChainJobFragmentRef(ctx, job.ID, resolved.ExistingFragment)
					}
					continue
				}
			}
		}
		frag.CanonicalPath = effectivePath
		// Keep LastVerifiedAt zero so freshness checks won't skip scouting for newly discovered nodes.
		if frag.Metadata.CreatedAt.IsZero() {
			frag.Metadata.CreatedAt = time.Now().UTC()
		}
		frag.Metadata.LastUpdatedAt = time.Now().UTC()
		if frag.Metadata.EpistemicStatus == "" {
			frag.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}
		if frag.Provenance.Method == "" {
			frag.Provenance.Method = "scout_supply_chain"
		}
		if len(frag.Provenance.SourceIDs) == 0 && job != nil {
			frag.Provenance.SourceIDs = []string{job.ID}
		}

		// Pre-create validation/dedup (phonetic persons, org alias, geo enrichment, etc.)
		preCreatePath := strings.TrimSpace(frag.CanonicalPath)
		if e.preCreateValidator != nil {
			if pre, perr := e.preCreateValidator.ValidateAndProcess(ctx, frag); perr == nil && pre != nil {
				if pre.ReplacementFrag != nil && strings.TrimSpace(pre.ReplacementFrag.ID) != "" {
					cp := strings.TrimSpace(pre.ReplacementFrag.CanonicalPath)
					if cp != "" {
						pathToID[cp] = strings.TrimSpace(pre.ReplacementFrag.ID)
						pathToFragment[cp] = pre.ReplacementFrag
						if origPath != "" {
							pathRewrites[origPath] = cp
						}
						if effectivePath != "" {
							pathRewrites[effectivePath] = cp
						}
						if job != nil {
							e.setSupplyChainJobFragmentRef(ctx, job.ID, pre.ReplacementFrag)
						}
					}
					continue
				}
				if !pre.IsValid && strings.TrimSpace(pre.BlockReason) != "" {
					msg := fmt.Sprintf("[SupplyChainFragmentCreate] BLOCKED canonicalPath=%s reason=%s", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(pre.BlockReason))
					fmt.Printf("%s\n", msg)
					continue
				}
			}
		}
		postCreatePath := strings.TrimSpace(frag.CanonicalPath)
		if preCreatePath != "" && postCreatePath != "" && preCreatePath != postCreatePath {
			pathRewrites[preCreatePath] = postCreatePath
			if origPath != "" {
				pathRewrites[origPath] = postCreatePath
			}
			if effectivePath != "" {
				pathRewrites[effectivePath] = postCreatePath
			}
			effectivePath = postCreatePath
		}
		preparedFragments = append(preparedFragments, preparedSupplyChainFragment{fragment: frag})
		fragmentCanonicalPaths = append(fragmentCanonicalPaths, frag.CanonicalPath)
	}

	existingFragmentsByPath, batchGetErr := e.fragmentsRepo.BatchGetByCanonicalPaths(ctx, fragmentCanonicalPaths)
	if batchGetErr != nil {
		createErrors++
		msg := fmt.Sprintf("[SupplyChainFragmentLookup] FAILED batch err=%v", batchGetErr)
		fmt.Printf("%s\n", msg)
	}
	for _, prepared := range preparedFragments {
		frag := prepared.fragment
		if frag == nil {
			continue
		}
		existing := existingFragmentsByPath[strings.TrimSpace(frag.CanonicalPath)]
		if existing == nil && job != nil {
			if cached := e.getSupplyChainJobFragmentRef(ctx, job.ID, frag.CanonicalPath); cached != nil && strings.TrimSpace(cached.ID) != "" {
				existing = &models.HorizonFragment{
					ID:            strings.TrimSpace(cached.ID),
					CanonicalPath: strings.TrimSpace(cached.CanonicalPath),
					Type:          models.FragmentType(strings.TrimSpace(cached.Type)),
					TenantID:      strings.TrimSpace(cached.TenantID),
				}
			}
		}
		if existing == nil {
			frag.Metadata.CreatedAt = time.Now().UTC()
			frag.Metadata.LastUpdatedAt = time.Now().UTC()
			frag.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence

			id, createErr := e.fragmentsRepo.CreateAssumeAbsent(ctx, frag)
			if createErr != nil {
				createErrors++
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] FAILED canonicalPath=%s err=%v", frag.CanonicalPath, createErr)
				fmt.Printf("%s\n", msg)
				continue
			}
			frag.ID = id
			pathToID[frag.CanonicalPath] = id
			pathToFragment[frag.CanonicalPath] = frag
			if job != nil {
				e.setSupplyChainJobFragmentRef(ctx, job.ID, frag)
			}
			e.bestEffortEnrichRelationTargetGeo(ctx, frag, sourceFragment, "scout_supply_chain", job.ID)
			if lensID != "" && isScopeEligibleEntityType(frag.Type) {
				e.bestEffortDeferredScopeAddForJob(ctx, job, lensID, frag.CanonicalPath)
			}
			createdFrags++
			if createdFrags <= previewLimit {
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] OK canonicalPath=%s id=%s type=%s", frag.CanonicalPath, id, frag.Type)
				fmt.Printf("%s\n", msg)
			}
			continue
		}
		pathToID[frag.CanonicalPath] = existing.ID
		pathToFragment[frag.CanonicalPath] = existing
		if job != nil {
			e.setSupplyChainJobFragmentRef(ctx, job.ID, existing)
		}
		e.bestEffortEnrichRelationTargetGeo(ctx, existing, sourceFragment, "scout_supply_chain", job.ID)
		if lensID != "" && isScopeEligibleEntityType(existing.Type) {
			e.bestEffortDeferredScopeAddForJob(ctx, job, lensID, existing.CanonicalPath)
		}

		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		if existing.Identifiers == nil {
			existing.Identifiers = make(map[string]string)
		}
		updated := false
		for k, v := range frag.Labels {
			if existing.Labels[k] == "" && v != "" {
				existing.Labels[k] = v
				updated = true
			}
		}
		for k, v := range frag.Identifiers {
			if existing.Identifiers[k] == "" && v != "" {
				existing.Identifiers[k] = v
				updated = true
			}
		}
		if updated {
			existing.Metadata.LastUpdatedAt = time.Now().UTC()
			if err := e.fragmentsRepo.UpdateFast(ctx, existing); err != nil {
				msg := fmt.Sprintf("[SupplyChainFragmentUpdate] FAILED canonicalPath=%s id=%s err=%v", frag.CanonicalPath, existing.ID, err)
				fmt.Printf("%s\n", msg)
			} else {
				updatedFrags++
				if updatedFrags <= previewLimit {
					msg := fmt.Sprintf("[SupplyChainFragmentUpdate] OK canonicalPath=%s id=%s", frag.CanonicalPath, existing.ID)
					fmt.Printf("%s\n", msg)
				}
			}
		}
	}

	// Pre-compute artifact keys per chain so edges can be annotated with chain membership.
	// This maps "srcPath|dstPath" -> artifactCanonicalKey for each edge in each chain.
	edgeArtifactKeyMap := make(map[string]string)
	if e.supplyChainPathBuilder != nil {
		anchorPath := ""
		if sourceFragment != nil {
			anchorPath = strings.TrimSpace(sourceFragment.CanonicalPath)
		}
		for idx, singleParsed := range parsedChains {
			if singleParsed == nil || len(singleParsed.SupplyLinkSegments) == 0 {
				continue
			}
			segments := buildSegmentsFromParsed(singleParsed, pathRewrites, nil)
			if len(segments) == 0 {
				continue
			}
			canonicalKey := pathbuilder.BuildStructuralCanonicalKey("global", segments)
			if canonicalKey == "" {
				canonicalKey = buildSupplyChainArtifactCanonicalKey(job, anchorPath, idx, segments)
			}
			if canonicalKey == "" {
				continue
			}
			for _, ce := range singleParsed.Edges {
				key := strings.TrimSpace(ce.SourceCanonicalPath) + "|" + strings.TrimSpace(ce.TargetCanonicalPath)
				edgeArtifactKeyMap[key] = canonicalKey
			}
		}
	}

	// Create edges using fragment IDs
	// Supply chain strict validator: reject org→org, forbidden types, non-facility endpoints.
	scValidator := NewSupplyChainStrictValidator()
	provenanceMethod := "scout_supply_chain"
	createdEdges := 0
	edgeDupes := 0
	edgeCreateErrors := 0
	edgeReversed := 0
	edgePenalized := 0
	acceptedContextEdges := make([]*ChainEdge, 0, len(result.Edges))
	edgePreviewLimit := 40
	jobIDStr := ""
	if job != nil {
		jobIDStr = strings.TrimSpace(job.ID)
	}
	unresolvedEdgePaths := make([]string, 0, len(result.Edges)*2)
	for _, edge := range result.Edges {
		if edge == nil {
			continue
		}
		rewritten := rewriteChainEdgeWithPathRewrites(edge, pathRewrites)
		if rewritten == nil {
			continue
		}
		for _, cp := range []string{strings.TrimSpace(rewritten.SourceCanonicalPath), strings.TrimSpace(rewritten.TargetCanonicalPath)} {
			if cp == "" || pathToID[cp] != "" {
				continue
			}
			if job != nil {
				if cached := e.getSupplyChainJobFragmentRef(ctx, job.ID, cp); cached != nil && strings.TrimSpace(cached.ID) != "" {
					pathToID[cp] = strings.TrimSpace(cached.ID)
					pathToFragment[cp] = &models.HorizonFragment{
						ID:            strings.TrimSpace(cached.ID),
						CanonicalPath: strings.TrimSpace(cached.CanonicalPath),
						Type:          models.FragmentType(strings.TrimSpace(cached.Type)),
						TenantID:      strings.TrimSpace(cached.TenantID),
					}
					continue
				}
			}
			unresolvedEdgePaths = append(unresolvedEdgePaths, cp)
		}
	}
	if len(unresolvedEdgePaths) > 0 {
		if resolvedFragments, err := e.fragmentsRepo.BatchGetByCanonicalPaths(ctx, unresolvedEdgePaths); err == nil {
			for cp, fragment := range resolvedFragments {
				if fragment == nil {
					continue
				}
				pathToID[cp] = strings.TrimSpace(fragment.ID)
				pathToFragment[cp] = fragment
				if job != nil {
					e.setSupplyChainJobFragmentRef(ctx, job.ID, fragment)
				}
			}
		}
	}
	ensureFragment := func(path string) *models.HorizonFragment {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil
		}
		if fragment := pathToFragment[path]; fragment != nil && strings.TrimSpace(fragment.ID) != "" {
			return fragment
		}
		if id := pathToID[path]; id != "" {
			fragment := &models.HorizonFragment{ID: id, CanonicalPath: path, Type: InferFragmentTypeFromCanonicalPath(path), TenantID: "global"}
			pathToFragment[path] = fragment
			return fragment
		}
		ft := InferFragmentTypeFromCanonicalPath(path)
		if ft == "" {
			return nil
		}
		switch ft {
		case models.FragmentTypeProgram, models.FragmentTypeMarket, models.FragmentTypeCommunity,
			models.FragmentTypeOrganization, models.FragmentTypeFacility, models.FragmentTypeProduct,
			models.FragmentTypeGeoRegion, models.FragmentTypeDeposit:
		default:
			return nil
		}
		label := ""
		pathParts := strings.Split(strings.Trim(path, "/"), "/")
		if len(pathParts) > 0 {
			label = strings.ReplaceAll(strings.TrimSpace(pathParts[len(pathParts)-1]), "-", " ")
		}
		if label == "" {
			label = string(ft)
		}
		fragment := &models.HorizonFragment{
			TenantID:      "global",
			CanonicalPath: path,
			Type:          ft,
			Labels:        map[string]string{"en": label},
			Identifiers:   map[string]string{},
			State:         make(map[string]models.MetricValue),
			Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
			Metadata:      models.FragmentMetadata{CreatedAt: time.Now().UTC(), LastUpdatedAt: time.Now().UTC(), EpistemicStatus: models.EpistemicStatusLowConfidence},
			Provenance:    models.Provenance{Method: "scout_supply_chain"},
			Status:        "active",
		}
		if job != nil {
			fragment.Provenance.SourceIDs = []string{job.ID}
		}
		id, err := e.fragmentsRepo.CreateAssumeAbsent(ctx, fragment)
		if err != nil {
			return nil
		}
		fragment.ID = id
		pathToID[path] = id
		pathToFragment[path] = fragment
		if job != nil {
			e.setSupplyChainJobFragmentRef(ctx, job.ID, fragment)
		}
		createdFrags++
		if createdFrags <= previewLimit {
			e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainFragmentPrecreate] OK path=%s id=%s type=%s", path, id, ft))
		}
		return fragment
	}
	resolveTypeByCanonical := func(cp string) models.FragmentType {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			return ""
		}
		resolveFromPath := func(path string, current models.FragmentType) models.FragmentType {
			inferred := InferFragmentTypeFromCanonicalPath(path)
			if inferred == "" {
				return current
			}
			if current == "" || current != inferred {
				return inferred
			}
			return current
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == cp {
			return resolveFromPath(cp, sourceFragment.Type)
		}
		if f := pathToFragment[cp]; f != nil {
			return resolveFromPath(cp, f.Type)
		}
		return resolveFromPath(cp, "")
	}
	preparedEdgeCandidates := make([]*preparedSupplyChainEdgeCandidate, 0, len(result.Edges))
	for edgeIdx, chainEdge := range result.Edges {
		if chainEdge == nil {
			continue
		}
		origSrcPath := strings.TrimSpace(chainEdge.SourceCanonicalPath)
		origDstPath := strings.TrimSpace(chainEdge.TargetCanonicalPath)
		chainEdge = rewriteChainEdgeWithPathRewrites(chainEdge, pathRewrites)
		if chainEdge == nil {
			continue
		}
		srcPath := strings.TrimSpace(chainEdge.SourceCanonicalPath)
		dstPath := strings.TrimSpace(chainEdge.TargetCanonicalPath)
		// Strict validation: reject invalid supply chain edges (no transform, no persist)
		edgeToValidate := &ChainEdge{
			SourceCanonicalPath: srcPath,
			TargetCanonicalPath: dstPath,
			RelationType:        chainEdge.RelationType,
		}
		if err := scValidator.ValidateEdge(edgeToValidate, jobIDStr, edgeIdx); err != nil {
			e.logLine(ctx, job, "warn", err.Error())
			continue
		}

		sourceID := pathToID[srcPath]
		targetID := pathToID[dstPath]

		if sourceID == "" || targetID == "" {
			if sourceID == "" {
				if e.canonicalPathResolver != nil && srcPath != "" {
					if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, srcPath, InferFragmentTypeFromCanonicalPath(srcPath)); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
						if strings.TrimSpace(resolved.CanonicalPath) != srcPath {
							pathRewrites[srcPath] = strings.TrimSpace(resolved.CanonicalPath)
							srcPath = strings.TrimSpace(resolved.CanonicalPath)
						}
						if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
							sourceID = strings.TrimSpace(resolved.ExistingFragment.ID)
							pathToID[srcPath] = sourceID
							pathToFragment[srcPath] = resolved.ExistingFragment
							if job != nil {
								e.setSupplyChainJobFragmentRef(ctx, job.ID, resolved.ExistingFragment)
							}
						}
					}
				}
				if sourceID == "" {
					if fragment := ensureFragment(srcPath); fragment != nil {
						sourceID = strings.TrimSpace(fragment.ID)
					}
				}
			}
			if targetID == "" {
				if e.canonicalPathResolver != nil && dstPath != "" {
					if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, dstPath, InferFragmentTypeFromCanonicalPath(dstPath)); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
						if strings.TrimSpace(resolved.CanonicalPath) != dstPath {
							pathRewrites[dstPath] = strings.TrimSpace(resolved.CanonicalPath)
							dstPath = strings.TrimSpace(resolved.CanonicalPath)
						}
						if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
							targetID = strings.TrimSpace(resolved.ExistingFragment.ID)
							pathToID[dstPath] = targetID
							pathToFragment[dstPath] = resolved.ExistingFragment
							if job != nil {
								e.setSupplyChainJobFragmentRef(ctx, job.ID, resolved.ExistingFragment)
							}
						}
					}
				}
				if targetID == "" {
					if fragment := ensureFragment(dstPath); fragment != nil {
						targetID = strings.TrimSpace(fragment.ID)
					}
				}
			}
			if sourceID == "" || targetID == "" {
				if sourceID == "" {
					if fragment := ensureFragment(srcPath); fragment != nil {
						sourceID = strings.TrimSpace(fragment.ID)
					}
				}
				if targetID == "" {
					if fragment := ensureFragment(dstPath); fragment != nil {
						targetID = strings.TrimSpace(fragment.ID)
					}
				}
			}
			if sourceID == "" || targetID == "" {
				msg := fmt.Sprintf("[SupplyChainEdge] SKIP missingFragmentId source=%s target=%s type=%s",
					srcPath, dstPath, chainEdge.RelationType)
				fmt.Printf("%s\n", msg)
				e.logLine(ctx, job, "warn", msg)
				continue
			}
		}

		if err := scValidator.ValidateEdgeAgainstFragments(chainEdge, pathToFragment[srcPath], pathToFragment[dstPath], jobIDStr, edgeIdx); err != nil {
			e.logLine(ctx, job, "warn", err.Error())
			continue
		}

		edgeType := models.DependencyType(chainEdge.RelationType)
		productRelationKind := ""
		productRelationDictionaryNormalized := false
		dictionaryDirectionChanged := false
		dictionaryTypeChanged := false
		dictionaryNormalizedFromEdgeType := ""
		if e.canonicalPathResolver != nil && e.canonicalPathResolver.productDictionary != nil &&
			strings.HasPrefix(strings.TrimSpace(srcPath), "/product/") &&
			strings.HasPrefix(strings.TrimSpace(dstPath), "/product/") {
			resolution, rerr := e.canonicalPathResolver.productDictionary.ResolveProductRelationForTenant(
				ctx,
				strings.TrimSpace(firstNonEmpty(job.TenantID, "global")),
				srcPath,
				dstPath,
			)
			if rerr != nil {
				e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainEdge] product relation dictionary lookup failed source=%s target=%s err=%v", srcPath, dstPath, rerr))
			} else if resolution.Known {
				productRelationKind = resolution.RelationKind
				productRelationDictionaryNormalized = true
				if resolution.Swapped {
					sourceID, targetID = targetID, sourceID
					srcPath, dstPath = resolution.FromPath, resolution.ToPath
					dictionaryDirectionChanged = true
				}
				if edgeType != resolution.EdgeType {
					dictionaryNormalizedFromEdgeType = string(edgeType)
					edgeType = resolution.EdgeType
					dictionaryTypeChanged = true
				}
			}
		}

		// Policy: self-loop prevention
		if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(targetID) == "" || IsSelfLoop(sourceID, targetID) {
			continue
		}

		srcTypeActual := resolveTypeByCanonical(srcPath)
		dstTypeActual := resolveTypeByCanonical(dstPath)

		// Canonicalization policy (stage-rank direction fix for TRANSFORMS_INTO, type rewrites, gov/market rules).
		// Must run before validation policy so that edge type and direction are corrected first.
		// SupplyChainMode: true — this path is always supply-chain edge materialization.
		canon := policy.ApplyEdgeCanonicalizationPolicy(policy.EdgeCanonicalizationInput{
			EdgeType:        edgeType,
			SourceType:      srcTypeActual,
			TargetType:      dstTypeActual,
			SourcePath:      srcPath,
			TargetPath:      dstPath,
			Reason:          strings.TrimSpace(chainEdge.Reason),
			Qualifiers:      chainEdge.Qualifiers,
			SupplyChainMode: true,
		})
		if canon.Changed {
			edgeType = canon.EdgeType
			if canon.ShouldSwap {
				sourceID, targetID = targetID, sourceID
				srcPath, dstPath = dstPath, srcPath
				srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
			}
			sourceID, targetID, edgeType, _ = NormalizeSymmetricEdge(sourceID, targetID, edgeType)
		}

		// Policy: direction validation & reversal (best-effort, same behavior as processDiscoveredRelations).
		edgeCriticality := normalizeEdgeConfidence(chainEdge.Confidence)
		directionChanged := dictionaryDirectionChanged || dictionaryTypeChanged
		policyStatus := models.PolicyStatusClean
		policyReason := ""
		policyPenalty := 0.0
		if canon.PolicyPenalty > 0 {
			policyPenalty = canon.PolicyPenalty
			policyStatus = policy.MergePolicyStatus(policyStatus, canon.PolicyStatus)
			if strings.TrimSpace(canon.PolicyReason) != "" {
				policyReason = strings.TrimSpace(canon.PolicyReason)
			}
			edgeCriticality = edgeCriticality * canon.ConfidenceMult
			if edgeCriticality < 0 {
				edgeCriticality = 0
			}
		}
		if e.validationPolicy != nil {
			policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageInput, map[string]interface{}{
				"trace_id":       "na",
				"tenant_id":      job.TenantID,
				"job_id":         job.ID,
				"source_type":    srcTypeActual,
				"target_type":    dstTypeActual,
				"relation_type":  edgeType,
				"action":         "validate_edge_supply_chain",
				"policy_status":  "na",
				"policy_reason":  "",
				"policy_penalty": 0,
				"decision_path":  "runtime_supply_chain",
				"confidence":     0,
			})
			decision, derr := e.validationPolicy.Execute(ctx, policy.PolicyInput{
				Kind: "edge",
				Context: map[string]interface{}{
					"validationKind":      "edge",
					"fromSupplyChain":     true,
					"sourceFragmentID":    sourceID,
					"targetFragmentID":    targetID,
					"sourceType":          string(srcTypeActual),
					"targetType":          string(dstTypeActual),
					"sourceCanonicalPath": srcPath,
					"targetCanonicalPath": dstPath,
					"relationType":        string(edgeType),
					"reason":              strings.TrimSpace(chainEdge.Reason),
					"qualifiers":          chainEdge.Qualifiers,
				},
				Fragment: nil,
			})
			if derr == nil {
				logPrefix := "[SupplyChainPolicy]"
				reason := ""
				if rv, _ := decision.Params["reason"].(string); strings.TrimSpace(rv) != "" {
					reason = strings.TrimSpace(rv)
				}
				policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageDecision, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      job.TenantID,
					"job_id":         job.ID,
					"source_type":    srcTypeActual,
					"target_type":    dstTypeActual,
					"relation_type":  edgeType,
					"action":         decision.Action,
					"policy_status":  policyStatus,
					"policy_reason":  reason,
					"policy_penalty": policyPenalty,
					"decision_path":  decision.DecisionPath,
					"confidence":     decision.Confidence,
				})
				policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
					Reverse: func() {
						sourceID, targetID = targetID, sourceID
						srcPath, dstPath = dstPath, srcPath
						srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
						directionChanged = true
						edgeReversed++
						msg := fmt.Sprintf("%s REVERSE edge=%s -[%s]-> %s path=%s",
							logPrefix, chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, decision.DecisionPath)
						e.logLine(ctx, job, "info", msg)
					},
					RewriteType: func(newType string, _ string) {
						newEdgeType := models.DependencyType(strings.TrimSpace(newType))
						if newEdgeType == "" || newEdgeType == edgeType {
							return
						}
						edgeType = newEdgeType
						directionChanged = true
					},
					ApplyPenalty: func(penalty float64, _ string) {
						before := edgeCriticality
						edgeCriticality = edgeCriticality * (1.0 - penalty)
						edgePenalized++
						msg := fmt.Sprintf("%s PENALTY edge=%s -[%s]-> %s path=%s conf=%.3f->%.3f",
							logPrefix, chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, decision.DecisionPath, before, edgeCriticality)
						e.logLine(ctx, job, "warn", msg)
					},
					Warn: func(msg string) {
						e.logLine(ctx, job, "warn", fmt.Sprintf("%s %s", logPrefix, msg))
					},
					SetPolicyStatus: func(status, reason string, penalty float64) {
						policyStatus = policy.MergePolicyStatus(policyStatus, status)
						if strings.TrimSpace(reason) != "" {
							policyReason = strings.TrimSpace(reason)
						}
						if penalty > policyPenalty {
							policyPenalty = penalty
						}
					},
				})
			} else {
				msg := fmt.Sprintf("[SupplyChainPolicy] ERROR validationPolicy err=%v", derr)
				policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageError, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      job.TenantID,
					"job_id":         job.ID,
					"source_type":    srcTypeActual,
					"target_type":    dstTypeActual,
					"relation_type":  edgeType,
					"action":         "validate_edge_supply_chain",
					"policy_status":  "na",
					"policy_reason":  derr.Error(),
					"policy_penalty": 0,
					"decision_path":  "runtime_supply_chain",
					"error_class":    "policy_execution_error",
					"confidence":     0,
				})
				e.logLine(ctx, job, "warn", msg)
			}
		}

		// Policy: block edge creation when validation policy returned quarantine (business-ilogical edges must not enter graph)
		if policyStatus == models.PolicyStatusQuarantined {
			msg := fmt.Sprintf("[SupplyChainEdge] SKIP quarantine source=%s target=%s type=%s reason=%s",
				srcPath, dstPath, edgeType, policyReason)
			fmt.Printf("%s\n", msg)
			e.logLine(ctx, job, "warn", msg)
			continue
		}

		acceptedContextEdges = append(acceptedContextEdges, &ChainEdge{
			SourceCanonicalPath: srcPath,
			TargetCanonicalPath: dstPath,
			RelationType:        string(edgeType),
			Confidence:          chainEdge.Confidence,
			Reason:              chainEdge.Reason,
			Qualifiers:          cloneQualifierMap(chainEdge.Qualifiers),
		})

		qualifiers := map[string]interface{}{
			"reason":              chainEdge.Reason,
			"sourceCanonicalPath": srcPath,
			"targetCanonicalPath": dstPath,
			"rawConfidence":       chainEdge.Confidence,
			"fromSupplyChain":     true,
		}
		if ak := edgeArtifactKeyMap[srcPath+"|"+dstPath]; ak != "" {
			qualifiers["artifactKey"] = ak
		} else if ak := edgeArtifactKeyMap[origSrcPath+"|"+origDstPath]; ak != "" {
			qualifiers["artifactKey"] = ak
		}
		if chainEdge.Qualifiers != nil {
			for k, v := range chainEdge.Qualifiers {
				if k != "" && v != nil {
					qualifiers[k] = v
				}
			}
		}
		if productRelationKind != "" {
			qualifiers["productRelationKind"] = productRelationKind
		}
		if productRelationDictionaryNormalized {
			qualifiers["normalizedByProductRelationDictionary"] = true
		}
		if dictionaryNormalizedFromEdgeType != "" {
			qualifiers["normalizedFromEdgeType"] = dictionaryNormalizedFromEdgeType
		}
		// Canonicalize qualifier facility paths (at_facility, transport_via) — same rules as other fragments
		qualifiers = e.canonicalizeQualifierFacilityPaths(ctx, qualifiers, pathRewrites)
		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sourceID,
			TargetFragmentID: targetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   edgeCriticality,
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Qualifiers: qualifiers,
			Provenance: models.Provenance{
				Method:    provenanceMethod,
				SourceIDs: []string{job.ID},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     time.Now().UTC(),
				LastUpdatedAt: time.Now().UTC(),
			},
		}
		if directionChanged {
			edge.Qualifiers["normalizedDirection"] = true
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
		}
		edge.SetPolicyStatus(policyStatus, policyReason, policyPenalty)
		applyBestEffortEdgeEnrichment(edge, srcPath, dstPath)
		edge.SetSubstitutability(supplyChainSubstitutabilityHeuristic(edgeType, srcPath, dstPath))
		if edge.Qualifiers != nil {
			edge.Qualifiers[models.QualifierThroughputShare] = 1.0
		}
		applyPathStressToSupplyEdge(edge, chainEdge)

		// Final allowlist gate: never persist out-of-contract types for supply-chain edges.
		allowed := IsAllowedSupplyChainPersistRelation(srcPath, dstPath, edgeType)
		if !allowed {
			e.logLine(ctx, job, "info", fmt.Sprintf("> persist_final_gate_supply_chain: skipped type=%q source=%q target=%q reason=out_of_contract",
				edgeType, srcPath, dstPath))
			continue
		}

		if err := edge.Validate(); err != nil {
			edgeCreateErrors++
			e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainEdgeCreate] INVALID %s -[%s]-> %s err=%v",
				chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, err))
			continue
		}
		preparedEdgeCandidates = append(preparedEdgeCandidates, &preparedSupplyChainEdgeCandidate{
			edge:            edge,
			sourceID:        sourceID,
			targetID:        targetID,
			sourcePath:      srcPath,
			targetPath:      dstPath,
			artifactKey:     edgeArtifactKeyMap[srcPath+"|"+dstPath],
			variantKey:      chainEdgeVariantKey(string(edgeType), edge.Qualifiers),
			dedupKey:        buildSupplyChainEdgeDedupKey(sourceID, targetID, edgeType, edge.Qualifiers),
			contextEdge:     acceptedContextEdges[len(acceptedContextEdges)-1],
			rawConfidence:   chainEdge.Confidence,
			originalSrcPath: origSrcPath,
			originalDstPath: origDstPath,
		})
	}

	existingEdgeDedupKeys := collectExistingSupplyChainEdgeDedupKeys(ctx, e.edgesRepo, "global", preparedEdgeCandidates)
	localEdgeDedupKeys := make(map[string]struct{}, len(preparedEdgeCandidates))
	edgesToCreate := make([]*models.DependencyEdge, 0, len(preparedEdgeCandidates))
	createdCandidates := make([]*preparedSupplyChainEdgeCandidate, 0, len(preparedEdgeCandidates))
	for _, candidate := range preparedEdgeCandidates {
		if candidate == nil || candidate.edge == nil {
			continue
		}
		if _, ok := existingEdgeDedupKeys[candidate.dedupKey]; ok || e.hasSupplyChainEdgeSeen(ctx, candidate.dedupKey) {
			edgeDupes++
			continue
		}
		if _, ok := localEdgeDedupKeys[candidate.dedupKey]; ok {
			edgeDupes++
			continue
		}
		localEdgeDedupKeys[candidate.dedupKey] = struct{}{}
		edgesToCreate = append(edgesToCreate, candidate.edge)
		createdCandidates = append(createdCandidates, candidate)
	}
	if len(edgesToCreate) > 0 {
		ids, batchErr := e.edgesRepo.CreateMany(ctx, edgesToCreate)
		if batchErr != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainEdgeCreate] batch failed, fallback to single create: %v", batchErr))
			for _, candidate := range createdCandidates {
				id, createErr := e.edgesRepo.Create(ctx, candidate.edge)
				if createErr != nil {
					edgeCreateErrors++
					msg := fmt.Sprintf("[SupplyChainEdgeCreate] FAILED %s -[%s]-> %s err=%v",
						candidate.sourcePath, candidate.edge.Type, candidate.targetPath, createErr)
					fmt.Printf("%s\n", msg)
					continue
				}
				candidate.edge.ID = id
				createdEdges++
				e.markSupplyChainEdgeSeen(ctx, candidate.dedupKey)
				if createdEdges <= edgePreviewLimit {
					fmt.Printf("[SupplyChainEdgeCreate] OK %s -[%s]-> %s id=%s crit=%.3f raw=%.3f\n",
						candidate.sourcePath, candidate.edge.Type, candidate.targetPath, id, candidate.edge.Properties.Criticality, candidate.rawConfidence)
				}
				e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
					"tenantId":            job.TenantID,
					"projectId":           job.ProjectID,
					"lensId":              lensID,
					"edgeId":              candidate.edge.ID,
					"sourceFragmentId":    candidate.edge.SourceFragmentID,
					"targetFragmentId":    candidate.edge.TargetFragmentID,
					"sourceCanonicalPath": candidate.sourcePath,
					"targetCanonicalPath": candidate.targetPath,
					"type":                string(candidate.edge.Type),
					"properties":          candidate.edge.Properties,
					"qualifiers":          candidate.edge.Qualifiers,
					"fromSupplyChain":     true,
				})
				e.enqueueEdgeTriggeredPropagation(ctx, candidate.edge, job)
			}
		} else {
			for idx, id := range ids {
				if idx >= len(createdCandidates) {
					break
				}
				candidate := createdCandidates[idx]
				candidate.edge.ID = id
				createdEdges++
				e.markSupplyChainEdgeSeen(ctx, candidate.dedupKey)
				if createdEdges <= edgePreviewLimit {
					fmt.Printf("[SupplyChainEdgeCreate] OK %s -[%s]-> %s id=%s crit=%.3f raw=%.3f\n",
						candidate.sourcePath, candidate.edge.Type, candidate.targetPath, id, candidate.edge.Properties.Criticality, candidate.rawConfidence)
				}
				e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
					"tenantId":            job.TenantID,
					"projectId":           job.ProjectID,
					"lensId":              lensID,
					"edgeId":              candidate.edge.ID,
					"sourceFragmentId":    candidate.edge.SourceFragmentID,
					"targetFragmentId":    candidate.edge.TargetFragmentID,
					"sourceCanonicalPath": candidate.sourcePath,
					"targetCanonicalPath": candidate.targetPath,
					"type":                string(candidate.edge.Type),
					"properties":          candidate.edge.Properties,
					"qualifiers":          candidate.edge.Qualifiers,
					"fromSupplyChain":     true,
				})
				e.enqueueEdgeTriggeredPropagation(ctx, candidate.edge, job)
			}
		}
	}

	// Materialize SupplyLink fragments from parsed segments so PathBuilder can traverse
	// SupplyLink provenance: Scout/Research supply chains → parser → SupplyLink fragments
	// Full chain org→fac→product→fac→org. Keep org as-is; do not convert.
	materializedSegments := normalizeAndDedupSupplyLinkSegments(append(
		append([]*SupplyLinkSegment(nil), result.SupplyLinkSegments...),
		deriveSupplyLinkSegmentsFromEdges(acceptedContextEdges)...,
	))
	supplyLinksCreated := 0
	supplyLinksSkipped := 0
	materializedSegmentRefs := make(map[string]supplyLinkMaterializationRef, len(materializedSegments))
	capabilityProfileCache := make(map[string]*models.CapabilityProfile)
	type preparedSupplyLink struct {
		segment          *SupplyLinkSegment
		fragment         *models.HorizonFragment
		segmentLookupKey string
	}
	preparedSupplyLinks := make([]preparedSupplyLink, 0, len(materializedSegments))
	supplyLinkCanonicalPaths := make([]string, 0, len(materializedSegments))
	for _, seg := range materializedSegments {
		if seg == nil || !supplysemantics.IsMaterializableSegmentClass(seg.SegmentClass) {
			supplyLinksSkipped++
			continue
		}
		supplier := strings.TrimSpace(seg.SupplierRef)
		customer := strings.TrimSpace(seg.CustomerRef)
		product := strings.TrimSpace(seg.ProductRef)
		if supplier == "" || customer == "" || product == "" {
			continue
		}
		if !strings.HasPrefix(supplier, "/fac/") && !strings.HasPrefix(supplier, "/deposit/") && !strings.HasPrefix(supplier, "/org/") && !strings.HasPrefix(supplier, "/gov/") {
			continue
		}
		if !strings.HasPrefix(customer, "/fac/") && !strings.HasPrefix(customer, "/org/") && !strings.HasPrefix(customer, "/gov/") {
			continue
		}
		if rp, ok := pathRewrites[supplier]; ok && rp != "" {
			supplier = strings.TrimSpace(rp)
		}
		if rp, ok := pathRewrites[product]; ok && rp != "" {
			product = strings.TrimSpace(rp)
		}
		if rp, ok := pathRewrites[customer]; ok && rp != "" {
			customer = strings.TrimSpace(rp)
		}
		supplierRef := supplier
		customerRef := customer
		productRef := product
		transportViaRefs := make([]string, 0, len(seg.TransportViaRefs))
		for _, via := range seg.TransportViaRefs {
			via = strings.TrimSpace(via)
			if rp, ok := pathRewrites[via]; ok && rp != "" {
				via = strings.TrimSpace(rp)
			}
			if via != "" {
				transportViaRefs = append(transportViaRefs, via)
			}
		}
		segmentLookupKey := buildSupplyLinkSegmentLookupKey(supplier, product, customer, transportViaRefs)
		if cached := e.getSupplyLinkRefCached(ctx, segmentLookupKey); cached != nil && strings.TrimSpace(cached.CanonicalPath) != "" {
			materializedSegmentRefs[segmentLookupKey] = *cached
		}
		routeVariants := []models.SupplyLinkRouteVariant{
			models.NewSupplyLinkRouteVariant(transportViaRefs, effectiveVesselType(seg, nil), seg.Confidence),
		}
		slFrag := &models.HorizonFragment{
			Type:             models.FragmentTypeSupplyLink,
			TenantID:         "global",
			SupplierRef:      &supplierRef,
			ProductRef:       &productRef,
			CustomerRef:      &customerRef,
			TransportViaRefs: transportViaRefs,
			Status:           "active",
			Governance:       models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
			Provenance:       models.Provenance{Method: "scout_supply_chain", SourceIDs: []string{job.ID}},
			Metadata:         models.FragmentMetadata{CreatedAt: time.Now().UTC(), LastUpdatedAt: time.Now().UTC(), EpistemicStatus: models.EpistemicStatusLowConfidence},
		}
		slFrag.Qualifiers = map[string]interface{}{
			"fromSupplyChain": true,
			"segmentClass":    seg.SegmentClass,
			"routeVariants":   routeVariants,
		}
		if seg.FlowEdgeContract != nil {
			q := map[string]interface{}{
				"contractCompleteness": seg.FlowEdgeContract.ContractCompleteness,
				"validationGaps":       seg.FlowEdgeContract.ValidationGaps,
				"rawPayload":           seg.FlowEdgeContract.RawPayload,
			}
			if len(seg.FlowEdgeContract.ParseWarnings) > 0 {
				q["parseWarnings"] = seg.FlowEdgeContract.ParseWarnings
			}
			if seg.FlowEdgeContract.Provenance != nil {
				q["provenance"] = map[string]interface{}{
					"method":     seg.FlowEdgeContract.Provenance.Method,
					"source_ids": seg.FlowEdgeContract.Provenance.SourceIDs,
				}
			}
			slFrag.Qualifiers["flow_edge_contract"] = q
		}
		slFrag.State = map[string]models.MetricValue{"confidence": {Value: seg.Confidence, Confidence: 0.8}}
		if seg.State != nil {
			for k, v := range seg.State {
				slFrag.State[k] = v
			}
			if validFrom, validTo := supplyLinkValidityFromState(seg.State); validFrom != nil || validTo != nil {
				slFrag.ValidFrom = validFrom
				slFrag.ValidTo = validTo
			}
		}
		enrichSupplyLinkFragmentWithOperationalRisk(ctx, e, slFrag, seg, capabilityProfileCache)
		horizonrepo.PrepareFragmentForValidation(slFrag)
		if err := slFrag.Validate(); err != nil {
			supplyLinksSkipped++
			continue
		}
		preparedSupplyLinks = append(preparedSupplyLinks, preparedSupplyLink{
			segment:          seg,
			fragment:         slFrag,
			segmentLookupKey: segmentLookupKey,
		})
		supplyLinkCanonicalPaths = append(supplyLinkCanonicalPaths, slFrag.CanonicalPath)
	}

	existingSupplyLinksByPath, supplyLinkBatchErr := e.fragmentsRepo.BatchGetSupplyLinksByCanonicalPaths(ctx, supplyLinkCanonicalPaths)
	if supplyLinkBatchErr != nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyLinkMaterialize] preload failed: %v", supplyLinkBatchErr))
	}
	for _, prepared := range preparedSupplyLinks {
		seg := prepared.segment
		slFrag := prepared.fragment
		segmentLookupKey := prepared.segmentLookupKey
		existing := existingSupplyLinksByPath[strings.TrimSpace(slFrag.CanonicalPath)]
		if existing != nil && strings.TrimSpace(existing.ID) != "" {
			materializedSegmentRefs[segmentLookupKey] = supplyLinkMaterializationRef{
				ID:            strings.TrimSpace(existing.ID),
				CanonicalPath: strings.TrimSpace(existing.CanonicalPath),
				TenantID:      strings.TrimSpace(existing.TenantID),
			}
			e.setSupplyLinkRefCached(ctx, segmentLookupKey, materializedSegmentRefs[segmentLookupKey])
			changed := false
			if existing.State == nil {
				existing.State = make(map[string]models.MetricValue)
			}
			if mergeSupplyLinkMetricState(existing.State, slFrag.State) {
				changed = true
			}
			if slFrag.ValidFrom != nil && (existing.ValidFrom == nil || existing.ValidFrom.IsZero()) {
				existing.ValidFrom = slFrag.ValidFrom
				changed = true
			}
			if slFrag.ValidTo != nil && (existing.ValidTo == nil || existing.ValidTo.IsZero()) {
				existing.ValidTo = slFrag.ValidTo
				changed = true
			}
			// Explicitly keep max confidence regardless of unit/value replacement policy.
			existingConf := 0.0
			if mv, ok := existing.State["confidence"]; ok {
				existingConf = mv.Value
			}
			if seg.Confidence > existingConf {
				existing.State["confidence"] = models.MetricValue{Value: seg.Confidence, Confidence: 0.8}
				changed = true
			}
			existingVariants := models.ExtractSupplyLinkRouteVariants(existing.Qualifiers, existing.TransportViaRefs, effectiveVesselType(seg, existing), existingConf)
			incomingVariants := models.ExtractSupplyLinkRouteVariants(slFrag.Qualifiers, slFrag.TransportViaRefs, effectiveVesselType(seg, slFrag), seg.Confidence)
			mergedVariants := models.MergeSupplyLinkRouteVariants(existingVariants, incomingVariants)
			mergedQualifiers := mergeSupplyLinkQualifiers(existing.Qualifiers, slFrag.Qualifiers)
			if len(mergedVariants) > 0 {
				if mergedQualifiers == nil {
					mergedQualifiers = map[string]interface{}{}
				}
				mergedQualifiers["routeVariants"] = mergedVariants
			}
			if !equalInterfaceMaps(existing.Qualifiers, mergedQualifiers) {
				existing.Qualifiers = mergedQualifiers
				changed = true
			}
			if primary := models.PrimarySupplyLinkRouteVariant(mergedVariants); primary != nil && !slicesEqual(existing.TransportViaRefs, primary.TransportViaRefs) {
				existing.TransportViaRefs = append([]string(nil), primary.TransportViaRefs...)
				changed = true
			}
			mergedSources := mergeUniqueStrings(existing.Provenance.SourceIDs, slFrag.Provenance.SourceIDs)
			if !slicesEqual(mergedSources, existing.Provenance.SourceIDs) {
				existing.Provenance.SourceIDs = mergedSources
				changed = true
			}
			if !changed {
				supplyLinksSkipped++
				continue
			}
			existing.UpdatedAt = time.Now().UTC()
			existing.Metadata.LastUpdatedAt = time.Now().UTC()
			if err := e.fragmentsRepo.UpdateFast(ctx, existing); err != nil {
				supplyLinksSkipped++
			} else {
				supplyLinksCreated++
				persistSupplyChainScopeArtifacts(ctx, e, job, existing, seg)
				materializedSegmentRefs[segmentLookupKey] = supplyLinkMaterializationRef{
					ID:            strings.TrimSpace(existing.ID),
					CanonicalPath: strings.TrimSpace(existing.CanonicalPath),
					TenantID:      strings.TrimSpace(existing.TenantID),
				}
				e.setSupplyLinkRefCached(ctx, segmentLookupKey, materializedSegmentRefs[segmentLookupKey])
			}
			continue
		}
		if createdID, createErr := e.fragmentsRepo.CreateAssumeAbsent(ctx, slFrag); createErr != nil {
			supplyLinksSkipped++
		} else {
			supplyLinksCreated++
			slFrag.ID = strings.TrimSpace(createdID)
			persistSupplyChainScopeArtifacts(ctx, e, job, slFrag, seg)
			materializedSegmentRefs[segmentLookupKey] = supplyLinkMaterializationRef{
				ID:            strings.TrimSpace(createdID),
				CanonicalPath: strings.TrimSpace(slFrag.CanonicalPath),
				TenantID:      strings.TrimSpace(slFrag.TenantID),
			}
			e.setSupplyLinkRefCached(ctx, segmentLookupKey, materializedSegmentRefs[segmentLookupKey])
		}
	}

	if supplyLinksCreated > 0 || supplyLinksSkipped > 0 {
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyLinkMaterialize] created=%d skipped=%d", supplyLinksCreated, supplyLinksSkipped))
	}

	// Directed exploration should favor fast first materialization for live graph/map updates.
	// Deferred rebuild is useful for background quality improvement, but it is too expensive for
	// interactive hot-path runs and can trigger unrelated AI fallbacks.
	if supplyLinksCreated > 0 && e.supplyChainPathBuilder != nil && !isDirectedSupplyChainExplorationJob(job) {
		if job.State == nil {
			job.State = make(map[string]interface{})
		}
		job.State["deferredSupplyChainRebuild"] = map[string]interface{}{
			"scheduled":          true,
			"newSupplyLinks":     float64(supplyLinksCreated),
			"normalizedSegments": float64(len(materializedSegments)),
		}
		e.launchDeferredSupplyChainRebuild(job, materializedSegments)
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainRebuildDeferred] scheduled from %d new SupplyLinks", supplyLinksCreated))
	}

	// Materialize supply chain paths as MaterializedArtifact for every chain discovery.
	// Uses structural canonical key (rootSupplier/leafProduct/leafCustomer) for idempotent dedup & merge.
	// Chain reuse: checks if a MaterializedArtifact already exists with sufficient quality to skip re-processing.
	// Batches create/update for efficiency (BatchGetByCanonicalKeys + BatchUpsert).
	materializedPathsCount := 0
	directPathArtifactKeys := make([]string, 0, len(chains))
	if e.supplyChainPathBuilder != nil && len(chains) > 0 {
		maxDepth := parseStateInt(job.State["maxRecursionDepth"], 2)
		if maxDepth <= 0 {
			maxDepth = 6
		}
		tenantID := "global"
		anchorPath := ""
		if sourceFragment != nil {
			anchorPath = strings.TrimSpace(sourceFragment.CanonicalPath)
		}
		criticPassed := false
		materializationStage := "step4"
		if job != nil && job.State != nil {
			if st := strings.TrimSpace(strings.ToLower(parseStateString(job.State["supplyChainMaterializationStage"]))); st != "" {
				materializationStage = st
			}
		}
		isFinalMaterializationStage := materializationStage == "step4"
		if e.criticAgent != nil {
			scope := ensureWorkingProductScopeLoaded(ctx, e, job, nil, "query_seed", anchorPath)
			input := agents.CriticValidatorInput{
				Fragment:            sourceFragment,
				SupplyChains:        chains,
				WorkingProductScope: nil,
			}
			if scope != nil {
				input.WorkingProductScope = dedupStrings(scope.CanonicalProductRefs())
			}
			if vr, err := e.criticAgent.CriticPipeline().Validate(ctx, input, []string{taxonomy.DomainModeSupplyChain}); err == nil && vr != nil && vr.IsValid && len(vr.MissingInfo) == 0 {
				criticPassed = true
			}
		}
		var pathsToMaterialize []*pathbuilder.SupplyChainPath
		var keysToMaterialize []string
		const chainReuseLogSampleLimit = 6
		skipSamples := make([]string, 0, chainReuseLogSampleLimit)
		mergeSamples := make([]string, 0, chainReuseLogSampleLimit)
		type pendingPathMaterialization struct {
			index        int
			canonicalKey string
			segments     []pathbuilder.PathSegment
			path         *pathbuilder.SupplyChainPath
		}
		pendingPaths := make([]pendingPathMaterialization, 0, len(parsedChains))
		canonicalKeysForReuse := make([]string, 0, len(parsedChains))
		chainContext, _ := ParseChainContext(job.State["chainContext"])
		chainGraphContext := ""
		if chainContext != nil {
			chainGraphContext = chainContext.ToPromptContext()
		}
		for idx, singleParsed := range parsedChains {
			if singleParsed == nil || len(singleParsed.SupplyLinkSegments) == 0 {
				continue
			}
			segments := buildSegmentsFromParsed(singleParsed, pathRewrites, materializedSegmentRefs)
			if len(segments) == 0 {
				continue
			}
			canonicalKey := pathbuilder.BuildStructuralCanonicalKey(tenantID, segments)
			if canonicalKey == "" {
				canonicalKey = buildSupplyChainArtifactCanonicalKey(job, anchorPath, idx, segments)
			}
			buildMode := "scout_parsed_auto"
			if isSupplyChainPlanMode(job.State) {
				buildMode = "scout_parsed_plan"
			}
			path := &pathbuilder.SupplyChainPath{
				Segments:   segments,
				Confidence: normalizeEdgeConfidence(chains[idx].Confidence),
				BuildMode:  buildMode,
				MaxDepth:   maxDepth,
			}
			if e.criticAgent != nil && !isDirectedSupplyChainExplorationJob(job) {
				joinHints, joinGaps, err := e.buildSupplyChainJoinValidationHints(ctx, chains[idx], segments, chainGraphContext)
				if err != nil {
					e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainJoinValidation] chain=%d err=%v", idx, err))
				} else {
					path.JoinAssessmentHints = joinHints
					path.OpenGaps = uniqueSortedStrings(joinGaps)
				}
			}
			pendingPaths = append(pendingPaths, pendingPathMaterialization{
				index:        idx,
				canonicalKey: canonicalKey,
				segments:     segments,
				path:         path,
			})
			if canonicalKey != "" {
				canonicalKeysForReuse = append(canonicalKeysForReuse, canonicalKey)
			}
		}

		existingChainsByKey := map[string]*models.MaterializedArtifact{}
		if len(canonicalKeysForReuse) > 0 {
			if batchExisting, err := e.supplyChainPathBuilder.BatchGetChainsByCanonicalKeys(ctx, tenantID, canonicalKeysForReuse); err == nil {
				existingChainsByKey = batchExisting
			}
		}
		for _, pending := range pendingPaths {
			reuseDecision := evaluateChainReuseFromExisting(pending.canonicalKey, existingChainsByKey[pending.canonicalKey])
			logChainReuseDecision(ctx, job, reuseDecision, pending.index)
			// Never full-skip on provisional staged materialization (step1/step2/step3/U1/U2):
			// we want to preserve downstream enrichment at final step4 and avoid losing business links.
			if !isFinalMaterializationStage && reuseDecision.Level == ChainReuseFullSkip {
				reuseDecision.Level = ChainReuseEnrichMerge
				if strings.TrimSpace(reuseDecision.Reason) == "" {
					reuseDecision.Reason = fmt.Sprintf("provisional_stage=%s", materializationStage)
				} else {
					reuseDecision.Reason = fmt.Sprintf("%s,provisional_stage=%s", reuseDecision.Reason, materializationStage)
				}
			}

			if reuseDecision.Level == ChainReuseFullSkip {
				chainReuseSkipped++
				if strings.TrimSpace(reuseDecision.CanonicalKey) != "" {
					directPathArtifactKeys = append(directPathArtifactKeys, strings.TrimSpace(reuseDecision.CanonicalKey))
				}
				if reuseDecision.Artifact != nil && strings.TrimSpace(lensID) != "" && e.lensesRepo != nil {
					if addArtifactToLensScope(ctx, e.lensesRepo, lensID, reuseDecision.Artifact) {
						chainReuseScopeAdded++
					}
				}
				if len(skipSamples) < chainReuseLogSampleLimit {
					skipSamples = append(skipSamples, fmt.Sprintf("chain=%d key=%s reason=%s", pending.index, reuseDecision.CanonicalKey, reuseDecision.Reason))
				}
				continue
			}

			if reuseDecision.Level == ChainReuseEnrichMerge {
				chainReuseMerged++
				if strings.TrimSpace(reuseDecision.CanonicalKey) != "" {
					directPathArtifactKeys = append(directPathArtifactKeys, strings.TrimSpace(reuseDecision.CanonicalKey))
				}
				if len(mergeSamples) < chainReuseLogSampleLimit {
					mergeSamples = append(mergeSamples, fmt.Sprintf("chain=%d key=%s reason=%s", pending.index, reuseDecision.CanonicalKey, reuseDecision.Reason))
				}
			}
			pathsToMaterialize = append(pathsToMaterialize, pending.path)
			keysToMaterialize = append(keysToMaterialize, pending.canonicalKey)
		}
		if chainReuseSkipped > 0 {
			e.logLine(ctx, job, "info", fmt.Sprintf("[ChainReuse] SKIP summary skipped=%d stage=%s samples=[%s]",
				chainReuseSkipped, materializationStage, strings.Join(skipSamples, " | ")))
		}
		if chainReuseMerged > 0 {
			e.logLine(ctx, job, "info", fmt.Sprintf("[ChainReuse] MERGE summary merged=%d stage=%s samples=[%s]",
				chainReuseMerged, materializationStage, strings.Join(mergeSamples, " | ")))
		}
		if len(pathsToMaterialize) > 0 && e.supplyChainPathBuilder != nil {
			familyCandidates := make([]supplyChainFamilyCandidate, 0, len(pathsToMaterialize))
			currentRound := parseStateInt(job.State["supplyChainFamilyCoResearchRound"], 0)
			for i, path := range pathsToMaterialize {
				if path == nil {
					continue
				}
				sufficiencyPreview, previewErr := e.supplyChainPathBuilder.PreviewSufficiency(ctx, path)
				if previewErr != nil {
					e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainFamily] preview failed key=%s err=%v", keysToMaterialize[i], previewErr))
					continue
				}
				familyCandidates = append(familyCandidates, supplyChainFamilyCandidate{
					canonicalKey: keysToMaterialize[i],
					path:         path,
					sufficiency:  sufficiencyPreview,
				})
			}
			familyBatches = buildSupplyChainFamilyBatches(tenantID, currentRound, familyCandidates)
		}
		if len(pathsToMaterialize) > 0 {
			ids, err := e.supplyChainPathBuilder.MaterializePathsBatch(ctx, pathsToMaterialize, tenantID, lensID, keysToMaterialize)
			if err != nil {
				e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainMaterialize] batch failed: %v", err))
			} else {
				materializedCount := len(ids)
				materializedPathsCount = materializedCount
				directPathArtifactKeys = append(directPathArtifactKeys, keysToMaterialize...)
				for i, id := range ids {
					canonicalKey := keysToMaterialize[i]
					segmentCount := len(pathsToMaterialize[i].Segments)
					segmentLabel := "segments"
					if segmentCount == 1 {
						segmentLabel = "segment"
					}
					e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainMaterialize] Materialized supply path %d with %d %s", i+1, segmentCount, segmentLabel))
					if criticPassed && canonicalKey != "" {
						if updErr := e.supplyChainPathBuilder.UpdatePathStatus(ctx, tenantID, canonicalKey, "validated"); updErr != nil {
							e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainMaterialize] PathStatus update failed key=%s err=%v", canonicalKey, updErr))
						}
					}
					if strings.TrimSpace(lensID) != "" && e.lensesRepo != nil && id != "" {
						art, artErr := e.supplyChainPathBuilder.GetChainByCanonicalKey(ctx, tenantID, canonicalKey)
						if artErr == nil && art != nil {
							if addArtifactToLensScope(ctx, e.lensesRepo, lensID, art) {
								chainReuseScopeAdded++
							}
						}
					}
				}
				if materializedCount > 0 || chainReuseSkipped > 0 || chainReuseMerged > 0 {
					e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainMaterialize] Completed: %d new paths, %d reused, %d merged, %d added to lens",
						materializedCount, chainReuseSkipped, chainReuseMerged, chainReuseScopeAdded))
				}
			}
		} else if chainReuseSkipped > 0 || chainReuseMerged > 0 {
			e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainMaterialize] Completed: 0 new paths, %d reused, %d merged, %d added to lens",
				chainReuseSkipped, chainReuseMerged, chainReuseScopeAdded))
		}
		if len(dedupStrings(directPathArtifactKeys)) > 0 {
			persistDirectPathArtifactTruth(ctx, e, job, tenantID, dedupStrings(directPathArtifactKeys))
		}
	}
	// Level 3 FAC→ORG resolution: traverse OWNS/CONTROLS to owning ORG, max depth 5, cycle-prohibited
	resolver := NewSupplyChainOrgResolver(e.edgesRepo, e.fragmentsRepo)
	resolveTenantID := strings.TrimSpace(job.TenantID)
	if resolveTenantID == "" {
		resolveTenantID = "global"
	}
	facToOrgResult := resolver.Resolve(ctx, resolveTenantID, pathToID)
	if len(facToOrgResult.FacToOrg) > 0 {
		if job.State == nil {
			job.State = make(map[string]interface{})
		}
		job.State["facToOrgMap"] = facToOrgResult.FacToOrg
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainOrgResolver] resolved=%d FAC→ORG mappings", len(facToOrgResult.FacToOrg)))
	}

	// Update chain context in job state for propagation to child jobs
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	existingContext, _ := ParseChainContext(job.State["chainContext"])
	contextResult := &ParsedChainResult{
		Fragments: result.Fragments,
		Edges:     acceptedContextEdges,
	}
	existingContext.MergeFromParsedResult(contextResult)

	// Identify gaps: nodes that have only incoming or only outgoing edges
	gapHints := e.identifyChainGaps(result, existingContext)
	for _, batch := range familyBatches {
		if batch == nil {
			continue
		}
		gapHints = mergeUniqueStrings(gapHints, batch.OpenGaps)
	}
	existingContext.GapHints = gapHints
	discoveredNodesCount := len(existingContext.DiscoveredNodes)
	discoveredEdgesCount := len(existingContext.DiscoveredEdges)
	normalizedSegmentsCount := len(materializedSegments)
	materializedFragmentsCount := createdFrags + updatedFrags
	dupeCreateRatio := 0.0
	if createdEdges > 0 {
		dupeCreateRatio = float64(edgeDupes) / float64(createdEdges)
	}
	reverseRate := 0.0
	if createdEdges > 0 {
		reverseRate = float64(edgeReversed) / float64(createdEdges)
	}
	job.State["supplyChainDiscovery"] = map[string]interface{}{
		"discoveredNodes":    float64(discoveredNodesCount),
		"discoveredEdges":    float64(discoveredEdgesCount),
		"normalizedSegments": float64(normalizedSegmentsCount),
	}
	job.State["supplyChainMaterialization"] = map[string]interface{}{
		"materializedFragments":   float64(materializedFragmentsCount),
		"materializedEdges":       float64(createdEdges),
		"materializedSupplyLinks": float64(supplyLinksCreated),
		"materializedPaths":       float64(materializedPathsCount),
		"pathArtifactKeys":        dedupStrings(directPathArtifactKeys),
	}
	job.State["supplyChainSummary"] = map[string]interface{}{
		"discoveredNodes":         float64(discoveredNodesCount),
		"discoveredEdges":         float64(discoveredEdgesCount),
		"normalizedSegments":      float64(normalizedSegmentsCount),
		"materializedFragments":   float64(materializedFragmentsCount),
		"materializedEdges":       float64(createdEdges),
		"materializedSupplyLinks": float64(supplyLinksCreated),
		"materializedPaths":       float64(materializedPathsCount),
		"createdFragments":        float64(createdFrags),
		"updatedFragments":        float64(updatedFrags),
		"fragmentErrors":          float64(createErrors),
		"createdEdges":            float64(createdEdges),
		"edgeDupes":               float64(edgeDupes),
		"edgeErrors":              float64(edgeCreateErrors),
		"edgeReversed":            float64(edgeReversed),
		"edgePenalized":           float64(edgePenalized),
		"supplyLinksCreated":      float64(supplyLinksCreated),
		"supplyLinksSkipped":      float64(supplyLinksSkipped),
		"pathArtifactKeys":        dedupStrings(directPathArtifactKeys),
		"gapHints":                float64(len(gapHints)),
		"dupeCreateRatio":         dupeCreateRatio,
		"reverseRate":             reverseRate,
		"kpiGuardrailDCR":         dupeCreateRatio > 0.2,
		"kpiGuardrailRR":          reverseRate > 0.25,
		"chainReuseSkipped":       float64(chainReuseSkipped),
		"chainReuseMerged":        float64(chainReuseMerged),
		"chainReuseScopeAdded":    float64(chainReuseScopeAdded),
	}

	if contextJSON, err := existingContext.ToJSON(); err == nil {
		if job.State == nil {
			job.State = make(map[string]interface{})
		}
		job.State["chainContext"] = contextJSON
		if e.scoutingJobsRepo != nil {
			if err := e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
				"chainContext":               contextJSON,
				"supplyChainDiscovery":       job.State["supplyChainDiscovery"],
				"supplyChainMaterialization": job.State["supplyChainMaterialization"],
				"supplyChainSummary":         job.State["supplyChainSummary"],
			}); err != nil {
				logGroundingErrorWithContext(job, "scout", "supply_chain.persist_chain_context", "job_state_update_failed", "repo_update_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "supply_chain.persist_chain_context", "job_state_update_failed", "repo_update_error", true)
			} else {
				fmt.Printf("[SupplyChainPersisted] job=%s target=%s discovered(nodes=%d edges=%d) materialized(supplyLinks=%d paths=%d)\n",
					strings.TrimSpace(job.ID),
					strings.TrimSpace(job.TargetCanonicalPath),
					discoveredNodesCount,
					discoveredEdgesCount,
					supplyLinksCreated,
					materializedPathsCount,
				)
			}
		}
	}
	if len(familyBatches) > 0 && !parseStateBool(job.State["supplyChainSkipFamilyCoResearch"], false) {
		e.runSupplyChainFamilyCoResearch(ctx, job, sourceFragment, familyBatches)
	}

	// Cascade child scheduling: only if policy allows. Entity jobs with domain expansion
	// peer only materialize chains, they don't schedule children (domain expansion owns cascade).
	scPolicy := NewPostProcessingPolicy(job)
	if scPolicy.ShouldRun(StepCascadeChildScheduling) && !parseStateBool(job.State["supplyChainSkipChildScheduling"], false) {
		_ = e.scheduleSupplyChainChildJobs(ctx, job, sourceFragment, result, existingContext)
	}

	trace.Log(ctx, "scout_supply_chain", "scout.supply_chain.processed", map[string]any{
		"tenantId":       strings.TrimSpace(job.TenantID),
		"projectId":      strings.TrimSpace(job.ProjectID),
		"lensId":         lensID,
		"jobId":          strings.TrimSpace(job.ID),
		"chainsCount":    len(chains),
		"fragmentsCount": len(result.Fragments),
		"edgesCount":     len(result.Edges),
		"gapHintsCount":  len(gapHints),
		"supplyChainLogSummary": map[string]any{
			"discoveredNodes":         discoveredNodesCount,
			"discoveredEdges":         discoveredEdgesCount,
			"normalizedSegments":      normalizedSegmentsCount,
			"materializedFragments":   materializedFragmentsCount,
			"materializedEdges":       createdEdges,
			"materializedSupplyLinks": supplyLinksCreated,
			"materializedPaths":       materializedPathsCount,
			"createdFragments":        createdFrags,
			"updatedFragments":        updatedFrags,
			"fragmentErrors":          createErrors,
			"createdEdges":            createdEdges,
			"edgeDupes":               edgeDupes,
			"edgeErrors":              edgeCreateErrors,
			"edgeReversed":            edgeReversed,
			"edgePenalized":           edgePenalized,
			"supplyLinksCreated":      supplyLinksCreated,
			"supplyLinksSkipped":      supplyLinksSkipped,
			"chainReuseSkipped":       chainReuseSkipped,
			"chainReuseMerged":        chainReuseMerged,
			"chainReuseScopeAdded":    chainReuseScopeAdded,
		},
	})
	{
		msg := fmt.Sprintf("[SupplyChainSummary] job=%s target=%s discovered(nodes=%d edges=%d normalizedSegments=%d) materialized(fragments=%d edges=%d supplyLinks=%d paths=%d) fragments(created=%d updated=%d err=%d) edges(dupes=%d err=%d reversed=%d penalized=%d) supplyLinks(skipped=%d) gaps=%d chainReuse(skipped=%d merged=%d scopeAdded=%d)",
			strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), discoveredNodesCount, discoveredEdgesCount, normalizedSegmentsCount, materializedFragmentsCount, createdEdges, supplyLinksCreated, materializedPathsCount, createdFrags, updatedFrags, createErrors, edgeDupes, edgeCreateErrors, edgeReversed, edgePenalized, supplyLinksSkipped, len(gapHints), chainReuseSkipped, chainReuseMerged, chainReuseScopeAdded)
		fmt.Printf("%s\n", msg)
		desc := supplyChainSummaryDescription(discoveredNodesCount, materializedFragmentsCount, createdEdges, supplyLinksCreated, materializedPathsCount, len(gapHints))

		e.publish(ctx, realtime.EventType("horizon.scout.job.log"), map[string]interface{}{
			"tenantId":            job.TenantID,
			"projectId":           job.ProjectID,
			"lensId":              strings.TrimSpace(job.LensID),
			"jobId":               job.ID,
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"level":               "info",
			"line":                msg,
			"humanReadable": map[string]interface{}{
				"title":       "Supply chain saved",
				"description": desc,
			},
			"supplyChainDiscovery":       job.State["supplyChainDiscovery"],
			"supplyChainMaterialization": job.State["supplyChainMaterialization"],
		})
	}

	// Best-effort: rewrite lens scope entries if any canonical paths were normalized/deduplicated
	// during supply-chain persistence.
	if strings.TrimSpace(lensID) != "" && job != nil && len(pathRewrites) > 0 {
		tenantID := strings.TrimSpace(job.TenantID)
		if tenantID != "" {
			seen := map[string]struct{}{}
			for from, to := range pathRewrites {
				from = strings.TrimSpace(from)
				to = strings.TrimSpace(to)
				if from == "" || to == "" || from == to {
					continue
				}
				k := from + "->" + to
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, from, to)
			}
		}
	}

	return nil
}

// scheduleSupplyChainChildJobs enqueues child scouting jobs for discovered supply chain nodes
// (and especially gap nodes). This is part of the core scouting pipeline.
func (e *ScoutEngine) scheduleSupplyChainChildJobs(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	result *ParsedChainResult,
	context *ChainContext,
) error {
	if e == nil || job == nil || job.State == nil || e.scoutingJobsRepo == nil || e.fragmentsRepo == nil {
		return nil
	}

	// Depth guardrails
	currentDepth := parseStateInt(job.State["recursionDepth"], 0)
	maxDepth := parseStateInt(job.State["maxRecursionDepth"], 2)
	hasExecutionPlan := HasExecutionPlanContractState(job.State)
	executionPlan := ExecutionPlanContract{}
	if hasExecutionPlan {
		executionPlan = ExecutionPlanContractFromState(job.State)
	}
	if hasExecutionPlan && executionPlan.MaxRecursionDepth >= 0 && executionPlan.MaxRecursionDepth < maxDepth {
		maxDepth = executionPlan.MaxRecursionDepth
	}
	if maxDepth < 0 {
		maxDepth = 0
	}
	if currentDepth >= maxDepth {
		return nil
	}

	// Breadth guardrails (supply_chain is allowed to be wider by default).
	maxChildren := 80
	if v, ok := job.State["maxChildrenPerJob"]; ok {
		maxChildren = parseStateInt(v, maxChildren)
	}
	if hasExecutionPlan && executionPlan.MaxChildrenPerJob > 0 && executionPlan.MaxChildrenPerJob < maxChildren {
		maxChildren = executionPlan.MaxChildrenPerJob
	}
	if maxChildren < 0 {
		maxChildren = 0
	}
	if maxChildren == 0 {
		return nil
	}
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" {
		lensID = parseStateString(job.State["lensId"])
	}
	cascadeID := ""
	if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
		cascadeID = strings.TrimSpace(v)
	}
	if cascadeID == "" {
		cascadeID = strings.TrimSpace(job.ID)
	}
	cascadeLimitActive := hasExecutionPlan && executionPlan.MaxCascadeJobs > 0
	cascadeRemaining := 0
	cascadeUsed := 0
	if cascadeLimitActive {
		cascadeRemaining, cascadeUsed = e.estimateCascadeRemainingCapacity(ctx, job, lensID, cascadeID, executionPlan.MaxCascadeJobs)
		if cascadeRemaining <= 0 {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> supply-chain cascade cap reached (%s): used=%d limit=%d",
				RuntimeContractExecutionPlan, cascadeUsed, executionPlan.MaxCascadeJobs))
			return nil
		}
	}
	if scale, snap, ok := e.computeRuntimeBudgetAdaptation(ctx, job.TenantID, lensID, cascadeID); ok && scale < 1.0 {
		before := maxChildren
		maxChildren = scaleCapByBudget(maxChildren, 1, scale)
		if maxChildren < before {
			e.logLine(ctx, job, "info", fmt.Sprintf("> supply-chain fanout budget adapted (%s): maxChildren %d -> %d (%s)",
				RuntimeContractBudgetAdaptiveFanout, before, maxChildren, snap.summary()))
		}
	}

	// Best-effort: prefetch active (pending/running) scout jobs for this project/lens
	activeJobByPath := map[string]string{}     // canonicalPath -> jobId
	activeJobByIdentity := map[string]string{} // targetIdentityKey -> jobId
	{
		if jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, 300); err == nil {
			for _, j := range jobs {
				if j == nil {
					continue
				}
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				if p == "" {
					continue
				}
				if _, ok := activeJobByPath[p]; !ok {
					activeJobByPath[p] = j.ID
				}
				if j.State != nil {
					if v, ok := j.State["targetIdentityKey"].(string); ok {
						idk := strings.TrimSpace(v)
						if idk != "" {
							if _, exists := activeJobByIdentity[idk]; !exists {
								activeJobByIdentity[idk] = j.ID
							}
						}
					}
				}
			}
		}
	}

	// Build candidate list:
	// 1) newly seen nodes (this run)
	// 2) gap nodes (missing upstream/downstream)
	candidates := make([]string, 0, 64)
	seen := map[string]struct{}{}
	add := func(cp string) {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			return
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == cp {
			return
		}
		if _, ok := seen[cp]; ok {
			return
		}
		seen[cp] = struct{}{}
		candidates = append(candidates, cp)
	}

	if result != nil {
		for cp := range result.Fragments {
			add(cp)
		}
	}

	// Gap nodes derived from context edges
	if context != nil {
		incoming := map[string]int{}
		outgoing := map[string]int{}
		for _, e2 := range context.DiscoveredEdges {
			outgoing[strings.TrimSpace(e2.Source)]++
			incoming[strings.TrimSpace(e2.Target)]++
		}
		for _, cp := range context.DiscoveredNodes {
			cp = strings.TrimSpace(cp)
			if cp == "" {
				continue
			}
			isProduct := strings.HasPrefix(cp, "/product/")
			isActor := strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/fac/")
			// prioritize obvious open ends
			if isProduct && (incoming[cp] == 0 || outgoing[cp] == 0) {
				add(cp)
			}
			if isActor && (incoming[cp] == 0 || outgoing[cp] == 0) {
				add(cp)
			}
		}
	}

	if len(candidates) == 0 {
		msg := "[SupplyChainSchedule] no candidates"
		e.logLine(ctx, job, "info", msg)
		return nil
	}

	scheduled := 0
	scheduledChildren := map[string]struct{}{}
	scheduledIdentityChildren := map[string]struct{}{}
	skippedActive := 0
	skippedMissingFrag := 0
	skippedVerified := 0
	skippedDup := 0
	skippedVisited := 0
	skippedPlaceholder := 0
	skippedSameQualifier := 0
	skippedOutOfScope := 0
	sourceFacilityQualifier := resolveFacilityQualifierSemantic(sourceFragment, "")

	for _, targetPath := range candidates {
		if cascadeLimitActive && cascadeRemaining <= 0 {
			break
		}
		if scheduled >= maxChildren {
			break
		}
		if shouldSkipRecursivePlaceholderTarget(targetPath) {
			skippedPlaceholder++
			continue
		}
		if _, ok := scheduledChildren[targetPath]; ok {
			skippedDup++
			continue
		}
		if existingJobID, ok := activeJobByPath[targetPath]; ok && strings.TrimSpace(existingJobID) != "" {
			skippedActive++
			continue
		}

		frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		if err != nil || frag == nil {
			// If the fragment isn't there for some reason, skip; supplyChains materialization should have created it.
			skippedMissingFrag++
			continue
		}
		if shouldSkipSameFacilityQualifierRecursion(sourceFragment, targetPath, frag.Type, frag) {
			skippedSameQualifier++
			continue
		}
		if !isFacilityQualifierBusinessTargetType(sourceFacilityQualifier, targetPath, frag.Type) {
			skippedOutOfScope++
			continue
		}
		targetIdentityKey := buildFragmentIdentityKey(frag)
		if targetIdentityKey != "" {
			if _, ok := scheduledIdentityChildren[targetIdentityKey]; ok {
				skippedDup++
				continue
			}
			if existingJobID, ok := activeJobByIdentity[targetIdentityKey]; ok && strings.TrimSpace(existingJobID) != "" {
				skippedActive++
				continue
			}
		}

		// Skip if already verified (consistent with relation-driven recursion)
		shouldScout := frag.Metadata.LastVerifiedAt.IsZero() || frag.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence
		if !shouldScout {
			skippedVerified++
			continue
		}

		// Cascade visited set: enqueue-once-per-canonicalPath inside this cascade.
		// Best-effort delete on enqueue failure to avoid harming quality.
		if e.cascadeVisitedRepo != nil {
			already, vErr := e.cascadeVisitedRepo.TryMarkVisited(ctx, job.TenantID, job.ProjectID, cascadeID, targetPath, job.ID, currentDepth+1)
			if vErr != nil {
				msg := fmt.Sprintf("[SupplyChainVisited] ERROR mark tenant=%s cascade=%s target=%s err=%v",
					strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), vErr)
				fmt.Printf("%s\n", msg)
				e.logLine(ctx, job, "warn", msg)
			} else if already {
				skippedVisited++
				msg := fmt.Sprintf("[SupplyChainVisited] SKIP alreadyVisited tenant=%s cascade=%s target=%s depth=%d",
					strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), currentDepth+1)
				e.logLine(ctx, job, "info", msg)
				continue
			}
		}

		scheduledChildren[targetPath] = struct{}{}
		if targetIdentityKey != "" {
			scheduledIdentityChildren[targetIdentityKey] = struct{}{}
		}
		scheduled++

		agentID := job.AgentID
		if agentID == "" {
			agentID = "scout"
		}
		now := time.Now().UTC()
		childState := map[string]interface{}{
			"targetCanonicalPath": targetPath,
			"currentState":        string(models.ScoutingJobStatePending),
			"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
			"recursionDepth":      float64(currentDepth + 1),
			"maxRecursionDepth":   float64(maxDepth),
			"parentJobId":         job.ID,
		}
		if targetIdentityKey != "" {
			childState["targetIdentityKey"] = targetIdentityKey
		}
		// Propagate lens id
		if strings.TrimSpace(job.LensID) != "" {
			childState["lensId"] = strings.TrimSpace(job.LensID)
		} else if v := parseStateString(job.State["lensId"]); v != "" {
			childState["lensId"] = v
		}
		// Propagate breadth cap
		childState["maxChildrenPerJob"] = float64(maxChildren)
		// Propagate cascade root id for budget attribution
		if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
			childState["cascadeRootJobId"] = strings.TrimSpace(v)
		} else {
			childState["cascadeRootJobId"] = job.ID
		}
		// Pass through steering signals
		if rm, ok := job.State["requestedMetrics"]; ok {
			childState["requestedMetrics"] = rm
		}
		if v, ok := job.State["specialties"]; ok {
			childState["specialties"] = v
		}
		if v, ok := job.State["chainContext"]; ok {
			childState["chainContext"] = v
		}
		if v, ok := job.State["focus"]; ok {
			childState["focus"] = v
		}
		if v, ok := job.State["keywords"]; ok {
			childState["keywords"] = v
		}
		if v, ok := job.State["spherePaths"]; ok {
			childState["spherePaths"] = v
		}
		if v, ok := job.State["originalQuery"]; ok {
			childState["originalQuery"] = v
		}
		if v, ok := job.State["decompositionHints"]; ok {
			childState["decompositionHints"] = v
		}
		if v, ok := job.State["relationChildSchedulingOptIn"]; ok && parseStateBool(v, false) {
			childState["relationChildSchedulingOptIn"] = true
		}
		copyDomainModeStateToChild(job.State, childState)

		scoutingJob := &models.ScoutingJob{
			AgentJob: agentmodels.AgentJob{
				TenantID:  job.TenantID,
				ProjectID: job.ProjectID,
				Status:    agentmodels.AgentJobStatusPending,
				AgentID:   agentID,
				CreatedAt: now,
				UpdatedAt: now,
				State:     childState,
			},
			LensID:              strings.TrimSpace(job.LensID),
			TargetCanonicalPath: targetPath,
			ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
			BillingContext: models.BillingContext{
				ChargeModel: string(models.ChargeModelBounty),
				ProjectID:   job.ProjectID,
			},
			CurrentState: models.ScoutingJobStatePending,
		}
		if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
			// Undo visited marker on enqueue failure.
			if e.cascadeVisitedRepo != nil {
				_ = e.cascadeVisitedRepo.DeleteVisited(ctx, job.TenantID, cascadeID, targetPath)
			}
		} else if cascadeLimitActive && cascadeRemaining > 0 {
			cascadeRemaining--
		}
	}

	if scheduled > 0 {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=%d candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d placeholder=%d sameQualifier=%d outOfScope=%d) depth=%d/%d cap=%d",
			scheduled, len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, skippedPlaceholder, skippedSameQualifier, skippedOutOfScope, currentDepth, maxDepth, maxChildren)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
	} else {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=0 candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d placeholder=%d sameQualifier=%d outOfScope=%d) depth=%d/%d cap=%d",
			len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, skippedPlaceholder, skippedSameQualifier, skippedOutOfScope, currentDepth, maxDepth, maxChildren)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
	}
	return nil
}

// identifyChainGaps finds gaps in the supply chain that should be filled.
func (e *ScoutEngine) identifyChainGaps(_ *ParsedChainResult, context *ChainContext) []string {
	hints := make([]string, 0)

	// Build edge maps to identify nodes with missing upstream/downstream
	incomingCount := make(map[string]int)
	outgoingCount := make(map[string]int)

	for _, edge := range context.DiscoveredEdges {
		outgoingCount[edge.Source]++
		incomingCount[edge.Target]++
	}

	// Find nodes that might need more connections
	for _, node := range context.DiscoveredNodes {
		isProduct := strings.HasPrefix(node, "/product/")
		isActor := strings.HasPrefix(node, "/org/") || strings.HasPrefix(node, "/fac/")

		if isProduct {
			// Products: producer (fac) and org-recipients
			if incomingCount[node] == 0 {
				hints = append(hints, fmt.Sprintf("Who produces %s? Find facility that produces it. Format: JSON.", node))
			}
			if outgoingCount[node] == 0 {
				hints = append(hints, fmt.Sprintf("Who receives %s? Find org-recipients. Format: JSON.", node))
			}
		} else if isActor {
			// Actors at chain ends: producing facs or org-recipients
			if incomingCount[node] == 0 && outgoingCount[node] > 0 {
				hints = append(hints, fmt.Sprintf("Who supplies to %s? Find producing facilities. Format: JSON.", node))
			}
			if outgoingCount[node] == 0 && incomingCount[node] > 0 {
				hints = append(hints, fmt.Sprintf("Who does %s supply to? Find org-recipients. Format: JSON.", node))
			}
		}
	}

	// Limit hints to avoid prompt bloat
	if len(hints) > 5 {
		hints = hints[:5]
	}

	return hints
}

// shouldRunStrategicOverlay determines if Phase 2 (Strategic Layer Overlay) should run
// after Phase 1 (Domain Expansion) completes.
func (e *ScoutEngine) shouldRunStrategicOverlay(contractType string, entityCount, relationCount int, entityPaths []string) bool {
	if entityCount == 0 && relationCount == 0 {
		return false
	}

	// Gov entities are inherently decision-makers: always run strategic overlay
	// when the domain scope includes governance entities, regardless of contract type.
	for _, p := range entityPaths {
		if strings.HasPrefix(strings.TrimSpace(p), "/gov/") {
			return true
		}
	}

	// For non-gov scopes, strategic overlay is opt-in by contract family.
	strategicContractTypes := map[string]bool{
		// Risk / geopolitics
		"risk-assessment":     true,
		"geopolitical-risk":   true,
		"bilateral-relations": true,
		"sanctions-exposure":  true,
		"conflict-dynamics":   true,
		// Regulatory
		"regulatory-landscape":    true,
		"compliance-map":          true,
		"policy-and-regulation":   true,
		"policy-impact":           true,
		"regulatory-impact":       true,
		"compliance-landscape":    true,
		"security-and-compliance": true,
		"licensing-requirements":  true,
		"esg-landscape":           true,
		// Ecosystem mapping (general)
		"ecosystem-mapping":     true,
		"stakeholder-mapping":   true,
		"partnership-landscape": true,
		// Power dynamics (explicit)
		"power-dynamics":    true,
		"influence-network": true,
		"influence-map":     true,
		"stakeholder-map":   true,
		"decision-mapping":  true,
	}

	ct := strings.ToLower(strings.TrimSpace(contractType))
	return strategicContractTypes[ct]
}

// collectAllEntityPaths merges explicit first-order entity paths with target paths
// extracted from synthesized relations. This gives shouldRunStrategicOverlay full
// visibility into entities discovered during domain research (not just the initial set).
func collectAllEntityPaths(firstOrder []string, rels []agents.DiscoveredRelation) []string {
	seen := make(map[string]struct{}, len(firstOrder)+len(rels))
	out := make([]string, 0, len(firstOrder)+len(rels))
	for _, p := range firstOrder {
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
	for _, r := range rels {
		p := strings.TrimSpace(r.TargetCanonicalPath)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// triggerGovStrategicOverlayFromEntityJob checks if an entity-level scouting job
// discovered gov entities during Research and triggers the strategic overlay if
// no overlay job already exists for this lens.
func (e *ScoutEngine) triggerGovStrategicOverlayFromEntityJob(
	ctx context.Context,
	job *models.ScoutingJob,
	relations []agents.DiscoveredRelation,
) {
	if e == nil || e.scoutingJobsRepo == nil || job == nil || len(relations) == 0 {
		return
	}
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" {
		return
	}

	hasGov := false
	for _, r := range relations {
		if strings.HasPrefix(strings.TrimSpace(r.TargetCanonicalPath), "/gov/") {
			hasGov = true
			break
		}
	}
	if !hasGov {
		return
	}

	// Check if a strategic overlay job already exists for this lens.
	existingJobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, 200)
	if err != nil {
		fmt.Printf("[ScoutEngine] Failed to list jobs for gov overlay check: %v\n", err)
		return
	}
	for _, ej := range existingJobs {
		if ej.State != nil {
			if pp, ok := ej.State["pipelinePass"].(string); ok && pp == "strategic_overlay" {
				return // already scheduled/running
			}
		}
	}

	// Derive domain path: use cascadeRootJobId to find the domain expansion job,
	// or fall back to constructing from the lens scope.
	domainCP := ""
	if job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
			rootJobID := strings.TrimSpace(v)
			for _, ej := range existingJobs {
				if strings.TrimSpace(ej.ID) == rootJobID && strings.HasPrefix(strings.TrimSpace(ej.TargetCanonicalPath), "/domain/") {
					domainCP = strings.TrimSpace(ej.TargetCanonicalPath)
					break
				}
			}
		}
	}
	if domainCP == "" {
		// Fallback: find any domain job in this lens.
		for _, ej := range existingJobs {
			if strings.HasPrefix(strings.TrimSpace(ej.TargetCanonicalPath), "/domain/") {
				domainCP = strings.TrimSpace(ej.TargetCanonicalPath)
				break
			}
		}
	}
	if domainCP == "" {
		fmt.Printf("[ScoutEngine] Gov entities found but no domain path for strategic overlay (lens=%s)\n", lensID)
		return
	}

	fmt.Printf("[ScoutEngine] Gov entity discovered during entity scouting — triggering strategic overlay (lens=%s, domain=%s)\n", lensID, domainCP)
	e.logLine(ctx, job, "info", fmt.Sprintf("> gov entity discovered during research — triggering strategic overlay for %s", domainCP))
	e.triggerPhase2StrategicOverlay(ctx, job, domainCP)
}

// triggerPhase2StrategicOverlay creates a new ScoutingJob for the strategic projection pass
// (decision/mechanism layer overlay).
//
// Backward-compatibility note:
// - The name still mentions "phase2" because older deployments and UI code may rely on it.
// - The actual routing is now done via job.State["pipelinePass"].
func (e *ScoutEngine) triggerPhase2StrategicOverlay(
	ctx context.Context,
	phase1Job *models.ScoutingJob,
	domainCP string,
) {
	if e == nil || e.scoutingJobsRepo == nil || phase1Job == nil {
		return
	}

	lensID := strings.TrimSpace(phase1Job.LensID)
	if lensID == "" {
		return
	}

	// Extract correlation/session IDs from Phase 1 job
	correlationID := ""
	sessionID := ""
	if phase1Job.State != nil {
		if v, ok := phase1Job.State["correlationId"].(string); ok {
			correlationID = strings.TrimSpace(v)
		}
		if v, ok := phase1Job.State["sessionId"].(string); ok {
			sessionID = strings.TrimSpace(v)
		}
	}

	// Build strategic projection job state
	now := time.Now().UTC()
	state := map[string]interface{}{
		"targetCanonicalPath": domainCP,
		"lensId":              lensID,
		"currentState":        string(models.ScoutingJobStatePending),
		"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
		"recursionDepth":      0.0,
		"maxRecursionDepth":   0.0,
		// New routing key (preferred):
		pipeline.StateKeyPipelinePass: string(pipeline.PassStrategicProjection),
		// Legacy routing flags (kept temporarily):
		"domainOverlayMode": true,
		"phase2Overlay":     true,
		"phase1JobId":       phase1Job.ID,
		"trigger":           "auto.strategic_projection.after_base_enrichment",
		"triggerLegacy":     "auto.phase2.after_domain_expansion",
	}
	if correlationID != "" {
		state["correlationId"] = correlationID
	}
	if sessionID != "" {
		state["sessionId"] = sessionID
	}

	// Copy focus and keywords from Phase 1 if available
	if phase1Job.State != nil {
		if v, ok := phase1Job.State["focus"].(string); ok && strings.TrimSpace(v) != "" {
			state["focus"] = strings.TrimSpace(v)
		}
		if v, ok := phase1Job.State["keywords"]; ok {
			state["keywords"] = v
		}
		if v, ok := phase1Job.State["originalQuery"].(string); ok && strings.TrimSpace(v) != "" {
			state["originalQuery"] = strings.TrimSpace(v)
			state["query"] = strings.TrimSpace(v)
		}
		copyDomainModeStateToChild(phase1Job.State, state)
	}

	phase2Job := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  phase1Job.TenantID,
			ProjectID: phase1Job.ProjectID,
			Status:    agentmodels.AgentJobStatusPending,
			AgentID:   "scout",
			CreatedAt: now,
			UpdatedAt: now,
			State:     state,
		},
		LensID:              lensID,
		TargetCanonicalPath: domainCP,
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelBounty),
			ProjectID:   phase1Job.ProjectID,
		},
		CurrentState: models.ScoutingJobStatePending,
	}

	// Create job (Cloud Tasks dispatcher will pick it up)
	jobID, err := e.scoutingJobsRepo.Create(ctx, phase2Job)
	if err != nil {
		logGroundingErrorWithContext(phase1Job, "scout", "strategic_projection.enqueue", "strategic_projection_job_create_failed", "repo_write_error", true, err)
		e.emitGroundingErrorTelemetry(ctx, phase1Job, "scout", "strategic_projection.enqueue", "strategic_projection_job_create_failed", "repo_write_error", true)
		return
	}

	fmt.Printf("[ScoutEngine] Strategic projection triggered: jobId=%s domain=%s baseJobId=%s\n",
		jobID, domainCP, phase1Job.ID)

	// Publish realtime event
	e.publish(ctx, realtime.EventType("horizon.domain.overlay.scheduled"), map[string]interface{}{
		"tenantId":      phase1Job.TenantID,
		"projectId":     phase1Job.ProjectID,
		"lensId":        lensID,
		"jobId":         jobID,
		"phase1JobId":   phase1Job.ID,
		"canonicalPath": domainCP,
		"humanReadable": map[string]interface{}{
			"title":       "Strategic overlay scheduled",
			"description": fmt.Sprintf("Strategic projection scheduled for %s", domainCP),
		},
	})
	// New event namespace (preferred): strategic projection scheduling.
	e.publish(ctx, realtime.EventType("horizon.domain.strategic_projection.scheduled"), map[string]interface{}{
		"tenantId":      phase1Job.TenantID,
		"projectId":     phase1Job.ProjectID,
		"lensId":        lensID,
		"jobId":         jobID,
		"baseJobId":     phase1Job.ID,
		"canonicalPath": domainCP,
		"humanReadable": map[string]interface{}{
			"title":       "Strategic projection scheduled",
			"description": fmt.Sprintf("Strategic projection scheduled for %s", domainCP),
		},
	})
}

// ---------------------------------------------------------------------------
// Supply Chain Recursive Unwinding
// ---------------------------------------------------------------------------

// collectAnchorOrgsFromQueriesByDirection splits anchors by direction for upstream vs downstream unwinding.
// Returns (downOrgs, downFacs, upOrgs, upFacs). For direction "both", anchors go to both.
func collectAnchorOrgsFromQueriesByDirection(queries []agents.SupplyChainQuery, defaultDirection string) (downOrgs, downFacs, upOrgs, upFacs []string) {
	downSeen := map[string]struct{}{}
	upSeen := map[string]struct{}{}
	addDown := func(p string) {
		if p == "" {
			return
		}
		if _, ok := downSeen[p]; ok {
			return
		}
		downSeen[p] = struct{}{}
		if strings.HasPrefix(p, "/org/") || strings.HasPrefix(p, "/gov/") {
			downOrgs = append(downOrgs, p)
		} else if strings.HasPrefix(p, "/fac/") || strings.HasPrefix(p, "/deposit/") {
			downFacs = append(downFacs, p)
		}
	}
	addUp := func(p string) {
		if p == "" {
			return
		}
		if _, ok := upSeen[p]; ok {
			return
		}
		upSeen[p] = struct{}{}
		if strings.HasPrefix(p, "/org/") || strings.HasPrefix(p, "/gov/") {
			upOrgs = append(upOrgs, p)
		} else if strings.HasPrefix(p, "/fac/") || strings.HasPrefix(p, "/deposit/") {
			upFacs = append(upFacs, p)
		}
	}
	for _, q := range queries {
		paths := q.Anchors
		if len(paths) == 0 && strings.TrimSpace(q.Anchor) != "" {
			paths = []string{q.Anchor}
		}
		dir := strings.ToLower(strings.TrimSpace(q.Direction))
		if dir == "" {
			dir = strings.ToLower(strings.TrimSpace(defaultDirection))
			if dir == "" {
				dir = "down"
			}
		}
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			switch dir {
			case "up", "upstream":
				addUp(p)
			case "both":
				addDown(p)
				addUp(p)
			default: // down, downstream, ""
				addDown(p)
			}
		}
	}
	return
}

type supplyChainFilterScope struct {
	Product []string
	Geo     []string
}

func collectSupplyChainFiltersByDirection(queries []agents.SupplyChainQuery, defaultDirection string) (down, up supplyChainFilterScope) {
	addFilters := func(scope *supplyChainFilterScope, q agents.SupplyChainQuery) {
		if scope == nil {
			return
		}
		scope.Product = dedupStrings(append(scope.Product, q.ProductFilter...))
		scope.Geo = dedupStrings(append(scope.Geo, q.GeoFilter...))
	}
	for _, q := range queries {
		dir := strings.ToLower(strings.TrimSpace(q.Direction))
		if dir == "" {
			dir = strings.ToLower(strings.TrimSpace(defaultDirection))
			if dir == "" {
				dir = "down"
			}
		}
		switch dir {
		case "up", "upstream":
			addFilters(&up, q)
		case "both":
			addFilters(&down, q)
			addFilters(&up, q)
		default:
			addFilters(&down, q)
		}
	}
	return down, up
}

func (e *ScoutEngine) buildSupplyChainStepPromptContext(ctx context.Context, job *models.ScoutingJob, depth int, direction string, productFilters []string, geoFilters []string) agents.SupplyChainStepPromptContext {
	domainPath := ""
	if job != nil {
		domainPath = strings.TrimSpace(job.TargetCanonicalPath)
	}
	scope := ensureWorkingProductScopeLoaded(ctx, e, job, productFilters, "query_seed", domainPath)
	out := agents.SupplyChainStepPromptContext{
		Depth:               depth,
		Direction:           strings.TrimSpace(direction),
		ProductFilters:      dedupStrings(append([]string(nil), productFilters...)),
		GeoFilters:          dedupStrings(append([]string(nil), geoFilters...)),
		WorkingProductScope: nil,
	}
	if scope != nil {
		out.WorkingProductScope = dedupStrings(scope.CanonicalProductRefs())
	}
	if job == nil {
		return out
	}
	if job.State != nil {
		if q := strings.TrimSpace(parseStateString(job.State["originalQuery"])); q != "" {
			out.OriginalQuery = q
		} else if q := strings.TrimSpace(parseStateString(job.State["query"])); q != "" {
			out.OriginalQuery = q
		}
		if dm, ok := job.State["domainExpansion"]; ok && dm != nil {
			if m, ok := dm.(map[string]interface{}); ok {
				if cp := strings.TrimSpace(parseStateString(m["canonicalPath"])); cp != "" {
					out.DomainPath = cp
				}
			}
		}
	}
	if out.DomainPath == "" {
		out.DomainPath = strings.TrimSpace(job.TargetCanonicalPath)
	}
	if strings.HasPrefix(out.DomainPath, "/domain/") {
		_, facets := parseDomainPathFacets(out.DomainPath)
		if facets != nil {
			if g, ok := facets["geo"]; ok && len(g) > 0 {
				out.Geo = strings.TrimSpace(g[0])
			}
			for _, key := range []string{"product", "material", "commodity", "goods", "item"} {
				if p, ok := facets[key]; ok && len(p) > 0 {
					out.Product = strings.TrimSpace(p[0])
					break
				}
			}
			for _, key := range []string{"sector", "industry", "market"} {
				if s, ok := facets[key]; ok && len(s) > 0 {
					out.Sector = strings.TrimSpace(s[0])
					break
				}
			}
		}
	}
	return out
}

func filterSupplyChainsByPlanFilters(chains []agents.SupplyChain, productFilters []string, geoFilters []string) []agents.SupplyChain {
	if len(chains) == 0 {
		return nil
	}
	if len(productFilters) == 0 && len(geoFilters) == 0 {
		return chains
	}
	filtered := make([]agents.SupplyChain, 0, len(chains))
	for _, chain := range chains {
		if supplyChainMatchesPlanFilters(chain, productFilters, geoFilters) {
			filtered = append(filtered, chain)
		}
	}
	return filtered
}

func supplyChainMatchesPlanFilters(chain agents.SupplyChain, productFilters []string, geoFilters []string) bool {
	productOK := len(productFilters) == 0
	geoOK := len(geoFilters) == 0
	for _, node := range chain.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		if cp == "" {
			continue
		}
		if !productOK && supplyChainMatchesProductFilter(cp, productFilters) {
			productOK = true
		}
		if !geoOK && supplyChainNodeMatchesGeoFilter(cp, geoFilters) {
			geoOK = true
		}
		if productOK && geoOK {
			return true
		}
	}
	return productOK && geoOK
}

func filterSupplyChainsByAnchorScope(chains []agents.SupplyChain, query agents.SupplyChainQuery, prePlan *agents.SupplyChainPreHotPathPlan, targetGeoFilters []string) []agents.SupplyChain {
	if len(chains) == 0 {
		return nil
	}
	anchorTerms := buildSupplyChainAnchorScopeTerms(query, prePlan)
	targetGeoFilters = dedupStrings(targetGeoFilters)
	if len(targetGeoFilters) > 8 {
		targetGeoFilters = targetGeoFilters[:8]
	}
	if len(anchorTerms) == 0 && len(targetGeoFilters) == 0 {
		return chains
	}
	type scoredChain struct {
		chain         agents.SupplyChain
		score         float64
		anchorHit     bool
		targetGeoHit  bool
		anchorMatches int
	}
	scored := make([]scoredChain, 0, len(chains))
	strongN := 0
	anchorN := 0
	targetGeoN := 0
	for _, chain := range chains {
		score, anchorHit, targetGeoHit, anchorMatches := scoreSupplyChainAnchorScope(chain, anchorTerms, targetGeoFilters)
		if anchorHit {
			anchorN++
		}
		if targetGeoHit {
			targetGeoN++
		}
		if anchorHit && (len(targetGeoFilters) == 0 || targetGeoHit) {
			strongN++
		}
		scored = append(scored, scoredChain{
			chain:         chain,
			score:         score,
			anchorHit:     anchorHit,
			targetGeoHit:  targetGeoHit,
			anchorMatches: anchorMatches,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].anchorMatches > scored[j].anchorMatches
		}
		return scored[i].score > scored[j].score
	})
	keep := make([]agents.SupplyChain, 0, len(scored))
	appendChains := func(predicate func(scoredChain) bool) {
		for _, item := range scored {
			if predicate(item) {
				keep = append(keep, item.chain)
			}
		}
	}
	if len(targetGeoFilters) > 0 && strongN > 0 {
		topScore := scored[0].score
		for _, item := range scored {
			if !(item.anchorHit && item.targetGeoHit) {
				continue
			}
			if item.score+0.15 < topScore {
				continue
			}
			keep = append(keep, item.chain)
		}
		if len(keep) > 0 {
			if len(keep) > 12 {
				keep = keep[:12]
			}
			return keep
		}
	}
	switch {
	case strongN >= 3:
		appendChains(func(item scoredChain) bool {
			return item.anchorHit && (len(targetGeoFilters) == 0 || item.targetGeoHit)
		})
	case anchorN >= 3:
		appendChains(func(item scoredChain) bool { return item.anchorHit })
	case targetGeoN >= 3:
		appendChains(func(item scoredChain) bool { return item.targetGeoHit })
	default:
		topScore := 0.0
		if len(scored) > 0 {
			topScore = scored[0].score
		}
		for _, item := range scored {
			if item.score <= 0 {
				continue
			}
			if item.score+0.20 < topScore {
				continue
			}
			keep = append(keep, item.chain)
		}
	}
	if len(keep) == 0 {
		return chains
	}
	if len(keep) > 12 {
		keep = keep[:12]
	}
	return keep
}

func buildSupplyChainAnchorScopeTerms(query agents.SupplyChainQuery, prePlan *agents.SupplyChainPreHotPathPlan) []string {
	raw := make([]string, 0, 8)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			raw = append(raw, v)
		}
	}
	add(query.Anchor)
	for _, v := range query.Anchors {
		add(v)
	}
	if prePlan != nil {
		for _, v := range prePlan.CandidateAnchorTerms {
			add(v)
		}
	}
	out := make([]string, 0, len(raw)*2)
	seen := map[string]struct{}{}
	for _, candidate := range raw {
		for _, norm := range candidateAnchorScopeTerms(candidate) {
			if norm == "" {
				continue
			}
			if _, ok := seen[norm]; ok {
				continue
			}
			seen[norm] = struct{}{}
			out = append(out, norm)
		}
	}
	return out
}

func candidateAnchorScopeTerms(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := []string{raw}
	if strings.HasPrefix(raw, "/") {
		pathParts := strings.Split(strings.Trim(raw, "/"), "/")
		if len(pathParts) > 0 {
			parts = append(parts, pathParts[len(pathParts)-1])
		}
	}
	out := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		part = normalizeAnchorScopeText(part)
		if part == "" {
			continue
		}
		out = append(out, part)
		if fields := strings.Fields(part); len(fields) > 1 {
			out = append(out, strings.Join(fields[:1], " "))
		}
	}
	out = dedupStrings(out)
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}

func normalizeAnchorScopeText(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	raw = strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", ",", " ", "(", " ", ")", " ").Replace(raw)
	raw = strings.Join(strings.Fields(raw), " ")
	return raw
}

func scoreSupplyChainAnchorScope(chain agents.SupplyChain, anchorTerms []string, targetGeoFilters []string) (score float64, anchorHit bool, targetGeoHit bool, anchorMatches int) {
	if len(chain.Nodes) == 0 {
		return 0, false, false, 0
	}
	targetGeoFilters = dedupStrings(targetGeoFilters)
	if len(targetGeoFilters) > 8 {
		targetGeoFilters = targetGeoFilters[:8]
	}
	for _, node := range chain.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		if cp == "" {
			continue
		}
		if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/deposit/") {
			text := normalizeAnchorScopeText(cp + " " + strings.TrimSpace(node.Name))
			for _, term := range anchorTerms {
				if term == "" {
					continue
				}
				if strings.Contains(text, term) {
					anchorHit = true
					anchorMatches++
				}
			}
		}
		if !targetGeoHit && len(targetGeoFilters) > 0 && supplyChainNodeMatchesGeoFilter(cp, targetGeoFilters) {
			targetGeoHit = true
		}
	}
	if anchorHit {
		score += 0.65
	}
	if targetGeoHit {
		score += 0.35
	}
	if anchorMatches > 0 {
		score += clamp01(float64(anchorMatches) * 0.05)
	}
	if len(chain.TransportVia) > 0 {
		score += 0.05
	}
	if chain.Confidence > 0 {
		score += clamp01(chain.Confidence * 0.1)
	}
	return clamp01(score), anchorHit, targetGeoHit, anchorMatches
}

func supplyChainMatchesProductFilter(path string, productFilters []string) bool {
	if len(productFilters) == 0 {
		return true
	}
	path = strings.TrimSpace(strings.ToLower(path))
	if !strings.HasPrefix(path, "/product/") {
		return false
	}
	for _, raw := range productFilters {
		filter := taxonomy.Slugify(strings.ToLower(strings.TrimSpace(raw)))
		if filter == "" {
			continue
		}
		if strings.Contains(path, filter) {
			return true
		}
	}
	return false
}

func supplyChainNodeMatchesGeoFilter(path string, geoFilters []string) bool {
	if len(geoFilters) == 0 {
		return true
	}
	for _, token := range canonicalGeoTokensForPath(path) {
		for _, filter := range geoFilters {
			if geoTokenMatchesFilter(token, filter) {
				return true
			}
		}
	}
	return false
}

func canonicalGeoTokensForPath(path string) []string {
	parts, err := taxonomy.ParseCanonicalPathParts(strings.TrimSpace(path))
	if err != nil || parts == nil {
		return nil
	}
	switch parts.Namespace {
	case "org":
		if parts.Org != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Org.Geo))}
		}
	case "gov":
		if parts.Gov != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Gov.Geo))}
		}
	case "fac":
		if parts.Fac != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Fac.Geo))}
		}
	case "community":
		if parts.Community != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Community.Geo))}
		}
	case "market":
		if parts.Market != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Market.Geo))}
		}
	case "program":
		if parts.Program != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Program.Jurisdiction))}
		}
	case "loc":
		if parts.Loc != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Loc.Country))}
		}
	case "segment":
		if parts.Segment != nil {
			return []string{strings.ToLower(strings.TrimSpace(parts.Segment.Geo))}
		}
	}
	return nil
}

func geoTokenMatchesFilter(candidate string, filter string) bool {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	filter = strings.ToLower(strings.TrimSpace(filter))
	if candidate == "" || filter == "" {
		return false
	}
	if filter == "global" || candidate == filter {
		return true
	}
	if taxonomy.IsCompositeGeoToken(candidate) {
		for _, part := range strings.Split(candidate, "-") {
			if geoTokenMatchesFilter(part, filter) {
				return true
			}
		}
		return false
	}
	if taxonomy.IsCompositeGeoToken(filter) {
		for _, part := range strings.Split(filter, "-") {
			if geoTokenMatchesFilter(candidate, part) {
				return true
			}
		}
		return false
	}
	if taxonomy.IsRegionalGeoToken(filter) && len(candidate) == 2 {
		return taxonomy.IsCountryInRegion(filter, candidate)
	}
	if taxonomy.IsRegionalGeoToken(candidate) && len(filter) == 2 {
		return taxonomy.IsCountryInRegion(candidate, filter)
	}
	return false
}

// runSupplyChainRecursiveUnwinding implements step-by-step algorithmic unwinding:
// org→fac→product→recipientFac→recipientOrg, recursing from recipient orgs.
// Each step is a focused batched LLM call. maxDepth controls how many full
// cycles (org→fac→product→fac→org) to execute.
// anchorOrgs: initial /org/ paths to start from (Step 1).
// seedFacs: /fac/ anchors that skip Step 1 and enter directly at Step 2.
func (e *ScoutEngine) runSupplyChainRecursiveUnwinding(
	ctx context.Context, job *models.ScoutingJob, anchorOrgs []string, seedFacs []string, maxDepth int, productFilters []string, geoFilters []string,
) []agents.SupplyChain {
	if e.researchAgent == nil || (len(anchorOrgs) == 0 && len(seedFacs) == 0) {
		return nil
	}

	visited := map[string]struct{}{}
	for _, p := range anchorOrgs {
		visited[p] = struct{}{}
	}
	for _, p := range seedFacs {
		visited[p] = struct{}{}
	}

	type chainSegment struct {
		orgPath      string
		facPath      string
		productPath  string
		recipientFac string
		recipientOrg string
	}
	type productSource struct {
		productPath string
		facPath     string
	}
	type recipientLink struct {
		recipientFac string
		productPath  string
	}
	// facOwner tracks which org owns which fac across all depths.
	facOwner := map[string]string{}
	actorNodeType := func(path string) string {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "/deposit/") {
			return "DEPOSIT"
		}
		return "FACILITY"
	}
	buildChains := func(segments []chainSegment) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(segments))
		for _, seg := range segments {
			nodes := []agents.SupplyChainNode{
				{CanonicalPath: seg.orgPath, Type: "ORGANIZATION", Role: "supplier"},
				{CanonicalPath: seg.facPath, Type: actorNodeType(seg.facPath), Role: "producer"},
				{CanonicalPath: seg.productPath, Type: "PRODUCT"},
				{CanonicalPath: seg.recipientFac, Type: actorNodeType(seg.recipientFac), Role: "consumer"},
				{CanonicalPath: seg.recipientOrg, Type: "ORGANIZATION", Role: "customer"},
			}
			chains = append(chains, agents.SupplyChain{
				Nodes:      nodes,
				Confidence: 0.7,
				Reason:     "recursive unwinding",
			})
		}
		return chains
	}
	nodeMatchesProductFilter := func(cp string) bool {
		return supplyChainMatchesProductFilter(cp, productFilters)
	}
	buildStep1Chains := func(depthFacs []string) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(depthFacs))
		seen := map[string]struct{}{}
		for _, fac := range depthFacs {
			fac = strings.TrimSpace(fac)
			org := strings.TrimSpace(facOwner[fac])
			if fac == "" || org == "" {
				continue
			}
			if !strings.HasPrefix(org, "/org/") && !strings.HasPrefix(org, "/gov/") {
				continue
			}
			key := org + "|" + fac
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			chains = append(chains, agents.SupplyChain{
				Nodes: []agents.SupplyChainNode{
					{CanonicalPath: org, Type: "ORGANIZATION", Role: "supplier"},
					{CanonicalPath: fac, Type: actorNodeType(fac), Role: "producer"},
				},
				Confidence: 0.6,
				Reason:     "recursive unwinding step1",
			})
		}
		return chains
	}
	buildStep2Chains := func(productSources []productSource) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(productSources))
		seen := map[string]struct{}{}
		for _, ps := range productSources {
			fac := strings.TrimSpace(ps.facPath)
			prod := strings.TrimSpace(ps.productPath)
			org := strings.TrimSpace(facOwner[fac])
			if fac == "" || prod == "" || org == "" {
				continue
			}
			if !strings.HasPrefix(org, "/org/") && !strings.HasPrefix(org, "/gov/") {
				continue
			}
			key := org + "|" + fac + "|" + prod
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			chains = append(chains, agents.SupplyChain{
				Nodes: []agents.SupplyChainNode{
					{CanonicalPath: org, Type: "ORGANIZATION", Role: "supplier"},
					{CanonicalPath: fac, Type: actorNodeType(fac), Role: "producer"},
					{CanonicalPath: prod, Type: "PRODUCT"},
				},
				Confidence: 0.65,
				Reason:     "recursive unwinding step2",
			})
		}
		return chains
	}
	buildStep3Chains := func(productToSourceFacs map[string][]string, recipientLinks []recipientLink) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(recipientLinks))
		seen := map[string]struct{}{}
		for _, rl := range recipientLinks {
			recipFac := strings.TrimSpace(rl.recipientFac)
			prod := strings.TrimSpace(rl.productPath)
			if recipFac == "" || prod == "" {
				continue
			}
			for _, srcFac := range productToSourceFacs[prod] {
				srcFac = strings.TrimSpace(srcFac)
				srcOrg := strings.TrimSpace(facOwner[srcFac])
				if srcFac == "" || srcOrg == "" {
					continue
				}
				if !strings.HasPrefix(srcOrg, "/org/") && !strings.HasPrefix(srcOrg, "/gov/") {
					continue
				}
				key := srcOrg + "|" + srcFac + "|" + prod + "|" + recipFac
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				chains = append(chains, agents.SupplyChain{
					Nodes: []agents.SupplyChainNode{
						{CanonicalPath: srcOrg, Type: "ORGANIZATION", Role: "supplier"},
						{CanonicalPath: srcFac, Type: actorNodeType(srcFac), Role: "producer"},
						{CanonicalPath: prod, Type: "PRODUCT"},
						{CanonicalPath: recipFac, Type: actorNodeType(recipFac), Role: "consumer"},
					},
					Confidence: 0.68,
					Reason:     "recursive unwinding step3",
				})
			}
		}
		return chains
	}
	materializeStage := func(stage string, depth int, direction string, chains []agents.SupplyChain) {
		chains = filterSupplyChainsByPlanFilters(chains, productFilters, geoFilters)
		if len(chains) == 0 {
			return
		}
		prevStage := ""
		if job != nil {
			if job.State == nil {
				job.State = map[string]interface{}{}
			}
			prevStage = parseStateString(job.State["supplyChainMaterializationStage"])
			job.State["supplyChainMaterializationStage"] = stage
		}
		if err := e.processSupplyChains(ctx, job, nil, chains); err != nil {
			if job != nil {
				if strings.TrimSpace(prevStage) == "" {
					delete(job.State, "supplyChainMaterializationStage")
				} else {
					job.State["supplyChainMaterializationStage"] = prevStage
				}
			}
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwind] incremental materialization failed stage=%s depth=%d chains=%d: %v", stage, depth, len(chains), err))
			return
		}
		if job != nil {
			if job.State == nil {
				job.State = map[string]interface{}{}
			}
			job.State["supplyChainIncrementalMaterialized"] = true
			e.publish(ctx, realtime.EventType("horizon.supply_chain.depth.materialized"), map[string]interface{}{
				"tenantId":      job.TenantID,
				"projectId":     job.ProjectID,
				"lensId":        strings.TrimSpace(job.LensID),
				"jobId":         job.ID,
				"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"depth":         depth,
				"chainsN":       len(chains),
				"direction":     direction,
				"stage":         stage,
			})
			e.publishDirectedExplorationMaterialized(ctx, job, stage, direction, chains)
			if strings.TrimSpace(prevStage) == "" {
				delete(job.State, "supplyChainMaterializationStage")
			} else {
				job.State["supplyChainMaterializationStage"] = prevStage
			}
		}
	}

	var allSegments []chainSegment
	orgQueue := append([]string{}, anchorOrgs...)

	for depth := 0; depth < maxDepth; depth++ {
		stepCtx := agents.WithSupplyChainStepPromptContext(ctx, e.buildSupplyChainStepPromptContext(ctx, job, depth, "downstream", productFilters, geoFilters))
		maxOrgsPerDepth := supplyChainFastFirstIntLimit(job, "supplyChainFastFirstMaxOrgsPerDepth", agents.ScUnwindMaxOrgsPerDepth)
		maxFacsPerDepth := supplyChainFastFirstIntLimit(job, "supplyChainFastFirstMaxFacsPerDepth", agents.ScUnwindMaxFacsPerDepth)
		skipRecursiveStep4 := parseStateBool(job.State["supplyChainSkipRecursiveStep4"], false)
		// ---- Step 1: org → child orgs + facilities ----
		var depthFacs []string
		var childOrgs []string
		orgsForStep1 := append([]string{}, orgQueue...)

		if len(orgQueue) > 0 {
			if len(orgsForStep1) > maxOrgsPerDepth {
				orgsForStep1 = orgsForStep1[:maxOrgsPerDepth]
				scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step1 orgQueue capped %d→%d", depth, len(orgQueue), len(orgsForStep1)))
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step1 orgQueue=%d orgs=%v", depth, len(orgsForStep1), truncateSlice(orgsForStep1, 5)))

			orgFacResults, err := e.researchAgent.FindFacilitiesForOrgs(stepCtx, orgsForStep1)
			if err != nil {
				scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwind] step1 org→fac failed depth=%d: %v", depth, err))
				break
			}

			for _, r := range orgFacResults {
				for _, child := range r.ChildOrgs {
					child = strings.TrimSpace(child)
					if child == "" || !strings.HasPrefix(child, "/org/") || hasUnknownJurisdiction(child) {
						continue
					}
					if _, ok := visited[child]; !ok {
						visited[child] = struct{}{}
						childOrgs = append(childOrgs, child)
					}
				}
				for _, f := range r.Facilities {
					fp := strings.TrimSpace(f.Path)
					if fp == "" || (!strings.HasPrefix(fp, "/fac/") && !strings.HasPrefix(fp, "/deposit/")) {
						continue
					}
					if _, ok := visited[fp]; !ok {
						visited[fp] = struct{}{}
						depthFacs = append(depthFacs, fp)
						if owner := strings.TrimSpace(r.OrgPath); owner != "" && !hasUnknownJurisdiction(owner) {
							facOwner[fp] = owner
						}
					}
				}
			}

			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] step1 done depth=%d facs=%d childOrgs=%d", depth, len(depthFacs), len(childOrgs)))
		}

		// Inject seed facs at depth 0 (anchors that were already /fac/ paths).
		if depth == 0 && len(seedFacs) > 0 {
			depthFacs = append(depthFacs, seedFacs...)
			seedOrgResults, _ := e.researchAgent.FindOrgsForFacs(stepCtx, seedFacs)
			for _, r := range seedOrgResults {
				fp := strings.TrimSpace(r.FacPath)
				op := strings.TrimSpace(r.OrgPath)
				if fp != "" && (strings.HasPrefix(fp, "/fac/") || strings.HasPrefix(fp, "/deposit/")) && op != "" && !hasUnknownJurisdiction(op) {
					facOwner[fp] = op
				}
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=0 injected %d seed facs", len(seedFacs)))
		}

		// If child orgs were found, recurse Step 1 for them (capped levels and batch size).
		for recurseLevel := 0; recurseLevel < agents.ScUnwindMaxChildOrgRecurseLevels && len(childOrgs) > 0; recurseLevel++ {
			childOrgsBatch := childOrgs
			if len(childOrgsBatch) > maxOrgsPerDepth {
				childOrgsBatch = childOrgsBatch[:maxOrgsPerDepth]
				scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step1-recurse level=%d childOrgs capped %d→%d", depth, recurseLevel, len(childOrgs), len(childOrgsBatch)))
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step1-recurse level=%d childOrgs=%d", depth, recurseLevel, len(childOrgsBatch)))
			childResults, err := e.researchAgent.FindFacilitiesForOrgs(stepCtx, childOrgsBatch)
			if err != nil {
				scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwind] step1-recurse failed depth=%d level=%d: %v", depth, recurseLevel, err))
				break
			}
			var nextChildren []string
			for _, r := range childResults {
				for _, child := range r.ChildOrgs {
					child = strings.TrimSpace(child)
					if child == "" || !strings.HasPrefix(child, "/org/") || hasUnknownJurisdiction(child) {
						continue
					}
					if _, ok := visited[child]; !ok {
						visited[child] = struct{}{}
						nextChildren = append(nextChildren, child)
					}
				}
				for _, f := range r.Facilities {
					fp := strings.TrimSpace(f.Path)
					if fp == "" || (!strings.HasPrefix(fp, "/fac/") && !strings.HasPrefix(fp, "/deposit/")) {
						continue
					}
					if _, ok := visited[fp]; !ok {
						visited[fp] = struct{}{}
						depthFacs = append(depthFacs, fp)
						if owner := strings.TrimSpace(r.OrgPath); owner != "" && !hasUnknownJurisdiction(owner) {
							facOwner[fp] = owner
						}
					}
				}
			}
			childOrgs = nextChildren
		}

		if len(depthFacs) == 0 {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] no facilities at depth=%d, stopping", depth))
			break
		}
		materializeStage("step1", depth, "downstream", buildStep1Chains(depthFacs))
		if shouldStopSupplyChainAfterFirstIncremental(job) {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d fast-first stop after step1 incremental materialization", depth))
			break
		}

		// ---- Step 2: fac → products ----
		facsForStep2 := depthFacs
		if len(facsForStep2) > maxFacsPerDepth {
			facsForStep2 = facsForStep2[:maxFacsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step2 depthFacs capped %d→%d", depth, len(depthFacs), len(facsForStep2)))
		}
		facProdResults, err := e.researchAgent.FindProductsForFacs(stepCtx, facsForStep2)
		if err != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwind] step2 fac→product failed depth=%d: %v", depth, err))
			break
		}

		// One fac can produce many products: store (product, sourceFac) pairs.
		var productSources []productSource
		for _, r := range facProdResults {
			for _, p := range r.Products {
				pp := strings.TrimSpace(p.Path)
				if pp == "" || !strings.HasPrefix(pp, "/product/") {
					continue
				}
				if !nodeMatchesProductFilter(pp) {
					continue
				}
				if _, ok := visited[pp]; !ok {
					visited[pp] = struct{}{}
				}
				productSources = append(productSources, productSource{productPath: pp, facPath: r.FacPath})
			}
		}

		uniqueProducts := map[string]struct{}{}
		var productList []string
		for _, ps := range productSources {
			if _, ok := uniqueProducts[ps.productPath]; !ok {
				uniqueProducts[ps.productPath] = struct{}{}
				productList = append(productList, ps.productPath)
			}
		}

		scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] step2 done depth=%d products=%d (from %d facs)", depth, len(productList), len(depthFacs)))
		materializeStage("step2", depth, "downstream", buildStep2Chains(productSources))
		if shouldStopSupplyChainAfterFirstIncremental(job) {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d fast-first stop after step2 incremental materialization", depth))
			break
		}

		if len(productList) == 0 {
			break
		}

		// ---- Step 3: product → recipient facilities ----
		productsForStep3 := productList
		if len(productsForStep3) > agents.ScUnwindMaxProductsPerDepth {
			productsForStep3 = productsForStep3[:agents.ScUnwindMaxProductsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step3 productList capped %d→%d", depth, len(productList), len(productsForStep3)))
		}
		prodRecipResults, err := e.researchAgent.FindRecipientFacsForProducts(stepCtx, productsForStep3)
		if err != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwind] step3 product→recipientFac failed depth=%d: %v", depth, err))
			break
		}

		// One product can go to many recipients: store (recipientFac, productPath) pairs.
		var recipientLinks []recipientLink
		var recipientFacList []string
		recipientSeen := map[string]struct{}{}
		for _, r := range prodRecipResults {
			for _, rf := range r.Recipients {
				rfp := strings.TrimSpace(rf.Path)
				if rfp == "" || !strings.HasPrefix(rfp, "/fac/") {
					continue
				}
				recipientLinks = append(recipientLinks, recipientLink{recipientFac: rfp, productPath: r.ProductPath})
				if _, ok := recipientSeen[rfp]; !ok {
					recipientSeen[rfp] = struct{}{}
					if _, ok2 := visited[rfp]; !ok2 {
						visited[rfp] = struct{}{}
					}
					recipientFacList = append(recipientFacList, rfp)
				}
			}
		}

		scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] step3 done depth=%d recipientFacs=%d links=%d", depth, len(recipientFacList), len(recipientLinks)))

		if len(recipientFacList) == 0 {
			break
		}
		productToSourceFacs := map[string][]string{}
		for _, ps := range productSources {
			productToSourceFacs[ps.productPath] = append(productToSourceFacs[ps.productPath], ps.facPath)
		}
		materializeStage("step3", depth, "downstream", buildStep3Chains(productToSourceFacs, recipientLinks))
		if shouldStopSupplyChainAfterFirstIncremental(job) {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d fast-first stop after step3 incremental materialization", depth))
			break
		}

		// ---- Step 4: recipient fac → owning org ----
		if skipRecursiveStep4 {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step4 skipped by fast-first cap", depth))
			break
		}
		recipFacsForStep4 := recipientFacList
		if len(recipFacsForStep4) > agents.ScUnwindMaxRecipientFacsPerDepth {
			recipFacsForStep4 = recipFacsForStep4[:agents.ScUnwindMaxRecipientFacsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d step4 recipientFacList capped %d→%d", depth, len(recipientFacList), len(recipFacsForStep4)))
		}
		facOrgResults, err := e.researchAgent.FindOrgsForFacs(stepCtx, recipFacsForStep4)
		if err != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwind] step4 fac→org failed depth=%d: %v", depth, err))
			break
		}

		recipFacOrg := map[string]string{}
		for _, r := range facOrgResults {
			orgP := strings.TrimSpace(r.OrgPath)
			facP := strings.TrimSpace(r.FacPath)
			if orgP != "" && facP != "" && !hasUnknownJurisdiction(orgP) {
				recipFacOrg[facP] = orgP
				facOwner[facP] = orgP
			}
		}

		segmentsBeforeDepth := len(allSegments)
		nextOrgQueue := map[string]struct{}{}
		for _, rl := range recipientLinks {
			recipOrg := recipFacOrg[rl.recipientFac]
			sourceFacs := productToSourceFacs[rl.productPath]
			for _, sf := range sourceFacs {
				srcOrg := facOwner[sf]
				if srcOrg == "" || recipOrg == "" {
					continue
				}
				if !strings.HasPrefix(srcOrg, "/org/") && !strings.HasPrefix(srcOrg, "/gov/") {
					continue
				}
				if !strings.HasPrefix(recipOrg, "/org/") && !strings.HasPrefix(recipOrg, "/gov/") {
					continue
				}
				allSegments = append(allSegments, chainSegment{
					orgPath:      srcOrg,
					facPath:      sf,
					productPath:  rl.productPath,
					recipientFac: rl.recipientFac,
					recipientOrg: recipOrg,
				})
			}
			if recipOrg != "" && (strings.HasPrefix(recipOrg, "/org/") || strings.HasPrefix(recipOrg, "/gov/")) {
				if hasUnknownJurisdiction(recipOrg) {
					continue
				}
				if _, ok := visited[recipOrg]; !ok {
					visited[recipOrg] = struct{}{}
					nextOrgQueue[recipOrg] = struct{}{}
				}
			}
		}

		orgQueue = make([]string, 0, len(nextOrgQueue))
		for o := range nextOrgQueue {
			orgQueue = append(orgQueue, o)
		}
		if len(orgQueue) > maxOrgsPerDepth {
			orgQueue = orgQueue[:maxOrgsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d nextOrgQueue capped to %d", depth, maxOrgsPerDepth))
		}

		scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] step4 done depth=%d totalSegments=%d nextOrgs=%d", depth, len(allSegments), len(orgQueue)))
		if len(allSegments) > segmentsBeforeDepth {
			newChains := buildChains(allSegments[segmentsBeforeDepth:])
			materializeStage("step4", depth, "downstream", newChains)
			if shouldStopSupplyChainAfterFirstIncremental(job) {
				scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] depth=%d fast-first stop after step4 incremental materialization", depth))
				break
			}
		}

		if len(orgQueue) == 0 {
			break
		}
	}

	scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwind] complete: totalSegments=%d visited=%d", len(allSegments), len(visited)))

	return filterSupplyChainsByPlanFilters(buildChains(allSegments), productFilters, geoFilters)
}

// runSupplyChainUpstreamUnwinding implements step-by-step UPSTREAM unwinding:
// consumerOrg→consumerFac→product→supplierFac→supplierOrg, recursing from supplier orgs.
// Flow: Step 1 (org→fac), U1 (fac→input products), U2 (product→supplier facs), Step 4 (fac→org).
func (e *ScoutEngine) runSupplyChainUpstreamUnwinding(
	ctx context.Context, job *models.ScoutingJob, anchorOrgs []string, seedFacs []string, maxDepth int, productFilters []string, geoFilters []string,
) []agents.SupplyChain {
	if e.researchAgent == nil || (len(anchorOrgs) == 0 && len(seedFacs) == 0) {
		return nil
	}

	visited := map[string]struct{}{}
	for _, p := range anchorOrgs {
		visited[p] = struct{}{}
	}
	for _, p := range seedFacs {
		visited[p] = struct{}{}
	}

	type chainSegment struct {
		supplierOrg string
		supplierFac string
		productPath string
		consumerFac string
		consumerOrg string
	}
	type facProductLink struct {
		consumerFac string
		productPath string
	}
	facOwner := map[string]string{}
	actorNodeType := func(path string) string {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "/deposit/") {
			return "DEPOSIT"
		}
		return "FACILITY"
	}
	buildChains := func(segments []chainSegment) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(segments))
		for _, seg := range segments {
			nodes := []agents.SupplyChainNode{
				{CanonicalPath: seg.supplierOrg, Type: "ORGANIZATION", Role: "supplier"},
				{CanonicalPath: seg.supplierFac, Type: actorNodeType(seg.supplierFac), Role: "producer"},
				{CanonicalPath: seg.productPath, Type: "PRODUCT"},
				{CanonicalPath: seg.consumerFac, Type: actorNodeType(seg.consumerFac), Role: "consumer"},
				{CanonicalPath: seg.consumerOrg, Type: "ORGANIZATION", Role: "customer"},
			}
			chains = append(chains, agents.SupplyChain{
				Nodes:      nodes,
				Confidence: 0.7,
				Reason:     "upstream unwinding",
			})
		}
		return chains
	}
	buildStep1Chains := func(depthFacs []string) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(depthFacs))
		seen := map[string]struct{}{}
		for _, fac := range depthFacs {
			fac = strings.TrimSpace(fac)
			org := strings.TrimSpace(facOwner[fac])
			if fac == "" || org == "" {
				continue
			}
			if !strings.HasPrefix(org, "/org/") && !strings.HasPrefix(org, "/gov/") {
				continue
			}
			key := org + "|" + fac
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			chains = append(chains, agents.SupplyChain{
				Nodes: []agents.SupplyChainNode{
					{CanonicalPath: org, Type: "ORGANIZATION", Role: "customer"},
					{CanonicalPath: fac, Type: actorNodeType(fac), Role: "consumer"},
				},
				Confidence: 0.6,
				Reason:     "upstream unwinding step1",
			})
		}
		return chains
	}
	buildU1Chains := func(facProductLinks []facProductLink) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(facProductLinks))
		seen := map[string]struct{}{}
		for _, fpl := range facProductLinks {
			fac := strings.TrimSpace(fpl.consumerFac)
			prod := strings.TrimSpace(fpl.productPath)
			org := strings.TrimSpace(facOwner[fac])
			if fac == "" || prod == "" || org == "" {
				continue
			}
			if !strings.HasPrefix(org, "/org/") && !strings.HasPrefix(org, "/gov/") {
				continue
			}
			key := org + "|" + fac + "|" + prod
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			chains = append(chains, agents.SupplyChain{
				Nodes: []agents.SupplyChainNode{
					{CanonicalPath: org, Type: "ORGANIZATION", Role: "customer"},
					{CanonicalPath: fac, Type: actorNodeType(fac), Role: "consumer"},
					{CanonicalPath: prod, Type: "PRODUCT"},
				},
				Confidence: 0.65,
				Reason:     "upstream unwinding U1",
			})
		}
		return chains
	}
	buildU2Chains := func(facProductLinks []facProductLink, productToSupplierFacs map[string][]string) []agents.SupplyChain {
		chains := make([]agents.SupplyChain, 0, len(facProductLinks))
		seen := map[string]struct{}{}
		for _, fpl := range facProductLinks {
			consumerFac := strings.TrimSpace(fpl.consumerFac)
			prod := strings.TrimSpace(fpl.productPath)
			if consumerFac == "" || prod == "" {
				continue
			}
			consumerOrg := strings.TrimSpace(facOwner[consumerFac])
			if consumerOrg == "" {
				continue
			}
			if !strings.HasPrefix(consumerOrg, "/org/") && !strings.HasPrefix(consumerOrg, "/gov/") {
				continue
			}
			for _, supplierFac := range productToSupplierFacs[prod] {
				supplierFac = strings.TrimSpace(supplierFac)
				if supplierFac == "" {
					continue
				}
				key := consumerOrg + "|" + supplierFac + "|" + prod + "|" + consumerFac
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				chains = append(chains, agents.SupplyChain{
					Nodes: []agents.SupplyChainNode{
						{CanonicalPath: consumerOrg, Type: "ORGANIZATION", Role: "customer"},
						{CanonicalPath: supplierFac, Type: actorNodeType(supplierFac), Role: "supplier"},
						{CanonicalPath: prod, Type: "PRODUCT"},
						{CanonicalPath: consumerFac, Type: actorNodeType(consumerFac), Role: "consumer"},
					},
					Confidence: 0.68,
					Reason:     "upstream unwinding U2",
				})
			}
		}
		return chains
	}
	nodeMatchesProductFilter := func(cp string) bool {
		return supplyChainMatchesProductFilter(cp, productFilters)
	}
	materializeStage := func(stage string, depth int, direction string, chains []agents.SupplyChain) {
		chains = filterSupplyChainsByPlanFilters(chains, productFilters, geoFilters)
		if len(chains) == 0 {
			return
		}
		prevStage := ""
		if job != nil {
			if job.State == nil {
				job.State = map[string]interface{}{}
			}
			prevStage = parseStateString(job.State["supplyChainMaterializationStage"])
			job.State["supplyChainMaterializationStage"] = stage
		}
		if err := e.processSupplyChains(ctx, job, nil, chains); err != nil {
			if job != nil {
				if strings.TrimSpace(prevStage) == "" {
					delete(job.State, "supplyChainMaterializationStage")
				} else {
					job.State["supplyChainMaterializationStage"] = prevStage
				}
			}
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwindUp] incremental materialization failed stage=%s depth=%d chains=%d: %v", stage, depth, len(chains), err))
			return
		}
		if job != nil {
			if job.State == nil {
				job.State = map[string]interface{}{}
			}
			job.State["supplyChainIncrementalMaterialized"] = true
			e.publish(ctx, realtime.EventType("horizon.supply_chain.depth.materialized"), map[string]interface{}{
				"tenantId":      job.TenantID,
				"projectId":     job.ProjectID,
				"lensId":        strings.TrimSpace(job.LensID),
				"jobId":         job.ID,
				"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"depth":         depth,
				"chainsN":       len(chains),
				"direction":     direction,
				"stage":         stage,
			})
			e.publishDirectedExplorationMaterialized(ctx, job, stage, direction, chains)
			if strings.TrimSpace(prevStage) == "" {
				delete(job.State, "supplyChainMaterializationStage")
			} else {
				job.State["supplyChainMaterializationStage"] = prevStage
			}
		}
	}

	var allSegments []chainSegment
	orgQueue := append([]string{}, anchorOrgs...)

	for depth := 0; depth < maxDepth; depth++ {
		stepCtx := agents.WithSupplyChainStepPromptContext(ctx, e.buildSupplyChainStepPromptContext(ctx, job, depth, "upstream", productFilters, geoFilters))
		maxOrgsPerDepth := supplyChainFastFirstIntLimit(job, "supplyChainFastFirstMaxOrgsPerDepth", agents.ScUnwindMaxOrgsPerDepth)
		maxFacsPerDepth := supplyChainFastFirstIntLimit(job, "supplyChainFastFirstMaxFacsPerDepth", agents.ScUnwindMaxFacsPerDepth)
		skipRecursiveStep4 := parseStateBool(job.State["supplyChainSkipRecursiveStep4"], false)
		var depthFacs []string
		var childOrgs []string
		orgsForStep1 := append([]string{}, orgQueue...)

		if len(orgQueue) > 0 {
			if len(orgsForStep1) > maxOrgsPerDepth {
				orgsForStep1 = orgsForStep1[:maxOrgsPerDepth]
				scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d step1 orgQueue capped %d→%d", depth, len(orgQueue), len(orgsForStep1)))
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d step1 orgQueue=%d orgs=%v", depth, len(orgsForStep1), truncateSlice(orgsForStep1, 5)))

			orgFacResults, err := e.researchAgent.FindFacilitiesForOrgs(stepCtx, orgsForStep1)
			if err != nil {
				scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwindUp] step1 org→fac failed depth=%d: %v", depth, err))
				break
			}

			for _, r := range orgFacResults {
				for _, child := range r.ChildOrgs {
					child = strings.TrimSpace(child)
					if child == "" || !strings.HasPrefix(child, "/org/") || hasUnknownJurisdiction(child) {
						continue
					}
					if _, ok := visited[child]; !ok {
						visited[child] = struct{}{}
						childOrgs = append(childOrgs, child)
					}
				}
				for _, f := range r.Facilities {
					fp := strings.TrimSpace(f.Path)
					if fp == "" || (!strings.HasPrefix(fp, "/fac/") && !strings.HasPrefix(fp, "/deposit/")) {
						continue
					}
					if _, ok := visited[fp]; !ok {
						visited[fp] = struct{}{}
						depthFacs = append(depthFacs, fp)
						if owner := strings.TrimSpace(r.OrgPath); owner != "" && !hasUnknownJurisdiction(owner) {
							facOwner[fp] = owner
						}
					}
				}
			}

			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] step1 done depth=%d facs=%d childOrgs=%d", depth, len(depthFacs), len(childOrgs)))
		}

		if depth == 0 && len(seedFacs) > 0 {
			depthFacs = append(depthFacs, seedFacs...)
			// Populate facOwner for seed facs (they skip Step 1)
			seedOrgResults, _ := e.researchAgent.FindOrgsForFacs(stepCtx, seedFacs)
			for _, r := range seedOrgResults {
				if fp, op := strings.TrimSpace(r.FacPath), strings.TrimSpace(r.OrgPath); fp != "" && (strings.HasPrefix(fp, "/fac/") || strings.HasPrefix(fp, "/deposit/")) && op != "" && !hasUnknownJurisdiction(op) {
					facOwner[fp] = op
				}
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=0 injected %d seed facs", len(seedFacs)))
		}

		for recurseLevel := 0; recurseLevel < agents.ScUnwindMaxChildOrgRecurseLevels && len(childOrgs) > 0; recurseLevel++ {
			childOrgsBatch := childOrgs
			if len(childOrgsBatch) > maxOrgsPerDepth {
				childOrgsBatch = childOrgsBatch[:maxOrgsPerDepth]
				scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d step1-recurse level=%d childOrgs capped %d→%d", depth, recurseLevel, len(childOrgs), len(childOrgsBatch)))
			}
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d step1-recurse level=%d childOrgs=%d", depth, recurseLevel, len(childOrgsBatch)))
			childResults, err := e.researchAgent.FindFacilitiesForOrgs(stepCtx, childOrgsBatch)
			if err != nil {
				scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwindUp] step1-recurse failed depth=%d level=%d: %v", depth, recurseLevel, err))
				break
			}
			var nextChildren []string
			for _, r := range childResults {
				for _, child := range r.ChildOrgs {
					child = strings.TrimSpace(child)
					if child == "" || !strings.HasPrefix(child, "/org/") || hasUnknownJurisdiction(child) {
						continue
					}
					if _, ok := visited[child]; !ok {
						visited[child] = struct{}{}
						nextChildren = append(nextChildren, child)
					}
				}
				for _, f := range r.Facilities {
					fp := strings.TrimSpace(f.Path)
					if fp == "" || (!strings.HasPrefix(fp, "/fac/") && !strings.HasPrefix(fp, "/deposit/")) {
						continue
					}
					if _, ok := visited[fp]; !ok {
						visited[fp] = struct{}{}
						depthFacs = append(depthFacs, fp)
						if owner := strings.TrimSpace(r.OrgPath); owner != "" && !hasUnknownJurisdiction(owner) {
							facOwner[fp] = owner
						}
					}
				}
			}
			childOrgs = nextChildren
		}

		if len(depthFacs) == 0 {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] no facilities at depth=%d, stopping", depth))
			break
		}
		materializeStage("step1", depth, "upstream", buildStep1Chains(depthFacs))
		if shouldStopSupplyChainAfterFirstIncremental(job) {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d fast-first stop after step1 incremental materialization", depth))
			break
		}

		// ---- Step U1: fac → input products ----
		facsForU1 := depthFacs
		if len(facsForU1) > maxFacsPerDepth {
			facsForU1 = facsForU1[:maxFacsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d U1 depthFacs capped %d→%d", depth, len(depthFacs), len(facsForU1)))
		}
		facInputResults, err := e.researchAgent.FindInputProductsForFacs(stepCtx, facsForU1)
		if err != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwindUp] U1 fac→inputProducts failed depth=%d: %v", depth, err))
			break
		}

		var facProductLinks []facProductLink
		productSeen := map[string]struct{}{}
		var productList []string
		for _, r := range facInputResults {
			for _, p := range r.InputProducts {
				pp := strings.TrimSpace(p.Path)
				if pp == "" || !strings.HasPrefix(pp, "/product/") {
					continue
				}
				if !nodeMatchesProductFilter(pp) {
					continue
				}
				facProductLinks = append(facProductLinks, facProductLink{consumerFac: r.FacPath, productPath: pp})
				if _, ok := productSeen[pp]; !ok {
					productSeen[pp] = struct{}{}
					productList = append(productList, pp)
				}
			}
		}

		scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] U1 done depth=%d products=%d (from %d facs)", depth, len(productList), len(depthFacs)))
		materializeStage("U1", depth, "upstream", buildU1Chains(facProductLinks))
		if shouldStopSupplyChainAfterFirstIncremental(job) {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d fast-first stop after U1 incremental materialization", depth))
			break
		}

		if len(productList) == 0 {
			break
		}

		// ---- Step U2: product → supplier facilities ----
		productsForU2 := productList
		if len(productsForU2) > agents.ScUnwindMaxProductsPerDepth {
			productsForU2 = productsForU2[:agents.ScUnwindMaxProductsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d U2 productList capped %d→%d", depth, len(productList), len(productsForU2)))
		}
		prodSuppResults, err := e.researchAgent.FindSupplierFacsForProducts(stepCtx, productsForU2)
		if err != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwindUp] U2 product→suppliers failed depth=%d: %v", depth, err))
			break
		}

		productToSupplierFacs := map[string][]string{}
		var supplierFacList []string
		supplierSeen := map[string]struct{}{}
		for _, r := range prodSuppResults {
			for _, sf := range r.Suppliers {
				sfp := strings.TrimSpace(sf.Path)
				if sfp == "" || (!strings.HasPrefix(sfp, "/fac/") && !strings.HasPrefix(sfp, "/deposit/")) {
					continue
				}
				productToSupplierFacs[r.ProductPath] = append(productToSupplierFacs[r.ProductPath], sfp)
				if _, ok := supplierSeen[sfp]; !ok {
					supplierSeen[sfp] = struct{}{}
					supplierFacList = append(supplierFacList, sfp)
				}
			}
		}

		scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] U2 done depth=%d supplierFacs=%d", depth, len(supplierFacList)))

		if len(supplierFacList) == 0 {
			break
		}
		materializeStage("U2", depth, "upstream", buildU2Chains(facProductLinks, productToSupplierFacs))
		if shouldStopSupplyChainAfterFirstIncremental(job) {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d fast-first stop after U2 incremental materialization", depth))
			break
		}

		// ---- Step 4: supplier fac → owning org ----
		if skipRecursiveStep4 {
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d step4 skipped by fast-first cap", depth))
			break
		}
		supplierFacsForStep4 := supplierFacList
		if len(supplierFacsForStep4) > agents.ScUnwindMaxRecipientFacsPerDepth {
			supplierFacsForStep4 = supplierFacsForStep4[:agents.ScUnwindMaxRecipientFacsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d step4 supplierFacList capped %d→%d", depth, len(supplierFacList), len(supplierFacsForStep4)))
		}
		facOrgResults, err := e.researchAgent.FindOrgsForFacs(stepCtx, supplierFacsForStep4)
		if err != nil {
			scLog(ctx, e, job, "warn", fmt.Sprintf("[SCUnwindUp] step4 fac→org failed depth=%d: %v", depth, err))
			break
		}

		supplierFacOrg := map[string]string{}
		for _, r := range facOrgResults {
			orgP := strings.TrimSpace(r.OrgPath)
			facP := strings.TrimSpace(r.FacPath)
			if orgP != "" && facP != "" && !hasUnknownJurisdiction(orgP) {
				supplierFacOrg[facP] = orgP
				facOwner[facP] = orgP
			}
		}

		// Build segments: for each facProductLink, trace to supplierFacs and build supplierOrg→supplierFac→product→consumerFac→consumerOrg.
		segmentsBeforeDepth := len(allSegments)
		nextOrgQueue := map[string]struct{}{}
		for _, fpl := range facProductLinks {
			supplierFacs := productToSupplierFacs[fpl.productPath]
			consumerOrg := facOwner[fpl.consumerFac]
			for _, sf := range supplierFacs {
				supplierOrg := supplierFacOrg[sf]
				// Skip segments with empty orgs — parser can't materialize valid edges.
				if supplierOrg == "" || consumerOrg == "" {
					continue
				}
				if !strings.HasPrefix(supplierOrg, "/org/") && !strings.HasPrefix(supplierOrg, "/gov/") {
					continue
				}
				if !strings.HasPrefix(consumerOrg, "/org/") && !strings.HasPrefix(consumerOrg, "/gov/") {
					continue
				}
				allSegments = append(allSegments, chainSegment{
					supplierOrg: supplierOrg,
					supplierFac: sf,
					productPath: fpl.productPath,
					consumerFac: fpl.consumerFac,
					consumerOrg: consumerOrg,
				})
			}
			for _, sf := range supplierFacs {
				supplierOrg := supplierFacOrg[sf]
				if supplierOrg != "" && (strings.HasPrefix(supplierOrg, "/org/") || strings.HasPrefix(supplierOrg, "/gov/")) {
					if hasUnknownJurisdiction(supplierOrg) {
						continue
					}
					if _, ok := visited[supplierOrg]; !ok {
						visited[supplierOrg] = struct{}{}
						nextOrgQueue[supplierOrg] = struct{}{}
					}
				}
			}
		}

		orgQueue = make([]string, 0, len(nextOrgQueue))
		for o := range nextOrgQueue {
			orgQueue = append(orgQueue, o)
		}
		if len(orgQueue) > maxOrgsPerDepth {
			orgQueue = orgQueue[:maxOrgsPerDepth]
			scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d nextOrgQueue capped to %d", depth, maxOrgsPerDepth))
		}

		scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] step4 done depth=%d totalSegments=%d nextOrgs=%d", depth, len(allSegments), len(orgQueue)))
		if len(allSegments) > segmentsBeforeDepth {
			newChains := buildChains(allSegments[segmentsBeforeDepth:])
			materializeStage("step4", depth, "upstream", newChains)
			if shouldStopSupplyChainAfterFirstIncremental(job) {
				scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] depth=%d fast-first stop after step4 incremental materialization", depth))
				break
			}
		}

		if len(orgQueue) == 0 {
			break
		}
	}

	scLog(ctx, e, job, "info", fmt.Sprintf("[SCUnwindUp] complete: totalSegments=%d visited=%d", len(allSegments), len(visited)))

	return filterSupplyChainsByPlanFilters(buildChains(allSegments), productFilters, geoFilters)
}

// scLog logs to both realtime (frontend) and stderr (GCP).
func scLog(ctx context.Context, e *ScoutEngine, job *models.ScoutingJob, level string, msg string) {
	log.Printf("%s", msg)
	if e != nil {
		e.logLine(ctx, job, level, msg)
	}
}

func truncateSlice(s []string, max int) []string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
