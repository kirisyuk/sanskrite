package ai

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassifyProviderPressureError(t *testing.T) {
	cases := []struct {
		err      error
		wantKind string
		wantOK   bool
	}{
		{err: fmt.Errorf("rate limit 429"), wantKind: "rate_limit", wantOK: true},
		{err: fmt.Errorf("empty response from OpenAI Chat API"), wantKind: "empty_response", wantOK: true},
		{err: fmt.Errorf("validation failed"), wantKind: "", wantOK: false},
	}
	for _, tc := range cases {
		gotKind, gotOK := classifyProviderPressureError(tc.err)
		if gotOK != tc.wantOK || gotKind != tc.wantKind {
			t.Fatalf("classify(%v) = (%q,%v), want (%q,%v)", tc.err, gotKind, gotOK, tc.wantKind, tc.wantOK)
		}
	}
}

func TestApplyProviderPressureExclusions_IncludesBlockedProviderModels(t *testing.T) {
	mm := NewAIModelManager()
	exec := NewAITaskExecutor(nil, mm)
	exec.providerPressure[AIProviderOpenAI] = providerPressureState{
		ConsecutiveFailures: 2,
		BlockedUntil:        time.Now().UTC().Add(30 * time.Second),
		LastErrorClass:      "rate_limit",
	}
	excluded, added := exec.applyProviderPressureExclusions(nil)
	if added <= 0 {
		t.Fatalf("expected blocked models to be added to exclusion list")
	}
	haveOpenAI := false
	for _, m := range excluded {
		if strings.HasPrefix(m, "gpt-") {
			haveOpenAI = true
			break
		}
	}
	if !haveOpenAI {
		t.Fatalf("expected openai models in exclusion list when provider is blocked")
	}
}
