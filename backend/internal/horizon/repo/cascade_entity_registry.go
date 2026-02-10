package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CascadeEntityRecord stores best-effort entity identity mapping within a single cascade.
// Collection: horizon_cascade_entities
// Document ID is derived from (tenantId + cascadeId + identityKey).
type CascadeEntityRecord struct {
	TenantID      string    `firestore:"tenantId"`
	ProjectID     string    `firestore:"projectId,omitempty"`
	LensID        string    `firestore:"lensId,omitempty"`
	CascadeID     string    `firestore:"cascadeId"`
	IdentityKey   string    `firestore:"identityKey"`
	CanonicalPath string    `firestore:"canonicalPath,omitempty"`
	FragmentID    string    `firestore:"fragmentId,omitempty"`
	FirstSeenAt   time.Time `firestore:"firstSeenAt"`
	LastSeenAt    time.Time `firestore:"lastSeenAt"`
	SeenCount     int       `firestore:"seenCount"`
	CreatedByJob  string    `firestore:"createdByJob,omitempty"`
	LastJobID     string    `firestore:"lastJobId,omitempty"`
}

type CascadeEntityRegistryRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

func NewCascadeEntityRegistryRepository(db *firestore.Client) *CascadeEntityRegistryRepository {
	if db == nil {
		return &CascadeEntityRegistryRepository{}
	}
	return &CascadeEntityRegistryRepository{
		col: db.Collection("horizon_cascade_entities"),
		db:  db,
	}
}

func (r *CascadeEntityRegistryRepository) docID(tenantID, cascadeID, identityKey string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(cascadeID) + "|" + strings.TrimSpace(identityKey)))
	h := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s_%s_%s", strings.TrimSpace(tenantID), strings.TrimSpace(cascadeID), h)
}

func (r *CascadeEntityRegistryRepository) Get(ctx context.Context, tenantID, cascadeID, identityKey string) (*CascadeEntityRecord, error) {
	if r == nil || r.col == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	cascadeID = strings.TrimSpace(cascadeID)
	identityKey = strings.TrimSpace(identityKey)
	if tenantID == "" || cascadeID == "" || identityKey == "" {
		return nil, nil
	}
	doc, err := r.col.Doc(r.docID(tenantID, cascadeID, identityKey)).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var out CascadeEntityRecord
	if err := doc.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Touch records an observation for identityKey in this cascade.
// It does not overwrite bound fragmentId once set; use BindFragment for binding.
func (r *CascadeEntityRegistryRepository) Touch(
	ctx context.Context,
	tenantID string,
	projectID string,
	lensID string,
	cascadeID string,
	identityKey string,
	canonicalPath string,
	jobID string,
) (*CascadeEntityRecord, error) {
	if r == nil || r.col == nil || r.db == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	cascadeID = strings.TrimSpace(cascadeID)
	identityKey = strings.TrimSpace(identityKey)
	canonicalPath = strings.TrimSpace(canonicalPath)
	jobID = strings.TrimSpace(jobID)
	if tenantID == "" || cascadeID == "" || identityKey == "" {
		return nil, nil
	}

	doc := r.col.Doc(r.docID(tenantID, cascadeID, identityKey))
	now := time.Now().UTC()
	var out *CascadeEntityRecord
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		var cur CascadeEntityRecord
		snap, err := tx.Get(doc)
		if err != nil {
			if status.Code(err) != codes.NotFound {
				return err
			}
		} else if err := snap.DataTo(&cur); err != nil {
			return err
		}
		if cur.FirstSeenAt.IsZero() {
			cur.FirstSeenAt = now
		}
		cur.LastSeenAt = now
		cur.SeenCount++
		cur.TenantID = tenantID
		cur.ProjectID = projectID
		cur.LensID = lensID
		cur.CascadeID = cascadeID
		cur.IdentityKey = identityKey
		if cur.CanonicalPath == "" && canonicalPath != "" {
			cur.CanonicalPath = canonicalPath
		}
		if cur.CreatedByJob == "" && jobID != "" {
			cur.CreatedByJob = jobID
		}
		if jobID != "" {
			cur.LastJobID = jobID
		}
		out = &cur
		return tx.Set(doc, cur)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BindFragment binds identityKey to a specific fragment once we resolved or created it.
// First successful fragment binding wins; subsequent conflicting binds do not overwrite it.
func (r *CascadeEntityRegistryRepository) BindFragment(
	ctx context.Context,
	tenantID string,
	projectID string,
	lensID string,
	cascadeID string,
	identityKey string,
	fragmentID string,
	canonicalPath string,
	jobID string,
) (*CascadeEntityRecord, error) {
	if r == nil || r.col == nil || r.db == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	cascadeID = strings.TrimSpace(cascadeID)
	identityKey = strings.TrimSpace(identityKey)
	fragmentID = strings.TrimSpace(fragmentID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	jobID = strings.TrimSpace(jobID)
	if tenantID == "" || cascadeID == "" || identityKey == "" {
		return nil, nil
	}

	doc := r.col.Doc(r.docID(tenantID, cascadeID, identityKey))
	now := time.Now().UTC()
	var out *CascadeEntityRecord
	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		var cur CascadeEntityRecord
		snap, err := tx.Get(doc)
		if err != nil {
			if status.Code(err) != codes.NotFound {
				return err
			}
		} else if err := snap.DataTo(&cur); err != nil {
			return err
		}
		if cur.FirstSeenAt.IsZero() {
			cur.FirstSeenAt = now
		}
		cur.LastSeenAt = now
		cur.SeenCount++
		cur.TenantID = tenantID
		cur.ProjectID = projectID
		cur.LensID = lensID
		cur.CascadeID = cascadeID
		cur.IdentityKey = identityKey
		if cur.CreatedByJob == "" && jobID != "" {
			cur.CreatedByJob = jobID
		}
		if jobID != "" {
			cur.LastJobID = jobID
		}
		if cur.FragmentID == "" && fragmentID != "" {
			cur.FragmentID = fragmentID
		}
		if cur.CanonicalPath == "" && canonicalPath != "" {
			cur.CanonicalPath = canonicalPath
		}
		out = &cur
		return tx.Set(doc, cur)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
