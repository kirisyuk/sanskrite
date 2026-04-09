package scout

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
)

func TestNewInvestigationStateInitializesStructuredPlanState(t *testing.T) {
	plan := InvestigationPlan{
		Goal:              "trace battery pack supply",
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "who supplies precursor"},
		},
	}
	state := newInvestigationState(InvestigationRequest{Query: "battery pack"}, plan)
	if state == nil {
		t.Fatalf("state is nil")
	}
	if len(state.Branches) != 1 || state.Branches["b1"] == nil {
		t.Fatalf("expected branch runtime state for b1")
	}
	if state.Hypotheses["b1"] == nil || state.Hypotheses["b1"].Status != HypothesisProposed {
		t.Fatalf("expected proposed hypothesis for b1")
	}
}

func TestNewInvestigationStateInitializesGoalAndEnvironment(t *testing.T) {
	req := InvestigationRequest{
		Query:               "study upstream lithium hydroxide supply into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
		AllowWrites:         true,
	}
	state := newInvestigationState(req, InvestigationPlan{
		Goal:              "study lithium hydroxide dependencies",
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "who produces lithium hydroxide?"}},
	})
	if state == nil {
		t.Fatalf("state is nil")
	}
	if state.Goal.Objective == "" || state.Goal.PrimaryQuestion == "" {
		t.Fatalf("expected goal frame to be populated, got %+v", state.Goal)
	}
	if state.Goal.WriteDisposition != "land_verified_graph_artifacts" {
		t.Fatalf("expected write disposition to reflect writable mode, got %q", state.Goal.WriteDisposition)
	}
	if state.Environment.WorkspaceKind == "" || state.Environment.UnitOfWork == "" {
		t.Fatalf("expected environment frame to be populated, got %+v", state.Environment)
	}
	if len(state.Environment.EntitySemantics) == 0 {
		t.Fatalf("expected graph semantics in environment frame")
	}
}

func TestCompactInvestigationSnapshotIncludesGoalAndEnvironment(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "trace semiconductor wafer supply",
		TargetCanonicalPath: "/product/component/electronic/manufacturing/semiconductor-wafer",
	}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "who supplies wafers?"}},
	})
	snapshot := compactInvestigationSnapshot(state)
	goal, ok := snapshot["goal"].(map[string]any)
	if !ok || goal["objective"] == "" {
		t.Fatalf("expected compact goal frame in snapshot, got %#v", snapshot["goal"])
	}
	environment, ok := snapshot["environment"].(map[string]any)
	if !ok || environment["workspaceKind"] == "" {
		t.Fatalf("expected compact environment frame in snapshot, got %#v", snapshot["environment"])
	}
}

func TestCompactInvestigationSnapshotIncludesLatestOutcome(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "trace lithium route",
	}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "resolve route"}},
	})
	recordActionOutcome(state, ActionOutcome{
		Turn:         1,
		Action:       "bridge",
		BranchID:     "b1",
		OutcomeClass: "structured_bridge",
		QualityDelta: "material",
		Summary:      "Named the middle conversion layer.",
		GraphUseful:  true,
	})
	snapshot := compactInvestigationSnapshot(state)
	latest, ok := snapshot["latestOutcome"].(map[string]any)
	if !ok || latest["action"] != "bridge" || latest["qualityDelta"] != "material" {
		t.Fatalf("expected compact latest outcome in snapshot, got %#v", snapshot["latestOutcome"])
	}
}

func TestNewInvestigationStateInitializesRoleAndCompleteness(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Study the upstream supply chain for lithium hydroxide into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "Which upstream branches matter most?"}},
	})
	if state == nil {
		t.Fatalf("state is nil")
	}
	if state.Role.PrimaryRole != "" || state.Role.Domain != "" {
		t.Fatalf("expected no fallback role synthesis, got %+v", state.Role)
	}
	if !state.Completeness.BroadScanRequired {
		t.Fatalf("expected completeness to require broad scan")
	}
	if state.Completeness.BroadScanQuestion == "" || state.Completeness.CompletionRationale == "" {
		t.Fatalf("expected initialized completeness frame, got %+v", state.Completeness)
	}
}

func TestPlanMissionRoleFlowsIntoStateAndBranches(t *testing.T) {
	req := withRuntimeFrames(InvestigationRequest{
		Query: "Study lithium supply routes from Chile into Germany for battery materials",
	}, nil)
	plan := ensurePlanRoleAndCompleteness(req, InvestigationPlan{
		MaxResearchRounds: 1,
		MissionRole: RoleFrame{
			Domain:               "battery_materials_trade_intelligence",
			PrimaryRole:          "top-grade battery materials corridor strategist",
			SupportingRoles:      []string{"hazmat maritime logistics specialist", "critical minerals contracting analyst"},
			InvestigationPosture: "map all commercial corridors before ranking which ones are operationally real",
			RoleRationale:        "The task is about competing battery-material corridors, not a generic research role.",
		},
		Branches: []InvestigationBranch{{ID: "b1", Question: "What are the primary corridors?"}},
	})
	state := newInvestigationState(req, plan)
	if state == nil {
		t.Fatalf("state is nil")
	}
	if state.Role.PrimaryRole != "top-grade battery materials corridor strategist" {
		t.Fatalf("expected planner-selected mission role to populate state, got %+v", state.Role)
	}
	if state.Branches["b1"].Branch.Role != "top-grade battery materials corridor strategist" {
		t.Fatalf("expected branch role to inherit planner-selected mission role, got %+v", state.Branches["b1"].Branch)
	}
}

func TestValidateMissionRoleRejectsMissingRole(t *testing.T) {
	if err := validateMissionRole(InvestigationPlan{}); err == nil {
		t.Fatalf("expected missing mission role to fail validation")
	}
	if err := validateMissionRole(InvestigationPlan{
		MissionRole: RoleFrame{
			PrimaryRole:          "top-grade battery materials corridor strategist",
			InvestigationPosture: "map corridors before narrowing",
			RoleRationale:        "Role must be explicit.",
		},
	}); err != nil {
		t.Fatalf("expected valid mission role to pass validation, got %v", err)
	}
}

func TestCriticFeedbackTriggersReplan(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	applyCriticToState(state, CriticResult{
		RiskyClaims:   []string{"claim lacks support"},
		OpenQuestions: []string{"what is missing upstream?"},
	})
	if !state.NeedsReplan {
		t.Fatalf("expected state.NeedsReplan to be true")
	}
	(&Processor{}).applyReplan(context.Background(), nil, &contracts.MeshTask{ID: "root"}, InvestigationRequest{}, map[string]any{}, state)
	if len(state.BranchOrder) < 2 {
		t.Fatalf("expected replan to add at least one new branch")
	}
}

func TestRepeatedGapDetectionDowngradesWeakBranch(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:      "b1",
		Summary:       "thin evidence",
		Confidence:    0.2,
		OpenQuestions: []string{"need stronger source"},
	})
	if got := state.Branches["b1"].Status; got != BranchStatusCandidate {
		t.Fatalf("expected branch to downgrade to candidate, got %s", got)
	}
}

func TestPathActionRunsOnlyWhenStateWarrants(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	if shouldAttemptPath(state) {
		t.Fatalf("path should not run before evidence")
	}
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Summary:        "grounded",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/a", "/product/b", "/product/c"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/a"},
	})
	if !shouldAttemptPath(state) {
		t.Fatalf("path should run after supported hypothesis")
	}
}

func TestProgressiveMaterializationMarksDoneBranches(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Hypotheses["b1"].MaterializationReady = true
	state.PathResult.SourceBranchIDs = []string{"b1"}
	applyMaterializeToState(state, MaterializeResult{
		Persisted:       true,
		Created:         []string{"artifact-1"},
		LandedBranchIDs: []string{"b1"},
	})
	if got := state.Branches["b1"].Status; got != BranchStatusDone {
		t.Fatalf("expected done branch status after materialization, got %s", got)
	}
}

func TestPlanBranchDependenciesApplyToRuntimeBranch(t *testing.T) {
	plan := InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "upstream", Question: "u"},
			{ID: "downstream", Question: "d"},
		},
		BranchDependencies: []BranchDependency{{FromBranchID: "upstream", ToBranchID: "downstream"}},
	}
	state := newInvestigationState(InvestigationRequest{}, plan)
	got := state.Branches["downstream"].Branch.DependsOn
	if len(got) != 1 || got[0] != "upstream" {
		t.Fatalf("expected downstream to depend on upstream, got %v", got)
	}
}

func TestBranchDependencyBlocksInitially(t *testing.T) {
	plan := InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "first"},
			{ID: "b2", Question: "second", DependsOn: []string{"b1"}},
		},
	}
	state := newInvestigationState(InvestigationRequest{}, plan)
	if action, id := chooseNextAction(state); action != "research" || id != "b1" {
		t.Fatalf("expected research b1 first, got %s %s", action, id)
	}
}

func TestCriticWithholdMaterializationBlocksMaterializeButNotPath(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/a", "/product/b", "/product/c"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/a"},
	})
	applyCriticToState(state, CriticResult{WithholdMaterialization: true})
	if shouldMaterialize(state) {
		t.Fatalf("materialize should be withheld")
	}
	if !shouldAttemptPath(state) {
		t.Fatalf("path trace should still be allowed when only materialization is withheld")
	}
}

func TestCriticWithholdPathDoesNotBlockLocalPathVerification(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q", RequiresPath: true, ProductHints: []string{"/product/a"}}},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Summary:        "grounded upstream substrate",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/a", "/product/b", "/org/x"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/a"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", EdgeType: "TRANSFORMS_INTO", Rationale: "ev-1"},
		},
	})
	applyCriticToState(state, CriticResult{
		RiskyClaims:         []string{"full target binding is still weak"},
		RiskTargetBranchIDs: []string{"b1"},
		WithholdPath:        true,
	})
	verdicts := synthesizeGraphOpVerdicts(state, state.LatestCritic)
	applyGraphOpVerdicts(state, verdicts)
	if !shouldAttemptPath(state) {
		t.Fatalf("local path verification should still be allowed when only target-binding path is withheld")
	}
	if action, _ := chooseNextAction(state); action != "path" {
		t.Fatalf("expected path when local substrate exists despite withholdPath, got %s", action)
	}
}

func TestCriticPivotsToMorePromisingBranchInsteadOfForcingReplan(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium hydroxide supply into Germany",
		Goal: GoalFrame{
			Objective:           "Trace lithium hydroxide supply into Germany",
			PrimaryQuestion:     "Does this route lead to German supply?",
			TargetCanonicalPath: "/product/lithium-hydroxide",
		},
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Are direct EU refining projects already serving Germany?", RequiresPath: true, ProductHints: []string{"/product/lithium-hydroxide"}},
			{ID: "b2", Question: "Which Chinese refining hubs convert lithium carbonate into lithium hydroxide for German cathode plants?", RequiresPath: true, ProductHints: []string{"/product/lithium-hydroxide"}},
		},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Summary:        "European refining pilots exist but evidence of commercial German volumes is weak.",
		Confidence:     0.85,
		CanonicalRefs:  []string{"/product/lithium-hydroxide", "/org/rock-tech"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/lithium-hydroxide"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_fragment", SubjectPath: "/org/rock-tech", Rationale: "ev-1"},
		},
	})
	applyCriticToState(state, CriticResult{
		RiskyClaims:           []string{"direct EU refining is not yet the dominant route into Germany"},
		RiskTargetBranchIDs:   []string{"b1"},
		ReprioritizeBranchIDs: []string{"b2"},
		WithholdPath:          true,
	})
	if state.NeedsReplan {
		t.Fatalf("expected runtime to pivot within existing branches instead of forcing replan")
	}
	h1 := state.Hypotheses["b1"]
	h2 := state.Hypotheses["b2"]
	if h1 == nil || h1.PursuitDecision != BranchPursuitPivot {
		t.Fatalf("expected b1 to pivot, got %+v", h1)
	}
	if h2 == nil || h2.PursuitDecision != BranchPursuitPress {
		t.Fatalf("expected b2 to be pressed, got %+v", h2)
	}
	action, branchID := chooseNextAction(state)
	if action != "research" || branchID != "b2" {
		t.Fatalf("expected research b2 after pivot, got %s %s", action, branchID)
	}
}

func TestLocallyVerifiedButLowRelevanceBranchGetsParked(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium hydroxide supply into Germany",
		Goal: GoalFrame{
			Objective:           "Trace lithium hydroxide supply into Germany",
			PrimaryQuestion:     "Which branches actually reach Germany?",
			TargetCanonicalPath: "/product/lithium-hydroxide",
		},
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Who owns historical exploration licenses in Peru?", ProductHints: []string{"/product/copper-concentrate"}},
		},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Summary:        "Historical exploration ownership is well documented but not tied to the lithium hydroxide route into Germany.",
		Confidence:     0.88,
		CanonicalRefs:  []string{"/org/example-mining", "/loc/pe"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/copper-concentrate"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_fragment", SubjectPath: "/org/example-mining", Rationale: "ev-1"},
		},
	})
	h := state.Hypotheses["b1"]
	if h == nil {
		t.Fatalf("expected hypothesis")
	}
	if !h.LocalReady {
		t.Fatalf("expected local verification")
	}
	if h.PursuitDecision != BranchPursuitPark {
		t.Fatalf("expected branch to be parked, got %+v", h)
	}
	if branchNeedsMoreResearch(state, state.Branches["b1"]) {
		t.Fatalf("parked branch should not continue consuming research turns")
	}
}

func TestCriticPreservesLocallyReadyBranchWhenTargetBindingIsRisky(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Summary:        "grounded upstream substrate",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/a", "/product/b", "/org/x"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/a"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", EdgeType: "TRANSFORMS_INTO", Rationale: "ev-1"},
		},
	})
	applyCriticToState(state, CriticResult{
		RiskyClaims:             []string{"downstream target binding is still weak"},
		RiskTargetBranchIDs:     []string{"b1"},
		WithholdMaterialization: true,
	})
	h := state.Hypotheses["b1"]
	if h == nil {
		t.Fatalf("expected hypothesis")
	}
	if !h.LocalReady {
		t.Fatalf("expected local substrate to remain ready")
	}
	if h.TargetBound {
		t.Fatalf("target binding should remain false under critic risk")
	}
	if h.MaterializationReady {
		t.Fatalf("materialization should remain withheld")
	}
	if state.Branches["b1"].Status == BranchStatusBlocked {
		t.Fatalf("branch should not be blocked when only target binding is risky")
	}
}

func TestSynthesizedVerdictsPromoteLocalOpsWithoutTargetBinding(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/a", "/product/b"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/a"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", EdgeType: "TRANSFORMS_INTO", Rationale: "ev-1"},
			{Kind: "mark_gap", SubjectPath: "/org/gap-anchor", Rationale: "still missing"},
		},
	})
	applyCriticToState(state, CriticResult{
		RiskyClaims:             []string{"still unclear if this binds to the target market"},
		RiskTargetBranchIDs:     []string{"b1"},
		WithholdMaterialization: true,
	})
	verdicts := synthesizeGraphOpVerdicts(state, state.LatestCritic)
	applyGraphOpVerdicts(state, verdicts)
	if got := state.PendingGraphOps[0].Status; got != "verified_local" && got != "accepted_local" {
		t.Fatalf("expected local acceptance for op 0, got %s", got)
	}
	if !state.PendingGraphOps[0].LocalReady || state.PendingGraphOps[0].TargetBound {
		t.Fatalf("expected local-only readiness for op 0: %+v", state.PendingGraphOps[0])
	}
	if got := state.PendingGraphOps[1].Status; got != "verified_local" {
		t.Fatalf("expected local gap closure for mark_gap, got %s", got)
	}
}

func TestChooseNextActionPrefersPathForLocalReadySubstrate(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q", RequiresPath: true, ProductHints: []string{"/product/a"}}},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/a", "/product/b"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/a"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", EdgeType: "TRANSFORMS_INTO", Rationale: "ev-1"},
		},
	})
	applyCriticToState(state, CriticResult{
		RiskyClaims:             []string{"end-to-end target binding still unresolved"},
		RiskTargetBranchIDs:     []string{"b1"},
		WithholdMaterialization: true,
	})
	verdicts := synthesizeGraphOpVerdicts(state, state.LatestCritic)
	applyGraphOpVerdicts(state, verdicts)
	action, _ := chooseNextAction(state)
	if action != "path" {
		t.Fatalf("expected path to be preferred for local-ready substrate, got %s", action)
	}
}

func TestCriticKillBranchIDs(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "a"},
			{ID: "b2", Question: "b"},
		},
	})
	applyCriticToState(state, CriticResult{KillBranchIDs: []string{"b2"}})
	if state.Branches["b2"].Status != BranchStatusRejected {
		t.Fatalf("expected b2 rejected, got %s", state.Branches["b2"].Status)
	}
}

func TestReprioritizeBranchOrder(t *testing.T) {
	order := []string{"a", "b", "c"}
	got := reprioritizeBranchOrder(order, []string{"c", "a"})
	if len(got) != 3 || got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Fatalf("unexpected order: %v", got)
	}
}

func TestComputeBranchConfidenceIsGroundedNotSelfAssessed(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})

	// High model confidence, but no grounded evidence.
	weak := ResearchResult{
		BranchID:   "b1",
		Confidence: 0.95,
	}
	if score := computeBranchConfidence(state, "b1", weak); score >= 0.50 {
		t.Fatalf("ungrounded high model confidence should yield low computed score, got %f", score)
	}

	// Moderate model confidence, but well-grounded.
	strong := ResearchResult{
		BranchID:       "b1",
		Confidence:     0.70,
		CanonicalRefs:  []string{"/product/a", "/product/b", "/product/c", "/product/d"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3", "ev-4"},
		ProductRefs:    []string{"/product/a"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", EdgeType: "TRANSFORMS_INTO"},
		},
	}
	if score := computeBranchConfidence(state, "b1", strong); score < 0.70 {
		t.Fatalf("well-grounded research should yield high computed score, got %f", score)
	}
}

func TestProposedGraphOpsAccumulateInState(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})

	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Confidence:     0.7,
		CanonicalRefs:  []string{"/product/a"},
		EvidencePoints: []string{"ev-1"},
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", EdgeType: "SUPPLY_LINK", Rationale: "ev-1"},
			{Kind: "create_fragment", SubjectPath: "/product/c"},
		},
	})

	if len(state.PendingGraphOps) != 2 {
		t.Fatalf("expected 2 pending ops, got %d", len(state.PendingGraphOps))
	}
	if state.PendingGraphOps[0].Status != "proposed" {
		t.Fatalf("expected proposed status, got %s", state.PendingGraphOps[0].Status)
	}
	if state.PendingGraphOps[0].BranchID != "b1" {
		t.Fatalf("expected branchId b1, got %s", state.PendingGraphOps[0].BranchID)
	}
	if state.PendingGraphOps[0].SourceTurn != state.Turn {
		t.Fatalf("expected sourceTurn %d, got %d", state.Turn, state.PendingGraphOps[0].SourceTurn)
	}
	if state.PendingGraphOps[0].Confidence == 0 {
		t.Fatalf("expected non-zero computed op confidence")
	}
}

func TestGraphOpVerdictsUpdatePendingOps(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	state.PendingGraphOps = []ProposedGraphOp{
		{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", Status: "proposed", BranchID: "b1"},
		{Kind: "mark_gap", SubjectPath: "/product/c", Status: "proposed", BranchID: "b1"},
		{Kind: "create_fragment", SubjectPath: "/product/d", Status: "proposed", BranchID: "b1"},
	}

	applyGraphOpVerdicts(state, []GraphOpVerdict{
		{OpIndex: 0, Verdict: "accepted", Reason: "grounded"},
		{OpIndex: 1, Verdict: "rejected", Reason: "unverifiable"},
		{OpIndex: 2, Verdict: "needs_research", Reason: "more evidence needed"},
	})

	if state.PendingGraphOps[0].Status != "accepted" {
		t.Fatalf("op 0: expected accepted, got %s", state.PendingGraphOps[0].Status)
	}
	if state.PendingGraphOps[1].Status != "rejected" {
		t.Fatalf("op 1: expected rejected, got %s", state.PendingGraphOps[1].Status)
	}
	if state.PendingGraphOps[2].Status != "proposed" {
		t.Fatalf("op 2: expected proposed (needs_research resets to proposed), got %s", state.PendingGraphOps[2].Status)
	}
}

func TestPathVerifiesAcceptedOps(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	state.PendingGraphOps = []ProposedGraphOp{
		{Kind: "create_edge", SubjectPath: "/product/a", ObjectPath: "/product/b", Status: "accepted", BranchID: "b1", Confidence: 0.5},
		{Kind: "create_edge", SubjectPath: "/product/c", ObjectPath: "/product/d", Status: "accepted", BranchID: "b1", Confidence: 0.4},
	}

	markVerifiedOpsFromPath(state, PathResult{
		QueriedProducts: []string{"/product/a", "/product/c"},
		PathHypotheses: []PathHypothesis{
			{ProductRef: "/product/a", BindingStatus: "bound"},
			{ProductRef: "/product/c", BindingStatus: "unbound"},
		},
	})

	if state.PendingGraphOps[0].Status != "verified" {
		t.Fatalf("op 0: expected verified (bound product), got %s", state.PendingGraphOps[0].Status)
	}
	if state.PendingGraphOps[0].Confidence <= 0.5 {
		t.Fatalf("verified op should have boosted confidence")
	}
	if state.PendingGraphOps[1].Status != "accepted" {
		t.Fatalf("op 1: expected still accepted (unbound product), got %s", state.PendingGraphOps[1].Status)
	}
}

func TestBuildCheckpointOutputCapturesExecutionJournalAndState(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium hydroxide into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 2,
		MissionRole: RoleFrame{
			Domain:               "battery_materials_trade_intelligence",
			PrimaryRole:          "top-grade battery materials corridor strategist",
			InvestigationPosture: "map corridors before narrowing",
			RoleRationale:        "Checkpoint should preserve planner-selected mission role.",
		},
		Branches: []InvestigationBranch{{ID: "b1", Question: "trace route", RequiresPath: true, ProductHints: []string{"/product/lithium-hydroxide"}}},
	})
	state.Turn = 1
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:       "b1",
		Summary:        "German cathode plants depend on imported lithium hydroxide.",
		Confidence:     0.9,
		CanonicalRefs:  []string{"/product/lithium-hydroxide", "/loc/de"},
		EvidencePoints: []string{"ev-1", "ev-2", "ev-3"},
		ProductRefs:    []string{"/product/lithium-hydroxide"},
		GapDiagnosis: GapDiagnosis{
			Kind:          "missing_completeness",
			Summary:       "Broad scan still needs to rule out materially different alternatives.",
			NextAction:    "research",
			BranchID:      "b1",
			QuestionClass: "completeness_scan",
		},
	})
	appendReasoningBeat(state, ReasoningBeat{
		Turn:           1,
		Phase:          "decision",
		Decision:       "research",
		DecisionWhy:    "Need grounded German landing facts.",
		WhatWeLookedAt: []string{"goal", "active_frontier"},
		WhatWeFound:    []string{"local substrate exists"},
	})
	taskRefs := map[string]any{"research": []string{"child-1"}, "critic": []string{}, "path": []string{}, "materialize": []string{}}
	output := buildCheckpointOutput(InvestigationRequest{Query: "Trace lithium hydroxide into Germany"}, state, taskRefs, "research", "b1", "child-1", researchMap(ResearchResult{
		BranchID:   "b1",
		Summary:    "German cathode plants depend on imported lithium hydroxide.",
		Confidence: 0.9,
	}))

	if inProgress, _ := output["inProgress"].(bool); !inProgress {
		t.Fatalf("expected checkpoint to be marked in progress")
	}
	checkpoint, _ := output["checkpoint"].(map[string]any)
	if checkpoint["action"] != "research" || checkpoint["childTaskId"] != "child-1" {
		t.Fatalf("unexpected checkpoint metadata: %#v", checkpoint)
	}
	latestStep, _ := output["latestStep"].(map[string]any)
	if latestStep["type"] != "research" {
		t.Fatalf("expected latest step type research, got %#v", latestStep)
	}
	journal, _ := output["executionJournal"].([]map[string]any)
	if len(journal) == 0 {
		t.Fatalf("expected non-empty execution journal")
	}
	stateMap, _ := output["state"].(map[string]any)
	if stateMap["currentBottleneck"] == nil {
		t.Fatalf("expected compact checkpoint state, got %#v", stateMap)
	}
	if gap, _ := stateMap["latestGapDiagnosis"].(map[string]any); gap["kind"] != "missing_completeness" {
		t.Fatalf("expected latest gap diagnosis in checkpoint state, got %#v", stateMap["latestGapDiagnosis"])
	}
	role, _ := stateMap["role"].(map[string]any)
	if role["primaryRole"] == "" {
		t.Fatalf("expected role frame in checkpoint state, got %#v", stateMap["role"])
	}
	completeness, _ := stateMap["completeness"].(map[string]any)
	if broadScanRequired, _ := completeness["broadScanRequired"].(bool); !broadScanRequired {
		t.Fatalf("expected completeness frame in checkpoint state, got %#v", stateMap["completeness"])
	}
}

func TestStopWhenAllGraphOpsResolved(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q1"}},
	})
	state.ResearchResults = []ResearchResult{{BranchID: "b1", Summary: "done"}}
	state.Branches["b1"].Status = BranchStatusDone
	state.PendingGraphOps = []ProposedGraphOp{
		{Kind: "create_edge", SubjectPath: "/product/a", Status: "landed", BranchID: "b1"},
		{Kind: "mark_gap", SubjectPath: "/product/b", Status: "rejected", BranchID: "b1"},
	}

	action, _ := chooseNextAction(state)
	if action != "stop" {
		t.Fatalf("expected stop when all graph ops resolved, got %s", action)
	}
	if state.StopReason != "all_graph_ops_resolved" {
		t.Fatalf("expected all_graph_ops_resolved stop reason, got %s", state.StopReason)
	}
}

func TestEnsurePlanRoleAndCompletenessAddsBroadScanBranchAndRoles(t *testing.T) {
	req := withRuntimeFrames(InvestigationRequest{
		Query:               "Study lithium hydroxide supply into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, nil)
	plan := ensurePlanRoleAndCompleteness(req, InvestigationPlan{
		Goal:              "Trace lithium hydroxide routes",
		MaxResearchRounds: 2,
		MissionRole: RoleFrame{
			PrimaryRole:          "top-grade lithium corridor analyst",
			InvestigationPosture: "map all corridor variants before narrowing",
			RoleRationale:        "This test needs an explicit planner-owned mission role.",
		},
		FocusProducts: []string{"/product/component/chemical/refining/lithium-hydroxide"},
		FocusGeos:     []string{"Germany", "Chile"},
		Branches: []InvestigationBranch{
			{ID: "landing", Question: "Are there direct Chile-to-Germany lithium hydroxide shipments?", RequiresPath: true},
		},
	})
	if len(plan.Branches) == 0 || !plan.Branches[0].BroadScan {
		t.Fatalf("expected broad-scan branch to be injected at the front, got %+v", plan.Branches)
	}
	if plan.Branches[0].Role == "" || plan.Branches[1].Role == "" {
		t.Fatalf("expected inferred professional roles on plan branches, got %+v", plan.Branches)
	}
}

func TestChooseNextActionPrioritizesBroadScanBeforeNarrowBranch(t *testing.T) {
	req := withRuntimeFrames(InvestigationRequest{
		Query:               "Study lithium hydroxide supply into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, nil)
	plan := ensurePlanRoleAndCompleteness(req, InvestigationPlan{
		Goal:              "Trace lithium hydroxide routes",
		MaxResearchRounds: 2,
		MissionRole: RoleFrame{
			PrimaryRole:          "top-grade lithium corridor analyst",
			InvestigationPosture: "map all corridor variants before narrowing",
			RoleRationale:        "This test needs an explicit planner-owned mission role.",
		},
		FocusProducts: []string{"/product/component/chemical/refining/lithium-hydroxide"},
		Branches: []InvestigationBranch{
			{ID: "landing", Question: "Are there direct Chile-to-Germany shipments?", RequiresPath: true, ProductHints: []string{"/product/component/chemical/refining/lithium-hydroxide"}},
		},
	})
	state := newInvestigationState(req, plan)
	action, branchID := chooseNextAction(state)
	if action != "research" || branchID != "broad-scan" {
		t.Fatalf("expected broad-scan research first, got %s %s", action, branchID)
	}
}

func TestCompletenessGateBlocksPrematureStopUntilClosed(t *testing.T) {
	req := withRuntimeFrames(InvestigationRequest{
		Query:               "Study lithium hydroxide supply into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, nil)
	state := newInvestigationState(req, InvestigationPlan{
		MaxResearchRounds: 2,
		MissionRole: RoleFrame{
			PrimaryRole:          "top-grade lithium corridor analyst",
			InvestigationPosture: "map all corridor variants before narrowing",
			RoleRationale:        "This test needs an explicit planner-owned mission role.",
		},
		Branches: []InvestigationBranch{
			{ID: "broad-scan", Question: req.Completeness.BroadScanQuestion, BroadScan: true},
		},
	})
	state.ResearchResults = []ResearchResult{{BranchID: "broad-scan", Summary: "Found a Chile route but have not yet ruled out Australia or China conversion routes."}}
	state.Branches["broad-scan"].Status = BranchStatusDone
	state.Completeness.BroadScanCompleted = true
	state.Completeness.ExplorationStatus = "in_progress"
	state.PendingGraphOps = []ProposedGraphOp{
		{Kind: "create_edge", SubjectPath: "/product/a", Status: "landed", BranchID: "broad-scan"},
	}

	action, _ := chooseNextAction(state)
	if action == "stop" {
		t.Fatalf("expected completeness gate to block stop before explicit closure")
	}

	state.LatestCritic = CriticResult{
		CompletenessAssessment: CompletenessAssessment{
			Status:                 "satisfied",
			Reason:                 "Broad scan found no remaining material alternatives.",
			BroadScanCompleted:     true,
			NoMaterialAlternatives: true,
		},
	}
	applyCompletenessAssessmentToState(state, state.LatestCritic.CompletenessAssessment)
	action, _ = chooseNextAction(state)
	if action != "stop" {
		t.Fatalf("expected stop after completeness closure, got %s", action)
	}
}

func TestDecodeProposedGraphOps(t *testing.T) {
	raw := []any{
		map[string]any{"kind": "create_edge", "subjectPath": "/product/a", "objectPath": "/product/b", "edgeType": "TRANSFORMS_INTO", "rationale": "test"},
		map[string]any{"kind": "mark_gap", "subjectPath": "/product/c"},
		map[string]any{"objectPath": "/product/d"}, // missing kind and subjectPath — should be skipped
	}
	ops := decodeProposedGraphOps(raw)
	if len(ops) != 2 {
		t.Fatalf("expected 2 valid ops, got %d", len(ops))
	}
	if ops[0].Kind != "create_edge" || ops[0].SubjectPath != "/product/a" {
		t.Fatalf("unexpected first op: %+v", ops[0])
	}
	if ops[0].Status != "proposed" {
		t.Fatalf("expected default proposed status, got %s", ops[0].Status)
	}
}

func TestDecodeGraphOpVerdicts(t *testing.T) {
	raw := []any{
		map[string]any{"opIndex": float64(0), "verdict": "accepted", "reason": "grounded"},
		map[string]any{"opIndex": float64(2), "verdict": "rejected"},
		map[string]any{"verdict": "accepted"}, // missing opIndex — should be skipped
	}
	verdicts := decodeGraphOpVerdicts(raw)
	if len(verdicts) != 2 {
		t.Fatalf("expected 2 valid verdicts, got %d", len(verdicts))
	}
	if verdicts[0].OpIndex != 0 || verdicts[0].Verdict != "accepted" {
		t.Fatalf("unexpected first verdict: %+v", verdicts[0])
	}
}

func TestExplainStopReasonAddsRuntimeDiagnostics(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	state.Turn = 5
	state.StopReason = "no_action_available"
	state.PendingGraphOps = []ProposedGraphOp{
		{Kind: "create_edge", SubjectPath: "/product/a", Status: "proposed"},
	}

	beat := explainStopReason(state)
	if beat.Phase != "stop" {
		t.Fatalf("expected stop phase, got %s", beat.Phase)
	}
	if len(beat.Diagnostics) == 0 {
		t.Fatalf("expected stop diagnostics, got none")
	}
	if beat.Diagnostics[0].Code == "" {
		t.Fatalf("expected diagnostic code")
	}
}

func TestDetectCriticLoop(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	state.ActionLog = []string{"critic", "critic", "critic", "critic", "research", "critic"}
	if !detectCriticLoop(state) {
		t.Fatalf("expected critic loop detection")
	}
}

func TestAppendReasoningBeatStoresRuntimeDiagnosis(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 1,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q"}},
	})
	appendReasoningBeat(state, ReasoningBeat{
		Turn:  1,
		Phase: "critic",
		Diagnostics: []RuntimeDiagnosis{
			{Code: "critic_loop_detected", Severity: "error", Message: "loop"},
		},
	})
	if len(state.ReasoningTrace) != 1 {
		t.Fatalf("expected one reasoning beat, got %d", len(state.ReasoningTrace))
	}
	if len(state.RuntimeDiagnosis) != 1 || state.RuntimeDiagnosis[0].Code != "critic_loop_detected" {
		t.Fatalf("expected runtime diagnosis to be propagated, got %#v", state.RuntimeDiagnosis)
	}
}

func TestChooseNextActionPrefersBridgeForMissingMiddleGap(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Trace lithium from Chile into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Trace Chile carbonate into German cathode plants", RequiresPath: true, ProductHints: []string{"/product/component/chemical/refining/lithium-carbonate"}, GeoHints: []string{"Chile", "Germany"}},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].LastConfidence = 0.84
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.72
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.PathResult = PathResult{
		ContinuityStatus: "broken",
		OpenGaps:         []string{"No converter or logistics leg between Chilean carbonate and German destination was identified."},
		SourceBranchIDs:  []string{"b1"},
	}
	state.PendingGraphOps = []ProposedGraphOp{
		{BranchID: "b1", Kind: "create_edge", SubjectPath: "/org/sqm", ObjectPath: "/product/component/chemical/refining/lithium-carbonate", EdgeType: "PRODUCES", Status: "verified_local", LocalReady: true},
	}

	action, branchID := chooseNextAction(state)
	if action != "bridge" || branchID != "b1" {
		t.Fatalf("expected bridge for missing middle, got %s %s", action, branchID)
	}
}

func TestChooseNextActionEscalatesToBridgeAfterRepeatedUnresolvedMiddle(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium carbonate from Chile into BASF Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the intermediate refining layer", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].ResearchRounds = 3
	state.Branches["b1"].MarginalGainStreak = 1
	state.Branches["b1"].OpenQuestions = []string{"What sits between Chilean carbonate output and BASF input?"}
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.73
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.PendingGraphOps = []ProposedGraphOp{
		{BranchID: "b1", Kind: "create_edge", SubjectPath: "/org/sqm", ObjectPath: "/product/component/chemical/refining/lithium-carbonate", EdgeType: "PRODUCES", Status: "accepted_local", LocalReady: true},
	}
	state.ActionLog = []string{"research", "critic", "research", "critic", "research"}

	action, branchID := chooseNextAction(state)
	if action != "bridge" || branchID != "b1" {
		t.Fatalf("expected repeated unresolved middle to escalate to bridge, got %s %s", action, branchID)
	}
}

func TestChooseNextActionAllowsBridgeForTargetBoundButStructurallyUnresolvedBranch(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Investigate export-control intermediaries between ASML and SMIC",
	}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Identify distributor and licensing middle layers", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].ResearchRounds = 3
	state.Branches["b1"].MarginalGainStreak = 1
	state.Branches["b1"].OpenQuestions = []string{"Which institutional distributor sits between ASML and the restricted fab?"}
	state.Hypotheses["b1"].Status = HypothesisSupported
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetBound = true
	state.Hypotheses["b1"].TargetRelevance = 0.92
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.ActionLog = []string{"research", "critic", "research", "critic", "path", "critic"}

	action, branchID := chooseNextAction(state)
	if action != "bridge" || branchID != "b1" {
		t.Fatalf("expected target-bound but unresolved branch to escalate to bridge, got %s %s", action, branchID)
	}
}

func TestChooseNextActionHonorsExplicitRecommendedNeighborAction(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Trace lithium from Chile into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Trace Chile carbonate into German cathode plants", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusActive
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.71
	state.ActionLog = []string{"research", "critic", "path"}
	state.LatestRecommendedAction = ActionRecommendation{
		Action:      "bridge",
		BranchID:    "b1",
		Reason:      "Path failed; bridge the missing middle next.",
		FailureMode: "path_continuity_gap",
		Question:    "What sits between Chilean carbonate export and German refining demand?",
	}

	action, branchID := chooseNextAction(state)
	if action != "bridge" || branchID != "b1" {
		t.Fatalf("expected explicit recommendation to drive bridge, got %s %s", action, branchID)
	}
}

func TestChooseNextActionPrefersGapDiagnosisOverConflictingRecommendation(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Trace lithium from Chile into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Trace Chile carbonate into German cathode plants", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].LastConfidence = 0.82
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.78
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.LatestGapDiagnosis = GapDiagnosis{
		Kind:           "missing_middle",
		Summary:        "The route endpoints are known but the middle layer remains unknown.",
		QuestionClass:  "bridge_query",
		NextAction:     "bridge",
		BranchID:       "b1",
		EndpointsKnown: true,
	}
	state.LatestRecommendedAction = ActionRecommendation{
		Action:      "research",
		BranchID:    "b1",
		Reason:      "Keep gathering nearby facts.",
		FailureMode: "insufficient_signal",
	}

	action, branchID := chooseNextAction(state)
	if action != "bridge" || branchID != "b1" {
		t.Fatalf("expected gap diagnosis to drive bridge, got %s %s", action, branchID)
	}
}

func TestChooseNextActionIgnoresStuckResearchRecommendationAndPivots(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Trace lithium from Chile into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Trace Chile carbonate into German cathode plants", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].ResearchRounds = 5
	state.Branches["b1"].LastConfidence = 0.82
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.8
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.PathResult = PathResult{
		ContinuityStatus: "broken",
		OpenGaps:         []string{"No converter or logistics leg identified."},
		SourceBranchIDs:  []string{"b1"},
	}
	stuck := ActionRecommendation{
		Action:      "research",
		BranchID:    "b1",
		Reason:      "Keep searching for the same hidden processor.",
		FailureMode: "opaque_contracts",
	}
	state.RecommendedActions = []ActionRecommendation{stuck, stuck, stuck}
	state.LatestRecommendedAction = stuck

	action, branchID := chooseNextAction(state)
	if action != "bridge" || branchID != "b1" {
		t.Fatalf("expected loop guard to ignore stuck research and pivot to bridge, got %s %s", action, branchID)
	}
}

func TestChooseNextActionPersistsWorkingPathWhenCriticSaysGoodEnough(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Branches["b1"].LastConfidence = 0.88
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.84
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.LatestBridge = BridgeResult{BranchID: "b1", BridgeExists: true, Summary: "A structurally plausible direct route exists."}
	state.LatestCritic = CriticResult{
		GoodEnoughAssessment: GoodEnoughAssessment{
			Status:              "route_good_enough",
			ClosureLevel:        "route_class",
			PersistWorkingPath:  true,
			FurtherWorkLowValue: true,
			Reason:              "The route is already good enough to persist as a working path.",
			TargetBranchIDs:     []string{"b1"},
		},
		WithholdPath: true,
	}

	action, branchID := chooseNextAction(state)
	if action != "path" || branchID != "" {
		t.Fatalf("expected path for good-enough working path persistence, got %s %s", action, branchID)
	}
}

func TestChooseNextActionStopsWhenCriticMarksGoodEnoughAndLowValue(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true, BroadScan: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusDone
	state.Completeness.BroadScanCompleted = true
	state.Completeness.ExplorationStatus = "satisfied"
	state.Completeness.NoMaterialAlternatives = true
	state.LatestCritic = CriticResult{
		GoodEnoughAssessment: GoodEnoughAssessment{
			Status:              "business_good_enough",
			ClosureLevel:        "business_answer",
			StopNow:             true,
			FurtherWorkLowValue: true,
			Reason:              "Remaining gaps are non-blocking for this graph-filling tier.",
		},
	}

	action, _ := chooseNextAction(state)
	if action != "stop" {
		t.Fatalf("expected stop when critic marks result good enough, got %s", action)
	}
}

func TestChooseNextActionAllowsPersistWorkingPathAfterRegularPathBudgetIfTargetChanged(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true},
		},
	})
	state.PathAttempts = 4
	state.Branches["b1"].Status = BranchStatusSupported
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.91
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.PathResult = PathResult{
		TraceDigests:     []map[string]any{{"route": "old"}},
		SourceBranchIDs:  []string{"other_branch"},
		ContinuityStatus: "partial",
	}
	state.LatestCritic = CriticResult{
		GoodEnoughAssessment: GoodEnoughAssessment{
			Status:              "route_good_enough",
			ClosureLevel:        "route_class",
			PersistWorkingPath:  true,
			FurtherWorkLowValue: true,
			Reason:              "Current direct route is now good enough to persist.",
			TargetBranchIDs:     []string{"b1"},
		},
	}

	action, branchID := chooseNextAction(state)
	if action != "path" || branchID != "" {
		t.Fatalf("expected extra path attempt for new good-enough target branch, got %s %s", action, branchID)
	}
}

func TestApplyCriticToStateDoesNotSynthesizeGoodEnoughFromState(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.82
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.LatestBridge = BridgeResult{
		BranchID:     "b1",
		BridgeExists: true,
		Summary:      "A plausible direct bridge exists.",
	}

	applyCriticToState(state, CriticResult{
		AcceptedFindings: []string{"There is a plausible direct route."},
	})

	item := state.LatestCritic.GoodEnoughAssessment
	if item.Status != "insufficient" || item.PersistWorkingPath || item.StopNow {
		t.Fatalf("expected neutral default good-enough assessment, got %+v", item)
	}
}

func TestBuildDecisionCandidatesIncludesBridgeForKnownMissingMiddle(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true},
		},
	})
	state.LatestGapDiagnosis = GapDiagnosis{
		Kind:           "missing_middle",
		BranchID:       "b1",
		EndpointA:      "/loc/cl/salar_de_atacama",
		EndpointB:      "/actor/basf",
		EndpointsKnown: true,
		Summary:        "Need the refining and logistics middle layer.",
		Why:            "Known endpoints exist but the middle entities are still unresolved.",
	}

	candidates := buildDecisionCandidates(state)
	found := false
	for _, item := range candidates {
		if item.Action == "bridge" && item.BranchID == "b1" && item.Available {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected bridge candidate for known missing-middle gap, got %+v", candidates)
	}
}

func TestHandleRetryableChildFailureRetriesThenStops(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{Query: "x"}, InvestigationPlan{})
	err := fmt.Errorf("Vertex AI API error 429: resource exhausted")
	if !handleRetryableChildFailure(state, "research", "b1", err) {
		t.Fatalf("first retryable failure should allow retry")
	}
	if state.SystemErrorCount != 1 {
		t.Fatalf("count after first failure: got %d", state.SystemErrorCount)
	}
	if !handleRetryableChildFailure(state, "research", "b1", err) {
		t.Fatalf("second retryable failure should still allow retry")
	}
	if handleRetryableChildFailure(state, "research", "b1", err) {
		t.Fatalf("third retryable failure should stop retries")
	}
	if state.StopReason != "system_error_retry_exhausted" {
		t.Fatalf("unexpected stop reason: %q", state.StopReason)
	}
	if state.LatestOutcome.Action != "research" || !state.LatestOutcome.SystemError {
		t.Fatalf("expected latest outcome to capture retryable system error, got %+v", state.LatestOutcome)
	}
}

func TestBuildDecisionCandidatesDisablesRepeatedLowGainResearchAfterOneReformulation(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true},
		},
	})
	state.CurrentBottleneck = string(BottleneckMissingMiddle)
	state.Branches["b1"].ResearchRounds = 2
	state.Branches["b1"].MarginalGainStreak = 1
	state.Branches["b1"].ReformulationCount = 1
	state.Branches["b1"].Status = BranchStatusSupported
	state.Hypotheses["b1"].PursuitDecision = BranchPursuitPress
	state.Hypotheses["b1"].TargetRelevance = 0.9

	candidates := buildDecisionCandidates(state)
	for _, item := range candidates {
		if item.Action == "research" && item.BranchID == "b1" {
			if item.Available {
				t.Fatalf("expected research candidate to be disabled after one low-gain reformulation, got %+v", item)
			}
			return
		}
	}
	t.Fatalf("expected research candidate for b1 to exist")
}

func TestApplyResearchToStateStoresGapDiagnosis(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Investigate what sits between SQM and BASF",
	}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Find the intermediate refining layer", RequiresPath: true},
		},
	})
	applyResearchToState(state, "b1", ResearchResult{
		BranchID:   "b1",
		Summary:    "SQM and BASF look plausible as endpoints, but the intermediate converter is still missing.",
		Confidence: 0.71,
		GapDiagnosis: GapDiagnosis{
			Kind:           "missing_middle",
			Summary:        "The middle conversion layer remains unresolved.",
			QuestionClass:  "bridge_query",
			NextAction:     "bridge",
			BranchID:       "b1",
			EndpointA:      "/org/sqm",
			EndpointB:      "/org/basf",
			EndpointsKnown: true,
		},
	})
	if state.LatestGapDiagnosis.Kind != "missing_middle" || state.LatestGapDiagnosis.NextAction != "bridge" {
		t.Fatalf("expected latest gap diagnosis to be stored, got %+v", state.LatestGapDiagnosis)
	}
	if len(state.GapDiagnoses) != 1 {
		t.Fatalf("expected gap diagnosis history to record the research diagnosis, got %d", len(state.GapDiagnoses))
	}
}

func TestBuildDecisionCandidatesIncludesRetryCandidateAfterSystemError(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query: "Trace lithium routes from Chile into Germany",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Resolve the direct route", RequiresPath: true},
		},
	})
	recordActionOutcome(state, ActionOutcome{
		Turn:         2,
		Action:       "bridge",
		BranchID:     "b1",
		OutcomeClass: "system_error",
		QualityDelta: "none",
		Summary:      "Bridge failed before producing output.",
		SystemError:  true,
		Retryable:    true,
		RetryAttempt: 1,
	})
	candidates := buildDecisionCandidates(state)
	for _, item := range candidates {
		if item.Action == "bridge" && item.BranchID == "b1" && item.Available {
			return
		}
	}
	t.Fatalf("expected retry candidate for latest retryable system error, got %+v", candidates)
}

func TestApplyBridgeToStateCreatesOpsAndSubscans(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Trace lithium from Chile into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Trace Chile carbonate into Germany", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Hypotheses["b1"].LocalReady = true
	state.Hypotheses["b1"].TargetRelevance = 0.68
	applyBridgeToState(state, "b1", BridgeResult{
		BranchID:          "b1",
		EndpointA:         "/org/sqm",
		EndpointB:         "/fac/de/amg-lithium-bitterfeld",
		BridgeQuestion:    "What sits between SQM and AMG Bitterfeld?",
		Summary:           "A plausible bridge runs through lithium carbonate export from Chile, maritime shipment into Europe, and conversion at AMG Bitterfeld.",
		BridgeExists:      true,
		BridgeKind:        "export_to_conversion_corridor",
		IntermediateNodes: []string{"/port/cl/antofagasta", "/port/nl/rotterdam", "/fac/de/amg-lithium-bitterfeld"},
		OpenQuestions:     []string{"Is AMG already receiving commercial feedstock from Chile?"},
		Confidence:        0.73,
		ProposedOps: []ProposedGraphOp{
			{Kind: "create_edge", SubjectPath: "/org/sqm", ObjectPath: "/port/cl/antofagasta", EdgeType: "SUPPLY_LINK", Rationale: "SQM exports Chilean lithium chemicals through Antofagasta."},
		},
		SuggestedSubscans: []InvestigationBranch{
			{ID: "bridge-follow-1", Question: "Verify the operating status and feedstock origin for AMG Bitterfeld", RequiresPath: true, GeoHints: []string{"Germany"}, ActorHints: []string{"AMG Lithium"}},
		},
	})

	if state.BridgeAttempts != 1 {
		t.Fatalf("expected bridgeAttempts=1, got %d", state.BridgeAttempts)
	}
	if state.LatestBridge.BranchID != "b1" {
		t.Fatalf("expected latest bridge on b1, got %+v", state.LatestBridge)
	}
	if len(state.PendingGraphOps) != 1 {
		t.Fatalf("expected one bridge op, got %d", len(state.PendingGraphOps))
	}
	if state.PendingGraphOps[0].BranchID != "b1" || state.PendingGraphOps[0].Status != "proposed" {
		t.Fatalf("unexpected bridge op state: %+v", state.PendingGraphOps[0])
	}
	if state.Hypotheses["b1"].PursuitDecision != BranchPursuitPress {
		t.Fatalf("expected branch to stay pressed after viable bridge, got %s", state.Hypotheses["b1"].PursuitDecision)
	}
	if state.Branches["bridge-follow-1"] == nil {
		t.Fatalf("expected suggested subscan branch to be created")
	}
	if state.Branches["bridge-follow-1"].Branch.DependsOn[0] != "b1" {
		t.Fatalf("expected suggested subscan to depend on b1, got %+v", state.Branches["bridge-follow-1"].Branch.DependsOn)
	}
}

func TestApplyBridgeRecommendationCreatesNeighborResearchBranch(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{
		Query:               "Trace lithium from Chile into Germany",
		TargetCanonicalPath: "/product/component/chemical/refining/lithium-hydroxide",
	}, InvestigationPlan{
		MaxResearchRounds: 3,
		Branches: []InvestigationBranch{
			{ID: "b1", Question: "Trace Chile carbonate into Germany", RequiresPath: true},
		},
	})
	state.Branches["b1"].Status = BranchStatusSupported
	state.Hypotheses["b1"].LocalReady = true

	applyBridgeToState(state, "b1", BridgeResult{
		BranchID:       "b1",
		BridgeQuestion: "What sits between SQM and BASF?",
		Summary:        "The current direct route is not validated.",
		BridgeExists:   false,
		RecommendedAction: ActionRecommendation{
			Action:       "research",
			BranchID:     "bridge-pivot-1",
			Reason:       "The direct route looks weak; inspect third-country converters instead.",
			FailureMode:  "route_invalid",
			Question:     "Which third-country converters transform Chilean lithium carbonate before it reaches German cathode demand?",
			ProductHints: []string{"/product/component/chemical/refining/lithium-carbonate", "/product/component/chemical/refining/lithium-hydroxide"},
			GeoHints:     []string{"Chile", "China", "Germany"},
			ActorHints:   []string{"SQM", "BASF"},
			RequiresPath: true,
		},
	})

	if state.LatestRecommendedAction.Action != "research" {
		t.Fatalf("expected latest recommendation to be research, got %+v", state.LatestRecommendedAction)
	}
	branch := state.Branches["bridge-pivot-1"]
	if branch == nil {
		t.Fatalf("expected recommended neighbor branch to be created")
	}
	if branch.Branch.Question == "" || len(branch.Branch.ProductHints) == 0 {
		t.Fatalf("expected recommended branch to carry question and hints, got %+v", branch.Branch)
	}
}

func TestCompactLatestStepOutputBridgeIncludesEndpoints(t *testing.T) {
	step := compactLatestStepOutput("bridge", map[string]any{
		"branchId":          "b1",
		"endpointA":         "/org/sqm",
		"endpointB":         "/fac/de/amg-lithium-bitterfeld",
		"bridgeQuestion":    "What sits between them?",
		"summary":           "A plausible bridge exists via maritime export and conversion.",
		"bridgeExists":      true,
		"intermediateNodes": []any{"/port/cl/antofagasta", "/port/nl/rotterdam"},
		"confidence":        0.71,
	})
	if step["endpointA"] != "/org/sqm" {
		t.Fatalf("expected endpointA in compact bridge step, got %#v", step)
	}
	if step["bridgeExists"] != true {
		t.Fatalf("expected bridgeExists=true, got %#v", step)
	}
}

func TestApplyPathToStateTurnsUnknownEmptyPathIntoBridgeGap(t *testing.T) {
	state := newInvestigationState(InvestigationRequest{}, InvestigationPlan{
		MaxResearchRounds: 2,
		Branches:          []InvestigationBranch{{ID: "b1", Question: "q", RequiresPath: true}},
	})
	state.Hypotheses["b1"].LocalReady = true
	applyPathToState(state, PathResult{
		ContinuityStatus: "unknown",
		SourceBranchIDs:  []string{"b1"},
	})
	if len(state.PathResult.OpenGaps) == 0 {
		t.Fatalf("expected unknown empty path to produce an open gap")
	}
	if state.PathResult.ContinuityStatus != "broken" {
		t.Fatalf("expected unknown empty path to downgrade to broken, got %q", state.PathResult.ContinuityStatus)
	}
	if !shouldAttemptBridgeScan(state) {
		t.Fatalf("expected bridge scan to become available after unknown empty path")
	}
}

func TestDerivePathRecommendedActionSuggestsBridgeOnBrokenContinuity(t *testing.T) {
	result := derivePathRecommendedAction(PathResult{
		QueriedProducts:  []string{"/product/component/chemical/refining/lithium-carbonate"},
		ContinuityStatus: "broken",
		OpenGaps:         []string{"No converter or logistics leg identified."},
		SourceBranchIDs:  []string{"b1"},
	})
	if result.Action != "bridge" || result.BranchID != "b1" {
		t.Fatalf("expected bridge recommendation on broken path, got %+v", result)
	}
	if result.Question == "" {
		t.Fatalf("expected bridge recommendation to carry a question, got %+v", result)
	}
}

func TestProcessBridgeSynthesizesArtifactsFromBridgeSteps(t *testing.T) {
	p := &Processor{}
	p.runModelObjectFn = func(_ context.Context, _ *agentmeshruntime.Service, _ *contracts.MeshTask, _ map[string]any) map[string]any {
		return map[string]any{
			"branchId":       "b1",
			"endpointA":      "/org/cl/sqm",
			"endpointB":      "/org/de/basf",
			"bridgeQuestion": "What sits between SQM and BASF?",
			"summary":        "The route runs through Chilean export, a European port, and a converter before BASF.",
			"bridgeExists":   true,
			"bridgeKind":     "logistics_to_conversion_corridor",
			"bridgeSteps": []any{
				map[string]any{
					"index":        1,
					"stepClass":    "port",
					"nodeRef":      "/port/cl/antofagasta",
					"nodeKind":     "port",
					"fromRef":      "/org/cl/sqm",
					"toRef":        "/port/cl/antofagasta",
					"relationType": "SUPPLY_LINK",
					"whyNeeded":    "SQM exports through Antofagasta.",
				},
				map[string]any{
					"index":             2,
					"stepClass":         "processor",
					"nodeRef":           "/fac/de/amg-lithium-bitterfeld",
					"nodeKind":          "facility",
					"fromRef":           "/port/cl/antofagasta",
					"toRef":             "/fac/de/amg-lithium-bitterfeld",
					"relationType":      "SUPPLY_LINK",
					"whyNeeded":         "Feedstock arrives to a converter before entering German cathode demand.",
					"operationalStatus": "unclear",
				},
				map[string]any{
					"index":        3,
					"stepClass":    "market_link",
					"fromRef":      "/fac/de/amg-lithium-bitterfeld",
					"toRef":        "/org/de/basf",
					"relationType": "SUPPLY_LINK",
					"whyNeeded":    "Converted material can supply BASF downstream.",
				},
			},
		}
	}
	task := &contracts.MeshTask{
		ID:         "bridge-task",
		Capability: "scout.bridge",
		Input: map[string]any{
			"request": map[string]any{"tenantId": "global", "query": "Trace lithium route"},
			"branch":  map[string]any{"id": "b1", "question": "What sits between SQM and BASF?", "role": "Supply Chain Graph Architect"},
			"plan":    map[string]any{},
			"bridgeContext": map[string]any{
				"bridgeQuestion": "What sits between SQM and BASF?",
				"endpointA":      "/org/cl/sqm",
				"endpointB":      "/org/de/basf",
			},
		},
	}
	res, err := p.processBridge(context.Background(), nil, task)
	if err != nil {
		t.Fatalf("processBridge err: %v", err)
	}
	if res == nil || res.Status != contracts.TaskStatusSucceeded {
		t.Fatalf("unexpected result: %#v", res)
	}
	out := decodeBridgeResult(res.Output)
	if len(out.BridgeSteps) != 3 {
		t.Fatalf("expected articulated bridge steps, got %+v", out.BridgeSteps)
	}
	if len(out.ProposedOps) < 3 {
		t.Fatalf("expected synthesized ops from bridge steps, got %+v", out.ProposedOps)
	}
	if len(out.SuggestedSubscans) == 0 {
		t.Fatalf("expected synthesized follow-up subscan from unclear operational status")
	}
}
