from __future__ import annotations

import re
from pathlib import Path

from agentloom.contracts import InvestigationRole, RoleContract

ROLE_SUPPLY_CHAIN: InvestigationRole = "supply_chain"
ROLE_GOVERNANCE: InvestigationRole = "governance_and_control"
ROLE_POLICY: InvestigationRole = "policy_impact"

SPECIALIZED_INVESTIGATOR_BY_ROLE: dict[InvestigationRole, str] = {
    ROLE_SUPPLY_CHAIN: "supply_chain_investigator_agent",
    ROLE_GOVERNANCE: "governance_and_control_investigator_agent",
    ROLE_POLICY: "policy_impact_investigator_agent",
}

DOMAIN_SUPPLY_CHAIN = "supply_chain"
DOMAIN_GOVERNANCE = "governance"
DOMAIN_POLICY = "policy_impact"

REPO_ROOT = Path(__file__).resolve().parents[3]
DOMAIN_MODE_FILE = REPO_ROOT / "backend/internal/taxonomy/domain_mode.go"
SEMANTICS_FILE = REPO_ROOT / "backend/internal/horizon/contractsemantics/semantics.go"
MISSING_RULES_FILE = REPO_ROOT / "backend/internal/horizon/entity_semantics/domain_mode_rules.go"
OWNERSHIP_CONTRACT_FILE = REPO_ROOT / "backend/internal/horizon/scout/contracts/ownership_structure.go"
CONTROL_MAP_CONTRACT_FILE = REPO_ROOT / "backend/internal/horizon/scout/contracts/control_map.go"

_QUOTED_RE = re.compile(r'"([^"]+)"')
_MODE_CASE_RE = re.compile(r"case\s+([A-Za-z0-9_.]+):\s*return\s+\[]string\{([^}]*)\}", re.S)
_MODE_STRING_CASE_RE = re.compile(r'case\s+([A-Za-z0-9_.]+):\s*return\s+"([^"]+)"', re.S)
_VAR_BLOCK_RE = re.compile(r"var\s+([A-Za-z0-9_]+)\s*=\s*\[]string\{([^}]*)\}", re.S)
_PROFILE_BLOCK_RE = re.compile(
    r"case\s+taxonomy\.([A-Za-z0-9_]+):\s*return\s+DomainModeProfile\{(.*?)\n\t\t\}",
    re.S,
)
_QUESTION_TEMPLATE_RE = re.compile(r'QuestionTemplate:\s+"([^"]+)"')


def load_role_contracts() -> dict[InvestigationRole, RoleContract]:
    domain_mode_text = _read_required(DOMAIN_MODE_FILE)
    semantics_text = _read_required(SEMANTICS_FILE)
    missing_rules_text = _read_required(MISSING_RULES_FILE)
    ownership_contract_text = _read_required(OWNERSHIP_CONTRACT_FILE)
    control_map_contract_text = _read_required(CONTROL_MAP_CONTRACT_FILE)

    allowed_entities = _parse_mode_string_list_switch(domain_mode_text, "AllowedEntityTypesForDomainMode")
    actor_entities = _parse_mode_actor_lists(domain_mode_text, "EntityActorTypesForDomainMode")
    named_arrays = _parse_named_string_arrays(domain_mode_text)
    descriptions = _parse_profile_descriptions(semantics_text)
    overlays = _parse_profile_edge_lists(semantics_text, "OverlayEdges")
    path_carried = _parse_profile_edge_lists(semantics_text, "PathCarriedEdges")
    missing_prompts = _parse_missing_info_prompts(missing_rules_text)
    governance_contract_types = _parse_contract_types(ownership_contract_text) + _parse_contract_types(control_map_contract_text)

    supply_allowed = named_arrays["SupplyChainEdgeTypes"]
    governance_allowed = named_arrays["GovernanceEdgeTypes"]
    policy_allowed = named_arrays["PolicyImpactEdgeTypes"]

    contracts = {
        ROLE_SUPPLY_CHAIN: RoleContract(
            role=ROLE_SUPPLY_CHAIN,
            domainMode=DOMAIN_SUPPLY_CHAIN,
            contractTypes=["supply-chain-path"],
            description=descriptions[DOMAIN_SUPPLY_CHAIN],
            expectedEntityTypes=allowed_entities[DOMAIN_SUPPLY_CHAIN],
            actorEntityTypes=actor_entities[DOMAIN_SUPPLY_CHAIN],
            allowedEdgeTypes=supply_allowed,
            pathCarriedEdges=path_carried[DOMAIN_SUPPLY_CHAIN],
            overlayEdges=overlays[DOMAIN_SUPPLY_CHAIN],
            missingInfoPrompts=missing_prompts[DOMAIN_SUPPLY_CHAIN],
            investigationPlaybook=_supply_chain_playbook(),
            toolUseGuidance=_supply_chain_tool_guidance(),
            structuralClosureCriteria=_supply_chain_closure_criteria(),
        ),
        ROLE_GOVERNANCE: RoleContract(
            role=ROLE_GOVERNANCE,
            domainMode=DOMAIN_GOVERNANCE,
            contractTypes=_dedupe(governance_contract_types),
            description=descriptions[DOMAIN_GOVERNANCE],
            expectedEntityTypes=allowed_entities[DOMAIN_GOVERNANCE],
            actorEntityTypes=actor_entities[DOMAIN_GOVERNANCE],
            allowedEdgeTypes=governance_allowed,
            pathCarriedEdges=[],
            overlayEdges=overlays.get(DOMAIN_GOVERNANCE, governance_allowed),
            missingInfoPrompts=missing_prompts[DOMAIN_GOVERNANCE],
            investigationPlaybook=_governance_playbook(),
            toolUseGuidance=_governance_tool_guidance(),
            structuralClosureCriteria=_governance_closure_criteria(),
        ),
        ROLE_POLICY: RoleContract(
            role=ROLE_POLICY,
            domainMode=DOMAIN_POLICY,
            contractTypes=[
                "regulatory-impact-map",
                "regulatory-impact-transmission-path",
            ],
            description=descriptions[DOMAIN_POLICY],
            expectedEntityTypes=allowed_entities[DOMAIN_POLICY],
            actorEntityTypes=actor_entities[DOMAIN_POLICY],
            allowedEdgeTypes=policy_allowed,
            pathCarriedEdges=[],
            overlayEdges=overlays[DOMAIN_POLICY],
            missingInfoPrompts=missing_prompts[DOMAIN_POLICY],
            investigationPlaybook=_policy_playbook(),
            toolUseGuidance=_policy_tool_guidance(),
            structuralClosureCriteria=_policy_closure_criteria(),
        ),
    }
    return contracts


def _supply_chain_playbook() -> list[str]:
    return [
        "Work as an operational supply-chain investigator, not as a general industry explainer.",
        "Colloquial names for commodities, elements, or materials in the user question are shorthand, not operational nodes: interpret them as the traded or processed product-stage ladder implied by the question's use-case. Encode operational truth as PRODUCT stages and canonical transforms; an elemental or commodity label alone is not a satisfactory sink or source node.",
        "Illustrative only (non-exhaustive, not domain-specific): product ladders may include, for example, ores and concentrates, intermediate chemicals or salts, alloys, or precursor stages to downstream goods—adapt the examples to the sector named in the question; the list is not a checklist to force every investigation.",
        "A supply-chain investigation result is primarily typed graph artifacts — GraphFragment observations, working-graph mutations, reachability, and candidate paths — plus compact learned findings. Domain prose without these artifacts does not close operational questions.",
        "Start by reconstructing one broad operational path from source-side anchor to sink-side anchor along that product-stage ladder, including derivative or intermediate stages where the use-case implies them (still as concrete PRODUCT nodes, not as abstract commodity labels).",
        "Keep the path balanced. When you narrow the sink side, keep source-side, product-stage, and intermediary gaps explicit so the hypothesis does not drift into downstream-only investigation.",
        "Treat countries, sectors, and markets only as geographic or market-scope anchors in the question. In the graph, replace them with concrete ORG, FACILITY, DEPOSIT, and PRODUCT nodes as soon as context allows; never treat a GEO_REGION as if it produced, transformed, shipped, or consumed a product.",
        "Do not accept a root or overview supply-chain hypothesis while broad source, sink, or intermediary placeholders are still alive in the same hypothesis graph.",
        "Use canonical product identity and canonical transform pairs as the default way to expand product stages. Ask for the nearest missing product-stage or derivative step instead of jumping across several transformations.",
        "Model procurement, offtake, long-term sourcing, localization, recycling strategy, and vertical integration as a contractual or commercial layer. Those findings are useful, but they do not count as operational flow closure unless they produce concrete typed supply or transform edges.",
        "If the path breaks, ask what immediate receiver, converter, intermediary, or downstream product stage sits between the two graph islands.",
        "If the downstream sink is unclear, expand the frame by asking which closest downstream industries, manufacturers, or facilities consume the current product stage or derivative.",
        "Adjacent-frame expansion: when the user names a narrow geographic or trade corridor (e.g. source region to sink region), you may record materially relevant but non-coextensive context in a separate fragment family, learnedFinding, or hypothesis—such as regional conversion near the sink, alternative suppliers of the same material class, or market-pool effects—without faking a direct SUPPLIES_TO or TRANSFORMS_INTO edge that evidence does not support. Mark such items with low confidence and supportType=model_knowledge or hypothesis so they never read as document-grounded flow.",
        "If a direct downstream link fails, pivot to the nearest operational intermediary layer rather than broad procurement strategy or generic market commentary.",
        "If the current route seems narrow, explicitly test whether materially different route families exist by technology, geography, intermediary class, or processing path.",
        "Keep multiplicity. Several facilities may occupy the same source, transformation, or sink layer. Preserve that as parallel candidates inside the same hypothesis until a genuinely different route family appears.",
        "If the graph already contains a usable hot path but still carries unresolved structural frontier from earlier turns, keep the hypothesis open and continue replacing or bridging those specific weak slots instead of accepting early.",
        "When placeholder or broad-anchor nodes are still in the working graph, the next useful work is **unpacking** them: separate branches that each name **one** slot (one layer or one candidate class), then consolidate with **`replace_node` / `replace_fragment`** using the correct **`oldId`** from the graph—do not introduce a parallel second placeholder for the same slot. Multiplicity is **parallel dependent** branches with **different** `knownContext` / target nodes, not duplicate vague shells.",
        "Prefer returning mergeable **typed graph fragments** (including patch modes) over prose-only relabeling when a flow hop is still labeled as a placeholder.",
        "When the supervisor has already commissioned a broad skeleton graph, follow-up investigator output should stay narrow: one break-point, one bridge class, or one explicit openGap—do not re-expand the whole end-to-end thesis unless new evidence invalidated the prior frame.",
        "Treat ownership or policy facts as overlays that help explain leverage or chokepoints. They do not replace missing operational flow edges and should stay in their own role unless a direct intersection with the current operational node must be recorded.",
        "Do not stack ownership, control, or financing overlays on **operational placeholders** or on nodes that are not anchored in a **flow** chain yet; first resolve or replace the operational endpoint, or record an explicit **bridge-candidate** path if evidence only supports that.",
        "Do not spend turns on stable background facts that a competent model already knows, such as well-established jurisdiction-wide regulatory frameworks, unless the question is about a current amendment, implementation date, enforcement change, or company-specific response.",
        "Prioritize qualitative structural closure over quantitative detail. A correctly typed edge with unknown tonnage is more valuable than a numeric estimate without a typed edge.",
        "A supply-chain hypothesis is healthy when its graph contains a decision-useful path or route family with concrete typed nodes and explicit transform or supply edges, even if some contract volumes remain unknown.",
    ]


def _supply_chain_tool_guidance() -> dict[str, str]:
    return {
        "reason": "Use for the first broad path, transform-ladder design, path-family comparison, placeholder replacement design, graph synthesis when the next missing step is structural, and stable background facts that a well-trained model should already know without web lookup.",
        "retrieve": "Use for internal memory, request corpus, and Vertex internal evidence when you need named facilities, product stages, deposits, or previously discovered graph context.",
        "web_search": "Use for public facility facts, current plant status, named intermediary identification, public route evidence, time-sensitive company disclosures, and current regulatory updates. Do not use it just to re-ask stable background regulation or generic procurement concepts.",
    }


def _supply_chain_closure_criteria() -> list[str]:
    return [
        "The graph contains a source-side typed node or an explicit frontier saying which source-side node must replace a broad anchor.",
        "The graph contains an operational chain of ORG/FACILITY/DEPOSIT/PRODUCT nodes connected by path-carried supply-chain edges.",
        "The product ladder uses immediate product stages or canonical transform steps rather than skipping several stages without explanation.",
        "Downstream sink nodes are concrete facilities or organizations when the question requires operational recipients.",
        "Remaining gaps are bridge questions or detail questions, not broad fake endpoints or fake intermediaries.",
    ]


def _governance_playbook() -> list[str]:
    return [
        "Work as a governance and control investigator, not as a generic corporate summarizer.",
        "Begin from already named operational assets or entities and ask who owns, controls, manages, finances, or economically constrains them.",
        "Separate ownership, control, management, and investment. Do not collapse shareholder, operator, board control, park operator, lender, and offtake leverage into one relation.",
        "Follow the legal and economic chain upward: subsidiary, JV, parent, strategic investor, state actor, UBO, and management control where evidence supports it.",
        "Preserve multiplicity in ownership layers. Multiple parents, JV partners, investors, or controllers may coexist and should remain explicit.",
        "When a governance actor seems strategically important but has no operational edge into the supply graph, surface it as an overlay or bridge candidate rather than faking a supply path.",
        "Ask for the exact entity-to-entity control path that explains leverage over the operational node already in the graph.",
        "Prioritize entity resolution and control structure over percentages once the relevant control path is already structurally visible.",
        "A governance hypothesis is healthy when the graph can explain who owns, controls, or meaningfully influences the operational nodes that matter to the parent investigation.",
    ]


def _governance_tool_guidance() -> dict[str, str]:
    return {
        "reason": "Use for control-structure synthesis, JV/parent-chain interpretation, and separating ownership from management or influence when the graph already names the asset.",
        "retrieve": "Use for previously discovered entity trees, internal ownership data, or request-corpus evidence about parents, subsidiaries, executives, and investors.",
        "web_search": "Use for current corporate ownership, shareholder disclosures, board and JV information, public filings, and corporate structure updates.",
    }


def _governance_closure_criteria() -> list[str]:
    return [
        "Key operational nodes already in scope have explicit OWNS, CONTROLS, MANAGES, or INVESTS_IN overlays where public or internal evidence supports them.",
        "The graph distinguishes legal owner, operator, controller, and investor when they are not the same actor.",
        "Ultimate parent or meaningful control chain is represented when it materially affects the investigated path.",
        "Remaining gaps are quantitative stake details or deeper UBO refinement, not missing core control paths.",
    ]


def _policy_playbook() -> list[str]:
    return [
        "Work as a regulatory-impact investigator focused on transmission paths, not as a general policy commentator.",
        "Start from a named policy, authority, mechanism, permit regime, export restriction, traceability requirement, or compliance program that can act on the path.",
        "Map transmission explicitly: regulation or mechanism -> condition or access rule -> affected product, facility, organization, or geography.",
        "Separate direct regulatory scope from downstream business response. The graph should show both the formal rule and the path by which it changes sourcing, market access, or technology choice.",
        "Treat stable, widely known policy structures as model-knowledge context first. Use web or retrieval only when the task depends on current amendments, implementation timing, enforcement posture, named company response, or jurisdiction-specific detail that may have changed.",
        "When a policy blocks or constrains one route, ask whether a workaround, alternative jurisdiction path, or substitute product path emerges.",
        "Prefer named agencies, regulations, and mechanisms over vague 'regulatory pressure' nodes.",
        "Keep the policy layer connected to real operational nodes. A policy graph that never touches facilities, products, or organizations is not decision-useful.",
        "Do not attach regulation or compliance edges to **unnamed** or **placeholder** operational nodes; map each mechanism to **named** authorities/regulations **and** **named** facilities, organizations, or product stages that the rule actually constrains.",
        "Prioritize structural transmission over quantitative policy impact estimates unless the graph already has the relevant transmission edges.",
        "A policy hypothesis is healthy when the graph can explain which mechanism acts on which operational node and how that changes the viability or shape of the path.",
    ]


def _policy_tool_guidance() -> dict[str, str]:
    return {
        "reason": "Use for regulatory transmission-path design, mechanism synthesis, stable policy background, and identifying where a policy or rule should attach to the existing operational graph.",
        "retrieve": "Use for previously known policy entities, internal regulatory corpora, or request evidence already supplied to the system.",
        "web_search": "Use for current laws, regulations, agency guidance, sanctions, export bans, compliance regimes, enforcement updates, and public statements about how firms respond to them.",
    }


def _policy_closure_criteria() -> list[str]:
    return [
        "The graph identifies the relevant regulation, authority, or mechanism explicitly.",
        "There is an explicit transmission path from the policy node to operational nodes such as products, facilities, organizations, or geographies.",
        "The graph distinguishes route-blocking or route-shaping mechanisms from generic policy background commentary.",
        "Remaining gaps are impact magnitude or implementation detail, not missing transmission edges.",
    ]

def _read_required(path: Path) -> str:
    text = path.read_text(encoding="utf-8")
    if not text.strip():
        raise RuntimeError(f"required taxonomy source is empty: {path}")
    return text


def _parse_mode_string_list_switch(text: str, function_name: str) -> dict[str, list[str]]:
    marker = f"func {function_name}(mode string) []string"
    start = text.find(marker)
    if start < 0:
        raise RuntimeError(f"cannot find {function_name} in {DOMAIN_MODE_FILE}")
    end = text.find("\n}\n", start)
    block = text[start:end]
    result: dict[str, list[str]] = {}
    for mode_name, values_block in _MODE_CASE_RE.findall(block):
        mode = _normalize_go_mode(mode_name)
        if mode is None:
            continue
        result[mode] = _parse_string_literals(values_block)
    return result


def _parse_mode_actor_lists(text: str, function_name: str) -> dict[str, list[str]]:
    marker = f"func {function_name}(mode string) string"
    start = text.find(marker)
    if start < 0:
        raise RuntimeError(f"cannot find {function_name} in {DOMAIN_MODE_FILE}")
    end = text.find("\n}\n", start)
    block = text[start:end]
    result: dict[str, list[str]] = {}
    for mode_name, actor_string in _MODE_STRING_CASE_RE.findall(block):
        mode = _normalize_go_mode(mode_name)
        if mode is None:
            continue
        tokens = re.findall(r"\b[A-Z_]+\b", actor_string)
        result[mode] = _dedupe(tokens)
    return result


def _parse_named_string_arrays(text: str) -> dict[str, list[str]]:
    arrays: dict[str, list[str]] = {}
    for name, values_block in _VAR_BLOCK_RE.findall(text):
        arrays[name] = _parse_string_literals(values_block)
    return arrays


def _parse_profile_descriptions(text: str) -> dict[str, str]:
    descriptions: dict[str, str] = {}
    for mode_name, block in _PROFILE_BLOCK_RE.findall(text):
        mode = _normalize_go_mode(f"taxonomy.{mode_name}")
        if mode is None:
            continue
        match = re.search(r'Description:\s+"([^"]+)"', block)
        if match:
            descriptions[mode] = match.group(1)
    return descriptions


def _parse_profile_edge_lists(text: str, field_name: str) -> dict[str, list[str]]:
    result: dict[str, list[str]] = {}
    pattern = re.compile(field_name + r":\s+\[]models\.DependencyType\{([^}]*)\}", re.S)
    for mode_name, block in _PROFILE_BLOCK_RE.findall(text):
        mode = _normalize_go_mode(f"taxonomy.{mode_name}")
        if mode is None:
            continue
        match = pattern.search(block)
        if not match:
            continue
        result[mode] = re.findall(r"models\.DependencyType([A-Za-z0-9_]+)", match.group(1))
        result[mode] = [_camel_to_upper_snake(value) for value in result[mode]]
    return result


def _parse_missing_info_prompts(text: str) -> dict[str, list[str]]:
    result: dict[str, list[str]] = {}
    for domain_name in (DOMAIN_SUPPLY_CHAIN, DOMAIN_GOVERNANCE, DOMAIN_POLICY):
        go_name = _domain_to_go_mode(domain_name)
        marker = f"taxonomy.{go_name}:"
        start = text.find(marker)
        if start < 0:
            raise RuntimeError(f"cannot find missing-info rules for {domain_name}")
        next_marker = text.find("\n\ttaxonomy.", start + 1)
        block = text[start: next_marker if next_marker >= 0 else len(text)]
        result[domain_name] = _QUESTION_TEMPLATE_RE.findall(block)
    return result


def _parse_contract_types(text: str) -> list[str]:
    match = re.search(r'func\s+\([^)]+\)\s+ContractType\(\)\s+string\s*\{\s*return\s+"([^"]+)"', text, re.S)
    if not match:
        raise RuntimeError("cannot parse scout contract type")
    return [match.group(1)]


def _parse_string_literals(values_block: str) -> list[str]:
    return _QUOTED_RE.findall(values_block)


def _normalize_go_mode(mode_name: str) -> str | None:
    mapping = {
        "DomainModeSupplyChain": DOMAIN_SUPPLY_CHAIN,
        "taxonomy.DomainModeSupplyChain": DOMAIN_SUPPLY_CHAIN,
        "DomainModeGovernance": DOMAIN_GOVERNANCE,
        "taxonomy.DomainModeGovernance": DOMAIN_GOVERNANCE,
        "DomainModePolicyImpact": DOMAIN_POLICY,
        "taxonomy.DomainModePolicyImpact": DOMAIN_POLICY,
    }
    normalized = mapping.get(mode_name)
    return normalized


def _domain_to_go_mode(domain_name: str) -> str:
    mapping = {
        DOMAIN_SUPPLY_CHAIN: "DomainModeSupplyChain",
        DOMAIN_GOVERNANCE: "DomainModeGovernance",
        DOMAIN_POLICY: "DomainModePolicyImpact",
    }
    return mapping[domain_name]


def _camel_to_upper_snake(value: str) -> str:
    chunks = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", value).upper()
    return chunks


def _dedupe(values: list[str]) -> list[str]:
    seen: set[str] = set()
    result: list[str] = []
    for value in values:
        if not value or value in seen:
            continue
        seen.add(value)
        result.append(value)
    return result


ROLE_CONTRACTS_BY_ROLE = load_role_contracts()
