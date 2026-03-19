package policy

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

type RequestContext struct {
	CallerAgentID string
	DataZone      string
	ModelAlias    string
	ToolName      string
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) EvaluateAgentInvocation(agent contracts.AgentSpec, req RequestContext) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	if req.CallerAgentID != "" && len(agent.Policy.AllowedCallers) > 0 && !containsFold(agent.Policy.AllowedCallers, req.CallerAgentID) && !containsFold(agent.Policy.AllowedCallers, "internal") {
		return fmt.Errorf("caller %q is not allowed to invoke %s", req.CallerAgentID, agent.ID)
	}
	if req.DataZone != "" && len(agent.Policy.AllowedDataZones) > 0 && !containsFold(agent.Policy.AllowedDataZones, req.DataZone) {
		return fmt.Errorf("data zone %q is not allowed for %s", req.DataZone, agent.ID)
	}
	if req.ModelAlias != "" && len(agent.Policy.AllowedModels) > 0 && !containsFold(agent.Policy.AllowedModels, req.ModelAlias) {
		return fmt.Errorf("model alias %q is not allowed for %s", req.ModelAlias, agent.ID)
	}
	return nil
}

func (s *Service) EvaluateToolInvocation(agent contracts.AgentSpec, toolName string) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return fmt.Errorf("tool name is required")
	}
	if len(agent.Policy.AllowedTools) > 0 && !containsFold(agent.Policy.AllowedTools, toolName) {
		return fmt.Errorf("tool %q is not allowed for %s", toolName, agent.ID)
	}
	return nil
}

func (s *Service) EvaluateModelAlias(agent contracts.AgentSpec, modelAlias string) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	modelAlias = strings.TrimSpace(modelAlias)
	if modelAlias == "" {
		return nil
	}
	if len(agent.Policy.AllowedModels) > 0 && !containsFold(agent.Policy.AllowedModels, modelAlias) {
		return fmt.Errorf("model alias %q is not allowed for %s", modelAlias, agent.ID)
	}
	return nil
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}
