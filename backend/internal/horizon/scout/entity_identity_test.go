package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestBuildRelationTargetIdentityKey_ProductSlugStable(t *testing.T) {
	rel := agents.DiscoveredRelation{
		TargetName: "Avatr 12",
		TargetType: string(models.FragmentTypeProduct),
	}
	k1 := buildRelationTargetIdentityKey(rel, "/product/avatr-12", models.FragmentTypeProduct)
	k2 := buildRelationTargetIdentityKey(rel, "/product/finished/ev/sedan/avatr-12", models.FragmentTypeProduct)
	if k1 == "" || k2 == "" {
		t.Fatalf("identity key must not be empty: k1=%q k2=%q", k1, k2)
	}
	if k1 != k2 {
		t.Fatalf("product identity keys differ: %q vs %q", k1, k2)
	}
}

func TestBuildRelationTargetIdentityKey_OrgNameBeatsSlug(t *testing.T) {
	rel1 := agents.DiscoveredRelation{
		TargetType:      string(models.FragmentTypeOrganization),
		TargetShortName: "Changan Automobile",
	}
	rel2 := agents.DiscoveredRelation{
		TargetType:      string(models.FragmentTypeOrganization),
		TargetLegalName: "Changan Automobile",
	}
	k1 := buildRelationTargetIdentityKey(rel1, "/org/cn/chongqing-changan-automobile-ltd", models.FragmentTypeOrganization)
	k2 := buildRelationTargetIdentityKey(rel2, "/org/cn/changan-automobile-ltd", models.FragmentTypeOrganization)
	if k1 == "" || k2 == "" {
		t.Fatalf("identity key must not be empty: k1=%q k2=%q", k1, k2)
	}
	if k1 != k2 {
		t.Fatalf("org identity keys differ: %q vs %q", k1, k2)
	}
}
