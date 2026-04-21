package supplychainreport

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
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
		kind := strings.TrimSpace(firstNonEmpty(string(edge.Relation)))
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
			key := strings.TrimSpace(firstNonEmpty(string(edge.Relation)))
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

func sumCounts(items []refCount) int {
	total := 0
	for _, item := range items {
		total += item.Count
	}
	return total
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


