"""Canonicalizer I/O: fragments and edges shaped like core HorizonFragment / DependencyEdge."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from agenthorde_common.canonicalizer.taxonomy import Layer, LayerSection


class SourceRef(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    url: str = ""
    title: str = ""
    domain: str = ""


class Provenance(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    source_refs: list[SourceRef] = Field(default_factory=list, alias="sourceRefs")
    method: str = "agenthorde_canonicalizer"
    observed_at: str | None = Field(default=None, alias="observedAt")
    canonicalizer_version: str = Field(default="", alias="canonicalizerVersion")
    run_id: str = Field(default="", alias="runId")
    claim_ids: list[str] = Field(default_factory=list, alias="claimIds")


class MetricValue(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    value: float | int | str
    unit: str = ""
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    provenance: Provenance | None = None
    observed_at: str | None = Field(default=None, alias="observedAt")
    kind: Literal["magnitude", "directional"] = "magnitude"


class SiteTransform(BaseModel):
    """Site-centric transform: N inputs -> one output on a site node (not product-product edges)."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    inputs: list[str] = Field(default_factory=list)
    output_product: str = Field(alias="outputProduct")
    kind: Literal["transformation", "assembly", "usage"] = "transformation"
    validated: bool = True

    @model_validator(mode="before")
    @classmethod
    def _coerce_legacy_single_input(cls, data: Any) -> Any:
        if not isinstance(data, dict):
            return data
        out = dict(data)
        if "inputs" not in out and ("inputProduct" in out or "input_product" in out):
            ip = str(out.get("inputProduct") or out.get("input_product") or "").strip()
            out["inputs"] = [ip] if ip else []
        out.pop("inputProduct", None)
        out.pop("input_product", None)
        if "outputProduct" not in out and "output" in out:
            out["outputProduct"] = out.get("output") or ""
        out.pop("output", None)
        return out


class CanonicalFragment(BaseModel):
    """Maps to HorizonFragment: canonicalPath, type, flexible state/qualifiers."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    id: str
    canonical_path: str = Field(alias="canonicalPath")
    fragment_class: str = Field(alias="fragmentClass")  # first segment
    subtype: str = ""
    surface_forms: list[str] = Field(default_factory=list, alias="surfaceForms")
    attributes: dict[str, Any] = Field(default_factory=dict)
    metrics: dict[str, MetricValue] = Field(default_factory=dict)
    provenance: Provenance = Field(default_factory=Provenance)
    valid_from: str | None = Field(default=None, alias="validFrom")
    valid_to: str | None = Field(default=None, alias="validTo")
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    mention_count: int = Field(default=0, alias="mentionCount")
    site_transforms: list[SiteTransform] = Field(default_factory=list, alias="siteTransforms")


class CanonicalEdge(BaseModel):
    """Maps to DependencyEdge: source/target IDs, type, qualifiers, metrics."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    id: str
    source_fragment_id: str = Field(alias="sourceFragmentId")
    target_fragment_id: str = Field(alias="targetFragmentId")
    relation: str
    layer: Layer = "structural"
    layer_section: LayerSection | str = Field(default="flow_topology", alias="layerSection")
    attributes: dict[str, Any] = Field(default_factory=dict)
    qualifiers: dict[str, Any] = Field(default_factory=dict)
    metrics: dict[str, MetricValue] = Field(default_factory=dict)
    provenance: Provenance = Field(default_factory=Provenance)
    valid_from: str | None = Field(default=None, alias="validFrom")
    valid_to: str | None = Field(default=None, alias="validTo")
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    source_claim_ids: list[str] = Field(default_factory=list, alias="sourceClaimIds")


class CanonicalGraph(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    fragments: list[CanonicalFragment] = Field(default_factory=list)
    edges: list[CanonicalEdge] = Field(default_factory=list)


# -----------------------------------------------------------------------------
# Evidence archive: audit-trail layer, kept OUT of the graph.
#
# Rationale: source URLs retrieved by web_search are provenance substrate,
# not ontology nodes. They belong in object storage + a foreign-key join table,
# not in the canonical graph. Persisting them alongside the graph keeps the
# unit-of-transfer convenient for dev, but the split makes it explicit that
# the graph-apply path and the evidence-apply path are different.
# -----------------------------------------------------------------------------


class EvidenceSource(BaseModel):
    """One web-retrieved document. Keyed by stable hash of the URL."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    id: str  # sha256(url)[:16] — stable across runs for the same URL
    url: str
    domain: str = ""
    title: str = ""


class EvidenceLink(BaseModel):
    """Join record between a claim and a supporting EvidenceSource."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    claim_id: str = Field(alias="claimId")
    source_id: str = Field(alias="sourceId")
    relation: Literal["asserted_by", "supported_by"]
    score: int = 0  # fuzz.partial_ratio(claim_text, web_search.answer) at link time
    rank: int = 0  # 0 = primary citation (asserted_by), >=1 = fan-out order


# Claim / hypothesis / counter anchors live OUTSIDE the graph substrate. Previously
# they were materialized as /evidence/* fragments with supported_by edges back into
# the graph; that inflated edge counts (≥ half of all edges) with non-structural links.
# Now they are first-class records in the archive, and business edges reference them
# via CanonicalEdge.source_claim_ids (already persisted there).


class EvidenceClaim(BaseModel):
    """A reasoning-claim anchor retained for audit, not as a graph fragment."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    id: str
    text: str = ""
    hypothesis_id: str | None = Field(default=None, alias="hypothesisId")


class EvidenceHypothesis(BaseModel):
    """A hypothesis anchor retained for audit, not as a graph fragment."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    id: str
    text: str = ""


class EvidenceCounter(BaseModel):
    """A counter-claim anchor. contradicts_claim_id pins the claim it rebuts."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    id: str
    text: str = ""
    contradicts_claim_id: str | None = Field(default=None, alias="contradictsClaimId")


class EvidenceArchive(BaseModel):
    """Source-document records + claim/hypothesis/counter anchors; not part of CanonicalGraph."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    sources: list[EvidenceSource] = Field(default_factory=list)
    links: list[EvidenceLink] = Field(default_factory=list)
    claims: list[EvidenceClaim] = Field(default_factory=list)
    hypotheses: list[EvidenceHypothesis] = Field(default_factory=list)
    counters: list[EvidenceCounter] = Field(default_factory=list)


class UnknownEntityTypeCandidate(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    surface: str
    reason: str = ""
    claim_refs: list[str] = Field(default_factory=list, alias="claimRefs")


class UnknownRelationCandidate(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    relation: str
    from_id: str = Field(alias="fromId")
    to_id: str = Field(alias="toId")
    reason: str = ""


class RegistryCoverageIssue(BaseModel):
    """Phase 1 registry row neither linked by a kept mention nor listed as skipped."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    agent_registry_id: str = Field(alias="agentRegistryId")
    label: str = ""
    reason: str = ""


class UnknownTransformCandidate(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    site_fragment_id: str = Field(alias="siteFragmentId")
    from_product: str = Field(alias="fromProduct")
    to_product: str = Field(alias="toProduct")


class UnknownProductCandidate(BaseModel):
    """Unresolved cargo on a supplies edge (pre- or post-remap fragment ids)."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    surface: str
    from_site_fragment_id: str = Field(alias="fromSiteFragmentId")
    to_site_fragment_id: str | None = Field(default=None, alias="toSiteFragmentId")
    reason: str = ""


class MergeCandidate(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    entity_ids: list[str] = Field(alias="entityIds")
    surfaces: list[str] = Field(default_factory=list)
    score: float = 0.0
    reason: str = ""


class Candidates(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    unknown_entity_types: list[UnknownEntityTypeCandidate] = Field(
        default_factory=list, alias="unknownEntityTypes"
    )
    unknown_relations: list[UnknownRelationCandidate] = Field(
        default_factory=list, alias="unknownRelations"
    )
    unknown_transforms: list[UnknownTransformCandidate] = Field(
        default_factory=list, alias="unknownTransforms"
    )
    unknown_products: list[UnknownProductCandidate] = Field(default_factory=list, alias="unknownProducts")
    merge_candidates: list[MergeCandidate] = Field(default_factory=list, alias="mergeCandidates")
    registry_coverage_issues: list[RegistryCoverageIssue] = Field(
        default_factory=list, alias="registryCoverageIssues"
    )


class ValidationIssue(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    code: str
    message: str = ""
    detail: dict[str, Any] = Field(default_factory=dict)


class ValidationReport(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    input_errors: list[ValidationIssue] = Field(default_factory=list, alias="inputErrors")
    path_errors: list[ValidationIssue] = Field(default_factory=list, alias="pathErrors")
    relation_errors: list[ValidationIssue] = Field(default_factory=list, alias="relationErrors")
    relation_kind_warnings: list[ValidationIssue] = Field(
        default_factory=list, alias="relationKindWarnings"
    )
    layer_errors: list[ValidationIssue] = Field(default_factory=list, alias="layerErrors")
    transform_errors: list[ValidationIssue] = Field(default_factory=list, alias="transformErrors")
    orphan_nodes: list[str] = Field(default_factory=list, alias="orphanNodes")
    dictionary_warnings: list[str] = Field(default_factory=list, alias="dictionaryWarnings")


class CanonicalizerProvenance(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    run_id: str = Field(alias="runId")
    canonicalizer_version: str = Field(alias="canonicalizerVersion")
    agent_run_ref: str = Field(default="", alias="agentRunRef")
    phase_timings_ms: dict[str, float] = Field(default_factory=dict, alias="phaseTimingsMs")


class CanonicalizerInput(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    pack: dict[str, Any] = Field(default_factory=dict)
    analyzer_output: dict[str, Any] = Field(default_factory=dict, alias="analyzerOutput")
    governance_output: dict[str, Any] = Field(default_factory=dict, alias="governanceOutput")
    sanctions_output: dict[str, Any] = Field(
        default_factory=dict,
        alias="sanctionsOutput",
        description=(
            "Sanctions analyzer output blob. Same shape as analyzer_output / governance_output "
            "(chainSummary, proposedEntities, proposedRelations, proposedNodes, openQuestions). "
            "Added when sanctions_analyzer was added to the agent roster — without this field "
            "the canonicalizer silently drops every entity/relation that originated from "
            "sanctions analysis (only pack-merged ones survive)."
        ),
    )
    decision_influence_output: dict[str, Any] = Field(
        default_factory=dict,
        alias="decisionInfluenceOutput",
        description=(
            "Decision & influence analyzer output blob. Same shape as the other analyzer outputs. "
            "Carries decision/mechanism fragments and gatekeeps/wields/enacted_via/imposes/influences "
            "relations. Without this field, the canonicalizer drops everything that came from the "
            "decision_influence_analyzer (only pack-merged copies survive)."
        ),
    )
    tool_ledger: list[dict[str, Any]] = Field(default_factory=list, alias="toolLedger")
    final_answer: str = Field(default="", alias="finalAnswer")
    run_id: str = Field(default="", alias="runId")


class CanonicalizerResult(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    schema_version: int = Field(default=2, alias="schemaVersion")
    graph: CanonicalGraph
    evidence_archive: EvidenceArchive = Field(default_factory=EvidenceArchive, alias="evidenceArchive")
    candidates: Candidates = Field(default_factory=Candidates)
    validation_report: ValidationReport = Field(default_factory=ValidationReport, alias="validationReport")
    provenance: CanonicalizerProvenance
    follow_up_plan: dict[str, Any] = Field(
        default_factory=dict,
        alias="followUpPlan",
        description="Aggregate mentions and deferred work; populated by phase 9 when applicable.",
    )


# --- Internal pipeline models ---


class ContextHints(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    iso2_hint: str | None = Field(default=None, alias="iso2Hint")
    sector_hint: str | None = Field(default=None, alias="sectorHint")
    as_of_hint: str | None = Field(default=None, alias="asOfHint")


class EntityMention(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)

    mention_id: str = ""
    # When this mention backs an upstream analyzer registry row (namespaced id, e.g. "analyzer:e1").
    agent_registry_id: str | None = Field(default=None, alias="agentRegistryId")
    surface: str
    type: str | None = None  # fragment class or None
    subtype: str | None = None
    claim_refs: list[str] = Field(default_factory=list, alias="claimRefs")
    context_hints: ContextHints = Field(default_factory=ContextHints, alias="contextHints")
    per_claim_confidence: float = Field(default=0.7, ge=0.0, le=1.0, alias="perClaimConfidence")
    attributes: dict[str, Any] = Field(default_factory=dict)


class CanonicalEntity(BaseModel):
    """After dedup, before final fragment id assignment."""

    model_config = ConfigDict(extra="allow", populate_by_name=True)

    entity_id: str = Field(alias="entityId")
    type: str
    subtype: str | None = None
    surface_forms: list[str] = Field(default_factory=list, alias="surfaceForms")
    mention_count: int = Field(default=0, alias="mentionCount")
    merge_reason: str = Field(default="exact", alias="mergeReason")
    claim_refs: list[str] = Field(default_factory=list, alias="claimRefs")
    context_hints: ContextHints = Field(default_factory=ContextHints, alias="contextHints")
    per_claim_confidence: float = Field(default=0.7, ge=0.0, le=1.0, alias="perClaimConfidence")
    merge_candidate: bool = Field(default=False, alias="mergeCandidate")
    canonical_path: str = Field(default="", alias="canonicalPath")
    attributes: dict[str, Any] = Field(default_factory=dict)
