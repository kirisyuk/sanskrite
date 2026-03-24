package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/cmd/tools/internal/productsem"
)

type transformPathMeta struct {
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

type transformDesiredDoc struct {
	DocID        string
	FromPath     string
	FromName     string
	FromID       string
	ToPath       string
	ToName       string
	ToID         string
	RelationKind string
	Reason       string
	Source       string
	Confidence   float64
	Active       bool
}

func backfillSemanticTransformDedup(ctx context.Context, db *firestore.Client) (int, int, error) {
	existing, err := loadActiveTransformPairDocs(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	desired, synonymDesired := buildDesiredTransformDocs(existing, nil)
	if _, err := syncDesiredSynonymDocsTransform(ctx, db, loadActiveSynonymPairDocsTransform(ctx, db), synonymDesired); err != nil {
		return 0, 0, err
	}
	deactivated, err := syncDesiredTransformDocs(ctx, db, existing, desired)
	if err != nil {
		return 0, 0, err
	}
	return len(desired), deactivated, nil
}

func loadActiveTransformPairDocs(ctx context.Context, db *firestore.Client) ([]*firestore.DocumentSnapshot, error) {
	col := db.Collection(strings.TrimSpace(*pairCollection))
	iter := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).Where("active", "==", true).Documents(ctx)
	defer iter.Stop()
	out := make([]*firestore.DocumentSnapshot, 0, 512)
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

func buildDesiredTransformDocs(existing []*firestore.DocumentSnapshot, reviewed []transformReviewEntry) (map[string]transformDesiredDoc, map[string]synonymDesiredDoc) {
	pathMeta := map[string]transformPathMeta{}
	candidates := make([]productsem.PathCandidate, 0, len(existing)*2+len(reviewed)*2)
	addMeta := func(path, name, fragmentID string, quality int) {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/product/") {
			return
		}
		current, ok := pathMeta[path]
		next := transformPathMeta{
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
		if doc == nil {
			continue
		}
		data := doc.Data()
		addMeta(stringValue(data["fromPath"]), stringValue(data["fromName"]), stringValue(data["fromFragmentId"]), 100)
		addMeta(stringValue(data["toPath"]), stringValue(data["toName"]), stringValue(data["toFragmentId"]), 100)
	}
	for _, pair := range reviewed {
		if !pair.Accepted {
			continue
		}
		addMeta(pair.FromPath, pair.FromName, pair.FromFragmentID, 120)
		addMeta(pair.ToPath, pair.ToName, pair.ToFragmentID, 120)
	}

	winnerMap := productsem.BuildWinnerMap(candidates)
	synonymDesired := map[string]synonymDesiredDoc{}
	for _, group := range productsem.BuildGroups(candidates) {
		if len(group.Members) < 2 {
			continue
		}
		winnerMeta := bestTransformMeta(pathMeta[group.Winner.Path], group.Winner)
		for _, member := range group.Members {
			memberPath := strings.TrimSpace(member.Path)
			if memberPath == "" || memberPath == winnerMeta.Path {
				continue
			}
			memberMeta := bestTransformMeta(pathMeta[memberPath], member)
			doc := synonymDesiredDoc{
				CanonicalPath: winnerMeta.Path,
				CanonicalName: firstNonEmpty(winnerMeta.Name, humanizeTransformPath(winnerMeta.Path)),
				CanonicalID:   winnerMeta.FragmentID,
				SynonymPath:   memberMeta.Path,
				SynonymName:   firstNonEmpty(memberMeta.Name, humanizeTransformPath(memberMeta.Path)),
				SynonymID:     memberMeta.FragmentID,
				Reason:        "semantic_canonical_dedup_from_transform_pairs",
				Source:        "transform_semantic_dedup_backfill",
				Confidence:    1,
				Score:         float64(winnerMeta.Quality*100 + memberMeta.Quality),
			}
			synonymDesired[synonymPairDocIDTransform(doc.CanonicalPath, doc.SynonymPath)] = doc
		}
	}

	desired := map[string]transformDesiredDoc{}
	mergePair := func(fromPath, fromName, fromID, toPath, toName, toID, relationKind, reason, source string, confidence float64) {
		fromPath = firstNonEmpty(winnerMap[strings.TrimSpace(fromPath)], strings.TrimSpace(fromPath))
		toPath = firstNonEmpty(winnerMap[strings.TrimSpace(toPath)], strings.TrimSpace(toPath))
		relationKind = firstNonEmpty(strings.TrimSpace(strings.ToLower(relationKind)), "transformation")
		if fromPath == "" || toPath == "" || fromPath == toPath {
			return
		}
		docID := transformPairDocID(fromPath, toPath)
		doc := transformDesiredDoc{
			DocID:        docID,
			FromPath:     fromPath,
			FromName:     firstNonEmpty(fromName, pathMeta[fromPath].Name, humanizeTransformPath(fromPath)),
			FromID:       firstNonEmpty(fromID, pathMeta[fromPath].FragmentID),
			ToPath:       toPath,
			ToName:       firstNonEmpty(toName, pathMeta[toPath].Name, humanizeTransformPath(toPath)),
			ToID:         firstNonEmpty(toID, pathMeta[toPath].FragmentID),
			RelationKind: relationKind,
			Reason:       firstNonEmpty(reason, "semantic_dedup"),
			Source:       firstNonEmpty(source, "semantic_dedup_backfill"),
			Confidence:   confidence,
			Active:       true,
		}
		current, ok := desired[docID]
		if !ok || doc.Confidence >= current.Confidence {
			desired[docID] = doc
		}
	}

	for _, doc := range existing {
		if doc == nil {
			continue
		}
		data := doc.Data()
		mergePair(
			stringValue(data["fromPath"]),
			stringValue(data["fromName"]),
			stringValue(data["fromFragmentId"]),
			stringValue(data["toPath"]),
			stringValue(data["toName"]),
			stringValue(data["toFragmentId"]),
			stringValue(data["relationKind"]),
			stringValue(data["reason"]),
			stringValue(data["decisionSource"]),
			floatValueTransform(data["confidence"]),
		)
	}
	for _, pair := range reviewed {
		if !pair.Accepted {
			continue
		}
		mergePair(pair.FromPath, pair.FromName, pair.FromFragmentID, pair.ToPath, pair.ToName, pair.ToFragmentID, inferRelationKind(pair), pair.Reason, pair.DecisionSource, pair.Confidence)
	}

	return desired, collapseDesiredSynonymDocsTransform(synonymDesired)
}

func loadActiveSynonymPairDocsTransform(ctx context.Context, db *firestore.Client) []*firestore.DocumentSnapshot {
	col := db.Collection("horizon_product_synonym_pairs")
	iter := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).Where("active", "==", true).Documents(ctx)
	defer iter.Stop()
	out := make([]*firestore.DocumentSnapshot, 0, 256)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return out
		}
		out = append(out, doc)
	}
	return out
}

func syncDesiredSynonymDocsTransform(ctx context.Context, db *firestore.Client, existing []*firestore.DocumentSnapshot, desired map[string]synonymDesiredDoc) (int, error) {
	col := db.Collection("horizon_product_synonym_pairs")
	now := firestore.ServerTimestamp
	_ = existing
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
			"normalizedSynonym":   normalizeAliasKeyTransform(doc.SynonymName),
			"decisionSource":      doc.Source,
			"confidence":          doc.Confidence,
			"reason":              doc.Reason,
			"active":              true,
			"updatedAt":           now,
		}
		if _, err := col.Doc(docID).Set(ctx, record, firestore.MergeAll); err != nil {
			return 0, err
		}
		if toolProductDictRuntime != nil {
			if err := toolProductDictRuntime.SyncSynonymPairWrite(ctx, strings.TrimSpace(*tenantID), doc.CanonicalPath, doc.SynonymPath, true); err != nil {
				return 0, err
			}
		}
		written++
		if written%100 == 0 {
			log.Printf("transform semantic dedup: synonym upserts=%d/%d", written, len(desired))
		}
	}
	if written > 0 {
		log.Printf("transform semantic dedup: synonym upserts=%d", written)
	}
	return 0, nil
}

func collapseDesiredSynonymDocsTransform(in map[string]synonymDesiredDoc) map[string]synonymDesiredDoc {
	ordered := make([]synonymDesiredDoc, 0, len(in))
	for _, doc := range in {
		ordered = append(ordered, doc)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return betterSynonymDesiredDocTransform(ordered[i], ordered[j])
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
		out[synonymPairDocIDTransform(doc.CanonicalPath, doc.SynonymPath)] = doc
		if pathKey != "" {
			usedPaths[pathKey] = struct{}{}
		}
		if idKey != "" {
			usedIDs[idKey] = struct{}{}
		}
	}
	return out
}

func betterSynonymDesiredDocTransform(left, right synonymDesiredDoc) bool {
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

func syncDesiredTransformDocs(ctx context.Context, db *firestore.Client, existing []*firestore.DocumentSnapshot, desired map[string]transformDesiredDoc) (int, error) {
	col := db.Collection(strings.TrimSpace(*pairCollection))
	now := firestore.ServerTimestamp
	deactivated := 0
	log.Printf("semantic transform dedup: existing_active=%d desired_active=%d", len(existing), len(desired))
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
			"updatedBy":  "transform_semantic_dedup_backfill",
		}, firestore.MergeAll); err != nil {
			return deactivated, err
		}
		deactivated++
	}
	written := 0
	for _, doc := range desired {
		record := map[string]interface{}{
			"tenantId":         strings.TrimSpace(*tenantID),
			"fromFragmentId":   doc.FromID,
			"fromPath":         doc.FromPath,
			"fromName":         doc.FromName,
			"toFragmentId":     doc.ToID,
			"toPath":           doc.ToPath,
			"toName":           doc.ToName,
			"relationKind":     doc.RelationKind,
			"decisionSource":   doc.Source,
			"confidence":       doc.Confidence,
			"reason":           doc.Reason,
			"active":           true,
			"updatedAt":        now,
		}
		if _, err := col.Doc(doc.DocID).Set(ctx, record, firestore.MergeAll); err != nil {
			return deactivated, err
		}
		if toolProductDictRuntime != nil {
			if err := toolProductDictRuntime.SyncTransformPairWrite(ctx, strings.TrimSpace(*tenantID), doc.FromPath, doc.ToPath, doc.RelationKind, true); err != nil {
				return deactivated, err
			}
		}
		written++
		if written%100 == 0 {
			log.Printf("semantic transform dedup: upserted=%d/%d", written, len(desired))
		}
	}
	log.Printf("semantic transform dedup: deactivated=%d upserted=%d", deactivated, written)
	return deactivated, nil
}

func bestTransformMeta(meta transformPathMeta, candidate productsem.PathCandidate) transformPathMeta {
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

func humanizeTransformPath(path string) string {
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

func floatValueTransform(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

func synonymPairDocIDTransform(canonicalPath, synonymPath string) string {
	raw := strings.TrimSpace(canonicalPath) + "|" + strings.TrimSpace(synonymPath)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func normalizeAliasKeyTransform(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	return strings.Join(strings.Fields(value), " ")
}
