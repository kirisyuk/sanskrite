package scout

import (
	"context"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

type directedSupplyChainExplorationRuntime struct {
	queries      []agents.SupplyChainQuery
	direction    string
	renderTarget string
	unwindDepth  int

	downOrgs []string
	downFacs []string
	upOrgs   []string
	upFacs   []string

	downFilters supplyChainFilterScope
	upFilters   supplyChainFilterScope

	hotPathChains []agents.SupplyChain
}

func newDirectedSupplyChainExplorationRuntime(job *horizonmodels.ScoutingJob) *directedSupplyChainExplorationRuntime {
	rt := &directedSupplyChainExplorationRuntime{
		direction:    directedExplorationDirection(job),
		renderTarget: directedExplorationRenderTarget(job),
		unwindDepth:  1,
	}
	if job != nil && job.State != nil {
		if v := parseStateInt(job.State["maxRecursionDepth"], 0); v > 0 && v < 4 {
			rt.unwindDepth = v
		}
	}
	return rt
}

func (rt *directedSupplyChainExplorationRuntime) ensureQueries(ctx context.Context, e *ScoutEngine, job *horizonmodels.ScoutingJob) bool {
	if rt == nil || e == nil || job == nil {
		return false
	}
	if len(rt.queries) > 0 {
		return true
	}
	queries := buildDirectedSupplyChainQueries(job)
	if len(queries) == 0 {
		return false
	}
	rt.queries = queries
	if !(directedExplorationHotPathOnly(job) && strings.EqualFold(strings.TrimSpace(queries[0].AnchorType), "product")) {
		rt.queries = e.expandSupplyChainQueriesWithOrgs(ctx, job, queries)
	}
	rt.downOrgs, rt.downFacs, rt.upOrgs, rt.upFacs = collectAnchorOrgsFromQueriesByDirection(rt.queries, rt.direction)
	rt.downFilters, rt.upFilters = collectSupplyChainFiltersByDirection(rt.queries, rt.direction)
	return len(rt.queries) > 0
}

type directedSupplyChainExplorationTool interface {
	Name() string
	Run(ctx context.Context, e *ScoutEngine, job *horizonmodels.ScoutingJob, rt *directedSupplyChainExplorationRuntime) directedSupplyChainToolRun
}

type directedSupplyChainToolRun struct {
	Status  string
	Metrics map[string]any
	Error   error
}

type directedSupplyChainHotPathTool struct{}

func (directedSupplyChainHotPathTool) Name() string { return "supply_chain.hot_path" }

func (directedSupplyChainHotPathTool) Run(ctx context.Context, e *ScoutEngine, job *horizonmodels.ScoutingJob, rt *directedSupplyChainExplorationRuntime) directedSupplyChainToolRun {
	if rt == nil || !rt.ensureQueries(ctx, e, job) {
		return directedSupplyChainToolRun{
			Status: "skipped",
			Metrics: map[string]any{
				"reason": "no_queries",
			},
		}
	}
	rt.hotPathChains = e.runSupplyChainFastFirstHotPath(ctx, job, rt.queries)
	if len(rt.hotPathChains) > 0 {
		_ = e.materializeSupplyChainHotPathChains(ctx, job, rt.hotPathChains)
	}
	return directedSupplyChainToolRun{
		Status: "completed",
		Metrics: map[string]any{
			"queriesN":               len(rt.queries),
			"hotPathChainsN":         len(rt.hotPathChains),
			"downstreamOrgAnchorsN":  len(rt.downOrgs),
			"downstreamFacAnchorsN":  len(rt.downFacs),
			"upstreamOrgAnchorsN":    len(rt.upOrgs),
			"upstreamFacAnchorsN":    len(rt.upFacs),
			"includeCapabilities":    directedExplorationWantsCapabilityPass(job),
			"includeEdgeMetrics":     directedExplorationIncludeEdgeMetrics(job),
			"customQueryApplied":     directedExplorationCustomQuery(job) != "",
			"renderTarget":           rt.renderTarget,
			"resolvedHotPathOnly":    directedExplorationHotPathOnly(job),
			"resolvedDirection":      rt.direction,
			"resolvedHotPathNodeIds": len(directedExplorationAnchorCanonicalPaths(job)),
		},
	}
}

type directedSupplyChainDownstreamTool struct{}

func (directedSupplyChainDownstreamTool) Name() string { return "supply_chain.downstream_unwind" }

func (directedSupplyChainDownstreamTool) Run(ctx context.Context, e *ScoutEngine, job *horizonmodels.ScoutingJob, rt *directedSupplyChainExplorationRuntime) directedSupplyChainToolRun {
	if rt == nil || !rt.ensureQueries(ctx, e, job) {
		return directedSupplyChainToolRun{Status: "skipped", Metrics: map[string]any{"reason": "no_queries"}}
	}
	if len(rt.downOrgs) == 0 && len(rt.downFacs) == 0 {
		return directedSupplyChainToolRun{Status: "skipped", Metrics: map[string]any{"reason": "no_downstream_anchors"}}
	}
	chains := e.runSupplyChainRecursiveUnwinding(ctx, job, rt.downOrgs, rt.downFacs, rt.unwindDepth, rt.downFilters.Product, rt.downFilters.Geo)
	return directedSupplyChainToolRun{
		Status: "completed",
		Metrics: map[string]any{
			"anchorOrgsN":     len(rt.downOrgs),
			"anchorFacsN":     len(rt.downFacs),
			"productFiltersN": len(rt.downFilters.Product),
			"geoFiltersN":     len(rt.downFilters.Geo),
			"unwindDepth":     rt.unwindDepth,
			"chainsN":         len(chains),
		},
	}
}

type directedSupplyChainUpstreamTool struct{}

func (directedSupplyChainUpstreamTool) Name() string { return "supply_chain.upstream_unwind" }

func (directedSupplyChainUpstreamTool) Run(ctx context.Context, e *ScoutEngine, job *horizonmodels.ScoutingJob, rt *directedSupplyChainExplorationRuntime) directedSupplyChainToolRun {
	if rt == nil || !rt.ensureQueries(ctx, e, job) {
		return directedSupplyChainToolRun{Status: "skipped", Metrics: map[string]any{"reason": "no_queries"}}
	}
	if len(rt.upOrgs) == 0 && len(rt.upFacs) == 0 {
		return directedSupplyChainToolRun{Status: "skipped", Metrics: map[string]any{"reason": "no_upstream_anchors"}}
	}
	chains := e.runSupplyChainUpstreamUnwinding(ctx, job, rt.upOrgs, rt.upFacs, rt.unwindDepth, rt.upFilters.Product, rt.upFilters.Geo)
	return directedSupplyChainToolRun{
		Status: "completed",
		Metrics: map[string]any{
			"anchorOrgsN":     len(rt.upOrgs),
			"anchorFacsN":     len(rt.upFacs),
			"productFiltersN": len(rt.upFilters.Product),
			"geoFiltersN":     len(rt.upFilters.Geo),
			"unwindDepth":     rt.unwindDepth,
			"chainsN":         len(chains),
		},
	}
}

func directedSupplyChainExplorationTools(job *horizonmodels.ScoutingJob) []directedSupplyChainExplorationTool {
	tools := []directedSupplyChainExplorationTool{
		directedSupplyChainHotPathTool{},
	}
	if directedExplorationHotPathOnly(job) {
		return tools
	}
	switch directedExplorationDirection(job) {
	case "upstream":
		tools = append(tools, directedSupplyChainUpstreamTool{})
	default:
		tools = append(tools, directedSupplyChainDownstreamTool{})
	}
	return tools
}

func directedExplorationIncludeEdgeMetrics(job *horizonmodels.ScoutingJob) bool {
	cfg := parseDirectedExplorationConfig(job)
	return cfg != nil && cfg.IncludeEdgeMetrics
}

func appendDirectedExplorationToolRun(job *horizonmodels.ScoutingJob, toolName string, run directedSupplyChainToolRun, startedAt time.Time, finishedAt time.Time) {
	if job == nil {
		return
	}
	if job.State == nil {
		job.State = map[string]any{}
	}
	record := map[string]any{
		"name":       toolName,
		"status":     firstNonEmpty(run.Status, "completed"),
		"startedAt":  startedAt.UTC().Format(time.RFC3339Nano),
		"finishedAt": finishedAt.UTC().Format(time.RFC3339Nano),
	}
	if len(run.Metrics) > 0 {
		record["metrics"] = run.Metrics
	}
	if run.Error != nil {
		record["error"] = run.Error.Error()
	}
	existing := parseStateSliceMap(job.State["directedExplorationToolRuns"])
	if len(existing) >= 24 {
		existing = existing[len(existing)-23:]
	}
	existing = append(existing, record)
	job.State["directedExplorationToolRuns"] = existing
	job.State["directedExplorationLastTool"] = toolName
}
