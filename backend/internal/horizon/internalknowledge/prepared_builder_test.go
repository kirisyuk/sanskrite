package internalknowledge

import (
	"strings"
	"testing"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildPreparedCorpusDocsIncludesLegProductRefsAndLabels(t *testing.T) {
	artifact := &hmodels.MaterializedArtifact{
		ID:             "artifact-1",
		CanonicalKey:   "/artifact/supply-chain/onions-kz",
		DisplayName:    "Zhetysai onions to Almaty",
		Kind:           hmodels.MaterializedKindSupplyChainPath,
		PathStatus:     "validated",
		NodeRefs:       []string{"/fac/kz/zhetysai-farm", "/fac/kz/almaty-dc"},
		GeoRefs:        []string{"/loc/kz"},
		AnchorSupplier: "/fac/kz/zhetysai-farm",
		AnchorCustomer: "/fac/kz/almaty-dc",
		ProductAxis:    "/product/kz/onions",
		Payload: map[string]any{
			"v": 1,
			"nodes": []map[string]any{
				{"type": "fac", "ref": "/fac/kz/zhetysai-farm"},
				{"type": "product", "ref": "/product/kz/onions"},
				{"type": "fac", "ref": "/fac/kz/almaty-dc"},
			},
			"legs": []map[string]any{
				{
					"index":                   0,
					"supplierRef":             "/fac/kz/zhetysai-farm",
					"customerRef":             "/fac/kz/almaty-dc",
					"productRef":              "/product/kz/onions",
					"supplyLinkCanonicalPath": "/supply-link/kz/zhetysai-onions-to-almaty",
					"transportViaRefs":        []string{"/fac/kz/saryagash-border"},
					"mode":                    "road",
				},
			},
		},
	}
	artifacts := map[string]*hmodels.MaterializedArtifact{artifact.ID: artifact}
	sub := &PreparedSubstrate{
		CorpusID:  "kz-onions",
		Lens:      &hmodels.Lens{ID: "lens-1", Name: "Kazakhstan onions"},
		Question:  "Какие маршруты поставок лука есть в Казахстане?",
		Artifacts: artifacts,
		Routes:    summarizeRoutes(artifacts),
		Products:  summarizeProducts(artifacts),
		Fragments: map[string]*hmodels.HorizonFragment{
			"/product/kz/onions":                        {CanonicalPath: "/product/kz/onions", Labels: map[string]string{"en": "Onions"}},
			"/fac/kz/zhetysai-farm":                     {CanonicalPath: "/fac/kz/zhetysai-farm", Labels: map[string]string{"en": "Zhetysai Farm"}},
			"/fac/kz/almaty-dc":                         {CanonicalPath: "/fac/kz/almaty-dc", Labels: map[string]string{"en": "Almaty Distribution Center"}},
			"/fac/kz/saryagash-border":                  {CanonicalPath: "/fac/kz/saryagash-border", Labels: map[string]string{"en": "Saryagash Border Crossing"}},
			"/supply-link/kz/zhetysai-onions-to-almaty": {CanonicalPath: "/supply-link/kz/zhetysai-onions-to-almaty", Labels: map[string]string{"en": "Zhetysai onions to Almaty"}},
		},
	}

	docs := buildPreparedCorpusDocs(sub)

	var productsDoc *PreparedCorpusDoc
	var corridorDoc *PreparedCorpusDoc
	var routeArtifactDoc *PreparedCorpusDoc
	for i := range docs {
		doc := &docs[i]
		switch doc.Kind {
		case "products":
			productsDoc = doc
		case "corridor":
			corridorDoc = doc
		case "route_artifact":
			routeArtifactDoc = doc
		}
	}

	if productsDoc == nil {
		t.Fatal("expected products doc")
	}
	if !strings.Contains(productsDoc.Content, "Onions (/product/kz/onions)") {
		t.Fatalf("expected product doc to include human-readable label, got %q", productsDoc.Content)
	}
	if corridorDoc == nil || !strings.Contains(corridorDoc.Content, "Onions (/product/kz/onions)") {
		t.Fatalf("expected corridor doc to include labeled product, got %+v", corridorDoc)
	}
	if routeArtifactDoc == nil {
		t.Fatal("expected route_artifact doc")
	}
	refs := compactUnique(anyStrings(routeArtifactDoc.StructData["canonicalPaths"]))
	if !containsString(refs, "/product/kz/onions") {
		t.Fatalf("expected route artifact refs to include leg product ref, got %+v", refs)
	}
	if !containsString(refs, "/supply-link/kz/zhetysai-onions-to-almaty") {
		t.Fatalf("expected route artifact refs to include supply link ref, got %+v", refs)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func anyStrings(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
