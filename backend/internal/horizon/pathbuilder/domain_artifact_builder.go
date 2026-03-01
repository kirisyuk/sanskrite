package pathbuilder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// DomainArtifactBuilder builds materialized artifacts for non-supply-chain domains
// (governance chains, regulatory impact maps, ecosystem maps).
type DomainArtifactBuilder struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	artifactsRepo *repo.MaterializedArtifactsRepository
}

// NewDomainArtifactBuilder creates a new builder.
func NewDomainArtifactBuilder(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	artifactsRepo *repo.MaterializedArtifactsRepository,
) *DomainArtifactBuilder {
	return &DomainArtifactBuilder{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		artifactsRepo: artifactsRepo,
	}
}

// MaterializeRelationsGraph builds and persists a domain-specific relations graph artifact.
// It collects all edges reachable from rootCanonicalPath (filtered by domain edge allowlist),
// resolves the participating fragments, and materializes them into the artifact store.
func (b *DomainArtifactBuilder) MaterializeRelationsGraph(
	ctx context.Context,
	tenantID, projectID, lensID string,
	rootCanonicalPath string,
	domainMode string,
	displayName string,
) (*models.MaterializedArtifact, error) {
	if b.edgesRepo == nil || b.artifactsRepo == nil || b.fragmentsRepo == nil {
		return nil, fmt.Errorf("repos not initialized")
	}

	kind := domainModeToKind(domainMode)
	if kind == "" {
		return nil, fmt.Errorf("unsupported domain mode for materialization: %s", domainMode)
	}

	// Resolve root fragment.
	rootFrag, err := b.fragmentsRepo.GetByCanonicalPath(ctx, rootCanonicalPath)
	if err != nil || rootFrag == nil {
		return nil, fmt.Errorf("root fragment not found: %s", rootCanonicalPath)
	}

	allowedEdgeTypes := domainEdgeTypes(domainMode)

	// Collect edges radiating from the root fragment (1 hop outbound + inbound).
	outEdges, _ := b.edgesRepo.ListBySource(ctx, tenantID, rootFrag.ID, nil, 200)
	inEdges, _ := b.edgesRepo.ListByTarget(ctx, tenantID, rootFrag.ID, nil, 200)

	allEdges := append(outEdges, inEdges...)

	// Collect neighbor fragment IDs for BFS hop-2.
	neighborIDs := map[string]bool{}
	for _, e := range allEdges {
		if isAllowedEdge(e.Type, allowedEdgeTypes) {
			neighborIDs[e.SourceFragmentID] = true
			neighborIDs[e.TargetFragmentID] = true
		}
	}

	for nID := range neighborIDs {
		if nID == rootFrag.ID {
			continue
		}
		hop2, _ := b.edgesRepo.ListBySource(ctx, tenantID, nID, nil, 100)
		allEdges = append(allEdges, hop2...)
	}

	// Deduplicate edges and filter by allowlist.
	type edgeKey struct{ src, tgt, rel string }
	seen := map[edgeKey]bool{}
	var filteredEdges []*models.DependencyEdge
	for _, e := range allEdges {
		relType := strings.TrimSpace(string(e.Type))
		if !isAllowedEdge(e.Type, allowedEdgeTypes) {
			continue
		}
		k := edgeKey{e.SourceFragmentID, e.TargetFragmentID, relType}
		if seen[k] {
			continue
		}
		seen[k] = true
		filteredEdges = append(filteredEdges, e)
	}

	if len(filteredEdges) == 0 {
		return nil, nil
	}

	// Collect all unique fragment IDs.
	fragIDSet := map[string]bool{rootFrag.ID: true}
	for _, e := range filteredEdges {
		fragIDSet[e.SourceFragmentID] = true
		fragIDSet[e.TargetFragmentID] = true
	}

	// Resolve fragments → build nodes.
	fragByID := map[string]*models.HorizonFragment{rootFrag.ID: rootFrag}
	for fID := range fragIDSet {
		if _, ok := fragByID[fID]; ok {
			continue
		}
		frag, err := b.fragmentsRepo.Get(ctx, fID)
		if err != nil || frag == nil {
			continue
		}
		fragByID[fID] = frag
	}

	// Sort fragments for deterministic output.
	sortedIDs := make([]string, 0, len(fragByID))
	for id := range fragByID {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	nodeIndex := map[string]int{}
	nodes := make([]models.RelationsGraphNode, 0, len(sortedIDs))
	nodeRefs := make([]string, 0, len(sortedIDs))
	for i, id := range sortedIDs {
		frag := fragByID[id]
		nodeIndex[id] = i
		name := ""
		if frag.Labels != nil {
			name = strings.TrimSpace(frag.Labels["en"])
		}
		if name == "" {
			name = inferNameFromPath(frag.CanonicalPath)
		}
		nodes = append(nodes, models.RelationsGraphNode{
			Type: strings.ToLower(string(frag.Type)),
			Ref:  frag.CanonicalPath,
			Name: name,
		})
		nodeRefs = append(nodeRefs, frag.CanonicalPath)
	}

	// Build edge list.
	graphEdges := make([]models.RelationsGraphEdge, 0, len(filteredEdges))
	for _, e := range filteredEdges {
		fromIdx, fOK := nodeIndex[e.SourceFragmentID]
		toIdx, tOK := nodeIndex[e.TargetFragmentID]
		if !fOK || !tOK {
			continue
		}
		graphEdges = append(graphEdges, models.RelationsGraphEdge{
			From:     fromIdx,
			To:       toIdx,
			Relation: string(e.Type),
		})
	}

	canonicalKey := buildRelationsGraphCanonicalKey(tenantID, domainMode, rootCanonicalPath, nodeRefs)

	if displayName == "" {
		rootName := inferNameFromPath(rootCanonicalPath)
		if rootFrag.Labels != nil {
			if en := strings.TrimSpace(rootFrag.Labels["en"]); en != "" {
				rootName = en
			}
		}
		displayName = fmt.Sprintf("%s: %s", kind, rootName)
	}

	payload := models.BuildRelationsGraphPayload(nodes, graphEdges, domainMode, rootCanonicalPath, nil)

	artifact := &models.MaterializedArtifact{
		TenantID:     tenantID,
		ProjectID:    projectID,
		LensID:       lensID,
		Kind:         kind,
		CanonicalKey: canonicalKey,
		Payload:      payload,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "domain_artifact_builder",
		UpdatedBy:    "domain_artifact_builder",
		Status:       "active",
		DisplayName:  displayName,
		NodeRefs:     nodeRefs,
		Labels: map[string]string{
			"domainMode": domainMode,
			"rootEntity": rootCanonicalPath,
			"nodeCount":  fmt.Sprintf("%d", len(nodes)),
			"edgeCount":  fmt.Sprintf("%d", len(graphEdges)),
		},
	}

	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("validate artifact: %w", err)
	}

	// Upsert by canonical key.
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, kind, canonicalKey)
	if err == nil && existing != nil {
		artifact.ID = existing.ID
		artifact.CreatedAt = existing.CreatedAt
		artifact.CreatedBy = existing.CreatedBy
	}

	if err := b.artifactsRepo.Upsert(ctx, artifact); err != nil {
		return nil, fmt.Errorf("upsert artifact: %w", err)
	}
	if artifact.ID == "" {
		if stored, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, kind, canonicalKey); err == nil && stored != nil {
			artifact.ID = stored.ID
		}
	}
	return artifact, nil
}

func domainModeToKind(domainMode string) models.MaterializedKind {
	switch domainMode {
	case taxonomy.DomainModeGovernance:
		return models.MaterializedKindGovernanceChain
	case taxonomy.DomainModePolicyImpact:
		return models.MaterializedKindRegulatoryImpactMap
	case taxonomy.DomainModeEcosystem:
		return models.MaterializedKindEcosystemMap
	default:
		return ""
	}
}

func domainEdgeTypes(domainMode string) []string {
	switch domainMode {
	case taxonomy.DomainModeGovernance:
		return taxonomy.GovernanceEdgeTypes
	case taxonomy.DomainModePolicyImpact:
		return []string{"REGULATES", "CONTROLS_ACCESS", "IMPOSES_CONDITION", "USES_MECHANISM", "APPLIES_TO", "GATEKEEPS_DECISION", "INFLUENCES", "DEPENDS_ON", "OPERATES_IN"}
	case taxonomy.DomainModeEcosystem:
		return taxonomy.EcosystemEdgeTypes
	default:
		return nil
	}
}

func isAllowedEdge(edgeType models.DependencyType, allowed []string) bool {
	t := strings.TrimSpace(string(edgeType))
	for _, a := range allowed {
		if strings.EqualFold(t, a) {
			return true
		}
	}
	return false
}

func inferNameFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	last := parts[len(parts)-1]
	return strings.ReplaceAll(strings.ReplaceAll(last, "-", " "), "_", " ")
}

func buildRelationsGraphCanonicalKey(tenantID, domainMode, rootPath string, nodeRefs []string) string {
	h := sha256.New()
	h.Write([]byte(tenantID + "|" + domainMode + "|" + rootPath + "|"))
	sorted := make([]string, len(nodeRefs))
	copy(sorted, nodeRefs)
	sort.Strings(sorted)
	h.Write([]byte(strings.Join(sorted, ",")))
	hash := fmt.Sprintf("%x", h.Sum(nil))[:16]
	kindPrefix := strings.ToLower(strings.ReplaceAll(domainMode, "_", "-"))
	return fmt.Sprintf("/%s/%s/%s", kindPrefix, tenantID, hash)
}
