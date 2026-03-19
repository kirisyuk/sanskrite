package policy

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

func TestEvaluateToolInvocation(t *testing.T) {
	svc := NewService()
	agent := contracts.AgentSpec{
		ID:          "mesh.worker",
		DisplayName: "Worker",
		Version:     "v1",
		A2AVersion:  "1.0.0",
		TrustZone:   contracts.TrustZoneInternal,
		Capabilities: []contracts.CapabilitySpec{
			{Name: "mesh.execute"},
		},
		Policy: contracts.InvocationPolicy{
			AllowedTools: []string{"vertex.generate"},
		},
	}
	if err := svc.EvaluateToolInvocation(agent, "vertex.generate"); err != nil {
		t.Fatalf("allowed tool rejected: %v", err)
	}
	if err := svc.EvaluateToolInvocation(agent, "agentmesh.task.get"); err == nil {
		t.Fatalf("expected forbidden tool failure")
	}
}
