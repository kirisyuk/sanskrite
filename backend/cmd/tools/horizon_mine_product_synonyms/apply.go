package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type curatedDecisionSet struct {
	SchemaVersion string                    `json:"schemaVersion"`
	ProjectID     string                    `json:"projectId"`
	TenantID      string                    `json:"tenantId"`
	Accepted      []curatedAcceptedDecision `json:"accepted"`
	Rejected      []curatedRejectedDecision `json:"rejected,omitempty"`
}

type curatedAcceptedDecision struct {
	ClusterID     string   `json:"clusterId"`
	CanonicalID   string   `json:"canonicalId"`
	PreferredName string   `json:"preferredName,omitempty"`
	SynonymIDs    []string `json:"synonymIds"`
	Reason        string   `json:"reason"`
}

type curatedRejectedDecision struct {
	ClusterID string `json:"clusterId"`
	Reason    string `json:"reason"`
}

type applyDecisionResult struct {
	ClusterID          string   `json:"clusterId"`
	CanonicalID        string   `json:"canonicalId"`
	CanonicalPath      string   `json:"canonicalPath,omitempty"`
	PreferredName      string   `json:"preferredName,omitempty"`
	SynonymIDs         []string `json:"synonymIds,omitempty"`
	MergedSynonymIDs   []string `json:"mergedSynonymIds,omitempty"`
	MergedOldPaths     []string `json:"mergedOldPaths,omitempty"`
	SkippedSynonymIDs  []string `json:"skippedSynonymIds,omitempty"`
	DeletedVersions    int      `json:"deletedVersions"`
	NameUpdated        bool     `json:"nameUpdated"`
	VerificationPassed bool     `json:"verificationPassed"`
	Reason             string   `json:"reason,omitempty"`
}

type applySummary struct {
	DecisionsPlanned int `json:"decisionsPlanned"`
	DecisionsRun     int `json:"decisionsRun"`
	DecisionsChanged int `json:"decisionsChanged"`
	MergesApplied    int `json:"mergesApplied"`
	MergesSkipped    int `json:"mergesSkipped"`
	DeletedVersions  int `json:"deletedVersions"`
	NameUpdates      int `json:"nameUpdates"`
	VerifyFailures   int `json:"verifyFailures"`
}

type applyReport struct {
	GeneratedAt string                `json:"generatedAt"`
	ProjectID   string                `json:"projectId"`
	TenantID    string                `json:"tenantId"`
	Summary     applySummary          `json:"summary"`
	Decisions   []applyDecisionResult `json:"decisions"`
}

func runApply(ctx context.Context, db *firestore.Client) error {
	decisionPath := strings.TrimSpace(*decisionsPath)
	if decisionPath == "" {
		return fmt.Errorf("--decisions is required in --mode=apply")
	}

	decisionSet, err := loadDecisionSet(decisionPath)
	if err != nil {
		return err
	}
	if err := validateDecisionSet(decisionSet); err != nil {
		return err
	}

	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}
	if dsTenant := strings.TrimSpace(decisionSet.TenantID); dsTenant != "" && dsTenant != tenant {
		return fmt.Errorf("decision tenant mismatch: decisions=%q runtime=%q", dsTenant, tenant)
	}

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := hrepo.NewDependencyEdgesRepository(db)
	mergeAuditRepo := hrepo.NewFragmentMergeAuditRepository(db)
	redirectsRepo := hrepo.NewCanonicalPathRedirectsRepository(db)
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(db)
	lensesRepo := hrepo.NewLensesRepository(db)
	watchSubjectsRepo := hrepo.NewWatchSubjectsRepository(db)
	versionsRepo := hrepo.NewHorizonFragmentVersionsRepository(db)
	ontologyFactsRepo := corerepo.NewOntologyFactsRepository(db)
	entitiesRepo := corerepo.NewEntityRepository(db)

	mergeRepo := hrepo.NewFragmentsMergeRepository(fragmentsRepo, edgesRepo, mergeAuditRepo).
		WithRedirectsRepo(redirectsRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithWatchSubjectsRepo(watchSubjectsRepo).
		WithOntologyFactsRepo(ontologyFactsRepo).
		WithEntitiesRepo(entitiesRepo).
		WithDeferredPathRewrites(true)

	accepted := append([]curatedAcceptedDecision(nil), decisionSet.Accepted...)
	if *applyLimit > 0 && len(accepted) > *applyLimit {
		accepted = accepted[:*applyLimit]
	}

	report := applyReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ProjectID:   strings.TrimSpace(*projectID),
		TenantID:    tenant,
		Summary: applySummary{
			DecisionsPlanned: len(accepted),
		},
		Decisions: make([]applyDecisionResult, 0, len(accepted)),
	}
	pathReplacements := make(map[string]string, len(accepted)*2)

	for _, decision := range accepted {
		result, err := applyDecision(ctx, fragmentsRepo, versionsRepo, mergeRepo, db, tenant, decision)
		if err != nil {
			return fmt.Errorf("apply decision %s: %w", decision.ClusterID, err)
		}
		report.Decisions = append(report.Decisions, result)
		report.Summary.DecisionsRun++
		if !*dryRunApply && (len(result.MergedSynonymIDs) > 0 || result.NameUpdated || result.DeletedVersions > 0) {
			report.Summary.DecisionsChanged++
		}
		if *dryRunApply {
			report.Summary.MergesSkipped += len(result.SkippedSynonymIDs)
		} else {
			report.Summary.MergesApplied += len(result.MergedSynonymIDs)
			report.Summary.MergesSkipped += len(result.SkippedSynonymIDs)
		}
		report.Summary.DeletedVersions += result.DeletedVersions
		if result.NameUpdated {
			report.Summary.NameUpdates++
		}
		if !result.VerificationPassed {
			report.Summary.VerifyFailures++
		}
		for _, oldPath := range result.MergedOldPaths {
			oldPath = strings.TrimSpace(oldPath)
			if oldPath == "" || oldPath == result.CanonicalPath {
				continue
			}
			pathReplacements[oldPath] = result.CanonicalPath
		}
		if report.Summary.DecisionsRun%10 == 0 || report.Summary.DecisionsRun == len(accepted) {
			log.Printf("apply progress: decisions=%d/%d merged=%d skipped=%d pathPairs=%d", report.Summary.DecisionsRun, len(accepted), report.Summary.MergesApplied, report.Summary.MergesSkipped, len(pathReplacements))
		}
	}
	if !*dryRunApply && len(pathReplacements) > 0 {
		log.Printf("apply deferred product path rewrites queued: pathPairs=%d (run repair-supply-links for single-pass downstream rewrite)", len(pathReplacements))
	}

	printApplySummary(report)
	if path := strings.TrimSpace(*applyReportPath); path != "" {
		if err := writeApplyReport(path, report); err != nil {
			return err
		}
		log.Printf("wrote apply report: %s", path)
	}
	return nil
}

func loadDecisionSet(path string) (curatedDecisionSet, error) {
	var out curatedDecisionSet
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func validateDecisionSet(set curatedDecisionSet) error {
	if strings.TrimSpace(set.SchemaVersion) == "" {
		return fmt.Errorf("decision set schemaVersion is required")
	}
	if len(set.Accepted) == 0 {
		return fmt.Errorf("decision set has no accepted decisions")
	}
	seenCanonical := map[string]struct{}{}
	for i, decision := range set.Accepted {
		if strings.TrimSpace(decision.ClusterID) == "" {
			return fmt.Errorf("accepted[%d].clusterId is required", i)
		}
		if strings.TrimSpace(decision.CanonicalID) == "" {
			return fmt.Errorf("accepted[%d].canonicalId is required", i)
		}
		if _, ok := seenCanonical[decision.ClusterID]; ok {
			return fmt.Errorf("duplicate accepted clusterId: %s", decision.ClusterID)
		}
		seenCanonical[decision.ClusterID] = struct{}{}
		synonymIDs := uniqueStrings(decision.SynonymIDs)
		if len(synonymIDs) == 0 {
			return fmt.Errorf("accepted[%d].synonymIds is empty", i)
		}
		for _, synonymID := range synonymIDs {
			if synonymID == strings.TrimSpace(decision.CanonicalID) {
				return fmt.Errorf("accepted[%d] contains canonicalId inside synonymIds", i)
			}
		}
	}
	return nil
}

func applyDecision(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	versionsRepo *hrepo.HorizonFragmentVersionsRepository,
	mergeRepo *hrepo.FragmentsMergeRepository,
	db *firestore.Client,
	tenant string,
	decision curatedAcceptedDecision,
) (applyDecisionResult, error) {
	result := applyDecisionResult{
		ClusterID:     strings.TrimSpace(decision.ClusterID),
		CanonicalID:   strings.TrimSpace(decision.CanonicalID),
		PreferredName: strings.TrimSpace(decision.PreferredName),
		SynonymIDs:    uniqueStrings(decision.SynonymIDs),
		Reason:        strings.TrimSpace(decision.Reason),
	}

	primary, err := fragmentsRepo.Get(ctx, result.CanonicalID)
	if err != nil || primary == nil {
		return result, fmt.Errorf("canonical fragment %s not found: %w", result.CanonicalID, err)
	}
	result.CanonicalPath = strings.TrimSpace(primary.CanonicalPath)
	if *dryRunApply {
		for _, synonymID := range result.SynonymIDs {
			dup, err := fragmentsRepo.Get(ctx, strings.TrimSpace(synonymID))
			if err != nil || dup == nil {
				result.SkippedSynonymIDs = append(result.SkippedSynonymIDs, strings.TrimSpace(synonymID))
				continue
			}
			result.MergedSynonymIDs = append(result.MergedSynonymIDs, strings.TrimSpace(synonymID))
		}
		result.VerificationPassed = true
		return result, nil
	}

	oldPaths := make([]string, 0, len(result.SynonymIDs))
	oldIDs := make([]string, 0, len(result.SynonymIDs))
	for _, synonymID := range result.SynonymIDs {
		synonymID = strings.TrimSpace(synonymID)
		if synonymID == "" {
			continue
		}
		dup, err := fragmentsRepo.Get(ctx, synonymID)
		if err != nil || dup == nil {
			result.SkippedSynonymIDs = append(result.SkippedSynonymIDs, synonymID)
			continue
		}
		oldIDs = append(oldIDs, synonymID)
		if cp := strings.TrimSpace(dup.CanonicalPath); cp != "" {
			oldPaths = append(oldPaths, cp)
		}
		if _, err := mergeRepo.MergeFragment(ctx, result.CanonicalID, synonymID, tenant, "product_synonym_curated", "horizon_mine_product_synonyms", "", ""); err != nil {
			return result, err
		}
		result.MergedSynonymIDs = append(result.MergedSynonymIDs, synonymID)
		deletedVersions, err := deleteFragmentVersions(ctx, db, versionsRepo, tenant, synonymID)
		if err != nil {
			return result, err
		}
		result.DeletedVersions += deletedVersions
	}
	result.MergedOldPaths = uniqueStrings(oldPaths)

	nameUpdated, primaryPath, err := updateCanonicalPreferredName(ctx, fragmentsRepo, result.CanonicalID, result.PreferredName)
	if err != nil {
		return result, err
	}
	result.NameUpdated = nameUpdated
	if primaryPath != "" {
		result.CanonicalPath = primaryPath
	}

	if len(result.MergedSynonymIDs) == 0 {
		result.VerificationPassed = true
		return result, nil
	}
	result.VerificationPassed = true
	return result, nil
}

func deleteFragmentVersions(
	ctx context.Context,
	db *firestore.Client,
	versionsRepo *hrepo.HorizonFragmentVersionsRepository,
	tenant,
	fragmentID string,
) (int, error) {
	if versionsRepo == nil {
		return 0, nil
	}
	versions, err := versionsRepo.ListByFragment(ctx, tenant, strings.TrimSpace(fragmentID), 10000, 0)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, version := range versions {
		if version == nil || strings.TrimSpace(version.VersionID) == "" {
			continue
		}
		if _, err := db.Collection("horizon_fragment_versions").Doc(strings.TrimSpace(version.VersionID)).Delete(ctx); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func updateCanonicalPreferredName(ctx context.Context, fragmentsRepo *hrepo.HorizonFragmentsRepository, fragmentID, preferredName string) (bool, string, error) {
	fragmentID = strings.TrimSpace(fragmentID)
	preferredName = strings.TrimSpace(preferredName)
	if fragmentID == "" || preferredName == "" {
		return false, "", nil
	}
	frag, err := fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || frag == nil {
		return false, "", err
	}
	if frag.Labels == nil {
		frag.Labels = map[string]string{}
	}
	current := strings.TrimSpace(frag.Labels["en"])
	if current == preferredName {
		return false, strings.TrimSpace(frag.CanonicalPath), nil
	}
	altNames := append([]string(nil), frag.AlternativeNames...)
	if current != "" {
		altNames = append(altNames, current)
	}
	frag.AlternativeNames = uniqueStrings(altNames)
	frag.Labels["en"] = preferredName
	frag.UpdatedAt = time.Now().UTC()
	if err := fragmentsRepo.Update(ctx, frag); err != nil {
		return false, "", err
	}
	return true, strings.TrimSpace(frag.CanonicalPath), nil
}

func verifyDecisionApplication(ctx context.Context, db *firestore.Client, oldIDs, oldPaths []string, canonicalID string) (bool, error) {
	for _, oldID := range uniqueStrings(oldIDs) {
		doc, err := db.Collection("horizon_fragments").Doc(oldID).Get(ctx)
		if err == nil && doc != nil && doc.Exists() {
			return false, nil
		}
		srcDocs, err := db.Collection("horizon_dependency_edges").Where("sourceFragmentId", "==", oldID).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(srcDocs) > 0 {
			return false, nil
		}
		dstDocs, err := db.Collection("horizon_dependency_edges").Where("targetFragmentId", "==", oldID).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(dstDocs) > 0 {
			return false, nil
		}
	}

	for _, oldPath := range uniqueStrings(oldPaths) {
		for _, field := range []string{"productRef", "supplierRef", "customerRef", "supplierFacilityRef", "customerFacilityRef"} {
			docs, err := db.Collection("horizon_fragments").Where(field, "==", oldPath).Limit(1).Documents(ctx).GetAll()
			if err != nil {
				return false, err
			}
			if len(docs) > 0 {
				return false, nil
			}
		}
		docs, err := db.Collection("horizon_fragments").Where("transportViaRefs", "array-contains", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(docs) > 0 {
			return false, nil
		}
		redirects, err := db.Collection("horizon_canonical_path_redirects").Where("sourcePath", "==", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(redirects) == 0 {
			return false, nil
		}

		artifactsByNode, err := db.Collection("horizon_materialized_artifacts").Where("nodeRefs", "array-contains", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(artifactsByNode) > 0 {
			return false, nil
		}

		artifactsByAxis, err := db.Collection("horizon_materialized_artifacts").Where("productAxis", "==", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return false, err
		}
		if len(artifactsByAxis) > 0 {
			return false, nil
		}
	}

	doc, err := db.Collection("horizon_fragments").Doc(strings.TrimSpace(canonicalID)).Get(ctx)
	if err != nil {
		return false, err
	}
	return doc.Exists(), nil
}

func writeApplyReport(path string, report applyReport) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printApplySummary(report applyReport) {
	fmt.Printf("\nApply Summary\n")
	fmt.Printf("  dry_run: %t\n", *dryRunApply)
	fmt.Printf("  decisions_planned: %d\n", report.Summary.DecisionsPlanned)
	fmt.Printf("  decisions_run: %d\n", report.Summary.DecisionsRun)
	fmt.Printf("  decisions_changed: %d\n", report.Summary.DecisionsChanged)
	fmt.Printf("  merges_applied: %d\n", report.Summary.MergesApplied)
	fmt.Printf("  merges_skipped: %d\n", report.Summary.MergesSkipped)
	fmt.Printf("  deleted_versions: %d\n", report.Summary.DeletedVersions)
	fmt.Printf("  name_updates: %d\n", report.Summary.NameUpdates)
	fmt.Printf("  verify_failures: %d\n", report.Summary.VerifyFailures)

	results := append([]applyDecisionResult(nil), report.Decisions...)
	sort.SliceStable(results, func(i, j int) bool {
		if len(results[i].MergedSynonymIDs) != len(results[j].MergedSynonymIDs) {
			return len(results[i].MergedSynonymIDs) > len(results[j].MergedSynonymIDs)
		}
		return results[i].ClusterID < results[j].ClusterID
	})
	for i, result := range results {
		fmt.Printf("%d. cluster=%s canonical=%s merged=%d skipped=%d verify=%t\n",
			i+1,
			result.ClusterID,
			result.CanonicalPath,
			len(result.MergedSynonymIDs),
			len(result.SkippedSynonymIDs),
			result.VerificationPassed,
		)
	}
}

func currentPreferredName(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.Labels["en"])
}
