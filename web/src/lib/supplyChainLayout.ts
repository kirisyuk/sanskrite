/**
 * Supply Chain layout module.
 * Filters nodes/edges to supply-chain entities and applies simple hierarchical layout.
 * Right-to-left: right = upstream, left = downstream. No blocks.
 */

import type { Node, Edge } from 'reactflow';
import { applyElkLayout } from './elkLayout';
import type { LayoutOptions_Custom } from './elkLayout';

const SUPPLY_CHAIN_PATH_PREFIXES = [
  '/gov/',
  '/org/',
  '/deposit/',
  '/product/',
  '/fac/',
  '/loc/',
  '/region/',
];

/** Canonical path prefixes for supply chain nodes. */
export function isSupplyChainCanonicalPath(path: string): boolean {
  const p = String(path || '').trim();
  if (!p || !p.startsWith('/')) return false;
  return SUPPLY_CHAIN_PATH_PREFIXES.some((prefix) => p.startsWith(prefix));
}

/** Edge type strength for deduplication: lower rank = stronger. Unknown types → 40. */
export const SUPPLY_CHAIN_EDGE_STRENGTH: Record<string, number> = {
  EXTRACTS_FROM: 0,
  PRODUCES: 1,
  transforms_into: 2,
  SUPPLIES_TO: 3,
  CONSUMES: 4,
  DEPENDS_ON: 5,
  REQUIRES: 6,
  NEEDS: 6,
  USES: 6,
  LOCATED_IN: 7,
  OPERATES_IN: 8,
  BASED_IN: 8,
  HEADQUARTERED_IN: 8,
  REGULATES: 9,
  ADMINISTERS: 10,
  PARTICIPATES_IN: 11,
  TRADED_IN: 11,
  DOMINATES: 11,
  HAS_MARKET_SEGMENT: 12,
  HAS_SUBMARKET: 12,
  INFLUENCES: 13,
  AFFECTS: 14,
  IMPACTS: 14,
  OWNS: 15,
  SUBSIDIARY_OF: 15,
  CONTROLS: 16,
  MANAGES: 16,
  INVESTS_IN: 17,
  FOUNDED: 17,
  BOARD_MEMBER_OF: 18,
  EXECUTIVE_OF: 18,
  EMPLOYED_BY: 18,
  ASSURES: 19,
  AUDITED_BY: 19,
  COMPETES_WITH: 20,
  CONFLICTS_WITH: 20,
  OPPOSES: 20,
  PARTNERS_WITH: 21,
  COOPERATES_WITH: 21,
  ASSOCIATED_WITH: 21,
  CONTROLS_ACCESS: 22,
  IMPOSES_CONDITION: 22,
  GATEKEEPS_DECISION: 23,
  USES_MECHANISM: 23,
  APPLIES_TO: 23,
  FOCUSES_ON: 24,
  INVOLVES_ACTOR: 24,
  IN_GEO_SCOPE: 24,
  ABOUT: 24,
  RELATED_DOMAIN: 25,
  DOMAIN_RELATION: 25,
  BROADENS_TO: 25,
  NARROWS_TO: 25,
  SURFACES: 26,
  TRIGGERED_BY: 26,
  MEMBER_OF: 27,
  AFFILIATED_WITH: 27,
  RELATED_TO: 28,
  RELATIVE_OF: 28,
  WORKS_FOR: 28,
  KEY_PERSON: 29,
  HAS_KEY_PERSON: 29,
  OPERATES_VIA: 30,
  HAS_SUBGROUP: 31,
  CORPORATE_TRANSITION: 32,
  RENAMED_TO: 32,
  REBRANDED_TO: 32,
  LEGAL_SUCCESSOR_OF: 33,
  MERGED_INTO: 33,
  SPUN_OFF_FROM: 33,
  ACQUIRED_BY: 33,
  LINKED_TO: 40,
  SIMILAR_TO: 40,
  SUPPORTS: 40,
};

const DEFAULT_EDGE_STRENGTH = 40;

function normalizeEdgeType(t: string | undefined): string {
  return String(t || '').toUpperCase().trim() || 'OTHER';
}

function getEdgeStrength(edgeType: string): number {
  const norm = normalizeEdgeType(edgeType);
  return SUPPLY_CHAIN_EDGE_STRENGTH[norm] ?? DEFAULT_EDGE_STRENGTH;
}

function getNodeCanonicalPath(n: Node): string {
  const d = (n.data as any) || {};
  const cp = String(d.canonicalPath || d.canonical_path || '').trim();
  if (cp) return cp;
  const fid = String(d.fragmentId || d.fragment_id || n.id || '').trim();
  if (fid.startsWith('/')) return fid;
  return '';
}

/** Filter nodes to supply-chain canonical paths. Layout recreates groups/virtual nodes. */
export function filterSupplyChainNodes<T = any>(nodes: Node<T>[]): Node<T>[] {
  const isGroup = (n: Node) => Boolean((n.data as any)?.isGroup);
  const isVirtual = (n: Node) => {
    const id = String((n as any)?.id || '').trim();
    return (
      id.startsWith('levels:') ||
      id.startsWith('linear:') ||
      id.includes(':card:') ||
      id.includes(':ctrl:')
    );
  };

  return nodes.filter((n) => {
    if (isGroup(n) || isVirtual(n)) return false;
    const path = getNodeCanonicalPath(n);
    return path && isSupplyChainCanonicalPath(path);
  });
}

/** Filter edges to those between supply-chain nodes. Deduplicate by (source, target) keeping strongest. */
export function filterSupplyChainEdges<T = any>(
  edges: Edge[],
  nodeIds: Set<string>
): Edge[] {
  const byPair = new Map<string, Edge>();
  for (const e of edges) {
    const src = String(e.source || '').trim();
    const trg = String(e.target || '').trim();
    if (!nodeIds.has(src) || !nodeIds.has(trg)) continue;
    const key = `${src}::${trg}`;
    const et = (e.data as any)?.edgeType || (e.data as any)?.type || (e as any)?.label || '';
    const strength = getEdgeStrength(et);
    const existing = byPair.get(key);
    const existingStrength = existing
      ? getEdgeStrength((existing.data as any)?.edgeType || (existing.data as any)?.type || (existing as any)?.label || '')
      : DEFAULT_EDGE_STRENGTH;
    if (!existing || strength < existingStrength) {
      byPair.set(key, e);
    }
  }
  return Array.from(byPair.values());
}

/** Layout supply chain: apply simple ELK hierarchical layout to ALL content nodes.
 * Direction LEFT = right-to-left flow: right = upstream, left = downstream.
 * No groups, no filtering of types (show all expanded nodes).
 */
export async function layoutSupplyChain<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: LayoutOptions_Custom = {}
): Promise<Node<T>[]> {
  // Filter out existing groups/virtuals to ensure clean slate, but keep ALL content nodes.
  const isGroup = (n: Node) => Boolean((n.data as any)?.isGroup);
  const isVirtual = (n: Node) => {
    const id = String((n as any)?.id || '').trim();
    return (
      id.startsWith('levels:') ||
      id.startsWith('linear:') ||
      id.includes(':card:') ||
      id.includes(':ctrl:')
    );
  };
  
  const contentNodes = nodes.filter((n) => !isGroup(n) && !isVirtual(n));
  const nodeIdSet = new Set(contentNodes.map((n) => String(n.id).trim()));
  
  // Keep edges only between valid content nodes
  const validEdges = edges.filter(e => 
    nodeIdSet.has(String(e.source).trim()) && 
    nodeIdSet.has(String(e.target).trim())
  );

  return applyElkLayout(contentNodes, validEdges, {
    ...options,
    direction: 'LEFT', // Right = upstream, left = downstream
  });
}
