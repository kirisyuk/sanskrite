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
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type repairSupplyLinksSummary struct {
	PairDocsLoaded      int `json:"pairDocsLoaded"`
	PathRewritesKnown   int `json:"pathRewritesKnown"`
	StalePathsBefore    int `json:"stalePathsBefore"`
	PathRewritesPlanned int `json:"pathRewritesPlanned"`
	SupplyLinksUpdated  int `json:"supplyLinksUpdated"`
	MaterializedUpdated int `json:"materializedUpdated"`
	VerifyFailures      int `json:"verifyFailures"`
	RemainingStalePaths int `json:"remainingStalePaths"`
}

type repairSupplyLinksReport struct {
	GeneratedAt string                   `json:"generatedAt"`
	ProjectID   string                   `json:"projectId"`
	TenantID    string                   `json:"tenantId"`
	Summary     repairSupplyLinksSummary `json:"summary"`
	Failures    []string                 `json:"failures,omitempty"`
}

type repairWorklist struct {
	GeneratedAt string            `json:"generatedAt"`
	ProjectID   string            `json:"projectId"`
	TenantID    string            `json:"tenantId"`
	Paths       map[string]string `json:"paths"`
}

func runRepairSupplyLinks(ctx context.Context, db *firestore.Client) error {
	log.Printf("repair-supply-links: loading active synonym pair docs")
	pairs, err := loadActiveSynonymPairDocs(ctx, db)
	if err != nil {
		return err
	}
	log.Printf("repair-supply-links: loaded pair docs=%d", len(pairs))
	log.Printf("repair-supply-links: prefetching current fragment infos")
	prefetched, err := prefetchCurrentFragmentInfos(ctx, db, collectPairDocFragmentIDs(pairs))
	if err != nil {
		return err
	}
	log.Printf("repair-supply-links: prefetched fragment infos=%d", len(prefetched))
	log.Printf("repair-supply-links: building path rewrite map")
	pathRewrites, err := buildPathRewritesFromPairs(ctx, nil, pairs, prefetched)
	if err != nil {
		return err
	}
	log.Printf("repair-supply-links: known path rewrites=%d", len(pathRewrites))
	staleBefore := map[string]string{}
	staleFailures := []string(nil)
	scanExhausted := true
	plannedRewrites := map[string]string{}
	worklistLoaded := false
	if path := strings.TrimSpace(*repairWorklistIn); path != "" {
		worklist, err := loadRepairWorklist(path)
		if err != nil {
			return err
		}
		worklistLoaded = true
		for oldPath, newPath := range worklist.Paths {
			if canonical := strings.TrimSpace(pathRewrites[oldPath]); canonical != "" && canonical == strings.TrimSpace(newPath) {
				staleBefore[oldPath] = canonical
			}
		}
		plannedRewrites = limitRepairWorklist(staleBefore, *applyLimit)
		log.Printf("repair-supply-links: loaded worklist paths=%d", len(plannedRewrites))
	} else {
		staleScanLimit := 0
		if *applyLimit > 0 {
			staleScanLimit = *applyLimit
		}
		log.Printf("repair-supply-links: scanning stale paths stopAfter=%d", staleScanLimit)
		var err error
		staleBefore, staleFailures, scanExhausted, err = detectStalePathRewrites(ctx, db, pathRewrites, staleScanLimit)
		if err != nil {
			return err
		}
		plannedRewrites = staleBefore
		if path := strings.TrimSpace(*repairWorklistOut); path != "" {
			if err := writeRepairWorklist(path, repairWorklist{
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
				ProjectID:   strings.TrimSpace(*projectID),
				TenantID:    tenantOrDefault(),
				Paths:       staleBefore,
			}); err != nil {
				return err
			}
			log.Printf("repair-supply-links: wrote worklist paths=%d file=%s", len(staleBefore), path)
		}
	}
	log.Printf("repair-supply-links: stale paths before=%d scanExhausted=%t", len(staleBefore), scanExhausted)

	if *dryRunApply {
		report := repairSupplyLinksReport{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			ProjectID:   strings.TrimSpace(*projectID),
			TenantID:    tenantOrDefault(),
			Summary: repairSupplyLinksSummary{
				PairDocsLoaded:      len(pairs),
				PathRewritesKnown:   len(pathRewrites),
				StalePathsBefore:    len(staleBefore),
				PathRewritesPlanned: len(plannedRewrites),
				SupplyLinksUpdated:  0,
				MaterializedUpdated: 0,
				VerifyFailures:      0,
				RemainingStalePaths: len(staleBefore),
			},
			Failures: chooseRepairFailurePreview(staleFailures, nil, scanExhausted),
		}
		printRepairSupplyLinksSummary(report)
		if path := strings.TrimSpace(*applyReportPath); path != "" {
			if err := writeRepairSupplyLinksReport(path, report); err != nil {
				return err
			}
			log.Printf("wrote repair report: %s", path)
		}
		return nil
	}

	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	log.Printf("repair-supply-links: rewriting downstream refs for planned paths=%d", len(plannedRewrites))
	repairStats, err := applySinglePassRepair(ctx, db, tenant, plannedRewrites)
	if err != nil {
		return err
	}
	log.Printf("repair-supply-links: updated supply links=%d materialized=%d", repairStats.SupplyLinksUpdated, repairStats.MaterializedUpdated)
	log.Printf("repair-supply-links: verifying rewritten paths=%d", len(plannedRewrites))
	remainingStale, failures, _, err := detectStalePathRewrites(ctx, db, plannedRewrites, 0)
	if err != nil {
		return err
	}
	log.Printf("repair-supply-links: remaining stale paths=%d verifyFailures=%d", len(remainingStale), len(failures))

	report := repairSupplyLinksReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ProjectID:   strings.TrimSpace(*projectID),
		TenantID:    tenant,
		Summary: repairSupplyLinksSummary{
			PairDocsLoaded:      len(pairs),
			PathRewritesKnown:   len(pathRewrites),
			StalePathsBefore:    len(staleBefore),
			PathRewritesPlanned: len(plannedRewrites),
			SupplyLinksUpdated:  repairStats.SupplyLinksUpdated,
			MaterializedUpdated: repairStats.MaterializedUpdated,
			VerifyFailures:      len(failures),
			RemainingStalePaths: len(remainingStale),
		},
		Failures: chooseRepairFailurePreview(staleFailures, failures, scanExhausted),
	}

	printRepairSupplyLinksSummary(report)
	if worklistLoaded {
		nextWorklist := buildNextRepairWorklist(staleBefore, plannedRewrites, remainingStale)
		targetPath := strings.TrimSpace(*repairWorklistOut)
		if targetPath == "" {
			targetPath = strings.TrimSpace(*repairWorklistIn)
		}
		if targetPath != "" {
			if err := writeRepairWorklist(targetPath, repairWorklist{
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
				ProjectID:   strings.TrimSpace(*projectID),
				TenantID:    tenant,
				Paths:       nextWorklist,
			}); err != nil {
				return err
			}
			log.Printf("repair-supply-links: wrote next worklist remaining=%d file=%s", len(nextWorklist), targetPath)
		}
	}
	if path := strings.TrimSpace(*applyReportPath); path != "" {
		if err := writeRepairSupplyLinksReport(path, report); err != nil {
			return err
		}
		log.Printf("wrote repair report: %s", path)
	}
	return nil
}

func buildPathRewritesFromPairs(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	pairs []synonymPairDoc,
	prefetched map[string]cachedFragmentInfo,
) (map[string]string, error) {
	parent := map[string]string{}
	meta := map[string]currentFragmentInfo{}
	inboundCount := map[string]int{}

	for _, pair := range pairs {
		if prev, ok := parent[pair.SynonymFragmentID]; ok && prev != pair.CanonicalFragmentID {
			return nil, fmt.Errorf("conflicting canonical assignment for synonym %s: %s vs %s", pair.SynonymFragmentID, prev, pair.CanonicalFragmentID)
		}
		parent[pair.SynonymFragmentID] = pair.CanonicalFragmentID
		inboundCount[pair.CanonicalFragmentID]++
		if pair.CanonicalFragmentID != "" {
			meta[pair.CanonicalFragmentID] = currentFragmentInfo{
				ID:            pair.CanonicalFragmentID,
				CanonicalPath: pair.CanonicalPath,
				PreferredName: pair.CanonicalName,
			}
		}
		if pair.SynonymFragmentID != "" {
			if _, ok := meta[pair.SynonymFragmentID]; !ok {
				meta[pair.SynonymFragmentID] = currentFragmentInfo{
					ID:            pair.SynonymFragmentID,
					CanonicalPath: pair.SynonymPath,
					PreferredName: pair.SynonymName,
				}
			}
		}
	}

	rootCache := map[string]string{}
	resolveRoot := func(id string) (string, error) {
		id = strings.TrimSpace(id)
		if id == "" {
			return "", nil
		}
		if root, ok := rootCache[id]; ok {
			return root, nil
		}
		path := make([]string, 0, 8)
		seenAt := map[string]int{}
		cur := id
		root := ""
		for {
			if cached, ok := rootCache[cur]; ok {
				root = cached
				break
			}
			if idx, ok := seenAt[cur]; ok {
				cycleNodes := append([]string(nil), path[idx:]...)
				root = chooseCycleRoot(cycleNodes, meta, inboundCount)
				for _, cycleNode := range cycleNodes {
					rootCache[cycleNode] = root
				}
				break
			}
			seenAt[cur] = len(path)
			path = append(path, cur)
			parentID, ok := parent[cur]
			if !ok || strings.TrimSpace(parentID) == "" {
				root = cur
				break
			}
			cur = strings.TrimSpace(parentID)
		}
		if root == "" {
			root = id
		}
		for _, node := range path {
			if _, ok := rootCache[node]; !ok {
				rootCache[node] = root
			}
		}
		return root, nil
	}

	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		synonymID := strings.TrimSpace(pair.SynonymFragmentID)
		synonymPath := strings.TrimSpace(pair.SynonymPath)
		if synonymID == "" || synonymPath == "" {
			continue
		}
		rootID, err := resolveRoot(synonymID)
		if err != nil {
			return nil, err
		}
		if rootID == "" || rootID == synonymID {
			continue
		}
		rootInfo, ok := lookupCurrentFragmentInfo(ctx, fragmentsRepo, rootID, meta[rootID], prefetched)
		if !ok {
			rootInfo = meta[rootID]
		}
		targetPath := strings.TrimSpace(rootInfo.CanonicalPath)
		if targetPath == "" || targetPath == synonymPath {
			continue
		}
		if prev, exists := out[synonymPath]; exists && prev != targetPath {
			return nil, fmt.Errorf("conflicting rewrite target for path %s: %s vs %s", synonymPath, prev, targetPath)
		}
		out[synonymPath] = targetPath
	}
	return out, nil
}

func detectStalePathRewrites(ctx context.Context, db *firestore.Client, rewrites map[string]string, stopAfter int) (map[string]string, []string, bool, error) {
	if len(rewrites) == 0 {
		return nil, nil, true, nil
	}
	stale := make(map[string]string)
	failures := make([]string, 0, 16)
	oldPaths := make([]string, 0, len(rewrites))
	for oldPath := range rewrites {
		oldPaths = append(oldPaths, oldPath)
	}
	sort.Strings(oldPaths)
	for idx, oldPath := range oldPaths {
		if idx > 0 && idx%10 == 0 {
			log.Printf("repair stale-scan progress: scanned=%d stale=%d stopAfter=%d total=%d", idx, len(stale), stopAfter, len(oldPaths))
		}
		for _, field := range []string{"productRef", "supplierRef", "customerRef", "supplierFacilityRef", "customerFacilityRef"} {
			docs, err := db.Collection("horizon_fragments").Where(field, "==", oldPath).Limit(1).Documents(ctx).GetAll()
			if err != nil {
				return stale, failures, false, err
			}
			if len(docs) > 0 {
				stale[oldPath] = rewrites[oldPath]
				failures = append(failures, fmt.Sprintf("%s still referenced via %s", oldPath, field))
				break
			}
		}
		if stopAfter > 0 && len(stale) >= stopAfter {
			return stale, failures, false, nil
		}
		docs, err := db.Collection("horizon_fragments").Where("transportViaRefs", "array-contains", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return stale, failures, false, err
		}
		if len(docs) > 0 {
			stale[oldPath] = rewrites[oldPath]
			failures = append(failures, fmt.Sprintf("%s still referenced via transportViaRefs", oldPath))
			if stopAfter > 0 && len(stale) >= stopAfter {
				return stale, failures, false, nil
			}
			continue
		}
		artifactsByNode, err := db.Collection("horizon_materialized_artifacts").Where("nodeRefs", "array-contains", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return stale, failures, false, err
		}
		if len(artifactsByNode) > 0 {
			stale[oldPath] = rewrites[oldPath]
			failures = append(failures, fmt.Sprintf("%s still referenced via materialized.nodeRefs", oldPath))
			if stopAfter > 0 && len(stale) >= stopAfter {
				return stale, failures, false, nil
			}
			continue
		}
		artifactsByAxis, err := db.Collection("horizon_materialized_artifacts").Where("productAxis", "==", oldPath).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return stale, failures, false, err
		}
		if len(artifactsByAxis) > 0 {
			stale[oldPath] = rewrites[oldPath]
			failures = append(failures, fmt.Sprintf("%s still referenced via materialized.productAxis", oldPath))
			if stopAfter > 0 && len(stale) >= stopAfter {
				return stale, failures, false, nil
			}
		}
	}
	return stale, failures, true, nil
}

func chooseRepairFailurePreview(before, after []string, scanExhausted bool) []string {
	prefix := func(items []string, note string) []string {
		if note == "" || len(items) == 0 {
			return append([]string(nil), items...)
		}
		out := make([]string, 0, len(items)+1)
		out = append(out, note)
		out = append(out, items...)
		return out
	}
	switch {
	case len(after) > 0:
		return prefix(after, "")
	case len(before) > 0:
		note := ""
		if !scanExhausted {
			note = "scan stopped after reaching batch limit"
		}
		return prefix(before, note)
	default:
		return nil
	}
}

func loadRepairWorklist(path string) (repairWorklist, error) {
	var out repairWorklist
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.Paths == nil {
		out.Paths = map[string]string{}
	}
	return out, nil
}

func writeRepairWorklist(path string, worklist repairWorklist) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(worklist)
}

func tenantOrDefault() string {
	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		return "global"
	}
	return tenant
}

func limitRepairWorklist(worklist map[string]string, limit int) map[string]string {
	if len(worklist) == 0 || limit <= 0 || len(worklist) <= limit {
		return worklist
	}
	ordered := make([]string, 0, len(worklist))
	for oldPath := range worklist {
		ordered = append(ordered, oldPath)
	}
	sort.Strings(ordered)
	out := make(map[string]string, limit)
	for _, oldPath := range ordered[:limit] {
		out[oldPath] = worklist[oldPath]
	}
	return out
}

func buildNextRepairWorklist(all, planned, remaining map[string]string) map[string]string {
	out := make(map[string]string, len(all))
	for oldPath, newPath := range all {
		if _, wasPlanned := planned[oldPath]; !wasPlanned {
			out[oldPath] = newPath
		}
	}
	for oldPath, newPath := range remaining {
		out[oldPath] = newPath
	}
	return out
}

func writeRepairSupplyLinksReport(path string, report repairSupplyLinksReport) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printRepairSupplyLinksSummary(report repairSupplyLinksReport) {
	fmt.Printf("\nRepair Supply Links Summary\n")
	fmt.Printf("  pair_docs_loaded: %d\n", report.Summary.PairDocsLoaded)
	fmt.Printf("  path_rewrites_known: %d\n", report.Summary.PathRewritesKnown)
	fmt.Printf("  stale_paths_before: %d\n", report.Summary.StalePathsBefore)
	fmt.Printf("  path_rewrites_planned: %d\n", report.Summary.PathRewritesPlanned)
	fmt.Printf("  supply_links_updated: %d\n", report.Summary.SupplyLinksUpdated)
	fmt.Printf("  materialized_updated: %d\n", report.Summary.MaterializedUpdated)
	fmt.Printf("  verify_failures: %d\n", report.Summary.VerifyFailures)
	fmt.Printf("  remaining_stale_paths: %d\n", report.Summary.RemainingStalePaths)
	for i, failure := range report.Failures {
		fmt.Printf("%d. %s\n", i+1, failure)
	}
}
