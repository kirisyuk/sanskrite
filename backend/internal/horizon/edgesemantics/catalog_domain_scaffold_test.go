package edgesemantics

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestDomainScaffoldRelationTypes_AreBackedByDirectionPolicy(t *testing.T) {
	for _, rt := range DomainScaffoldRelationTypes() {
		rule, ok := EdgeDirectionPolicies[rt]
		if !ok {
			t.Fatalf("domain scaffold type %s is missing in direction policy", rt)
		}
		hasDomainSource := false
		for _, src := range rule.ValidSourceTypes {
			if src == models.FragmentTypeDomain {
				hasDomainSource = true
				break
			}
		}
		if !hasDomainSource {
			t.Fatalf("domain scaffold type %s does not allow DOMAIN as source", rt)
		}
	}
}

func TestDomainScaffoldRelationTypesString_ContainsAllTypes(t *testing.T) {
	csv := DomainScaffoldRelationTypesString()
	for _, rt := range DomainScaffoldRelationTypes() {
		if !strings.Contains(csv, string(rt)) {
			t.Fatalf("domain scaffold csv is missing %s", rt)
		}
	}
}
