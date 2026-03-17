package lens

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestPreserveScopeAutoAddCounters(t *testing.T) {
	lens := &models.Lens{
		ViewConfig: map[string]interface{}{
			models.ScopeAutoAddEntityCounterKey: 1,
		},
	}
	oldLens := &models.Lens{
		ViewConfig: map[string]interface{}{
			models.ScopeAutoAddEntityCounterKey:   4,
			models.ScopeAutoAddArtifactCounterKey: 2,
		},
	}

	preserveScopeAutoAddCounters(lens, oldLens)

	if got := lens.ViewConfig[models.ScopeAutoAddEntityCounterKey]; got != 4 {
		t.Fatalf("entity counter = %v, want 4", got)
	}
	if got := lens.ViewConfig[models.ScopeAutoAddArtifactCounterKey]; got != 2 {
		t.Fatalf("artifact counter = %v, want 2", got)
	}
}
