package horizon

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// collectNearbyDomainPaths returns domain paths from lens scope (domain is config; typically empty).
func (h *HorizonHandlers) collectNearbyDomainPaths(ctx context.Context, lens *models.Lens) []string {
	if lens == nil || len(lens.Scope) == 0 {
		return nil
	}
	out := make([]string, 0, len(lens.Scope))
	for _, p := range lens.Scope {
		p = strings.TrimSpace(p)
		if p != "" && strings.HasPrefix(p, "/domain/") {
			out = append(out, p)
		}
	}
	return out
}

// collectExistingDomainPathsForProject returns domain paths from project lenses (domain is config; typically empty).
func (h *HorizonHandlers) collectExistingDomainPathsForProject(ctx context.Context, tenantID, projectID string) []string {
	// Domain is config; no domain fragments. Return empty.
	return nil
}
