package scout

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
)

func TestEnrichScoutModelInputPropagatesParentSession(t *testing.T) {
	parent := &contracts.MeshTask{
		ID:         "plan-root",
		SessionRef: "vertex-session-shared-1",
	}
	in := map[string]any{"request": "x"}
	out := enrichScoutModelInput(in, parent)
	if got := stringValue(out, "parentSessionRef"); got != "vertex-session-shared-1" {
		t.Fatalf("parentSessionRef: got %q want vertex-session-shared-1", got)
	}
}

func TestEnrichScoutModelInputPreservesExplicitParentSessionWhenTaskEmpty(t *testing.T) {
	in := map[string]any{"parentSessionRef": "from-input-only"}
	out := enrichScoutModelInput(in, &contracts.MeshTask{ID: "x"})
	if got := stringValue(out, "parentSessionRef"); got != "from-input-only" {
		t.Fatalf("got %q", got)
	}
}

func TestChooseNextActionStrongSupportedBranchDoesNotForceMoreResearch(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 5,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	state.ResearchResults = []ResearchResult{{BranchID: "b1", Summary: "ok", Confidence: 0.9}}
	b := state.Branches["b1"]
	b.Status = BranchStatusSupported
	b.LastConfidence = 0.9
	b.ResearchRounds = 1
	state.Hypotheses["b1"].Status = HypothesisSupported

	action, branchID := chooseNextAction(state)
	if action == "research" || branchID != "" {
		t.Fatalf("expected non-research next action with no branch id, got %s %q", action, branchID)
	}
}

func TestChooseNextActionReplanRunsBeforeResearch(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	state.NeedsReplan = true
	if action, _ := chooseNextAction(state); action != "replan" {
		t.Fatalf("expected replan, got %s", action)
	}
}

func TestChooseNextActionBoundedStop(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	state.MaxTurns = 2
	state.Turn = 2
	if action, _ := chooseNextAction(state); action != "stop" {
		t.Fatalf("expected stop when Turn >= MaxTurns, got %s", action)
	}
	if state.StopReason != "bounded_iteration_stop" {
		t.Fatalf("StopReason: got %q", state.StopReason)
	}
}

func TestCriticRiskyClaimsBlockOnlyTargetBranches(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "a"},
			{ID: "b2", Question: "b"},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b2"].Status = BranchStatusSupported
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b2"].Status = HypothesisSupported

	applyCriticToState(state, CriticResult{
		RiskyClaims:         []string{"weak upstream"},
		RiskTargetBranchIDs: []string{"b1"},
	})

	if state.Branches["b1"].Status != BranchStatusBlocked {
		t.Fatalf("b1 should be blocked, got %s", state.Branches["b1"].Status)
	}
	if state.Branches["b2"].Status != BranchStatusSupported {
		t.Fatalf("b2 should stay supported, got %s", state.Branches["b2"].Status)
	}
	if !state.Hypotheses["b2"].MaterializationReady {
		t.Fatalf("b2 should remain materialization-ready when not targeted by risk")
	}
}

func TestPathStateUpdateOnlyAffectsScopedBranches(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "a"},
			{ID: "b2", Question: "b"},
		},
	})
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b2"].Status = HypothesisSupported

	applyPathToState(state, PathResult{
		ContinuityStatus:        "partial",
		SourceBranchIDs:         []string{"b1"},
		MaterializedArtifactIDs: []string{},
	})

	if state.Hypotheses["b1"].Status != HypothesisWeaklySupported {
		t.Fatalf("b1 hypothesis: got %s want weakly_supported", state.Hypotheses["b1"].Status)
	}
	if state.Hypotheses["b2"].Status != HypothesisSupported {
		t.Fatalf("b2 should be untouched, got %s", state.Hypotheses["b2"].Status)
	}
}

func TestMaterializeMarksOnlyLandedBranchesWhenSpecified(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "a"},
			{ID: "b2", Question: "b"},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b2"].Status = BranchStatusSupported

	applyMaterializeToState(state, MaterializeResult{
		Persisted:       true,
		LandedBranchIDs: []string{"b1"},
		Created:         []string{"art-1"},
	})

	if state.Branches["b1"].Status != BranchStatusDone {
		t.Fatalf("b1: got %s want done", state.Branches["b1"].Status)
	}
	if state.Branches["b2"].Status != BranchStatusSupported {
		t.Fatalf("b2 should stay supported, got %s", state.Branches["b2"].Status)
	}
}

func TestMaterializeRejectedBranchIDsDowngradesBranch(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "a"},
			{ID: "b2", Question: "b"},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b2"].Status = BranchStatusSupported
	state.Hypotheses["b2"].MaterializationReady = true

	applyMaterializeToState(state, MaterializeResult{
		Persisted:         true,
		LandedBranchIDs:   []string{"b1"},
		RejectedBranchIDs: []string{"b2"},
		Created:           []string{"art-1"},
	})

	if state.Branches["b2"].Status != BranchStatusCandidate {
		t.Fatalf("b2: got %s want candidate_needs_review", state.Branches["b2"].Status)
	}
	if state.Hypotheses["b2"].MaterializationReady {
		t.Fatalf("b2 should not be materialization-ready after rejection")
	}
}

func TestMergeCriticResultParsesRiskTargetBranchIds(t *testing.T) {
	base := CriticResult{}
	parsed := map[string]any{"riskTargetBranchIds": []any{"x", "y"}}
	got := mergeCriticDelta(base, decodeCriticDelta(parsed))
	if len(got.RiskTargetBranchIDs) != 2 || got.RiskTargetBranchIDs[0] != "x" {
		t.Fatalf("got %#v", got.RiskTargetBranchIDs)
	}
}

func TestSelectPathScopeChoosesNarrowTargetSet(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "trace cathode chain", ProductHints: []string{"/product/li_ion/cathode"}, RequiresPath: true},
			{ID: "b2", Question: "ownership side branch", ProductHints: []string{"/product/ownership/holding"}},
			{ID: "b3", Question: "downstream assembly", ProductHints: []string{"/product/li_ion/pack"}, RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b3"].Status = BranchStatusSupported
	state.Branches["b2"].Status = BranchStatusActive
	state.Hypotheses["b1"].MaterializationReady = true
	state.ResearchResults = []ResearchResult{
		{BranchID: "b1", ProductRefs: []string{"/product/li_ion/cathode"}, RecommendedPathProducts: []string{"/product/li_ion/cathode"}},
		{BranchID: "b2", ProductRefs: []string{"/product/ownership/holding"}},
		{BranchID: "b3", ProductRefs: []string{"/product/li_ion/pack"}, RecommendedPathProducts: []string{"/product/li_ion/pack"}},
	}
	state.LatestCritic = CriticResult{PathProducts: []string{"/product/li_ion/cathode"}}

	scope := selectPathScope(state)
	if len(scope.TargetBranchIDs) == 0 {
		t.Fatalf("expected non-empty target path scope")
	}
	if !stringSliceContains(scope.TargetBranchIDs, "b1") {
		t.Fatalf("expected b1 in target scope, got %v", scope.TargetBranchIDs)
	}
	if stringSliceContains(scope.TargetBranchIDs, "b2") {
		t.Fatalf("unexpected broad scope: b2 included in %v", scope.TargetBranchIDs)
	}
}

func TestMaterializeFallbackIsConservativeWhenLandedUnknown(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "a", RequiresPath: true},
			{ID: "b2", Question: "b", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b2"].Status = BranchStatusSupported
	state.Hypotheses["b1"].MaterializationReady = true
	state.Hypotheses["b2"].MaterializationReady = true
	state.PathResult = PathResult{
		SourceBranchIDs:         []string{"b1", "b2"},
		MaterializedArtifactIDs: []string{"art-1"},
		ContinuityStatus:        "partial",
	}

	applyMaterializeToState(state, MaterializeResult{
		Persisted: true,
		Created:   []string{"art-1"},
	})

	if state.Branches["b1"].Status == BranchStatusDone || state.Branches["b2"].Status == BranchStatusDone {
		t.Fatalf("uncertain fallback should not finish branches, got b1=%s b2=%s", state.Branches["b1"].Status, state.Branches["b2"].Status)
	}
	if !state.Hypotheses["b1"].MaterializationReady || !state.Hypotheses["b2"].MaterializationReady {
		t.Fatalf("uncertain fallback should preserve readiness; got b1=%v b2=%v", state.Hypotheses["b1"].MaterializationReady, state.Hypotheses["b2"].MaterializationReady)
	}
}

func TestCriticUnknownRiskTargetsHoldsMaterializationWithoutFalseContradiction(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "battery upstream contracts"},
			{ID: "b2", Question: "regulatory exposure sanctions"},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b2"].Status = BranchStatusSupported
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b2"].Status = HypothesisSupported

	applyCriticToState(state, CriticResult{
		RiskyClaims: []string{"counterparty claim cannot be verified"},
	})

	if state.Branches["b1"].Status == BranchStatusBlocked || state.Branches["b2"].Status == BranchStatusBlocked {
		t.Fatalf("unknown risk targets should not arbitrarily block branches: b1=%s b2=%s", state.Branches["b1"].Status, state.Branches["b2"].Status)
	}
	if state.Hypotheses["b1"].Status == HypothesisContradicted || state.Hypotheses["b2"].Status == HypothesisContradicted {
		t.Fatalf("unknown risk targets should not arbitrarily contradict branches")
	}
	if state.Hypotheses["b1"].MaterializationReady || state.Hypotheses["b2"].MaterializationReady {
		t.Fatalf("unknown risk targets should still hold materialization readiness")
	}
}

func TestChooseNextActionPrioritizesPathWhenStrongScopedBranchReady(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "trace cathode", RequiresPath: true, ProductHints: []string{"/product/li_ion/cathode"}},
			{ID: "b2", Question: "ownership", ProductHints: []string{"/product/ownership/holding"}},
		},
	})
	state.Branches["b2"].Status = BranchStatusDone
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].LastConfidence = 0.9
	state.Branches["b1"].ResearchRounds = 1
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b1"].MaterializationReady = true
	state.ResearchResults = []ResearchResult{
		{BranchID: "b1", Confidence: 0.9, RecommendedPathProducts: []string{"/product/li_ion/cathode"}},
	}
	state.LatestCritic = CriticResult{
		MaterializePath: true,
		PathProducts:    []string{"/product/li_ion/cathode"},
	}

	action, _ := chooseNextAction(state)
	if action != "path" {
		t.Fatalf("expected path priority for strong path-relevant branch, got %s", action)
	}
}

func TestBuildActiveFrontierSnapshotIsNarrowerThanDurableState(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "q1", ExpectedInfoGain: 0.9},
			{ID: "b2", Question: "q2", ExpectedInfoGain: 0.7},
			{ID: "b3", Question: "q3", ExpectedInfoGain: 0.5},
			{ID: "b4", Question: "q4", ExpectedInfoGain: 0.2},
		},
	})
	state.OpenQuestions = []string{"oq1", "oq2", "oq3", "oq4", "oq5", "oq6"}
	for _, id := range []string{"b1", "b2", "b3", "b4"} {
		state.Branches[id].Status = BranchStatusActive
		state.Branches[id].LastConfidence = 0.2
		state.ResearchResults = append(state.ResearchResults, ResearchResult{BranchID: id, Confidence: 0.2})
	}

	snapshot := buildActiveFrontierSnapshot(state)
	if len(snapshot.TopBranches) == 0 {
		t.Fatalf("expected non-empty frontier")
	}
	if len(snapshot.TopBranches) >= len(state.Branches) {
		t.Fatalf("frontier must be narrower than durable state: top=%d durable=%d", len(snapshot.TopBranches), len(state.Branches))
	}
	if len(snapshot.OpenQuestions) > 4 {
		t.Fatalf("open questions must be compacted, got %d", len(snapshot.OpenQuestions))
	}
}

func TestCompactStateForTurnCompactsCompletedBranches(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	state.Turn = 4
	state.LastCompactedTurn = 0
	state.Branches["b1"].Status = BranchStatusDone
	state.Branches["b1"].OpenQuestions = []string{"old1", "old2"}
	state.Branches["b1"].History = []BranchHistoryEntry{
		{Turn: 1, Action: "research", Note: "n1"},
		{Turn: 2, Action: "research", Note: "n2"},
	}
	state.Hypotheses["b1"].Evidence = []string{"e1", "e2", "e3", "e4"}
	state.OpenQuestions = []string{"dup same question", "dup same question", "unique"}

	compactStateForTurn(state, "materialize")

	if len(state.Branches["b1"].History) != 1 {
		t.Fatalf("expected compacted history length=1, got %d", len(state.Branches["b1"].History))
	}
	if len(state.Branches["b1"].OpenQuestions) != 0 {
		t.Fatalf("expected open questions cleared for completed branch")
	}
	if len(state.Hypotheses["b1"].Evidence) > 2 {
		t.Fatalf("expected compact evidence <=2, got %d", len(state.Hypotheses["b1"].Evidence))
	}
	if len(state.OpenQuestions) >= 3 {
		t.Fatalf("expected deduped global questions, got %v", state.OpenQuestions)
	}
}

func TestChooseNextActionStopsOnNoHighValueAction(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	state.Branches["b1"].Status = BranchStatusDone
	state.Hypotheses["b1"].Status = HypothesisConfirmedMaterializable
	state.PathAttempts = 2
	state.MaterializeAttempts = 2

	action, _ := chooseNextAction(state)
	if action != "stop" {
		t.Fatalf("expected stop, got %s", action)
	}
	if state.StopReason == "" {
		t.Fatalf("expected stop reason to be set")
	}
}

func TestCompactPlanningContextPreservesRealPlanningSubstrate(t *testing.T) {
	input := map[string]any{
		"query":               "trace target",
		"targetCanonicalPath": "/product/a",
		"targetFragment":      map[string]any{"id": "f1"},
		"outgoingEdges":       []map[string]any{{"id": "e1"}},
		"incomingEdges":       []map[string]any{{"id": "e2"}},
	}
	got := compactPlanningContext(input)
	if got["query"] == nil || got["targetFragment"] == nil {
		t.Fatalf("expected compact planning context to retain real substrate, got %#v", got)
	}
	if _, ok := got["outgoingEdges"]; !ok {
		t.Fatalf("expected outgoingEdges in compact context")
	}
}

func TestProcessPlanUsesNarrowPayloadsAndExpectedActionSequence(t *testing.T) {
	p := &Processor{}
	var childCaps []string
	var criticInput, aggregateInput, researchInput map[string]any
	p.runModelObjectFn = func(_ context.Context, _ *agentmeshruntime.Service, task *contracts.MeshTask, input map[string]any) map[string]any {
		if task != nil && task.Capability == "scout.plan" {
			return map[string]any{
				"goal": "trace supply path",
				"missionRole": map[string]any{
					"domain":               "battery_materials_trade_intelligence",
					"primaryRole":          "top-grade battery materials corridor strategist",
					"investigationPosture": "map corridors before narrowing",
					"roleRationale":        "planner must return an explicit role",
				},
				"focusProducts":     []any{"/product/li_ion/cathode"},
				"maxResearchRounds": 2,
				"branches": []any{
					map[string]any{
						"id":           "b1",
						"question":     "trace cathode upstream",
						"requiresPath": true,
						"productHints": []any{"/product/li_ion/cathode"},
					},
				},
			}
		}
		return nil
	}
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, input map[string]any) (*contracts.MeshTask, map[string]any, error) {
		childCaps = append(childCaps, capability)
		switch capability {
		case "scout.decide":
			items, _ := input["candidateActions"].([]any)
			chosenAction := "stop"
			chosenBranch := ""
			priorities := []string{"bridge", "path", "materialize", "critic", "research", "stop"}
			for _, want := range priorities {
				for _, item := range items {
					object := mapValue(item)
					if !boolValue(object, "available", false) {
						continue
					}
					if stringValue(object, "action") != want {
						continue
					}
					chosenAction = want
					chosenBranch = stringValue(object, "branchId")
					break
				}
				if chosenAction != "stop" || want == "stop" {
					break
				}
			}
			return &contracts.MeshTask{ID: "d1", Capability: capability}, map[string]any{
				"stateSummary": "select highest-priority available candidate in test",
				"chosenAction": chosenAction,
				"branchId":     chosenBranch,
				"whyChosen":    "test decider selected highest-priority available candidate",
			}, nil
		case "scout.research":
			researchInput = input
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				applyResearchToState(sessionState, "b1", ResearchResult{
					BranchID:       "b1",
					Confidence:     0.92,
					ProductRefs:    []string{"/product/li_ion/cathode"},
					OpenQuestions:  []string{},
					CanonicalRefs:  []string{"/product/li_ion/cathode", "/product/li_ion/precursor", "/product/li_ion/electrolyte"},
					EvidencePoints: []string{"source-a", "source-b", "source-c"},
				})
			}
			return &contracts.MeshTask{ID: "r1", Capability: capability}, map[string]any{
				"branchId":       "b1",
				"confidence":     0.92,
				"canonicalRefs":  []any{"/product/li_ion/cathode", "/product/li_ion/precursor", "/product/li_ion/electrolyte"},
				"evidencePoints": []any{"source-a", "source-b", "source-c"},
				"productRefs":    []any{"/product/li_ion/cathode"},
				"openQuestions":  []any{},
				"proposedGraphOps": []any{
					map[string]any{"kind": "create_edge", "subjectPath": "/product/li_ion/cathode", "objectPath": "/product/li_ion/precursor", "edgeType": "TRANSFORMS_INTO", "rationale": "cathode from precursor"},
				},
				"sessionState": input["sessionState"],
			}, nil
		case "scout.critic":
			criticInput = input
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				critic := CriticResult{
					AcceptedFindings: []string{"supported"},
					MaterializePath:  true,
					PathProducts:     []string{"/product/li_ion/cathode"},
					GraphOpVerdicts: []GraphOpVerdict{
						{OpIndex: 0, Verdict: "accepted", Reason: "grounded"},
					},
				}
				applyCriticToState(sessionState, critic)
				applyGraphOpVerdicts(sessionState, critic.GraphOpVerdicts)
			}
			return &contracts.MeshTask{ID: "c1", Capability: capability}, map[string]any{
				"acceptedFindings":    []any{"supported"},
				"materializePath":     true,
				"pathProducts":        []any{"/product/li_ion/cathode"},
				"riskTargetBranchIds": []any{},
				"graphOpVerdicts": []any{
					map[string]any{"opIndex": 0, "verdict": "accepted", "reason": "grounded"},
				},
				"sessionState": input["sessionState"],
			}, nil
		case "scout.path":
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				path := PathResult{
					QueriedProducts:        []string{"/product/li_ion/cathode"},
					MaterializedArtifactIDs: []string{"art-1"},
					ContinuityStatus:       "landed",
					SourceBranchIDs:        []string{"b1"},
				}
				applyPathToState(sessionState, path)
				markVerifiedOpsFromPath(sessionState, path)
			}
			return &contracts.MeshTask{ID: "p1", Capability: capability}, map[string]any{
				"queriedProducts":         []any{"/product/li_ion/cathode"},
				"materializedArtifactIds": []any{"art-1"},
				"continuityStatus":        "landed",
				"sourceBranchIds":         []any{"b1"},
				"sessionState":            input["sessionState"],
			}, nil
		case "scout.materialize":
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				mat := MaterializeResult{
					Summary:         "landed",
					Persisted:       true,
					ArtifactIDs:     []string{"art-1"},
					Created:         []string{"art-1"},
					LandedBranchIDs: []string{"b1"},
				}
				applyMaterializeToState(sessionState, mat)
				markLandedOps(sessionState, mat)
			}
			return &contracts.MeshTask{ID: "m1", Capability: capability}, map[string]any{
				"summary":         "landed",
				"persisted":       true,
				"artifactIds":     []any{"art-1"},
				"created":         []any{"art-1"},
				"landedBranchIds": []any{"b1"},
				"sessionState":    input["sessionState"],
			}, nil
		case "scout.aggregate":
			aggregateInput = input
			return &contracts.MeshTask{ID: "a1", Capability: capability}, map[string]any{
				"summary": "done",
			}, nil
		case "scout.review":
			return &contracts.MeshTask{ID: "v1", Capability: capability}, map[string]any{
				"accepted":  true,
				"rationale": "ok",
				"quality":   "good",
			}, nil
		default:
			return &contracts.MeshTask{ID: "x1", Capability: capability}, map[string]any{}, nil
		}
	}

	task := &contracts.MeshTask{
		ID:         "plan1",
		Capability: "scout.plan",
		TenantID:   "t1",
		Input: map[string]any{
			"tenantId":            "t1",
			"query":               "trace cathode upstream",
			"targetCanonicalPath": "/product/li_ion/cathode",
			"allowWrites":         true,
		},
	}

	result, err := p.processPlan(context.Background(), nil, task, "lease")
	if err != nil {
		t.Fatalf("processPlan err: %v", err)
	}
	if result == nil || result.Status != contracts.TaskStatusSucceeded {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(childCaps) < 6 {
		t.Fatalf("expected full orchestration sequence, got %v", childCaps)
	}
	// Auto-paired critic must follow the first research.
	if countCapability(childCaps, "scout.critic") < 1 {
		t.Fatalf("expected at least one critic (auto-paired after research), got %v", childCaps)
	}
	if childCaps[len(childCaps)-2] != "scout.aggregate" || childCaps[len(childCaps)-1] != "scout.review" {
		t.Fatalf("expected aggregate/review tail, got %v", childCaps)
	}
	if countCapability(childCaps, "scout.research") < 1 {
		t.Fatalf("expected at least one research turn, got %v", childCaps)
	}
	if researchInput["activeFrontier"] == nil {
		t.Fatalf("research should receive activeFrontier")
	}
	researchSession, ok := researchInput["sessionState"].(*InvestigationState)
	if !ok || researchSession == nil || researchSession.Branches["b1"] == nil {
		t.Fatalf("research should receive authoritative full sessionState with branches, got %#v", researchInput["sessionState"])
	}
	rc := mapValue(researchInput["planningContext"])
	if rc["query"] == nil {
		t.Fatalf("research planningContext lost query: %#v", rc)
	}
	if _, ok := criticInput["researchDeltas"]; !ok {
		t.Fatalf("critic should receive researchDeltas")
	}
	if _, ok := criticInput["research"]; ok {
		t.Fatalf("critic should not receive full research payload")
	}
	if _, ok := aggregateInput["researchDeltas"]; !ok {
		t.Fatalf("aggregate should receive researchDeltas")
	}
	if _, ok := aggregateInput["research"]; ok {
		t.Fatalf("aggregate should not receive full research payload")
	}
	out := mapValue(result.Output)
	if _, ok := out["reasoningTrace"]; !ok {
		t.Fatalf("expected reasoningTrace in final output")
	}
	if _, ok := out["reasoningSummary"]; !ok {
		t.Fatalf("expected reasoningSummary in final output")
	}
}

func TestDecideNextActionFallsBackWhenModelFails(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{Query: "trace cathode"}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "upstream trace", RequiresPath: true}},
	})
	p := &Processor{}
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, _ map[string]any) (*contracts.MeshTask, map[string]any, error) {
		if capability == "scout.decide" {
			return nil, nil, fmt.Errorf("vertex unavailable")
		}
		return &contracts.MeshTask{ID: "x"}, map[string]any{}, nil
	}
	task := &contracts.MeshTask{ID: "plan1", Capability: "scout.plan"}
	child, decision, action, _, err := p.decideNextAction(context.Background(), nil, task, state.Request, nil, state)
	if err != nil {
		t.Fatalf("decideNextAction should not propagate decide failure: %v", err)
	}
	if child != nil {
		t.Fatalf("expected nil child on failure, got %v", child)
	}
	if decision.ValidationStatus != "fallback" {
		t.Fatalf("expected fallback validation, got %q", decision.ValidationStatus)
	}
	if action == "" {
		t.Fatalf("expected non-empty fallback action")
	}
	foundDiag := false
	for _, d := range state.RuntimeDiagnosis {
		if d.Code == "decision_task_failed" {
			foundDiag = true
		}
	}
	if !foundDiag {
		t.Fatalf("expected decision_task_failed diagnosis")
	}
}

func TestDecideNextActionUsesModelDecisionWhenValid(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{Query: "trace cathode"}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "upstream trace", RequiresPath: true},
			{ID: "b2", Question: "downstream assembly"},
		},
	})
	p := &Processor{}
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, input map[string]any) (*contracts.MeshTask, map[string]any, error) {
		if capability == "scout.decide" {
			return &contracts.MeshTask{ID: "d1", Capability: capability}, map[string]any{
				"stateSummary": "model selected b2 for research based on expected info gain",
				"chosenAction": "research",
				"branchId":     "b2",
				"whyChosen":    "b2 has higher expected information gain for downstream assembly mapping",
				"whyNotStop":   "investigation has not yet produced any research results",
				"confidence":   0.85,
			}, nil
		}
		return &contracts.MeshTask{ID: "x"}, map[string]any{}, nil
	}
	task := &contracts.MeshTask{ID: "plan1", Capability: "scout.plan"}
	child, decision, action, branchID, err := p.decideNextAction(context.Background(), nil, task, state.Request, nil, state)
	if err != nil {
		t.Fatalf("decideNextAction err: %v", err)
	}
	if child == nil || child.ID != "d1" {
		t.Fatalf("expected child task from model, got %v", child)
	}
	if decision.ValidationStatus != "accepted" {
		t.Fatalf("expected accepted validation, got %q (%s)", decision.ValidationStatus, decision.ValidationReason)
	}
	if action != "research" || branchID != "b2" {
		t.Fatalf("expected model's choice (research/b2), got %s/%s", action, branchID)
	}
	if decision.WhyChosen == "" {
		t.Fatalf("expected model-provided reasoning in WhyChosen")
	}
}

func TestDecideNextActionRejectsInfeasibleModelChoice(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{Query: "trace cathode"}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "upstream trace"}},
	})
	state.Branches["b1"].Status = BranchStatusDone
	p := &Processor{}
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, _ map[string]any) (*contracts.MeshTask, map[string]any, error) {
		if capability == "scout.decide" {
			return &contracts.MeshTask{ID: "d1", Capability: capability}, map[string]any{
				"chosenAction": "research",
				"branchId":     "b1",
				"whyChosen":    "model hallucinated that b1 still needs work",
			}, nil
		}
		return &contracts.MeshTask{ID: "x"}, map[string]any{}, nil
	}
	task := &contracts.MeshTask{ID: "plan1", Capability: "scout.plan"}
	child, decision, _, _, err := p.decideNextAction(context.Background(), nil, task, state.Request, nil, state)
	if err != nil {
		t.Fatalf("decideNextAction err: %v", err)
	}
	if child == nil {
		t.Fatalf("expected child task even on fallback")
	}
	if decision.ValidationStatus != "fallback" {
		t.Fatalf("expected fallback when model chooses infeasible action, got %q", decision.ValidationStatus)
	}
	foundDiag := false
	for _, d := range state.RuntimeDiagnosis {
		if d.Code == "decision_fallback" {
			foundDiag = true
		}
	}
	if !foundDiag {
		t.Fatalf("expected decision_fallback diagnosis for infeasible model choice")
	}
}

func countCapability(items []string, want string) int {
	n := 0
	for _, item := range items {
		if item == want {
			n++
		}
	}
	return n
}

func filterOutCapability(items []string, exclude string) []string {
	var out []string
	for _, item := range items {
		if item != exclude {
			out = append(out, item)
		}
	}
	return out
}

func TestProcessPlanRunsBridgeWhenPathGapNeedsMissingMiddle(t *testing.T) {
	p := &Processor{}
	var childCaps []string
	pathCalls := 0
	p.runModelObjectFn = func(_ context.Context, _ *agentmeshruntime.Service, task *contracts.MeshTask, input map[string]any) map[string]any {
		if task != nil && task.Capability == "scout.plan" {
			return map[string]any{
				"goal": "trace lithium route",
				"missionRole": map[string]any{
					"domain":               "battery_materials_trade_intelligence",
					"primaryRole":          "Supply Chain Graph Architect",
					"investigationPosture": "map candidate corridors before committing to a route",
					"roleRationale":        "This task needs route-structure inference, not generic research.",
				},
				"focusProducts":     []any{"/product/component/chemical/refining/lithium-carbonate"},
				"maxResearchRounds": 2,
				"branches": []any{
					map[string]any{
						"id":           "b1",
						"question":     "Trace Chile carbonate into Germany",
						"requiresPath": true,
						"productHints": []any{"/product/component/chemical/refining/lithium-carbonate"},
						"geoHints":     []any{"Chile", "Germany"},
					},
				},
			}
		}
		return nil
	}
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, input map[string]any) (*contracts.MeshTask, map[string]any, error) {
		childCaps = append(childCaps, capability)
		switch capability {
		case "scout.decide":
			items, _ := input["candidateActions"].([]any)
			chosenAction := "stop"
			chosenBranch := ""
			priorities := []string{"bridge", "path", "materialize", "critic", "research", "stop"}
			for _, want := range priorities {
				for _, item := range items {
					object := mapValue(item)
					if !boolValue(object, "available", false) {
						continue
					}
					if stringValue(object, "action") != want {
						continue
					}
					chosenAction = want
					chosenBranch = stringValue(object, "branchId")
					break
				}
				if chosenAction != "stop" || want == "stop" {
					break
				}
			}
			return &contracts.MeshTask{ID: "d1", Capability: capability}, map[string]any{
				"stateSummary": "select highest-priority available candidate in test",
				"chosenAction": chosenAction,
				"branchId":     chosenBranch,
				"whyChosen":    "test decider selected highest-priority available candidate",
			}, nil
		case "scout.research":
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				applyResearchToState(sessionState, "b1", ResearchResult{
					BranchID:      "b1",
					Confidence:    0.86,
					ProductRefs:   []string{"/product/component/chemical/refining/lithium-carbonate"},
					OpenQuestions: []string{"What sits between Chilean carbonate export and German refining?"},
				})
			}
			return &contracts.MeshTask{ID: "r1", Capability: capability}, map[string]any{
				"branchId":      "b1",
				"summary":       "SQM exports lithium carbonate from Chile but Germany binding remains unclear.",
				"confidence":    0.86,
				"canonicalRefs": []any{"/org/sqm", "/product/component/chemical/refining/lithium-carbonate"},
				"productRefs":   []any{"/product/component/chemical/refining/lithium-carbonate"},
				"openQuestions": []any{"What sits between Chilean carbonate export and German refining?"},
				"proposedGraphOps": []any{
					map[string]any{"kind": "create_edge", "subjectPath": "/org/sqm", "objectPath": "/product/component/chemical/refining/lithium-carbonate", "edgeType": "PRODUCES", "rationale": "SQM produces lithium carbonate"},
				},
				"sessionState": input["sessionState"],
			}, nil
		case "scout.critic":
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				critic := CriticResult{
					AcceptedFindings: []string{"Chilean carbonate substrate is locally strong."},
					OpenQuestions:    []string{"What converter or port chain connects Chilean carbonate to Germany if the deterministic path breaks?"},
					PathProducts:     []string{"/product/component/chemical/refining/lithium-carbonate"},
					GraphOpVerdicts: []GraphOpVerdict{
						{OpIndex: 0, Verdict: "accepted_target", Reason: "Local substrate is strong enough for path verification."},
					},
				}
				applyCriticToState(sessionState, critic)
				applyGraphOpVerdicts(sessionState, critic.GraphOpVerdicts)
			}
			return &contracts.MeshTask{ID: "c1", Capability: capability}, map[string]any{
				"acceptedFindings": []any{"Chilean carbonate substrate is locally strong."},
				"riskyClaims":      []any{},
				"openQuestions":    []any{"What converter or port chain connects Chilean carbonate to Germany if the deterministic path breaks?"},
				"pathProducts":     []any{"/product/component/chemical/refining/lithium-carbonate"},
				"graphOpVerdicts": []any{
					map[string]any{"opIndex": 0, "verdict": "accepted_target", "reason": "Local substrate is strong enough for path verification."},
				},
				"branchAssessments": []any{
					map[string]any{"branchId": "b1", "targetRelevance": 0.78, "pursuitDecision": "press", "pursuitReason": "Attempt deterministic path first, then bridge the missing middle if needed.", "routeHypothesis": "Chile carbonate likely reaches Germany through an intermediate conversion/logistics layer."},
				},
				"sessionState": input["sessionState"],
			}, nil
		case "scout.bridge":
			ctx := mapValue(input["bridgeContext"])
			if ctx["endpointA"] == nil || ctx["endpointB"] == nil {
				t.Fatalf("bridge should receive explicit endpoints, got %#v", ctx)
			}
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				applyBridgeToState(sessionState, "b1", BridgeResult{
					BranchID:       "b1",
					BridgeExists:   true,
					BridgeKind:     "maritime_to_conversion_corridor",
					OpenQuestions:  []string{},
					Confidence:     0.76,
					RouteSketches:  []string{"Chile -> Rotterdam -> AMG Bitterfeld -> Germany"},
					ProposedOps:    []ProposedGraphOp{{Kind: "create_edge", EdgeType: "SUPPLY_LINK"}},
					SuggestedSubscans: []InvestigationBranch{
						{ID: "follow-bridge", Question: "Verify the commercial operating status of AMG Bitterfeld for Chilean feedstock", RequiresPath: true},
					},
				})
			}
			return &contracts.MeshTask{ID: "b1-bridge", Capability: capability}, map[string]any{
				"branchId":          "b1",
				"endpointA":         ctx["endpointA"],
				"endpointB":         ctx["endpointB"],
				"bridgeQuestion":    ctx["bridgeQuestion"],
				"summary":           "A plausible bridge runs via maritime export from Chile into Rotterdam and conversion at AMG Bitterfeld.",
				"bridgeExists":      true,
				"bridgeKind":        "maritime_to_conversion_corridor",
				"intermediateNodes": []any{"/port/cl/antofagasta", "/port/nl/rotterdam", "/fac/de/amg-lithium-bitterfeld"},
				"routeSketches":     []any{"Chile -> Rotterdam -> AMG Bitterfeld -> Germany"},
				"confidence":        0.76,
				"proposedGraphOps": []any{
					map[string]any{"kind": "create_edge", "subjectPath": "/product/component/chemical/refining/lithium-carbonate", "objectPath": "/fac/de/amg-lithium-bitterfeld", "edgeType": "SUPPLY_LINK", "rationale": "Chilean carbonate can be supplied into AMG Bitterfeld through a European import corridor."},
				},
				"suggestedSubscans": []any{
					map[string]any{"id": "follow-bridge", "question": "Verify the commercial operating status of AMG Bitterfeld for Chilean feedstock", "requiresPath": true, "geoHints": []any{"Germany"}, "actorHints": []any{"AMG Lithium"}},
				},
				"sessionState": input["sessionState"],
			}, nil
		case "scout.path":
			pathCalls++
			if pathCalls == 1 {
				if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
					path := PathResult{
						QueriedProducts: []string{"/product/component/chemical/refining/lithium-carbonate"},
						ContinuityStatus: "broken",
						OpenGaps:        []string{"No converter or logistics leg between Chilean carbonate export and German destination was identified."},
						SourceBranchIDs: []string{"b1"},
					}
					applyPathToState(sessionState, path)
				}
				return &contracts.MeshTask{ID: "p1", Capability: capability}, map[string]any{
					"queriedProducts":  []any{"/product/component/chemical/refining/lithium-carbonate"},
					"continuityStatus": "broken",
					"openGaps":         []any{"No converter or logistics leg between Chilean carbonate export and German destination was identified."},
					"sourceBranchIds":  []any{"b1"},
					"sessionState":     input["sessionState"],
				}, nil
			}
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				path := PathResult{
					QueriedProducts:         []string{"/product/component/chemical/refining/lithium-carbonate"},
					ContinuityStatus:        "landed",
					MaterializedArtifactIDs: []string{"art-bridge-1"},
					SourceBranchIDs:         []string{"b1"},
				}
				applyPathToState(sessionState, path)
				markVerifiedOpsFromPath(sessionState, path)
			}
			return &contracts.MeshTask{ID: "p2", Capability: capability}, map[string]any{
				"queriedProducts":         []any{"/product/component/chemical/refining/lithium-carbonate"},
				"continuityStatus":        "landed",
				"materializedArtifactIds": []any{"art-bridge-1"},
				"sourceBranchIds":         []any{"b1"},
				"sessionState":            input["sessionState"],
			}, nil
		case "scout.materialize":
			if sessionState, ok := input["sessionState"].(*InvestigationState); ok && sessionState != nil {
				mat := MaterializeResult{
					Summary:         "landed",
					Persisted:       true,
					ArtifactIDs:     []string{"art-bridge-1"},
					Created:         []string{"art-bridge-1"},
					LandedBranchIDs: []string{"b1"},
				}
				applyMaterializeToState(sessionState, mat)
				markLandedOps(sessionState, mat)
			}
			return &contracts.MeshTask{ID: "m1", Capability: capability}, map[string]any{
				"summary":         "landed",
				"persisted":       true,
				"artifactIds":     []any{"art-bridge-1"},
				"created":         []any{"art-bridge-1"},
				"landedBranchIds": []any{"b1"},
				"sessionState":    input["sessionState"],
			}, nil
		case "scout.aggregate":
			return &contracts.MeshTask{ID: "a1", Capability: capability}, map[string]any{"summary": "done"}, nil
		case "scout.review":
			return &contracts.MeshTask{ID: "v1", Capability: capability}, map[string]any{"accepted": true, "rationale": "ok"}, nil
		default:
			return &contracts.MeshTask{ID: capability, Capability: capability}, map[string]any{}, nil
		}
	}

	task := &contracts.MeshTask{
		ID:         "plan-bridge",
		Capability: "scout.plan",
		TenantID:   "t1",
		Input: map[string]any{
			"tenantId":            "t1",
			"query":               "Trace lithium supply routes from Chile into Germany",
			"targetCanonicalPath": "/product/component/chemical/refining/lithium-hydroxide",
		},
	}

	result, err := p.processPlan(context.Background(), nil, task, "lease")
	if err != nil {
		t.Fatalf("processPlan err: %v", err)
	}
	if result == nil || result.Status != contracts.TaskStatusSucceeded {
		t.Fatalf("unexpected result: %#v", result)
	}
	if countCapability(childCaps, "scout.research") < 1 {
		t.Fatalf("expected at least one research turn, got %v", childCaps)
	}
	if countCapability(childCaps, "scout.critic") < 1 {
		t.Fatalf("expected at least one critic turn, got %v", childCaps)
	}
	if childCaps[len(childCaps)-2] != "scout.aggregate" || childCaps[len(childCaps)-1] != "scout.review" {
		t.Fatalf("expected aggregate/review tail, got %v", childCaps)
	}
}

func TestProcessPlanBootstrapDelegatesToScoutOrchestrate(t *testing.T) {
	p := &Processor{}
	called := ""
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, input map[string]any) (*contracts.MeshTask, map[string]any, error) {
		called = capability
		if _, ok := input["query"]; !ok {
			t.Fatalf("expected bootstrap to forward original task input")
		}
		return &contracts.MeshTask{ID: "orch-1", Capability: capability, Status: contracts.TaskStatusSucceeded}, map[string]any{
			"reviewAccepted": true,
			"review":         map[string]any{"accepted": true, "rationale": "ok"},
			"state":          map[string]any{"turn": float64(2)},
		}, nil
	}
	task := &contracts.MeshTask{
		ID:         "root-1",
		Capability: "scout.plan",
		TenantID:   "t1",
		Input: map[string]any{
			"query": "trace cathode route",
		},
	}
	result, err := p.processPlanBootstrap(context.Background(), nil, task, "lease")
	if err != nil {
		t.Fatalf("bootstrap err: %v", err)
	}
	if called != "scout.orchestrate" {
		t.Fatalf("expected delegate to scout.orchestrate, got %q", called)
	}
	if result == nil || result.Status != contracts.TaskStatusSucceeded {
		t.Fatalf("unexpected bootstrap result: %#v", result)
	}
	if accepted := result.ReviewAccepted; accepted == nil || !*accepted {
		t.Fatalf("expected reviewAccepted=true, got %#v", result.ReviewAccepted)
	}
}

func TestProcessOrchestrateResumesSessionStateWithoutReframingLoop(t *testing.T) {
	p := &Processor{}
	var childCaps []string
	p.runChildTaskFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, capability, _ string, _ map[string]any) (*contracts.MeshTask, map[string]any, error) {
		childCaps = append(childCaps, capability)
		switch capability {
		case "scout.aggregate":
			return &contracts.MeshTask{ID: "a1", Capability: capability, Status: contracts.TaskStatusSucceeded}, map[string]any{
				"summary": "session resumed aggregate",
			}, nil
		case "scout.review":
			return &contracts.MeshTask{ID: "r1", Capability: capability, Status: contracts.TaskStatusSucceeded}, map[string]any{
				"accepted":  true,
				"rationale": "ok",
				"quality":   "good",
			}, nil
		default:
			return &contracts.MeshTask{ID: "x1", Capability: capability, Status: contracts.TaskStatusSucceeded}, map[string]any{}, nil
		}
	}
	task := &contracts.MeshTask{
		ID:         "orchestrate-1",
		Capability: "scout.orchestrate",
		TenantID:   "t1",
		SessionRef: "vertex-session-1",
		Input: map[string]any{
			"query": "resume investigation",
			"sessionState": map[string]any{
				"request": map[string]any{
					"query": "resume investigation",
				},
				"goal": map[string]any{
					"primaryQuestion": "resume investigation",
				},
				"plan": map[string]any{
					"goal":              "resume investigation",
					"maxResearchRounds": float64(1),
					"branches": []any{
						map[string]any{"id": "b1", "question": "q1"},
					},
				},
				"turn":      float64(5),
				"maxTurns":  float64(5),
				"branchOrder": []any{"b1"},
				"branches": map[string]any{
					"b1": map[string]any{
						"branch": map[string]any{"id": "b1", "question": "q1"},
						"status": "done",
					},
				},
				"hypotheses": map[string]any{
					"b1": map[string]any{
						"id":       "hypothesis:b1",
						"branchId": "b1",
						"status":   "confirmed_materializable",
					},
				},
			},
		},
	}
	result, err := p.processOrchestrate(context.Background(), nil, task, "lease")
	if err != nil {
		t.Fatalf("orchestrate err: %v", err)
	}
	if result == nil || result.Status != contracts.TaskStatusSucceeded {
		t.Fatalf("unexpected orchestrate result: %#v", result)
	}
	if len(childCaps) != 2 || childCaps[0] != "scout.aggregate" || childCaps[1] != "scout.review" {
		t.Fatalf("expected resumed state to skip decision loop and only run aggregate/review, got %v", childCaps)
	}
}

func TestProcessResearchReturnsUpdatedSessionState(t *testing.T) {
	p := &Processor{}
	p.runModelObjectFn = func(_ context.Context, _ *agentmeshruntime.Service, task *contracts.MeshTask, _ map[string]any) map[string]any {
		if task == nil || task.Capability != "scout.research" {
			return nil
		}
		return map[string]any{
			"branchId":      "b1",
			"summary":       "new evidence",
			"confidence":    0.91,
			"canonicalRefs": []any{"/product/li_ion/cathode"},
			"productRefs":   []any{"/product/li_ion/cathode"},
			"openQuestions": []any{},
		}
	}
	task := &contracts.MeshTask{
		ID:         "research-1",
		Capability: "scout.research",
		Input: map[string]any{
			"branch": map[string]any{"id": "b1", "question": "trace"},
			"sessionState": map[string]any{
				"request": map[string]any{"query": "trace"},
				"goal":    map[string]any{"primaryQuestion": "trace"},
				"plan": map[string]any{
					"goal":              "trace",
					"maxResearchRounds": float64(2),
					"branches": []any{
						map[string]any{"id": "b1", "question": "trace"},
					},
				},
				"branches": map[string]any{
					"b1": map[string]any{
						"branch": map[string]any{"id": "b1", "question": "trace"},
						"status": "active",
					},
				},
				"hypotheses": map[string]any{
					"b1": map[string]any{
						"id":       "hypothesis:b1",
						"branchId": "b1",
						"status":   "unknown",
					},
				},
			},
		},
	}
	result, err := p.processResearch(context.Background(), nil, task)
	if err != nil {
		t.Fatalf("processResearch err: %v", err)
	}
	if result == nil || result.Status != contracts.TaskStatusSucceeded {
		t.Fatalf("unexpected research result: %#v", result)
	}
	output := mapValue(result.Output)
	sessionOut, ok := output["sessionState"].(*InvestigationState)
	if !ok || sessionOut == nil || sessionOut.Branches["b1"] == nil {
		t.Fatalf("expected typed sessionState in output, got %#v", output["sessionState"])
	}
	if sessionOut.Branches["b1"].ResearchRounds < 1 {
		t.Fatalf("expected research rounds to evolve in sessionState, got %d", sessionOut.Branches["b1"].ResearchRounds)
	}
	if sessionOut.Branches["b1"].LastConfidence <= 0 {
		t.Fatalf("expected branch confidence to be updated in sessionState, got %f", sessionOut.Branches["b1"].LastConfidence)
	}
}

func TestProcessResearchRequiresSessionState(t *testing.T) {
	p := &Processor{}
	task := &contracts.MeshTask{
		ID:         "strict-research-1",
		Capability: "scout.research",
		Input: map[string]any{
			"request": map[string]any{"query": "trace one branch"},
			"branch":  map[string]any{"id": "b1", "question": "q1"},
			"plan": map[string]any{
				"goal":              "trace one branch",
				"maxResearchRounds": float64(1),
				"branches": []any{
					map[string]any{"id": "b1", "question": "q1"},
				},
			},
		},
	}
	result, err := p.processResearch(context.Background(), nil, task)
	if err != nil {
		t.Fatalf("processResearch err: %v", err)
	}
	if result == nil || result.Status != contracts.TaskStatusFailed {
		t.Fatalf("expected failed status when sessionState is missing, got %#v", result)
	}
	if result.LastError == "" {
		t.Fatalf("expected error rationale for missing sessionState")
	}
}
