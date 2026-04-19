package scout

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestBuildRelationTargetIdentityKey_ProductSlugStable(t *testing.T) {
	rel := agents.DiscoveredRelation{
		TargetName: "Avatr 12",
		TargetType: string(FragmentTypeProduct),
	}
	k1 := buildRelationTargetIdentityKey(rel, "/product/avatr-12", FragmentTypeProduct)
	k2 := buildRelationTargetIdentityKey(rel, "/product/finished/ev/sedan/avatr-12", FragmentTypeProduct)
	if k1 == "" || k2 == "" {
		t.Fatalf("identity key must not be empty: k1=%q k2=%q", k1, k2)
	}
	if k1 != k2 {
		t.Fatalf("product identity keys differ: %q vs %q", k1, k2)
	}
}

func TestBuildRelationTargetIdentityKey_MarketIncludesGeoDomain(t *testing.T) {
	rel := agents.DiscoveredRelation{TargetType: string(FragmentTypeMarket)}
	k1 := buildRelationTargetIdentityKey(rel, "/market/sa/finance/banking", FragmentTypeMarket)
	k2 := buildRelationTargetIdentityKey(rel, "/market/sa/technology/banking", FragmentTypeMarket)
	k3 := buildRelationTargetIdentityKey(rel, "/market/ae/finance/banking", FragmentTypeMarket)
	if k1 == "" || k2 == "" || k3 == "" {
		t.Fatalf("identity keys must not be empty: k1=%q k2=%q k3=%q", k1, k2, k3)
	}
	if k1 == k2 {
		t.Fatalf("different domains (finance vs technology) must produce different keys: %q == %q", k1, k2)
	}
	if k1 == k3 {
		t.Fatalf("different geo (sa vs ae) must produce different keys: %q == %q", k1, k3)
	}
	if !strings.Contains(k1, "geo=sa") || !strings.Contains(k1, "domain=finance") {
		t.Fatalf("market identity key must include geo and domain: %q", k1)
	}
}

func TestBuildRelationTargetIdentityKey_OrgNameBeatsSlug(t *testing.T) {
	rel1 := agents.DiscoveredRelation{
		TargetType:      string(FragmentTypeOrganization),
		TargetShortName: "Changan Automobile",
	}
	rel2 := agents.DiscoveredRelation{
		TargetType:      string(FragmentTypeOrganization),
		TargetLegalName: "Changan Automobile",
	}
	k1 := buildRelationTargetIdentityKey(rel1, "/org/cn/chongqing-changan-automobile-ltd", FragmentTypeOrganization)
	k2 := buildRelationTargetIdentityKey(rel2, "/org/cn/changan-automobile-ltd", FragmentTypeOrganization)
	if k1 == "" || k2 == "" {
		t.Fatalf("identity key must not be empty: k1=%q k2=%q", k1, k2)
	}
	if k1 != k2 {
		t.Fatalf("org identity keys differ: %q vs %q", k1, k2)
	}
}
