package importance

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type Config struct {
	DirectEdgeLimit   int
	NeighborEdgeLimit int
	MaxBlastHops      int
	MinPathStrength   float64
}

type Scorecard struct {
	StrategicImportance    float64 `json:"strategicImportance"`
	BridgeImportance       float64 `json:"bridgeImportance"`
	BusinessBlastRadius    float64 `json:"businessBlastRadius"`
	ConflictBlastRadius    float64 `json:"conflictBlastRadius"`
	StructuralImportance   float64 `json:"structuralImportance,omitempty"`
	DataQualityConfidence  float64 `json:"dataQualityConfidence"`
	DirectEdgeCount        int     `json:"directEdgeCount,omitempty"`
	DirectBusinessEdges    int     `json:"directBusinessEdges,omitempty"`
	DirectContextEdges     int     `json:"directContextEdges,omitempty"`
	SemanticGroupDiversity int     `json:"semanticGroupDiversity,omitempty"`
	NeighborTypeDiversity  int     `json:"neighborTypeDiversity,omitempty"`
}

type Snapshot struct {
	RootID string
	Nodes  map[string]*horizonmodels.HorizonFragment
	Out    map[string][]*horizonmodels.DependencyEdge
	In     map[string][]*horizonmodels.DependencyEdge
}

type traversalBreakdown struct {
	Base                float64
	PhysicsMultiplier   float64
	CriticalityFactor   float64
	AmplificationFactor float64
	NeighborBoost       float64
	Final               float64
	Family              string
	PhysicsReasons      []string
}

type Scorer struct {
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
	edgesRepo     *horizonrepo.DependencyEdgesRepository
	cfg           Config
}

func DefaultConfig() Config {
	return Config{
		DirectEdgeLimit:   80,
		NeighborEdgeLimit: 40,
		MaxBlastHops:      2,
		MinPathStrength:   0.08,
	}
}

func NewScorer(fragmentsRepo *horizonrepo.HorizonFragmentsRepository, edgesRepo *horizonrepo.DependencyEdgesRepository) *Scorer {
	return &Scorer{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		cfg:           DefaultConfig(),
	}
}

func (s *Scorer) WithConfig(cfg Config) *Scorer {
	if cfg.DirectEdgeLimit <= 0 {
		cfg.DirectEdgeLimit = DefaultConfig().DirectEdgeLimit
	}
	if cfg.NeighborEdgeLimit <= 0 {
		cfg.NeighborEdgeLimit = DefaultConfig().NeighborEdgeLimit
	}
	if cfg.MaxBlastHops <= 0 {
		cfg.MaxBlastHops = DefaultConfig().MaxBlastHops
	}
	if cfg.MinPathStrength <= 0 {
		cfg.MinPathStrength = DefaultConfig().MinPathStrength
	}
	s.cfg = cfg
	return s
}

func (s *Scorer) ScoreFragment(ctx context.Context, tenantID, fragmentID string) (Scorecard, *horizonmodels.HorizonFragment, error) {
	if s == nil || s.fragmentsRepo == nil || s.edgesRepo == nil {
		return Scorecard{}, nil, fmt.Errorf("importance scorer is not configured")
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return Scorecard{}, nil, fmt.Errorf("fragment id is required")
	}
	frag, err := s.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return Scorecard{}, nil, err
	}
	if frag == nil {
		return Scorecard{}, nil, fmt.Errorf("fragment not found: %s", fragmentID)
	}
	snap, err := s.LoadSnapshot(ctx, tenantID, frag)
	if err != nil {
		return Scorecard{}, nil, err
	}
	return ScoreSnapshot(snap, s.cfg), frag, nil
}

func (s *Scorer) ScoreFragmentByPath(ctx context.Context, tenantID, canonicalPath string) (Scorecard, *horizonmodels.HorizonFragment, error) {
	if s == nil || s.fragmentsRepo == nil {
		return Scorecard{}, nil, fmt.Errorf("importance scorer is not configured")
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return Scorecard{}, nil, fmt.Errorf("canonical path is required")
	}
	frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil {
		return Scorecard{}, nil, err
	}
	if frag == nil {
		return Scorecard{}, nil, fmt.Errorf("fragment not found: %s", canonicalPath)
	}
	snap, err := s.LoadSnapshot(ctx, tenantID, frag)
	if err != nil {
		return Scorecard{}, nil, err
	}
	return ScoreSnapshot(snap, s.cfg), frag, nil
}

func (s *Scorer) LoadSnapshot(ctx context.Context, tenantID string, root *horizonmodels.HorizonFragment) (Snapshot, error) {
	if s == nil || root == nil || strings.TrimSpace(root.ID) == "" {
		return Snapshot{}, fmt.Errorf("root fragment is required")
	}
	tenantID = normalizeTenant(tenantID, root)
	outEdges, err := s.edgesRepo.ListBySource(ctx, tenantID, root.ID, nil, s.cfg.DirectEdgeLimit)
	if err != nil {
		return Snapshot{}, err
	}
	inEdges, err := s.edgesRepo.ListByTarget(ctx, tenantID, root.ID, nil, s.cfg.DirectEdgeLimit)
	if err != nil {
		return Snapshot{}, err
	}

	nodes := map[string]*horizonmodels.HorizonFragment{
		root.ID: root,
	}
	outMap := map[string][]*horizonmodels.DependencyEdge{
		root.ID: filterImportanceEdges(outEdges),
	}
	inMap := map[string][]*horizonmodels.DependencyEdge{
		root.ID: filterImportanceEdges(inEdges),
	}

	firstHopIDs := collectNeighborIDs(root.ID, outMap[root.ID], inMap[root.ID])
	neighbors, err := s.fragmentsRepo.GetMany(ctx, firstHopIDs)
	if err != nil {
		return Snapshot{}, err
	}
	for id, frag := range neighbors {
		nodes[id] = frag
	}

	secondHopIDs := make([]string, 0, 64)
	for _, id := range firstHopIDs {
		neighbor := nodes[id]
		if neighbor == nil {
			continue
		}
		nOut, _ := s.edgesRepo.ListBySource(ctx, tenantID, id, nil, s.cfg.NeighborEdgeLimit)
		nIn, _ := s.edgesRepo.ListByTarget(ctx, tenantID, id, nil, s.cfg.NeighborEdgeLimit)
		outMap[id] = filterImportanceEdges(nOut)
		inMap[id] = filterImportanceEdges(nIn)
		secondHopIDs = append(secondHopIDs, collectNeighborIDs(id, outMap[id], inMap[id])...)
	}

	missing := make([]string, 0, len(secondHopIDs))
	for _, id := range secondHopIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := nodes[id]; ok {
			continue
		}
		missing = append(missing, id)
	}
	if len(missing) > 0 {
		secondHopFrags, err := s.fragmentsRepo.GetMany(ctx, missing)
		if err != nil {
			return Snapshot{}, err
		}
		for id, frag := range secondHopFrags {
			nodes[id] = frag
		}
	}

	return Snapshot{
		RootID: root.ID,
		Nodes:  nodes,
		Out:    outMap,
		In:     inMap,
	}, nil
}

func ScoreSnapshot(snap Snapshot, cfg Config) Scorecard {
	root := snap.Nodes[snap.RootID]
	if root == nil {
		return Scorecard{}
	}
	overlay := propagation.BuildPropagationNodeOverlay(root)
	directEdges := append([]*horizonmodels.DependencyEdge{}, snap.Out[snap.RootID]...)
	directEdges = append(directEdges, snap.In[snap.RootID]...)

	structuralRaw := 0.0
	semanticGroups := map[string]struct{}{}
	neighborTypes := map[horizonmodels.FragmentType]struct{}{}
	businessEdges := 0
	contextEdges := 0
	strongEdgeFamilies := map[string]struct{}{}

	for _, edge := range directEdges {
		if edge == nil {
			continue
		}
		neighborID := otherEndpoint(edge, snap.RootID)
		neighbor := snap.Nodes[neighborID]
		if neighbor == nil {
			continue
		}
		semanticGroups[horizonmodels.SemanticGroupForEdgeType(edge.Type)] = struct{}{}
		neighborTypes[neighbor.Type] = struct{}{}
		if isBusinessType(neighbor.Type) {
			businessEdges++
		}
		if isContextType(neighbor.Type) {
			contextEdges++
		}
		strong, family := directEdgeImportanceContribution(root, neighbor, edge, snap.RootID)
		structuralRaw += strong
		if family != "" {
			strongEdgeFamilies[family] = struct{}{}
		}
	}

	structural := saturatingScore(structuralRaw, 4.2)
	semanticDiversity := normalizedCount(len(semanticGroups), 5)
	typeDiversity := normalizedCount(len(neighborTypes), 6)
	contextBridge := 0.0
	switch {
	case businessEdges > 0 && contextEdges > 0:
		contextBridge = 1.0
	case businessEdges > 0 || contextEdges > 0:
		contextBridge = 0.45
	}
	familyDiversity := normalizedCount(len(strongEdgeFamilies), 4)
	bridge := clamp01(
		0.28*overlay.BridgeScore +
			0.24*semanticDiversity +
			0.18*typeDiversity +
			0.12*familyDiversity +
			0.18*contextBridge,
	)

	businessBlast := clamp01(
		0.42*axisReachScore(snap, cfg, horizonmodels.AxisPhysicalFlowSupply) +
			0.28*axisReachScore(snap, cfg, horizonmodels.AxisOperationalContinuity) +
			0.16*axisReachScore(snap, cfg, horizonmodels.AxisLogisticsCapacity) +
			0.14*axisReachScore(snap, cfg, horizonmodels.AxisLogisticsLatency),
	)
	conflictBlast := clamp01(axisReachScore(snap, cfg, horizonmodels.AxisGeoConflict))
	strategic := clamp01(
		0.30*structural +
			0.22*bridge +
			0.20*businessBlast +
			0.16*conflictBlast +
			0.12*overlay.CriticalityScore,
	)

	return Scorecard{
		StrategicImportance:    strategic,
		BridgeImportance:       bridge,
		BusinessBlastRadius:    businessBlast,
		ConflictBlastRadius:    conflictBlast,
		StructuralImportance:   structural,
		DataQualityConfidence:  scoreConfidence(root, directEdges, businessEdges, contextEdges),
		DirectEdgeCount:        len(directEdges),
		DirectBusinessEdges:    businessEdges,
		DirectContextEdges:     contextEdges,
		SemanticGroupDiversity: len(semanticGroups),
		NeighborTypeDiversity:  len(neighborTypes),
	}
}

func MetricsFromScorecard(score Scorecard, now time.Time) map[string]horizonmodels.MetricValue {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	provenance := horizonmodels.Provenance{
		SourceIDs:  []string{"importance-heuristic-v1"},
		Method:     "importance_heuristic_v1",
		ObservedAt: &now,
	}
	return map[string]horizonmodels.MetricValue{
		horizonmodels.MetricPathImportanceStrategic: {
			Value:      clamp01(score.StrategicImportance),
			Confidence: clamp01(score.DataQualityConfidence),
			ObservedAt: now,
			Provenance: provenance,
		},
		horizonmodels.MetricPathImportanceBridge: {
			Value:      clamp01(score.BridgeImportance),
			Confidence: clamp01(score.DataQualityConfidence),
			ObservedAt: now,
			Provenance: provenance,
		},
		horizonmodels.MetricPathImportanceBusinessBlastRadius: {
			Value:      clamp01(score.BusinessBlastRadius),
			Confidence: clamp01(score.DataQualityConfidence),
			ObservedAt: now,
			Provenance: provenance,
		},
		horizonmodels.MetricPathImportanceConflictBlastRadius: {
			Value:      clamp01(score.ConflictBlastRadius),
			Confidence: clamp01(score.DataQualityConfidence),
			ObservedAt: now,
			Provenance: provenance,
		},
	}
}

func ApplyScorecard(fragment *horizonmodels.HorizonFragment, score Scorecard, now time.Time) {
	if fragment == nil {
		return
	}
	if fragment.State == nil {
		fragment.State = map[string]horizonmodels.MetricValue{}
	}
	for key, metric := range MetricsFromScorecard(score, now) {
		fragment.State[key] = metric
	}
}

func axisReachScore(snap Snapshot, cfg Config, axis string) float64 {
	root := snap.Nodes[snap.RootID]
	if root == nil {
		return 0
	}
	profiles := horizonmodels.DefaultPropagationProfiles()
	profile, ok := profiles[axis]
	if !ok {
		return 0
	}
	physicsProfile := propagation.DefaultEdgeFamilyPhysicsProfile()

	type queueState struct {
		id       string
		strength float64
		hops     int
	}
	best := map[string]float64{
		snap.RootID: 1.0,
	}
	queue := []queueState{{id: snap.RootID, strength: 1.0, hops: 0}}
	tails := map[string]float64{}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.hops >= cfg.MaxBlastHops {
			continue
		}
		currentFrag := snap.Nodes[cur.id]
		if currentFrag == nil {
			continue
		}
		for _, step := range incidentSteps(snap, cur.id) {
			edge := step.edge
			if edge == nil || !edge.IsActiveNow() || edge.IsQuarantined() {
				continue
			}
			nextFrag := snap.Nodes[step.nextID]
			if nextFrag == nil {
				continue
			}
			edgeWeight := traversalEdgeWeight(axis, profile, physicsProfile, currentFrag, nextFrag, edge, step.forward)
			if edgeWeight <= 0 {
				continue
			}
			pathWeight := cur.strength * edgeWeight * hopDecay(cur.hops+1) * axisNodeWeight(axis, nextFrag)
			if pathWeight < cfg.MinPathStrength {
				continue
			}
			if prev, ok := best[step.nextID]; ok && prev >= pathWeight {
				continue
			}
			best[step.nextID] = pathWeight
			if step.nextID != snap.RootID && isBusinessType(nextFrag.Type) {
				tails[step.nextID] = math.Max(tails[step.nextID], pathWeight*axisBusinessTailWeight(axis, nextFrag))
			}
			queue = append(queue, queueState{id: step.nextID, strength: pathWeight, hops: cur.hops + 1})
		}
	}

	if len(tails) == 0 {
		return 0
	}
	scores := make([]float64, 0, len(tails))
	for _, v := range tails {
		scores = append(scores, v)
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i] > scores[j] })
	raw := 0.0
	for i, v := range scores {
		if i >= 8 {
			break
		}
		raw += v / float64(i+1)
	}
	rootOverlay := propagation.BuildPropagationNodeOverlay(root)
	if axis == horizonmodels.AxisGeoConflict && rootOverlay.PrimaryRole == "chokepoint" {
		raw *= 1.10
	}
	if axis == horizonmodels.AxisPhysicalFlowSupply && rootOverlay.HasExposure("energy_input") {
		raw *= 1.06
	}
	return saturatingScore(raw, 1.85)
}

type incidentStep struct {
	edge    *horizonmodels.DependencyEdge
	nextID  string
	forward bool
}

func incidentSteps(snap Snapshot, id string) []incidentStep {
	out := make([]incidentStep, 0, len(snap.Out[id])+len(snap.In[id]))
	for _, edge := range snap.Out[id] {
		if edge == nil {
			continue
		}
		out = append(out, incidentStep{
			edge:    edge,
			nextID:  strings.TrimSpace(edge.TargetFragmentID),
			forward: true,
		})
	}
	for _, edge := range snap.In[id] {
		if edge == nil {
			continue
		}
		out = append(out, incidentStep{
			edge:    edge,
			nextID:  strings.TrimSpace(edge.SourceFragmentID),
			forward: false,
		})
	}
	return out
}

func traversalEdgeWeight(
	axis string,
	profile horizonmodels.PropagationProfile,
	physicsProfile propagation.EdgeFamilyPhysicsProfile,
	currentFrag *horizonmodels.HorizonFragment,
	nextFrag *horizonmodels.HorizonFragment,
	edge *horizonmodels.DependencyEdge,
	forward bool,
) float64 {
	return traversalEdgeBreakdown(axis, profile, physicsProfile, currentFrag, nextFrag, edge, forward).Final
}

func traversalEdgeBreakdown(
	axis string,
	profile horizonmodels.PropagationProfile,
	physicsProfile propagation.EdgeFamilyPhysicsProfile,
	currentFrag *horizonmodels.HorizonFragment,
	nextFrag *horizonmodels.HorizonFragment,
	edge *horizonmodels.DependencyEdge,
	forward bool,
) traversalBreakdown {
	base := strongTraversalWeight(profile, edge, forward)
	if base <= 0 {
		base = fallbackTraversalWeight(axis, edge)
	}
	if base <= 0 {
		return traversalBreakdown{}
	}
	physicsSummary := propagation.ApplyEdgeFamilyPhysics(axis, currentFrag, edge, physicsProfile)
	physics := physicsSummary.Multiplier
	criticality := 0.72 + 0.28*clamp01(edge.Properties.Criticality)
	amp := 1.0
	if edge.Properties.Amplification > 0 {
		amp += math.Min(edge.Properties.Amplification, 1.0) * 0.12
	}
	neighborBoost := 1.0
	if nextFrag != nil {
		neighborBoost = 0.88 + 0.18*typeTransmissionWeight(nextFrag.Type)
	}
	return traversalBreakdown{
		Base:                base,
		PhysicsMultiplier:   physics,
		CriticalityFactor:   criticality,
		AmplificationFactor: amp,
		NeighborBoost:       neighborBoost,
		Final:               clamp(edgeWeightProduct(base, physics, criticality, amp, neighborBoost), 0, 1.35),
		Family:              physicsSummary.Family,
		PhysicsReasons:      physicsSummary.Reasons,
	}
}

func edgeWeightProduct(parts ...float64) float64 {
	out := 1.0
	for _, part := range parts {
		out *= part
	}
	return out
}

func strongTraversalWeight(profile horizonmodels.PropagationProfile, edge *horizonmodels.DependencyEdge, forward bool) float64 {
	if edge == nil {
		return 0
	}
	for _, spec := range profile.StrongEdges {
		if strings.TrimSpace(spec.Type) != strings.TrimSpace(string(edge.Type)) {
			continue
		}
		switch strings.TrimSpace(spec.Direction) {
		case "both":
			return spec.Weight
		case "source_to_target":
			if forward {
				return spec.Weight
			}
		case "target_to_source":
			if !forward {
				return spec.Weight
			}
		}
	}
	return 0
}

func fallbackTraversalWeight(axis string, edge *horizonmodels.DependencyEdge) float64 {
	if edge == nil {
		return 0
	}
	group := horizonmodels.SemanticGroupForEdgeType(edge.Type)
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisGeoConflict:
		switch group {
		case horizonmodels.SemanticGroupSupplyChain:
			return 0.54
		case horizonmodels.SemanticGroupStrategic, horizonmodels.SemanticGroupGovernance:
			return 0.48
		case horizonmodels.SemanticGroupMarket:
			return 0.34
		case horizonmodels.SemanticGroupDomain:
			return 0.18
		default:
			if edge.Type == horizonmodels.DependencyTypeAffects || edge.Type == horizonmodels.DependencyTypeLocatedIn || edge.Type == horizonmodels.DependencyTypeOperatesIn {
				return 0.68
			}
			return 0.22
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		switch group {
		case horizonmodels.SemanticGroupSupplyChain:
			return 0.58
		case horizonmodels.SemanticGroupMarket:
			return 0.36
		case horizonmodels.SemanticGroupGovernance, horizonmodels.SemanticGroupStrategic:
			return 0.28
		case horizonmodels.SemanticGroupDomain:
			return 0.16
		default:
			return 0.20
		}
	case horizonmodels.AxisOperationalContinuity:
		switch group {
		case horizonmodels.SemanticGroupGovernance:
			return 0.52
		case horizonmodels.SemanticGroupSupplyChain:
			return 0.44
		case horizonmodels.SemanticGroupStrategic:
			return 0.34
		case horizonmodels.SemanticGroupMarket:
			return 0.26
		default:
			return 0.20
		}
	default:
		return 0.18
	}
}

func axisNodeWeight(axis string, frag *horizonmodels.HorizonFragment) float64 {
	if frag == nil {
		return 0.4
	}
	overlay := propagation.BuildPropagationNodeOverlay(frag)
	weight := 0.76 + 0.14*overlay.OperationalScore + 0.10*overlay.CriticalityScore
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisGeoConflict:
		if isBusinessType(frag.Type) {
			weight += 0.08
		}
		if frag.Type == horizonmodels.FragmentTypeAffectedZone || frag.Type == horizonmodels.FragmentTypeGeoRegion {
			weight += 0.06
		}
		if overlay.PrimaryRole == "chokepoint" {
			weight += 0.10
		}
		if overlay.HasExposure("direct_geo") {
			weight += 0.06
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		if overlay.HasExposure("route_dependency") {
			weight += 0.10
		}
		if overlay.HasExposure("energy_input") {
			weight += 0.08
		}
		if isBusinessType(frag.Type) {
			weight += 0.06
		}
	case horizonmodels.AxisOperationalContinuity:
		if frag.Type == horizonmodels.FragmentTypeFacility || frag.Type == horizonmodels.FragmentTypeOrganization || frag.Type == horizonmodels.FragmentTypeDeposit {
			weight += 0.10
		}
		if overlay.HasRole("controller") {
			weight += 0.06
		}
	}
	if isWeakContextType(frag.Type) {
		weight -= 0.16
	}
	return clamp(weight, 0.42, 1.24)
}

func axisBusinessTailWeight(axis string, frag *horizonmodels.HorizonFragment) float64 {
	if frag == nil {
		return 1.0
	}
	overlay := propagation.BuildPropagationNodeOverlay(frag)
	weight := 1.0
	switch frag.Type {
	case horizonmodels.FragmentTypeFacility:
		weight += 0.16
	case horizonmodels.FragmentTypeOrganization:
		weight += 0.10
	case horizonmodels.FragmentTypeMarket:
		weight += 0.12
	case horizonmodels.FragmentTypeDeposit, horizonmodels.FragmentTypeProduct:
		weight += 0.08
	}
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisGeoConflict:
		if frag.Type == horizonmodels.FragmentTypeFacility || frag.Type == horizonmodels.FragmentTypeMarket || frag.Type == horizonmodels.FragmentTypeDeposit {
			weight += 0.14
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		if overlay.HasExposure("route_dependency") {
			weight += 0.12
		}
		if overlay.HasExposure("energy_input") {
			weight += 0.10
		}
	}
	return clamp(weight, 0.8, 1.35)
}

func directEdgeImportanceContribution(
	root *horizonmodels.HorizonFragment,
	neighbor *horizonmodels.HorizonFragment,
	edge *horizonmodels.DependencyEdge,
	rootID string,
) (float64, string) {
	if root == nil || neighbor == nil || edge == nil {
		return 0, ""
	}
	edgeTypeWeight := directEdgeTypeWeight(edge.Type)
	neighborWeight := typeTransmissionWeight(neighbor.Type)
	directionWeight := 0.94
	if strings.TrimSpace(edge.SourceFragmentID) == strings.TrimSpace(rootID) {
		directionWeight = 1.0
	}
	criticalityFactor := 0.70 + 0.30*clamp01(edge.Properties.Criticality)
	qualifierBonus := 0.0
	if edge.IsTransmissionSensitive() {
		qualifierBonus += edge.GetThroughputShare() * 0.10
		qualifierBonus += edge.GetCapacityConstraint() * 0.08
		qualifierBonus += edge.GetReroutePenalty() * 0.06
		qualifierBonus -= edge.GetSubstitutability() * 0.05
	}
	family := propagation.ApplyEdgeFamilyPhysics(horizonmodels.AxisPhysicalFlowSupply, root, edge, propagation.DefaultEdgeFamilyPhysicsProfile()).Family
	return clamp(edgeTypeWeight*neighborWeight*directionWeight*criticalityFactor+qualifierBonus, 0, 1.35), family
}

func directEdgeTypeWeight(edgeType horizonmodels.DependencyType) float64 {
	switch edgeType {
	case horizonmodels.DependencyTypeAffects,
		horizonmodels.DependencyTypeControlsAccess,
		horizonmodels.DependencyTypeGatekeepsDecision,
		horizonmodels.DependencyTypeDependsOn:
		return 0.98
	case horizonmodels.DependencyTypeSuppliesTo,
		horizonmodels.DependencyTypeProduces,
		horizonmodels.DependencyTypeTransformsInto,
		horizonmodels.DependencyTypeExtractsFrom,
		horizonmodels.DependencyTypeRegulates,
		horizonmodels.DependencyTypeControls,
		horizonmodels.DependencyTypeOwns:
		return 0.86
	case horizonmodels.DependencyTypeParticipatesIn,
		horizonmodels.DependencyTypeHasMarketSegment,
		horizonmodels.DependencyTypeDominates,
		horizonmodels.DependencyTypeOperatesIn,
		horizonmodels.DependencyTypeLocatedIn,
		horizonmodels.DependencyTypeAdministers:
		return 0.66
	case horizonmodels.DependencyTypeFocusesOn,
		horizonmodels.DependencyTypeSurfaces,
		horizonmodels.DependencyTypeInGeoScope,
		horizonmodels.DependencyTypeAbout:
		return 0.34
	case horizonmodels.DependencyTypeInfluences:
		return 0.26
	default:
		return 0.42
	}
}

func typeTransmissionWeight(t horizonmodels.FragmentType) float64 {
	switch t {
	case horizonmodels.FragmentTypeFacility:
		return 1.00
	case horizonmodels.FragmentTypeOrganization:
		return 0.96
	case horizonmodels.FragmentTypeMarket:
		return 0.92
	case horizonmodels.FragmentTypeProduct:
		return 0.88
	case horizonmodels.FragmentTypeDeposit:
		return 0.90
	case horizonmodels.FragmentTypeAffectedZone:
		return 0.90
	case horizonmodels.FragmentTypeGeoRegion:
		return 0.76
	case horizonmodels.FragmentTypeDecisionPoint, horizonmodels.FragmentTypeMechanism:
		return 0.78
	case horizonmodels.FragmentTypeEvent:
		return 0.70
	case horizonmodels.FragmentTypeDomain:
		return 0.50
	case horizonmodels.FragmentTypeSphere:
		return 0.34
	default:
		return 0.62
	}
}

func filterImportanceEdges(edges []*horizonmodels.DependencyEdge) []*horizonmodels.DependencyEdge {
	out := make([]*horizonmodels.DependencyEdge, 0, len(edges))
	for _, edge := range edges {
		if edge == nil || edge.IsQuarantined() {
			continue
		}
		out = append(out, edge)
	}
	return out
}

func collectNeighborIDs(rootID string, outEdges, inEdges []*horizonmodels.DependencyEdge) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(outEdges)+len(inEdges))
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || id == strings.TrimSpace(rootID) {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, edge := range outEdges {
		if edge != nil {
			add(edge.TargetFragmentID)
		}
	}
	for _, edge := range inEdges {
		if edge != nil {
			add(edge.SourceFragmentID)
		}
	}
	return out
}

func otherEndpoint(edge *horizonmodels.DependencyEdge, rootID string) string {
	if edge == nil {
		return ""
	}
	if strings.TrimSpace(edge.SourceFragmentID) == strings.TrimSpace(rootID) {
		return strings.TrimSpace(edge.TargetFragmentID)
	}
	return strings.TrimSpace(edge.SourceFragmentID)
}

func scoreConfidence(root *horizonmodels.HorizonFragment, edges []*horizonmodels.DependencyEdge, businessEdges, contextEdges int) float64 {
	if root == nil {
		return 0.60
	}
	conf := 0.68
	switch root.Metadata.EpistemicStatus {
	case horizonmodels.EpistemicStatusVerified:
		conf += 0.12
	case horizonmodels.EpistemicStatusLowConfidence:
		conf -= 0.06
	}
	conf += normalizedCount(len(edges), 10) * 0.08
	if businessEdges > 0 {
		conf += 0.05
	}
	if businessEdges > 0 && contextEdges > 0 {
		conf += 0.05
	}
	for _, edge := range edges {
		if edge != nil && (edge.Properties.Criticality > 0 || edge.HasAnyTransmissionQualifier()) {
			conf += 0.02
			break
		}
	}
	return clamp01(conf)
}

func hopDecay(hops int) float64 {
	if hops <= 0 {
		return 1.0
	}
	return math.Pow(0.82, float64(hops-1))
}

func saturatingScore(raw, scale float64) float64 {
	if raw <= 0 || scale <= 0 {
		return 0
	}
	return clamp01(1.0 - math.Exp(-raw/scale))
}

func normalizedCount(n, capN int) float64 {
	if n <= 0 || capN <= 0 {
		return 0
	}
	if n >= capN {
		return 1
	}
	return float64(n) / float64(capN)
}

func isBusinessType(t horizonmodels.FragmentType) bool {
	switch t {
	case horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeProduct,
		horizonmodels.FragmentTypeDeposit,
		horizonmodels.FragmentTypeProgram:
		return true
	default:
		return false
	}
}

func isContextType(t horizonmodels.FragmentType) bool {
	switch t {
	case horizonmodels.FragmentTypeGeoRegion,
		horizonmodels.FragmentTypeAffectedZone,
		horizonmodels.FragmentTypeEvent,
		horizonmodels.FragmentTypeDomain,
		horizonmodels.FragmentTypeSphere,
		horizonmodels.FragmentTypeDecisionPoint,
		horizonmodels.FragmentTypeMechanism,
		horizonmodels.FragmentTypeOrganization:
		return true
	default:
		return false
	}
}

func isWeakContextType(t horizonmodels.FragmentType) bool {
	switch t {
	case horizonmodels.FragmentTypeDomain, horizonmodels.FragmentTypeSphere:
		return true
	default:
		return false
	}
}

func normalizeTenant(tenantID string, fragment *horizonmodels.HorizonFragment) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		return tenantID
	}
	if fragment != nil && strings.TrimSpace(fragment.TenantID) != "" {
		return strings.TrimSpace(fragment.TenantID)
	}
	return "global"
}

func clamp(v, low, high float64) float64 {
	return math.Max(low, math.Min(high, v))
}

func clamp01(v float64) float64 {
	return clamp(v, 0, 1)
}
