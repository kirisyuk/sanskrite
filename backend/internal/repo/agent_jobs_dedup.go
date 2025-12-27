package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// CreateDedup creates a new AgentJob with a transactional deduplication lock.
//
// If a job already exists for the provided lockKey and is still active (pending/running),
// this returns the existing job ID with created=false.
//
// Locking is implemented via Firestore transaction over a dedicated collection:
// "agent_job_dedup_locks/{sha256(lockKey)}".
//
// Notes:
// - This is intentionally generic and does not assume any particular job schema beyond AgentJob.
// - lockKey should be deterministic and stable for the idempotency scope you want.
func (r *AgentJobsRepository) CreateDedup(ctx context.Context, job *models.AgentJob, lockKey string) (jobID string, created bool, err error) {
	if r == nil || r.col == nil || r.db == nil {
		return "", false, ErrInvalidAgentJob
	}
	if job == nil || strings.TrimSpace(job.TenantID) == "" || strings.TrimSpace(job.ProjectID) == "" || strings.TrimSpace(job.AgentID) == "" {
		return "", false, ErrInvalidAgentJob
	}
	lockKey = strings.TrimSpace(lockKey)
	if lockKey == "" {
		id, err := r.Create(ctx, job)
		return id, true, err
	}

	// Normalize base AgentJob fields (same as Create).
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	if job.Status == "" {
		job.Status = models.AgentJobStatusPending
	}
	if job.Steps == nil {
		job.Steps = []models.AgentStep{}
	}

	// Pre-generate a stable ID for the transaction.
	newRef := r.col.NewDoc()
	newID := newRef.ID

	lockID := sha256Hex(lockKey)
	lockRef := r.db.Collection("agent_job_dedup_locks").Doc(lockID)

	var reusedID string

	txnErr := r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// 1) Read lock
		lockSnap, lerr := tx.Get(lockRef)
		if lerr == nil && lockSnap.Exists() {
			var lock struct {
				JobID string `firestore:"jobId"`
			}
			_ = lockSnap.DataTo(&lock)
			candidate := strings.TrimSpace(lock.JobID)
			if candidate != "" {
				// 2) Read candidate job and reuse if active
				jobSnap, jerr := tx.Get(r.col.Doc(candidate))
				if jerr == nil && jobSnap.Exists() {
					var existing models.AgentJob
					if derr := jobSnap.DataTo(&existing); derr == nil {
						if existing.Status == models.AgentJobStatusPending || existing.Status == models.AgentJobStatusRunning {
							reusedID = candidate
							// Touch lock (helps debugging/monitoring).
							_ = tx.Set(lockRef, map[string]interface{}{
								"jobId":      candidate,
								"updatedAt":  now,
								"tenantId":   strings.TrimSpace(job.TenantID),
								"projectId":  strings.TrimSpace(job.ProjectID),
								"agentId":    strings.TrimSpace(job.AgentID),
								"lockKey":    lockKey,
								"lockKeySha": lockID,
							}, firestore.MergeAll)
							return nil
						}
					}
				}
				// If candidate job missing or inactive, we will create a new job and overwrite the lock.
			}
		} else if lerr != nil && status.Code(lerr) != codes.NotFound {
			return lerr
		}

		// 3) Create new job
		j := *job
		j.ID = newID
		if err := tx.Set(newRef, &j); err != nil {
			return err
		}
		// 4) Upsert lock
		if err := tx.Set(lockRef, map[string]interface{}{
			"jobId":      newID,
			"createdAt":  now,
			"updatedAt":  now,
			"tenantId":   strings.TrimSpace(job.TenantID),
			"projectId":  strings.TrimSpace(job.ProjectID),
			"agentId":    strings.TrimSpace(job.AgentID),
			"lockKey":    lockKey,
			"lockKeySha": lockID,
		}, firestore.MergeAll); err != nil {
			return err
		}
		return nil
	})
	if txnErr != nil {
		return "", false, fmt.Errorf("CreateDedup transaction failed: %w", txnErr)
	}

	if strings.TrimSpace(reusedID) != "" {
		job.ID = reusedID
		return reusedID, false, nil
	}
	job.ID = newID
	return newID, true, nil
}

// MergeStateStringArray merges string values into a state.{key} array field using Firestore ArrayUnion.
// This is safe under concurrent writers and avoids overwriting the full state map.
func (r *AgentJobsRepository) MergeStateStringArray(ctx context.Context, jobID string, key string, values []string) error {
	if r == nil || r.col == nil {
		return ErrInvalidAgentJob
	}
	jobID = strings.TrimSpace(jobID)
	key = strings.TrimSpace(key)
	if jobID == "" || key == "" {
		return ErrInvalidAgentJob
	}
	if len(values) == 0 {
		return nil
	}
	args := make([]interface{}, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		args = append(args, v)
	}
	if len(args) == 0 {
		return nil
	}
	updates := []firestore.Update{
		{Path: "state." + key, Value: firestore.ArrayUnion(args...)},
		{Path: "updatedAt", Value: time.Now().UTC()},
	}
	_, err := r.col.Doc(jobID).Update(ctx, updates)
	return err
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}


