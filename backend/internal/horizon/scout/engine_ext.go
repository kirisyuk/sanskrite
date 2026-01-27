package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
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

func lastCanonicalSegment(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return ""
	}
	parts := strings.Split(cp, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(parts[i])
		if seg != "" {
			return seg
		}
	}
	return ""
}

func bestEffortDisplayLabel(label string, canonicalPath string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = lastCanonicalSegment(canonicalPath)
	}
	return bestEffortTitleCase(label)
}

func isPlaceholderLabel(label string) bool {
	l := strings.TrimSpace(label)
	if l == "" {
		return true
	}
	if len(l) <= 2 {
		return true
	}
	if strings.EqualFold(l, "na") || strings.EqualFold(l, "n/a") || strings.EqualFold(l, "n.a.") {
		return true
	}
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, l)
	return cleaned == "na"
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
		if strings.TrimSpace(label) != "" && strings.TrimSpace(frag.Labels["en"]) == "" {
			frag.Labels["en"] = strings.TrimSpace(label)
			updated = true
		}
		if strings.TrimSpace(description) != "" && strings.TrimSpace(frag.Description) == "" {
			frag.Description = strings.TrimSpace(description)
			updated = true
		}
		if updated {
			_ = e.fragmentsRepo.Update(ctx, frag)
		}
		return frag, nil
	}
	if fragType == "" {
		fragType = InferFragmentTypeFromCanonicalPath(cp)
	}
	if strings.TrimSpace(label) == "" {
		label = bestEffortDisplayLabel("", cp)
	}
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
	return newFrag, nil
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
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "scout_discovery", map[string]interface{}{"source": "scout"})
}

// processHypothesisRelations materializes relations produced from intent decomposition.
// These are hypotheses (not grounded claims), so we mark provenance/qualifiers accordingly.
func (e *ScoutEngine) processHypothesisRelations(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
) error {
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "intent_decomposition", map[string]interface{}{"source": "intent_decomposition", "hypothesis": true})
}

type domainContractForExpansion struct {
	CanonicalPath      string
	ContractType       string
	Title              string
	Description        string
	Facets             map[string][]string
	FirstOrderEntities []string
	RelatedDomains     []string
	AffectedDomains    []string
}

func (e *ScoutEngine) bestEffortFindDomainContract(job *models.ScoutingJob, domainCanonicalPath string) *domainContractForExpansion {
	cp := strings.TrimSpace(domainCanonicalPath)
	if job == nil || job.State == nil || cp == "" {
		return nil
	}
	raw, ok := job.State["domains"]
	if !ok || raw == nil {
		return nil
	}

	// NOTE: job.State may contain:
	// - []interface{} (when loaded from Firestore/JSON)
	// - []agents.DomainResolution (when created in-process from lens.ViewConfig)
	// - []map[string]interface{} (various runtime encodings)

	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			if strings.TrimSpace(d.CanonicalPath) != cp {
				continue
			}
			return &domainContractForExpansion{
				CanonicalPath:      strings.TrimSpace(d.CanonicalPath),
				ContractType:       strings.TrimSpace(d.ContractType),
				Title:              strings.TrimSpace(d.Title),
				Description:        strings.TrimSpace(d.Description),
				Facets:             d.Facets,
				FirstOrderEntities: append([]string(nil), d.FirstOrderEntities...),
				RelatedDomains:     append([]string(nil), d.RelatedDomains...),
				AffectedDomains:    append([]string(nil), d.AffectedDomains...),
			}
		}
		return nil
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
		if strings.TrimSpace(v) != cp {
			continue
		}
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
		return out
	}
	return nil
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

	_ = e.transition(ctx, job, models.ScoutingJobStateExecution, 0.4, "domain_expansion")

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

	// Better default title: use contractType + facets rather than the last path token (which is often just a geo code).
	if title == "" {
		facetsForTitle := parsedFacets
		if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
			facetsForTitle = contract.Facets
		}
		title = buildDomainTitle(contractType, domainCP, facetsForTitle)
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
	if e.researchAgent != nil {
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
		enrichedFragment, newRelations, newChains, err := e.researchAgent.EnrichFragment(ctx, domainFragment, domainMissing, focus, spherePaths, keywords, specialties, "", originalQuery, nil)
		_ = enrichedFragment
		if err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> domain research failed: %v", err))
		} else {
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
			_ = e.scoutingJobsRepo.Update(ctx, job)
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
	if isPlaceholderLabel(frag.Labels["en"]) {
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
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_fragment_create_failed")
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
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_fragment_update_failed")
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
	_ = e.scoutingJobsRepo.Update(ctx, job)

	_ = e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "domain_expansion_done")

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

	// Decomposition: schedule first-order entity scouts.
	if len(rels) > 0 {
		_ = e.processDiscoveredRelations(ctx, job, frag, rels)
	}
	if len(domainResearchChains) > 0 {
		_ = e.processSupplyChains(ctx, job, frag, domainResearchChains)
	}

	// Policy audit + optional enforcement (best-effort).
	e.runPolicyAudit(ctx, job)

	// Best-effort propagation trigger for the domain node.
	if e.propagationTrigger != nil {
		rc, _ := rtctx.Get(ctx)
		go func(fid string) {
			propBase := context.Background()
			if strings.TrimSpace(rc.TenantID) != "" || strings.TrimSpace(rc.ProjectID) != "" || strings.TrimSpace(rc.LensID) != "" || strings.TrimSpace(rc.CorrelationID) != "" || strings.TrimSpace(rc.SessionID) != "" {
				propBase = rtctx.With(propBase, rc)
			}
			propCtx, cancel := context.WithTimeout(propBase, 2*time.Minute)
			defer cancel()
			_ = e.propagationTrigger(propCtx, fid)
		}(fragmentID)
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
	if e.shouldRunStrategicOverlay(contractType, len(firstOrder), len(rels)) {
		e.triggerPhase2StrategicOverlay(ctx, job, domainCP)
	}

	// Check if all scout jobs for this lens are completed.
	e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
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

	_ = e.transition(ctx, job, models.ScoutingJobStateExecution, 0.4, "domain_overlay")

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
	if title == "" {
		facetsForTitle := parsedFacets
		if contract != nil && contract.Facets != nil && len(contract.Facets) > 0 {
			facetsForTitle = contract.Facets
		}
		title = buildDomainTitle(contractType, domainCP, facetsForTitle)
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
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_fragment_create_failed")
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
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_fragment_update_failed")
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
	overlayResult, err := overlayAgent.Extract(ctx, frag, graphContext, focus, keywords)
	if err != nil {
		_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "domain_overlay_failed")
		return err
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
				_ = e.fragmentsRepo.Update(ctx, f)
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
		if cp == "" || taxonomy.ValidateCanonicalPath(cp) != nil {
			continue
		}
		f, err := e.getOrCreateFragmentByPath(ctx, cp, models.FragmentTypeMechanism, mech.Title, mech.Description, "strategic_overlay", job.ID)
		if err == nil && f != nil {
			if f.Type != models.FragmentTypeMechanism {
				f.Type = models.FragmentTypeMechanism
				_ = e.fragmentsRepo.Update(ctx, f)
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
		if err := e.processRelationsInternal(ctx, job, sourceFrag, rels, "strategic_overlay", map[string]interface{}{"source": "strategic_overlay", "domainOverlay": true}); err == nil {
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
	_ = e.scoutingJobsRepo.Update(ctx, job)

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
				_ = e.lensArtifactsRepo.MarkProcessedForStrategic(ctx, artIDs)
			}
		}
	}

	_ = e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "domain_overlay_done")
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
	e.runPolicyAudit(ctx, job)
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
	e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
	return nil
}

func (e *ScoutEngine) processRelationsInternal(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
	provenanceMethod string,
	baseQualifiers map[string]interface{},
) error {
	if len(relations) == 0 {
		return nil
	}

	// Guardrails for recursive scouting:
	// - Depth is controlled by recursionDepth/maxRecursionDepth.
	// - Breadth is capped per parent job to avoid runaway fanout (LLM can emit many relations).
	// - We deduplicate targets within this job execution to avoid enqueueing the same target multiple times.
	maxChildren := 25
	if job != nil && job.State != nil {
		if v, ok := job.State["maxChildrenPerJob"]; ok {
			maxChildren = parseStateInt(v, maxChildren)
		}
	}
	if maxChildren < 0 {
		maxChildren = 0
	}
	scheduledChildren := map[string]struct{}{}
	scheduledCount := 0

	// Best-effort: prefetch active (pending/running) scout jobs for this project/lens,
	// so we can avoid enqueueing duplicate child jobs for the same canonicalPath.
	activeJobByPath := map[string]string{} // canonicalPath -> jobId
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
				string(models.DependencyTypeTransformsInto):
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
			fmt.Printf("[ScoutEngine] Normalized relationType: %q -> %q\n", rawRelationType, normalizedRelationType)
		}
		targetTypeHint := e.normalizeFragmentType(ctx, rel.TargetType)
		targetType := targetTypeHint

		// 1. Determine target canonical path
		targetPath, err := e.resolveCanonicalPath(ctx, rel)
		if err != nil {
			fmt.Printf("Canonicalization failed for relation target=%q: %v\n", rel.TargetName, err)
			continue
		}
		// Align targetType with canonical path prefix (auto-fix mismatches).
		if inferred := InferFragmentTypeFromCanonicalPath(targetPath); inferred != "" && inferred != targetType {
			targetType = inferred
		}

		// 2. Find or Create Target Fragment
		targetFragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		var targetID string

		if err != nil {
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
			if rel.TargetShortName != "" {
				newFragment.Identifiers["shortName"] = rel.TargetShortName
			}

			// Pre-create validation: check policies and deduplication (same as in processScoutResult)
			usedExistingDuplicate := false
			if e.preCreateValidator != nil {
				preValidation, valErr := e.preCreateValidator.ValidateAndProcess(ctx, newFragment)
				if valErr != nil {
					fmt.Printf("[ScoutEngine] Warning: pre-create validation error for %s: %v\n", targetPath, valErr)
				} else if preValidation != nil {
					// Log warnings
					for _, w := range preValidation.Warnings {
						fmt.Printf("[ScoutEngine] Pre-create warning for %s: %s\n", targetPath, w)
					}
					// Check if creation should be blocked
					if !preValidation.IsValid {
						fmt.Printf("[ScoutEngine] Fragment creation blocked for %s: %s\n", targetPath, preValidation.BlockReason)
						continue
					}
					// Check if we should use an existing fragment instead (deduplication)
					if preValidation.ReplacementFrag != nil && preValidation.ReplacementFrag.ID != "" {
						fmt.Printf("[ScoutEngine] Using existing fragment %s instead of creating %s\n", preValidation.ReplacementFrag.ID, targetPath)
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
					fmt.Printf("Failed to create discovered fragment %s: %v\n", targetPath, err)
					continue
				}
				targetID = id
				targetFragment = newFragment
				targetFragment.ID = id
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
							msg := fmt.Sprintf("[SupplyChainVisited] ERROR mark tenant=%s cascade=%s target=%s err=%v",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), vErr)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "warn", msg)
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
							msg := fmt.Sprintf("[SupplyChainVisited] ERROR mark tenant=%s cascade=%s target=%s err=%v",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), vErr)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "warn", msg)
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
						fmt.Printf("Failed to create recursive job for %s: %v\n", targetPath, err)
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
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
					} else {
						fmt.Printf("Triggered recursive scouting for: %s\n", targetPath)
					}
				}
			}

		} else {
			targetID = targetFragment.ID
			if targetFragment != nil && targetFragment.Type != "" {
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
					if !shouldSkipJob && targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
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
						if e.edgesRepo != nil && sourceFragment != nil && sourceFragment.ID != "" && targetFragment != nil && targetFragment.ID != "" {
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

					if shouldSkipJob {
						continue // Skip to next relation
					}

					scheduledChildren[targetPath] = struct{}{}
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
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
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
			}
		}

		// 3. Materialize edge (centralized path).
		// NOTE: This is the unified edge materializer used for both base enrichment and strategic projection.
		// The legacy inline materialization block below is kept temporarily for safe diffing/migration,
		// but is bypassed via continue.
		_ = e.materializeRelationEdge(
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
		)
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
					fmt.Printf("[ScoutEngine] Failed to create explicit source fragment %s: %v\n", explicitSourcePath, createErr)
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
			// Check explicit source fragment first (Phase 2 strategic relations)
			if explicitSourceFragment != nil && strings.TrimSpace(explicitSourceFragment.ID) == id {
				return explicitSourceFragment.Type
			}
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
				return sourceFragment.Type
			}
			if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
				return targetFragment.Type
			}
			// Fallback: best-effort fetch (rare; only when endpoints differ from source/target fragments).
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
					return f.Type
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

		// Special-case canonicalization: REGULATES involving /gov paths.
		// In reality, government bodies (namespace /gov/...) are regulators; companies are regulated.
		// LLMs frequently output inverted direction as ORG -> GOV for REGULATES; fix it best-effort.
		govRegulatesSwapApplied := false
		govRegulatesSwapReason := ""
		if edgeType == models.DependencyTypeRegulates {
			srcPathActual := resolvePathByID(edgeSourceID)
			dstPathActual := resolvePathByID(edgeTargetID)
			if strings.HasPrefix(srcPathActual, "/org/") && strings.HasPrefix(dstPathActual, "/gov/") {
				edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
				srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
				directionChanged = true
				govRegulatesSwapApplied = true
				govRegulatesSwapReason = "canonicalize: REGULATES /org -> /gov swapped to /gov -> /org (gov bodies are regulators)"
				// Re-apply symmetric normalization after any swap (noop for non-symmetric).
				edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
			}
		}

		// Canonicalize certain mis-modeled edges where reversing is not sufficient.
		// Example: PRODUCT OWNS ORGANIZATION should become ORGANIZATION PRODUCES PRODUCT (not ORG OWNS PRODUCT).
		// Use path-aware canonicalization to handle community subtypes (dynasty/tribe).
		srcPathActual := resolvePathByID(edgeSourceID)
		dstPathActual := resolvePathByID(edgeTargetID)
		canonFromEdgeType := ""
		canonReason := ""
		if newType, shouldSwap, changed, reason := CanonicalizeEdgeWithPaths(edgeType, srcTypeActual, dstTypeActual, srcPathActual, dstPathActual); changed {
			canonFromEdgeType = string(edgeType)
			canonReason = reason
			edgeType = newType
			directionChanged = true
			if shouldSwap {
				edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
				srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
				srcPathActual, dstPathActual = dstPathActual, srcPathActual
			}
			// Re-apply symmetric normalization after any swap/type-change (noop for non-symmetric).
			edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
		}

		// Policy: direction validation and reversal (enforced for known edge types).
		// Use unified ValidationPolicy if available, otherwise fallback to hardcoded rules
		if e.validationPolicy != nil && sourceFragment != nil {
			logPrefix := "[ScoutEngine:processRelations]"
			fmt.Printf("%s Using ValidationPolicy for edge validation\n", logPrefix)

			decision, err := e.validationPolicy.Execute(ctx, policy.PolicyInput{
				Kind: "edge",
				Context: map[string]interface{}{
					"validationKind":   "edge",
					"sourceFragmentID": edgeSourceID,
					"targetFragmentID": edgeTargetID,
					"sourceType":       string(srcTypeActual),
					"targetType":       string(dstTypeActual),
					"relationType":     string(edgeType),
				},
				// IMPORTANT: do not pass Fragment here, or ValidationPolicy will overwrite sourceType from it,
				// which can be incorrect after edge normalization swaps.
				Fragment: nil,
			})

			if err != nil {
				fmt.Printf("%s ERROR: ValidationPolicy failed: %v, falling back to hardcoded rules\n", logPrefix, err)
			} else {
				fmt.Printf("%s ValidationPolicy decision: action=%s, path=%s, confidence=%.2f\n",
					logPrefix, decision.Action, decision.DecisionPath, decision.Confidence)

				switch decision.Action {
				case "reject":
					// Don't block - treat as warning/penalty instead
					reason, _ := decision.Params["reason"].(string)
					fmt.Printf("%s WARNING: ValidationPolicy would reject edge (non-blocking): %s\n", logPrefix, reason)
					fmt.Printf("%s Applying penalty instead of blocking - edge will be created with low confidence\n", logPrefix)
					// Apply penalty to edge confidence instead of blocking
					// Continue processing - edge will be created but with lower confidence
				case "reverse":
					shouldReverse, _ := decision.Params["shouldReverse"].(bool)
					if shouldReverse {
						fmt.Printf("%s REVERSING edge direction by ValidationPolicy\n", logPrefix)
						edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
						directionChanged = true
						edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
					}
				case "penalty":
					penalty, _ := decision.Params["penalty"].(float64)
					reason, _ := decision.Params["reason"].(string)
					fmt.Printf("%s PENALTY by ValidationPolicy: %.2f, reason: %s\n", logPrefix, penalty, reason)
					// Penalty will be applied to edge.Properties.Criticality later
				case "allow":
					fmt.Printf("%s ALLOWED by ValidationPolicy\n", logPrefix)
				}
			}
		} else if sourceFragment != nil {
			// Fallback to hardcoded validation
			// Use actual endpoint types (post-normalization).
			st := srcTypeActual
			tt := dstTypeActual
			if st == "" {
				st = sourceFragment.Type
			}
			if tt == "" {
				tt = targetType
			}
			if valid, shouldReverse, rule := ValidateEdgeDirection(edgeType, st, tt); !valid {
				if shouldReverse {
					edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
					directionChanged = true
					// If we reversed, re-apply symmetric normalization (noop for non-symmetric types).
					edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
				} else if rule != nil {
					// Neither direction is valid: reject the edge to keep the graph consistent.
					fmt.Printf("Skipping invalid-direction edge: %s -> %s (%s) rule=%s\n", sourceFragment.CanonicalPath, targetPath, edgeType, rule.Description)
					continue
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
				Criticality:   normalizeEdgeConfidence(rel.Confidence),
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Qualifiers: map[string]interface{}{
				"reason":                 strings.TrimSpace(rel.Reason),
				"rawRelationType":        rawRelationType,
				"normalizedRelationType": normalizedRelationType,
				"rawConfidence":          rel.Confidence,
				"targetName":             strings.TrimSpace(rel.TargetName),
				"targetCanonicalPath":    strings.TrimSpace(resolvedTargetPath),
				"sourceCanonicalPath":    strings.TrimSpace(resolvedSourcePath),
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
		if canonFromEdgeType != "" {
			edge.Qualifiers["canonicalized"] = true
			edge.Qualifiers["canonicalizedFromEdgeType"] = canonFromEdgeType
			edge.Qualifiers["canonicalizationReason"] = canonReason
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
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
		if directionChanged {
			edge.Qualifiers["normalizedDirection"] = true
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
			edge.Qualifiers["normalizedSourceFragmentId"] = edge.SourceFragmentID
			edge.Qualifiers["normalizedTargetFragmentId"] = edge.TargetFragmentID
		}
		if govRegulatesSwapApplied {
			edge.Qualifiers["canonicalizedGovRegulatesSwap"] = true
			edge.Qualifiers["govRegulatesSwapReason"] = govRegulatesSwapReason
		}

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
				rel.Confidence,
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
		enrichedFrag, newRelations, _, err := e.researchAgent.EnrichFragment(
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
		)
		if err != nil {
			continue
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
			if err := e.processRelationsInternal(ctx, job, enrichedFrag, newRelations, "strategic_overlay", map[string]interface{}{"source": "strategic_overlay", "domainOverlay": true}); err == nil {
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
		// Leadership/people relationships are directional and typically originate from PERSON -> ORG.
		// When scouting an ORGANIZATION, LLM output may attach PERSON targets with these types;
		// when the "management" specialty is enabled, we normalize the direction by swapping endpoints.
		if enableManagement && isOrg(srcType) && isPerson(targetType) {
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
		if err != nil {
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
			createdFrags++
			if createdFrags <= previewLimit {
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] OK canonicalPath=%s id=%s type=%s", frag.CanonicalPath, id, frag.Type)
				e.logLine(ctx, job, "info", msg)
			}
		} else {
			// Fragment exists, store ID for edge creation
			pathToID[frag.CanonicalPath] = existing.ID

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
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == cp {
				return sourceFragment.Type
			}
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); ferr == nil && f != nil {
					return f.Type
				}
			}
			return ""
		}
		srcTypeActual := resolveTypeByCanonical(srcPath)
		dstTypeActual := resolveTypeByCanonical(dstPath)

		// Policy: direction validation & reversal (best-effort, same behavior as processDiscoveredRelations).
		edgeCriticality := normalizeEdgeConfidence(chainEdge.Confidence)
		directionChanged := false
		if e.validationPolicy != nil {
			decision, derr := e.validationPolicy.Execute(ctx, policy.PolicyInput{
				Kind: "edge",
				Context: map[string]interface{}{
					"validationKind":   "edge",
					"sourceFragmentID": sourceID,
					"targetFragmentID": targetID,
					"sourceType":       string(srcTypeActual),
					"targetType":       string(dstTypeActual),
					"relationType":     string(edgeType),
				},
				Fragment: nil,
			})
			if derr == nil {
				switch decision.Action {
				case "reject":
					// Non-blocking: apply penalty to criticality to de-prioritize doubtful edges.
					edgeCriticality = edgeCriticality * 0.35
					edgePenalized++
					msg := fmt.Sprintf("[SupplyChainPolicy] PENALTY action=reject edge=%s -[%s]-> %s path=%s conf=%.3f->%.3f",
						chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, decision.DecisionPath, normalizeEdgeConfidence(chainEdge.Confidence), edgeCriticality)
					e.logLine(ctx, job, "warn", msg)
				case "reverse":
					shouldReverse, _ := decision.Params["shouldReverse"].(bool)
					if shouldReverse {
						sourceID, targetID = targetID, sourceID
						directionChanged = true
						edgeReversed++
						msg := fmt.Sprintf("[SupplyChainPolicy] REVERSE edge=%s -[%s]-> %s path=%s",
							chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, decision.DecisionPath)
						e.logLine(ctx, job, "info", msg)
					}
				}
			} else {
				msg := fmt.Sprintf("[SupplyChainPolicy] ERROR validationPolicy err=%v", derr)
				e.logLine(ctx, job, "warn", msg)
			}
		}

		// Check for existing edge
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, edgeType); err == nil {
			// Edge already exists, skip
			edgeDupes++
			continue
		}

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
			Qualifiers: map[string]interface{}{
				"reason":              chainEdge.Reason,
				"sourceCanonicalPath": chainEdge.SourceCanonicalPath,
				"targetCanonicalPath": chainEdge.TargetCanonicalPath,
				"rawConfidence":       chainEdge.Confidence,
				"fromSupplyChain":     true,
			},
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
				fmt.Printf("[ScoutEngine] Failed to update job chain context: %v\n", err)
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
			"createdFragments": createdFrags,
			"updatedFragments": updatedFrags,
			"fragmentErrors":   createErrors,
			"createdEdges":     createdEdges,
			"edgeDupes":        edgeDupes,
			"edgeErrors":       edgeCreateErrors,
			"edgeReversed":     edgeReversed,
			"edgePenalized":    edgePenalized,
		},
	})
	{
		msg := fmt.Sprintf("[SupplyChainSummary] fragments(created=%d updated=%d err=%d) edges(created=%d dupes=%d err=%d reversed=%d penalized=%d) gaps=%d",
			createdFrags, updatedFrags, createErrors, createdEdges, edgeDupes, edgeCreateErrors, edgeReversed, edgePenalized, len(gapHints))
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
	activeJobByPath := map[string]string{} // canonicalPath -> jobId
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
	skippedActive := 0
	skippedMissingFrag := 0
	skippedVerified := 0
	skippedDup := 0
	skippedVisited := 0

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
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=%d candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d) depth=%d/%d cap=%d",
			scheduled, len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, currentDepth, maxDepth, maxChildren)
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
			"depth":            currentDepth,
			"maxDepth":         maxDepth,
			"cap":              maxChildren,
		})
	} else {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=0 candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d) depth=%d/%d cap=%d",
			len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, currentDepth, maxDepth, maxChildren)
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
		"regulatory-landscape": true,
		"compliance-map":       true,
	"policy-and-regulation": true,
	"regulatory-impact":     true,
	"compliance-landscape":  true,
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
		"phase1JobId":         phase1Job.ID,
		"trigger":             "auto.strategic_projection.after_base_enrichment",
		"triggerLegacy":       "auto.phase2.after_domain_expansion",
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
		fmt.Printf("[ScoutEngine] Strategic projection job creation failed for %s: %v\n", domainCP, err)
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
