package scout

import (
	"reflect"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestInferTransportViaRefsFromEvidence_OrdersKnownMaritimeHints(t *testing.T) {
	got := inferTransportViaRefsFromEvidence(nil, "Breakfast Cereal Sea via Jebel Ali Port, Suez Canal, and Sohar Port into Oman.")
	want := []string{
		"/fac/ae/jebel-ali-port",
		"/fac/eg/suez-terminal",
		"/fac/om/sohar-port",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("via=%v, want %v", got, want)
	}
}

func TestInferTransportViaRefsFromEvidence_DoesNotPromoteGenericGeoWithoutRouteContext(t *testing.T) {
	got := inferTransportViaRefsFromEvidence(nil, "Unilever Dubai Personal Care Factory supplies Muscat retail customers.")
	if len(got) != 0 {
		t.Fatalf("via=%v, want empty without route context", got)
	}
}

func TestCollectSegmentRouteEvidence_UsesMatchingRouteObservations(t *testing.T) {
	chain := agents.SupplyChain{
		Reason: "Regional distribution into Oman.",
		RouteObservations: []agents.RouteObservation{
			{
				SupplierRef: "/fac/ae/unilever-dubai-personal-care-factory",
				ProductRef:  "/product/consumer-goods/personal-care",
				CustomerRef: "/fac/om/khimji-ramdas-muscat-showroom",
				Qualifiers: map[string]interface{}{
					"caption": "Personal Care Products Sea via Jebel Ali Port, Sohar Port, and Suez Canal",
				},
			},
			{
				SupplierRef: "/fac/ae/other-factory",
				ProductRef:  "/product/consumer-goods/personal-care",
				CustomerRef: "/fac/om/khimji-ramdas-muscat-showroom",
				Qualifiers: map[string]interface{}{
					"caption": "Sea via Port of Rotterdam",
				},
			},
		},
	}

	explicitRefs, texts := collectSegmentRouteEvidence(
		chain,
		"/fac/ae/unilever-dubai-personal-care-factory",
		"/product/consumer-goods/personal-care",
		"/fac/om/khimji-ramdas-muscat-showroom",
	)
	got := inferTransportViaRefsFromEvidence(explicitRefs, texts...)
	want := []string{
		"/fac/ae/jebel-ali-port",
		"/fac/om/sohar-port",
		"/fac/eg/suez-terminal",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("via=%v, want %v", got, want)
	}
}

func TestInferTransportViaRefsFromEvidence_CanonicalizesKnownWaypointAliases(t *testing.T) {
	got := inferTransportViaRefsFromEvidence(
		[]string{"/fac/ae/jebel-ali-terminal"},
		"Passenger vehicles sea via Strait of Malacca and Port of Sohar.",
	)
	want := []string{
		"/fac/ae/jebel-ali-port",
		"/fac/int/strait-of-malacca",
		"/fac/om/sohar-port",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("via=%v, want %v", got, want)
	}
}

func TestInferTransportViaRefsFromEvidence_ReordersWrongExplicitMaritimeSequenceUsingCaption(t *testing.T) {
	got := inferTransportViaRefsFromEvidence(
		[]string{
			"/fac/id/port-of-tanjung-priok",
			"/fac/it/port-of-genoa",
			"/fac/sg/port-of-singapore",
		},
		"Crocodile Skin Sea via Port of Singapore to Port of Genoa.",
	)
	want := []string{
		"/fac/id/port-of-tanjung-priok",
		"/fac/sg/port-of-singapore",
		"/fac/it/port-of-genoa",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("via=%v, want %v", got, want)
	}
}
