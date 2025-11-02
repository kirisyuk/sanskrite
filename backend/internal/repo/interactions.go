package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

// ParticipantRef references an author entity (human/ai/system)
type ParticipantRef struct {
	ParticipantID string  `json:"participantId" firestore:"participantId"`
	Kind          string  `json:"kind" firestore:"kind"` // human|ai|system|tool
	DisplayName   *string `json:"displayName,omitempty" firestore:"displayName,omitempty"`
}

// AgentProfileSnapshot stores minimal model config alongside an interaction
// Note: full AgentProfiles collection is not required yet; two presets will be provided on the frontend
type AgentProfileSnapshot struct {
	Provider     string          `json:"provider" firestore:"provider"` // gemini|openai|grok
	Model        string          `json:"model" firestore:"model"`
	Version      *string         `json:"version,omitempty" firestore:"version,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty" firestore:"capabilities,omitempty"` // tools, webSearch, reasoning, refinePrevious, etc.
	Params       map[string]any  `json:"params,omitempty" firestore:"params,omitempty"`             // temperature, systemPromptHash, etc.
}

// InteractionMetadata contains extensible metadata flags and settings
type InteractionMetadata struct {
	Pinned *bool    `json:"pinned,omitempty" firestore:"pinned,omitempty"` // User-pinned for quick access
	Notes  []string `json:"notes,omitempty" firestore:"notes,omitempty"`
}

type Interaction struct {
	ID               string  `json:"id" firestore:"id"`
	TenantID         string  `json:"tenantId" firestore:"tenantId"`
	ProjectID        string  `json:"projectId" firestore:"projectId"`
	Kind             string  `json:"kind" firestore:"kind"` // e.g., "discovery.answer"
	Subkind          *string `json:"subkind,omitempty" firestore:"subkind,omitempty"`
	Content          string  `json:"content" firestore:"content"`
	ContentSanitized *string `json:"contentSanitized,omitempty" firestore:"contentSanitized,omitempty"`
	Summary          *string `json:"summary,omitempty" firestore:"summary,omitempty"`
	// Sections: optional grouping of interactions inside Unfold chat
	SectionID *string `json:"sectionId,omitempty" firestore:"sectionId,omitempty"`
	// Optional model/provider metadata for assistant/system interactions
	Model *string    `json:"model,omitempty" firestore:"model,omitempty"`
	Usage *UsageMeta `json:"usage,omitempty" firestore:"usage,omitempty"`
	// Extended multi-participant fields (optional, backward compatible)
	Author       *ParticipantRef       `json:"author,omitempty" firestore:"author,omitempty"`
	Recipients   []string              `json:"recipients,omitempty" firestore:"recipients,omitempty"`
	ReplyTo      *string               `json:"replyTo,omitempty" firestore:"replyTo,omitempty"`
	AgentProfile *AgentProfileSnapshot `json:"agentProfile,omitempty" firestore:"agentProfile,omitempty"`
	Evaluations  map[string]any        `json:"evaluations,omitempty" firestore:"evaluations,omitempty"`
	Metadata     *InteractionMetadata  `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	// Relations between interactions (answers/refines/evaluates)
	Relations []struct {
		Type     string `json:"type" firestore:"type"`
		TargetID string `json:"targetId" firestore:"targetId"`
	} `json:"relations,omitempty" firestore:"relations,omitempty"`
	// Additional optional metadata used by the UI
	Language                 *string `json:"language,omitempty" firestore:"language,omitempty"`
	SystemInstructionApplied *string `json:"systemInstructionApplied,omitempty" firestore:"systemInstructionApplied,omitempty"`
	GenerationParams         *struct {
		Temperature      *float64 `json:"temperature,omitempty" firestore:"temperature,omitempty"`
		MaxOutputTokens  *int     `json:"maxOutputTokens,omitempty" firestore:"maxOutputTokens,omitempty"`
		GroundingEnabled *bool    `json:"groundingEnabled,omitempty" firestore:"groundingEnabled,omitempty"`
	} `json:"generationParams,omitempty" firestore:"generationParams,omitempty"`
	HighRisk *struct {
		Level     string   `json:"level" firestore:"level"`
		Score     float64  `json:"score" firestore:"score"`
		Rationale *string  `json:"rationale,omitempty" firestore:"rationale,omitempty"`
		Triggers  []string `json:"triggers,omitempty" firestore:"triggers,omitempty"`
	} `json:"highRisk,omitempty" firestore:"highRisk,omitempty"`
	DiscussionFacts *struct {
		Dependencies      *string `json:"dependencies,omitempty" firestore:"dependencies,omitempty"`           // What the decision/action depends on
		Prerequisites     *string `json:"prerequisites,omitempty" firestore:"prerequisites,omitempty"`         // What must happen first
		Participants      *string `json:"participants,omitempty" firestore:"participants,omitempty"`          // Who is involved (user, family, colleagues, clients)
		ParticipantRoles  *string `json:"participantRoles,omitempty" firestore:"participantRoles,omitempty"`  // Roles and relationships between participants
		AvailableResources *string `json:"availableResources,omitempty" firestore:"availableResources,omitempty"` // Not only constraints, but what is available
		PreviousAttempts  *string `json:"previousAttempts,omitempty" firestore:"previousAttempts,omitempty"` // Previous attempts or experience
		ResultFormat      *string `json:"resultFormat,omitempty" firestore:"resultFormat,omitempty"`         // Format of desired result
	} `json:"discussionFacts,omitempty" firestore:"discussionFacts,omitempty"`
	OriginalMessageID *string   `json:"originalMessageId,omitempty" firestore:"originalMessageId,omitempty"`
	CoreDocIDs        []string  `json:"coreDocIds,omitempty" firestore:"coreDocIds,omitempty"`
	SearchQueries     []string  `json:"searchQueries,omitempty" firestore:"searchQueries,omitempty"`
	Sources           []string  `json:"sources,omitempty" firestore:"sources,omitempty"`
	CreatedAt         time.Time `json:"createdAt" firestore:"createdAt"`
}

// UsageMeta captures token usage and related counters reported by providers
type UsageMeta struct {
	PromptTokens        int `json:"promptTokens,omitempty" firestore:"promptTokens,omitempty"`
	OutputTokens        int `json:"outputTokens,omitempty" firestore:"outputTokens,omitempty"`
	TotalTokens         int `json:"totalTokens,omitempty" firestore:"totalTokens,omitempty"`
	ReasoningTokens     int `json:"reasoningTokens,omitempty" firestore:"reasoningTokens,omitempty"`
	ToolUsePromptTokens int `json:"toolUsePromptTokens,omitempty" firestore:"toolUsePromptTokens,omitempty"`
}

type InteractionsRepository struct{ col *firestore.CollectionRef }

func NewInteractionsRepository(db *firestore.Client) *InteractionsRepository {
	return &InteractionsRepository{col: db.Collection("core_interactions")}
}

func (r *InteractionsRepository) Create(ctx context.Context, it *Interaction) (string, error) {
	doc := r.col.NewDoc()
	it.ID = doc.ID
	it.CreatedAt = time.Now().UTC()
	_, err := doc.Set(ctx, it)
	if err != nil {
		return "", err
	}
	return doc.ID, nil
}

// Update applies a partial update to an interaction document by id using merge semantics
func (r *InteractionsRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return ErrInvalidDocument
	}
	if len(updates) == 0 {
		return nil
	}
	// ensure server-side updated CreatedAt is not accidentally overwritten
	delete(updates, "id")
	delete(updates, "tenantId")
	delete(updates, "projectId")
	delete(updates, "createdAt")
	_, err := r.col.Doc(id).Set(ctx, updates, firestore.MergeAll)
	return err
}

// Get returns an interaction by id
func (r *InteractionsRepository) Get(ctx context.Context, id string) (*Interaction, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var it Interaction
	if err := snap.DataTo(&it); err != nil {
		return nil, err
	}
	return &it, nil
}

// DeleteByID deletes an interaction document by id
func (r *InteractionsRepository) DeleteByID(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteByProject removes all interactions for a given project
func (r *InteractionsRepository) DeleteByProject(ctx context.Context, projectID string) error {
	// Delete documents matching either field casing (backward compatibility)
	fieldNames := []string{"projectId", "ProjectID"}
	for _, fname := range fieldNames {
		docs, err := r.col.Where(fname, "==", projectID).Documents(ctx).GetAll()
		if err != nil {
			continue
		}
		for _, d := range docs {
			_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
		}
	}
	return nil
}

// ListByProject returns interactions for a project ordered by createdAt asc
func (r *InteractionsRepository) ListByProject(ctx context.Context, projectID string) ([]*Interaction, error) {
	// Try modern field first, then legacy casing
	fieldNames := []string{"projectId", "ProjectID"}
	var res []*Interaction
	for i, fname := range fieldNames {
		q := r.col.Where(fname, "==", projectID).OrderBy("createdAt", firestore.Asc)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			if i == len(fieldNames)-1 {
				return nil, err
			}
			continue
		}
		for _, d := range docs {
			var it Interaction
			if err := d.DataTo(&it); err != nil {
				continue
			}
			res = append(res, &it)
		}
		if len(res) > 0 {
			break
		}
	}
	return res, nil
}
