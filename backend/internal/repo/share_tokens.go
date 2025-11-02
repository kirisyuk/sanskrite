package repo

import (
    "context"
    "time"

    "cloud.google.com/go/firestore"
)

type ShareToken struct {
    ID        string    `json:"id" firestore:"id"`
    ProjectID string    `json:"projectId" firestore:"projectId"`
    Token     string    `json:"token" firestore:"token"`
    IsActive  bool      `json:"isActive" firestore:"isActive"`
    ExpiresAt time.Time `json:"expiresAt" firestore:"expiresAt"`
    CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
    CreatedBy string    `json:"createdBy" firestore:"createdBy"`
    LastAccessedAt *time.Time `json:"lastAccessedAt,omitempty" firestore:"lastAccessedAt,omitempty"`
    ViewCount int       `json:"viewCount,omitempty" firestore:"viewCount,omitempty"`
}

type ShareTokensRepository struct{ col *firestore.CollectionRef }

func NewShareTokensRepository(db *firestore.Client) *ShareTokensRepository {
    return &ShareTokensRepository{col: db.Collection("core_share_tokens")}
}

func (r *ShareTokensRepository) Create(ctx context.Context, st *ShareToken) (string, error) {
    doc := r.col.NewDoc()
    st.ID = doc.ID
    if st.CreatedAt.IsZero() {
        st.CreatedAt = time.Now().UTC()
    }
    st.IsActive = true
    _, err := doc.Set(ctx, st)
    if err != nil {
        return "", err
    }
    return doc.ID, nil
}

func (r *ShareTokensRepository) GetByToken(ctx context.Context, token string) (*ShareToken, string, error) {
    q := r.col.Where("token", "==", token).Limit(1)
    docs, err := q.Documents(ctx).GetAll()
    if err != nil || len(docs) == 0 {
        return nil, "", err
    }
    var st ShareToken
    if err := docs[0].DataTo(&st); err != nil {
        return nil, "", err
    }
    return &st, docs[0].Ref.ID, nil
}

func (r *ShareTokensRepository) Revoke(ctx context.Context, id string) error {
    if id == "" { return ErrInvalidDocument }
    _, err := r.col.Doc(id).Set(ctx, map[string]any{"isActive": false}, firestore.MergeAll)
    return err
}

func (r *ShareTokensRepository) TouchAccess(ctx context.Context, id string) error {
    now := time.Now().UTC()
    _, err := r.col.Doc(id).Set(ctx, map[string]any{"lastAccessedAt": now, "viewCount": firestore.Increment(1)}, firestore.MergeAll)
    return err
}


