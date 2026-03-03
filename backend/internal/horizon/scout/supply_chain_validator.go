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

// SupplyChainStrictValidator enforces facility-level only supply chain operational links.
// No transformations: invalid edges/links are rejected and logged.
// org→org SUPPLIES_TO is explicitly rejected (operational edges require fac/deposit endpoints).
type SupplyChainStrictValidator struct{}

// NewSupplyChainStrictValidator returns a new validator.
func NewSupplyChainStrictValidator() *SupplyChainStrictValidator {
	return &SupplyChainStrictValidator{}
}

var operationalEdgeTypes = map[string]bool{
	"PRODUCES":       true,
	"SUPPLIES_TO":    true,
	"TRANSFORMS_INTO": true,
	"EXTRACTS_FROM":  true,
}

var forbiddenEdgeTypes = map[string]bool{
	"DEPENDS_ON":       true,
	"INFLUENCES":       true,
	"CONSUMES":         true,
	"REGULATES":        true,
	"PARTICIPATES_IN":  true,
	"ADMINISTERS":      true,
	"RELATED_TO":       true,
	"AFFILIATED_WITH":  true,
	"PARTNERS_WITH":    true,
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
	// Operational types: no org→org; enforce endpoint constraints per type
	if operationalEdgeTypes[relType] {
		if strings.HasPrefix(srcPath, "/org/") || strings.HasPrefix(dstPath, "/org/") {
			return &SupplyChainValidationError{Reason: "org_endpoint_not_allowed", Path: srcPath + "->" + dstPath, JobID: jobID, ChainIdx: chainIdx}
		}
		switch relType {
		case "SUPPLIES_TO":
			// fac→fac or fac→product
			if !strings.HasPrefix(srcPath, "/fac/") && !strings.HasPrefix(srcPath, "/deposit/") {
				return &SupplyChainValidationError{Reason: "supplies_to_source_must_be_fac_or_deposit", Path: srcPath, JobID: jobID, ChainIdx: chainIdx}
			}
			if !strings.HasPrefix(dstPath, "/fac/") && !strings.HasPrefix(dstPath, "/product/") {
				return &SupplyChainValidationError{Reason: "supplies_to_target_must_be_fac_or_product", Path: dstPath, JobID: jobID, ChainIdx: chainIdx}
			}
		case "PRODUCES":
			// fac→product
			if !strings.HasPrefix(srcPath, "/fac/") && !strings.HasPrefix(srcPath, "/deposit/") {
				return &SupplyChainValidationError{Reason: "produces_source_must_be_fac_or_deposit", Path: srcPath, JobID: jobID, ChainIdx: chainIdx}
			}
			if !strings.HasPrefix(dstPath, "/product/") {
				return &SupplyChainValidationError{Reason: "produces_target_must_be_product", Path: dstPath, JobID: jobID, ChainIdx: chainIdx}
			}
		case "TRANSFORMS_INTO":
			// product→product (value chain)
			if !strings.HasPrefix(srcPath, "/product/") || !strings.HasPrefix(dstPath, "/product/") {
				return &SupplyChainValidationError{Reason: "transforms_into_must_be_product_to_product", Path: srcPath + "->" + dstPath, JobID: jobID, ChainIdx: chainIdx}
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
	// Strict: SupplierRef and CustomerRef must already be /fac/ or /deposit/
	if f.SupplierRef != nil {
		p := strings.TrimSpace(*f.SupplierRef)
		if p != "" && !strings.HasPrefix(p, "/fac/") && !strings.HasPrefix(p, "/deposit/") {
			return &SupplyChainValidationError{Reason: "supplier_ref_must_be_fac_or_deposit", Path: p}
		}
	}
	if f.CustomerRef != nil {
		p := strings.TrimSpace(*f.CustomerRef)
		if p != "" && !strings.HasPrefix(p, "/fac/") {
			return &SupplyChainValidationError{Reason: "customer_ref_must_be_fac", Path: p}
		}
	}
	return nil
}

// IsOperationalEdgeType returns true for PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM.
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
