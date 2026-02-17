package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestUnitGraphEdgesFromRelations_SuppliesTo(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			RelationType:        "SUPPLIES_TO",
			TargetCanonicalPath: "/org/kz/kcell-jsc",
			Confidence:          0.9,
			Qualifiers: map[string]interface{}{
				models.UnitQualifierSourceUnitRef: "consumer",
				models.UnitQualifierTargetUnitRef: "retail",
			},
		},
	}
	out := unitGraphEdgesFromRelations("/org/cn/huawei-technologies-co-ltd", rels)
	if len(out) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(out))
	}
	e := out[0]
	if e.SupplierOrgRef != "/org/cn/huawei-technologies-co-ltd" || e.CustomerOrgRef != "/org/kz/kcell-jsc" {
		t.Fatalf("unexpected org refs: %+v", e)
	}
	if e.Direction != models.UnitGraphDirectionDownstream {
		t.Fatalf("direction=%q, want %q", e.Direction, models.UnitGraphDirectionDownstream)
	}
	if e.SupplierUnitRef != "consumer" || e.CustomerUnitRef != "retail" {
		t.Fatalf("unexpected unit refs: %+v", e)
	}
}

func TestUnitGraphEdgesFromRelations_DependsOnInvertsSupplierCustomer(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			RelationType:        "DEPENDS_ON",
			TargetCanonicalPath: "/org/us/cisco-systems-inc",
			Qualifiers: map[string]interface{}{
				models.UnitQualifierSourceUnitRef: "enterprise", // customer unit
				models.UnitQualifierTargetUnitRef: "enterprise", // supplier unit
			},
		},
	}
	out := unitGraphEdgesFromRelations("/org/kz/kazakhtelecom-jsc", rels)
	if len(out) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(out))
	}
	e := out[0]
	if e.SupplierOrgRef != "/org/us/cisco-systems-inc" || e.CustomerOrgRef != "/org/kz/kazakhtelecom-jsc" {
		t.Fatalf("unexpected org refs: %+v", e)
	}
	if e.Direction != models.UnitGraphDirectionUpstream {
		t.Fatalf("direction=%q, want %q", e.Direction, models.UnitGraphDirectionUpstream)
	}
}
