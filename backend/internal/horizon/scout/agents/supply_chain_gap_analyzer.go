package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ---------------------------------------------------------------------------
// Gap report types
// ---------------------------------------------------------------------------

// SupplyChainGapReport is the output of gap analysis over all chains of an entity.
type SupplyChainGapReport struct {
	Score              float64             `json:"score"`
	Sufficient         bool                `json:"sufficient"`
	TierGaps           []TierGap           `json:"tierGaps,omitempty"`
	ConcentrationRisks []ConcentrationRisk `json:"concentrationRisks,omitempty"`
	MetricGaps         []MetricGap         `json:"metricGaps,omitempty"`
	LogisticsGaps      []LogisticsGap      `json:"logisticsGaps,omitempty"`
	TemporalGaps       []TemporalGap       `json:"temporalGaps,omitempty"`
	DepthGaps          []DepthGap          `json:"depthGaps,omitempty"`
	IndustryGaps       []IndustryGap       `json:"industryGaps,omitempty"`
	RefineDirectives   []RefineDirective   `json:"refineDirectives"`

	// Diagnostics (not serialized to external consumers, used for telemetry).
	llmFailed           bool
	llmError            string
	llmTierGapsAdded    int
	llmIndustryGapsAdded int
}

// TierGap flags a missing transformation stage between two known nodes.
type TierGap struct {
	ChainIndex        int    `json:"chainIndex"`
	BetweenUpstream   string `json:"betweenUpstream"`
	BetweenDownstream string `json:"betweenDownstream"`
	ExpectedStage     string `json:"expectedStage"`
	Reason            string `json:"reason"`
}

// ConcentrationRisk flags a product supplied by a single actor without alternatives.
type ConcentrationRisk struct {
	ChainIndex         int      `json:"chainIndex"`
	ProductPath        string   `json:"productPath"`
	ProductNodeIndex   int      `json:"productNodeIndex"`
	CurrentSuppliers   []string `json:"currentSuppliers"`
	AlternativesNeeded bool     `json:"alternativesNeeded"`
	Reason             string   `json:"reason"`
}

// MetricGap flags a segment that lacks volume or share data.
type MetricGap struct {
	ChainIndex    int    `json:"chainIndex"`
	FromNodeIndex int    `json:"fromNodeIndex"`
	ToNodeIndex   int    `json:"toNodeIndex"`
	MissingFields string `json:"missingFields"` // "volume" | "share" | "volume,share"
	Reason        string `json:"reason"`
}

// LogisticsGap flags a cross-region segment with no TransportVia data.
type LogisticsGap struct {
	ChainIndex       int    `json:"chainIndex"`
	FromNodePath     string `json:"fromNodePath"`
	ToNodePath       string `json:"toNodePath"`
	FromNodeGeoHint  string `json:"fromNodeGeoHint,omitempty"`
	ToNodeGeoHint    string `json:"toNodeGeoHint,omitempty"`
	Reason           string `json:"reason"`
}

// TemporalGap flags a segment whose SegmentMetrics lack a period/start/end.
type TemporalGap struct {
	ChainIndex    int    `json:"chainIndex"`
	FromNodeIndex int    `json:"fromNodeIndex"`
	ToNodeIndex   int    `json:"toNodeIndex"`
	Reason        string `json:"reason"`
}

// DepthGap flags a chain end that should be explored further in a given direction.
type DepthGap struct {
	ChainIndex    int    `json:"chainIndex"`
	Direction     string `json:"direction"` // "upstream" | "downstream"
	TerminalNode  string `json:"terminalNode"`
	CurrentDepth  int    `json:"currentDepth"`
	Reason        string `json:"reason"`
}

// IndustryGap is an LLM-identified domain-specific gap (missing industry stages,
// known chokepoints, geopolitical risks, etc.).
type IndustryGap struct {
	ChainIndex    int    `json:"chainIndex"`
	GapKind       string `json:"gapKind"` // free-form: "missing_stage", "chokepoint", "geopolitical_risk", ...
	Description   string `json:"description"`
	AnchorPath    string `json:"anchorPath,omitempty"`
	Reason        string `json:"reason"`
}

// RefineDirective is an actionable instruction for the refine LLM call.
type RefineDirective struct {
	GapType            string   `json:"gapType"`
	ChainIndex         int      `json:"chainIndex"`
	AnchorUpstream     string   `json:"anchorUpstream"`
	AnchorDownstream   string   `json:"anchorDownstream"`
	ExpectedStages     []string `json:"expectedStages,omitempty"`
	Query              string   `json:"query"`
	Priority           string   `json:"priority"`
	SuggestedPatchType string   `json:"suggestedPatchType,omitempty"`
}

// SupplyChainPatch is a mini-chain filling a specific gap.
// For "splice" patches: first AND last nodes are anchors existing in the chain.
// For "extend" patches: only first OR last node is an anchor.
type SupplyChainPatch struct {
	DirectiveIndex int                   `json:"directiveIndex"`
	PatchType      string                `json:"patchType"` // "splice" | "extend_upstream" | "extend_downstream" | "alternatives"
	Nodes          []SupplyChainNode     `json:"nodes"`
	SegmentMetrics []SegmentMetrics      `json:"segmentMetrics,omitempty"`
	Alternatives   []AlternativeSuppliers `json:"alternatives,omitempty"`
	Confidence     float64               `json:"confidence"`
	Reason         string                `json:"reason"`
}

// PatchSummary returns a compact description of the patch for logging.
func (p *SupplyChainPatch) PatchSummary() string {
	return fmt.Sprintf("dir=%d type=%s nodes=%d conf=%.2f", p.DirectiveIndex, p.PatchType, len(p.Nodes), p.Confidence)
}

// PatchTypeCounts returns a map of patchType -> count for a slice of patches.
func PatchTypeCounts(patches []SupplyChainPatch) map[string]int {
	m := map[string]int{}
	for _, p := range patches {
		m[p.PatchType]++
	}
	return m
}

// ---------------------------------------------------------------------------
// Analyzer
// ---------------------------------------------------------------------------

const supplyChainSufficiencyThreshold = 0.6

// DiagnosticFields returns telemetry-safe diagnostics from the gap report.
func (r *SupplyChainGapReport) DiagnosticFields() map[string]any {
	m := map[string]any{
		"llmFailed":            r.llmFailed,
		"llmTierGapsAdded":     r.llmTierGapsAdded,
		"llmIndustryGapsAdded": r.llmIndustryGapsAdded,
	}
	if r.llmError != "" {
		m["llmError"] = r.llmError
	}
	return m
}

// DirectiveSummaries returns a compact slice of directive descriptions for logging.
func (r *SupplyChainGapReport) DirectiveSummaries() []string {
	out := make([]string, 0, len(r.RefineDirectives))
	for i, d := range r.RefineDirectives {
		out = append(out, fmt.Sprintf("[%d] %s chain=%d prio=%s q=%q", i, d.GapType, d.ChainIndex, d.Priority, truncate(d.Query, 120)))
	}
	return out
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// SupplyChainGapAnalyzer detects semantic white spots in supply chains and
// produces structured RefineDirectives for targeted re-research.
type SupplyChainGapAnalyzer struct {
	executor *ai.AITaskExecutor // nil => deterministic-only mode
}

// NewSupplyChainGapAnalyzer creates a new analyzer.
// Pass nil executor to disable the LLM gap-analysis layer.
func NewSupplyChainGapAnalyzer(executor *ai.AITaskExecutor) *SupplyChainGapAnalyzer {
	return &SupplyChainGapAnalyzer{executor: executor}
}

// Analyze runs deterministic + (optionally) LLM gap detection over all chains,
// computes a sufficiency score, and builds RefineDirectives for any found gaps.
func (a *SupplyChainGapAnalyzer) Analyze(
	ctx context.Context,
	chains []SupplyChain,
	fragment *models.HorizonFragment,
) (*SupplyChainGapReport, error) {
	report := &SupplyChainGapReport{}
	if len(chains) == 0 {
		report.Sufficient = true
		report.Score = 1.0
		return report, nil
	}

	a.checkTierDepth(chains, report)
	a.checkUpstreamDownstream(chains, fragment, report)
	a.checkConcentration(chains, report)
	a.checkMetricCoverage(chains, report)
	a.checkLogistics(chains, report)
	a.checkTemporalCoverage(chains, report)

	deterministicTierGaps := len(report.TierGaps)

	if a.executor != nil {
		if err := a.runLLMGapAnalysis(ctx, chains, fragment, report); err != nil {
			report.llmFailed = true
			report.llmError = err.Error()
		} else {
			report.llmTierGapsAdded = len(report.TierGaps) - deterministicTierGaps
			report.llmIndustryGapsAdded = len(report.IndustryGaps)
		}
	}

	a.computeScore(report, chains)
	a.buildDirectives(report, chains)
	return report, nil
}

// ---------------------------------------------------------------------------
// Deterministic checks
// ---------------------------------------------------------------------------

func isActorType(t string) bool {
	ft := models.FragmentType(strings.ToUpper(strings.TrimSpace(t)))
	switch ft {
	case models.FragmentTypeOrganization, models.FragmentTypeFacility,
		models.FragmentTypeCommunity, models.FragmentTypeProgram:
		return true
	}
	return false
}

func isProductType(t string) bool {
	return models.FragmentType(strings.ToUpper(strings.TrimSpace(t))) == models.FragmentTypeProduct
}

// checkTierDepth counts TRANSFORMS_INTO hops (product->product adjacencies
// mediated by facilities). A manufacturing chain with < 3 product nodes
// is flagged; commodity chains with < 2 are flagged.
func (a *SupplyChainGapAnalyzer) checkTierDepth(chains []SupplyChain, report *SupplyChainGapReport) {
	for ci, ch := range chains {
		productCount := 0
		for _, n := range ch.Nodes {
			if isProductType(n.Type) {
				productCount++
			}
		}
		minProducts := 2
		if chainLooksManufacturing(ch) {
			minProducts = 3
		}
		if productCount < minProducts {
			for i := 0; i < len(ch.Nodes)-1; i++ {
				if isProductType(ch.Nodes[i].Type) && isProductType(ch.Nodes[i+1].Type) {
					continue
				}
				n1 := ch.Nodes[i]
				n2 := ch.Nodes[len(ch.Nodes)-1]
				if isActorType(n1.Type) && len(ch.Nodes) > 2 {
					n1 = ch.Nodes[1]
				}
				if isActorType(n2.Type) && len(ch.Nodes) > 2 {
					n2 = ch.Nodes[len(ch.Nodes)-2]
				}
				report.TierGaps = append(report.TierGaps, TierGap{
					ChainIndex:        ci,
					BetweenUpstream:   n1.CanonicalPath,
					BetweenDownstream: n2.CanonicalPath,
					ExpectedStage:     "intermediate_processing",
					Reason:            fmt.Sprintf("chain has %d product tier(s), expected >= %d", productCount, minProducts),
				})
				break
			}
		}
	}
}

func chainLooksManufacturing(ch SupplyChain) bool {
	for _, n := range ch.Nodes {
		role := strings.ToLower(strings.TrimSpace(n.Role))
		if role == "assembler" || role == "manufacturer" || role == "processor" {
			return true
		}
		if strings.Contains(strings.ToLower(n.Name), "assembl") ||
			strings.Contains(strings.ToLower(n.Name), "manufactur") {
			return true
		}
	}
	return false
}

// checkUpstreamDownstream verifies the anchor fragment (if present in chains)
// has both upstream (incoming supply) and downstream (outgoing supply) links.
func (a *SupplyChainGapAnalyzer) checkUpstreamDownstream(chains []SupplyChain, fragment *models.HorizonFragment, report *SupplyChainGapReport) {
	if fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" {
		return
	}
	anchorPath := strings.TrimSpace(fragment.CanonicalPath)
	hasUpstream := false
	hasDownstream := false
	for _, ch := range chains {
		for ni, n := range ch.Nodes {
			if strings.TrimSpace(n.CanonicalPath) != anchorPath {
				continue
			}
			if ni > 0 {
				hasUpstream = true
			}
			if ni < len(ch.Nodes)-1 {
				hasDownstream = true
			}
		}
	}
	if !hasUpstream {
		report.DepthGaps = append(report.DepthGaps, DepthGap{
			ChainIndex:   0,
			Direction:    "upstream",
			TerminalNode: anchorPath,
			CurrentDepth: 0,
			Reason:       fmt.Sprintf("anchor %s missing upstream coverage", anchorPath),
		})
	}
	if !hasDownstream {
		report.DepthGaps = append(report.DepthGaps, DepthGap{
			ChainIndex:   0,
			Direction:    "downstream",
			TerminalNode: anchorPath,
			CurrentDepth: 0,
			Reason:       fmt.Sprintf("anchor %s missing downstream coverage", anchorPath),
		})
	}
}

// checkConcentration finds product nodes supplied by a single actor with no alternatives.
func (a *SupplyChainGapAnalyzer) checkConcentration(chains []SupplyChain, report *SupplyChainGapReport) {
	for ci, ch := range chains {
		for ni, n := range ch.Nodes {
			if !isProductType(n.Type) {
				continue
			}
			suppliers := uniqueAdjacentActors(ch, ni, "upstream")
			hasAlternatives := alternativesExistForProduct(ch, ni)
			if len(suppliers) <= 1 && !hasAlternatives {
				report.ConcentrationRisks = append(report.ConcentrationRisks, ConcentrationRisk{
					ChainIndex:         ci,
					ProductPath:        n.CanonicalPath,
					ProductNodeIndex:   ni,
					CurrentSuppliers:   suppliers,
					AlternativesNeeded: true,
					Reason:             fmt.Sprintf("product %s has %d supplier(s), no alternatives mapped", n.Name, len(suppliers)),
				})
			}
		}
	}
}

func uniqueAdjacentActors(ch SupplyChain, productIdx int, direction string) []string {
	seen := map[string]struct{}{}
	var out []string
	switch direction {
	case "upstream":
		if productIdx > 0 {
			prev := ch.Nodes[productIdx-1]
			if isActorType(prev.Type) && strings.TrimSpace(prev.CanonicalPath) != "" {
				if _, ok := seen[prev.CanonicalPath]; !ok {
					seen[prev.CanonicalPath] = struct{}{}
					out = append(out, prev.CanonicalPath)
				}
			}
		}
	case "downstream":
		if productIdx < len(ch.Nodes)-1 {
			next := ch.Nodes[productIdx+1]
			if isActorType(next.Type) && strings.TrimSpace(next.CanonicalPath) != "" {
				if _, ok := seen[next.CanonicalPath]; !ok {
					seen[next.CanonicalPath] = struct{}{}
					out = append(out, next.CanonicalPath)
				}
			}
		}
	}
	return out
}

func alternativesExistForProduct(ch SupplyChain, productIdx int) bool {
	for _, alt := range ch.Alternatives {
		if alt.ProductNodeIndex == productIdx && len(alt.Suppliers) > 0 {
			return true
		}
	}
	return false
}

// checkMetricCoverage flags segments (consecutive node pairs) that lack
// volume and/or share in SegmentMetrics.
func (a *SupplyChainGapAnalyzer) checkMetricCoverage(chains []SupplyChain, report *SupplyChainGapReport) {
	for ci, ch := range chains {
		if len(ch.Nodes) < 2 {
			continue
		}
		type coverage struct {
			hasVolume bool
			hasShare  bool
		}
		pairCoverage := map[[2]int]*coverage{}
		for _, sm := range ch.SegmentMetrics {
			key := [2]int{sm.FromNodeIndex, sm.ToNodeIndex}
			c := pairCoverage[key]
			if c == nil {
				c = &coverage{}
				pairCoverage[key] = c
			}
			if sm.Volume != nil {
				c.hasVolume = true
			}
			if sm.Share != nil {
				c.hasShare = true
			}
		}
		for i := 0; i < len(ch.Nodes)-1; i++ {
			key := [2]int{i, i + 1}
			c := pairCoverage[key]
			var missing []string
			if c == nil {
				missing = append(missing, "volume", "share")
			} else {
				if !c.hasVolume {
					missing = append(missing, "volume")
				}
				if !c.hasShare {
					missing = append(missing, "share")
				}
			}
			if len(missing) == 0 {
				continue
			}
			report.MetricGaps = append(report.MetricGaps, MetricGap{
				ChainIndex:    ci,
				FromNodeIndex: i,
				ToNodeIndex:   i + 1,
				MissingFields: strings.Join(missing, ","),
				Reason:        fmt.Sprintf("segment %s -> %s missing %s", ch.Nodes[i].Name, ch.Nodes[i+1].Name, strings.Join(missing, " and ")),
			})
		}
	}
}

// checkLogistics flags chains with TransportVia empty where nodes hint at
// different geographic regions.
func (a *SupplyChainGapAnalyzer) checkLogistics(chains []SupplyChain, report *SupplyChainGapReport) {
	for ci, ch := range chains {
		if len(ch.TransportVia) > 0 {
			continue
		}
		regions := distinctGeoHints(ch)
		if len(regions) >= 2 {
			first := ch.Nodes[0]
			last := ch.Nodes[len(ch.Nodes)-1]
			report.LogisticsGaps = append(report.LogisticsGaps, LogisticsGap{
				ChainIndex:      ci,
				FromNodePath:    first.CanonicalPath,
				ToNodePath:      last.CanonicalPath,
				FromNodeGeoHint: geoHintFromNode(first),
				ToNodeGeoHint:   geoHintFromNode(last),
				Reason:          fmt.Sprintf("chain spans %d geo regions but has no transport routes", len(regions)),
			})
		}
	}
}

func distinctGeoHints(ch SupplyChain) map[string]struct{} {
	seen := map[string]struct{}{}
	for _, n := range ch.Nodes {
		hint := geoHintFromNode(n)
		if hint != "" {
			seen[hint] = struct{}{}
		}
	}
	return seen
}

func geoHintFromNode(n SupplyChainNode) string {
	cp := strings.TrimSpace(n.CanonicalPath)
	// Extract country/region slug from canonical paths like /org/{tenant}/... or /fac/{org}/{slug}
	// Heuristic: use the second path segment as a geo hint if it's 2-3 chars (country code).
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 2 {
		candidate := strings.ToLower(parts[1])
		if len(candidate) >= 2 && len(candidate) <= 3 {
			return candidate
		}
	}
	return ""
}

// checkTemporalCoverage flags SegmentMetrics entries that lack period/start/end.
func (a *SupplyChainGapAnalyzer) checkTemporalCoverage(chains []SupplyChain, report *SupplyChainGapReport) {
	for ci, ch := range chains {
		for _, sm := range ch.SegmentMetrics {
			hasTemporal := strings.TrimSpace(sm.Period) != "" ||
				(strings.TrimSpace(sm.Start) != "" && strings.TrimSpace(sm.End) != "")
			if !hasTemporal {
				report.TemporalGaps = append(report.TemporalGaps, TemporalGap{
					ChainIndex:    ci,
					FromNodeIndex: sm.FromNodeIndex,
					ToNodeIndex:   sm.ToNodeIndex,
					Reason:        "segment metrics lack period or start/end dates",
				})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Sufficiency scoring
// ---------------------------------------------------------------------------

func (a *SupplyChainGapAnalyzer) computeScore(report *SupplyChainGapReport, chains []SupplyChain) {
	tierScore := gapRatio(len(report.TierGaps))
	depthScore := gapRatio(len(report.DepthGaps))
	concScore := gapRatio(len(report.ConcentrationRisks))
	metricScore := gapRatio(len(report.MetricGaps))
	logScore := gapRatio(len(report.LogisticsGaps))
	tempScore := gapRatio(len(report.TemporalGaps))

	raw := 0.30*tierScore +
		0.20*depthScore +
		0.15*concScore +
		0.15*metricScore +
		0.10*logScore +
		0.10*tempScore

	// Chains with too few nodes cannot be sufficient regardless of gap counts,
	// because "no gaps found" when there's nothing to check is not a sign of quality.
	totalNodes := 0
	for _, ch := range chains {
		totalNodes += len(ch.Nodes)
	}
	const minNodesForFullScore = 6
	if totalNodes < minNodesForFullScore {
		ceiling := float64(totalNodes) / float64(minNodesForFullScore)
		if raw > ceiling {
			raw = ceiling
		}
	}

	report.Score = raw
	report.Sufficient = report.Score >= supplyChainSufficiencyThreshold
}

// gapRatio returns 1.0 when gapCount == 0 and decreases toward 0.0 as gaps grow.
// Uses 1/(1+gaps) so it never reaches exactly 0 (there's always some partial info).
func gapRatio(gapCount int) float64 {
	if gapCount <= 0 {
		return 1.0
	}
	return 1.0 / (1.0 + float64(gapCount))
}

// ---------------------------------------------------------------------------
// Directive builder
// ---------------------------------------------------------------------------

func (a *SupplyChainGapAnalyzer) buildDirectives(report *SupplyChainGapReport, chains []SupplyChain) {
	nameIndex := buildNodeNameIndex(chains)
	resolve := func(canonicalPath string) string {
		if name, ok := nameIndex[strings.TrimSpace(canonicalPath)]; ok {
			return name
		}
		return canonicalPath
	}

	seen := map[string]struct{}{}
	add := func(d RefineDirective) {
		dedup := d.GapType + "|" + d.Query
		if _, ok := seen[dedup]; ok {
			return
		}
		seen[dedup] = struct{}{}
		report.RefineDirectives = append(report.RefineDirectives, d)
	}

	for _, g := range report.TierGaps {
		add(RefineDirective{
			GapType:            "missing_tier",
			ChainIndex:         g.ChainIndex,
			AnchorUpstream:     g.BetweenUpstream,
			AnchorDownstream:   g.BetweenDownstream,
			ExpectedStages:     []string{g.ExpectedStage},
			Query:              fmt.Sprintf("Find intermediate processing stages between %s and %s", resolve(g.BetweenUpstream), resolve(g.BetweenDownstream)),
			Priority:           "P0",
			SuggestedPatchType: "splice",
		})
	}
	for _, g := range report.DepthGaps {
		d := RefineDirective{
			GapType:    "depth_gap",
			ChainIndex: g.ChainIndex,
			Query:      fmt.Sprintf("Extend the supply chain %s from %s", g.Direction, resolve(g.TerminalNode)),
			Priority:   "P0",
		}
		if g.Direction == "upstream" {
			d.AnchorDownstream = g.TerminalNode
			d.SuggestedPatchType = "extend_upstream"
		} else {
			d.AnchorUpstream = g.TerminalNode
			d.SuggestedPatchType = "extend_downstream"
		}
		add(d)
	}
	for _, g := range report.ConcentrationRisks {
		if !g.AlternativesNeeded {
			continue
		}
		add(RefineDirective{
			GapType:            "concentration_risk",
			ChainIndex:         g.ChainIndex,
			AnchorUpstream:     g.ProductPath,
			Query:              fmt.Sprintf("Find alternative suppliers for %s (%s)", resolve(g.ProductPath), g.ProductPath),
			Priority:           "P1",
			SuggestedPatchType: "alternatives",
		})
	}
	for _, g := range report.LogisticsGaps {
		add(RefineDirective{
			GapType:            "logistics_gap",
			ChainIndex:         g.ChainIndex,
			AnchorUpstream:     g.FromNodePath,
			AnchorDownstream:   g.ToNodePath,
			Query:              fmt.Sprintf("Identify transport routes (ports, terminals, pipelines) between %s and %s", resolve(g.FromNodePath), resolve(g.ToNodePath)),
			Priority:           "P1",
			SuggestedPatchType: "splice",
		})
	}
	for _, g := range report.IndustryGaps {
		d := RefineDirective{
			GapType:    "industry_gap",
			ChainIndex: g.ChainIndex,
			Query:      g.Description,
			Priority:   "P1",
		}
		if anchorCP := strings.TrimSpace(g.AnchorPath); anchorCP != "" {
			d.AnchorUpstream = anchorCP
			d.SuggestedPatchType = "extend_downstream"
		}
		add(d)
	}
}

// buildNodeNameIndex creates a map from canonical path -> human-readable name
// across all chain nodes, so directive queries can reference names.
func buildNodeNameIndex(chains []SupplyChain) map[string]string {
	m := map[string]string{}
	for _, ch := range chains {
		for _, n := range ch.Nodes {
			cp := strings.TrimSpace(n.CanonicalPath)
			name := strings.TrimSpace(n.Name)
			if cp != "" && name != "" {
				m[cp] = name
			}
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// LLM gap analysis
// ---------------------------------------------------------------------------

func (a *SupplyChainGapAnalyzer) runLLMGapAnalysis(
	ctx context.Context,
	chains []SupplyChain,
	fragment *models.HorizonFragment,
	report *SupplyChainGapReport,
) error {
	if a.executor == nil {
		return nil
	}
	prompt := a.buildLLMGapAnalysisPrompt(chains, fragment, report)
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			MinCapabilityScore:        7,
			MinContextWindow:          16000,
			MaxOutputSize:             8192,
		},
		Verifier: verifier,
		UseCase:  "horizon.scout.supply_chain_gap_analysis",
	}
	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return fmt.Errorf("supply chain gap LLM analysis: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal LLM gap result: %w", err)
	}
	var llmResult llmGapAnalysisResult
	if err := json.Unmarshal(jsonData, &llmResult); err != nil {
		return fmt.Errorf("unmarshal LLM gap result: %w", err)
	}
	for _, ig := range llmResult.IndustryGaps {
		report.IndustryGaps = append(report.IndustryGaps, ig)
	}
	for _, tg := range llmResult.AdditionalTierGaps {
		report.TierGaps = append(report.TierGaps, tg)
	}
	return nil
}

type llmGapAnalysisResult struct {
	IndustryGaps       []IndustryGap `json:"industryGaps"`
	AdditionalTierGaps []TierGap     `json:"additionalTierGaps"`
}

func (a *SupplyChainGapAnalyzer) buildLLMGapAnalysisPrompt(
	chains []SupplyChain,
	fragment *models.HorizonFragment,
	report *SupplyChainGapReport,
) string {
	chainsJSON, _ := json.MarshalIndent(chains, "", "  ")
	entityContext := ""
	if fragment != nil {
		entityContext = fmt.Sprintf("Entity: %s (type: %s, path: %s)",
			fragmentLabel(fragment), string(fragment.Type), fragment.CanonicalPath)
	}

	deterministicSummary := formatDeterministicGapSummary(report)

	return fmt.Sprintf(`You are a supply chain domain expert. Analyze the following supply chains for an entity and identify SEMANTIC gaps that deterministic checks cannot catch.

ENTITY TYPES in supply chain: %s.
RELATION TYPES: %s.

%s

SUPPLY CHAINS (JSON):
%s

DETERMINISTIC GAPS ALREADY FOUND:
%s

YOUR TASK:
1. Given the industry/product context, identify typical value chain stages that are MISSING from these chains.
2. Identify known logistics chokepoints (Suez, Malacca, Panama, major ports) that should appear but don't.
3. Identify geopolitical/regulatory risks affecting this supply chain that are not represented.
4. Identify substitution opportunities that should be explored.

Return JSON:
{
  "industryGaps": [
    {
      "chainIndex": 0,
      "gapKind": "missing_stage" | "chokepoint" | "geopolitical_risk" | "substitution_opportunity",
      "description": "human-readable description of the gap and what should be researched",
      "anchorPath": "/path/to/nearest/existing/node (optional)",
      "reason": "why this matters for supply chain resilience"
    }
  ],
  "additionalTierGaps": [
    {
      "chainIndex": 0,
      "betweenUpstream": "/path/to/upstream/node",
      "betweenDownstream": "/path/to/downstream/node",
      "expectedStage": "refining",
      "reason": "why this stage is expected"
    }
  ]
}

Return ONLY valid JSON. No markdown.`,
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain),
		taxonomy.SupplyChainEdgeTypesString(),
		entityContext,
		string(chainsJSON),
		deterministicSummary,
	)
}

func fragmentLabel(f *models.HorizonFragment) string {
	if f.Labels != nil {
		if en, ok := f.Labels["en"]; ok && strings.TrimSpace(en) != "" {
			return strings.TrimSpace(en)
		}
		for _, v := range f.Labels {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return strings.TrimSpace(f.CanonicalPath)
}

func formatDeterministicGapSummary(report *SupplyChainGapReport) string {
	var parts []string
	if len(report.TierGaps) > 0 {
		parts = append(parts, fmt.Sprintf("- %d tier depth gap(s)", len(report.TierGaps)))
	}
	if len(report.DepthGaps) > 0 {
		parts = append(parts, fmt.Sprintf("- %d depth gap(s) (missing upstream/downstream)", len(report.DepthGaps)))
	}
	if len(report.ConcentrationRisks) > 0 {
		parts = append(parts, fmt.Sprintf("- %d concentration risk(s) (single-source)", len(report.ConcentrationRisks)))
	}
	if len(report.MetricGaps) > 0 {
		parts = append(parts, fmt.Sprintf("- %d metric gap(s)", len(report.MetricGaps)))
	}
	if len(report.LogisticsGaps) > 0 {
		parts = append(parts, fmt.Sprintf("- %d logistics gap(s)", len(report.LogisticsGaps)))
	}
	if len(report.TemporalGaps) > 0 {
		parts = append(parts, fmt.Sprintf("- %d temporal gap(s)", len(report.TemporalGaps)))
	}
	if len(parts) == 0 {
		return "No deterministic gaps found."
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// Patch application
// ---------------------------------------------------------------------------

// ApplyPatches applies patches to chains in append-only mode.
// Existing nodes and metrics are never modified or removed.
// Patches whose anchor nodes don't match are silently skipped (returned in warnings).
func ApplyPatches(chains []SupplyChain, patches []SupplyChainPatch) ([]SupplyChain, []string) {
	result := make([]SupplyChain, len(chains))
	for i := range chains {
		result[i] = deepCopyChain(chains[i])
	}
	var warnings []string

	for _, patch := range patches {
		switch patch.PatchType {
		case "splice":
			applied, warn := applySplicePatch(&result, patch)
			if !applied {
				warnings = append(warnings, warn)
			}
		case "extend_upstream":
			applied, warn := applyExtendPatch(&result, patch, "upstream")
			if !applied {
				warnings = append(warnings, warn)
			}
		case "extend_downstream":
			applied, warn := applyExtendPatch(&result, patch, "downstream")
			if !applied {
				warnings = append(warnings, warn)
			}
		case "alternatives":
			applied, warn := applyAlternativesPatch(&result, patch)
			if !applied {
				warnings = append(warnings, warn)
			}
		default:
			warnings = append(warnings, fmt.Sprintf("directive %d: unknown patchType %q", patch.DirectiveIndex, patch.PatchType))
		}
	}
	return result, warnings
}

func applySplicePatch(chains *[]SupplyChain, patch SupplyChainPatch) (bool, string) {
	if len(patch.Nodes) < 3 {
		return false, fmt.Sprintf("directive %d: splice patch needs >= 3 nodes (anchor + inner + anchor), got %d", patch.DirectiveIndex, len(patch.Nodes))
	}
	anchorUp := strings.TrimSpace(patch.Nodes[0].CanonicalPath)
	anchorDown := strings.TrimSpace(patch.Nodes[len(patch.Nodes)-1].CanonicalPath)
	innerNodes := patch.Nodes[1 : len(patch.Nodes)-1]

	for ci := range *chains {
		ch := &(*chains)[ci]
		upIdx := findNodeIndex(ch.Nodes, anchorUp)
		downIdx := findNodeIndex(ch.Nodes, anchorDown)
		if upIdx < 0 || downIdx < 0 || upIdx >= downIdx {
			continue
		}

		// Preserve any existing nodes between the anchors (append-only invariant).
		existingMiddle := ch.Nodes[upIdx+1 : downIdx]

		newNodes := make([]SupplyChainNode, 0, len(ch.Nodes)+len(innerNodes))
		newNodes = append(newNodes, ch.Nodes[:upIdx+1]...)
		newNodes = append(newNodes, innerNodes...)
		newNodes = append(newNodes, existingMiddle...)
		newNodes = append(newNodes, ch.Nodes[downIdx:]...)

		inserted := len(innerNodes) + len(existingMiddle) - (downIdx - upIdx - 1)
		newMetrics := reindexMetrics(ch.SegmentMetrics, upIdx, inserted)
		for _, sm := range patch.SegmentMetrics {
			sm.FromNodeIndex += upIdx + 1
			sm.ToNodeIndex += upIdx + 1
			newMetrics = append(newMetrics, sm)
		}

		ch.Nodes = newNodes
		ch.SegmentMetrics = newMetrics
		return true, ""
	}
	return false, fmt.Sprintf("directive %d: anchor nodes %q / %q not found in any chain", patch.DirectiveIndex, anchorUp, anchorDown)
}

func applyExtendPatch(chains *[]SupplyChain, patch SupplyChainPatch, direction string) (bool, string) {
	if len(patch.Nodes) < 2 {
		return false, fmt.Sprintf("directive %d: extend patch needs >= 2 nodes, got %d", patch.DirectiveIndex, len(patch.Nodes))
	}

	switch direction {
	case "upstream":
		anchorPath := strings.TrimSpace(patch.Nodes[len(patch.Nodes)-1].CanonicalPath)
		// Try terminal position first (preferred: extends existing chain in place).
		for ci := range *chains {
			ch := &(*chains)[ci]
			if findNodeIndex(ch.Nodes, anchorPath) == 0 {
				newNodes := make([]SupplyChainNode, 0, len(ch.Nodes)+len(patch.Nodes)-1)
				newNodes = append(newNodes, patch.Nodes[:len(patch.Nodes)-1]...)
				newNodes = append(newNodes, ch.Nodes...)

				offset := len(patch.Nodes) - 1
				newMetrics := reindexMetrics(ch.SegmentMetrics, -1, offset)
				for _, sm := range patch.SegmentMetrics {
					newMetrics = append(newMetrics, sm)
				}
				ch.Nodes = newNodes
				ch.SegmentMetrics = newMetrics
				return true, ""
			}
		}
		// Anchor exists but not at position 0: create a new branch chain.
		for _, ch := range *chains {
			if findNodeIndex(ch.Nodes, anchorPath) >= 0 {
				branch := SupplyChain{
					Nodes:          patch.Nodes,
					SegmentMetrics: patch.SegmentMetrics,
					Confidence:     patch.Confidence,
					Reason:         fmt.Sprintf("upstream branch from %s (refine directive %d)", anchorPath, patch.DirectiveIndex),
				}
				*chains = append(*chains, branch)
				return true, ""
			}
		}

	case "downstream":
		anchorPath := strings.TrimSpace(patch.Nodes[0].CanonicalPath)
		for ci := range *chains {
			ch := &(*chains)[ci]
			if findNodeIndex(ch.Nodes, anchorPath) == len(ch.Nodes)-1 {
				newNodes := make([]SupplyChainNode, 0, len(ch.Nodes)+len(patch.Nodes)-1)
				newNodes = append(newNodes, ch.Nodes...)
				newNodes = append(newNodes, patch.Nodes[1:]...)

				baseLen := len(ch.Nodes)
				for _, sm := range patch.SegmentMetrics {
					sm.FromNodeIndex += baseLen - 1
					sm.ToNodeIndex += baseLen - 1
					ch.SegmentMetrics = append(ch.SegmentMetrics, sm)
				}
				ch.Nodes = newNodes
				return true, ""
			}
		}
		for _, ch := range *chains {
			if findNodeIndex(ch.Nodes, anchorPath) >= 0 {
				branch := SupplyChain{
					Nodes:          patch.Nodes,
					SegmentMetrics: patch.SegmentMetrics,
					Confidence:     patch.Confidence,
					Reason:         fmt.Sprintf("downstream branch from %s (refine directive %d)", anchorPath, patch.DirectiveIndex),
				}
				*chains = append(*chains, branch)
				return true, ""
			}
		}
	}
	return false, fmt.Sprintf("directive %d: anchor node not found in any chain for %s extension", patch.DirectiveIndex, direction)
}

func applyAlternativesPatch(chains *[]SupplyChain, patch SupplyChainPatch) (bool, string) {
	if len(patch.Alternatives) == 0 {
		return false, fmt.Sprintf("directive %d: alternatives patch has no alternatives", patch.DirectiveIndex)
	}
	if len(*chains) == 0 {
		return false, fmt.Sprintf("directive %d: no chains to apply alternatives to", patch.DirectiveIndex)
	}

	// Apply to first chain that has matching product/customer node indices.
	// If no match found, apply to first chain (best-effort).
	targetCI := 0
	for ci := range *chains {
		ch := (*chains)[ci]
		for _, newAlt := range patch.Alternatives {
			if newAlt.ProductNodeIndex < len(ch.Nodes) && newAlt.CustomerNodeIndex < len(ch.Nodes) {
				targetCI = ci
				break
			}
		}
	}

	ch := &(*chains)[targetCI]
	for _, newAlt := range patch.Alternatives {
		merged := false
		for ai := range ch.Alternatives {
			existing := &ch.Alternatives[ai]
			if existing.CustomerNodeIndex == newAlt.CustomerNodeIndex && existing.ProductNodeIndex == newAlt.ProductNodeIndex {
				existing.Suppliers = mergeAlternativeContracts(existing.Suppliers, newAlt.Suppliers)
				merged = true
				break
			}
		}
		if !merged {
			ch.Alternatives = append(ch.Alternatives, newAlt)
		}
	}
	return true, ""
}

func mergeAlternativeContracts(existing, new_ []AlternativeContract) []AlternativeContract {
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		seen[strings.TrimSpace(e.SupplierRef)] = struct{}{}
	}
	for _, n := range new_ {
		ref := strings.TrimSpace(n.SupplierRef)
		if _, ok := seen[ref]; !ok {
			seen[ref] = struct{}{}
			existing = append(existing, n)
		}
	}
	return existing
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findNodeIndex(nodes []SupplyChainNode, canonicalPath string) int {
	cp := strings.TrimSpace(canonicalPath)
	for i, n := range nodes {
		if strings.TrimSpace(n.CanonicalPath) == cp {
			return i
		}
	}
	return -1
}

// reindexMetrics shifts SegmentMetrics indices after an insertion at insertAfterIdx.
// All indices > insertAfterIdx are shifted by offset.
func reindexMetrics(metrics []SegmentMetrics, insertAfterIdx, offset int) []SegmentMetrics {
	out := make([]SegmentMetrics, len(metrics))
	for i, sm := range metrics {
		if sm.FromNodeIndex > insertAfterIdx {
			sm.FromNodeIndex += offset
		}
		if sm.ToNodeIndex > insertAfterIdx {
			sm.ToNodeIndex += offset
		}
		if sm.ProductIndex > insertAfterIdx {
			sm.ProductIndex += offset
		}
		out[i] = sm
	}
	return out
}

func deepCopyChain(ch SupplyChain) SupplyChain {
	out := SupplyChain{
		Confidence: ch.Confidence,
		Reason:     ch.Reason,
	}
	out.Nodes = make([]SupplyChainNode, len(ch.Nodes))
	copy(out.Nodes, ch.Nodes)
	if len(ch.TransportVia) > 0 {
		out.TransportVia = make([]string, len(ch.TransportVia))
		copy(out.TransportVia, ch.TransportVia)
	}
	if len(ch.SegmentMetrics) > 0 {
		out.SegmentMetrics = make([]SegmentMetrics, len(ch.SegmentMetrics))
		copy(out.SegmentMetrics, ch.SegmentMetrics)
	}
	if len(ch.Alternatives) > 0 {
		out.Alternatives = make([]AlternativeSuppliers, len(ch.Alternatives))
		for i, alt := range ch.Alternatives {
			out.Alternatives[i] = AlternativeSuppliers{
				CustomerNodeIndex: alt.CustomerNodeIndex,
				ProductNodeIndex:  alt.ProductNodeIndex,
			}
			if len(alt.Suppliers) > 0 {
				out.Alternatives[i].Suppliers = make([]AlternativeContract, len(alt.Suppliers))
				copy(out.Alternatives[i].Suppliers, alt.Suppliers)
			}
		}
	}
	return out
}
