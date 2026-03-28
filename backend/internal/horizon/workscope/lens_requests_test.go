package workscope

import "testing"

func TestNewLensGraphEnrichmentRequest_scopeRefsOnlyPolicy(t *testing.T) {
	scope := FromLensStrictWorkScope("tenant", "project", "lens-1", []string{"/org/a", "/org/b"})
	request := NewLensGraphEnrichmentRequest(scope, MutationModePersistWorldScopeRefsOnly)
	if !RequestAllowsScopeAutoAdd(request, "/org/a") {
		t.Fatalf("expected /org/a to be auto-add allowed")
	}
	if RequestAllowsScopeAutoAdd(request, "/org/c") {
		t.Fatalf("expected /org/c to stay outside lens scope")
	}
}

func TestApplyRequestToState_persistsScopeMutationPolicy(t *testing.T) {
	scope := FromLensStrictWorkScope("tenant", "project", "lens-1", []string{"/org/a"})
	request := NewLensGraphEnrichmentRequest(scope, MutationModePersistWorldKeepScope)
	state := ApplyRequestToState(nil, request)
	if StateAllowsScopeAutoAdd(state, "/org/a") {
		t.Fatalf("keep-scope policy should block auto-add")
	}
	if got := state[StateKeyScopeMutationMode]; got != MutationModePersistWorldKeepScope {
		t.Fatalf("scope mutation mode = %v", got)
	}
	if _, ok := state[StateKeyWorkRequest]; !ok {
		t.Fatalf("work request state missing")
	}
}

func TestStateAllowsScopeAutoAdd_legacyStateRemainsPermissive(t *testing.T) {
	if !StateAllowsScopeAutoAdd(map[string]interface{}{}, "/org/a") {
		t.Fatalf("legacy empty state should remain permissive")
	}
}
