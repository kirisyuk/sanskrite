package domain

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	telemetry "github.com/igorkirisyuk/sanskrite/internal/horizon/telemetrycollector"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type LensArtifactsReader interface {
	ListByLens(ctx context.Context, lensID string, limit int) ([]*models.LensArtifact, error)
}

type lensArtifactsPaginatedReader interface {
	ListByLensPaginated(ctx context.Context, lensID string, limit int, cursor *time.Time) ([]*models.LensArtifact, *time.Time, error)
}

type FragmentsReader interface {
	Get(ctx context.Context, id string) (*models.HorizonFragment, error)
	GetByCanonicalPath(ctx context.Context, canonicalPath string) (*models.HorizonFragment, error)
	GetMany(ctx context.Context, ids []string) (map[string]*models.HorizonFragment, error)
}

type EdgesReader interface {
	Get(ctx context.Context, id string) (*models.DependencyEdge, error)
	GetMany(ctx context.Context, ids []string) (map[string]*models.DependencyEdge, error)
}

type ScoutingJobsWriter interface {
	Create(ctx context.Context, job *models.ScoutingJob) (string, error)
	Get(ctx context.Context, id string) (*models.ScoutingJob, error)
}

type SnapshotsWriter interface {
	Upsert(ctx context.Context, snapshot *models.DomainAnswerSnapshot) (string, error)
}

type Engine struct {
	gateConfig    GateConfig
	lensArtifacts LensArtifactsReader
	fragments     FragmentsReader
	edges         EdgesReader
	scoutingJobs  ScoutingJobsWriter
	snapshots     SnapshotsWriter
	aiExecutor    *ai.AITaskExecutor
	publisher     EventPublisher
}

const (
	domainCollectArtifactsPageSize = 1000
	domainCollectArtifactsMaxTotal = 20000
	domainEnrichAwaitTimeout       = 20 * time.Second
	domainEnrichAwaitPoll          = 1500 * time.Millisecond
)

func NewEngine(
	lensArtifacts LensArtifactsReader,
	fragments FragmentsReader,
	edges EdgesReader,
	scoutingJobs ScoutingJobsWriter,
	snapshots SnapshotsWriter,
	aiExecutor *ai.AITaskExecutor,
	publisher EventPublisher,
) *Engine {
	return &Engine{
		gateConfig:    DefaultGateConfig(),
		lensArtifacts: lensArtifacts,
		fragments:     fragments,
		edges:         edges,
		scoutingJobs:  scoutingJobs,
		snapshots:     snapshots,
		aiExecutor:    aiExecutor,
		publisher:     publisher,
	}
}

func (e *Engine) WithGateConfig(cfg GateConfig) *Engine {
	e.gateConfig = cfg
	return e
}

// RunModeDiagnostic performs a lightweight analysis: collectGraph + assembleDeterministic + Gate L1 only.
// Skips enrichment loop and LLM gates. Returns a DomainAnswerSnapshot with deterministic signals.
func (e *Engine) RunModeDiagnostic(ctx context.Context, in RunInput) (*models.DomainAnswerSnapshot, error) {
	if e == nil {
		return nil, fmt.Errorf("domain engine is nil")
	}
	if in.Job == nil {
		return nil, fmt.Errorf("run input job is nil")
	}
	contract := normalizeContract(in.Contract, in.Job)
	if strings.TrimSpace(contract.ContractType) == "" {
		return nil, fmt.Errorf("contractType is required")
	}
	domainID := domainIDFromContract(contract, in.Job)
	expectations := generateCrossFacetExpectations(contract)

	graph := e.collectGraph(ctx, in.Job, contract)
	if len(graph.NodesByID) == 0 {
		return nil, fmt.Errorf("diagnostic: no nodes in graph for lens %s", strings.TrimSpace(in.Job.LensID))
	}

	snapshot := e.assembleDeterministic(in.Job, contract, domainID, expectations, graph)
	l1 := e.evaluateGateL1(contract, expectations, snapshot, graph)
	failedIDs := make([]string, 0)
	for _, c := range l1.Checks {
		if c.Status == "fail" {
			failedIDs = append(failedIDs, c.CheckID)
		}
	}
	snapshot.Gate = models.DomainGateSummary{
		OverallStatus:    l1.OverallStatus,
		TotalChecks:      l1.TotalChecks,
		Passed:           l1.Passed,
		Failed:           l1.Failed,
		Partial:          l1.Partial,
		Checks:           l1.Checks,
		FailedCheckIDs:   failedIDs,
		EvidenceCoverage: l1.Score,
	}
	snapshot.EnrichmentCycles = 0
	snapshot.ReadinessStatus = models.DomainReadinessDiagnostic

	return snapshot, nil
}

func (e *Engine) Run(ctx context.Context, in RunInput) (*RunResult, error) {
	if e == nil {
		return nil, fmt.Errorf("domain engine is nil")
	}
	if in.Job == nil {
		return nil, fmt.Errorf("run input job is nil")
	}
	contract := normalizeContract(in.Contract, in.Job)
	if strings.TrimSpace(contract.ContractType) == "" {
		return nil, fmt.Errorf("contractType is required")
	}
	steeringProfile := steeringProfileForContractType(contract.ContractType)
	domainID := domainIDFromContract(contract, in.Job)
	expectations := generateCrossFacetExpectations(contract)
	originalQuery := extractOriginalQuery(in.Job)
	logDomainContract(ctx, "run.start", in.Job, &contract, map[string]any{
		"domainId":          domainID,
		"steeringProfile":   string(steeringProfile),
		"facetCount":        len(contract.Facets),
		"expectationCount":  len(expectations),
		"maxCycles":         e.gateConfig.MaxEnrichmentCycles,
		"maxAdaptiveCycles": e.gateConfig.MaxAdaptiveCycles,
	})

	e.emit(ctx, in.Job, contract, "horizon.domain.contract.resolve.started",
		"Resolving domain contract",
		"Normalizing domain contract and facets",
		map[string]interface{}{
			"domainId":        domainID,
			"contractType":    contract.ContractType,
			"steeringProfile": string(steeringProfile),
			"facets":          contract.Facets,
			"query":           originalQuery,
		},
	)
	e.emit(ctx, in.Job, contract, "horizon.domain.contract.resolve.completed",
		fmt.Sprintf("Domain contract resolved: %s", contract.ContractType),
		fmt.Sprintf("Facets: %d", len(contract.Facets)),
		map[string]interface{}{
			"domainId":               domainID,
			"contractType":           contract.ContractType,
			"steeringProfile":        string(steeringProfile),
			"facetCount":             len(contract.Facets),
			"crossFacetExpectations": expectations,
		},
	)

	anchorDirectives := buildAnchorDirectives(contract)
	e.emit(ctx, in.Job, contract, "horizon.domain.contract.steer.started",
		fmt.Sprintf("Steering scouting for %s", contract.ContractType),
		fmt.Sprintf("Anchor wave directives: %d", len(anchorDirectives)),
		map[string]interface{}{
			"domainId":       domainID,
			"wave":           "anchor",
			"directiveCount": len(anchorDirectives),
		},
	)
	if in.ExecuteBaseCollect != nil {
		if err := in.ExecuteBaseCollect(ctx, in.GroundingEvidence); err != nil {
			logDomainContract(ctx, "steer.base_collect.error", in.Job, &contract, map[string]any{
				"domainId": domainID,
				"wave":     "anchor",
				"error":    err.Error(),
			})
			return nil, err
		}
	}
	if in.ExecuteStrategicCollect != nil {
		if err := in.ExecuteStrategicCollect(ctx, in.GroundingEvidence); err != nil {
			// Strategic layer is best-effort for many contracts; continue with base graph.
			logDomainContract(ctx, "steer.strategic_collect.error", in.Job, &contract, map[string]any{
				"domainId": domainID,
				"wave":     "anchor",
				"error":    err.Error(),
			})
		}
	}
	steerDescription := "Base graph collection completed"
	if in.ExecuteStrategicCollect != nil {
		steerDescription = "Base and strategic graph collection completed"
	}
	e.emit(ctx, in.Job, contract, "horizon.domain.contract.steer.completed",
		fmt.Sprintf("Steering complete: %d directives", len(anchorDirectives)),
		steerDescription,
		map[string]interface{}{
			"domainId":               domainID,
			"wave":                   "anchor",
			"directiveCount":         len(anchorDirectives),
			"strategicCollectInline": in.ExecuteStrategicCollect != nil,
		},
	)

	baseMaxCycles := e.gateConfig.MaxEnrichmentCycles
	if baseMaxCycles < 0 {
		baseMaxCycles = 0
	}
	maxCycles := baseMaxCycles
	adaptiveRemaining := e.gateConfig.MaxAdaptiveCycles
	if adaptiveRemaining < 0 {
		adaptiveRemaining = 0
	}
	cycle := 0
	var finalSnapshot *models.DomainAnswerSnapshot
	var finalGate gateResult
	var lastGraph graphSnapshot
	var initialExplanatory *models.DomainExplanatoryLayer
	initialExplanatoryHash := ""
	initialSnapshotPublished := false
	graphTruncatedObserved := false
	partialWaitTimeoutObserved := false

	telCol := telemetry.CollectorFromContext(ctx)
	for {
		logDomainContract(ctx, "cycle.start", in.Job, &contract, map[string]any{
			"domainId": domainID,
			"cycle":    cycle,
		})
		if telCol != nil {
			telCol.BeginGroup("collect_graph")
		}
		lastGraph = e.collectGraph(ctx, in.Job, contract)
		if lastGraph.ArtifactsTruncated {
			graphTruncatedObserved = true
		}
		if telCol != nil {
			telCol.EndGroup()
		}
		logDomainContract(ctx, "cycle.graph.collected", in.Job, &contract, map[string]any{
			"domainId":  domainID,
			"cycle":     cycle,
			"nodeCount": len(lastGraph.NodesByID),
			"edgeCount": len(lastGraph.Edges),
			"truncated": lastGraph.ArtifactsTruncated,
		})
		if telCol != nil {
			telCol.BeginGroup("assemble_deterministic")
		}
		snapshot := e.assembleDeterministic(in.Job, contract, domainID, expectations, lastGraph)
		if cycle == 0 {
			e.emit(ctx, in.Job, contract, "horizon.domain.contract.explain.started",
				"Building explanatory layer",
				"Generating domain-specific mechanism explanations from graph artifacts",
				map[string]interface{}{
					"domainId": domainID,
					"phase":    "initial",
				},
			)
			e.applyExplanatoryLayer(ctx, contract, snapshot, lastGraph, originalQuery, "initial")
			initialExplanatory = cloneDomainExplanatoryLayer(snapshot.ExplanatoryLayer)
			initialExplanatoryHash = dependencyHashFromCriticalDependencies(snapshot.CriticalDependencies)
			unitCount := 0
			candidateCount := 0
			tokenBudget := 0
			consumedInput := 0
			consumedOutput := 0
			explainStatus := ""
			dependencyHash := ""
			if snapshot.ExplanatoryLayer != nil {
				unitCount = len(snapshot.ExplanatoryLayer.Units)
				candidateCount = snapshot.ExplanatoryLayer.CandidateCount
				tokenBudget = snapshot.ExplanatoryLayer.TokenBudget
				consumedInput = snapshot.ExplanatoryLayer.ConsumedInputTokens
				consumedOutput = snapshot.ExplanatoryLayer.ConsumedOutputTokens
				explainStatus = snapshot.ExplanatoryLayer.Status
				dependencyHash = snapshot.ExplanatoryLayer.SourceDependencyHash
			}
			e.emit(ctx, in.Job, contract, "horizon.domain.contract.explain.completed",
				"Explanatory layer generated",
				fmt.Sprintf("Units: %d", unitCount),
				map[string]interface{}{
					"domainId":       domainID,
					"phase":          "initial",
					"unitCount":      unitCount,
					"candidateCount": candidateCount,
					"tokenBudget":    tokenBudget,
					"consumedInput":  consumedInput,
					"consumedOutput": consumedOutput,
					"explainStatus":  explainStatus,
					"dependencyHash": dependencyHash,
				},
			)
		}

		if telCol != nil {
			telCol.EndGroup()
		}
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.gate.started",
			"Checking completeness",
			fmt.Sprintf("Running Gate L1 cycle %d", cycle),
			map[string]interface{}{
				"domainId": domainID,
				"level":    1,
				"cycle":    cycle,
			},
		)
		if telCol != nil {
			telCol.BeginGroup("gate_l1")
		}
		l1 := e.evaluateGateL1(contract, expectations, snapshot, lastGraph)
		if telCol != nil {
			telCol.EndGroup()
		}
		if l1.OverallStatus == "pass" {
			e.emit(ctx, in.Job, contract, "horizon.domain.contract.gate.passed",
				"Completeness check passed (L1)",
				fmt.Sprintf("Checks passed: %d/%d", l1.Passed, l1.TotalChecks),
				map[string]interface{}{
					"domainId": domainID,
					"level":    1,
					"score":    l1.Score,
					"checks":   l1.Checks,
				},
			)
			e.emit(ctx, in.Job, contract, "horizon.domain.contract.gate.started",
				"Checking completeness",
				"Running semantic Gate L2",
				map[string]interface{}{
					"domainId": domainID,
					"level":    2,
					"cycle":    cycle,
				},
			)
			l2 := e.evaluateGateL2(ctx, contract, snapshot, l1, originalQuery)
			if l2.OverallStatus == "pass" {
				e.emit(ctx, in.Job, contract, "horizon.domain.contract.gate.passed",
					"Completeness check passed (L2)",
					"Semantic completeness reached",
					map[string]interface{}{
						"domainId":           domainID,
						"level":              2,
						"score":              l2.Score,
						"checks":             l2.Checks,
						"recoverableReasons": gateSummaryWithRuntime(l2, graphTruncatedObserved, partialWaitTimeoutObserved).RecoverableReasons,
					},
				)
				snapshot.Gate = gateSummaryWithRuntime(l2, graphTruncatedObserved, partialWaitTimeoutObserved)
				finalSnapshot = snapshot
				finalGate = l2
				break
			}
			l1 = l2
		}

		finalGate = l1
		if cycle == 0 && !initialSnapshotPublished && e.snapshots != nil {
			snapshot.ReadinessStatus = models.DomainReadinessReadyWithGaps
			snapshot.Gate = gateSummaryWithRuntime(l1, graphTruncatedObserved, partialWaitTimeoutObserved)
			snapshot.EnrichmentCycles = cycle
			if id, err := e.snapshots.Upsert(ctx, snapshot); err == nil {
				snapshot.ID = id
				initialSnapshotPublished = true
				logDomainContract(ctx, "snapshot.upsert.initial.ok", in.Job, &contract, map[string]any{
					"domainId":   domainID,
					"snapshotId": strings.TrimSpace(id),
					"cycle":      cycle,
				})
			} else {
				logDomainContract(ctx, "snapshot.upsert.initial.error", in.Job, &contract, map[string]any{
					"domainId":   domainID,
					"snapshotId": strings.TrimSpace(snapshot.ID),
					"cycle":      cycle,
					"error":      err.Error(),
				})
			}
		}
		if cycle >= maxCycles {
			if adaptiveRemaining > 0 && shouldGrantAdaptiveCycle(l1) {
				adaptiveRemaining--
				maxCycles++
				logDomainContract(ctx, "enrich.adaptive_cycle_granted", in.Job, &contract, map[string]any{
					"domainId":           domainID,
					"cycle":              cycle,
					"baseMaxCycles":      baseMaxCycles,
					"effectiveMaxCycles": maxCycles,
					"adaptiveRemaining":  adaptiveRemaining,
					"failedChecks":       l1.failedCheckIDs(),
				})
			} else {
				logDomainContract(ctx, "enrich.limit_reached", in.Job, &contract, map[string]any{
					"domainId":     domainID,
					"cycle":        cycle,
					"maxCycles":    maxCycles,
					"failedChecks": l1.failedCheckIDs(),
					"gateLevel":    l1.Level,
					"gateStatus":   l1.OverallStatus,
				})
				break
			}
		}
		tasks := buildEnrichmentTasks(contract, l1)
		if graphTruncatedObserved {
			tasks = append(tasks, EnrichmentTask{
				Priority:    "P1",
				GapType:     "artifact_coverage_expand",
				Hint:        "Artifact collection was truncated; expand artifact coverage and rerun enrichment",
				MissingInfo: []string{"Expand artifact coverage beyond current truncation cap for this domain"},
				TargetPath:  strings.TrimSpace(in.Job.TargetCanonicalPath),
			})
		}
		if warning := buildDomainBudgetWarning(in.Job, cycle+1, maxCycles); warning != nil {
			e.emit(ctx, in.Job, contract, "horizon.domain.contract.budget.warning",
				warning.Title,
				warning.Description,
				map[string]interface{}{
					"domainId":   domainID,
					"cycle":      cycle + 1,
					"usedUSD":    warning.UsedUSD,
					"capUSD":     warning.CapUSD,
					"usedTokens": warning.UsedTokens,
					"usagePct":   warning.UsagePct,
				},
			)
		}
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.gate.failed",
			fmt.Sprintf("Completeness gaps found (L%d)", l1.Level),
			"Scheduling enrichment tasks",
			map[string]interface{}{
				"domainId":        domainID,
				"level":           l1.Level,
				"failedChecks":    l1.failedCheckIDs(),
				"enrichmentTasks": tasks,
			},
		)
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.enrich.started",
			fmt.Sprintf("Enriching: %d tasks", len(tasks)),
			fmt.Sprintf("Cycle %d", cycle+1),
			map[string]interface{}{
				"domainId":   domainID,
				"cycle":      cycle + 1,
				"taskCount":  len(tasks),
				"focusAreas": l1.failedCheckIDs(),
			},
		)
		beforeNodes := len(lastGraph.NodesByID)
		beforeEdges := len(lastGraph.Edges)
		childJobIDs := e.enqueueEnrichmentTasks(ctx, in.Job, contract, tasks, cycle+1)
		if in.ExecuteBaseCollect != nil {
			if err := in.ExecuteBaseCollect(ctx, in.GroundingEvidence); err != nil {
				logDomainContract(ctx, "enrich.base_collect.error", in.Job, &contract, map[string]any{
					"domainId": domainID,
					"cycle":    cycle + 1,
					"error":    err.Error(),
				})
			}
		}
		if in.ExecuteStrategicCollect != nil {
			if err := in.ExecuteStrategicCollect(ctx, in.GroundingEvidence); err != nil {
				logDomainContract(ctx, "enrich.strategic_collect.error", in.Job, &contract, map[string]any{
					"domainId": domainID,
					"cycle":    cycle + 1,
					"error":    err.Error(),
				})
			}
		}
		afterGraph := e.awaitGraphDelta(ctx, in.Job, contract, beforeNodes, beforeEdges, cycle+1, childJobIDs)
		if afterGraph.ArtifactsTruncated {
			graphTruncatedObserved = true
		}
		if afterGraph.PartialWaitTimeout {
			partialWaitTimeoutObserved = true
		}
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.enrich.completed",
			fmt.Sprintf("Enrichment cycle %d complete", cycle+1),
			"Graph enriched with additional tasks",
			map[string]interface{}{
				"domainId":           domainID,
				"cycle":              cycle + 1,
				"newNodes":           maxInt(0, len(afterGraph.NodesByID)-beforeNodes),
				"newEdges":           maxInt(0, len(afterGraph.Edges)-beforeEdges),
				"partialWaitTimeout": afterGraph.PartialWaitTimeout,
				"graphTruncated":     afterGraph.ArtifactsTruncated,
			},
		)
		logDomainContract(ctx, "enrich.completed", in.Job, &contract, map[string]any{
			"domainId":             domainID,
			"cycle":                cycle + 1,
			"newNodes":             maxInt(0, len(afterGraph.NodesByID)-beforeNodes),
			"newEdges":             maxInt(0, len(afterGraph.Edges)-beforeEdges),
			"partial_wait_timeout": afterGraph.PartialWaitTimeout,
			"graph_truncated":      afterGraph.ArtifactsTruncated,
		})
		cycle++
	}

	if finalSnapshot == nil {
		finalSnapshot = e.assembleDeterministic(in.Job, contract, domainID, expectations, lastGraph)
		finalSnapshot.ReadinessStatus = models.DomainReadinessReadyWithGaps
		finalSnapshot.Gate = gateSummaryWithRuntime(finalGate, graphTruncatedObserved, partialWaitTimeoutObserved)
	}
	if finalSnapshot.ExplanatoryLayer == nil && initialExplanatory != nil {
		finalSnapshot.ExplanatoryLayer = cloneDomainExplanatoryLayer(initialExplanatory)
	}
	if finalSnapshot.ExplanatoryLayer != nil {
		finalHash := dependencyHashFromCriticalDependencies(finalSnapshot.CriticalDependencies)
		if initialExplanatoryHash != "" && finalHash != "" && initialExplanatoryHash != finalHash {
			finalSnapshot.ExplanatoryLayer.Stale = true
			finalSnapshot.ExplanatoryLayer.Status = "stale"
		}
	}
	finalSnapshot.EnrichmentCycles = cycle
	finalSnapshot.GroundingContractVersion = "v1"
	applyGroundingEvidenceToSnapshot(finalSnapshot, in.GroundingEvidence)
	if in.Job != nil && in.Job.State != nil {
		if v, ok := in.Job.State["originMode"].(string); ok && strings.TrimSpace(finalSnapshot.OriginMode) == "" {
			finalSnapshot.OriginMode = strings.TrimSpace(v)
		}
		if raw, ok := in.Job.State["resolutionStats"]; ok {
			if m, ok := raw.(map[string]interface{}); ok && m != nil {
				finalSnapshot.ResolutionStats = &models.GroundingResolutionStats{
					RelationsResolvedByGrounding: toInt(m["relationsResolvedByGrounding"]),
					RelationsCreatedNew:          toInt(m["relationsCreatedNew"]),
					DedupRedirectsByGrounding:    toInt(m["dedupRedirectsByGrounding"]),
				}
			}
		}
	}

	e.emit(ctx, in.Job, contract, "horizon.domain.contract.assemble.started",
		"Assembling domain answer",
		"Building deterministic artifacts, structured narrative, and explanatory layer",
		map[string]interface{}{
			"domainId":     domainID,
			"contractType": contract.ContractType,
		},
	)
	e.applyStructuredNarrative(ctx, contract, finalSnapshot)
	if finalSnapshot.ExplanatoryLayer == nil || len(finalSnapshot.ExplanatoryLayer.Units) == 0 {
		e.applyExplanatoryLayer(ctx, contract, finalSnapshot, lastGraph, originalQuery, "final")
	}
	if finalSnapshot.ExplanatoryLayer != nil {
		finalSnapshot.ExplanatoryLayer.ThesisContext = strings.TrimSpace(finalSnapshot.Thesis.Text)
		if strings.TrimSpace(finalSnapshot.ExplanatoryLayer.SourceDependencyHash) == "" {
			finalSnapshot.ExplanatoryLayer.SourceDependencyHash = dependencyHashFromCriticalDependencies(finalSnapshot.CriticalDependencies)
		}
	}
	e.emit(ctx, in.Job, contract, "horizon.domain.contract.assemble.completed",
		"Domain answer assembled",
		shortThesis(finalSnapshot.Thesis.Text),
		map[string]interface{}{
			"domainId":        domainID,
			"snapshotId":      finalSnapshot.ID,
			"readinessStatus": finalSnapshot.ReadinessStatus,
			"artifactCount":   len(finalSnapshot.CriticalDependencies) + len(finalSnapshot.ImpactPaths) + len(finalSnapshot.DataGaps),
			"explanatoryUnits": func() int {
				if finalSnapshot.ExplanatoryLayer == nil {
					return 0
				}
				return len(finalSnapshot.ExplanatoryLayer.Units)
			}(),
		},
	)

	if strings.TrimSpace(finalSnapshot.ReadinessStatus) == "" {
		finalSnapshot.ReadinessStatus = models.DomainReadinessReady
	}
	publishBlockedRecoverable := hasRecoverableCriticalOpen(finalGate, graphTruncatedObserved, partialWaitTimeoutObserved)
	if finalGate.OverallStatus != "pass" {
		finalSnapshot.ReadinessStatus = models.DomainReadinessReadyWithGaps
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.fallback",
			"Answer ready with gaps",
			"Reached enrichment cycle limit with unresolved critical checks",
			map[string]interface{}{
				"domainId":                  domainID,
				"gapCount":                  len(finalSnapshot.DataGaps),
				"readinessStatus":           finalSnapshot.ReadinessStatus,
				"graphTruncated":            graphTruncatedObserved,
				"partialWaitTimeout":        partialWaitTimeoutObserved,
				"publishBlockedRecoverable": publishBlockedRecoverable,
			},
		)
	}
	finalSnapshot.Gate = gateSummaryWithRuntime(finalGate, graphTruncatedObserved, partialWaitTimeoutObserved)

	if e.snapshots != nil {
		if id, err := e.snapshots.Upsert(ctx, finalSnapshot); err == nil {
			finalSnapshot.ID = id
			logDomainContract(ctx, "snapshot.upsert.ok", in.Job, &contract, map[string]any{
				"domainId":   domainID,
				"snapshotId": strings.TrimSpace(id),
			})
		} else {
			logDomainContract(ctx, "snapshot.upsert.error", in.Job, &contract, map[string]any{
				"domainId":   domainID,
				"snapshotId": strings.TrimSpace(finalSnapshot.ID),
				"error":      err.Error(),
			})
		}
	}

	if publishBlockedRecoverable {
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.publish.blocked",
			"Domain answer publication deferred",
			"Recoverable critical gaps detected (graph truncation / wait timeout); rerun enrichment before publish",
			map[string]interface{}{
				"domainId":             domainID,
				"snapshotId":           finalSnapshot.ID,
				"contractType":         contract.ContractType,
				"readinessStatus":      finalSnapshot.ReadinessStatus,
				"graphTruncated":       graphTruncatedObserved,
				"partialWaitTimeout":   partialWaitTimeoutObserved,
				"recoverableReasons":   finalSnapshot.Gate.RecoverableReasons,
				"criticalOpenCount":    finalSnapshot.Gate.CriticalOpenCount,
				"evidenceCoverage":     finalSnapshot.Gate.EvidenceCoverage,
				"partial_wait_timeout": partialWaitTimeoutObserved,
			},
		)
	} else {
		e.emit(ctx, in.Job, contract, "horizon.domain.contract.publish.completed",
			"Domain answer published",
			fmt.Sprintf("Snapshot %s", strings.TrimSpace(finalSnapshot.ID)),
			map[string]interface{}{
				"domainId":           domainID,
				"snapshotId":         finalSnapshot.ID,
				"contractType":       contract.ContractType,
				"readinessStatus":    finalSnapshot.ReadinessStatus,
				"graphTruncated":     graphTruncatedObserved,
				"partialWaitTimeout": partialWaitTimeoutObserved,
			},
		)
	}

	out := &RunResult{
		Snapshot:      finalSnapshot,
		ViewDirective: adaptSnapshotToViewDirective(finalSnapshot),
	}
	logDomainContract(ctx, "run.completed", in.Job, &contract, map[string]any{
		"domainId":                    domainID,
		"snapshotId":                  strings.TrimSpace(finalSnapshot.ID),
		"readinessStatus":             strings.TrimSpace(finalSnapshot.ReadinessStatus),
		"enrichmentCycles":            cycle,
		"criticalDeps":                len(finalSnapshot.CriticalDependencies),
		"impactPaths":                 len(finalSnapshot.ImpactPaths),
		"dataGaps":                    len(finalSnapshot.DataGaps),
		"gateOverallStatus":           finalGate.OverallStatus,
		"graph_truncated":             graphTruncatedObserved,
		"partial_wait_timeout":        partialWaitTimeoutObserved,
		"publish_blocked_recoverable": publishBlockedRecoverable,
		"explanatoryUnits": func() int {
			if finalSnapshot.ExplanatoryLayer == nil {
				return 0
			}
			return len(finalSnapshot.ExplanatoryLayer.Units)
		}(),
	})
	return out, nil
}

type graphSnapshot struct {
	NodesByID          map[string]*models.HorizonFragment
	NodesByPath        map[string]*models.HorizonFragment
	Edges              []*models.DependencyEdge
	ArtifactsTruncated bool
	PartialWaitTimeout bool
}

func (e *Engine) listLensArtifacts(ctx context.Context, lensID string) ([]*models.LensArtifact, bool, error) {
	if e == nil || e.lensArtifacts == nil {
		return nil, false, nil
	}
	if paged, ok := e.lensArtifacts.(lensArtifactsPaginatedReader); ok {
		all := make([]*models.LensArtifact, 0, domainCollectArtifactsPageSize)
		var cursor *time.Time
		for len(all) < domainCollectArtifactsMaxTotal {
			batch, next, err := paged.ListByLensPaginated(ctx, lensID, domainCollectArtifactsPageSize, cursor)
			if err != nil {
				return nil, false, err
			}
			if len(batch) == 0 {
				return all, false, nil
			}
			all = append(all, batch...)
			if next == nil {
				return all, false, nil
			}
			if len(all) >= domainCollectArtifactsMaxTotal {
				return all[:domainCollectArtifactsMaxTotal], true, nil
			}
			cursor = next
		}
		return all, len(all) >= domainCollectArtifactsMaxTotal, nil
	}

	artifacts, err := e.lensArtifacts.ListByLens(ctx, lensID, domainCollectArtifactsMaxTotal)
	if err != nil {
		return nil, false, err
	}
	return artifacts, len(artifacts) >= domainCollectArtifactsMaxTotal, nil
}

func (e *Engine) awaitGraphDelta(
	ctx context.Context,
	job *models.ScoutingJob,
	contract Contract,
	beforeNodes, beforeEdges, cycle int,
	childJobIDs []string,
) graphSnapshot {
	snapshot := e.collectGraph(ctx, job, contract)
	if len(snapshot.NodesByID) > beforeNodes || len(snapshot.Edges) > beforeEdges {
		return snapshot
	}

	startedAt := time.Now()
	deadline := time.Now().Add(domainEnrichAwaitTimeout)

	useJobPolling := len(childJobIDs) > 0 && e.scoutingJobs != nil
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			snapshot.PartialWaitTimeout = true
			return snapshot
		case <-time.After(domainEnrichAwaitPoll):
		}

		if useJobPolling {
			allTerminal := true
			for _, jid := range childJobIDs {
				childJob, err := e.scoutingJobs.Get(ctx, jid)
				if err != nil {
					continue
				}
				st := string(childJob.Status)
				if st != "completed" && st != "failed" && st != "partial" {
					allTerminal = false
					break
				}
			}
			if allTerminal {
				snapshot = e.collectGraph(ctx, job, contract)
				logDomainContract(ctx, "enrich.await_graph_delta.jobs_terminal", job, &contract, map[string]any{
					"cycle":     cycle,
					"waitMs":    int64(time.Since(startedAt) / time.Millisecond),
					"nodes":     len(snapshot.NodesByID),
					"edges":     len(snapshot.Edges),
					"beforeN":   beforeNodes,
					"beforeE":   beforeEdges,
					"childJobs": len(childJobIDs),
				})
				return snapshot
			}
		}

		snapshot = e.collectGraph(ctx, job, contract)
		if len(snapshot.NodesByID) > beforeNodes || len(snapshot.Edges) > beforeEdges {
			logDomainContract(ctx, "enrich.await_graph_delta.ok", job, &contract, map[string]any{
				"cycle":   cycle,
				"waitMs":  int64(time.Since(startedAt) / time.Millisecond),
				"nodes":   len(snapshot.NodesByID),
				"edges":   len(snapshot.Edges),
				"beforeN": beforeNodes,
				"beforeE": beforeEdges,
			})
			return snapshot
		}
	}

	logDomainContract(ctx, "enrich.await_graph_delta.timeout", job, &contract, map[string]any{
		"cycle":   cycle,
		"timeout": domainEnrichAwaitTimeout.String(),
		"nodes":   len(snapshot.NodesByID),
		"edges":   len(snapshot.Edges),
		"beforeN": beforeNodes,
		"beforeE": beforeEdges,
	})
	snapshot.PartialWaitTimeout = true
	return snapshot
}

func containsFold(slice []string, val string) bool {
	val = strings.TrimSpace(strings.ToLower(val))
	for _, s := range slice {
		if strings.TrimSpace(strings.ToLower(s)) == val {
			return true
		}
	}
	return false
}

func (e *Engine) collectGraph(ctx context.Context, job *models.ScoutingJob, contract Contract) graphSnapshot {
	out := graphSnapshot{
		NodesByID:   map[string]*models.HorizonFragment{},
		NodesByPath: map[string]*models.HorizonFragment{},
		Edges:       []*models.DependencyEdge{},
	}
	if e.lensArtifacts == nil || e.fragments == nil || e.edges == nil || job == nil {
		logDomainContract(ctx, "collect.skipped", job, &contract, map[string]any{
			"reason": "missing_dependencies_or_job",
		})
		return out
	}
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" {
		logDomainContract(ctx, "collect.skipped", job, &contract, map[string]any{
			"reason": "missing_lens_id",
		})
		return out
	}
	artifacts, truncated, err := e.listLensArtifacts(ctx, lensID)
	if err != nil {
		logDomainContract(ctx, "collect.artifacts.error", job, &contract, map[string]any{
			"lensId": lensID,
			"error":  err.Error(),
		})
		return out
	}
	if truncated {
		out.ArtifactsTruncated = true
		logDomainContract(ctx, "collect.artifacts.truncated", job, &contract, map[string]any{
			"lensId":    lensID,
			"maxTotal":  domainCollectArtifactsMaxTotal,
			"artifacts": len(artifacts),
		})
	}
	if len(artifacts) == 0 {
		logDomainContract(ctx, "collect.artifacts.empty", job, &contract, map[string]any{
			"lensId": lensID,
		})
		return out
	}
	fragmentIDs := make([]string, 0, len(artifacts))
	fragSeen := map[string]struct{}{}
	edgeIDs := make([]string, 0, len(artifacts))
	edgeSeen := map[string]struct{}{}
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		if a.IsFragment() {
			id := strings.TrimSpace(a.FragmentID)
			if id != "" {
				if _, ok := fragSeen[id]; !ok {
					fragSeen[id] = struct{}{}
					fragmentIDs = append(fragmentIDs, id)
				}
			}
		}
		if a.IsEdge() {
			id := strings.TrimSpace(a.EdgeID)
			if id != "" {
				if _, ok := edgeSeen[id]; !ok {
					edgeSeen[id] = struct{}{}
					edgeIDs = append(edgeIDs, id)
				}
			}
		}
	}

	allowedEntities := expectedEntitiesForContractType(contract.ContractType)
	allowedEdges := expectedRelationsForContractType(contract.ContractType)
	droppedEntities := 0
	droppedEdges := 0

	if len(fragmentIDs) > 0 {
		byID, err := e.fragments.GetMany(ctx, fragmentIDs)
		if err != nil {
			logDomainContract(ctx, "collect.fragments.error", job, &contract, map[string]any{
				"fragmentIdCount": len(fragmentIDs),
				"error":           err.Error(),
			})
		}
		if byID == nil {
			byID = map[string]*models.HorizonFragment{}
		}
		for _, id := range fragmentIDs {
			f := byID[strings.TrimSpace(id)]
			if f == nil {
				continue
			}
			if len(allowedEntities) > 0 {
				ft := string(f.Class)
				if ft != "DOMAIN" && ft != "SPHERE" && !containsFold(allowedEntities, ft) {
					droppedEntities++
					continue
				}
			}
			out.NodesByID[f.ID] = f
			cp := strings.TrimSpace(f.CanonicalPath)
			if cp != "" {
				out.NodesByPath[cp] = f
			}
		}
	}
	if len(edgeIDs) > 0 {
		edgesByID, err := e.edges.GetMany(ctx, edgeIDs)
		if err != nil {
			logDomainContract(ctx, "collect.edges.error", job, &contract, map[string]any{
				"edgeIdCount": len(edgeIDs),
				"error":       err.Error(),
			})
		}
		if edgesByID == nil {
			edgesByID = map[string]*models.DependencyEdge{}
		}
		for _, edgeID := range edgeIDs {
			edge := edgesByID[strings.TrimSpace(edgeID)]
			if edge == nil {
				continue
			}
			if len(allowedEdges) > 0 {
				if !IsDomainScaffoldRelationType(edge.Relation) && !containsFold(allowedEdges, string(edge.Relation)) {
					droppedEdges++
					continue
				}
			}
			if edge.SourceFragmentID == "" || edge.TargetFragmentID == "" {
				continue
			}
			if out.NodesByID[edge.SourceFragmentID] == nil || out.NodesByID[edge.TargetFragmentID] == nil {
				continue
			}
			out.Edges = append(out.Edges, edge)
		}
		missing := len(edgeIDs) - len(edgesByID)
		if missing > 0 {
			logDomainContract(ctx, "collect.edges.missing", job, &contract, map[string]any{
				"edgeIdCount": len(edgeIDs),
				"missing":     missing,
			})
		}
	}

	if droppedEntities > 0 || droppedEdges > 0 {
		logDomainContract(ctx, "collect.artifacts.filtered", job, &contract, map[string]any{
			"droppedEntities": droppedEntities,
			"droppedEdges":    droppedEdges,
			"allowedEntities": allowedEntities,
			"allowedEdges":    allowedEdges,
		})
	}

	fallbackFragmentErrors := 0
	if len(out.NodesByID) == 0 && len(contract.FirstOrderEntities) > 0 && e.fragments != nil {
		for _, cp := range contract.FirstOrderEntities {
			cp = strings.TrimSpace(cp)
			if cp == "" {
				continue
			}
			if f, err := e.fragments.GetByCanonicalPath(ctx, cp); err == nil && f != nil {
				out.NodesByID[f.ID] = f
				out.NodesByPath[cp] = f
			} else if err != nil {
				fallbackFragmentErrors++
			}
		}
		if fallbackFragmentErrors > 0 {
			logDomainContract(ctx, "collect.fragments_fallback.error", job, &contract, map[string]any{
				"errorCount": fallbackFragmentErrors,
			})
		}
	}
	return out
}

func normalizeContract(c Contract, job *models.ScoutingJob) Contract {
	out := c
	out.CanonicalPath = strings.TrimSpace(out.CanonicalPath)
	out.ContractType = strings.TrimSpace(strings.ToLower(out.ContractType))
	out.Title = strings.TrimSpace(out.Title)
	out.Description = strings.TrimSpace(out.Description)
	if out.CanonicalPath != "" && len(out.Facets) == 0 {
		out.Facets = parseFacetsFromDomainPath(out.CanonicalPath)
	}
	if len(out.Facets) == 0 {
		out.Facets = map[string][]string{"default": {strings.TrimSpace(job.TargetCanonicalPath)}}
	}
	for k, vals := range out.Facets {
		nk := strings.TrimSpace(strings.ToLower(k))
		if nk == "" {
			delete(out.Facets, k)
			continue
		}
		seen := map[string]struct{}{}
		clean := make([]string, 0, len(vals))
		for _, v := range vals {
			v = strings.TrimSpace(strings.ToLower(v))
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			clean = append(clean, v)
		}
		delete(out.Facets, k)
		out.Facets[nk] = clean
	}
	if out.ContractType == "" {
		parts := strings.Split(strings.Trim(out.CanonicalPath, "/"), "/")
		if len(parts) >= 3 && parts[0] == "domain" && parts[1] == "v1" {
			out.ContractType = strings.TrimSpace(parts[2])
		}
	}
	if out.Title == "" {
		out.Title = strings.TrimSpace(job.TargetCanonicalPath)
	}
	if out.Polarity == nil {
		out.Polarity = []string{"risk", "opportunity"}
	}
	return out
}

func parseFacetsFromDomainPath(canonicalPath string) map[string][]string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")
	// /domain/v1/{contractType}/(facet/value)*
	if len(parts) < 4 {
		return map[string][]string{}
	}
	rest := parts[3:]
	if len(rest)%2 != 0 {
		return map[string][]string{}
	}
	out := map[string][]string{}
	for i := 0; i < len(rest); i += 2 {
		k := strings.TrimSpace(strings.ToLower(rest[i]))
		v := strings.TrimSpace(strings.ToLower(rest[i+1]))
		if k == "" || v == "" {
			continue
		}
		out[k] = append(out[k], v)
	}
	return out
}

func domainIDFromContract(c Contract, job *models.ScoutingJob) string {
	if strings.TrimSpace(c.CanonicalPath) != "" {
		return strings.TrimSpace(c.CanonicalPath)
	}
	if job != nil && strings.TrimSpace(job.TargetCanonicalPath) != "" {
		return strings.TrimSpace(job.TargetCanonicalPath)
	}
	return strings.TrimSpace(c.ContractType)
}

func generateCrossFacetExpectations(contract Contract) []CrossFacetExpectation {
	keys := make([]string, 0, len(contract.Facets))
	for k := range contract.Facets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rels := expectedRelationsForContractType(contract.ContractType)
	out := make([]CrossFacetExpectation, 0, len(keys))
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			out = append(out, CrossFacetExpectation{
				FacetA:              keys[i],
				FacetB:              keys[j],
				ExpectedRelationSet: append([]string(nil), rels...),
			})
		}
	}
	return out
}

// expectedRelationsForContractType returns relation tokens fed into LLM
// steering / domain-answer prompts (Category B — prompt vocabulary), not
// runtime ah.RelationName slices. Values stay UPPER_SNAKE until prompts are
// re-authored to emit ah wire.
func expectedRelationsForContractType(contractType string) []string {
	switch steeringProfileForContractType(contractType) {
	case SteeringProfileSupplyChain:
		return taxonomy.AllowedEdgeTypesForDomainMode(taxonomy.DomainModeSupplyChain)
	case SteeringProfileRegulatoryExposure:
		return []string{"REGULATES", "CONTROLS_ACCESS", "USES_MECHANISM", "APPLIES_TO"}
	case SteeringProfilePolicyImpact:
		return taxonomy.AllowedEdgeTypesForDomainMode(taxonomy.DomainModePolicyImpact)
	case SteeringProfileCompetitiveLandscape:
		return []string{"PARTICIPATES_IN", "COMPETES_WITH", "PRODUCES"}
	case SteeringProfileSectorIndustry:
		// Universal sector: same market semantics (market research is sub-contract for /market/ scope)
		return []string{"PARTICIPATES_IN", "HAS_MARKET_SEGMENT", "REGULATES", "COMPETES_WITH", "SUPPLIES_TO", "PRODUCES"}
	case SteeringProfileGeographicConcentration:
		return []string{"LOCATED_IN", "OPERATES_IN", "EXTRACTS_FROM"}
	case SteeringProfileInvestmentFinance:
		return taxonomy.GovernanceEdgeTypes
	default:
		return []string{"DEPENDS_ON", "AFFECTS", "RELATED_DOMAIN"}
	}
}

func expectedEntitiesForContractType(contractType string) []string {
	switch steeringProfileForContractType(contractType) {
	case SteeringProfileSupplyChain:
		return taxonomy.AllowedEntityTypesForDomainMode(taxonomy.DomainModeSupplyChain)
	case SteeringProfileRegulatoryExposure, SteeringProfilePolicyImpact:
		return taxonomy.AllowedEntityTypesForDomainMode(taxonomy.DomainModePolicyImpact)
	case SteeringProfileCompetitiveLandscape:
		return taxonomy.AllowedEntityTypesForDomainMode(taxonomy.DomainModeMarket)
	case SteeringProfileInvestmentFinance:
		return taxonomy.AllowedEntityTypesForDomainMode(taxonomy.DomainModeGovernance)
	case SteeringProfileEcosystemStakeholder:
		return taxonomy.AllowedEntityTypesForDomainMode(taxonomy.DomainModeEcosystem)
	default:
		return nil
	}
}

func buildAnchorDirectives(contract Contract) []EnrichmentTask {
	out := make([]EnrichmentTask, 0, len(contract.Facets))
	for facet, vals := range contract.Facets {
		hint := fmt.Sprintf("Expand facet '%s' with additional entities", facet)
		if len(vals) > 0 {
			hint += fmt.Sprintf(" (%s)", strings.Join(vals, ", "))
		}
		out = append(out, EnrichmentTask{
			Priority: "P1",
			GapType:  "anchor_expansion",
			FacetKey: facet,
			Hint:     hint,
		})
	}
	return out
}

func buildFacetNodes(contract Contract, g graphSnapshot) map[string][]*models.HorizonFragment {
	out := map[string][]*models.HorizonFragment{}
	for k := range contract.Facets {
		out[k] = []*models.HorizonFragment{}
	}
	if len(out) == 0 {
		out["default"] = []*models.HorizonFragment{}
	}
	for _, f := range g.NodesByID {
		if f == nil {
			continue
		}
		cp := strings.ToLower(strings.TrimSpace(f.CanonicalPath))
		if cp == "" {
			continue
		}
		matched := false
		for facet, vals := range contract.Facets {
			if len(vals) == 0 {
				continue
			}
			for _, raw := range vals {
				v := strings.TrimSpace(strings.ToLower(raw))
				if v == "" {
					continue
				}
				if strings.Contains(cp, "/"+v+"/") || strings.HasSuffix(cp, "/"+v) {
					out[facet] = append(out[facet], f)
					matched = true
					break
				}
				if facet == "geo" && len(v) == 2 && strings.Contains(cp, "/"+v+"/") {
					out[facet] = append(out[facet], f)
					matched = true
					break
				}
			}
		}
		if !matched {
			out["default"] = append(out["default"], f)
		}
	}
	return out
}

func (e *Engine) assembleDeterministic(job *models.ScoutingJob, contract Contract, domainID string, expectations []CrossFacetExpectation, g graphSnapshot) *models.DomainAnswerSnapshot {
	snapshot := &models.DomainAnswerSnapshot{
		TenantID:            strings.TrimSpace(job.TenantID),
		ProjectID:           strings.TrimSpace(job.ProjectID),
		LensID:              strings.TrimSpace(job.LensID),
		DomainID:            domainID,
		DomainCanonicalPath: strings.TrimSpace(contract.CanonicalPath),
		DomainContract: models.DomainContractSnapshot{
			CanonicalPath: strings.TrimSpace(contract.CanonicalPath),
			ContractType:  strings.TrimSpace(contract.ContractType),
			Title:         strings.TrimSpace(contract.Title),
			Description:   strings.TrimSpace(contract.Description),
			Facets:        contract.Facets,
			Polarity:      contract.Polarity,
		},
		AssembledAt:           time.Now().UTC(),
		ReadinessStatus:       models.DomainReadinessReady,
		FacetSummary:          map[string]models.FacetSummary{},
		CriticalDependencies:  []models.CriticalDependency{},
		CriticalNodes:         []models.CriticalNode{},
		ConcentrationEvidence: []models.ConcentrationEvidence{},
		ControlExposure:       []models.ControlExposure{},
		ImpactPaths:           []models.ImpactPath{},
		DataGaps:              []models.DataGap{},
	}
	for _, ex := range expectations {
		snapshot.DomainContract.CrossFacetExpectations = append(snapshot.DomainContract.CrossFacetExpectations, ex.FacetA+"<->"+ex.FacetB)
	}

	facetNodes := buildFacetNodes(contract, g)
	for facet, nodes := range facetNodes {
		score := qualityScore(nodes)
		summary := snapshot.FacetSummary[facet]
		summary.NodeCount = len(nodes)
		summary.QualityScore = score
		snapshot.FacetSummary[facet] = summary
	}

	edges := make([]*models.DependencyEdge, 0, len(g.Edges))
	for _, edge := range g.Edges {
		if edge == nil {
			continue
		}
		edges = append(edges, edge)
	}

	sort.SliceStable(edges, func(i, j int) bool {
		return topDependencyScore(edges[i], g) > topDependencyScore(edges[j], g)
	})
	topK := e.gateConfig.TopKDependencies
	if topK <= 0 {
		topK = 10
	}
	for idx, edge := range edges {
		if idx >= topK {
			break
		}
		src := g.NodesByID[edge.SourceFragmentID]
		dst := g.NodesByID[edge.TargetFragmentID]
		dep := models.CriticalDependency{
			EdgeID:             strings.TrimSpace(edge.ID),
			SourceFragmentID:   strings.TrimSpace(edge.SourceFragmentID),
			TargetFragmentID:   strings.TrimSpace(edge.TargetFragmentID),
			RelationType:       string(edge.Relation),
			TopDependencyScore: topDependencyScore(edge, g),
			Confidence:         dependencyConfidence(edge),
			SourceRefs:         dependencySourceRefs(edge),
		}
		if src != nil {
			dep.SourceCanonicalPath = strings.TrimSpace(src.CanonicalPath)
		}
		if dst != nil {
			dep.TargetCanonicalPath = strings.TrimSpace(dst.CanonicalPath)
		}
		dep.FacetKey = inferFacetForDependency(dep, facetNodes)
		snapshot.CriticalDependencies = append(snapshot.CriticalDependencies, dep)
	}

	criticalNodes := make([]models.CriticalNode, 0, len(g.NodesByID))
	for _, f := range g.NodesByID {
		if f == nil {
			continue
		}
		crit := fragmentCriticality(f, edges)
		criticalNodes = append(criticalNodes, models.CriticalNode{
			FragmentID:        strings.TrimSpace(f.ID),
			CanonicalPath:     strings.TrimSpace(f.CanonicalPath),
			Criticality:       crit,
			CriticalityReason: "degree_centrality",
			Confidence:        clamp01(0.5 + crit*0.5),
		})
	}
	sort.SliceStable(criticalNodes, func(i, j int) bool { return criticalNodes[i].Criticality > criticalNodes[j].Criticality })
	if len(criticalNodes) > topK {
		criticalNodes = criticalNodes[:topK]
	}
	snapshot.CriticalNodes = criticalNodes

	snapshot.ConcentrationEvidence = buildConcentrationEvidence(g)
	snapshot.ControlExposure = buildControlExposure(g)
	snapshot.ImpactPaths = buildImpactPaths(g, snapshot.CriticalDependencies)
	snapshot.DataGaps = capDataGaps(detectDataGaps(contract, facetNodes, snapshot), e.gateConfig.MaxDataGaps)
	snapshot.HighlightedNodeIDs, snapshot.HighlightedEdgeIDs = highlightedIDs(snapshot)

	return snapshot
}

func qualityScore(nodes []*models.HorizonFragment) float64 {
	if len(nodes) == 0 {
		return 0
	}
	sum := 0.0
	for _, f := range nodes {
		if f == nil {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(string(f.Metadata.EpistemicStatus))) {
		case string(models.EpistemicStatusVerified):
			sum += 1.0
		case string(models.EpistemicStatusLowConfidence):
			sum += 0.5
		case string(models.EpistemicStatusRejected):
			sum += 0.0
		default:
			sum += 0.5
		}
	}
	return sum / float64(len(nodes))
}

func topDependencyScore(edge *models.DependencyEdge, g graphSnapshot) float64 {
	if edge == nil {
		return 0
	}
	impact := clamp01(edge.Properties.Criticality)
	if impact == 0 {
		impact = 0.5
	}
	exposure := clamp01(maxFloat(edge.GetThroughputShare(), edge.GetCapacityConstraint()))
	if exposure == 0 {
		exposure = 0.5
	}
	concentration := deriveConcentration(edge)
	substitutability := clamp01(edgeSubstitutability(edge))
	if substitutability == 0 {
		substitutability = 0.5
	}
	sanctions := sanctionExposure(edge, g)
	confidence := dependencyConfidence(edge)
	score := impact * exposure * concentration * (1.0 - substitutability) * sanctions * confidence
	if score == 0 {
		// Fallback when sparse qualifiers are present.
		score = clamp01((impact + exposure + concentration + sanctions + confidence) / 5.0)
	}
	return clamp01(score)
}

func dependencyConfidence(edge *models.DependencyEdge) float64 {
	if edge == nil {
		return 0.5
	}
	base := 0.5
	if edge.Provenance.ObservedAt != nil {
		base += 0.1
	}
	if len(dependencySourceRefs(edge)) >= 2 {
		base += 0.15
	}
	if edge.GetPolicyStatus() == models.PolicyStatusPenalized {
		base -= 0.2
	}
	if edge.GetPolicyStatus() == models.PolicyStatusQuarantined {
		base -= 0.4
	}
	return clamp01(base)
}

func dependencySourceRefs(edge *models.DependencyEdge) []string {
	if edge == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, id := range edge.Provenance.SourceIDs {
		add(id)
	}
	for _, id := range edge.Provenance.DataSourceIDs {
		add(id)
	}
	for _, ref := range edge.Provenance.SourceRefs {
		add(ref.SourceID)
	}
	return out
}

func deriveConcentration(edge *models.DependencyEdge) float64 {
	if edge == nil {
		return 0.5
	}
	for _, key := range []string{"cr4", "CR4", "hhi", "HHI"} {
		if edge.Qualifiers != nil {
			if v, ok := parseFloat(edge.Qualifiers[key]); ok {
				if strings.EqualFold(key, "hhi") {
					return clamp01(v / 5000.0)
				}
				return clamp01(v)
			}
		}
		if edge.Metrics != nil {
			if mv, ok := edge.Metrics[key]; ok {
				if strings.EqualFold(key, "hhi") {
					return clamp01(mv.Value / 5000.0)
				}
				return clamp01(mv.Value)
			}
		}
	}
	return 0.5
}

func sanctionExposure(edge *models.DependencyEdge, g graphSnapshot) float64 {
	if edge == nil {
		return 0.5
	}
	if edge.Relation == DependencyTypeRegulates || edge.Relation == DependencyTypeControlsAccess || edge.Relation == DependencyTypeUsesMechanism {
		return 1.0
	}
	src := g.NodesByID[edge.SourceFragmentID]
	tgt := g.NodesByID[edge.TargetFragmentID]
	srcID := ""
	tgtID := ""
	if src != nil {
		srcID = strings.TrimSpace(src.ID)
	}
	if tgt != nil {
		tgtID = strings.TrimSpace(tgt.ID)
	}
	for _, ed := range g.Edges {
		if ed == nil {
			continue
		}
		if ed.Relation != DependencyTypeRegulates && ed.Relation != DependencyTypeControlsAccess && ed.Relation != DependencyTypeUsesMechanism {
			continue
		}
		if ed.SourceFragmentID == srcID || ed.TargetFragmentID == srcID || ed.SourceFragmentID == tgtID || ed.TargetFragmentID == tgtID {
			return 0.9
		}
	}
	return 0.5
}

func inferFacetForDependency(dep models.CriticalDependency, facetNodes map[string][]*models.HorizonFragment) string {
	source := strings.TrimSpace(dep.SourceCanonicalPath)
	target := strings.TrimSpace(dep.TargetCanonicalPath)
	for facet, nodes := range facetNodes {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			cp := strings.TrimSpace(n.CanonicalPath)
			if cp != "" && (cp == source || cp == target) {
				return facet
			}
		}
	}
	return "default"
}

func fragmentCriticality(f *models.HorizonFragment, edges []*models.DependencyEdge) float64 {
	if f == nil {
		return 0
	}
	degree := 0
	for _, e := range edges {
		if e == nil {
			continue
		}
		if e.SourceFragmentID == f.ID || e.TargetFragmentID == f.ID {
			degree++
		}
	}
	centrality := clamp01(float64(degree) / 12.0)
	return centrality
}

func buildConcentrationEvidence(g graphSnapshot) []models.ConcentrationEvidence {
	out := make([]models.ConcentrationEvidence, 0, 8)
	for _, f := range g.NodesByID {
		if f == nil || f.Class != FragmentTypeMarket {
			continue
		}
		evidence := models.ConcentrationEvidence{
			MarketCanonicalPath: strings.TrimSpace(f.CanonicalPath),
		}
		setMetric := func(metricName string, dst **float64) {
			if mv, ok := f.State[metricName]; ok {
				v := mv.Value
				*dst = &v
			}
		}
		setMetric("TAM", &evidence.TAM)
		setMetric("CAGR", &evidence.CAGR)
		setMetric("CR4", &evidence.CR4)
		setMetric("HHI", &evidence.HHI)
		if evidence.TAM == nil && evidence.CAGR == nil && evidence.CR4 == nil && evidence.HHI == nil {
			evidence.DataGap = "missing_market_metrics"
		}
		out = append(out, evidence)
	}
	return out
}

func buildControlExposure(g graphSnapshot) []models.ControlExposure {
	out := make([]models.ControlExposure, 0, 16)
	for _, edge := range g.Edges {
		if edge == nil {
			continue
		}
		if edge.Relation != DependencyTypeRegulates && edge.Relation != DependencyTypeControlsAccess && edge.Relation != DependencyTypeUsesMechanism {
			continue
		}
		path := []string{}
		if src := g.NodesByID[edge.SourceFragmentID]; src != nil {
			path = append(path, strings.TrimSpace(src.CanonicalPath))
		}
		if tgt := g.NodesByID[edge.TargetFragmentID]; tgt != nil {
			path = append(path, strings.TrimSpace(tgt.CanonicalPath))
		}
		out = append(out, models.ControlExposure{
			MechanismType: strings.ToLower(string(edge.Relation)),
			Severity:      severityFromEdge(edge),
			Confidence:    dependencyConfidence(edge),
			EdgeIDs:       []string{strings.TrimSpace(edge.ID)},
			Path:          path,
		})
	}
	return out
}

func severityFromEdge(edge *models.DependencyEdge) string {
	if edge == nil {
		return "medium"
	}
	if edge.GetPolicyStatus() == models.PolicyStatusQuarantined {
		return "high"
	}
	if edge.GetPolicyStatus() == models.PolicyStatusPenalized {
		return "medium"
	}
	if edge.Properties.Criticality >= 0.75 {
		return "high"
	}
	if edge.Properties.Criticality <= 0.3 {
		return "low"
	}
	return "medium"
}

func buildImpactPaths(g graphSnapshot, deps []models.CriticalDependency) []models.ImpactPath {
	out := make([]models.ImpactPath, 0, len(deps))
	for _, dep := range deps {
		pathNodes, pathEdges := shortestPath(g, dep.SourceFragmentID, dep.TargetFragmentID, 6)
		if len(pathNodes) == 0 {
			continue
		}
		nodePaths := make([]string, 0, len(pathNodes))
		for _, id := range pathNodes {
			if f := g.NodesByID[id]; f != nil {
				nodePaths = append(nodePaths, strings.TrimSpace(f.CanonicalPath))
			}
		}
		out = append(out, models.ImpactPath{
			DependencyEdgeID: strings.TrimSpace(dep.EdgeID),
			NodePaths:        nodePaths,
			EdgeIDs:          append([]string(nil), pathEdges...),
			FacetKey:         dep.FacetKey,
			Criticality:      dep.TopDependencyScore,
		})
	}
	return out
}

func detectDataGaps(contract Contract, facetNodes map[string][]*models.HorizonFragment, snapshot *models.DomainAnswerSnapshot) []models.DataGap {
	gaps := make([]models.DataGap, 0, 16)
	for facet, nodes := range facetNodes {
		if len(nodes) == 0 {
			gaps = append(gaps, models.DataGap{
				GapType:       "empty_facet",
				AffectedFacet: facet,
				Hint:          fmt.Sprintf("Expand facet '%s' with more entities", facet),
				Priority:      "P0",
			})
		}
	}
	if len(snapshot.ConcentrationEvidence) == 0 {
		gaps = append(gaps, models.DataGap{
			GapType:  "missing_metric",
			Hint:     "No market concentration metrics (TAM/CAGR/CR4/HHI) found in scope",
			Priority: "P1",
		})
	}
	if len(snapshot.ControlExposure) == 0 && isRegulatorySensitive(contract.ContractType) {
		gaps = append(gaps, models.DataGap{
			GapType:  "no_regulatory",
			Hint:     "No regulatory/control exposure edges found",
			Priority: "P1",
		})
	}
	if len(snapshot.ImpactPaths) == 0 && len(snapshot.CriticalDependencies) > 0 {
		gaps = append(gaps, models.DataGap{
			GapType:  "no_paths",
			Hint:     "Critical dependencies exist without explainable impact paths",
			Priority: "P0",
		})
	}
	return gaps
}

func isRegulatorySensitive(contractType string) bool {
	switch steeringProfileForContractType(contractType) {
	case SteeringProfileSupplyChain, SteeringProfileRegulatoryExposure, SteeringProfilePolicyImpact:
		return true
	default:
		return false
	}
}

func highlightedIDs(snapshot *models.DomainAnswerSnapshot) ([]string, []string) {
	nodeSet := map[string]struct{}{}
	edgeSet := map[string]struct{}{}
	for _, dep := range snapshot.CriticalDependencies {
		if dep.SourceFragmentID != "" {
			nodeSet[dep.SourceFragmentID] = struct{}{}
		}
		if dep.TargetFragmentID != "" {
			nodeSet[dep.TargetFragmentID] = struct{}{}
		}
		if dep.EdgeID != "" {
			edgeSet[dep.EdgeID] = struct{}{}
		}
	}
	for _, p := range snapshot.ImpactPaths {
		for _, edgeID := range p.EdgeIDs {
			if edgeID != "" {
				edgeSet[edgeID] = struct{}{}
			}
		}
	}
	nodes := make([]string, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)
	edges := make([]string, 0, len(edgeSet))
	for id := range edgeSet {
		edges = append(edges, id)
	}
	sort.Strings(edges)
	return nodes, edges
}

type gateResult struct {
	Level         int
	OverallStatus string
	TotalChecks   int
	Passed        int
	Failed        int
	Partial       int
	Score         float64
	Checks        []models.DomainGateCheck
}

func (r gateResult) failedCheckIDs() []string {
	out := make([]string, 0, len(r.Checks))
	for _, c := range r.Checks {
		if c.Status == "fail" {
			out = append(out, c.CheckID)
		}
	}
	return out
}

func (r gateResult) failedCheckIDsLimited(limit int) []string {
	all := r.failedCheckIDs()
	if limit <= 0 || len(all) <= limit {
		return all
	}
	out := append([]string(nil), all[:limit]...)
	out = append(out, fmt.Sprintf("..._%d_more", len(all)-limit))
	return out
}

func (r gateResult) toSummary() models.DomainGateSummary {
	return models.DomainGateSummary{
		OverallStatus:  r.OverallStatus,
		TotalChecks:    r.TotalChecks,
		Passed:         r.Passed,
		Failed:         r.Failed,
		Partial:        r.Partial,
		Checks:         r.Checks,
		FailedCheckIDs: r.failedCheckIDsLimited(40),
	}
}

func (r gateResult) criticalOpenCount() int {
	total := 0
	for _, c := range r.Checks {
		if strings.TrimSpace(strings.ToLower(c.Status)) != "fail" {
			continue
		}
		priority := strings.TrimSpace(strings.ToUpper(c.Priority))
		if priority == "P0" || priority == "P1" {
			total++
		}
	}
	return total
}

func (r gateResult) evidenceCoverage() float64 {
	if r.TotalChecks <= 0 {
		return 1.0
	}
	score := float64(r.Passed) + 0.5*float64(r.Partial)
	coverage := score / float64(r.TotalChecks)
	if coverage < 0 {
		return 0
	}
	if coverage > 1 {
		return 1
	}
	return coverage
}

func gateSummaryWithRuntime(r gateResult, graphTruncated, partialWaitTimeout bool) models.DomainGateSummary {
	summary := r.toSummary()
	summary.GraphTruncated = graphTruncated
	summary.PartialWaitTimeout = partialWaitTimeout
	summary.CriticalOpenCount = r.criticalOpenCount()
	summary.EvidenceCoverage = r.evidenceCoverage()
	reasons := make([]string, 0, 2)
	if graphTruncated {
		reasons = append(reasons, "graph_truncated")
	}
	if partialWaitTimeout {
		reasons = append(reasons, "partial_wait_timeout")
	}
	summary.RecoverableReasons = reasons
	return summary
}

func hasRecoverableCriticalOpen(gate gateResult, graphTruncated, partialWaitTimeout bool) bool {
	if strings.TrimSpace(strings.ToLower(gate.OverallStatus)) == "pass" {
		return false
	}
	if !graphTruncated && !partialWaitTimeout {
		return false
	}
	return gate.criticalOpenCount() > 0
}

func shouldGrantAdaptiveCycle(gate gateResult) bool {
	// Adaptive cycle is useful only when unresolved checks include critical P0/P1 fails
	// that can improve via async enrichment catch-up.
	if strings.TrimSpace(strings.ToLower(gate.OverallStatus)) == "pass" {
		return false
	}
	hasCritical := false
	allTemporalOrDiversity := true
	for _, c := range gate.Checks {
		if c.Status != "fail" {
			continue
		}
		priority := strings.TrimSpace(strings.ToUpper(c.Priority))
		if priority == "P0" || priority == "P1" {
			hasCritical = true
		}
		if c.CheckID != "U7_temporal_validity" && c.CheckID != "U8_source_diversity" {
			allTemporalOrDiversity = false
		}
	}
	if !hasCritical {
		return false
	}
	return !allTemporalOrDiversity
}

func capDataGaps(gaps []models.DataGap, max int) []models.DataGap {
	if max <= 0 || len(gaps) <= max {
		return gaps
	}
	out := append([]models.DataGap(nil), gaps[:max-1]...)
	out = append(out, models.DataGap{
		GapType:  "truncated",
		Hint:     fmt.Sprintf("additional gaps omitted: %d", len(gaps)-(max-1)),
		Priority: "P2",
	})
	return out
}

func capGateChecks(checks []models.DomainGateCheck, maxChecks int) []models.DomainGateCheck {
	if maxChecks <= 0 || len(checks) <= maxChecks {
		return checks
	}
	if maxChecks < 2 {
		maxChecks = 2
	}
	head := append([]models.DomainGateCheck(nil), checks[:maxChecks-1]...)
	tail := checks[maxChecks-1:]
	failCount := 0
	partialCount := 0
	for _, c := range tail {
		switch strings.TrimSpace(strings.ToLower(c.Status)) {
		case "fail":
			failCount++
		case "partial":
			partialCount++
		}
	}
	status := "pass"
	priority := "P3"
	if failCount > 0 {
		status = "fail"
		priority = "P0"
	} else if partialCount > 0 {
		status = "partial"
		priority = "P1"
	}
	head = append(head, models.DomainGateCheck{
		CheckID:        "U0_gate_overflow_summary",
		Status:         status,
		Priority:       priority,
		GapDescription: fmt.Sprintf("omitted_checks=%d omitted_fail=%d omitted_partial=%d", len(tail), failCount, partialCount),
		EnrichmentHint: "Detailed gate diagnostics truncated to keep domain summary actionable",
	})
	return head
}

func (e *Engine) evaluateGateL1(contract Contract, expectations []CrossFacetExpectation, snapshot *models.DomainAnswerSnapshot, g graphSnapshot) gateResult {
	checks := make([]models.DomainGateCheck, 0, 64)
	facetNodes := buildFacetNodes(contract, g)
	// U1 Facet population
	for facet, nodes := range facetNodes {
		n := len(nodes)
		status := "pass"
		priority := "P2"
		if n == 0 {
			status = "fail"
			priority = "P0"
		} else if n < e.gateConfig.MinNodesPerFacet {
			status = "fail"
			priority = "P1"
		}
		checks = append(checks, models.DomainGateCheck{
			CheckID:        "U1_facet_population_" + facet,
			Status:         status,
			Priority:       priority,
			GapDescription: fmt.Sprintf("facet=%s nodes=%d", facet, n),
			Affected:       []string{facet},
			EnrichmentHint: fmt.Sprintf("Expand facet '%s': find more entities", facet),
		})
	}

	// U2 quality score by facet
	for facet, nodes := range facetNodes {
		score := qualityScore(nodes)
		status := "pass"
		if score < e.gateConfig.MinQualityRatio {
			status = "fail"
		}
		checks = append(checks, models.DomainGateCheck{
			CheckID:        "U2_facet_node_quality_" + facet,
			Status:         status,
			Priority:       "P1",
			GapDescription: fmt.Sprintf("facet=%s quality_score=%.3f threshold=%.3f", facet, score, e.gateConfig.MinQualityRatio),
			Affected:       []string{facet},
			EnrichmentHint: fmt.Sprintf("Re-scout low-quality nodes for facet '%s'", facet),
		})
	}

	// U3 inter-facet connectivity.
	maxCrossFacetChecks := e.gateConfig.MaxCrossFacetChecks
	if maxCrossFacetChecks <= 0 {
		maxCrossFacetChecks = 24
	}
	failedPairs := make([]string, 0, len(expectations))
	passedPairs := 0
	for idx, ex := range expectations {
		aNodes := facetNodes[ex.FacetA]
		bNodes := facetNodes[ex.FacetB]
		ok := hasConnectivityBetweenFacets(g, aNodes, bNodes, e.gateConfig.MaxInterFacetHops)
		if ok {
			passedPairs++
		} else {
			failedPairs = append(failedPairs, ex.FacetA+"<->"+ex.FacetB)
		}
		if idx < maxCrossFacetChecks {
			checks = append(checks, models.DomainGateCheck{
				CheckID:        "U3_inter_facet_connectivity_" + ex.FacetA + "_" + ex.FacetB,
				Status:         ternary(ok, "pass", "fail"),
				Priority:       "P0",
				GapDescription: fmt.Sprintf("connectivity=%v max_hops=%d", ok, e.gateConfig.MaxInterFacetHops),
				Affected:       []string{ex.FacetA, ex.FacetB},
				EnrichmentHint: fmt.Sprintf("Cross-facet disconnect: %s ↔ %s", ex.FacetA, ex.FacetB),
			})
		}
	}
	if len(expectations) > maxCrossFacetChecks {
		affected := make([]string, 0, 12)
		for _, p := range failedPairs {
			affected = append(affected, p)
			if len(affected) >= 12 {
				break
			}
		}
		checks = append(checks, models.DomainGateCheck{
			CheckID:  "U3_inter_facet_connectivity_aggregate",
			Status:   ternary(len(failedPairs) == 0, "pass", "fail"),
			Priority: "P0",
			GapDescription: fmt.Sprintf(
				"pair_checks_total=%d detailed=%d failed_pairs=%d passed_pairs=%d",
				len(expectations), maxCrossFacetChecks, len(failedPairs), passedPairs,
			),
			Affected:       affected,
			EnrichmentHint: "Resolve inter-facet disconnects across omitted facet pairs",
		})
	}

	// U4 critical dependency paths.
	for i, dep := range snapshot.CriticalDependencies {
		found := false
		for _, p := range snapshot.ImpactPaths {
			if p.DependencyEdgeID == dep.EdgeID {
				found = true
				break
			}
		}
		priority := "P1"
		if i < 3 {
			priority = "P0"
		}
		checks = append(checks, models.DomainGateCheck{
			CheckID:        "U4_critical_dependency_path_" + dep.EdgeID,
			Status:         ternary(found, "pass", "fail"),
			Priority:       priority,
			GapDescription: fmt.Sprintf("dependency=%s path_found=%v", dep.EdgeID, found),
			Affected:       []string{dep.EdgeID},
			EnrichmentHint: "Scout intermediate nodes for missing path",
		})
	}

	// U5 policy & direction.
	policyMissing := []string{}
	directionMissing := []string{}
	for _, p := range snapshot.ImpactPaths {
		for _, edgeID := range p.EdgeIDs {
			edge := edgeByID(g.Edges, edgeID)
			if edge == nil {
				continue
			}
			if strings.TrimSpace(edge.GetPolicyStatus()) == "" {
				policyMissing = append(policyMissing, edgeID)
			}
			if strings.TrimSpace(edge.SourceFragmentID) == "" || strings.TrimSpace(edge.TargetFragmentID) == "" {
				directionMissing = append(directionMissing, edgeID)
			}
		}
	}
	u5ok := len(policyMissing) == 0 && len(directionMissing) == 0
	checks = append(checks, models.DomainGateCheck{
		CheckID:        "U5_edge_direction_policy",
		Status:         ternary(u5ok, "pass", "fail"),
		Priority:       "P1",
		GapDescription: fmt.Sprintf("missing_policy=%d missing_direction=%d", len(policyMissing), len(directionMissing)),
		Affected:       append(policyMissing, directionMissing...),
		EnrichmentHint: "Re-run policy checks for critical paths",
	})

	// U6 data gap ratio.
	totalArtifacts := len(snapshot.CriticalDependencies) + len(snapshot.ImpactPaths) + len(snapshot.CriticalNodes) + len(snapshot.ConcentrationEvidence) + len(snapshot.ControlExposure)
	if totalArtifacts <= 0 {
		totalArtifacts = 1
	}
	gapRatio := float64(len(snapshot.DataGaps)) / float64(totalArtifacts)
	u6Status := "pass"
	u6Priority := "P2"
	if gapRatio >= e.gateConfig.MaxGapRatioP0 {
		u6Status = "fail"
		u6Priority = "P0"
	} else if gapRatio >= e.gateConfig.MaxGapRatioP1 {
		u6Status = "fail"
		u6Priority = "P1"
	}
	checks = append(checks, models.DomainGateCheck{
		CheckID:        "U6_data_gap_ratio",
		Status:         u6Status,
		Priority:       u6Priority,
		GapDescription: fmt.Sprintf("gap_ratio=%.3f", gapRatio),
		EnrichmentHint: "Prioritize top data gaps",
	})

	// U7 temporal validity.
	temporalCoverage := temporalCoverageRatio(g, snapshot.ImpactPaths, contract)
	checks = append(checks, models.DomainGateCheck{
		CheckID:        "U7_temporal_validity",
		Status:         ternary(temporalCoverage >= e.gateConfig.MinTemporalCoverage, "pass", "fail"),
		Priority:       "P2",
		GapDescription: fmt.Sprintf("temporal_coverage=%.3f threshold=%.3f", temporalCoverage, e.gateConfig.MinTemporalCoverage),
		EnrichmentHint: "Refresh stale edges in critical paths",
	})

	// U8 source diversity.
	sourceDiversityOK := true
	depsChecked := 0
	depsBelowThreshold := 0
	depsWithoutSources := 0
	for i, dep := range snapshot.CriticalDependencies {
		if i >= e.gateConfig.TopKDependencies {
			break
		}
		depsChecked++
		srcN := len(dep.SourceRefs)
		if srcN == 0 {
			depsWithoutSources++
		}
		if srcN < e.gateConfig.MinSourcesPerDep {
			depsBelowThreshold++
			sourceDiversityOK = false
		}
	}
	u8Status := "pass"
	u8Priority := "P3"
	if depsChecked > 0 && !sourceDiversityOK {
		// When at least one source exists, treat as advisory partial rather than hard fail.
		// Strict fail is kept only for total source absence across checked dependencies.
		if depsWithoutSources == depsChecked {
			u8Status = "fail"
			u8Priority = "P2"
		} else {
			u8Status = "partial"
		}
	}
	checks = append(checks, models.DomainGateCheck{
		CheckID:        "U8_source_diversity",
		Status:         u8Status,
		Priority:       u8Priority,
		GapDescription: fmt.Sprintf("min_sources_per_dep=%d checked=%d below_threshold=%d without_sources=%d", e.gateConfig.MinSourcesPerDep, depsChecked, depsBelowThreshold, depsWithoutSources),
		EnrichmentHint: "Corroborate single-source dependencies with additional sources",
	})

	// Contract-specific checks
	checks = append(checks, e.contractSpecificChecks(contract, g, facetNodes, snapshot)...)
	checks = capGateChecks(checks, e.gateConfig.MaxGateChecks)

	return summarizeChecks(1, checks)
}

func (e *Engine) contractSpecificChecks(contract Contract, g graphSnapshot, facetNodes map[string][]*models.HorizonFragment, snapshot *models.DomainAnswerSnapshot) []models.DomainGateCheck {
	profile := steeringProfileForContractType(contract.ContractType)
	out := make([]models.DomainGateCheck, 0, 12)
	switch profile {
	case SteeringProfileSupplyChain:
		out = append(out,
			checkSupplyChainEdgesOperational(g, facetNodes),
			checkSupplyChainBidirectional(g, facetNodes),
		)
	case SteeringProfileRegulatoryExposure:
		out = append(out,
			checkRegulatoryEdges(g),
			checkDecisionPoints(g),
			checkJurisdictionCoverage(facetNodes),
			checkMechanismSpecificity(g),
		)
	case SteeringProfileCompetitiveLandscape:
		out = append(out,
			checkMarketParticipation(g),
			checkMarketShareData(g),
			checkConcentrationEvidence(snapshot, g),
		)
	case SteeringProfileSectorIndustry:
		// Universal sector overview: market participation + structure (market research = sub-contract for markets)
		out = append(out,
			checkMarketParticipation(g),
			checkConcentrationEvidence(snapshot, g),
		)
	case SteeringProfileGeographicConcentration:
		out = append(out,
			checkGeoEdges(g, e.gateConfig.MinGeoCoverage),
			checkGeoDiversity(g),
		)
	}
	return out
}

func summarizeChecks(level int, checks []models.DomainGateCheck) gateResult {
	res := gateResult{
		Level:       level,
		Checks:      checks,
		TotalChecks: len(checks),
	}
	hasP0Fail := false
	hasP1Fail := false
	for _, c := range checks {
		switch c.Status {
		case "pass":
			res.Passed++
		case "partial":
			res.Partial++
		default:
			res.Failed++
			if strings.EqualFold(c.Priority, "P0") {
				hasP0Fail = true
			}
			if strings.EqualFold(c.Priority, "P1") {
				hasP1Fail = true
			}
		}
	}
	switch {
	case hasP0Fail:
		res.OverallStatus = "fail"
	case hasP1Fail:
		res.OverallStatus = "partial"
	default:
		res.OverallStatus = "pass"
	}
	if res.TotalChecks > 0 {
		res.Score = float64(res.Passed) / float64(res.TotalChecks)
	}
	return res
}

type semanticGateResponse struct {
	Status            string   `json:"status"`
	CriticalGaps      []string `json:"criticalGaps"`
	EnrichmentSignals []string `json:"enrichmentSignals"`
	Confidence        float64  `json:"confidence"`
}

func (e *Engine) evaluateGateL2(ctx context.Context, contract Contract, snapshot *models.DomainAnswerSnapshot, l1 gateResult, originalQuery string) gateResult {
	if e.aiExecutor == nil {
		logDomainContract(ctx, "gate.l2.skipped", nil, &contract, map[string]any{
			"reason":   "ai_executor_nil",
			"l1Status": l1.OverallStatus,
		})
		return gateResult{
			Level:         2,
			OverallStatus: "pass",
			TotalChecks:   1,
			Passed:        1,
			Score:         1.0,
			Checks: []models.DomainGateCheck{{
				CheckID:  "L2_semantic_completeness",
				Status:   "pass",
				Priority: "P2",
			}},
		}
	}
	prompt := buildL2Prompt(contract, snapshot, l1, originalQuery)
	verifier := ai.NewGenericVerifier(func(r semanticGateResponse) error {
		st := strings.TrimSpace(strings.ToLower(r.Status))
		if st != "pass" && st != "partial" && st != "fail" {
			return fmt.Errorf("invalid status")
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              6, // Flash-tier for cost control (was 7/pro)
			MinContextWindow:                12000,
			MaxOutputSize:                   2000,
		},
		Verifier: verifier,
	}
	raw, fullResult, err := e.aiExecutor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		logDomainContract(ctx, "gate.l2.ai.error", nil, &contract, map[string]any{
			"error": err.Error(),
		})
		return gateResult{
			Level:         2,
			OverallStatus: "partial",
			TotalChecks:   1,
			Failed:        1,
			Checks: []models.DomainGateCheck{{
				CheckID:        "L2_semantic_completeness",
				Status:         "fail",
				Priority:       "P1",
				GapDescription: "semantic critic unavailable",
				EnrichmentHint: "Retry semantic critic with refreshed context",
			}},
		}
	}
	if snapshot != nil && fullResult != nil {
		applyGroundingEvidenceToSnapshot(snapshot, fullResult.GroundingEvidence)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		logDomainContract(ctx, "gate.l2.parse.error", nil, &contract, map[string]any{
			"stage": "marshal",
			"error": err.Error(),
		})
		b = []byte("{}")
	}
	var parsed semanticGateResponse
	if err := json.Unmarshal(b, &parsed); err != nil {
		logDomainContract(ctx, "gate.l2.parse.error", nil, &contract, map[string]any{
			"stage":       "unmarshal",
			"payloadSize": len(b),
			"error":       err.Error(),
		})
	}
	status := strings.TrimSpace(strings.ToLower(parsed.Status))
	if status == "" {
		logDomainContract(ctx, "gate.l2.status.empty", nil, &contract, map[string]any{
			"fallbackStatus": "partial",
		})
		status = "partial"
	}
	priority := "P1"
	if status == "pass" {
		priority = "P3"
	}
	check := models.DomainGateCheck{
		CheckID:        "L2_semantic_completeness",
		Status:         status,
		Priority:       priority,
		GapDescription: strings.Join(parsed.CriticalGaps, "; "),
		EnrichmentHint: strings.Join(parsed.EnrichmentSignals, "; "),
		Affected:       parsed.CriticalGaps,
	}
	res := summarizeChecks(2, []models.DomainGateCheck{check})
	if status == "partial" && res.OverallStatus == "pass" {
		res.OverallStatus = "partial"
	}
	return res
}

func buildL2Prompt(contract Contract, snapshot *models.DomainAnswerSnapshot, l1 gateResult, originalQuery string) string {
	originalQuery = strings.Join(strings.Fields(strings.TrimSpace(originalQuery)), " ")
	if originalQuery == "" {
		originalQuery = "(not provided)"
	}
	return strings.TrimSpace(fmt.Sprintf(`
Return JSON only:
{
  "status": "pass|partial|fail",
  "criticalGaps": ["..."],
  "enrichmentSignals": ["..."],
  "confidence": 0.0
}

Domain contract:
- type: %s
- title: %s
- facets: %s

Original user query:
- %s

Artifacts:
- criticalDependencies: %d
- criticalNodes: %d
- concentrationEvidence: %d
- controlExposure: %d
- impactPaths: %d
- dataGaps: %d

Gate L1:
- status: %s
- failedChecks: %s

Evaluate semantic completeness against the original contract. If answer can be delivered with current evidence, status=pass.
`, contract.ContractType, contract.Title, asJSON(contract.Facets), originalQuery, len(snapshot.CriticalDependencies), len(snapshot.CriticalNodes), len(snapshot.ConcentrationEvidence), len(snapshot.ControlExposure), len(snapshot.ImpactPaths), len(snapshot.DataGaps), l1.OverallStatus, strings.Join(l1.failedCheckIDs(), ", ")))
}

func buildEnrichmentTasks(_ Contract, gate gateResult) []EnrichmentTask {
	out := make([]EnrichmentTask, 0, len(gate.Checks))
	for _, c := range gate.Checks {
		if c.Status != "fail" {
			continue
		}
		task := EnrichmentTask{
			Priority:    c.Priority,
			GapType:     c.CheckID,
			Hint:        c.EnrichmentHint,
			MissingInfo: []string{},
		}
		if strings.Contains(c.CheckID, "inter_facet_connectivity") {
			task.GapType = "cross_facet_connectivity"
		}
		if strings.Contains(c.CheckID, "market") {
			task.GapType = "metric_gap"
		}
		if strings.Contains(c.CheckID, "critical_dependency_path") {
			task.GapType = "path_gap"
		}
		for _, a := range c.Affected {
			if strings.HasPrefix(a, "/") {
				task.TargetPath = a
				break
			}
		}
		if strings.TrimSpace(task.Hint) != "" {
			task.MissingInfo = append(task.MissingInfo, task.Hint)
		}
		out = append(out, task)
	}
	sort.SliceStable(out, func(i, j int) bool { return priorityRank(out[i].Priority) < priorityRank(out[j].Priority) })
	return out
}

func priorityRank(p string) int {
	switch strings.TrimSpace(strings.ToUpper(p)) {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	default:
		return 3
	}
}

func (e *Engine) enqueueEnrichmentTasks(ctx context.Context, parent *models.ScoutingJob, contract Contract, tasks []EnrichmentTask, cycle int) []string {
	if e.scoutingJobs == nil || parent == nil {
		logDomainContract(ctx, "enrich.enqueue.skipped", parent, &contract, map[string]any{
			"reason": "missing_scouting_jobs_or_parent",
			"cycle":  cycle,
		})
		return nil
	}
	// Supply chain: enrichment is done by domain expansion (EnrichSupplyChainFull → processSupplyChains).
	// Entity jobs would run extraction+processRelations (org→org SUPPLIES_TO, DEPENDS_ON) — wrong pipeline.
	if steeringProfileForContractType(contract.ContractType) == SteeringProfileSupplyChain {
		logDomainContract(ctx, "enrich.enqueue.skipped", parent, &contract, map[string]any{
			"reason": "supply_chain_enrichment_via_domain_expansion_only",
			"cycle":  cycle,
		})
		return nil
	}
	var createdJobIDs []string
	maxJobs := 8
	for i, task := range tasks {
		if i >= maxJobs {
			break
		}
		targetPath := strings.TrimSpace(task.TargetPath)
		if targetPath == "" {
			if len(contract.FirstOrderEntities) > 0 {
				targetPath = strings.TrimSpace(contract.FirstOrderEntities[i%len(contract.FirstOrderEntities)])
			} else {
				targetPath = strings.TrimSpace(parent.TargetCanonicalPath)
			}
		}
		if targetPath == "" {
			logDomainContract(ctx, "enrich.enqueue.skip_target", parent, &contract, map[string]any{
				"cycle":    cycle,
				"priority": task.Priority,
				"gapType":  task.GapType,
			})
			continue
		}
		now := time.Now().UTC()
		state := map[string]interface{}{
			"targetCanonicalPath": targetPath,
			"lensId":              strings.TrimSpace(parent.LensID),
			"currentState":        string(models.ScoutingJobStatePending),
			"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
			"recursionDepth":      0.0,
			"maxRecursionDepth":   1.0,
			"domains": []map[string]interface{}{
				{
					"canonicalPath": strings.TrimSpace(contract.CanonicalPath),
					"contractType":  strings.TrimSpace(contract.ContractType),
					"title":         strings.TrimSpace(contract.Title),
					"description":   strings.TrimSpace(contract.Description),
					"facets":        contract.Facets,
				},
			},
			"missingInfo": []string{strings.TrimSpace(task.Hint)},
			"trigger":     "domain_contract_engine_enrich",
			"enrichment": map[string]interface{}{
				"cycle":    cycle,
				"gapType":  task.GapType,
				"priority": task.Priority,
			},
		}
		if parent.State != nil {
			if v, ok := parent.State["correlationId"]; ok {
				state["correlationId"] = v
			}
			if v, ok := parent.State["sessionId"]; ok {
				state["sessionId"] = v
			}
		}
		job := &models.ScoutingJob{
			AgentJob: agentmodels.AgentJob{
				TenantID:  strings.TrimSpace(parent.TenantID),
				ProjectID: strings.TrimSpace(parent.ProjectID),
				AgentID:   "scout",
				Status:    agentmodels.AgentJobStatusPending,
				CreatedAt: now,
				UpdatedAt: now,
				State:     state,
			},
			LensID:              strings.TrimSpace(parent.LensID),
			TargetCanonicalPath: targetPath,
			ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
			BillingContext: models.BillingContext{
				ChargeModel: string(models.ChargeModelBounty),
				ProjectID:   strings.TrimSpace(parent.ProjectID),
			},
			CurrentState: models.ScoutingJobStatePending,
		}
		jobID, err := e.scoutingJobs.Create(ctx, job)
		if err != nil {
			logDomainContract(ctx, "enrich.enqueue.error", parent, &contract, map[string]any{
				"cycle":      cycle,
				"targetPath": targetPath,
				"gapType":    task.GapType,
				"priority":   task.Priority,
				"error":      err.Error(),
			})
			continue
		}
		createdJobIDs = append(createdJobIDs, strings.TrimSpace(jobID))
		logDomainContract(ctx, "enrich.enqueue.ok", parent, &contract, map[string]any{
			"cycle":      cycle,
			"targetPath": targetPath,
			"gapType":    task.GapType,
			"priority":   task.Priority,
			"childJobId": strings.TrimSpace(jobID),
		})
	}
	return createdJobIDs
}

type explanatoryUnitResponse struct {
	EdgeID        string   `json:"edgeId"`
	Mechanism     string   `json:"mechanism"`
	RoleInThesis  string   `json:"roleInThesis"`
	Explanation   string   `json:"explanation"`
	Confidence    float64  `json:"confidence"`
	AnchorNodeIDs []string `json:"anchorNodeIds"`
	AnchorEdgeIDs []string `json:"anchorEdgeIds"`
	EvidenceRefs  []string `json:"evidenceRefs"`
}

type explanatoryResponse struct {
	Units []explanatoryUnitResponse `json:"units"`
}

func (e *Engine) applyExplanatoryLayer(ctx context.Context, contract Contract, snapshot *models.DomainAnswerSnapshot, g graphSnapshot, originalQuery, phase string) {
	if snapshot == nil {
		return
	}

	candidates := e.buildExplanatoryCandidates(contract, g)
	tokenBudget := e.gateConfig.ExplanatoryTokenBudget
	if tokenBudget <= 0 {
		tokenBudget = 100000
	}
	maxUnits := e.gateConfig.ExplanatoryMaxUnits
	if maxUnits <= 0 {
		maxUnits = 120
	}
	thesisContext := strings.TrimSpace(snapshot.Thesis.Text)
	if thesisContext == "" {
		thesisContext = fallbackThesis(contract, snapshot)
	}

	layer := &models.DomainExplanatoryLayer{
		Status:               "ready",
		Phase:                strings.TrimSpace(phase),
		GeneratedAt:          time.Now().UTC(),
		ThesisContext:        thesisContext,
		CandidateCount:       len(candidates),
		TokenBudget:          tokenBudget,
		SourceDependencyHash: dependencyHashFromCriticalDependencies(candidates),
		Units:                []models.DomainExplanatoryUnit{},
		Errors:               []string{},
	}

	if len(candidates) == 0 {
		snapshot.ExplanatoryLayer = layer
		return
	}

	// Fallback path when AI is unavailable: still persist domain-bound explanation atoms.
	if e.aiExecutor == nil {
		layer.Units = deterministicExplanatoryUnits(candidates, maxUnits)
		layer.UnitCount = len(layer.Units)
		layer.CoveredDependencyCount = len(layer.Units)
		snapshot.ExplanatoryLayer = layer
		return
	}

	chunkTokens := e.gateConfig.ExplanatoryChunkTokens
	if chunkTokens <= 0 {
		chunkTokens = 12000
	}
	chunks := chunkCriticalDependenciesByToken(candidates, chunkTokens)
	candidateByEdgeID := map[string]models.CriticalDependency{}
	rankByEdgeID := map[string]int{}
	for i, dep := range candidates {
		candidateByEdgeID[strings.TrimSpace(dep.EdgeID)] = dep
		rankByEdgeID[strings.TrimSpace(dep.EdgeID)] = i + 1
	}
	unitsByEdge := map[string]models.DomainExplanatoryUnit{}
	tokenSpent := 0

	verifier := ai.NewGenericVerifier(func(r explanatoryResponse) error {
		// Empty payload is allowed (we fallback below).
		for _, u := range r.Units {
			if strings.TrimSpace(u.EdgeID) == "" {
				return fmt.Errorf("edgeId is required for each explanatory unit")
			}
		}
		return nil
	})

	for idx, chunk := range chunks {
		if len(chunk) == 0 || len(unitsByEdge) >= maxUnits {
			break
		}
		prompt := buildExplanatoryPrompt(contract, originalQuery, thesisContext, idx+1, len(chunks), chunk)
		estInputTokens := len(prompt) / 4
		if tokenSpent+estInputTokens > tokenBudget && len(unitsByEdge) > 0 {
			break
		}
		task := &ai.AITask{
			TaskType:     ai.TaskTypeKnowledgeSynthesis,
			InputContent: prompt,
			AIRequirements: ai.AIRequirements{
				TextGenerationCapability:        true,
				SearchGroundingCapability:       true,
				InternalDataGroundingCapability: true,
				MinCapabilityScore:              6, // Flash-tier for cost control (was 7/pro)
				MinContextWindow:                maxInt(12000, estInputTokens+2500),
				MaxOutputSize:                   3200,
			},
			Verifier: verifier,
		}
		raw, fullResult, err := e.aiExecutor.ExecuteStructuredTaskFull(ctx, task)
		if err != nil {
			layer.Errors = append(layer.Errors, fmt.Sprintf("chunk_%d: %s", idx+1, strings.TrimSpace(err.Error())))
			continue
		}
		if fullResult != nil {
			layer.ConsumedInputTokens += maxInt(0, fullResult.InputTokens)
			layer.ConsumedOutputTokens += maxInt(0, fullResult.OutputTokens)
			tokenSpent += maxInt(0, fullResult.InputTokens) + maxInt(0, fullResult.OutputTokens)
		} else {
			layer.ConsumedInputTokens += estInputTokens
			tokenSpent += estInputTokens
		}
		data, err := json.Marshal(raw)
		if err != nil {
			layer.Errors = append(layer.Errors, fmt.Sprintf("chunk_%d_marshal: %s", idx+1, strings.TrimSpace(err.Error())))
			continue
		}
		var parsed explanatoryResponse
		if err := json.Unmarshal(data, &parsed); err != nil {
			layer.Errors = append(layer.Errors, fmt.Sprintf("chunk_%d_unmarshal: %s", idx+1, strings.TrimSpace(err.Error())))
			continue
		}
		for _, u := range parsed.Units {
			edgeID := strings.TrimSpace(u.EdgeID)
			if edgeID == "" {
				continue
			}
			dep, ok := candidateByEdgeID[edgeID]
			if !ok {
				continue
			}
			unit := mapExplanatoryUnitResponse(dep, rankByEdgeID[edgeID], u)
			if prev, exists := unitsByEdge[edgeID]; exists && prev.Confidence >= unit.Confidence {
				continue
			}
			unitsByEdge[edgeID] = unit
			if len(unitsByEdge) >= maxUnits {
				break
			}
		}
		if tokenSpent >= tokenBudget {
			break
		}
	}

	if len(unitsByEdge) == 0 {
		layer.Units = deterministicExplanatoryUnits(candidates, maxUnits)
		// Keep layer usable even if AI parse/execution failed for all chunks.
		layer.Status = "ready"
	} else {
		units := make([]models.DomainExplanatoryUnit, 0, len(unitsByEdge))
		for _, u := range unitsByEdge {
			units = append(units, u)
		}
		sort.SliceStable(units, func(i, j int) bool {
			if units[i].DependencyRank == units[j].DependencyRank {
				return units[i].Confidence > units[j].Confidence
			}
			return units[i].DependencyRank < units[j].DependencyRank
		})
		if len(units) > maxUnits {
			units = units[:maxUnits]
		}
		layer.Units = units
	}
	if len(layer.Errors) > 8 {
		layer.Errors = append(layer.Errors[:8], fmt.Sprintf("..._%d_more_errors", len(layer.Errors)-8))
	}
	layer.UnitCount = len(layer.Units)
	layer.CoveredDependencyCount = len(layer.Units)
	if strings.TrimSpace(layer.Status) == "" {
		layer.Status = "ready"
	}
	snapshot.ExplanatoryLayer = layer
}

func (e *Engine) buildExplanatoryCandidates(contract Contract, g graphSnapshot) []models.CriticalDependency {
	edges := make([]*models.DependencyEdge, 0, len(g.Edges))
	for _, edge := range g.Edges {
		if edge == nil {
			continue
		}
		edges = append(edges, edge)
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return topDependencyScore(edges[i], g) > topDependencyScore(edges[j], g)
	})
	maxCandidates := e.gateConfig.ExplanatoryMaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 120
	}
	facetNodes := buildFacetNodes(contract, g)
	out := make([]models.CriticalDependency, 0, minInt(maxCandidates, len(edges)))
	for i, edge := range edges {
		if i >= maxCandidates {
			break
		}
		src := g.NodesByID[edge.SourceFragmentID]
		dst := g.NodesByID[edge.TargetFragmentID]
		dep := models.CriticalDependency{
			EdgeID:             strings.TrimSpace(edge.ID),
			SourceFragmentID:   strings.TrimSpace(edge.SourceFragmentID),
			TargetFragmentID:   strings.TrimSpace(edge.TargetFragmentID),
			RelationType:       string(edge.Relation),
			TopDependencyScore: topDependencyScore(edge, g),
			Confidence:         dependencyConfidence(edge),
			SourceRefs:         dependencySourceRefs(edge),
		}
		if src != nil {
			dep.SourceCanonicalPath = strings.TrimSpace(src.CanonicalPath)
		}
		if dst != nil {
			dep.TargetCanonicalPath = strings.TrimSpace(dst.CanonicalPath)
		}
		dep.FacetKey = inferFacetForDependency(dep, facetNodes)
		out = append(out, dep)
	}
	return out
}

func chunkCriticalDependenciesByToken(deps []models.CriticalDependency, chunkTokens int) [][]models.CriticalDependency {
	if len(deps) == 0 {
		return nil
	}
	if chunkTokens <= 0 {
		chunkTokens = 12000
	}
	overhead := 900 // prompt/schema overhead
	out := make([][]models.CriticalDependency, 0, 8)
	cur := make([]models.CriticalDependency, 0, 24)
	curTokens := overhead
	push := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, cur)
		cur = make([]models.CriticalDependency, 0, 24)
		curTokens = overhead
	}
	for _, dep := range deps {
		itemTokens := len(asJSON(dep))/4 + 40
		if curTokens+itemTokens > chunkTokens && len(cur) > 0 {
			push()
		}
		cur = append(cur, dep)
		curTokens += itemTokens
	}
	push()
	return out
}

func buildExplanatoryPrompt(contract Contract, originalQuery, thesisContext string, chunkIndex, totalChunks int, deps []models.CriticalDependency) string {
	if strings.TrimSpace(originalQuery) == "" {
		originalQuery = "(not provided)"
	}
	return strings.TrimSpace(fmt.Sprintf(`
Return JSON only:
{
  "units": [
    {
      "edgeId": "...",
      "mechanism": "...",
      "roleInThesis": "...",
      "explanation": "...",
      "confidence": 0.0,
      "anchorNodeIds": ["sourceFragmentId","targetFragmentId"],
      "anchorEdgeIds": ["edgeId"],
      "evidenceRefs": ["sourceRef1","sourceRef2"]
    }
  ]
}

Rules:
- Use ONLY edgeId values from the provided dependency list.
- Explain mechanism in domain context (not generic node description).
- Keep explanation concise (1-2 sentences per unit).
- Prefer explicit policy/mechanism words from relationType.
- If evidenceRefs are unavailable, return empty array.

Domain context:
- contractType: %s
- title: %s
- description: %s
- facets: %s
- originalQuery: %s
- thesisContext: %s
- chunk: %d/%d

Dependency candidates:
%s
`, contract.ContractType, contract.Title, contract.Description, asJSON(contract.Facets), originalQuery, thesisContext, chunkIndex, totalChunks, asJSON(deps)))
}

func mapExplanatoryUnitResponse(dep models.CriticalDependency, rank int, in explanatoryUnitResponse) models.DomainExplanatoryUnit {
	edgeID := strings.TrimSpace(dep.EdgeID)
	anchorEdgeIDs := cleanStringList(in.AnchorEdgeIDs, 4)
	if len(anchorEdgeIDs) == 0 {
		anchorEdgeIDs = []string{edgeID}
	}
	anchorNodeIDs := cleanStringList(in.AnchorNodeIDs, 4)
	if len(anchorNodeIDs) == 0 {
		if strings.TrimSpace(dep.SourceFragmentID) != "" {
			anchorNodeIDs = append(anchorNodeIDs, strings.TrimSpace(dep.SourceFragmentID))
		}
		if strings.TrimSpace(dep.TargetFragmentID) != "" {
			anchorNodeIDs = append(anchorNodeIDs, strings.TrimSpace(dep.TargetFragmentID))
		}
	}
	mechanism := strings.TrimSpace(in.Mechanism)
	if mechanism == "" {
		mechanism = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(dep.RelationType), "_", " "))
	}
	role := strings.TrimSpace(in.RoleInThesis)
	if role == "" {
		role = fmt.Sprintf("dependency in %s domain mechanism", strings.TrimSpace(dep.FacetKey))
	}
	explanation := strings.TrimSpace(in.Explanation)
	if explanation == "" {
		explanation = fmt.Sprintf(
			"%s -> %s via %s contributes to the domain mechanism and influences thesis outcomes.",
			shortCanonicalForExplanation(dep.SourceCanonicalPath),
			shortCanonicalForExplanation(dep.TargetCanonicalPath),
			strings.ToLower(strings.ReplaceAll(strings.TrimSpace(dep.RelationType), "_", " ")),
		)
	}
	conf := clamp01(in.Confidence)
	if conf == 0 {
		conf = clamp01(0.35 + 0.35*dep.TopDependencyScore + 0.30*dep.Confidence)
	}
	return models.DomainExplanatoryUnit{
		UnitID:              explanatoryUnitID(edgeID, rank),
		DependencyEdgeID:    edgeID,
		DependencyRank:      rank,
		TopDependencyScore:  dep.TopDependencyScore,
		RelationType:        strings.TrimSpace(dep.RelationType),
		FacetKey:            strings.TrimSpace(dep.FacetKey),
		SourceCanonicalPath: strings.TrimSpace(dep.SourceCanonicalPath),
		TargetCanonicalPath: strings.TrimSpace(dep.TargetCanonicalPath),
		AnchorNodeIDs:       anchorNodeIDs,
		AnchorEdgeIDs:       anchorEdgeIDs,
		Mechanism:           mechanism,
		RoleInThesis:        role,
		ExplanationText:     explanation,
		EvidenceRefs:        cleanStringList(append(cleanStringList(in.EvidenceRefs, 6), dep.SourceRefs...), 6),
		Confidence:          conf,
	}
}

func deterministicExplanatoryUnits(deps []models.CriticalDependency, maxUnits int) []models.DomainExplanatoryUnit {
	if maxUnits <= 0 {
		maxUnits = 120
	}
	out := make([]models.DomainExplanatoryUnit, 0, minInt(maxUnits, len(deps)))
	for i, dep := range deps {
		if i >= maxUnits {
			break
		}
		relation := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(dep.RelationType), "_", " "))
		if relation == "" {
			relation = "dependency"
		}
		role := "material mechanism in domain thesis"
		if strings.TrimSpace(dep.FacetKey) != "" {
			role = fmt.Sprintf("material mechanism in facet '%s'", strings.TrimSpace(dep.FacetKey))
		}
		out = append(out, models.DomainExplanatoryUnit{
			UnitID:              explanatoryUnitID(dep.EdgeID, i+1),
			DependencyEdgeID:    strings.TrimSpace(dep.EdgeID),
			DependencyRank:      i + 1,
			TopDependencyScore:  dep.TopDependencyScore,
			RelationType:        strings.TrimSpace(dep.RelationType),
			FacetKey:            strings.TrimSpace(dep.FacetKey),
			SourceCanonicalPath: strings.TrimSpace(dep.SourceCanonicalPath),
			TargetCanonicalPath: strings.TrimSpace(dep.TargetCanonicalPath),
			AnchorNodeIDs: cleanStringList([]string{
				strings.TrimSpace(dep.SourceFragmentID),
				strings.TrimSpace(dep.TargetFragmentID),
			}, 4),
			AnchorEdgeIDs: []string{strings.TrimSpace(dep.EdgeID)},
			Mechanism:     relation,
			RoleInThesis:  role,
			ExplanationText: fmt.Sprintf(
				"%s affects %s through %s; this dependency is part of the domain explanatory layer.",
				shortCanonicalForExplanation(dep.SourceCanonicalPath),
				shortCanonicalForExplanation(dep.TargetCanonicalPath),
				relation,
			),
			EvidenceRefs: cleanStringList(dep.SourceRefs, 6),
			Confidence:   clamp01(0.35 + 0.35*dep.TopDependencyScore + 0.30*dep.Confidence),
		})
	}
	return out
}

func explanatoryUnitID(edgeID string, rank int) string {
	base := strings.TrimSpace(edgeID)
	if base == "" {
		base = fmt.Sprintf("rank_%d", rank)
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d", base, rank)))
	return "exu_" + hex.EncodeToString(sum[:])[:16]
}

func shortCanonicalForExplanation(cp string) string {
	cp = strings.TrimSpace(cp)
	if cp == "" {
		return "unknown"
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 0 {
		return cp
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return cp
	}
	return strings.ReplaceAll(last, "-", " ")
}

func cleanStringList(in []string, maxItems int) []string {
	if maxItems <= 0 {
		maxItems = len(in)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, minInt(maxItems, len(in)))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func cloneDomainExplanatoryLayer(in *models.DomainExplanatoryLayer) *models.DomainExplanatoryLayer {
	if in == nil {
		return nil
	}
	out := *in
	out.Errors = append([]string(nil), in.Errors...)
	out.Units = append([]models.DomainExplanatoryUnit(nil), in.Units...)
	return &out
}

func dependencyHashFromCriticalDependencies(deps []models.CriticalDependency) string {
	if len(deps) == 0 {
		return ""
	}
	keys := make([]string, 0, len(deps))
	for _, d := range deps {
		edgeID := strings.TrimSpace(d.EdgeID)
		if edgeID == "" {
			continue
		}
		keys = append(keys, edgeID)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	sum := sha1.Sum([]byte(strings.Join(keys, "|")))
	return hex.EncodeToString(sum[:])
}

type narrativeResponse struct {
	Thesis struct {
		Text       string  `json:"text"`
		Confidence float64 `json:"confidence"`
	} `json:"thesis"`
	Alternatives   []models.Alternative  `json:"alternatives"`
	ObjectiveCheck models.ObjectiveCheck `json:"objectiveCheck"`
}

func (e *Engine) applyStructuredNarrative(ctx context.Context, contract Contract, snapshot *models.DomainAnswerSnapshot) {
	if snapshot == nil {
		return
	}
	if e.aiExecutor == nil {
		logDomainContract(ctx, "narrative.skipped", nil, &contract, map[string]any{
			"reason": "ai_executor_nil",
		})
		snapshot.Thesis = models.DomainThesis{
			Text:       fallbackThesis(contract, snapshot),
			Confidence: 0.6,
		}
		snapshot.ObjectiveCheck = models.ObjectiveCheck{
			OntologyConformance: "pass",
			PolicyConformance:   "pass",
			Completeness:        snapshot.ReadinessStatus,
			Confidence:          0.6,
		}
		return
	}
	prompt := buildNarrativePrompt(contract, snapshot)
	verifier := ai.NewGenericVerifier(func(r narrativeResponse) error {
		if strings.TrimSpace(r.Thesis.Text) == "" {
			return fmt.Errorf("thesis.text is required")
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			MinCapabilityScore:              6, // Flash-tier for cost control (was 7/pro)
			MinContextWindow:                12000,
			MaxOutputSize:                   3000,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
		},
		Verifier: verifier,
	}
	raw, fullResult, err := e.aiExecutor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		logDomainContract(ctx, "narrative.ai.error", nil, &contract, map[string]any{
			"error": err.Error(),
		})
		snapshot.Thesis = models.DomainThesis{
			Text:       fallbackThesis(contract, snapshot),
			Confidence: 0.6,
		}
		snapshot.ObjectiveCheck = models.ObjectiveCheck{
			OntologyConformance: "partial",
			PolicyConformance:   "pass",
			Completeness:        snapshot.ReadinessStatus,
			Confidence:          0.55,
		}
		return
	}
	if fullResult != nil {
		applyGroundingEvidenceToSnapshot(snapshot, fullResult.GroundingEvidence)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		logDomainContract(ctx, "narrative.parse.error", nil, &contract, map[string]any{
			"stage": "marshal",
			"error": err.Error(),
		})
		snapshot.Thesis = models.DomainThesis{
			Text:       fallbackThesis(contract, snapshot),
			Confidence: 0.6,
		}
		snapshot.ObjectiveCheck = models.ObjectiveCheck{
			OntologyConformance: "partial",
			PolicyConformance:   "pass",
			Completeness:        snapshot.ReadinessStatus,
			Confidence:          0.55,
		}
		return
	}
	var parsed narrativeResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		logDomainContract(ctx, "narrative.parse.error", nil, &contract, map[string]any{
			"stage":       "unmarshal",
			"payloadSize": len(data),
			"error":       err.Error(),
		})
		snapshot.Thesis = models.DomainThesis{
			Text:       fallbackThesis(contract, snapshot),
			Confidence: 0.6,
		}
		snapshot.ObjectiveCheck = models.ObjectiveCheck{
			OntologyConformance: "partial",
			PolicyConformance:   "pass",
			Completeness:        snapshot.ReadinessStatus,
			Confidence:          0.55,
		}
		return
	}
	if strings.TrimSpace(parsed.Thesis.Text) == "" {
		logDomainContract(ctx, "narrative.parse.empty_thesis", nil, &contract, map[string]any{
			"fallback": true,
		})
		parsed.Thesis.Text = fallbackThesis(contract, snapshot)
		if parsed.Thesis.Confidence == 0 {
			parsed.Thesis.Confidence = 0.6
		}
	}
	snapshot.Thesis = models.DomainThesis{
		Text:       strings.TrimSpace(parsed.Thesis.Text),
		Confidence: clamp01(parsed.Thesis.Confidence),
	}
	snapshot.Alternatives = parsed.Alternatives
	snapshot.ObjectiveCheck = parsed.ObjectiveCheck
	if strings.TrimSpace(snapshot.ObjectiveCheck.Completeness) == "" {
		snapshot.ObjectiveCheck.Completeness = snapshot.ReadinessStatus
	}
}

func applyGroundingEvidenceToSnapshot(snapshot *models.DomainAnswerSnapshot, evidence *ai.GroundingEvidence) {
	if snapshot == nil || evidence == nil {
		return
	}
	if snapshot.InternalEvidence == nil {
		snapshot.InternalEvidence = &models.GroundingInternalEvidence{}
	}
	if snapshot.ExternalEvidence == nil {
		snapshot.ExternalEvidence = &models.GroundingExternalEvidence{}
	}
	snapshot.InternalEvidence.FragmentIDs = appendUniqueTrimmed(snapshot.InternalEvidence.FragmentIDs, evidence.DatastoreFragmentIDs...)
	snapshot.InternalEvidence.DatastoreURIs = appendUniqueTrimmed(snapshot.InternalEvidence.DatastoreURIs, evidence.DatastoreURIs...)
	for _, web := range evidence.WebSources {
		snapshot.ExternalEvidence.WebURLs = appendUniqueTrimmed(snapshot.ExternalEvidence.WebURLs, strings.TrimSpace(web.URL))
		citation := strings.TrimSpace(web.Title)
		if citation == "" {
			citation = strings.TrimSpace(web.URL)
		}
		snapshot.ExternalEvidence.Citations = appendUniqueTrimmed(snapshot.ExternalEvidence.Citations, citation)
	}
	hasInternal := len(snapshot.InternalEvidence.FragmentIDs) > 0 || len(snapshot.InternalEvidence.DatastoreURIs) > 0
	hasExternal := len(snapshot.ExternalEvidence.WebURLs) > 0 || len(snapshot.ExternalEvidence.Citations) > 0
	switch {
	case hasInternal && hasExternal:
		snapshot.OriginMode = "mixed"
	case hasInternal:
		snapshot.OriginMode = "internal_only"
	case hasExternal:
		snapshot.OriginMode = "external_only"
	}
}

func appendUniqueTrimmed(base []string, values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(values))
	for _, v := range base {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func buildNarrativePrompt(contract Contract, snapshot *models.DomainAnswerSnapshot) string {
	return strings.TrimSpace(fmt.Sprintf(`
Return JSON only:
{
  "thesis": {"text":"...", "confidence":0.0},
  "alternatives": [{"title":"...", "description":"...", "feasibility":"...", "capacityDelta":"...", "latencyDelta":"...", "costDelta":"...", "timeToSwitch":"...", "confidence":0.0}],
  "objectiveCheck": {
    "ontologyConformance":"pass|partial|fail",
    "policyConformance":"pass|partial|fail",
    "provenanceQuality":"high|medium|low",
    "biasAssessment":"...",
    "completeness":"ready|ready_with_gaps",
    "confidence":0.0
  }
}

Contract:
- type: %s
- title: %s
- description: %s
- facets: %s

Artifacts:
- criticalDependencies: %s
- concentrationEvidence: %s
- controlExposure: %s
- dataGaps: %s
`, contract.ContractType, contract.Title, contract.Description, asJSON(contract.Facets), asJSON(snapshot.CriticalDependencies), asJSON(snapshot.ConcentrationEvidence), asJSON(snapshot.ControlExposure), asJSON(snapshot.DataGaps)))
}

func fallbackThesis(contract Contract, snapshot *models.DomainAnswerSnapshot) string {
	return fmt.Sprintf("%s analysis identified %d critical dependencies, %d impact paths, and %d explicit data gaps.",
		strings.TrimSpace(contract.Title), len(snapshot.CriticalDependencies), len(snapshot.ImpactPaths), len(snapshot.DataGaps))
}

func adaptSnapshotToViewDirective(snapshot *models.DomainAnswerSnapshot) *models.ViewDirective {
	if snapshot == nil {
		return nil
	}
	d := &models.ViewDirective{
		ContractType: strings.TrimSpace(snapshot.DomainContract.ContractType),
		Summary:      strings.TrimSpace(snapshot.Thesis.Text),
	}
	d.GraphView = models.GraphViewForDirective(d.ContractType, d.HierarchyAxis)
	for _, p := range snapshot.ImpactPaths {
		if len(p.NodePaths) == 0 {
			continue
		}
		d.HighlightPaths = append(d.HighlightPaths, models.HighlightPath{
			NodePaths: append([]string(nil), p.NodePaths...),
			Label:     "impact path",
			Style:     "primary",
		})
	}
	for _, dep := range snapshot.CriticalDependencies {
		if dep.SourceCanonicalPath != "" {
			d.FocusNodePaths = append(d.FocusNodePaths, dep.SourceCanonicalPath)
		}
		if dep.TargetCanonicalPath != "" {
			d.FocusNodePaths = append(d.FocusNodePaths, dep.TargetCanonicalPath)
		}
	}
	seenFocus := map[string]struct{}{}
	focus := make([]string, 0, len(d.FocusNodePaths))
	for _, cp := range d.FocusNodePaths {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			continue
		}
		if _, ok := seenFocus[cp]; ok {
			continue
		}
		seenFocus[cp] = struct{}{}
		focus = append(focus, cp)
	}
	d.FocusNodePaths = focus
	for _, ev := range snapshot.ConcentrationEvidence {
		if ev.MarketCanonicalPath == "" {
			continue
		}
		if ev.CR4 != nil {
			d.Annotations = append(d.Annotations, models.Annotation{
				CanonicalPath: ev.MarketCanonicalPath,
				Key:           "cr4",
				Value:         fmt.Sprintf("%.3f", *ev.CR4),
			})
		}
		if ev.HHI != nil {
			d.Annotations = append(d.Annotations, models.Annotation{
				CanonicalPath: ev.MarketCanonicalPath,
				Key:           "hhi",
				Value:         fmt.Sprintf("%.1f", *ev.HHI),
			})
		}
	}
	if len(snapshot.DataGaps) > 0 {
		rp := &models.ResearchPlan{
			MissingEdges: []models.MissingEdge{},
			MissingNodes: []string{},
			Reason:       "domain_answer_data_gaps",
		}
		for _, gap := range snapshot.DataGaps {
			if gap.AffectedNodePath != "" {
				rp.MissingNodes = append(rp.MissingNodes, gap.AffectedNodePath)
			}
		}
		d.ResearchPlan = rp
	}
	d.FilterRelTypes = defaultFilterRelTypes(snapshot.DomainContract.ContractType)
	return d
}

// defaultFilterRelTypes enumerates legacy edge labels for LLM filter hints
// (Category B — same contract as expectedRelationsForContractType).
func defaultFilterRelTypes(contractType string) []string {
	switch steeringProfileForContractType(contractType) {
	case SteeringProfileSupplyChain:
		return []string{"SUPPLIES_TO", "PRODUCES", "transforms_into", "assembles_into", "USED_IN", "EXTRACTS_FROM"}
	case SteeringProfileRegulatoryExposure:
		return []string{"REGULATES", "CONTROLS_ACCESS", "USES_MECHANISM", "APPLIES_TO"}
	case SteeringProfilePolicyImpact:
		return []string{"REGULATES", "CONTROLS_ACCESS", "IMPOSES_CONDITION", "USES_MECHANISM", "APPLIES_TO", "INFLUENCES", "DEPENDS_ON", "ADMINISTERS"}
	case SteeringProfileCompetitiveLandscape:
		return []string{"PARTICIPATES_IN", "COMPETES_WITH", "PRODUCES"}
	case SteeringProfileSectorIndustry:
		return []string{"PARTICIPATES_IN", "HAS_MARKET_SEGMENT", "REGULATES", "COMPETES_WITH", "SUPPLIES_TO", "PRODUCES"}
	case SteeringProfileGeographicConcentration:
		return []string{"LOCATED_IN", "OPERATES_IN", "EXTRACTS_FROM"}
	case SteeringProfileInvestmentFinance:
		return taxonomy.GovernanceEdgeTypes
	default:
		return []string{}
	}
}

func (e *Engine) emit(ctx context.Context, job *models.ScoutingJob, contract Contract, eventType, title, description string, extra map[string]interface{}) {
	if e.publisher == nil || job == nil {
		return
	}
	payload := map[string]interface{}{
		"tenantId":      strings.TrimSpace(job.TenantID),
		"projectId":     strings.TrimSpace(job.ProjectID),
		"lensId":        strings.TrimSpace(job.LensID),
		"jobId":         strings.TrimSpace(job.ID),
		"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"contractType":  strings.TrimSpace(contract.ContractType),
		"humanReadable": map[string]interface{}{
			"title":       title,
			"description": description,
			"entityName":  strings.TrimSpace(contract.Title),
		},
	}
	for k, v := range extra {
		payload[k] = v
	}
	e.publisher.Publish(ctx, eventType, payload)
}

func hasConnectivityBetweenFacets(g graphSnapshot, aNodes, bNodes []*models.HorizonFragment, maxHops int) bool {
	if len(aNodes) == 0 || len(bNodes) == 0 {
		return false
	}
	targetSet := map[string]struct{}{}
	for _, n := range bNodes {
		if n != nil && strings.TrimSpace(n.ID) != "" {
			targetSet[n.ID] = struct{}{}
		}
	}
	for _, src := range aNodes {
		if src == nil || strings.TrimSpace(src.ID) == "" {
			continue
		}
		for targetID := range targetSet {
			nodes, _ := shortestPath(g, src.ID, targetID, maxHops)
			if len(nodes) > 0 {
				return true
			}
		}
	}
	return false
}

func shortestPath(g graphSnapshot, fromID, toID string, maxHops int) ([]string, []string) {
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	if fromID == "" || toID == "" {
		return nil, nil
	}
	if fromID == toID {
		return []string{fromID}, nil
	}
	if maxHops <= 0 {
		maxHops = 6
	}
	type state struct {
		NodeID  string
		Depth   int
		Path    []string
		EdgeIDs []string
	}
	adj := map[string][]*models.DependencyEdge{}
	for _, e := range g.Edges {
		if e == nil {
			continue
		}
		adj[e.SourceFragmentID] = append(adj[e.SourceFragmentID], e)
		adj[e.TargetFragmentID] = append(adj[e.TargetFragmentID], e) // undirected for explainability path search
	}
	q := []state{{NodeID: fromID, Depth: 0, Path: []string{fromID}, EdgeIDs: []string{}}}
	visitedDepth := map[string]int{fromID: 0}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if cur.Depth >= maxHops {
			continue
		}
		for _, e := range adj[cur.NodeID] {
			next := e.TargetFragmentID
			if next == cur.NodeID {
				next = e.SourceFragmentID
			}
			if next == "" {
				continue
			}
			nextDepth := cur.Depth + 1
			if prev, ok := visitedDepth[next]; ok && prev <= nextDepth {
				continue
			}
			visitedDepth[next] = nextDepth
			nextPath := append(append([]string{}, cur.Path...), next)
			nextEdges := append(append([]string{}, cur.EdgeIDs...), strings.TrimSpace(e.ID))
			if next == toID {
				return nextPath, nextEdges
			}
			q = append(q, state{NodeID: next, Depth: nextDepth, Path: nextPath, EdgeIDs: nextEdges})
		}
	}
	return nil, nil
}

func temporalCoverageRatio(g graphSnapshot, paths []models.ImpactPath, contract Contract) float64 {
	if len(paths) == 0 {
		return 1
	}
	total := 0
	valid := 0
	for _, p := range paths {
		for _, edgeID := range p.EdgeIDs {
			edge := edgeByID(g.Edges, edgeID)
			if edge == nil {
				continue
			}
			total++
			if edgeOverlapsHorizon(edge, contract) {
				valid++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(valid) / float64(total)
}

func edgeOverlapsHorizon(edge *models.DependencyEdge, contract Contract) bool {
	if edge == nil {
		return false
	}
	start := contract.TemporalStart
	end := contract.TemporalEnd
	if start == nil && end == nil {
		// No explicit horizon: temporal check is non-blocking.
		// Missing ValidFrom/ValidTo should not fail domain readiness in timeless contracts.
		return true
	}
	ef := edge.ValidFrom
	et := edge.ValidTo
	if ef == nil && et == nil {
		return false
	}
	if ef == nil {
		// Edge exists until et.
		if start != nil && et.Before(*start) {
			return false
		}
		return true
	}
	if et == nil {
		// Edge starts at ef and is open-ended.
		if end != nil && ef.After(*end) {
			return false
		}
		return true
	}
	if start != nil && et.Before(*start) {
		return false
	}
	if end != nil && ef.After(*end) {
		return false
	}
	return true
}

func edgeByID(edges []*models.DependencyEdge, edgeID string) *models.DependencyEdge {
	edgeID = strings.TrimSpace(edgeID)
	if edgeID == "" {
		return nil
	}
	for _, e := range edges {
		if e != nil && strings.TrimSpace(e.ID) == edgeID {
			return e
		}
	}
	return nil
}

// checkSupplyChainEdgesOperational uses only supply-chain operational edge types:
// PRODUCES, SUPPLIES_TO, transforms_into, assembles_into, USED_IN, EXTRACTS_FROM. No CONSUMES or DEPENDS_ON.
func checkSupplyChainEdgesOperational(g graphSnapshot, facetNodes map[string][]*models.HorizonFragment) models.DomainGateCheck {
	needed := map[DependencyType]struct{}{
		DependencyTypeSuppliesTo:     {},
		DependencyTypeProduces:       {},
		DependencyTypeTransformsInto: {},
		DependencyTypeAssemblesInto:  {},
		DependencyTypeUsedIn:         {},
		DependencyTypeExtractsFrom:   {},
	}
	perFacet := map[string]bool{}
	for facet := range facetNodes {
		perFacet[facet] = false
	}
	pathToFacet := map[string]string{}
	for facet, nodes := range facetNodes {
		for _, n := range nodes {
			if n != nil {
				pathToFacet[n.ID] = facet
			}
		}
	}
	for _, edge := range g.Edges {
		if edge == nil {
			continue
		}
		if _, ok := needed[edge.Relation]; !ok {
			continue
		}
		if facet, ok := pathToFacet[edge.SourceFragmentID]; ok {
			perFacet[facet] = true
		}
		if facet, ok := pathToFacet[edge.TargetFragmentID]; ok {
			perFacet[facet] = true
		}
	}
	missing := []string{}
	for facet, ok := range perFacet {
		if !ok {
			missing = append(missing, facet)
		}
	}
	sort.Strings(missing)
	return models.DomainGateCheck{
		CheckID:        "SC1_supply_chain_edges_exist",
		Status:         ternary(len(missing) == 0, "pass", "fail"),
		Priority:       "P0",
		GapDescription: fmt.Sprintf("missing_facets=%s", strings.Join(missing, ",")),
		Affected:       missing,
		EnrichmentHint: "Add SUPPLIES_TO/PRODUCES/transforms_into/assembles_into/USED_IN/EXTRACTS_FROM edges for missing facets",
	}
}

// checkSupplyChainBidirectional verifies anchor nodes have both incoming (SUPPLIES_TO as target)
// and outgoing (PRODUCES or SUPPLIES_TO as source) edges. Supply chain model: upstream = receives supplies, downstream = produces/supplies.
func checkSupplyChainBidirectional(g graphSnapshot, facetNodes map[string][]*models.HorizonFragment) models.DomainGateCheck {
	outgoingTypes := map[DependencyType]struct{}{
		DependencyTypeSuppliesTo: {},
		DependencyTypeProduces:   {},
	}
	incomingTypes := map[DependencyType]struct{}{
		DependencyTypeSuppliesTo: {},
	}
	failures := []string{}
	for _, nodes := range facetNodes {
		for _, n := range nodes {
			if n == nil || n.ID == "" {
				continue
			}
			hasUpstream := false
			hasDownstream := false
			for _, edge := range g.Edges {
				if edge == nil {
					continue
				}
				if edge.SourceFragmentID == n.ID {
					if _, ok := outgoingTypes[edge.Relation]; ok {
						hasDownstream = true
					}
				}
				if edge.TargetFragmentID == n.ID {
					if _, ok := incomingTypes[edge.Relation]; ok {
						hasUpstream = true
					}
				}
			}
			if !hasUpstream || !hasDownstream {
				failures = append(failures, strings.TrimSpace(n.CanonicalPath))
			}
		}
	}
	return models.DomainGateCheck{
		CheckID:        "SC2_upstream_downstream_coverage",
		Status:         ternary(len(failures) == 0, "pass", "fail"),
		Priority:       "P1",
		GapDescription: fmt.Sprintf("anchors_missing_bidirectional=%d", len(failures)),
		Affected:       failures,
		EnrichmentHint: "Find upstream (incoming SUPPLIES_TO) and downstream (outgoing PRODUCES/SUPPLIES_TO) links for anchor nodes",
	}
}

func checkConcentrationEvidence(snapshot *models.DomainAnswerSnapshot, g graphSnapshot) models.DomainGateCheck {
	if snapshot == nil {
		return models.DomainGateCheck{CheckID: "SC4_concentration_evidence", Status: "fail", Priority: "P1"}
	}
	for _, ev := range snapshot.ConcentrationEvidence {
		if ev.CR4 != nil || ev.HHI != nil {
			return models.DomainGateCheck{CheckID: "SC4_concentration_evidence", Status: "pass", Priority: "P1"}
		}
	}
	stats := computeMarketParticipationStats(g)
	if stats.MarketsWith3Orgs > 0 || stats.UniqueOrgParticipants >= 3 {
		return models.DomainGateCheck{
			CheckID:        "SC4_concentration_evidence",
			Status:         "partial",
			Priority:       "P1",
			GapDescription: fmt.Sprintf("CR4/HHI missing; proxy_org_participation markets_ge_3=%d unique_orgs=%d", stats.MarketsWith3Orgs, stats.UniqueOrgParticipants),
			Affected:       append([]string(nil), stats.MarketCanonicalPaths...),
			EnrichmentHint: "Collect CR4/HHI to replace participation proxy",
		}
	}
	return models.DomainGateCheck{
		CheckID:        "SC4_concentration_evidence",
		Status:         "fail",
		Priority:       "P1",
		GapDescription: "CR4/HHI missing",
		Affected:       append([]string(nil), stats.MarketCanonicalPaths...),
		EnrichmentHint: "Collect concentration metrics or proxy via market participation counts",
	}
}

func checkRegulatoryEdges(g graphSnapshot) models.DomainGateCheck {
	n := 0
	for _, edge := range g.Edges {
		if edge == nil {
			continue
		}
		if edge.Relation == DependencyTypeRegulates || edge.Relation == DependencyTypeControlsAccess || edge.Relation == DependencyTypeUsesMechanism {
			n++
		}
	}
	return models.DomainGateCheck{
		CheckID:        "RE1_regulatory_edges_exist",
		Status:         ternary(n > 0, "pass", "fail"),
		Priority:       "P0",
		GapDescription: fmt.Sprintf("regulatory_edges=%d", n),
		EnrichmentHint: "Find REGULATES/CONTROLS_ACCESS/USES_MECHANISM edges",
	}
}

func checkDecisionPoints(g graphSnapshot) models.DomainGateCheck {
	n := 0
	for _, f := range g.NodesByID {
		if f == nil {
			continue
		}
		if f.Class == FragmentTypeDecisionPoint || f.Class == FragmentTypeMechanism {
			n++
		}
	}
	return models.DomainGateCheck{
		CheckID:        "RE2_decision_points",
		Status:         ternary(n > 0, "pass", "fail"),
		Priority:       "P0",
		GapDescription: fmt.Sprintf("decision_or_mechanism_nodes=%d", n),
		EnrichmentHint: "Add decision points and mechanisms for regulatory exposure",
	}
}

func checkJurisdictionCoverage(facetNodes map[string][]*models.HorizonFragment) models.DomainGateCheck {
	failed := []string{}
	for facet, nodes := range facetNodes {
		ok := false
		for _, n := range nodes {
			if n == nil {
				continue
			}
			cp := strings.ToLower(strings.TrimSpace(n.CanonicalPath))
			if strings.HasPrefix(cp, "/gov/") || strings.Contains(cp, "/regulation/") {
				ok = true
				break
			}
		}
		if !ok {
			failed = append(failed, facet)
		}
	}
	return models.DomainGateCheck{
		CheckID:        "RE3_jurisdiction_coverage",
		Status:         ternary(len(failed) == 0, "pass", "fail"),
		Priority:       "P1",
		GapDescription: fmt.Sprintf("facets_without_jurisdiction=%s", strings.Join(failed, ",")),
		Affected:       failed,
		EnrichmentHint: "Add GOV/regulatory nodes by facet",
	}
}

func checkMechanismSpecificity(g graphSnapshot) models.DomainGateCheck {
	missing := []string{}
	for _, edge := range g.Edges {
		if edge == nil || edge.Relation != DependencyTypeRegulates {
			continue
		}
		mechanism := ""
		if edge.Qualifiers != nil {
			mechanism = strings.TrimSpace(fmt.Sprintf("%v", edge.Qualifiers["mechanism_type"]))
		}
		if mechanism == "" {
			missing = append(missing, strings.TrimSpace(edge.ID))
		}
	}
	return models.DomainGateCheck{
		CheckID:        "RE5_mechanism_specificity",
		Status:         ternary(len(missing) == 0, "pass", "fail"),
		Priority:       "P2",
		GapDescription: fmt.Sprintf("edges_without_mechanism_type=%d", len(missing)),
		Affected:       missing,
		EnrichmentHint: "Specify mechanism_type on REGULATES edges or add explicit data gap",
	}
}

func checkMarketParticipation(g graphSnapshot) models.DomainGateCheck {
	marketOK := false
	stats := computeMarketParticipationStats(g)
	if stats.MarketsWith3Orgs > 0 {
		marketOK = true
	}
	if marketOK {
		return models.DomainGateCheck{
			CheckID:        "CL1_market_participation",
			Status:         "pass",
			Priority:       "P0",
			GapDescription: fmt.Sprintf("markets_with_3_org_participants=%d", stats.MarketsWith3Orgs),
			Affected:       append([]string(nil), stats.MarketCanonicalPaths...),
			EnrichmentHint: "Add PARTICIPATES_IN edges from at least 3 organizations per primary market",
		}
	}
	if stats.UniqueOrgParticipants >= 3 {
		return models.DomainGateCheck{
			CheckID:        "CL1_market_participation",
			Status:         "partial",
			Priority:       "P1",
			GapDescription: fmt.Sprintf("strict_market_participation_missing; proxy_unique_org_participants=%d", stats.UniqueOrgParticipants),
			Affected:       append([]string(nil), stats.MarketCanonicalPaths...),
			EnrichmentHint: "Concentrate PARTICIPATES_IN edges into primary market node(s)",
		}
	}
	return models.DomainGateCheck{
		CheckID:        "CL1_market_participation",
		Status:         "fail",
		Priority:       "P0",
		GapDescription: "markets_with_3_org_participants=0",
		Affected:       append([]string(nil), stats.MarketCanonicalPaths...),
		EnrichmentHint: "Add PARTICIPATES_IN edges from at least 3 organizations per primary market",
	}
}

type marketParticipationStats struct {
	MarketsWith3Orgs      int
	UniqueOrgParticipants int
	MarketCanonicalPaths  []string
}

func computeMarketParticipationStats(g graphSnapshot) marketParticipationStats {
	countByMarket := map[string]map[string]struct{}{}
	uniqueOrgs := map[string]struct{}{}
	for _, edge := range g.Edges {
		if edge == nil || edge.Relation != DependencyTypeParticipatesIn {
			continue
		}
		src := g.NodesByID[edge.SourceFragmentID]
		if src == nil || src.Class != FragmentTypeOrganization {
			continue
		}
		targetID := strings.TrimSpace(edge.TargetFragmentID)
		if targetID == "" {
			continue
		}
		if _, ok := countByMarket[targetID]; !ok {
			countByMarket[targetID] = map[string]struct{}{}
		}
		sourceID := strings.TrimSpace(edge.SourceFragmentID)
		if sourceID == "" {
			continue
		}
		countByMarket[targetID][sourceID] = struct{}{}
		uniqueOrgs[sourceID] = struct{}{}
	}
	stats := marketParticipationStats{
		UniqueOrgParticipants: len(uniqueOrgs),
		MarketCanonicalPaths:  make([]string, 0, len(countByMarket)),
	}
	for targetID, participants := range countByMarket {
		if len(participants) >= 3 {
			stats.MarketsWith3Orgs++
		}
		if target := g.NodesByID[targetID]; target != nil {
			if cp := strings.TrimSpace(target.CanonicalPath); cp != "" {
				stats.MarketCanonicalPaths = append(stats.MarketCanonicalPaths, cp)
			}
		}
	}
	sort.Strings(stats.MarketCanonicalPaths)
	if len(stats.MarketCanonicalPaths) > 8 {
		stats.MarketCanonicalPaths = stats.MarketCanonicalPaths[:8]
	}
	return stats
}

func checkMarketShareData(g graphSnapshot) models.DomainGateCheck {
	withShare := 0
	for _, edge := range g.Edges {
		if edge == nil || edge.Relation != DependencyTypeParticipatesIn {
			continue
		}
		if edge.Qualifiers != nil {
			if _, ok := parseFloat(edge.Qualifiers["market_share"]); ok {
				withShare++
			}
		}
	}
	return models.DomainGateCheck{
		CheckID:        "CL2_market_share_data",
		Status:         ternary(withShare >= 4, "pass", "fail"),
		Priority:       "P1",
		GapDescription: fmt.Sprintf("participant_edges_with_market_share=%d", withShare),
		EnrichmentHint: "Collect market_share qualifiers for top competitors",
	}
}

func checkGeoEdges(g graphSnapshot, minCoverage float64) models.DomainGateCheck {
	if len(g.NodesByID) == 0 {
		return models.DomainGateCheck{CheckID: "GC1_geographic_edges", Status: "fail", Priority: "P0"}
	}
	withGeo := map[string]struct{}{}
	for _, edge := range g.Edges {
		if edge == nil {
			continue
		}
		if edge.Relation == DependencyTypeLocatedIn || edge.Relation == DependencyTypeOperatesIn {
			withGeo[edge.SourceFragmentID] = struct{}{}
			withGeo[edge.TargetFragmentID] = struct{}{}
		}
	}
	coverage := float64(len(withGeo)) / float64(len(g.NodesByID))
	return models.DomainGateCheck{
		CheckID:        "GC1_geographic_edges",
		Status:         ternary(coverage >= minCoverage, "pass", "fail"),
		Priority:       "P0",
		GapDescription: fmt.Sprintf("geo_coverage=%.3f threshold=%.3f", coverage, minCoverage),
		EnrichmentHint: "Add LOCATED_IN/OPERATES_IN edges for scoped entities",
	}
}

func checkGeoDiversity(g graphSnapshot) models.DomainGateCheck {
	regions := map[string]struct{}{}
	for _, f := range g.NodesByID {
		if f == nil {
			continue
		}
		cp := strings.ToLower(strings.TrimSpace(f.CanonicalPath))
		if strings.HasPrefix(cp, "/loc/") {
			parts := strings.Split(strings.Trim(cp, "/"), "/")
			if len(parts) >= 2 {
				regions[parts[1]] = struct{}{}
			}
		}
	}
	return models.DomainGateCheck{
		CheckID:        "GC2_geo_diversity",
		Status:         ternary(len(regions) >= 2, "pass", "fail"),
		Priority:       "P0",
		GapDescription: fmt.Sprintf("unique_geo_regions=%d", len(regions)),
		EnrichmentHint: "Add entities from at least two geographies",
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func parseFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case string:
		s := strings.TrimSpace(strings.TrimSuffix(t, "%"))
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func asJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func shortThesis(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 180 {
		return s
	}
	return s[:177] + "..."
}

func extractOriginalQuery(job *models.ScoutingJob) string {
	if job == nil || job.State == nil {
		return ""
	}
	if v, ok := job.State["originalQuery"].(string); ok {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(job.UserQuery)
}

type domainBudgetWarning struct {
	Title       string
	Description string
	UsedUSD     float64
	CapUSD      float64
	UsedTokens  int
	UsagePct    int
}

func buildDomainBudgetWarning(job *models.ScoutingJob, usedCycles, maxCycles int) *domainBudgetWarning {
	if job == nil || maxCycles <= 0 || usedCycles < maxCycles {
		return nil
	}
	usagePct := int(math.Round((float64(usedCycles) / float64(maxCycles)) * 100.0))
	if usagePct < 1 {
		usagePct = 1
	}
	usedUSD, _ := extractStateFloat(job.State,
		"usedUSD", "spendUSD", "spentUSD", "domainUsedUSD",
	)
	capUSD, _ := extractStateFloat(job.State,
		"capUSD", "budgetCapUSD", "domainBudgetCapUSD", "budgetUSD",
	)
	usedTokens, _ := extractStateInt(job.State,
		"usedTokens", "usageTokens", "outputTokens", "totalTokens", "domainUsedTokens",
	)

	description := fmt.Sprintf("Enrichment cycle budget is near cap (%d/%d cycles).", usedCycles, maxCycles)
	if capUSD > 0 && usedUSD > 0 {
		description = fmt.Sprintf("Enrichment budget is near cap (%.2f / %.2f USD).", usedUSD, capUSD)
	}
	return &domainBudgetWarning{
		Title:       fmt.Sprintf("Budget warning: %d%% used", usagePct),
		Description: description,
		UsedUSD:     usedUSD,
		CapUSD:      capUSD,
		UsedTokens:  usedTokens,
		UsagePct:    usagePct,
	}
}

func extractStateFloat(state map[string]interface{}, keys ...string) (float64, bool) {
	if len(state) == 0 || len(keys) == 0 {
		return 0, false
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if raw, ok := state[key]; ok {
			if v, ok2 := parseFloat(raw); ok2 {
				return v, true
			}
		}
	}
	if rawBudget, ok := state["budget"].(map[string]interface{}); ok && len(rawBudget) > 0 {
		for _, key := range keys {
			if key == "" {
				continue
			}
			if raw, ok := rawBudget[key]; ok {
				if v, ok2 := parseFloat(raw); ok2 {
					return v, true
				}
			}
		}
	}
	return 0, false
}

func extractStateInt(state map[string]interface{}, keys ...string) (int, bool) {
	if v, ok := extractStateFloat(state, keys...); ok {
		return int(math.Round(v)), true
	}
	return 0, false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return 0
		}
		if i, err := strconv.Atoi(t); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int(f)
		}
		return 0
	default:
		return 0
	}
}
