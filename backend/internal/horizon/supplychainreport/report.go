package supplychainreport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

type refCount struct {
	Ref   string
	Label string
	Count int
}

type familySummary struct {
	Key           string
	Title         string
	ProductAxis   string
	ProductLabel  string
	SupplierRef   string
	SupplierLabel string
	CustomerRef   string
	CustomerLabel string
	PathCount     int
	LegCount      int
	TypicalVia    []string
}

type corridorSummary struct {
	Key           string
	ProductRef    string
	ProductLabel  string
	SupplierRef   string
	SupplierLabel string
	CustomerRef   string
	CustomerLabel string
	ViaRefs       []string
	ViaLabels     []string
	PathCount     int
	LegCount      int
}

type relationSummary struct {
	Key   string
	Count int
}

type pathSummary struct {
	DisplayName    string
	ProductRef     string
	ProductLabel   string
	SupplierRef    string
	SupplierLabel  string
	CustomerRef    string
	CustomerLabel  string
	ViaRefs        []string
	ViaLabels      []string
	RelationKinds  []string
	ModeKinds      []string
	LegNarratives  []string
	LegCount       int
	SourcePriority int
}

type Output struct {
	Sections []coremodels.ArtifactSection
	Markdown string
	Packages []SectionPackage
}

func Build(input *Input) Output {
	packages := BuildSectionPackages(input)
	sections := BuildSectionsFromPackages(packages, nil)
	return Output{
		Sections: sections,
		Markdown: AssembleMarkdown(sections),
		Packages: packages,
	}
}

func BuildSections(input *Input) []coremodels.ArtifactSection {
	return BuildSectionsFromPackages(BuildSectionPackages(input), nil)
}

func AssembleMarkdown(sections []coremodels.ArtifactSection) string {
	if len(sections) == 0 {
		return ""
	}
	copied := append([]coremodels.ArtifactSection(nil), sections...)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Order < copied[j].Order
	})
	var builder strings.Builder
	for _, section := range copied {
		builder.WriteString("## ")
		builder.WriteString(section.Title)
		builder.WriteString("\n\n")
		if section.Content != nil && strings.TrimSpace(*section.Content) != "" {
			builder.WriteString(strings.TrimSpace(*section.Content))
			builder.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

func buildExecutiveSection(input *Input) string {
	families := collectFamilySummaries(input)
	hubs := collectHubCounts(input)
	suppliers := collectEndpointCounts(input, "supplier")
	customers := collectEndpointCounts(input, "customer")
	corridors := collectCorridorSummaries(input)
	products := collectProductCounts(input)

	lines := []string{
		fmt.Sprintf("Lens `%s` currently resolves to **%d supply-chain paths** across **%d path families**.", safeLensName(input.Lens), len(input.Chains), len(families)),
		"",
		"| Metric | Value |",
		"| --- | --- |",
		fmt.Sprintf("| Original workbench scope paths | %d |", len(input.ScopePaths)),
		fmt.Sprintf("| Map-expanded scope paths | %d |", len(input.Resolved.ScopePaths)),
		fmt.Sprintf("| Supply-chain paths selected | %d |", len(input.Chains)),
		fmt.Sprintf("| Pinned / scope-attached paths | %d |", input.ScopedChainCount),
		fmt.Sprintf("| Context-discovered paths | %d |", input.DiscoveredChainCnt),
		fmt.Sprintf("| Distinct hubs in selected paths | %d |", len(hubs)),
		fmt.Sprintf("| Distinct suppliers | %d |", len(suppliers)),
		fmt.Sprintf("| Distinct customers | %d |", len(customers)),
		fmt.Sprintf("| Distinct product anchors | %d |", len(products)),
		"",
		"### Key takeaways",
		"",
	}

	if len(families) > 0 {
		lines = append(lines, fmt.Sprintf("- The network is organized around `%s` as the leading path family, with %d materialized paths around the same supplier-customer-product axis.", families[0].Title, families[0].PathCount))
	}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("- Hub concentration is led by %s.", joinTopCounts(hubs, 3)))
	}
	if len(corridors) > 0 {
		lines = append(lines, fmt.Sprintf("- The most repeated corridor is %s.", corridorSentence(corridors[0])))
	}
	if len(products) > 0 {
		lines = append(lines, fmt.Sprintf("- Product concentration is led by %s.", joinTopCounts(products, 3)))
	}
	if len(customers) > 0 && len(suppliers) > 0 {
		lines = append(lines, fmt.Sprintf("- Demand-side concentration is most visible at %s, while supply-side concentration is most visible at %s.", joinTopCounts(customers, 2), joinTopCounts(suppliers, 2)))
	}
	lines = append(lines, "- This version intentionally prioritizes graph semantics: named objects, relation meanings, products, hubs, and corridor recurrence. Timing and stress metrics are suppressed from the narrative layer.")

	return strings.Join(lines, "\n")
}

func buildCoverageSection(input *Input) string {
	artifactCounts := map[string]int{}
	for _, artifact := range input.AllMaterialized {
		if artifact == nil {
			continue
		}
		artifactCounts[string(artifact.Kind)]++
	}
	lines := []string{
		"| Coverage element | Count |",
		"| --- | --- |",
		fmt.Sprintf("| Original workbench scope paths | %d |", len(input.ScopePaths)),
		fmt.Sprintf("| Map-expanded scope paths | %d |", len(input.Resolved.ScopePaths)),
		fmt.Sprintf("| Neighbor fragment IDs | %d |", len(input.Resolved.FragmentIDs)),
		fmt.Sprintf("| Materialized artifacts in resolved work scope | %d |", len(input.AllMaterialized)),
		fmt.Sprintf("| Supply-chain paths selected for report | %d |", len(input.Chains)),
		fmt.Sprintf("| Resolved fragment labels | %d |", len(input.FragmentsByPath)),
		"",
		"### Scope notes",
		"",
		"- Scope expansion uses the same wider-object resolver as the map lens, including one-hop graph neighbors.",
		fmt.Sprintf("- Work scope digest: `%s`.", workscope.Digest(input.Resolved)),
	}
	if len(input.Resolved.ScopePaths) > len(input.ScopePaths) {
		lines = append(lines, fmt.Sprintf("- Map expansion added **%d** extra canonical paths beyond the original lens scope.", len(input.Resolved.ScopePaths)-len(input.ScopePaths)))
	}
	lines = append(lines, "", "### Materialized substrate", "")
	if len(artifactCounts) == 0 {
		lines = append(lines, "- No materialized artifacts were returned for the current work scope.")
	} else {
		lines = append(lines, "| Kind | Count |", "| --- | --- |")
		for _, kind := range sortedKeysByCount(artifactCounts) {
			lines = append(lines, fmt.Sprintf("| `%s` | %d |", kind, artifactCounts[kind]))
		}
	}
	return strings.Join(lines, "\n")
}

func buildStructureSection(input *Input) string {
	families := collectFamilySummaries(input)
	relations := collectRelationSummaries(input)
	modes := collectModeCounts(input)

	lines := []string{"### Path families", ""}
	if len(families) == 0 {
		lines = append(lines, "- No reusable path families were derived from the current chain set.")
	} else {
		lines = append(lines, "| Family | Product axis | Supplier anchor | Customer anchor | Paths | Typical hubs |", "| --- | --- | --- | --- | --- | --- |")
		for _, family := range families[:minInt(len(families), 8)] {
			lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %d | %s |",
				markdownEscape(family.Title),
				markdownEscape(firstNonEmpty(family.ProductLabel, family.ProductAxis, "n/a")),
				markdownEscape(firstNonEmpty(family.SupplierLabel, family.SupplierRef, "n/a")),
				markdownEscape(firstNonEmpty(family.CustomerLabel, family.CustomerRef, "n/a")),
				family.PathCount,
				markdownEscape(firstNonEmpty(strings.Join(family.TypicalVia, " -> "), "direct / not explicit")),
			))
		}
	}

	lines = append(lines, "", "### Relation meanings", "")
	if len(relations) == 0 {
		lines = append(lines, "- No reusable edge relation semantics were present beyond path legs.")
	} else {
		lines = append(lines, "| Relation / role | Occurrences |", "| --- | --- |")
		for _, item := range relations[:minInt(len(relations), 8)] {
			lines = append(lines, fmt.Sprintf("| %s | %d |", markdownEscape(item.Key), item.Count))
		}
	}

	lines = append(lines, "", "### Transport semantics", "")
	if len(modes) == 0 {
		lines = append(lines, "- No explicit leg mode or spatial semantics were encoded on the selected paths.")
	} else {
		lines = append(lines, "| Mode / semantic | Legs |", "| --- | --- |")
		for _, item := range modes[:minInt(len(modes), 8)] {
			lines = append(lines, fmt.Sprintf("| %s | %d |", markdownEscape(item.Key), item.Count))
		}
	}
	return strings.Join(lines, "\n")
}

func buildCorridorsSection(input *Input) string {
	corridors := collectCorridorSummaries(input)
	paths := collectRepresentativePaths(input)

	lines := []string{"### Repeated corridors", ""}
	if len(corridors) == 0 {
		lines = append(lines, "- No recurring corridor patterns were derived from the selected paths.")
	} else {
		lines = append(lines, "| Product | From | Via | To | Paths | Avg legs |", "| --- | --- | --- | --- | --- | --- |")
		for _, corridor := range corridors[:minInt(len(corridors), 10)] {
			lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %d | %.1f |",
				markdownEscape(firstNonEmpty(corridor.ProductLabel, corridor.ProductRef, "n/a")),
				markdownEscape(firstNonEmpty(corridor.SupplierLabel, corridor.SupplierRef, "n/a")),
				markdownEscape(firstNonEmpty(strings.Join(corridor.ViaLabels, " -> "), "direct / not explicit")),
				markdownEscape(firstNonEmpty(corridor.CustomerLabel, corridor.CustomerRef, "n/a")),
				corridor.PathCount,
				float64(corridor.LegCount)/float64(maxInt(corridor.PathCount, 1)),
			))
		}
	}

	lines = append(lines, "", "### Representative path narratives", "")
	if len(paths) == 0 {
		lines = append(lines, "- No representative path narratives were derived.")
	} else {
		for _, path := range paths[:minInt(len(paths), 6)] {
			headline := firstNonEmpty(path.DisplayName, path.ProductLabel, path.ProductRef, "Unnamed path")
			lines = append(lines, fmt.Sprintf("- `%s`: %s.", headline, pathSentence(path)))
			if len(path.LegNarratives) > 0 {
				lines = append(lines, fmt.Sprintf("  Legs: %s.", strings.Join(path.LegNarratives[:minInt(len(path.LegNarratives), 4)], " | ")))
			}
			if len(path.RelationKinds) > 0 {
				lines = append(lines, fmt.Sprintf("  Semantics: %s.", strings.Join(path.RelationKinds[:minInt(len(path.RelationKinds), 4)], ", ")))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func buildConcentrationSection(input *Input) string {
	hubs := collectHubCounts(input)
	suppliers := collectEndpointCounts(input, "supplier")
	customers := collectEndpointCounts(input, "customer")
	products := collectProductCounts(input)
	zones := collectZoneCounts(input)

	lines := []string{"### Hub concentration", ""}
	if len(hubs) == 0 {
		lines = append(lines, "- No reusable hub references were encoded on the selected paths.")
	} else {
		lines = append(lines, "| Hub | Paths touching hub |", "| --- | --- |")
		for _, item := range hubs[:minInt(len(hubs), 8)] {
			lines = append(lines, fmt.Sprintf("| %s | %d |", markdownEscape(firstNonEmpty(item.Label, item.Ref)), item.Count))
		}
	}

	lines = append(lines, "", "### Endpoint concentration", "")
	lines = append(lines, "| Dimension | Leaders |", "| --- | --- |")
	lines = append(lines, fmt.Sprintf("| Suppliers | %s |", markdownEscape(joinTopCounts(suppliers, 4))))
	lines = append(lines, fmt.Sprintf("| Customers | %s |", markdownEscape(joinTopCounts(customers, 4))))
	lines = append(lines, fmt.Sprintf("| Products | %s |", markdownEscape(joinTopCounts(products, 4))))
	lines = append(lines, fmt.Sprintf("| Zones / affected zones | %s |", markdownEscape(joinTopCounts(zones, 4))))

	lines = append(lines, "", "### Dependency readout", "")
	for _, sentence := range buildDependencyReadout(hubs, suppliers, customers, products) {
		lines = append(lines, "- "+sentence)
	}
	return strings.Join(lines, "\n")
}

func buildAppendixSection(input *Input) string {
	lines := []string{
		"| Deterministic basis | Value |",
		"| --- | --- |",
		fmt.Sprintf("| Lens ID | `%s` |", input.Lens.ID),
		fmt.Sprintf("| Project ID | `%s` |", input.Lens.ProjectID),
		fmt.Sprintf("| Generated at (UTC) | `%s` |", input.CreatedAt.Format(time.RFC3339)),
		fmt.Sprintf("| Work scope digest | `%s` |", workscope.Digest(input.Resolved)),
		fmt.Sprintf("| Scope paths | %d |", len(input.Resolved.ScopePaths)),
		fmt.Sprintf("| Materialized artifact IDs | %d |", len(input.Resolved.MaterializedArtifactIDs)),
		"",
		"### Scope paths",
		"",
	}
	if len(input.Resolved.ScopePaths) == 0 {
		lines = append(lines, "- No canonical scope paths were resolved.")
	} else {
		for _, path := range input.Resolved.ScopePaths[:minInt(len(input.Resolved.ScopePaths), 40)] {
			lines = append(lines, fmt.Sprintf("- `%s`", path))
		}
		if len(input.Resolved.ScopePaths) > 40 {
			lines = append(lines, fmt.Sprintf("- ... and %d more", len(input.Resolved.ScopePaths)-40))
		}
	}
	lines = append(lines, "", "### Materialized artifact IDs", "")
	if len(input.Resolved.MaterializedArtifactIDs) == 0 {
		lines = append(lines, "- No materialized artifact IDs were resolved.")
	} else {
		for _, id := range input.Resolved.MaterializedArtifactIDs[:minInt(len(input.Resolved.MaterializedArtifactIDs), 30)] {
			lines = append(lines, fmt.Sprintf("- `%s`", id))
		}
		if len(input.Resolved.MaterializedArtifactIDs) > 30 {
			lines = append(lines, fmt.Sprintf("- ... and %d more", len(input.Resolved.MaterializedArtifactIDs)-30))
		}
	}
	return strings.Join(lines, "\n")
}

func collectFamilySummaries(input *Input) []familySummary {
	byKey := map[string]*familySummary{}
	for _, artifact := range input.Chains {
		if artifact == nil {
			continue
		}
		horizonmodels.PopulateSupplyChainPathIndexes(artifact)
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		summary := summarizePath(artifact, parsed, input.FragmentsByPath)
		key := strings.TrimSpace(artifact.FamilyID)
		if key == "" && parsed.Family != nil {
			key = strings.TrimSpace(parsed.Family.ID)
		}
		if key == "" {
			key = strings.TrimSpace(firstNonEmpty(summary.ProductRef, artifact.ProductAxis, summary.SupplierRef+"->"+summary.CustomerRef))
		}
		entry := byKey[key]
		if entry == nil {
			entry = &familySummary{
				Key:           key,
				ProductAxis:   strings.TrimSpace(firstNonEmpty(artifact.ProductAxis, summary.ProductRef)),
				ProductLabel:  summary.ProductLabel,
				SupplierRef:   summary.SupplierRef,
				SupplierLabel: summary.SupplierLabel,
				CustomerRef:   summary.CustomerRef,
				CustomerLabel: summary.CustomerLabel,
			}
			byKey[key] = entry
		}
		entry.PathCount++
		entry.LegCount += summary.LegCount
		entry.TypicalVia = mergeUniqueStrings(entry.TypicalVia, summary.ViaLabels)
		entry.Title = familyTitle(key, entry.ProductAxis, entry.ProductLabel, entry.SupplierLabel, entry.CustomerLabel)
	}
	out := make([]familySummary, 0, len(byKey))
	for _, entry := range byKey {
		out = append(out, *entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PathCount == out[j].PathCount {
			return out[i].Title < out[j].Title
		}
		return out[i].PathCount > out[j].PathCount
	})
	return out
}

func collectCorridorSummaries(input *Input) []corridorSummary {
	byKey := map[string]*corridorSummary{}
	for _, artifact := range input.Chains {
		if artifact == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		summary := summarizePath(artifact, parsed, input.FragmentsByPath)
		key := strings.Join([]string{
			summary.SupplierRef,
			strings.Join(summary.ViaRefs, "->"),
			summary.CustomerRef,
			summary.ProductRef,
		}, "|")
		entry := byKey[key]
		if entry == nil {
			entry = &corridorSummary{
				Key:           key,
				ProductRef:    summary.ProductRef,
				ProductLabel:  summary.ProductLabel,
				SupplierRef:   summary.SupplierRef,
				SupplierLabel: summary.SupplierLabel,
				CustomerRef:   summary.CustomerRef,
				CustomerLabel: summary.CustomerLabel,
				ViaRefs:       append([]string(nil), summary.ViaRefs...),
				ViaLabels:     append([]string(nil), summary.ViaLabels...),
			}
			byKey[key] = entry
		}
		entry.PathCount++
		entry.LegCount += summary.LegCount
	}
	out := make([]corridorSummary, 0, len(byKey))
	for _, entry := range byKey {
		out = append(out, *entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PathCount == out[j].PathCount {
			return out[i].Key < out[j].Key
		}
		return out[i].PathCount > out[j].PathCount
	})
	return out
}

func collectRepresentativePaths(input *Input) []pathSummary {
	out := make([]pathSummary, 0, len(input.Chains))
	for _, artifact := range input.Chains {
		if artifact == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		summary := summarizePath(artifact, parsed, input.FragmentsByPath)
		summary.SourcePriority = 0
		if artifact.IsPinned {
			summary.SourcePriority = 2
		} else if artifact.OverlapScore > 0 {
			summary.SourcePriority = 1
		}
		out = append(out, summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SourcePriority != out[j].SourcePriority {
			return out[i].SourcePriority > out[j].SourcePriority
		}
		if out[i].LegCount != out[j].LegCount {
			return out[i].LegCount > out[j].LegCount
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return uniqueRepresentativePaths(out)
}

func uniqueRepresentativePaths(paths []pathSummary) []pathSummary {
	seen := map[string]struct{}{}
	out := make([]pathSummary, 0, len(paths))
	for _, path := range paths {
		key := strings.Join([]string{
			path.SupplierRef,
			strings.Join(path.ViaRefs, "->"),
			path.CustomerRef,
			path.ProductRef,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func summarizePath(artifact *horizonmodels.MaterializedArtifact, parsed *horizonmodels.SupplyChainPathPayload, fragments map[string]*horizonmodels.HorizonFragment) pathSummary {
	summary := pathSummary{
		DisplayName: strings.TrimSpace(firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey, artifact.ID)),
	}
	if len(parsed.Legs) > 0 {
		firstLeg := parsed.Legs[0]
		lastLeg := parsed.Legs[len(parsed.Legs)-1]
		summary.SupplierRef = strings.TrimSpace(firstNonEmpty(artifact.AnchorSupplier, firstLeg.SupplierRef))
		summary.CustomerRef = strings.TrimSpace(firstNonEmpty(artifact.AnchorCustomer, lastLeg.CustomerRef))
	}
	productCounts := map[string]int{}
	relationCounts := map[string]int{}
	modeCounts := map[string]int{}
	viaRefs := []string{}
	for _, leg := range parsed.Legs {
		summary.LegCount++
		if productRef := strings.TrimSpace(leg.ProductRef); productRef != "" {
			productCounts[productRef]++
		}
		if mode := strings.TrimSpace(firstNonEmpty(leg.Mode, strings.Join(leg.SpatialSemantics, "/"))); mode != "" {
			modeCounts[mode]++
		}
		viaRefs = mergeUniqueStrings(viaRefs, leg.TransportViaRefs)
		legText := fmt.Sprintf("%s -> %s", labelForRef(leg.SupplierRef, fragments), labelForRef(leg.CustomerRef, fragments))
		if len(leg.TransportViaRefs) > 0 {
			legText += " via " + strings.Join(labelsForRefs(leg.TransportViaRefs, fragments), " -> ")
		}
		if productRef := strings.TrimSpace(leg.ProductRef); productRef != "" {
			legText += " carrying " + labelForRef(productRef, fragments)
		}
		summary.LegNarratives = append(summary.LegNarratives, legText)
	}
	for _, edge := range parsed.Edges {
		kind := strings.TrimSpace(firstNonEmpty(edge.Relation, edge.Role, edge.Type))
		if kind != "" {
			relationCounts[kind]++
		}
		if edge.ProductRef != "" && strings.TrimSpace(edge.ProductRef) != "" {
			productCounts[strings.TrimSpace(edge.ProductRef)]++
		}
	}
	summary.ProductRef = topKey(productCounts)
	if summary.ProductRef == "" {
		summary.ProductRef = strings.TrimSpace(artifact.ProductAxis)
	}
	summary.ProductLabel = labelForRef(summary.ProductRef, fragments)
	summary.SupplierLabel = labelForRef(summary.SupplierRef, fragments)
	summary.CustomerLabel = labelForRef(summary.CustomerRef, fragments)
	summary.ViaRefs = append([]string(nil), viaRefs...)
	summary.ViaLabels = labelsForRefs(viaRefs, fragments)
	summary.RelationKinds = sortedSummaryKeys(relationCounts)
	summary.ModeKinds = sortedSummaryKeys(modeCounts)
	return summary
}

func collectHubCounts(input *Input) []refCount {
	counts := map[string]int{}
	for _, artifact := range input.Chains {
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		perPath := map[string]struct{}{}
		for _, leg := range parsed.Legs {
			for _, ref := range leg.TransportViaRefs {
				ref = strings.TrimSpace(ref)
				if ref != "" {
					perPath[ref] = struct{}{}
				}
			}
		}
		for ref := range perPath {
			counts[ref]++
		}
	}
	return sortRefCounts(counts, input.FragmentsByPath)
}

func collectEndpointCounts(input *Input, side string) []refCount {
	counts := map[string]int{}
	for _, artifact := range input.Chains {
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil || len(parsed.Legs) == 0 {
			continue
		}
		ref := ""
		if side == "customer" {
			ref = strings.TrimSpace(firstNonEmpty(artifact.AnchorCustomer, parsed.Legs[len(parsed.Legs)-1].CustomerRef))
		} else {
			ref = strings.TrimSpace(firstNonEmpty(artifact.AnchorSupplier, parsed.Legs[0].SupplierRef))
		}
		if ref != "" {
			counts[ref]++
		}
	}
	return sortRefCounts(counts, input.FragmentsByPath)
}

func collectProductCounts(input *Input) []refCount {
	counts := map[string]int{}
	for _, artifact := range input.Chains {
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		perPath := map[string]struct{}{}
		for _, leg := range parsed.Legs {
			ref := strings.TrimSpace(leg.ProductRef)
			if ref != "" {
				perPath[ref] = struct{}{}
			}
		}
		for _, edge := range parsed.Edges {
			ref := strings.TrimSpace(edge.ProductRef)
			if ref != "" {
				perPath[ref] = struct{}{}
			}
		}
		if len(perPath) == 0 && strings.TrimSpace(artifact.ProductAxis) != "" {
			perPath[strings.TrimSpace(artifact.ProductAxis)] = struct{}{}
		}
		for ref := range perPath {
			counts[ref]++
		}
	}
	return sortRefCounts(counts, input.FragmentsByPath)
}

func collectZoneCounts(input *Input) []refCount {
	counts := map[string]int{}
	for _, artifact := range input.Chains {
		if artifact == nil {
			continue
		}
		perPath := map[string]struct{}{}
		for _, ref := range artifact.ZoneRefs {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				perPath[ref] = struct{}{}
			}
		}
		for _, ref := range artifact.AffectedZoneRefs {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				perPath[ref] = struct{}{}
			}
		}
		for ref := range perPath {
			counts[ref]++
		}
	}
	return sortRefCounts(counts, input.FragmentsByPath)
}

func collectRelationSummaries(input *Input) []relationSummary {
	counts := map[string]int{}
	for _, artifact := range input.Chains {
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, edge := range parsed.Edges {
			key := strings.TrimSpace(firstNonEmpty(edge.Relation, edge.Role, edge.Type))
			if key != "" {
				counts[key]++
			}
		}
	}
	out := make([]relationSummary, 0, len(counts))
	for key, count := range counts {
		out = append(out, relationSummary{Key: key, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func collectModeCounts(input *Input) []relationSummary {
	counts := map[string]int{}
	for _, artifact := range input.Chains {
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			if key := strings.TrimSpace(leg.Mode); key != "" {
				counts[key]++
			}
			for _, semantic := range leg.SpatialSemantics {
				semantic = strings.TrimSpace(semantic)
				if semantic != "" {
					counts[semantic]++
				}
			}
		}
	}
	out := make([]relationSummary, 0, len(counts))
	for key, count := range counts {
		out = append(out, relationSummary{Key: key, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func sortRefCounts(counts map[string]int, fragments map[string]*horizonmodels.HorizonFragment) []refCount {
	out := make([]refCount, 0, len(counts))
	for ref, count := range counts {
		out = append(out, refCount{
			Ref:   ref,
			Label: labelForRef(ref, fragments),
			Count: count,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return firstNonEmpty(out[i].Label, out[i].Ref) < firstNonEmpty(out[j].Label, out[j].Ref)
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func buildDependencyReadout(hubs, suppliers, customers, products []refCount) []string {
	out := []string{}
	if len(hubs) > 0 {
		out = append(out, fmt.Sprintf("The leading hub `%s` appears on %d of %d counted hub-touch events, making it the clearest relay dependency in the visible graph.", firstNonEmpty(hubs[0].Label, hubs[0].Ref), hubs[0].Count, sumCounts(hubs)))
	}
	if len(suppliers) > 0 {
		out = append(out, fmt.Sprintf("Supply-side concentration starts with `%s`, which anchors %d materialized paths in the current selection.", firstNonEmpty(suppliers[0].Label, suppliers[0].Ref), suppliers[0].Count))
	}
	if len(customers) > 0 {
		out = append(out, fmt.Sprintf("Demand-side concentration starts with `%s`, which receives %d materialized paths in the current selection.", firstNonEmpty(customers[0].Label, customers[0].Ref), customers[0].Count))
	}
	if len(products) > 0 {
		out = append(out, fmt.Sprintf("Product recurrence is led by `%s`, which appears in %d paths and therefore acts as the main organizing axis for the visible network.", firstNonEmpty(products[0].Label, products[0].Ref), products[0].Count))
	}
	if len(out) == 0 {
		out = append(out, "No strong concentration points were explicit in the selected path set.")
	}
	return out
}

func pathSentence(path pathSummary) string {
	parts := []string{}
	if path.SupplierLabel != "" && path.CustomerLabel != "" {
		parts = append(parts, fmt.Sprintf("%s flows from %s to %s", firstNonEmpty(path.ProductLabel, "product"), path.SupplierLabel, path.CustomerLabel))
	}
	if len(path.ViaLabels) > 0 {
		parts = append(parts, "via "+strings.Join(path.ViaLabels, " -> "))
	}
	if len(path.ModeKinds) > 0 {
		parts = append(parts, "using "+strings.Join(path.ModeKinds[:minInt(len(path.ModeKinds), 3)], ", "))
	}
	return strings.Join(parts, ", ")
}

func corridorSentence(c corridorSummary) string {
	out := fmt.Sprintf("`%s` from `%s` to `%s`", firstNonEmpty(c.ProductLabel, c.ProductRef, "product"), firstNonEmpty(c.SupplierLabel, c.SupplierRef, "supplier"), firstNonEmpty(c.CustomerLabel, c.CustomerRef, "customer"))
	if len(c.ViaLabels) > 0 {
		out += " via `" + strings.Join(c.ViaLabels, " -> ") + "`"
	}
	out += fmt.Sprintf(" (%d paths)", c.PathCount)
	return out
}

func familyTitle(key string, productAxis string, productLabel string, supplierLabel string, customerLabel string) string {
	if product := firstNonEmpty(productLabel, bestEffortLabel(productAxis)); product != "" && supplierLabel != "" && customerLabel != "" {
		return product + ": " + supplierLabel + " -> " + customerLabel
	}
	if product := firstNonEmpty(productLabel, bestEffortLabel(productAxis)); product != "" {
		return product
	}
	if strings.HasPrefix(strings.TrimSpace(key), "/") {
		return bestEffortLabel(key)
	}
	return strings.TrimSpace(key)
}

func labelsForRefs(refs []string, fragments map[string]*horizonmodels.HorizonFragment) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		label := labelForRef(ref, fragments)
		if label != "" {
			out = append(out, label)
		}
	}
	return mergeUniqueStrings(nil, out)
}

func labelForRef(ref string, fragments map[string]*horizonmodels.HorizonFragment) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if fragment := fragments[ref]; fragment != nil {
		if name := strings.TrimSpace(fragment.Labels["en"]); name != "" {
			return name
		}
		if fragment.Identifiers != nil {
			if name := strings.TrimSpace(fragment.Identifiers["legalName"]); name != "" {
				return name
			}
			if name := strings.TrimSpace(fragment.Identifiers["shortName"]); name != "" {
				return name
			}
		}
		for _, value := range fragment.Labels {
			if name := strings.TrimSpace(value); name != "" {
				return name
			}
		}
	}
	return bestEffortLabel(ref)
}

func bestEffortLabel(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	return naming.BestEffortLabelFromCanonicalPath(ref)
}

func topKey(counts map[string]int) string {
	top := ""
	topCount := 0
	for key, count := range counts {
		if count > topCount || (count == topCount && (top == "" || key < top)) {
			top = key
			topCount = count
		}
	}
	return top
}

func sortedSummaryKeys(counts map[string]int) []string {
	items := make([]relationSummary, 0, len(counts))
	for key, count := range counts {
		items = append(items, relationSummary{Key: key, Count: count})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Key < items[j].Key
		}
		return items[i].Count > items[j].Count
	})
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Key)
	}
	return out
}

func sortedKeysByCount(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if counts[keys[i]] == counts[keys[j]] {
			return keys[i] < keys[j]
		}
		return counts[keys[i]] > counts[keys[j]]
	})
	return keys
}

func joinTopCounts(items []refCount, limit int) string {
	if len(items) == 0 || limit <= 0 {
		return "n/a"
	}
	parts := make([]string, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		parts = append(parts, fmt.Sprintf("`%s` (%d)", firstNonEmpty(item.Label, item.Ref), item.Count))
	}
	return strings.Join(parts, ", ")
}

func safeLensName(lens *horizonmodels.Lens) string {
	if lens == nil {
		return "Lens"
	}
	return firstNonEmpty(strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID), "Lens")
}

func markdownEscape(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("|", "\\|", "\n", " ")
	return replacer.Replace(value)
}

func mergeUniqueStrings(base []string, values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(values))
	for _, value := range append(append([]string(nil), base...), values...) {
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

func uniqueTrimmedStrings(values []string) []string {
	return mergeUniqueStrings(nil, values)
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sumCounts(items []refCount) int {
	total := 0
	for _, item := range items {
		total += item.Count
	}
	return total
}
