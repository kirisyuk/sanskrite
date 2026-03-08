package agents

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// filterContains is a helper for checking slice inclusion case-insensitively.
func filterContains(slice []string, val string) bool {
	val = strings.TrimSpace(strings.ToUpper(val))
	for _, s := range slice {
		if strings.TrimSpace(strings.ToUpper(s)) == val {
			return true
		}
	}
	return false
}

// inferTypeFromPath returns a basic entity type based on canonical path prefix.
// Used for domain isolation filtering when explicit types are missing.
func inferTypeFromPath(path string) string {
	path = strings.TrimSpace(strings.ToLower(path))
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "ORGANIZATION"
	case strings.HasPrefix(path, "/fac/"):
		return "FACILITY"
	case strings.HasPrefix(path, "/product/"):
		return "PRODUCT"
	case strings.HasPrefix(path, "/deposit/"):
		return "DEPOSIT"
	case strings.HasPrefix(path, "/market/"):
		return "MARKET"
	case strings.HasPrefix(path, "/gov/"):
		return "ORGANIZATION"
	case strings.HasPrefix(path, "/person/"):
		return "PERSON"
	case strings.HasPrefix(path, "/community/"):
		return "COMMUNITY"
	case strings.HasPrefix(path, "/program/"):
		return "PROGRAM"
	case strings.HasPrefix(path, "/loc/") || strings.HasPrefix(path, "/geo/"):
		return "GEO_REGION"
	case strings.HasPrefix(path, "/decision/"):
		return "DECISION_POINT"
	case strings.HasPrefix(path, "/mechanism/"):
		return "MECHANISM"
	default:
		return ""
	}
}

// FilterDomainRelations drops relations that use disallowed edge types or connect disallowed entity types.
// It returns the filtered relations and the number of relations dropped.
func FilterDomainRelations(ctx context.Context, mode string, rels []DiscoveredRelation) ([]DiscoveredRelation, int) {
	if len(rels) == 0 {
		return rels, 0
	}

	profile := contractsemantics.ProfileForDomainMode(mode)
	allowedEdges := make([]string, 0, len(profile.AllowedEdges))
	for _, edgeType := range profile.AllowedEdges {
		allowedEdges = append(allowedEdges, string(edgeType))
	}
	allowedEntities := profile.AllowedEntities

	// If domain mode doesn't specify limits, allow all.
	if len(allowedEdges) == 0 && len(allowedEntities) == 0 {
		return rels, 0
	}

	var filtered []DiscoveredRelation
	dropped := 0

	for _, rel := range rels {
		// 1. Check Edge Type
		if len(allowedEdges) > 0 {
			normalizedType := taxonomy.NormalizeRelationType(rel.RelationType)
			if normalizedType == "" || !filterContains(allowedEdges, normalizedType) {
				dropped++
				trace.Log(ctx, "domain_isolation", "domain_isolation.exception", map[string]any{
					"mode":         mode,
					"reason":       "disallowed_edge_type",
					"relationType": rel.RelationType,
					"source":       rel.SourceCanonicalPath,
					"target":       rel.TargetCanonicalPath,
				})
				continue
			}
		}

		// 2. Check Entity Types (inferred from paths if explicit type is absent)
		if len(allowedEntities) > 0 {
			srcType := inferTypeFromPath(rel.SourceCanonicalPath)
			dstType := inferTypeFromPath(rel.TargetCanonicalPath)

			// Only strictly filter if we successfully inferred the type. If we can't infer it,
			// we allow it through here and rely on the downstream strict taxonomy validator to catch errors.
			if srcType != "" && !filterContains(allowedEntities, srcType) {
				dropped++
				trace.Log(ctx, "domain_isolation", "domain_isolation.exception", map[string]any{
					"mode":         mode,
					"reason":       "disallowed_source_entity_type",
					"entityType":   srcType,
					"path":         rel.SourceCanonicalPath,
					"relationType": rel.RelationType,
				})
				continue
			}

			if dstType != "" && !filterContains(allowedEntities, dstType) {
				dropped++
				trace.Log(ctx, "domain_isolation", "domain_isolation.exception", map[string]any{
					"mode":         mode,
					"reason":       "disallowed_target_entity_type",
					"entityType":   dstType,
					"path":         rel.TargetCanonicalPath,
					"relationType": rel.RelationType,
				})
				continue
			}
		}

		filtered = append(filtered, rel)
	}

	return filtered, dropped
}

// FilterDomainChains drops supply chains that contain invalid node types.
func FilterDomainChains(ctx context.Context, mode string, chains []SupplyChain) ([]SupplyChain, int) {
	if len(chains) == 0 {
		return chains, 0
	}

	allowedEntities := contractsemantics.ProfileForDomainMode(mode).AllowedEntities
	if len(allowedEntities) == 0 {
		return chains, 0
	}

	var filtered []SupplyChain
	dropped := 0

	for _, chain := range chains {
		isValid := true
		for _, node := range chain.Nodes {
			nodeType := strings.TrimSpace(strings.ToUpper(node.Type))
			if nodeType == "" {
				nodeType = inferTypeFromPath(node.CanonicalPath)
			}

			if nodeType != "" && !filterContains(allowedEntities, nodeType) {
				isValid = false
				trace.Log(ctx, "domain_isolation", "domain_isolation.exception", map[string]any{
					"mode":       mode,
					"reason":     "disallowed_chain_node_type",
					"entityType": nodeType,
					"path":       node.CanonicalPath,
					"name":       node.Name,
				})
				break
			}
		}

		if isValid {
			filtered = append(filtered, chain)
		} else {
			dropped++
		}
	}

	return filtered, dropped
}
