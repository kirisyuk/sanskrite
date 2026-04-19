package propagation

import (
	"context"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type EndpointInterestingness struct {
	FragmentID      string
	CanonicalPath   string
	FragmentType    FragmentType
	Country         string
	Hops            int
	Occurrences     int
	Remote          bool
	BusinessScore   float64
	SurpriseScore   float64
	Interestingness float64
	Reasons         []string
}

type EndpointInterestingnessSummary struct {
	BusinessEndpoints   int
	NonObviousEndpoints int
	FacilityEndpoints   int
	RemoteEndpoints     int
	LongPathEndpoints   int
	TopEndpoints        []EndpointInterestingness
}

func ScoreInterestingEndpoints(
	ctx context.Context,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	signal *horizonmodels.SignalDescriptor,
	propagationPaths [][]string,
	trace []SignalPropagationTraceEvent,
	limit int,
) (EndpointInterestingnessSummary, error) {
	summary := EndpointInterestingnessSummary{}
	if fragmentsRepo == nil || signal == nil || (len(propagationPaths) == 0 && len(trace) == 0) {
		return summary, nil
	}
	if limit <= 0 {
		limit = 5
	}
	profile := DefaultEndpointScoringProfile()

	localCountry := countryFromLocationPath(signal.LocationPath)
	cache := make(map[string]*horizonmodels.HorizonFragment, 64)
	items := make(map[string]*EndpointInterestingness)

	for _, path := range propagationPaths {
		if len(path) == 0 {
			continue
		}
		tailID := strings.TrimSpace(path[len(path)-1])
		if tailID == "" {
			continue
		}
		tailFrag, err := loadEndpointFragment(ctx, fragmentsRepo, cache, tailID)
		if err != nil || tailFrag == nil || !endpointFragmentAllowed(tailFrag) {
			continue
		}
		score := scoreEndpoint(signal, localCountry, tailFrag, path, fragmentsRepo, cache, ctx, profile)
		if score.FragmentID == "" {
			continue
		}
		mergeEndpointInterestingness(items, score)
	}

	for _, ev := range trace {
		if strings.TrimSpace(ev.Outcome) != "propagated" {
			continue
		}
		tailFrag, err := loadTraceEndpointFragment(ctx, fragmentsRepo, cache, ev)
		if err != nil || tailFrag == nil || !endpointFragmentAllowed(tailFrag) {
			continue
		}
		score := scoreTraceEndpoint(signal, localCountry, tailFrag, ev, profile)
		if score.FragmentID == "" {
			continue
		}
		mergeEndpointInterestingness(items, score)
	}

	scored := make([]EndpointInterestingness, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.Interestingness >= profile.BusinessThreshold {
			summary.BusinessEndpoints++
		}
		if item.Remote {
			summary.RemoteEndpoints++
		}
		if item.Hops >= 2 {
			summary.LongPathEndpoints++
		}
		if item.FragmentType == FragmentTypeFacility {
			summary.FacilityEndpoints++
		}
		if item.Remote && item.Hops >= profile.NonObviousMinHops && item.Interestingness >= profile.NonObviousThreshold {
			summary.NonObviousEndpoints++
		}
		scored = append(scored, *item)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Interestingness == scored[j].Interestingness {
			if scored[i].Occurrences == scored[j].Occurrences {
				return scored[i].CanonicalPath < scored[j].CanonicalPath
			}
			return scored[i].Occurrences > scored[j].Occurrences
		}
		return scored[i].Interestingness > scored[j].Interestingness
	})
	summary.TopEndpoints = rerankDiverseEndpoints(scored, limit, profile)
	return summary, nil
}

func mergeEndpointInterestingness(items map[string]*EndpointInterestingness, score EndpointInterestingness) {
	if items == nil || strings.TrimSpace(score.FragmentID) == "" {
		return
	}
	existing := items[score.FragmentID]
	if existing == nil {
		copyScore := score
		items[score.FragmentID] = &copyScore
		return
	}
	existing.Occurrences++
	if score.Hops > existing.Hops {
		existing.Hops = score.Hops
	}
	if score.Remote {
		existing.Remote = true
	}
	if score.Interestingness > existing.Interestingness {
		existing.CanonicalPath = score.CanonicalPath
		existing.FragmentType = score.FragmentType
		existing.Country = score.Country
		existing.Remote = score.Remote
		existing.BusinessScore = score.BusinessScore
		existing.SurpriseScore = score.SurpriseScore
		existing.Interestingness = score.Interestingness
		existing.Reasons = append([]string(nil), score.Reasons...)
	}
}

func scoreEndpoint(
	signal *horizonmodels.SignalDescriptor,
	localCountry string,
	tailFrag *horizonmodels.HorizonFragment,
	path []string,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	cache map[string]*horizonmodels.HorizonFragment,
	ctx context.Context,
	profile EndpointScoringProfile,
) EndpointInterestingness {
	out := EndpointInterestingness{
		FragmentID:    strings.TrimSpace(tailFrag.ID),
		CanonicalPath: strings.TrimSpace(tailFrag.CanonicalPath),
		FragmentType:  tailFrag.Class,
		Country:       countryFromFragment(tailFrag),
		Hops:          endpointMaxInt(0, len(path)-1),
		Occurrences:   1,
	}
	out.Remote = out.Country != "" && localCountry != "" && out.Country != localCountry
	overlay := BuildPropagationNodeOverlay(tailFrag)

	typeBonus, typeReason := endpointTypeBonus(signal.Axis, tailFrag, profile)
	out.BusinessScore += typeBonus
	if typeReason != "" {
		out.Reasons = append(out.Reasons, typeReason)
	}
	if crit, ok := fragmentMetricValue(tailFrag, "supply_chain_criticality"); ok {
		out.BusinessScore += crit * profile.CriticalityWeight
		out.Reasons = append(out.Reasons, "criticality")
	}
	if fragmentHasLowReplaceability(tailFrag) {
		out.BusinessScore += profile.LowReplaceabilityBonus
		out.Reasons = append(out.Reasons, "low_replaceability")
	}
	if fragmentLooksEnergyOrLogisticsSensitive(tailFrag) {
		out.BusinessScore += profile.EnergyLogisticsBonus
		out.Reasons = append(out.Reasons, "energy_or_logistics_sensitive")
	}
	out.BusinessScore += overlay.OperationalScore * profile.OverlayOperationalWeight
	out.BusinessScore += overlay.BridgeScore * profile.OverlayBridgeWeight
	out.BusinessScore += overlay.CriticalityScore * profile.OverlayCriticalityWeight
	if penalty, reasons := endpointAbstractionPenalty(tailFrag, overlay, profile); penalty > 0 {
		out.BusinessScore -= penalty
		out.Reasons = append(out.Reasons, reasons...)
	}
	if bonus, reasons := endpointSpecificityBonus(tailFrag, profile); bonus > 0 {
		out.BusinessScore += bonus
		out.Reasons = append(out.Reasons, reasons...)
	}
	if overlay.PrimaryRole == "chokepoint" {
		out.BusinessScore += profile.OverlayChokepointBonus
		out.Reasons = append(out.Reasons, "overlay_chokepoint")
	}
	if overlay.BusinessRole == "distributor" && !isFlowAxis(strings.TrimSpace(signal.Axis)) {
		out.BusinessScore -= profile.OverlayDistributorPenalty
		out.Reasons = append(out.Reasons, "overlay_generic_distributor")
	}
	if overlay.HasRole("bridge") {
		out.Reasons = append(out.Reasons, "overlay_bridge")
	}

	if out.Remote {
		out.SurpriseScore += profile.RemoteBonus
		out.Reasons = append(out.Reasons, "remote_country")
	}
	if out.Hops >= profile.MultiHopMinHops {
		out.SurpriseScore += profile.MultiHopBonus
		out.Reasons = append(out.Reasons, "multi_hop")
	}
	if out.Hops >= profile.DeepHopMinHops {
		out.SurpriseScore += profile.DeepHopBonus
	}
	distinctPathTypes := distinctBusinessPathTypes(path, fragmentsRepo, cache, ctx)
	if distinctPathTypes > 1 {
		out.SurpriseScore += endpointMinFloat(float64(distinctPathTypes-1)*profile.PathDiversityStep, profile.PathDiversityCap)
	}

	// FragmentTypeFacility (=site) already covers legacy DEPOSIT (site/deposit
	// subtype) under the AH taxonomy.
	if strings.TrimSpace(signal.Axis) == horizonmodels.AxisGeoConflict && out.Remote &&
		(out.FragmentType == FragmentTypeFacility || out.FragmentType == FragmentTypeMarket) {
		out.BusinessScore += profile.ConflictBusinessTailBonus
		out.Reasons = append(out.Reasons, "conflict_to_business_tail")
	}
	if (strings.TrimSpace(signal.Axis) == horizonmodels.AxisLogisticsCapacity ||
		strings.TrimSpace(signal.Axis) == horizonmodels.AxisLogisticsLatency ||
		strings.TrimSpace(signal.Axis) == horizonmodels.AxisPhysicalFlowSupply) &&
		(out.FragmentType == FragmentTypeFacility || out.FragmentType == FragmentTypeMarket) {
		out.BusinessScore += profile.FlowBusinessTailBonus
		out.Reasons = append(out.Reasons, "flow_tail")
	}

	rawInterestingness := out.BusinessScore*profile.BusinessWeight + out.SurpriseScore*profile.SurpriseWeight + profile.Bias
	out.Interestingness = endpointSigmoid(rawInterestingness / profile.Temperature)
	out.BusinessScore = endpointClamp01(out.BusinessScore)
	out.SurpriseScore = endpointClamp01(out.SurpriseScore)
	return out
}

func scoreTraceEndpoint(
	signal *horizonmodels.SignalDescriptor,
	localCountry string,
	tailFrag *horizonmodels.HorizonFragment,
	ev SignalPropagationTraceEvent,
	profile EndpointScoringProfile,
) EndpointInterestingness {
	out := EndpointInterestingness{
		FragmentID:    strings.TrimSpace(tailFrag.ID),
		CanonicalPath: strings.TrimSpace(tailFrag.CanonicalPath),
		FragmentType:  tailFrag.Class,
		Country:       countryFromFragment(tailFrag),
		Hops:          endpointMaxInt(0, ev.Hops),
		Occurrences:   1,
	}
	out.Remote = out.Country != "" && localCountry != "" && out.Country != localCountry
	overlay := BuildPropagationNodeOverlay(tailFrag)

	typeBonus, typeReason := endpointTypeBonus(signal.Axis, tailFrag, profile)
	out.BusinessScore += typeBonus
	if typeReason != "" {
		out.Reasons = append(out.Reasons, typeReason)
	}
	if crit, ok := fragmentMetricValue(tailFrag, "supply_chain_criticality"); ok {
		out.BusinessScore += crit * profile.CriticalityWeight
		out.Reasons = append(out.Reasons, "criticality")
	}
	if fragmentHasLowReplaceability(tailFrag) {
		out.BusinessScore += profile.LowReplaceabilityBonus
		out.Reasons = append(out.Reasons, "low_replaceability")
	}
	if fragmentLooksEnergyOrLogisticsSensitive(tailFrag) {
		out.BusinessScore += profile.EnergyLogisticsBonus
		out.Reasons = append(out.Reasons, "energy_or_logistics_sensitive")
	}
	out.BusinessScore += overlay.OperationalScore * profile.OverlayOperationalWeight
	out.BusinessScore += overlay.BridgeScore * profile.OverlayBridgeWeight
	out.BusinessScore += overlay.CriticalityScore * profile.OverlayCriticalityWeight
	if penalty, reasons := endpointAbstractionPenalty(tailFrag, overlay, profile); penalty > 0 {
		out.BusinessScore -= penalty
		out.Reasons = append(out.Reasons, reasons...)
	}
	if bonus, reasons := endpointSpecificityBonus(tailFrag, profile); bonus > 0 {
		out.BusinessScore += bonus
		out.Reasons = append(out.Reasons, reasons...)
	}
	if overlay.PrimaryRole == "chokepoint" {
		out.BusinessScore += profile.OverlayChokepointBonus
		out.Reasons = append(out.Reasons, "overlay_chokepoint")
	}
	if overlay.BusinessRole == "distributor" && !isFlowAxis(strings.TrimSpace(signal.Axis)) {
		out.BusinessScore -= profile.OverlayDistributorPenalty
		out.Reasons = append(out.Reasons, "overlay_generic_distributor")
	}
	if isBusinessPath(out.CanonicalPath) {
		out.BusinessScore += profile.TraceBusinessStepBonus
		out.Reasons = append(out.Reasons, "trace_business_step")
	}
	switch strings.TrimSpace(ev.Channel) {
	case "economic":
		out.BusinessScore += profile.TraceEconomicBonus
		out.Reasons = append(out.Reasons, "economic_channel")
	case "operational":
		out.BusinessScore += profile.TraceOperationalBonus
		out.Reasons = append(out.Reasons, "operational_channel")
	}
	if out.Remote {
		out.SurpriseScore += profile.RemoteBonus
		out.Reasons = append(out.Reasons, "remote_country")
	}
	if isCrossBorderEvent(ev) {
		out.SurpriseScore += profile.TraceCrossBorderBonus
		out.Reasons = append(out.Reasons, "cross_border_trace")
	}
	if out.Hops >= profile.MultiHopMinHops {
		out.SurpriseScore += profile.MultiHopBonus
		out.Reasons = append(out.Reasons, "multi_hop")
	}
	if out.Hops >= profile.DeepHopMinHops {
		out.SurpriseScore += profile.DeepHopBonus
	}
	physicsBonus := tracePhysicsBonus(ev.Factors, profile)
	if physicsBonus > 0 {
		out.BusinessScore += physicsBonus
		out.Reasons = append(out.Reasons, "transmission_physics")
	}
	if isFlowAxis(strings.TrimSpace(ev.AxisOut)) && isBusinessPath(out.CanonicalPath) {
		out.BusinessScore += profile.TraceFlowAxisBonus
		out.Reasons = append(out.Reasons, "flow_axis_bridge")
	}
	// FragmentTypeFacility (=site) already covers legacy DEPOSIT under the AH taxonomy.
	if strings.TrimSpace(signal.Axis) == horizonmodels.AxisGeoConflict && out.Remote &&
		(out.FragmentType == FragmentTypeFacility || out.FragmentType == FragmentTypeMarket) {
		out.BusinessScore += profile.ConflictBusinessTailBonus
		out.Reasons = append(out.Reasons, "conflict_to_business_tail")
	}
	if isFlowAxis(strings.TrimSpace(signal.Axis)) &&
		(out.FragmentType == FragmentTypeFacility || out.FragmentType == FragmentTypeMarket) {
		out.BusinessScore += profile.FlowBusinessTailBonus
		out.Reasons = append(out.Reasons, "flow_tail")
	}

	rawInterestingness := out.BusinessScore*profile.BusinessWeight + out.SurpriseScore*profile.SurpriseWeight + profile.Bias
	out.Interestingness = endpointSigmoid(rawInterestingness / profile.Temperature)
	out.BusinessScore = endpointClamp01(out.BusinessScore)
	out.SurpriseScore = endpointClamp01(out.SurpriseScore)
	return out
}

// endpointFragmentAllowed gates which fragments may serve as propagation
// endpoints. Person/event are filtered by class; legacy MEDIA_CHANNEL
// (org/media_channel under AH) is filtered via the predicate so the
// surrounding org/* taxonomy is not over-rejected.
func endpointFragmentAllowed(frag *horizonmodels.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	switch frag.Class {
	case FragmentTypePerson, FragmentTypeEvent:
		return false
	}
	if frag.IsLegacyMediaChannel() {
		return false
	}
	return true
}

func endpointTypeBonus(_ string, frag *horizonmodels.HorizonFragment, profile EndpointScoringProfile) (float64, string) {
	if frag == nil {
		return 0, ""
	}
	subtype := strings.TrimSpace(frag.Subtype)
	switch frag.Class {
	case FragmentTypeFacility:
		if subtype == "deposit" {
			return profile.DepositTypeWeight, "deposit_tail"
		}
		return profile.FacilityTypeWeight, "facility_tail"
	case FragmentTypeMarket:
		return profile.MarketTypeWeight, "market_tail"
	case FragmentTypeOrganization:
		if subtype == "program" {
			return profile.ProgramTypeWeight, "program_tail"
		}
		return profile.OrganizationTypeWeight, "org_tail"
	case FragmentTypeGeoRegion:
		if strings.Contains(strings.ToLower(strings.TrimSpace(frag.GeoFeatureType)), "strait") ||
			strings.Contains(strings.ToLower(strings.TrimSpace(frag.GeoFeatureType)), "canal") ||
			strings.Contains(strings.ToLower(strings.TrimSpace(frag.CanonicalPath)), "gulf") {
			return profile.ChokepointTypeWeight, "chokepoint_tail"
		}
		return profile.GeoTypeWeight, "geo_tail"
	default:
		return profile.GenericTypeWeight, "generic_tail"
	}
}

func fragmentMetricValue(frag *horizonmodels.HorizonFragment, metric string) (float64, bool) {
	if frag == nil || frag.State == nil {
		return 0, false
	}
	mv, ok := frag.State[strings.TrimSpace(metric)]
	if !ok {
		return 0, false
	}
	return endpointClamp01(mv.Value), true
}

func fragmentHasLowReplaceability(frag *horizonmodels.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	for _, path := range frag.IndexPaths {
		if strings.Contains(strings.ToLower(strings.TrimSpace(path)), "replaceability-low") {
			return true
		}
	}
	return false
}

func fragmentLooksEnergyOrLogisticsSensitive(frag *horizonmodels.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	text := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(frag.CanonicalPath),
		strings.TrimSpace(frag.MarketDomain),
		strings.TrimSpace(frag.MarketVertical),
		strings.TrimSpace(frag.GeoFeatureType),
	}, " "))
	for _, p := range frag.IndexPaths {
		text += " " + strings.ToLower(strings.TrimSpace(p))
	}
	for _, token := range []string{
		"lng", "natural-gas", "gas", "petrochem", "refinery", "pipeline", "terminal",
		"port", "shipping", "maritime", "transport", "logistics", "warehouse",
		"electricity", "power", "grid",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func endpointAbstractionPenalty(frag *horizonmodels.HorizonFragment, overlay PropagationNodeOverlay, profile EndpointScoringProfile) (float64, []string) {
	if frag == nil {
		return 0, nil
	}
	path := strings.TrimSpace(frag.CanonicalPath)
	penalty := 0.0
	reasons := make([]string, 0, 3)
	if strings.HasPrefix(path, "/sphere/") {
		penalty += profile.SphereHubPenalty
		reasons = append(reasons, "sphere_hub")
	}
	if strings.HasPrefix(path, "/product/") {
		penalty += profile.ProductHubPenalty
		reasons = append(reasons, "generic_product_tail")
	}
	if isGenericLocPath(path) {
		penalty += profile.GenericLocPenalty
		reasons = append(reasons, "generic_loc_tail")
	}
	if frag.Class == FragmentTypeOrganization && overlay.BusinessRole == "generic" && !strings.HasPrefix(path, "/gov/") {
		penalty += profile.GenericOrgPenalty
		reasons = append(reasons, "generic_org_tail")
	}
	switch canonical.ClassifyPlaceholderCanonicalPath(path) {
	case canonical.PlaceholderPathFacilitySynthetic:
		penalty += profile.SyntheticFacilityPenalty
		reasons = append(reasons, "facility_synthetic_placeholder")
	case canonical.PlaceholderPathOrgSynthetic, canonical.PlaceholderPathGovSynthetic:
		penalty += profile.SyntheticOrgPenalty
		reasons = append(reasons, "synthetic_org_placeholder")
	case canonical.PlaceholderPathFacilityUnknownGeo:
		penalty += profile.UnknownGeoFacilityPenalty
		reasons = append(reasons, "facility_unknown_geo")
	case canonical.PlaceholderPathOrgUnknownJurisdiction, canonical.PlaceholderPathGovUnknownJurisdiction:
		penalty += profile.UnknownJurisdictionPenalty
		reasons = append(reasons, "unknown_jurisdiction")
	}
	return penalty, reasons
}

func endpointSpecificityBonus(frag *horizonmodels.HorizonFragment, profile EndpointScoringProfile) (float64, []string) {
	if frag == nil {
		return 0, nil
	}
	path := strings.TrimSpace(frag.CanonicalPath)
	bonus := 0.0
	reasons := make([]string, 0, 2)
	if frag.Class == FragmentTypeFacility && !isGenericLocPath(path) {
		bonus += profile.ConcreteFacilityBonus
		reasons = append(reasons, "concrete_facility")
	}
	if frag.Class == FragmentTypeMarket && !strings.Contains(path, "/global/") {
		bonus += profile.ConcreteMarketBonus
		reasons = append(reasons, "concrete_market")
	}
	return bonus, reasons
}

func isGenericLocPath(path string) bool {
	switch canonical.ClassifyPlaceholderCanonicalPath(path) {
	case canonical.PlaceholderPathLocPlaceholderAdmin1, canonical.PlaceholderPathLocPlaceholderAdmin12, canonical.PlaceholderPathLocPlaceholderAdmin2:
		return true
	default:
		return false
	}
}

func distinctBusinessPathTypes(path []string, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, cache map[string]*horizonmodels.HorizonFragment, ctx context.Context) int {
	seen := make(map[FragmentType]bool)
	for _, id := range path {
		frag, err := loadEndpointFragment(ctx, fragmentsRepo, cache, strings.TrimSpace(id))
		if err != nil || frag == nil {
			continue
		}
		switch frag.Class {
		case FragmentTypeFacility, FragmentTypeMarket, FragmentTypeOrganization, FragmentTypeGeoRegion:
			seen[frag.Class] = true
		}
	}
	return len(seen)
}

func loadTraceEndpointFragment(ctx context.Context, repo *horizonrepo.HorizonFragmentsRepository, cache map[string]*horizonmodels.HorizonFragment, ev SignalPropagationTraceEvent) (*horizonmodels.HorizonFragment, error) {
	if repo == nil {
		return nil, nil
	}
	if frag, err := loadEndpointFragment(ctx, repo, cache, strings.TrimSpace(ev.ToFragment)); err == nil && frag != nil {
		return frag, nil
	}
	if path := strings.TrimSpace(ev.ToPath); path != "" {
		frag, err := repo.GetByCanonicalPath(ctx, path)
		if err == nil && frag != nil {
			cache[frag.ID] = frag
			return frag, nil
		}
	}
	return nil, nil
}

func loadEndpointFragment(ctx context.Context, repo *horizonrepo.HorizonFragmentsRepository, cache map[string]*horizonmodels.HorizonFragment, id string) (*horizonmodels.HorizonFragment, error) {
	if repo == nil || id == "" {
		return nil, nil
	}
	if frag, ok := cache[id]; ok {
		return frag, nil
	}
	frag, err := repo.Get(ctx, id)
	if err != nil || frag == nil {
		return nil, err
	}
	cache[id] = frag
	return frag, nil
}

func countryFromLocationPath(locationPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(locationPath)), "/"), "/")
	if len(parts) >= 2 && parts[0] == "loc" {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func countryFromFragment(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		return strings.ToLower(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1))
	}
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(fragment.CanonicalPath)), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "fac", "deposit", "market", "loc", "zone", "community":
		return strings.TrimSpace(parts[1])
	case "track":
		if len(parts) >= 3 {
			return strings.TrimSpace(parts[2])
		}
	}
	return ""
}

func endpointClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func endpointMinFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func endpointMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func tracePhysicsBonus(factors map[string]float64, profile EndpointScoringProfile) float64 {
	if len(factors) == 0 {
		return 0
	}
	raw := 0.0
	for _, key := range []string{
		"throughput_factor",
		"reroute_penalty_factor",
		"capacity_constraint_factor",
		"latency_factor",
		"cost_factor",
		"waveguide_factor",
	} {
		if v, ok := factors[key]; ok && v > 1.0 {
			raw += (v - 1.0)
		}
	}
	if raw <= 0 {
		return 0
	}
	return endpointMinFloat(raw*profile.TracePhysicsWeight, profile.TracePhysicsCap)
}

func isFlowAxis(axis string) bool {
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisOperationalContinuity:
		return true
	default:
		return false
	}
}

func rerankDiverseEndpoints(candidates []EndpointInterestingness, limit int, profile EndpointScoringProfile) []EndpointInterestingness {
	if len(candidates) == 0 || limit <= 0 {
		return nil
	}
	if len(candidates) <= limit {
		return append([]EndpointInterestingness(nil), candidates...)
	}
	selected := make([]EndpointInterestingness, 0, limit)
	used := make([]bool, len(candidates))
	for len(selected) < limit {
		bestIdx := -1
		bestScore := -1e9
		for i, candidate := range candidates {
			if used[i] {
				continue
			}
			redundancy := 0.0
			for _, chosen := range selected {
				similarity := endpointSimilarity(candidate, chosen)
				if similarity > redundancy {
					redundancy = similarity
				}
			}
			mmrScore := profile.DiversityLambda*candidate.Interestingness - (1.0-profile.DiversityLambda)*redundancy
			if bestIdx == -1 || mmrScore > bestScore {
				bestIdx = i
				bestScore = mmrScore
			}
		}
		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, candidates[bestIdx])
	}
	return selected
}

func endpointSimilarity(a, b EndpointInterestingness) float64 {
	if strings.TrimSpace(a.FragmentID) == "" || strings.TrimSpace(b.FragmentID) == "" {
		return 0
	}
	if a.FragmentID == b.FragmentID {
		return 1
	}
	similarity := 0.0
	if a.Country != "" && a.Country == b.Country {
		similarity += 0.45
	}
	if a.FragmentType == b.FragmentType {
		similarity += 0.30
	}
	if endpointDomainKey(a.CanonicalPath) != "" && endpointDomainKey(a.CanonicalPath) == endpointDomainKey(b.CanonicalPath) {
		similarity += 0.25
	}
	return endpointClamp01(similarity)
}

func endpointDomainKey(path string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(path)), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	limit := 3
	if len(parts) < limit {
		limit = len(parts)
	}
	return strings.Join(parts[:limit], "/")
}
