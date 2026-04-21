package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/lensresearch"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func parseDirectedExplorationConfig(job *horizonmodels.ScoutingJob) *horizonmodels.DirectedExplorationConfig {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State["directedExploration"]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case *horizonmodels.DirectedExplorationConfig:
		return t
	case horizonmodels.DirectedExplorationConfig:
		cp := t
		return &cp
	}
	b, err := json.Marshal(raw)
	if err != nil || len(b) == 0 {
		return nil
	}
	var cfg horizonmodels.DirectedExplorationConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil
	}
	if strings.TrimSpace(cfg.DomainMode) == "" {
		cfg.DomainMode = taxonomy.DomainModeSupplyChain
	}
	return &cfg
}

func isDirectedSupplyChainExplorationJob(job *horizonmodels.ScoutingJob) bool {
	cfg := parseDirectedExplorationConfig(job)
	return cfg != nil && strings.TrimSpace(strings.ToLower(cfg.DomainMode)) == taxonomy.DomainModeSupplyChain
}

func directedExplorationShouldIgnoreFreshness(job *horizonmodels.ScoutingJob) bool {
	if job == nil {
		return false
	}
	cfg := parseDirectedExplorationConfig(job)
	manualOverride := parseStateBool(job.State["ignoreFreshnessForUserRequest"], false) ||
		parseStateBool(job.State["forceDeepScout"], false)
	if triggerOrigin := strings.TrimSpace(parseStateString(job.State["triggerOrigin"])); triggerOrigin == "api_manual" {
		manualOverride = true
	}
	if cfg == nil {
		return manualOverride
	}
	return cfg.IgnoreFreshness || manualOverride
}

func directedExplorationRenderTarget(job *horizonmodels.ScoutingJob) string {
	cfg := parseDirectedExplorationConfig(job)
	if cfg == nil {
		return "graph"
	}
	switch strings.TrimSpace(strings.ToLower(cfg.RenderTarget)) {
	case "map":
		return "map"
	default:
		return "graph"
	}
}

func directedExplorationHotPathOnly(job *horizonmodels.ScoutingJob) bool {
	cfg := parseDirectedExplorationConfig(job)
	return cfg != nil && cfg.HotPathOnly
}

func directedExplorationWantsCapabilityPass(job *horizonmodels.ScoutingJob) bool {
	cfg := parseDirectedExplorationConfig(job)
	if cfg == nil {
		return true
	}
	return cfg.IncludeCapabilities || cfg.IncludeEdgeMetrics
}

func directedExplorationDirection(job *horizonmodels.ScoutingJob) string {
	cfg := parseDirectedExplorationConfig(job)
	if cfg == nil {
		return "downstream"
	}
	switch strings.TrimSpace(strings.ToLower(cfg.Direction)) {
	case "up", "upstream":
		return "upstream"
	default:
		return "downstream"
	}
}

func directedExplorationAnchorCanonicalPaths(job *horizonmodels.ScoutingJob) []string {
	if job == nil || job.State == nil {
		return nil
	}
	paths := parseStateStringSlice(job.State["hotPathCanonicalPaths"])
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		cp := strings.TrimSpace(raw)
		if cp == "" {
			continue
		}
		if !(strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") || strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/deposit/")) {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
	}
	return out
}

func directedExplorationCustomQuery(job *horizonmodels.ScoutingJob) string {
	cfg := parseDirectedExplorationConfig(job)
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.CustomQuery)
}

func supplyChainAnchorTypeForPath(cp string) string {
	cp = strings.TrimSpace(strings.ToLower(cp))
	switch {
	case strings.HasPrefix(cp, "/product/"):
		return "product"
	case strings.HasPrefix(cp, "/deposit/"):
		return "deposit"
	default:
		return "org"
	}
}

func applyDirectedExplorationQueryAugment(query agents.SupplyChainQuery, job *horizonmodels.ScoutingJob) agents.SupplyChainQuery {
	customQuery := directedExplorationCustomQuery(job)
	if customQuery == "" {
		return query
	}
	baseReason := strings.TrimSpace(query.Reason)
	if baseReason == "" {
		query.Reason = fmt.Sprintf("Directed exploration request: %s", customQuery)
	} else {
		query.Reason = fmt.Sprintf("%s | Directed exploration request: %s", baseReason, customQuery)
	}
	return query
}

func buildDirectedSupplyChainQueries(job *horizonmodels.ScoutingJob) []agents.SupplyChainQuery {
	if job == nil {
		return nil
	}
	targetPath := strings.TrimSpace(firstNonEmpty(
		strings.TrimSpace(job.TargetCanonicalPath),
		parseStateString(job.State["targetCanonicalPath"]),
	))
	if targetPath == "" {
		return nil
	}
	query := agents.SupplyChainQuery{
		Anchor:     targetPath,
		AnchorType: supplyChainAnchorTypeForPath(targetPath),
		Direction:  directedExplorationDirection(job),
		Depth:      4,
		Reason:     "Directed supply-chain exploration from lens",
	}
	if maxDepth := parseStateInt(job.State["maxRecursionDepth"], 0); maxDepth > 0 && maxDepth < 7 {
		query.Depth = maxDepth + 2
		if query.Depth < 3 {
			query.Depth = 3
		}
		if query.Depth > 6 {
			query.Depth = 6
		}
	}
	if anchors := directedExplorationAnchorCanonicalPaths(job); len(anchors) > 0 {
		query.Anchors = anchors
		if !strings.HasPrefix(strings.ToLower(targetPath), "/product/") {
			query.Anchor = ""
			query.AnchorType = "org"
		}
	}
	query = applyDirectedExplorationQueryAugment(query, job)
	return []agents.SupplyChainQuery{query}
}

func (e *ScoutEngine) runDirectedSupplyChainExploration(ctx context.Context, job *horizonmodels.ScoutingJob) {
	if e == nil || job == nil || e.researchAgent == nil || !isDirectedSupplyChainExplorationJob(job) {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	if parseStateBool(job.State["directedExplorationCompleted"], false) {
		return
	}
	rt := newDirectedSupplyChainExplorationRuntime(job)
	if !rt.ensureQueries(ctx, e, job) {
		return
	}
	for _, tool := range directedSupplyChainExplorationTools(job) {
		startedAt := time.Now().UTC()
		run := tool.Run(ctx, e, job, rt)
		finishedAt := time.Now().UTC()
		appendDirectedExplorationToolRun(job, tool.Name(), run, startedAt, finishedAt)
		if run.Error != nil {
			recordPipelineIssue(job, "directed_exploration."+tool.Name(), run.Error.Error())
		}
	}
	job.State["directedExplorationCompleted"] = true
}

func (e *ScoutEngine) publishDirectedExplorationMaterialized(ctx context.Context, job *horizonmodels.ScoutingJob, stage string, direction string, chains []agents.SupplyChain) {
	if e == nil || job == nil || !isDirectedSupplyChainExplorationJob(job) || len(chains) == 0 {
		return
	}
	type edgePayload struct {
		EdgeID              string         `json:"edgeId"`
		SourceCanonicalPath string         `json:"sourceCanonicalPath"`
		TargetCanonicalPath string         `json:"targetCanonicalPath"`
		Type                string         `json:"type"`
		Qualifiers          map[string]any `json:"qualifiers,omitempty"`
	}
	type nodePayload struct {
		CanonicalPath string `json:"canonicalPath"`
		Label         string `json:"label,omitempty"`
		Type          string `json:"type,omitempty"`
	}

	nodes := make([]nodePayload, 0, 32)
	nodeSeen := make(map[string]struct{}, 64)
	edges := make([]edgePayload, 0, 64)
	edgeSeen := make(map[string]struct{}, 128)
	artifacts := make([]map[string]any, 0, len(chains))
	researchRefs := make([]string, 0, 64)

	for chainIdx, chain := range chains {
		artifactNodes := make([]map[string]any, 0, len(chain.Nodes))
		legs := make([]map[string]any, 0)
		for idx, node := range chain.Nodes {
			cp := strings.TrimSpace(node.CanonicalPath)
			if cp == "" {
				continue
			}
			if _, ok := nodeSeen[cp]; !ok {
				nodeSeen[cp] = struct{}{}
				nodes = append(nodes, nodePayload{
					CanonicalPath: cp,
					Label:         strings.TrimSpace(firstNonEmpty(node.Name, humanizeCanonicalPathFallback(cp))),
					Type:          strings.TrimSpace(node.Type),
				})
			}
			researchRefs = append(researchRefs, cp)
			artifactNodes = append(artifactNodes, map[string]any{
				"type": strings.TrimSpace(node.Type),
				"ref":  cp,
				"name": strings.TrimSpace(node.Name),
			})
			if idx+1 >= len(chain.Nodes) {
				continue
			}
			next := chain.Nodes[idx+1]
			nextCP := strings.TrimSpace(next.CanonicalPath)
			if nextCP == "" {
				continue
			}
			edgeType := "supplies"
			switch {
			case strings.EqualFold(strings.TrimSpace(node.Type), "PRODUCT") && strings.EqualFold(strings.TrimSpace(next.Type), "PRODUCT"):
				edgeType = "transforms_into"
			case strings.EqualFold(strings.TrimSpace(next.Type), "PRODUCT"):
				edgeType = "produces"
			case strings.EqualFold(strings.TrimSpace(node.Type), "PRODUCT"):
				edgeType = "supplies"
			}
			edgeID := fmt.Sprintf("directed:%s:%s:%s:%d:%d", strings.TrimSpace(job.ID), cp, nextCP, chainIdx, idx)
			if _, ok := edgeSeen[edgeID]; !ok {
				edgeSeen[edgeID] = struct{}{}
				edges = append(edges, edgePayload{
					EdgeID:              edgeID,
					SourceCanonicalPath: cp,
					TargetCanonicalPath: nextCP,
					Type:                edgeType,
					Qualifiers: map[string]any{
						"fromDirectedExploration": true,
						"stage":                   stage,
					},
				})
			}
			if idx+2 < len(chain.Nodes) && strings.EqualFold(strings.TrimSpace(chain.Nodes[idx+1].Type), "PRODUCT") {
				legs = append(legs, map[string]any{
					"index":            len(legs),
					"supplierRef":      cp,
					"productRef":       strings.TrimSpace(chain.Nodes[idx+1].CanonicalPath),
					"customerRef":      strings.TrimSpace(chain.Nodes[idx+2].CanonicalPath),
					"transportViaRefs": append([]string(nil), chain.TransportVia...),
				})
			}
		}
		if len(artifactNodes) == 0 {
			continue
		}
		anchorSupplier := strings.TrimSpace(artifactNodes[0]["ref"].(string))
		anchorCustomer := strings.TrimSpace(artifactNodes[len(artifactNodes)-1]["ref"].(string))
		artifacts = append(artifacts, map[string]any{
			"id":             fmt.Sprintf("directed:%s:%d", strings.TrimSpace(job.ID), chainIdx),
			"canonicalKey":   fmt.Sprintf("/artifact/directed/%s/%d", strings.TrimSpace(job.ID), chainIdx),
			"displayName":    buildDirectedArtifactDisplayName(chain),
			"anchorSupplier": anchorSupplier,
			"anchorCustomer": anchorCustomer,
			"nodeRefs":       collectDirectedNodeRefs(chain),
			"payload": map[string]any{
				"v":     1,
				"nodes": artifactNodes,
				"legs":  legs,
			},
		})
	}
	if lensID := strings.TrimSpace(job.LensID); lensID != "" {
		e.bestEffortMergeLensResearchObjects(
			ctx,
			lensID,
			lensresearch.BuildFromCanonicalPaths(
				researchRefs,
				lensresearch.SourceKindMaterializedNodeRef,
				time.Now().UTC(),
				strings.TrimSpace(job.ID),
			),
		)
		e.publish(ctx, realtime.EventType("horizon.lens.updated"), map[string]any{
			"tenantId":  job.TenantID,
			"projectId": job.ProjectID,
			"lensId":    lensID,
			"jobId":     strings.TrimSpace(job.ID),
			"reason":    "directed_exploration_materialized",
		})
	}

	e.publish(ctx, realtime.EventType("horizon.supply_chain.exploration.materialized"), map[string]any{
		"tenantId":      job.TenantID,
		"projectId":     job.ProjectID,
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         strings.TrimSpace(job.ID),
		"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"direction":     direction,
		"renderTarget":  directedExplorationRenderTarget(job),
		"stage":         stage,
		"nodes":         nodes,
		"edges":         edges,
		"artifacts":     artifacts,
	})
	for _, node := range nodes {
		e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]any{
			"tenantId":      job.TenantID,
			"projectId":     job.ProjectID,
			"lensId":        strings.TrimSpace(job.LensID),
			"fragmentId":    "",
			"canonicalPath": node.CanonicalPath,
			"type":          node.Type,
			"label":         node.Label,
			"status":        "discovered",
			"qualifiers": map[string]any{
				"fromDirectedExploration": true,
				"stage":                   stage,
				"direction":               direction,
			},
		})
	}
	for _, edge := range edges {
		e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]any{
			"tenantId":            job.TenantID,
			"projectId":           job.ProjectID,
			"lensId":              strings.TrimSpace(job.LensID),
			"edgeId":              edge.EdgeID,
			"sourceCanonicalPath": edge.SourceCanonicalPath,
			"targetCanonicalPath": edge.TargetCanonicalPath,
			"type":                edge.Type,
			"qualifiers":          edge.Qualifiers,
		})
	}
}

func buildDirectedArtifactDisplayName(chain agents.SupplyChain) string {
	maxNames := len(chain.Nodes)
	if maxNames > 3 {
		maxNames = 3
	}
	names := make([]string, 0, maxNames)
	for _, node := range chain.Nodes {
		label := strings.TrimSpace(firstNonEmpty(node.Name, humanizeCanonicalPathFallback(node.CanonicalPath)))
		if label == "" {
			continue
		}
		names = append(names, label)
		if len(names) >= 3 {
			break
		}
	}
	if len(names) == 0 {
		return "Directed supply chain"
	}
	return strings.Join(names, " -> ")
}

func collectDirectedNodeRefs(chain agents.SupplyChain) []string {
	out := make([]string, 0, len(chain.Nodes))
	seen := make(map[string]struct{}, len(chain.Nodes))
	for _, node := range chain.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
	}
	return out
}

func humanizeCanonicalPathFallback(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return ""
	}
	parts := strings.Split(cp, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(parts[i]); s != "" {
			return strings.ReplaceAll(s, "-", " ")
		}
	}
	return cp
}
