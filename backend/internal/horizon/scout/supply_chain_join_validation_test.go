package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestBuildSupplyChainJoinAllowedRefs_DeduplicatesGraphContext(t *testing.T) {
	chain := agents.SupplyChain{
		Nodes: []agents.SupplyChainNode{
			{CanonicalPath: "/org/us/supplier-a"},
			{CanonicalPath: "/product/component/test/shared-input"},
			{CanonicalPath: "/org/us/customer-b"},
		},
	}
	segments := []pathbuilder.PathSegment{
		{
			SupplierRef:      "/org/us/supplier-a",
			ProductRef:       "/product/component/test/shared-input",
			CustomerRef:      "/org/us/customer-b",
			TransportViaRefs: []string{"/fac/us/port-of-houston"},
		},
	}
	graphContext := "Known entities:\n  - Supplier A (/org/us/supplier-a)\n  - Port (/fac/us/port-of-houston)\n"
	refs := buildSupplyChainJoinAllowedRefs(chain, segments, graphContext)
	if len(refs) != 4 {
		t.Fatalf("allowed refs=%v", refs)
	}
}

func TestMapJoinValidationAssessmentToModel_BlocksPolicyViolation(t *testing.T) {
	assessment := agents.SupplyChainJoinValidationAssessment{
		JoinIndex:        0,
		LeftLegIndex:     0,
		RightLegIndex:    1,
		Status:           "validated",
		ProductRelation:  "same",
		Reason:           "looks continuous",
		PolicyViolations: []string{"non_allowlisted_reuse_ref:/org/us/invented-middleman"},
	}
	got := mapJoinValidationAssessmentToModel(assessment)
	if got.Status != models.SupplyChainSufficiencyForensicGap {
		t.Fatalf("status=%s, want %s", got.Status, models.SupplyChainSufficiencyForensicGap)
	}
	if got.EntityReuse != models.SupplyChainJoinEntityReuseBlocked {
		t.Fatalf("entityReuse=%s, want %s", got.EntityReuse, models.SupplyChainJoinEntityReuseBlocked)
	}
}
