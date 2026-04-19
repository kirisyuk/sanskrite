from __future__ import annotations

import json
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from agenthorde_common.schemas.graph import ReasoningNode
from agenthorde_common.schemas.state import SupervisorNextAgent


def decode_wire_relation_attributes(attrs: Any) -> dict[str, Any]:
    """Normalize ``proposedRelations[].attributes`` from JSON.

    Accepts a legacy object map or a strict list of ``{key, value}`` rows (values are
    JSON text from :func:`json.dumps` for non-strings).
    """

    if attrs is None:
        return {}
    if isinstance(attrs, dict):
        return dict(attrs)
    if isinstance(attrs, list):
        out: dict[str, Any] = {}
        for item in attrs:
            if not isinstance(item, dict):
                continue
            k = str(item.get("key") or "").strip()
            if not k:
                continue
            raw_v = item.get("value", "")
            if isinstance(raw_v, str):
                if raw_v == "":
                    out[k] = ""
                    continue
                try:
                    out[k] = json.loads(raw_v)
                except json.JSONDecodeError:
                    out[k] = raw_v
            else:
                out[k] = raw_v
        return out
    return {}


class RelationAttributeEntry(BaseModel):
    """Strict JSON-Schema-friendly bag entry (Azure / OpenAI ``strict: true``)."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    key: str
    value: str = ""


def _encode_relation_attr_value(v: Any) -> str:
    return json.dumps(v, ensure_ascii=False)


def _relation_entries_from_mapping(m: dict[str, Any]) -> list[RelationAttributeEntry]:
    return [
        RelationAttributeEntry(key=k, value=_encode_relation_attr_value(v))
        for k, v in sorted(m.items(), key=lambda kv: kv[0])
    ]


class SupervisorDecision(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    next_agent: SupervisorNextAgent = Field(alias="nextAgent")
    mission: str = ""
    research_temperature: float = Field(
        default=0.35,
        alias="researchTemperature",
        ge=0.0,
        le=1.0,
    )
    rationale: str = ""
    parallel_agents: list[SupervisorNextAgent] = Field(
        default_factory=list,
        alias="parallelAgents",
        description=(
            "If non-empty, run these analyzers concurrently on this cycle. "
            "next_agent MUST be 'parallel_fanout' whenever this list is populated."
        ),
    )
    extend_cap_by: int = Field(
        default=0,
        alias="extendCapBy",
        ge=0,
        le=3,
        description=(
            "Supervisor-requested increase to the per-run supervisor cap, expressed as "
            "10% of the initial cap per unit (cap at 3 units = +30%). Honored only when "
            "red_team_agent has run or will run next."
        ),
    )


class EscalationHint(BaseModel):
    """Analyzer → supervisor hand-off signal.

    Emitted by analyzers (currently ``supply_chain_analyzer``) when the
    question touches a sub-domain that is better served by a specialist
    agent on the next cycle. The supervisor_router is instructed to prefer
    the ``suggested_agent`` of the most recent analyzer output unless it
    conflicts with a hard gate (cap reached, red-team owed, finalize due).
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    reason: str
    suggested_agent: Literal[
        "sanctions_analyzer",
        "governance_control_analyzer",
        "red_team_agent",
    ] = Field(alias="suggestedAgent")
    confidence: float = Field(default=0.5, ge=0.0, le=1.0)


class ProposedEntity(BaseModel):
    """Stable object id + verbatim label from the analyzer's own narrative (registry row)."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    id: str
    label: str


class ProposedRelation(BaseModel):
    """Structural edge between registry ids (same AnalyzerOutput scope).

    Consumed by the canonicalizer as a pre-pass before the Phase 4 LLM call; supplies
    the canonical pipeline with edges the analyzer already saw, so Phase 4 only needs
    to extend coverage rather than re-derive everything from prose.
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    src_id: str = Field(alias="srcId")
    dst_id: str = Field(alias="dstId")
    relation: str
    attributes: list[RelationAttributeEntry] = Field(default_factory=list)
    source_claim_ids: list[str] = Field(default_factory=list, alias="sourceClaimIds")

    @model_validator(mode="before")
    @classmethod
    def _coerce_attributes_from_wire(cls, data: Any) -> Any:
        if not isinstance(data, dict):
            return data
        attrs = data.get("attributes")
        if isinstance(attrs, dict):
            data = {**data, "attributes": _relation_entries_from_mapping(attrs)}
        elif attrs is None:
            data = {**data, "attributes": []}
        return data

    def attributes_as_dict(self) -> dict[str, Any]:
        """Decoded attribute map (same shape as legacy ``dict[str, Any]``)."""

        return decode_wire_relation_attributes([e.model_dump(mode="python") for e in self.attributes])


class ScopeArea(BaseModel):
    """One self-declared subsystem the analyzer commits to cover in this pass.

    Names are free-form (chosen by the analyzer for THIS question); we deliberately
    do NOT impose a fixed taxonomy like ``tier1``/``tier2`` — the analyzer reasons
    about depth from the question, not from a hardcoded ladder.
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    name: str = Field(
        description=(
            "Subsystem / area name chosen by the analyzer (e.g. 'lithography equipment', "
            "'OSAT (advanced packaging)', 'EDA tools', 'photomask supply', 'sanctions regimes')."
        )
    )
    why_relevant: str = Field(
        alias="whyRelevant",
        description="One sentence linking this area to something in the user question or mission.",
    )
    expected_evidence_kinds: list[str] = Field(
        default_factory=list,
        alias="expectedEvidenceKinds",
        description=(
            "Categories of evidence the analyzer plans to surface here "
            "(e.g. 'vendor names', 'capacity numbers', 'license records', 'as-of dates')."
        ),
    )


class ScopePlan(BaseModel):
    """Analyzer's self-declared coverage contract for THIS pass.

    Replaces the old "exhaustive coverage discipline" approach: instead of the
    prompt mandating every Tier-walk and every taxonomy axis, the analyzer
    reasons about which subsystems matter for this question and commits to
    them. The critic then judges adequacy and execution fidelity against
    this plan rather than against an open-ended quantifier.

    On follow-up calls (when ``pack.reasoningGraph`` is non-empty), the
    analyzer is expected to EXTEND its prior scope plan with new focus
    areas dictated by ``ah_mission`` / critic's recommendedScopeFocus.
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    focus_areas: list[ScopeArea] = Field(
        default_factory=list,
        alias="focusAreas",
        description="Subsystems the analyzer obliges itself to substantively cover this pass.",
    )
    depth_hint: Literal["narrow", "standard", "deep"] = Field(
        default="standard",
        alias="depthHint",
        description=(
            "Self-assessment of how deep this pass goes per area: narrow=top entities only, "
            "standard=primary dependencies, deep=multi-tier dependency walks."
        ),
    )
    deliberate_omissions: list[str] = Field(
        default_factory=list,
        alias="deliberateOmissions",
        description=(
            "Areas the analyzer consciously skips this pass with one-line justification each "
            "(e.g. 'utilities and chemicals — not relevant to a lithography-tier question'). "
            "Critic distinguishes deliberate omission from accidental gap."
        ),
    )
    rationale: str = Field(
        default="",
        description=(
            "One or two sentences explaining how this scope follows from the user question / mission. "
            "On follow-up calls, must explain how the scope was extended vs prior pass."
        ),
    )


class AnalyzerOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    chain_summary: str = Field(alias="chainSummary")
    scope_plan: ScopePlan | None = Field(
        default=None,
        alias="scopePlan",
        description=(
            "Self-declared coverage contract for THIS pass. REQUIRED on first pass; on "
            "follow-up calls, MUST extend rather than restate the previous scope. "
            "The critic uses this to score adequacy (does scope match the question?) "
            "and execution fidelity (did the analyzer actually populate what it declared?)."
        ),
    )
    proposed_entities: list[ProposedEntity] = Field(
        default_factory=list,
        alias="proposedEntities",
        description=(
            "Entity registry: stable ids + labels verbatim from chainSummary / proposedNodes text. "
            "On follow-up calls (pack.reasoningGraph non-empty) emit ONLY new entities you want to "
            "add to the registry; merge layer will not duplicate ids that already exist."
        ),
    )
    proposed_nodes: list[ReasoningNode] = Field(default_factory=list, alias="proposedNodes")
    proposed_relations: list[ProposedRelation] = Field(
        default_factory=list,
        alias="proposedRelations",
        description=(
            "Relations between registry ids. On follow-up calls, emit ONLY new relations; do not "
            "restate edges already present in pack.proposedRelations."
        ),
    )
    open_questions: list[str] = Field(default_factory=list, alias="openQuestions")
    escalation_hints: list[EscalationHint] = Field(
        default_factory=list,
        alias="escalationHints",
        description=(
            "Suggestions for the supervisor to route to a specialist analyzer "
            "(sanctions, person-heavy governance, red-team reframe) on the next cycle."
        ),
    )

    @model_validator(mode="after")
    def _relations_reference_registry_ids(self) -> AnalyzerOutput:
        """Referential integrity for relations.

        On a FIRST PASS, every srcId/dstId must appear in this output's own
        ``proposedEntities`` registry (analyzer-local ids like ``e1``, ``e2``).

        On a FOLLOW-UP PASS, the analyzer is allowed to point at entities
        already living in the pack-level registry from prior cycles. Those
        references arrive in one of two shapes:

         * **Namespaced** — ``analyzer:e6`` / ``governance:e3`` / ``sanctions:e1``
           — copied verbatim from ``pack.proposedEntities[].namespacedId``.
         * **Pack fragment** — ``n12`` etc., from ``pack.reasoningGraph.nodes[].id``
           (rare; relations to fragments are usually created by the canonicalizer,
           but we accept them rather than reject silently).

        We accept both as cross-document references and let the merge layer
        validate they actually exist in the pack. The local registry contract
        still holds for any id that doesn't carry a namespace prefix and isn't
        an obvious pack-fragment id.
        """
        reg = {(e.id or "").strip() for e in self.proposed_entities if (e.id or "").strip()}
        cross_doc_prefixes = ("analyzer:", "governance:", "sanctions:", "pack:")
        for r in self.proposed_relations:
            for label, ref_id in (("srcId", (r.src_id or "").strip()),
                                  ("dstId", (r.dst_id or "").strip())):
                if not ref_id:
                    continue
                if ref_id in reg:
                    continue
                # Cross-document reference (namespaced or pack-fragment shape).
                if ref_id.startswith(cross_doc_prefixes):
                    continue
                if ref_id and ref_id[0] == "n" and ref_id[1:].isdigit():
                    # Pack reasoning-graph fragment id (n1, n12, ...).
                    continue
                raise ValueError(f"proposedRelations {label} {ref_id!r} not in proposedEntities")
        return self


GapClass = Literal[
    # ---- Generic graph-quality classes (domain-neutral) ----
    "generic.node_depth",
    "generic.node_breadth",
    "generic.node_tier_jump",
    "generic.edge_existence",
    "generic.edge_semantics",
    "generic.node_attribution",
    "generic.resolution",
    "generic.cross_subgraph",
    "generic.counterfactual",
    "generic.conditional",
    "generic.confidence",
    # ---- Supply-chain-specific classes ----
    "supply_chain.tier_resolution",
    "supply_chain.product_on_edge",
    "supply_chain.transformation_process",
    "supply_chain.process_granularity",
    "supply_chain.mode_route",
    "supply_chain.capacity_share",
    "supply_chain.substitutability",
    "supply_chain.lead_time",
    "supply_chain.inter_tier_control",
    # ---- Governance / corporate-control classes ----
    "governance.owner_vs_ubo",
    "governance.layering",
    "governance.nominee",
    "governance.voting_vs_economic",
    "governance.affiliation",
    "governance.interlocking_directorates",
    "governance.jurisdiction_incorporation",
    "governance.trustee_spv",
    "governance.proximity_to_listed",
    # ---- Compliance-specific classes (regimes, designations, licenses) ----
    "compliance.regime_distinctness",
    "compliance.designation_lifecycle",
    "compliance.license_exemption",
    "compliance.conditional_applicability",
    "compliance.secondary_effect",
    "compliance.territorial_scope",
    "compliance.carve_out",
    "compliance.enforcement_body",
    "compliance.wind_down",
    # ---- Methodology classes (no specific object cited) ----
    "methodology.scope_adequacy",
    "methodology.execution_fidelity",
    "methodology.framing_bias",
    "methodology.composition_inconsistency",
]


class CriticFinding(BaseModel):
    """Single critic finding (critical or advisory) — prose summary + ids + class.

    ``gap_class`` is a typed code drawn from :data:`GapClass`. The four-prefix
    convention (``generic.*`` / ``supply_chain.*`` / ``governance.*`` /
    ``compliance.*`` / ``methodology.*``) lets downstream consumers route
    findings by domain without re-parsing free-form prose, and makes it
    explicit which slice of the critic's typology produced the finding.

    ``refs`` cites ids from the registry block injected into the critic prompt
    (entity ids like ``sc_e1`` / ``gov_e2``, reasoning-node ids like
    ``sc_c1``, or session-pack fragment ids like ``n10`` / ``n11``). Empty
    ``refs`` is allowed only for ``methodology.*`` findings that do not point
    at a specific object (e.g. ``methodology.framing_bias``,
    ``methodology.scope_adequacy``).
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    summary: str
    refs: list[str] = Field(default_factory=list)
    gap_class: GapClass = Field(
        alias="gapClass",
        description=(
            "Typed code from the GapClass enum identifying which class of "
            "incompleteness the finding belongs to. Required on every finding so "
            "downstream tooling (and the supervisor's routing) can dispatch by "
            "domain prefix without parsing prose. Pick the most specific code; "
            "fall back to a generic.* code only when no domain-specific class "
            "fits. Use methodology.* when the finding is about the analyzer's "
            "process rather than a graph object."
        ),
    )

    @model_validator(mode="after")
    def _refs_required_for_object_findings(self) -> CriticFinding:
        """Empty ``refs`` only acceptable for ``methodology.*`` findings.

        Every other gap class points at a graph object (node, edge, subgraph,
        path), so leaving ``refs=[]`` would orphan the finding from the
        registry it claims to critique. Methodology findings are about the
        analyzer's process and may legitimately not cite a specific object.
        """

        if not self.refs and not self.gap_class.startswith("methodology."):
            raise ValueError(
                f"refs=[] is only allowed for methodology.* findings; "
                f"gap_class={self.gap_class!r} requires at least one ref id "
                f"from the critic's object registry"
            )
        return self


CriticRecommendedNextAgent = Literal[
    "supply_chain_analyzer",
    "governance_control_analyzer",
    "sanctions_analyzer",
    "red_team_agent",
    "critic_agent",
    "none",
]


class ScopeAssessment(BaseModel):
    """Critic's two-axis judgement of the analyzer's self-declared scope plan.

    Replaces the old open-ended "is everything covered?" check. Two narrow
    questions are easier for any model (mini or frontier) to answer reliably:

    * **Adequacy** — does the scope chosen by the analyzer match what the
      user actually asked? Catches cases where the analyzer pruned too
      aggressively (e.g. semi question with no OSAT layer in scope).
    * **Execution fidelity** — did the analyzer actually populate what it
      itself declared? Catches cases where scope is good but execution is
      thin (areas declared but no substantive entities surfaced).
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    scope_adequate: bool = Field(
        alias="scopeAdequate",
        description=(
            "True when the analyzer's declared scope plan covers what the user question "
            "implies, accounting for the question plan and named entities."
        ),
    )
    scope_gaps: list[str] = Field(
        default_factory=list,
        alias="scopeGaps",
        description=(
            "Subsystems / areas the analyzer's scope omitted but that the question requires. "
            "Each entry is a free-form area name to add (e.g. 'OSAT', 'photomask supply', "
            "'OFAC SDN exposure of beneficial owners')."
        ),
    )
    execution_fidelity: bool = Field(
        alias="executionFidelity",
        description=(
            "True when the analyzer actually populated entities/relations for every "
            "area declared in its scope plan. False when scope is good but coverage is thin."
        ),
    )
    fidelity_gaps: list[str] = Field(
        default_factory=list,
        alias="fidelityGaps",
        description=(
            "Areas the analyzer declared in scope but failed to populate substantively. "
            "Each entry references a focus_areas[].name from the analyzer's scope plan."
        ),
    )


DepthAction = Literal[
    "walk_one_level_deeper",
    "broaden_at_current_level",
    "anchor_evidence",
    "stop_deepening",
]


class RecommendedScopeFocus(BaseModel):
    """Critic-emitted structured directive for the next analyzer pass.

    The supervisor reads this when routing to the next worker. Replaces the
    prior "prose mission" with a structured contract the analyzer can read
    without re-interpreting natural language.

    The ``recommended_depth_action`` field makes **depth control** an explicit
    part of the critic's output rather than a hardcoded "always walk to
    Tier-2" rule in the prompt. Critic decides depth from (a) the user
    question, (b) what is already in the pack, (c) execution fidelity of the
    last pass — not from a predetermined ladder.
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    target_agent: Literal[
        "supply_chain_analyzer",
        "governance_control_analyzer",
        "sanctions_analyzer",
        "red_team_agent",
        "none",
    ] = Field(
        default="none",
        alias="targetAgent",
        description="Which analyzer / agent should consume this focus on the next cycle.",
    )
    add_focus_areas: list[str] = Field(
        default_factory=list,
        alias="addFocusAreas",
        description=(
            "Names of LAYERS / SUBSYSTEMS the next pass must cover — categories of "
            "analysis, NOT vendor / person / regime names. "
            "Good: ['OSAT advanced packaging', 'photomask supply layer behind TSMC', "
            "'UBO chain beyond registered owner']. "
            "Bad: ['ZEISS', 'Cymer', 'Trumpf'] — those are answers the analyzer must "
            "discover; if such names belong here, replace with the layer name and "
            "(if the surface entity is already in the registry) move the proper noun "
            "to deepen_entity_ids, or to directive_text as a single 'e.g. <name>' anchor. "
            "The critic describes the SHAPE of what is missing; the analyzer discovers "
            "the entities."
        ),
    )
    deepen_entity_ids: list[str] = Field(
        default_factory=list,
        alias="deepenEntityIds",
        description=(
            "Pack-level registry ids of surface entities the analyzer ALREADY named "
            "(analyzer:e3, governance:e7, n12, …) that the next pass should dig "
            "BEHIND. This field exists precisely so the critic can point ('walk one "
            "layer deeper than THIS one') without having to name the deeper entity "
            "itself — that is the analyzer's job. Use ids verbatim from the Object "
            "registry; do NOT invent ids for entities the analyzer has not yet emitted."
        ),
    )
    evidence_gaps: list[str] = Field(
        default_factory=list,
        alias="evidenceGaps",
        description=(
            "KINDS of evidence missing — categories of artifacts, optionally narrowed "
            "to a layer. Good: ['as-of date for sanctions designations', "
            "'primary-filing URL for ownership claims', 'vendor identifiers in the "
            "lithography sub-supplier layer']. Bad: ['confirmation that ZEISS SMT "
            "makes the EUV lenses'] — that prescribes a specific fact rather than the "
            "kind of evidence missing. domain_priors_lookup results MUST NOT be "
            "copied verbatim here; use them only to detect that a layer is missing, "
            "then describe the layer / evidence kind abstractly."
        ),
    )
    recommended_depth_action: DepthAction | None = Field(
        default=None,
        alias="recommendedDepthAction",
        description=(
            "Explicit depth-control directive for the next pass — the critic decides this from "
            "the question and the current pack, NOT from a hardcoded 'always go to Tier-N' rule. "
            "Choose:\n"
            " * 'walk_one_level_deeper' — the analysis named a surface entity (monopoly / "
            "single jurisdiction / sole supplier); push the next pass to identify the deeper "
            "dependency that is the actual chokepoint. Pair with 'deepenEntityIds' listing "
            "which surface entities to walk behind.\n"
            " * 'broaden_at_current_level' — depth is fine but coverage at the current level "
            "is uneven (e.g. one Tier-2 named, three siblings missing). Pair with 'addFocusAreas'.\n"
            " * 'anchor_evidence' — the structure is right but evidence is thin. Stay at the "
            "current depth; surface as-of dates, source URLs, primary filings. Pair with "
            "'evidenceGaps'.\n"
            " * 'stop_deepening' — further depth would not change the answer (evidence horizon "
            "reached, or the question does not justify deeper walks). Use to cap runaway "
            "depth-walking when the analysis is already adequate."
        ),
    )
    directive_text: str = Field(
        default="",
        alias="directiveText",
        description=(
            "One-sentence STRUCTURAL imperative the supervisor stitches into "
            "ah_mission for the next analyzer call. Must be self-contained (no "
            "pronouns referring back to the verdict) and must describe the missing "
            "LAYER + the surface entity to dig behind, optionally with a single "
            "'e.g. <name>' anchor for depth calibration. "
            "Good: 'Walk the photomask and lithography sub-supplier layers behind "
            "TSMC and ASML — for example surface a sub-supplier such as the EUV "
            "optics provider — and emit the full layer with as-of dates.' "
            "Bad (prescribes the answer): 'Add ZEISS SMT, Cymer, Trumpf, JSR, Tokyo "
            "Ohka and confirm their roles.' The analyzer is responsible for "
            "discovering the entities; the critic is responsible for describing the "
            "shape of what is missing."
        ),
    )


class CriticVerdict(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    sufficient: bool
    critical_findings: list[CriticFinding] = Field(
        default_factory=list,
        alias="criticalFindings",
        description=(
            "Findings the critic considers material to the user question — coverage "
            "must improve before the answer is acceptable. Each entry MUST carry a "
            "gap_class code from the GapClass typology so the supervisor can route "
            "by domain (supply_chain.*, governance.*, compliance.*, generic.*, "
            "methodology.*)."
        ),
    )
    advisory_findings: list[CriticFinding] = Field(
        default_factory=list,
        alias="advisoryFindings",
        description=(
            "Findings worth recording but not material enough to gate the answer. "
            "Same shape and gap_class typology as critical_findings; the difference "
            "is materiality vs the user question, not class of incompleteness."
        ),
    )
    mission_hint: str = Field(default="", alias="missionHint")
    composition_recommendation: Literal["go", "conditional", "stop"] = Field(
        default="conditional",
        alias="compositionRecommendation",
    )
    recommended_next_agent: CriticRecommendedNextAgent = Field(
        default="none",
        alias="recommendedNextAgent",
        description=(
            "Explicit routing hand-off to the supervisor: which agent should run next "
            "to address the most important critical finding. Use 'none' only when "
            "sufficient=true and no follow-up is warranted. The supervisor MAY "
            "override on hard gates (cap reached, red-team owed) but otherwise "
            "honors this hint."
        ),
    )
    scope_assessment: ScopeAssessment | None = Field(
        default=None,
        alias="scopeAssessment",
        description=(
            "Two-axis evaluation of the analyzer's self-declared scope plan: did the "
            "scope match the question (adequacy) and did the execution match the scope "
            "(fidelity). Optional for backward compatibility, but should be populated "
            "whenever the analyzer emitted a scopePlan."
        ),
    )
    recommended_scope_focus: RecommendedScopeFocus | None = Field(
        default=None,
        alias="recommendedScopeFocus",
        description=(
            "Structured directive for the next analyzer pass when sufficient=false. "
            "Supervisor uses directive_text + add_focus_areas + deepen_entity_ids + "
            "evidence_gaps to construct the next ah_mission. Should be populated "
            "whenever recommendedNextAgent points to an analyzer."
        ),
    )

    @model_validator(mode="after")
    def _composition_consistency(self) -> CriticVerdict:
        """Reject calibration-inverted verdicts (``go`` while declaring ``sufficient=false``).

        Observed in prior runs: ``go`` + ``sufficient=false`` + non-empty
        ``criticalFindings``. Logically inconsistent and dangerous in
        production — a critic that says "not enough, but proceed anyway" is
        worse than no critic at all.
        """

        if self.composition_recommendation == "go" and not self.sufficient:
            raise ValueError(
                "compositionRecommendation='go' is incompatible with sufficient=false; "
                "use 'conditional' or 'stop' when sufficient=false"
            )
        if self.composition_recommendation == "go" and self.critical_findings:
            raise ValueError(
                "compositionRecommendation='go' is incompatible with non-empty "
                "criticalFindings; downgrade to 'conditional' or 'stop'"
            )
        return self


class RedTeamOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    reframe_notes: str = Field(alias="reframeNotes")
    suggested_temperature_delta: float = Field(
        default=0.0,
        alias="suggestedTemperatureDelta",
        ge=-0.5,
        le=0.5,
    )
    probe_missions: list[str] = Field(default_factory=list, alias="probeMissions")
    dominant_narrative: str = Field(
        default="",
        alias="dominantNarrative",
        description="One-sentence statement of the default hypothesis the analysis converged on.",
    )
    alternative_narratives: list[str] = Field(
        default_factory=list,
        alias="alternativeNarratives",
        description=(
            "≥2 alternative framings that re-prioritize different evidence from the same pack. "
            "Not negations of the dominant — different orderings of the same facts."
        ),
    )
    distinguishing_evidence: list[str] = Field(
        default_factory=list,
        alias="distinguishingEvidence",
        description=(
            "Types of evidence (not specific evidence) that would distinguish dominant vs alternatives. "
            "Each item names a measurable artifact category (e.g. 'BIS Entity List update post-2024-Q4', "
            "'TSMC fab-by-fab capacity utilization', 'Dutch VEB license number for SMIC')."
        ),
    )


class QuestionPlanSubDomain(BaseModel):
    """One sub-domain extracted from the user question with its drivers."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    name: Literal["supply_chain", "governance", "sanctions"]
    confidence: float = Field(default=0.5, ge=0.0, le=1.0)
    triggers: list[str] = Field(
        default_factory=list,
        description="Verbatim phrases / keywords from the user question that triggered this sub-domain.",
    )


class QuestionPlan(BaseModel):
    """Structured decomposition of the user question, produced once per session.

    Acts as a stable system-of-coordinates for downstream roles: supervisor
    uses it as a coverage matrix, analyzers use named entities as research
    seeds, critic uses it as the named-entity-coverage baseline.
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    primary_intent: str = Field(
        default="",
        alias="primaryIntent",
        description="One-sentence reformulation of what the user is actually asking for.",
    )
    named_entities: list[str] = Field(
        default_factory=list,
        alias="namedEntities",
        description=(
            "Proper nouns from the user question: organizations, products, vessels, "
            "people. Verbatim spelling — do not canonicalize at this stage."
        ),
    )
    named_regimes: list[str] = Field(
        default_factory=list,
        alias="namedRegimes",
        description=(
            "Regulatory regimes / laws / sanctions lists named in the question "
            "(CHIPS Act, BIS Entity List, OFAC SDN, EU oil price cap, ...)."
        ),
    )
    named_geos: list[str] = Field(
        default_factory=list,
        alias="namedGeos",
        description="Countries, regions, ports, straits, or jurisdictions named in the question.",
    )
    sub_domains: list[QuestionPlanSubDomain] = Field(
        default_factory=list,
        alias="subDomains",
        description=(
            "Detected sub-domains the question spans. Multi-domain questions get >1 entry. "
            "Use this list to drive parallel_fanout decisions."
        ),
    )
    quantitative_requirements: list[str] = Field(
        default_factory=list,
        alias="quantitativeRequirements",
        description=(
            "Specific measurable artifacts the question implies (e.g. 'EUV shipment shares "
            "TSMC vs Samsung vs Intel', 'as-of date for SMIC designation', 'stake_pct of UBO Y in X')."
        ),
    )
    spof_requested: bool = Field(
        default=False,
        alias="spofRequested",
        description="True when the question asks for single points of failure / chokepoints / risks.",
    )
    jurisdictional_scope: list[str] = Field(
        default_factory=list,
        alias="jurisdictionalScope",
        description=(
            "Jurisdictions the answer should respect distinctly: ISO-3166-2 codes when known "
            "(US, NL, JP, TW, KR, CN, RU, IR), else verbatim names."
        ),
    )
    temporal_scope: str = Field(
        default="",
        alias="temporalScope",
        description=(
            "Time horizon implied by the question: 'current' (default), 'historical (2022-2024)', "
            "'forward (next 18 months)', etc. Empty when the question is timeless."
        ),
    )
