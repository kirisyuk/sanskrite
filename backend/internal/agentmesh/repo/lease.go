package repo

import (
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

func CanClaimLease(status contracts.TaskStatus, lease contracts.Lease, now time.Time) bool {
	switch status {
	case contracts.TaskStatusSucceeded, contracts.TaskStatusFailed, contracts.TaskStatusCancelled, contracts.TaskStatusAwaitingReview, contracts.TaskStatusDegraded:
		return false
	}
	if lease.Owner == "" {
		return true
	}
	if lease.ExpiresAt.IsZero() {
		return true
	}
	return !lease.ExpiresAt.After(now)
}

func NewLease(prev contracts.Lease, owner string, now time.Time, leaseDuration time.Duration) contracts.Lease {
	prev.Owner = owner
	prev.Attempt++
	prev.HeartbeatAt = now
	prev.ExpiresAt = now.Add(leaseDuration)
	return prev
}
