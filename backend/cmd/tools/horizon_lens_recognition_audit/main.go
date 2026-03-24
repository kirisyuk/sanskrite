package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	agentmeshsim "github.com/igorkirisyuk/sanskrite/internal/agentmesh/simulation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonsim "github.com/igorkirisyuk/sanskrite/internal/horizon/simulation"
)

type lensRow struct {
	ID        string
	ProjectID string
	Name      string
	UpdatedAt time.Time
	Lens      *horizonmodels.Lens
}

func main() {
	ctx := context.Background()
	projectID := firstNonEmpty(os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram")
	tenantID := firstNonEmpty(os.Getenv("HORIZON_AUDIT_TENANT"), "igor@etigram.pro")
	outPath := firstNonEmpty(os.Getenv("HORIZON_AUDIT_OUT"), "/Users/kirisyuk/cursor/sanskrite/docs/horizon/implementation/latest-lens-recognition-audit.md")
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	lenses, err := latestLenses(ctx, client, tenantID, 8)
	if err != nil {
		log.Fatal(err)
	}

	report := buildReport(tenantID, lenses)
	if err := os.WriteFile(outPath, []byte(report), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", outPath)
}

func latestLenses(ctx context.Context, client *firestore.Client, tenantID string, limit int) ([]lensRow, error) {
	iter := client.Collection("horizon_lenses").Where("tenantId", "==", strings.TrimSpace(tenantID)).Documents(ctx)
	defer iter.Stop()
	rows := []lensRow{}
	for {
		doc, err := iter.Next()
		if err != nil {
			if strings.Contains(err.Error(), "no more items in iterator") {
				break
			}
			return nil, err
		}
		var lens horizonmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		lens.ID = doc.Ref.ID
		name := strings.TrimSpace(lens.Name)
		if name == "" || strings.EqualFold(name, "untitled lens") {
			continue
		}
		rows = append(rows, lensRow{
			ID:        lens.ID,
			ProjectID: strings.TrimSpace(lens.ProjectID),
			Name:      name,
			UpdatedAt: lens.UpdatedAt,
			Lens:      &lens,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func buildReport(tenantID string, rows []lensRow) string {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(&b, "# Latest Lens Recognition Audit\n\n")
	fmt.Fprintf(&b, "- tenant: `%s`\n", tenantID)
	fmt.Fprintf(&b, "- generatedAt: `%s`\n", now)
	fmt.Fprintf(&b, "- lensesAudited: `%d`\n\n", len(rows))
	fmt.Fprintf(&b, "## Goal\n\n")
	fmt.Fprintf(&b, "Audit the **first stage only**: how the system recognizes axes, objects, and scope volume from a natural-language request plus existing lens context, before full multi-agent simulation.\n\n")
	fmt.Fprintf(&b, "## Summary\n\n")

	axisGood := 0
	objectGood := 0
	scaleGood := 0
	type audit struct {
		row             lensRow
		query           string
		rec             map[string]any
		expectedAxes    []string
		expectedObjects []string
		expectedProfile string
		axisOverlap     []string
		objectOverlap   []string
		population      []string
	}
	audits := make([]audit, 0, len(rows))
	for _, row := range rows {
		query := auditQuery(row.Lens)
		refs := baselineRefsFromLens(row.Lens)
		rec := horizonsim.BuildInputRecognition(&horizonmodels.SimulationRunRequest{
			Question:  query,
			LensID:    row.ID,
			Mode:      strings.Join(row.Lens.DomainModes, ","),
			ProjectID: row.ProjectID,
		}, row.Lens, refs)
		expectedAxes := expectedAxesFromLens(row.Lens)
		expectedObjects := expectedObjectsFromLens(row.Lens)
		expectedProfile := expectedProfileFromLens(row.Lens)
		recognizedAxes := topAxisIDs(rec)
		recognizedObjects := topObjectPaths(rec)
		axisOverlap := overlap(expectedAxes, recognizedAxes)
		objectOverlap := overlap(expectedObjects, recognizedObjects)
		population := agentmeshsim.PreviewPopulationClasses(&horizonmodels.SimulationRun{
			Request:     horizonmodels.SimulationRunRequest{Question: query, Mode: strings.Join(row.Lens.DomainModes, ","), LensID: row.ID},
			Recognition: rec,
		}, 0)
		if len(axisOverlap) > 0 {
			axisGood++
		}
		if len(objectOverlap) >= min(len(expectedObjects), 2) {
			objectGood++
		}
		if mapStringValue(mapValue(rec["volumeProfile"]), "profile") == expectedProfile {
			scaleGood++
		}
		audits = append(audits, audit{
			row:             row,
			query:           query,
			rec:             rec,
			expectedAxes:    expectedAxes,
			expectedObjects: expectedObjects,
			expectedProfile: expectedProfile,
			axisOverlap:     axisOverlap,
			objectOverlap:   objectOverlap,
			population:      population,
		})
	}
	fmt.Fprintf(&b, "- axis grounding matched on `%d/%d` lenses\n", axisGood, len(audits))
	fmt.Fprintf(&b, "- object grounding matched strongly on `%d/%d` lenses\n", objectGood, len(audits))
	fmt.Fprintf(&b, "- volume/profile classification matched on `%d/%d` lenses\n\n", scaleGood, len(audits))
	fmt.Fprintf(&b, "## Per Lens\n\n")

	for _, item := range audits {
		volume := mapValue(item.rec["volumeProfile"])
		fmt.Fprintf(&b, "### %s\n\n", item.row.Name)
		fmt.Fprintf(&b, "- `lensId`: `%s`\n", item.row.ID)
		fmt.Fprintf(&b, "- `projectId`: `%s`\n", item.row.ProjectID)
		fmt.Fprintf(&b, "- query used: `%s`\n", item.query)
		fmt.Fprintf(&b, "- expected axes: `%s`\n", strings.Join(item.expectedAxes, "`, `"))
		fmt.Fprintf(&b, "- recognized primary axis: `%s`\n", mapStringValue(item.rec, "primaryAxis"))
		fmt.Fprintf(&b, "- axis overlap: `%s`\n", strings.Join(item.axisOverlap, "`, `"))
		fmt.Fprintf(&b, "- expected objects: `%s`\n", strings.Join(item.expectedObjects, "`, `"))
		fmt.Fprintf(&b, "- recognized objects: `%s`\n", strings.Join(topObjectPaths(item.rec), "`, `"))
		fmt.Fprintf(&b, "- object overlap: `%s`\n", strings.Join(item.objectOverlap, "`, `"))
		fmt.Fprintf(&b, "- expected profile: `%s`\n", item.expectedProfile)
		fmt.Fprintf(&b, "- recognized profile: `%s`\n", mapStringValue(volume, "profile"))
		fmt.Fprintf(&b, "- generic agent classes: `%s`\n", strings.Join(item.population, "`, `"))
		fmt.Fprintf(&b, "- recognition summary: %s\n\n", mapStringValue(item.rec, "summary"))
	}

	fmt.Fprintf(&b, "## Primary Changes Applied\n\n")
	fmt.Fprintf(&b, "- Added a simulation input recognition layer that infers `axisCandidates`, `objectCandidates`, and `volumeProfile` from natural-language query plus existing lens context.\n")
	fmt.Fprintf(&b, "- Wired recognition into `CreateRun`, so partially specified simulation requests now auto-fill primary axis, actor paths, location path, and seed scope.\n")
	fmt.Fprintf(&b, "- Upgraded generic agent assignment so non-hot-path scenarios use recognition-aware role mixes instead of the old flat `consumer/firm/media/regulator/bank` roster.\n\n")
	fmt.Fprintf(&b, "## Files\n\n")
	fmt.Fprintf(&b, "- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/recognition.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/recognition.go)\n")
	fmt.Fprintf(&b, "- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/service.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/service.go)\n")
	fmt.Fprintf(&b, "- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/simulation.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/simulation.go)\n")
	fmt.Fprintf(&b, "- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/processor.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/processor.go)\n")
	fmt.Fprintf(&b, "- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/population_preview.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/population_preview.go)\n")
	return b.String()
}

func auditQuery(lens *horizonmodels.Lens) string {
	if lens == nil {
		return ""
	}
	if strings.TrimSpace(lens.OriginalQuery) != "" {
		return strings.TrimSpace(lens.OriginalQuery)
	}
	if len(lens.DomainQueries) > 0 && strings.TrimSpace(lens.DomainQueries[0].Title) != "" {
		return strings.TrimSpace(lens.DomainQueries[0].Title)
	}
	if len(lens.DomainModes) > 0 {
		return fmt.Sprintf("Map the main %s dynamics for %s and identify the most material exposed objects.", strings.Join(lens.DomainModes, " and "), lens.Name)
	}
	return fmt.Sprintf("Identify the main exposure contours and critical objects for %s.", lens.Name)
}

func baselineRefsFromLens(lens *horizonmodels.Lens) horizonmodels.SimulationBaselineRefs {
	if lens == nil {
		return horizonmodels.SimulationBaselineRefs{}
	}
	scope := append([]string{}, lens.Scope...)
	scope = append(scope, viewConfigPaths(lens.ViewConfig)...)
	artifactKeys := make([]string, 0, len(lens.ScopeArtifacts))
	for key, ref := range lens.ScopeArtifacts {
		artifactKeys = append(artifactKeys, key)
		scope = append(scope, ref.AnchorSupplier, ref.AnchorCustomer, ref.ProductAxis)
		scope = append(scope, ref.NodeRefs...)
	}
	return horizonmodels.SimulationBaselineRefs{
		LensID:        lens.ID,
		ScopePaths:    compactStringsPreserveOrder(scope),
		FragmentPaths: compactStringsPreserveOrder(scope),
		ArtifactKeys:  compactStringsPreserveOrder(artifactKeys),
	}
}

func expectedAxesFromLens(lens *horizonmodels.Lens) []string {
	axes := []string{}
	if lens == nil {
		return axes
	}
	for _, mode := range lens.DomainModes {
		switch mode {
		case "supply_chain":
			axes = append(axes, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity)
		case "market":
			axes = append(axes, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisFinancialSettlement)
		}
	}
	for _, dq := range lens.DomainQueries {
		switch strings.ToLower(strings.TrimSpace(dq.ContractType)) {
		case "supply-chain":
			axes = append(axes, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity)
		case "sector-overview", "competitive-landscape":
			axes = append(axes, horizonmodels.AxisPhysicalFlowDemand)
		}
	}
	if containsText(strings.ToLower(lens.OriginalQuery), "sanction", "regulatory", "compliance") {
		axes = append(axes, horizonmodels.AxisLegalCompliance)
	}
	scopePaths := compactStringsPreserveOrder(append(append([]string{}, lens.Scope...), viewConfigPaths(lens.ViewConfig)...))
	kindCounts := map[string]int{}
	joined := strings.ToLower(strings.Join(scopePaths, " | "))
	for _, path := range scopePaths {
		kindCounts[objectKindFromPath(path)]++
	}
	if kindCounts["product"] >= 1 && (kindCounts["org"] >= 1 || kindCounts["fac"] >= 1 || kindCounts["domain"] >= 1) {
		axes = append(axes, horizonmodels.AxisPhysicalFlowSupply)
	}
	if kindCounts["product"] >= 2 && kindCounts["org"] == 0 && kindCounts["fac"] == 0 && kindCounts["domain"] == 0 && kindCounts["market"] == 0 {
		axes = append(axes, horizonmodels.AxisPhysicalFlowSupply)
	}
	if kindCounts["fac"] >= 1 && (strings.Contains(joined, "/port/") || strings.Contains(joined, "port-") || strings.Contains(joined, "terminal") || strings.Contains(joined, "logistics")) {
		axes = append(axes, horizonmodels.AxisLogisticsCapacity)
	}
	if kindCounts["market"] >= 1 {
		axes = append(axes, horizonmodels.AxisPhysicalFlowDemand)
	}
	if kindCounts["market"] >= 1 && kindCounts["product"] >= 1 {
		axes = append(axes, horizonmodels.AxisFinancialSettlement)
	}
	return compactStringsPreserveOrder(axes)
}

func expectedObjectsFromLens(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	paths := append([]string{}, lens.Scope...)
	paths = append(paths, viewConfigPaths(lens.ViewConfig)...)
	for _, dq := range lens.DomainQueries {
		paths = append(paths, dq.CanonicalPath)
	}
	for _, ref := range lens.ScopeArtifacts {
		paths = append(paths, ref.AnchorSupplier, ref.AnchorCustomer, ref.ProductAxis)
		paths = append(paths, ref.NodeRefs...)
	}
	filtered := make([]string, 0, 8)
	for _, path := range compactStringsPreserveOrder(paths) {
		kind := objectKindFromPath(path)
		if kind == "org" || kind == "fac" || kind == "market" || kind == "product" || kind == "domain" || kind == "geo" {
			filtered = append(filtered, path)
		}
		if len(filtered) >= 8 {
			break
		}
	}
	return filtered
}

func expectedProfileFromLens(lens *horizonmodels.Lens) string {
	if lens == nil {
		return "focused_object"
	}
	geoSet := map[string]struct{}{}
	scopePaths := append(append([]string{}, lens.Scope...), viewConfigPaths(lens.ViewConfig)...)
	kindCounts := map[string]int{}
	for _, path := range scopePaths {
		if geo := geoFromPath(path); geo != "" {
			geoSet[geo] = struct{}{}
		}
		kindCounts[objectKindFromPath(path)]++
	}
	queryText := strings.ToLower(strings.TrimSpace(lens.OriginalQuery + " " + lens.Name))
	chainBreadth := kindCounts["org"] + kindCounts["fac"] + kindCounts["product"] + kindCounts["gov"]
	switch {
	case len(scopePaths) <= 3 && kindCounts["domain"] == 0 && kindCounts["market"] == 0:
		return "focused_object"
	case len(geoSet) >= 4 && (len(lens.ScopeArtifacts) >= 2 || chainBreadth >= 5 || kindCounts["market"] >= 1):
		return "cross_border_network"
	case looksSectorNetwork(queryText):
		return "sector_network"
	case len(lens.DomainQueries) >= 2 || kindCounts["domain"] >= 2 || kindCounts["market"] >= 2:
		return "sector_network"
	case kindCounts["domain"] >= 1 && kindCounts["market"] >= 1 && chainBreadth >= 6:
		return "sector_network"
	case kindCounts["domain"] == 0 && kindCounts["market"] == 0 && chainBreadth <= 2:
		return "focused_object"
	default:
		return "single_chain"
	}
}

func topAxisIDs(recognition map[string]any) []string {
	out := []string{}
	for _, item := range sliceMapAny(recognition["axisCandidates"]) {
		out = append(out, mapStringValue(item, "axis"))
	}
	return compactStringsPreserveOrder(out)
}

func topObjectPaths(recognition map[string]any) []string {
	out := []string{}
	for _, item := range sliceMapAny(recognition["objectCandidates"]) {
		out = append(out, mapStringValue(item, "path"))
	}
	return compactStringsPreserveOrder(out)
}

func overlap(expected, actual []string) []string {
	index := map[string]struct{}{}
	for _, item := range actual {
		index[item] = struct{}{}
	}
	out := []string{}
	for _, item := range expected {
		if _, ok := index[item]; ok {
			out = append(out, item)
		}
	}
	return compactStringsPreserveOrder(out)
}

func containsMarketPath(paths []string) bool {
	for _, path := range paths {
		if strings.HasPrefix(strings.TrimSpace(path), "/market/") {
			return true
		}
	}
	return false
}

func objectKindFromPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "org"
	case strings.HasPrefix(path, "/fac/"):
		return "fac"
	case strings.HasPrefix(path, "/market/"):
		return "market"
	case strings.HasPrefix(path, "/product/"):
		return "product"
	case strings.HasPrefix(path, "/loc/"):
		return "geo"
	case strings.HasPrefix(path, "/domain/"):
		return "domain"
	case strings.HasPrefix(path, "/gov/"):
		return "gov"
	default:
		return "node"
	}
}

func looksSectorNetwork(query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return false
	}
	for _, probe := range []string{
		"whole industry",
		"industry supply chain",
		"sector",
		"ecosystem",
		"europe wide",
		"europe-wide",
		"brics",
		"gcc",
		"including producers",
		"value chain",
		"detailed",
		"map the supply chain",
	} {
		if strings.Contains(query, probe) {
			return true
		}
	}
	return false
}

func containsText(text string, probes ...string) bool {
	for _, probe := range probes {
		if strings.Contains(text, probe) {
			return true
		}
	}
	return false
}

func sliceMapAny(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, object)
			}
		}
		return out
	default:
		return nil
	}
}

func mapValue(raw any) map[string]any {
	if object, ok := raw.(map[string]any); ok {
		return object
	}
	return nil
}

func mapStringValue(input map[string]any, key string) string {
	if len(input) == 0 {
		return ""
	}
	if value, ok := input[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

func viewConfigPaths(viewConfig map[string]interface{}) []string {
	if len(viewConfig) == 0 {
		return nil
	}
	out := []string{}
	for _, key := range []string{"deferredScopePaths", "focusNodePaths", "highlightPaths", "spherePaths", "filterPaths"} {
		out = append(out, stringSliceFromAny(viewConfig[key])...)
	}
	return compactStringsPreserveOrder(out)
}

func stringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return compactStringsPreserveOrder(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return compactStringsPreserveOrder(out)
	default:
		return nil
	}
}

func geoFromPath(path string) string {
	parts := strings.Split(strings.TrimSpace(path), "/")
	for i, part := range parts {
		if (part == "loc" || part == "geo") && i+1 < len(parts) {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}
