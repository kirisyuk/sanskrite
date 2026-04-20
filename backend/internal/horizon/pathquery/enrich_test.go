package pathquery

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEnrichArtifactsInPlace_stress(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, Mode: "sea", SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	a := &horizonmodels.MaterializedArtifact{Kind: horizonmodels.MaterializedKindSupplyChainPath, Payload: payload}
	enrichArtifactsInPlace([]*horizonmodels.MaterializedArtifact{a}, true, false)
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(a.Payload)
	if err != nil || parsed == nil || parsed.Stress == nil {
		t.Fatal("expected stress after enrich")
	}
}

func TestEnrichArtifactsInPlace_noop(t *testing.T) {
	a := &horizonmodels.MaterializedArtifact{Kind: horizonmodels.MaterializedKindSupplyChainPath, Payload: map[string]interface{}{}}
	enrichArtifactsInPlace([]*horizonmodels.MaterializedArtifact{a}, false, false)
}
