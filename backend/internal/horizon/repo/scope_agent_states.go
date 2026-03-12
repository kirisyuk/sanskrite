package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type ScopeAgentStatesRepository struct {
	col *firestore.CollectionRef
}

func NewScopeAgentStatesRepository(db *firestore.Client) *ScopeAgentStatesRepository {
	return &ScopeAgentStatesRepository{col: db.Collection("horizon_scope_agent_states")}
}

func (r *ScopeAgentStatesRepository) Upsert(ctx context.Context, state *models.ScopeAgentState) error {
	if r == nil || r.col == nil || state == nil {
		return ErrInvalidDocument
	}
	if err := state.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	if strings.TrimSpace(state.ID) == "" {
		state.ID = strings.TrimSpace(state.ConfigID)
	}
	_, err := r.col.Doc(strings.TrimSpace(state.ID)).Set(ctx, state)
	return err
}

func (r *ScopeAgentStatesRepository) GetByConfigID(ctx context.Context, configID string) (*models.ScopeAgentState, error) {
	if r == nil || r.col == nil || strings.TrimSpace(configID) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(configID)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var state models.ScopeAgentState
	if err := snap.DataTo(&state); err != nil {
		return nil, err
	}
	if state.ID == "" {
		state.ID = snap.Ref.ID
	}
	return &state, nil
}

func (r *ScopeAgentStatesRepository) MergeWakeReason(ctx context.Context, configID, reason string, debounceUntil *time.Time) error {
	state, err := r.GetByConfigID(ctx, configID)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason != "" {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(state.PendingWakeReasons)+1)
		for _, existing := range state.PendingWakeReasons {
			existing = strings.TrimSpace(existing)
			if existing == "" {
				continue
			}
			if _, ok := seen[existing]; ok {
				continue
			}
			seen[existing] = struct{}{}
			out = append(out, existing)
		}
		if _, ok := seen[reason]; !ok {
			out = append(out, reason)
		}
		state.PendingWakeReasons = out
	}
	if debounceUntil != nil {
		state.DebounceUntil = debounceUntil
	}
	return r.Upsert(ctx, state)
}
