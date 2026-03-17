package models

import (
	"fmt"
	"strings"
	"time"
)

type MacroAgentStatus string

const (
	MacroAgentStatusActive MacroAgentStatus = "ACTIVE"
	MacroAgentStatusPaused MacroAgentStatus = "PAUSED"
)

type MacroAgentClass string

const (
	MacroAgentClassCountry      MacroAgentClass = "country"
	MacroAgentClassMarket       MacroAgentClass = "market"
	MacroAgentClassEntity       MacroAgentClass = "entity"
	MacroAgentClassLensUniverse MacroAgentClass = "lens_universe"
	MacroAgentClassPathOverlay  MacroAgentClass = "path_overlay"
)

type MacroAgentPolicy struct {
	ID                   string         `json:"id" firestore:"id"`
	TenantID             string         `json:"tenantId" firestore:"tenantId"`
	ProjectID            string         `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID               string         `json:"lensId,omitempty" firestore:"lensId,omitempty"`
	Name                 string         `json:"name" firestore:"name"`
	Status               string         `json:"status" firestore:"status"`
	AgentClass           string         `json:"agentClass" firestore:"agentClass"`
	AnchorCanonicalPath  string         `json:"anchorCanonicalPath,omitempty" firestore:"anchorCanonicalPath,omitempty"`
	ArtifactCanonicalKey string         `json:"artifactCanonicalKey,omitempty" firestore:"artifactCanonicalKey,omitempty"`
	DomainModes          []string       `json:"domainModes,omitempty" firestore:"domainModes,omitempty"`
	Roles                []string       `json:"roles,omitempty" firestore:"roles,omitempty"`
	Priority             int            `json:"priority" firestore:"priority"`
	SourceMixPolicy      map[string]any `json:"sourceMixPolicy,omitempty" firestore:"sourceMixPolicy,omitempty"`
	TimeWindowPolicy     map[string]any `json:"timeWindowPolicy,omitempty" firestore:"timeWindowPolicy,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt            time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (p *MacroAgentPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("macro agent policy is required")
	}
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch strings.TrimSpace(p.AgentClass) {
	case string(MacroAgentClassCountry), string(MacroAgentClassMarket), string(MacroAgentClassEntity),
		string(MacroAgentClassLensUniverse), string(MacroAgentClassPathOverlay):
	default:
		return fmt.Errorf("invalid agentClass")
	}
	if strings.TrimSpace(p.AgentClass) == string(MacroAgentClassLensUniverse) {
		if strings.TrimSpace(p.LensID) == "" {
			return fmt.Errorf("lensId is required for lens_universe")
		}
	} else if strings.TrimSpace(p.AgentClass) == string(MacroAgentClassPathOverlay) {
		if strings.TrimSpace(p.ArtifactCanonicalKey) == "" {
			return fmt.Errorf("artifactCanonicalKey is required for path_overlay")
		}
	} else if strings.TrimSpace(p.AnchorCanonicalPath) == "" {
		return fmt.Errorf("anchorCanonicalPath is required")
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = string(MacroAgentStatusActive)
	}
	if p.Priority == 0 {
		p.Priority = 50
	}
	return nil
}
