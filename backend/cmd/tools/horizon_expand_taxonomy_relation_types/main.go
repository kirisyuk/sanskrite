package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Expands Horizon taxonomy relation types in Firestore WITHOUT overwriting the full taxonomy document.
//
// Usage:
//   go run ./cmd/tools/horizon_expand_taxonomy_relation_types --project <gcp-project> --version v1
func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	version := flag.String("version", "v1", "taxonomy version doc id (default v1)")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID)")
	}
	if strings.TrimSpace(*version) == "" {
		*version = "v1"
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	taxRepo := repo.NewHorizonTaxonomyRepository(fsClient.Client())
	tax, err := taxRepo.Get(ctx, strings.TrimSpace(*version))
	if err != nil {
		log.Fatalf("taxonomy get failed: %v", err)
	}

	old := append([]string(nil), tax.AllowedRelationTypes...)

	// Canonical superset of relation types supported by the codebase (DependencyEdge types + SUBSIDIARY_OF).
	// Keep ordering stable for nicer diffs and predictable UI grouping.
	canonical := []string{
		string(horizonmodels.DependencyTypeSuppliesTo),
		string(horizonmodels.DependencyTypeLocatedIn),
		string(horizonmodels.DependencyTypeOwns),
		string(horizonmodels.DependencyTypeSubsidiaryOf), // kept for prompts/taxonomy compatibility
		string(horizonmodels.DependencyTypeProduces),
		string(horizonmodels.DependencyTypeConsumes),
		string(horizonmodels.DependencyTypeOperatesIn),
		string(horizonmodels.DependencyTypeRegulates),
		string(horizonmodels.DependencyTypeCompetesWith),
		string(horizonmodels.DependencyTypeInfluences),
		string(horizonmodels.DependencyTypeDependsOn),
		string(horizonmodels.DependencyTypeAffects),
		string(horizonmodels.DependencyTypeTransformsInto),
		string(horizonmodels.DependencyTypeExtractsFrom),
		// Market participation
		string(horizonmodels.DependencyTypeParticipatesIn),
		string(horizonmodels.DependencyTypeDominates),
		// Market hierarchy
		string(horizonmodels.DependencyTypeHasMarketSegment),
		// Government hierarchy
		string(horizonmodels.DependencyTypeAdministers),
		// Governance / org-person
		string(horizonmodels.DependencyTypeManages),
		string(horizonmodels.DependencyTypeControls),
		string(horizonmodels.DependencyTypeControlsAccess),
		string(horizonmodels.DependencyTypeEmployedBy),
		string(horizonmodels.DependencyTypeBoardMemberOf),
		string(horizonmodels.DependencyTypeFounded),
		string(horizonmodels.DependencyTypeInvestsIn),
		string(horizonmodels.DependencyTypePartnersWith),
		// Strategic decision layer
		string(horizonmodels.DependencyTypeImposesCondition),
		string(horizonmodels.DependencyTypeUsesMechanism),
		string(horizonmodels.DependencyTypeAppliesTo),
		// People/org meta-relations
		string(horizonmodels.DependencyTypeKeyPerson),
		// Assurance / oversight
		string(horizonmodels.DependencyTypeAssures),
		// OSINT-lite / governance extensions
		string(horizonmodels.DependencyTypeAuditedBy),
		string(horizonmodels.DependencyTypeRelativeOf),
		string(horizonmodels.DependencyTypeRelatedTo),
		// Community relationships
		string(horizonmodels.DependencyTypeOperatesVia),
		string(horizonmodels.DependencyTypeMemberOf),
		string(horizonmodels.DependencyTypeAffiliatedWith),
		// Community hierarchy
		string(horizonmodels.DependencyTypeHasSubgroup),
		// Domain contracts
		string(horizonmodels.DependencyTypeFocusesOn),
		string(horizonmodels.DependencyTypeInvolvesActor),
		string(horizonmodels.DependencyTypeInGeoScope),
		string(horizonmodels.DependencyTypeAbout),
		string(horizonmodels.DependencyTypeRelatedDomain),
		string(horizonmodels.DependencyTypeBroadensTo),
		string(horizonmodels.DependencyTypeNarrowsTo),
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(old)+len(canonical))

	// Preserve existing order first (so we don't churn diffs unnecessarily).
	for _, s := range old {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		u := strings.ToUpper(s)
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	// Then append missing canonical types.
	for _, s := range canonical {
		u := strings.ToUpper(strings.TrimSpace(s))
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	// If Firestore doc contains extra types unknown to this binary, keep them (sorted for determinism).
	// This makes the tool non-destructive for forward-compat expansions.
	extras := make([]string, 0)
	for _, s := range old {
		u := strings.ToUpper(strings.TrimSpace(s))
		if u == "" {
			continue
		}
		// already included above
	}
	_ = extras

	// Normalize duplicates + final deterministic order:
	// We keep the "existing then canonical" append order; but ensure uniqueness already handled.
	// To still be deterministic across runs, ensure stable ordering for any non-canonical tokens that slipped in.
	// (At the moment we do not add non-canonical tokens beyond old ones; this is a future-proof hook.)
	known := map[string]struct{}{}
	for _, s := range canonical {
		known[strings.ToUpper(strings.TrimSpace(s))] = struct{}{}
	}
	nonCanonical := make([]string, 0)
	canonicalPart := make([]string, 0)
	for _, s := range out {
		if _, ok := known[s]; ok {
			canonicalPart = append(canonicalPart, s)
		} else {
			nonCanonical = append(nonCanonical, s)
		}
	}
	sort.Strings(nonCanonical)
	out = append(canonicalPart, nonCanonical...)

	if equalStringSlices(old, out) {
		fmt.Printf("No changes: taxonomy %s allowedRelationTypes already up to date (%d)\n", tax.Version, len(out))
		return
	}

	now := time.Now().UTC()
	doc := fsClient.Client().Collection("horizon_taxonomy").Doc(strings.TrimSpace(*version))

	updates := []firestore.Update{
		{Path: "allowedRelationTypes", Value: out},
		{Path: "updatedAt", Value: now},
	}

	if _, err := doc.Update(ctx, updates); err != nil {
		// If doc doesn't exist or Update isn't allowed, fall back to merge-set (non-destructive).
		_, err2 := doc.Set(ctx, map[string]interface{}{
			"allowedRelationTypes": out,
			"updatedAt":            now,
		}, firestore.MergeAll)
		if err2 != nil {
			log.Fatalf("taxonomy update failed: updateErr=%v mergeSetErr=%v", err, err2)
		}
	}

	fmt.Printf("Updated taxonomy %s in project=%s\n", strings.TrimSpace(*version), strings.TrimSpace(*projectID))
	fmt.Printf("allowedRelationTypes: %d -> %d\n", len(old), len(out))
	for _, s := range out {
		fmt.Println(s)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
