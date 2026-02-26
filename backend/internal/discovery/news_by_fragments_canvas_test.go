package discovery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPrepareInputFragments_DedupAndNormalize(t *testing.T) {
	canvas := &NewsByFragmentsCanvas{}
	in := []FragmentInput{
		{CanonicalPath: " /org/acme/ ", CanonicalName: ""},
		{CanonicalPath: "/org/acme", CanonicalName: "Acme Corp"},
		{CanonicalPath: "invalid", CanonicalName: "X"},
		{CanonicalPath: "/market/ai", CanonicalName: ""},
	}

	got, err := canvas.prepareInputFragments(context.Background(), in)
	if err != nil {
		t.Fatalf("prepareInputFragments returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique valid fragments, got %d", len(got))
	}
	if got[0].CanonicalPath != "/org/acme" {
		t.Fatalf("expected normalized path /org/acme, got %q", got[0].CanonicalPath)
	}
	if got[0].CanonicalName != "Acme Corp" {
		t.Fatalf("expected canonical name to be preserved from duplicate, got %q", got[0].CanonicalName)
	}
	if got[1].CanonicalName != "/market/ai" {
		t.Fatalf("expected fallback canonical name to path, got %q", got[1].CanonicalName)
	}
}

func TestPrepareInputFragments_MaxLimit(t *testing.T) {
	canvas := &NewsByFragmentsCanvas{}
	in := make([]FragmentInput, 0, maxFragmentsPerNewsRequest+1)
	for i := 0; i < maxFragmentsPerNewsRequest+1; i++ {
		in = append(in, FragmentInput{CanonicalPath: fmt.Sprintf("/org/test-%d", i)})
	}
	_, err := canvas.prepareInputFragments(context.Background(), in)
	if err == nil {
		t.Fatalf("expected limit error, got nil")
	}
}

func TestComposeNewsDedupKeys_PrefersURLAndTitleDate(t *testing.T) {
	occurred := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	keys := composeNewsDedupKeys("  Big Event  ", "HTTPS://EXAMPLE.COM/a?x=1#frag", &occurred)
	if len(keys) < 3 {
		t.Fatalf("expected at least 3 dedup keys, got %d", len(keys))
	}
	foundURL := false
	foundTitle := false
	foundTitleDate := false
	for _, k := range keys {
		if k == "u:https://example.com/a" {
			foundURL = true
		}
		if k == "t:big event" {
			foundTitle = true
		}
		if k == "td:big event|2026-02-01" {
			foundTitleDate = true
		}
	}
	if !foundURL || !foundTitle || !foundTitleDate {
		t.Fatalf("unexpected dedup keys: %#v", keys)
	}
}

func TestIsOccurredAtInWindow(t *testing.T) {
	ref := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC)
	old := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	future := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	if !isOccurredAtInWindow(nil, ref, 7) {
		t.Fatalf("nil occurredAt should be allowed")
	}
	if !isOccurredAtInWindow(&inWindow, ref, 7) {
		t.Fatalf("expected in-window date to be allowed")
	}
	if isOccurredAtInWindow(&old, ref, 7) {
		t.Fatalf("expected too-old date to be filtered")
	}
	if isOccurredAtInWindow(&future, ref, 7) {
		t.Fatalf("expected future date to be filtered")
	}
}

func TestNormalizeNewsSourceL1Category(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "geopolitics", want: "geopolitics"},
		{in: "Geopolitical", want: "geopolitics"},
		{in: "/source/logistics/shipping", want: "logistics"},
		{in: "/source/finance/payments", want: "finance"},
		{in: "/sphere/policy/regulation", want: "policy"},
		{in: "policy/regulatory", want: "policy"},
		{in: "supply-chain", want: "logistics"},
		{in: "source/cyber/threat-intel", want: ""},
		{in: "unknown", want: ""},
	}
	for _, tt := range tests {
		got := normalizeNewsSourceL1Category(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeNewsSourceL1Category(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestInferNewsSourceL1FromSpherePaths(t *testing.T) {
	got := inferNewsSourceL1FromSpherePaths([]string{"/sphere/environment", "/sphere/geopolitics"})
	if got != "geopolitics" {
		t.Fatalf("inferNewsSourceL1FromSpherePaths fallback mismatch, got %q", got)
	}
}

func TestBuildGeminiPrompt_IncludesRiskL1Controls(t *testing.T) {
	canvas := &NewsByFragmentsCanvas{}
	prompt := canvas.buildGeminiPrompt(
		[]FragmentInput{{CanonicalPath: "/org/acme", CanonicalName: "Acme"}},
		7,
		"2026-02-26",
		nil,
		nil,
	)

	if !strings.Contains(prompt, "SINGLE search pass across these L1 risk categories") {
		t.Fatalf("prompt must enforce single-pass multi-category search, got: %s", prompt)
	}
	if !strings.Contains(prompt, "\"sourceL1Category\"") {
		t.Fatalf("prompt must include sourceL1Category in output schema")
	}
	if !strings.Contains(prompt, "no more than 1 item per sourceL1Category per fragment") {
		t.Fatalf("prompt must enforce per-category cap")
	}
	for _, category := range newsRiskL1Categories {
		if !strings.Contains(prompt, category) {
			t.Fatalf("prompt missing risk category %q", category)
		}
	}
}
