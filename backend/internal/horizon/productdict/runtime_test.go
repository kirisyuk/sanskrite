package productdict

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeRelationKind(t *testing.T) {
	if got := normalizeRelationKind("assembly"); got != "assembly" {
		t.Fatalf("normalizeRelationKind(assembly) = %q", got)
	}
	if got := normalizeRelationKind("usage"); got != "usage" {
		t.Fatalf("normalizeRelationKind(usage) = %q", got)
	}
	if got := normalizeRelationKind(""); got != "transformation" {
		t.Fatalf("normalizeRelationKind(empty) = %q", got)
	}
}

func TestDesiredEdgeTypeForKind(t *testing.T) {
	if got := desiredEdgeTypeForKind("assembly"); got != models.DependencyTypeAssemblesInto {
		t.Fatalf("desiredEdgeTypeForKind(assembly) = %q", got)
	}
	if got := desiredEdgeTypeForKind("usage"); got != models.DependencyTypeUsedIn {
		t.Fatalf("desiredEdgeTypeForKind(usage) = %q", got)
	}
	if got := desiredEdgeTypeForKind("transformation"); got != models.DependencyTypeTransformsInto {
		t.Fatalf("desiredEdgeTypeForKind(transformation) = %q", got)
	}
}

func TestSupportsProductRelationDictionary(t *testing.T) {
	for _, edgeType := range []models.DependencyType{
		models.DependencyTypeDependsOn,
		models.DependencyTypeTransformsInto,
		models.DependencyTypeAssemblesInto,
		models.DependencyTypeUsedIn,
	} {
		if !supportsProductRelationDictionary(edgeType) {
			t.Fatalf("expected edgeType=%q to be supported", edgeType)
		}
	}
	if supportsProductRelationDictionary(models.DependencyTypeProduces) {
		t.Fatalf("did not expect PRODUCES to be supported")
	}
}

func TestNormalizeAliasKey(t *testing.T) {
	if got := normalizeAliasKey("Battery-Packs (EV)"); got != "battery packs ev" {
		t.Fatalf("normalizeAliasKey returned %q", got)
	}
}

func TestStableDocIDDeterministic(t *testing.T) {
	a := stableDocID("x", "a", "b")
	b := stableDocID("x", "a", "b")
	c := stableDocID("x", "b", "a")
	if a != b {
		t.Fatalf("expected deterministic stableDocID")
	}
	if a == c {
		t.Fatalf("expected input order to matter")
	}
}

func TestNormalizeStreamDirection(t *testing.T) {
	if got, err := normalizeStreamDirection("downstream"); err != nil || got != StreamDirectionDownstream {
		t.Fatalf("normalizeStreamDirection(downstream) = %q, %v", got, err)
	}
	if got, err := normalizeStreamDirection("upstream"); err != nil || got != StreamDirectionUpstream {
		t.Fatalf("normalizeStreamDirection(upstream) = %q, %v", got, err)
	}
	if _, err := normalizeStreamDirection("sideways"); err == nil {
		t.Fatal("expected invalid direction to fail")
	}
}

func TestExpandProductStreamDownstream(t *testing.T) {
	rt := &Runtime{
		loaded:         true,
		synonymToCanon: map[string]map[string]string{},
		knownPaths: map[string]map[string]struct{}{
			"global": {
				"/product/raw/bauxite":        {},
				"/product/refined/alumina":    {},
				"/product/refined/aluminium":  {},
				"/product/component/car-body": {},
			},
		},
		transformKinds: map[string]map[string]string{
			"global": {
				transformKey("/product/raw/bauxite", "/product/refined/alumina"):          "transformation",
				transformKey("/product/refined/alumina", "/product/refined/aluminium"):    "transformation",
				transformKey("/product/refined/aluminium", "/product/component/car-body"): "assembly",
			},
		},
	}

	out, err := rt.ExpandProductStream(context.Background(), "/product/raw/bauxite", "downstream", 2, 10)
	if err != nil {
		t.Fatalf("ExpandProductStream returned error: %v", err)
	}
	if len(out.Nodes) != 3 {
		t.Fatalf("expected 3 nodes within 2 hops, got %d", len(out.Nodes))
	}
	if len(out.Edges) != 2 {
		t.Fatalf("expected 2 edges within 2 hops, got %d", len(out.Edges))
	}
	if out.Nodes[0].CanonicalPath != "/product/raw/bauxite" || out.Nodes[0].Hop != 0 {
		t.Fatalf("unexpected seed node: %+v", out.Nodes[0])
	}
	if out.Edges[1].RelationKind != "transformation" {
		t.Fatalf("expected second edge to remain transformation, got %+v", out.Edges[1])
	}
}

func TestExpandProductStreamUpstream(t *testing.T) {
	rt := &Runtime{
		loaded:         true,
		synonymToCanon: map[string]map[string]string{},
		knownPaths: map[string]map[string]struct{}{
			"global": {
				"/product/refined/alumina":    {},
				"/product/refined/aluminium":  {},
				"/product/component/car-body": {},
			},
		},
		transformKinds: map[string]map[string]string{
			"global": {
				transformKey("/product/refined/alumina", "/product/refined/aluminium"):    "transformation",
				transformKey("/product/refined/aluminium", "/product/component/car-body"): "assembly",
			},
		},
	}

	out, err := rt.ExpandProductStream(context.Background(), "/product/component/car-body", "upstream", 2, 10)
	if err != nil {
		t.Fatalf("ExpandProductStream returned error: %v", err)
	}
	if len(out.Nodes) != 3 {
		t.Fatalf("expected 3 upstream nodes, got %d", len(out.Nodes))
	}
	if out.Edges[0].RelationKind != "transformation" && out.Edges[1].RelationKind != "assembly" {
		t.Fatalf("expected upstream edges to preserve relation kinds, got %+v", out.Edges)
	}
}

func TestSyncTransformPairWriteUpdatesSnapshot(t *testing.T) {
	rt := &Runtime{
		loaded:         true,
		synonymToCanon: map[string]map[string]string{},
		knownPaths:     map[string]map[string]struct{}{},
		transformKinds: map[string]map[string]string{},
	}

	if err := rt.SyncTransformPairWrite(context.Background(), "global", "/product/raw/bauxite", "/product/refined/alumina", "transformation", true); err != nil {
		t.Fatalf("SyncTransformPairWrite returned error: %v", err)
	}

	if kind, ok := rt.lookupTransformKind("global", "/product/raw/bauxite", "/product/refined/alumina"); !ok || kind != "transformation" {
		t.Fatalf("expected transform kind to be stored, got %q, ok=%t", kind, ok)
	}

	down := rt.listTransformAdjacencyFromSnapshot("global", "/product/raw/bauxite", StreamDirectionDownstream)
	if len(down) != 1 || down[0].ToPath != "/product/refined/alumina" {
		t.Fatalf("unexpected downstream adjacency: %+v", down)
	}

	if err := rt.SyncTransformPairWrite(context.Background(), "global", "/product/raw/bauxite", "/product/refined/alumina", "transformation", false); err != nil {
		t.Fatalf("SyncTransformPairWrite deactivate returned error: %v", err)
	}
	if _, ok := rt.lookupTransformKind("global", "/product/raw/bauxite", "/product/refined/alumina"); ok {
		t.Fatal("expected transform kind to be removed after deactivate")
	}
}

func TestResolveProductRelationForTenantExactAndReverse(t *testing.T) {
	rt := &Runtime{
		loaded:         true,
		synonymToCanon: map[string]map[string]string{},
		knownPaths:     map[string]map[string]struct{}{},
		transformKinds: map[string]map[string]string{
			"global": {
				transformKey("/product/raw/mineral/extraction/bauxite", "/product/finished/chemical/processing/alumina"): "transformation",
			},
		},
	}

	exact, err := rt.ResolveProductRelation(context.Background(), "/product/raw/mineral/extraction/bauxite", "/product/finished/chemical/processing/alumina")
	if err != nil {
		t.Fatalf("ResolveProductRelation exact returned error: %v", err)
	}
	if !exact.Known || exact.Swapped {
		t.Fatalf("expected exact resolution without swap, got %+v", exact)
	}
	if exact.EdgeType != models.DependencyTypeTransformsInto {
		t.Fatalf("expected TRANSFORMS_INTO, got %+v", exact)
	}

	reverse, err := rt.ResolveProductRelation(context.Background(), "/product/finished/chemical/processing/alumina", "/product/raw/mineral/extraction/bauxite")
	if err != nil {
		t.Fatalf("ResolveProductRelation reverse returned error: %v", err)
	}
	if !reverse.Known || !reverse.Swapped {
		t.Fatalf("expected reverse resolution with swap, got %+v", reverse)
	}
	if reverse.FromPath != "/product/raw/mineral/extraction/bauxite" || reverse.ToPath != "/product/finished/chemical/processing/alumina" {
		t.Fatalf("expected normalized upstream/downstream orientation, got %+v", reverse)
	}
}

func TestSyncSynonymPairWriteRecanonicalizesTransforms(t *testing.T) {
	rt := &Runtime{
		loaded:         true,
		synonymToCanon: map[string]map[string]string{},
		knownPaths: map[string]map[string]struct{}{
			"global": {
				"/product/refined/aluminium":  {},
				"/product/component/car-body": {},
			},
		},
		transformKinds: map[string]map[string]string{
			"global": {
				transformKey("/product/refined/aluminium", "/product/component/car-body"): "assembly",
			},
		},
	}

	if err := rt.SyncSynonymPairWrite(context.Background(), "global", "/product/refined/aluminum", "/product/refined/aluminium", true); err != nil {
		t.Fatalf("SyncSynonymPairWrite returned error: %v", err)
	}

	if canonical, ok := rt.lookupCanonicalPath("global", "/product/refined/aluminium"); !ok || canonical != "/product/refined/aluminum" {
		t.Fatalf("expected synonym to canonicalize, got %q, ok=%t", canonical, ok)
	}
	if _, ok := rt.lookupTransformKind("global", "/product/refined/aluminium", "/product/component/car-body"); ok {
		t.Fatal("expected old transform key to be recanonicalized away")
	}
	if kind, ok := rt.lookupTransformKind("global", "/product/refined/aluminum", "/product/component/car-body"); !ok || kind != "assembly" {
		t.Fatalf("expected recanonicalized assembly edge, got %q, ok=%t", kind, ok)
	}
}

func TestSearchCanonicalProductPathsForTenant(t *testing.T) {
	rt := &Runtime{
		loaded:         true,
		synonymToCanon: map[string]map[string]string{},
		knownPaths:     map[string]map[string]struct{}{},
		transformKinds: map[string]map[string]string{
			"global": {
				transformKey("/product/raw/mineral/extraction/lithium-ore", "/product/finished/chemical/refining/lithium-carbonate"):   "transformation",
				transformKey("/product/raw/mineral/extraction/lithium-brine", "/product/finished/chemical/refining/lithium-carbonate"): "transformation",
			},
		},
	}

	out, err := rt.SearchCanonicalProductPathsForTenant(context.Background(), "global", "lithium", 8)
	if err != nil {
		t.Fatalf("SearchCanonicalProductPathsForTenant returned error: %v", err)
	}
	if len(out) < 2 {
		t.Fatalf("expected at least 2 pair-backed lithium products, got %v", out)
	}
	if out[0] != "/product/finished/chemical/refining/lithium-carbonate" && out[0] != "/product/raw/mineral/extraction/lithium-brine" && out[0] != "/product/raw/mineral/extraction/lithium-ore" {
		t.Fatalf("unexpected first result: %v", out)
	}
}
