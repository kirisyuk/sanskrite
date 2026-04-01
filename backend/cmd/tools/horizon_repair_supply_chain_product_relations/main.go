package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

const repairToolName = "tool:horizon_repair_supply_chain_product_relations"

var (
	projectID       = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID        = flag.String("tenant", "global", "tenant id")
	apply           = flag.Bool("apply", false, "apply writes")
	limit           = flag.Int("limit", 0, "max edges and artifacts to scan per source (0 = unlimited)")
	pageSize        = flag.Int("page-size", 250, "Firestore page size for artifact scanning")
	timeoutHours    = flag.Int("timeout-hours", 4, "context timeout in hours")
	repairEdges     = flag.Bool("repair-edges", true, "repair supply-chain product-product edges")
	repairArtifacts = flag.Bool("repair-artifacts", true, "repair supply-chain assembly artifacts")
	allowHeuristic  = flag.Bool("allow-heuristic", false, "allow conservative heuristic repairs when pair dictionary is missing")
)

type relationDecision struct {
	SourcePath   string
	TargetPath   string
	EdgeType     models.DependencyType
	RelationKind string
	Source       string
}

type pairAggregate struct {
	FromPath     string
	ToPath       string
	RelationKind string
	Count        int
}

type stats struct {
	EdgesScanned          int
	EdgesEligible         int
	EdgesRepaired         int
	EdgesDeleted          int
	EdgesQualifierUpdated int
	EdgesSkipped          int
	EdgesErrored          int
	ArtifactsScanned      int
	ArtifactsUpdated      int
	PairDocsUpserted      int
}

type runner struct {
	ctx              context.Context
	db               *firestore.Client
	tenantID         string
	apply            bool
	limit            int
	productDict      *productdict.Runtime
	fragmentsRepo    *repo.HorizonFragmentsRepository
	edgesRepo        *repo.DependencyEdgesRepository
	fragmentByID     map[string]*models.HorizonFragment
	fragmentByPath   map[string]*models.HorizonFragment
	activePairDocIDs map[string]struct{}
	pairs            map[string]*pairAggregate
	stats            stats
}

func main() {
	flag.Parse()

	timeout := 4 * time.Hour
	if timeoutHours != nil && *timeoutHours > 0 {
		timeout = time.Duration(*timeoutHours) * time.Hour
	}
	if pageSize != nil {
		if *pageSize <= 0 {
			*pageSize = 250
		}
		if *pageSize > 2000 {
			*pageSize = 2000
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	dict := productdict.NewRuntime(db).WithTenantID(strings.TrimSpace(*tenantID))
	fragmentsRepo := repo.NewHorizonFragmentsRepository(db).WithProductDictionary(dict)
	edgesRepo := repo.NewDependencyEdgesRepository(db).
		WithFragmentsRepo(fragmentsRepo).
		WithProductDictionary(dict)

	r := &runner{
		ctx:              ctx,
		db:               db,
		tenantID:         strings.TrimSpace(firstNonEmpty(*tenantID, "global")),
		apply:            *apply,
		limit:            maxInt(0, *limit),
		productDict:      dict,
		fragmentsRepo:    fragmentsRepo,
		edgesRepo:        edgesRepo,
		fragmentByID:     map[string]*models.HorizonFragment{},
		fragmentByPath:   map[string]*models.HorizonFragment{},
		activePairDocIDs: map[string]struct{}{},
		pairs:            map[string]*pairAggregate{},
	}

	if err := r.loadActivePairs(); err != nil {
		log.Fatalf("failed to load active pair docs: %v", err)
	}
	if *repairEdges {
		if err := r.repairEdgesByType(models.DependencyTypeTransformsInto); err != nil {
			log.Fatalf("failed repairing TRANSFORMS_INTO edges: %v", err)
		}
		if err := r.repairEdgesByType(models.DependencyTypeAssemblesInto); err != nil {
			log.Fatalf("failed repairing ASSEMBLES_INTO edges: %v", err)
		}
		if err := r.repairEdgesByType(models.DependencyTypeUsedIn); err != nil {
			log.Fatalf("failed repairing USED_IN edges: %v", err)
		}
	}
	if err := r.syncPairs(); err != nil {
		log.Fatalf("failed syncing pair docs: %v", err)
	}
	if *repairArtifacts {
		if err := r.repairAssemblyArtifacts(); err != nil {
			log.Fatalf("failed repairing assembly artifacts: %v", err)
		}
	}

	log.Printf("summary tenant=%s apply=%t edges_scanned=%d edges_eligible=%d edges_repaired=%d edges_deleted=%d edges_qualifier_updated=%d edges_skipped=%d edges_errored=%d pair_docs_upserted=%d artifacts_scanned=%d artifacts_updated=%d",
		r.tenantID,
		r.apply,
		r.stats.EdgesScanned,
		r.stats.EdgesEligible,
		r.stats.EdgesRepaired,
		r.stats.EdgesDeleted,
		r.stats.EdgesQualifierUpdated,
		r.stats.EdgesSkipped,
		r.stats.EdgesErrored,
		r.stats.PairDocsUpserted,
		r.stats.ArtifactsScanned,
		r.stats.ArtifactsUpdated,
	)
}

func (r *runner) repairEdgesByType(edgeType models.DependencyType) error {
	iter := r.db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", r.tenantID).
		Where("type", "==", string(edgeType)).
		Documents(r.ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		if r.limit > 0 && r.stats.EdgesScanned >= r.limit*2 {
			return nil
		}

		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			r.stats.EdgesErrored++
			log.Printf("edge decode failed id=%s type=%s err=%v", doc.Ref.ID, edgeType, err)
			continue
		}
		edge.ID = doc.Ref.ID
		r.stats.EdgesScanned++

		if !isSupplyChainEdge(&edge) {
			r.stats.EdgesSkipped++
			continue
		}

		sourcePath, targetPath, ok := r.edgePaths(&edge)
		if !ok {
			r.stats.EdgesSkipped++
			continue
		}
		if !strings.HasPrefix(sourcePath, "/product/") || !strings.HasPrefix(targetPath, "/product/") {
			r.stats.EdgesSkipped++
			continue
		}
		r.stats.EdgesEligible++

		currentSourceFrag, _ := r.fragmentForID(edge.SourceFragmentID)
		currentTargetFrag, _ := r.fragmentForID(edge.TargetFragmentID)

		decision, ok := r.decideRelation(
			sourcePath,
			targetPath,
			edge.Type,
			qualifierString(edge.Qualifiers, "reason"),
			fragmentName(currentSourceFrag, sourcePath),
			fragmentName(currentTargetFrag, targetPath),
		)
		if !ok {
			r.stats.EdgesSkipped++
			continue
		}

		sourceFrag, err := r.resolveFragmentForDecision(&edge, sourcePath, targetPath, decision.SourcePath, decision.TargetPath, true)
		if err != nil || sourceFrag == nil {
			r.stats.EdgesErrored++
			log.Printf("edge source resolve failed id=%s desired_source=%s err=%v", edge.ID, decision.SourcePath, err)
			continue
		}
		targetFrag, err := r.resolveFragmentForDecision(&edge, sourcePath, targetPath, decision.SourcePath, decision.TargetPath, false)
		if err != nil || targetFrag == nil {
			r.stats.EdgesErrored++
			log.Printf("edge target resolve failed id=%s desired_target=%s err=%v", edge.ID, decision.TargetPath, err)
			continue
		}
		if fragmentsCollapseToSelfLoop(sourceFrag, targetFrag, decision.SourcePath, decision.TargetPath) {
			if !r.apply {
				log.Printf("edge delete candidate id=%s source=%s target=%s type=%s via=self_loop_collapse",
					edge.ID, sourcePath, targetPath, edge.Type)
				r.stats.EdgesRepaired++
				r.stats.EdgesDeleted++
				continue
			}
			if err := r.edgesRepo.Delete(r.ctx, edge.ID); err != nil {
				r.stats.EdgesErrored++
				log.Printf("edge delete failed id=%s err=%v", edge.ID, err)
				continue
			}
			r.stats.EdgesRepaired++
			r.stats.EdgesDeleted++
			continue
		}

		needsIdentityRepair := strings.TrimSpace(edge.SourceFragmentID) != strings.TrimSpace(sourceFrag.ID) ||
			strings.TrimSpace(edge.TargetFragmentID) != strings.TrimSpace(targetFrag.ID) ||
			edge.Type != decision.EdgeType
		needsQualifierRepair := qualifierString(edge.Qualifiers, "sourceCanonicalPath") != decision.SourcePath ||
			qualifierString(edge.Qualifiers, "targetCanonicalPath") != decision.TargetPath ||
			qualifierString(edge.Qualifiers, "productRelationKind") != decision.RelationKind
		if !needsIdentityRepair && !needsQualifierRepair {
			continue
		}
		if decision.Source == "heuristic" && !*allowHeuristic {
			continue
		}
		if decision.Source == "heuristic" && !needsIdentityRepair {
			continue
		}
		if decision.Source == "dictionary" || decision.Source == "reason_reverse" || needsIdentityRepair {
			r.recordPair(decision)
		}

		repaired := edge
		repaired.ID = ""
		repaired.SourceFragmentID = strings.TrimSpace(sourceFrag.ID)
		repaired.TargetFragmentID = strings.TrimSpace(targetFrag.ID)
		repaired.Type = decision.EdgeType
		repaired.Qualifiers = cloneQualifiers(edge.Qualifiers)
		repaired.Qualifiers["sourceCanonicalPath"] = decision.SourcePath
		repaired.Qualifiers["targetCanonicalPath"] = decision.TargetPath
		repaired.Qualifiers["productRelationKind"] = decision.RelationKind
		repaired.Qualifiers["normalizedByProductRelationDictionary"] = decision.Source == "dictionary"
		repaired.Qualifiers["repairSource"] = repairToolName
		repaired.Qualifiers["repairSourceMode"] = decision.Source
		repaired.Metadata = edge.Metadata
		repaired.Provenance = edge.Provenance
		repaired.Properties = edge.Properties

		if !r.apply {
			log.Printf("edge repair candidate id=%s source=%s target=%s type=%s -> source=%s target=%s type=%s via=%s",
				edge.ID, sourcePath, targetPath, edge.Type, decision.SourcePath, decision.TargetPath, decision.EdgeType, decision.Source)
			r.stats.EdgesRepaired++
			continue
		}

		if needsIdentityRepair {
			newID, err := r.edgesRepo.Create(r.ctx, &repaired)
			if err != nil {
				r.stats.EdgesErrored++
				log.Printf("edge repair create failed id=%s err=%v", edge.ID, err)
				continue
			}
			r.stats.EdgesRepaired++
			if strings.TrimSpace(newID) != strings.TrimSpace(edge.ID) {
				if err := r.edgesRepo.Delete(r.ctx, edge.ID); err != nil {
					r.stats.EdgesErrored++
					log.Printf("edge repair delete failed old_id=%s new_id=%s err=%v", edge.ID, newID, err)
					continue
				}
				r.stats.EdgesDeleted++
			}
			continue
		}

		repaired.ID = edge.ID
		if err := r.edgesRepo.Update(r.ctx, &repaired); err != nil {
			r.stats.EdgesErrored++
			log.Printf("edge qualifier repair failed id=%s err=%v", edge.ID, err)
			continue
		}
		r.stats.EdgesQualifierUpdated++
	}
}

func (r *runner) syncPairs() error {
	keys := make([]string, 0, len(r.pairs))
	for key := range r.pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pair := r.pairs[key]
		if pair == nil {
			continue
		}
		docID := transformPairDocID(pair.FromPath, pair.ToPath)
		if _, ok := r.activePairDocIDs[docID]; ok {
			continue
		}
		if !r.apply {
			log.Printf("pair upsert candidate %s -> %s relation=%s evidence=%d", pair.FromPath, pair.ToPath, pair.RelationKind, pair.Count)
			r.stats.PairDocsUpserted++
			continue
		}
		fromFrag, _ := r.fragmentForPath(pair.FromPath)
		toFrag, _ := r.fragmentForPath(pair.ToPath)
		record := map[string]interface{}{
			"tenantId":       r.tenantID,
			"fromFragmentId": fragmentID(fromFrag),
			"fromPath":       pair.FromPath,
			"fromName":       fragmentName(fromFrag, pair.FromPath),
			"toFragmentId":   fragmentID(toFrag),
			"toPath":         pair.ToPath,
			"toName":         fragmentName(toFrag, pair.ToPath),
			"relationKind":   pair.RelationKind,
			"decisionSource": repairToolName,
			"confidence":     1.0,
			"reason":         fmt.Sprintf("backfilled_from_supply_chain_relation_repair evidence=%d", pair.Count),
			"active":         true,
			"updatedAt":      firestore.ServerTimestamp,
		}
		if _, err := r.db.Collection("horizon_product_transform_pairs").Doc(docID).Set(r.ctx, record, firestore.MergeAll); err != nil {
			return err
		}
		if err := r.productDict.SyncTransformPairWrite(r.ctx, r.tenantID, pair.FromPath, pair.ToPath, pair.RelationKind, true); err != nil {
			return err
		}
		r.activePairDocIDs[docID] = struct{}{}
		r.stats.PairDocsUpserted++
	}
	return nil
}

func (r *runner) repairAssemblyArtifacts() error {
	cursor := ""
	log.Printf("artifact scan started mode=document_id_page_filter page_size=%d tenant=%s", *pageSize, r.tenantID)
	for {
		q := r.db.Collection("horizon_materialized_artifacts").
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if cursor != "" {
			q = q.StartAfter(cursor)
		}

		iter := q.Documents(r.ctx)
		pageCount := 0
		lastDocID := ""
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				return err
			}
			pageCount++
			lastDocID = doc.Ref.ID
			if r.limit > 0 && r.stats.ArtifactsScanned >= r.limit {
				iter.Stop()
				return nil
			}

			var artifact models.MaterializedArtifact
			if err := doc.DataTo(&artifact); err != nil {
				continue
			}
			if strings.TrimSpace(artifact.TenantID) != r.tenantID {
				continue
			}
			if artifact.Kind != models.MaterializedKindSupplyChainAssemblyGraph {
				continue
			}
			artifact.ID = doc.Ref.ID
			r.stats.ArtifactsScanned++
			if r.stats.ArtifactsScanned%1000 == 0 {
				log.Printf("artifact scan progress tenant=%s scanned=%d updated=%d cursor=%s", r.tenantID, r.stats.ArtifactsScanned, r.stats.ArtifactsUpdated, artifact.ID)
			}

			parsed, err := models.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
			if err != nil || parsed == nil {
				continue
			}

			transforms, assemblies, changed := r.normalizeAssemblyEdgeLists(parsed.TransformEdges, parsed.AssemblyEdges)
			if !changed {
				continue
			}

			if !r.apply {
				log.Printf("artifact repair candidate id=%s key=%s", artifact.ID, artifact.CanonicalKey)
				r.stats.ArtifactsUpdated++
				continue
			}

			artifact.Payload = models.BuildSupplyChainAssemblyGraphPayloadDetailed(models.SupplyChainAssemblyGraphPayloadOptions{
				AnchorSet:                       parsed.AnchorSet,
				SourceAnchors:                   parsed.SourceAnchors,
				ResolvedAnchors:                 parsed.ResolvedAnchors,
				UnresolvedAnchors:               parsed.UnresolvedAnchors,
				AnchorProducts:                  parsed.AnchorProducts,
				AnchorComponents:                parsed.AnchorComponents,
				ProductNodes:                    parsed.ProductNodes,
				ActorNodes:                      parsed.ActorNodes,
				SupplyEdges:                     parsed.SupplyEdges,
				TransformEdges:                  transforms,
				AssemblyEdges:                   assemblies,
				RouteBindings:                   parsed.RouteBindings,
				Coverage:                        parsed.Coverage,
				OpenGaps:                        parsed.OpenGaps,
				DirectPathKeys:                  parsed.DirectPathKeys,
				DerivedPathKeys:                 parsed.DerivedPathKeys,
				PrimaryBranches:                 parsed.PrimaryBranches,
				SecondaryBranches:               parsed.SecondaryBranches,
				TopRoutes:                       parsed.TopRoutes,
				TopDistributionNodes:            parsed.TopDistributionNodes,
				TopSuppliers:                    parsed.TopSuppliers,
				TopProductFamilies:              parsed.TopProductFamilies,
				SuppressedSupportBranches:       parsed.SuppressedSupportBranches,
				Stage:                           parsed.Stage,
				Provisional:                     parsed.Provisional,
				SourceMode:                      parsed.SourceMode,
				BackgroundContinuationScheduled: parsed.BackgroundContinuationScheduled,
				BackgroundContinuationJobIDs:    parsed.BackgroundContinuationJobIDs,
				Notes:                           parsed.Notes,
			})
			artifact.UpdatedAt = time.Now().UTC()
			artifact.UpdatedBy = repairToolName
			models.PopulateMaterializedArtifactIndexes(&artifact)
			if _, err := r.db.Collection("horizon_materialized_artifacts").Doc(artifact.ID).Set(r.ctx, &artifact); err != nil {
				iter.Stop()
				return err
			}
			r.stats.ArtifactsUpdated++
		}
		iter.Stop()
		if pageCount == 0 || pageCount < *pageSize {
			return nil
		}
		cursor = lastDocID
	}
}

func (r *runner) normalizeAssemblyEdgeLists(
	transforms []models.SupplyChainAssemblyTransformEdge,
	assemblies []models.SupplyChainAssemblyTransformEdge,
) ([]models.SupplyChainAssemblyTransformEdge, []models.SupplyChainAssemblyTransformEdge, bool) {
	changed := false
	outTransforms := make([]models.SupplyChainAssemblyTransformEdge, 0, len(transforms))
	outAssemblies := make([]models.SupplyChainAssemblyTransformEdge, 0, len(assemblies))
	seen := map[string]struct{}{}
	applyOne := func(edge models.SupplyChainAssemblyTransformEdge) {
		fromPath := r.canonicalizeArtifactProductPath(edge.FromProductRef)
		toPath := r.canonicalizeArtifactProductPath(edge.ToProductRef)
		if fromPath == "" || toPath == "" {
			return
		}
		if fromPath != edge.FromProductRef || toPath != edge.ToProductRef {
			changed = true
			edge.FromProductRef = fromPath
			edge.ToProductRef = toPath
		}
		if edge.FromProductRef == edge.ToProductRef {
			changed = true
			return
		}
		decision, ok := r.decideRelation(edge.FromProductRef, edge.ToProductRef, models.DependencyType(strings.TrimSpace(edge.Relation)), "", "", "")
		if !ok {
			return
		}
		if decision.Source == "heuristic" && !*allowHeuristic {
			return
		}
		if edge.FromProductRef != decision.SourcePath || edge.ToProductRef != decision.TargetPath || edge.Relation != string(decision.EdgeType) {
			changed = true
		}
		edge.FromProductRef = decision.SourcePath
		edge.ToProductRef = decision.TargetPath
		edge.Relation = string(decision.EdgeType)
		key := edge.FromProductRef + "->" + edge.ToProductRef + "|" + edge.Relation
		if _, ok := seen[key]; ok {
			changed = true
			return
		}
		seen[key] = struct{}{}
		if decision.EdgeType == models.DependencyTypeAssemblesInto {
			outAssemblies = append(outAssemblies, edge)
			return
		}
		outTransforms = append(outTransforms, edge)
	}
	for _, edge := range transforms {
		applyOne(edge)
	}
	for _, edge := range assemblies {
		applyOne(edge)
	}
	return outTransforms, outAssemblies, changed
}

func (r *runner) decideRelation(sourcePath, targetPath string, currentType models.DependencyType, reason, sourceName, targetName string) (relationDecision, bool) {
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	if sourcePath == "" || targetPath == "" || sourcePath == targetPath {
		return relationDecision{}, false
	}
	if !strings.HasPrefix(sourcePath, "/product/") || !strings.HasPrefix(targetPath, "/product/") {
		return relationDecision{}, false
	}
	if !isMeaningfulSupplyProductPath(sourcePath) || !isMeaningfulSupplyProductPath(targetPath) {
		return relationDecision{}, false
	}
	resolution, err := r.productDict.ResolveProductRelationForTenant(r.ctx, r.tenantID, sourcePath, targetPath)
	if err == nil && resolution.Known {
		return relationDecision{
			SourcePath:   resolution.FromPath,
			TargetPath:   resolution.ToPath,
			EdgeType:     resolution.EdgeType,
			RelationKind: resolution.RelationKind,
			Source:       "dictionary",
		}, true
	}

	desiredSource := sourcePath
	desiredTarget := targetPath
	srcRank, srcOK := models.ProductStageRankFromCanonicalPath(desiredSource)
	dstRank, dstOK := models.ProductStageRankFromCanonicalPath(desiredTarget)
	if (!srcOK || !dstOK || srcRank == dstRank) && strings.TrimSpace(reason) != "" {
		if decision, ok := decisionFromReasonReverse(sourcePath, targetPath, currentType, reason, sourceName, targetName); ok {
			return decision, true
		}
	}
	if !srcOK || !dstOK || srcRank == dstRank {
		return relationDecision{}, false
	}
	if srcOK && dstOK && srcRank > dstRank {
		desiredSource, desiredTarget = desiredTarget, desiredSource
	}
	if !isMeaningfulHeuristicCategoryPair(productCategory(desiredSource), productCategory(desiredTarget)) {
		return relationDecision{}, false
	}
	desiredType := models.ProductToProductEdgeTypeFromPaths(desiredSource, desiredTarget)
	return relationDecision{
		SourcePath:   desiredSource,
		TargetPath:   desiredTarget,
		EdgeType:     desiredType,
		RelationKind: relationKindForEdgeType(desiredType),
		Source:       "heuristic",
	}, true
}

func decisionFromReasonReverse(sourcePath, targetPath string, currentType models.DependencyType, reason, sourceName, targetName string) (relationDecision, bool) {
	reasonNorm := normalizeText(reason)
	sourceNorm := normalizeText(firstNonEmpty(sourceName, lastPathSegment(sourcePath)))
	targetNorm := normalizeText(firstNonEmpty(targetName, lastPathSegment(targetPath)))
	if reasonNorm == "" || sourceNorm == "" || targetNorm == "" {
		return relationDecision{}, false
	}
	patterns := []struct {
		pattern  string
		edgeType models.DependencyType
	}{
		{pattern: targetNorm + " transforms into " + sourceNorm, edgeType: models.DependencyTypeTransformsInto},
		{pattern: targetNorm + " transitions into " + sourceNorm, edgeType: models.DependencyTypeTransformsInto},
		{pattern: targetNorm + " assembles into " + sourceNorm, edgeType: models.DependencyTypeAssemblesInto},
		{pattern: targetNorm + " assembled into " + sourceNorm, edgeType: models.DependencyTypeAssemblesInto},
		{pattern: targetNorm + " used in " + sourceNorm, edgeType: models.DependencyTypeUsedIn},
		{pattern: targetNorm + " is used in " + sourceNorm, edgeType: models.DependencyTypeUsedIn},
	}
	matchedType := models.DependencyType("")
	for _, item := range patterns {
		if strings.Contains(reasonNorm, item.pattern) {
			matchedType = item.edgeType
			break
		}
	}
	if matchedType == "" {
		return relationDecision{}, false
	}
	edgeType := currentType
	if matchedType == models.DependencyTypeUsedIn {
		edgeType = models.DependencyTypeUsedIn
	} else if edgeType != models.DependencyTypeTransformsInto && edgeType != models.DependencyTypeAssemblesInto {
		edgeType = models.ProductToProductEdgeTypeFromPaths(targetPath, sourcePath)
	}
	return relationDecision{
		SourcePath:   targetPath,
		TargetPath:   sourcePath,
		EdgeType:     edgeType,
		RelationKind: relationKindForEdgeType(edgeType),
		Source:       "reason_reverse",
	}, true
}

func (r *runner) edgePaths(edge *models.DependencyEdge) (string, string, bool) {
	sourcePath := qualifierString(edge.Qualifiers, "sourceCanonicalPath")
	targetPath := qualifierString(edge.Qualifiers, "targetCanonicalPath")
	if sourcePath == "" {
		if frag, _ := r.fragmentForID(edge.SourceFragmentID); frag != nil {
			sourcePath = strings.TrimSpace(frag.CanonicalPath)
		}
	}
	if targetPath == "" {
		if frag, _ := r.fragmentForID(edge.TargetFragmentID); frag != nil {
			targetPath = strings.TrimSpace(frag.CanonicalPath)
		}
	}
	if sourcePath == "" || targetPath == "" {
		return "", "", false
	}
	return sourcePath, targetPath, true
}

func (r *runner) resolveFragmentForDecision(edge *models.DependencyEdge, currentSourcePath, currentTargetPath, desiredSourcePath, desiredTargetPath string, source bool) (*models.HorizonFragment, error) {
	var currentSourceFrag *models.HorizonFragment
	var currentTargetFrag *models.HorizonFragment
	if edge != nil {
		currentSourceFrag, _ = r.fragmentForID(edge.SourceFragmentID)
		currentTargetFrag, _ = r.fragmentForID(edge.TargetFragmentID)
		if source && currentSourceFrag != nil && strings.TrimSpace(currentSourceFrag.CanonicalPath) == strings.TrimSpace(desiredSourcePath) {
			return currentSourceFrag, nil
		}
		if source && currentTargetFrag != nil && strings.TrimSpace(currentTargetFrag.CanonicalPath) == strings.TrimSpace(desiredSourcePath) {
			return currentTargetFrag, nil
		}
		if !source && currentTargetFrag != nil && strings.TrimSpace(currentTargetFrag.CanonicalPath) == strings.TrimSpace(desiredTargetPath) {
			return currentTargetFrag, nil
		}
		if !source && currentSourceFrag != nil && strings.TrimSpace(currentSourceFrag.CanonicalPath) == strings.TrimSpace(desiredTargetPath) {
			return currentSourceFrag, nil
		}
		if source && strings.TrimSpace(currentSourcePath) == strings.TrimSpace(desiredSourcePath) {
			return r.fragmentForID(edge.SourceFragmentID)
		}
		if source && strings.TrimSpace(currentTargetPath) == strings.TrimSpace(desiredSourcePath) {
			return r.fragmentForID(edge.TargetFragmentID)
		}
		if !source && strings.TrimSpace(currentTargetPath) == strings.TrimSpace(desiredTargetPath) {
			return r.fragmentForID(edge.TargetFragmentID)
		}
		if !source && strings.TrimSpace(currentSourcePath) == strings.TrimSpace(desiredTargetPath) {
			return r.fragmentForID(edge.SourceFragmentID)
		}
	}
	if source {
		return r.fragmentForPath(desiredSourcePath)
	}
	return r.fragmentForPath(desiredTargetPath)
}

func (r *runner) fragmentForID(id string) (*models.HorizonFragment, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if frag, ok := r.fragmentByID[id]; ok {
		return frag, nil
	}
	frag, err := r.fragmentsRepo.Get(r.ctx, id)
	if err != nil || frag == nil {
		return nil, err
	}
	r.fragmentByID[id] = frag
	if path := strings.TrimSpace(frag.CanonicalPath); path != "" {
		r.fragmentByPath[path] = frag
	}
	return frag, nil
}

func (r *runner) fragmentForPath(path string) (*models.HorizonFragment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if frag, ok := r.fragmentByPath[path]; ok {
		return frag, nil
	}
	frag, err := r.fragmentsRepo.GetByCanonicalPath(r.ctx, path)
	if err != nil || frag == nil {
		return nil, err
	}
	r.fragmentByPath[path] = frag
	if id := strings.TrimSpace(frag.ID); id != "" {
		r.fragmentByID[id] = frag
	}
	return frag, nil
}

func (r *runner) loadActivePairs() error {
	iter := r.db.Collection("horizon_product_transform_pairs").
		Where("tenantId", "==", r.tenantID).
		Where("active", "==", true).
		Documents(r.ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		r.activePairDocIDs[doc.Ref.ID] = struct{}{}
	}
}

func (r *runner) recordPair(decision relationDecision) {
	if decision.SourcePath == "" || decision.TargetPath == "" || decision.RelationKind == "" {
		return
	}
	key := decision.SourcePath + "\n" + decision.TargetPath + "\n" + decision.RelationKind
	current, ok := r.pairs[key]
	if !ok {
		r.pairs[key] = &pairAggregate{
			FromPath:     decision.SourcePath,
			ToPath:       decision.TargetPath,
			RelationKind: decision.RelationKind,
			Count:        1,
		}
		return
	}
	current.Count++
}

func isSupplyChainEdge(edge *models.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	if qualifierBool(edge.Qualifiers, "fromSupplyChain") {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(edge.Provenance.Method)), "supply") {
		return true
	}
	if strings.Contains(strings.TrimSpace(qualifierString(edge.Qualifiers, "artifactKey")), "/supply-chain/") {
		return true
	}
	return false
}

func relationKindForEdgeType(edgeType models.DependencyType) string {
	switch edgeType {
	case models.DependencyTypeAssemblesInto:
		return "assembly"
	case models.DependencyTypeUsedIn:
		return "usage"
	default:
		return "transformation"
	}
}

func isMeaningfulSupplyProductPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return false
	}
	rejectTokens := []string{
		"/service/",
		"/logistics/",
		"/delivery/",
		"/distribution/",
		"/shipping/",
		"/freight/",
		"/handling/",
		"/maintenance/",
		"/cargo",
		"/route",
		"/document",
		"/compliance",
	}
	for _, token := range rejectTokens {
		if strings.Contains(path, token) {
			return false
		}
	}
	return true
}

func productCategory(path string) string {
	path = strings.TrimSpace(strings.Trim(path, "/"))
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "product" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[1]))
}

func lastPathSegment(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func normalizeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", "(", " ", ")", " ", ",", " ", ":", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func isMeaningfulHeuristicCategoryPair(sourceCategory, targetCategory string) bool {
	sourceCategory = strings.ToLower(strings.TrimSpace(sourceCategory))
	targetCategory = strings.ToLower(strings.TrimSpace(targetCategory))
	if sourceCategory == "" || targetCategory == "" {
		return false
	}
	if sourceCategory == "finished" {
		return false
	}
	if targetCategory == "raw" {
		return false
	}
	if sourceCategory == "raw" && targetCategory == "raw" {
		return false
	}
	return true
}

func transformPairDocID(fromPath, toPath string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(fromPath) + "\n" + strings.TrimSpace(toPath)))
	return hex.EncodeToString(sum[:])
}

func qualifierBool(qualifiers map[string]interface{}, key string) bool {
	if qualifiers == nil {
		return false
	}
	value, ok := qualifiers[strings.TrimSpace(key)]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	default:
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		return s == "true" || s == "1" || s == "yes"
	}
}

func qualifierString(qualifiers map[string]interface{}, key string) string {
	if qualifiers == nil {
		return ""
	}
	value, ok := qualifiers[strings.TrimSpace(key)]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func cloneQualifiers(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		if key == "" || value == nil {
			continue
		}
		out[key] = value
	}
	return out
}

func fragmentsCollapseToSelfLoop(sourceFrag, targetFrag *models.HorizonFragment, sourcePath, targetPath string) bool {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(targetPath) == "" {
		return false
	}
	if strings.TrimSpace(sourcePath) == strings.TrimSpace(targetPath) {
		return true
	}
	sourceID := fragmentID(sourceFrag)
	targetID := fragmentID(targetFrag)
	return sourceID != "" && sourceID == targetID
}

func (r *runner) canonicalizeArtifactProductPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if r == nil || r.productDict == nil {
		return path
	}
	canonical, _, err := r.productDict.CanonicalizeProductPathForTenant(r.ctx, r.tenantID, path)
	if err != nil || strings.TrimSpace(canonical) == "" {
		return path
	}
	return canonical
}

func fragmentID(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	return strings.TrimSpace(frag.ID)
}

func fragmentName(frag *models.HorizonFragment, fallbackPath string) string {
	if frag != nil && frag.Labels != nil {
		if value := strings.TrimSpace(frag.Labels["en"]); value != "" {
			return value
		}
	}
	segment := strings.TrimSpace(fallbackPath)
	if idx := strings.LastIndex(segment, "/"); idx >= 0 && idx+1 < len(segment) {
		segment = segment[idx+1:]
	}
	segment = strings.ReplaceAll(segment, "-", " ")
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return fallbackPath
	}
	return strings.Title(segment)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
