package scout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type SupplyChainAssemblyDiagnosticOptions struct {
	RequireDerivedPaths       bool
	FailOnMissingDerivedPaths bool
	RequireAssemblyEdges      bool
	MinAnchorFanin            int
	MinPrimaryBranches        int
	MaxNoiseRatio             float64
}

func DefaultSupplyChainAssemblyDiagnosticOptions() SupplyChainAssemblyDiagnosticOptions {
	return SupplyChainAssemblyDiagnosticOptions{
		RequireDerivedPaths:       true,
		FailOnMissingDerivedPaths: true,
		RequireAssemblyEdges:      false,
		MinAnchorFanin:            1,
		MinPrimaryBranches:        1,
		MaxNoiseRatio:             0.75,
	}
}

type SupplyChainAssemblyDiagnosticReport struct {
	Pass                   bool           `json:"pass"`
	ArtifactID             string         `json:"artifactId,omitempty"`
	ArtifactKey            string         `json:"artifactKey,omitempty"`
	TenantID               string         `json:"tenantId,omitempty"`
	PathStatus             string         `json:"pathStatus,omitempty"`
	SummaryStatus          string         `json:"summaryStatus,omitempty"`
	Stage                  string         `json:"stage,omitempty"`
	Provisional            bool           `json:"provisional,omitempty"`
	SourceMode             string         `json:"sourceMode,omitempty"`
	AnchorSet              []string       `json:"anchorSet,omitempty"`
	ResolvedAnchors        []string       `json:"resolvedAnchors,omitempty"`
	AnchorProducts         []string       `json:"anchorProducts,omitempty"`
	ProductNodesN          int            `json:"productNodesN"`
	ActorNodesN            int            `json:"actorNodesN"`
	SupplyEdgesN           int            `json:"supplyEdgesN"`
	TransformEdgesN        int            `json:"transformEdgesN"`
	AssemblyEdgesN         int            `json:"assemblyEdgesN"`
	RouteBindingsN         int            `json:"routeBindingsN"`
	DerivedPathKeysN       int            `json:"derivedPathKeysN"`
	DerivedPathArtifactsN  int            `json:"derivedPathArtifactsN"`
	PrimaryBranchesN       int            `json:"primaryBranchesN"`
	SecondaryBranchesN     int            `json:"secondaryBranchesN"`
	SupportBranchesN       int            `json:"supportBranchesN"`
	PrimaryRouteCoverage   float64        `json:"primaryRouteCoverage,omitempty"`
	NamedFacilityCoverage  float64        `json:"namedFacilityCoverage,omitempty"`
	NamedProductCoverage   float64        `json:"namedProductCoverage,omitempty"`
	NoiseRatio             float64        `json:"noiseRatio,omitempty"`
	TopRoutes              []string       `json:"topRoutes,omitempty"`
	TopDistributionNodes   []string       `json:"topDistributionNodes,omitempty"`
	TopSuppliers           []string       `json:"topSuppliers,omitempty"`
	TopProductFamilies     []string       `json:"topProductFamilies,omitempty"`
	OpenGaps               []string       `json:"openGaps,omitempty"`
	MissingAnchors         []string       `json:"missingAnchors,omitempty"`
	MissingDerivedPathKeys []string       `json:"missingDerivedPathKeys,omitempty"`
	SummaryMismatches      []string       `json:"summaryMismatches,omitempty"`
	Warnings               []string       `json:"warnings,omitempty"`
	Errors                 []string       `json:"errors,omitempty"`
	AnchorFanin            map[string]int `json:"anchorFanin,omitempty"`
}

func DiagnoseSupplyChainAssembly(
	artifact *models.MaterializedArtifact,
	pathArtifacts []*models.MaterializedArtifact,
	summary map[string]interface{},
	opts SupplyChainAssemblyDiagnosticOptions,
) (*SupplyChainAssemblyDiagnosticReport, error) {
	report := &SupplyChainAssemblyDiagnosticReport{
		Pass:                   true,
		AnchorFanin:            map[string]int{},
		SummaryMismatches:      []string{},
		Warnings:               []string{},
		Errors:                 []string{},
		MissingAnchors:         []string{},
		MissingDerivedPathKeys: []string{},
	}
	if artifact == nil {
		report.Errors = append(report.Errors, "assembly artifact is nil")
		report.Pass = false
		return report, nil
	}
	report.ArtifactID = strings.TrimSpace(artifact.ID)
	report.ArtifactKey = strings.TrimSpace(artifact.CanonicalKey)
	report.TenantID = strings.TrimSpace(artifact.TenantID)
	report.PathStatus = strings.TrimSpace(artifact.PathStatus)

	if artifact.Kind != models.MaterializedKindSupplyChainAssemblyGraph {
		report.Errors = append(report.Errors, fmt.Sprintf("artifact kind=%q, want %q", artifact.Kind, models.MaterializedKindSupplyChainAssemblyGraph))
		report.Pass = false
		return report, nil
	}
	if err := artifact.Validate(); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("artifact validation failed: %v", err))
		report.Pass = false
		return report, nil
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("artifact payload parse failed: %v", err))
		report.Pass = false
		return report, nil
	}
	report.Stage = strings.TrimSpace(parsed.Stage)
	report.Provisional = parsed.Provisional
	report.SourceMode = strings.TrimSpace(parsed.SourceMode)
	report.AnchorSet = append([]string(nil), parsed.AnchorSet...)
	report.ResolvedAnchors = append([]string(nil), firstNonEmptyDiagnosticAnchors(parsed.ResolvedAnchors, parsed.AnchorSet, parsed.AnchorProducts)...)
	report.AnchorProducts = append([]string(nil), parsed.AnchorProducts...)
	report.ProductNodesN = len(parsed.ProductNodes)
	report.ActorNodesN = len(parsed.ActorNodes)
	report.SupplyEdgesN = len(parsed.SupplyEdges)
	report.TransformEdgesN, report.AssemblyEdgesN = models.AssemblyGraphSiteTransformCounts(parsed)
	report.RouteBindingsN = len(parsed.RouteBindings)
	report.DerivedPathKeysN = len(parsed.DerivedPathKeys)
	report.PrimaryBranchesN = len(parsed.PrimaryBranches)
	report.SecondaryBranchesN = len(parsed.SecondaryBranches)
	report.SupportBranchesN = len(parsed.SuppressedSupportBranches)
	report.TopRoutes = append([]string(nil), parsed.TopRoutes...)
	report.TopDistributionNodes = append([]string(nil), parsed.TopDistributionNodes...)
	report.TopSuppliers = append([]string(nil), parsed.TopSuppliers...)
	report.TopProductFamilies = append([]string(nil), parsed.TopProductFamilies...)
	report.OpenGaps = append([]string(nil), parsed.OpenGaps...)

	productRefs := make(map[string]struct{}, len(parsed.ProductNodes))
	for _, node := range parsed.ProductNodes {
		ref := strings.TrimSpace(node.Ref)
		if ref != "" {
			productRefs[ref] = struct{}{}
		}
	}
	anchorsToCheck := firstNonEmptyDiagnosticAnchors(parsed.ResolvedAnchors, parsed.AnchorSet, parsed.AnchorProducts)
	if len(anchorsToCheck) == 0 {
		report.Errors = append(report.Errors, "resolved anchors are empty")
	}
	if len(parsed.ProductNodes) == 0 && len(parsed.ActorNodes) == 0 {
		report.Errors = append(report.Errors, "productNodes and actorNodes are empty")
	}
	if len(parsed.SupplyEdges) == 0 && report.TransformEdgesN == 0 && report.AssemblyEdgesN == 0 {
		report.Errors = append(report.Errors, "assembly graph has no supply, transform, or assembly edges")
	}
	if opts.MinPrimaryBranches > 0 && len(parsed.PrimaryBranches) < opts.MinPrimaryBranches {
		report.Errors = append(report.Errors, fmt.Sprintf("primaryBranches=%d, want >= %d", len(parsed.PrimaryBranches), opts.MinPrimaryBranches))
	}
	actorRefs := make(map[string]struct{}, len(parsed.ActorNodes))
	for _, node := range parsed.ActorNodes {
		ref := strings.TrimSpace(node.Ref)
		if ref != "" {
			actorRefs[ref] = struct{}{}
		}
	}
	for _, anchor := range anchorsToCheck {
		anchor = strings.TrimSpace(anchor)
		if anchor == "" {
			continue
		}
		if _, ok := productRefs[anchor]; ok {
			continue
		}
		if _, ok := actorRefs[anchor]; ok {
			continue
		}
		if len(parsed.UnresolvedAnchors) > 0 && containsDiagnosticString(parsed.UnresolvedAnchors, anchor) {
			continue
		}
		report.MissingAnchors = append(report.MissingAnchors, anchor)
	}
	if len(report.MissingAnchors) > 0 {
		report.Errors = append(report.Errors, "resolved anchors are missing from assembly nodes")
	}

	incoming := map[string]map[string]struct{}{}
	addIncoming := func(toRef, fromRef string) {
		toRef = strings.TrimSpace(toRef)
		fromRef = strings.TrimSpace(fromRef)
		if toRef == "" || fromRef == "" {
			return
		}
		if incoming[toRef] == nil {
			incoming[toRef] = map[string]struct{}{}
		}
		incoming[toRef][fromRef] = struct{}{}
	}
	for _, an := range parsed.ActorNodes {
		for _, st := range an.SiteTransforms {
			for _, fromRef := range st.InputProducts {
				for _, toRef := range st.OutputProducts {
					addIncoming(toRef, fromRef)
				}
			}
		}
	}
	for _, pn := range parsed.ProductNodes {
		for _, st := range pn.SiteTransforms {
			for _, fromRef := range st.InputProducts {
				for _, toRef := range st.OutputProducts {
					addIncoming(toRef, fromRef)
				}
			}
		}
	}
	for _, edge := range parsed.SupplyEdges {
		addIncoming(edge.CustomerRef, edge.SupplierRef)
		addIncoming(edge.ProductRef, edge.SupplierRef)
		addIncoming(edge.CustomerRef, edge.ProductRef)
	}
	for _, anchor := range anchorsToCheck {
		anchor = strings.TrimSpace(anchor)
		report.AnchorFanin[anchor] = len(incoming[anchor])
		if opts.MinAnchorFanin > 0 && len(incoming[anchor]) < opts.MinAnchorFanin {
			report.Errors = append(report.Errors, fmt.Sprintf("anchor %s fan-in=%d, want >= %d", anchor, len(incoming[anchor]), opts.MinAnchorFanin))
		}
	}

	if opts.RequireAssemblyEdges && report.AssemblyEdgesN == 0 {
		report.Errors = append(report.Errors, "assembly siteTransforms (assembly kind) is empty")
	}
	if opts.RequireDerivedPaths && len(parsed.DerivedPathKeys) == 0 {
		report.Errors = append(report.Errors, "derivedPathKeys is empty")
	}

	pathByKey := map[string]*models.MaterializedArtifact{}
	for _, pathArtifact := range pathArtifacts {
		if pathArtifact == nil {
			continue
		}
		key := strings.TrimSpace(pathArtifact.CanonicalKey)
		if key == "" {
			continue
		}
		pathByKey[key] = pathArtifact
	}
	report.DerivedPathArtifactsN = len(pathByKey)
	for _, key := range parsed.DerivedPathKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		pathArtifact, ok := pathByKey[key]
		if !ok {
			report.MissingDerivedPathKeys = append(report.MissingDerivedPathKeys, key)
			continue
		}
		if pathArtifact.Kind != models.MaterializedKindSupplyChainPath {
			report.Warnings = append(report.Warnings, fmt.Sprintf("derived artifact %s has kind=%q", key, pathArtifact.Kind))
		}
	}
	sort.Strings(report.MissingDerivedPathKeys)
	if opts.FailOnMissingDerivedPaths && len(report.MissingDerivedPathKeys) > 0 {
		report.Errors = append(report.Errors, "some derived path artifacts are missing")
	}

	if summary != nil {
		report.SummaryStatus = strings.TrimSpace(parseStateString(summary["status"]))
		if report.SummaryStatus != "" && report.PathStatus != "" && report.SummaryStatus != report.PathStatus {
			report.SummaryMismatches = append(report.SummaryMismatches, fmt.Sprintf("status summary=%s artifact=%s", report.SummaryStatus, report.PathStatus))
		}
		if status := report.SummaryStatus; status == "" {
			report.SummaryMismatches = append(report.SummaryMismatches, "summary.status is empty")
		}
		compareSummaryCount(&report.SummaryMismatches, "productsN", parseStateInt(summary["productsN"], -1), report.ProductNodesN)
		compareSummaryCount(&report.SummaryMismatches, "actorsN", parseStateInt(summary["actorsN"], -1), report.ActorNodesN)
		compareSummaryCount(&report.SummaryMismatches, "transformsN", parseStateInt(summary["transformsN"], -1), report.TransformEdgesN+report.AssemblyEdgesN)
		compareSummaryCount(&report.SummaryMismatches, "derivedPathsN", parseStateInt(summary["derivedPathsN"], -1), report.DerivedPathKeysN)
		compareSummaryCount(&report.SummaryMismatches, "openGapsN", parseStateInt(summary["openGapsN"], -1), len(report.OpenGaps))
		compareSummaryCount(&report.SummaryMismatches, "primaryBranchesN", parseStateInt(summary["primaryBranchesN"], -1), report.PrimaryBranchesN)
		compareSummaryCount(&report.SummaryMismatches, "secondaryBranchesN", parseStateInt(summary["secondaryBranchesN"], -1), report.SecondaryBranchesN)
		compareSummaryCount(&report.SummaryMismatches, "supportBranchesN", parseStateInt(summary["supportBranchesN"], -1), report.SupportBranchesN)
		if artifactKey := strings.TrimSpace(parseStateString(summary["artifactKey"])); artifactKey != "" && artifactKey != report.ArtifactKey {
			report.SummaryMismatches = append(report.SummaryMismatches, fmt.Sprintf("artifactKey summary=%s artifact=%s", artifactKey, report.ArtifactKey))
		}
		summaryAnchors := uniqueSortedDiagnosticStrings(firstNonEmptyDiagnosticAnchors(
			parseStateStringSlice(summary["resolvedAnchors"]),
			parseStateStringSlice(summary["anchorSet"]),
			parseStateStringSlice(summary["anchorProducts"]),
		))
		artifactAnchors := uniqueSortedDiagnosticStrings(anchorsToCheck)
		if !equalStringSlices(summaryAnchors, artifactAnchors) {
			report.SummaryMismatches = append(report.SummaryMismatches, fmt.Sprintf("anchors summary=%v artifact=%v", summaryAnchors, artifactAnchors))
		}
	}

	if len(report.SummaryMismatches) > 0 {
		report.Warnings = append(report.Warnings, report.SummaryMismatches...)
	}
	enrichSupplyChainQualityReport(report, parsed, opts)
	if len(report.Errors) > 0 {
		report.Pass = false
	}
	return report, nil
}

func enrichSupplyChainQualityReport(report *SupplyChainAssemblyDiagnosticReport, parsed *models.SupplyChainAssemblyGraphPayload, opts SupplyChainAssemblyDiagnosticOptions) {
	if report == nil || parsed == nil {
		return
	}
	totalEdges := len(parsed.SupplyEdges) + report.TransformEdgesN + report.AssemblyEdgesN
	if parsed.Coverage != nil && totalEdges > 0 {
		report.NoiseRatio = float64(parsed.Coverage.NoiseEdgesN) / float64(totalEdges)
		if opts.MaxNoiseRatio > 0 && report.NoiseRatio > opts.MaxNoiseRatio {
			report.Errors = append(report.Errors, fmt.Sprintf("noise_ratio=%.2f, want <= %.2f", report.NoiseRatio, opts.MaxNoiseRatio))
		}
	}
	primaryRouteBound := 0
	namedFacilityBranches := 0
	namedProductBranches := 0
	for _, branch := range parsed.PrimaryBranches {
		if len(branch.RouteRefs) > 0 {
			primaryRouteBound++
		}
		if branchHasNamedFacility(branch) {
			namedFacilityBranches++
		}
		if len(branch.ProductRefs) > 0 {
			namedProductBranches++
		}
	}
	if len(parsed.PrimaryBranches) > 0 {
		report.PrimaryRouteCoverage = float64(primaryRouteBound) / float64(len(parsed.PrimaryBranches))
		report.NamedFacilityCoverage = float64(namedFacilityBranches) / float64(len(parsed.PrimaryBranches))
		report.NamedProductCoverage = float64(namedProductBranches) / float64(len(parsed.PrimaryBranches))
	}
	if isRetailLikeAssembly(parsed) {
		if !hasGoodsOrDistributionPrimaryBranch(parsed.PrimaryBranches) {
			report.Errors = append(report.Errors, "retail/distribution assembly has no promoted goods or distribution primary branch")
		}
		if len(parsed.TopDistributionNodes) == 0 {
			report.Errors = append(report.Errors, "retail/distribution assembly is missing topDistributionNodes")
		}
	}
	if primarySupportOutranksGoods(parsed.PrimaryBranches) {
		report.Errors = append(report.Errors, "support branch outranks goods/distribution primary branches")
	}
	if len(parsed.PrimaryBranches) > 0 && report.NamedFacilityCoverage == 0 {
		report.Warnings = append(report.Warnings, "primary branches lack named facilities")
	}
	if len(parsed.PrimaryBranches) > 0 && report.NamedProductCoverage == 0 {
		report.Warnings = append(report.Warnings, "primary branches lack named product families")
	}
}

func isRetailLikeAssembly(parsed *models.SupplyChainAssemblyGraphPayload) bool {
	text := strings.ToLower(strings.Join(append(append(append([]string{}, parsed.AnchorSet...), parsed.TopDistributionNodes...), parsed.TopProductFamilies...), "|"))
	for _, token := range []string{"retail", "store", "warehouse", "distribution", "fulfillment", "marwin", "meloman"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func hasGoodsOrDistributionPrimaryBranch(branches []models.SupplyChainAssemblyBranchSummary) bool {
	for _, branch := range branches {
		switch strings.TrimSpace(branch.BranchClass) {
		case "primary_goods_flow", "distribution_retail_branch", "component_chain", "material_input_chain":
			return true
		}
	}
	return false
}

func primarySupportOutranksGoods(branches []models.SupplyChainAssemblyBranchSummary) bool {
	if len(branches) == 0 {
		return false
	}
	bestSupport := -1.0
	bestGoods := -1.0
	for _, branch := range branches {
		score := branch.DecisionValueScore
		switch strings.TrimSpace(branch.BranchClass) {
		case "route_logistics_support", "infra_energy_support", "packaging_support", "background_noise":
			if score > bestSupport {
				bestSupport = score
			}
		default:
			if score > bestGoods {
				bestGoods = score
			}
		}
	}
	return bestSupport > bestGoods && bestGoods >= 0
}

func branchHasNamedFacility(branch models.SupplyChainAssemblyBranchSummary) bool {
	for _, ref := range branch.ActorRefs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "/fac/") || strings.Contains(strings.ToLower(ref), "warehouse") || strings.Contains(strings.ToLower(ref), "distribution") {
			return true
		}
	}
	return false
}

func compareSummaryCount(dst *[]string, field string, summaryValue int, actualValue int) {
	if summaryValue < 0 {
		return
	}
	if summaryValue != actualValue {
		*dst = append(*dst, fmt.Sprintf("%s summary=%d artifact=%d", field, summaryValue, actualValue))
	}
}

func uniqueSortedDiagnosticStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
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
	sort.Strings(out)
	return out
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func firstNonEmptyDiagnosticAnchors(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return append([]string(nil), value...)
		}
	}
	return nil
}

func containsDiagnosticString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
