package metriccore

import (
	"testing"
	"time"

	gencombined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestParseGroundedHydrationEnvelopeAcceptsEnvelope(t *testing.T) {
	t.Parallel()
	raw := `{"results":[{"targetRef":"/loc/test","metrics":[{"key":"chokepointBlockageScore","value":0.8,"confidence":0.9}]}]}`
	got, err := parseGroundedHydrationEnvelope(raw)
	if err != nil {
		t.Fatalf("parseGroundedHydrationEnvelope returned error: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].TargetRef != "/loc/test" {
		t.Fatalf("unexpected results: %#v", got.Results)
	}
}

func TestParseGroundedHydrationEnvelopeAcceptsBareArray(t *testing.T) {
	t.Parallel()
	raw := `[{"targetRef":"/loc/test","metrics":[{"key":"chokepointBlockageScore","value":0.8,"confidence":0.9}]}]`
	got, err := parseGroundedHydrationEnvelope(raw)
	if err != nil {
		t.Fatalf("parseGroundedHydrationEnvelope returned error: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].TargetRef != "/loc/test" {
		t.Fatalf("unexpected results: %#v", got.Results)
	}
}

func TestParseGroundedHydrationEnvelopeAcceptsFencedArray(t *testing.T) {
	t.Parallel()
	raw := "```json\n[{\"targetRef\":\"/loc/test\",\"metrics\":[{\"key\":\"chokepointBlockageScore\",\"value\":0.8,\"confidence\":0.9}]}]\n```"
	got, err := parseGroundedHydrationEnvelope(raw)
	if err != nil {
		t.Fatalf("parseGroundedHydrationEnvelope returned error: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].TargetRef != "/loc/test" {
		t.Fatalf("unexpected results: %#v", got.Results)
	}
}

func TestParseGroundedHydrationEnvelopeAcceptsTargetsWrapper(t *testing.T) {
	t.Parallel()
	raw := `{"data":{"targets":[{"targetRef":"/loc/test","metrics":[{"key":"chokepointBlockageScore","value":0.8,"confidence":0.9}]}]}}`
	got, err := parseGroundedHydrationEnvelope(raw)
	if err != nil {
		t.Fatalf("parseGroundedHydrationEnvelope returned error: %v", err)
	}
	if len(got.Results) != 1 || got.Results[0].TargetRef != "/loc/test" {
		t.Fatalf("unexpected results: %#v", got.Results)
	}
}

func TestParseObservedAtDateOnlyUsesFallbackClock(t *testing.T) {
	t.Parallel()
	fallback := time.Date(2026, time.March, 26, 18, 37, 2, 0, time.UTC)
	got, ok := parseObservedAt("2026-03-26", fallback)
	if !ok {
		t.Fatalf("parseObservedAt returned ok=false")
	}
	want := time.Date(2026, time.March, 26, 18, 37, 2, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected parsed time: got %s want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestInferOperationalTargetContractRecognizesToyMarketAndCustoms(t *testing.T) {
	t.Parallel()
	market := &horizonmodels.HorizonFragment{
		CanonicalPath:  "/market/kz/consumer-goods/toys",
		Class:          FragmentTypeMarket,
		MarketDomain:   "consumer",
		MarketVertical: "toys",
		Labels:         map[string]string{"en": "Kazakhstan toy market"},
	}
	class, reqs := inferOperationalTargetContract(market)
	if class != "market_surveillance" {
		t.Fatalf("expected market_surveillance, got %q", class)
	}
	if len(reqs) < 4 {
		t.Fatalf("expected market requirements, got %#v", reqs)
	}

	customs := &horizonmodels.HorizonFragment{
		CanonicalPath: "/gov/kz/state-revenue-committee",
		Class:         FragmentTypeOrganization,
		GovType:       "committee",
		Labels:        map[string]string{"en": "State Revenue Committee"},
		Description:   "Kazakhstan customs and revenue authority",
	}
	class, reqs = inferOperationalTargetContract(customs)
	if class != "compliance_gate" {
		t.Fatalf("expected compliance_gate, got %q", class)
	}
	if len(reqs) < 3 {
		t.Fatalf("expected customs requirements, got %#v", reqs)
	}

	product := &horizonmodels.HorizonFragment{
		CanonicalPath: "/product/finished/mechanical/assembly/toys",
		Class:         FragmentTypeProduct,
		Labels:        map[string]string{"en": "Toys"},
	}
	class, reqs = inferOperationalTargetContract(product)
	if class != "product_safety" {
		t.Fatalf("expected product_safety for toy product, got %q", class)
	}
	if len(reqs) < 4 {
		t.Fatalf("expected product safety requirements, got %#v", reqs)
	}

	industrialProduct := &horizonmodels.HorizonFragment{
		CanonicalPath: "/product/component/mechanical/manufacturing/automotive-spare-parts",
		Class:         FragmentTypeProduct,
		Labels:        map[string]string{"en": "Automotive Spare Parts"},
	}
	class, reqs = inferOperationalTargetContract(industrialProduct)
	if class != "" || len(reqs) != 0 {
		t.Fatalf("expected industrial product to be ignored, got class=%q reqs=%#v", class, reqs)
	}
}

func TestPreferredGroundedSourcesUsesResponseGrounding(t *testing.T) {
	t.Parallel()
	resp := &gencombined.GenAIResponse{
		Sources: []string{
			"https://kgd.gov.kz/en/news/press-release-1-138435",
			"https://eec.eaeunion.org/en/news/vstupayut-v-silu-izmeneniya-v-tekhreglament-soyuza-na-igrushki/",
		},
	}
	got := preferredGroundedSources(groundedSourceURLs(resp), []string{"https://invented.example.com/foo"}, []string{"https://invented.example.com/bar"})
	if len(got) != 2 {
		t.Fatalf("expected 2 grounded sources, got %#v", got)
	}
	for _, raw := range got {
		if raw == "https://invented.example.com/foo" || raw == "https://invented.example.com/bar" {
			t.Fatalf("unexpected fallback URL in grounded sources: %#v", got)
		}
	}
}
