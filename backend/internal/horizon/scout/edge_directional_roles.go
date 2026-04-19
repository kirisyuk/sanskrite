package scout

import (
	"strings"

)

// withDeterministicDirectionalRoleQualifiers fills direction-role qualifiers only for
// relation/type combinations where business direction is deterministic by contract.
// It also repairs partial/invalid directional role payloads into canonical values.
func withDeterministicDirectionalRoleQualifiers(
	edgeType DependencyType,
	sourceType, targetType FragmentType,
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
	edgeType DependencyType,
	sourceType, targetType FragmentType,
) bool {
	// MIGRATION NOTE (AH taxonomy): legacy FragmentTypeProgram and
	// FragmentTypeMediaChannel collapse onto (org, program) and
	// (org, media_channel). Class-only callers can no longer distinguish
	// them from a bare org; isProgram / isMedia therefore always return
	// false and the surrounding switches treat program-actors and
	// media-targets as plain orgs (they all match isOrg). This is a known
	// behavioural drift; the legacy program/media-specific code paths
	// are migrating to (Class, Subtype)-aware policy in P3+.
	isOrg := func(t FragmentType) bool { return t == FragmentTypeOrganization }
	isFac := func(t FragmentType) bool { return t == FragmentTypeFacility }
	isGeo := func(t FragmentType) bool { return t == FragmentTypeGeoRegion }
	isDecision := func(t FragmentType) bool { return t == FragmentTypeDecisionPoint }
	isMechanism := func(t FragmentType) bool { return t == FragmentTypeMechanism }
	isProgram := func(t FragmentType) bool { _ = t; return false }
	isMarket := func(t FragmentType) bool { return t == FragmentTypeMarket }
	isProduct := func(t FragmentType) bool { return t == FragmentTypeProduct }
	isCommunity := func(t FragmentType) bool { return t == FragmentTypeCommunity }
	isMedia := func(t FragmentType) bool { _ = t; return false }
	isPerson := func(t FragmentType) bool { return t == FragmentTypePerson }
	isActorLike := func(t FragmentType) bool {
		return isOrg(t) || isFac(t) || isProgram(t) || isCommunity(t) || isPerson(t)
	}
	isPolicyTarget := func(t FragmentType) bool {
		return isActorLike(t) || isMarket(t) || isProduct(t)
	}

	switch edgeType {
	case DependencyTypeOwns,
		DependencyTypeSubsidiaryOf,
		DependencyTypeInvestsIn,
		DependencyTypeAssures,
		DependencyTypeControls:
		return isOrg(sourceType) && isOrg(targetType)
	case DependencyTypeDependsOn:
		return isActorLike(sourceType) && isActorLike(targetType)
	case DependencyTypeSuppliesTo:
		return (isOrg(sourceType) || isFac(sourceType)) && (isOrg(targetType) || isFac(targetType))
	case DependencyTypeGatekeepsDecision:
		return (isOrg(sourceType) || isProgram(sourceType) || isCommunity(sourceType) || isPerson(sourceType)) && isDecision(targetType)
	case DependencyTypeUsesMechanism:
		return (isOrg(sourceType) || isProgram(sourceType) || isCommunity(sourceType) || isDecision(sourceType)) && isMechanism(targetType)
	case DependencyTypeControlsAccess:
		return (isOrg(sourceType) || isProgram(sourceType) || isCommunity(sourceType)) && isPolicyTarget(targetType)
	case DependencyTypeImposesCondition:
		return (isMechanism(sourceType) || isOrg(sourceType) || isProgram(sourceType) || isCommunity(sourceType)) && isPolicyTarget(targetType)
	case DependencyTypeAppliesTo:
		return isMechanism(sourceType) && isPolicyTarget(targetType)
	case DependencyTypeAdministers:
		return (isOrg(sourceType) || isProgram(sourceType)) && (isOrg(targetType) || isProgram(targetType) || isGeo(targetType))
	case DependencyTypeInGeoScope:
		return (isDecision(sourceType) || isMechanism(sourceType) || isOrg(sourceType) || isProgram(sourceType) || isMarket(sourceType)) && isGeo(targetType)
	case DependencyTypeAffects:
		return (isMechanism(sourceType) || isProgram(sourceType) || isOrg(sourceType) || isCommunity(sourceType) || isMarket(sourceType)) &&
			(isActorLike(targetType) || isMarket(targetType) || isProduct(targetType) || isGeo(targetType) || isMedia(targetType))
	default:
		return false
	}
}

func deterministicDirectionalRoleContract(edgeType DependencyType) (sourceRole, targetRole, directionBasis string, ok bool) {
	switch edgeType {
	case DependencyTypeOwns:
		return "owner", "owned_entity", "owner_to_owned", true
	case DependencyTypeSubsidiaryOf:
		return "subsidiary", "parent_owner", "subsidiary_to_parent", true
	case DependencyTypeInvestsIn:
		return "investor", "investee", "investor_to_investee", true
	case DependencyTypeAssures:
		return "assurer", "assuree", "assurer_to_assuree", true
	case DependencyTypeControls:
		return "controller", "controlled_entity", "controller_to_controlled", true
	case DependencyTypeDependsOn:
		return "dependent", "dependency", "dependent_to_dependency", true
	case DependencyTypeSuppliesTo:
		return "supplier", "customer", "supplier_to_customer", true
	case DependencyTypeGatekeepsDecision:
		return "gatekeeper", "decision_point", "gatekeeper_to_decision", true
	case DependencyTypeUsesMechanism:
		return "mechanism_user", "mechanism", "user_to_mechanism", true
	case DependencyTypeControlsAccess:
		return "controller", "access_subject", "controller_to_access_subject", true
	case DependencyTypeImposesCondition:
		return "imposer", "constrained_entity", "imposer_to_constrained_entity", true
	case DependencyTypeAppliesTo:
		return "mechanism", "applicable_entity", "mechanism_to_applicable_entity", true
	case DependencyTypeAdministers:
		return "administrator", "administered_entity", "administrator_to_administered_entity", true
	case DependencyTypeInGeoScope:
		return "scope_owner", "geo_scope", "scope_owner_to_geo_scope", true
	case DependencyTypeAffects:
		return "impact_source", "affected_entity", "impact_source_to_affected_entity", true
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

func canonicalDirectionalSourceRole(edgeType DependencyType, role string) (string, bool) {
	return canonicalDirectionalRole(edgeType, role, true)
}

func canonicalDirectionalTargetRole(edgeType DependencyType, role string) (string, bool) {
	return canonicalDirectionalRole(edgeType, role, false)
}

func canonicalDirectionalRole(edgeType DependencyType, role string, source bool) (string, bool) {
	role = normalizedDirectionalRole(role)
	if role == "" {
		return "", false
	}
	switch edgeType {
	case DependencyTypeOwns:
		if source && inDirectionalRole(role, "owner", "parent_owner", "shareholder", "beneficial_owner") {
			return "owner", true
		}
		if !source && inDirectionalRole(role, "owned_entity", "subsidiary", "asset", "controlled_entity", "controlled") {
			return "owned_entity", true
		}
	case DependencyTypeSubsidiaryOf:
		if source && inDirectionalRole(role, "subsidiary", "owned_entity", "child_entity") {
			return "subsidiary", true
		}
		if !source && inDirectionalRole(role, "parent_owner", "owner", "parent") {
			return "parent_owner", true
		}
	case DependencyTypeInvestsIn:
		if source && inDirectionalRole(role, "investor", "shareholder") {
			return "investor", true
		}
		if !source && inDirectionalRole(role, "investee", "portfolio_company", "target", "owned_entity") {
			return "investee", true
		}
	case DependencyTypeAssures:
		if source && inDirectionalRole(role, "assurer", "auditor", "certifier", "inspector", "regulator") {
			return "assurer", true
		}
		if !source && inDirectionalRole(role, "assuree", "audited_entity", "certified_entity", "inspected_entity", "regulated_entity") {
			return "assuree", true
		}
	case DependencyTypeControls:
		if source && inDirectionalRole(role, "controller", "owner", "parent_owner") {
			return "controller", true
		}
		if !source && inDirectionalRole(role, "controlled_entity", "controlled", "owned_entity", "subsidiary") {
			return "controlled_entity", true
		}
	case DependencyTypeDependsOn:
		if source && inDirectionalRole(role, "dependent", "consumer", "requirer", "requester") {
			return "dependent", true
		}
		if !source && inDirectionalRole(role, "dependency", "supplier", "provider", "enabler", "input") {
			return "dependency", true
		}
	case DependencyTypeSuppliesTo:
		if source && inDirectionalRole(role, "supplier", "provider", "seller", "producer", "vendor") {
			return "supplier", true
		}
		if !source && inDirectionalRole(role, "customer", "buyer", "consumer", "recipient", "offtaker") {
			return "customer", true
		}
	case DependencyTypeGatekeepsDecision:
		if source && inDirectionalRole(role, "gatekeeper", "approver", "decision_maker", "authority", "veto_holder") {
			return "gatekeeper", true
		}
		if !source && inDirectionalRole(role, "decision_point", "decision", "approval", "ruling") {
			return "decision_point", true
		}
	case DependencyTypeUsesMechanism:
		if source && inDirectionalRole(role, "mechanism_user", "policy_source", "decision", "regulator", "administrator") {
			return "mechanism_user", true
		}
		if !source && inDirectionalRole(role, "mechanism", "instrument", "policy_mechanism") {
			return "mechanism", true
		}
	case DependencyTypeControlsAccess:
		if source && inDirectionalRole(role, "controller", "gatekeeper", "access_controller", "regulator") {
			return "controller", true
		}
		if !source && inDirectionalRole(role, "access_subject", "controlled_access_target", "affected_entity", "regulated_entity", "subject") {
			return "access_subject", true
		}
	case DependencyTypeImposesCondition:
		if source && inDirectionalRole(role, "imposer", "condition_imposer", "regulator", "mechanism") {
			return "imposer", true
		}
		if !source && inDirectionalRole(role, "constrained_entity", "conditioned_entity", "affected_entity", "subject") {
			return "constrained_entity", true
		}
	case DependencyTypeAppliesTo:
		if source && inDirectionalRole(role, "mechanism", "instrument", "rule") {
			return "mechanism", true
		}
		if !source && inDirectionalRole(role, "applicable_entity", "covered_entity", "subject", "affected_entity") {
			return "applicable_entity", true
		}
	case DependencyTypeAdministers:
		if source && inDirectionalRole(role, "administrator", "administering_body", "operator") {
			return "administrator", true
		}
		if !source && inDirectionalRole(role, "administered_entity", "administered_program", "subordinate") {
			return "administered_entity", true
		}
	case DependencyTypeInGeoScope:
		if source && inDirectionalRole(role, "scope_owner", "scoped_instrument", "mechanism", "policy_source") {
			return "scope_owner", true
		}
		if !source && inDirectionalRole(role, "geo_scope", "jurisdiction", "geography", "location") {
			return "geo_scope", true
		}
	case DependencyTypeAffects:
		if source && inDirectionalRole(role, "impact_source", "mechanism", "source", "driver") {
			return "impact_source", true
		}
		if !source && inDirectionalRole(role, "affected_entity", "impacted_entity", "exposure_target") {
			return "affected_entity", true
		}
	}
	return "", false
}

func canonicalDirectionalBasis(edgeType DependencyType, direction string) (string, bool) {
	dir := normalizedDirectionalRole(direction)
	if dir == "" {
		return "", false
	}
	switch edgeType {
	case DependencyTypeOwns:
		if inDirectionalRole(dir, "owner_to_owned", "parent_to_subsidiary", "shareholder_to_company") {
			return "owner_to_owned", true
		}
	case DependencyTypeSubsidiaryOf:
		if inDirectionalRole(dir, "subsidiary_to_parent", "child_to_parent") {
			return "subsidiary_to_parent", true
		}
	case DependencyTypeInvestsIn:
		if inDirectionalRole(dir, "investor_to_investee", "investor_to_target") {
			return "investor_to_investee", true
		}
	case DependencyTypeAssures:
		if inDirectionalRole(dir, "assurer_to_assuree", "auditor_to_audited") {
			return "assurer_to_assuree", true
		}
	case DependencyTypeControls:
		if inDirectionalRole(dir, "controller_to_controlled", "owner_to_controlled") {
			return "controller_to_controlled", true
		}
	case DependencyTypeDependsOn:
		if inDirectionalRole(dir, "dependent_to_dependency", "consumer_to_supplier") {
			return "dependent_to_dependency", true
		}
	case DependencyTypeSuppliesTo:
		if inDirectionalRole(dir, "supplier_to_customer", "provider_to_recipient") {
			return "supplier_to_customer", true
		}
	case DependencyTypeGatekeepsDecision:
		if inDirectionalRole(dir, "gatekeeper_to_decision", "authority_to_decision", "approver_to_decision") {
			return "gatekeeper_to_decision", true
		}
	case DependencyTypeUsesMechanism:
		if inDirectionalRole(dir, "user_to_mechanism", "decision_to_mechanism", "source_to_mechanism") {
			return "user_to_mechanism", true
		}
	case DependencyTypeControlsAccess:
		if inDirectionalRole(dir, "controller_to_access_subject", "gatekeeper_to_subject", "controller_to_target") {
			return "controller_to_access_subject", true
		}
	case DependencyTypeImposesCondition:
		if inDirectionalRole(dir, "imposer_to_constrained_entity", "mechanism_to_constrained_entity", "regulator_to_constrained_entity") {
			return "imposer_to_constrained_entity", true
		}
	case DependencyTypeAppliesTo:
		if inDirectionalRole(dir, "mechanism_to_applicable_entity", "instrument_to_subject") {
			return "mechanism_to_applicable_entity", true
		}
	case DependencyTypeAdministers:
		if inDirectionalRole(dir, "administrator_to_administered_entity", "operator_to_administered_entity") {
			return "administrator_to_administered_entity", true
		}
	case DependencyTypeInGeoScope:
		if inDirectionalRole(dir, "scope_owner_to_geo_scope", "mechanism_to_geo_scope", "instrument_to_jurisdiction") {
			return "scope_owner_to_geo_scope", true
		}
	case DependencyTypeAffects:
		if inDirectionalRole(dir, "impact_source_to_affected_entity", "source_to_affected_entity", "driver_to_affected_entity") {
			return "impact_source_to_affected_entity", true
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
func isRoleInvertedForDirection(edgeType DependencyType, qualifiers map[string]interface{}) bool {
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
