package scout

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type preparedSupplyChainEdgeCandidate struct {
	edge            *models.DependencyEdge
	sourceID        string
	targetID        string
	sourcePath      string
	targetPath      string
	artifactKey     string
	variantKey      string
	dedupKey        string
	contextEdge     *ChainEdge
	rawConfidence   float64
	originalSrcPath string
	originalDstPath string
}

func buildSupplyChainEdgeDedupKey(sourceID, targetID string, edgeType DependencyType, qualifiers map[string]interface{}) string {
	return strings.Join([]string{
		strings.TrimSpace(sourceID),
		strings.TrimSpace(targetID),
		strings.TrimSpace(string(edgeType)),
		chainEdgeVariantKey(string(edgeType), qualifiers),
	}, "|")
}

func collectExistingSupplyChainEdgeDedupKeys(
	ctx context.Context,
	edgesRepo interface {
		ListBySourceIDsAndType(context.Context, string, []string, DependencyType) ([]*models.DependencyEdge, error)
	},
	tenantID string,
	candidates []*preparedSupplyChainEdgeCandidate,
) map[string]struct{} {
	out := make(map[string]struct{})
	if edgesRepo == nil || len(candidates) == 0 {
		return out
	}
	typeToSources := make(map[DependencyType]map[string]struct{})
	typeToTargets := make(map[DependencyType]map[string]map[string]struct{})
	for _, candidate := range candidates {
		if candidate == nil || candidate.edge == nil {
			continue
		}
		if _, ok := typeToSources[candidate.edge.Relation]; !ok {
			typeToSources[candidate.edge.Relation] = map[string]struct{}{}
		}
		typeToSources[candidate.edge.Relation][candidate.sourceID] = struct{}{}
		if _, ok := typeToTargets[candidate.edge.Relation]; !ok {
			typeToTargets[candidate.edge.Relation] = map[string]map[string]struct{}{}
		}
		if _, ok := typeToTargets[candidate.edge.Relation][candidate.sourceID]; !ok {
			typeToTargets[candidate.edge.Relation][candidate.sourceID] = map[string]struct{}{}
		}
		typeToTargets[candidate.edge.Relation][candidate.sourceID][candidate.targetID] = struct{}{}
	}
	for edgeType, sourceSet := range typeToSources {
		sourceIDs := make([]string, 0, len(sourceSet))
		for sourceID := range sourceSet {
			sourceIDs = append(sourceIDs, sourceID)
		}
		sort.Strings(sourceIDs)
		existing, err := edgesRepo.ListBySourceIDsAndType(ctx, tenantID, sourceIDs, edgeType)
		if err != nil {
			continue
		}
		for _, edge := range existing {
			if edge == nil {
				continue
			}
			targets := typeToTargets[edgeType][strings.TrimSpace(edge.SourceFragmentID)]
			if len(targets) == 0 {
				continue
			}
			if _, ok := targets[strings.TrimSpace(edge.TargetFragmentID)]; !ok {
				continue
			}
			out[buildSupplyChainEdgeDedupKey(edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation, edge.Qualifiers)] = struct{}{}
		}
	}
	return out
}

func (e *ScoutEngine) launchDeferredSupplyChainRebuild(job *models.ScoutingJob, segments []*SupplyLinkSegment) {
	if e == nil || e.supplyChainPathBuilder == nil || len(segments) == 0 {
		return
	}
	tenantID := "global"
	if job != nil && strings.TrimSpace(job.TenantID) != "" {
		tenantID = strings.TrimSpace(job.TenantID)
	}
	clone := append([]*SupplyLinkSegment(nil), segments...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		seen := map[string]struct{}{}
		rebuiltCount := 0
		for _, seg := range clone {
			if seg == nil {
				continue
			}
			for _, ref := range []string{strings.TrimSpace(seg.SupplierRef), strings.TrimSpace(seg.CustomerRef)} {
				if ref == "" {
					continue
				}
				if _, ok := seen[ref]; ok {
					continue
				}
				seen[ref] = struct{}{}
				affected, err := e.supplyChainPathBuilder.FindChainsByNodeRef(ctx, tenantID, ref)
				if err != nil {
					continue
				}
				for _, art := range affected {
					if art == nil || art.Labels == nil {
						continue
					}
					leafCustomer := strings.TrimSpace(art.Labels["anchorCustomer"])
					if leafCustomer == "" {
						continue
					}
					if err := e.supplyChainPathBuilder.RebuildFromSupplyLinks(ctx, art, leafCustomer); err == nil {
						rebuiltCount++
					}
				}
			}
		}
		if rebuiltCount > 0 {
			log.Printf("[SupplyChainRebuildDeferred] tenant=%s rebuilt=%d", tenantID, rebuiltCount)
		}
	}()
}

func supplyChainSummaryDescription(discoveredNodes, materializedFragments, materializedEdges, materializedSupplyLinks, materializedPaths, gapCount int) string {
	parts := make([]string, 0, 5)
	if discoveredNodes > 0 {
		parts = append(parts, fmt.Sprintf("%d discovered nodes", discoveredNodes))
	}
	if materializedFragments > 0 {
		parts = append(parts, fmt.Sprintf("%d fragments saved", materializedFragments))
	}
	if materializedEdges > 0 {
		parts = append(parts, fmt.Sprintf("%d edges saved", materializedEdges))
	}
	if materializedSupplyLinks > 0 {
		parts = append(parts, fmt.Sprintf("%d supply links saved", materializedSupplyLinks))
	}
	if materializedPaths > 0 {
		parts = append(parts, fmt.Sprintf("%d paths saved", materializedPaths))
	}
	if gapCount > 0 {
		parts = append(parts, fmt.Sprintf("%d gaps identified", gapCount))
	}
	if len(parts) == 0 {
		return "No supply-chain changes"
	}
	return strings.Join(parts, " • ")
}
