package scout

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// SupplyChainValidationError describes a rejected supply chain edge or link.
type SupplyChainValidationError struct {
	Reason   string
	Path     string
	JobID    string
	ChainIdx int
}

func (e *SupplyChainValidationError) Error() string {
	return fmt.Sprintf("[SupplyChainValidation] REJECT reason=%s path=%s jobId=%s chainIndex=%d",
		e.Reason, e.Path, e.JobID, e.ChainIdx)
}

// SupplyChainStrictValidator enforces supply chain operational links.
// Full chain org|gov→fac|deposit→product→fac|deposit→org|gov allowed.
type SupplyChainStrictValidator struct{}

// NewSupplyChainStrictValidator returns a new validator.
func NewSupplyChainStrictValidator() *SupplyChainStrictValidator {
	return &SupplyChainStrictValidator{}
}

var operationalEdgeTypes = map[string]bool{
	"PRODUCES":        true,
	"SUPPLIES_TO":     true,
	"TRANSFORMS_INTO": true,
	"ASSEMBLES_INTO":  true,
	"EXTRACTS_FROM":   true,
}

var forbiddenEdgeTypes = map[string]bool{
	"DEPENDS_ON":      true,
	"INFLUENCES":      true,
	"CONSUMES":        true,
	"REGULATES":       true,
	"PARTICIPATES_IN": true,
	"ADMINISTERS":     true,
	"RELATED_TO":      true,
	"AFFILIATED_WITH": true,
	"PARTNERS_WITH":   true,
}

// ValidateEdge returns nil if the ChainEdge is valid for supply chain operational layer.
// Returns SupplyChainValidationError when the edge must be rejected (no persist).
func (v *SupplyChainStrictValidator) ValidateEdge(edge *ChainEdge, jobID string, chainIdx int) error {
	if edge == nil {
		return &SupplyChainValidationError{Reason: "nil_edge", JobID: jobID, ChainIdx: chainIdx}
	}
	relType := strings.TrimSpace(strings.ToUpper(edge.RelationType))
	srcPath := strings.TrimSpace(edge.SourceCanonicalPath)
	dstPath := strings.TrimSpace(edge.TargetCanonicalPath)
	if srcPath == "" || dstPath == "" {
		return &SupplyChainValidationError{Reason: "empty_endpoint", Path: srcPath + "->" + dstPath, JobID: jobID, ChainIdx: chainIdx}
	}
	// Forbidden types: reject
	if forbiddenEdgeTypes[relType] {
		return &SupplyChainValidationError{Reason: "forbidden_edge_type", Path: relType, JobID: jobID, ChainIdx: chainIdx}
	}
	// Operational types: full chain org→fac→product→fac→org allowed. Org as endpoint is valid.
	if operationalEdgeTypes[relType] {
		switch relType {
		case "SUPPLIES_TO":
			// fac→fac, fac→product, fac→org, org→org, product→org (product supplies to consumer)
			if !strings.HasPrefix(srcPath, "/fac/") && !strings.HasPrefix(srcPath, "/deposit/") && !strings.HasPrefix(srcPath, "/product/") && !strings.HasPrefix(srcPath, "/org/") && !strings.HasPrefix(srcPath, "/gov/") {
				return &SupplyChainValidationError{Reason: "supplies_to_source_must_be_fac_deposit_product_or_org", Path: srcPath, JobID: jobID, ChainIdx: chainIdx}
			}
			if !strings.HasPrefix(dstPath, "/fac/") && !strings.HasPrefix(dstPath, "/product/") && !strings.HasPrefix(dstPath, "/org/") && !strings.HasPrefix(dstPath, "/gov/") {
				return &SupplyChainValidationError{Reason: "supplies_to_target_must_be_fac_product_org_or_gov", Path: dstPath, JobID: jobID, ChainIdx: chainIdx}
			}
		case "PRODUCES":
			// fac→product or org→product (org's general fac implied)
			if !strings.HasPrefix(srcPath, "/fac/") && !strings.HasPrefix(srcPath, "/deposit/") && !strings.HasPrefix(srcPath, "/org/") && !strings.HasPrefix(srcPath, "/gov/") {
				return &SupplyChainValidationError{Reason: "produces_source_must_be_fac_deposit_org_or_gov", Path: srcPath, JobID: jobID, ChainIdx: chainIdx}
			}
			if !strings.HasPrefix(dstPath, "/product/") {
				return &SupplyChainValidationError{Reason: "produces_target_must_be_product", Path: dstPath, JobID: jobID, ChainIdx: chainIdx}
			}
		case "TRANSFORMS_INTO", "ASSEMBLES_INTO":
			// product→product (value chain)
			if !strings.HasPrefix(srcPath, "/product/") || !strings.HasPrefix(dstPath, "/product/") {
				return &SupplyChainValidationError{Reason: "product_transition_must_be_product_to_product", Path: srcPath + "->" + dstPath, JobID: jobID, ChainIdx: chainIdx}
			}
		case "EXTRACTS_FROM":
			// fac→deposit
			if !strings.HasPrefix(srcPath, "/fac/") {
				return &SupplyChainValidationError{Reason: "extracts_from_source_must_be_fac", Path: srcPath, JobID: jobID, ChainIdx: chainIdx}
			}
			if !strings.HasPrefix(dstPath, "/deposit/") {
				return &SupplyChainValidationError{Reason: "extracts_from_target_must_be_deposit", Path: dstPath, JobID: jobID, ChainIdx: chainIdx}
			}
		}
	}
	return nil
}

// ValidateEdgeAgainstFragments applies fragment-aware supply-chain checks that
// cannot be enforced from canonical path prefixes alone.
func (v *SupplyChainStrictValidator) ValidateEdgeAgainstFragments(edge *ChainEdge, sourceFragment, targetFragment *models.HorizonFragment, jobID string, chainIdx int) error {
	if err := v.ValidateEdge(edge, jobID, chainIdx); err != nil {
		return err
	}
	if edge == nil {
		return &SupplyChainValidationError{Reason: "nil_edge", JobID: jobID, ChainIdx: chainIdx}
	}
	if !strings.EqualFold(strings.TrimSpace(edge.RelationType), "PRODUCES") {
		return nil
	}
	if sourceFragment == nil || sourceFragment.Type != models.FragmentTypeFacility {
		return nil
	}
	if targetFragment == nil {
		return nil
	}
	targetPath := strings.TrimSpace(targetFragment.CanonicalPath)
	if targetPath == "" {
		targetPath = strings.TrimSpace(edge.TargetCanonicalPath)
	}
	if strings.HasPrefix(targetPath, "/product/service/") {
		return nil
	}
	if !facilityMayProducePhysicalGoods(sourceFragment) {
		return &SupplyChainValidationError{
			Reason:   "produces_source_facility_non_producing_role",
			Path:     strings.TrimSpace(sourceFragment.CanonicalPath),
			JobID:    jobID,
			ChainIdx: chainIdx,
		}
	}
	return nil
}

// ValidateEdges filters out invalid edges. Returns the subset that pass validation.
// Rejected edges are logged via the optional logFn.
func (v *SupplyChainStrictValidator) ValidateEdges(edges []*ChainEdge, jobID string, logFn func(msg string)) []*ChainEdge {
	if v == nil || len(edges) == 0 {
		return edges
	}
	valid := make([]*ChainEdge, 0, len(edges))
	for i, e := range edges {
		if err := v.ValidateEdge(e, jobID, i); err != nil {
			if logFn != nil {
				logFn(err.Error())
			}
			continue
		}
		valid = append(valid, e)
	}
	return valid
}

// ValidateSupplyLink returns nil if the SupplyLink fragment passes strict validation.
// ValidateSupplyLinkFields is called first; this adds strict /fac/ only checks (no fallback).
func (v *SupplyChainStrictValidator) ValidateSupplyLink(f *models.HorizonFragment) error {
	if f == nil || f.Type != models.FragmentTypeSupplyLink {
		return nil
	}
	if err := models.ValidateSupplyLinkFields(f); err != nil {
		return err
	}
	// SupplierRef and CustomerRef: /fac/, /deposit/, /org/, or /gov/
	// (full chain org|gov→fac→product→fac→org|gov)
	if f.SupplierRef != nil {
		p := strings.TrimSpace(*f.SupplierRef)
		if p != "" && !strings.HasPrefix(p, "/fac/") && !strings.HasPrefix(p, "/deposit/") && !strings.HasPrefix(p, "/org/") && !strings.HasPrefix(p, "/gov/") {
			return &SupplyChainValidationError{Reason: "supplier_ref_must_be_fac_deposit_org_or_gov", Path: p}
		}
	}
	if f.CustomerRef != nil {
		p := strings.TrimSpace(*f.CustomerRef)
		if p != "" && !strings.HasPrefix(p, "/fac/") && !strings.HasPrefix(p, "/org/") && !strings.HasPrefix(p, "/gov/") {
			return &SupplyChainValidationError{Reason: "customer_ref_must_be_fac_org_or_gov", Path: p}
		}
	}
	return nil
}

// IsOperationalEdgeType returns true for PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, EXTRACTS_FROM.
func IsOperationalEdgeType(relType string) bool {
	return operationalEdgeTypes[strings.TrimSpace(strings.ToUpper(relType))]
}

func init() {
	// Ensure taxonomy operational types match our allowlist
	for _, t := range taxonomy.SupplyChainOperationalEdgeTypes {
		if !operationalEdgeTypes[t] {
			operationalEdgeTypes[t] = true
		}
	}
}

func facilityMayProducePhysicalGoods(fragment *models.HorizonFragment) bool {
	if fragment == nil || fragment.Type != models.FragmentTypeFacility {
		return true
	}
	role := taxonomy.NormalizeFacilityRole(firstNonEmptyFacilityValue(
		fragmentIdentifier(fragment, "facilityType"),
		fragmentIdentifier(fragment, "supplyChainRole"),
	))
	switch role {
	case taxonomy.FacilityRoleDistribution, taxonomy.FacilityRoleStorage, taxonomy.FacilityRoleLogistics:
		return false
	}
	kind := taxonomy.NormalizeFacilityKind(fragmentIdentifier(fragment, "facilityKind"))
	if kind == taxonomy.FacilityKindUnknown {
		kind = taxonomy.InferFacilityKind(
			strings.TrimSpace(fragment.CanonicalPath),
			fragmentLabel(fragment, "en"),
			fragmentIdentifier(fragment, "shortName"),
			fragmentIdentifier(fragment, "legalName"),
			fragmentIdentifier(fragment, "facilityType"),
			fragmentIdentifier(fragment, "supplyChainRole"),
		)
	}
	switch kind {
	case taxonomy.FacilityKindWarehouse, taxonomy.FacilityKindTerminal, taxonomy.FacilityKindSeaport, taxonomy.FacilityKindAirport:
		return false
	default:
		return true
	}
}

func fragmentIdentifier(fragment *models.HorizonFragment, key string) string {
	if fragment == nil || fragment.Identifiers == nil {
		return ""
	}
	return strings.TrimSpace(fragment.Identifiers[key])
}

func fragmentLabel(fragment *models.HorizonFragment, key string) string {
	if fragment == nil || fragment.Labels == nil {
		return ""
	}
	return strings.TrimSpace(fragment.Labels[key])
}

func firstNonEmptyFacilityValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
