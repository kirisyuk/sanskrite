package main

import (
	"net/http"
	"testing"
)

func TestVerifyOIDCTokenRejectsMissingBearerByDefault(t *testing.T) {
	t.Setenv("AGENTMESH_ALLOW_UNAUTH_INTERNAL_TASKS", "")
	t.Setenv("AGENTMESH_DEV_ALLOW_UNAUTH_INTERNAL_TASKS", "")
	req, _ := http.NewRequest(http.MethodPost, "https://agentmesh.example.com/api/v1/internal/tasks/agentmesh/run", nil)
	if err := verifyOIDCToken(req); err == nil {
		t.Fatalf("expected missing bearer token to be rejected")
	}
}

func TestVerifyOIDCTokenAllowsUnauthOnlyWithDevGate(t *testing.T) {
	t.Setenv("AGENTMESH_ALLOW_UNAUTH_INTERNAL_TASKS", "1")
	t.Setenv("AGENTMESH_DEV_ALLOW_UNAUTH_INTERNAL_TASKS", "1")
	req, _ := http.NewRequest(http.MethodPost, "https://agentmesh.example.com/api/v1/internal/tasks/agentmesh/run", nil)
	if err := verifyOIDCToken(req); err != nil {
		t.Fatalf("expected dev unauth bypass: %v", err)
	}
}
