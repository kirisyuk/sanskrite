package importance

import (
	"context"
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
)

type ExplainConfig struct {
	MaxDirectContributors int `json:"maxDirectContributors"`
	MaxAxisPaths          int `json:"maxAxisPaths"`
}

type Explanation struct {
	FragmentID             string                     `json:"fragmentId"`
	CanonicalPath          string                     `json:"canonicalPath"`
	Type                   horizonmodels.FragmentType `json:"type"`
	Scorecard              Scorecard                  `json:"scorecard"`
	StrategicBreakdown     StrategicBreakdown         `json:"strategicBreakdown"`
	BridgeBreakdown        BridgeBreakdown            `json:"bridgeBreakdown"`
	StructuralContributors []DirectContributor        `json:"structuralContributors,omitempty"`
	BusinessComponents     []AxisComponentExplanation `json:"businessComponents,omitempty"`
	ConflictComponent      AxisComponentExplanation   `json:"conflictComponent"`
}

type StrategicBreakdown struct {
	StructuralComponent  float64 `json:"structuralComponent"`
	BridgeComponent      float64 `json:"bridgeComponent"`
	BusinessComponent    float64 `json:"businessComponent"`
	ConflictComponent    float64 `json:"conflictComponent"`
	CriticalityComponent float64 `json:"criticalityComponent"`
	StructuralScore      float64 `json:"structuralScore"`
	BridgeScore          float64 `json:"bridgeScore"`
	BusinessScore        float64 `json:"businessScore"`
	ConflictScore        float64 `json:"conflictScore"`
	CriticalityScore     float64 `json:"criticalityScore"`
	StrategicImportance  float64 `json:"strategicImportance"`
}

type BridgeBreakdown struct {
	OverlayBridgeScore    float64 `json:"overlayBridgeScore"`
	SemanticDiversity     float64 `json:"semanticDiversity"`
	NeighborTypeDiversity float64 `json:"neighborTypeDiversity"`
	FamilyDiversity       float64 `json:"familyDiversity"`
	ContextBridge         float64 `json:"contextBridge"`
	BridgeImportance      float64 `json:"bridgeImportance"`
}

type DirectContributor struct {
	EdgeID                string                       `json:"edgeId,omitempty"`
	EdgeType              horizonmodels.DependencyType `json:"edgeType"`
	Direction             string                       `json:"direction"`
	NeighborID            string                       `json:"neighborId"`
	NeighborPath          string                       `json:"neighborPath"`
	NeighborType          horizonmodels.FragmentType   `json:"neighborType"`
	SemanticGroup         string                       `json:"semanticGroup"`
	Family                string                       `json:"family,omitempty"`
	Contribution          float64                      `json:"contribution"`
	Criticality           float64                      `json:"criticality"`
	BusinessNeighbor      bool                         `json:"businessNeighbor"`
	ContextNeighbor       bool                         `json:"contextNeighbor"`
	TransmissionSensitive bool                         `json:"transmissionSensitive"`
}

type AxisComponentExplanation struct {
	Axis                   string            `json:"axis"`
	Score                  float64           `json:"score"`
	WeightedContribution   float64           `json:"weightedContribution"`
	VisitedNodes           int               `json:"visitedNodes"`
	TraversedEdges         int               `json:"traversedEdges"`
	ReachableBusinessTails int               `json:"reachableBusinessTails"`
	ZeroReason             string            `json:"zeroReason,omitempty"`
	TopPaths               []AxisPathExplain `json:"topPaths,omitempty"`
}

type AxisPathExplain struct {
	TailID     string                     `json:"tailId"`
	TailPath   string                     `json:"tailPath"`
	TailType   horizonmodels.FragmentType `json:"tailType"`
	PathWeight float64                    `json:"pathWeight"`
	TailWeight float64                    `json:"tailWeight"`
	Hops       int                        `json:"hops"`
	Steps      []AxisStepExplain          `json:"steps"`
}

type AxisStepExplain struct {
	FromID              string                       `json:"fromId"`
	FromPath            string                       `json:"fromPath"`
	FromType            horizonmodels.FragmentType   `json:"fromType"`
	ToID                string                       `json:"toId"`
	ToPath              string                       `json:"toPath"`
	ToType              horizonmodels.FragmentType   `json:"toType"`
	EdgeID              string                       `json:"edgeId,omitempty"`
	EdgeType            horizonmodels.DependencyType `json:"edgeType"`
	Forward             bool                         `json:"forward"`
	EdgeWeight          float64                      `json:"edgeWeight"`
	BaseWeight          float64                      `json:"baseWeight"`
	PhysicsMultiplier   float64                      `json:"physicsMultiplier"`
	CriticalityFactor   float64                      `json:"criticalityFactor"`
	AmplificationFactor float64                      `json:"amplificationFactor"`
	NeighborBoost       float64                      `json:"neighborBoost"`
	Family              string                       `json:"family,omitempty"`
	PhysicsReasons      []string                     `json:"physicsReasons,omitempty"`
}

func DefaultExplainConfig() ExplainConfig {
	return ExplainConfig{
		MaxDirectContributors: 6,
		MaxAxisPaths:          4,
	}
}

func (c ExplainConfig) normalized() ExplainConfig {
	if c.MaxDirectContributors <= 0 {
		c.MaxDirectContributors = DefaultExplainConfig().MaxDirectContributors
	}
	if c.MaxAxisPaths <= 0 {
		c.MaxAxisPaths = DefaultExplainConfig().MaxAxisPaths
	}
	return c
}

func (s *Scorer) ExplainFragment(ctx context.Context, tenantID, fragmentID string, cfg ExplainConfig) (Explanation, *horizonmodels.HorizonFragment, error) {
	if s == nil || s.fragmentsRepo == nil || s.edgesRepo == nil {
		return Explanation{}, nil, fmt.Errorf("importance scorer is not configured")
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return Explanation{}, nil, fmt.Errorf("fragment id is required")
	}
	frag, err := s.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return Explanation{}, nil, err
	}
	if frag == nil {
		return Explanation{}, nil, fmt.Errorf("fragment not found: %s", fragmentID)
	}
	snap, err := s.LoadSnapshot(ctx, tenantID, frag)
	if err != nil {
		return Explanation{}, nil, err
	}
	return ExplainSnapshot(snap, s.cfg, cfg), frag, nil
}

func (s *Scorer) ExplainFragmentByPath(ctx context.Context, tenantID, canonicalPath string, cfg ExplainConfig) (Explanation, *horizonmodels.HorizonFragment, error) {
	if s == nil || s.fragmentsRepo == nil || s.edgesRepo == nil {
		return Explanation{}, nil, fmt.Errorf("importance scorer is not configured")
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return Explanation{}, nil, fmt.Errorf("canonical path is required")
	}
	frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil {
		return Explanation{}, nil, err
	}
	if frag == nil {
		return Explanation{}, nil, fmt.Errorf("fragment not found: %s", canonicalPath)
	}
	snap, err := s.LoadSnapshot(ctx, tenantID, frag)
	if err != nil {
		return Explanation{}, nil, err
	}
	return ExplainSnapshot(snap, s.cfg, cfg), frag, nil
}

func ExplainSnapshot(snap Snapshot, cfg Config, explainCfg ExplainConfig) Explanation {
	explainCfg = explainCfg.normalized()
	root := snap.Nodes[snap.RootID]
	if root == nil {
		return Explanation{}
	}
	score := ScoreSnapshot(snap, cfg)
	overlay := propagation.BuildPropagationNodeOverlay(root)
	directEdges := append([]*horizonmodels.DependencyEdge{}, snap.Out[snap.RootID]...)
	directEdges = append(directEdges, snap.In[snap.RootID]...)

	semanticGroups := map[string]struct{}{}
	neighborTypes := map[horizonmodels.FragmentType]struct{}{}
	strongFamilies := map[string]struct{}{}
	businessEdges := 0
	contextEdges := 0
	directContributors := make([]DirectContributor, 0, len(directEdges))

	for _, edge := range directEdges {
		if edge == nil {
			continue
		}
		neighborID := otherEndpoint(edge, snap.RootID)
		neighbor := snap.Nodes[neighborID]
		if neighbor == nil {
			continue
		}
		semanticGroup := horizonmodels.SemanticGroupForEdgeType(edge.Type)
		semanticGroups[semanticGroup] = struct{}{}
		neighborTypes[neighbor.Type] = struct{}{}
		businessNeighbor := isBusinessType(neighbor.Type)
		contextNeighbor := isContextType(neighbor.Type)
		if businessNeighbor {
			businessEdges++
		}
		if contextNeighbor {
			contextEdges++
		}
		contribution, family := directEdgeImportanceContribution(root, neighbor, edge, snap.RootID)
		if family != "" {
			strongFamilies[family] = struct{}{}
		}
		directContributors = append(directContributors, DirectContributor{
			EdgeID:                strings.TrimSpace(edge.ID),
			EdgeType:              edge.Type,
			Direction:             directContributorDirection(edge, snap.RootID),
			NeighborID:            strings.TrimSpace(neighbor.ID),
			NeighborPath:          strings.TrimSpace(neighbor.CanonicalPath),
			NeighborType:          neighbor.Type,
			SemanticGroup:         semanticGroup,
			Family:                family,
			Contribution:          contribution,
			Criticality:           clamp01(edge.Properties.Criticality),
			BusinessNeighbor:      businessNeighbor,
			ContextNeighbor:       contextNeighbor,
			TransmissionSensitive: edge.IsTransmissionSensitive(),
		})
	}

	sort.Slice(directContributors, func(i, j int) bool {
		if directContributors[i].Contribution == directContributors[j].Contribution {
			return directContributors[i].NeighborPath < directContributors[j].NeighborPath
		}
		return directContributors[i].Contribution > directContributors[j].Contribution
	})
	if len(directContributors) > explainCfg.MaxDirectContributors {
		directContributors = directContributors[:explainCfg.MaxDirectContributors]
	}

	semanticDiversity := normalizedCount(len(semanticGroups), 5)
	typeDiversity := normalizedCount(len(neighborTypes), 6)
	contextBridge := 0.0
	switch {
	case businessEdges > 0 && contextEdges > 0:
		contextBridge = 1.0
	case businessEdges > 0 || contextEdges > 0:
		contextBridge = 0.45
	}
	familyDiversity := normalizedCount(len(strongFamilies), 4)
	bridgeScore := clamp01(
		0.28*overlay.BridgeScore +
			0.24*semanticDiversity +
			0.18*typeDiversity +
			0.12*familyDiversity +
			0.18*contextBridge,
	)

	businessAxes := []struct {
		axis   string
		weight float64
	}{
		{axis: horizonmodels.AxisPhysicalFlowSupply, weight: 0.42},
		{axis: horizonmodels.AxisOperationalContinuity, weight: 0.28},
		{axis: horizonmodels.AxisLogisticsCapacity, weight: 0.16},
		{axis: horizonmodels.AxisLogisticsLatency, weight: 0.14},
	}
	businessComponents := make([]AxisComponentExplanation, 0, len(businessAxes))
	for _, item := range businessAxes {
		trace := traceAxisReach(snap, cfg, item.axis, explainCfg.MaxAxisPaths)
		businessComponents = append(businessComponents, AxisComponentExplanation{
			Axis:                   item.axis,
			Score:                  axisReachScore(snap, cfg, item.axis),
			WeightedContribution:   item.weight * axisReachScore(snap, cfg, item.axis),
			VisitedNodes:           trace.VisitedNodes,
			TraversedEdges:         trace.TraversedEdges,
			ReachableBusinessTails: trace.ReachableBusinessTails,
			ZeroReason:             trace.ZeroReason,
			TopPaths:               trace.TopPaths,
		})
	}

	conflictTrace := traceAxisReach(snap, cfg, horizonmodels.AxisGeoConflict, explainCfg.MaxAxisPaths)
	conflictComponent := AxisComponentExplanation{
		Axis:                   horizonmodels.AxisGeoConflict,
		Score:                  score.ConflictBlastRadius,
		WeightedContribution:   0.16 * score.ConflictBlastRadius,
		VisitedNodes:           conflictTrace.VisitedNodes,
		TraversedEdges:         conflictTrace.TraversedEdges,
		ReachableBusinessTails: conflictTrace.ReachableBusinessTails,
		ZeroReason:             conflictTrace.ZeroReason,
		TopPaths:               conflictTrace.TopPaths,
	}

	return Explanation{
		FragmentID:    strings.TrimSpace(root.ID),
		CanonicalPath: strings.TrimSpace(root.CanonicalPath),
		Type:          root.Type,
		Scorecard:     score,
		StrategicBreakdown: StrategicBreakdown{
			StructuralComponent:  0.30 * score.StructuralImportance,
			BridgeComponent:      0.22 * score.BridgeImportance,
			BusinessComponent:    0.20 * score.BusinessBlastRadius,
			ConflictComponent:    0.16 * score.ConflictBlastRadius,
			CriticalityComponent: 0.12 * overlay.CriticalityScore,
			StructuralScore:      score.StructuralImportance,
			BridgeScore:          score.BridgeImportance,
			BusinessScore:        score.BusinessBlastRadius,
			ConflictScore:        score.ConflictBlastRadius,
			CriticalityScore:     overlay.CriticalityScore,
			StrategicImportance:  score.StrategicImportance,
		},
		BridgeBreakdown: BridgeBreakdown{
			OverlayBridgeScore:    overlay.BridgeScore,
			SemanticDiversity:     semanticDiversity,
			NeighborTypeDiversity: typeDiversity,
			FamilyDiversity:       familyDiversity,
			ContextBridge:         contextBridge,
			BridgeImportance:      bridgeScore,
		},
		StructuralContributors: directContributors,
		BusinessComponents:     businessComponents,
		ConflictComponent:      conflictComponent,
	}
}

type axisTrace struct {
	VisitedNodes           int
	TraversedEdges         int
	ReachableBusinessTails int
	ZeroReason             string
	TopPaths               []AxisPathExplain
}

func traceAxisReach(snap Snapshot, cfg Config, axis string, maxPaths int) axisTrace {
	root := snap.Nodes[snap.RootID]
	if root == nil {
		return axisTrace{ZeroReason: "root fragment is missing"}
	}
	profiles := horizonmodels.DefaultPropagationProfiles()
	profile, ok := profiles[axis]
	if !ok {
		return axisTrace{ZeroReason: "axis profile is missing"}
	}
	physicsProfile := propagation.DefaultEdgeFamilyPhysicsProfile()
	type queueState struct {
		id       string
		strength float64
		hops     int
		steps    []AxisStepExplain
	}
	best := map[string]float64{snap.RootID: 1.0}
	queue := []queueState{{id: snap.RootID, strength: 1.0, hops: 0}}
	tails := map[string]AxisPathExplain{}
	traversedEdges := 0

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
			breakdown := traversalEdgeBreakdown(axis, profile, physicsProfile, currentFrag, nextFrag, edge, step.forward)
			if breakdown.Final <= 0 {
				continue
			}
			traversedEdges++
			pathWeight := cur.strength * breakdown.Final * hopDecay(cur.hops+1) * axisNodeWeight(axis, nextFrag)
			if pathWeight < cfg.MinPathStrength {
				continue
			}
			if prev, ok := best[step.nextID]; ok && prev >= pathWeight {
				continue
			}
			nextSteps := append(append([]AxisStepExplain{}, cur.steps...), AxisStepExplain{
				FromID:              strings.TrimSpace(currentFrag.ID),
				FromPath:            strings.TrimSpace(currentFrag.CanonicalPath),
				FromType:            currentFrag.Type,
				ToID:                strings.TrimSpace(nextFrag.ID),
				ToPath:              strings.TrimSpace(nextFrag.CanonicalPath),
				ToType:              nextFrag.Type,
				EdgeID:              strings.TrimSpace(edge.ID),
				EdgeType:            edge.Type,
				Forward:             step.forward,
				EdgeWeight:          breakdown.Final,
				BaseWeight:          breakdown.Base,
				PhysicsMultiplier:   breakdown.PhysicsMultiplier,
				CriticalityFactor:   breakdown.CriticalityFactor,
				AmplificationFactor: breakdown.AmplificationFactor,
				NeighborBoost:       breakdown.NeighborBoost,
				Family:              breakdown.Family,
				PhysicsReasons:      breakdown.PhysicsReasons,
			})
			best[step.nextID] = pathWeight
			if step.nextID != snap.RootID && isBusinessType(nextFrag.Type) {
				tailWeight := pathWeight * axisBusinessTailWeight(axis, nextFrag)
				prevTail, ok := tails[step.nextID]
				if !ok || prevTail.TailWeight < tailWeight {
					tails[step.nextID] = AxisPathExplain{
						TailID:     strings.TrimSpace(nextFrag.ID),
						TailPath:   strings.TrimSpace(nextFrag.CanonicalPath),
						TailType:   nextFrag.Type,
						PathWeight: pathWeight,
						TailWeight: tailWeight,
						Hops:       len(nextSteps),
						Steps:      nextSteps,
					}
				}
			}
			queue = append(queue, queueState{id: step.nextID, strength: pathWeight, hops: cur.hops + 1, steps: nextSteps})
		}
	}

	paths := make([]AxisPathExplain, 0, len(tails))
	for _, path := range tails {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].TailWeight == paths[j].TailWeight {
			return paths[i].TailPath < paths[j].TailPath
		}
		return paths[i].TailWeight > paths[j].TailWeight
	})
	if maxPaths > 0 && len(paths) > maxPaths {
		paths = paths[:maxPaths]
	}

	zeroReason := ""
	if len(tails) == 0 {
		switch {
		case len(best) <= 1:
			zeroReason = fmt.Sprintf("no traversable neighbors above min strength %.2f", cfg.MinPathStrength)
		default:
			zeroReason = fmt.Sprintf("no business tails reached within %d hops above min strength %.2f", cfg.MaxBlastHops, cfg.MinPathStrength)
		}
	}

	return axisTrace{
		VisitedNodes:           maxInt(len(best)-1, 0),
		TraversedEdges:         traversedEdges,
		ReachableBusinessTails: len(tails),
		ZeroReason:             zeroReason,
		TopPaths:               paths,
	}
}

func directContributorDirection(edge *horizonmodels.DependencyEdge, rootID string) string {
	if edge == nil {
		return ""
	}
	if strings.TrimSpace(edge.SourceFragmentID) == strings.TrimSpace(rootID) {
		return "out"
	}
	return "in"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
