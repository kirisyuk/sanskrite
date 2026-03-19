package contracts

import (
	"fmt"
	"strings"
	"time"
)

type TrustZone string

const (
	TrustZoneInternal TrustZone = "internal"
	TrustZoneGCP      TrustZone = "gcp"
	TrustZoneExternal TrustZone = "external"
)

type InvocationPolicy struct {
	AllowedCallers         []string `json:"allowedCallers,omitempty" firestore:"allowedCallers,omitempty"`
	AllowedDataZones       []string `json:"allowedDataZones,omitempty" firestore:"allowedDataZones,omitempty"`
	AllowedTools           []string `json:"allowedTools,omitempty" firestore:"allowedTools,omitempty"`
	AllowedModels          []string `json:"allowedModels,omitempty" firestore:"allowedModels,omitempty"`
	HumanApprovalRequired  bool     `json:"humanApprovalRequired,omitempty" firestore:"humanApprovalRequired,omitempty"`
	RequireSignedAgentCard bool     `json:"requireSignedAgentCard,omitempty" firestore:"requireSignedAgentCard,omitempty"`
}

type CapabilitySpec struct {
	Name         string         `json:"name" firestore:"name"`
	Description  string         `json:"description,omitempty" firestore:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema,omitempty" firestore:"inputSchema,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty" firestore:"outputSchema,omitempty"`
}

type AgentCardSignature struct {
	Alg       string    `json:"alg" firestore:"alg"`
	KeyID     string    `json:"keyId" firestore:"keyId"`
	Signature string    `json:"signature" firestore:"signature"`
	SignedAt  time.Time `json:"signedAt" firestore:"signedAt"`
	Version   string    `json:"version" firestore:"version"`
}

type AgentSpec struct {
	ID           string              `json:"id" firestore:"id"`
	DisplayName  string              `json:"displayName" firestore:"displayName"`
	Description  string              `json:"description,omitempty" firestore:"description,omitempty"`
	Version      string              `json:"version" firestore:"version"`
	A2AVersion   string              `json:"a2aVersion" firestore:"a2aVersion"`
	Endpoint     string              `json:"endpoint,omitempty" firestore:"endpoint,omitempty"`
	TrustZone    TrustZone           `json:"trustZone" firestore:"trustZone"`
	Capabilities []CapabilitySpec    `json:"capabilities" firestore:"capabilities"`
	Policy       InvocationPolicy    `json:"policy" firestore:"policy"`
	Metadata     map[string]any      `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	Signature    *AgentCardSignature `json:"signature,omitempty" firestore:"signature,omitempty"`
}

func (s AgentSpec) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("agent id is required")
	}
	if strings.TrimSpace(s.DisplayName) == "" {
		return fmt.Errorf("agent displayName is required")
	}
	if strings.TrimSpace(s.Version) == "" {
		return fmt.Errorf("agent version is required")
	}
	if strings.TrimSpace(s.A2AVersion) == "" {
		return fmt.Errorf("agent a2aVersion is required")
	}
	if s.TrustZone == "" {
		return fmt.Errorf("agent trustZone is required")
	}
	if len(s.Capabilities) == 0 {
		return fmt.Errorf("agent capabilities are required")
	}
	for _, c := range s.Capabilities {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("capability name is required")
		}
	}
	return nil
}

func (s AgentSpec) SupportsCapability(capability string) bool {
	capability = strings.TrimSpace(capability)
	if capability == "" {
		return false
	}
	for _, c := range s.Capabilities {
		if strings.EqualFold(strings.TrimSpace(c.Name), capability) {
			return true
		}
	}
	return false
}
