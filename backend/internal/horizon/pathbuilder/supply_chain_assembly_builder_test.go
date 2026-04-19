package pathbuilder

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

func TestBuildAssemblyPayloadFromInputs_UsesProductRelationDictionaryBeforeHeuristic(t *testing.T) {
	dict := productdict.NewRuntime(nil)
	if err := dict.SyncTransformPairWrite(
		context.Background(),
		"global",
		"/product/finished/mechanical/assembly/helium",
		"/product/finished/mechanical/assembly/mri-scanner",
		"usage",
		true,
	); err != nil {
		t.Fatalf("SyncTransformPairWrite() error = %v", err)
	}

	opts := DefaultSupplyChainAssemblyOptions()
	opts.TenantID = "global"
	opts.RawNodeRefs = []string{"/product/finished/mechanical/assembly/mri-scanner", "/product/finished/mechanical/assembly/helium"}
	opts.RawEdges = []SupplyChainAssemblyRawEdge{
		{
			Source:   "/product/finished/mechanical/assembly/mri-scanner",
			Target:   "/product/finished/mechanical/assembly/helium",
			EdgeType: "USED_IN",
		},
	}
	payload, _, _, _, _ := buildAssemblyPayloadFromInputs(
		[]string{"/product/finished/mechanical/assembly/mri-scanner"},
		nil,
		opts,
		dict,
	)
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}

	found := false
	for _, edge := range append(parsed.TransformEdges, parsed.AssemblyEdges...) {
		if edge.FromProductRef == "/product/finished/mechanical/assembly/mri-scanner" &&
			edge.ToProductRef == "/product/finished/mechanical/assembly/helium" {
			t.Fatalf("found reversed edge in payload: %+v", edge)
		}
		if edge.FromProductRef == "/product/finished/mechanical/assembly/helium" &&
			edge.ToProductRef == "/product/finished/mechanical/assembly/mri-scanner" {
			found = true
			if edge.Relation != string(DependencyTypeUsedIn) {
				t.Fatalf("relation=%q, want USED_IN", edge.Relation)
			}
		}
	}
	if !found {
		t.Fatalf("expected helium -> mri-scanner edge in assembly payload")
	}
}
