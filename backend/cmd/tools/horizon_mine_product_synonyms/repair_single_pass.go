package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

const repairRedirectStableTTL = 7 * 24 * time.Hour

type singlePassRepairStats struct {
	SupplyLinksUpdated  int
	MaterializedUpdated int
}

func applySinglePassRepair(
	ctx context.Context,
	db *firestore.Client,
	tenant string,
	replacements map[string]string,
) (singlePassRepairStats, error) {
	stats := singlePassRepairStats{}
	replacements = normalizeReplacementMapLocal(replacements)
	if len(replacements) == 0 {
		return stats, nil
	}

	materializedRepo := hrepo.NewMaterializedArtifactsRepository(db)
	redirectsRepo := hrepo.NewCanonicalPathRedirectsRepository(db)

	recordRedirect := func(fragType, oldPath, newPath string) {
		oldPath = strings.TrimSpace(oldPath)
		newPath = strings.TrimSpace(newPath)
		if oldPath == "" || newPath == "" || oldPath == newPath {
			return
		}
		_, _, _, _ = redirectsRepo.RecordObservation(ctx, "global", oldPath, newPath, "merge", fragType, 1, repairRedirectStableTTL)
	}

	linkPathRewrites, updatedLinks, err := rewriteSupplyLinksSinglePass(ctx, db, recordRedirect, replacements)
	if err != nil {
		return stats, err
	}
	stats.SupplyLinksUpdated = updatedLinks

	combined := make(map[string]string, len(replacements)+len(linkPathRewrites))
	for oldPath, newPath := range replacements {
		combined[oldPath] = newPath
	}
	for oldPath, newPath := range linkPathRewrites {
		combined[oldPath] = newPath
	}

	updatedArtifacts, err := rewriteMaterializedArtifactsSinglePass(ctx, db, materializedRepo, combined)
	if err != nil {
		return stats, err
	}
	stats.MaterializedUpdated = updatedArtifacts
	return stats, nil
}

func rewriteSupplyLinksSinglePass(
	ctx context.Context,
	db *firestore.Client,
	recordRedirect func(fragType, oldPath, newPath string),
	replacements map[string]string,
) (map[string]string, int, error) {
	linkPathRewrites := make(map[string]string)
	updated := 0
	seenDocIDs := make(map[string]struct{}, 256)
	changedLinks := make([]*models.HorizonFragment, 0, 128)
	changedOldPaths := make([]string, 0, 128)
	keys := orderedReplacementKeys(replacements)
	for idx, oldPath := range keys {
		newPath := replacements[oldPath]
		if idx < 3 {
			log.Printf("repair targeted supply-links path[%d/%d]: %s -> %s", idx+1, len(keys), oldPath, newPath)
		}
		docs, err := db.Collection("horizon_fragments").
			Where("type", "==", string(models.FragmentTypeSupplyLink)).
			Where("productRef", "==", oldPath).
			Documents(ctx).
			GetAll()
		if err != nil {
			return nil, updated, err
		}
		if (idx+1)%10 == 0 || idx == len(keys)-1 {
			log.Printf("repair targeted supply-links progress: paths=%d/%d updated=%d candidates=%d", idx+1, len(keys), updated, len(docs))
		}
		for _, doc := range docs {
			if _, seen := seenDocIDs[doc.Ref.ID]; seen {
				continue
			}
			seenDocIDs[doc.Ref.ID] = struct{}{}

			var link models.HorizonFragment
			if err := doc.DataTo(&link); err != nil {
				continue
			}
			link.ID = doc.Ref.ID
			if link.ProductRef == nil || strings.TrimSpace(*link.ProductRef) != oldPath {
				continue
			}
			*link.ProductRef = newPath

			oldLinkPath := strings.TrimSpace(link.CanonicalPath)
			if newLinkPath, err := models.BuildSupplyLinkCanonicalPath(link.TenantID, link.SupplierRef, link.ProductRef, link.CustomerRef); err == nil && strings.TrimSpace(newLinkPath) != "" {
				link.CanonicalPath = strings.TrimSpace(newLinkPath)
				if link.CanonicalPath != oldLinkPath {
					link.AlternativePaths = uniqueSortedStrings(append(link.AlternativePaths, oldLinkPath))
					linkPathRewrites[oldLinkPath] = link.CanonicalPath
					recordRedirect(string(models.FragmentTypeSupplyLink), oldLinkPath, link.CanonicalPath)
				}
			}
			linkCopy := link
			changedLinks = append(changedLinks, &linkCopy)
			changedOldPaths = append(changedOldPaths, oldLinkPath)
			if len(changedLinks) >= 150 {
				log.Printf("repair targeted supply-links batch commit: size=%d updated=%d", len(changedLinks), updated)
				written, err := writeSupplyLinksBatchDirect(ctx, db, changedLinks, changedOldPaths)
				if err != nil {
					return nil, updated, err
				}
				updated += written
				changedLinks = changedLinks[:0]
				changedOldPaths = changedOldPaths[:0]
			}
		}
	}
	if len(changedLinks) == 0 {
		return linkPathRewrites, updated, nil
	}
	written, err := writeSupplyLinksBatchDirect(ctx, db, changedLinks, changedOldPaths)
	if err != nil {
		return nil, updated, err
	}
	updated += written
	return linkPathRewrites, updated, nil
}

func rewriteMaterializedArtifactsSinglePass(
	ctx context.Context,
	db *firestore.Client,
	materializedRepo *hrepo.MaterializedArtifactsRepository,
	replacements map[string]string,
) (int, error) {
	updated := 0
	changedByID := make(map[string]*models.MaterializedArtifact, 256)
	keys := orderedReplacementKeys(replacements)
	for idx, oldPath := range keys {
		newPath := replacements[oldPath]
		if idx < 3 {
			log.Printf("repair targeted materialized path[%d/%d]: %s -> %s", idx+1, len(keys), oldPath, newPath)
		}
		queries := []firestore.Query{
			db.Collection("horizon_materialized_artifacts").Where("nodeRefs", "array-contains", oldPath),
			db.Collection("horizon_materialized_artifacts").Where("flowRefs", "array-contains", oldPath),
			db.Collection("horizon_materialized_artifacts").Where("productAxis", "==", oldPath),
		}
		candidates := 0
		for _, q := range queries {
			docs, err := q.Documents(ctx).GetAll()
			if err != nil {
				return updated, err
			}
			candidates += len(docs)
			for _, doc := range docs {
				art, ok := changedByID[doc.Ref.ID]
				if !ok {
					var loaded models.MaterializedArtifact
					if err := doc.DataTo(&loaded); err != nil {
						continue
					}
					loaded.ID = doc.Ref.ID
					art = &loaded
				}
				if models.ReplaceCanonicalPathInArtifact(art, oldPath, newPath) {
					changedByID[doc.Ref.ID] = art
				}
			}
		}
		if (idx+1)%10 == 0 || idx == len(keys)-1 {
			log.Printf("repair targeted materialized progress: paths=%d/%d changed_docs=%d candidates=%d", idx+1, len(keys), len(changedByID), candidates)
		}
	}
	if len(changedByID) == 0 {
		return updated, nil
	}
	changedArtifacts := make([]*models.MaterializedArtifact, 0, len(changedByID))
	for _, art := range changedByID {
		changedArtifacts = append(changedArtifacts, art)
	}
	written, err := writeMaterializedArtifactsBatch(ctx, db, materializedRepo, changedArtifacts)
	if err != nil {
		return updated, err
	}
	updated += written
	return updated, nil
}

func orderedReplacementKeys(replacements map[string]string) []string {
	keys := make([]string, 0, len(replacements))
	for oldPath := range replacements {
		keys = append(keys, oldPath)
	}
	sort.Strings(keys)
	return keys
}

func normalizeReplacementMapLocal(replacements map[string]string) map[string]string {
	out := make(map[string]string, len(replacements))
	for oldPath, newPath := range replacements {
		oldPath = strings.TrimSpace(oldPath)
		newPath = strings.TrimSpace(newPath)
		if oldPath == "" || newPath == "" || oldPath == newPath {
			continue
		}
		out[oldPath] = newPath
	}
	return out
}

func writeSupplyLinksBatchDirect(ctx context.Context, db *firestore.Client, links []*models.HorizonFragment, oldCanonicalPaths []string) (int, error) {
	if db == nil || len(links) == 0 {
		return 0, nil
	}
	if len(links) != len(oldCanonicalPaths) {
		return 0, fmt.Errorf("supply-link batch size mismatch: links=%d oldPaths=%d", len(links), len(oldCanonicalPaths))
	}
	const maxOps = 450
	batch := db.Batch()
	ops := 0
	written := 0
	commit := func() error {
		if ops == 0 {
			return nil
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
		batch = db.Batch()
		ops = 0
		return nil
	}
	for idx, link := range links {
		if link == nil || strings.TrimSpace(link.ID) == "" {
			continue
		}
		now := time.Now().UTC()
		link.UpdatedAt = now
		if strings.TrimSpace(link.UpdatedBy) == "" {
			link.UpdatedBy = "system"
		}
		newCanonicalPath := strings.TrimSpace(link.CanonicalPath)
		oldCanonicalPath := strings.TrimSpace(oldCanonicalPaths[idx])
		neededOps := 1
		if newCanonicalPath != "" {
			neededOps++
		}
		if oldCanonicalPath != "" && oldCanonicalPath != newCanonicalPath {
			neededOps++
		}
		if ops+neededOps > maxOps {
			if err := commit(); err != nil {
				return written, err
			}
		}
		batch.Set(db.Collection("horizon_fragments").Doc(strings.TrimSpace(link.ID)), link)
		ops++
		if newCanonicalPath != "" {
			claim := map[string]any{
				"tenantId":      "global",
				"canonicalPath": newCanonicalPath,
				"fragmentId":    strings.TrimSpace(link.ID),
				"updatedAt":     now,
			}
			batch.Set(db.Collection("horizon_fragment_path_claims").Doc("cp_"+claimDocHash("global", newCanonicalPath)), claim)
			ops++
		}
		if oldCanonicalPath != "" && oldCanonicalPath != newCanonicalPath {
			batch.Delete(db.Collection("horizon_fragment_path_claims").Doc("cp_" + claimDocHash("global", oldCanonicalPath)))
			ops++
		}
		written++
	}
	if err := commit(); err != nil {
		return written, err
	}
	return written, nil
}

func writeMaterializedArtifactsBatch(
	ctx context.Context,
	db *firestore.Client,
	_ *hrepo.MaterializedArtifactsRepository,
	artifacts []*models.MaterializedArtifact,
) (int, error) {
	if db == nil || len(artifacts) == 0 {
		return 0, nil
	}
	const maxOps = 50
	batch := db.Batch()
	ops := 0
	written := 0
	commit := func() error {
		if ops == 0 {
			return nil
		}
		log.Printf("repair targeted materialized batch commit: size=%d written=%d", ops, written)
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
		batch = db.Batch()
		ops = 0
		return nil
	}
	for _, art := range artifacts {
		if art == nil || strings.TrimSpace(art.ID) == "" {
			continue
		}
		models.PopulateMaterializedArtifactIndexes(art)
		if err := art.Validate(); err != nil {
			return written, err
		}
		art.UpdatedAt = time.Now().UTC()
		if strings.TrimSpace(art.UpdatedBy) == "" {
			art.UpdatedBy = "system"
		}
		if ops+1 > maxOps {
			if err := commit(); err != nil {
				return written, err
			}
		}
		batch.Set(db.Collection("horizon_materialized_artifacts").Doc(strings.TrimSpace(art.ID)), art)
		ops++
		written++
	}
	if err := commit(); err != nil {
		return written, err
	}
	return written, nil
}

func claimDocHash(tenantID, canonicalPath string) string {
	raw := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(canonicalPath)
	sum := sha1Bytes([]byte(raw))
	return sum[:20]
}

func sha1Bytes(raw []byte) string {
	sum := sha1.Sum(raw)
	return hex.EncodeToString(sum[:])
}

func replaceOptionalStringByMap(value *string, replacements map[string]string) bool {
	if value == nil {
		return false
	}
	oldPath := strings.TrimSpace(*value)
	newPath, ok := replacements[oldPath]
	if !ok || strings.TrimSpace(newPath) == "" || oldPath == strings.TrimSpace(newPath) {
		return false
	}
	*value = strings.TrimSpace(newPath)
	return true
}

func replaceStringSliceByMap(values []string, replacements map[string]string) bool {
	changed := false
	for i, value := range values {
		oldPath := strings.TrimSpace(value)
		newPath, ok := replacements[oldPath]
		if !ok || strings.TrimSpace(newPath) == "" || oldPath == strings.TrimSpace(newPath) {
			continue
		}
		values[i] = newPath
		changed = true
	}
	return changed
}

func artifactCandidatePaths(art *models.MaterializedArtifact, replacements map[string]string) []string {
	if art == nil || len(replacements) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, 16)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := replacements[path]; !ok {
			return
		}
		seen[path] = struct{}{}
	}
	for _, path := range art.NodeRefs {
		add(path)
	}
	for _, path := range art.FlowRefs {
		add(path)
	}
	for _, path := range art.GeoRefs {
		add(path)
	}
	for _, path := range art.ZoneRefs {
		add(path)
	}
	for _, path := range art.AffectedZoneRefs {
		add(path)
	}
	add(art.ProductAxis)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
