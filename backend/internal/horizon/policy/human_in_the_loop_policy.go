package policy

import (
	"context"
	"strings"

	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HumanInTheLoopPolicy groups "curation/moderation" checks & conventions.
//
// NOTE: This policy is intentionally lightweight and may be used from scattered places
// (HTTP handlers, background jobs, UI-assisted moderation) while still sharing a common name.
type HumanInTheLoopPolicy struct {
	UsersRepo *corerepo.UsersRepository
}

// IsAdmin returns true if the (tenantID, uid) user exists and has role=admin.
// System calls are handled outside of this policy (by auth middleware).
func (p *HumanInTheLoopPolicy) IsAdmin(ctx context.Context, tenantID, uid string) bool {
	if p == nil || p.UsersRepo == nil {
		return false
	}
	tenantID = strings.TrimSpace(tenantID)
	uid = strings.TrimSpace(uid)
	if tenantID == "" || uid == "" {
		return false
	}
	u, err := p.UsersRepo.GetByIDAndTenant(ctx, uid, tenantID)
	if err != nil || u == nil || u.Role == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(*u.Role), "admin")
}



