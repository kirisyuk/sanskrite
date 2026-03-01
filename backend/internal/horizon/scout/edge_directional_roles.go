package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// withDeterministicDirectionalRoleQualifiers fills direction-role qualifiers only for
// relation/type combinations where business direction is deterministic by contract.
// It also repairs partial/invalid directional role payloads into canonical values.
func withDeterministicDirectionalRoleQualifiers(
	edgeType models.DependencyType,
	sourceType, targetType models.FragmentType,
	qualifiers map[string]interface{},
) (map[string]interface{}, bool) {
	if !requiresDirectionalRoleAutofill(edgeType, sourceType, targetType) {
		return qualifiers, false
	}

	sourceRole, targetRole, directionBasis, ok := deterministicDirectionalRoleContract(edgeType)
	if !ok {
		return qualifiers, false
	}

	currentSourceRole := readDirectionalQualifier(qualifiers, "source_role", "sourceRole", "subject_role", "subjectRole")
	currentTargetRole := readDirectionalQualifier(qualifiers, "target_role", "targetRole", "object_role", "objectRole")
	currentDirectionBasis := readDirectionalQualifier(qualifiers, "direction_basis", "directionBasis", "relation_direction", "relationDirection")

	changed := false
	out := cloneQualifiersMap(qualifiers)
	if canonical, ok := canonicalDirectionalSourceRole(edgeType, currentSourceRole); !ok || canonical == "" {
		out["source_role"] = sourceRole
		changed = true
	} else if canonical != currentSourceRole {
		out["source_role"] = canonical
		changed = true
	}
	if canonical, ok := canonicalDirectionalTargetRole(edgeType, currentTargetRole); !ok || canonical == "" {
		out["target_role"] = targetRole
		changed = true
	} else if canonical != currentTargetRole {
		out["target_role"] = canonical
		changed = true
	}
	if canonical, ok := canonicalDirectionalBasis(edgeType, currentDirectionBasis); !ok || canonical == "" {
		out["direction_basis"] = directionBasis
		changed = true
	} else if canonical != currentDirectionBasis {
		out["direction_basis"] = canonical
		changed = true
	}
	if !changed {
		return qualifiers, false
	}
	return out, true
}

func requiresDirectionalRoleAutofill(
	edgeType models.DependencyType,
	sourceType, targetType models.FragmentType,
) bool {
	isOrg := func(t models.FragmentType) bool { return t == models.FragmentTypeOrganization }
	isFac := func(t models.FragmentType) bool { return t == models.FragmentTypeFacility }
	isProgram := func(t models.FragmentType) bool { return t == models.FragmentTypeProgram }
	isCommunity := func(t models.FragmentType) bool { return t == models.FragmentTypeCommunity }
	isPerson := func(t models.FragmentType) bool { return t == models.FragmentTypePerson }
	isActorLike := func(t models.FragmentType) bool {
		return isOrg(t) || isFac(t) || isProgram(t) || isCommunity(t) || isPerson(t)
	}

	switch edgeType {
	case models.DependencyTypeOwns,
		models.DependencyTypeSubsidiaryOf,
		models.DependencyTypeInvestsIn,
		models.DependencyTypeAssures,
		models.DependencyTypeControls:
		return isOrg(sourceType) && isOrg(targetType)
	case models.DependencyTypeDependsOn:
		return isActorLike(sourceType) && isActorLike(targetType)
	case models.DependencyTypeSuppliesTo:
		return (isOrg(sourceType) || isFac(sourceType)) && (isOrg(targetType) || isFac(targetType))
	default:
		return false
	}
}

func deterministicDirectionalRoleContract(edgeType models.DependencyType) (sourceRole, targetRole, directionBasis string, ok bool) {
	switch edgeType {
	case models.DependencyTypeOwns:
		return "owner", "owned_entity", "owner_to_owned", true
	case models.DependencyTypeSubsidiaryOf:
		return "subsidiary", "parent_owner", "subsidiary_to_parent", true
	case models.DependencyTypeInvestsIn:
		return "investor", "investee", "investor_to_investee", true
	case models.DependencyTypeAssures:
		return "assurer", "assuree", "assurer_to_assuree", true
	case models.DependencyTypeControls:
		return "controller", "controlled_entity", "controller_to_controlled", true
	case models.DependencyTypeDependsOn:
		return "dependent", "dependency", "dependent_to_dependency", true
	case models.DependencyTypeSuppliesTo:
		return "supplier", "customer", "supplier_to_customer", true
	default:
		return "", "", "", false
	}
}

func readDirectionalQualifier(m map[string]interface{}, keys ...string) string {
	if len(m) == 0 {
		return ""
	}
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		if s := strings.TrimSpace(rawString(raw)); s != "" {
			return strings.ToLower(s)
		}
	}
	return ""
}

func canonicalDirectionalSourceRole(edgeType models.DependencyType, role string) (string, bool) {
	return canonicalDirectionalRole(edgeType, role, true)
}

func canonicalDirectionalTargetRole(edgeType models.DependencyType, role string) (string, bool) {
	return canonicalDirectionalRole(edgeType, role, false)
}

func canonicalDirectionalRole(edgeType models.DependencyType, role string, source bool) (string, bool) {
	role = normalizedDirectionalRole(role)
	if role == "" {
		return "", false
	}
	switch edgeType {
	case models.DependencyTypeOwns:
		if source && inDirectionalRole(role, "owner", "parent_owner", "shareholder", "beneficial_owner") {
			return "owner", true
		}
		if !source && inDirectionalRole(role, "owned_entity", "subsidiary", "asset", "controlled_entity", "controlled") {
			return "owned_entity", true
		}
	case models.DependencyTypeSubsidiaryOf:
		if source && inDirectionalRole(role, "subsidiary", "owned_entity", "child_entity") {
			return "subsidiary", true
		}
		if !source && inDirectionalRole(role, "parent_owner", "owner", "parent") {
			return "parent_owner", true
		}
	case models.DependencyTypeInvestsIn:
		if source && inDirectionalRole(role, "investor", "shareholder") {
			return "investor", true
		}
		if !source && inDirectionalRole(role, "investee", "portfolio_company", "target", "owned_entity") {
			return "investee", true
		}
	case models.DependencyTypeAssures:
		if source && inDirectionalRole(role, "assurer", "auditor", "certifier", "inspector", "regulator") {
			return "assurer", true
		}
		if !source && inDirectionalRole(role, "assuree", "audited_entity", "certified_entity", "inspected_entity", "regulated_entity") {
			return "assuree", true
		}
	case models.DependencyTypeControls:
		if source && inDirectionalRole(role, "controller", "owner", "parent_owner") {
			return "controller", true
		}
		if !source && inDirectionalRole(role, "controlled_entity", "controlled", "owned_entity", "subsidiary") {
			return "controlled_entity", true
		}
	case models.DependencyTypeDependsOn:
		if source && inDirectionalRole(role, "dependent", "consumer", "requirer", "requester") {
			return "dependent", true
		}
		if !source && inDirectionalRole(role, "dependency", "supplier", "provider", "enabler", "input") {
			return "dependency", true
		}
	case models.DependencyTypeSuppliesTo:
		if source && inDirectionalRole(role, "supplier", "provider", "seller", "producer", "vendor") {
			return "supplier", true
		}
		if !source && inDirectionalRole(role, "customer", "buyer", "consumer", "recipient", "offtaker") {
			return "customer", true
		}
	}
	return "", false
}

func canonicalDirectionalBasis(edgeType models.DependencyType, direction string) (string, bool) {
	dir := normalizedDirectionalRole(direction)
	if dir == "" {
		return "", false
	}
	switch edgeType {
	case models.DependencyTypeOwns:
		if inDirectionalRole(dir, "owner_to_owned", "parent_to_subsidiary", "shareholder_to_company") {
			return "owner_to_owned", true
		}
	case models.DependencyTypeSubsidiaryOf:
		if inDirectionalRole(dir, "subsidiary_to_parent", "child_to_parent") {
			return "subsidiary_to_parent", true
		}
	case models.DependencyTypeInvestsIn:
		if inDirectionalRole(dir, "investor_to_investee", "investor_to_target") {
			return "investor_to_investee", true
		}
	case models.DependencyTypeAssures:
		if inDirectionalRole(dir, "assurer_to_assuree", "auditor_to_audited") {
			return "assurer_to_assuree", true
		}
	case models.DependencyTypeControls:
		if inDirectionalRole(dir, "controller_to_controlled", "owner_to_controlled") {
			return "controller_to_controlled", true
		}
	case models.DependencyTypeDependsOn:
		if inDirectionalRole(dir, "dependent_to_dependency", "consumer_to_supplier") {
			return "dependent_to_dependency", true
		}
	case models.DependencyTypeSuppliesTo:
		if inDirectionalRole(dir, "supplier_to_customer", "provider_to_recipient") {
			return "supplier_to_customer", true
		}
	}
	return "", false
}

func inDirectionalRole(role string, allowed ...string) bool {
	role = normalizedDirectionalRole(role)
	if role == "" {
		return false
	}
	for _, a := range allowed {
		if role == normalizedDirectionalRole(a) {
			return true
		}
	}
	return false
}

func normalizedDirectionalRole(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func rawString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return ""
	}
}

func cloneQualifiersMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// isRoleInvertedForDirection returns true when source_role/target_role in qualifiers
// indicate the edge direction is inverted (e.g. source=owned_entity, target=owner for OWNS).
// Used to swap source/target when the LLM outputs wrong direction but correct role semantics.
func isRoleInvertedForDirection(edgeType models.DependencyType, qualifiers map[string]interface{}) bool {
	if len(qualifiers) == 0 {
		return false
	}
	srcRole := readDirectionalQualifier(qualifiers, "source_role", "sourceRole", "subject_role", "subjectRole")
	tgtRole := readDirectionalQualifier(qualifiers, "target_role", "targetRole", "object_role", "objectRole")
	if srcRole == "" || tgtRole == "" {
		return false
	}
	// Correct: source has source-role, target has target-role.
	// Inverted: source has target-role (e.g. owned_entity), target has source-role (e.g. owner).
	_, srcHasTargetRole := canonicalDirectionalTargetRole(edgeType, srcRole)
	_, tgtHasSourceRole := canonicalDirectionalSourceRole(edgeType, tgtRole)
	return srcHasTargetRole && tgtHasSourceRole
}
