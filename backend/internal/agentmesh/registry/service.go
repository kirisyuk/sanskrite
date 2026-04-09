package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

type Signer interface {
	Sign(ctx context.Context, payload []byte, version string) (*contracts.AgentCardSignature, error)
	Verify(ctx context.Context, payload []byte, signature *contracts.AgentCardSignature) error
}

type Service struct {
	mu     sync.RWMutex
	agents map[string]contracts.AgentSpec
	signer Signer
}

func NewService(signer Signer) *Service {
	return &Service{
		agents: make(map[string]contracts.AgentSpec),
		signer: signer,
	}
}

func (s *Service) BootstrapSystemAgents(ctx context.Context, baseURL string) error {
	for _, spec := range systemAgents(strings.TrimRight(strings.TrimSpace(baseURL), "/")) {
		signed, err := s.SignAgentCard(ctx, spec)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.agents[signed.ID] = signed
		s.mu.Unlock()
	}
	return nil
}

func (s *Service) SignAgentCard(ctx context.Context, spec contracts.AgentSpec) (contracts.AgentSpec, error) {
	if err := spec.Validate(); err != nil {
		return contracts.AgentSpec{}, err
	}
	if s.signer == nil {
		return contracts.AgentSpec{}, fmt.Errorf("agent card signer is required")
	}
	payload, err := canonicalPayload(spec)
	if err != nil {
		return contracts.AgentSpec{}, err
	}
	signature, err := s.signer.Sign(ctx, payload, spec.A2AVersion)
	if err != nil {
		return contracts.AgentSpec{}, err
	}
	spec.Signature = signature
	return spec, nil
}

func (s *Service) ValidateSignedAgentCard(ctx context.Context, spec contracts.AgentSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if spec.Signature == nil {
		return fmt.Errorf("agent card signature is required")
	}
	if s.signer == nil {
		return fmt.Errorf("agent card signer is not configured")
	}
	payload, err := canonicalPayload(spec)
	if err != nil {
		return err
	}
	return s.signer.Verify(ctx, payload, spec.Signature)
}

func (s *Service) ParseAndValidateEncodedCard(ctx context.Context, encoded string) (*contracts.AgentSpec, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, err
	}
	var spec contracts.AgentSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, err
	}
	if err := s.ValidateSignedAgentCard(ctx, spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *Service) GetAgent(id string) (contracts.AgentSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.agents[strings.TrimSpace(id)]
	return spec, ok
}

func (s *Service) ListAgents() []contracts.AgentSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contracts.AgentSpec, 0, len(s.agents))
	for _, spec := range s.agents {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func canonicalPayload(spec contracts.AgentSpec) ([]byte, error) {
	spec.Signature = nil
	return json.Marshal(spec)
}

func systemAgents(baseURL string) []contracts.AgentSpec {
	return []contracts.AgentSpec{
		{
			ID:          "mesh.supervisor",
			DisplayName: "AgentMesh Supervisor",
			Description: "Routes tasks, enforces policy, and orchestrates execution.",
			Version:     "v1",
			A2AVersion:  "1.0.0",
			Endpoint:    baseURL + "/a2a",
			TrustZone:   contracts.TrustZoneInternal,
			Capabilities: []contracts.CapabilitySpec{
				{Name: "mesh.route", Description: "Route and prepare tasks for execution."},
				{Name: "mesh.plan", Description: "Plan execution strategy and budgets."},
				{Name: "sim.plan", Description: "Plan simulation ensemble execution and world fan-out."},
				{Name: "sim.aggregate", Description: "Aggregate world outputs into a simulation bundle."},
				{Name: "scout.plan", Description: "Plan and orchestrate one agentic scouting investigation."},
				{Name: "scout.aggregate", Description: "Aggregate one agentic scouting investigation for operator consumption."},
			},
			Policy: contracts.InvocationPolicy{
				AllowedCallers:         []string{"internal", "mesh.supervisor", "mesh.worker", "mesh.reviewer"},
				AllowedDataZones:       []string{"internal", "gcp"},
				AllowedTools:           []string{"vertex.generate", "agentmesh.task.get"},
				AllowedModels:          []string{"flash", "pro"},
				RequireSignedAgentCard: true,
			},
		},
		{
			ID:          "mesh.worker",
			DisplayName: "AgentMesh Worker",
			Description: "Executes routed tasks against Vertex.",
			Version:     "v1",
			A2AVersion:  "1.0.0",
			Endpoint:    baseURL + "/a2a",
			TrustZone:   contracts.TrustZoneGCP,
			Capabilities: []contracts.CapabilitySpec{
				{Name: "mesh.execute", Description: "Execute tasks using Vertex AI."},
				{Name: "sim.world.bootstrap", Description: "Bootstrap one simulation world and execute its ticks."},
				{Name: "sim.population.generate", Description: "Generate normalized simulation population archetypes."},
				{Name: "sim.world.step", Description: "Generate normalized actions for one simulation world step."},
				{Name: "scout.research", Description: "Run one grounded research branch for an agentic scouting investigation."},
				{Name: "scout.critic", Description: "Review research outputs and decide on path/materialization actions."},
				{Name: "scout.path", Description: "Run deterministic target-scoped supply-chain path synthesis and landing."},
				{Name: "scout.materialize", Description: "Persist agentic scouting outputs and deterministic artifact references."},
			},
			Policy: contracts.InvocationPolicy{
				AllowedCallers:         []string{"internal", "mesh.supervisor"},
				AllowedDataZones:       []string{"internal", "gcp"},
				AllowedTools:           []string{"vertex.generate", "agentmesh.task.get"},
				AllowedModels:          []string{"flash", "pro"},
				RequireSignedAgentCard: true,
			},
		},
		{
			ID:          "mesh.reviewer",
			DisplayName: "AgentMesh Reviewer",
			Description: "Validates final task output before success.",
			Version:     "v1",
			A2AVersion:  "1.0.0",
			Endpoint:    baseURL + "/a2a",
			TrustZone:   contracts.TrustZoneInternal,
			Capabilities: []contracts.CapabilitySpec{
				{Name: "mesh.review", Description: "Review output and decide acceptance/rejection."},
				{Name: "sim.review", Description: "Review simulation bundle consistency and grounding coverage."},
				{Name: "scout.review", Description: "Review final agentic scouting output before success."},
			},
			Policy: contracts.InvocationPolicy{
				AllowedCallers:         []string{"internal", "mesh.supervisor", "mesh.worker"},
				AllowedDataZones:       []string{"internal"},
				AllowedTools:           []string{"vertex.generate", "agentmesh.task.get"},
				AllowedModels:          []string{"pro"},
				RequireSignedAgentCard: true,
			},
		},
	}
}
