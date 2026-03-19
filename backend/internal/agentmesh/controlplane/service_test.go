package controlplane

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
)

func TestRouteTask(t *testing.T) {
	reg := registry.NewService(registry.NewHMACSigner("secret", "kid"))
	if err := reg.BootstrapSystemAgents(context.Background(), "https://agentmesh.example.com"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	cp := NewService(reg, policy.NewService())
	task := contracts.MeshTask{
		TenantID:   "global",
		Capability: "mesh.review",
	}
	agent, err := cp.RouteTask(task)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if agent.ID != "mesh.reviewer" {
		t.Fatalf("agent=%q", agent.ID)
	}
}

func TestRouteTaskFallsBackToWorker(t *testing.T) {
	reg := registry.NewService(registry.NewHMACSigner("secret", "kid"))
	if err := reg.BootstrapSystemAgents(context.Background(), "https://agentmesh.example.com"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	cp := NewService(reg, policy.NewService())
	agent, err := cp.RouteTask(contracts.MeshTask{
		TenantID:   "global",
		Capability: "capability.not.registered",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if agent.ID != "mesh.worker" {
		t.Fatalf("fallback agent=%q", agent.ID)
	}
}
