package models

import (
	"fmt"
	"time"
)

// LensArtifactType distinguishes between fragment and edge artifacts.
type LensArtifactType string

const (
	LensArtifactTypeFragment LensArtifactType = "FRAGMENT"
	LensArtifactTypeEdge     LensArtifactType = "EDGE"
)

// LensArtifact represents a finalized fragment or edge created within a lens's jobs.
// This collection accumulates all entities and relations that passed canonicalization
// and policy validation, serving as the input for Phase 2 Strategic Overlay.
//
// Key properties:
//   - Owned by a Lens (not by a Domain)
//   - Contains references to global fragments/edges (not copies)
//   - Populated by jobs upon successful completion
//   - Used by Phase 2 to build strategic decision layer
//
// Firestore collection: horizon_lens_artifacts
// Composite indexes required for efficient queries.
type LensArtifact struct {
	ID       string `json:"id" firestore:"id"`
	TenantID string `json:"tenantId" firestore:"tenantId"`
	LensID   string `json:"lensId" firestore:"lensId"`

	// Artifact type: FRAGMENT or EDGE
	Type LensArtifactType `json:"type" firestore:"type"`

	// For FRAGMENT artifacts: reference to global fragment
	FragmentID    string `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	CanonicalPath string `json:"canonicalPath,omitempty" firestore:"canonicalPath,omitempty"`
	FragmentType  string `json:"fragmentType,omitempty" firestore:"fragmentType,omitempty"` // ORGANIZATION, PERSON, etc.

	// For EDGE artifacts: reference to global edge
	EdgeID         string `json:"edgeId,omitempty" firestore:"edgeId,omitempty"`
	EdgeType       string `json:"edgeType,omitempty" firestore:"edgeType,omitempty"` // SUPPLIES_TO, OWNS, etc.
	SourcePath     string `json:"sourcePath,omitempty" firestore:"sourcePath,omitempty"`
	TargetPath     string `json:"targetPath,omitempty" firestore:"targetPath,omitempty"`
	SourceFragID   string `json:"sourceFragId,omitempty" firestore:"sourceFragId,omitempty"`
	TargetFragID   string `json:"targetFragId,omitempty" firestore:"targetFragId,omitempty"`

	// Provenance: which job created/updated this artifact
	SourceJobID string `json:"sourceJobId" firestore:"sourceJobId"`

	// Timestamps
	AddedAt   time.Time `json:"addedAt" firestore:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	// Strategic overlay processing status
	// nil = not yet processed, set = when Phase 2 last processed this artifact
	ProcessedForStrategicAt *time.Time `json:"processedForStrategicAt,omitempty" firestore:"processedForStrategicAt,omitempty"`

	// Optional: display label for LLM context building (simplified form)
	DisplayLabel string `json:"displayLabel,omitempty" firestore:"displayLabel,omitempty"`
}

// Validate checks LensArtifact invariants.
func (a *LensArtifact) Validate() error {
	if a.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if a.LensID == "" {
		return fmt.Errorf("lensId is required")
	}
	if a.Type != LensArtifactTypeFragment && a.Type != LensArtifactTypeEdge {
		return fmt.Errorf("type must be FRAGMENT or EDGE")
	}
	if a.Type == LensArtifactTypeFragment {
		if a.FragmentID == "" {
			return fmt.Errorf("fragmentId is required for FRAGMENT artifacts")
		}
		if a.CanonicalPath == "" {
			return fmt.Errorf("canonicalPath is required for FRAGMENT artifacts")
		}
	}
	if a.Type == LensArtifactTypeEdge {
		if a.EdgeID == "" {
			return fmt.Errorf("edgeId is required for EDGE artifacts")
		}
		if a.SourceFragID == "" || a.TargetFragID == "" {
			return fmt.Errorf("sourceFragId and targetFragId are required for EDGE artifacts")
		}
	}
	if a.SourceJobID == "" {
		return fmt.Errorf("sourceJobId is required")
	}
	return nil
}

// IsFragment returns true if this artifact references a fragment.
func (a *LensArtifact) IsFragment() bool {
	return a.Type == LensArtifactTypeFragment
}

// IsEdge returns true if this artifact references an edge.
func (a *LensArtifact) IsEdge() bool {
	return a.Type == LensArtifactTypeEdge
}

// NeedsStrategicProcessing returns true if this artifact hasn't been processed
// by Phase 2 Strategic Overlay yet.
func (a *LensArtifact) NeedsStrategicProcessing() bool {
	return a.ProcessedForStrategicAt == nil
}

// MarkProcessedForStrategic sets the processing timestamp.
func (a *LensArtifact) MarkProcessedForStrategic() {
	now := time.Now()
	a.ProcessedForStrategicAt = &now
	a.UpdatedAt = now
}

// NewFragmentArtifact creates a LensArtifact for a fragment.
func NewFragmentArtifact(tenantID, lensID, fragmentID, canonicalPath, fragmentType, sourceJobID, displayLabel string) *LensArtifact {
	now := time.Now()
	return &LensArtifact{
		TenantID:      tenantID,
		LensID:        lensID,
		Type:          LensArtifactTypeFragment,
		FragmentID:    fragmentID,
		CanonicalPath: canonicalPath,
		FragmentType:  fragmentType,
		SourceJobID:   sourceJobID,
		DisplayLabel:  displayLabel,
		AddedAt:       now,
		UpdatedAt:     now,
	}
}

// NewEdgeArtifact creates a LensArtifact for an edge.
func NewEdgeArtifact(tenantID, lensID, edgeID, edgeType, sourcePath, targetPath, sourceFragID, targetFragID, sourceJobID string) *LensArtifact {
	now := time.Now()
	return &LensArtifact{
		TenantID:     tenantID,
		LensID:       lensID,
		Type:         LensArtifactTypeEdge,
		EdgeID:       edgeID,
		EdgeType:     edgeType,
		SourcePath:   sourcePath,
		TargetPath:   targetPath,
		SourceFragID: sourceFragID,
		TargetFragID: targetFragID,
		SourceJobID:  sourceJobID,
		AddedAt:      now,
		UpdatedAt:    now,
	}
}

// LensArtifactPage represents a paginated batch of artifacts for LLM processing.
// Artifacts are grouped by connectivity to maximize context coherence per page.
type LensArtifactPage struct {
	Artifacts     []*LensArtifact `json:"artifacts"`
	PageNumber    int             `json:"pageNumber"`
	TotalPages    int             `json:"totalPages"`
	TotalArtifacts int            `json:"totalArtifacts"`
	NextCursor    string          `json:"nextCursor,omitempty"`
}

// LensArtifactContextItem represents a simplified artifact for LLM context.
// Uses short IDs (f1, f2, r1, r2) instead of real UUIDs.
type LensArtifactContextItem struct {
	ShortID      string `json:"id"`      // f1, f2, r1, r2, etc.
	Type         string `json:"type"`    // fragment type or edge type
	Label        string `json:"label"`   // display label
	Path         string `json:"path"`    // canonical path (for fragments)
	SourceID     string `json:"source,omitempty"` // for edges: short ID of source
	TargetID     string `json:"target,omitempty"` // for edges: short ID of target
}

// LensArtifactContext is the simplified context sent to LLM for strategic overlay.
type LensArtifactContext struct {
	Fragments []LensArtifactContextItem `json:"fragments"`
	Edges     []LensArtifactContextItem `json:"edges"`
	
	// Mapping for decoding LLM response
	// shortID -> real artifact
	fragmentMapping map[string]*LensArtifact
	edgeMapping     map[string]*LensArtifact
}

// NewLensArtifactContext builds a simplified context from artifacts.
func NewLensArtifactContext(artifacts []*LensArtifact) *LensArtifactContext {
	ctx := &LensArtifactContext{
		Fragments:       make([]LensArtifactContextItem, 0),
		Edges:           make([]LensArtifactContextItem, 0),
		fragmentMapping: make(map[string]*LensArtifact),
		edgeMapping:     make(map[string]*LensArtifact),
	}
	
	fragCounter := 1
	edgeCounter := 1
	fragIDToShort := make(map[string]string) // fragmentID -> shortID
	
	// First pass: fragments
	for _, a := range artifacts {
		if a.IsFragment() {
			shortID := fmt.Sprintf("f%d", fragCounter)
			fragCounter++
			fragIDToShort[a.FragmentID] = shortID
			ctx.fragmentMapping[shortID] = a
			
			label := a.DisplayLabel
			if label == "" {
				label = a.CanonicalPath
			}
			ctx.Fragments = append(ctx.Fragments, LensArtifactContextItem{
				ShortID: shortID,
				Type:    a.FragmentType,
				Label:   label,
				Path:    a.CanonicalPath,
			})
		}
	}
	
	// Second pass: edges (with resolved source/target short IDs)
	for _, a := range artifacts {
		if a.IsEdge() {
			shortID := fmt.Sprintf("r%d", edgeCounter)
			edgeCounter++
			ctx.edgeMapping[shortID] = a
			
			sourceShort := fragIDToShort[a.SourceFragID]
			targetShort := fragIDToShort[a.TargetFragID]
			if sourceShort == "" {
				sourceShort = a.SourcePath // fallback to path
			}
			if targetShort == "" {
				targetShort = a.TargetPath
			}
			
			ctx.Edges = append(ctx.Edges, LensArtifactContextItem{
				ShortID:  shortID,
				Type:     a.EdgeType,
				SourceID: sourceShort,
				TargetID: targetShort,
			})
		}
	}
	
	return ctx
}

// GetFragmentByShortID resolves a short ID to the original artifact.
func (c *LensArtifactContext) GetFragmentByShortID(shortID string) *LensArtifact {
	return c.fragmentMapping[shortID]
}

// GetEdgeByShortID resolves a short ID to the original artifact.
func (c *LensArtifactContext) GetEdgeByShortID(shortID string) *LensArtifact {
	return c.edgeMapping[shortID]
}

// AllFragmentShortIDs returns all fragment short IDs.
func (c *LensArtifactContext) AllFragmentShortIDs() []string {
	ids := make([]string, 0, len(c.fragmentMapping))
	for id := range c.fragmentMapping {
		ids = append(ids, id)
	}
	return ids
}

// AllEdgeShortIDs returns all edge short IDs.
func (c *LensArtifactContext) AllEdgeShortIDs() []string {
	ids := make([]string, 0, len(c.edgeMapping))
	for id := range c.edgeMapping {
		ids = append(ids, id)
	}
	return ids
}
