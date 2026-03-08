package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type kv struct {
	Key   string
	Count int
}

type fragLite struct {
	ID   string
	Path string
	Type h.FragmentType
}

type edgeLite struct {
	Type       h.DependencyType
	Qualifiers map[string]interface{}
}

type sampleLink struct {
	Path                  string
	Supplier              string
	Product               string
	Customer              string
	SupplierFacility      string
	CustomerFacility      string
	TransportVia          []string
	DirectEdgeTypes       []string
	ArtifactTriadMatched  bool
	FlowContract          string
	TransmissionQualifier bool
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	topN := flag.Int("top", 20, "top N rows")
	sampleN := flag.Int("samples", 8, "sample rows per bucket")
	flag.Parse()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	db := fs.Client()

	fragByPath := map[string]*fragLite{}
	fragByID := map[string]*fragLite{}
	supplyLinks := make([]*h.HorizonFragment, 0, 8192)

	fragIter := db.Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	for {
		doc, err := fragIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var f h.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			continue
		}
		lite := &fragLite{ID: strings.TrimSpace(f.ID), Path: strings.TrimSpace(f.CanonicalPath), Type: f.Type}
		if lite.ID != "" {
			fragByID[lite.ID] = lite
		}
		if lite.Path != "" {
			fragByPath[lite.Path] = lite
		}
		if f.Type == h.FragmentTypeSupplyLink {
			supplyLinks = append(supplyLinks, &f)
		}
	}

	edgesByPair := map[string][]edgeLite{}
	edgeIter := db.Collection("horizon_dependency_edges").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	for {
		doc, err := edgeIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var e h.DependencyEdge
		if err := doc.DataTo(&e); err != nil {
			continue
		}
		src := strings.TrimSpace(e.SourceFragmentID)
		dst := strings.TrimSpace(e.TargetFragmentID)
		if src == "" || dst == "" {
			continue
		}
		key := pairKey(src, dst)
		edgesByPair[key] = append(edgesByPair[key], edgeLite{Type: e.Type, Qualifiers: e.Qualifiers})
	}

	artifactTriads := map[string]int{}
	artifactNodeHits := map[string]int{}
	artifactIter := db.Collection("horizon_materialized_artifacts").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("kind", "==", string(h.MaterializedKindSupplyChainPath)).
		Documents(ctx)
	for {
		doc, err := artifactIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var a h.MaterializedArtifact
		if err := doc.DataTo(&a); err != nil {
			continue
		}
		for _, nodeRef := range a.NodeRefs {
			nodeRef = strings.TrimSpace(nodeRef)
			if nodeRef != "" {
				artifactNodeHits[nodeRef]++
			}
		}
		for _, key := range extractArtifactSupplyTriads(a.Payload) {
			artifactTriads[key]++
		}
	}

	supplierKinds := map[string]int{}
	customerKinds := map[string]int{}
	transportKinds := map[string]int{}
	stateKeys := map[string]int{}
	flowContractModes := map[string]int{}
	directEdgeTypes := map[string]int{}
	linkBuckets := map[string]int{}
	endpointKinds := map[string]int{}

	total := len(supplyLinks)
	withSupplierFac := 0
	withCustomerFac := 0
	withTransportVia := 0
	withDirectSupplies := 0
	withDirectAnyEdge := 0
	withTransmissionQualifier := 0
	withArtifactTriad := 0
	withArtifactNodeHit := 0
	orgGovEndpointOnly := 0
	deadLinks := make([]sampleLink, 0, *sampleN)
	richLinks := make([]sampleLink, 0, *sampleN)
	orgGovLinks := make([]sampleLink, 0, *sampleN)

	for _, link := range supplyLinks {
		if link == nil {
			continue
		}
		supplier := deref(link.SupplierRef)
		customer := deref(link.CustomerRef)
		product := deref(link.ProductRef)
		supplierFacility := deref(link.SupplierFacilityRef)
		customerFacility := deref(link.CustomerFacilityRef)

		if supplierFacility != "" {
			withSupplierFac++
		}
		if customerFacility != "" {
			withCustomerFac++
		}
		if len(link.TransportViaRefs) > 0 {
			withTransportVia++
			for _, via := range link.TransportViaRefs {
				inc(transportKinds, prefixOf(via))
			}
		}
		inc(supplierKinds, prefixOf(supplier))
		inc(customerKinds, prefixOf(customer))

		for key := range link.State {
			inc(stateKeys, key)
		}

		flowContract := flowContractStatus(link.Qualifiers)
		if flowContract != "" {
			inc(flowContractModes, flowContract)
		} else {
			inc(flowContractModes, "(missing)")
		}

		supplierEndpoint := firstNonEmpty(strings.TrimSpace(supplierFacility), strings.TrimSpace(supplier))
		customerEndpoint := firstNonEmpty(strings.TrimSpace(customerFacility), strings.TrimSpace(customer))
		inc(endpointKinds, prefixOf(supplierEndpoint)+" -> "+prefixOf(customerEndpoint))

		supplierFrag := fragByPath[supplierEndpoint]
		customerFrag := fragByPath[customerEndpoint]
		edgeTypes := make([]string, 0, 6)
		hasDirectAny := false
		hasDirectSupplies := false
		hasTransmission := false
		if supplierFrag != nil && customerFrag != nil {
			for _, edge := range edgesByPair[pairKey(supplierFrag.ID, customerFrag.ID)] {
				hasDirectAny = true
				edgeTypes = append(edgeTypes, string(edge.Type))
				inc(directEdgeTypes, string(edge.Type))
				if edge.Type == h.DependencyTypeSuppliesTo {
					hasDirectSupplies = true
				}
				if hasTransmissionQualifiers(edge.Qualifiers) {
					hasTransmission = true
				}
			}
		}
		if hasDirectAny {
			withDirectAnyEdge++
		}
		if hasDirectSupplies {
			withDirectSupplies++
		}
		if hasTransmission {
			withTransmissionQualifier++
		}

		triadMatched := artifactTriads[supplyTriadKey(supplier, product, customer)] > 0
		if triadMatched {
			withArtifactTriad++
		}
		if artifactNodeHits[supplier] > 0 || artifactNodeHits[customer] > 0 || artifactNodeHits[product] > 0 ||
			(supplierFacility != "" && artifactNodeHits[supplierFacility] > 0) ||
			(customerFacility != "" && artifactNodeHits[customerFacility] > 0) {
			withArtifactNodeHit++
		}

		orgGovOnly := isOrgGovLike(supplierEndpoint) || isOrgGovLike(customerEndpoint)
		if orgGovOnly {
			orgGovEndpointOnly++
		}

		bucket := classifyLinkBucket(hasDirectSupplies, hasDirectAny, triadMatched, orgGovOnly)
		inc(linkBuckets, bucket)

		sample := sampleLink{
			Path:                  strings.TrimSpace(link.CanonicalPath),
			Supplier:              supplier,
			Product:               product,
			Customer:              customer,
			SupplierFacility:      supplierFacility,
			CustomerFacility:      customerFacility,
			TransportVia:          append([]string(nil), link.TransportViaRefs...),
			DirectEdgeTypes:       uniqStrings(edgeTypes),
			ArtifactTriadMatched:  triadMatched,
			FlowContract:          flowContract,
			TransmissionQualifier: hasTransmission,
		}
		if bucket == "dead_link" && len(deadLinks) < *sampleN {
			deadLinks = append(deadLinks, sample)
		}
		if (len(link.TransportViaRefs) > 0 || hasTransmission || triadMatched) && len(richLinks) < *sampleN {
			richLinks = append(richLinks, sample)
		}
		if orgGovOnly && len(orgGovLinks) < *sampleN {
			orgGovLinks = append(orgGovLinks, sample)
		}
	}

	fmt.Printf("tenant=%s project=%s\nsupply_links=%d\n\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), total)
	fmt.Println("coverage:")
	fmt.Printf("  supplier_facility_ref: %d (%.1f%%)\n", withSupplierFac, pct(withSupplierFac, total))
	fmt.Printf("  customer_facility_ref: %d (%.1f%%)\n", withCustomerFac, pct(withCustomerFac, total))
	fmt.Printf("  transport_via: %d (%.1f%%)\n", withTransportVia, pct(withTransportVia, total))
	fmt.Printf("  direct_supplies_edge: %d (%.1f%%)\n", withDirectSupplies, pct(withDirectSupplies, total))
	fmt.Printf("  direct_any_edge: %d (%.1f%%)\n", withDirectAnyEdge, pct(withDirectAnyEdge, total))
	fmt.Printf("  direct_edge_with_transmission_qualifier: %d (%.1f%%)\n", withTransmissionQualifier, pct(withTransmissionQualifier, total))
	fmt.Printf("  materialized_artifact_triad: %d (%.1f%%)\n", withArtifactTriad, pct(withArtifactTriad, total))
	fmt.Printf("  materialized_artifact_node_hit: %d (%.1f%%)\n", withArtifactNodeHit, pct(withArtifactNodeHit, total))
	fmt.Printf("  org_or_gov_endpoint: %d (%.1f%%)\n\n", orgGovEndpointOnly, pct(orgGovEndpointOnly, total))

	printTop("supplier_prefixes", topCounts(supplierKinds, *topN))
	printTop("customer_prefixes", topCounts(customerKinds, *topN))
	printTop("endpoint_pairs", topCounts(endpointKinds, *topN))
	printTop("transport_via_prefixes", topCounts(transportKinds, *topN))
	printTop("direct_edge_types_between_supply_endpoints", topCounts(directEdgeTypes, *topN))
	printTop("flow_edge_contract_status", topCounts(flowContractModes, *topN))
	printTop("supply_link_state_keys", topCounts(stateKeys, *topN))
	printTop("link_buckets", topCounts(linkBuckets, *topN))

	printSamples("dead_link_samples", deadLinks)
	printSamples("rich_link_samples", richLinks)
	printSamples("org_gov_endpoint_samples", orgGovLinks)
}

func extractArtifactSupplyTriads(payload map[string]interface{}) []string {
	if payload == nil {
		return nil
	}
	nodesRaw, ok := payload["nodes"].([]interface{})
	if !ok || len(nodesRaw) == 0 {
		return nil
	}
	nodeRefs := make([]string, len(nodesRaw))
	for i, raw := range nodesRaw {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		ref := getString(m, "ref", "Ref")
		if strings.TrimSpace(ref) != "" {
			nodeRefs[i] = strings.TrimSpace(ref)
		}
	}
	edgesRaw, ok := payload["edges"].([]interface{})
	if !ok || len(edgesRaw) == 0 {
		return nil
	}
	out := make([]string, 0, len(edgesRaw))
	for _, raw := range edgesRaw {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		typ := getString(m, "type", "Type")
		if strings.TrimSpace(strings.ToUpper(typ)) != "SUPPLY_LINK" {
			continue
		}
		from := toInt(getAny(m, "from", "From"))
		to := toInt(getAny(m, "to", "To"))
		via := toOptionalInt(getAny(m, "viaProductIndex", "ViaProductIndex"))
		if from < 0 || to < 0 || via < 0 || from >= len(nodeRefs) || to >= len(nodeRefs) || via >= len(nodeRefs) {
			continue
		}
		out = append(out, supplyTriadKey(nodeRefs[from], nodeRefs[via], nodeRefs[to]))
	}
	return out
}

func getAny(m map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

func classifyLinkBucket(hasDirectSupplies, hasDirectAny, triadMatched, orgGovOnly bool) string {
	switch {
	case hasDirectSupplies && triadMatched:
		return "direct_and_artifact"
	case hasDirectSupplies:
		return "direct_only"
	case triadMatched:
		return "artifact_only"
	case hasDirectAny:
		return "edge_without_supplies_to"
	case orgGovOnly:
		return "org_gov_only_dead"
	default:
		return "dead_link"
	}
}

func flowContractStatus(q map[string]interface{}) string {
	if len(q) == 0 {
		return ""
	}
	raw, ok := q["flow_edge_contract"].(map[string]interface{})
	if !ok || len(raw) == 0 {
		return ""
	}
	if v, _ := raw["contractCompleteness"].(string); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return "present"
}

func hasTransmissionQualifiers(q map[string]interface{}) bool {
	if len(q) == 0 {
		return false
	}
	for _, key := range []string{
		h.QualifierThroughputShare,
		h.QualifierReroutePenalty,
		h.QualifierCapacityConstraint,
		"substitutability",
		h.QualifierTransportVia,
	} {
		if v, ok := q[key]; ok && !isZeroish(v) {
			return true
		}
	}
	return false
}

func isZeroish(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case float64:
		return t == 0
	case float32:
		return t == 0
	case int:
		return t == 0
	case int64:
		return t == 0
	case string:
		return strings.TrimSpace(t) == ""
	case []string:
		return len(t) == 0
	case []interface{}:
		return len(t) == 0
	default:
		return false
	}
}

func isOrgGovLike(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/gov/")
}

func supplyTriadKey(supplier, product, customer string) string {
	return strings.TrimSpace(supplier) + "|" + strings.TrimSpace(product) + "|" + strings.TrimSpace(customer)
}

func pairKey(srcID, dstID string) string {
	return strings.TrimSpace(srcID) + "->" + strings.TrimSpace(dstID)
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func printSamples(title string, samples []sampleLink) {
	fmt.Printf("%s:\n", title)
	if len(samples) == 0 {
		fmt.Println("  (none)\n")
		return
	}
	for _, s := range samples {
		fmt.Printf("  - %s\n", s.Path)
		fmt.Printf("    supplier=%s product=%s customer=%s\n", s.Supplier, s.Product, s.Customer)
		if s.SupplierFacility != "" || s.CustomerFacility != "" {
			fmt.Printf("    supplierFacility=%s customerFacility=%s\n", s.SupplierFacility, s.CustomerFacility)
		}
		if len(s.TransportVia) > 0 {
			fmt.Printf("    transportVia=%s\n", strings.Join(s.TransportVia, ", "))
		}
		if len(s.DirectEdgeTypes) > 0 {
			fmt.Printf("    directEdges=%s\n", strings.Join(s.DirectEdgeTypes, ", "))
		}
		if s.FlowContract != "" {
			fmt.Printf("    flowContract=%s\n", s.FlowContract)
		}
		fmt.Printf("    artifactTriad=%v transmissionQualifiers=%v\n", s.ArtifactTriadMatched, s.TransmissionQualifier)
	}
	fmt.Println()
}

func inc(m map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "(empty)"
	}
	m[key]++
}

func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func prefixOf(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "(empty)"
	}
	if !strings.HasPrefix(path, "/") {
		return "(raw)"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "(empty)"
	}
	return "/" + parts[0]
}

func topCounts(m map[string]int, limit int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func printTop(title string, rows []kv) {
	fmt.Printf("%s:\n", title)
	if len(rows) == 0 {
		fmt.Println("  (none)\n")
		return
	}
	for _, row := range rows {
		fmt.Printf("  %s: %d\n", row.Key, row.Count)
	}
	fmt.Println()
}

func uniqStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	default:
		return -1
	}
}

func toOptionalInt(v interface{}) int {
	if v == nil {
		return -1
	}
	return toInt(v)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
