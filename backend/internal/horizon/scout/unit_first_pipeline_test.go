package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEnsureUnitQualifiersForRelations_AliasMappingAndFallback(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			RelationType: "SUPPLIES_TO",
			Qualifiers: map[string]interface{}{
				"supplierUnitRef":   "enterprise",
				"supplierUnitCode":  "enterprise",
				"customer_unit_ref": "consumer",
			},
		},
		{
			RelationType: "DEPENDS_ON",
		},
	}

	out := ensureUnitQualifiersForRelations(rels, []string{taxonomy.DomainModeSupplyChain})
	if len(out) != 2 {
		t.Fatalf("expected 2 relations, got %d", len(out))
	}
	if got := out[0].Qualifiers[models.UnitQualifierSourceUnitRef]; got != "enterprise" {
		t.Fatalf("source_unit_ref=%v, want enterprise", got)
	}
	if got := out[0].Qualifiers[models.UnitQualifierTargetUnitRef]; got != "consumer" {
		t.Fatalf("target_unit_ref=%v, want consumer", got)
	}
	if got := out[1].Qualifiers[models.UnitQualifierSourceUnitRef]; got != string(models.UnitCodeUnclassified) {
		t.Fatalf("fallback source_unit_ref=%v, want unclassified", got)
	}
	if got := out[1].Qualifiers[models.UnitQualifierTargetUnitRef]; got != string(models.UnitCodeUnclassified) {
		t.Fatalf("fallback target_unit_ref=%v, want unclassified", got)
	}
}

func TestDeriveSourceUnitProfilesFromRelations(t *testing.T) {
	rels := []agents.DiscoveredRelation{
		{
			Qualifiers: map[string]interface{}{
				models.UnitQualifierSourceUnitRef:   "enterprise",
				models.UnitQualifierSourceUnitCode:  "enterprise",
				models.UnitQualifierSourceUnitLabel: "Enterprise Business",
			},
		},
		{
			Qualifiers: map[string]interface{}{
				models.UnitQualifierSourceUnitRef:   "enterprise",
				models.UnitQualifierSourceUnitCode:  "enterprise",
				models.UnitQualifierSourceUnitLabel: "Enterprise Business",
			},
		},
	}
	units := deriveSourceUnitProfilesFromRelations("/org/kz/kazakhtelecom-jsc", rels)
	if len(units) != 1 {
		t.Fatalf("expected 1 deduplicated unit, got %d", len(units))
	}
	if units[0].UnitID != "enterprise" {
		t.Fatalf("unitId=%q, want enterprise", units[0].UnitID)
	}
	if units[0].ParentOrgRef != "/org/kz/kazakhtelecom-jsc" {
		t.Fatalf("parentOrgRef=%q", units[0].ParentOrgRef)
	}
	if units[0].UnitCode != models.UnitCodeEnterprise {
		t.Fatalf("unitCode=%q, want %q", units[0].UnitCode, models.UnitCodeEnterprise)
	}
}

func TestShouldPersistUnitProfilesToGlobal_DefaultOff(t *testing.T) {
	job := &models.ScoutingJob{}
	job.State = map[string]interface{}{}
	if !shouldPersistUnitProfilesToGlobal(job) {
		t.Fatalf("default must be true")
	}
	job.State["disableGlobalUnitProfiles"] = true
	if shouldPersistUnitProfilesToGlobal(job) {
		t.Fatalf("explicit opt-out must disable global persistence")
	}
}

func TestSetFragmentUnitProfilesQualifier_ByDomain(t *testing.T) {
	fragment := &models.HorizonFragment{}
	units := []models.OrgUnitProfile{
		{
			UnitID:       "enterprise",
			ParentOrgRef: "/org/kz/kazakhtelecom-jsc",
			UnitCode:     models.UnitCodeEnterprise,
			Label:        "Enterprise",
		},
	}
	setFragmentUnitProfilesQualifier(fragment, units, "/domain/v1/supply-chain/telecom", true)
	if fragment.Qualifiers == nil {
		t.Fatalf("expected qualifiers map")
	}
	if _, ok := fragment.Qualifiers["unitProfiles"]; !ok {
		t.Fatalf("expected compatibility qualifier unitProfiles")
	}
	raw, ok := fragment.Qualifiers["unitProfilesByDomain"]
	if !ok {
		t.Fatalf("expected unitProfilesByDomain qualifier")
	}
	byDomain, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("unitProfilesByDomain type = %T, want map[string]interface{}", raw)
	}
	if _, ok := byDomain["/domain/v1/supply-chain/telecom"]; !ok {
		t.Fatalf("expected domain key in unitProfilesByDomain")
	}
}

func TestSetFragmentUnitProfilesQualifier_EmptyDomainUsesGeneralKey(t *testing.T) {
	fragment := &models.HorizonFragment{}
	units := []models.OrgUnitProfile{
		{
			UnitID:       "enterprise",
			ParentOrgRef: "/org/kz/kazakhtelecom-jsc",
			UnitCode:     models.UnitCodeEnterprise,
			Label:        "Enterprise",
		},
	}
	setFragmentUnitProfilesQualifier(fragment, units, "", true)
	raw, ok := fragment.Qualifiers["unitProfilesByDomain"]
	if !ok {
		t.Fatalf("expected unitProfilesByDomain qualifier")
	}
	byDomain, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("unitProfilesByDomain type = %T, want map[string]interface{}", raw)
	}
	if _, ok := byDomain[taxonomy.DomainModeGeneral]; !ok {
		t.Fatalf("expected %q key in unitProfilesByDomain", taxonomy.DomainModeGeneral)
	}
}

func TestSetFragmentUnitProfilesQualifier_MigratesReservedDefaultKey(t *testing.T) {
	fragment := &models.HorizonFragment{
		Qualifiers: map[string]interface{}{
			"unitProfilesByDomain": map[string]interface{}{
				"__default__": []interface{}{
					map[string]interface{}{"unitId": "legacy"},
				},
			},
		},
	}
	units := []models.OrgUnitProfile{
		{
			UnitID:       "enterprise",
			ParentOrgRef: "/org/kz/kazakhtelecom-jsc",
			UnitCode:     models.UnitCodeEnterprise,
			Label:        "Enterprise",
		},
	}
	setFragmentUnitProfilesQualifier(fragment, units, "", true)
	raw, ok := fragment.Qualifiers["unitProfilesByDomain"]
	if !ok {
		t.Fatalf("expected unitProfilesByDomain qualifier")
	}
	byDomain, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("unitProfilesByDomain type = %T, want map[string]interface{}", raw)
	}
	for k := range byDomain {
		if len(k) >= 4 && k[:2] == "__" && k[len(k)-2:] == "__" {
			t.Fatalf("reserved key must be sanitized, got %q", k)
		}
	}
	if _, ok := byDomain[taxonomy.DomainModeGeneral]; !ok {
		t.Fatalf("expected %q key after migration", taxonomy.DomainModeGeneral)
	}
}
