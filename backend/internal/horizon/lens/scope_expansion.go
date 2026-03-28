package lens

import (
	"context"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ScopeExpansionRequest is the single parametric user-facing contract for explicit lens scope growth.
// It only controls which explicit canonical paths enter the lens scope; graph enrichment stays separate.
type ScopeExpansionRequest struct {
	LensID         string
	CanonicalPaths []string
	MaxAdds        int
	Reason         string
}

// ScopeExpansionResult reports what explicit scope growth was requested and what actually entered the lens.
type ScopeExpansionResult struct {
	RequestedPaths []string
	AppliedPaths   []string
	AlreadyPresent []string
	ScopeChanged   bool
}

func normalizeScopeExpansionPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func planScopeExpansion(existingScope []string, requested []string, maxAdds int) (apply []string, already []string) {
	existing := make(map[string]struct{}, len(existingScope))
	for _, raw := range existingScope {
		if path := strings.TrimSpace(raw); path != "" {
			existing[path] = struct{}{}
		}
	}
	requested = normalizeScopeExpansionPaths(requested)
	if maxAdds > 0 && len(requested) > maxAdds {
		requested = requested[:maxAdds]
	}
	apply = make([]string, 0, len(requested))
	already = make([]string, 0, len(requested))
	for _, path := range requested {
		if _, ok := existing[path]; ok {
			already = append(already, path)
			continue
		}
		apply = append(apply, path)
	}
	return apply, already
}

// ExpandScope is the single parametric path for explicit user-driven lens scope expansion.
// It persists only the requested canonical paths and never piggybacks graph-enrichment side effects.
func (s *LensService) ExpandScope(ctx context.Context, lens *models.Lens, req ScopeExpansionRequest) (ScopeExpansionResult, error) {
	var out ScopeExpansionResult
	if s == nil || s.lensesRepo == nil {
		return out, nil
	}
	lensID := strings.TrimSpace(req.LensID)
	if lens != nil && lensID == "" {
		lensID = strings.TrimSpace(lens.ID)
	}
	if lensID == "" {
		return out, nil
	}
	if lens == nil || strings.TrimSpace(lens.ID) != lensID {
		loaded, err := s.lensesRepo.Get(ctx, lensID)
		if err != nil {
			return out, err
		}
		lens = loaded
	}
	requested := normalizeScopeExpansionPaths(req.CanonicalPaths)
	if req.MaxAdds > 0 && len(requested) > req.MaxAdds {
		requested = requested[:req.MaxAdds]
	}
	out.RequestedPaths = append([]string(nil), requested...)
	toApply, already := planScopeExpansion(lens.Scope, requested, 0)
	out.AlreadyPresent = already
	if len(toApply) == 0 {
		return out, nil
	}
	if err := s.lensesRepo.AddScopeCanonicalPaths(ctx, lensID, toApply); err != nil {
		return out, err
	}
	if lens != nil {
		lens.Scope = append(lens.Scope, toApply...)
		lens.UpdatedAt = time.Now().UTC()
	}
	out.AppliedPaths = toApply
	out.ScopeChanged = len(toApply) > 0
	return out, nil
}
