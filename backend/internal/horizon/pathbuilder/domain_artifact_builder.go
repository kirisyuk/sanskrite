package pathbuilder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
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
	profile := contractsemantics.ProfileForDomainMode(domainMode)

	// Collect a bounded undirected neighborhood around the root.
	// This keeps ecosystem/influence artifacts useful for cross-border graphs
	// without letting a high-degree root explode into an unbounded traversal.
	const maxArtifactHops = 2
	allEdges := make([]*models.DependencyEdge, 0, 256)
	visitedNodes := map[string]struct{}{rootFrag.ID: struct{}{}}
	frontier := []string{rootFrag.ID}
	for hop := 0; hop <= maxArtifactHops && len(frontier) > 0; hop++ {
		next := make([]string, 0, len(frontier)*2)
		nextSeen := map[string]struct{}{}
		for _, nodeID := range frontier {
			outEdges, _ := b.edgesRepo.ListBySource(ctx, tenantID, nodeID, nil, 200)
			inEdges, _ := b.edgesRepo.ListByTarget(ctx, tenantID, nodeID, nil, 200)
			allEdges = append(allEdges, outEdges...)
			allEdges = append(allEdges, inEdges...)
			if hop == maxArtifactHops {
				continue
			}
			for _, e := range append(outEdges, inEdges...) {
				if !isAllowedEdge(e.Type, allowedEdgeTypes) {
					continue
				}
				for _, neighborID := range []string{e.SourceFragmentID, e.TargetFragmentID} {
					if neighborID == "" || neighborID == nodeID {
						continue
					}
					if _, seen := visitedNodes[neighborID]; seen {
						continue
					}
					if _, seen := nextSeen[neighborID]; seen {
						continue
					}
					nextSeen[neighborID] = struct{}{}
					next = append(next, neighborID)
				}
			}
		}
		for _, nodeID := range next {
			visitedNodes[nodeID] = struct{}{}
		}
		frontier = next
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

	notes := map[string]interface{}{
		"graphLayer":         string(profile.PrimaryLayer),
		"allowedEdges":       dependencyTypesToStrings(profile.AllowedEdges),
		"overlayEdges":       dependencyTypesToStrings(profile.OverlayEdges),
		"interpretiveEdges":  dependencyTypesToStrings(profile.InterpretiveEdges),
		"complementaryModes": append([]string(nil), profile.ComplementaryModes...),
	}
	payload := models.BuildRelationsGraphPayload(nodes, graphEdges, domainMode, rootCanonicalPath, notes)

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
		GeoRefs:      collectArtifactGeoRefs(nodeRefs),
		Labels: map[string]string{
			"domainMode":   domainMode,
			"rootEntity":   rootCanonicalPath,
			"nodeCount":    fmt.Sprintf("%d", len(nodes)),
			"edgeCount":    fmt.Sprintf("%d", len(graphEdges)),
			"graphLayer":   string(profile.PrimaryLayer),
			"artifactKind": string(kind),
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
	case "governance":
		return models.MaterializedKindGovernanceChain
	case "policy_impact":
		return models.MaterializedKindRegulatoryImpactMap
	case "ecosystem":
		return models.MaterializedKindEcosystemMap
	default:
		return ""
	}
}

func domainEdgeTypes(domainMode string) []string {
	profile := contractsemantics.ProfileForDomainMode(domainMode)
	if len(profile.AllowedEdges) == 0 {
		return nil
	}
	return dependencyTypesToStrings(profile.AllowedEdges)
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

func dependencyTypesToStrings(values []models.DependencyType) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" {
			continue
		}
		out = append(out, string(value))
	}
	sort.Strings(out)
	return out
}

func collectArtifactGeoRefs(nodeRefs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(nodeRefs))
	for _, ref := range nodeRefs {
		geoRef := artifactGeoRef(ref)
		if geoRef == "" {
			continue
		}
		if _, ok := seen[geoRef]; ok {
			continue
		}
		seen[geoRef] = struct{}{}
		out = append(out, geoRef)
	}
	sort.Strings(out)
	return out
}

func artifactGeoRef(ref string) string {
	parts, err := taxonomy.ParseCanonicalPathParts(strings.TrimSpace(ref))
	if err != nil || parts == nil {
		return ""
	}
	geo := ""
	switch parts.Namespace {
	case "loc":
		if parts.Loc != nil {
			geo = parts.Loc.Country
		}
	case "org":
		if parts.Org != nil {
			geo = parts.Org.Geo
		}
	case "gov":
		if parts.Gov != nil {
			geo = parts.Gov.Geo
		}
	case "fac":
		if parts.Fac != nil {
			geo = parts.Fac.Geo
		}
	case "market":
		if parts.Market != nil {
			geo = parts.Market.Geo
		}
	case "community":
		if parts.Community != nil {
			geo = parts.Community.Geo
		}
	case "program":
		if parts.Program != nil {
			geo = parts.Program.Jurisdiction
		}
	}
	geo = strings.ToLower(strings.TrimSpace(geo))
	if geo == "" || geo == "na" || geo == "int" {
		return ""
	}
	return "/loc/" + geo
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
