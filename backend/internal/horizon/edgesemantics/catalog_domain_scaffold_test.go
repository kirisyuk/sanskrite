package edgesemantics

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// TestDomainScaffoldRelations_AreBackedByDirectionPolicy ensures every
// declared domain-scaffold relation has a direction-policy entry. Under
// the AH taxonomy /domain/ paths are folded into evidence (see
// docs/archive/taxonomy-migration-2026/migration_map.yaml: DOMAIN→evidence/domain_legacy), so we no longer
// require a literal "DOMAIN" source class — instead we only assert the
// rule exists. The legacy DOMAIN→/scaffold relationships will be
// rewritten by migrate-cli during the data migration phase.
func TestDomainScaffoldRelations_AreBackedByDirectionPolicy(t *testing.T) {
	for _, rel := range DomainScaffoldRelations() {
		if _, ok := EdgeDirectionPolicies[rel]; !ok {
			t.Fatalf("domain scaffold relation %s missing in direction policy", rel)
		}
	}
}

// TestDomainScaffoldRelationsString_ContainsAllRelations is a smoke
// test on the prompt-facing CSV.
func TestDomainScaffoldRelationsString_ContainsAllRelations(t *testing.T) {
	csv := DomainScaffoldRelationsString()
	for _, rel := range DomainScaffoldRelations() {
		if !strings.Contains(csv, string(rel)) {
			t.Fatalf("domain scaffold csv missing %s", rel)
		}
	}
}

// TestDomainScaffoldRelations_AreKnownAH guards that the curated set
// only references AH-known relations (no legacy strings leaking).
func TestDomainScaffoldRelations_AreKnownAH(t *testing.T) {
	for _, rel := range DomainScaffoldRelations() {
		if !ah.IsKnownRelation(rel) {
			t.Fatalf("domain scaffold relation %s is not an AH-known relation", rel)
		}
	}
}
