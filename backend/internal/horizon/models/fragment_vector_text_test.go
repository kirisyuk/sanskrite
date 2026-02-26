package models

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateVectorText_IncludesIdentityAndNeighborhood(t *testing.T) {
	t.Parallel()

	frag := &HorizonFragment{
		Type:          FragmentTypeOrganization,
		CanonicalPath: "/org/sa/acme-inc",
		Labels: map[string]string{
			"en":  "ACME",
			"alt": "Acme Alt",
		},
		Identifiers: map[string]string{
			"legalName": "Acme Incorporated",
			"shortName": "Acme",
		},
		ParentPath:        "/org/sa/acme-group",
		AffiliatedOrgPath: "/org/sa/industry-association",
		SpherePaths:       []string{"/sphere/economy/industry"},
	}

	text := frag.GenerateVectorText()

	if !strings.Contains(text, "Identity envelope:") {
		t.Fatalf("expected identity envelope in vector text, got:\n%s", text)
	}
	if !strings.Contains(text, "jurisdiction=sa") {
		t.Fatalf("expected jurisdiction in identity envelope, got:\n%s", text)
	}
	if !strings.Contains(text, "Neighborhood context:") {
		t.Fatalf("expected neighborhood context in vector text, got:\n%s", text)
	}
	if !strings.Contains(text, "/org/sa/acme-group") {
		t.Fatalf("expected parent path in neighborhood context, got:\n%s", text)
	}
}

func TestGenerateVectorText_LimitsMetricLines(t *testing.T) {
	t.Parallel()

	state := make(map[string]MetricValue, 45)
	for i := 0; i < 45; i++ {
		key := fmt.Sprintf("metric_%02d", i)
		state[key] = MetricValue{Value: float64(i), Confidence: 0.8}
	}
	frag := &HorizonFragment{
		Type:          FragmentTypeOrganization,
		CanonicalPath: "/org/us/test-inc",
		Labels:        map[string]string{"en": "Test"},
		State:         state,
	}

	text := frag.GenerateVectorText()
	if !strings.Contains(text, "Metrics omitted: 5") {
		t.Fatalf("expected metric truncation marker, got:\n%s", text)
	}
}
