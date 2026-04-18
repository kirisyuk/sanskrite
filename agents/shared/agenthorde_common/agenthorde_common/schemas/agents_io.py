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


class AnalyzerOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    chain_summary: str = Field(alias="chainSummary")
    proposed_entities: list[ProposedEntity] = Field(
        default_factory=list,
        alias="proposedEntities",
        description="Entity registry: stable ids + labels verbatim from chainSummary / proposedNodes text.",
    )
    proposed_nodes: list[ReasoningNode] = Field(default_factory=list, alias="proposedNodes")
    proposed_relations: list[ProposedRelation] = Field(
        default_factory=list, alias="proposedRelations"
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
        reg = {(e.id or "").strip() for e in self.proposed_entities if (e.id or "").strip()}
        for r in self.proposed_relations:
            sid = (r.src_id or "").strip()
            did = (r.dst_id or "").strip()
            if sid and sid not in reg:
                raise ValueError(f"proposedRelations srcId {sid!r} not in proposedEntities")
            if did and did not in reg:
                raise ValueError(f"proposedRelations dstId {did!r} not in proposedEntities")
        return self


class CriticBlocker(BaseModel):
    """Single blocker / non-blocker entry with prose summary + entity-id refs.

    ``refs`` should cite ids from the registry block injected into the critic
    prompt (entity ids like ``sc_e1`` / ``gov_e2``, reasoning-node ids like
    ``sc_c1``, or session-pack fragment ids like ``n10`` / ``n11``).

    Empty ``refs`` is allowed for genuinely methodological critiques that do
    not point at a specific object (e.g. "no risk-rating framework").
    """

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    summary: str
    refs: list[str] = Field(default_factory=list)


CriticRecommendedNextAgent = Literal[
    "supply_chain_analyzer",
    "governance_control_analyzer",
    "sanctions_analyzer",
    "red_team_agent",
    "critic_agent",
    "none",
]


class CriticVerdict(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    sufficient: bool
    blockers: list[CriticBlocker] = Field(default_factory=list)
    non_blockers: list[CriticBlocker] = Field(default_factory=list, alias="nonBlockers")
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
            "to address the most important blocker. Use 'none' only when sufficient=true "
            "and no follow-up is warranted. The supervisor MAY override on hard gates "
            "(cap reached, red-team owed) but otherwise honors this hint."
        ),
    )

    @model_validator(mode="after")
    def _composition_consistency(self) -> CriticVerdict:
        """Reject calibration-inverted verdicts (``go`` while declaring ``sufficient=false``).

        Observed in prior runs (nemotron-3-nano-30b: ``go`` + ``sufficient=false`` + 3 blockers).
        Logically inconsistent and dangerous in production — a critic that says "not enough,
        but proceed anyway" is worse than no critic at all.
        """

        if self.composition_recommendation == "go" and not self.sufficient:
            raise ValueError(
                "compositionRecommendation='go' is incompatible with sufficient=false; "
                "use 'conditional' or 'stop' when sufficient=false"
            )
        if self.composition_recommendation == "go" and self.blockers:
            raise ValueError(
                "compositionRecommendation='go' is incompatible with non-empty blockers; "
                "downgrade to 'conditional' or 'stop'"
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
