from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class SourceRef(StrictModel):
    id: str = ""
    title: str = ""
    url: str = ""
    provenance: str = ""


class ToolDiagnostic(StrictModel):
    code: str = ""
    severity: str = ""
    message: str = ""


class ToolOutcome(StrictModel):
    ok: bool = False
    status: str = ""
    retryable: bool = False
    errorCode: str = ""
    message: str = ""
    data: dict[str, Any] = Field(default_factory=dict)
    diagnostics: list[ToolDiagnostic] = Field(default_factory=list)
    truncated: bool = False
    promptInjectionSuspected: bool = False
    sourceRefs: list[SourceRef] = Field(default_factory=list)


class PlanBranch(StrictModel):
    id: str = ""
    question: str = ""
    role: str = ""
    productHints: list[str] = Field(default_factory=list)
    geoHints: list[str] = Field(default_factory=list)
    actorHints: list[str] = Field(default_factory=list)
    requiresPath: bool = False
    dependsOn: list[str] = Field(default_factory=list)
    testCriteria: list[str] = Field(default_factory=list)
    expectedInfoGain: float = 0.0


class PlanSection(StrictModel):
    goal: str = ""
    focusProducts: list[str] = Field(default_factory=list)
    focusGeos: list[str] = Field(default_factory=list)
    focusActors: list[str] = Field(default_factory=list)
    wantsPathMaterialize: bool = False
    maxResearchRounds: int = 0
    planningRationale: list[str] = Field(default_factory=list)
    materializationStrategy: str = ""
    branches: list[PlanBranch] = Field(default_factory=list)


class PendingOp(StrictModel):
    id: str = ""
    kind: str = ""
    branchId: str = ""
    status: str = ""
    rationale: str = ""


class StateSection(StrictModel):
    status: str = ""
    turn: int = 0
    maxTurns: int = 0
    activeFrontier: list[str] = Field(default_factory=list)
    knownEndpoints: list[str] = Field(default_factory=list)
    pendingOps: list[PendingOp] = Field(default_factory=list)
    noProgressStreak: int = 0
    recentQueries: list[str] = Field(default_factory=list)
    recentFailures: list[str] = Field(default_factory=list)
    candidateGaps: list[str] = Field(default_factory=list)
    stopReason: str = ""


class ActionSection(StrictModel):
    type: str = ""
    reason: str = ""
    terminal: bool = False
    nextCapability: str = ""
    nextBranchIds: list[str] = Field(default_factory=list)
    reviewRequired: bool = False


class RuntimeDiagnosis(StrictModel):
    code: str = ""
    severity: str = ""
    message: str = ""


class EvidenceLedgerItem(StrictModel):
    id: str = ""
    claim: str = ""
    sourceRefIds: list[str] = Field(default_factory=list)
    provenance: str = ""
    confidence: float = 0.0
    freshness: str = ""


class UncertaintyLedgerItem(StrictModel):
    id: str = ""
    topic: str = ""
    summary: str = ""
    impact: str = ""
    nextCheck: str = ""


class DecisionLedgerItem(StrictModel):
    id: str = ""
    phase: str = ""
    branchId: str = ""
    decision: str = ""
    why: str = ""
    evidenceRefs: list[str] = Field(default_factory=list)
    uncertaintyRefs: list[str] = Field(default_factory=list)
    confidence: float = 0.0
    nextAction: str = ""


class ReasoningStep(StrictModel):
    phase: str = ""
    branchId: str = ""
    decision: str = ""
    why: str = ""
    evidenceRefs: list[str] = Field(default_factory=list)
    uncertaintyRefs: list[str] = Field(default_factory=list)
    confidence: float = 0.0
    nextAction: str = ""


class Review(StrictModel):
    accepted: bool = False
    rationale: str = ""
    quality: str = ""
    issues: list[str] = Field(default_factory=list)


class TaskRefs(StrictModel):
    decide: list[str] = Field(default_factory=list)
    research: list[str] = Field(default_factory=list)
    bridge: list[str] = Field(default_factory=list)
    critic: list[str] = Field(default_factory=list)
    path: list[str] = Field(default_factory=list)
    materialize: list[str] = Field(default_factory=list)
    aggregate: list[str] = Field(default_factory=list)
    review: list[str] = Field(default_factory=list)


class ResearchSummary(StrictModel):
    summary: str = ""
    branchIds: list[str] = Field(default_factory=list)
    hitCount: int = 0
    sourceRefIds: list[str] = Field(default_factory=list)


class CriticSummary(StrictModel):
    summary: str = ""
    confidence: float = 0.0
    openQuestions: list[str] = Field(default_factory=list)
    recommendedAction: str = ""


class PathSummary(StrictModel):
    summary: str = ""
    continuityStatus: str = ""
    artifactIds: list[str] = Field(default_factory=list)
    openGaps: list[str] = Field(default_factory=list)


class MaterializeSummary(StrictModel):
    summary: str = ""
    persisted: bool = False
    artifactIds: list[str] = Field(default_factory=list)
    rejected: list[str] = Field(default_factory=list)


class AggregateSummary(StrictModel):
    summary: str = ""
    findings: list[str] = Field(default_factory=list)
    openQuestions: list[str] = Field(default_factory=list)
    artifactIds: list[str] = Field(default_factory=list)
    candidateProducts: list[str] = Field(default_factory=list)
    recommendedNextStep: str = ""


class ScoutTaskSnapshot(StrictModel):
    plan: PlanSection = Field(default_factory=PlanSection)
    state: StateSection = Field(default_factory=StateSection)
    action: ActionSection = Field(default_factory=ActionSection)
    research: ResearchSummary = Field(default_factory=ResearchSummary)
    critic: CriticSummary = Field(default_factory=CriticSummary)
    path: PathSummary = Field(default_factory=PathSummary)
    materialize: MaterializeSummary = Field(default_factory=MaterializeSummary)
    aggregate: AggregateSummary = Field(default_factory=AggregateSummary)
    review: Review = Field(default_factory=Review)
    reasoningTrace: list[ReasoningStep] = Field(default_factory=list)
    runtimeDiagnosis: list[RuntimeDiagnosis] = Field(default_factory=list)
    evidenceLedger: list[EvidenceLedgerItem] = Field(default_factory=list)
    uncertaintyLedger: list[UncertaintyLedgerItem] = Field(default_factory=list)
    decisionLedger: list[DecisionLedgerItem] = Field(default_factory=list)
    reasoningSummary: str = ""
    sessionRef: str = ""
    memoryBankRef: str = ""
    taskRefs: TaskRefs = Field(default_factory=TaskRefs)


ScoutEnvelopeV2 = ScoutTaskSnapshot
ScoutEnvelope = ScoutTaskSnapshot
