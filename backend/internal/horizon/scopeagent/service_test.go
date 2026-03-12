package scopeagent

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBatchKeyStableAcrossOrdering(t *testing.T) {
	a := &horizonmodels.ScopeAgentConfig{
		TenantID:    "global",
		DomainModes: []string{"regulatory", "supply_chain"},
		Roles:       []string{"forecast", "monitor"},
		LLMPolicy: horizonmodels.LLMPolicy{
			PrimaryModel:    "gemini-3-flash-preview",
			EscalationModel: "gemini-3-pro-preview",
		},
	}
	b := &horizonmodels.ScopeAgentConfig{
		TenantID:    "global",
		DomainModes: []string{"supply_chain", "regulatory"},
		Roles:       []string{"monitor", "forecast"},
		LLMPolicy: horizonmodels.LLMPolicy{
			PrimaryModel:    "gemini-3-flash-preview",
			EscalationModel: "gemini-3-pro-preview",
		},
	}
	if batchKey(a) != batchKey(b) {
		t.Fatalf("expected batch keys to match for equivalent config ordering")
	}
}

func TestProposalDedupKeyStableAcrossCanonicalPathOrdering(t *testing.T) {
	a := &horizonmodels.AgentProposal{
		TenantID:             "global",
		ProposalType:         string(horizonmodels.AgentProposalTypeSignalCandidate),
		SignalKey:            "pressure:market",
		TargetCanonicalPaths: []string{"/loc/ua", "/org/us/tesla-inc"},
	}
	b := &horizonmodels.AgentProposal{
		TenantID:             "global",
		ProposalType:         string(horizonmodels.AgentProposalTypeSignalCandidate),
		SignalKey:            "pressure:market",
		TargetCanonicalPaths: []string{"/org/us/tesla-inc", "/loc/ua"},
	}
	if proposalDedupKey(a) != proposalDedupKey(b) {
		t.Fatalf("expected dedup key to ignore canonical path ordering")
	}
}
