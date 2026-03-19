package controlplane

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
)

type Service struct {
	registry *registry.Service
	policy   *policy.Service
}

func NewService(registrySvc *registry.Service, policySvc *policy.Service) *Service {
	return &Service{registry: registrySvc, policy: policySvc}
}

func (s *Service) PrepareTask(task *contracts.MeshTask, req policy.RequestContext) (*contracts.AgentSpec, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	if err := task.ValidateForCreate(); err != nil {
		return nil, err
	}
	agent, err := s.RouteTask(*task)
	if err != nil {
		return nil, err
	}
	if err := s.policy.EvaluateAgentInvocation(agent, req); err != nil {
		return nil, err
	}
	task.RoutedAgentID = agent.ID
	task.ReviewAgentID = "mesh.reviewer"
	task.ApprovalRequired = task.ApprovalRequired || agent.Policy.HumanApprovalRequired
	return &agent, nil
}

func (s *Service) RouteTask(task contracts.MeshTask) (contracts.AgentSpec, error) {
	if s == nil || s.registry == nil {
		return contracts.AgentSpec{}, fmt.Errorf("control plane registry is not configured")
	}
	if requested := strings.TrimSpace(task.RequestedAgentID); requested != "" {
		if agent, ok := s.registry.GetAgent(requested); ok {
			return agent, nil
		}
		return contracts.AgentSpec{}, fmt.Errorf("requested agent %q not found", requested)
	}
	for _, agent := range s.registry.ListAgents() {
		if agent.SupportsCapability(strings.TrimSpace(task.Capability)) {
			return agent, nil
		}
	}
	if agent, ok := s.registry.GetAgent("mesh.worker"); ok {
		return agent, nil
	}
	return contracts.AgentSpec{}, fmt.Errorf("no agent found for capability %q", task.Capability)
}
