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

// CanonicalPathRedirect stores a canonical path rewrite observed by the pipeline.
// Collection: horizon_canonical_path_redirects
// Document ID is derived from (tenantId + sourcePath) to avoid slash issues.
type CanonicalPathRedirect struct {
	TenantID     string    `firestore:"tenantId"`
	SourcePath   string    `firestore:"sourcePath"`
	TargetPath   string    `firestore:"targetPath"`
	Reason       string    `firestore:"reason,omitempty"`
	FragmentType string    `firestore:"fragmentType,omitempty"`
	FirstSeenAt  time.Time `firestore:"firstSeenAt"`
	LastSeenAt   time.Time `firestore:"lastSeenAt"`
	SeenCount    int       `firestore:"seenCount"`
	LastTarget   string    `firestore:"lastTarget,omitempty"`
	LastTargetAt time.Time `firestore:"lastTargetAt,omitempty"`
	StableStreak int       `firestore:"stableStreak,omitempty"`
	Variability  int       `firestore:"variability,omitempty"`
	StableTarget string    `firestore:"stableTarget,omitempty"`
	StableUntil  time.Time `firestore:"stableUntil,omitempty"`
}

// CanonicalPathRedirectsRepository manages canonical path rewrite mappings.
type CanonicalPathRedirectsRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

func NewCanonicalPathRedirectsRepository(db *firestore.Client) *CanonicalPathRedirectsRepository {
	if db == nil {
		return &CanonicalPathRedirectsRepository{}
	}
	return &CanonicalPathRedirectsRepository{
		col: db.Collection("horizon_canonical_path_redirects"),
		db:  db,
	}
}

func (r *CanonicalPathRedirectsRepository) docID(tenantID, sourcePath string) string {
	tenantID = strings.TrimSpace(tenantID)
	sourcePath = strings.TrimSpace(sourcePath)
	sum := sha1.Sum([]byte(tenantID + "|" + sourcePath))
	h := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s_%s", tenantID, h)
}

// Get returns a redirect for (tenantId, sourcePath). Returns nil if not found.
func (r *CanonicalPathRedirectsRepository) Get(ctx context.Context, tenantID, sourcePath string) (*CanonicalPathRedirect, error) {
	if r == nil || r.col == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	sourcePath = strings.TrimSpace(sourcePath)
	if tenantID == "" || sourcePath == "" {
		return nil, nil
	}
	doc, err := r.col.Doc(r.docID(tenantID, sourcePath)).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var out CanonicalPathRedirect
	if err := doc.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Upsert records (or updates) a canonical path redirect.
func (r *CanonicalPathRedirectsRepository) Upsert(
	ctx context.Context,
	tenantID string,
	sourcePath string,
	targetPath string,
	reason string,
	fragmentType string,
) error {
	if r == nil || r.col == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	reason = strings.TrimSpace(reason)
	fragmentType = strings.TrimSpace(fragmentType)
	if tenantID == "" || sourcePath == "" || targetPath == "" || sourcePath == targetPath {
		return nil
	}

	now := time.Now().UTC()
	doc := r.col.Doc(r.docID(tenantID, sourcePath))
	entry := CanonicalPathRedirect{
		TenantID:     tenantID,
		SourcePath:   sourcePath,
		TargetPath:   targetPath,
		Reason:       reason,
		FragmentType: fragmentType,
		FirstSeenAt:  now,
		LastSeenAt:   now,
		SeenCount:    1,
	}

	_, err := doc.Create(ctx, entry)
	if err == nil {
		return nil
	}
	if status.Code(err) != codes.AlreadyExists {
		return err
	}

	updates := []firestore.Update{
		{Path: "targetPath", Value: targetPath},
		{Path: "lastSeenAt", Value: now},
		{Path: "seenCount", Value: firestore.Increment(1)},
	}
	if reason != "" {
		updates = append(updates, firestore.Update{Path: "reason", Value: reason})
	}
	if fragmentType != "" {
		updates = append(updates, firestore.Update{Path: "fragmentType", Value: fragmentType})
	}
	_, err = doc.Update(ctx, updates)
	return err
}

// RecordObservation updates redirect stats and computes stability for warm-up.
// Returns the updated redirect, whether it became stable, and whether variability was detected.
func (r *CanonicalPathRedirectsRepository) RecordObservation(
	ctx context.Context,
	tenantID string,
	sourcePath string,
	targetPath string,
	reason string,
	fragmentType string,
	warmupThreshold int,
	stableTTL time.Duration,
) (*CanonicalPathRedirect, bool, bool, error) {
	if r == nil || r.col == nil || r.db == nil {
		return nil, false, false, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	reason = strings.TrimSpace(reason)
	fragmentType = strings.TrimSpace(fragmentType)
	if tenantID == "" || sourcePath == "" || targetPath == "" {
		return nil, false, false, nil
	}
	if warmupThreshold <= 0 {
		warmupThreshold = 3
	}
	if stableTTL <= 0 {
		stableTTL = 7 * 24 * time.Hour
	}

	var updated *CanonicalPathRedirect
	var becameStable bool
	var variability bool
	doc := r.col.Doc(r.docID(tenantID, sourcePath))
	now := time.Now().UTC()

	err := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(doc)
		var current CanonicalPathRedirect
		if err != nil {
			if status.Code(err) != codes.NotFound {
				return err
			}
		} else if err := snap.DataTo(&current); err != nil {
			return err
		}

		if current.FirstSeenAt.IsZero() {
			current.FirstSeenAt = now
		}
		current.LastSeenAt = now
		current.SeenCount++
		current.TenantID = tenantID
		current.SourcePath = sourcePath
		current.TargetPath = targetPath
		if reason != "" {
			current.Reason = reason
		}
		if fragmentType != "" {
			current.FragmentType = fragmentType
		}

		prevLastTarget := strings.TrimSpace(current.LastTarget)
		if prevLastTarget != "" && prevLastTarget != targetPath {
			variability = true
			current.Variability++
			current.StableTarget = ""
			current.StableUntil = time.Time{}
		}

		if prevLastTarget != "" && prevLastTarget == targetPath {
			current.StableStreak++
		} else {
			current.StableStreak = 1
		}
		current.LastTarget = targetPath
		current.LastTargetAt = now

		if current.StableStreak >= warmupThreshold {
			if current.StableTarget != targetPath || current.StableUntil.Before(now) {
				becameStable = true
			}
			current.StableTarget = targetPath
			current.StableUntil = now.Add(stableTTL)
		}

		updated = &current
		return tx.Set(doc, current)
	})
	if err != nil {
		return nil, false, false, err
	}
	return updated, becameStable, variability, nil
}
