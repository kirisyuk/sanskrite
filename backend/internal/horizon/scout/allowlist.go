package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// EdgeAllowlistByDomainMode returns the list of dependency types allowed for write/filter for the given domain mode.
// For general mode returns nil (no restriction: all types allowed).
// Per TZ §3.4.
func EdgeAllowlistByDomainMode(mode string) []models.DependencyType {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case taxonomy.DomainModeSupplyChain:
		return []models.DependencyType{
			models.DependencyTypeDependsOn,
			models.DependencyTypeProduces,
			models.DependencyTypeConsumes,
			models.DependencyTypeSuppliesTo,
			models.DependencyTypeTransformsInto,
			models.DependencyTypeExtractsFrom,
			models.DependencyTypeLocatedIn,
			models.DependencyTypeOperatesIn,
		}
	case taxonomy.DomainModeGovernance:
		return []models.DependencyType{
			models.DependencyTypeOwns,
			models.DependencyTypeSubsidiaryOf,
			models.DependencyTypeControls,
			models.DependencyTypeManages,
			models.DependencyTypeInvestsIn,
			models.DependencyTypeBoardMemberOf,
			models.DependencyTypeEmployedBy,
			models.DependencyTypeFounded,
			models.DependencyTypeAdministers,
		}
	case taxonomy.DomainModeAssurance:
		return []models.DependencyType{
			models.DependencyTypeAssures,
			models.DependencyTypeAuditedBy,
			models.DependencyTypeRegulates,
		}
	case taxonomy.DomainModeMarket:
		return []models.DependencyType{
			models.DependencyTypeParticipatesIn,
			models.DependencyTypeHasMarketSegment,
		}
	case taxonomy.DomainModeStrategic:
		return []models.DependencyType{
			models.DependencyTypeControlsAccess,
			models.DependencyTypeImposesCondition,
			models.DependencyTypeGatekeepsDecision,
			models.DependencyTypeUsesMechanism,
			models.DependencyTypeAppliesTo,
		}
	case taxonomy.DomainModePolicyImpact:
		return []models.DependencyType{
			models.DependencyTypeRegulates,
			models.DependencyTypeControlsAccess,
			models.DependencyTypeImposesCondition,
			models.DependencyTypeUsesMechanism,
			models.DependencyTypeAppliesTo,
			models.DependencyTypeInfluences,
			models.DependencyTypeDependsOn,
			models.DependencyTypeParticipatesIn,
			models.DependencyTypeLocatedIn,
			models.DependencyTypeOperatesIn,
			models.DependencyTypeAdministers,
		}
	case taxonomy.DomainModeGeneral:
		return nil // full set: no filter
	default:
		return nil
	}
}

// EdgeAllowlistByDomainModes returns the union of allowlists for the given modes (deduplicated).
// If any mode is general or the list is empty, returns nil (no restriction).
func EdgeAllowlistByDomainModes(modes []string) []models.DependencyType {
	if len(modes) == 0 {
		return nil
	}
	seen := make(map[models.DependencyType]struct{})
	for _, mode := range modes {
		mode = strings.TrimSpace(strings.ToLower(mode))
		if mode == taxonomy.DomainModeGeneral {
			return nil
		}
		for _, dt := range EdgeAllowlistByDomainMode(mode) {
			seen[dt] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]models.DependencyType, 0, len(seen))
	for dt := range seen {
		out = append(out, dt)
	}
	return out
}

// AllowlistContains returns true if the allowlist is nil (allow all) or contains the given type.
func AllowlistContains(allowlist []models.DependencyType, t models.DependencyType) bool {
	if allowlist == nil {
		return true
	}
	for _, a := range allowlist {
		if a == t {
			return true
		}
	}
	return false
}
