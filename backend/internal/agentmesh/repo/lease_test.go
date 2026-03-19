package repo

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

func TestCanClaimLease(t *testing.T) {
	now := time.Now().UTC()
	if !CanClaimLease(contracts.TaskStatusPending, contracts.Lease{}, now) {
		t.Fatalf("empty lease should be claimable")
	}
	if CanClaimLease(contracts.TaskStatusSucceeded, contracts.Lease{}, now) {
		t.Fatalf("completed task should not be claimable")
	}
	if CanClaimLease(contracts.TaskStatusAwaitingReview, contracts.Lease{}, now) {
		t.Fatalf("awaiting_review task should not be claimable")
	}
	if CanClaimLease(contracts.TaskStatusDegraded, contracts.Lease{}, now) {
		t.Fatalf("degraded task should not be claimable")
	}
	if CanClaimLease(contracts.TaskStatusRunning, contracts.Lease{
		Owner:     "worker-1",
		ExpiresAt: now.Add(time.Minute),
	}, now) {
		t.Fatalf("active lease should not be claimable")
	}
	if !CanClaimLease(contracts.TaskStatusRunning, contracts.Lease{
		Owner:     "worker-1",
		ExpiresAt: now.Add(-time.Minute),
	}, now) {
		t.Fatalf("expired lease should be claimable")
	}
}

func TestNewLease(t *testing.T) {
	now := time.Now().UTC()
	lease := NewLease(contracts.Lease{Attempt: 2, DedupKey: "d1"}, "worker-2", now, 90*time.Second)
	if lease.Owner != "worker-2" {
		t.Fatalf("owner=%q", lease.Owner)
	}
	if lease.Attempt != 3 {
		t.Fatalf("attempt=%d", lease.Attempt)
	}
	if !lease.ExpiresAt.After(now) {
		t.Fatalf("expiresAt=%v", lease.ExpiresAt)
	}
	if lease.DedupKey != "d1" {
		t.Fatalf("dedupKey=%q", lease.DedupKey)
	}
}
