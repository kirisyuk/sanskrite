package importance

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestScoreSnapshot_PrefersStrongBusinessEdgesOverWeakContextDegree(t *testing.T) {
	rootStrong := &horizonmodels.HorizonFragment{
		ID:            "root-strong",
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/critical-energy-co",
	}
	rootWeak := &horizonmodels.HorizonFragment{
		ID:            "root-weak",
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/context-heavy-shell",
	}
	fac := &horizonmodels.HorizonFragment{ID: "fac", Type: horizonmodels.FragmentTypeFacility, CanonicalPath: "/fac/us/export-terminal"}
	product := &horizonmodels.HorizonFragment{ID: "prod", Type: horizonmodels.FragmentTypeProduct, CanonicalPath: "/product/raw/hydrocarbon/extraction/crude-oil"}
	market := &horizonmodels.HorizonFragment{ID: "mkt", Type: horizonmodels.FragmentTypeMarket, CanonicalPath: "/market/us/energy"}
	sphereA := &horizonmodels.HorizonFragment{ID: "s1", Type: horizonmodels.FragmentTypeSphere, CanonicalPath: "/sphere/energy"}
	sphereB := &horizonmodels.HorizonFragment{ID: "s2", Type: horizonmodels.FragmentTypeSphere, CanonicalPath: "/sphere/geopolitics"}
	domain := &horizonmodels.HorizonFragment{ID: "d1", Type: horizonmodels.FragmentTypeDomain, CanonicalPath: "/domain/v1/ecosystem-mapping/geo/us"}
	event := &horizonmodels.HorizonFragment{ID: "e1", Type: horizonmodels.FragmentTypeEvent, CanonicalPath: "/event/market-noise"}

	strongSnap := Snapshot{
		RootID: rootStrong.ID,
		Nodes: map[string]*horizonmodels.HorizonFragment{
			rootStrong.ID: rootStrong,
			fac.ID:        fac,
			product.ID:    product,
			market.ID:     market,
		},
		Out: map[string][]*horizonmodels.DependencyEdge{
			rootStrong.ID: {
				{ID: "e1", SourceFragmentID: rootStrong.ID, TargetFragmentID: fac.ID, Type: horizonmodels.DependencyTypeSuppliesTo, Properties: horizonmodels.EdgeProperties{Criticality: 0.9}},
				{ID: "e2", SourceFragmentID: rootStrong.ID, TargetFragmentID: product.ID, Type: horizonmodels.DependencyTypeProduces, Properties: horizonmodels.EdgeProperties{Criticality: 0.8}},
				{ID: "e3", SourceFragmentID: rootStrong.ID, TargetFragmentID: market.ID, Type: horizonmodels.DependencyTypeParticipatesIn, Properties: horizonmodels.EdgeProperties{Criticality: 0.6}},
			},
			fac.ID: {
				{ID: "e4", SourceFragmentID: fac.ID, TargetFragmentID: market.ID, Type: horizonmodels.DependencyTypeSuppliesTo, Properties: horizonmodels.EdgeProperties{Criticality: 0.8}},
			},
		},
		In: map[string][]*horizonmodels.DependencyEdge{},
	}
	weakSnap := Snapshot{
		RootID: rootWeak.ID,
		Nodes: map[string]*horizonmodels.HorizonFragment{
			rootWeak.ID: rootWeak,
			sphereA.ID:  sphereA,
			sphereB.ID:  sphereB,
			domain.ID:   domain,
			event.ID:    event,
		},
		Out: map[string][]*horizonmodels.DependencyEdge{},
		In: map[string][]*horizonmodels.DependencyEdge{
			rootWeak.ID: {
				{ID: "w1", SourceFragmentID: sphereA.ID, TargetFragmentID: rootWeak.ID, Type: horizonmodels.DependencyTypeInfluences},
				{ID: "w2", SourceFragmentID: sphereB.ID, TargetFragmentID: rootWeak.ID, Type: horizonmodels.DependencyTypeInfluences},
				{ID: "w3", SourceFragmentID: domain.ID, TargetFragmentID: rootWeak.ID, Type: horizonmodels.DependencyTypeFocusesOn},
				{ID: "w4", SourceFragmentID: event.ID, TargetFragmentID: rootWeak.ID, Type: horizonmodels.DependencyTypeAbout},
			},
		},
	}

	strongScore := ScoreSnapshot(strongSnap, DefaultConfig())
	weakScore := ScoreSnapshot(weakSnap, DefaultConfig())

	if strongScore.StrategicImportance <= weakScore.StrategicImportance {
		t.Fatalf("expected strong typed graph to outrank weak contextual degree: strong=%.3f weak=%.3f", strongScore.StrategicImportance, weakScore.StrategicImportance)
	}
	if strongScore.BusinessBlastRadius <= weakScore.BusinessBlastRadius {
		t.Fatalf("expected strong business graph to have larger business blast: strong=%.3f weak=%.3f", strongScore.BusinessBlastRadius, weakScore.BusinessBlastRadius)
	}
}

func TestScoreSnapshot_ChokepointZoneBoostsConflictBlastRadius(t *testing.T) {
	zone := &horizonmodels.HorizonFragment{
		ID:            "zone",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		CanonicalPath: "/zone/sea/political_tension/strait-of-hormuz",
		Labels:        map[string]string{"en": "Strait of Hormuz Security Zone"},
		Tags:          []string{"chokepoint"},
	}
	product := &horizonmodels.HorizonFragment{ID: "prod", Type: horizonmodels.FragmentTypeProduct, CanonicalPath: "/product/raw/hydrocarbon/extraction/crude-oil"}
	fac := &horizonmodels.HorizonFragment{ID: "fac", Type: horizonmodels.FragmentTypeFacility, CanonicalPath: "/fac/na/export-terminal"}
	org := &horizonmodels.HorizonFragment{ID: "org", Type: horizonmodels.FragmentTypeOrganization, CanonicalPath: "/org/us/chevron-corp"}
	domain := &horizonmodels.HorizonFragment{ID: "domain", Type: horizonmodels.FragmentTypeDomain, CanonicalPath: "/domain/v1/geopolitical-risk/geo/ir-us"}

	snap := Snapshot{
		RootID: zone.ID,
		Nodes: map[string]*horizonmodels.HorizonFragment{
			zone.ID:    zone,
			product.ID: product,
			fac.ID:     fac,
			org.ID:     org,
			domain.ID:  domain,
		},
		Out: map[string][]*horizonmodels.DependencyEdge{
			zone.ID: {
				{
					ID:               "z1",
					SourceFragmentID: zone.ID,
					TargetFragmentID: product.ID,
					Type:             horizonmodels.DependencyTypeAffects,
					Properties:       horizonmodels.EdgeProperties{Criticality: 0.95},
				},
			},
			product.ID: {
				{
					ID:               "z2",
					SourceFragmentID: product.ID,
					TargetFragmentID: fac.ID,
					Type:             horizonmodels.DependencyTypeSuppliesTo,
					Properties:       horizonmodels.EdgeProperties{Criticality: 0.85},
				},
			},
			fac.ID: {
				{
					ID:               "z3",
					SourceFragmentID: fac.ID,
					TargetFragmentID: org.ID,
					Type:             horizonmodels.DependencyTypeDependsOn,
					Properties:       horizonmodels.EdgeProperties{Criticality: 0.80},
					Qualifiers: map[string]interface{}{
						"facet":            string(horizonmodels.FacetLogistics),
						"throughput_share": 0.85,
						"substitutability": 0.10,
					},
				},
			},
		},
		In: map[string][]*horizonmodels.DependencyEdge{
			zone.ID: {
				{
					ID:               "z4",
					SourceFragmentID: domain.ID,
					TargetFragmentID: zone.ID,
					Type:             horizonmodels.DependencyTypeFocusesOn,
				},
			},
		},
	}

	score := ScoreSnapshot(snap, DefaultConfig())
	if score.ConflictBlastRadius < 0.55 {
		t.Fatalf("expected chokepoint zone conflict blast to be materially high, got %.3f", score.ConflictBlastRadius)
	}
	if score.BridgeImportance < 0.35 {
		t.Fatalf("expected chokepoint zone bridge score to be elevated, got %.3f", score.BridgeImportance)
	}
}

func TestExplainSnapshot_ExposesTopConflictPath(t *testing.T) {
	zone := &horizonmodels.HorizonFragment{
		ID:            "zone",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		CanonicalPath: "/zone/sea/political_tension/strait-of-hormuz",
		Tags:          []string{"chokepoint"},
	}
	product := &horizonmodels.HorizonFragment{ID: "prod", Type: horizonmodels.FragmentTypeProduct, CanonicalPath: "/product/raw/hydrocarbon/extraction/crude-oil"}
	fac := &horizonmodels.HorizonFragment{ID: "fac", Type: horizonmodels.FragmentTypeFacility, CanonicalPath: "/fac/na/export-terminal"}
	org := &horizonmodels.HorizonFragment{ID: "org", Type: horizonmodels.FragmentTypeOrganization, CanonicalPath: "/org/us/chevron-corp"}

	snap := Snapshot{
		RootID: zone.ID,
		Nodes: map[string]*horizonmodels.HorizonFragment{
			zone.ID:    zone,
			product.ID: product,
			fac.ID:     fac,
			org.ID:     org,
		},
		Out: map[string][]*horizonmodels.DependencyEdge{
			zone.ID: {
				{ID: "z1", SourceFragmentID: zone.ID, TargetFragmentID: product.ID, Type: horizonmodels.DependencyTypeAffects, Properties: horizonmodels.EdgeProperties{Criticality: 0.95}},
			},
			product.ID: {
				{ID: "z2", SourceFragmentID: product.ID, TargetFragmentID: fac.ID, Type: horizonmodels.DependencyTypeSuppliesTo, Properties: horizonmodels.EdgeProperties{Criticality: 0.85}},
			},
			fac.ID: {
				{ID: "z3", SourceFragmentID: fac.ID, TargetFragmentID: org.ID, Type: horizonmodels.DependencyTypeDependsOn, Properties: horizonmodels.EdgeProperties{Criticality: 0.80}},
			},
		},
		In: map[string][]*horizonmodels.DependencyEdge{},
	}

	explain := ExplainSnapshot(snap, DefaultConfig(), ExplainConfig{MaxDirectContributors: 3, MaxAxisPaths: 2})
	if len(explain.ConflictComponent.TopPaths) == 0 {
		t.Fatalf("expected conflict explanation to expose at least one path")
	}
	top := explain.ConflictComponent.TopPaths[0]
	if top.TailPath != fac.CanonicalPath && top.TailPath != product.CanonicalPath {
		t.Fatalf("expected top conflict path to end in business tail, got %s", top.TailPath)
	}
	if len(explain.StructuralContributors) == 0 || explain.StructuralContributors[0].NeighborPath != product.CanonicalPath {
		t.Fatalf("expected direct structural contributors to include product edge first")
	}
}

func TestExplainSnapshot_ReportsZeroReasonWhenBusinessTailsMissing(t *testing.T) {
	zone := &horizonmodels.HorizonFragment{
		ID:            "zone",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		CanonicalPath: "/zone/global/maritime_chokepoint/suez-canal",
	}
	contextA := &horizonmodels.HorizonFragment{ID: "domain", Type: horizonmodels.FragmentTypeDomain, CanonicalPath: "/domain/v1/maritime-risk/geo/eg"}
	contextB := &horizonmodels.HorizonFragment{ID: "event", Type: horizonmodels.FragmentTypeEvent, CanonicalPath: "/event/traffic-delay"}

	snap := Snapshot{
		RootID: zone.ID,
		Nodes: map[string]*horizonmodels.HorizonFragment{
			zone.ID:     zone,
			contextA.ID: contextA,
			contextB.ID: contextB,
		},
		Out: map[string][]*horizonmodels.DependencyEdge{
			zone.ID: {
				{ID: "a1", SourceFragmentID: zone.ID, TargetFragmentID: contextB.ID, Type: horizonmodels.DependencyTypeTriggeredBy},
			},
		},
		In: map[string][]*horizonmodels.DependencyEdge{
			zone.ID: {
				{ID: "a2", SourceFragmentID: contextA.ID, TargetFragmentID: zone.ID, Type: horizonmodels.DependencyTypeFocusesOn},
			},
		},
	}

	explain := ExplainSnapshot(snap, DefaultConfig(), ExplainConfig{MaxDirectContributors: 3, MaxAxisPaths: 2})
	if explain.ConflictComponent.Score != 0 {
		t.Fatalf("expected zero conflict score, got %.3f", explain.ConflictComponent.Score)
	}
	if explain.ConflictComponent.ZeroReason == "" {
		t.Fatal("expected zero reason for empty conflict tail reach")
	}
}
