package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// =============================================================================
// EDGE POLICIES
// =============================================================================
// This file defines normalization and validation policies for dependency edges.
// Policies are enforced at creation time and during cleanup operations.

// =============================================================================
// Policy 1: Symmetric Edge Normalization
// =============================================================================
// Some edge types represent symmetric relationships where direction doesn't matter.
// To avoid duplicates (A->B and B->A), we normalize by always placing the
// lexicographically smaller ID as source.

// SymmetricEdgeTypes defines edge types where the relationship is reciprocal.
// For these types, we normalize source/target to ensure consistent representation.
var SymmetricEdgeTypes = map[models.DependencyType]bool{
	models.DependencyTypeCompetesWith: true, // "A competes with B" = "B competes with A"
	models.DependencyTypePartnersWith: true, // "A partners with B" = "B partners with A"
	// Person-person social graph (directionless by nature).
	models.DependencyTypeRelativeOf: true, // "A is relative of B" = "B is relative of A"
	models.DependencyTypeRelatedTo:  true, // "A is related to B" = "B is related to A"
}

// NormalizeSymmetricEdge ensures consistent source/target ordering for symmetric edges.
// Returns: (normalizedSourceID, normalizedTargetID, edgeType, wasSwapped)
func NormalizeSymmetricEdge(sourceID, targetID string, edgeType models.DependencyType) (string, string, models.DependencyType, bool) {
	if !SymmetricEdgeTypes[edgeType] {
		return sourceID, targetID, edgeType, false
	}

	// Normalize by placing smaller ID as source
	if strings.Compare(sourceID, targetID) > 0 {
		return targetID, sourceID, edgeType, true
	}
	return sourceID, targetID, edgeType, false
}

// =============================================================================
// Policy 2: Edge Direction Validation
// =============================================================================
// Certain edge types have semantically meaningful directions that must be enforced.
// When an edge violates direction rules, it should be reversed (not deleted).

// EdgeDirectionRule defines valid source and target fragment types for an edge type.
type EdgeDirectionRule struct {
	ValidSourceTypes []models.FragmentType
	ValidTargetTypes []models.FragmentType
	Description      string // Human-readable explanation
}

// =============================================================================
// Policy 2.5: Edge Canonicalization (Type + Direction Rewrites)
// =============================================================================
// Some edge patterns are not just "wrong direction" but "wrong relation type".
// In those cases, reversing is not sufficient: we rewrite type and (optionally) swap.
//
// NOTE: Keep this conservative and explicit. This is used at creation time and during cleanup.

// CanonicalizeEdge rewrites an edge type (and optionally direction) based on endpoint fragment types.
// Returns: (newType, shouldSwapEndpoints, changed, reason).
func CanonicalizeEdge(edgeType models.DependencyType, sourceType, targetType models.FragmentType) (models.DependencyType, bool, bool, string) {
	// Keep the canonicalization logic in models to avoid import cycles (repo <-> scout).
	return models.CanonicalizeEdge(edgeType, sourceType, targetType)
}

// EdgeDirectionPolicies defines which fragment types are allowed as source/target.
// If an edge violates these rules, reverse source<->target if the reverse is valid.
var EdgeDirectionPolicies = map[models.DependencyType]EdgeDirectionRule{
	// REGULATES: Only regulatory bodies (Organizations) can regulate.
	// Facilities, Products, Persons cannot be regulators.
	models.DependencyTypeRegulates: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeGeoRegion,
		},
		Description: "Regulator (org) regulates regulated entity",
	},

	// OWNS: Ownership flows from owner to owned.
	// Organizations and Persons can own; Facilities and Products cannot.
	models.DependencyTypeOwns: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeDeposit, // Deposits can be owned
		},
		Description: "Owner owns asset/subsidiary/deposit",
	},

	// CONTROLS: Control flows from controller to controlled entity.
	models.DependencyTypeControls: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		Description: "Controller controls entity (non-equity or indirect control)",
	},

	// MANAGES: Formal management relationship (CEO, director, operator).
	models.DependencyTypeManages: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProgram,
		},
		Description: "Manager manages entity (formal authority, not ownership)",
	},

	// EMPLOYED_BY: Employment relationship (person employed by org).
	models.DependencyTypeEmployedBy: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person is employed by organization",
	},

	// BOARD_MEMBER_OF: Person is a board member of organization.
	models.DependencyTypeBoardMemberOf: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person is board member of organization",
	},

	// WORKS_FOR: Legacy employment relationship (prefer EMPLOYED_BY).
	models.DependencyTypeWorksFor: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person works for organization (legacy, prefer EMPLOYED_BY)",
	},

	// KEY_PERSON: Person is a key person of organization.
	models.DependencyTypeKeyPerson: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person is key person of organization",
	},

	// HAS_KEY_PERSON: Inverse of KEY_PERSON (for normalization).
	// NOTE: this is legacy. Canonical representation is KEY_PERSON (PERSON → ORG).
	models.DependencyTypeHasKeyPerson: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Legacy alias; normalize to KEY_PERSON (person -> org)",
	},

	// FOUNDED: Founder relationship.
	models.DependencyTypeFounded: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypePerson,
			models.FragmentTypeOrganization,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeProgram,
		},
		Description: "Founder founded organization/program",
	},

	// INVESTS_IN: Investment relationship.
	models.DependencyTypeInvestsIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeProgram,
			models.FragmentTypeProduct,
		},
		Description: "Investor invests in investee",
	},

	// PARTNERS_WITH: Symmetric partnership between organizations/facilities.
	models.DependencyTypePartnersWith: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		Description: "Partners collaborate (symmetric)",
	},

	// PRODUCES: Production flows from producer to product.
	// Organizations, Facilities, and Deposits can produce.
	models.DependencyTypeProduces: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeDeposit, // Deposits produce resources
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
		Description:      "Producer produces product",
	},

	// CONSUMES: Consumption flows from consumer to product.
	// Organizations, Facilities, and Products can consume a product.
	models.DependencyTypeConsumes: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
		Description:      "Consumer consumes product",
	},

	// LOCATED_IN: Entity is located within a geographic region.
	// Target must always be GEO_REGION.
	models.DependencyTypeLocatedIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeFacility,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeGeoRegion, // Nested regions: city in state
			models.FragmentTypeDeposit,   // Deposits are located in regions
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion},
		Description:      "Entity is located within region",
	},

	// OPERATES_IN: Organization operates in a region, facility, or deposit.
	// Only Organizations can operate.
	models.DependencyTypeOperatesIn: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeGeoRegion,
			models.FragmentTypeFacility,
			models.FragmentTypeDeposit, // Companies operate in deposits (mining, O&G)
		},
		Description: "Organization operates in location/deposit",
	},

	// SUPPLIES_TO: Supply chain relationship.
	// Organizations and Facilities supply to each other.
	models.DependencyTypeSuppliesTo: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		Description: "Supplier supplies to customer",
	},

	// DEPENDS_ON: Dependency relationship.
	// Almost any entity can depend on another.
	models.DependencyTypeDependsOn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeGeoRegion,
		},
		Description: "Dependent entity depends on dependency",
	},

	// TRANSFORMS_INTO: Value chain transformation (product -> product).
	models.DependencyTypeTransformsInto: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeProduct},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
		Description:      "Product transforms into downstream product",
	},

	// EXTRACTS_FROM: Facility extracts resources from a deposit.
	// Direction: FACILITY → DEPOSIT (more precise than LOCATED_IN).
	models.DependencyTypeExtractsFrom: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeFacility},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDeposit},
		Description:      "Facility extracts from deposit (mining, O&G)",
	},

	// AUDITED_BY: organization/facility is audited by an audit firm (organization).
	models.DependencyTypeAuditedBy: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Entity is audited by external audit firm",
	},

	// RELATIVE_OF: symmetric person-person relationship.
	models.DependencyTypeRelativeOf: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypePerson},
		Description:      "Person is relative of person (symmetric)",
	},

	// RELATED_TO: symmetric person-person relationship (public associates).
	models.DependencyTypeRelatedTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypePerson},
		Description:      "Person is related to person (symmetric)",
	},
}

// ValidateEdgeDirection checks if an edge has correct direction according to policies.
// Returns: (isValid, shouldReverse, rule)
// - isValid: true if current direction is valid
// - shouldReverse: true if reversing would make it valid
// - rule: the applicable rule (nil if no rule defined)
func ValidateEdgeDirection(edgeType models.DependencyType, sourceType, targetType models.FragmentType) (bool, bool, *EdgeDirectionRule) {
	rule, exists := EdgeDirectionPolicies[edgeType]
	if !exists {
		return true, false, nil // No rule = valid
	}

	sourceValid := containsType(rule.ValidSourceTypes, sourceType)
	targetValid := containsType(rule.ValidTargetTypes, targetType)

	if sourceValid && targetValid {
		return true, false, &rule
	}

	// Check if reversed direction would be valid
	reversedSourceValid := containsType(rule.ValidSourceTypes, targetType)
	reversedTargetValid := containsType(rule.ValidTargetTypes, sourceType)

	if reversedSourceValid && reversedTargetValid {
		return false, true, &rule
	}

	return false, false, &rule
}

// ShouldReverseEdge is a convenience function that returns true if the edge
// should be reversed to comply with direction policies.
func ShouldReverseEdge(edgeType models.DependencyType, sourceType, targetType models.FragmentType) bool {
	_, shouldReverse, _ := ValidateEdgeDirection(edgeType, sourceType, targetType)
	return shouldReverse
}

func containsType(types []models.FragmentType, t models.FragmentType) bool {
	for _, vt := range types {
		if vt == t {
			return true
		}
	}
	return false
}

// =============================================================================
// Policy 3: Self-Loop Prevention
// =============================================================================
// Edges where source == target are typically errors and should be rejected.

// IsSelfLoop returns true if the edge points to itself.
func IsSelfLoop(sourceID, targetID string) bool {
	return strings.TrimSpace(sourceID) == strings.TrimSpace(targetID)
}

// =============================================================================
// Policy 4: Edge Deduplication
// =============================================================================
// Duplicate edges (same source, target, type) should be merged, not duplicated.
// This is handled at the repository level, but the policy is documented here.

// EdgeKey generates a unique key for edge deduplication.
// For symmetric edges, the key is normalized to ensure A->B and B->A produce the same key.
func EdgeKey(sourceID, targetID string, edgeType models.DependencyType) string {
	if SymmetricEdgeTypes[edgeType] {
		if strings.Compare(sourceID, targetID) > 0 {
			sourceID, targetID = targetID, sourceID
		}
	}
	return sourceID + "|" + targetID + "|" + string(edgeType)
}
