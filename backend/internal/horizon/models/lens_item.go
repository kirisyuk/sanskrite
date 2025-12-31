package models

import (
	"fmt"
	"math"
	"time"
)

// LensItem представляет визуальную часть линзы - то, что отображается в графе и списке.
// Аналог Interactions для Horizon.
type LensItem struct {
	ID        string `json:"id" firestore:"id"`
	TenantID  string `json:"tenantId" firestore:"tenantId"`
	ProjectID string `json:"projectId" firestore:"projectId"`
	LensID    string `json:"lensId" firestore:"lensId"` // связь с Lens

	// kind/subkind — расширяемая классификация (как у Interaction).
	Kind    string  `json:"kind" firestore:"kind"`                             // "node" | "edge" | "viewport" | "layout"
	Subkind *string `json:"subkind,omitempty" firestore:"subkind,omitempty"`   // для edge: DependencyType; для node: "fragment" | ...

	// kind="node"
	FragmentID    *string            `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	CanonicalPath *string            `json:"canonicalPath,omitempty" firestore:"canonicalPath,omitempty"`
	Label         *string            `json:"label,omitempty" firestore:"label,omitempty"`
	Type          *string            `json:"type,omitempty" firestore:"type,omitempty"` // FragmentType (строкой для гибкости)
	State         map[string]float64 `json:"state,omitempty" firestore:"state,omitempty"`
	Status        *string            `json:"status,omitempty" firestore:"status,omitempty"`

	// UI состояние узла (boolean поля вместо subkind)
	Expanded  *bool `json:"expanded,omitempty" firestore:"expanded,omitempty"`
	Collapsed *bool `json:"collapsed,omitempty" firestore:"collapsed,omitempty"`
	Critical  *bool `json:"critical,omitempty" firestore:"critical,omitempty"`

	// Позиция для графа (приоритет)
	Position *LensItemPosition `json:"position,omitempty" firestore:"position,omitempty"`

	// kind="edge"
	SourceFragmentID *string  `json:"sourceFragmentId,omitempty" firestore:"sourceFragmentId,omitempty"`
	TargetFragmentID *string  `json:"targetFragmentId,omitempty" firestore:"targetFragmentId,omitempty"`
	EdgeType         *string  `json:"edgeType,omitempty" firestore:"edgeType,omitempty"` // дублирует subkind для удобства
	Criticality      *float64 `json:"criticality,omitempty" firestore:"criticality,omitempty"`
	Amplification    *float64 `json:"amplification,omitempty" firestore:"amplification,omitempty"`
	LatencyHours     *int     `json:"latencyHours,omitempty" firestore:"latencyHours,omitempty"`
	Direction        *string  `json:"direction,omitempty" firestore:"direction,omitempty"` // "forward" | "reverse" | "bidirectional"

	// kind="viewport"
	ViewportConfig map[string]interface{} `json:"viewportConfig,omitempty" firestore:"viewportConfig,omitempty"`

	// kind="layout"
	LayoutData map[string]interface{} `json:"layoutData,omitempty" firestore:"layoutData,omitempty"`

	// metadata
	Metadata  *LensItemMetadata `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt" firestore:"updatedAt"`
}

type LensItemPosition struct {
	X float64 `json:"x" firestore:"x"`
	Y float64 `json:"y" firestore:"y"`
}

// LensItemMetadata метаданные для LensItem
type LensItemMetadata struct {
	Pinned     *bool    `json:"pinned,omitempty" firestore:"pinned,omitempty"`
	Notes      []string `json:"notes,omitempty" firestore:"notes,omitempty"`
	Visibility *string  `json:"visibility,omitempty" firestore:"visibility,omitempty"` // "visible" | "hidden" | "collapsed"
	Color      *string  `json:"color,omitempty" firestore:"color,omitempty"`
}

func (li *LensItem) Validate() error {
	if li == nil {
		return fmt.Errorf("lensItem is nil")
	}
	if li.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if li.ProjectID == "" {
		return fmt.Errorf("projectId is required")
	}
	if li.LensID == "" {
		return fmt.Errorf("lensId is required")
	}
	if li.Kind == "" {
		return fmt.Errorf("kind is required")
	}

	validKinds := map[string]bool{
		"node":     true,
		"edge":     true,
		"viewport": true,
		"layout":   true,
	}
	if !validKinds[li.Kind] {
		return fmt.Errorf("invalid kind: %s, must be one of: node, edge, viewport, layout", li.Kind)
	}

	// Validate nested position if provided
	if li.Position != nil {
		if math.IsNaN(li.Position.X) || math.IsNaN(li.Position.Y) ||
			math.IsInf(li.Position.X, 0) || math.IsInf(li.Position.Y, 0) {
			return fmt.Errorf("position must be finite numbers")
		}
	}

	switch li.Kind {
	case "node":
		if li.FragmentID == nil && li.CanonicalPath == nil {
			return fmt.Errorf("fragmentId or canonicalPath is required for node kind")
		}
	case "edge":
		if li.SourceFragmentID == nil || *li.SourceFragmentID == "" {
			return fmt.Errorf("sourceFragmentId is required for edge kind")
		}
		if li.TargetFragmentID == nil || *li.TargetFragmentID == "" {
			return fmt.Errorf("targetFragmentId is required for edge kind")
		}
		if *li.SourceFragmentID == *li.TargetFragmentID {
			return fmt.Errorf("sourceFragmentId and targetFragmentId must be different")
		}

		// subkind for edge must match DependencyType taxonomy
		if li.Subkind == nil || *li.Subkind == "" {
			return fmt.Errorf("subkind (DependencyType) is required for edge kind")
		}
		validEdgeTypes := map[string]bool{
			string(DependencyTypeSuppliesTo):   true,
			string(DependencyTypeLocatedIn):    true,
			string(DependencyTypeOwns):         true,
			string(DependencyTypeSubsidiaryOf): true,
			string(DependencyTypeProduces):     true,
			string(DependencyTypeConsumes):     true,
			string(DependencyTypeOperatesIn):   true,
			string(DependencyTypeRegulates):    true,
			string(DependencyTypeCompetesWith): true,
			string(DependencyTypeDependsOn):    true,
			string(DependencyTypeInfluences):   true,
			string(DependencyTypeTransformsInto): true,
			// Governance / org-person
			string(DependencyTypeManages):       true,
			string(DependencyTypeControls):      true,
			string(DependencyTypeEmployedBy):    true,
			string(DependencyTypeBoardMemberOf): true,
			string(DependencyTypeFounded):       true,
			string(DependencyTypeInvestsIn):     true,
			string(DependencyTypePartnersWith):  true,
		}
		if !validEdgeTypes[*li.Subkind] {
			return fmt.Errorf("invalid subkind for edge: %s", *li.Subkind)
		}
		if li.EdgeType != nil && *li.EdgeType != "" && *li.EdgeType != *li.Subkind {
			return fmt.Errorf("edgeType must match subkind when provided")
		}

		if li.Direction != nil && *li.Direction != "" {
			validDirections := map[string]bool{
				"forward":       true,
				"reverse":       true,
				"bidirectional": true,
			}
			if !validDirections[*li.Direction] {
				return fmt.Errorf("invalid direction: %s, must be one of: forward, reverse, bidirectional", *li.Direction)
			}
		}
	case "viewport", "layout":
		// No required fields beyond base.
	}

	// Validate visibility
	if li.Metadata != nil && li.Metadata.Visibility != nil && *li.Metadata.Visibility != "" {
		validVisibility := map[string]bool{
			"visible":   true,
			"hidden":    true,
			"collapsed": true,
		}
		if !validVisibility[*li.Metadata.Visibility] {
			return fmt.Errorf("invalid visibility: %s, must be one of: visible, hidden, collapsed", *li.Metadata.Visibility)
		}
	}

	return nil
}




