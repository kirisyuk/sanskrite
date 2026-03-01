package scout

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// buildGraphSkeletonContext produces a compact context block for Critic/Research to reason about
// hierarchy completeness for MARKET and GOV entities using existing graph state.
//
// Best-effort: returns "" if it cannot build useful context.
func (e *ScoutEngine) buildGraphSkeletonContext(ctx context.Context, canonicalPath string) string {
	if e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return ""
	}
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return ""
	}

	switch {
	case strings.HasPrefix(cp, "/market/"):
		b, _ := e.buildMarketSkeletonContext(ctx, cp)
		return b
	case strings.HasPrefix(cp, "/gov/"):
		b, _ := e.buildGovSkeletonContext(ctx, cp)
		return b
	default:
		return ""
	}
}

type skeletonEdgeView struct {
	Type        string `json:"type"`
	SourcePath  string `json:"sourceCanonicalPath,omitempty"`
	TargetPath  string `json:"targetCanonicalPath,omitempty"`
	SourceID    string `json:"sourceFragmentId,omitempty"`
	TargetID    string `json:"targetFragmentId,omitempty"`
	Criticality any    `json:"criticality,omitempty"`
}

type marketSkeletonContext struct {
	Kind          string `json:"kind"` // "market_skeleton"
	CanonicalPath string `json:"canonicalPath"`
	Exists        bool   `json:"exists"`
	FragmentID    string `json:"fragmentId,omitempty"`

	Parsed struct {
		Geo      string `json:"geo,omitempty"`
		IsV2     bool   `json:"isV2"`
		Domain   string `json:"domain,omitempty"`
		Vertical string `json:"vertical,omitempty"`
		Slug     string `json:"slug,omitempty"` // legacy
	} `json:"parsed"`

	ExpectedParentPath   string `json:"expectedParentPath,omitempty"`
	ExpectedParentExists bool   `json:"expectedParentExists,omitempty"`

	ParentEdges  []skeletonEdgeView `json:"parentEdges,omitempty"`  // incoming HAS_MARKET_SEGMENT (parent -> this)
	ChildEdges   []skeletonEdgeView `json:"childEdges,omitempty"`   // outgoing HAS_MARKET_SEGMENT (this -> child)
	SiblingPaths []string           `json:"siblingPaths,omitempty"` // other children of the same parent (if any)

	NearbyPaths []string `json:"nearbyPaths,omitempty"` // existing market nodes in same geo/(domain) prefix (cap)
}

type govSkeletonContext struct {
	Kind          string `json:"kind"` // "gov_skeleton"
	CanonicalPath string `json:"canonicalPath"`
	Exists        bool   `json:"exists"`
	FragmentID    string `json:"fragmentId,omitempty"`

	Parsed struct {
		GeoISO2 string `json:"geoIso2,omitempty"`
		Slug    string `json:"slug,omitempty"`
	} `json:"parsed"`

	GovType       string `json:"govType,omitempty"`
	ParentGovPath string `json:"parentGovPath,omitempty"`

	ParentEdges  []skeletonEdgeView `json:"parentEdges,omitempty"` // incoming ADMINISTERS (parent -> this)
	ChildEdges   []skeletonEdgeView `json:"childEdges,omitempty"`  // outgoing ADMINISTERS (this -> child)
	SiblingPaths []string           `json:"siblingPaths,omitempty"`

	// Strategic decision layer: edges connecting this gov entity to decision points and mechanisms.
	DecisionEdges []skeletonEdgeView `json:"decisionEdges,omitempty"` // GATEKEEPS_DECISION, CONTROLS_ACCESS, USES_MECHANISM, APPLIES_TO outgoing
	IncomingStratEdges []skeletonEdgeView `json:"incomingStrategicEdges,omitempty"` // strategic edges targeting this gov entity

	NearbyPaths []string `json:"nearbyPaths,omitempty"` // existing gov nodes in same jurisdiction prefix (cap)
}

func (e *ScoutEngine) buildMarketSkeletonContext(ctx context.Context, canonicalPath string) (string, error) {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || !strings.HasPrefix(cp, "/market/") {
		return "", nil
	}

	var out marketSkeletonContext
	out.Kind = "market_skeleton"
	out.CanonicalPath = cp

	// Parse:
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// parts: ["market", geo, ...]
	if len(parts) >= 3 {
		out.Parsed.Geo = strings.TrimSpace(parts[1])
		rest := parts[2:]
		// v2: /market/{geo}/{domain}/{vertical}
		if len(rest) >= 2 {
			out.Parsed.IsV2 = true
			out.Parsed.Domain = strings.TrimSpace(rest[0])
			out.Parsed.Vertical = strings.TrimSpace(rest[1])
		} else if len(rest) == 1 {
			out.Parsed.IsV2 = false
			out.Parsed.Slug = strings.TrimSpace(rest[0])
		}
	}

	// Resolve fragment if it exists
	frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, cp)
	if err == nil && frag != nil && frag.ID != "" {
		out.Exists = true
		out.FragmentID = strings.TrimSpace(frag.ID)
		// Prefer explicit fields when present
		if strings.TrimSpace(frag.ParentMarketPath) != "" {
			out.ExpectedParentPath = strings.TrimSpace(frag.ParentMarketPath)
		}
	}

	// Compute expected parent path when not set, based on canonical path conventions.
	if out.ExpectedParentPath == "" && out.Parsed.Geo != "" {
		if out.Parsed.IsV2 && out.Parsed.Domain != "" && out.Parsed.Vertical != "" {
			// Prefer dictionary-driven parent when possible (MarketHierarchyBuilder uses SegmentVerticalParent).
			pv := taxonomy.SegmentVerticalParent(out.Parsed.Vertical)
			if pv != "" && pv != out.Parsed.Vertical {
				out.ExpectedParentPath = "/market/" + out.Parsed.Geo + "/" + out.Parsed.Domain + "/" + pv
			} else {
				out.ExpectedParentPath = "/market/" + out.Parsed.Geo + "/" + out.Parsed.Domain
			}
		} else if !out.Parsed.IsV2 && out.Parsed.Slug != "" {
			parentSlug := inferParentMarketSlug(out.Parsed.Slug)
			if parentSlug != "" && parentSlug != out.Parsed.Slug {
				out.ExpectedParentPath = "/market/" + out.Parsed.Geo + "/" + parentSlug
			}
		}
	}

	if out.ExpectedParentPath != "" {
		if pf, err := e.fragmentsRepo.GetByCanonicalPath(ctx, out.ExpectedParentPath); err == nil && pf != nil && pf.ID != "" {
			out.ExpectedParentExists = true
		}
	}

	// Edges (only if fragment exists in DB; otherwise we can't query edges by ID).
	if out.FragmentID != "" {
		edgeType := models.DependencyTypeHasMarketSegment
		parents, _ := e.edgesRepo.ListByTarget(ctx, "global", out.FragmentID, &edgeType, 5)
		children, _ := e.edgesRepo.ListBySource(ctx, "global", out.FragmentID, &edgeType, 15)
		legacyType := models.DependencyTypeHasSubmarket
		if legParents, _ := e.edgesRepo.ListByTarget(ctx, "global", out.FragmentID, &legacyType, 5); len(legParents) > 0 {
			parents = append(parents, legParents...)
		}
		if legChildren, _ := e.edgesRepo.ListBySource(ctx, "global", out.FragmentID, &legacyType, 15); len(legChildren) > 0 {
			children = append(children, legChildren...)
		}

		// Collect all fragment IDs referenced by edges, then batch-fetch.
		fragIDSet := make(map[string]struct{})
		for _, ed := range parents {
			if ed == nil {
				continue
			}
			fragIDSet[strings.TrimSpace(ed.SourceFragmentID)] = struct{}{}
			fragIDSet[strings.TrimSpace(ed.TargetFragmentID)] = struct{}{}
		}
		for _, ed := range children {
			if ed == nil {
				continue
			}
			fragIDSet[strings.TrimSpace(ed.SourceFragmentID)] = struct{}{}
			fragIDSet[strings.TrimSpace(ed.TargetFragmentID)] = struct{}{}
		}

		// Sibling edges: load from parent before batch-fetch so we include sibling target IDs.
		var sibEdges []*models.DependencyEdge
		if len(parents) > 0 {
			firstParentSourceID := ""
			for _, ed := range parents {
				if ed != nil && strings.TrimSpace(ed.SourceFragmentID) != "" {
					firstParentSourceID = strings.TrimSpace(ed.SourceFragmentID)
					break
				}
			}
			if firstParentSourceID != "" {
				sibEdges, _ = e.edgesRepo.ListBySource(ctx, "global", firstParentSourceID, &edgeType, 20)
				if legSib, _ := e.edgesRepo.ListBySource(ctx, "global", firstParentSourceID, &legacyType, 20); len(legSib) > 0 {
					sibEdges = append(sibEdges, legSib...)
				}
				for _, ed := range sibEdges {
					if ed != nil && strings.TrimSpace(ed.TargetFragmentID) != "" {
						fragIDSet[strings.TrimSpace(ed.TargetFragmentID)] = struct{}{}
					}
				}
			}
		}

		var fragIDs []string
		for id := range fragIDSet {
			if id != "" {
				fragIDs = append(fragIDs, id)
			}
		}
		fragMap, _ := e.fragmentsRepo.GetMany(ctx, fragIDs)
		if fragMap == nil {
			fragMap = map[string]*models.HorizonFragment{}
		}

		resolvePath := func(id string) string {
			if f := fragMap[id]; f != nil {
				return strings.TrimSpace(f.CanonicalPath)
			}
			return ""
		}

		for _, ed := range parents {
			if ed == nil {
				continue
			}
			out.ParentEdges = append(out.ParentEdges, skeletonEdgeView{
				Type:       string(ed.Type),
				SourceID:   strings.TrimSpace(ed.SourceFragmentID),
				TargetID:   strings.TrimSpace(ed.TargetFragmentID),
				SourcePath: resolvePath(strings.TrimSpace(ed.SourceFragmentID)),
				TargetPath: resolvePath(strings.TrimSpace(ed.TargetFragmentID)),
				Criticality: ed.Properties.Criticality,
			})
		}

		for _, ed := range children {
			if ed == nil {
				continue
			}
			out.ChildEdges = append(out.ChildEdges, skeletonEdgeView{
				Type:       string(ed.Type),
				SourceID:   strings.TrimSpace(ed.SourceFragmentID),
				TargetID:   strings.TrimSpace(ed.TargetFragmentID),
				SourcePath: resolvePath(strings.TrimSpace(ed.SourceFragmentID)),
				TargetPath: resolvePath(strings.TrimSpace(ed.TargetFragmentID)),
				Criticality: ed.Properties.Criticality,
			})
		}

		for _, ed := range sibEdges {
			if ed == nil {
				continue
			}
			tgtID := strings.TrimSpace(ed.TargetFragmentID)
			if tgtID == "" || tgtID == out.FragmentID {
				continue
			}
			if p := resolvePath(tgtID); p != "" {
				out.SiblingPaths = append(out.SiblingPaths, p)
			}
		}
	}

	// Nearby nodes in the same region/domain namespace (prefix search; cap to keep prompt small)
	prefix := ""
	if out.Parsed.Geo != "" && out.Parsed.IsV2 && out.Parsed.Domain != "" {
		prefix = "/market/" + out.Parsed.Geo + "/" + out.Parsed.Domain + "/"
	} else if out.Parsed.Geo != "" {
		prefix = "/market/" + out.Parsed.Geo + "/"
	}
	if prefix != "" {
		if nearby, err := e.fragmentsRepo.SearchPrefix(ctx, prefix, 15); err == nil && len(nearby) > 0 {
			for _, f := range nearby {
				if f == nil {
					continue
				}
				p := strings.TrimSpace(f.CanonicalPath)
				if p != "" && p != cp {
					out.NearbyPaths = append(out.NearbyPaths, p)
				}
			}
		}
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (e *ScoutEngine) buildGovSkeletonContext(ctx context.Context, canonicalPath string) (string, error) {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || !strings.HasPrefix(cp, "/gov/") {
		return "", nil
	}

	var out govSkeletonContext
	out.Kind = "gov_skeleton"
	out.CanonicalPath = cp

	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// parts: ["gov", iso2, slug...]
	if len(parts) >= 3 {
		out.Parsed.GeoISO2 = strings.TrimSpace(parts[1])
		out.Parsed.Slug = strings.TrimSpace(parts[2])
	}

	frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, cp)
	if err == nil && frag != nil && frag.ID != "" {
		out.Exists = true
		out.FragmentID = strings.TrimSpace(frag.ID)
		out.GovType = strings.TrimSpace(frag.GovType)
		out.ParentGovPath = strings.TrimSpace(frag.ParentGovPath)
	}

	if out.FragmentID != "" {
		edgeType := models.DependencyTypeAdministers
		parents, _ := e.edgesRepo.ListByTarget(ctx, "global", out.FragmentID, &edgeType, 5)
		children, _ := e.edgesRepo.ListBySource(ctx, "global", out.FragmentID, &edgeType, 15)

		// Strategic decision layer: query edges connecting this gov entity to decision points and mechanisms.
		stratOutgoing := e.queryGovStrategicEdgesOutgoing(ctx, out.FragmentID)
		stratIncoming := e.queryGovStrategicEdgesIncoming(ctx, out.FragmentID)

		// Collect all referenced fragment IDs for batch fetch.
		fragIDSet := make(map[string]struct{})
		collectEdgeFragIDs := func(edges []*models.DependencyEdge) {
			for _, ed := range edges {
				if ed == nil {
					continue
				}
				if sid := strings.TrimSpace(ed.SourceFragmentID); sid != "" {
					fragIDSet[sid] = struct{}{}
				}
				if tid := strings.TrimSpace(ed.TargetFragmentID); tid != "" {
					fragIDSet[tid] = struct{}{}
				}
			}
		}
		collectEdgeFragIDs(parents)
		collectEdgeFragIDs(children)
		collectEdgeFragIDs(stratOutgoing)
		collectEdgeFragIDs(stratIncoming)

		var sibEdges []*models.DependencyEdge
		if len(parents) > 0 {
			firstParentSourceID := ""
			for _, ed := range parents {
				if ed != nil && strings.TrimSpace(ed.SourceFragmentID) != "" {
					firstParentSourceID = strings.TrimSpace(ed.SourceFragmentID)
					break
				}
			}
			if firstParentSourceID != "" {
				sibEdges, _ = e.edgesRepo.ListBySource(ctx, "global", firstParentSourceID, &edgeType, 20)
				for _, ed := range sibEdges {
					if ed != nil && strings.TrimSpace(ed.TargetFragmentID) != "" {
						fragIDSet[strings.TrimSpace(ed.TargetFragmentID)] = struct{}{}
					}
				}
			}
		}

		var fragIDs []string
		for id := range fragIDSet {
			if id != "" {
				fragIDs = append(fragIDs, id)
			}
		}
		fragMap, _ := e.fragmentsRepo.GetMany(ctx, fragIDs)
		if fragMap == nil {
			fragMap = map[string]*models.HorizonFragment{}
		}

		resolvePath := func(id string) string {
			if f := fragMap[id]; f != nil {
				return strings.TrimSpace(f.CanonicalPath)
			}
			return ""
		}

		toView := func(ed *models.DependencyEdge) skeletonEdgeView {
			return skeletonEdgeView{
				Type:        string(ed.Type),
				SourceID:    strings.TrimSpace(ed.SourceFragmentID),
				TargetID:    strings.TrimSpace(ed.TargetFragmentID),
				SourcePath:  resolvePath(strings.TrimSpace(ed.SourceFragmentID)),
				TargetPath:  resolvePath(strings.TrimSpace(ed.TargetFragmentID)),
				Criticality: ed.Properties.Criticality,
			}
		}

		for _, ed := range parents {
			if ed != nil {
				out.ParentEdges = append(out.ParentEdges, toView(ed))
			}
		}
		for _, ed := range children {
			if ed != nil {
				out.ChildEdges = append(out.ChildEdges, toView(ed))
			}
		}
		for _, ed := range stratOutgoing {
			if ed != nil {
				out.DecisionEdges = append(out.DecisionEdges, toView(ed))
			}
		}
		for _, ed := range stratIncoming {
			if ed != nil {
				out.IncomingStratEdges = append(out.IncomingStratEdges, toView(ed))
			}
		}

		for _, ed := range sibEdges {
			if ed == nil {
				continue
			}
			tgtID := strings.TrimSpace(ed.TargetFragmentID)
			if tgtID == "" || tgtID == out.FragmentID {
				continue
			}
			if p := resolvePath(tgtID); p != "" {
				out.SiblingPaths = append(out.SiblingPaths, p)
			}
		}
	}

	// Nearby nodes in same jurisdiction
	if out.Parsed.GeoISO2 != "" {
		prefix := "/gov/" + out.Parsed.GeoISO2 + "/"
		if nearby, err := e.fragmentsRepo.SearchPrefix(ctx, prefix, 15); err == nil && len(nearby) > 0 {
			for _, f := range nearby {
				if f == nil {
					continue
				}
				p := strings.TrimSpace(f.CanonicalPath)
				if p != "" && p != cp {
					out.NearbyPaths = append(out.NearbyPaths, p)
				}
			}
		}
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// govStrategicEdgeTypes lists edge types that connect gov entities to the decision/mechanism layer.
var govStrategicEdgeTypes = []models.DependencyType{
	models.DependencyTypeGatekeepsDecision,
	models.DependencyTypeControlsAccess,
	models.DependencyTypeUsesMechanism,
	models.DependencyTypeImposesCondition,
	models.DependencyTypeAppliesTo,
	models.DependencyTypeRegulates,
}

// queryGovStrategicEdgesOutgoing returns outgoing strategic edges from this gov entity
// (e.g., gov GATEKEEPS_DECISION decision_point, gov CONTROLS_ACCESS org, gov USES_MECHANISM mechanism).
func (e *ScoutEngine) queryGovStrategicEdgesOutgoing(ctx context.Context, fragmentID string) []*models.DependencyEdge {
	var out []*models.DependencyEdge
	for _, et := range govStrategicEdgeTypes {
		et := et
		edges, err := e.edgesRepo.ListBySource(ctx, "global", fragmentID, &et, 10)
		if err == nil {
			out = append(out, edges...)
		}
	}
	return out
}

// queryGovStrategicEdgesIncoming returns incoming strategic edges targeting this gov entity
// (e.g., decision_point APPLIES_TO gov, mechanism APPLIES_TO gov, org DEPENDS_ON gov).
func (e *ScoutEngine) queryGovStrategicEdgesIncoming(ctx context.Context, fragmentID string) []*models.DependencyEdge {
	var out []*models.DependencyEdge
	for _, et := range govStrategicEdgeTypes {
		et := et
		edges, err := e.edgesRepo.ListByTarget(ctx, "global", fragmentID, &et, 10)
		if err == nil {
			out = append(out, edges...)
		}
	}
	return out
}
