package policy

import (
	"context"
	"fmt"
	"strings"
	"time"

	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HumanInTheLoopPolicy groups "curation/moderation" checks & conventions.
//
// It implements the Policy interface so it can be registered in the PolicyRegistry
// alongside other policies and be discoverable/auditable via the same contract.
//
// NOTE: This policy is intentionally lightweight and may be used from scattered places
// (HTTP handlers, background jobs, UI-assisted moderation) while still sharing a common name.
type HumanInTheLoopPolicy struct {
	UsersRepo *corerepo.UsersRepository
}

// Name returns the policy identifier.
func (p *HumanInTheLoopPolicy) Name() string {
	return "human_in_the_loop"
}

// Decide checks whether the caller (identified via context["tenantId"] + context["uid"])
// is an admin and therefore allowed to perform the requested moderation action.
// Returns action "allow" if admin, "deny" otherwise.
func (p *HumanInTheLoopPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	start := time.Now()

	tenantID, _ := input.Context["tenantId"].(string)
	uid, _ := input.Context["uid"].(string)

	isAdmin := p.IsAdmin(ctx, tenantID, uid)

	action := "deny"
	confidence := 1.0
	if isAdmin {
		action = "allow"
	}

	return PolicyDecision{
		Action:       action,
		Params:       map[string]interface{}{"isAdmin": isAdmin, "tenantId": tenantID, "uid": uid},
		DecisionPath: "rule:admin_check",
		Confidence:   confidence,
		LatencyMs:    int(time.Since(start).Milliseconds()),
	}, nil
}

// Explain returns a human-readable explanation for a HumanInTheLoopPolicy decision.
func (p *HumanInTheLoopPolicy) Explain(decision PolicyDecision) string {
	isAdmin, _ := decision.Params["isAdmin"].(bool)
	uid, _ := decision.Params["uid"].(string)
	if isAdmin {
		return fmt.Sprintf("user %s is admin, action allowed", uid)
	}
	return fmt.Sprintf("user %s is not admin, action denied", uid)
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

