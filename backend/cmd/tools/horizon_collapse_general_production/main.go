package main

import (
	"context"
	"flag"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var (
	projectID = flag.String("project", "", "GCP project id")
	apply     = flag.Bool("apply", false, "Apply collapse")
	path      = flag.String("path", "/fac/na/general-production", "Facility canonical path to collapse")
	limit     = flag.Int("limit", 0, "Max matching facilities to process (0 = unlimited)")
	samples   = flag.Int("samples", 20, "How many sample collapses to print")
)

type counters struct {
	scanned        int
	candidates     int
	orgReused      int
	orgCreated     int
	merged         int
	skippedGeneric int
	skippedNoOrg   int
}

type orgCandidate struct {
	fragment *models.HorizonFragment
	score    int
	reason   string
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(client.Client())
	edgesRepo := hrepo.NewDependencyEdgesRepository(client.Client())
	mergeAuditRepo := hrepo.NewFragmentMergeAuditRepository(client.Client())
	redirectsRepo := hrepo.NewCanonicalPathRedirectsRepository(client.Client())
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(client.Client())
	lensesRepo := hrepo.NewLensesRepository(client.Client())
	watchSubjectsRepo := hrepo.NewWatchSubjectsRepository(client.Client())
	mergeRepo := hrepo.NewFragmentsMergeRepository(fragmentsRepo, edgesRepo, mergeAuditRepo).
		WithRedirectsRepo(redirectsRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithWatchSubjectsRepo(watchSubjectsRepo).
		WithOntologyFactsRepo(corerepo.NewOntologyFactsRepository(client.Client())).
		WithEntitiesRepo(corerepo.NewEntityRepository(client.Client()))

	frags, err := fragmentsRepo.ListByCanonicalPath(ctx, strings.TrimSpace(*path))
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(frags, func(i, j int) bool { return strings.TrimSpace(frags[i].ID) < strings.TrimSpace(frags[j].ID) })

	var st counters
	var sampleLines []string
	for _, frag := range frags {
		if frag == nil || frag.Type != models.FragmentTypeFacility || strings.TrimSpace(frag.TenantID) != "global" {
			continue
		}
		st.scanned++
		orgFrag, created, reason, err := resolveCollapseTargetOrg(ctx, fragmentsRepo, edgesRepo, frag)
		if err != nil || orgFrag == nil || strings.TrimSpace(orgFrag.ID) == "" {
			if reason == "generic_facility_label" {
				st.skippedGeneric++
			} else {
				st.skippedNoOrg++
			}
			continue
		}
		st.candidates++
		if created {
			st.orgCreated++
		} else {
			st.orgReused++
		}
		if len(sampleLines) < *samples {
			sampleLines = append(sampleLines, strings.TrimSpace(frag.CanonicalPath)+"["+strings.TrimSpace(frag.ID)+"] -> "+strings.TrimSpace(orgFrag.CanonicalPath)+"["+strings.TrimSpace(orgFrag.ID)+"] ("+reason+")")
		}
		if *apply {
			if _, err := mergeRepo.MergeFragment(ctx, strings.TrimSpace(orgFrag.ID), strings.TrimSpace(frag.ID), "global", "tool_dedup", "collapse_general_production", "", ""); err != nil {
				log.Printf("WARN collapse merge failed facilityId=%s orgId=%s err=%v", frag.ID, orgFrag.ID, err)
				continue
			}
			st.merged++
		}
		if *limit > 0 && st.candidates >= *limit {
			break
		}
	}

	sort.Strings(sampleLines)
	for _, line := range sampleLines {
		log.Printf("sample collapse: %s", line)
	}
	log.Printf("summary scanned=%d candidates=%d org_reused=%d org_created=%d merged=%d skipped_generic=%d skipped_no_org=%d",
		st.scanned, st.candidates, st.orgReused, st.orgCreated, st.merged, st.skippedGeneric, st.skippedNoOrg)
	if *apply {
		log.Printf("completed at %s", time.Now().UTC().Format(time.RFC3339))
	}
}

func resolveCollapseTargetOrg(ctx context.Context, fragmentsRepo *hrepo.HorizonFragmentsRepository, edgesRepo *hrepo.DependencyEdgesRepository, frag *models.HorizonFragment) (*models.HorizonFragment, bool, string, error) {
	if frag == nil || fragmentsRepo == nil || edgesRepo == nil {
		return nil, false, "missing_dependencies", nil
	}
	if existing, reason := chooseAdjacentOrgCandidate(ctx, fragmentsRepo, edgesRepo, frag); existing != nil && strings.TrimSpace(existing.ID) != "" {
		return existing, false, reason, nil
	}
	orgLabel := deriveOrgLabelFromSyntheticFacility(frag)
	if orgLabel == "" {
		return nil, false, "generic_facility_label", nil
	}
	orgFrag, created, err := ensureCollapseTargetOrg(ctx, fragmentsRepo, orgLabel)
	if err != nil || orgFrag == nil || strings.TrimSpace(orgFrag.ID) == "" {
		return nil, false, "no_org_target", err
	}
	if created {
		return orgFrag, true, "created_from_facility_label", nil
	}
	return orgFrag, false, "reused_from_facility_label", nil
}

func chooseAdjacentOrgCandidate(ctx context.Context, fragmentsRepo *hrepo.HorizonFragmentsRepository, edgesRepo *hrepo.DependencyEdgesRepository, frag *models.HorizonFragment) (*models.HorizonFragment, string) {
	outEdges, _ := edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), nil, 100)
	inEdges, _ := edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), nil, 100)
	byID := make(map[string]orgCandidate)
	stem := strings.ToLower(strings.TrimSpace(deriveOrgLabelFromSyntheticFacility(frag)))
	scoreCandidate := func(edge *models.DependencyEdge, dir string, other *models.HorizonFragment) (int, string) {
		if edge == nil || other == nil || other.Type != models.FragmentTypeOrganization {
			return 0, ""
		}
		if canonical.ClassifyPlaceholderCanonicalPath(strings.TrimSpace(other.CanonicalPath)) == canonical.PlaceholderPathOrgSynthetic {
			return 0, ""
		}
		score := 0
		reason := "adjacent_org"
		switch {
		case dir == "in" && (edge.Type == models.DependencyTypeOperatesIn || edge.Type == models.DependencyTypeOwns || edge.Type == models.DependencyTypeManages || edge.Type == models.DependencyTypeControls || edge.Type == models.DependencyTypeAdministers):
			score = 100
			reason = "adjacent_org_operator"
		case dir == "out" && edge.Type == models.DependencyTypeSuppliesTo:
			score = 80
			reason = "adjacent_org_customer"
		case dir == "in" && edge.Type == models.DependencyTypeSuppliesTo:
			score = 60
			reason = "adjacent_org_supplier"
		case edge.Type == models.DependencyTypeProduces || edge.Type == models.DependencyTypeDependsOn || edge.Type == models.DependencyTypeParticipatesIn:
			score = 40
		default:
			score = 20
		}
		orgLabel := strings.ToLower(strings.TrimSpace(firstNonEmpty(other.Labels["en"], other.Identifiers["legalName"], other.Identifiers["shortName"])))
		if stem != "" && orgLabel != "" {
			if orgLabel == stem {
				score += 30
			} else if strings.Contains(orgLabel, stem) || strings.Contains(stem, orgLabel) {
				score += 18
			}
		}
		return score, reason
	}
	collect := func(edges []*models.DependencyEdge, dir string) {
		for _, edge := range edges {
			if edge == nil {
				continue
			}
			otherID := strings.TrimSpace(edge.TargetFragmentID)
			if dir == "in" {
				otherID = strings.TrimSpace(edge.SourceFragmentID)
			}
			if otherID == "" || otherID == strings.TrimSpace(frag.ID) {
				continue
			}
			other, err := fragmentsRepo.Get(ctx, otherID)
			if err != nil || other == nil || other.Type != models.FragmentTypeOrganization {
				continue
			}
			score, reason := scoreCandidate(edge, dir, other)
			if score <= 0 {
				continue
			}
			current, ok := byID[otherID]
			if !ok || score > current.score {
				byID[otherID] = orgCandidate{fragment: other, score: score, reason: reason}
			}
		}
	}
	collect(outEdges, "out")
	collect(inEdges, "in")
	var best orgCandidate
	for _, candidate := range byID {
		if candidate.fragment == nil || strings.TrimSpace(candidate.fragment.ID) == "" {
			continue
		}
		if best.fragment == nil || candidate.score > best.score || (candidate.score == best.score && strings.TrimSpace(candidate.fragment.ID) < strings.TrimSpace(best.fragment.ID)) {
			best = candidate
		}
	}
	if best.fragment != nil {
		return best.fragment, best.reason
	}
	return nil, ""
}

func deriveOrgLabelFromSyntheticFacility(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	label := strings.TrimSpace(firstNonEmpty(
		strings.TrimSpace(fragment.Identifiers["legalName"]),
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
	))
	if label == "" {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(label))
	if lower == "" || lower == "general production" || lower == "general production facility" {
		return ""
	}
	suffixes := []string{
		" production facility",
		" manufacturing facility",
		" processing facility",
		" processing plant",
		" distribution center",
		" distribution",
		" processing",
		" logistics center",
		" logistics hub",
		" general production",
		" general logistics",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(strings.ToLower(label), suffix) {
			trimmed := strings.TrimSpace(label[:len(label)-len(suffix)])
			if trimmed != "" && strings.ToLower(trimmed) != "general" {
				label = trimmed
				break
			}
		}
	}
	label = strings.TrimSpace(strings.TrimSuffix(label, " Facility"))
	label = strings.TrimSpace(strings.TrimSuffix(label, " facility"))
	if label == "" || strings.EqualFold(label, "general") || strings.EqualFold(label, "general production") {
		return ""
	}
	return label
}

func ensureCollapseTargetOrg(ctx context.Context, fragmentsRepo *hrepo.HorizonFragmentsRepository, orgLabel string) (*models.HorizonFragment, bool, error) {
	if fragmentsRepo == nil || strings.TrimSpace(orgLabel) == "" {
		return nil, false, nil
	}
	orgLabel = strings.TrimSpace(orgLabel)
	candidate := &models.HorizonFragment{
		Type: models.FragmentTypeOrganization,
		Labels: map[string]string{
			"en": orgLabel,
		},
		Identifiers: map[string]string{
			"legalName": orgLabel,
			"shortName": orgLabel,
		},
	}
	orgPath := strings.TrimSpace(scout.InferOrgCanonicalPathFromHints(candidate))
	if orgPath == "" {
		orgPath = taxonomy.OrgCanonicalPath("na", orgLabel)
	}
	if existing, err := fragmentsRepo.GetByCanonicalPath(ctx, orgPath); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
		return existing, false, nil
	}
	for _, lookup := range []func(context.Context, string, models.FragmentType) ([]*models.HorizonFragment, error){
		func(ctx context.Context, value string, fragType models.FragmentType) ([]*models.HorizonFragment, error) {
			return fragmentsRepo.FindByLabel(ctx, "en", value, fragType)
		},
		func(ctx context.Context, value string, fragType models.FragmentType) ([]*models.HorizonFragment, error) {
			return fragmentsRepo.FindByIdentifierAndType(ctx, "legalName", value, fragType)
		},
		func(ctx context.Context, value string, fragType models.FragmentType) ([]*models.HorizonFragment, error) {
			return fragmentsRepo.FindByIdentifierAndType(ctx, "shortName", value, fragType)
		},
	} {
		if found, err := lookup(ctx, orgLabel, models.FragmentTypeOrganization); err == nil {
			for _, item := range found {
				if item != nil && strings.TrimSpace(item.ID) != "" {
					return item, false, nil
				}
			}
		}
	}

	now := time.Now().UTC()
	org := &models.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: orgPath,
		Type:          models.FragmentTypeOrganization,
		Labels: map[string]string{
			"en": orgLabel,
		},
		Identifiers: map[string]string{
			"legalName": orgLabel,
			"shortName": orgLabel,
		},
		Status: string(models.FragmentStatusActive),
		Governance: models.Governance{
			Owner:  "SYSTEM",
			Status: string(models.FragmentStatusActive),
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
	}
	id, err := fragmentsRepo.Create(ctx, org)
	if err != nil {
		return nil, false, err
	}
	org.ID = id
	return org, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
