package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type capabilityKey struct {
	CarrierRef string
	ProductRef string
}

type riskKey struct {
	ScopeRef   string
	ProductRef string
}

type metricSummary struct {
	Keys       []string `json:"keys,omitempty"`
	Count      int      `json:"count"`
	NonEmpty   int      `json:"nonEmpty"`
	WithUnit   int      `json:"withUnit"`
	WithValues int      `json:"withValues"`
}

type workingProductScopeSummary struct {
	Found            bool                     `json:"found"`
	CanonicalRefs    []string                 `json:"canonicalRefs,omitempty"`
	RejectedRefs     []string                 `json:"rejectedRefs,omitempty"`
	Entries          []map[string]interface{} `json:"entries,omitempty"`
	LastExpandedAt   string                   `json:"lastExpandedAt,omitempty"`
	LastValidatedAt  string                   `json:"lastValidatedAt,omitempty"`
	CanonicalCount   int                      `json:"canonicalCount"`
	ConfirmedCount   int                      `json:"confirmedCount"`
	PartialCoverageN int                      `json:"partialCoverageCount"`
}

type supplyLinkFragmentSummary struct {
	CanonicalPath         string                 `json:"canonicalPath"`
	ProductRef            string                 `json:"productRef,omitempty"`
	SupplierRef           string                 `json:"supplierRef,omitempty"`
	CustomerRef           string                 `json:"customerRef,omitempty"`
	TransportViaRefs      []string               `json:"transportViaRefs,omitempty"`
	StateKeys             []string               `json:"stateKeys,omitempty"`
	Qualifiers            map[string]interface{} `json:"qualifiers,omitempty"`
	SupplyCriticality     float64                `json:"supplyCriticality,omitempty"`
	HasFlowEdgeContract   bool                   `json:"hasFlowEdgeContract"`
	HasTransportVia       bool                   `json:"hasTransportVia"`
	HasRouteOrCustomsData bool                   `json:"hasRouteOrCustomsData"`
}

type capabilityProfileSummary struct {
	CarrierRef      string   `json:"carrierRef"`
	ProductRef      string   `json:"productRef"`
	MetricKeys      []string `json:"metricKeys,omitempty"`
	QualifierKeys   []string `json:"qualifierKeys,omitempty"`
	CoverageStatus  string   `json:"coverageStatus,omitempty"`
	LastObservedAt  string   `json:"lastObservedAt,omitempty"`
	LastRolledUpAt  string   `json:"lastRolledUpAt,omitempty"`
	HasLeadTime     bool     `json:"hasLeadTime"`
	HasInventory    bool     `json:"hasInventory"`
	HasSubstitution bool     `json:"hasSubstitution"`
}

type derivedRiskSummary struct {
	ScopeRef      string   `json:"scopeRef"`
	ProductRef    string   `json:"productRef,omitempty"`
	ProfileTypes  []string `json:"profileTypes,omitempty"`
	PrimaryMetric []string `json:"primaryMetricKeys,omitempty"`
}

type pathArtifactSummary struct {
	CanonicalKey      string   `json:"canonicalKey"`
	PathStatus        string   `json:"pathStatus,omitempty"`
	NodeRefsN         int      `json:"nodeRefsN"`
	LegsN             int      `json:"legsN"`
	LegProducts       []string `json:"legProducts,omitempty"`
	TransportViaRefs  []string `json:"transportViaRefs,omitempty"`
	VesselTypes       []string `json:"vesselTypes,omitempty"`
	StressScore       float64  `json:"stressScore,omitempty"`
	BottleneckLeg     int      `json:"bottleneckLegIndex,omitempty"`
	HasRouteBindings  bool     `json:"hasRouteBindings"`
	HasTransportVia   bool     `json:"hasTransportVia"`
	HasStress         bool     `json:"hasStress"`
	HasOperationalLeg bool     `json:"hasOperationalLeg"`
}

type inspectionReport struct {
	JobID                 string                      `json:"jobId"`
	ProjectID             string                      `json:"projectId"`
	TenantID              string                      `json:"tenantId"`
	Status                string                      `json:"status"`
	CurrentState          string                      `json:"currentState"`
	TargetCanonicalPath   string                      `json:"targetCanonicalPath"`
	GraphBuildMode        string                      `json:"graphBuildMode,omitempty"`
	SupplyChainSummary    map[string]interface{}      `json:"supplyChainSummary,omitempty"`
	SupplyChainMaterialization map[string]interface{} `json:"supplyChainMaterialization,omitempty"`
	SupplyChainAssembly   map[string]interface{}      `json:"supplyChainAssembly,omitempty"`
	WorkingProductScope   workingProductScopeSummary  `json:"workingProductScope"`
	CapabilityObsCount    int                         `json:"capabilityObservationCount"`
	SupplyLinkObsCount    int                         `json:"supplyLinkObservationCount"`
	RouteObsCount         int                         `json:"routeObservationCount"`
	CapabilityMetrics     metricSummary               `json:"capabilityMetrics"`
	SupplyLinkMetrics     metricSummary               `json:"supplyLinkMetrics"`
	RouteQualifierKeys    []string                    `json:"routeQualifierKeys,omitempty"`
	SupplyLinkFragments   []supplyLinkFragmentSummary `json:"supplyLinkFragments,omitempty"`
	CapabilityProfiles    []capabilityProfileSummary  `json:"capabilityProfiles,omitempty"`
	DerivedRiskProfiles   []derivedRiskSummary        `json:"derivedRiskProfiles,omitempty"`
	AssemblyArtifactKey   string                      `json:"assemblyArtifactKey,omitempty"`
	AssemblyDerivedPathsN int                         `json:"assemblyDerivedPathsCount"`
	AssemblyOpenGaps      []string                    `json:"assemblyOpenGaps,omitempty"`
	PathArtifacts         []pathArtifactSummary       `json:"pathArtifacts,omitempty"`
	Assessment            map[string]interface{}      `json:"assessment,omitempty"`
	GeneratedAt           string                      `json:"generatedAt"`
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	jobID := flag.String("job", "", "scouting job id")
	jsonOut := flag.Bool("json", true, "emit json")
	flag.Parse()

	if strings.TrimSpace(*jobID) == "" {
		log.Fatal("--job is required")
	}

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()
	db := fs.Client()

	agentJobsRepo := corerepo.NewAgentJobsRepository(db)
	scoutingJobsRepo := hrepo.NewScoutingJobsRepository(agentJobsRepo)
	workingScopesRepo := hrepo.NewWorkingProductScopesRepository(db)
	capabilityProfilesRepo := hrepo.NewCapabilityProfilesRepository(db)
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(db)

	job, err := scoutingJobsRepo.Get(ctx, strings.TrimSpace(*jobID))
	if err != nil {
		log.Fatalf("load job failed: %v", err)
	}

	report := inspectionReport{
		JobID:               strings.TrimSpace(job.ID),
		ProjectID:           strings.TrimSpace(job.ProjectID),
		TenantID:            strings.TrimSpace(firstNonEmpty(job.TenantID, "global")),
		Status:              strings.TrimSpace(string(job.Status)),
		CurrentState:        strings.TrimSpace(firstNonEmpty(string(job.CurrentState), asString(job.State["currentState"]))),
		TargetCanonicalPath: strings.TrimSpace(job.TargetCanonicalPath),
		GraphBuildMode:      strings.TrimSpace(asString(job.State["supplyChainGraphBuildMode"])),
		SupplyChainSummary:  normalizeMap(job.State["supplyChainSummary"]),
		SupplyChainMaterialization: normalizeMap(job.State["supplyChainMaterialization"]),
		SupplyChainAssembly: normalizeMap(job.State["supplyChainAssembly"]),
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	scope, _ := workingScopesRepo.GetByJobID(ctx, strings.TrimSpace(job.ID))
	if scope == nil {
		scope = loadWorkingProductScopeFromJobState(job.State)
	}
	report.WorkingProductScope = summarizeWorkingProductScope(scope)

	capObs, err := loadBySourceID[hmodels.CapabilityObservation](ctx, db.Collection("horizon_capability_observations"), strings.TrimSpace(job.ID))
	if err != nil {
		log.Fatalf("load capability observations failed: %v", err)
	}
	linkObs, err := loadBySourceID[hmodels.SupplyLinkObservation](ctx, db.Collection("horizon_supply_link_observations"), strings.TrimSpace(job.ID))
	if err != nil {
		log.Fatalf("load supply link observations failed: %v", err)
	}
	routeObs, err := loadBySourceID[hmodels.RouteObservation](ctx, db.Collection("horizon_route_observations"), strings.TrimSpace(job.ID))
	if err != nil {
		log.Fatalf("load route observations failed: %v", err)
	}

	report.CapabilityObsCount = len(capObs)
	report.SupplyLinkObsCount = len(linkObs)
	report.RouteObsCount = len(routeObs)
	report.CapabilityMetrics = summarizeMetricMapsCapability(capObs)
	report.SupplyLinkMetrics = summarizeMetricMapsSupply(linkObs)
	report.RouteQualifierKeys = summarizeRouteQualifierKeys(routeObs)

	capabilityKeys := make([]capabilityKey, 0, len(capObs))
	seenCapability := map[capabilityKey]struct{}{}
	for _, obs := range capObs {
		key := capabilityKey{CarrierRef: strings.TrimSpace(obs.CarrierRef), ProductRef: strings.TrimSpace(obs.ProductRef)}
		if key.CarrierRef == "" || key.ProductRef == "" {
			continue
		}
		if _, ok := seenCapability[key]; ok {
			continue
		}
		seenCapability[key] = struct{}{}
		capabilityKeys = append(capabilityKeys, key)
	}
	sort.Slice(capabilityKeys, func(i, j int) bool {
		if capabilityKeys[i].CarrierRef == capabilityKeys[j].CarrierRef {
			return capabilityKeys[i].ProductRef < capabilityKeys[j].ProductRef
		}
		return capabilityKeys[i].CarrierRef < capabilityKeys[j].CarrierRef
	})

	capabilityProfiles := make([]capabilityProfileSummary, 0, len(capabilityKeys))
	for _, key := range capabilityKeys {
		profile, err := capabilityProfilesRepo.Get(ctx, key.CarrierRef, key.ProductRef)
		if err != nil || profile == nil {
			continue
		}
		capabilityProfiles = append(capabilityProfiles, summarizeCapabilityProfile(profile))
	}
	report.CapabilityProfiles = capabilityProfiles

	supplyLinkPaths := make([]string, 0, len(linkObs))
	seenPaths := map[string]struct{}{}
	for _, obs := range linkObs {
		path := strings.TrimSpace(obs.SupplyLinkCanonicalPath)
		if path == "" {
			continue
		}
		if _, ok := seenPaths[path]; ok {
			continue
		}
		seenPaths[path] = struct{}{}
		supplyLinkPaths = append(supplyLinkPaths, path)
	}
	sort.Strings(supplyLinkPaths)

	supplyLinksByPath, err := fragmentsRepo.BatchGetSupplyLinksByCanonicalPaths(ctx, supplyLinkPaths)
	if err != nil {
		log.Fatalf("load supply link fragments failed: %v", err)
	}
	linkSummaries := make([]supplyLinkFragmentSummary, 0, len(supplyLinksByPath))
	for _, path := range supplyLinkPaths {
		frag := supplyLinksByPath[path]
		if frag == nil {
			continue
		}
		linkSummaries = append(linkSummaries, summarizeSupplyLinkFragment(frag))
	}
	if len(linkSummaries) == 0 {
		fallbackFragments, err := loadSupplyLinkFragmentsByJobSourceID(ctx, db, report.TenantID, strings.TrimSpace(job.ID))
		if err != nil {
			log.Fatalf("load fallback supply link fragments failed: %v", err)
		}
		for _, frag := range fallbackFragments {
			linkSummaries = append(linkSummaries, summarizeSupplyLinkFragment(frag))
			path := strings.TrimSpace(frag.CanonicalPath)
			if path == "" {
				continue
			}
			if _, ok := seenPaths[path]; ok {
				continue
			}
			seenPaths[path] = struct{}{}
			supplyLinkPaths = append(supplyLinkPaths, path)
		}
		sort.Strings(supplyLinkPaths)
	}
	report.SupplyLinkFragments = linkSummaries

	riskProfiles, err := loadDerivedRiskProfiles(ctx, db, supplyLinkPaths, collectObservedProducts(linkObs))
	if err != nil {
		log.Fatalf("load derived risk profiles failed: %v", err)
	}
	report.DerivedRiskProfiles = riskProfiles

	assemblyKey := strings.TrimSpace(asString(report.SupplyChainAssembly["artifactKey"]))
	report.AssemblyArtifactKey = assemblyKey
	if assemblyKey != "" {
		artifact, err := loadAssemblyArtifact(ctx, materializedRepo, report.TenantID, assemblyKey)
		if err == nil && artifact != nil {
			if parsed, perr := hmodels.ParseSupplyChainAssemblyGraphPayload(artifact.Payload); perr == nil && parsed != nil {
				report.AssemblyDerivedPathsN = len(parsed.DerivedPathKeys)
				report.AssemblyOpenGaps = append([]string(nil), parsed.OpenGaps...)
				pathArtifacts, _ := loadDerivedPathArtifacts(ctx, materializedRepo, report.TenantID, parsed.DerivedPathKeys)
				report.PathArtifacts = summarizePathArtifacts(pathArtifacts)
			}
		}
	}
	if len(report.PathArtifacts) == 0 {
		directKeys := extractDirectPathArtifactKeys(report.SupplyChainMaterialization, report.SupplyChainSummary)
		if len(directKeys) > 0 {
			pathArtifacts, _ := loadDerivedPathArtifacts(ctx, materializedRepo, report.TenantID, directKeys)
			report.PathArtifacts = summarizePathArtifacts(pathArtifacts)
		}
	}

	report.Assessment = assessReport(report)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Fatalf("encode report failed: %v", err)
		}
		return
	}
	printTextReport(report)
}

func loadBySourceID[T any](ctx context.Context, col *firestore.CollectionRef, jobID string) ([]T, error) {
	iter := col.Where("sourceIds", "array-contains", strings.TrimSpace(jobID)).Documents(ctx)
	defer iter.Stop()
	out := make([]T, 0, 32)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var item T
		if err := doc.DataTo(&item); err != nil {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func loadWorkingProductScopeFromJobState(state map[string]interface{}) *hmodels.WorkingProductScope {
	if len(state) == 0 {
		return nil
	}
	raw, ok := state["workingProductScope"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case hmodels.WorkingProductScope:
		scope := v
		return &scope
	case *hmodels.WorkingProductScope:
		if v == nil {
			return nil
		}
		scope := *v
		return &scope
	case map[string]interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var scope hmodels.WorkingProductScope
		if err := json.Unmarshal(b, &scope); err != nil {
			return nil
		}
		return &scope
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var scope hmodels.WorkingProductScope
		if err := json.Unmarshal(b, &scope); err != nil {
			return nil
		}
		return &scope
	}
}

func loadSupplyLinkFragmentsByJobSourceID(ctx context.Context, db *firestore.Client, tenantID, jobID string) ([]*hmodels.HorizonFragment, error) {
	iter := db.Collection("horizon_fragments").
		Where("tenantId", "==", strings.TrimSpace(firstNonEmpty(tenantID, "global"))).
		Where("type", "==", string(hmodels.FragmentTypeSupplyLink)).
		Where("provenance.sourceIds", "array-contains", strings.TrimSpace(jobID)).
		Documents(ctx)
	defer iter.Stop()

	out := make([]*hmodels.HorizonFragment, 0, 32)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var frag hmodels.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		out = append(out, &frag)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].CanonicalPath) < strings.TrimSpace(out[j].CanonicalPath)
	})
	return out, nil
}

func summarizeWorkingProductScope(scope *hmodels.WorkingProductScope) workingProductScopeSummary {
	if scope == nil {
		return workingProductScopeSummary{}
	}
	out := workingProductScopeSummary{
		Found:           true,
		RejectedRefs:    append([]string(nil), scope.RejectedProductRefs...),
		LastExpandedAt:  scope.LastExpandedAt.Format(time.RFC3339),
		LastValidatedAt: scope.LastValidatedAt.Format(time.RFC3339),
	}
	for _, entry := range scope.CanonicalProducts {
		ref := strings.TrimSpace(entry.ProductRef)
		if ref != "" {
			out.CanonicalRefs = append(out.CanonicalRefs, ref)
			out.CanonicalCount++
		}
		if entry.Status == hmodels.WorkingProductStatusConfirmed {
			out.ConfirmedCount++
		}
		if strings.TrimSpace(entry.CoverageStatus) == "partial" {
			out.PartialCoverageN++
		}
		out.Entries = append(out.Entries, map[string]interface{}{
			"productRef":             ref,
			"primaryOperationalRef":  strings.TrimSpace(entry.PrimaryOperationalRef),
			"equivalentProductRefs":  append([]string(nil), entry.EquivalentProductRefs...),
			"seedRefs":               append([]string(nil), entry.SeedRefs...),
			"status":                 string(entry.Status),
			"coverageStatus":         strings.TrimSpace(entry.CoverageStatus),
			"aliases":                append([]string(nil), entry.Aliases...),
			"sourceKinds":            append([]string(nil), entry.SourceKinds...),
			"discoveryReasons":       append([]string(nil), entry.DiscoveryReasons...),
			"confidence":             entry.Confidence,
		})
	}
	sort.Strings(out.CanonicalRefs)
	sort.Strings(out.RejectedRefs)
	return out
}

func summarizeMetricMapsCapability(observations []hmodels.CapabilityObservation) metricSummary {
	keys := map[string]struct{}{}
	out := metricSummary{Count: len(observations)}
	for _, obs := range observations {
		if len(obs.Metrics) > 0 {
			out.NonEmpty++
		}
		for key, mv := range obs.Metrics {
			keys[strings.TrimSpace(key)] = struct{}{}
			if mv.Unit != nil && strings.TrimSpace(*mv.Unit) != "" {
				out.WithUnit++
			}
			out.WithValues++
		}
	}
	out.Keys = sortedKeySet(keys)
	return out
}

func summarizeMetricMapsSupply(observations []hmodels.SupplyLinkObservation) metricSummary {
	keys := map[string]struct{}{}
	out := metricSummary{Count: len(observations)}
	for _, obs := range observations {
		if len(obs.Metrics) > 0 {
			out.NonEmpty++
		}
		for key, mv := range obs.Metrics {
			keys[strings.TrimSpace(key)] = struct{}{}
			if mv.Unit != nil && strings.TrimSpace(*mv.Unit) != "" {
				out.WithUnit++
			}
			out.WithValues++
		}
	}
	out.Keys = sortedKeySet(keys)
	return out
}

func summarizeRouteQualifierKeys(observations []hmodels.RouteObservation) []string {
	keys := map[string]struct{}{}
	for _, obs := range observations {
		for key := range obs.Qualifiers {
			keys[strings.TrimSpace(key)] = struct{}{}
		}
		for key := range obs.Metrics {
			keys[strings.TrimSpace(key)] = struct{}{}
		}
	}
	return sortedKeySet(keys)
}

func summarizeCapabilityProfile(profile *hmodels.CapabilityProfile) capabilityProfileSummary {
	metricKeys := make([]string, 0, len(profile.Metrics))
	for key := range profile.Metrics {
		metricKeys = append(metricKeys, strings.TrimSpace(key))
	}
	sort.Strings(metricKeys)
	qualifierKeys := make([]string, 0, len(profile.Qualifiers))
	for key := range profile.Qualifiers {
		qualifierKeys = append(qualifierKeys, strings.TrimSpace(key))
	}
	sort.Strings(qualifierKeys)
	_, hasLeadTime := profile.Metrics["lead_time_days_baseline"]
	_, hasInventory := profile.Metrics["inventory_days_baseline"]
	_, hasTechSub := profile.Metrics["technical_substitutability"]
	_, hasRegSub := profile.Metrics["regulatory_substitutability"]
	return capabilityProfileSummary{
		CarrierRef:      strings.TrimSpace(profile.CarrierRef),
		ProductRef:      strings.TrimSpace(profile.ProductRef),
		MetricKeys:      metricKeys,
		QualifierKeys:   qualifierKeys,
		CoverageStatus:  strings.TrimSpace(profile.CoverageStatus),
		LastObservedAt:  formatTime(profile.LastObservedAt),
		LastRolledUpAt:  formatTime(profile.LastRolledUpAt),
		HasLeadTime:     hasLeadTime,
		HasInventory:    hasInventory,
		HasSubstitution: hasTechSub || hasRegSub,
	}
}

func summarizeSupplyLinkFragment(frag *hmodels.HorizonFragment) supplyLinkFragmentSummary {
	stateKeys := make([]string, 0, len(frag.State))
	for key := range frag.State {
		stateKeys = append(stateKeys, strings.TrimSpace(key))
	}
	sort.Strings(stateKeys)
	summary := supplyLinkFragmentSummary{
		CanonicalPath:       strings.TrimSpace(frag.CanonicalPath),
		ProductRef:          deref(frag.ProductRef),
		SupplierRef:         deref(frag.SupplierRef),
		CustomerRef:         deref(frag.CustomerRef),
		TransportViaRefs:    append([]string(nil), frag.TransportViaRefs...),
		StateKeys:           stateKeys,
		Qualifiers:          cloneMap(frag.Qualifiers),
		HasFlowEdgeContract: hasMap(frag.Qualifiers, "flow_edge_contract"),
		HasTransportVia:     len(frag.TransportViaRefs) > 0,
	}
	if mv, ok := frag.State["supply_chain_criticality"]; ok {
		summary.SupplyCriticality = mv.Value
	}
	if _, ok := frag.State["customs_clearance_days"]; ok {
		summary.HasRouteOrCustomsData = true
	}
	if len(frag.TransportViaRefs) > 0 {
		summary.HasRouteOrCustomsData = true
	}
	return summary
}

func loadDerivedRiskProfiles(ctx context.Context, db *firestore.Client, scopeRefs []string, productRefs []string) ([]derivedRiskSummary, error) {
	col := db.Collection("horizon_derived_risk_profiles")
	productSet := map[string]struct{}{}
	for _, ref := range productRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			productSet[ref] = struct{}{}
		}
	}
	byKey := map[riskKey]*derivedRiskSummary{}
	for _, scopeRef := range scopeRefs {
		scopeRef = strings.TrimSpace(scopeRef)
		if scopeRef == "" {
			continue
		}
		iter := col.Where("scopeRef", "==", scopeRef).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				return nil, err
			}
			var profile hmodels.DerivedRiskProfile
			if err := doc.DataTo(&profile); err != nil {
				continue
			}
			productRef := strings.TrimSpace(profile.ProductRef)
			if len(productSet) > 0 {
				if _, ok := productSet[productRef]; !ok {
					continue
				}
			}
			key := riskKey{ScopeRef: strings.TrimSpace(profile.ScopeRef), ProductRef: productRef}
			if _, ok := byKey[key]; !ok {
				byKey[key] = &derivedRiskSummary{
					ScopeRef:   key.ScopeRef,
					ProductRef: key.ProductRef,
				}
			}
			summary := byKey[key]
			summary.ProfileTypes = append(summary.ProfileTypes, strings.TrimSpace(profile.ProfileType))
			for metricKey := range profile.Metrics {
				summary.PrimaryMetric = append(summary.PrimaryMetric, strings.TrimSpace(metricKey))
			}
		}
		iter.Stop()
	}
	out := make([]derivedRiskSummary, 0, len(byKey))
	for _, summary := range byKey {
		summary.ProfileTypes = uniqueSorted(summary.ProfileTypes)
		summary.PrimaryMetric = uniqueSorted(summary.PrimaryMetric)
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScopeRef == out[j].ScopeRef {
			return out[i].ProductRef < out[j].ProductRef
		}
		return out[i].ScopeRef < out[j].ScopeRef
	})
	return out, nil
}

func loadAssemblyArtifact(ctx context.Context, repo *hrepo.MaterializedArtifactsRepository, tenantID, canonicalKey string) (*hmodels.MaterializedArtifact, error) {
	for _, candidateTenant := range []string{strings.TrimSpace(tenantID), "global"} {
		if candidateTenant == "" {
			continue
		}
		artifact, err := repo.GetByCanonicalKey(ctx, candidateTenant, hmodels.MaterializedKindSupplyChainAssemblyGraph, canonicalKey)
		if err == nil && artifact != nil {
			return artifact, nil
		}
	}
	return nil, fmt.Errorf("assembly artifact not found: %s", canonicalKey)
}

func loadDerivedPathArtifacts(ctx context.Context, repo *hrepo.MaterializedArtifactsRepository, tenantID string, keys []string) ([]*hmodels.MaterializedArtifact, error) {
	out := make([]*hmodels.MaterializedArtifact, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		var found *hmodels.MaterializedArtifact
		for _, candidateTenant := range []string{strings.TrimSpace(tenantID), "global"} {
			if candidateTenant == "" {
				continue
			}
			artifact, err := repo.GetByCanonicalKey(ctx, candidateTenant, hmodels.MaterializedKindSupplyChainPath, key)
			if err == nil && artifact != nil {
				found = artifact
				break
			}
		}
		if found != nil {
			out = append(out, found)
		}
	}
	return out, nil
}

func summarizePathArtifacts(artifacts []*hmodels.MaterializedArtifact) []pathArtifactSummary {
	out := make([]pathArtifactSummary, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		transportVia := map[string]struct{}{}
		vesselTypes := map[string]struct{}{}
		legProducts := map[string]struct{}{}
		for _, leg := range parsed.Legs {
			for _, via := range leg.TransportViaRefs {
				transportVia[strings.TrimSpace(via)] = struct{}{}
			}
			if vt := strings.TrimSpace(leg.VesselType); vt != "" {
				vesselTypes[vt] = struct{}{}
			}
			if ref := strings.TrimSpace(leg.ProductRef); ref != "" {
				legProducts[ref] = struct{}{}
			}
		}
		hasRouteBindings := false
		for _, leg := range parsed.Legs {
			if len(leg.GeoRefs) > 0 || len(leg.FlowRefs) > 0 || len(leg.ZoneRefs) > 0 || len(leg.AffectedZoneRefs) > 0 {
				hasRouteBindings = true
				break
			}
		}
		summary := pathArtifactSummary{
			CanonicalKey:      strings.TrimSpace(artifact.CanonicalKey),
			PathStatus:        strings.TrimSpace(artifact.PathStatus),
			NodeRefsN:         len(artifact.NodeRefs),
			LegsN:             len(parsed.Legs),
			LegProducts:       sortedKeySet(legProducts),
			TransportViaRefs:  sortedKeySet(transportVia),
			VesselTypes:       sortedKeySet(vesselTypes),
			HasRouteBindings:  hasRouteBindings,
			HasTransportVia:   len(transportVia) > 0,
			HasOperationalLeg: len(parsed.Legs) > 0,
		}
		if parsed.Stress != nil {
			summary.HasStress = true
			summary.StressScore = parsed.Stress.PathScore
			summary.BottleneckLeg = parsed.Stress.BottleneckLegIndex
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CanonicalKey < out[j].CanonicalKey })
	return out
}

func assessReport(report inspectionReport) map[string]interface{} {
	return map[string]interface{}{
		"workingProductScopePresent":    report.WorkingProductScope.Found,
		"workingProductScopeProductsN":  report.WorkingProductScope.CanonicalCount,
		"capabilityObservationsPresent": report.CapabilityObsCount > 0,
		"capabilityProfilesPresent":     len(report.CapabilityProfiles) > 0,
		"supplyLinkObservationsPresent": report.SupplyLinkObsCount > 0,
		"supplyLinkFragmentsPresent":    len(report.SupplyLinkFragments) > 0,
		"routeObservationsPresent":      report.RouteObsCount > 0,
		"assemblyArtifactPresent":       report.AssemblyArtifactKey != "",
		"derivedPathsPresent":           report.AssemblyDerivedPathsN > 0 || len(report.PathArtifacts) > 0,
		"pathLegsPresent":               anyPathHasLegs(report.PathArtifacts),
		"transportViaPresent":           anyPathHasTransportVia(report.PathArtifacts) || anyLinkHasTransportVia(report.SupplyLinkFragments),
		"capabilityMetricKeys":          report.CapabilityMetrics.Keys,
		"supplyLinkMetricKeys":          report.SupplyLinkMetrics.Keys,
		"routeQualifierKeys":            report.RouteQualifierKeys,
		"graphBuildMode":                report.GraphBuildMode,
		"supplyLinkRiskProfilesPresent": len(report.DerivedRiskProfiles) > 0,
	}
}

func extractDirectPathArtifactKeys(materialization map[string]interface{}, summary map[string]interface{}) []string {
	keys := parseStringSlice(materialization["pathArtifactKeys"])
	if len(keys) == 0 {
		keys = parseStringSlice(summary["pathArtifactKeys"])
	}
	return uniqueSorted(keys)
}

func printTextReport(report inspectionReport) {
	fmt.Printf("job=%s project=%s tenant=%s status=%s current=%s graph_mode=%s\n", report.JobID, report.ProjectID, report.TenantID, report.Status, report.CurrentState, report.GraphBuildMode)
	fmt.Printf("target=%s\n", report.TargetCanonicalPath)
	fmt.Printf("working_scope found=%t products=%d confirmed=%d partial=%d rejected=%d\n", report.WorkingProductScope.Found, report.WorkingProductScope.CanonicalCount, report.WorkingProductScope.ConfirmedCount, report.WorkingProductScope.PartialCoverageN, len(report.WorkingProductScope.RejectedRefs))
	fmt.Printf("capability_obs=%d supply_link_obs=%d route_obs=%d\n", report.CapabilityObsCount, report.SupplyLinkObsCount, report.RouteObsCount)
	fmt.Printf("capability_metric_keys=%v\n", report.CapabilityMetrics.Keys)
	fmt.Printf("supply_link_metric_keys=%v\n", report.SupplyLinkMetrics.Keys)
	fmt.Printf("route_qualifier_keys=%v\n", report.RouteQualifierKeys)
	fmt.Printf("capability_profiles=%d supply_link_fragments=%d derived_risk_profiles=%d\n", len(report.CapabilityProfiles), len(report.SupplyLinkFragments), len(report.DerivedRiskProfiles))
	fmt.Printf("assembly_artifact=%s derived_paths=%d path_artifacts=%d\n", report.AssemblyArtifactKey, report.AssemblyDerivedPathsN, len(report.PathArtifacts))
}

func normalizeMap(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	default:
		if raw == nil {
			return nil
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal(b, &out); err != nil {
			return nil
		}
		return out
	}
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func asString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return v
	default:
		if raw == nil {
			return ""
		}
		return fmt.Sprintf("%v", raw)
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func hasMap(in map[string]interface{}, key string) bool {
	if len(in) == 0 {
		return false
	}
	_, ok := in[key]
	return ok
}

func sortedKeySet(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueSorted(values []string) []string {
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

func collectObservedProducts(observations []hmodels.SupplyLinkObservation) []string {
	out := make([]string, 0, len(observations))
	for _, obs := range observations {
		if ref := strings.TrimSpace(obs.ProductRef); ref != "" {
			out = append(out, ref)
		}
	}
	return uniqueSorted(out)
}

func anyPathHasLegs(paths []pathArtifactSummary) bool {
	for _, path := range paths {
		if path.LegsN > 0 {
			return true
		}
	}
	return false
}

func anyPathHasTransportVia(paths []pathArtifactSummary) bool {
	for _, path := range paths {
		if path.HasTransportVia {
			return true
		}
	}
	return false
}

func anyLinkHasTransportVia(links []supplyLinkFragmentSummary) bool {
	for _, link := range links {
		if link.HasTransportVia {
			return true
		}
	}
	return false
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

func parseStringSlice(raw interface{}) []string {
	switch typed := raw.(type) {
	case []string:
		return uniqueSorted(typed)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, asString(item))
		}
		return uniqueSorted(out)
	default:
		if raw == nil {
			return nil
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out []string
		if err := json.Unmarshal(b, &out); err != nil {
			return nil
		}
		return uniqueSorted(out)
	}
}
