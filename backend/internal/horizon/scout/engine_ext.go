package scout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
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

func hasUnknownGeoSegments(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	if !strings.HasPrefix(p, "/loc/") {
		return false
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// Strict geo shape: /loc/{country}/{admin1}/{city}
	if len(parts) < 4 {
		return true
	}
	return parts[2] == "na" || parts[3] == "na"
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
	if strings.HasPrefix(canonicalPath, "/loc/") {
		parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
		if len(parts) >= 4 {
			cc := strings.TrimSpace(strings.ToLower(parts[1]))
			admin1 := strings.TrimSpace(strings.ToLower(parts[2]))
			city := strings.TrimSpace(strings.ToLower(parts[3]))
			if cc != "" && admin1 == "na" && city == "na" {
				if label == "" || len(label) <= 2 || strings.EqualFold(label, cc) {
					if n := countryNameByISO2(cc); n != "" {
						return n
					}
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
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 4 {
			cc := strings.ToLower(strings.TrimSpace(parts[1]))
			admin1 := strings.ToLower(strings.TrimSpace(parts[2]))
			city := strings.ToLower(strings.TrimSpace(parts[3]))
			// /loc/{cc}/na/na is valid taxonomy, but never a good display label.
			if (admin1 == "na" && city == "na") && (lcur == "na" || lcur == cc || lcur == city) {
				return true
			}
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

		// Sector / industry
		case "sector-overview":
			base = "Sector overview"
		case "industry-structure":
			base = "Industry structure"
		case "vertical-deep-dive":
			base = "Vertical deep-dive"

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
	for _, key := range []string{"domainMode", "domainModes", "domains"} {
		if v, ok := parentState[key]; ok && v != nil {
			childState[key] = v
		}
	}
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

	// 1) Direct scope nodes (exclude domains; Phase2 job already has the domain fragment).
	directPaths := make([]string, 0, len(lens.Scope))
	seenPath := map[string]struct{}{}
	for _, p := range lens.Scope {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "/domain/") {
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
// then create LOCATED_IN -> /loc/{geo}/na/na (best effort).
// Special-case:
// - /org/int/... maps to /loc/global/na/na as the international bucket.
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
	targetPath := fmt.Sprintf("/loc/%s/na/na", geoToken)
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

func (e *ScoutEngine) orgHasAnyGeoLocationEdge(ctx context.Context, orgID string) bool {
	if e == nil || e.edgesRepo == nil || strings.TrimSpace(orgID) == "" {
		return false
	}
	types := []models.DependencyType{
		models.DependencyTypeLocatedIn,
		models.DependencyTypeOperatesIn,
	}
	for _, t := range types {
		tt := t
		edges, err := e.edgesRepo.ListBySource(ctx, "global", orgID, &tt, 5)
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
		if norm, err := taxonomy.NormalizeDomainCanonicalPath(cp); err == nil && strings.TrimSpace(norm) != "" {
			return strings.TrimSpace(norm)
		}
	}
	return cp
}

func domainFacetSignature(contractType string, facets map[string][]string) string {
	ct := strings.ToLower(strings.TrimSpace(contractType))
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

// ensureJobDomainMode computes domain mode(s) from job state (domains, path, specialties) and writes job.State["domainMode"] or job.State["domainModes"].
// Call once at the start of job execution, after effectiveCanonicalPath is known.
func (e *ScoutEngine) ensureJobDomainMode(job *models.ScoutingJob, effectiveCanonicalPath string) {
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
		if raw, ok := job.State["domains"]; ok && raw != nil {
			switch t := raw.(type) {
			case []agents.DomainResolution:
				for _, d := range t {
					ct := strings.TrimSpace(d.ContractType)
					if ct != "" {
						contractTypes = append(contractTypes, ct)
					}
				}
			case []map[string]interface{}:
				for _, m := range t {
					ct := getStringField(m, "contractType", "ContractType")
					if ct != "" {
						contractTypes = append(contractTypes, ct)
					}
				}
			case []interface{}:
				for _, it := range t {
					m, ok := it.(map[string]interface{})
					if !ok {
						continue
					}
					ct := getStringField(m, "contractType", "ContractType")
					if ct != "" {
						contractTypes = append(contractTypes, ct)
					}
				}
			}
		}
	}
	// 3) From specialties fallback
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
	if len(contractTypes) > 0 {
		modes = domain.DomainModesForContractTypes(contractTypes)
	}
	modes = taxonomy.NormalizeDomainModes(modes)
	if len(modes) == 0 {
		modes = []string{taxonomy.DomainModeGeneral}
	}
	if len(modes) == 1 {
		job.State["domainMode"] = modes[0]
		delete(job.State, "domainModes")
	} else {
		job.State["domainModes"] = modes
		delete(job.State, "domainMode")
	}
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
	if domainCP == "" || !strings.HasPrefix(domainCP, "/domain/") {
		return nil
	}
	internalFirstContext := ""
	domainFastPathApplied := false
	if internalFirstGroundingEnabled() {
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
			// Minimal deterministic market anchor:
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
			// - If industry present: /market/{geo}/technology/{industry}
			if inds := getFacet("industry"); len(inds) > 0 {
				ind := taxonomy.Slugify(inds[0])
				if ind != "" {
					mp := "/market/" + geoToken + "/technology/" + ind
					if err := taxonomy.ValidateCanonicalPath(mp); err == nil {
						firstOrder = append(firstOrder, mp)
					}
				}
			}
			// - If sector present: /market/{geo}/technology/{sector}
			if secs := getFacet("sector"); len(secs) > 0 {
				sec := taxonomy.Slugify(secs[0])
				if sec != "" {
					mp := "/market/" + geoToken + "/technology/" + sec
					if err := taxonomy.ValidateCanonicalPath(mp); err == nil {
						firstOrder = append(firstOrder, mp)
					}
				}
			}
		}
	}

	// LLM-based domain research: reuse standard ResearchAgent to keep parity with non-domain research.
	// IntentResolver is NOT required to fully expand domains; this step is responsible for that.
	var domainResearchRelations []agents.DiscoveredRelation
	var domainResearchChains []agents.SupplyChain
	var domainGroundingEvidence *ai.GroundingEvidence
	if e.researchAgent != nil && !domainFastPathApplied {
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
			fmt.Sprintf("Expand this DOMAIN contract into 5-10 first-order entities and relations. Use DOMAIN relation types (FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, AFFECTS, DEPENDS_ON) and valid canonical paths only. ContractType=%s; Facets=%v; Title=%s.", strings.TrimSpace(contractType), parsedFacets, title),
		}
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
		enrichedFragment, newRelations, newChains, evidence, err := e.researchAgent.EnrichFragmentWithGrounding(ctx, domainFragment, domainMissing, focus, spherePaths, keywords, specialties, internalFirstContext, originalQuery, nil, existingEntities)
		_ = enrichedFragment
		if err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> domain research failed: %v", err))
		} else {
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
					string(models.DependencyTypeAffects):
					// ok
				default:
					rt = string(models.DependencyTypeFocusesOn)
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
			job.State["domainResearch"] = map[string]interface{}{
				"relationsN":    len(newRelations),
				"supplyChainsN": len(newChains),
			}
			if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
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
	if existingFragment == nil || strings.TrimSpace(existingFragment.ID) == "" {
		id, err := e.fragmentsRepo.Create(ctx, frag)
		if err != nil {
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

	// Facet-driven relations (deterministic; best-effort).
	// - geo -> GEO_REGION via IN_GEO_SCOPE (country-level /loc/{cc}/na/na)
	// - material/application/topic/subject -> PRODUCT via ABOUT (flat /product/{slug})
	if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
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
					loc := "/loc/" + cc + "/na/na"
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
				cp := "/product/" + slug
				if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
					// /product/{slug} is valid for some paths but if validator rejects due to taxonomy segments,
					// still allow it (ScoutEngine has a canonicalizer for flat products).
					// Keep this conservative: only allow flat /product/{slug}.
					if strings.Count(strings.Trim(cp, "/"), "/") != 1 {
						continue
					}
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
		rels = append(rels, domainResearchRelations...)
	}

	// Persist tracker state into job.State before policy check.
	if tr := tracking.FromContext(ctx); tr != nil {
		job.State = tr.ApplyToState(job.State)
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	// Persist finalized job outputs into LensArtifacts (Phase 2 input).
	e.persistLensArtifactsFromJobState(ctx, job)
	job.State["domainExpansion"] = map[string]interface{}{
		"canonicalPath":       domainCP,
		"title":               title,
		"firstOrderEntitiesN": len(firstOrder),
		"relatedDomainsN":     len(related),
		"affectedDomainsN":    len(affected),
		"relationsEmittedN":   len(rels),
	}
	if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
		logGroundingErrorWithContext(job, "scout", "domain_expansion.persist_state", "job_state_update_failed", "repo_update_error", true, err)
		e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.persist_state", "job_state_update_failed", "repo_update_error", true)
	}

	if !inlineDomainContractRun {
		if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "domain_expansion_done"); err != nil {
			logGroundingErrorWithContext(job, "scout", "domain_expansion.transition_verified", "job_transition_failed", "state_transition_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.transition_verified", "job_transition_failed", "state_transition_error", true)
		}

		// Realtime: job completed.
		e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
			"tenantId":      job.TenantID,
			"projectId":     job.ProjectID,
			"lensId":        strings.TrimSpace(job.LensID),
			"jobId":         job.ID,
			"fragmentId":    fragmentID,
			"canonicalPath": domainCP,
			"confidence":    1.0,
			"hint":          "base_enrichment",
			"legacyHint":    "domain_expansion",
		})
	}

	// Decomposition: schedule first-order entity scouts.
	if len(rels) > 0 {
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
			}
		} else {
			if err := e.processDiscoveredRelations(ctx, job, frag, rels); err != nil {
				logGroundingErrorWithContext(job, "scout", "domain_expansion.relations_fallback", "relations_materialization_failed", "materialization_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.relations_fallback", "relations_materialization_failed", "materialization_error", true)
			}
		}
	}
	if len(domainResearchChains) > 0 {
		if err := e.processSupplyChains(ctx, job, frag, domainResearchChains); err != nil {
			logGroundingErrorWithContext(job, "scout", "domain_expansion.supply_chains", "supply_chain_processing_failed", "materialization_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_expansion.supply_chains", "supply_chain_processing_failed", "materialization_error", true)
		}
	}

	// Policy audit + optional enforcement (best-effort).
	if !inlineDomainContractRun {
		e.runPolicyAudit(ctx, job)
	}

	// Best-effort propagation trigger for the domain node.
	if e.propagationTrigger != nil {
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
	if !skipAutoStrategicOverlay && e.shouldRunStrategicOverlay(contractType, len(firstOrder), len(rels)) {
		e.triggerPhase2StrategicOverlay(ctx, job, domainCP)
	}

	// Check if all scout jobs for this lens are completed.
	if !inlineDomainContractRun {
		e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
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
	if domainCP == "" || !strings.HasPrefix(domainCP, "/domain/") {
		return nil
	}

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
	if existingFragment == nil || strings.TrimSpace(existingFragment.ID) == "" {
		id, err := e.fragmentsRepo.Create(ctx, frag)
		if err != nil {
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
	overlayResult, overlayEvidence, err := overlayAgent.ExtractWithGrounding(ctx, frag, graphContext, focus, keywords)
	if err != nil {
		if !inlineDomainContractRun {
			if trErr := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_failed"); trErr != nil {
				logGroundingErrorWithContext(job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true, trErr)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.transition_failed", "job_transition_failed", "state_transition_error", true)
			}
		}
		return err
	}
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
			// Realtime: update graph edges (this is what Cockpit subscribes to).
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":            job.TenantID,
				"projectId":           job.ProjectID,
				"lensId":              strings.TrimSpace(job.LensID),
				"edgeId":              surfaceEdge.ID,
				"sourceFragmentId":    surfaceEdge.SourceFragmentID,
				"targetFragmentId":    surfaceEdge.TargetFragmentID,
				"sourceCanonicalPath": domainCP,
				"targetCanonicalPath": path,
				"type":                string(surfaceEdge.Type),
				"properties":          surfaceEdge.Properties,
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
	for sourcePath, rels := range relationsBySource {
		ft := sourceTypeByPath[sourcePath]
		sourceFrag, err := e.getOrCreateFragmentByPath(ctx, sourcePath, ft, "", "", "strategic_overlay", job.ID)
		if err != nil || sourceFrag == nil {
			continue
		}
		if err := e.processRelationsInternal(ctx, job, sourceFrag, rels, "strategic_overlay", map[string]interface{}{"source": "strategic_overlay", "domainOverlay": true}, nil); err == nil {
			edgesEmitted += len(rels)
		}
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
	e.persistLensArtifactsFromJobState(ctx, job)
	job.State["domainOverlay"] = map[string]interface{}{
		"canonicalPath":    domainCP,
		"title":            title,
		"decisions":        decisionCount,
		"mechanisms":       mechanismCount,
		"surfacesEdges":    surfacesCount,
		"relationsEmitted": edgesEmitted,
	}
	if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
		logGroundingErrorWithContext(job, "scout", "domain_overlay.persist_state", "job_state_update_failed", "repo_update_error", true, err)
		e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.persist_state", "job_state_update_failed", "repo_update_error", true)
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
				}
			}
		}
	}

	if !inlineDomainContractRun {
		if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "domain_overlay_done"); err != nil {
			logGroundingErrorWithContext(job, "scout", "domain_overlay.transition_verified", "job_transition_failed", "state_transition_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "domain_overlay.transition_verified", "job_transition_failed", "state_transition_error", true)
		}
	}
	if !inlineDomainContractRun {
		e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
			"tenantId":      job.TenantID,
			"projectId":     job.ProjectID,
			"lensId":        strings.TrimSpace(job.LensID),
			"jobId":         job.ID,
			"fragmentId":    fragmentID,
			"canonicalPath": domainCP,
			"confidence":    1.0,
			"hint":          "strategic_projection",
			"legacyHint":    "domain_overlay",
		})
	}
	if !inlineDomainContractRun {
		e.runPolicyAudit(ctx, job)
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
	if !inlineDomainContractRun {
		e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
	}
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
		return nil
	}

	// Guardrails for recursive scouting:
	// - Depth is controlled by recursionDepth/maxRecursionDepth.
	// - Breadth is capped per parent job to avoid runaway fanout (LLM can emit many relations).
	// - Domain-derived jobs use lower cap to control cost explosion.
	// - We deduplicate targets within this job execution to avoid enqueueing the same target multiple times.
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
	if maxChildren < 0 {
		maxChildren = 0
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

	// Specialties (optional): used to enable/disable extra normalization rules safely.
	// NOTE: We read specialties early so shouldRecurse can use them for supply_chain mode filtering.
	specialties := []string(nil)
	if job != nil && job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}
	isSupplyChainMode := taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain)
	isEquityMode := taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyEquity)
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
		if sourceFragment != nil && isSoftDeniedRelationForRecurse(sourceFragment.Type, targetType, sourceFragment.CanonicalPath, targetPath, rt) {
			return false
		}
		if conf > 0 && conf < 0.55 {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(targetPath), "/sphere/") || targetType == models.FragmentTypeSphere {
			return false
		}

		// Supply chain mode: strict filtering to only follow supply-chain edges
		if isSupplyChainMode {
			switch rt {
			case string(models.DependencyTypeSuppliesTo),
				string(models.DependencyTypeDependsOn),
				string(models.DependencyTypeProduces),
				string(models.DependencyTypeConsumes),
				string(models.DependencyTypeTransformsInto),
				string(models.DependencyTypeExtractsFrom):
				return true
			default:
				// In supply chain mode, skip non-supply-chain edges to stay focused
				return false
			}
		}

		// Equity mode: follow only ownership/control edges
		if isEquityMode {
			switch rt {
			case string(models.DependencyTypeOwns),
				string(models.DependencyTypeSubsidiaryOf),
				string(models.DependencyTypeControls),
				string(models.DependencyTypeInvestsIn):
				return true
			default:
				return false
			}
		}

		// General mode: exclude known noisy edge types
		switch rt {
		case string(models.DependencyTypeCompetesWith),
			string(models.DependencyTypeInfluences),
			// OSINT-lite / social edges can explode recursion breadth; keep nodes/edges but don't auto-expand.
			string(models.DependencyTypeRelatedTo),
			string(models.DependencyTypeRelativeOf),
			// Auditors are a governance fact; not useful to recursively expand by default.
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

	for _, rel := range relations {
		if rel.TargetName == "" {
			continue
		}

		// Normalize relation type early (runtime taxonomy allowlist) to avoid creating edges with unsupported types.
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

		// 1. Determine target canonical path
		targetPath, err := e.resolveCanonicalPath(ctx, rel)
		if err != nil {
			logGroundingErrorWithContext(job, "scout", "relations.resolve_canonical_path", "canonicalization_failed", "canonicalization_error", true, err)
			e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.resolve_canonical_path", "canonicalization_failed", "canonicalization_error", true)
			continue
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
			// Fragment doesn't exist -> Create minimal fragment (low confidence) and schedule scouting to fill it.
			// We do NOT use /discovered paths; canonicalPath must already be stable here.
			ft := e.normalizeFragmentType(ctx, rel.TargetType)
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
				relationsCreatedNew++
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

			// Daily budget guardrails (depth limiting, not breadth):
			// If we've exceeded cascade or lens daily limits, stop expanding deeper.
			if e.budgetPolicy.Enabled && e.spendLedger != nil && job != nil {
				lensID := ""
				if job.State != nil {
					if v, ok := job.State["lensId"].(string); ok {
						lensID = strings.TrimSpace(v)
					}
				}
				cascadeID := ""
				if job.State != nil {
					if v, ok := job.State["cascadeRootJobId"].(string); ok {
						cascadeID = strings.TrimSpace(v)
					}
				}
				if cascadeID == "" {
					cascadeID = job.ID
				}
				if lensID != "" && cascadeID != "" {
					day := time.Now().UTC().Format("2006-01-02")
					if cSpend, _, cOut, err := e.spendLedger.GetCascadeSpendUSD(ctx, job.TenantID, cascadeID, day); err == nil {
						if e.budgetPolicy.CascadeDailyMaxUSD > 0 && cSpend >= e.budgetPolicy.CascadeDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.CascadeDailyMaxOutputTokens > 0 && cOut >= e.budgetPolicy.CascadeDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
					if dSpend, _, dOut, err := e.spendLedger.GetLensDaySpendUSD(ctx, job.TenantID, lensID, day); err == nil {
						if e.budgetPolicy.LensDailyMaxUSD > 0 && dSpend >= e.budgetPolicy.LensDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.LensDailyMaxOutputTokens > 0 && dOut >= e.budgetPolicy.LensDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
				}
			}

			// Ограничиваем глубину
			if currentDepth < maxDepth {
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

					// Supply-chain cascade visited set: prevent enqueueing same targetPath multiple times inside this cascade.
					// This is a guardrail against cycles/avalanche fanout; enabled only in supply_chain mode.
					if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
						already, vErr := e.cascadeVisitedRepo.TryMarkVisited(
							ctx,
							job.TenantID,
							job.ProjectID,
							cascadeID,
							targetPath,
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
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.skip", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
								"depth":     currentDepth + 1,
							})
							continue
						} else {
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.mark", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
								"depth":     currentDepth + 1,
							})
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
					// Supply-chain cascade visited set: prevent enqueueing same targetPath multiple times inside this cascade.
					if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
						already, vErr := e.cascadeVisitedRepo.TryMarkVisited(
							ctx,
							job.TenantID,
							job.ProjectID,
							cascadeID,
							targetPath,
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
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.skip", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
								"depth":     currentDepth + 1,
							})
							continue
						}
					}

					if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
						logGroundingErrorWithContext(job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true, err)
						e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true)
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
							if delErr := e.cascadeVisitedRepo.DeleteVisited(ctx, job.TenantID, cascadeID, targetPath); delErr != nil {
								logGroundingErrorWithContext(job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true, delErr)
								e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true)
							}
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.unmark_on_create_fail", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
							})
						}
					} else {
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

			// Daily budget guardrails (depth limiting, not breadth)
			if e.budgetPolicy.Enabled && e.spendLedger != nil && job != nil {
				lensID := ""
				if job.State != nil {
					if v, ok := job.State["lensId"].(string); ok {
						lensID = strings.TrimSpace(v)
					}
				}
				cascadeID := ""
				if job.State != nil {
					if v, ok := job.State["cascadeRootJobId"].(string); ok {
						cascadeID = strings.TrimSpace(v)
					}
				}
				if cascadeID == "" {
					cascadeID = job.ID
				}
				if lensID != "" && cascadeID != "" {
					day := time.Now().UTC().Format("2006-01-02")
					if cSpend, _, cOut, err := e.spendLedger.GetCascadeSpendUSD(ctx, job.TenantID, cascadeID, day); err == nil {
						if e.budgetPolicy.CascadeDailyMaxUSD > 0 && cSpend >= e.budgetPolicy.CascadeDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.CascadeDailyMaxOutputTokens > 0 && cOut >= e.budgetPolicy.CascadeDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
					if dSpend, _, dOut, err := e.spendLedger.GetLensDaySpendUSD(ctx, job.TenantID, lensID, day); err == nil {
						if e.budgetPolicy.LensDailyMaxUSD > 0 && dSpend >= e.budgetPolicy.LensDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.LensDailyMaxOutputTokens > 0 && dOut >= e.budgetPolicy.LensDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
				}
			}

			if shouldScout && currentDepth < maxDepth {
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
						trace.Log(ctx, "scout_decompose", "child_job.skip.self_reference", map[string]any{
							"tenantId":            strings.TrimSpace(job.TenantID),
							"projectId":           strings.TrimSpace(job.ProjectID),
							"lensId":              strings.TrimSpace(job.LensID),
							"jobId":               strings.TrimSpace(job.ID),
							"sourceCanonicalPath": strings.TrimSpace(sourceFragment.CanonicalPath),
							"targetCanonicalPath": strings.TrimSpace(targetPath),
						})
						fmt.Printf("[ScoutEngine] Skipping self-reference job: %s -> %s. Circular dependency avoided.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}

					// CRITICAL: Skip if target fragment is already verified (regardless of edge existence)
					// This prevents re-scouting entities that were just processed
					if !shouldSkipJob && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
						trace.Log(ctx, "scout_decompose", "child_job.skip.target_verified", map[string]any{
							"tenantId":            strings.TrimSpace(job.TenantID),
							"projectId":           strings.TrimSpace(job.ProjectID),
							"lensId":              strings.TrimSpace(job.LensID),
							"jobId":               strings.TrimSpace(job.ID),
							"targetCanonicalPath": strings.TrimSpace(targetPath),
							"epistemicStatus":     string(targetFragment.Metadata.EpistemicStatus),
						})
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
							trace.Log(ctx, "scout_decompose", "child_job.skip.active_exists", map[string]any{
								"tenantId":            strings.TrimSpace(job.TenantID),
								"projectId":           strings.TrimSpace(job.ProjectID),
								"lensId":              strings.TrimSpace(job.LensID),
								"jobId":               strings.TrimSpace(job.ID),
								"targetCanonicalPath": strings.TrimSpace(targetPath),
								"existingJobId":       strings.TrimSpace(existingJobID),
							})
							fmt.Printf("[ScoutEngine] Active scouting job already exists for %s (jobId=%s). Skipping duplicate.\n",
								targetPath, existingJobID)
							scheduledChildren[targetPath] = struct{}{}
							shouldSkipJob = true
						}
					}
					if !shouldSkipJob && targetIdentityKey != "" {
						if existingJobID, ok := activeJobByIdentity[targetIdentityKey]; ok && strings.TrimSpace(existingJobID) != "" {
							trace.Log(ctx, "scout_decompose", "child_job.skip.active_identity_exists", map[string]any{
								"tenantId":            strings.TrimSpace(job.TenantID),
								"projectId":           strings.TrimSpace(job.ProjectID),
								"lensId":              strings.TrimSpace(job.LensID),
								"jobId":               strings.TrimSpace(job.ID),
								"targetIdentityKey":   targetIdentityKey,
								"targetCanonicalPath": strings.TrimSpace(targetPath),
								"existingJobId":       strings.TrimSpace(existingJobID),
							})
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
					if job != nil && job.State != nil {
						if rm, ok := job.State["requestedMetrics"]; ok {
							childState["requestedMetrics"] = rm
						}
						if v, ok := job.State["specialties"]; ok {
							childState["specialties"] = v
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
						fmt.Printf("Triggered recursive scouting (existing fragment) for: %s\n", targetPath)
					} else {
						logGroundingErrorWithContext(job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true, err)
						e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.enqueue_recursive_job", "recursive_job_create_failed", "repo_write_error", true)
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
							if delErr := e.cascadeVisitedRepo.DeleteVisited(ctx, job.TenantID, cascadeID, targetPath); delErr != nil {
								logGroundingErrorWithContext(job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true, delErr)
								e.emitGroundingErrorTelemetry(ctx, job, "scout", "relations.supply_chain_visited", "visited_unmark_failed", "repo_update_error", true)
							}
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.unmark_on_create_fail", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
							})
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

		// 3. Materialize edge (centralized path).
		// NOTE: This is the unified edge materializer used for both base enrichment and strategic projection.
		// The legacy inline materialization block below is kept temporarily for safe diffing/migration,
		// but is bypassed via continue.
		if matErr := e.materializeRelationEdge(
			ctx,
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
		// Normalize and validate relation types against runtime taxonomy (Firestore-backed).
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

		// In-memory dedup (per job run), after all normalization.
		ek := EdgeKey(edgeSourceID, edgeTargetID, edgeType)
		if _, ok := seenEdgeKeys[ek]; ok {
			fmt.Printf("Skipping duplicate edge (in-memory): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}
		seenEdgeKeys[ek] = struct{}{}

		// Firestore dedup check (cross-run).
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edgeSourceID, edgeTargetID, edgeType); err == nil {
			fmt.Printf("Skipping duplicate edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
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
		if len(rel.Qualifiers) > 0 {
			edge.Qualifiers = mergeQualifiers(edge.Qualifiers, rel.Qualifiers)
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
		applyBestEffortEdgeEnrichment(edge)

		// Check if edge already exists to prevent duplicates (idempotency)
		if e.edgesRepo != nil {
			if existing, _ := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edge.SourceFragmentID, edge.TargetFragmentID, edge.Type); existing != nil {
				// Edge exists: update properties/qualifiers instead of creating new
				// Or skip if identical. For now, we skip to be safe and simple.
				// In future, we might want to merge confidence or update timestamps.
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
		enrichedFrag, newRelations, _, evidence, err := e.researchAgent.EnrichFragmentWithGrounding(
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
				_ = e.missingInfoStatusRepo.RecordAttempt(ctx, strings.TrimSpace(frag.ID), q, foundSomething, 0)
			}
		}
	}

	return enriched, addedEdges
}

// NOTE: legacy sanitizePathName removed.

// normalizeRelationEdge enforces directional semantics for common relation types.
// It returns possibly swapped source/target IDs and/or adjusted edge type.
// This is intentionally conservative: if unsure, it leaves the relation as-is.
func normalizeRelationEdge(sourceFrag *models.HorizonFragment, sourceID, targetID string, edgeType models.DependencyType, targetType models.FragmentType, enableManagement bool) (string, string, models.DependencyType, bool) {
	if sourceFrag == nil {
		return sourceID, targetID, edgeType, false
	}
	srcType := sourceFrag.Type
	isGeo := func(t models.FragmentType) bool { return t == models.FragmentTypeGeoRegion }
	isOrg := func(t models.FragmentType) bool { return t == models.FragmentTypeOrganization }
	isFac := func(t models.FragmentType) bool { return t == models.FragmentTypeFacility }
	isProd := func(t models.FragmentType) bool { return t == models.FragmentTypeProduct }
	isPerson := func(t models.FragmentType) bool { return t == models.FragmentTypePerson }

	changed := false

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
		if !isOrg(srcType) && !isPerson(srcType) && (isOrg(targetType) || isPerson(targetType)) && !isGeo(srcType) {
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

	parser := NewSupplyChainParser()
	result, err := parser.ParseSupplyChains(chains, sourceFragment.CanonicalPath)
	if err != nil {
		msg := fmt.Sprintf("[SupplyChainParse] failed to parse supplyChains: %v", err)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "warn", msg)
		return fmt.Errorf("failed to parse supply chains: %w", err)
	}

	lensID := ""
	if job.State != nil {
		if v, ok := job.State["lensId"]; ok {
			lensID, _ = v.(string)
		}
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
	if sourceFragment != nil && sourceFragment.ID != "" {
		pathToID[sourceFragment.CanonicalPath] = sourceFragment.ID
	}
	// Track canonical-path rewrites within this supply-chain run so edge endpoints remain consistent
	// even when we normalize (legacy geo -> /loc, /loc country aliases, product taxonomy upgrades, etc.).
	pathRewrites := map[string]string{} // originalPath -> canonicalPath

	// Persist fragments discovered in supply chains
	createdFrags := 0
	updatedFrags := 0
	createErrors := 0
	previewLimit := 20
	for canonicalPath, frag := range result.Fragments {
		origPath := strings.TrimSpace(canonicalPath)
		effectivePath := origPath
		if frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
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
		if e.preCreateValidator != nil {
			if pre, perr := e.preCreateValidator.ValidateAndProcess(ctx, frag); perr == nil && pre != nil {
				if pre.ReplacementFrag != nil && strings.TrimSpace(pre.ReplacementFrag.ID) != "" {
					cp := strings.TrimSpace(pre.ReplacementFrag.CanonicalPath)
					if cp != "" {
						pathToID[cp] = strings.TrimSpace(pre.ReplacementFrag.ID)
						if origPath != "" {
							pathRewrites[origPath] = cp
						}
						if effectivePath != "" {
							pathRewrites[effectivePath] = cp
						}
					}
					continue
				}
				if !pre.IsValid && strings.TrimSpace(pre.BlockReason) != "" {
					msg := fmt.Sprintf("[SupplyChainFragmentCreate] BLOCKED canonicalPath=%s reason=%s", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(pre.BlockReason))
					fmt.Printf("%s\n", msg)
					e.logLine(ctx, job, "warn", msg)
					continue
				}
			}
		}

		// Check if fragment already exists
		existing, err := e.fragmentsRepo.GetByCanonicalPath(ctx, frag.CanonicalPath)
		if errors.Is(err, horizonrepo.ErrFragmentNotFound) {
			// Create new fragment
			frag.Metadata.CreatedAt = time.Now().UTC()
			frag.Metadata.LastUpdatedAt = time.Now().UTC()
			frag.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence

			id, createErr := e.fragmentsRepo.Create(ctx, frag)
			if createErr != nil {
				createErrors++
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] FAILED canonicalPath=%s err=%v", frag.CanonicalPath, createErr)
				fmt.Printf("%s\n", msg)
				e.logLine(ctx, job, "warn", msg)
				continue
			}
			frag.ID = id
			pathToID[frag.CanonicalPath] = id
			e.ensureOrgGeoFallbackLink(ctx, frag, "scout_supply_chain", job.ID)
			createdFrags++
			if createdFrags <= previewLimit {
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] OK canonicalPath=%s id=%s type=%s", frag.CanonicalPath, id, frag.Type)
				e.logLine(ctx, job, "info", msg)
			}
		} else if err == nil && existing != nil {
			// Fragment exists, store ID for edge creation
			pathToID[frag.CanonicalPath] = existing.ID
			e.ensureOrgGeoFallbackLink(ctx, existing, "scout_supply_chain", job.ID)

			// Optionally update labels/identifiers if new info
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
				if err := e.fragmentsRepo.Update(ctx, existing); err != nil {
					msg := fmt.Sprintf("[SupplyChainFragmentUpdate] FAILED canonicalPath=%s id=%s err=%v", frag.CanonicalPath, existing.ID, err)
					fmt.Printf("%s\n", msg)
					e.logLine(ctx, job, "warn", msg)
				} else {
					updatedFrags++
					if updatedFrags <= previewLimit {
						msg := fmt.Sprintf("[SupplyChainFragmentUpdate] OK canonicalPath=%s id=%s", frag.CanonicalPath, existing.ID)
						e.logLine(ctx, job, "info", msg)
					}
				}
			}
		} else if err != nil {
			createErrors++
			msg := fmt.Sprintf("[SupplyChainFragmentLookup] FAILED canonicalPath=%s err=%v", frag.CanonicalPath, err)
			fmt.Printf("%s\n", msg)
			e.logLine(ctx, job, "warn", msg)
		}
	}

	// Create edges using fragment IDs
	provenanceMethod := "scout_supply_chain"
	createdEdges := 0
	edgeDupes := 0
	edgeCreateErrors := 0
	edgeReversed := 0
	edgePenalized := 0
	edgePreviewLimit := 40
	for _, chainEdge := range result.Edges {
		srcPath := strings.TrimSpace(chainEdge.SourceCanonicalPath)
		dstPath := strings.TrimSpace(chainEdge.TargetCanonicalPath)
		if rp, ok := pathRewrites[srcPath]; ok && strings.TrimSpace(rp) != "" {
			srcPath = strings.TrimSpace(rp)
		}
		if rp, ok := pathRewrites[dstPath]; ok && strings.TrimSpace(rp) != "" {
			dstPath = strings.TrimSpace(rp)
		}

		sourceID := pathToID[srcPath]
		targetID := pathToID[dstPath]

		if sourceID == "" || targetID == "" {
			// Try to look up missing IDs
			if sourceID == "" {
				// Best-effort: resolve canonical path for endpoints not seen in fragment map.
				if e.canonicalPathResolver != nil && srcPath != "" {
					if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, srcPath, InferFragmentTypeFromCanonicalPath(srcPath)); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
						if strings.TrimSpace(resolved.CanonicalPath) != srcPath {
							pathRewrites[srcPath] = strings.TrimSpace(resolved.CanonicalPath)
							srcPath = strings.TrimSpace(resolved.CanonicalPath)
						}
						if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
							sourceID = strings.TrimSpace(resolved.ExistingFragment.ID)
							pathToID[srcPath] = sourceID
						}
					}
				}
				if sourceID == "" {
					if frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, srcPath); err == nil && frag != nil {
						sourceID = frag.ID
						pathToID[srcPath] = sourceID
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
						}
					}
				}
				if targetID == "" {
					if frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, dstPath); err == nil && frag != nil {
						targetID = frag.ID
						pathToID[dstPath] = targetID
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

		edgeType := models.DependencyType(chainEdge.RelationType)

		// Policy: self-loop prevention
		if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(targetID) == "" || IsSelfLoop(sourceID, targetID) {
			continue
		}

		// Resolve endpoint types for policy validation (best-effort).
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
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); ferr == nil && f != nil {
					return resolveFromPath(cp, f.Type)
				}
			}
			return resolveFromPath(cp, "")
		}
		srcTypeActual := resolveTypeByCanonical(srcPath)
		dstTypeActual := resolveTypeByCanonical(dstPath)

		// Policy: direction validation & reversal (best-effort, same behavior as processDiscoveredRelations).
		edgeCriticality := normalizeEdgeConfidence(chainEdge.Confidence)
		directionChanged := false
		policyStatus := models.PolicyStatusClean
		policyReason := ""
		policyPenalty := 0.0
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
					"sourceFragmentID":    sourceID,
					"targetFragmentID":    targetID,
					"sourceType":          string(srcTypeActual),
					"targetType":          string(dstTypeActual),
					"sourceCanonicalPath": srcPath,
					"targetCanonicalPath": dstPath,
					"relationType":        string(edgeType),
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

		// Check for existing edge
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, edgeType); err == nil {
			// Edge already exists, skip
			edgeDupes++
			continue
		}

		qualifiers := map[string]interface{}{
			"reason":              chainEdge.Reason,
			"sourceCanonicalPath": chainEdge.SourceCanonicalPath,
			"targetCanonicalPath": chainEdge.TargetCanonicalPath,
			"rawConfidence":       chainEdge.Confidence,
			"fromSupplyChain":     true,
		}
		if chainEdge.Qualifiers != nil {
			for k, v := range chainEdge.Qualifiers {
				if k != "" && v != nil {
					qualifiers[k] = v
				}
			}
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
		applyBestEffortEdgeEnrichment(edge)

		if id, createErr := e.edgesRepo.Create(ctx, edge); createErr != nil {
			edgeCreateErrors++
			msg := fmt.Sprintf("[SupplyChainEdgeCreate] FAILED %s -[%s]-> %s err=%v",
				chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, createErr)
			fmt.Printf("%s\n", msg)
			e.logLine(ctx, job, "warn", msg)
		} else {
			edge.ID = id
			createdEdges++
			if createdEdges <= edgePreviewLimit {
				e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainEdgeCreate] OK %s -[%s]-> %s id=%s crit=%.3f raw=%.3f",
					chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, id, edge.Properties.Criticality, chainEdge.Confidence))
			}

			// Realtime event
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":         job.TenantID,
				"projectId":        job.ProjectID,
				"lensId":           lensID,
				"edgeId":           edge.ID,
				"sourceFragmentId": edge.SourceFragmentID,
				"targetFragmentId": edge.TargetFragmentID,
				"type":             string(edge.Type),
				"properties":       edge.Properties,
				"fromSupplyChain":  true,
			})
		}
	}

	// Materialize SupplyLink fragments from parsed segments so PathBuilder can traverse
	// SupplyLink provenance: Scout/Research supply chains → parser → SupplyLink fragments
	supplyLinksCreated := 0
	supplyLinksSkipped := 0
	for _, seg := range result.SupplyLinkSegments {
		supplier := strings.TrimSpace(seg.SupplierRef)
		product := strings.TrimSpace(seg.ProductRef)
		customer := strings.TrimSpace(seg.CustomerRef)
		if supplier == "" || product == "" || customer == "" {
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
		slFrag := &models.HorizonFragment{
			Type:              models.FragmentTypeSupplyLink,
			TenantID:          "global",
			SupplierRef:       &supplierRef,
			ProductRef:        &productRef,
			CustomerRef:       &customerRef,
			TransportViaRefs:  seg.TransportViaRefs,
			Status:            "active",
			Governance:        models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
			Provenance:        models.Provenance{Method: "scout_supply_chain", SourceIDs: []string{job.ID}},
			Metadata:          models.FragmentMetadata{CreatedAt: time.Now().UTC(), LastUpdatedAt: time.Now().UTC(), EpistemicStatus: models.EpistemicStatusLowConfidence},
		}
		slFrag.State = map[string]models.MetricValue{"confidence": {Value: seg.Confidence, Confidence: 0.8}}
		if err := slFrag.Validate(); err != nil {
			supplyLinksSkipped++
			continue
		}
		// Dedup: SupplyLink canonicalPath is deterministic (tenant:supplier:product:customer).
		// GetByCanonicalPath gives reliable lookup by unique key, unlike FindSupplyLinksByCustomer(limit 100).
		if existing, _ := e.fragmentsRepo.GetByCanonicalPath(ctx, slFrag.CanonicalPath); existing != nil && strings.TrimSpace(existing.ID) != "" {
			supplyLinksSkipped++
			continue
		}
		if _, createErr := e.fragmentsRepo.Create(ctx, slFrag); createErr != nil {
			supplyLinksSkipped++
		} else {
			supplyLinksCreated++
		}
	}

	if supplyLinksCreated > 0 || supplyLinksSkipped > 0 {
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyLinkMaterialize] created=%d skipped=%d", supplyLinksCreated, supplyLinksSkipped))
	}

	// Update chain context in job state for propagation to child jobs
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	existingContext, _ := ParseChainContext(job.State["chainContext"])
	existingContext.MergeFromParsedResult(result)

	// Identify gaps: nodes that have only incoming or only outgoing edges
	gapHints := e.identifyChainGaps(result, existingContext)
	existingContext.GapHints = gapHints

	if contextJSON, err := existingContext.ToJSON(); err == nil {
		if job.State == nil {
			job.State = make(map[string]interface{})
		}
		job.State["chainContext"] = contextJSON
		// Update job state in repository
		if e.scoutingJobsRepo != nil {
			if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
				logGroundingErrorWithContext(job, "scout", "supply_chain.persist_chain_context", "job_state_update_failed", "repo_update_error", true, err)
				e.emitGroundingErrorTelemetry(ctx, job, "scout", "supply_chain.persist_chain_context", "job_state_update_failed", "repo_update_error", true)
			}
		}
	}

	// In supply_chain mode, schedule follow-up scout jobs based on supply chain nodes/gaps,
	// not only on plain relations. This enables branching chain expansion even when LLM outputs
	// most structure via supplyChains.
	_ = e.scheduleSupplyChainChildJobs(ctx, job, sourceFragment, result, existingContext)

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
			"createdFragments":   createdFrags,
			"updatedFragments":   updatedFrags,
			"fragmentErrors":     createErrors,
			"createdEdges":       createdEdges,
			"edgeDupes":          edgeDupes,
			"edgeErrors":         edgeCreateErrors,
			"edgeReversed":       edgeReversed,
			"edgePenalized":      edgePenalized,
			"supplyLinksCreated": supplyLinksCreated,
			"supplyLinksSkipped": supplyLinksSkipped,
		},
	})
	{
		msg := fmt.Sprintf("[SupplyChainSummary] fragments(created=%d updated=%d err=%d) edges(created=%d dupes=%d err=%d reversed=%d penalized=%d) supplyLinks(created=%d skipped=%d) gaps=%d",
			createdFrags, updatedFrags, createErrors, createdEdges, edgeDupes, edgeCreateErrors, edgeReversed, edgePenalized, supplyLinksCreated, supplyLinksSkipped, len(gapHints))
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
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
// (and especially gap nodes). This is supply_chain-only and does not affect general mode.
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

	specialties := []string(nil)
	if v, ok := job.State["specialties"]; ok {
		specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
	}
	if !taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain) {
		return nil
	}

	// Depth guardrails
	currentDepth := parseStateInt(job.State["recursionDepth"], 0)
	maxDepth := parseStateInt(job.State["maxRecursionDepth"], 2)
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
	if maxChildren < 0 {
		maxChildren = 0
	}
	if maxChildren == 0 {
		return nil
	}

	// Best-effort: prefetch active (pending/running) scout jobs for this project/lens
	activeJobByPath := map[string]string{}     // canonicalPath -> jobId
	activeJobByIdentity := map[string]string{} // targetIdentityKey -> jobId
	{
		lensID := strings.TrimSpace(job.LensID)
		if lensID == "" {
			lensID = parseStateString(job.State["lensId"])
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

	cascadeID := ""
	if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
		cascadeID = strings.TrimSpace(v)
	}
	if cascadeID == "" {
		cascadeID = strings.TrimSpace(job.ID)
	}
	for _, targetPath := range candidates {
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
				trace.Log(ctx, "SupplyChain", "SupplyChain.visited.unmark_on_create_fail", map[string]any{
					"tenantId":  strings.TrimSpace(job.TenantID),
					"projectId": strings.TrimSpace(job.ProjectID),
					"lensId":    strings.TrimSpace(job.LensID),
					"jobId":     strings.TrimSpace(job.ID),
					"cascadeId": strings.TrimSpace(cascadeID),
					"target":    strings.TrimSpace(targetPath),
				})
			}
		}
	}

	if scheduled > 0 {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=%d candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d placeholder=%d) depth=%d/%d cap=%d",
			scheduled, len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, skippedPlaceholder, currentDepth, maxDepth, maxChildren)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
		trace.Log(ctx, "SupplyChain", "SupplyChain.schedule.children", map[string]any{
			"tenantId":         strings.TrimSpace(job.TenantID),
			"projectId":        strings.TrimSpace(job.ProjectID),
			"lensId":           strings.TrimSpace(job.LensID),
			"jobId":            strings.TrimSpace(job.ID),
			"candidatesN":      len(candidates),
			"enqueuedN":        scheduled,
			"skippedActiveN":   skippedActive,
			"skippedVerifiedN": skippedVerified,
			"skippedMissingN":  skippedMissingFrag,
			"skippedDupN":      skippedDup,
			"skippedVisitedN":  skippedVisited,
			"skippedPlaceN":    skippedPlaceholder,
			"depth":            currentDepth,
			"maxDepth":         maxDepth,
			"cap":              maxChildren,
		})
	} else {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=0 candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d placeholder=%d) depth=%d/%d cap=%d",
			len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, skippedPlaceholder, currentDepth, maxDepth, maxChildren)
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
			// Products should have both incoming (producer) and outgoing (consumer) edges
			if incomingCount[node] == 0 {
				hints = append(hints, fmt.Sprintf("Who produces %s? Find upstream supplier.", node))
			}
			if outgoingCount[node] == 0 {
				hints = append(hints, fmt.Sprintf("Who consumes %s? Find downstream customer.", node))
			}
		} else if isActor {
			// Actors at chain ends might need extension
			if incomingCount[node] == 0 && outgoingCount[node] > 0 {
				hints = append(hints, fmt.Sprintf("Who supplies to %s? Find upstream supplier.", node))
			}
			if outgoingCount[node] == 0 && incomingCount[node] > 0 {
				hints = append(hints, fmt.Sprintf("Who does %s supply to? Find downstream customer.", node))
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
func (e *ScoutEngine) shouldRunStrategicOverlay(contractType string, entityCount, relationCount int) bool {
	// Must have at least some entities or relations to analyze
	if entityCount == 0 && relationCount == 0 {
		return false
	}

	// Contract types that benefit from strategic overlay analysis
	// (power dynamics, risk assessment, competitive analysis, etc.)
	strategicContractTypes := map[string]bool{
		// Risk / geopolitics
		"risk-assessment":     true,
		"geopolitical-risk":   true,
		"bilateral-relations": true,
		"sanctions-exposure":  true,
		"conflict-dynamics":   true,
		// Supply chain / value chain (control points matter)
		"supply-chain":    true,
		"value-chain":     true,
		"logistics-flow":  true,
		"trade-flow":      true,
		"sourcing-map":    true,
		"procurement-map": true,
		// Markets / competitive
		"competitive-landscape": true,
		"market-entry":          true,
		// Investment / finance
		"investment-thesis":  true,
		"portfolio-exposure": true,
		// Technology / innovation
		"technology-landscape": true,
		"innovation-map":       true,
		"ip-landscape":         true,
		// Regulatory
		"regulatory-landscape":    true,
		"compliance-map":          true,
		"policy-and-regulation":   true,
		"regulatory-impact":       true,
		"compliance-landscape":    true,
		"security-and-compliance": true,
		"licensing-requirements":  true,
		"esg-landscape":           true,
		// Ecosystem mapping (general)
		"ecosystem-mapping": true,
		// Power dynamics (explicit)
		"power-dynamics":   true,
		"influence-map":    true,
		"stakeholder-map":  true,
		"decision-mapping": true,
	}

	ct := strings.ToLower(strings.TrimSpace(contractType))
	if strategicContractTypes[ct] {
		return true
	}
	// Default fallback: best-effort strategic overlay for unknown/other contracts.
	// This avoids surprises when new contract types are introduced.
	return true
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
