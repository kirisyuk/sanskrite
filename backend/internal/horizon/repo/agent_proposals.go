package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type AgentProposalsRepository struct {
	col *firestore.CollectionRef
}

func NewAgentProposalsRepository(db *firestore.Client) *AgentProposalsRepository {
	return &AgentProposalsRepository{col: db.Collection("horizon_agent_proposals")}
}

func (r *AgentProposalsRepository) Create(ctx context.Context, proposal *models.AgentProposal) (string, error) {
	if r == nil || r.col == nil || proposal == nil {
		return "", ErrInvalidDocument
	}
	if err := proposal.Validate(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = now
	}
	proposal.UpdatedAt = now
	doc := r.col.NewDoc()
	proposal.ID = doc.ID
	_, err := doc.Set(ctx, proposal)
	return proposal.ID, err
}

func (r *AgentProposalsRepository) Update(ctx context.Context, proposal *models.AgentProposal) error {
	if r == nil || r.col == nil || proposal == nil || strings.TrimSpace(proposal.ID) == "" {
		return ErrInvalidDocument
	}
	if err := proposal.Validate(); err != nil {
		return err
	}
	proposal.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(proposal.ID)).Set(ctx, proposal)
	return err
}

func (r *AgentProposalsRepository) Get(ctx context.Context, id string) (*models.AgentProposal, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var proposal models.AgentProposal
	if err := snap.DataTo(&proposal); err != nil {
		return nil, err
	}
	if proposal.ID == "" {
		proposal.ID = snap.Ref.ID
	}
	return &proposal, nil
}

func (r *AgentProposalsRepository) FindByDedupKey(ctx context.Context, tenantID, dedupKey string) (*models.AgentProposal, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(dedupKey) == "" {
		return nil, ErrInvalidDocument
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("dedupKey", "==", strings.TrimSpace(dedupKey)).OrderBy("createdAt", firestore.Desc).Limit(1).Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	var proposal models.AgentProposal
	if err := docs[0].DataTo(&proposal); err != nil {
		return nil, err
	}
	if proposal.ID == "" {
		proposal.ID = docs[0].Ref.ID
	}
	return &proposal, nil
}

func (r *AgentProposalsRepository) ListByWatchTarget(ctx context.Context, tenantID, watchTargetID string, limit int) ([]*models.AgentProposal, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(watchTargetID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("watchTargetId", "==", strings.TrimSpace(watchTargetID)).OrderBy("createdAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.AgentProposal, 0, len(docs))
	for _, doc := range docs {
		var proposal models.AgentProposal
		if err := doc.DataTo(&proposal); err != nil {
			continue
		}
		if proposal.ID == "" {
			proposal.ID = doc.Ref.ID
		}
		out = append(out, &proposal)
	}
	return out, nil
}
