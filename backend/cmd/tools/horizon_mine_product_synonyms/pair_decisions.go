package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type synonymPairDoc struct {
	CanonicalFragmentID string
	CanonicalPath       string
	CanonicalName       string
	SynonymFragmentID   string
	SynonymPath         string
	SynonymName         string
}

type pairDecisionStats struct {
	PairDocsLoaded       int
	CanonicalChains      int
	CycleComponents      int
	DecisionsBuilt       int
	SynonymsPlanned      int
	MissingCanonicals    int
	MissingSynonyms      int
	SkippedEmptyDecision int
}

type currentFragmentInfo struct {
	ID            string
	CanonicalPath string
	PreferredName string
}

func runPairDecisionsMode(ctx context.Context, db *firestore.Client) error {
	pairs, err := loadActiveSynonymPairDocs(ctx, db)
	if err != nil {
		return err
	}
	prefetched, err := prefetchCurrentFragmentInfos(ctx, db, collectPairDocFragmentIDs(pairs))
	if err != nil {
		return err
	}
	decisionSet, stats, err := buildDecisionSetFromPairDocs(ctx, nil, pairs, prefetched)
	if err != nil {
		return err
	}
	printPairDecisionSummary(stats)
	if path := strings.TrimSpace(*pairsDecisionsOut); path != "" {
		if err := writeDecisionSet(path, decisionSet); err != nil {
			return err
		}
		fmt.Printf("  decisions_out: %s\n", path)
	}
	return nil
}

func loadActiveSynonymPairDocs(ctx context.Context, db *firestore.Client) ([]synonymPairDoc, error) {
	if db == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	iter := db.Collection(strings.TrimSpace(*pairCollection)).Documents(ctx)
	defer iter.Stop()

	out := make([]synonymPairDoc, 0, 512)
	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		data := doc.Data()
		if strings.TrimSpace(stringValue(data["tenantId"])) != tenant {
			continue
		}
		if !boolValue(data["active"]) {
			continue
		}
		item := synonymPairDoc{
			CanonicalFragmentID: strings.TrimSpace(stringValue(data["canonicalFragmentId"])),
			CanonicalPath:       strings.TrimSpace(stringValue(data["canonicalPath"])),
			CanonicalName:       strings.TrimSpace(stringValue(data["canonicalName"])),
			SynonymFragmentID:   strings.TrimSpace(stringValue(data["synonymFragmentId"])),
			SynonymPath:         strings.TrimSpace(stringValue(data["synonymPath"])),
			SynonymName:         strings.TrimSpace(stringValue(data["synonymName"])),
		}
		if item.CanonicalFragmentID == "" || item.SynonymFragmentID == "" || item.CanonicalFragmentID == item.SynonymFragmentID {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func buildDecisionSetFromPairDocs(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	pairs []synonymPairDoc,
	prefetched map[string]cachedFragmentInfo,
) (curatedDecisionSet, pairDecisionStats, error) {
	stats := pairDecisionStats{PairDocsLoaded: len(pairs)}
	parent := map[string]string{}
	meta := map[string]currentFragmentInfo{}
	inboundCount := map[string]int{}

	for _, pair := range pairs {
		if prev, ok := parent[pair.SynonymFragmentID]; ok && prev != pair.CanonicalFragmentID {
			return curatedDecisionSet{}, stats, fmt.Errorf("conflicting canonical assignment for synonym %s: %s vs %s", pair.SynonymFragmentID, prev, pair.CanonicalFragmentID)
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
				stats.CycleComponents++
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
		if root != id {
			stats.CanonicalChains++
		}
		return root, nil
	}

	grouped := map[string]map[string]struct{}{}
	for synonymID := range parent {
		root, err := resolveRoot(synonymID)
		if err != nil {
			return curatedDecisionSet{}, stats, err
		}
		if root == "" || root == synonymID {
			continue
		}
		if grouped[root] == nil {
			grouped[root] = map[string]struct{}{}
		}
		grouped[root][synonymID] = struct{}{}
	}

	decisions := make([]curatedAcceptedDecision, 0, len(grouped))
	for canonicalID, synonymSet := range grouped {
		canonicalInfo, ok := lookupCurrentFragmentInfo(ctx, fragmentsRepo, canonicalID, meta[canonicalID], prefetched)
		if !ok {
			stats.MissingCanonicals++
			continue
		}
		synonymIDs := make([]string, 0, len(synonymSet))
		for synonymID := range synonymSet {
			if _, exists := lookupCurrentFragmentInfo(ctx, fragmentsRepo, synonymID, meta[synonymID], prefetched); !exists {
				stats.MissingSynonyms++
				continue
			}
			if synonymID == canonicalID {
				continue
			}
			synonymIDs = append(synonymIDs, synonymID)
		}
		synonymIDs = uniqueStrings(synonymIDs)
		sort.Strings(synonymIDs)
		if len(synonymIDs) == 0 {
			stats.SkippedEmptyDecision++
			continue
		}
		decisions = append(decisions, curatedAcceptedDecision{
			ClusterID:     fmt.Sprintf("pair-canonical:%s", canonicalID),
			CanonicalID:   canonicalID,
			PreferredName: canonicalInfo.PreferredName,
			SynonymIDs:    synonymIDs,
			Reason:        "built_from_active_synonym_pairs",
		})
		stats.DecisionsBuilt++
		stats.SynonymsPlanned += len(synonymIDs)
	}

	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].PreferredName != decisions[j].PreferredName {
			return decisions[i].PreferredName < decisions[j].PreferredName
		}
		return decisions[i].CanonicalID < decisions[j].CanonicalID
	})

	set := curatedDecisionSet{
		SchemaVersion: "v1",
		ProjectID:     strings.TrimSpace(*projectID),
		TenantID:      strings.TrimSpace(*tenantID),
		Accepted:      decisions,
	}
	return set, stats, nil
}

func lookupCurrentFragmentInfo(ctx context.Context, fragmentsRepo *hrepo.HorizonFragmentsRepository, fragmentID string, fallback currentFragmentInfo, prefetched map[string]cachedFragmentInfo) (currentFragmentInfo, bool) {
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return currentFragmentInfo{}, false
	}
	if cached, ok := prefetched[fragmentID]; ok {
		return cached.Info, cached.Exists
	}
	if cached, ok := getCachedFragmentInfo(ctx, fragmentID); ok {
		return cached.Info, cached.Exists
	}
	if fragmentsRepo == nil {
		info := fallback
		setCachedFragmentInfo(ctx, fragmentID, cachedFragmentInfo{Exists: true, Info: info})
		return info, true
	}
	frag, err := fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || frag == nil {
		setCachedFragmentInfo(ctx, fragmentID, cachedFragmentInfo{Exists: false})
		return currentFragmentInfo{}, false
	}
	info := currentFragmentInfo{
		ID:            fragmentID,
		CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
		PreferredName: currentPreferredName(frag),
	}
	if info.PreferredName == "" {
		info.PreferredName = strings.TrimSpace(fallback.PreferredName)
	}
	if info.CanonicalPath == "" {
		info.CanonicalPath = strings.TrimSpace(fallback.CanonicalPath)
	}
	setCachedFragmentInfo(ctx, fragmentID, cachedFragmentInfo{Exists: true, Info: info})
	return info, true
}

func collectPairDocFragmentIDs(pairs []synonymPairDoc) []string {
	ids := make([]string, 0, len(pairs)*2)
	for _, pair := range pairs {
		if pair.CanonicalFragmentID != "" {
			ids = append(ids, pair.CanonicalFragmentID)
		}
		if pair.SynonymFragmentID != "" {
			ids = append(ids, pair.SynonymFragmentID)
		}
	}
	return uniqueStrings(ids)
}

func prefetchCurrentFragmentInfos(ctx context.Context, db *firestore.Client, ids []string) (map[string]cachedFragmentInfo, error) {
	out := make(map[string]cachedFragmentInfo, len(ids))
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return out, nil
	}

	missing := make([]string, 0, len(ids))
	if toolRedisClient != nil {
		keys := make([]string, 0, len(ids))
		keyToID := make(map[string]string, len(ids))
		for _, id := range ids {
			key := fragmentInfoRedisKey(id)
			keys = append(keys, key)
			keyToID[key] = id
		}
		rctx, cancel := context.WithTimeout(ctx, productSynonymRedisTimeout)
		values, err := toolRedisClient.MGet(rctx, keys...).Result()
		cancel()
		if err == nil {
			for idx, raw := range values {
				if raw == nil {
					continue
				}
				var payload []byte
				switch t := raw.(type) {
				case string:
					payload = []byte(t)
				case []byte:
					payload = t
				default:
					continue
				}
				var cached cachedFragmentInfo
				if err := json.Unmarshal(payload, &cached); err != nil {
					continue
				}
				out[ids[idx]] = cached
			}
		}
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			missing = append(missing, id)
		}
	}
	if db == nil || len(missing) == 0 {
		return out, nil
	}

	const chunkSize = 300
	for start := 0; start < len(missing); start += chunkSize {
		end := start + chunkSize
		if end > len(missing) {
			end = len(missing)
		}
		chunk := missing[start:end]
		refs := make([]*firestore.DocumentRef, 0, len(chunk))
		for _, id := range chunk {
			refs = append(refs, db.Collection("horizon_fragments").Doc(id))
		}
		docs, err := db.GetAll(ctx, refs)
		if err != nil {
			return out, err
		}
		for idx, snap := range docs {
			id := chunk[idx]
			cached := cachedFragmentInfo{Exists: false}
			if snap != nil && snap.Exists() {
				info := currentFragmentInfo{
					ID:            id,
					CanonicalPath: strings.TrimSpace(stringValue(snap.Data()["canonicalPath"])),
				}
				if labelsRaw, ok := snap.Data()["labels"].(map[string]interface{}); ok {
					info.PreferredName = strings.TrimSpace(stringValue(labelsRaw["en"]))
				}
				cached = cachedFragmentInfo{Exists: true, Info: info}
			}
			out[id] = cached
			setCachedFragmentInfo(ctx, id, cached)
		}
	}
	return out, nil
}

func printPairDecisionSummary(stats pairDecisionStats) {
	fmt.Printf("\nPair Decision Summary\n")
	fmt.Printf("  pair_docs_loaded: %d\n", stats.PairDocsLoaded)
	fmt.Printf("  canonical_chains: %d\n", stats.CanonicalChains)
	fmt.Printf("  cycle_components: %d\n", stats.CycleComponents)
	fmt.Printf("  decisions_built: %d\n", stats.DecisionsBuilt)
	fmt.Printf("  synonyms_planned: %d\n", stats.SynonymsPlanned)
	fmt.Printf("  missing_canonicals: %d\n", stats.MissingCanonicals)
	fmt.Printf("  missing_synonyms: %d\n", stats.MissingSynonyms)
	fmt.Printf("  skipped_empty_decisions: %d\n", stats.SkippedEmptyDecision)
}

func chooseCycleRoot(ids []string, meta map[string]currentFragmentInfo, inboundCount map[string]int) string {
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return ""
	}
	sort.SliceStable(ids, func(i, j int) bool {
		left := ids[i]
		right := ids[j]
		if inboundCount[left] != inboundCount[right] {
			return inboundCount[left] > inboundCount[right]
		}
		leftPath := strings.TrimSpace(meta[left].CanonicalPath)
		rightPath := strings.TrimSpace(meta[right].CanonicalPath)
		leftAssembly := strings.Contains(leftPath, "/finished/mechanical/assembly/")
		rightAssembly := strings.Contains(rightPath, "/finished/mechanical/assembly/")
		if leftAssembly != rightAssembly {
			return !leftAssembly
		}
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return left < right
	})
	return ids[0]
}

func writeDecisionSet(path string, set curatedDecisionSet) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(set)
}
