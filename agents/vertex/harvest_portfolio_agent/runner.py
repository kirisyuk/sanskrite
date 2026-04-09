from __future__ import annotations

import argparse
import json
import mimetypes
import os
import re
import traceback
import uuid
from datetime import UTC, datetime
from io import BytesIO
from pathlib import PurePosixPath
from urllib.parse import parse_qs, unquote, urljoin, urlparse

import requests
from bs4 import BeautifulSoup
from google import genai
from google.cloud import firestore, storage
from google.cloud.firestore_v1.base_query import FieldFilter
from google.genai import types
from pydantic import ValidationError
from pypdf import PdfReader

from harvest_portfolio_agent.agent import run_material_rescue_search, run_portfolio_search
from harvest_portfolio_agent.schemas import HarvestEnrichment, HarvestRunRecord, MaterialBatch, SourceMaterial, StoredMaterial

DEFAULT_LOCATION = "global"
DEFAULT_BUCKET_REGION = "US-CENTRAL1"
DEFAULT_BUCKET_SUFFIX = "vertex-harvest-agent-artifacts"
RAW_BYTES_LIMIT = 10 * 1024 * 1024
TEXT_PREVIEW_LIMIT = 12000
REQUEST_TIMEOUT = 20
MAX_DISCOVERY_MATERIALS = 6


class RunTracer:
    def __init__(self, storage_client: storage.Client, bucket_name: str, prefix: str):
        self.storage_client = storage_client
        self.bucket_name = bucket_name
        self.prefix = prefix
        self.events: list[dict] = []
        self.sequence = 0

    def record(self, stage: str, status: str, payload: object | None = None) -> None:
        self.sequence += 1
        event = {
            "sequence": self.sequence,
            "timestamp": datetime.now(UTC).isoformat(),
            "stage": stage,
            "status": status,
        }
        if payload is not None:
            event["payload_preview"] = preview_payload(payload)
        self.events.append(event)
        print(f"[trace] {stage} [{status}]")
        self._safe_upload_json("trace/events.json", {"events": self.events})
        if payload is not None:
            self._safe_upload_json(
                f"trace/{self.sequence:02d}-{slugify(stage) or 'stage'}-{slugify(status) or 'status'}.json",
                {
                    "stage": stage,
                    "status": status,
                    "timestamp": event["timestamp"],
                    "payload": payload,
                },
            )

    def failure(self, error: BaseException) -> None:
        self.record(
            "run.failure",
            "error",
            {
                "error_type": type(error).__name__,
                "error": str(error),
                "traceback": traceback.format_exc(),
            },
        )

    def _safe_upload_json(self, object_name: str, payload: dict) -> None:
        try:
            upload_json(self.storage_client, self.bucket_name, f"{self.prefix}/{object_name}", payload)
        except Exception:
            pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run the first Vertex harvest portfolio agent on a Horizon fragment.")
    parser.add_argument("--project", required=True, help="GCP project ID")
    parser.add_argument("--canonical-path", required=True, help="Fragment canonical path")
    parser.add_argument("--tenant", default="global", help="Tenant ID")
    parser.add_argument("--location", default=DEFAULT_LOCATION, help="Vertex location")
    parser.add_argument("--bucket", default="", help="Optional bucket name for harvested materials")
    parser.add_argument("--bucket-region", default=DEFAULT_BUCKET_REGION, help="Bucket region to create if missing")
    parser.add_argument("--max-materials", type=int, default=6, help="How many curated materials to keep in the final report")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    os.environ["GOOGLE_GENAI_USE_VERTEXAI"] = "True"
    os.environ["GOOGLE_CLOUD_PROJECT"] = args.project
    os.environ["GOOGLE_CLOUD_LOCATION"] = args.location

    fs_client = firestore.Client(project=args.project)
    storage_client = storage.Client(project=args.project)
    fragment = get_fragment_by_canonical_path(fs_client, args.canonical_path)
    if fragment is None:
        raise SystemExit(f"fragment not found for canonical path {args.canonical_path}")

    bucket_name = args.bucket.strip() or f"{args.project}-{DEFAULT_BUCKET_SUFFIX}"
    ensure_bucket(storage_client, bucket_name, args.bucket_region)

    run_id = build_run_id(fragment["id"])
    prefix = f"runs/{run_id}"
    entity_context = build_entity_context(fragment)
    tracer = RunTracer(storage_client, bucket_name, prefix)
    tracer.record(
        "run.start",
        "ok",
        {
            "run_id": run_id,
            "project_id": args.project,
            "canonical_path": args.canonical_path,
        },
    )
    upload_json(storage_client, bucket_name, f"{prefix}/entity_context.json", entity_context)
    tracer.record("entity_context", "ok", entity_context)

    try:
        search_prompt = build_search_prompt(entity_context)
        tracer.record("search_prompt", "ok", {"search_prompt": search_prompt})
        initial_payload = run_portfolio_search(
            search_prompt,
            project_id=args.project,
            location=args.location,
            trace_fn=tracer.record,
        )
        initial_payload.setdefault("entity_name", entity_context["entity_name"])
        initial_payload.setdefault("entity_canonical_path", entity_context["entity_canonical_path"])
        initial_payload.setdefault("entity_type", entity_context["entity_type"])
        tracer.record("initial_payload", "ok", initial_payload)
        initial_report = validate_enrichment(initial_payload)
        initial_report_uri = upload_json(storage_client, bucket_name, f"{prefix}/initial_report.json", initial_report.model_dump(mode="json"))
        tracer.record("initial_report", "ok", initial_report.model_dump(mode="json"))

        candidate_materials = rank_source_materials(dedupe_source_materials(initial_report.materials))[: max(args.max_materials, MAX_DISCOVERY_MATERIALS)]
        tracer.record("candidate_materials", "ok", {"count": len(candidate_materials), "materials": [item.model_dump(mode="json") for item in candidate_materials]})
        stored_materials, synthesis_snippets, mined_candidates = fetch_materials(
            storage_client=storage_client,
            bucket_name=bucket_name,
            prefix=prefix,
            materials=candidate_materials,
            entity_context=entity_context,
            start_index=1,
        )
        tracer.record("fetch_materials.initial", "ok", {"stored_material_count": len(stored_materials), "mined_candidate_count": len(mined_candidates)})
        gap_assessment = assess_material_coverage(entity_context, stored_materials)
        tracer.record("gap_assessment.initial", "ok", gap_assessment)

        same_domain_candidates = dedupe_source_materials(mined_candidates)
        tracer.record("same_domain_candidates", "ok", {"count": len(same_domain_candidates), "materials": [item.model_dump(mode="json") for item in same_domain_candidates]})
        if same_domain_candidates:
            extra_candidates = filter_new_materials(same_domain_candidates, stored_materials)[:2]
            tracer.record("same_domain_candidates.filtered", "ok", {"count": len(extra_candidates), "materials": [item.model_dump(mode="json") for item in extra_candidates]})
            extra_stored, extra_snippets, _ = fetch_materials(
                storage_client=storage_client,
                bucket_name=bucket_name,
                prefix=prefix,
                materials=extra_candidates,
                entity_context=entity_context,
                start_index=len(stored_materials) + 1,
            )
            tracer.record("fetch_materials.same_domain", "ok", {"stored_material_count": len(extra_stored)})
            stored_materials.extend(extra_stored)
            synthesis_snippets.extend(extra_snippets)
            gap_assessment = assess_material_coverage(entity_context, stored_materials)
            tracer.record("gap_assessment.after_same_domain", "ok", gap_assessment)

        if should_run_rescue(gap_assessment):
            rescue_candidates = discover_rescue_materials(
                project_id=args.project,
                location=args.location,
                entity_context=entity_context,
                initial_report=initial_report,
                stored_materials=stored_materials,
                gap_assessment=gap_assessment,
                limit=2,
                trace_fn=tracer.record,
            )
            rescue_candidates = filter_new_materials(rescue_candidates, stored_materials)
            tracer.record("rescue_candidates.filtered", "ok", {"count": len(rescue_candidates), "materials": [item.model_dump(mode="json") for item in rescue_candidates]})
            if rescue_candidates:
                rescue_stored, rescue_snippets, _ = fetch_materials(
                    storage_client=storage_client,
                    bucket_name=bucket_name,
                    prefix=prefix,
                    materials=rescue_candidates,
                    entity_context=entity_context,
                    start_index=len(stored_materials) + 1,
                )
                tracer.record("fetch_materials.rescue", "ok", {"stored_material_count": len(rescue_stored)})
                stored_materials.extend(rescue_stored)
                synthesis_snippets.extend(rescue_snippets)
                gap_assessment = assess_material_coverage(entity_context, stored_materials)
                tracer.record("gap_assessment.after_rescue", "ok", gap_assessment)

        validated_materials = select_usable_materials(stored_materials, limit=max(args.max_materials, 4))
        tracer.record("validated_materials", "ok", {"count": len(validated_materials), "materials": [m.model_dump(mode="json") for m in validated_materials]})
        upload_json(storage_client, bucket_name, f"{prefix}/validated_materials.json", {"materials": [m.model_dump(mode="json") for m in stored_materials]})
        upload_json(storage_client, bucket_name, f"{prefix}/gap_assessment.json", gap_assessment)

        final_report = synthesize_final_report(
            project_id=args.project,
            location=args.location,
            entity_context=entity_context,
            initial_report=initial_report,
            stored_materials=validated_materials,
            gap_assessment=gap_assessment,
        )
        tracer.record("final_report", "ok", final_report.model_dump(mode="json"))
        final_report_uri = upload_json(storage_client, bucket_name, f"{prefix}/final_report.json", final_report.model_dump(mode="json"))

        run_record = HarvestRunRecord(
            run_id=run_id,
            project_id=args.project,
            tenant_id=entity_context["tenant_id"],
            entity_id=entity_context["entity_id"],
            entity_name=entity_context["entity_name"],
            entity_canonical_path=entity_context["entity_canonical_path"],
            entity_type=entity_context["entity_type"],
            bucket=bucket_name,
            prefix=prefix,
            initial_report_gcs_uri=initial_report_uri,
            final_report_gcs_uri=final_report_uri,
            materials=stored_materials,
            enrichment=final_report,
            usable_material_count=len(validated_materials),
            created_at=datetime.now(UTC),
        )
        persist_run_record(fs_client, run_record)
        upload_json(storage_client, bucket_name, f"{prefix}/run_record.json", run_record.model_dump(mode="json"))
        tracer.record("run.complete", "ok", {"usable_material_count": len(validated_materials)})
        print(json.dumps(run_record.model_dump(mode="json"), ensure_ascii=False, indent=2))
    except Exception as exc:
        tracer.failure(exc)
        raise


def get_fragment_by_canonical_path(client: firestore.Client, canonical_path: str) -> dict | None:
    docs = (
        client.collection("horizon_fragments")
        .where(filter=FieldFilter("canonicalPath", "==", canonical_path.strip()))
        .limit(1)
        .stream()
    )
    for doc in docs:
        data = doc.to_dict()
        data["id"] = data.get("id") or doc.id
        return data
    return None


def build_entity_context(fragment: dict) -> dict:
    labels = fragment.get("labels") or {}
    identifiers = fragment.get("identifiers") or {}
    home_location = fragment.get("homeLocation") or {}
    coordinates = fragment.get("coordinates") or {}
    kb_refs = fragment.get("kbRefs") or []
    return {
        "entity_id": fragment.get("id", ""),
        "tenant_id": fragment.get("tenantId", "global"),
        "entity_name": first_non_empty(
            labels.get("en"),
            identifiers.get("legalName"),
            identifiers.get("shortName"),
            fragment.get("canonicalPath", "").rsplit("/", 1)[-1],
        ),
        "entity_canonical_path": fragment.get("canonicalPath", ""),
        "entity_type": fragment.get("type", ""),
        "description": fragment.get("description", ""),
        "geo_slug": fragment.get("geoSlug", ""),
        "entity_path": fragment.get("entityPath", ""),
        "industry_paths": fragment.get("industryPaths") or [],
        "market_segments": fragment.get("marketSegments") or [],
        "sphere_paths": fragment.get("spherePaths") or [],
        "org_size": fragment.get("orgSize", ""),
        "lifecycle_stage": fragment.get("lifecycleStage", ""),
        "home_location": home_location,
        "coordinates": coordinates,
        "identifier_hints": compact_dict(
            {
                "legalName": identifiers.get("legalName"),
                "shortName": identifiers.get("shortName"),
                "website": identifiers.get("website"),
                "imo": identifiers.get("imo"),
                "operator": identifiers.get("operator"),
            }
        ),
        "kb_refs": kb_refs[:5],
    }


def build_search_prompt(entity_context: dict) -> str:
    expected_roles = ", ".join(expected_source_roles(entity_context["entity_type"]))
    return (
        "Build a consulting-grade portfolio for this Horizon entity.\n\n"
        f"{json.dumps(entity_context, ensure_ascii=False, indent=2)}\n\n"
        "Requirements:\n"
        "- Treat this as an intelligence harvest for an analyst, not a casual summary.\n"
        "- The executive brief must explain why this entity matters.\n"
        "- key_judgments must be explicit, decision-useful, and tied to source titles/URLs.\n"
        "- related_entities should name operators, owners, parent groups, nearby ports/terminals, regulators, key customers, or strategic counterparties when relevant.\n"
        f"- materials should prioritize coverage across these source roles: {expected_roles}.\n"
        "- materials should prioritize official pages, annual reports, investor decks, terminal/operator pages, infrastructure profiles, exchange filings, and high-quality trade coverage.\n"
        "- If evidence is ambiguous, say so in access_notes rather than hallucinating certainty.\n"
        "- Prefer concise, fact-dense wording."
    )


def validate_enrichment(payload: dict) -> HarvestEnrichment:
    payload = normalize_enrichment_payload(payload)
    try:
        return HarvestEnrichment.model_validate(payload)
    except ValidationError as exc:
        raise RuntimeError(f"harvest payload validation failed: {exc}") from exc


def validate_material_batch(payload: dict) -> list[SourceMaterial]:
    normalized = normalize_enrichment_payload({"materials": (payload or {}).get("materials") or []})
    try:
        batch = MaterialBatch.model_validate({"materials": normalized.get("materials") or []})
    except ValidationError as exc:
        raise RuntimeError(f"material batch validation failed: {exc}") from exc
    return batch.materials


def fetch_materials(
    *,
    storage_client: storage.Client,
    bucket_name: str,
    prefix: str,
    materials: list,
    entity_context: dict,
    start_index: int,
) -> tuple[list[StoredMaterial], list[dict], list[SourceMaterial]]:
    stored: list[StoredMaterial] = []
    synthesis_snippets: list[dict] = []
    mined_candidates: list[SourceMaterial] = []
    for idx, material in enumerate(materials, start=start_index):
        url = material.url.strip()
        slug = slugify(material.title) or f"material-{idx}"
        raw_object = f"{prefix}/materials/{idx:02d}-{slug}{guess_extension(url)}"
        text_object = f"{prefix}/materials/{idx:02d}-{slug}.txt"
        record = StoredMaterial(
            title=material.title,
            url=url,
            publisher=material.publisher,
            document_type=material.document_type,
            source_role=material.source_role,
            priority=material.priority,
            evidence_strength=material.evidence_strength,
            why_it_matters=material.why_it_matters,
            cross_references=material.cross_references,
        )
        try:
            response = requests.get(
                url,
                timeout=REQUEST_TIMEOUT,
                headers={
                    "User-Agent": "Mozilla/5.0 (compatible; VertexHarvestPortfolioAgent/0.1; +https://cloud.google.com/vertex-ai)"
                },
                stream=True,
                allow_redirects=True,
            )
            record.http_status = response.status_code
            record.final_url = normalize_result_url(response.url)
            record.content_type = response.headers.get("content-type", "")
            payload = read_limited_response(response)
            record.raw_gcs_uri = upload_bytes(
                storage_client,
                bucket_name,
                raw_object,
                payload,
                record.content_type or guess_content_type(raw_object),
            )
            extracted_text = extract_text(payload, record.content_type or "")
            record.extracted_text_chars = len(extracted_text)
            record.quality_score, record.quality_label, record.quality_rationale, record.is_usable = score_material_quality(
                record=record,
                extracted_text=extracted_text,
                entity_context=entity_context,
            )
            if extracted_text:
                record.extracted_text_gcs_uri = upload_text(storage_client, bucket_name, text_object, extracted_text)
                if record.is_usable:
                    synthesis_snippets.append(build_synthesis_snippet(record, extracted_text))
                if is_html_content(record.content_type or "", payload) and material.source_role == "official_entity_page":
                    mined_candidates.extend(
                        mine_same_domain_candidates(
                            payload=payload,
                            base_url=record.final_url or url,
                            material=material,
                            entity_context=entity_context,
                        )
                    )
            record.fetched_at = datetime.now(UTC)
        except Exception as exc:  # noqa: BLE001
            record.fetch_error = str(exc)
        stored.append(record)
    return stored, synthesis_snippets, mined_candidates


def synthesize_final_report(
    *,
    project_id: str,
    location: str,
    entity_context: dict,
    initial_report: HarvestEnrichment,
    stored_materials: list[StoredMaterial],
    gap_assessment: dict,
) -> HarvestEnrichment:
    client = genai.Client(vertexai=True, project=project_id, location=location)
    validated_materials = [material.model_dump(mode="json") for material in stored_materials]
    prompt = (
        "You are refining a harvested entity portfolio into a consulting-grade enrichment payload.\n"
        "Return exactly one JSON object matching this structure:\n"
        "{entity_name, entity_canonical_path, entity_type, executive_brief, consulting_grade_portfolio, key_judgments, related_entities, materials, evidence_gaps}\n"
        "consulting_grade_portfolio must contain: strategic_role, operating_profile, ownership_and_control, commercial_relevance, risk_vectors, monitoring_recommendations.\n"
        "key_judgments must contain: statement, implication, confidence, supporting_material_titles, supporting_material_urls.\n"
        "materials must contain only validated, usable materials from VALIDATED_MATERIALS. Do not invent URLs or include failed materials.\n"
        "Each material must include: title, url, publisher, document_type, source_role, priority, evidence_strength, why_it_matters, access_notes, cross_references.\n"
        "evidence_gaps must contain: gap, why_it_matters, recommended_next_step.\n"
        "Base claims only on validated evidence. If support is partial, downgrade confidence or record an evidence gap.\n"
        "Keep the brief fact-dense and decision-useful.\n\n"
        f"ENTITY_CONTEXT:\n{json.dumps(entity_context, ensure_ascii=False, indent=2)}\n\n"
        f"INITIAL_REPORT:\n{json.dumps(initial_report.model_dump(mode='json'), ensure_ascii=False, indent=2)}\n\n"
        f"VALIDATED_MATERIALS:\n{json.dumps(validated_materials, ensure_ascii=False, indent=2)}\n\n"
        f"GAP_ASSESSMENT:\n{json.dumps(gap_assessment, ensure_ascii=False, indent=2)}"
    )
    response = client.models.generate_content(
        model="gemini-3-flash-preview",
        contents=prompt,
        config=types.GenerateContentConfig(temperature=0.2, response_mime_type="application/json"),
    )
    payload = extract_json_object(response.text)
    payload.setdefault("entity_name", entity_context["entity_name"])
    payload.setdefault("entity_canonical_path", entity_context["entity_canonical_path"])
    payload.setdefault("entity_type", entity_context["entity_type"])
    if not payload.get("materials"):
        payload["materials"] = [
            material_to_enrichment_payload(material) for material in stored_materials if material.is_usable
        ]
    return validate_enrichment(payload)


def extract_json_object(raw: str) -> dict:
    text = raw.strip()
    match = re.search(r"```(?:json)?\s*(\{.*\})\s*```", text, re.DOTALL)
    if match:
        text = match.group(1).strip()
    start = text.find("{")
    end = text.rfind("}")
    if start == -1 or end == -1 or end <= start:
        raise RuntimeError(f"model did not return JSON: {raw[:500]}")
    return json.loads(text[start : end + 1])


def persist_run_record(client: firestore.Client, run_record: HarvestRunRecord) -> None:
    payload = run_record.model_dump(mode="json")
    client.collection("vertex_harvest_runs").document(run_record.run_id).set(payload)
    client.collection("vertex_harvest_enrichments").document(run_record.entity_id).set(payload)


def ensure_bucket(client: storage.Client, bucket_name: str, region: str) -> None:
    try:
        client.get_bucket(bucket_name)
        return
    except Exception:  # noqa: BLE001
        pass
    bucket = client.bucket(bucket_name)
    bucket.storage_class = "STANDARD"
    client.create_bucket(bucket, location=region)


def upload_json(client: storage.Client, bucket_name: str, object_name: str, payload: dict) -> str:
    return upload_text(client, bucket_name, object_name, json.dumps(payload, ensure_ascii=False, indent=2), "application/json")


def upload_text(
    client: storage.Client,
    bucket_name: str,
    object_name: str,
    payload: str,
    content_type: str = "text/plain; charset=utf-8",
) -> str:
    return upload_bytes(client, bucket_name, object_name, payload.encode("utf-8"), content_type)


def upload_bytes(
    client: storage.Client,
    bucket_name: str,
    object_name: str,
    payload: bytes,
    content_type: str,
) -> str:
    blob = client.bucket(bucket_name).blob(object_name)
    blob.upload_from_string(payload, content_type=content_type)
    return f"gs://{bucket_name}/{object_name}"


def read_limited_response(response: requests.Response) -> bytes:
    response.raise_for_status()
    out = bytearray()
    for chunk in response.iter_content(chunk_size=8192):
        if not chunk:
            continue
        remaining = RAW_BYTES_LIMIT - len(out)
        if remaining <= 0:
            break
        out.extend(chunk[:remaining])
    return bytes(out)


def extract_text(payload: bytes, content_type: str) -> str:
    lowered = content_type.lower()
    if "pdf" in lowered:
        return extract_pdf_text(payload)
    if "html" in lowered or "<html" in payload[:200].decode("utf-8", errors="ignore").lower():
        return extract_html_text(payload)
    return payload.decode("utf-8", errors="ignore")[:TEXT_PREVIEW_LIMIT]


def extract_pdf_text(payload: bytes) -> str:
    try:
        reader = PdfReader(BytesIO(payload))
    except Exception:  # noqa: BLE001
        return ""
    pages = []
    for page in reader.pages[:6]:
        try:
            pages.append(page.extract_text() or "")
        except Exception:  # noqa: BLE001
            continue
    return "\n".join(pages)[:TEXT_PREVIEW_LIMIT]


def extract_html_text(payload: bytes) -> str:
    soup = BeautifulSoup(payload, "html.parser")
    for tag in soup(["script", "style", "noscript", "header", "footer", "nav", "form", "aside"]):
        tag.decompose()
    main_candidate = soup.find("article") or soup.find("main") or soup.find(attrs={"role": "main"})
    if main_candidate is None:
        candidates = sorted(
            soup.find_all(["section", "div"]),
            key=lambda tag: len(" ".join(tag.stripped_strings)),
            reverse=True,
        )
        main_candidate = candidates[0] if candidates else soup
    text = " ".join(main_candidate.stripped_strings)
    return text[:TEXT_PREVIEW_LIMIT]


def guess_extension(url: str) -> str:
    path = PurePosixPath(urlparse(url).path)
    suffix = path.suffix.lower()
    if suffix and len(suffix) <= 8:
        return suffix
    return ".bin"


def guess_content_type(object_name: str) -> str:
    guessed, _ = mimetypes.guess_type(object_name)
    return guessed or "application/octet-stream"


def slugify(value: str) -> str:
    text = re.sub(r"[^a-zA-Z0-9]+", "-", value.strip().lower()).strip("-")
    return text[:80]


def build_run_id(fragment_id: str) -> str:
    ts = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    return f"{ts}-{fragment_id}-{uuid.uuid4().hex[:8]}"


def first_non_empty(*values: str) -> str:
    for value in values:
        if value and str(value).strip():
            return str(value).strip()
    return ""


def compact_dict(value: dict) -> dict:
    return {k: v for k, v in value.items() if v not in (None, "", [], {})}


def preview_payload(value: object, max_len: int = 500) -> object:
    if isinstance(value, dict):
        return {key: preview_payload(val, max_len=max_len) for key, val in list(value.items())[:8]}
    if isinstance(value, list):
        return [preview_payload(item, max_len=max_len) for item in value[:5]]
    if isinstance(value, str):
        return value if len(value) <= max_len else value[:max_len] + "...[truncated]"
    return value


def expected_source_roles(entity_type: str) -> list[str]:
    roles = ["official_entity_page", "annual_report", "investor_relations", "trade_press"]
    if entity_type in {"FACILITY", "PORT", "TERMINAL"}:
        roles.insert(2, "asset_page")
    else:
        roles.insert(2, "partner_page")
    return roles


def dedupe_source_materials(materials: list[SourceMaterial]) -> list[SourceMaterial]:
    out: list[SourceMaterial] = []
    seen: set[str] = set()
    for material in materials:
        key = canonicalize_url(material.url)
        if not key or key in seen:
            continue
        seen.add(key)
        out.append(material)
    return out


def rank_source_materials(materials: list[SourceMaterial]) -> list[SourceMaterial]:
    role_rank = {
        "official_entity_page": 6,
        "annual_report": 5,
        "investor_relations": 4,
        "sustainability_report": 4,
        "asset_page": 3,
        "partner_page": 3,
        "filing": 3,
        "trade_press": 2,
        "breaking_news": 1,
        "other": 0,
    }
    strength_rank = {"high": 2, "medium": 1, "low": 0}
    return sorted(
        materials,
        key=lambda item: (
            item.priority == "primary",
            role_rank.get(item.source_role, 0),
            strength_rank.get(item.evidence_strength, 0),
        ),
        reverse=True,
    )


def filter_new_materials(candidates: list[SourceMaterial], existing: list[StoredMaterial]) -> list[SourceMaterial]:
    seen = {canonicalize_url(item.final_url or item.url) for item in existing}
    out: list[SourceMaterial] = []
    for candidate in candidates:
        key = canonicalize_url(candidate.url)
        if not key or key in seen:
            continue
        seen.add(key)
        out.append(candidate)
    return out


def assess_material_coverage(entity_context: dict, stored_materials: list[StoredMaterial]) -> dict:
    required_roles = expected_source_roles(entity_context["entity_type"])
    usable = [item for item in stored_materials if item.is_usable]
    covered_roles = sorted({item.source_role for item in usable})
    missing_roles = [role for role in required_roles if role not in covered_roles]
    weak_roles = sorted(
        {
            item.source_role
            for item in stored_materials
            if item.source_role in required_roles and (not item.is_usable or item.quality_label == "low")
        }
    )
    failed_primary = [
        {
            "title": item.title,
            "url": item.url,
            "source_role": item.source_role,
            "fetch_error": item.fetch_error,
            "http_status": item.http_status,
        }
        for item in stored_materials
        if item.priority == "primary" and not item.is_usable
    ]
    return {
        "required_roles": required_roles,
        "covered_roles": covered_roles,
        "missing_roles": missing_roles,
        "weak_roles": weak_roles,
        "failed_primary": failed_primary,
        "usable_material_count": len(usable),
        "usable_titles": [item.title for item in usable],
    }


def should_run_rescue(gap_assessment: dict) -> bool:
    critical_roles = {"official_entity_page"}
    missing_roles = set(gap_assessment.get("missing_roles") or [])
    failed_primary = gap_assessment.get("failed_primary") or []
    covered_roles = set(gap_assessment.get("covered_roles") or [])
    has_report_layer = bool({"annual_report", "investor_relations"} & covered_roles)
    critical_failure = any((item.get("source_role") or "") in (critical_roles | {"annual_report", "investor_relations"}) for item in failed_primary)
    return bool((missing_roles & critical_roles) or not has_report_layer or critical_failure or gap_assessment.get("usable_material_count", 0) < 2)


def discover_rescue_materials(
    *,
    project_id: str,
    location: str,
    entity_context: dict,
    initial_report: HarvestEnrichment,
    stored_materials: list[StoredMaterial],
    gap_assessment: dict,
    limit: int,
    trace_fn=None,
) -> list[SourceMaterial]:
    prompt = (
        "Find replacement or missing materials for this entity. "
        "Return exactly one JSON object with one key: materials.\n\n"
        f"ENTITY_CONTEXT:\n{json.dumps(entity_context, ensure_ascii=False, indent=2)}\n\n"
        f"INITIAL_MATERIALS:\n{json.dumps(initial_report.model_dump(mode='json')['materials'], ensure_ascii=False, indent=2)}\n\n"
        f"CURRENT_FETCH_RESULTS:\n{json.dumps([item.model_dump(mode='json') for item in stored_materials], ensure_ascii=False, indent=2)}\n\n"
        f"GAP_ASSESSMENT:\n{json.dumps(gap_assessment, ensure_ascii=False, indent=2)}\n\n"
        f"Requirements:\n- Return at most {limit} materials.\n"
        "- Focus on replacing failed primary materials and covering missing source roles.\n"
        "- Prefer exact accessible URLs, especially PDF annual reports, investor pages, sustainability reports, official asset/project pages, and operator pages.\n"
        "- Avoid URLs already listed in CURRENT_FETCH_RESULTS.\n"
        "- Each material must contain: title, url, publisher, document_type, source_role, priority, evidence_strength, why_it_matters, access_notes, cross_references."
    )
    payload = run_material_rescue_search(prompt, project_id=project_id, location=location, trace_fn=trace_fn)
    return validate_material_batch(payload)[:limit]


def select_usable_materials(stored_materials: list[StoredMaterial], limit: int) -> list[StoredMaterial]:
    usable = [item for item in stored_materials if item.is_usable]
    usable.sort(
        key=lambda item: (
            item.priority == "primary",
            item.quality_score or 0.0,
            item.evidence_strength == "high",
        ),
        reverse=True,
    )
    return usable[:limit]


def build_synthesis_snippet(record: StoredMaterial, extracted_text: str) -> dict:
    return {
        "title": record.title,
        "url": record.final_url or record.url,
        "publisher": record.publisher,
        "source_role": record.source_role,
        "document_type": record.document_type,
        "quality_label": record.quality_label,
        "quality_rationale": record.quality_rationale,
        "text_preview": extracted_text[:TEXT_PREVIEW_LIMIT],
    }


def is_html_content(content_type: str, payload: bytes) -> bool:
    lowered = content_type.lower()
    return "html" in lowered or "<html" in payload[:200].decode("utf-8", errors="ignore").lower()


def mine_same_domain_candidates(
    *,
    payload: bytes,
    base_url: str,
    material: SourceMaterial,
    entity_context: dict,
) -> list[SourceMaterial]:
    soup = BeautifulSoup(payload, "html.parser")
    base_host = urlparse(base_url).netloc.lower()
    entity_name = entity_context["entity_name"].lower()
    candidates: list[SourceMaterial] = []
    for anchor in soup.find_all("a", href=True):
        href = urljoin(base_url, anchor.get("href", "").strip())
        host = urlparse(href).netloc.lower()
        if not href.startswith("http") or host != base_host:
            continue
        label = " ".join(anchor.stripped_strings)
        searchable = f"{label} {href}".lower()
        if not any(term in searchable for term in ("annual", "report", "investor", "sustain", "esg", "results", "presentation", "pdf", "bmp", "project", "operations")):
            continue
        source_role, document_type = infer_source_role_and_doc_type(searchable)
        title = label or readable_title_from_url(href)
        candidates.append(
            SourceMaterial(
                title=title,
                url=href,
                publisher=material.publisher,
                document_type=document_type,
                source_role=source_role,
                priority="primary" if source_role in {"annual_report", "sustainability_report", "investor_relations", "asset_page"} else "secondary",
                evidence_strength="high" if entity_name.split()[0] in host or source_role != "other" else "medium",
                why_it_matters=f"Discovered on the official {material.publisher} domain while harvesting corroborating evidence.",
                access_notes="Discovered via official site link mining.",
                cross_references=material.cross_references,
            )
        )
    return dedupe_source_materials(candidates)


def infer_source_role_and_doc_type(searchable: str) -> tuple[str, str]:
    if "annual" in searchable and "report" in searchable:
        return "annual_report", "report"
    if "sustain" in searchable or "esg" in searchable:
        return "sustainability_report", "report"
    if "investor" in searchable or "results" in searchable or "presentation" in searchable:
        return "investor_relations", "report"
    if "project" in searchable or "operations" in searchable or "bmp" in searchable:
        return "asset_page", "official_page"
    if "filing" in searchable:
        return "filing", "filing"
    return "other", "official_page"


def score_material_quality(*, record: StoredMaterial, extracted_text: str, entity_context: dict) -> tuple[float, str, str, bool]:
    if record.fetch_error or (record.http_status or 0) >= 400:
        return 0.0, "low", "fetch failed", False
    score = 0.3
    reasons: list[str] = ["fetch succeeded"]
    text_len = len(extracted_text.strip())
    if text_len > 2500:
        score += 0.25
        reasons.append("substantial extracted text")
    elif text_len > 900:
        score += 0.15
        reasons.append("moderate extracted text")
    elif text_len > 300:
        score += 0.05
    entity_tokens = [token for token in re.split(r"[^a-z0-9]+", entity_context["entity_name"].lower()) if len(token) > 3]
    if any(token in extracted_text.lower() for token in entity_tokens[:3]):
        score += 0.12
        reasons.append("entity terms present")
    if record.source_role in {"official_entity_page", "annual_report", "sustainability_report", "investor_relations", "asset_page", "filing"}:
        score += 0.15
        reasons.append("high-value source role")
    if record.document_type in {"report", "official_page", "filing"}:
        score += 0.08
    if "pdf" in (record.content_type or "").lower():
        score += 0.08
        reasons.append("pdf-like document")
    noise_hits = sum(
        term in extracted_text[:2000].lower()
        for term in ("privacy policy", "terms of use", "newsletter", "advertise", "all rights reserved", "latest news", "contact us")
    )
    if noise_hits >= 3:
        score -= 0.18
        reasons.append("layout/navigation heavy")
    score = max(0.0, min(score, 1.0))
    label = "high" if score >= 0.72 else "medium" if score >= 0.52 else "low"
    usable = label != "low" and text_len >= 300
    return score, label, ", ".join(reasons), usable


def material_to_enrichment_payload(material: StoredMaterial) -> dict:
    return {
        "title": material.title,
        "url": material.final_url or material.url,
        "publisher": material.publisher,
        "document_type": material.document_type,
        "source_role": material.source_role,
        "priority": material.priority,
        "evidence_strength": material.evidence_strength,
        "why_it_matters": material.why_it_matters,
        "access_notes": f"Validated fetch with quality={material.quality_label}. {material.quality_rationale or ''}".strip(),
        "cross_references": [item.model_dump(mode="json") for item in material.cross_references],
    }


def canonicalize_url(value: str) -> str:
    if not value:
        return ""
    parsed = urlparse(normalize_result_url(value.strip()))
    return f"{parsed.scheme.lower()}://{parsed.netloc.lower()}{parsed.path}".rstrip("/")


def readable_title_from_url(value: str) -> str:
    path = PurePosixPath(urlparse(value).path)
    stem = path.stem or path.name or "document"
    return re.sub(r"[-_]+", " ", stem).strip().title()


def normalize_result_url(value: str) -> str:
    if not value:
        return ""
    parsed = urlparse(value)
    host = parsed.netloc.lower()
    if host == "validate.perfdrive.com":
        qs = parse_qs(parsed.query)
        target = (qs.get("ssc") or [""])[0]
        if target:
            return unquote(target)
    return value


def normalize_enrichment_payload(payload: dict) -> dict:
    normalized = dict(payload)
    portfolio = dict(normalized.get("consulting_grade_portfolio") or {})
    for key in ("risk_vectors", "monitoring_recommendations"):
        portfolio[key] = normalize_string_list(portfolio.get(key))
    normalized["consulting_grade_portfolio"] = portfolio

    normalized["key_judgments"] = [
        normalize_key_judgment(item) for item in (normalized.get("key_judgments") or []) if item
    ]

    normalized["related_entities"] = [
        normalize_related_entity(item) for item in (normalized.get("related_entities") or []) if item
    ]

    normalized["evidence_gaps"] = [
        normalize_evidence_gap(item) for item in (normalized.get("evidence_gaps") or []) if item
    ]

    materials = []
    for item in normalized.get("materials") or []:
        if not item:
            continue
        material = dict(item)
        material["document_type"] = normalize_document_type(material.get("document_type"))
        material["source_role"] = normalize_source_role(material.get("source_role"), material["document_type"])
        material["priority"] = normalize_priority(material.get("priority"))
        material["evidence_strength"] = normalize_evidence_strength(material.get("evidence_strength"))
        material["cross_references"] = normalize_related_entity_list(material.get("cross_references"))
        materials.append(material)
    normalized["materials"] = materials
    return normalized


def normalize_related_entity(value: dict) -> dict:
    if isinstance(value, dict):
        item = dict(value)
    else:
        text = str(value).strip()
        item = {
            "name": text,
            "role": "",
            "why_relevant": "",
            "canonical_path_hint": None,
        }
    return {
        "name": str(item.get("name", "")).strip(),
        "role": str(item.get("role", "")).strip(),
        "why_relevant": str(item.get("why_relevant", "")).strip(),
        "canonical_path_hint": str(item.get("canonical_path_hint", "")).strip() or None,
    }


def normalize_related_entity_list(value: object) -> list[dict]:
    if value is None:
        return []
    if isinstance(value, list):
        return [normalize_related_entity(item) for item in value if isinstance(item, dict) and item]
    if isinstance(value, dict):
        return [normalize_related_entity(value)]
    return []


def normalize_key_judgment(value: dict) -> dict:
    item = dict(value)
    return {
        "statement": str(item.get("statement", "")).strip(),
        "implication": str(item.get("implication", "")).strip(),
        "confidence": normalize_evidence_strength(item.get("confidence")),
        "supporting_material_titles": normalize_string_list(item.get("supporting_material_titles")),
        "supporting_material_urls": normalize_string_list(item.get("supporting_material_urls")),
    }


def normalize_evidence_gap(value: dict) -> dict:
    item = dict(value)
    return {
        "gap": str(item.get("gap", "")).strip(),
        "why_it_matters": str(item.get("why_it_matters", "")).strip(),
        "recommended_next_step": str(item.get("recommended_next_step", "")).strip(),
    }


def normalize_string_list(value: object) -> list[str]:
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    if value is None:
        return []
    text = str(value).strip()
    if not text:
        return []
    parts = re.split(r"\n+|(?:^|\s)[-\u2022]\s+|;\s+", text)
    normalized = [part.strip(" -\u2022\t\r\n") for part in parts if part.strip(" -\u2022\t\r\n")]
    if normalized:
        return normalized
    return [text]


def normalize_document_type(value: object) -> str:
    text = str(value or "").strip().lower()
    if "report" in text or "deck" in text or "analysis" in text:
        return "report"
    if "official" in text or "company page" in text or "profile" in text:
        return "official_page"
    if "filing" in text or "exchange" in text:
        return "filing"
    if "news" in text or "press" in text:
        return "news"
    if "market" in text or "trade" in text:
        return "market_note"
    return "other"


def normalize_source_role(value: object, document_type: str) -> str:
    text = str(value or "").strip().lower()
    mapping = {
        "official": "official_entity_page",
        "official_page": "official_entity_page",
        "company_page": "official_entity_page",
        "annual_report": "annual_report",
        "sustainability": "sustainability_report",
        "sustainability_report": "sustainability_report",
        "investor": "investor_relations",
        "investor_relations": "investor_relations",
        "asset": "asset_page",
        "asset_page": "asset_page",
        "project_page": "asset_page",
        "filing": "filing",
        "partner": "partner_page",
        "partner_page": "partner_page",
        "trade_press": "trade_press",
        "trade": "trade_press",
        "breaking_news": "breaking_news",
        "news": "breaking_news",
        "other": "other",
    }
    for key, normalized in mapping.items():
        if key in text:
            return normalized
    if document_type == "filing":
        return "filing"
    if document_type == "report":
        return "annual_report"
    if document_type == "official_page":
        return "official_entity_page"
    if document_type == "news":
        return "breaking_news"
    if document_type == "market_note":
        return "trade_press"
    return "other"


def normalize_priority(value: object) -> str:
    text = str(value or "").strip().lower()
    if text in {"high", "primary", "p1"}:
        return "primary"
    return "secondary"


def normalize_evidence_strength(value: object) -> str:
    text = str(value or "").strip().lower()
    if text in {"high", "strong", "primary"}:
        return "high"
    if text in {"low", "weak"}:
        return "low"
    return "medium"


if __name__ == "__main__":
    main()
