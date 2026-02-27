package scout

import (
	"testing"
	"time"
)

func TestPolicyPersistBackoffBounded(t *testing.T) {
	if got := policyPersistBackoff(1); got != 3*time.Second {
		t.Fatalf("attempt=1: got %s want 3s", got)
	}
	if got := policyPersistBackoff(2); got != 6*time.Second {
		t.Fatalf("attempt=2: got %s want 6s", got)
	}
	if got := policyPersistBackoff(6); got > 2*time.Minute {
		t.Fatalf("attempt=6: got %s exceeds cap", got)
	}
}
