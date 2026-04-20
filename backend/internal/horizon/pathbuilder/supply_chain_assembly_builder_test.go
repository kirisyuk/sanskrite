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
	helium := "/product/finished/mechanical/assembly/helium"
	scanner := "/product/finished/mechanical/assembly/mri-scanner"
	checkTransforms := func(nodes []models.SupplyChainAssemblyNode) {
		for _, an := range nodes {
			for _, st := range an.SiteTransforms {
				for _, in := range st.InputProducts {
					for _, out := range st.OutputProducts {
						if in == scanner && out == helium {
							t.Fatalf("found reversed siteTransform in payload: %+v", st)
						}
						if in == helium && out == scanner {
							found = true
						}
					}
				}
			}
		}
	}
	checkTransforms(parsed.ActorNodes)
	checkTransforms(parsed.ProductNodes)
	if !found {
		t.Fatalf("expected helium -> mri-scanner siteTransform in assembly payload")
	}
}
