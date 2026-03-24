package main

import (
	"context"
	"log"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/cmd/tools/internal/productsem"
)

type synonymPathMeta struct {
	Path       string
	Name       string
	FragmentID string
	Quality    int
}

type synonymDesiredDoc struct {
	CanonicalPath string
	CanonicalName string
	CanonicalID   string
	SynonymPath   string
	SynonymName   string
	SynonymID     string
	Reason        string
	Source        string
	Confidence    float64
	Score         float64
}

func backfillSemanticDedup(ctx context.Context, db *firestore.Client) (int, int, error) {
	existing, err := loadActiveSynonymPairDocs(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	existingSnaps, err := loadActiveSynonymPairSnapshots(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	desired := buildDesiredSynonymDocs(existing, nil)
	deactivated, err := syncDesiredSynonymDocs(ctx, db, existingSnaps, desired)
	if err != nil {
		return 0, 0, err
	}
	return len(desired), deactivated, nil
}

func loadActiveSynonymPairSnapshots(ctx context.Context, db *firestore.Client) ([]*firestore.DocumentSnapshot, error) {
	col := db.Collection(strings.TrimSpace(*pairCollection))
	iter := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).Where("active", "==", true).Documents(ctx)
	defer iter.Stop()
	out := make([]*firestore.DocumentSnapshot, 0, 256)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

func buildDesiredSynonymDocs(existing []synonymPairDoc, reviewed []pairReviewEntry) map[string]synonymDesiredDoc {
	pathMeta := map[string]synonymPathMeta{}
	candidates := make([]productsem.PathCandidate, 0, len(existing)*2+len(reviewed)*2)
	addMeta := func(path, name, fragmentID string, quality int) {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/product/") {
			return
		}
		current, ok := pathMeta[path]
		next := synonymPathMeta{
			Path:       path,
			Name:       firstNonEmpty(strings.TrimSpace(name), current.Name),
			FragmentID: firstNonEmpty(strings.TrimSpace(fragmentID), current.FragmentID),
			Quality:    quality,
		}
		if !ok || quality >= current.Quality {
			pathMeta[path] = next
		}
		candidates = append(candidates, productsem.PathCandidate{Path: path, Name: name, Quality: quality})
	}

	for _, doc := range existing {
		addMeta(doc.CanonicalPath, doc.CanonicalName, doc.CanonicalFragmentID, 100)
		addMeta(doc.SynonymPath, doc.SynonymName, doc.SynonymFragmentID, 80)
	}
	for _, pair := range reviewed {
		if !pair.Accepted {
			continue
		}
		addMeta(pair.CanonicalPath, pair.CanonicalName, pair.CanonicalID, max(120, pair.CanonicalQuality))
		addMeta(pair.SynonymPath, pair.SynonymName, pair.SynonymID, max(90, pair.SynonymQuality))
	}

	winnerMap := productsem.BuildWinnerMap(candidates)
	desired := map[string]synonymDesiredDoc{}

	for _, group := range productsem.BuildGroups(candidates) {
		if len(group.Members) < 2 {
			continue
		}
		winnerMeta := bestSynonymMeta(pathMeta[group.Winner.Path], group.Winner)
		for _, member := range group.Members {
			memberPath := strings.TrimSpace(member.Path)
			if memberPath == "" || memberPath == winnerMeta.Path {
				continue
			}
			memberMeta := bestSynonymMeta(pathMeta[memberPath], member)
			doc := synonymDesiredDoc{
				CanonicalPath: winnerMeta.Path,
				CanonicalName: firstNonEmpty(winnerMeta.Name, humanizePath(winnerMeta.Path)),
				CanonicalID:   winnerMeta.FragmentID,
				SynonymPath:   memberMeta.Path,
				SynonymName:   firstNonEmpty(memberMeta.Name, humanizePath(memberMeta.Path)),
				SynonymID:     memberMeta.FragmentID,
				Reason:        "semantic_canonical_dedup",
				Source:        "semantic_dedup_backfill",
				Confidence:    1,
				Score:         float64(winnerMeta.Quality*100 + memberMeta.Quality),
			}
			desired[synonymPairDocID(doc.CanonicalPath, doc.SynonymPath)] = doc
		}
	}

	mergePair := func(canonicalPath, canonicalName, canonicalID, synonymPath, synonymName, synonymID, reason, source string, confidence float64) {
		canonicalPath = firstNonEmpty(winnerMap[strings.TrimSpace(canonicalPath)], strings.TrimSpace(canonicalPath))
		synonymPath = firstNonEmpty(winnerMap[strings.TrimSpace(synonymPath)], strings.TrimSpace(synonymPath))
		if canonicalPath == "" || synonymPath == "" || canonicalPath == synonymPath {
			return
		}
		docID := synonymPairDocID(canonicalPath, synonymPath)
		doc := synonymDesiredDoc{
			CanonicalPath: canonicalPath,
			CanonicalName: firstNonEmpty(canonicalName, pathMeta[canonicalPath].Name, humanizePath(canonicalPath)),
			CanonicalID:   firstNonEmpty(canonicalID, pathMeta[canonicalPath].FragmentID),
			SynonymPath:   synonymPath,
			SynonymName:   firstNonEmpty(synonymName, pathMeta[synonymPath].Name, humanizePath(synonymPath)),
			SynonymID:     firstNonEmpty(synonymID, pathMeta[synonymPath].FragmentID),
			Reason:        firstNonEmpty(reason, "semantic_dedup"),
			Source:        firstNonEmpty(source, "semantic_dedup_backfill"),
			Confidence:    confidence,
			Score:         confidence*1000 + float64(pathMeta[canonicalPath].Quality),
		}
		current, ok := desired[docID]
		if !ok || betterSynonymDesiredDoc(doc, current) {
			desired[docID] = doc
		}
	}

	for _, doc := range existing {
		mergePair(
			doc.CanonicalPath,
			doc.CanonicalName,
			doc.CanonicalFragmentID,
			doc.SynonymPath,
			doc.SynonymName,
			doc.SynonymFragmentID,
			"",
			"semantic_dedup_existing_doc",
			1,
		)
	}
	for _, pair := range reviewed {
		if !pair.Accepted {
			continue
		}
		mergePair(pair.CanonicalPath, pair.CanonicalName, pair.CanonicalID, pair.SynonymPath, pair.SynonymName, pair.SynonymID, pair.Reason, pair.DecisionSource, pair.Confidence)
	}

	return collapseDesiredSynonymDocs(desired)
}

func syncDesiredSynonymDocs(ctx context.Context, db *firestore.Client, existing []*firestore.DocumentSnapshot, desired map[string]synonymDesiredDoc) (int, error) {
	col := db.Collection(strings.TrimSpace(*pairCollection))
	now := firestore.ServerTimestamp
	deactivated := 0
	log.Printf("semantic synonym dedup: existing_active=%d desired_active=%d", len(existing), len(desired))
	for _, snap := range existing {
		if snap == nil {
			continue
		}
		if _, ok := desired[snap.Ref.ID]; ok {
			continue
		}
		if _, err := snap.Ref.Set(ctx, map[string]interface{}{
			"tenantId":   strings.TrimSpace(*tenantID),
			"active":     false,
			"updatedAt":  now,
			"reason":     "semantic_canonical_dedup_deactivated",
			"updatedBy":  "semantic_dedup_backfill",
		}, firestore.MergeAll); err != nil {
			return deactivated, err
		}
		deactivated++
	}
	written := 0
	for docID, doc := range desired {
		record := map[string]interface{}{
			"tenantId":            strings.TrimSpace(*tenantID),
			"canonicalFragmentId": doc.CanonicalID,
			"canonicalPath":       doc.CanonicalPath,
			"canonicalName":       doc.CanonicalName,
			"synonymFragmentId":   doc.SynonymID,
			"synonymPath":         doc.SynonymPath,
			"synonymName":         doc.SynonymName,
			"normalizedSynonym":   normalizeProductAliasKey(doc.SynonymName),
			"decisionSource":      doc.Source,
			"confidence":          doc.Confidence,
			"reason":              doc.Reason,
			"active":              true,
			"updatedAt":           now,
		}
		if _, err := col.Doc(docID).Set(ctx, record, firestore.MergeAll); err != nil {
			return deactivated, err
		}
		if toolProductDictRuntime != nil {
			if err := toolProductDictRuntime.SyncSynonymPairWrite(ctx, strings.TrimSpace(*tenantID), doc.CanonicalPath, doc.SynonymPath, true); err != nil {
				return deactivated, err
			}
		}
		written++
		if written%100 == 0 {
			log.Printf("semantic synonym dedup: upserted=%d/%d", written, len(desired))
		}
	}
	log.Printf("semantic synonym dedup: deactivated=%d upserted=%d", deactivated, written)
	return deactivated, nil
}

func collapseDesiredSynonymDocs(in map[string]synonymDesiredDoc) map[string]synonymDesiredDoc {
	ordered := make([]synonymDesiredDoc, 0, len(in))
	for _, doc := range in {
		ordered = append(ordered, doc)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return betterSynonymDesiredDoc(ordered[i], ordered[j])
	})
	usedPaths := map[string]struct{}{}
	usedIDs := map[string]struct{}{}
	out := make(map[string]synonymDesiredDoc, len(ordered))
	for _, doc := range ordered {
		pathKey := strings.TrimSpace(doc.SynonymPath)
		idKey := strings.TrimSpace(doc.SynonymID)
		if pathKey == "" && idKey == "" {
			continue
		}
		if pathKey != "" {
			if _, ok := usedPaths[pathKey]; ok {
				continue
			}
		}
		if idKey != "" {
			if _, ok := usedIDs[idKey]; ok {
				continue
			}
		}
		out[synonymPairDocID(doc.CanonicalPath, doc.SynonymPath)] = doc
		if pathKey != "" {
			usedPaths[pathKey] = struct{}{}
		}
		if idKey != "" {
			usedIDs[idKey] = struct{}{}
		}
	}
	return out
}

func betterSynonymDesiredDoc(left, right synonymDesiredDoc) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.Confidence != right.Confidence {
		return left.Confidence > right.Confidence
	}
	if left.CanonicalPath != right.CanonicalPath {
		return len(left.CanonicalPath) < len(right.CanonicalPath)
	}
	return left.CanonicalName < right.CanonicalName
}

func bestSynonymMeta(meta synonymPathMeta, candidate productsem.PathCandidate) synonymPathMeta {
	if strings.TrimSpace(meta.Path) == "" {
		meta.Path = strings.TrimSpace(candidate.Path)
	}
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = strings.TrimSpace(candidate.Name)
	}
	if candidate.Quality > meta.Quality {
		meta.Quality = candidate.Quality
	}
	return meta
}

func humanizePath(path string) string {
	segment := strings.TrimSpace(path)
	if idx := strings.LastIndex(segment, "/"); idx >= 0 && idx+1 < len(segment) {
		segment = segment[idx+1:]
	}
	segment = strings.ReplaceAll(segment, "-", " ")
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return path
	}
	parts := strings.Fields(segment)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
