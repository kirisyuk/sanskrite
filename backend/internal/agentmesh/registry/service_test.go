package registry

import (
	"context"
	"testing"
)

func TestSignAndValidateAgentCard(t *testing.T) {
	svc := NewService(NewHMACSigner("secret-key", "kid-1"))
	if err := svc.BootstrapSystemAgents(context.Background(), "https://agentmesh.example.com"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	agent, ok := svc.GetAgent("mesh.supervisor")
	if !ok {
		t.Fatalf("missing system agent")
	}
	if agent.Signature == nil {
		t.Fatalf("missing signature")
	}
	if err := svc.ValidateSignedAgentCard(context.Background(), agent); err != nil {
		t.Fatalf("validate: %v", err)
	}
	agent.Signature.Signature = "bad"
	if err := svc.ValidateSignedAgentCard(context.Background(), agent); err == nil {
		t.Fatalf("expected signature validation failure")
	}
}
