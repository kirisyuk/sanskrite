package scout

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Reason codes for contract errors (non-retryable).
const (
	ReasonCodeTypeRequired          = "type_required"
	ReasonCodeGovernanceOwner       = "governance_owner"
	ReasonCodeCanonicalPathRequired = "canonical_path_required"
	ReasonCodeTenantID              = "tenant_id"
	ReasonCodeValidation            = "validation_error"
)

// NormalizeResult holds the outcome of ContractNormalizer.Normalize.
type NormalizeResult struct {
	Snapshot   *models.HorizonFragment // Ready for persist; nil if Error != nil
	Error      error
	Retryable  bool   // true = transient (retry may help), false = contract error (retry won't help)
	ReasonCode string // For contract errors: type_required, governance_owner, etc.
}

// PreCreateValidatorInterface is the interface used by pre-create validation (policy, dedup).
// *PreCreateValidator implements this.
type PreCreateValidatorInterface interface {
	ValidateAndProcess(ctx context.Context, fragment *models.HorizonFragment) (*ValidationResult, error)
}

// ContractNormalizer applies the persist contract to base + patch fragments.
// It ensures Type, Governance.Owner, TenantID, CanonicalPath and runs validation.
// All entity pipeline materialization paths must go through this before fragmentsRepo.Create.
type ContractNormalizer struct {
	// PreCreateValidator is optional; when set, runs policy/dedup checks after normalization.
	PreCreateValidator PreCreateValidatorInterface
}

// Normalize merges patch into base (if patch non-nil), applies invariants, and validates.
// Returns a valid snapshot ready for persist, or an error with Retryable and ReasonCode set.
func (n *ContractNormalizer) Normalize(ctx context.Context, base *models.HorizonFragment, patch *models.FragmentPatch, effectivePath string) *NormalizeResult {
	if base == nil {
		return &NormalizeResult{
			Error:      fmt.Errorf("base fragment is nil"),
			Retryable:  false,
			ReasonCode: ReasonCodeValidation,
		}
	}
	effectivePath = strings.TrimSpace(effectivePath)

	// Merge patch into base if provided
	if patch != nil {
		models.ApplyPatchToFragment(base, patch, effectivePath)
	}

	// Apply required invariants
	if strings.TrimSpace(base.CanonicalPath) == "" && effectivePath != "" {
		base.CanonicalPath = effectivePath
	}
	if base.Class == "" {
		inferred := InferFragmentTypeFromCanonicalPath(strings.TrimSpace(base.CanonicalPath))
		if inferred == "" {
			inferred = FragmentTypeOrganization
		}
		base.Class = inferred
	}
	if strings.TrimSpace(base.TenantID) == "" {
		base.TenantID = "global"
	}
	if strings.TrimSpace(base.Governance.Owner) == "" {
		base.Governance.Owner = "SYSTEM"
	}
	if strings.TrimSpace(base.Governance.Status) == "" {
		base.Governance.Status = string(models.FragmentStatusActive)
	}
	if base.State == nil {
		base.State = make(map[string]models.MetricValue)
	}

	// Run repo-level preparation (canonical path normalization, type reconciliation)
	repo.PrepareFragmentForValidation(base)

	// Pre-create validation (optional)
	if n.PreCreateValidator != nil {
		pre, err := n.PreCreateValidator.ValidateAndProcess(ctx, base)
		if err != nil {
			return &NormalizeResult{
				Error:      fmt.Errorf("pre-create validation failed: %w", err),
				Retryable:  isRetryableError(err),
				ReasonCode: classifyErrorReason(err),
			}
		}
		if pre != nil && !pre.IsValid && strings.TrimSpace(pre.BlockReason) != "" {
			blockErr := fmt.Errorf("fragment creation blocked: %s", pre.BlockReason)
			return &NormalizeResult{
				Error:      blockErr,
				Retryable:  false,
				ReasonCode: classifyErrorReason(blockErr),
			}
		}
		if pre != nil && pre.ReplacementFrag != nil && strings.TrimSpace(pre.ReplacementFrag.ID) != "" {
			return &NormalizeResult{
				Snapshot:   pre.ReplacementFrag,
				Retryable:  false,
				ReasonCode: "",
			}
		}
	}

	// Validate
	if err := base.Validate(); err != nil {
		return &NormalizeResult{
			Error:      err,
			Retryable:  false,
			ReasonCode: classifyErrorReason(err),
		}
	}

	return &NormalizeResult{
		Snapshot:   base,
		Retryable:  false,
		ReasonCode: "",
	}
}

// NormalizeFromFragment is a convenience for when we have a full HorizonFragment (e.g. from extraction)
// without a separate patch. Applies invariants and validation.
func (n *ContractNormalizer) NormalizeFromFragment(ctx context.Context, frag *models.HorizonFragment, effectivePath string) *NormalizeResult {
	return n.Normalize(ctx, frag, nil, effectivePath)
}

// classifyErrorReason maps validation errors to reason codes.
func classifyErrorReason(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "type is required") {
		return ReasonCodeTypeRequired
	}
	if strings.Contains(msg, "governance.owner") && strings.Contains(msg, "system") {
		return ReasonCodeGovernanceOwner
	}
	if strings.Contains(msg, "canonicalpath") || strings.Contains(msg, "canonical path") {
		return ReasonCodeCanonicalPathRequired
	}
	if strings.Contains(msg, "tenantid") || strings.Contains(msg, "tenant id") {
		return ReasonCodeTenantID
	}
	return ReasonCodeValidation
}

// isRetryableError returns true for transient errors that may succeed on retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Firestore/GRPC transient errors
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
			return true
		case codes.AlreadyExists:
			return false
		}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "validation error") {
		return false
	}
	if strings.Contains(msg, "type is required") || strings.Contains(msg, "governance.owner") {
		return false
	}
	if strings.Contains(msg, "fragment creation blocked") {
		return false
	}
	return true
}
