from __future__ import annotations

import re
from pathlib import Path

from agentloom.contracts import InvestigationRole, RoleContract

ROLE_SUPPLY_CHAIN: InvestigationRole = "supply_chain"
ROLE_GOVERNANCE: InvestigationRole = "governance_and_control"
ROLE_POLICY: InvestigationRole = "policy_impact"

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
        ),
    }
    return contracts

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
