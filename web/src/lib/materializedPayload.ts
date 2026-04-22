/**
 * Readers for materialized SUPPLY_CHAIN_PATH (and similar) payloads — AH `class` / `relation`
 * with legacy `type` / UPPER_SNAKE fallbacks. See docs/taxonomy/materialized_payload.md.
 */
import type { FragmentClass } from './taxonomy.generated';
import { FRAGMENT_CLASS, RELATION } from './taxonomy.generated';
import { normalizeEdgeType, tryParseFragmentClass } from './taxonomy';

/** Effective fragment class from a path payload node (`class` preferred, else legacy `type`). */
export function pathPayloadNodeFragmentClass(node: unknown): FragmentClass | undefined {
  const n = node as Record<string, unknown> | null | undefined;
  if (!n) return undefined;
  const cls = String(n.class ?? '').trim();
  const legacy = String(n.type ?? '').trim();
  return tryParseFragmentClass(cls || legacy);
}

/** True if payload node represents a product (AH `class: product`, legacy `type`, or `/product/` ref). */
export function pathPayloadNodeIsProduct(node: unknown): boolean {
  const n = node as Record<string, unknown> | null | undefined;
  const ref = String(n?.ref ?? '').trim();
  if (ref.startsWith('/product/')) return true;
  return pathPayloadNodeFragmentClass(node) === FRAGMENT_CLASS.Product;
}

/**
 * Canonical AH relation for a path payload edge (`relation` preferred; legacy `type` + `role`).
 */
export function pathPayloadEdgeRelation(edge: unknown): string {
  const e = edge as Record<string, unknown> | null | undefined;
  if (!e) return RELATION.Supplies;
  const relRaw = String(e.relation ?? '').trim();
  if (relRaw) return normalizeEdgeType(relRaw);
  const role = String(e.role ?? '').trim();
  const typ = String(e.type ?? '').trim();
  if (role === 'logistics_leg' || typ === 'LOGISTICS') return RELATION.LegConnects;
  if (typ === 'supply_link') return RELATION.Supplies;
  return RELATION.Supplies;
}
