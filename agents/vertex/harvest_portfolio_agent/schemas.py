from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field


class RelatedEntity(BaseModel):
    name: str
    role: str
    why_relevant: str
    canonical_path_hint: str | None = None


class SourceMaterial(BaseModel):
    title: str
    url: str
    publisher: str
    document_type: Literal["report", "official_page", "filing", "news", "market_note", "other"]
    source_role: Literal[
        "official_entity_page",
        "annual_report",
        "sustainability_report",
        "investor_relations",
        "asset_page",
        "filing",
        "partner_page",
        "trade_press",
        "breaking_news",
        "other",
    ] = "other"
    priority: Literal["primary", "secondary"]
    evidence_strength: Literal["high", "medium", "low"] = "medium"
    why_it_matters: str
    access_notes: str | None = None
    cross_references: list[RelatedEntity] = Field(default_factory=list)


class KeyJudgment(BaseModel):
    statement: str
    implication: str
    confidence: Literal["high", "medium", "low"] = "medium"
    supporting_material_titles: list[str] = Field(default_factory=list)
    supporting_material_urls: list[str] = Field(default_factory=list)


class EvidenceGap(BaseModel):
    gap: str
    why_it_matters: str
    recommended_next_step: str


class PortfolioSections(BaseModel):
    strategic_role: str
    operating_profile: str
    ownership_and_control: str
    commercial_relevance: str
    risk_vectors: list[str] = Field(default_factory=list)
    monitoring_recommendations: list[str] = Field(default_factory=list)


class HarvestEnrichment(BaseModel):
    entity_name: str
    entity_canonical_path: str
    entity_type: str
    executive_brief: str
    consulting_grade_portfolio: PortfolioSections
    key_judgments: list[KeyJudgment] = Field(default_factory=list)
    related_entities: list[RelatedEntity] = Field(default_factory=list)
    materials: list[SourceMaterial] = Field(default_factory=list)
    evidence_gaps: list[EvidenceGap] = Field(default_factory=list)


class StoredMaterial(BaseModel):
    title: str
    url: str
    final_url: str | None = None
    publisher: str
    document_type: str
    source_role: str = "other"
    priority: str
    evidence_strength: str = "medium"
    why_it_matters: str
    content_type: str | None = None
    http_status: int | None = None
    raw_gcs_uri: str | None = None
    extracted_text_gcs_uri: str | None = None
    extracted_text_chars: int | None = None
    quality_score: float | None = None
    quality_label: str | None = None
    quality_rationale: str | None = None
    is_usable: bool = False
    fetch_error: str | None = None
    fetched_at: datetime | None = None
    cross_references: list[RelatedEntity] = Field(default_factory=list)


class MaterialBatch(BaseModel):
    materials: list[SourceMaterial] = Field(default_factory=list)


class HarvestRunRecord(BaseModel):
    run_id: str
    project_id: str
    tenant_id: str
    entity_id: str
    entity_name: str
    entity_canonical_path: str
    entity_type: str
    bucket: str
    prefix: str
    initial_report_gcs_uri: str
    final_report_gcs_uri: str
    materials: list[StoredMaterial] = Field(default_factory=list)
    enrichment: HarvestEnrichment
    usable_material_count: int = 0
    created_at: datetime
