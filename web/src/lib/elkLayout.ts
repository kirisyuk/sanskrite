/**
 * ELK (Eclipse Layout Kernel) integration for ReactFlow
 *
 * Provides hierarchical layout with:
 * - Layered algorithm (Sugiyama-style)
 * - Wrapping to prevent too many nodes in one row
 * - Incremental mode (existing nodes don't move)
 * - Edge routing with minimal crossings
 */

import ELK, { ElkNode, ElkExtendedEdge, LayoutOptions } from 'elkjs/lib/elk.bundled.js';
import type { Node, Edge } from 'reactflow';

const elk = new ELK();

// Default node dimensions (should match your HorizonFragmentNode)
export const DEFAULT_NODE_WIDTH = 180;
// Layout height includes the "+" button protruding half outside the visual card (26px button => +13px).
export const DEFAULT_NODE_HEIGHT = 161;

// Compact node dimensions (for left pane groups in Levels layout)
export const COMPACT_NODE_WIDTH = 180;
// Goal: 3 compact nodes fit into one full (layout) slot: 3*49 + 2*7 = 161
export const COMPACT_NODE_HEIGHT = 49;
export const COMPACT_NODE_LAYOUT_HEIGHT = 49;

// Maximum rows for children (3 = more rows, fewer columns)
const MAX_CHILD_ROWS = 3;

// Will be calculated dynamically to fit MAX_CHILD_ROWS
const DEFAULT_MAX_NODES_PER_ROW = 8;

// Layout options for hierarchical (layered) layout
const DEFAULT_LAYOUT_OPTIONS: LayoutOptions = {
  'elk.algorithm': 'layered',
  'elk.direction': 'DOWN', // Top to bottom
  'elk.spacing.nodeNode': '60', // Horizontal spacing between nodes
  'elk.layered.spacing.nodeNodeBetweenLayers': '100', // Vertical spacing between layers
  'elk.layered.spacing.edgeNodeBetweenLayers': '40', // Space between edges and nodes
  'elk.layered.spacing.edgeEdgeBetweenLayers': '20', // Space between edges
  'elk.spacing.componentComponent': '80', // Space between disconnected components
  'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES', // Respect order
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP', // Minimize edge crossings
  'elk.layered.crossingMinimization.greedySwitch.type': 'TWO_SIDED', // Better crossing minimization
  'elk.edgeRouting': 'ORTHOGONAL', // Clean right-angle edges
  'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX', // Better node placement
  'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED', // Balanced alignment
  'elk.hierarchyHandling': 'INCLUDE_CHILDREN', // Handle nested graphs
  'elk.layered.wrapping.strategy': 'MULTI_EDGE', // Wrap long layers
  'elk.layered.wrapping.additionalEdgeSpacing': '40', // Extra space when wrapping
  'elk.separateConnectedComponents': 'true', // Handle disconnected parts separately
  'elk.layered.compaction.connectedComponents': 'true', // Compact connected components
  'elk.aspectRatio': '1.5', // Prefer wider layouts
};

// Options for incremental layout (don't move existing nodes)
const INCREMENTAL_LAYOUT_OPTIONS: LayoutOptions = {
  ...DEFAULT_LAYOUT_OPTIONS,
  'elk.layered.crossingMinimization.semiInteractive': 'true',
};

// Priority of edge types for placement + ordering (lower = more important).
//
// This is used in two critical places:
// - to pick a "canonical" relation when a node has multiple edges to the central node
//   (determines which group/quadrant it belongs to)
// - to sort nodes inside a group (important first)
//
// Product rule of thumb:
// - Ownership/control is the strongest signal (e.g. OWNS beats INFLUENCES)
// - Then: products & dependency (inputs/outputs)
// - Then: market relations (competition/partnership)
// - Then: regulation/influence
// - Then: generic context (geography, location)
const EDGE_TYPE_PRIORITY: Record<string, number> = {
  // 0) Market-defining relation (stronger than generic influence/competition for placement)
  'DOMINATES': 0,
  // 1) Ownership / control (strongest)
  'OWNS': 1,
  'PARENT_OF': 1,
  'SUBSIDIARY_OF': 1,
  'MANAGES': 1,
  'CONTROLS': 1,
  'BOARD_MEMBER_OF': 1,
  'EXECUTIVE_OF': 1,
  // Governance (strong, but not control)
  'AUDITED_BY': 2,
  // Administrative hierarchy (gov skeleton)
  'ADMINISTERS': 2,

  // 2) Products & dependency (inputs/outputs)
  'PRODUCES': 2,
  'SUPPLIES_TO': 2,
  'CONSUMES': 2,
  'DEPENDS_ON': 2,
  'REQUIRES': 2,
  'USES': 2,
  'NEEDS': 2,
  'EXTRACTS_FROM': 2,
  // Market hierarchy (market skeleton)
  'HAS_SUBMARKET': 2,
  // Community hierarchy (dynasty/tribe skeleton)
  'HAS_SUBGROUP': 2,

  // 3) Market (peers)
  'COMPETES_WITH': 3,
  'CONFLICTS_WITH': 3,
  'OPPOSES': 3,
  'PARTNERS_WITH': 3,
  'COOPERATES_WITH': 3,
  // Market participation (org in market)
  'PARTICIPATES_IN': 3,
  // Person-person social graph (peers)
  'RELATED_TO': 3,
  'RELATIVE_OF': 3,
  // Community relationships
  'MEMBER_OF': 3,
  'AFFILIATED_WITH': 3,
  'OPERATES_VIA': 2,

  // 4) Regulation / influence
  'REGULATES': 4,
  'INFLUENCES': 4,
  'AFFECTS': 4,
  'IMPACTS': 4,

  // 5) Context / geography (weakest)
  'LOCATED_IN': 5,
  'OPERATES_IN': 5,
  'BASED_IN': 5,
  'HEADQUARTERED_IN': 5,
};
const DEFAULT_EDGE_PRIORITY = 6;

export const getEdgePriority = (edgeType: string | undefined): number => {
  const type = (edgeType || '').toUpperCase();
  return EDGE_TYPE_PRIORITY[type] ?? DEFAULT_EDGE_PRIORITY;
};

// Hot path: normalizeEdgeType is called a lot during layout + edge aggregation.
// Cache results to avoid repeated string allocations/uppercasing.
const _normalizeEdgeTypeCache = new Map<string, string>();
const normalizeEdgeType = (t: string | undefined) => {
  const raw = String(t || '');
  // Preserve legacy behavior: empty -> OTHER
  const key = raw ? raw.toUpperCase().trim() : 'OTHER';
  const norm = key || 'OTHER';
  const hit = _normalizeEdgeTypeCache.get(norm);
  if (hit) return hit;
  _normalizeEdgeTypeCache.set(norm, norm);
  return norm;
};

/**
 * Semantic groups for business-oriented graph layout.
 * Maps edge types to one of 4 groups positioned around the parent node.
 */
export type SemanticGroup = 'supply-chain' | 'customers' | 'market' | 'context';

/**
 * Legacy "linear" layout sections (used by LensCanvasCockpit Linear mode and UI labels).
 * These are a coarse 2x2 quadrant split plus the central band.
 */
export type LinearSection = 'top-left' | 'top-right' | 'bottom-left' | 'bottom-right' | 'center';

export const SEMANTIC_GROUP_LABELS: Record<SemanticGroup, string> = {
  'supply-chain': 'Supply Chain',
  'customers': 'Customers & Outputs',
  'market': 'Market Position',
  'context': 'Operating Context',
};

export const SEMANTIC_GROUP_POSITION: Record<SemanticGroup, 'top' | 'bottom' | 'left' | 'right'> = {
  'supply-chain': 'top',    // upstream — сверху
  'customers': 'bottom',     // downstream — снизу
  'market': 'left',          // peers — слева
  'context': 'right',        // context — справа
};

/**
 * Grid-based layout system for hierarchical node expansion.
 * Each cell can contain a node or a group. The grid grows dynamically
 * as nodes are expanded.
 */
export interface GridCell {
  row: number;
  col: number;
  type: 'node' | 'group' | 'empty';
  nodeId?: string;           // Fragment ID if type is 'node'
  groupId?: string;          // Group ID if type is 'group'
  semanticGroup?: SemanticGroup; // For groups: which semantic category
  parentNodeId?: string;     // Which node this cell belongs to (for groups)
}

export interface GridState {
  cells: Map<string, GridCell>;  // Key: "row:col"
  bounds: {
    minRow: number;
    maxRow: number;
    minCol: number;
    maxCol: number;
  };
  rootNodeId: string | null;     // The first expanded node (center of grid)
  expandedNodes: Set<string>;    // Nodes that have been expanded (have children)
}

// Grid cell size must fit a full subgraph around a center node (node + groups on all sides).
// We conservatively size it based on the maximum possible group container (4x3 children + padding),
// plus layer spacing between the center and each group.
const GRID_LAYER_SPACING = 150; // keep in sync with default layout layerSpacing
const MAX_GROUP_CONTAINER_WIDTH = 4 * DEFAULT_NODE_WIDTH + 3 * 60 + 32;  // ~920px
const MAX_GROUP_CONTAINER_HEIGHT = 3 * DEFAULT_NODE_HEIGHT + 2 * 63 + 34 + 16 + 24; // ~631px
export const GRID_CELL_WIDTH =
  DEFAULT_NODE_WIDTH + 2 * GRID_LAYER_SPACING + 2 * MAX_GROUP_CONTAINER_WIDTH; // ~2320px
export const GRID_CELL_HEIGHT =
  DEFAULT_NODE_HEIGHT + 2 * GRID_LAYER_SPACING + 2 * MAX_GROUP_CONTAINER_HEIGHT; // ~1723px

const gridKey = (row: number, col: number) => `${row}:${col}`;

export function createEmptyGrid(): GridState {
  return {
    cells: new Map(),
    bounds: { minRow: 0, maxRow: 0, minCol: 0, maxCol: 0 },
    rootNodeId: null,
    expandedNodes: new Set(),
  };
}

export function getGridCell(grid: GridState, row: number, col: number): GridCell | undefined {
  return grid.cells.get(gridKey(row, col));
}

export function setGridCell(grid: GridState, cell: GridCell): GridState {
  const newCells = new Map(grid.cells);
  newCells.set(gridKey(cell.row, cell.col), cell);

  return {
    ...grid,
    cells: newCells,
    bounds: {
      minRow: Math.min(grid.bounds.minRow, cell.row),
      maxRow: Math.max(grid.bounds.maxRow, cell.row),
      minCol: Math.min(grid.bounds.minCol, cell.col),
      maxCol: Math.max(grid.bounds.maxCol, cell.col),
    },
  };
}

/**
 * Find the grid cell for a node that's being expanded from a group.
 * Returns the cell position based on the semantic group's direction.
 */
export function findCellForExpandedNode(
  grid: GridState,
  parentNodeId: string,
  semanticGroup: SemanticGroup,
): { row: number; col: number } {
  // Find parent node's cell
  let parentRow = 0;
  let parentCol = 0;
  let foundParent = false;
  for (const [, cell] of grid.cells) {
    if (cell.type === 'node' && cell.nodeId === parentNodeId) {
      parentRow = cell.row;
      parentCol = cell.col;
      foundParent = true;
      break;
    }
  }
  // Fallback: if parent isn't registered in grid yet, anchor to root (if available).
  // This avoids silently anchoring to (0,0) when we actually have a known root elsewhere.
  if (!foundParent && grid.rootNodeId) {
    for (const [, cell] of grid.cells) {
      if (cell.type === 'node' && cell.nodeId === grid.rootNodeId) {
        parentRow = cell.row;
        parentCol = cell.col;
        break;
      }
    }
  }

  // Determine offset based on semantic group direction
  const position = SEMANTIC_GROUP_POSITION[semanticGroup];
  let offsetRow = 0;
  let offsetCol = 0;

  switch (position) {
    case 'top':
      offsetRow = -1;
      break;
    case 'bottom':
      offsetRow = 1;
      break;
    case 'left':
      offsetCol = -1;
      break;
    case 'right':
      offsetCol = 1;
      break;
  }

  // Zone Preservation Logic:
  // If parent is in a specific zone (e.g. Right: col > 0), ensure children don't cross into Center (col=0) or Left (col<0).
  const parentInRight = parentCol > 0;
  const parentInLeft = parentCol < 0;
  const parentInBottom = parentRow > 0;
  const parentInTop = parentRow < 0;

  const isValidZone = (r: number, c: number) => {
    if (parentInRight && c <= 0) return false;
    if (parentInLeft && c >= 0) return false;
    if (parentInBottom && r <= 0) return false;
    if (parentInTop && r >= 0) return false;
    return true;
  };

  const isOccupied = (r: number, c: number) => {
    const cell = getGridCell(grid, r, c);
    return cell && cell.type !== 'empty';
  };

  // 1. Try ideal position
  const idealRow = parentRow + offsetRow;
  const idealCol = parentCol + offsetCol;

  if (isValidZone(idealRow, idealCol) && !isOccupied(idealRow, idealCol)) {
    return { row: idealRow, col: idealCol };
  }

  // 2. Search for nearest valid free cell using "expanding square" strategy around the ideal target
  // We center search around ideal target to prioritize directional intent, but strictly filter by zone.
  for (let r = 1; r <= 20; r++) {
    for (let dr = -r; dr <= r; dr++) {
      for (let dc = -r; dc <= r; dc++) {
        // Optimization: only check perimeter of current ring
        if (Math.abs(dr) !== r && Math.abs(dc) !== r) continue;

        const checkRow = idealRow + dr;
        const checkCol = idealCol + dc;

        if (isValidZone(checkRow, checkCol) && !isOccupied(checkRow, checkCol)) {
          return { row: checkRow, col: checkCol };
        }
      }
    }
  }

  // Fallback (should rarely happen): just return ideal and let it overlap or handle elsewhere
  return { row: idealRow, col: idealCol };
}

/**
 * Convert grid coordinates to canvas position.
 */
export function gridToCanvasPosition(
  row: number,
  col: number,
  nodeWidth: number = DEFAULT_NODE_WIDTH,
  nodeHeight: number = DEFAULT_NODE_HEIGHT,
): { x: number; y: number } {
  // Center of grid is (0, 0)
  // Each cell is GRID_CELL_WIDTH x GRID_CELL_HEIGHT
  // Node is centered within its cell
  const cellCenterX = col * GRID_CELL_WIDTH;
  const cellCenterY = row * GRID_CELL_HEIGHT;

  return {
    x: cellCenterX - nodeWidth / 2,
    y: cellCenterY - nodeHeight / 2,
  };
}

/**
 * Map edge type + direction to semantic group.
 * Direction is relative to the expanded node:
 * - outgoing: edge goes FROM expanded node TO neighbor
 * - incoming: edge goes FROM neighbor TO expanded node
 */
export function getSemanticGroup(edgeType: string | undefined, isOutgoing: boolean): SemanticGroup {
  const type = normalizeEdgeType(edgeType);
  
  switch (type) {
    // Market-defining relation:
    // - If the central node DOMINATES the neighbor (outgoing), the neighbor is "market" (left).
    // - If the neighbor DOMINATES the central node (incoming), the neighbor is "context" (right).
    // This places orgs that define a market to the RIGHT of the market node.
    case 'DOMINATES':
      return isOutgoing ? 'market' : 'context';
    // People / governance relationships should be treated as "context" for placement:
    // founders/board/executives/staff are not supply-chain nor customers.
    case 'FOUNDED':
    case 'BOARD_MEMBER_OF':
    case 'EXECUTIVE_OF':
    case 'EMPLOYED_BY':
    case 'KEY_PERSON':
    case 'WORKS_FOR':
      return 'context';

    // Hierarchies (skeleton):
    // - Market hierarchy: parent should appear to the RIGHT, child to the LEFT.
    case 'HAS_SUBMARKET':
      return isOutgoing ? 'market' : 'context';
    // - Government admin hierarchy: treat as governance/context for left-group placement.
    case 'ADMINISTERS':
      return 'context';
    
    // Community relationships
    case 'OPERATES_VIA':
      return 'context';  // channels used by community
    case 'MEMBER_OF':
      return isOutgoing ? 'context' : 'supply-chain';  // person is member of community
    case 'AFFILIATED_WITH':
      return 'context';  // affiliation with formal org
    case 'HAS_SUBGROUP':
      return isOutgoing ? 'market' : 'context';  // community hierarchy (like HAS_SUBMARKET)

    // Supply Chain vs Customers depends on direction
    case 'SUPPLIES_TO':
      return isOutgoing ? 'customers' : 'supply-chain';  // outgoing = we supply to them (customer), incoming = they supply to us (supplier)
    case 'DEPENDS_ON':
      return isOutgoing ? 'supply-chain' : 'customers';  // outgoing = we depend on them (upstream), incoming = they depend on us (downstream)
    case 'EXTRACTS_FROM':
      return isOutgoing ? 'supply-chain' : 'customers';  // outgoing = we extract from them (upstream resource), incoming = they extract from us (rare)
    case 'CONSUMES':
      return 'supply-chain';  // we consume something = upstream
    
    // Ownership: OWNS направленная на центральную ноду (incoming) идет в верхнюю группу (supply-chain)
    // OWNS от центральной ноды (outgoing) идет в нижнюю группу (customers)
    case 'OWNS':
      return isOutgoing ? 'customers' : 'supply-chain';  // incoming (на центральную ноду) -> сверху, outgoing (от центральной ноды) -> снизу
    case 'MANAGES':
    case 'CONTROLS':
    case 'PARENT_OF':
      return isOutgoing ? 'customers' : 'supply-chain';
    case 'SUBSIDIARY_OF':
      return isOutgoing ? 'supply-chain' : 'customers';  // inverse of OWNS
    case 'AUDITED_BY':
      return 'context'; // governance/compliance context
    
    // Production always downstream
    case 'PRODUCES':
      return 'customers';
    
    // Symmetric / Peers — market position
    case 'COMPETES_WITH':
    case 'CONFLICTS_WITH':
    case 'OPPOSES':
    case 'PARTNERS_WITH':
    case 'COOPERATES_WITH':
    case 'PARTICIPATES_IN':
    case 'RELATED_TO':
    case 'RELATIVE_OF':
    case 'SIMILAR_TO':
      return 'market';
    
    // LOCATED_IN имеет приоритет для размещения в supply-chain группе (верхняя)
    // только если связь направлена НА центральную ноду (incoming)
    case 'LOCATED_IN':
      return isOutgoing ? 'context' : 'supply-chain';  // incoming = нода находится в объекте сверху -> supply-chain
    // Context — geography, regulation, influence (остальные географические связи)
    case 'OPERATES_IN':
    case 'BASED_IN':
    case 'HEADQUARTERED_IN':
      return 'context';
    case 'REGULATES':
      return 'context';  // regulator = context for the entity
    case 'INFLUENCES':
    case 'AFFECTS':
    case 'IMPACTS':
      return 'context';
    
    default:
      // Unknown types: use direction as hint
      return isOutgoing ? 'customers' : 'supply-chain';
  }
}

/**
 * Map edge type + direction into one of 4 linear layout quadrants.
 *
 * 2026 placement rules (product semantics):
 * - Vertical (top/bottom): determined by direction relative to the central node:
 *   - incoming (neighbor -> center) => TOP
 *   - outgoing (center -> neighbor) => BOTTOM
 *   - EXCEPT symmetric relations (e.g. competition): direction does not decide vertical placement.
 * - Horizontal (left/right): determined by meaning:
 *   - LEFT: ownership + products (inputs/outputs)
 *   - RIGHT: environment (regulation, competitors, influence in/out, context)
 */
export function getLinearSection(edgeType: string | undefined, isOutgoing: boolean): LinearSection {
  const t = normalizeEdgeType(edgeType);

  // Special case: DOMINATES should be primarily a horizontal ordering signal:
  // - outgoing (center dominates neighbor) => neighbor should appear LEFT of center
  // - incoming (neighbor dominates center) => neighbor should appear RIGHT of center
  // Keep it in TOP band for stability (avoid pushing dominant semantics into "downstream").
  if (t === 'DOMINATES') {
    const isLeft = Boolean(isOutgoing);
    return isLeft ? 'top-left' : 'top-right';
  }

  // Special case: HAS_SUBMARKET / HAS_SUBGROUP should be purely horizontal.
  // - outgoing (parent -> child) => child LEFT
  // - incoming (parent -> child) => parent RIGHT
  if (t === 'HAS_SUBMARKET' || t === 'HAS_SUBGROUP') {
    return isOutgoing ? 'top-left' : 'top-right';
  }

  // Direction-agnostic relations: direction does NOT define top/bottom.
  // These are typically "peer" or symmetric relations.
  const SYMMETRIC = new Set<string>([
    'COMPETES_WITH',
    'CONFLICTS_WITH',
    'OPPOSES',
    'PARTNERS_WITH',
    'COOPERATES_WITH',
    'RELATED_TO',
    'RELATIVE_OF',
    'SIMILAR_TO',
    'LINKED_TO',
  ]);

  // LEFT side: ownership + products (both input and output).
  const LEFT = new Set<string>([
    // Ownership / control
    'OWNS',
    'SUBSIDIARY_OF',
    'PARENT_OF',
    'MANAGES',
    'CONTROLS',
    'BOARD_MEMBER_OF',
    'EXECUTIVE_OF',
    // Products / supply / dependencies (inputs+outputs)
    'PRODUCES',
    'SUPPLIES_TO',
    'CONSUMES',
    'DEPENDS_ON',
    'REQUIRES',
    'USES',
    'NEEDS',
    'EXTRACTS_FROM',
  ]);

  // RIGHT side: environment (context/regulation/competition/influence).
  // Anything not explicitly LEFT defaults to RIGHT to keep "unknown" relations in the environment bucket.
  const RIGHT_EXPLICIT = new Set<string>([
    // Geography / context
    'LOCATED_IN',
    'OPERATES_IN',
    'BASED_IN',
    'HEADQUARTERED_IN',
    // Regulation / policy
    'REGULATES',
    // Gov skeleton hierarchy
    'ADMINISTERS',
    // Community relationships
    'OPERATES_VIA',
    'MEMBER_OF',
    'AFFILIATED_WITH',
    'HAS_SUBGROUP',
    // Influence (in/out)
    'INFLUENCES',
    'AFFECTS',
    'IMPACTS',
  ]);

  const isLeft = LEFT.has(t);
  // note: we don't need `RIGHT_EXPLICIT` for logic; it's here to document intent.
  void RIGHT_EXPLICIT;

  // Vertical: top/bottom by direction, except symmetric relations.
  // Symmetric relations (Competition/Partnership) go to TOP (environment/context).
  let isBottom = SYMMETRIC.has(t) ? false : Boolean(isOutgoing);

  // Special case: Dependency and Context relations have inverted meaning for direction.
  // - "A depends on B" (outgoing A->B) means B is a prerequisite/upstream -> TOP.
  // - "A operates in B" (outgoing A->B) means B is the environment -> TOP.
  // - "B depends on A" (incoming B->A) means B is dependent/downstream -> BOTTOM.
  const REVERSE_VERTICAL = new Set<string>([
    // Dependencies
    'DEPENDS_ON',
    'REQUIRES',
    'NEEDS',
    'USES',
    'CONSUMES',
    'EXTRACTS_FROM',
    // Context / Geography
    'LOCATED_IN',
    'OPERATES_IN',
    'BASED_IN',
    'HEADQUARTERED_IN',
  ]);

  if (REVERSE_VERTICAL.has(t)) {
    // Invert the vertical direction logic for these types.
    isBottom = !isBottom;
  }

  if (isBottom) {
    return isLeft ? 'bottom-left' : 'bottom-right';
  }
  return isLeft ? 'top-left' : 'top-right';
}

/**
 * "Hierarchical / Levels" layout (product-first).
 *
 * Similar to Linear in semantics (4 quadrants), but extended by depth:
 * - depth===0 nodes are placed in the central row
 * - depth>0 nodes are placed into TOP/BOTTOM bands by quadrant *and* level (=depth)
 *
 * NOTE: This layout intentionally DOES NOT create group container nodes yet.
 * It is a flat layout (no parentId) to keep interaction consistent and to avoid blocking clicks.
 */
export async function layoutLevelsFullGraph<T = any>(
  nodes: Node<T>[],
  _edges: Edge[],
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  if (nodes.length === 0) return nodes;

  // Flat graph: strip parent/extent props to avoid ReactFlow parent linkage issues.
  const flatNodes: Node<T>[] = nodes.map((n) => {
    const out: any = { ...n };
    delete out.parentId;
    delete out.parentNode;
    delete out.extent;
    delete out.expandParent;
    return out as Node<T>;
  });

  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 40, // Compact: reduced from 60
    layerSpacing = 100, // Compact: reduced from 150
  } = options;

  // Determine central nodes (depth === 0). If none are marked, fall back to "no parent" roots.
  let centralIds = new Set<string>(
    flatNodes.filter((n) => (n.data as any)?.depth === 0).map((n) => n.id),
  );
  if (centralIds.size === 0) {
    for (const n of flatNodes) {
      if (!(n.data as any)?.parentFragmentId) centralIds.add(n.id);
    }
  }
  if (centralIds.size === 0) centralIds = new Set([flatNodes[0]!.id]);

  const central: Node<T>[] = [];
  const others: Node<T>[] = [];
  for (const n of flatNodes) {
    if (centralIds.has(n.id)) central.push(n);
    else others.push(n);
  }

  // Stable ordering for central row: keep existing x order if available.
  const centralSorted = [...central].sort((a, b) => (a.position?.x ?? 0) - (b.position?.x ?? 0));
  const centralWidth = centralSorted.length * nodeWidth + Math.max(0, centralSorted.length - 1) * nodeSpacing;
  const centralStartX = -centralWidth / 2;

  const pos = new Map<string, { x: number; y: number }>();

  // Build lookup map for roots by both node id and fragmentId.
  const rootAnchorById = new Map<string, string>(); // nodeId|fragmentId -> centralNodeId
  centralSorted.forEach((n, i) => {
    const x = centralStartX + i * (nodeWidth + nodeSpacing);
    pos.set(n.id, { x, y: 0 });
    rootAnchorById.set(String(n.id), n.id);
    const fid = (n.data as any)?.fragmentId;
    if (fid) rootAnchorById.set(String(fid), n.id);
  });

  // Group others by (root, quadrant, level)
  type GroupKey = string;
  const groups = new Map<GroupKey, Node<T>[]>();

  const norm = (v: any) => String(v || '').trim();
  const getRootId = (n: Node<T>) => {
    const d: any = n.data;
    const rid = norm(d?.rootFragmentId) || norm(d?.parentFragmentId) || norm(d?.fragmentId) || norm(n.id);
    return rootAnchorById.get(rid) || centralSorted[0]!.id;
  };

  // 1. Group nodes by (root, quadrant, level)
  const maxLevel = new Map<string, number>(); // rootId -> max depth encountered

  for (const n of others) {
    const d: any = n.data;
    const edgeType = d?.edgeTypeFromParent;
    const isOutgoing = d?.directionFromParent !== 'incoming';
    const section = getLinearSection(edgeType, isOutgoing);
    const depthRaw = typeof d?.depth === 'number' ? d.depth : 1;
    const level = Math.max(1, Math.abs(depthRaw));
    const rootId = getRootId(n);
    const key = `${rootId}|${section}|${level}`;
    const arr = groups.get(key) || [];
    arr.push(n);
    groups.set(key, arr);

    const prevMax = maxLevel.get(rootId) || 0;
    if (level > prevMax) maxLevel.set(rootId, level);
  }

  // Calculate dynamic width needed for each root tree PER LEVEL (Skyline packing)
  // We need precise extents to allow "pyramids" to overlap their empty spaces.
  // Extents are relative to the Root Node's Left Edge (anchor).
  // LeftExtent: positive distance to the left of anchor.
  // RightExtent: positive distance to the right of anchor.
  type TreeContour = Map<string, { left: number; right: number }>; // key: "top:1", "bottom:2" etc
  const treeContours = new Map<string, TreeContour>();
  const colW = nodeWidth + nodeSpacing;

  const getLevelKey = (section: string, level: number) => {
    if (section.startsWith('top')) return `top:${level}`;
    if (section.startsWith('bottom')) return `bottom:${level}`;
    return `center:0`;
  };

  for (const [key, list] of groups.entries()) {
    const [rootId, section, levelStr] = key.split('|');
    const level = parseInt(levelStr || '1', 10);
    const levelKey = getLevelKey(section, level);

    const contour = treeContours.get(rootId) || new Map();
    if (!treeContours.has(rootId)) treeContours.set(rootId, contour);

    const extents = contour.get(levelKey) || { left: 0, right: 0 };
    
    // Calculate extents based on column count
    const cols = Math.min(6, Math.max(1, list.length));
    
    if (section.endsWith('left')) {
      // Left group grows leftwards from (anchor - colW).
      // Leftmost edge = anchor - cols * colW.
      // LeftExtent = cols * colW.
      extents.left = Math.max(extents.left, cols * colW);
    } else {
      // Right group grows rightwards from (anchor + colW).
      // Rightmost edge = anchor + cols * colW + nodeWidth.
      // RightExtent = cols * colW + nodeWidth.
      extents.right = Math.max(extents.right, cols * colW + nodeWidth);
    }
    contour.set(levelKey, extents);
  }

  // Skyline Packing
  // Keep track of the rightmost edge of the placed trees for each level.
  const globalSkyline = new Map<string, number>(); // levelKey -> maxX
  const rootAnchors = new Map<string, number>();
  
  // Initial skyline at -Infinity
  // Note: Level 0 (Center) is special. The root node itself occupies [0, nodeWidth].
  // So for Level 0, LeftExtent=0, RightExtent=nodeWidth.
  
  centralSorted.forEach((n, idx) => {
    const rootId = n.id;
    const contour = treeContours.get(rootId) || new Map();

    // Always ensure Level 0 exists in contour
    if (!contour.has('center:0')) {
        contour.set('center:0', { left: 0, right: nodeWidth });
    }

    // Determine minX (anchor) for this tree based on overlap with global skyline
    let requiredAnchor = idx === 0 ? 0 : -Infinity;

    if (idx > 0) {
        // Check collision on all levels defined in this tree
        for (const [lvlKey, extents] of contour.entries()) {
            const currentSkylineX = globalSkyline.get(lvlKey);
            // If this level hasn't been used yet by previous trees, we theoretically
            // don't collide. But to be safe and avoid "floating" overlaps if intermediate 
            // trees didn't have this level, we should check against "Center" or 0?
            // Actually, if skyline doesn't have this level, it means no previous tree
            // extended into this level. We can place it freely relative to others?
            // Safer to check against the max skyline of "adjacent" levels? 
            // No, strictly per-level is fine for "levels" layout.
            
            const sky = currentSkylineX !== undefined ? currentSkylineX : -Infinity;
            // Overlap condition: Anchor - LeftExtent > Skyline + Gap
            // Anchor > Skyline + Gap + LeftExtent
            
            // If skyline is -Infinity (empty level), we default to the placement 
            // determined by Level 0 (Center).
            // We'll calculate a valid anchor for EACH level and take the MAX.
            
            if (currentSkylineX !== undefined) {
                 // Compact: reduced gap between distinct trees from layerSpacing (100+) to nodeSpacing*1.5 (~60px)
                 const treeGap = Math.floor(nodeSpacing * 1.5);
                 requiredAnchor = Math.max(requiredAnchor, sky + treeGap + extents.left);
            }
        }
        
        // Also check against Level 0 explicitly if not covered loop (it is covered above)
        // If requiredAnchor is still -Infinity (e.g. unique levels), align with previous root?
        // We should at least respect the "visual stream". 
        // Let's enforce that it's at least (PreviousRootAnchor + NodeSpacing + PreviousRootWidth).
        // But simpler: just enforce Level 0 collision as a baseline.
        // (Handled by the loop since 'center:0' is in contour).
    }

    rootAnchors.set(rootId, requiredAnchor);
    pos.set(rootId, { x: requiredAnchor, y: 0 });

    // Update Global Skyline
    for (const [lvlKey, extents] of contour.entries()) {
        const rightEdge = requiredAnchor + extents.right;
        const currentSky = globalSkyline.get(lvlKey) || -Infinity;
        globalSkyline.set(lvlKey, Math.max(currentSky, rightEdge));
    }
  });

  // Center the whole layout
  // Find minX and maxX of the placed roots (Level 0)
  // Or better: minX of skyline vs maxX of skyline across all levels?
  // Usually we just center the roots visually.
  const finalMinX = rootAnchors.get(centralSorted[0].id) || 0;
  const finalMaxX = globalSkyline.get('center:0') || 0; // rough width based on centers
  
  // Actually, let's look at the TOTAL extent
  let minContentX = Infinity;
  let maxContentX = -Infinity;
  rootAnchors.forEach((anchor, rid) => {
     const contour = treeContours.get(rid);
     if (contour) {
         for (const ext of contour.values()) {
             minContentX = Math.min(minContentX, anchor - ext.left);
             maxContentX = Math.max(maxContentX, anchor + ext.right);
         }
     } else {
         minContentX = Math.min(minContentX, anchor);
         maxContentX = Math.max(maxContentX, anchor + nodeWidth);
     }
  });
  
  if (!isFinite(minContentX)) minContentX = 0;
  if (!isFinite(maxContentX)) maxContentX = 0;
  
  const totalW = maxContentX - minContentX;
  const shiftX = -(minContentX + totalW / 2);

  centralSorted.forEach((n) => {
    const anchor = rootAnchors.get(n.id) || 0;
    pos.set(n.id, { x: anchor + shiftX, y: 0 });
  });

  // 2. Calculate dynamic heights for each level (accumulating offsets from center).
  // Separate accumulation for TOP (growing upward) and BOTTOM (growing downward).
  // Map: rootId -> level -> { topY, bottomY } (absolute Y coordinates for this level)
  const levelOffsets = new Map<string, Map<number, { topY: number; bottomY: number }>>();

  // Initial offset is just outside the central row
  const INITIAL_OFFSET_Y = Math.floor(layerSpacing / 2) + Math.floor(nodeHeight / 2);

  for (const rootId of centralIds) {
    // We only care about rootIds that actually have children in `groups`.
    if (!maxLevel.has(rootId)) continue;

    const offsets = new Map<number, { topY: number; bottomY: number }>();
    let currentTopY = -INITIAL_OFFSET_Y; // Growing UP (negative Y)
    let currentBottomY = INITIAL_OFFSET_Y; // Growing DOWN (positive Y)

    const maxL = maxLevel.get(rootId) || 0;

    for (let l = 1; l <= maxL; l++) {
      // --- TOP BAND (Top-Left + Top-Right) ---
      // Height needed for this level = max(rows in TL, rows in TR) * rowHeight + spacing
      const tlKey = `${rootId}|top-left|${l}`;
      const trKey = `${rootId}|top-right|${l}`;
      const tlNodes = groups.get(tlKey) || [];
      const trNodes = groups.get(trKey) || [];

      // Calculate height for this level's Top band
      let topHeight = 0;
      if (tlNodes.length > 0 || trNodes.length > 0) {
        // Calculate rows needed for TL
        const tlCols = Math.min(6, Math.max(1, tlNodes.length));
        const tlRows = Math.ceil(tlNodes.length / tlCols);
        
        // Calculate rows needed for TR
        const trCols = Math.min(6, Math.max(1, trNodes.length));
        const trRows = Math.ceil(trNodes.length / trCols);

        // Max rows determines the band height
        const maxRows = Math.max(tlRows, trRows);
        // Base row height (node + internal gap)
        // Compact: reduced vertical gap between rows within a band
        const rowH = nodeHeight + 40; 
        
        // Total height for this level = rows * height
        topHeight = maxRows * rowH;
      }

      // If there's content, shift currentTopY UP by this height.
      // Position anchors will be relative to this band.
      // We place the level such that its "bottom" touches the previous "top".
      // currentTopY is the Y coordinate of the *bottom* edge of this new level.
      // The nodes will be placed between (currentTopY - topHeight) and currentTopY.
      // Actually, let's track the *anchor* line.
      // Let's place the first row of this level at `currentTopY - nodeHeight/2`.
      // And next level will start at `currentTopY - topHeight - layerSpacing`.
      
      const thisLevelTopAnchor = currentTopY - Math.floor(nodeHeight / 2) - Math.floor(layerSpacing * 0.5); // extra padding
      // But we need to account for multi-row growing UPWARDS from the anchor?
      // Standard Sugiyama puts level N at fixed Y. Here we want level N to be "above" level N-1.
      
      // Let's accumulate:
      // levelTopY = currentTopY - (layerSpacing);  // Start of this level space
      // BUT we need to fit `topHeight` into it.
      // If we place nodes starting at `levelTopY - nodeHeight/2` (first row),
      // subsequent rows (if any) go further UP? Or further DOWN?
      // Visual hierarchy: Level 1 is closest to Center. Level 2 is further.
      // So Level 1 Row 0 is at Y=-100. Level 1 Row 1 is at Y=-200 (further away).
      // Yes, "outward" growth.
      
      // Position for Level L (Top):
      // Starts at `currentTopY - layerSpacing`.
      // Grows UPWARD by `topHeight`.
      // New currentTopY = `currentTopY - layerSpacing - topHeight`.
      
      const levelTopStart = currentTopY - layerSpacing;
      // We store the "base" Y for the FIRST row of this level.
      // Rows will subtract from this Y (move further up).
      const levelTopBaseY = levelTopStart - Math.floor(nodeHeight / 2);
      
      // Update accumulator for next level
      // If topHeight is 0 (empty level), we still step? No, compact it?
      // User says "Strict hierarchy". Better to keep empty levels visible? 
      // Usually better to collapse empty levels if they are truly empty.
      // But `groups` iteration ensures we only process levels that exist? 
      // No, we iterate 1..maxL. Some intermediate levels might be empty.
      // If empty, `topHeight` is 0. 
      // If empty, we should arguably NOT add layerSpacing.
      // But to keep alignment across levels (if we had global layers), we might want to.
      // Here we process per-root, so collapsing empty levels is fine.
      
      if (topHeight > 0) {
        currentTopY = levelTopStart - topHeight;
      } else {
        // If the level is empty, we don't accumulate height, BUT we should preserve the logical spacing
        // if we want strict grid. However, collapsing makes more sense for sparse graphs.
        // Let's stick to "if empty, ignore".
        // BUT: if we skip layer N, then layer N+1 will start right where N would have started.
        // This is fine.
      }

      // --- BOTTOM BAND (Bottom-Left + Bottom-Right) ---
      const blKey = `${rootId}|bottom-left|${l}`;
      const brKey = `${rootId}|bottom-right|${l}`;
      const blNodes = groups.get(blKey) || [];
      const brNodes = groups.get(brKey) || [];

      let bottomHeight = 0;
      if (blNodes.length > 0 || brNodes.length > 0) {
        const blCols = Math.min(6, Math.max(1, blNodes.length));
        const blRows = Math.ceil(blNodes.length / blCols);
        const brCols = Math.min(6, Math.max(1, brNodes.length));
        const brRows = Math.ceil(brNodes.length / brCols);
        
        const maxRows = Math.max(blRows, brRows);
        const rowH = nodeHeight + 40; // Compact
        bottomHeight = maxRows * rowH;
      }

      // Position for Level L (Bottom):
      // Starts at `currentBottomY + layerSpacing`.
      // Grows DOWNWARD by `bottomHeight`.
      const levelBottomStart = currentBottomY + layerSpacing;
      // We store the "base" Y for the FIRST row of this level.
      // Rows will ADD to this Y (move further down).
      const levelBottomBaseY = levelBottomStart + Math.floor(nodeHeight / 2);

      if (bottomHeight > 0) {
        currentBottomY = levelBottomStart + bottomHeight;
      }

      offsets.set(l, { topY: levelTopBaseY, bottomY: levelBottomBaseY });
    }
    levelOffsets.set(rootId, offsets);
  }

  const rowH = nodeHeight + Math.max(32, Math.floor(layerSpacing * 0.25));

  for (const [key, list] of groups.entries()) {
    const [rootId, section, levelStr] = key.split('|');
    const level = Math.max(1, parseInt(levelStr || '1', 10) || 1);
    const anchor = pos.get(rootId) || { x: 0, y: 0 };

    const offsets = levelOffsets.get(rootId);
    if (!offsets) continue;
    const levelPos = offsets.get(level);
    if (!levelPos) continue;

    const isTop = String(section).startsWith('top');
    const isLeft = String(section).endsWith('left');

    // Use the dynamic Y we calculated
    const baseY = isTop ? levelPos.topY : levelPos.bottomY;
    
    // X calculation:
    // Left side grows leftwards from (anchor - colW)
    // Right side grows rightwards from (anchor + colW)
    // Note: colW includes spacing
    const baseXInner = anchor.x + (isLeft ? -colW : colW);

    const nodesPerRow = Math.min(6, Math.max(1, list.length));
    
    for (let idx = 0; idx < list.length; idx++) {
      const rowIdx = Math.floor(idx / nodesPerRow);
      const colIdx = idx % nodesPerRow;

      const x = isLeft ? (baseXInner - colIdx * colW) : (baseXInner + colIdx * colW);
      // Grow OUTWARDS from the base Y:
      // Top grows UP (negative Y direction)
      // Bottom grows DOWN (positive Y direction)
      const y = isTop ? (baseY - rowIdx * rowH) : (baseY + rowIdx * rowH);

      pos.set(list[idx]!.id, { x, y });
    }
  }

  return flatNodes.map((n) => {
    const p = pos.get(n.id);
    if (!p) return n;
    return { ...n, position: { x: p.x, y: p.y } };
  });
}

/**
 * Levels (Hierarchical) — Two-pane layout (contract-driven, v2).
 *
 * Contract:
 * - **Left side**: per-node expanded hierarchy shown top-down inside a (potentially large) group.
 *   - max 6 items per row, unlimited rows (no pagination).
 * - **Right side**: a backbone of "single nodes" shown as a left-to-right hierarchy:
 *   - less subordinate (upstream/parents) to the LEFT
 *   - more subordinate (downstream/children) to the RIGHT
 * - **Upstream group disappears**:
 *   - any upstream neighbor (directionFromParent === 'incoming') is promoted into the right backbone.
 *
 * Notes:
 * - Edge routing stays as-is (spaghetti allowed).
 * - This is only intended for "levels" mode and is invoked from `LensCanvasCockpit`.
 */
export async function layoutLevelsTwoPane<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 60,
    layerSpacing = 150,
    collapsedSubcategories = new Set<string>(),
    onToggleSubcategoryCollapse,
    compactLeftPane = false,
    rightDockMode = false,
    rightDockStateByNodeId,
    onToggleRightDock,
  } = options;

  if (!nodes || nodes.length === 0) return nodes;

  const isGroup = (n: Node<T>) => Boolean((n.data as any)?.isGroup);

  // Helper to check if a subcategory (edge type) is collapsed for a given group.
  // Supports both per-group state (`groupId:edgeType`) and global defaults (`edgeType`).
  // Also supports explicit expand markers (`groupId:edgeType:expanded`) to override defaults.
  const isSubcategoryCollapsed = (groupId: string, edgeType: string): boolean => {
    const perGroupKey = `${groupId}:${edgeType}`;
    const expandedKey = `${perGroupKey}:expanded`;
    // Explicit expand marker overrides everything (including defaults).
    if (collapsedSubcategories.has(expandedKey)) return false;
    // Explicit collapse marker
    if (collapsedSubcategories.has(perGroupKey)) return true;
    // Check global default (edge type alone)
    if (collapsedSubcategories.has(edgeType)) return true;
    // Check default-collapsed edge types (e.g., RELATIVE_OF)
    if (DEFAULT_COLLAPSED_EDGE_TYPES.has(normalizeEdgeType(edgeType))) {
        return true;
    }
    return false;
  };

  // Strip any prior grouping from fragment nodes (ReactFlow parent linkage must be rebuilt deterministically).
  const fragments: Node<T>[] = nodes
    .filter((n) => !isGroup(n))
    .map((n) => {
      // Clone node + data to allow deterministic view-only rewrites (without mutating input).
      const out: any = { ...n, data: { ...(n.data as any) } };
      delete out.parentId;
      delete out.parentNode;
      delete out.extent;
      delete out.expandParent;
      // Derived hiding (hiddenByStack) must NOT be treated as manual hidden across relayouts.
      // Clear stale `hidden` so the current collapse state can decide visibility deterministically.
      if (Boolean(out.hidden) && Boolean((out?.data as any)?.hiddenByStack)) {
        delete out.hidden;
      }
      // When Right Dock mode is enabled, remove stale RIGHT-stack fields so the UI does not show stack toggles.
      // IMPORTANT: left-pane subcategory stacks must remain intact, so only clear known right-stack keys.
      if (rightDockMode) {
        const d: any = out?.data;
        const k = String(d?.stackGroupKey || '').trim();
        const isRightStackKey = k.startsWith('levels:rightstack:') || k.startsWith('levels:rightstackp:');
        if (isRightStackKey) {
          delete d.hiddenByStack;
          delete d.stackGroupKey;
          delete d.stackCount;
          delete d.stackExpanded;
          delete (d as any).stackControllerId;
          delete d.stackTitle;
          delete d.stackSubtitle;
          delete d.stackKind;
          delete d.stackVisualType;
          delete d.onToggleStack;
        }
      }
      return out as Node<T>;
    });

  // Preserve any "manually hidden" nodes (e.g., via UI filters) across relayouts.
  // IMPORTANT: do NOT treat "hidden by stack" as manual, otherwise expanding a stack would never reveal its nodes.
  const preHiddenIds = new Set<string>(
    fragments
      .filter((n) => Boolean((n as any).hidden) && !Boolean(((n as any)?.data as any)?.hiddenByStack))
      .map((n) => n.id),
  );

  const normId = (v: any) => String(v || '').trim();
  const edgeTypeOf = (e: any): string => normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);

  // === Right Dock (4 filters) ===
  const RIGHT_DOCK_KEYS = ['governance', 'supply', 'dependencies', 'structure'] as const;
  type RightDockKey = (typeof RIGHT_DOCK_KEYS)[number];
  const isRightDockKey = (v: any): v is RightDockKey => RIGHT_DOCK_KEYS.includes(v as any);

  const DOCK_GOVERNANCE = new Set([
    'OWNS',
    'INVESTS_IN',
    'CONTROLS',
    'MANAGES',
    'BOARD_MEMBER_OF',
    'EXECUTIVE_OF',
    'FOUNDED',
    'PARENT_OF',
    'SUBSIDIARY_OF',
    'ADMINISTERS',
  ]);
  const DOCK_SUPPLY = new Set(['SUPPLIES_TO', 'PRODUCES', 'TRANSFORMS_INTO']);
  const DOCK_DEPENDENCIES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
  const DOCK_STRUCTURE = new Set(['DOMINATES', 'HAS_SUBMARKET', 'HAS_SUBGROUP']);

  const rightDockCategoryForEdgeType = (edgeType: string | undefined): RightDockKey | null => {
    const et = normalizeEdgeType(edgeType);
    if (!et) return null;
    if (DOCK_GOVERNANCE.has(et)) return 'governance';
    if (DOCK_SUPPLY.has(et)) return 'supply';
    if (DOCK_DEPENDENCIES.has(et)) return 'dependencies';
    if (DOCK_STRUCTURE.has(et)) return 'structure';
    return null;
  };

  // Hierarchy edge sets (same intent as `deriveLevelsTreeMetadata`, but local to this layout).
  // Parent is the "more ultimate" entity (should be further RIGHT).
  // IMPORTANT: When multiple hierarchy-ish edges exist between the same two nodes
  // (e.g. OWNS vs INFLUENCES), we must pick a single "best" direction to avoid
  // contradictions. We prioritize direct control/ownership over influence.
  const STRONG_CONTROL_TYPES = new Set([
    'OWNS',
    'SUBSIDIARY_OF',
    'PARENT_OF',
    'MANAGES',
    'CONTROLS',
    'INVESTS_IN',
    'FOUNDED',
    'ADMINISTERS',
  ]);
  const GOVERNANCE_CONTROL_TYPES = new Set(['BOARD_MEMBER_OF', 'EXECUTIVE_OF', 'EMPLOYED_BY']);
  const SUPPLY_CHAIN_TYPES = new Set(['PRODUCES', 'SUPPLIES_TO', 'TRANSFORMS_INTO']);
  const INFLUENCE_TYPES = new Set(['INFLUENCES', 'AFFECTS', 'REGULATES', 'IMPACTS']);
  const hierarchyScore = (et: string): number => {
    const t = String(et || '').trim();
    if (!t) return 0;
    if (STRONG_CONTROL_TYPES.has(t)) return 400;
    // Market-defining relation: strong, but below hard control/ownership.
    if (t === 'DOMINATES') return 260;
    // Market hierarchy (parent -> submarket): strong structural signal.
    if (t === 'HAS_SUBMARKET') return 240;
    // Community hierarchy (dynasty/tribe): strong structural signal.
    if (t === 'HAS_SUBGROUP') return 240;
    if (GOVERNANCE_CONTROL_TYPES.has(t)) return 220;
    if (SUPPLY_CHAIN_TYPES.has(t)) return 120;
    if (INFLUENCE_TYPES.has(t)) return 40;
    // Dependencies/context still participate, but below direct control.
    return 80;
  };
  const HIERARCHY_TYPES = new Set([
    'OWNS',
    'PARENT_OF',
    'MANAGES',
    'CONTROLS',
    'INVESTS_IN',
    'FOUNDED',
    'BOARD_MEMBER_OF',
    'EXECUTIVE_OF',
    'PRODUCES',
    'SUPPLIES_TO',
    'TRANSFORMS_INTO',
    'ADMINISTERS',
    'INFLUENCES',
    'AFFECTS',
    'REGULATES',
    'IMPACTS',
    // Market-defining relations (org -> market): treat as hierarchy signal so the org is placed to the RIGHT.
    'DOMINATES',
    // Market hierarchy (parent -> submarket).
    'HAS_SUBMARKET',
  ]);
  const INVERTED_HIERARCHY_TYPES = new Set([
    'SUBSIDIARY_OF',
    'DEPENDS_ON',
    'REQUIRES',
    'NEEDS',
    'USES',
    'CONSUMES',
    'EXTRACTS_FROM',
    // Governance/employment: PERSON -(EMPLOYED_BY)-> ORG means ORG is the "more ultimate" node (parent).
    'EMPLOYED_BY',
    'LOCATED_IN',
    'OPERATES_IN',
    'BASED_IN',
    'HEADQUARTERED_IN',
  ]);

  // Peer / market relations should NOT be treated as "upstream controllers".
  // If we promote incoming peer neighbors into the right backbone, they form tall columns (as in the Partners example)
  // and steal space from the meaningful control hierarchy. Keep them grouped on the left instead.
  const PEER_TYPES = new Set([
    'COMPETES_WITH',
    'CONFLICTS_WITH',
    'OPPOSES',
    'PARTNERS_WITH',
    'COOPERATES_WITH',
    'PARTICIPATES_IN',
    'RELATED_TO',
    'RELATIVE_OF',
    'SIMILAR_TO',
    'LINKED_TO',
  ]);
  // In Levels mode, some relations are "inverted" for upstream promotion.
  // Example: A -(DEPENDS_ON)-> B means A is downstream of B.
  // If B is expanded/central, the edge is incoming to B, but A should stay in the LEFT group (downstream),
  // not be promoted into the right backbone.
  const INCOMING_STAY_IN_GROUP_TYPES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM', 'AUDITED_BY']);

  // Context / geography relations should stay in LEFT groups even when they are "incoming" to an expanded node.
  // Otherwise expanding a GEO node would promote many orgs/places into the right backbone and compete for space.
  const CONTEXT_GROUP_ONLY_TYPES = new Set([
    'LOCATED_IN',
    'OPERATES_IN',
    'BASED_IN',
    'HEADQUARTERED_IN',
    'REGULATES',
    // Influence relations should stay grouped (context), not be promoted into the right backbone.
    // Otherwise an incoming INFLUENCES edge can incorrectly "hijack" the hierarchy layout.
    'INFLUENCES',
    'AFFECTS',
    'IMPACTS',
  ]);

  const getParentChild = (e: any): { parent: string; child: string } | null => {
    const s = normId(e?.source);
    const t = normId(e?.target);
    if (!s || !t) return null;
    const et = edgeTypeOf(e);
    if (HIERARCHY_TYPES.has(et)) return { parent: s, child: t };
    if (INVERTED_HIERARCHY_TYPES.has(et)) return { parent: t, child: s };
    return null;
  };

  type PcScored = { parent: string; child: string; et: string; score: number };
  const pairKey = (a: string, b: string) => (a < b ? `${a}||${b}` : `${b}||${a}`);
  const getParentChildScored = (e: any): PcScored | null => {
    const s = normId(e?.source);
    const t = normId(e?.target);
    if (!s || !t) return null;
    const et = edgeTypeOf(e);
    if (HIERARCHY_TYPES.has(et)) return { parent: s, child: t, et, score: hierarchyScore(et) };
    if (INVERTED_HIERARCHY_TYPES.has(et)) return { parent: t, child: s, et, score: hierarchyScore(et) };
    return null;
  };

  // Precompute the best hierarchy relation for every node-pair (to avoid conflicts).
  const bestPcByPair = new Map<string, PcScored>();
  for (const e of (edges || []) as any[]) {
    const pc = getParentChildScored(e);
    if (!pc) continue;
    const key = pairKey(pc.parent, pc.child);
    const prev = bestPcByPair.get(key);
    if (!prev || pc.score > prev.score) {
      bestPcByPair.set(key, pc);
      continue;
    }
    // Tie-break (stable): prefer non-influence when scores match (extra safety for future scoring tweaks).
    if (prev && pc.score === prev.score) {
      const prevIsInfluence = INFLUENCE_TYPES.has(prev.et);
      const curIsInfluence = INFLUENCE_TYPES.has(pc.et);
      if (prevIsInfluence && !curIsInfluence) {
        bestPcByPair.set(key, pc);
      }
    }
  }

  // === 1) Build Right backbone set ===
  // Base: expanded nodes + roots (depth=0).
  const rightIds = new Set<string>();
  for (const n of fragments) {
    const d: any = n.data;
    if (d?.expanded) rightIds.add(n.id);
    // Trace can optionally force certain nodes into the backbone for multi-hop visualization.
    if (d?.traceBackbone) rightIds.add(n.id);
    if (typeof d?.depth === 'number' && d.depth === 0) rightIds.add(n.id);
  }
  if (rightIds.size === 0 && fragments[0]) rightIds.add(fragments[0].id);

  const fragmentIdSet = new Set(fragments.map((n) => n.id));
  const fragmentById = new Map<string, Node<T>>(fragments.map((n) => [String(n.id || '').trim(), n]));

  const rightDockDefaultsForType = (tk: string): RightDockKey[] => {
    const t = String(tk || '').toUpperCase().trim();
    if (!t) return ['governance'];
    const isOrg = t === 'ORGANIZATION' || t === 'ORG' || t === 'COMPANY';
    const isGov = t === 'GOV' || t === 'GOVERNMENT' || t === 'GOVERNMENT_AGENCY' || t === 'AGENCY' || t === 'MINISTRY';
    const isPerson = t === 'PERSON';
    const isProd = t === 'PRODUCT' || t === 'SERVICE' || t === 'TECHNOLOGY';
    const isFac = t === 'FACILITY' || t === 'DEPOSIT';
    const isMarket = t === 'MARKET' || t.startsWith('MARKET_');
    const isGeo = ['GEO_REGION', 'LOCATION', 'COUNTRY', 'STATE', 'PROVINCE', 'CITY'].includes(t) || t.includes('GEO');

    if (isOrg) return ['governance', 'dependencies'];
    if (isGov) return ['governance', 'structure'];
    if (isPerson) return ['governance'];
    if (isProd || isFac) return ['supply', 'dependencies'];
    if (isMarket) return ['structure'];
    if (isGeo) return ['structure'];
    return ['governance'];
  };

  const rightDockEnabledSetForNode = (id: string): Set<RightDockKey> => {
    const nid = String(id || '').trim();
    const fromOpt = rightDockStateByNodeId ? (rightDockStateByNodeId as any)[nid] : undefined;
    // If the caller provided an explicit array (even empty), treat it as an explicit override.
    // Empty array means "all filters off".
    if (Array.isArray(fromOpt)) {
      const s = new Set<RightDockKey>();
      for (const k of fromOpt) if (isRightDockKey(k)) s.add(k);
      return s;
    }
    const node = fragmentById.get(nid);
    const d: any = node?.data;
    // NOTE: normalizeFragmentType is declared later in this function, so avoid TDZ by normalizing inline here.
    const tk = String(d?.type || '')
      .trim()
      .toUpperCase()
      .replace(/\s+/g, '_') || 'UNKNOWN';
    return new Set(rightDockDefaultsForType(tk));
  };

  // Precompute ALL edge types per undirected node-pair (for robust stacking decisions).
  // We must use real graph edges, not derived metadata like edgeTypeFromParent, because that metadata can be dominated
  // by a different relation for placement, while we still want to recognize supply/control semantics for stacks.
  const pairEdgeTypes = new Map<string, Set<string>>();
  const directedEdgeTypes = new Map<string, Set<string>>(); // `${src}||${dst}` -> Set<edgeType>
  for (const e of (edges || []) as any[]) {
    const eid = String(e?.id || '').trim();
    const ed: any = e?.data || {};
    if (eid.startsWith('levels:agg:')) continue;
    if (eid.startsWith('levels:stackproxy:')) continue;
    if (eid.startsWith('levels:gproxy:')) continue;
    if (Boolean(ed.levelsAgg)) continue;
    if (Boolean(ed.levelsStackProxy)) continue;
    if (Boolean(ed.levelsGroupProxy)) continue;
    const s = normId(e?.source);
    const t = normId(e?.target);
    if (!s || !t) continue;
    if (s === t) continue;
    if (!fragmentIdSet.has(s) || !fragmentIdSet.has(t)) continue;
    const et = edgeTypeOf(e);
    if (!et) continue;
    const k = pairKey(s, t);
    const set = pairEdgeTypes.get(k) || new Set<string>();
    set.add(String(et || '').trim());
    pairEdgeTypes.set(k, set);

    const dk = `${s}||${t}`;
    const dset = directedEdgeTypes.get(dk) || new Set<string>();
    dset.add(String(et || '').trim());
    directedEdgeTypes.set(dk, dset);
  }

  // Promote upstream neighbors into the right backbone (Upstream group disappears).
  // Two signals:
  // - metadata-based (expansion): parentFragmentId in rightIds AND directionFromParent === 'incoming'
  // - edge-based (hierarchy): if there is a hierarchy edge Parent -> Child and Child is already on the right,
  //   then Parent becomes a right-node too (if it exists on canvas).
  //
  // Iterate until stable to capture multi-step promotions.
  let changed = true;
  while (changed) {
    changed = false;
    for (const n of fragments) {
      if (rightIds.has(n.id)) continue;
      const d: any = n.data;
      const pid = String(d?.parentFragmentId || '').trim();
      if (!pid || !rightIds.has(pid)) continue;
      const etp = normalizeEdgeType(d?.edgeTypeFromParent);
      // Right Dock gating: only promote if the parent node has the corresponding dock filter enabled.
      if (rightDockMode) {
        const cat = rightDockCategoryForEdgeType(etp);
        // If we can't categorize the edge, treat it as not promotable by dock (keeps right pane predictable).
        if (!cat) continue;
        const enabled = rightDockEnabledSetForNode(pid);
        if (!enabled.has(cat)) continue;
      }
      if (
        d?.directionFromParent === 'incoming' &&
        !PEER_TYPES.has(etp) &&
        !CONTEXT_GROUP_ONLY_TYPES.has(etp) &&
        !INCOMING_STAY_IN_GROUP_TYPES.has(etp)
      ) {
        rightIds.add(n.id);
        changed = true;
      }
    }

    for (const pc of bestPcByPair.values()) {
      if (!fragmentIdSet.has(pc.parent)) continue;
      // Never promote context/geography parents into the right backbone; keep them grouped on the left.
      if (CONTEXT_GROUP_ONLY_TYPES.has(normalizeEdgeType(pc.et))) continue;
      if (rightDockMode) {
        const cat = rightDockCategoryForEdgeType(pc.et);
        if (cat) {
          const enabled = rightDockEnabledSetForNode(pc.child);
          if (!enabled.has(cat)) continue;
        } else {
          // Unknown category: keep it out of the right backbone in dock mode (stable UX).
          continue;
        }
      }
      if (rightIds.has(pc.child) && !rightIds.has(pc.parent)) {
        rightIds.add(pc.parent);
        changed = true;
      }
    }
  }

  // === 1.25) Lens focus islands ===
  // Nodes pinned to the Lens scope but currently disconnected (degree=0) should NOT compete for space
  // inside the main right hierarchy. Instead, we render them in a dedicated far-right group.
  // Once they gain any edge (e.g. after expansion), they automatically rejoin the normal hierarchy.
  const degreeById = new Map<string, number>();
  for (const e of (edges || []) as any[]) {
    const eid = String(e?.id || '').trim();
    if (eid.startsWith('levels:agg:')) continue;
    if (Boolean(e?.data?.levelsAgg)) continue;
    if (Boolean(e?.data?.levelsStackProxy)) continue;
    const s = normId(e?.source);
    const t = normId(e?.target);
    if (!s || !t) continue;
    if (s === t) continue;
    if (!fragmentIdSet.has(s) || !fragmentIdSet.has(t)) continue;
    degreeById.set(s, (degreeById.get(s) || 0) + 1);
    degreeById.set(t, (degreeById.get(t) || 0) + 1);
  }

  const focusIslandNodesAll = fragments.filter((n) => {
    const d: any = n.data;
    if (!d?.pinnedToScope) return false;
    if (d?.expanded) return false;
    if (d?.traceBackbone) return false;
    const deg = degreeById.get(n.id) || 0;
    return deg === 0;
  });
  const focusIslandIds = new Set<string>(focusIslandNodesAll.map((n) => n.id));
  // Keep at least one right node (algorithm expects a non-empty backbone).
  if (focusIslandIds.size > 0) {
    for (const id of focusIslandIds) rightIds.delete(id);
    if (rightIds.size === 0 && fragments[0]) {
      const keep = fragments[0].id;
      rightIds.add(keep);
      focusIslandIds.delete(keep);
    }
  }
  const focusIslandNodes = focusIslandNodesAll.filter((n) => focusIslandIds.has(n.id));

  const rightNodes = fragments.filter((n) => rightIds.has(n.id));
  // Annotate right-pane nodes with dock state so the node renderer can show the dock panel.
  if (rightDockMode) {
    for (const rn of rightNodes) {
      const d: any = rn.data;
      const enabled = Array.from(rightDockEnabledSetForNode(rn.id));
      d.rightDockEnabled = enabled;
      d.onToggleRightDock = onToggleRightDock;
    }
  }

  // === 2) Compute RIGHT horizontal levels (hierarchy within right nodes) ===
  // We build hierarchy from real graph edges between right nodes (not from expansion parentFragmentId),
  // so that nodes that were in the Lens (depth=0) still get correctly placed relative to each other
  // (e.g., Group42 left of Mubadala, UAE right of Mubadala).
  const rightIdSet = new Set(rightNodes.map((n) => n.id));
  const childrenOf = new Map<string, Set<string>>(); // parent -> children
  const parentsOf = new Map<string, Set<string>>(); // child -> parents

  for (const pc of bestPcByPair.values()) {
    if (!rightIdSet.has(pc.parent) || !rightIdSet.has(pc.child)) continue;
    const kids = childrenOf.get(pc.parent) || new Set<string>();
    kids.add(pc.child);
    childrenOf.set(pc.parent, kids);
    const pars = parentsOf.get(pc.child) || new Set<string>();
    pars.add(pc.parent);
    parentsOf.set(pc.child, pars);
  }

  // Horizontal level is computed as "height" in the right-only hierarchy:
  // leaves (most subordinate) = 0, parent = max(child)+1.
  const hLevel = new Map<string, number>();
  const hVisiting = new Set<string>();
  const computeLevel = (id: string): number => {
    if (hLevel.has(id)) return hLevel.get(id)!;
    if (hVisiting.has(id)) return 0; // cycle guard
    hVisiting.add(id);
    const kids = Array.from(childrenOf.get(id) || []);
    if (kids.length === 0) {
      hLevel.set(id, 0);
      hVisiting.delete(id);
      return 0;
    }
    let m = 0;
    for (const k of kids) m = Math.max(m, computeLevel(k));
    const lv = m + 1;
    hLevel.set(id, lv);
    hVisiting.delete(id);
    return lv;
  };
  let maxLevel = 0;
  for (const n of rightNodes) {
    maxLevel = Math.max(maxLevel, computeLevel(n.id));
  }

  // === 3) Build Left blocks (Downstream only) ===
  // Anything attached to a right node (parentFragmentId == rightId) that is NOT in right backbone
  // stays in the left block. This is the "big group" that may be multi-row.
  //
  // IMPORTANT: When dock filters change, a node might be demoted from the right pane to a left group.
  // In that case, the demoted node's children must ALSO be collected into a left group, recursively.
  // Otherwise, they become "orphan" passthrough nodes and get placed far off-screen (dock area).
  const downstreamByRight = new Map<string, Node<T>[]>();

  // Build parent->children map for all fragments (needed for recursive collection).
  const childrenByParent = new Map<string, Node<T>[]>();
  for (const n of fragments) {
    const d: any = n.data;
    const pid = String(d?.parentFragmentId || '').trim();
    if (!pid) continue;
    const list = childrenByParent.get(pid) || [];
    list.push(n);
    childrenByParent.set(pid, list);
  }

  // Recursively collect all descendants of a node that are NOT in rightIds.
  // Returns them grouped by the NEAREST right-side ancestor (for proper left-group association).
  const collectDescendantsNotInRight = (nodeId: string, rightAncestor: string, visited: Set<string>) => {
    if (visited.has(nodeId)) return;
    visited.add(nodeId);
    const children = childrenByParent.get(nodeId) || [];
    for (const child of children) {
      if (rightIds.has(child.id)) continue; // Child is on the right, skip
      // Add this child to the rightAncestor's left group
      const list = downstreamByRight.get(rightAncestor) || [];
      list.push(child);
      downstreamByRight.set(rightAncestor, list);
      // Recursively collect this child's descendants too
      collectDescendantsNotInRight(child.id, rightAncestor, visited);
    }
  };

  // For each right-side node, collect all its descendants that are not on the right.
  const collectedIds = new Set<string>();
  for (const rid of rightIds) {
    collectDescendantsNotInRight(rid, rid, collectedIds);
  }

  const sortByPriorityThenLabel = (list: Node<T>[]) => {
    return [...list].sort((a, b) => {
      const ap = getEdgePriority((a.data as any)?.edgeTypeFromParent);
      const bp = getEdgePriority((b.data as any)?.edgeTypeFromParent);
      if (ap !== bp) return ap - bp;
      const al = String((a.data as any)?.label || a.id);
      const bl = String((b.data as any)?.label || b.id);
      return al.localeCompare(bl);
    });
  };

  for (const [rid, list] of downstreamByRight.entries()) {
    downstreamByRight.set(rid, sortByPriorityThenLabel(list));
  }

  // === 4) Geometry constants ===
  // Right hierarchy is laid out in "columns" by horizontal level.
  // Spec: distance between right nodes horizontally is ~1 node.
  // We interpret this as 1 node-width GAP between columns => step ~= 2 * nodeWidth.
  const RIGHT_LEVEL_STEP_X = Math.max(nodeWidth * 2, nodeWidth + nodeSpacing);
  const RIGHT_BASE_X = 520; // leftmost right-hierarchy column anchor

  // Extra air between left (groups) and right (hierarchy) — about half a node width.
  const GROUP_GAP_X = 22 + Math.floor(nodeWidth / 2);
  const GROUP_GAP_Y = Math.max(16, Math.floor(layerSpacing * 0.12));

  // Right-side vertical density:
  // The right hierarchy should be compact (especially the first column / nearest level).
  const RIGHT_GAP_Y_BASE = Math.max(18, Math.floor(nodeSpacing * 0.35)); // ~20-24px with defaults
  // Right Dock (icons under nodes) needs reserved vertical space:
  // small gap + icon row (28px) + small gap.
  const RIGHT_DOCK_ICON_H = 28;
  const RIGHT_DOCK_ICON_GAP = 6;
  const RIGHT_DOCK_TOTAL_GAP_Y = RIGHT_DOCK_ICON_GAP * 2 + RIGHT_DOCK_ICON_H; // 40px
  const rightGapY = (level: number) => {
    // Level 1 (closest to the root node) should be tighter.
    const base = level === 1 ? Math.max(12, Math.floor(RIGHT_GAP_Y_BASE * 0.65)) : RIGHT_GAP_Y_BASE;
    // In dock mode, never go below the reserved icon gap, otherwise dock icons overlap the next node.
    if (rightDockMode) return Math.max(base, RIGHT_DOCK_TOTAL_GAP_Y);
    return base;
  };

  // Group content wrap:
  // - Fill order is column-major (vertical first, then horizontal).
  // - Rows:
  //   - count <= 2  => stacked vertically
  //   - count <= 12 => 2 rows
  //   - count > 12  => 3 rows
  const PAD_X = 16;
  // Extra vertical air so the (optional) type headers fit comfortably inside the group.
  // In compact left pane we render type column headers; give them a bit more breathing room.
  const PAD_TOP = compactLeftPane ? 34 : 28;
  const PAD_BOTTOM_BASE = 20;
  const REL_SUBTITLE_H = 14; // space for relation subtitles (only used when group is large)
  // Visual padding inside relation subcategory containers so nodes don't touch divider lines.
  // Kept deterministic (constant), independent of zoom.
  const LEFT_REL_CONTAINER_PAD_X = 14;
  // Keep left-group grids compact regardless of right-hierarchy spacing.
  // IMPORTANT:
  // In compact left pane we render relation/type subcategories as real containers with dividers,
  // and there is no explicit inter-block gap (see interRelGapX/interBlockGapX = 0).
  // So the visual "gap" between the last node in subcategory A and the first node in subcategory B
  // is effectively `2 * LEFT_REL_CONTAINER_PAD_X`. To avoid the UX oddity where nodes are closer
  // across subcategory boundaries than within a subcategory, match the intra-grid gap to that value.
  const groupChildGapX = compactLeftPane ? LEFT_REL_CONTAINER_PAD_X * 2 : Math.min(40, nodeSpacing);
  // Compact nodes should pack tighter (left pane only). Mixed rows will auto-expand as needed.
  // Match the 3-per-slot invariant: 3*49 + 2*7 = 161
  const groupChildGapY = compactLeftPane ? 7 : 40;

  const normalizeFragmentType = (t: any): string => {
    const raw = String(t || '').trim();
    if (!raw) return 'UNKNOWN';
    return raw.toUpperCase().replace(/\s+/g, '_');
  };

  // Left-pane compact test predicate (GEO + MARKET nodes).
  // When true, we render the node as compact AND pack it with compact height in the left group grid.
  const isLeftPaneCompactNode = (d: any): boolean => {
    if (!compactLeftPane) return false;
    const nodeType = String(d?.type || '').toUpperCase().trim().replace(/\s+/g, '_');
    const cp = String(d?.canonicalPath || '').trim();
    return (
      // Products & Services
      nodeType === 'PRODUCT' ||
      nodeType === 'SERVICE' ||
      nodeType === 'TECHNOLOGY' ||
      // Facilities / resources (left pane should be mini)
      nodeType === 'FACILITY' ||
      nodeType === 'DEPOSIT' ||
      nodeType === 'MARKET' ||
      cp.startsWith('/market/') ||
      nodeType === 'GEO_REGION' ||
      nodeType === 'LOCATION' ||
      nodeType === 'COUNTRY' ||
      nodeType === 'CITY' ||
      nodeType === 'STATE' ||
      nodeType === 'PROVINCE'
    );
  };

  const leftPaneLayoutHeightFor = (d: any): number => {
    // Hot path: this is called many times while computing rowHeights/offsets.
    // Cache by stable fields that affect compactness/height.
    const key =
      String(d?.nodeSize || '').toLowerCase().trim() +
      '|' +
      String(d?.type || '').toUpperCase().trim().replace(/\s+/g, '_') +
      '|' +
      String(d?.canonicalPath || '').trim();
    const cached = (leftPaneLayoutHeightFor as any)._cache?.get(key);
    if (typeof cached === 'number') return cached;
    // Respect explicit nodeSize when present (A/B by node property).
    // This avoids mismatches where rendering is compact but layout still reserves full height.
    const out =
      String(d?.nodeSize || '').toLowerCase().trim() === 'compact'
        ? COMPACT_NODE_LAYOUT_HEIGHT
        : (isLeftPaneCompactNode(d) ? COMPACT_NODE_LAYOUT_HEIGHT : nodeHeight);
    const m = (leftPaneLayoutHeightFor as any)._cache || new Map<string, number>();
    m.set(key, out);
    (leftPaneLayoutHeightFor as any)._cache = m;
    return out;
  };

  // Business-friendly labels for type groups in left pane
  const getTypeGroupLabel = (typeKey: string): string => {
    const tt = normalizeFragmentType(typeKey);
    const TYPE_LABEL_MAP: Record<string, string> = {
      // Products & Services
      'PRODUCT': 'Products & Services',
      'SERVICE': 'Products & Services',
      'TECHNOLOGY': 'Products & Services',
      // Business Interests
      'ORGANIZATION': 'Business Interests',
      'MEDIA_CHANNEL': 'Business Interests',
      // Communities & Networks
      'COMMUNITY': 'Communities & Networks',
      // Key People
      'PERSON': 'Key People',
      // Geographic Presence
      'GEO_REGION': 'Geo Presence',
      'LOCATION': 'Geo Presence',
      'COUNTRY': 'Geo Presence',
      'STATE': 'Geo Presence',
      'PROVINCE': 'Geo Presence',
      'CITY': 'Geo Presence',
      // Facilities
      'FACILITY': 'Facilities',
      // Natural Resources
      'DEPOSIT': 'Natural Resources',
      // Milestones
      'EVENT': 'Milestones',
      // Threats
      'RISK': 'Threats',
    };
    return TYPE_LABEL_MAP[tt] || typeKey
      .replace(/_/g, ' ')
      .toLowerCase()
      .replace(/\b\w/g, (c) => c.toUpperCase());
  };

  // Type bucketing order for horizontal separation inside left groups.
  // Product request: keep Products separate from Organizations, etc.
  const typeOrderScore = (t: string): number => {
    const tt = normalizeFragmentType(t);
    // "Products"
    if (tt === 'PRODUCT') return 10;
    if (tt === 'SERVICE') return 20;
    if (tt === 'TECHNOLOGY') return 30;
    // "Organizations / actors"
    if (tt === 'ORGANIZATION') return 110;
    if (tt === 'PERSON') return 120;
    if (tt === 'MEDIA_CHANNEL') return 130;
    if (tt === 'COMMUNITY') return 140;
    // "Places / assets"
    if (tt === 'FACILITY') return 210;
    if (tt === 'DEPOSIT') return 215;
    if (tt === 'GEO_REGION') return 220;
    if (tt === 'LOCATION') return 230;
    // Other domain objects
    if (tt === 'EVENT') return 310;
    if (tt === 'RISK') return 320;
    if (tt === 'UNKNOWN') return 900;
    return 500; // stable middle bucket for new/unknown types
  };

  type RelBlock<TNode = any> = {
    edgeTypeKey: string;
    label: string;
    /**
     * Nodes that are actually laid out in the grid for this subcategory.
     * If collapsed: contains ONLY the chosen controller node.
     */
    nodes: Node<TNode>[];
    cols: number;
    startX: number;
    blockW: number;
    /** Total node count (even when collapsed, we need to show "N items"). */
    nodeCount: number;
    /** Whether this subcategory is collapsed (renders as a stack-controller node). */
    collapsed: boolean;
    /** First node id in this subcategory (stable ordering, used for focus rules). */
    firstNodeId?: string;
    /** If collapsed, all other member node IDs that are hidden under the controller. */
    hiddenMemberIds?: string[];
    /** If collapsed, the chosen controller node id. */
    controllerId?: string;
    /** If collapsed, stable key used for stack-proxy edge generation. */
    stackKey?: string;
    /** Optional override for row count within this subcategory block. */
    rows?: number;
  };
  type TypeBlock<TNode = any> = {
    typeKey: string;
    rows: number;
    cols: number;
    startX: number;
    blockW: number;
    relBlocks: RelBlock<TNode>[];
  };

  // Default-collapsed edge types (subcategories that start collapsed by default).
  // "Relatives" + "Related" are often secondary info and can be very noisy in the left pane.
  const DEFAULT_COLLAPSED_EDGE_TYPES = new Set(['RELATIVE_OF', 'RELATED_TO']);

  const edgeTypeCaptionShortLocal = (raw: any): string => {
    const t = normalizeEdgeType(String(raw || '').trim());
    const MAP: Record<string, string> = {
      DEPENDS_ON: 'Depends',
      REQUIRES: 'Requires',
      NEEDS: 'Needs',
      USES: 'Uses',
      CONSUMES: 'Consumes',
      OWNS: 'Ownership',
      MANAGES: 'Manages',
      CONTROLS: 'Controls',
      INVESTS_IN: 'Invests',
      PARENT_OF: 'Parent',
      SUBSIDIARY_OF: 'Subsidiary',
      BOARD_MEMBER_OF: 'Board',
      EXECUTIVE_OF: 'Executive',
      // UX copy: for an auditor node, these are audited entities (clients), not "audited by".
      AUDITED_BY: 'Audit',
      RELATIVE_OF: 'Relatives',
      RELATED_TO: 'Related',
      SUPPLIES_TO: 'Supplies',
      PRODUCES: 'Produces',
      TRANSFORMS_INTO: 'Transforms',
      LOCATED_IN: 'Located',
      OPERATES_IN: 'Operates',
      BASED_IN: 'Based',
      HEADQUARTERED_IN: 'HQ',
      COMPETES_WITH: 'Competitors',
      PARTNERS_WITH: 'Partners',
      COOPERATES_WITH: 'Cooperates',
      REGULATES: 'Regulates',
      INFLUENCES: 'Influences',
      AFFECTS: 'Affects',
      IMPACTS: 'Impacts',
      // Market / hierarchy
      PARTICIPATES_IN: 'Participates',
      DOMINATES: 'Dominates',
      HAS_SUBMARKET: 'Submarket',
      HAS_SUBGROUP: 'Subgroup',
      ADMINISTERS: 'Administers',
      // Community relationships
      OPERATES_VIA: 'Via',
      MEMBER_OF: 'Member',
      AFFILIATED_WITH: 'Affiliated',
      // Extractive
      EXTRACTS_FROM: 'Extracts',
    };
    if (MAP[t]) return MAP[t]!;
    const human = t
      .replace(/_WITH$/g, '')
      .replace(/_TO$/g, '')
      .replace(/_OF$/g, '')
      .replace(/_/g, ' ')
      .toLowerCase()
      .replace(/\b\w/g, (c) => c.toUpperCase());
    return human.length > 18 ? `${human.slice(0, 18)}…` : human;
  };

  const groupDimsWrap = (children: Node<T>[], parentId: string, groupId?: string) => {
    // IMPORTANT: group grid must be computed from VISIBLE children only.
    // If we include `hidden` nodes (e.g. local relation filters), we can end up reserving empty rows/slots.
    const safeChildren = Array.isArray(children)
      ? children
          .filter(Boolean)
          .filter((n) => !preHiddenIds.has(n.id) && !Boolean((n as any).hidden))
      : [];
    const safeCount = safeChildren.length;
    if (safeCount <= 0) {
      return {
        w: PAD_X * 2,
        h: PAD_TOP + PAD_BOTTOM_BASE,
        cols: 0,
        rows: 0,
        blocks: [] as TypeBlock<T>[],
        interBlockGapX: 0,
        relLabelY: PAD_TOP,
        padBottom: PAD_BOTTOM_BASE,
      };
    }

    // Use provided groupId or construct a default one for collapse state lookup.
    const effectiveGroupId = groupId || `levels:left:${parentId}:down`;

    // Row policy is applied PER TYPE BLOCK (otherwise splitting into types can create "empty rows").
    // 2026 UX: cap at 2 rows max — 3 rows make groups too tall on laptops.
    const rowsForCount = (cnt: number): number => {
      const c = Math.max(0, Math.floor(cnt || 0));
      // UX: for small groups (<5 items) always keep a single row to avoid wasting vertical space.
      if (c <= 0) return 1;
      if (c <= 4) return 1;
      // Cap at 2 rows for better UX on smaller screens
      return 2;
    };
    const useRelGrouping = safeCount > 4;

    // Cap vertical stacking inside a single column:
    // - Compact mini-nodes: up to 6 per column.
    // - Full-size nodes: up to 2 per column (anything beyond spills into next column).
    const maxVerticalForNodes = (nodes: Array<Node<T> | undefined | null>): number => {
      const list = (nodes || []).filter(Boolean) as Node<T>[];
      if (list.length === 0) return 1;
      const hasFull = list.some((n) => leftPaneLayoutHeightFor((n as any)?.data || {}) > COMPACT_NODE_LAYOUT_HEIGHT + 1);
      return hasFull ? 2 : 6;
    };

    // Bucket by `data.type` (stable: preserve order within each bucket).
    const byType = new Map<string, Node<T>[]>();
    for (const n of safeChildren) {
      const d: any = n.data;
      const tk = normalizeFragmentType(d?.type);
      const arr = byType.get(tk) || [];
      arr.push(n);
      byType.set(tk, arr);
    }

    const typeKeys = Array.from(byType.keys()).sort((a, b) => {
      const sa = typeOrderScore(a);
      const sb = typeOrderScore(b);
      if (sa !== sb) return sa - sb;
      return a.localeCompare(b);
    });

    const blocks: TypeBlock<T>[] = [];
    // If any type block already needs 2 rows, allow other blocks to expand to 2 rows as well.
    // This keeps width compact without increasing overall height (which is already 2 rows).
    const forceTwoRows = Array.from(byType.values()).some((list) => (list?.length || 0) > 4);
    // In compact left pane, vertical density matters more than width — and forcing 2 rows across
    // *all* type blocks can create "phantom" empty vertical capacity (looks like space for extra mini-nodes).
    // So only apply the 2-row coercion in non-compact modes.
    const minRowsForBlock = forceTwoRows && !compactLeftPane ? 2 : 1;
    // IMPORTANT (Approach #1, nested containers):
    // Type sections are now real container nodes with crisp vertical dividers (`groupBorderMode: divider-right`).
    // If we also keep a physical inter-block X gap, the first "segment" becomes narrower (no leading gap),
    // while middle/last segments include the gap — visually выглядят шире/прилипают.
    // So in compact left pane we rely on dividers (not empty space) for separation.
    const interBlockGapX = compactLeftPane ? 0 : Math.max(84, Math.floor(nodeWidth * 0.45));
    // Same rationale as interBlockGapX: relation subcategories are real containers with dividers.
    // Avoid "empty space gaps" that make some segments visually wider/narrower.
    const interRelGapX = compactLeftPane ? 0 : Math.max(24, Math.floor(nodeWidth * 0.18));

    const bestRelTypeToParent = (child: Node<T>): string => {
      const cid = String(child?.id || '').trim();
      const pid = String(parentId || '').trim();
      if (!pid || !cid) return normalizeEdgeType((child.data as any)?.edgeTypeFromParent);
      const found = new Set<string>();
      for (const e of (edges || []) as any[]) {
        const eid = String(e?.id || '').trim();
        if (eid.startsWith('levels:agg:')) continue;
        if (Boolean(e?.data?.levelsAgg)) continue;
        if (Boolean(e?.data?.levelsStackProxy)) continue;
        const s = normId(e?.source);
        const t = normId(e?.target);
        if (!s || !t) continue;
        const hit = (s === pid && t === cid) || (s === cid && t === pid);
        if (!hit) continue;
        const et = edgeTypeOf(e);
        if (et) found.add(et);
      }
      // Product rule: if the expanded node (group parent) has an OUTGOING PRODUCES edge to this child,
      // it must dominate relation grouping even if other edge types exist between the same nodes.
      // (UX: products should always be surfaced as "Produces" when present.)
      if (found.size > 0 && found.has('PRODUCES')) {
        for (const e of (edges || []) as any[]) {
          const eid = String(e?.id || '').trim();
          if (eid.startsWith('levels:agg:')) continue;
          if (Boolean(e?.data?.levelsAgg)) continue;
          if (Boolean(e?.data?.levelsStackProxy)) continue;
          const s = normId(e?.source);
          const t = normId(e?.target);
          if (s !== pid || t !== cid) continue; // only OUTGOING from parent -> child
          if (edgeTypeOf(e) === 'PRODUCES') return 'PRODUCES';
        }
      }
      if (found.size === 0) return normalizeEdgeType((child.data as any)?.edgeTypeFromParent);
      const ordered = Array.from(found).sort((a, b) => {
        const ap = getEdgePriority(a);
        const bp = getEdgePriority(b);
        if (ap !== bp) return ap - bp;
        return a.localeCompare(b);
      });
      return ordered[0] || normalizeEdgeType((child.data as any)?.edgeTypeFromParent);
    };

    let cursorX = 0;
    let totalCols = 0;
    let maxInnerH = 0;
    let maxRows = 1;
    for (let i = 0; i < typeKeys.length; i++) {
      const typeKey = typeKeys[i]!;
      const list = byType.get(typeKey) || [];
      if (list.length === 0) continue;

      // Special case: MARKET nodes in the left pane should render as tall columns:
      // "2 full nodes vertically" == 6 compact nodes per column (3 per full slot).
      if (compactLeftPane && String(typeKey).toUpperCase().trim() === 'MARKET') {
        const nodesFlat = list.slice().sort((a, b) => {
          const al = String((a.data as any)?.label || a.id);
          const bl = String((b.data as any)?.label || b.id);
          return al.localeCompare(bl);
        });

        const rowsForType = Math.max(1, Math.min(6, nodesFlat.length));
        const cols = Math.max(1, Math.ceil(nodesFlat.length / rowsForType));
        const minBase = COMPACT_NODE_LAYOUT_HEIGHT;
        const rowHeights = new Array(rowsForType).fill(minBase);
        // Column-major fill: row index is idx % rowsForType
        for (let idx = 0; idx < nodesFlat.length; idx++) {
          const rowIdx = idx % rowsForType;
          const nd: any = (nodesFlat[idx] as any)?.data || {};
          rowHeights[rowIdx] = Math.max(rowHeights[rowIdx], leftPaneLayoutHeightFor(nd));
        }
        const innerHForType =
          rowHeights.reduce((acc, h) => acc + h, 0) +
          Math.max(0, rowsForType - 1) * groupChildGapY;
        maxInnerH = Math.max(maxInnerH, innerHForType);
        maxRows = Math.max(maxRows, rowsForType);

        const blockW = cols * nodeWidth + Math.max(0, cols - 1) * groupChildGapX + LEFT_REL_CONTAINER_PAD_X * 2;
        const relBlocks: RelBlock<T>[] = [
          {
            edgeTypeKey: '',
            label: '',
            nodes: nodesFlat,
            cols,
            startX: 0,
            blockW,
            nodeCount: nodesFlat.length,
            collapsed: false,
          } as any,
        ];
        totalCols += cols;
        blocks.push({ typeKey, rows: rowsForType, cols, startX: cursorX, blockW, relBlocks });
        cursorX += blockW;

        if (i < typeKeys.length - 1) cursorX += interBlockGapX;
        continue;
      }

      // Optionally group within the type block by the PRIORITY relation type to the parent.
      const relBlocks: RelBlock<T>[] = [];
      const isProductsServicesType =
        String(typeKey).toUpperCase().trim() === 'PRODUCT' ||
        String(typeKey).toUpperCase().trim() === 'SERVICE' ||
        String(typeKey).toUpperCase().trim() === 'TECHNOLOGY';
      const isFacilitiesOrDepositType =
        String(typeKey).toUpperCase().trim() === 'FACILITY' ||
        String(typeKey).toUpperCase().trim() === 'DEPOSIT';
      const isGeoType =
        String(typeKey).toUpperCase().trim() === 'GEO_REGION' ||
        String(typeKey).toUpperCase().trim() === 'LOCATION' ||
        String(typeKey).toUpperCase().trim() === 'COUNTRY' ||
        String(typeKey).toUpperCase().trim() === 'STATE' ||
        String(typeKey).toUpperCase().trim() === 'PROVINCE' ||
        String(typeKey).toUpperCase().trim() === 'CITY';
      // UX: Products & Services should NOT be subdivided by relation edge types.
      // Render as simple column-major grid (taller columns).
      // UX: GEO should also NOT be subdivided by relation edge types (LOCATED/OPERATES/etc).
      // UX: Facilities and Deposits should also NOT be subdivided by relation edge types.
      const useRelGroupingForType = useRelGrouping && !isProductsServicesType && !isGeoType && !isFacilitiesOrDepositType;

      if (useRelGroupingForType) {
        const byRel = new Map<string, Node<T>[]>();
        for (const n of list) {
          const rel = bestRelTypeToParent(n);
          const arr = byRel.get(rel) || [];
          arr.push(n);
          byRel.set(rel, arr);
        }
        // Auto-shrink rows if the "last row" would be entirely empty in this type block.
        // IMPORTANT: compute this from VISIBLE sizes:
        // - if a relation subcategory is collapsed, it occupies 1 slot (controller) even if it has many members.
        const baseRowsForType = rowsForCount(list.length);
        let maxRelSizeVisible = 1;
        let competesMaxVisible = 0;
        for (const [rk, v] of byRel.entries()) {
          const relKey = String(rk || '').trim();
          const nodesRel = (v || []);
          const collapsed = isSubcategoryCollapsed(effectiveGroupId, relKey) && nodesRel.length > 1;
          const visibleSize = collapsed ? 1 : nodesRel.length;
          maxRelSizeVisible = Math.max(maxRelSizeVisible, Math.max(1, visibleSize));
          if (normalizeEdgeType(relKey) === 'COMPETES_WITH') {
            competesMaxVisible = Math.max(competesMaxVisible, Math.max(0, visibleSize));
          }
        }
        let rowsForType = forceTwoRows
          ? Math.max(minRowsForBlock, baseRowsForType)
          : Math.max(1, Math.min(baseRowsForType, maxRelSizeVisible));
        // UX: if this type-block is ONLY "Competes", render as tall columns (up to 6 compact nodes per column).
        // This avoids tiny groups becoming a single horizontal row (2+ items side-by-side).
        if (compactLeftPane) {
          // In Levels-left, we want deterministic tall columns:
          // - mini nodes stack vertically up to 6 per column
          // - full-size nodes stack vertically up to 2 per column
          const typeMaxV = maxVerticalForNodes(list);
          rowsForType = Math.max(1, Math.min(typeMaxV, maxRelSizeVisible));
          // (competesMaxVisible is included in maxRelSizeVisible already; keep for future debugging)
          void competesMaxVisible;
        }
        // Compute per-row max height for this type block (supports mixed compact/full nodes).
        const minBase = compactLeftPane ? COMPACT_NODE_LAYOUT_HEIGHT : nodeHeight;
        const rowHeights = new Array(rowsForType).fill(minBase);
        for (const [rk, v] of byRel.entries()) {
          const relKey = String(rk || '').trim();
          const nodesRel = (v || []).slice().sort((a, b) => {
            const al = String((a.data as any)?.label || a.id);
            const bl = String((b.data as any)?.label || b.id);
            return al.localeCompare(bl);
          });
          const collapsed = isSubcategoryCollapsed(effectiveGroupId, relKey) && nodesRel.length > 1;
          const controller = nodesRel[0];
          const nodesForLayout = collapsed ? (controller ? [controller] : []) : nodesRel;
          for (let idx = 0; idx < nodesForLayout.length; idx++) {
            const rowIdx = idx % Math.max(1, rowsForType);
            const nd: any = (nodesForLayout[idx] as any)?.data || {};
            rowHeights[rowIdx] = Math.max(rowHeights[rowIdx], leftPaneLayoutHeightFor(nd));
          }
        }
        const innerHForType =
          rowHeights.reduce((acc, h) => acc + h, 0) +
          Math.max(0, rowsForType - 1) * groupChildGapY;
        maxInnerH = Math.max(maxInnerH, innerHForType);
        maxRows = Math.max(maxRows, rowsForType);

        const relKeys = Array.from(byRel.keys()).sort((a, b) => {
          const ap = getEdgePriority(a);
          const bp = getEdgePriority(b);
          if (ap !== bp) return ap - bp;
          return a.localeCompare(b);
        });
        let relCursor = 0;
        let relColsTotal = 0;
        for (let ri = 0; ri < relKeys.length; ri++) {
          const relKey = relKeys[ri]!;
          const nodesRel = (byRel.get(relKey) || []).slice().sort((a, b) => {
            const al = String((a.data as any)?.label || a.id);
            const bl = String((b.data as any)?.label || b.id);
            return al.localeCompare(bl);
          });

          // Subcategory collapse is STRUCTURAL:
          // - If collapsed: only ONE controller node participates in the grid.
          // - Other members are hiddenByStack and their edges will be proxied via stackproxy edges.
          const collapsed = isSubcategoryCollapsed(effectiveGroupId, relKey) && nodesRel.length > 1;
          const controller = nodesRel[0];
          const hiddenMemberIds = collapsed
            ? nodesRel.slice(1).map((n) => String(n.id || '').trim()).filter(Boolean)
            : [];
          const nodesForLayout = collapsed ? (controller ? [controller] : []) : nodesRel;

          const relKeyNorm = normalizeEdgeType(relKey);
          // UX: "Competes" should stack in a compact column even if other subcategories exist.
          const rowsForRel =
            compactLeftPane && relKeyNorm === 'COMPETES_WITH'
              // NOTE: competitors in Levels-left are always rendered as compact mini-nodes (nodeSize is applied later),
              // so treat them as compact for row calculation to avoid "full-size" spacing.
              ? Math.max(1, Math.min(6, nodesForLayout.length))
              : Math.max(1, rowsForType);
          const cols = collapsed ? 1 : Math.ceil(nodesForLayout.length / rowsForRel);
          const blockW = cols * nodeWidth + Math.max(0, cols - 1) * groupChildGapX + LEFT_REL_CONTAINER_PAD_X * 2;

          const controllerId = collapsed ? String(controller?.id || '').trim() : undefined;
          const firstNodeId = String(nodesRel[0]?.id || '').trim() || controllerId;
          const stackKey = collapsed ? `levels:leftsubcat:${effectiveGroupId}:${typeKey}:${relKey}` : undefined;

          relBlocks.push({
            edgeTypeKey: relKey,
            label: edgeTypeCaptionShortLocal(relKey),
            nodes: nodesForLayout,
            cols,
            startX: relCursor,
            blockW,
            nodeCount: nodesRel.length,
            collapsed,
            firstNodeId,
            hiddenMemberIds: collapsed ? hiddenMemberIds : undefined,
            controllerId,
            stackKey,
            rows: rowsForRel,
          });
          relColsTotal += cols;
          relCursor += blockW;
          if (ri < relKeys.length - 1) relCursor += interRelGapX;
        }
        totalCols += relColsTotal;
        const typeBlockW = relCursor;
        blocks.push({ typeKey, rows: rowsForType, cols: relColsTotal, startX: cursorX, blockW: typeBlockW, relBlocks });
        cursorX += typeBlockW;
      } else {
        // For compact Products & Services, make taller columns:
        // "2 full nodes vertically" == 6 compact nodes per column (3 per full slot).
        const rowsForType =
          compactLeftPane && (isProductsServicesType || isGeoType || isFacilitiesOrDepositType)
            ? Math.max(1, Math.min(maxVerticalForNodes(list), list.length))
            : Math.max(minRowsForBlock, rowsForCount(list.length));
        const nodesFlat = list.slice().sort((a, b) => {
          const al = String((a.data as any)?.label || a.id);
          const bl = String((b.data as any)?.label || b.id);
          return al.localeCompare(bl);
        });
        // Compute per-row max height (supports mixed compact/full nodes).
        const minBase = compactLeftPane ? COMPACT_NODE_LAYOUT_HEIGHT : nodeHeight;
        const rowHeights = new Array(rowsForType).fill(minBase);
        for (let idx = 0; idx < nodesFlat.length; idx++) {
          const rowIdx = idx % Math.max(1, rowsForType);
          const nd: any = (nodesFlat[idx] as any)?.data || {};
          rowHeights[rowIdx] = Math.max(rowHeights[rowIdx], leftPaneLayoutHeightFor(nd));
        }
        const innerHForType =
          rowHeights.reduce((acc, h) => acc + h, 0) +
          Math.max(0, rowsForType - 1) * groupChildGapY;
        maxInnerH = Math.max(maxInnerH, innerHForType);
        maxRows = Math.max(maxRows, rowsForType);

        const cols = Math.ceil(nodesFlat.length / Math.max(1, rowsForType));
        const blockW = cols * nodeWidth + Math.max(0, cols - 1) * groupChildGapX + LEFT_REL_CONTAINER_PAD_X * 2;
        relBlocks.push({
          edgeTypeKey: '',
          label: '',
          nodes: nodesFlat,
          cols,
          startX: 0,
          blockW,
          nodeCount: nodesFlat.length,
          collapsed: false, // Non-grouped blocks are never collapsed
        });
        totalCols += cols;
        blocks.push({ typeKey, rows: rowsForType, cols, startX: cursorX, blockW, relBlocks });
        cursorX += blockW;
      }

      if (i < typeKeys.length - 1) cursorX += interBlockGapX;
    }

    const innerW = cursorX;
    const minBase = compactLeftPane ? COMPACT_NODE_LAYOUT_HEIGHT : nodeHeight;
    const innerH = Math.max(minBase, maxInnerH);
    // Width is a pure function of column counts + fixed widths/gaps.
    // We include a deterministic *half-gap* on BOTH sides so the content doesn't visually "stick"
    // to the right edge (group is right-anchored in Levels mode) and borders stay symmetric.
    const outerPadX = blocks.length > 1 ? Math.max(0, Math.floor(interBlockGapX / 2)) : 0;
    const w = innerW + PAD_X * 2 + outerPadX * 2;
    // Height: keep a little air, but don't over-reserve.
    // In compact left pane, relation labels are rendered INSIDE relation containers (footer),
    // so we only need enough bottom space to avoid overlap:
    // footer uses `bottom: 6` and `height: 16` => ~22px, plus a small gap.
    const LEFT_FOOTER_BLOCK_H = 22;
    const LEFT_FOOTER_GAP_H = 8;
    const padBottom = compactLeftPane
      ? Math.max(PAD_BOTTOM_BASE, LEFT_FOOTER_BLOCK_H + LEFT_FOOTER_GAP_H)
      : (PAD_BOTTOM_BASE + (useRelGrouping ? REL_SUBTITLE_H : 0));
    const relLabelY = PAD_TOP + innerH + 4;
    const h = innerH + PAD_TOP + padBottom;
    return { w, h, cols: totalCols, rows: Math.max(1, maxRows), blocks, interBlockGapX, relLabelY, padBottom, interRelGapX, outerPadX };
  };

  // === 5) Place Right hierarchy nodes ===
  // X: by horizontal level (leaf=0 on the left, ultimate=maxLevel on the right).
  // Y: symmetric / barycentric layout:
  // - nodes with children are centered around their children
  // - multiple parents are placed symmetrically around the node to the right (via overlap resolution per column)
  const rightSorted = [...rightNodes].sort((a, b) => {
    const la = hLevel.get(a.id) || 0;
    const lb = hLevel.get(b.id) || 0;
    if (la !== lb) return la - lb;
    const ya = a.position?.y ?? 0;
    const yb = b.position?.y ?? 0;
    if (ya !== yb) return ya - yb;
    const al = String((a.data as any)?.label || a.id);
    const bl = String((b.data as any)?.label || b.id);
    return al.localeCompare(bl);
  });

  const rightPos = new Map<string, { x: number; y: number }>();
  const unitHeights = new Map<string, number>();

  for (const rn of rightSorted) {
    const children = downstreamByRight.get(rn.id) || [];
    const h = children.length > 0 ? Math.max(nodeHeight, groupDimsWrap(children, rn.id).h) : nodeHeight;
    unitHeights.set(rn.id, h);
  }

  const unitH = (id: string) => unitHeights.get(id) || nodeHeight;

  // Group right nodes by horizontal level.
  const idsByLevel = new Map<number, string[]>();
  for (const rn of rightSorted) {
    const lv = hLevel.get(rn.id) || 0;
    const arr = idsByLevel.get(lv) || [];
    arr.push(rn.id);
    idsByLevel.set(lv, arr);
  }

  // Precompute labels for fast sort
  const labelById = new Map<string, string>();
  for (const n of rightNodes) {
    labelById.set(n.id, String((n.data as any)?.label || n.id));
  }

  // Helper to sort nodes by clustering related ones together (connected components).
  // This ensures that partners/related entities stay vertically adjacent.
  const sortNodesByClusters = (nodeIds: string[]): string[] => {
    if (nodeIds.length <= 1) return nodeIds;
    const idSet = new Set(nodeIds);
    const adj = new Map<string, Set<string>>();
    for (const id of nodeIds) adj.set(id, new Set());

    // Build adjacency from valid edges (using precomputed pairEdgeTypes)
    for (const key of pairEdgeTypes.keys()) {
      const parts = key.split('||');
      if (parts.length !== 2) continue;
      const s = parts[0];
      const t = parts[1];
      if (idSet.has(s) && idSet.has(t)) {
        adj.get(s)?.add(t);
        adj.get(t)?.add(s);
      }
    }

    const visited = new Set<string>();
    const clusters: string[][] = [];

    // Find connected components
    // Sort initial list alphabetically to ensure deterministic cluster discovery order
    const sortedIds = [...nodeIds].sort((a, b) => {
       const al = labelById.get(a) || a;
       const bl = labelById.get(b) || b;
       return al.localeCompare(bl);
    });

    for (const id of sortedIds) {
      if (visited.has(id)) continue;
      const cluster: string[] = [];
      const queue = [id];
      visited.add(id);
      
      while (queue.length > 0) {
        const curr = queue.shift()!;
        cluster.push(curr);
        const neighbors = Array.from(adj.get(curr) || []).sort(); // Deterministic traversal
        for (const neighbor of neighbors) {
          if (!visited.has(neighbor)) {
            visited.add(neighbor);
            queue.push(neighbor);
          }
        }
      }
      
      // Sort nodes within cluster alphabetically
      cluster.sort((a, b) => {
         const al = labelById.get(a) || a;
         const bl = labelById.get(b) || b;
         return al.localeCompare(bl);
      });
      clusters.push(cluster);
    }

    // Sort clusters by their first element (representative)
    clusters.sort((a, b) => {
       const al = labelById.get(a[0]) || a[0];
       const bl = labelById.get(b[0]) || b[0];
       return al.localeCompare(bl);
    });

    return clusters.flat();
  };

  for (const [lv, arr] of idsByLevel.entries()) {
    idsByLevel.set(lv, sortNodesByClusters(arr));
  }

  // Compute y-centers by propagating from leaves (level 0) to ultimate nodes (maxLevel).
  const yCenter = new Map<string, number>();
  let cursorY = 0;
  const ensureLevelY = (level: number) => {
    const ids = idsByLevel.get(level) || [];
    if (ids.length === 0) return;
    const gapY = rightGapY(level);

    const desired = new Map<string, number>();
    for (const id of ids) {
      const kids = Array.from(childrenOf.get(id) || []);
      if (kids.length > 0) {
        const ys = kids.map((k) => yCenter.get(k)).filter((v): v is number => typeof v === 'number');
        if (ys.length > 0) {
          desired.set(id, ys.reduce((s, v) => s + v, 0) / ys.length);
          continue;
        }
      }
      // Leaf fallback: append at the end.
      desired.set(id, cursorY + unitH(id) / 2);
      cursorY += unitH(id) + gapY;
    }

    // Resolve overlaps within this level (keeps vertical symmetry; parents with same target Y spread evenly).
    const sorted = [...ids].sort((a, b) => (desired.get(a)! - desired.get(b)!));
    let prevBottom = -Infinity;
    for (const id of sorted) {
      const half = unitH(id) / 2;
      const minY = isFinite(prevBottom) ? prevBottom + gapY + half : -Infinity;
      const yc = Math.max(desired.get(id)!, minY);
      yCenter.set(id, yc);
      prevBottom = yc + half;
    }
  };

  // Level 0: deterministic stack.
  ensureLevelY(0);
  // Higher levels: barycentric.
  for (let lv = 1; lv <= maxLevel; lv++) ensureLevelY(lv);

  // Assign final positions.
  for (const rn of rightSorted) {
    const lv = hLevel.get(rn.id) || 0;
    const x = RIGHT_BASE_X + lv * RIGHT_LEVEL_STEP_X;
    const yc = yCenter.get(rn.id) ?? 0;
    rightPos.set(rn.id, { x, y: yc - nodeHeight / 2 });
  }

  // Center vertically around 0 for nicer initial view
  if (rightSorted.length > 0) {
    let minY = Infinity;
    let maxY = -Infinity;
    for (const rn of rightSorted) {
      const p = rightPos.get(rn.id)!;
      const h = unitHeights.get(rn.id) || nodeHeight;
      minY = Math.min(minY, p.y);
      maxY = Math.max(maxY, p.y + h);
    }
    const shiftY = -((minY + maxY) / 2);
    for (const rn of rightSorted) {
      const p = rightPos.get(rn.id)!;
      rightPos.set(rn.id, { x: p.x, y: p.y + shiftY });
    }
  }

  // === 5.75) Right-side temporary stacking (compute BEFORE groups) ===
  // IMPORTANT: stacks must be computed before group packing & barycentric sweeps, otherwise hidden stack members
  // keep reserving vertical space and make groups appear "too low" / non-contiguous.
  // When Right Dock mode is enabled, we disable right-side stack controllers entirely.
  // (Dock-panel filters replace them.)
  const STACK_MIN = rightDockMode ? Number.POSITIVE_INFINITY : 2;
  // Stack group layout constants (used for spacing + container size)
  const STACK_GROUP_PAD_X = 8;
  const STACK_GROUP_PAD_TOP = 44; // room for header with collapse button
  const STACK_GROUP_PAD_BOTTOM = 8;
  const stackGroupGapY = 22;
  const expandedStackKeys = new Set<string>();
  for (const n of fragments) {
    const d: any = n.data;
    const k = String(d?.stackGroupKey || '').trim();
    if (!k) continue;
    if (d?.stackExpanded) expandedStackKeys.add(k);
  }

  const stackHiddenIds = new Set<string>();
  const expandedStackMemberIds = new Set<string>();
  const expandedStackMembersByKey = new Map<string, string[]>();
  const stackExpandedHeights = new Map<string, number>(); // controllerId -> group height
  const stackMetaById = new Map<
    string,
    { key: string; count: number; expanded: boolean; title: string; subtitle: string; kind: string; visualType: string }
  >();
  const stackMembersByKey = new Map<string, string[]>();
  // Membership maps (used to preserve connectivity via stack-proxy edges).
  const stackKeyByMemberId = new Map<string, string>(); // memberId -> stackGroupKey
  const stackControllerByMemberId = new Map<string, string>(); // memberId -> controllerId

  // Control / ownership / governance signals that should be bucketed into Stakeholders.
  // NOTE: This is used for right-pane stacking in Levels layout.
  const CONTROL_EDGE_TYPES = new Set(['OWNS', 'PARENT_OF', 'SUBSIDIARY_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'FOUNDED']);
  // Person -> Org "control / governance" signals. If present, we bucket the PERSON into Stakeholders instead of generic People.
  // (Example: founder or investor should appear among stakeholders for the expanded org.)
  const PERSON_CONTROL_EDGE_TYPES = new Set(['FOUNDED', 'INVESTS_IN', 'OWNS', 'CONTROLS', 'MANAGES', 'BOARD_MEMBER_OF', 'EXECUTIVE_OF']);
  const GEO_TYPES = new Set(['GEO_REGION', 'LOCATION', 'COUNTRY', 'STATE', 'PROVINCE', 'CITY']);
  const SUPPLY_EDGE_TYPES = new Set(['SUPPLIES_TO', 'PRODUCES']);
  const DEPENDENCY_EDGE_TYPES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
  const DOMINATES_EDGE_TYPES = new Set(['DOMINATES']);
  const stackKindOfRightNode = (id: string, relatedId?: string): { kind: string; title: string; visualType: string } | null => {
    const node = fragments.find((x) => x.id === id);
    const d: any = (node as any)?.data;
    const tk = normalizeFragmentType(String(d?.type || '').trim());
    if (!tk) return null;

    const pairTypes = (() => {
      const a = String(id || '').trim();
      const b = String(relatedId || '').trim();
      if (!a || !b) return null;
      return pairEdgeTypes.get(pairKey(a, b)) || null;
    })();

    // Dependencies: prerequisites for the expanded node (outgoing DEPENDS_ON from expanded -> prerequisite).
    // Rule: only bucket when the pair has ONLY dependency edge types (no other relations compete for placement).
    if (relatedId && pairTypes && pairTypes.size > 0) {
      const a = String(relatedId || '').trim(); // anchor (expanded)
      const b = String(id || '').trim(); // candidate prerequisite
      const pureDependencyPair = Array.from(pairTypes).every((x) => DEPENDENCY_EDGE_TYPES.has(normalizeEdgeType(x)));
      if (pureDependencyPair) {
        const dk = `${a}||${b}`;
        const dset = directedEdgeTypes.get(dk) || null;
        const hasOutgoingDependency = dset ? Array.from(dset).some((x) => DEPENDENCY_EDGE_TYPES.has(normalizeEdgeType(x))) : false;
        if (hasOutgoingDependency) {
          return { kind: 'dependencies', title: 'Dependencies', visualType: 'DEPENDENCY' };
        }
      }
    }

    const isMarketType = (t: string) => t === 'MARKET' || t.startsWith('MARKET_');
    const isOrgType = (t: string) => t === 'ORGANIZATION' || t === 'COMPANY' || t === 'ORG';

    // Dominates: orgs that dominate a market (stack those orgs together).
    if (relatedId && pairTypes && pairTypes.size > 0) {
      const hasDominates = Array.from(pairTypes).some((x) => DOMINATES_EDGE_TYPES.has(normalizeEdgeType(x)));
      if (hasDominates && isOrgType(tk)) {
        const rel = fragments.find((x) => x.id === relatedId);
        const relType = normalizeFragmentType(String((rel as any)?.data?.type || '').trim());
        if (relType && isMarketType(relType)) {
          return { kind: 'dominates', title: 'Dominates', visualType: 'ORGANIZATION' };
        }
      }
    }

    // Persons:
    // - If the person is connected via EMPLOYED_BY to the expanded org, bucket into a dedicated Staff stack.
    // - Otherwise keep the generic People stack.
    if (tk === 'PERSON') {
      const rel = String(relatedId || '').trim();
      const self = String(id || '').trim();
      if (rel && self) {
        const dk = `${self}||${rel}`; // person -> org (directed)
        const dkRev = `${rel}||${self}`; // org -> person (may happen if edge direction is noisy)
        const dset = directedEdgeTypes.get(dk) || null;
        const dsetRev = directedEdgeTypes.get(dkRev) || null;
        const hasEmployedBy = dset ? Array.from(dset).some((x) => normalizeEdgeType(x) === 'EMPLOYED_BY') : false;
        if (hasEmployedBy) return { kind: 'staff', title: 'Staff', visualType: 'PERSON' };
        const hasControlSignal =
          (dset ? Array.from(dset).some((x) => PERSON_CONTROL_EDGE_TYPES.has(normalizeEdgeType(x))) : false) ||
          (dsetRev ? Array.from(dsetRev).some((x) => PERSON_CONTROL_EDGE_TYPES.has(normalizeEdgeType(x))) : false) ||
          (pairTypes ? Array.from(pairTypes).some((x) => PERSON_CONTROL_EDGE_TYPES.has(normalizeEdgeType(x))) : false);
        if (hasControlSignal) return { kind: 'control', title: 'Stakeholders', visualType: 'ORGANIZATION' };
      }
      return { kind: 'persons', title: 'People', visualType: 'PERSON' };
    }
    // Geolocation: geo-like nodes
    if (GEO_TYPES.has(tk) || tk.includes('GEO')) return { kind: 'geolocation', title: 'Locations', visualType: 'GEO_REGION' };
    // Products: only for "incoming products" (these are the products that end up on the right pane)
    if (tk === 'PRODUCT' && String(d?.directionFromParent || '').trim() === 'incoming') return { kind: 'products', title: 'Products', visualType: 'PRODUCT' };
    // Supply: immediate suppliers/producers feeding INTO the expanded node (incoming edges to central).
    // Include facilities (plants/mines) as well as organizations.
    if (tk === 'FACILITY') {
      const et = normalizeEdgeType(String(d?.edgeTypeFromParent || '').trim());
      const dir = String(d?.directionFromParent || '').trim();
      if ((pairTypes && Array.from(pairTypes).some((x) => SUPPLY_EDGE_TYPES.has(normalizeEdgeType(x)))) || (SUPPLY_EDGE_TYPES.has(et) && dir === 'incoming')) {
        return { kind: 'suppliers', title: 'Suppliers', visualType: 'SUPPLIER' };
      }
    }
    // Control: organizations that are owners/investors/controllers (ignore weak reverse relations)
    if (tk === 'ORGANIZATION') {
      const et = normalizeEdgeType(String(d?.edgeTypeFromParent || '').trim());
      const dir = String(d?.directionFromParent || '').trim();
      // If the pair has any control relation, it always dominates Supply for semantic stacking.
      if (
        CONTROL_EDGE_TYPES.has(et) ||
        (pairTypes && Array.from(pairTypes).some((x) => CONTROL_EDGE_TYPES.has(normalizeEdgeType(x))))
      ) {
        return { kind: 'control', title: 'Stakeholders', visualType: 'ORGANIZATION' };
      }
      // Suppliers: organizations that supply to us (SUPPLIES_TO incoming = they supply to us)
      if (
        (pairTypes && Array.from(pairTypes).some((x) => SUPPLY_EDGE_TYPES.has(normalizeEdgeType(x)))) ||
        (SUPPLY_EDGE_TYPES.has(et) && dir === 'incoming')
      ) {
        return { kind: 'suppliers', title: 'Suppliers', visualType: 'SUPPLIER' };
      }
      // If we know the related counterpart in the right-hierarchy, prefer the best hierarchy edge between them.
      if (relatedId) {
        const a = String(id || '').trim();
        const b = String(relatedId || '').trim();
        if (a && b) {
          const key = a < b ? `${a}||${b}` : `${b}||${a}`;
          const pc = bestPcByPair.get(key);
          const et2 = normalizeEdgeType(String((pc as any)?.et || '').trim());
          if (CONTROL_EDGE_TYPES.has(et2)) return { kind: 'control', title: 'Stakeholders', visualType: 'ORGANIZATION' };
        }
      }
    }
    return null;
  };

  const stackSubjectLabel = (id: string): string => {
    if (!id) return '';
    const node = fragments.find((x) => x.id === id);
    const d: any = node?.data;
    const label = String(d?.label || '').trim();
    if (label) return label;
    const cp = String(d?.canonicalPath || '').trim();
    if (cp) {
      const last = cp.split('/').filter(Boolean).slice(-1)[0] || '';
      return last ? last.replace(/[-_]/g, ' ') : '';
    }
    return '';
  };

  const isStackEligibleRightNode = (id: string) => {
    if (!id) return false;
    if (!rightIds.has(id)) return false;
    // Must not have a left-group (no downstream block).
    const leftKids = downstreamByRight.get(id);
    if (leftKids && leftKids.length > 0) return false;
    // Prefer stable nodes (not explicitly expanded).
    const node = fragments.find((x) => x.id === id);
    const d: any = node?.data;
    // Scope-pinned nodes must stay individually visible (never collapse into stacks).
    if (d?.pinnedToScope) return false;
    if (d?.expanded) return false;
    return true;
  };

  const isStackEligibleRightNodeRelaxed = (id: string, allowPinned: boolean) => {
    if (!id) return false;
    if (!rightIds.has(id)) return false;
    const leftKids = downstreamByRight.get(id);
    if (leftKids && leftKids.length > 0) return false;
    const node = fragments.find((x) => x.id === id);
    const d: any = node?.data;
    if (!allowPinned && d?.pinnedToScope) return false;
    if (d?.expanded) return false;
    return true;
  };

  const parseStackKey = (key: string): { mode: 'rightstack' | 'rightstackp'; pivotId: string; level: number; kind: string } | null => {
    const parts = String(key || '').split(':');
    if (parts.length < 5) return null;
    if (parts[0] !== 'levels') return null;
    const mode = parts[1] === 'rightstack' ? 'rightstack' : (parts[1] === 'rightstackp' ? 'rightstackp' : null);
    if (!mode) return null;
    const pivotId = String(parts[2] || '').trim();
    const level = parseInt(String(parts[3] || '0'), 10);
    const kind = String(parts.slice(4).join(':') || '').trim();
    if (!pivotId || Number.isNaN(level) || !kind) return null;
    return { mode, pivotId, level, kind };
  };

  const buildExpandedMembersForKey = (key: string, ordered: string[], metaKind: string): string[] => {
    const parsed = parseStackKey(key);
    if (!parsed) return ordered;
    const { mode, pivotId, level, kind } = parsed;
    if (kind !== metaKind) return ordered;
    const baseList = mode === 'rightstack'
      ? Array.from(childrenOf.get(pivotId) || [])
      : Array.from(parentsOf.get(pivotId) || []);
    const candidateIds = baseList.filter((id) => rightIds.has(id) && (hLevel.get(id) || 0) === level);
    const out = new Set<string>();
    for (const id of ordered) out.add(id);
    for (const id of candidateIds) {
      const mk = stackKindOfRightNode(id, pivotId);
      if (!mk || mk.kind !== metaKind) continue;
      if (mode === 'rightstack') {
        const rightKids = childrenOf.get(id);
        if (rightKids && rightKids.size > 0) continue;
      }
      if (!isStackEligibleRightNodeRelaxed(id, true)) continue;
      out.add(id);
    }
    const list = Array.from(out);
    return list.sort((a, b) => {
      const pa = rightPos.get(a);
      const pb = rightPos.get(b);
      const ya = (pa?.y ?? 0) + nodeHeight / 2;
      const yb = (pb?.y ?? 0) + nodeHeight / 2;
      return ya - yb;
    });
  };

  // Build candidate groups by (parentId, childLevel)
  for (const [parentId, kidsSet] of childrenOf.entries()) {
    const kids = Array.from(kidsSet || []).filter((id) => rightIds.has(id));
    if (kids.length === 0) continue;
    const buckets = new Map<number, string[]>();
    for (const kid of kids) {
      const lv = hLevel.get(kid) || 0;
      const arr = buckets.get(lv) || [];
      arr.push(kid);
      buckets.set(lv, arr);
    }
    for (const [lv, list] of buckets.entries()) {
      // Partition by semantic stack kind so we get stable named stacks (Persons/Products/Control/Geolocation).
      const byKind = new Map<string, { meta: { kind: string; title: string; visualType: string }; ids: string[] }>();
      for (const kid of list) {
        const meta = stackKindOfRightNode(kid, parentId);
        if (!meta) continue;
        const hit = byKind.get(meta.kind) || { meta, ids: [] as string[] };
        hit.ids.push(kid);
        byKind.set(meta.kind, hit);
      }
      for (const { meta, ids } of byKind.values()) {
        const eligible = ids.filter((kid) => {
          if (!isStackEligibleRightNode(kid)) return false;
          // Must be a leaf in the right-hierarchy (no right-children). This is for stacking "child columns".
          const rightKids = childrenOf.get(kid);
          if (rightKids && rightKids.size > 0) return false;
          return true;
        });
        if (eligible.length < STACK_MIN) continue;

        const key = `levels:rightstack:${parentId}:${lv}:${meta.kind}`;
        stackMembersByKey.set(key, eligible);
        const expanded = expandedStackKeys.has(key);

        // Choose a controller node (top-most by Y) to carry the stack control.
        const ordered = [...eligible].sort((a, b) => {
          const pa = rightPos.get(a);
          const pb = rightPos.get(b);
          const ya = (pa?.y ?? 0) + nodeHeight / 2;
          const yb = (pb?.y ?? 0) + nodeHeight / 2;
          return ya - yb;
        });
        const controllerId = ordered[0]!;

        stackMetaById.set(controllerId, {
          key,
          count: eligible.length,
          expanded,
          title: meta.title,
          subtitle: stackSubjectLabel(parentId),
          kind: meta.kind,
          visualType: meta.visualType,
        });
        // Record membership for all members so the UI can proxy edges to the controller.
        for (const id of ordered) {
          if (!stackKeyByMemberId.has(id)) stackKeyByMemberId.set(id, key);
          if (!stackControllerByMemberId.has(id)) stackControllerByMemberId.set(id, controllerId);
        }
        if (expanded) {
          const expandedMembers = buildExpandedMembersForKey(key, ordered, meta.kind);
          expandedStackMembersByKey.set(key, expandedMembers);
          for (const id of expandedMembers) expandedStackMemberIds.add(id);
          const count = expandedMembers.length;
          const groupH = (count * nodeHeight) + Math.max(0, count - 1) * stackGroupGapY + STACK_GROUP_PAD_TOP + STACK_GROUP_PAD_BOTTOM;
          stackExpandedHeights.set(controllerId, groupH);
        } else {
          for (const id of ordered) {
            if (id === controllerId) continue;
            stackHiddenIds.add(id);
          }
        }
      }
    }
  }

  // Also support stacking "parent columns" (many parents for a node).
  for (const [childId, parsSet] of parentsOf.entries()) {
    if (!rightIds.has(childId)) continue;
    const pars = Array.from(parsSet || []).filter((id) => rightIds.has(id));
    if (pars.length < 2) continue;
    const buckets = new Map<number, string[]>();
    for (const pid of pars) {
      const lv = hLevel.get(pid) || 0;
      const arr = buckets.get(lv) || [];
      arr.push(pid);
      buckets.set(lv, arr);
    }
    for (const [lv, list] of buckets.entries()) {
      const byKind = new Map<string, { meta: { kind: string; title: string; visualType: string }; ids: string[] }>();
      for (const pid of list) {
        const meta = stackKindOfRightNode(pid, childId);
        if (!meta) continue;
        const hit = byKind.get(meta.kind) || { meta, ids: [] as string[] };
        hit.ids.push(pid);
        byKind.set(meta.kind, hit);
      }
      for (const { meta, ids } of byKind.values()) {
        const eligible = ids.filter((pid) => {
          if (!isStackEligibleRightNode(pid)) return false;
          return true;
        });
        if (eligible.length < STACK_MIN) continue;
        const key = `levels:rightstackp:${childId}:${lv}:${meta.kind}`;
        stackMembersByKey.set(key, eligible);
        const expanded = expandedStackKeys.has(key);

        const ordered = [...eligible].sort((a, b) => {
          const pa = rightPos.get(a);
          const pb = rightPos.get(b);
          const ya = (pa?.y ?? 0) + nodeHeight / 2;
          const yb = (pb?.y ?? 0) + nodeHeight / 2;
          return ya - yb;
        });
        const controllerId = ordered[0]!;
        // If this node is already a controller for another stack, keep the first assignment (stable).
        if (!stackMetaById.has(controllerId)) {
          stackMetaById.set(controllerId, {
            key,
            count: eligible.length,
            expanded,
            title: meta.title,
            subtitle: stackSubjectLabel(childId),
            kind: meta.kind,
            visualType: meta.visualType,
          });
        }
        // Record membership for all members so the UI can proxy edges to the controller.
        for (const id of ordered) {
          if (!stackKeyByMemberId.has(id)) stackKeyByMemberId.set(id, key);
          if (!stackControllerByMemberId.has(id)) stackControllerByMemberId.set(id, controllerId);
        }
        if (expanded) {
          const expandedMembers = buildExpandedMembersForKey(key, ordered, meta.kind);
          expandedStackMembersByKey.set(key, expandedMembers);
          for (const id of expandedMembers) expandedStackMemberIds.add(id);
          const count = expandedMembers.length;
          const groupH = (count * nodeHeight) + Math.max(0, count - 1) * stackGroupGapY + STACK_GROUP_PAD_TOP + STACK_GROUP_PAD_BOTTOM;
          stackExpandedHeights.set(controllerId, groupH);
        } else {
          for (const id of ordered) {
            if (id === controllerId) continue;
            // Do not hide if it is already a controller for some stack (keeps a single visible toggle per stack).
            if (stackMetaById.has(id)) continue;
            stackHiddenIds.add(id);
          }
        }
      }
    }
  }

  // Snapshot positions before repack so we can preserve vertical centering.
  const rightPosBeforeRepack = new Map(rightPos);

  // Re-pack levels that contain hidden stack members so they don't reserve empty vertical space.
  for (const [lv, ids] of idsByLevel.entries()) {
    const hasHidden = ids.some((id) => stackHiddenIds.has(id));
    const hasExpanded = ids.some((id) => stackExpandedHeights.has(id));
    if (!hasHidden && !hasExpanded) continue;
    const visible = ids.filter((id) =>
      !stackHiddenIds.has(id) &&
      !preHiddenIds.has(id) &&
      (!expandedStackMemberIds.has(id) || stackExpandedHeights.has(id)),
    );
    if (visible.length <= 1) continue;
    const gapY = rightGapY(lv);
    const sortByY = (list: string[]) =>
      [...list].sort((a, b) => {
        const pa = rightPos.get(a);
        const pb = rightPos.get(b);
        const ya = (pa?.y ?? 0) + nodeHeight / 2;
        const yb = (pb?.y ?? 0) + nodeHeight / 2;
        return ya - yb;
      });

    const isCollapsedStackController = (id: string) => {
      const meta = stackMetaById.get(id);
      return Boolean(meta && !meta.expanded && (meta.count || 0) > 1);
    };

    // UX: when a column mixes stacks and non-stacks, place non-stack nodes first,
    // then collapsed stack controllers. (If a stack is expanded, its members are visible and behave like normal nodes.)
    const nonStack = visible.filter((id) => !isCollapsedStackController(id));
    const stacks = visible.filter((id) => isCollapsedStackController(id));
    const ordered = [...sortByY(nonStack), ...sortByY(stacks)];
    // Preserve vertical centers as much as possible while preventing overlaps.
    const desiredTops = ordered.map((id) => {
      const base = rightPosBeforeRepack.get(id) || rightPos.get(id);
      const h = stackExpandedHeights.get(id) || nodeHeight;
      const center = (base?.y ?? 0) + nodeHeight / 2;
      return center - h / 2;
    });
    let cursor = desiredTops.length > 0 ? Math.min(...desiredTops) : 0;
    for (const id of ordered) {
      const base = rightPosBeforeRepack.get(id) || rightPos.get(id);
      const h = stackExpandedHeights.get(id) || nodeHeight;
      const center = (base?.y ?? 0) + nodeHeight / 2;
      const desiredTop = center - h / 2;
      const top = Math.max(desiredTop, cursor);
      rightPos.set(id, { x: rightPos.get(id)?.x ?? (RIGHT_BASE_X + lv * RIGHT_LEVEL_STEP_X), y: top });
      cursor = top + h + gapY;
    }
  }

  // === 5.85) Center right column relative to pivot (level 0) node ===
  // After repack, recalculate the bounding box of the right column and center it
  // relative to the pivot node's vertical center.
  const level0Ids = idsByLevel.get(0) || [];
  if (level0Ids.length > 0 && rightSorted.length > level0Ids.length) {
    // Find the pivot node (level 0) center
    const pivotId = level0Ids[0]!;
    const pivotPos = rightPos.get(pivotId);
    const pivotCenterY = pivotPos ? pivotPos.y + nodeHeight / 2 : 0;

    // Calculate current bounding box of all non-level-0 right nodes
    let minY = Infinity;
    let maxY = -Infinity;
    for (const rn of rightSorted) {
      const lv = hLevel.get(rn.id) || 0;
      if (lv === 0) continue; // Skip pivot itself
      const p = rightPos.get(rn.id);
      if (!p) continue;
      const h = stackExpandedHeights.get(rn.id) || (unitHeights.get(rn.id) || nodeHeight);
      minY = Math.min(minY, p.y);
      maxY = Math.max(maxY, p.y + h);
    }

    if (minY < Infinity && maxY > -Infinity) {
      const currentCenterY = (minY + maxY) / 2;
      const shiftY = pivotCenterY - currentCenterY;

      // Apply shift to all non-level-0 nodes
      for (const rn of rightSorted) {
        const lv = hLevel.get(rn.id) || 0;
        if (lv === 0) continue;
        const p = rightPos.get(rn.id);
        if (!p) continue;
        rightPos.set(rn.id, { x: p.x, y: p.y + shiftY });
      }
    }
  }

  // === 6) Build group containers (Downstream only) + attach children ===
  const groupNodes: Node<T>[] = [];
  const childNodes: Node<T>[] = [];
  const nodeById = new Map<string, Node<T>>();
  for (const n of fragments) nodeById.set(String(n.id || '').trim(), n);

  const attachChildrenWrap = (
    blocks: TypeBlock<T>[],
    groupId: string,
    rows: number,
    groupParentId: string,
    outerPadX: number,
    groupH: number,
  ) => {
    const pNode = nodeById.get(String(groupParentId || '').trim());
    const pData: any = (pNode as any)?.data || {};
    const parentLabel =
      String(pData?.label || '').trim() ||
      String(pData?.canonicalPath || '').trim() ||
      String(groupParentId || '').trim();

    // IMPORTANT (nested containers):
    // Root `levels:left:*` groups carry extra symmetric padding (PAD_X + outerPadX) so aggregated edge ports
    // have breathing room and the group doesn't "stick" to the right band.
    // If we draw the outline on the ROOT, that padding becomes part of the first/last "column segment",
    // making крайние визуально шире when every type has exactly 1 column.
    //
    // Fix: keep ROOT borderless (ports still attach to it), and draw the outline on an INNER frame
    // that wraps exactly the content columns (no side padding).
    const frameId = `levels:leftframe:${groupId}`;
    // Give a little visual "air" inside the outline without affecting deterministic column math.
    const FRAME_PAD_X = 14;
    const contentW = (() => {
      let maxX = 0;
      for (const b of (blocks || []) as any[]) {
        const sx = Math.max(0, Math.floor((b as any)?.startX || 0));
        const bw = Math.max(0, Math.floor((b as any)?.blockW || 0));
        maxX = Math.max(maxX, sx + bw);
      }
      return Math.max(1, Math.round(maxX));
    })();
    groupNodes.push({
      id: frameId,
      type: 'horizonGroup' as any,
      position: { x: Math.round(PAD_X + outerPadX - FRAME_PAD_X), y: 0 },
      parentId: groupId,
      extent: 'parent' as any,
      selectable: false,
      draggable: false,
      connectable: false,
      focusable: false,
      deletable: false,
      style: { width: Math.max(1, Math.round(contentW + FRAME_PAD_X * 2)), height: Math.max(1, Math.round(groupH)), zIndex: 0 } as any,
      data: {
        isGroup: true,
        groupIsBackdrop: true,
        groupInteractive: false,
        groupHideHeader: true,
        groupNoFill: true,
        groupBorderMode: 'none',
        groupParentId: groupParentId,
      } as any,
    } as any);

    for (let bIdx = 0; bIdx < blocks.length; bIdx++) {
      const b = blocks[bIdx]!;
      const safeRows = Math.max(1, Math.floor((b as any)?.rows || rows || 1));
      const minBase = compactLeftPane ? COMPACT_NODE_LAYOUT_HEIGHT : nodeHeight;
      // Compute per-row max height for this type block (supports mixed compact/full nodes).
      const rowHeights = new Array(safeRows).fill(minBase);
      for (const rb of (b.relBlocks || [])) {
        for (let idx = 0; idx < (rb.nodes || []).length; idx++) {
          const childN = rb.nodes[idx]!;
          const rowIdx = idx % Math.max(1, safeRows);
          const nd: any = (childN as any)?.data || {};
          rowHeights[rowIdx] = Math.max(rowHeights[rowIdx], leftPaneLayoutHeightFor(nd));
        }
      }
      const rowOffsets = new Array(safeRows).fill(0);
      for (let r = 1; r < safeRows; r++) {
        rowOffsets[r] = rowOffsets[r - 1] + rowHeights[r - 1] + groupChildGapY;
      }

      // Create a real container for this type section (Approach #1).
      // Children will be nested under this container, so ReactFlow owns the geometry and dividers stay crisp.
      const typeKey = String((b as any)?.typeKey || '').trim() || 'UNKNOWN';
      const typeLabel = getTypeGroupLabel(typeKey);
      const typeGroupId = `levels:lefttype:${groupId}:${typeKey}`;
      const typeGroupX = Math.round(FRAME_PAD_X + Math.max(0, Math.floor((b as any)?.startX || 0)));
      // Container width is derived from column counts (stored in blockW).
      const typeGroupW = Math.max(1, Math.round(Math.floor((b as any)?.blockW || 0) || 1));
      groupNodes.push({
        id: typeGroupId,
        type: 'horizonGroup' as any,
        position: { x: typeGroupX, y: 0 },
        parentId: frameId,
        extent: 'parent' as any,
        selectable: false,
        draggable: false,
        connectable: false,
        focusable: false,
        deletable: false,
        style: { width: typeGroupW, height: Math.max(1, Math.round(groupH)), zIndex: 0 } as any,
        data: {
          isGroup: true,
          groupIsBackdrop: true,
          groupInteractive: false,
          groupHideHeader: false,
          groupNoFill: true,
          groupTitle: typeLabel,
          // Vertical dividers between type columns:
          groupBorderMode: bIdx < blocks.length - 1 ? 'divider-right' : 'none',
          // For future: useful for debugging and potential “scroll to section”
          groupParentId: groupParentId,
        } as any,
      } as any);

      const relBlocks = (b.relBlocks || []) as any[];
      for (let rbIdx = 0; rbIdx < relBlocks.length; rbIdx++) {
        const rb: any = relBlocks[rbIdx]!;
        const edgeTypeKey = String((rb as any)?.edgeTypeKey || '').trim();
        const relKeyNorm = normalizeEdgeType(edgeTypeKey);
        const isCompetesSubcategory = compactLeftPane && relKeyNorm === 'COMPETES_WITH';
        // IMPORTANT: competitors are rendered as compact in Levels-left, but nodeSize is applied below,
        // so we must use compact offsets unconditionally here to avoid full-size row spacing.
        const competesUseCompactOffsets = isCompetesSubcategory;
        const relTitle = String((rb as any)?.label || edgeTypeKey || '').trim();
        const nodeCount = typeof (rb as any)?.nodeCount === 'number' ? (rb as any).nodeCount : (rb?.nodes?.length || 0);
        const firstNodeId = String((rb as any)?.firstNodeId || '').trim();
        const collapsed = Boolean((rb as any)?.collapsed);

        // Create a real container for this relation subcategory within the type section.
        const rbStart = Math.round(Math.max(0, Math.floor((rb as any)?.startX || 0)));
        const rbW = Math.max(1, Math.round(Math.floor((rb as any)?.blockW || 0) || 1));
        const relGroupId = `levels:leftrel:${typeGroupId}:${relKeyNorm || 'OTHER'}:${rbIdx}`;
        groupNodes.push({
          id: relGroupId,
          type: 'horizonGroup' as any,
          position: { x: rbStart, y: 0 },
          parentId: typeGroupId,
          extent: 'parent' as any,
          selectable: false,
          draggable: false,
          connectable: false,
          focusable: false,
          deletable: false,
          style: { width: rbW, height: Math.max(1, Math.round(groupH)), zIndex: 0 } as any,
          data: {
            isGroup: true,
            groupIsBackdrop: false,
            groupInteractive: false,
            groupHideHeader: true,
            groupNoFill: true,
            // Only draw vertical dividers between relation subcategories.
            groupBorderMode: rbIdx < relBlocks.length - 1 ? 'divider-right' : 'none',
            // Footer label + collapse control (rendered by HorizonGroupNode).
            groupFooterTitle: relTitle,
            groupFooterEdgeTypeKey: relKeyNorm,
            groupFooterNodeCount: nodeCount,
            groupFooterCollapsed: collapsed,
            groupFooterFirstNodeId: firstNodeId,
            onToggleSubcategoryCollapse,
            groupId: groupId, // root Levels-left group
          } as any,
        } as any);

        for (let idx = 0; idx < rb.nodes.length; idx++) {
          const child = rb.nodes[idx]!;
          // Column-major within each relation sub-block: first fill vertical, then move right.
          // For COMPETES: compute its own compact row offsets so it doesn't inherit tall row heights
          // from other subcategories (e.g. AUDIT) within the same type block.
          const rbRows = isCompetesSubcategory
            ? Math.max(1, Math.floor((rb as any)?.rows || Math.min(6, rb.nodes.length) || 1))
            : safeRows;
          const rowIdx = idx % rbRows;
          const colIdx = Math.floor(idx / rbRows);
          // Keep some "air" from the left divider inside each relation container.
          const cx = Math.round(LEFT_REL_CONTAINER_PAD_X + colIdx * (nodeWidth + groupChildGapX));
          const cy = (() => {
            if (!competesUseCompactOffsets) return PAD_TOP + (rowOffsets[rowIdx] || 0);
            // Compact rows: each row is COMPACT_NODE_LAYOUT_HEIGHT + groupChildGapY.
            const rbRowOffsets = new Array(rbRows).fill(0);
            for (let r = 1; r < rbRows; r++) {
              rbRowOffsets[r] = rbRowOffsets[r - 1] + COMPACT_NODE_LAYOUT_HEIGHT + groupChildGapY;
            }
            return PAD_TOP + (rbRowOffsets[rowIdx] || 0);
          })();
          const isCollapsed = Boolean((rb as any)?.collapsed);
          const controllerId = String((rb as any)?.controllerId || '').trim();
          const stackKey = String((rb as any)?.stackKey || '').trim();
          const nodeCount = typeof (rb as any)?.nodeCount === 'number' ? (rb as any).nodeCount : (rb.nodes?.length || 0);
          const isController = Boolean(isCollapsed && controllerId && String(child.id || '').trim() === controllerId);

          const d0: any = (child as any)?.data || {};
          const d: any = { ...d0 };
          // Clear stale stack fields; collapse state will be re-applied deterministically.
          delete d.hiddenByStack;
          delete d.stackGroupKey;
          delete d.stackCount;
          delete d.stackExpanded;
          delete (d as any).stackControllerId;
          delete (d as any).subcategoryStack;
          delete d.onToggleStack;
          delete d.stackTitle;
          delete d.stackSubtitle;

          if (isController && isCollapsed && stackKey && edgeTypeKey && nodeCount > 1) {
            d.stackGroupKey = stackKey;
            d.stackCount = nodeCount;
            d.stackExpanded = false;
            (d as any).stackControllerId = controllerId;
            (d as any).subcategoryEdgeType = edgeTypeKey;
            // UX copy (category-first): title = relation label, subtitle = parent object.
            // Keep it scannable and consistent with other stacks.
            d.stackTitle = String((rb as any)?.label || edgeTypeKey || 'Related').trim();
            d.stackSubtitle = parentLabel;
            (d as any).subcategoryStack = true;
            if (typeof onToggleSubcategoryCollapse === 'function') {
              d.onToggleStack = (_k: string, nextExpanded: boolean) => {
                // Focus rule:
                // - Expanding: focus the first node of the subcategory (this controller id).
                // - Collapsing: focus the stack controller node (same id after relayout).
                onToggleSubcategoryCollapse(groupId, edgeTypeKey, !nextExpanded, controllerId);
              };
            }
          }
          
          // Apply compact node size for left pane when enabled.
          if (isCompetesSubcategory || isLeftPaneCompactNode(d)) {
            d.nodeSize = 'compact';
          }

          childNodes.push({
            ...(child as any),
            parentId: relGroupId,
            extent: 'parent' as any,
            zIndex: 1,
            // Preserve manual hidden state; derived stack hiding is applied below.
            hidden: preHiddenIds.has(child.id) || Boolean((child as any).hidden),
            data: d,
            position: { x: cx, y: cy },
          } as any);

          // If collapsed: add hidden stack members so LensCanvasCockpit can generate stackproxy edges.
          if (isController && isCollapsed && Array.isArray((rb as any)?.hiddenMemberIds) && (rb as any)?.hiddenMemberIds?.length) {
            const hiddenIds = ((rb as any).hiddenMemberIds as any[])
              .map((x) => String(x || '').trim())
              .filter(Boolean);
            for (const hid of hiddenIds) {
              const member = nodeById.get(hid);
              if (!member) continue;
              const md0: any = (member as any)?.data || {};
              const md: any = { ...md0 };
              md.stackGroupKey = stackKey;
              md.stackCount = nodeCount;
              md.stackExpanded = false;
              md.hiddenByStack = true;
              (md as any).stackControllerId = controllerId;
              // Apply compact node size for left pane when enabled.
              if (isCompetesSubcategory || isLeftPaneCompactNode(md)) {
                md.nodeSize = 'compact';
              }
              childNodes.push({
                ...(member as any),
                parentId: relGroupId,
                extent: 'parent' as any,
                zIndex: 0,
                hidden: true,
                data: md,
                position: { x: cx, y: cy },
              } as any);
            }
          }
        }
      }
    }
  };

  // Build left-band groups first, then pack vertically to avoid overlaps.
  type GroupInfo = {
    parentId: string;
    groupId: string;
    w: number;
    h: number;
    cols: number;
    rows: number;
    gx: number;
    desiredGy: number;
    gy: number;
    children: Node<T>[];
    blocks: TypeBlock<T>[];
    interBlockGapX: number;
    relLabelY: number;
    outerPadX: number;
    edgeTypes: string[];
  };

  const groupInfos: GroupInfo[] = [];
  const leftBandRightX = RIGHT_BASE_X - GROUP_GAP_X;

  for (const rn of rightSorted) {
    const allChildren = downstreamByRight.get(rn.id) || [];
    // Build groups from children excluding MANUAL hidden.
    // Derived hiddenByStack should still participate (ports/edgeTypes), but must not reserve layout space
    // (handled by groupDimsWrap + attachChildrenWrap which lay out only the controller when collapsed).
    const children = allChildren.filter((c) => {
      if (preHiddenIds.has(c.id)) return false;
      const d: any = (c as any)?.data || {};
      const isDerivedStackHidden = Boolean(d.hiddenByStack);
      if (Boolean((c as any).hidden) && !isDerivedStackHidden) return false;
      return true;
    });
    if (children.length === 0) continue;
    const rp = rightPos.get(rn.id)!;
    const groupId = `levels:left:${rn.id}:down`;
    const dim = groupDimsWrap(children, rn.id, groupId);
    const gx = leftBandRightX - dim.w;

    const nodeCenterY = rp.y + nodeHeight / 2;
    const desiredGy = nodeCenterY - dim.h / 2;

    // Edge types for this group: derive from REAL graph edges between (parent rn) and its children.
    // This enables showing ALL relation types (OWNS + COMPETES + ...), not only one metadata edgeTypeFromParent.
    const childIdSet = new Set(children.map((n) => String(n.id || '').trim()).filter(Boolean));
    const edgeTypeSet = new Set<string>();
    for (const e of (edges || []) as any[]) {
      const eid = String(e?.id || '').trim();
      if (eid.startsWith('levels:agg:')) continue;
      if (Boolean(e?.data?.levelsAgg)) continue;
      if (Boolean(e?.data?.levelsStackProxy)) continue;
      if (eid.startsWith('levels:stackproxy:')) continue;
      if (Boolean(e?.data?.levelsGroupProxy)) continue;
      if (eid.startsWith('levels:gproxy:')) continue;
      const s = normId(e?.source);
      const t = normId(e?.target);
      if (!s || !t) continue;
      if (s === rn.id && childIdSet.has(t)) edgeTypeSet.add(edgeTypeOf(e));
      else if (t === rn.id && childIdSet.has(s)) edgeTypeSet.add(edgeTypeOf(e));
      // Also include edge types for edges that touch group children but connect OUTSIDE the group.
      // These edges will be proxied to group handlers in Levels mode (so the group needs ports for them).
      else if (childIdSet.has(s) && !childIdSet.has(t)) edgeTypeSet.add(edgeTypeOf(e));
      else if (childIdSet.has(t) && !childIdSet.has(s)) edgeTypeSet.add(edgeTypeOf(e));
    }
    // Fallback: if no edges found (rare), use metadata.
    if (edgeTypeSet.size === 0) {
      for (const c of children) {
        const et = normalizeEdgeType((c.data as any)?.edgeTypeFromParent);
        if (et) edgeTypeSet.add(et);
      }
    }
    const edgeTypes = Array.from(edgeTypeSet).sort((a, b) => {
      const ap = getEdgePriority(a);
      const bp = getEdgePriority(b);
      if (ap !== bp) return ap - bp;
      return a.localeCompare(b);
    });

    groupInfos.push({
      parentId: rn.id,
      groupId,
      w: dim.w,
      h: dim.h,
      cols: dim.cols,
      rows: dim.rows,
      gx,
      desiredGy,
      gy: desiredGy,
      children,
      blocks: dim.blocks,
      interBlockGapX: dim.interBlockGapX,
      relLabelY: dim.relLabelY,
      outerPadX: Math.max(0, Math.floor((dim as any)?.outerPadX || 0)),
      edgeTypes,
    });
  }

  // Pack groups in the left band to prevent overlaps.
  // Desired behavior: if two groups overlap, the lower one gets a slot below the upper one.
  const GROUP_STACK_GAP_Y = Math.max(16, Math.floor(groupChildGapY * 0.4)); // compact but readable

  // === Barycentric pull for groups: move each group towards the center-of-mass of its backbone neighbors ===
  // This prevents long edges when an expanded node (e.g. Lithography Scanner) has many consumers above it.
  {
    // Build adjacency: parentId -> Set<backbone neighbor ids> (via real edges, not proxy).
    const backboneNeighbors = new Map<string, Set<string>>();
    const rightIdSet = new Set(rightSorted.map((n) => n.id));
    for (const e of (edges || []) as any[]) {
      const eid = String(e?.id || '').trim();
      if (eid.startsWith('levels:agg:')) continue;
      if (eid.startsWith('levels:stackproxy:')) continue;
      if (eid.startsWith('levels:gproxy:')) continue;
      if (Boolean(e?.data?.levelsAgg)) continue;
      if (Boolean(e?.data?.levelsStackProxy)) continue;
      if (Boolean(e?.data?.levelsGroupProxy)) continue;
      const s = normId(e?.source);
      const t = normId(e?.target);
      if (!s || !t || s === t) continue;
      // Both endpoints must be in backbone.
      if (!rightIdSet.has(s) || !rightIdSet.has(t)) continue;
      const setS = backboneNeighbors.get(s) || new Set<string>();
      setS.add(t);
      backboneNeighbors.set(s, setS);
      const setT = backboneNeighbors.get(t) || new Set<string>();
      setT.add(s);
      backboneNeighbors.set(t, setT);
    }

    // For each group, compute pull = average Y of backbone neighbors of parentId.
    // Blend desiredGy towards pull (weight 0.5) to avoid drastic jumps.
    const PULL_WEIGHT = 0.5;
    for (const g of groupInfos) {
      const neighbors = backboneNeighbors.get(g.parentId);
      if (!neighbors || neighbors.size === 0) continue;
      let sumY = 0;
      let count = 0;
      for (const nid of neighbors) {
        const p = rightPos.get(nid);
        if (p) {
          sumY += p.y + nodeHeight / 2;
          count++;
        }
      }
      if (count === 0) continue;
      const pullCenterY = sumY / count;
      const pullGroupY = pullCenterY - g.h / 2;
      // Blend: move desiredGy towards pullGroupY.
      g.desiredGy = g.desiredGy * (1 - PULL_WEIGHT) + pullGroupY * PULL_WEIGHT;
    }
  }

  groupInfos.sort((a, b) => a.desiredGy - b.desiredGy);
  let prevBottom = -Infinity;
  const anchoredRightIds = new Set<string>(); // right nodes that must stay aligned with their (packed) group
  for (const g of groupInfos) {
    const top = g.desiredGy;
    const minTop = isFinite(prevBottom) ? prevBottom + GROUP_STACK_GAP_Y : -Infinity;
    // Snap groups to integer pixels to avoid blurry dashed borders / drifting dividers on zoom.
    g.gy = Math.round(Math.max(top, minTop));
    prevBottom = g.gy + g.h;
    // Keep the parent node strictly aligned to the group's vertical center.
    const newNodeY = Math.round((g.gy + g.h / 2) - nodeHeight / 2);
    const rp = rightPos.get(g.parentId);
    if (rp) rightPos.set(g.parentId, { x: rp.x, y: newNodeY });
    anchoredRightIds.add(g.parentId);
  }

  // After groups are packed (and their parent right-nodes were shifted),
  // re-balance the RIGHT hierarchy vertically so that:
  // - nodes expanded from groups don't "fly away"
  // - children stay close to their parents
  // while keeping anchored nodes fixed (those that have groups).
  //
  // We do a couple of barycentric sweeps:
  // - right-to-left: align a node to the average Y of its parents
  // - left-to-right: align a node to the average Y of its children
  // with collision resolution per level.
  {
    const centerY = (id: string) => {
      const p = rightPos.get(id);
      return (p ? p.y : 0) + nodeHeight / 2;
    };

    const yC = new Map<string, number>();
    for (const rn of rightSorted) yC.set(rn.id, centerY(rn.id));

    const resolveCollisions = (ids: string[], gapY: number) => {
      const visibleIds = ids.filter((id) => !stackHiddenIds.has(id) && !preHiddenIds.has(id));
      if (visibleIds.length <= 1) return;
      // Iterative 1D separation with anchors.
      const H = (id: string) => nodeHeight;
      for (let iter = 0; iter < 10; iter++) {
        const ordered = [...visibleIds].sort((a, b) => (yC.get(a)! - yC.get(b)!));
        let moved = false;
        for (let i = 0; i < ordered.length - 1; i++) {
          const a = ordered[i]!;
          const b = ordered[i + 1]!;
          const ay = yC.get(a)!;
          const by = yC.get(b)!;
          const minDist = H(a) / 2 + H(b) / 2 + gapY;
          const dist = by - ay;
          if (dist >= minDist) continue;
          const push = (minDist - dist);
          const aAnch = anchoredRightIds.has(a);
          const bAnch = anchoredRightIds.has(b);
          if (aAnch && bAnch) continue; // can't fix; should be rare
          if (aAnch) {
            yC.set(b, by + push);
            moved = true;
          } else if (bAnch) {
            yC.set(a, ay - push);
            moved = true;
          } else {
            yC.set(a, ay - push / 2);
            yC.set(b, by + push / 2);
            moved = true;
          }
        }
        // Re-pin anchors to their current (post-pack) positions
        for (const id of anchoredRightIds) yC.set(id, centerY(id));
        if (!moved) break;
      }
    };

    const idsForLevel = (lv: number) => idsByLevel.get(lv) || [];

    const blend = (cur: number, next: number, a: number) => cur * (1 - a) + next * a;
    const alpha = 0.65; // fairly strong pull towards target

    for (let sweep = 0; sweep < 2; sweep++) {
      // Right-to-left: follow parents (keeps newly promoted nodes close to their right-side ancestors).
      for (let lv = maxLevel - 1; lv >= 0; lv--) {
        const ids = idsForLevel(lv).filter((id) => !stackHiddenIds.has(id) && !preHiddenIds.has(id));
        if (ids.length === 0) continue;
        for (const id of ids) {
          if (anchoredRightIds.has(id)) continue;
          const pars = Array.from(parentsOf.get(id) || []).filter(
            (p) => typeof yC.get(p) === 'number' && !stackHiddenIds.has(p) && !preHiddenIds.has(p),
          );
          if (pars.length === 0) continue;
          const target = pars.reduce((s, p) => s + (yC.get(p) || 0), 0) / pars.length;
          yC.set(id, blend(yC.get(id) || 0, target, alpha));
        }
        resolveCollisions(ids, rightGapY(lv));
      }

      // Left-to-right: follow children (keeps parents centered around their subtrees).
      for (let lv = 1; lv <= maxLevel; lv++) {
        const ids = idsForLevel(lv).filter((id) => !stackHiddenIds.has(id) && !preHiddenIds.has(id));
        if (ids.length === 0) continue;
        for (const id of ids) {
          if (anchoredRightIds.has(id)) continue;
          const kids = Array.from(childrenOf.get(id) || []).filter(
            (c) => typeof yC.get(c) === 'number' && !stackHiddenIds.has(c) && !preHiddenIds.has(c),
          );
          if (kids.length === 0) continue;
          const target = kids.reduce((s, c) => s + (yC.get(c) || 0), 0) / kids.length;
          yC.set(id, blend(yC.get(id) || 0, target, alpha * 0.75));
        }
        resolveCollisions(ids, rightGapY(lv));
      }
    }

    // Commit y back into rightPos (x stays the same).
    for (const rn of rightSorted) {
      const p = rightPos.get(rn.id);
      if (!p) continue;
      const yc = yC.get(rn.id) ?? (p.y + nodeHeight / 2);
      rightPos.set(rn.id, { x: p.x, y: yc - nodeHeight / 2 });
    }

    // Keep group alignment strict after right-side relaxation:
    // group stays where it was packed, node stays anchored to it (already fixed),
    // non-anchored nodes moved, groups unaffected.
  }

  // Emit group nodes + children after packing.
  // IMPORTANT: group children must NOT also be returned as standalone nodes, otherwise ReactFlow
  // may apply the "last duplicate id wins" behavior and scramble the grid when multiple groups exist.
  const groupedChildIds = new Set<string>();
  for (const g of groupInfos) {
    for (const c of g.children) groupedChildIds.add(c.id);

    // Build ports for ALL edge types present in this group (one port per edgeType).
    // Ports are placed on the RIGHT edge (facing backbone), packed into the central 50% with 25% air top+bottom.
    const groupEdgePorts = (() => {
      const types = Array.isArray(g.edgeTypes) ? g.edgeTypes.filter(Boolean) : [];
      const count = types.length;
      if (count === 0) return [];
      const airTop = Math.floor(g.h * 0.25);
      const airBottom = Math.floor(g.h * 0.25);
      const yTop = Math.max(airTop, PAD_TOP + 18);
      const yBot = Math.min(g.h - Math.max(airBottom, PAD_BOTTOM_BASE + 18), Math.floor(g.h * 0.75));
      const yHi = Math.max(yTop, yBot);
      // Attach ports to central 50% of the edge.
      const midStart = yTop;
      const midEnd = yHi;
      return types.map((et, idx) => {
        const edgeType = normalizeEdgeType(et);
        const targetHandleId = `edge:${edgeType}:target`;
        const sourceHandleId = `edge:${edgeType}:source`;
        const yCenter =
          count <= 1
            ? Math.round((midStart + midEnd) / 2)
            : Math.round(midStart + (idx * (midEnd - midStart)) / Math.max(1, count - 1));
        const y = Math.max(0, Math.min(g.h - 10, yCenter - 5)); // handle is 10px tall
        return { edgeType, targetHandleId, sourceHandleId, y };
      });
    })();

    groupNodes.push({
      id: g.groupId,
      type: 'horizonGroup' as any,
      // Snap to integer pixels to keep dashed outlines/dividers crisp.
      position: { x: Math.round(g.gx), y: Math.round(g.gy) },
      selectable: false,
      draggable: false,
      connectable: false,
      focusable: false,
      deletable: false,
      style: { width: g.w, height: g.h, zIndex: 0 } as any,
      data: {
        isGroup: true,
        groupIsBackdrop: false,
        groupInteractive: false,
        groupHideHeader: true,
        groupNoFill: true,
        // Levels root group: no outline; use a subtle background tint for separation.
        groupBorderMode: 'none',
        groupBgTint: 'subtle',
        // Callback to toggle subcategory collapse state.
        onToggleSubcategoryCollapse,
        groupId: g.groupId,
        // Ports for aggregated edges (one per edge type).
        groupEdgePorts,
        groupParentId: g.parentId,
        groupSemanticGroup: 'customers',
        groupEdgeTypes: g.edgeTypes,
        groupCount: g.children.length,
        groupAllChildIds: g.children.map((n) => n.id),
        // Pagination disabled
        groupCurrentPage: 0,
        groupTotalPages: 1,
        groupPageSize: g.children.length,
        groupMaxCols: g.cols,
        groupMaxRows: g.rows,
      } as any,
    } as any);

    attachChildrenWrap(g.blocks, g.groupId, g.rows, g.parentId, g.outerPadX || 0, g.h);
  }

  // Lens focus objects: far-right group for pinned, disconnected nodes (degree=0).
  if (focusIslandNodes.length > 0) {
    const FOCUS_GROUP_ID = 'levels:right:focus';
    for (const n of focusIslandNodes) groupedChildIds.add(n.id);

    const count = focusIslandNodes.length;
    const focusRows = Math.max(1, Math.min(4, count));
    const focusCols = Math.max(1, Math.ceil(count / focusRows));
    const FOCUS_PAD_X = 14;
    const FOCUS_PAD_TOP = 36; // leave room for the group header
    const FOCUS_PAD_BOTTOM = 14;
    const focusGapX = Math.min(40, nodeSpacing);
    const focusGapY = 40;
    const innerW = focusCols * nodeWidth + Math.max(0, focusCols - 1) * focusGapX;
    const innerH = focusRows * nodeHeight + Math.max(0, focusRows - 1) * focusGapY;
    const focusW = innerW + FOCUS_PAD_X * 2;
    const focusH = innerH + FOCUS_PAD_TOP + FOCUS_PAD_BOTTOM;

    // Place one column to the right of the rightmost hierarchy column.
    const focusX = RIGHT_BASE_X + (maxLevel + 1) * RIGHT_LEVEL_STEP_X + Math.floor(nodeWidth * 0.75);
    // Keep centered vertically around the origin (the hierarchy is centered already).
    const focusY = -Math.floor(focusH / 2);

    groupNodes.push({
      id: FOCUS_GROUP_ID,
      type: 'horizonGroup' as any,
      position: { x: focusX, y: focusY },
      selectable: false,
      draggable: false,
      connectable: false,
      focusable: false,
      deletable: false,
      style: { width: focusW, height: focusH, zIndex: 0 } as any,
      data: {
        isGroup: true,
        groupIsBackdrop: false,
        groupInteractive: false,
        groupHideHeader: false,
        groupNoFill: true,
        groupTitle: 'Lens focus objects',
        groupCount: count,
        groupEdgeTypes: [],
        groupCurrentPage: 0,
        groupTotalPages: 1,
        groupPageSize: count,
        groupAllChildIds: focusIslandNodes.map((n) => n.id),
      } as any,
    } as any);

    // Attach children as a simple grid.
    for (let idx = 0; idx < focusIslandNodes.length; idx++) {
      const child = focusIslandNodes[idx]!;
      const rowIdx = idx % focusRows;
      const colIdx = Math.floor(idx / focusRows);
      const cx = FOCUS_PAD_X + colIdx * (nodeWidth + focusGapX);
      const cy = FOCUS_PAD_TOP + rowIdx * (nodeHeight + focusGapY);
      childNodes.push({
        ...(child as any),
        parentId: FOCUS_GROUP_ID,
        extent: 'parent' as any,
        zIndex: 1,
        hidden: preHiddenIds.has(child.id),
        position: { x: cx, y: cy },
      } as any);
    }
  }

  // NOTE: right-side stacking is computed before groups (see 5.75). Keeping it earlier prevents
  // hidden stack members from reserving vertical space and pushing packed groups downward.

  // === 6.5) Create group containers for EXPANDED right-side stacks ===
  // When a stack is expanded, wrap its members in a visible group container with a collapse button.
  for (const [stackKey, memberIds] of stackMembersByKey.entries()) {
    // Find the controller meta for this stack
    const controllerEntry = [...stackMetaById.entries()].find(([, m]) => m.key === stackKey);
    if (!controllerEntry) continue;
    const [controllerId, meta] = controllerEntry;

    // Only create groups for EXPANDED stacks
    if (!meta.expanded) continue;

    // Get the ordered list of visible members
    const orderedMembers = [...memberIds].sort((a, b) => {
      const pa = rightPos.get(a);
      const pb = rightPos.get(b);
      const ya = (pa?.y ?? 0);
      const yb = (pb?.y ?? 0);
      return ya - yb;
    });

    if (orderedMembers.length === 0) continue;

    // Ensure stack members match the intended stack kind (by relation type).
    const relatedId = (() => {
      const parts = String(stackKey || '').split(':');
      if (parts.length < 5) return '';
      if (parts[0] === 'levels' && parts[1] === 'rightstack') return String(parts[2] || '').trim();
      if (parts[0] === 'levels' && parts[1] === 'rightstackp') return String(parts[2] || '').trim();
      return '';
    })();
    const expandedMembers = expandedStackMembersByKey.get(stackKey);
    const filteredMembers = expandedMembers || (relatedId
      ? orderedMembers.filter((id) => {
          const mk = stackKindOfRightNode(id, relatedId);
          return Boolean(mk && mk.kind === meta.kind);
        })
      : orderedMembers);
    if (filteredMembers.length === 0) continue;

    // Controller position becomes the group anchor
    const controllerPos = rightPos.get(controllerId);
    if (!controllerPos) continue;

    // Calculate group dimensions (vertical layout)
    const count = filteredMembers.length;
    const innerH = count * nodeHeight + Math.max(0, count - 1) * stackGroupGapY;
    const groupW = nodeWidth + STACK_GROUP_PAD_X * 2;
    const groupH = innerH + STACK_GROUP_PAD_TOP + STACK_GROUP_PAD_BOTTOM;

    // Find the parent (pivot) node position to center the group relative to it
    const pivotId = relatedId || '';
    const pivotPos = pivotId ? rightPos.get(pivotId) : null;
    const pivotCenterY = pivotPos
      ? pivotPos.y + nodeHeight / 2
      : controllerPos.y + nodeHeight / 2;

    // Position group so its vertical center aligns with the pivot node's center
    const groupX = controllerPos.x - STACK_GROUP_PAD_X;
    const groupY = pivotCenterY - groupH / 2;

    const stackGroupId = `stack:group:${stackKey}`;

    // Collect unique edge types from all members for handle ports
    const memberEdgeTypes = new Set<string>();
    for (const mid of filteredMembers) {
      const mnode = nodeById.get(mid);
      const md: any = (mnode as any)?.data || {};
      const et = normalizeEdgeType(String(md?.edgeTypeFromParent || '').trim());
      if (et && et !== 'OTHER') memberEdgeTypes.add(et);
    }
    const edgeTypesArr = Array.from(memberEdgeTypes);

    // Build edge ports for the group (left side, since stack groups are on right pane)
    const stackGroupEdgePorts = edgeTypesArr.map((et, idx) => {
      const innerSpan = Math.max(0, groupH - STACK_GROUP_PAD_TOP - STACK_GROUP_PAD_BOTTOM);
      const midStart = STACK_GROUP_PAD_TOP + innerSpan * 0.35;
      const midEnd = STACK_GROUP_PAD_TOP + innerSpan * 0.65;
      const yCenter = edgeTypesArr.length <= 1
        ? Math.floor((midStart + midEnd) / 2)
        : Math.floor(midStart + (idx * (midEnd - midStart)) / Math.max(1, edgeTypesArr.length - 1));
      return {
        edgeType: et,
        targetHandleId: `edge:${et}:target`,
        sourceHandleId: `edge:${et}:source`,
        y: yCenter,
      };
    });

    // Create the group node
    groupNodes.push({
      id: stackGroupId,
      type: 'horizonGroup' as any,
      position: { x: groupX, y: groupY },
      selectable: true,
      draggable: true,
      connectable: false,
      focusable: false,
      deletable: false,
      style: { width: groupW, height: groupH, zIndex: 5 } as any,
      data: {
        isGroup: true,
        groupIsBackdrop: false,
        groupInteractive: true,
        groupHideHeader: false,
        groupNoFill: false,
        groupTitle: meta.title,
        groupCount: count,
        groupEdgeTypes: edgeTypesArr,
        groupEdgePorts: stackGroupEdgePorts,
        groupAllChildIds: filteredMembers,
        // Stack group specific fields
        isStackGroup: true,
        stackGroupKey: stackKey,
        stackGroupExpanded: true,
        // Callback will be attached in LensCanvasCockpit
      } as any,
    } as any);

    // Attach children as a vertical list inside the group
    for (let idx = 0; idx < filteredMembers.length; idx++) {
      const memberId = filteredMembers[idx]!;
      const member = nodeById.get(memberId);
      if (!member) continue;

      const cx = STACK_GROUP_PAD_X;
      const cy = STACK_GROUP_PAD_TOP + idx * (nodeHeight + stackGroupGapY);

      // Mark as grouped so it doesn't render as standalone
      groupedChildIds.add(memberId);

      // Clear stale stack fields from member data
      const d0: any = (member as any)?.data || {};
      const d: any = { ...d0 };
      delete d.stackGroupKey;
      delete d.stackCount;
      delete d.stackExpanded;
      delete d.hiddenByStack;
      delete (d as any).stackControllerId;
      d.levelsPane = 'right';
      d.insideStackGroup = true;
      d.parentStackGroupKey = stackKey;

      childNodes.push({
        ...(member as any),
        parentId: stackGroupId,
        extent: 'parent' as any,
        zIndex: 1,
        hidden: preHiddenIds.has(memberId),
        position: { x: cx, y: cy },
        data: d,
      } as any);
    }
  }

  // === 6.6) Resolve overlaps between stack groups and other nodes in same column ===
  // After creating stack groups, ensure no standalone nodes overlap with them.
  const stackGroupPositions = new Map<string, { x: number; y: number; w: number; h: number; level: number }>();
  for (const gn of groupNodes) {
    const gid = String(gn.id || '').trim();
    if (!gid.startsWith('stack:group:')) continue;
    const gd: any = gn.data || {};
    const stackKey = String(gd?.stackGroupKey || '').trim();
    // Find the controller to get the level
    const controllerEntry = [...stackMetaById.entries()].find(([, m]) => m.key === stackKey);
    if (!controllerEntry) continue;
    const [controllerId] = controllerEntry;
    const lv = hLevel.get(controllerId) || 0;
    const pos = gn.position || { x: 0, y: 0 };
    const style: any = gn.style || {};
    stackGroupPositions.set(gid, {
      x: pos.x,
      y: pos.y,
      w: style.width || (nodeWidth + STACK_GROUP_PAD_X * 2),
      h: style.height || nodeHeight,
      level: lv,
    });
  }

  // For each level with a stack group, repack non-grouped nodes to avoid overlap
  const levelToStackGroups = new Map<number, Array<{ id: string; y: number; h: number }>>();
  for (const [gid, gpos] of stackGroupPositions.entries()) {
    const arr = levelToStackGroups.get(gpos.level) || [];
    arr.push({ id: gid, y: gpos.y, h: gpos.h });
    levelToStackGroups.set(gpos.level, arr);
  }

  for (const [lv, stackGroups] of levelToStackGroups.entries()) {
    if (stackGroups.length === 0) continue;
    const ids = idsByLevel.get(lv) || [];
    // Get non-grouped nodes at this level
    const standaloneIds = ids.filter((id) =>
      !groupedChildIds.has(id) &&
      !stackHiddenIds.has(id) &&
      !preHiddenIds.has(id) &&
      !stackMetaById.has(id) // Not a controller (controllers become groups)
    );
    if (standaloneIds.length === 0) continue;

    // Sort stack groups by Y
    const sortedGroups = [...stackGroups].sort((a, b) => a.y - b.y);

    // For each standalone node, check if it overlaps with any group
    for (const nodeId of standaloneIds) {
      const nodePos = rightPos.get(nodeId);
      if (!nodePos) continue;
      const nodeH = unitHeights.get(nodeId) || nodeHeight;
      const nodeTop = nodePos.y;
      const nodeBottom = nodeTop + nodeH;

      // Check overlap with each group
      for (const grp of sortedGroups) {
        const grpTop = grp.y;
        const grpBottom = grp.y + grp.h;

        // If node overlaps with group, move it below the group
        if (nodeBottom > grpTop && nodeTop < grpBottom) {
          const newY = grpBottom + rightGapY(lv);
          rightPos.set(nodeId, { x: nodePos.x, y: newY });
          break; // Only move once
        }
      }
    }
  }

  // === 7) Dock remaining nodes (islands) ===
  // In Levels mode with edge-type filters, the graph can be disconnected. Keep islands visible.
  const usedChildIds = new Set<string>(childNodes.map((n) => n.id));
  const passthrough = fragments.filter((n) => !rightIds.has(n.id) && !usedChildIds.has(n.id));
  const dockCols = 6;
  const dockColW = nodeWidth + nodeSpacing;
  const dockRowH = nodeHeight + Math.max(40, Math.floor(nodeSpacing * 1.1));
  const dockStartY = (rightPos.get(rightSorted[0]?.id || '')?.y ?? 0) - 2 * dockRowH;
  // Dock lives strictly to the LEFT of the left-band (groups) so it never interferes with the right hierarchy.
  const dockX = (RIGHT_BASE_X - GROUP_GAP_X) - 420 - dockCols * dockColW;
  const docked = passthrough.map((n, i) => {
    const row = Math.floor(i / dockCols);
    const col = i % dockCols;
    return { ...n, position: { x: dockX + col * dockColW, y: dockStartY + row * dockRowH } };
  });

  // === 8) Output ===
  const dockMap = new Map(docked.map((n) => [n.id, n]));

  const placedFragments: Node<T>[] = fragments.map((n) => {
    // If this node is rendered inside a group, it must not appear as a standalone node.
    if (groupedChildIds.has(n.id)) return null as any;
    if (rightIds.has(n.id)) {
      const p = rightPos.get(n.id);
      if (!p) return n;
      const d0: any = (n as any).data || {};
      // Clear stale stack fields unless explicitly re-applied below.
      const d = { ...d0 };
      delete d.stackGroupKey;
      delete d.stackCount;
      delete d.stackExpanded;
      delete d.hiddenByStack;
      delete (d as any).stackControllerId;
      delete d.levelsPane;
      // IMPORTANT: right-pane (backbone) nodes must render in full size.
      // If the node was previously compact in the left pane, clear the override.
      delete d.nodeSize;

      // Preserve stack membership for ALL members (not only the controller).
      const memberKey = stackKeyByMemberId.get(n.id);
      if (memberKey) d.stackGroupKey = memberKey;
      const ctrl = stackControllerByMemberId.get(n.id);
      if (ctrl) (d as any).stackControllerId = ctrl;

      const meta = stackMetaById.get(n.id);
      if (meta) {
        d.stackGroupKey = meta.key;
        d.stackCount = meta.count;
        d.stackExpanded = meta.expanded;
        // Premium stack controller UI (rendered by HorizonFragmentNode)
        (d as any).stackTitle = meta.title;
        (d as any).stackSubtitle = meta.subtitle;
        (d as any).stackKind = meta.kind;
        (d as any).stackVisualType = meta.visualType;
      }
      const hiddenByStack = stackHiddenIds.has(n.id);
      d.hiddenByStack = hiddenByStack;
      d.levelsPane = 'right';
      const hidden = preHiddenIds.has(n.id) || hiddenByStack;
      // Stack controllers get higher zIndex so they render above regular nodes when expanded
      const isStackController = !!meta;
      const zIndex = isStackController ? 10 : undefined;
      return ({ ...n, hidden, data: d, position: { x: Math.round(p.x), y: Math.round(p.y) }, zIndex } as any);
    }
    if (dockMap.has(n.id)) return dockMap.get(n.id)!;
    return n;
  });

  return [...groupNodes, ...placedFragments.filter(Boolean) as any, ...childNodes];
}

/**
 * Derive stable "Levels" metadata (depth/parent/root + edgeType/direction) for an existing graph.
 *
 * Why: nodes loaded from backend are flat (often depth=0 for everything). In Levels mode, that
 * breaks the notion of "layers". This helper rebuilds a spanning tree from a chosen root so that
 * `layoutLevelsFullGraph` can place nodes into meaningful layers.
 *
 * Notes:
 * - We purposely keep depth as a non-negative integer "distance from root" (0..N)
 * - Quadrant placement is still computed later by `layoutLevelsFullGraph` via `getLinearSection`
 *   using `edgeTypeFromParent` and `directionFromParent`.
 * - If `allowedEdgeTypes` is provided, only edges of those types participate in the tree.
 * - NEW: Rank Assignment instead of simple BFS distance.
 *   - Start at Root = Rank 0.
 *   - Outgoing child edges (OWNS, etc) -> Rank + 1.
 *   - Incoming parent edges (OWNS, etc) -> Rank - 1.
 *   - Peer edges -> Rank same.
 *   - Final Depth = Rank (offset so min rank is 1? No, offset so Root is 0, parents are -1?
 *     Wait, `layoutLevelsFullGraph` expects `depth >= 0` and separates `depth=0` as central.
 *     Actually, `depth` in `layoutLevelsFullGraph` is treated as `level`.
 *     Level 1 is first layer above/below.
 *     If we have Rank -1 (parent), it means Level 1 (Top).
 *     If we have Rank +1 (child), it means Level 1 (Bottom).
 *     We need to map Rank to Level + Section.
 */
export function deriveLevelsTreeMetadata<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  rootId: string,
  allowedEdgeTypes?: string[],
): Node<T>[] {
  const byId = new Map<string, Node<T>>();
  for (const n of nodes) byId.set(String(n.id || '').trim(), n);

  const root = String(rootId || '').trim();
  if (!root || !byId.has(root)) return nodes;

  const allowed =
    Array.isArray(allowedEdgeTypes) && allowedEdgeTypes.length > 0
      ? new Set(allowedEdgeTypes.map((t) => normalizeEdgeType(t)))
      : null;

  const edgeTypeOf = (e: any): string => {
    return normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);
  };

  // Peer types that keep nodes on the same rank (siblings/partners)
  const PEER_TYPES = new Set([
    'COMPETES_WITH',
    'CONFLICTS_WITH',
    'OPPOSES',
    'PARTNERS_WITH',
    'COOPERATES_WITH',
    'RELATED_TO',
    'SIMILAR_TO',
    'LINKED_TO',
    'PARTICIPATES_IN',
  ]);

  // Types that imply hierarchy (Parent -> Child)
  // If we traverse OUTGOING edge of this type: Rank + 1
  // If we traverse INCOMING edge of this type: Rank - 1
  const HIERARCHY_TYPES = new Set([
    // Corporate structure / control
    'OWNS', 'PARENT_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'FOUNDED', 'ADMINISTERS', 'BOARD_MEMBER_OF', 'EXECUTIVE_OF',
    'PRODUCES', 'SUPPLIES_TO', 'TRANSFORMS_INTO',
    // Market-defining (org -> market): treat as hierarchy signal
    'DOMINATES',
    // Market hierarchy (parent -> submarket)
    'HAS_SUBMARKET',
    // Influence is usually Top-Down: Regulator (Parent) -> Company (Child)
    'INFLUENCES', 'AFFECTS', 'REGULATES', 'IMPACTS'
  ]);
  const STRONG_CONTROL_TYPES = new Set([
    'OWNS',
    'SUBSIDIARY_OF',
    'PARENT_OF',
    'MANAGES',
    'CONTROLS',
    'INVESTS_IN',
    'FOUNDED',
    'ADMINISTERS',
  ]);
  const INFLUENCE_TYPES = new Set(['INFLUENCES', 'AFFECTS', 'REGULATES', 'IMPACTS']);
  const hierarchyScore = (et: string): number => {
    const t = String(et || '').toUpperCase().trim();
    if (!t) return 0;
    if (STRONG_CONTROL_TYPES.has(t)) return 400;
    if (t === 'DOMINATES') return 260;
    if (t === 'HAS_SUBMARKET') return 240;
    if (t === 'BOARD_MEMBER_OF' || t === 'EXECUTIVE_OF') return 220;
    if (t === 'PRODUCES' || t === 'SUPPLIES_TO' || t === 'TRANSFORMS_INTO') return 120;
    if (INFLUENCE_TYPES.has(t)) return 40;
    return 80;
  };
  
  const INVERTED_HIERARCHY_TYPES = new Set([
    // NOTE: SUBSIDIARY_OF is inverse of OWNS (subsidiary -> parent). The backend often normalizes it into OWNS,
    // but keep this here for robustness when legacy edges leak through.
    'SUBSIDIARY_OF',
    'DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM',
    // Context acts like upstream/parent
    'LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN'
  ]);

  type Adj = { to: string; edgeType: string; dir: 'incoming' | 'outgoing' };
  const adj = new Map<string, Adj[]>();
  const add = (from: string, item: Adj) => {
    const arr = adj.get(from);
    if (arr) arr.push(item);
    else adj.set(from, [item]);
  };

  // Keep only the best hierarchy-ish edge per node-pair (prevents OWNS vs INFLUENCES contradictions).
  const bestHierByPair = new Map<string, { s: string; t: string; et: string; score: number }>();

  for (const e of edges as any[]) {
    const s = String(e?.source || '').trim();
    const t = String(e?.target || '').trim();
    if (!s || !t) continue;
    if (!byId.has(s) || !byId.has(t)) continue;
    const et = edgeTypeOf(e);
    if (allowed && !allowed.has(et)) continue;

    // For hierarchy-ish edges, keep only the best one per node pair (prevents OWNS vs INFLUENCES contradictions).
    const isHier = HIERARCHY_TYPES.has(et) || INVERTED_HIERARCHY_TYPES.has(et);
    if (isHier) {
      const key = s < t ? `${s}||${t}` : `${t}||${s}`;
      const prev = bestHierByPair.get(key);
      const cur = { s, t, et, score: hierarchyScore(et) };
      if (!prev || cur.score > prev.score || (cur.score === prev.score && INFLUENCE_TYPES.has(prev.et) && !INFLUENCE_TYPES.has(cur.et))) {
        bestHierByPair.set(key, cur);
      }
      continue;
    }

    // Non-hierarchy edges: store as-is.
    add(s, { to: t, edgeType: et, dir: 'outgoing' });
    add(t, { to: s, edgeType: et, dir: 'incoming' });
  }

  // Materialize best hierarchy edges (if any).
  if (bestHierByPair.size > 0) {
    for (const v of bestHierByPair.values()) {
      add(v.s, { to: v.t, edgeType: v.et, dir: 'outgoing' });
      add(v.t, { to: v.s, edgeType: v.et, dir: 'incoming' });
    }
  }

  const visited = new Set<string>();
  const parentMap = new Map<string, { parentId: string; edgeType: string; dir: 'incoming' | 'outgoing' }>();
  const rankMap = new Map<string, number>();

  // 1. Find the "Best Visual Root" to anchor the hierarchy.
  // Instead of blindly using `rootId` (which might be a leaf node deeply nested),
  // we traverse UP from `rootId` to find the highest-level ancestor present in the current graph.
  // This ensures the tree is drawn Top-Down relative to the true start, not the current focus.
  
  let bestRoot = root;
  const candidates = new Set<string>([root]);
  
  // Simple traversal up to find a node with no incoming hierarchy edges (roots)
  // Warning: prevent infinite loops if cycles exist
  const pathVisited = new Set<string>([root]);
  let curr = root;
  let climbing = true;
  
  while (climbing) {
    const incomingEdges = adj.get(curr) || [];
    // Find parent: someone connected via Incoming Hierarchy or Outgoing Inverted
    const parentEdge = incomingEdges
      .filter(a => {
        // We are looking for the node that IS the parent of `curr`.
        // If edge is Hierarchy (OWNS): Parent -> Child. 
        // We want Parent. So we need `curr` to be target (Incoming).
        if (HIERARCHY_TYPES.has(a.edgeType) && a.dir === 'incoming') return true;
        
        // If edge is Inverted (DEPENDS_ON): Child -> Parent.
        // We want Parent. So we need `curr` to be source (Outgoing).
        if (INVERTED_HIERARCHY_TYPES.has(a.edgeType) && a.dir === 'outgoing') return true;
        
        return false;
      })
      .sort((a, b) => (hierarchyScore(b.edgeType) - hierarchyScore(a.edgeType)))[0];

    if (parentEdge && !pathVisited.has(parentEdge.to)) {
        curr = parentEdge.to;
        pathVisited.add(curr);
        // Update best candidate
        bestRoot = curr;
    } else {
        climbing = false;
    }
  }

  // Queue: { id, rank }
  // Start BFS from the Best Root we found.
  // Note: Rank 0 will be assigned to BestRoot.
  // The original `rootId` will naturally fall into its correct Rank (e.g. 2 or 3).
  // This preserves the "Tree" shape regardless of which node is selected.
  const q: Array<{ id: string; r: number }> = [{ id: bestRoot, r: 0 }];
  visited.add(bestRoot);
  rankMap.set(bestRoot, 0);

  while (q.length > 0) {
    const cur = q.shift()!;
    const curId = cur.id;
    const curRank = cur.r;
    
    const next = adj.get(curId) || [];
    
    for (const a of next) {
      const nid = a.to;
      if (visited.has(nid)) continue;
      
      const et = a.edgeType;
      let nextRank = curRank;
      
      if (PEER_TYPES.has(et)) {
        nextRank = curRank;
      } else if (HIERARCHY_TYPES.has(et)) {
        // Normal hierarchy: Source is Parent, Target is Child.
        // If we traversed OUTGOING (cur -> nid), then Cur is Parent, Nid is Child -> Rank + 1
        // If we traversed INCOMING (nid -> cur), then Nid is Parent, Cur is Child -> Rank - 1
        nextRank = (a.dir === 'outgoing') ? curRank + 1 : curRank - 1;
      } else if (INVERTED_HIERARCHY_TYPES.has(et)) {
        // Inverted: Source is Child, Target is Parent.
        // If OUTGOING (cur -> nid): Cur depends on Nid -> Nid is Parent -> Rank - 1
        // If INCOMING (nid -> cur): Nid depends on Cur -> Nid is Child -> Rank + 1
        nextRank = (a.dir === 'outgoing') ? curRank - 1 : curRank + 1;
      } else {
        // Fallback for unknown types:
        // Assume Outgoing = Downstream (Child), Incoming = Upstream (Parent).
        nextRank = (a.dir === 'outgoing') ? curRank + 1 : curRank - 1;
      }
      
      visited.add(nid);
      parentMap.set(nid, { parentId: curId, edgeType: et, dir: a.dir });
      rankMap.set(nid, nextRank);
      q.push({ id: nid, r: nextRank });
    }
  }

  // Apply derived metadata.
  // layoutLevelsFullGraph expects `depth` to be positive level index.
  // But we have signed Ranks (0=Center, -1=Top L1, +1=Bottom L1).
  // We need to map Rank back to the "Level" and "Section" concepts.
  // 
  // Level = abs(Rank).
  // Section depends on edge type AND sign of rank?
  // Actually, `getLinearSection` calculates section based on edge type & direction.
  // BUT `getLinearSection` logic might contradict our Rank logic if edge direction is ambiguous.
  //
  // Let's force `depth` to be `abs(rank)`.
  // And we need to ensure `directionFromParent` + `edgeTypeFromParent` yields the correct quadrant.
  //
  // If Rank < 0 (Upstream/Top):
  // We want `getLinearSection` to return 'top-left' or 'top-right'.
  // `getLinearSection` returns Top if:
  // - isBottom is false.
  // - isBottom is false if `SYMMETRIC` OR (!isOutgoing AND !REVERSE_VERTICAL) OR (isOutgoing AND REVERSE_VERTICAL).
  //
  // This is complex to reverse engineer.
  // Alternatively, we can trust `getLinearSection` if we provide the correct "parent" relationship.
  // In `parentMap`, we stored the edge used to reach the node.
  // If we reached it via BFS, that edge defines the relationship.
  
  return nodes.map((n) => {
    const id = String(n.id || '').trim();
    if (!visited.has(id)) return n;
    
    const d: any = (n as any).data || {};
    const rank = rankMap.get(id) || 0;
    
    // Central row
    if (rank === 0) {
      return {
        ...(n as any),
        data: {
          ...d,
          parentFragmentId: undefined,
          rootFragmentId: root,
          depth: 0,
          edgeTypeFromParent: undefined,
          directionFromParent: undefined,
        },
      };
    }

    const p = parentMap.get(id);
    // Depth is absolute distance from center
    const dep = Math.abs(rank);
    
    // We keep the edge metadata as is.
    // `layoutLevelsFullGraph` will use `getLinearSection` to determine quadrant.
    // Rank < 0 should ideally land in Top quadrants.
    // Rank > 0 should land in Bottom quadrants.
    // 
    // Does `getLinearSection` guarantee this?
    // It relies on `edgeType` and `isOutgoing`.
    // If we have `OWNS` (Hierarchy), and Rank is +1 (Child):
    // We traversed Outgoing `OWNS`. `edgeType`=OWNS, `dir`=outgoing.
    // `getLinearSection` for OWNS + outgoing -> isBottom=true -> Bottom. CORRECT.
    //
    // If we have `OWNS` (Hierarchy), and Rank is -1 (Parent):
    // We traversed Incoming `OWNS`. `edgeType`=OWNS, `dir`=incoming.
    // `getLinearSection` for OWNS + incoming -> isBottom=false -> Top. CORRECT.
    //
    // It seems `getLinearSection` is consistent with our Rank logic for Hierarchy types.
    // Peer types (Rank 0) are handled above (return depth=0).
    
    return {
      ...(n as any),
      data: {
        ...d,
        parentFragmentId: p?.parentId,
        rootFragmentId: root,
        depth: dep,
        edgeTypeFromParent: p?.edgeType,
        directionFromParent: p?.dir,
      },
    };
  });
}

/**
 * Legacy full-graph layout used by "Linear" mode in `LensCanvasCockpit`.
 *
 * It positions:
 * - depth===0 nodes into a centered "central" row
 * - all other nodes into 4 quadrants around that row (derived from `getLinearSection`)
 *
 * In addition to fragment nodes, we also generate non-interactive `horizonGroup` backdrop nodes
 * (one per section) for modern UX: users can visually parse the 4 quadrants + center band.
 *
 * These are NOT parent containers (no `parentId` on children) to avoid ReactFlow parent linkage issues.
 */
export async function layoutLinearFullGraph<T = any>(
  nodes: Node<T>[],
  _edges: Edge[],
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  if (nodes.length === 0) return nodes;

  // IMPORTANT (ReactFlow):
  // If a node has `parentId`, ReactFlow expects the parent node to be present in the same `nodes` array.
  // When switching layout modes (freeform -> linear), we often drop group container nodes,
  // but child nodes may still carry a stale `parentId` (e.g. "group:<parent>:context").
  // That crashes ReactFlow with: "Parent node ... not found".
  //
  // Linear layout in this app is rendered as a flat graph, so we must strip any parent/extent props.
  const flatNodes: Node<T>[] = nodes.map((n) => {
    const out: any = { ...n };
    delete out.parentId;
    delete out.parentNode;
    delete out.extent;
    delete out.expandParent;
    return out as Node<T>;
  });

  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 60,
    layerSpacing = 150,
    maxChildRows = MAX_CHILD_ROWS,
  } = options;

  // Determine central nodes (depth === 0). If none are marked, fall back to "no parent" roots.
  let centralIds = new Set<string>(
    flatNodes.filter((n) => (n.data as any)?.depth === 0).map((n) => n.id),
  );
  if (centralIds.size === 0) {
    for (const n of flatNodes) {
      if (!(n.data as any)?.parentFragmentId) centralIds.add(n.id);
    }
  }
  if (centralIds.size === 0) centralIds = new Set([flatNodes[0]!.id]);

  const central: Node<T>[] = [];
  const others: Node<T>[] = [];
  for (const n of flatNodes) {
    if (centralIds.has(n.id)) central.push(n);
    else others.push(n);
  }

  const sections: Record<Exclude<LinearSection, 'center'>, Node<T>[]> = {
    'top-left': [],
    'top-right': [],
    'bottom-left': [],
    'bottom-right': [],
  };

  for (const n of others) {
    const d: any = n.data;
    const edgeType = d?.edgeTypeFromParent;
    const isOutgoing = d?.directionFromParent !== 'incoming';
    const section = getLinearSection(edgeType, isOutgoing);
    if (section === 'center') {
      sections['bottom-left'].push(n);
    } else {
      sections[section].push(n);
    }
  }

  // Layout helpers
  // UX: keep quadrants closer to the central band than Freeform, so the layout feels cohesive.
  const bandGapY = Math.max(70, Math.floor(layerSpacing * 0.55));
  const rowGapY = nodeHeight + Math.max(52, Math.floor(layerSpacing * 0.35));
  const baseTopY = -(nodeHeight + bandGapY);
  const baseBottomY = nodeHeight + bandGapY;

  const centralWidth =
    central.length * nodeWidth + Math.max(0, central.length - 1) * nodeSpacing;
  const centralStartX = -centralWidth / 2;
  const centralEndX = centralStartX + centralWidth;
  const centralCenterX = (centralStartX + centralEndX) / 2;

  const pos = new Map<string, { x: number; y: number }>();

  // Central row
  central.forEach((n, i) => {
    pos.set(n.id, { x: centralStartX + i * (nodeWidth + nodeSpacing), y: 0 });
  });

  // Build real group containers for sections with pagination (max 6x3).
  // Central nodes stay ungrouped.
  const HALF_GAP = nodeWidth / 2; // Leave one node width between left and right groups

  const sectionMeta: Array<{
    section: Exclude<LinearSection, 'center'>;
    isTop: boolean;
    isLeft: boolean;
    semanticGroup: SemanticGroup;
  }> = [
    { section: 'top-left', isTop: true, isLeft: true, semanticGroup: 'supply-chain' },
    { section: 'top-right', isTop: true, isLeft: false, semanticGroup: 'context' },
    { section: 'bottom-left', isTop: false, isLeft: true, semanticGroup: 'customers' },
    { section: 'bottom-right', isTop: false, isLeft: false, semanticGroup: 'market' },
  ];

  const uniqueEdgeTypes = (list: Node<T>[]) => {
    const s = new Set<string>();
    for (const n of list) {
      const t = String((n.data as any)?.edgeTypeFromParent || '').trim();
      if (t) s.add(normalizeEdgeType(t));
    }
    return Array.from(s);
  };

  const groupDims = (count: number) => {
    const totalPages = Math.ceil(count / GROUP_PAGE_SIZE);
    const hasPagination = totalPages > 1;

    let nodesPerRow: number;
    let rows: number;
    if (hasPagination) {
      nodesPerRow = GROUP_MAX_COLS;
      rows = GROUP_MAX_ROWS;
    } else {
      nodesPerRow = Math.min(count, GROUP_MAX_COLS);
      rows = Math.min(GROUP_MAX_ROWS, Math.ceil(count / Math.max(1, nodesPerRow)));
    }
    const innerW = nodesPerRow * nodeWidth + Math.max(0, nodesPerRow - 1) * nodeSpacing;
    const innerH = rows * nodeHeight + Math.max(0, rows - 1) * Math.max(48, Math.floor(nodeSpacing * 1.05));
    const w = innerW + GROUP_PADDING_X * 2;
    const h = innerH + GROUP_HEADER_HEIGHT + GROUP_PADDING_BOTTOM + (hasPagination ? GROUP_PAGINATION_HEIGHT : 0);
    return { w, h, nodesPerRow, rows, totalPages, hasPagination };
  };

  const groupNodes: Node<T>[] = [];
  const placed: Node<T>[] = [];

  // Place central nodes (absolute)
  for (const n of central) {
    const p = pos.get(n.id);
    placed.push(p ? ({ ...n, position: p } as any) : n);
  }

  // Place section groups + children (child positions are relative to group)
  for (const meta of sectionMeta) {
    const list = sections[meta.section];
    if (!list || list.length === 0) continue;

    const dim = groupDims(list.length);
    const baseY = meta.isTop ? (baseTopY - (dim.h - nodeHeight)) : baseBottomY;

    const x = meta.isLeft
      ? (centralCenterX - HALF_GAP - dim.w)
      : (centralCenterX + HALF_GAP);
    const y = meta.isTop ? baseY : baseY;

    const groupId = `linear:${meta.section}`;
    const allChildIds = list.map((n) => n.id);

    groupNodes.push({
      id: groupId,
      type: 'horizonGroup' as any,
      position: { x, y },
      selectable: false,
      draggable: false,
      connectable: false,
      focusable: false,
      deletable: false,
      style: { width: dim.w, height: dim.h, zIndex: 0 } as any,
      data: {
        isGroup: true,
        groupIsBackdrop: false,
        groupInteractive: false,
        groupSemanticGroup: meta.semanticGroup,
        groupLinearSection: meta.section,
        groupCount: list.length,
        groupEdgeTypes: uniqueEdgeTypes(list),
        groupAllChildIds: allChildIds,
        groupCurrentPage: 0,
        groupTotalPages: dim.totalPages,
        groupPageSize: GROUP_PAGE_SIZE,
        groupMaxCols: GROUP_MAX_COLS,
        groupMaxRows: GROUP_MAX_ROWS,
      } as any,
    });

    const childGapX = nodeSpacing;
    const childGapY = Math.max(48, Math.floor(nodeSpacing * 1.05));
    const padTop = GROUP_HEADER_HEIGHT;

    const startIdx = 0;
    const endIdx = Math.min(GROUP_PAGE_SIZE, list.length);

    for (let idx = 0; idx < list.length; idx++) {
      const child = list[idx]!;
      const isVisible = idx >= startIdx && idx < endIdx;
      const pageIdx = idx - startIdx;
      const rowIdx = Math.floor(pageIdx / dim.nodesPerRow);
      const colIdx = pageIdx % dim.nodesPerRow;
      const rtlColIdx = dim.nodesPerRow - 1 - colIdx;
      const cx = GROUP_PADDING_X + rtlColIdx * (nodeWidth + childGapX);
      const cy = padTop + rowIdx * (nodeHeight + childGapY);

      placed.push({
        ...(child as any),
        parentId: groupId,
        extent: 'parent' as any,
        zIndex: 1,
        position: { x: cx, y: cy },
        hidden: !isVisible,
      } as any);
    }
  }

  // Include "others" that weren't assigned (safety)
  const placedIds = new Set<string>(placed.map((n) => n.id));
  for (const n of others) {
    if (!placedIds.has(n.id)) placed.push(n);
  }

  // Group nodes first (behind), then central + children.
  return [...groupNodes, ...placed];
}

/**
 * Normalize edge type for grouping purposes.
 * OPERATES_IN and LOCATED_IN are grouped together as LOCATED_IN.
 */
const normalizeEdgeTypeForGrouping = (t: string | undefined): string => {
  const normalized = normalizeEdgeType(t);
  if (normalized === 'OPERATES_IN') return 'LOCATED_IN';
  return normalized;
};

const makeSafeId = (raw: string) => raw.replace(/[^a-zA-Z0-9:_-]/g, '_');

const isGroupNode = (n: Node<any>) => Boolean((n.data as any)?.isGroup);

const GROUP_PADDING_X = 16;
const GROUP_PADDING_BOTTOM = 16;
const GROUP_HEADER_HEIGHT = 34; // space for label row in container
const GROUP_GAP_X = 40;
const GROUP_GAP_Y = 40;

// Pagination constants for groups
// 2025/2026 UX: fill right-to-left, top-to-bottom; up to 6 columns x 3 rows per page.
const GROUP_MAX_COLS = 6;           // Max columns in group
const GROUP_MAX_ROWS = 3;           // Max rows in group
const GROUP_PAGE_SIZE = GROUP_MAX_COLS * GROUP_MAX_ROWS; // Max items per page (6x3 matrix)
const GROUP_PAGINATION_HEIGHT = 24; // Height for "Page N of M" footer

// Group containers are keyed by (parentId, semanticGroup).
// This creates exactly 4 groups max per parent (one per side of the node).
const defaultGroupId = (parentId: string, semanticGroup: SemanticGroup) =>
  makeSafeId(`group:${parentId}:${semanticGroup}`);

function makeGroupParentNode<T = any>(params: {
  id: string;
  position: { x: number; y: number };
  parentId: string;
  ownerLabel?: string;
  ownerType?: string;
  ownerCanonicalPath?: string;
  semanticGroup: SemanticGroup;
  edgeTypes: string[];  // All edge types in this semantic group (for display)
  count: number;
  /** Explicit container width (used to preserve padding and collision sizing) */
  containerWidth?: number;
  /** Explicit container height (used to preserve padding and collision sizing) */
  containerHeight?: number;
  nodeWidth: number;
  nodeHeight: number;
  exemplarData?: any;
  // Pagination fields
  allChildIds?: string[];
  currentPage?: number;
  onGroupPageChange?: (groupId: string, page: number) => void;
}): Node<T> {
  const {
    id,
    position,
    parentId,
    ownerLabel,
    ownerType,
    ownerCanonicalPath,
    semanticGroup,
    edgeTypes,
    count,
    containerWidth,
    containerHeight,
    nodeWidth,
    nodeHeight,
    exemplarData,
    allChildIds,
    currentPage = 0,
    onGroupPageChange,
  } = params;

  const totalPages = Math.ceil(count / GROUP_PAGE_SIZE);
  const hasPagination = totalPages > 1;

  const w = typeof containerWidth === 'number' && Number.isFinite(containerWidth) ? containerWidth : Math.max(220, nodeWidth + 40);
  // Add pagination height if there are multiple pages
  const baseH = typeof containerHeight === 'number' && Number.isFinite(containerHeight) ? containerHeight : Math.max(120, nodeHeight + 60);
  const h = hasPagination ? baseH + GROUP_PAGINATION_HEIGHT : baseH;

  return {
    id,
    type: 'horizonGroup' as any,
    position,
    // Groups are visual containers; keep them non-interactive so they don't steal clicks from children.
    selectable: false,
    draggable: false,
    connectable: false,
    focusable: false,
    deletable: false,
    // Explicit size so we preserve padding and collision detection is correct.
    style: { width: w, height: h, zIndex: 0 } as any,
    data: {
      ...(exemplarData || {}),
      isGroup: true,
      groupInteractive: false,
      groupIsBackdrop: false,
      groupParentId: parentId,
      groupOwnerFragmentId: parentId,
      groupOwnerLabel: ownerLabel,
      groupOwnerType: ownerType,
      groupOwnerCanonicalPath: ownerCanonicalPath,
      groupSemanticGroup: semanticGroup,
      groupEdgeTypes: edgeTypes,
      groupCount: count,
      // Legacy fields for backwards compatibility with hover logic
      groupDirection: SEMANTIC_GROUP_POSITION[semanticGroup] === 'top' ? 'incoming'
        : SEMANTIC_GROUP_POSITION[semanticGroup] === 'bottom' ? 'outgoing' : 'both',
      groupEdgeType: edgeTypes[0] || 'OTHER',
      // Pagination fields
      groupCurrentPage: currentPage,
      groupTotalPages: totalPages,
      groupPageSize: GROUP_PAGE_SIZE,
      groupAllChildIds: allChildIds || [],
      onGroupPageChange,
    } as any,
  };
}

function attachChildToGroup<T = any>(
  child: Node<T>,
  groupId: string,
  relPos: { x: number; y: number },
): Node<T> {
  return {
    ...child,
    parentId: groupId,
    // ReactFlow legacy prop still supported, but we should use parentId
    // parentNode: groupId,
    extent: 'parent' as any,
    zIndex: 1,
    position: relPos,
  };
}

export interface LayoutOptions_Custom {
  /** Use incremental layout (existing nodes keep positions) */
  incremental?: boolean;
  /** Direction: 'DOWN' | 'RIGHT' | 'UP' | 'LEFT' */
  direction?: 'DOWN' | 'RIGHT' | 'UP' | 'LEFT';
  /** Node width override */
  nodeWidth?: number;
  /** Node height override */
  nodeHeight?: number;
  /** Horizontal spacing between nodes */
  nodeSpacing?: number;
  /** Vertical spacing between layers */
  layerSpacing?: number;
  /** IDs of nodes that should keep their positions (for incremental) */
  fixedNodeIds?: Set<string>;
  /** Maximum nodes per row before wrapping */
  maxNodesPerRow?: number;
  /** Maximum number of rows for children (default 3) */
  maxChildRows?: number;
  /**
   * Collapsed subcategories in left-pane groups (Levels layout).
   * Key format: `${groupId}:${edgeType}` for per-group state, or just `edgeType` for global default.
   * When an edge type is in this set, its nodes in the group are rendered as a compact stack.
   */
  collapsedSubcategories?: Set<string>;
  /**
   * Callback to toggle subcategory collapse state (Levels layout).
   * Called from group UI when user clicks the collapse/expand button.
   */
  onToggleSubcategoryCollapse?: (groupId: string, edgeType: string, nextCollapsed: boolean, focusNodeId?: string) => void;
  /**
   * Use compact node rendering for left pane groups (Levels layout).
   * When true, left pane nodes will have nodeSize: 'compact' and use COMPACT_NODE_LAYOUT_HEIGHT.
   */
  compactLeftPane?: boolean;

  /**
   * Right Dock mode (Levels layout).
   * When enabled, right-side "stack controller" nodes are disabled and upstream promotion into the right backbone
   * is gated by 4 dock filters: Governance / Supply chain / Dependencies / Structure.
   */
  rightDockMode?: boolean;
  /**
   * Per-node enabled dock filters. Keys are node ids; values are enabled filter keys.
   * When not provided for a node, defaults are derived from node type (see layoutLevelsTwoPane).
   */
  rightDockStateByNodeId?: Record<string, Array<'governance' | 'supply' | 'dependencies' | 'structure'>>;
  /** Toggle callback used by the node UI (HorizonFragmentNode). */
  onToggleRightDock?: (nodeId: string, key: 'governance' | 'supply' | 'dependencies' | 'structure', nextEnabled: boolean) => void;
}

/**
 * Apply ELK hierarchical layout to ReactFlow nodes and edges
 *
 * @param nodes - ReactFlow nodes
 * @param edges - ReactFlow edges
 * @param options - Layout options
 * @returns Nodes with updated positions
 */
export async function applyElkLayout<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  if (nodes.length === 0) return nodes;

  const {
    incremental = false,
    direction = 'DOWN',
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 60,
    layerSpacing = 150, // Extra space for incoming/outgoing children
    fixedNodeIds = new Set<string>(),
    maxNodesPerRow = DEFAULT_MAX_NODES_PER_ROW,
    maxChildRows = MAX_CHILD_ROWS,
  } = options;

  // Build ELK graph
  const elkNodes: ElkNode[] = nodes.map((node) => ({
    id: node.id,
    width: nodeWidth,
    height: nodeHeight,
    // For incremental layout, set fixed position for existing nodes
    ...(incremental && fixedNodeIds.has(node.id)
      ? {
          x: node.position.x,
          y: node.position.y,
          layoutOptions: {
            'elk.position': `(${node.position.x},${node.position.y})`,
          },
        }
      : {}),
  }));

  const elkEdges: ElkExtendedEdge[] = edges.map((edge) => ({
    id: edge.id,
    sources: [edge.source],
    targets: [edge.target],
  }));

  // Calculate preferred aspect ratio based on maxNodesPerRow
  // This helps ELK prefer wider layouts that don't exceed max nodes per row
  const preferredWidth = maxNodesPerRow * (nodeWidth + nodeSpacing);
  const aspectRatio = Math.max(1.0, preferredWidth / (2 * (nodeHeight + layerSpacing)));

  const layoutOptions: LayoutOptions = {
    ...(incremental ? INCREMENTAL_LAYOUT_OPTIONS : DEFAULT_LAYOUT_OPTIONS),
    'elk.direction': direction,
    'elk.spacing.nodeNode': String(nodeSpacing),
    'elk.layered.spacing.nodeNodeBetweenLayers': String(layerSpacing),
    'elk.aspectRatio': String(aspectRatio.toFixed(2)),
    // Wrapping to limit nodes per layer
    'elk.layered.wrapping.strategy': 'MULTI_EDGE',
    'elk.layered.wrapping.correctionFactor': '1.0',
    'elk.layered.wrapping.cutting.strategy': 'ARD',
  };

  const elkGraph: ElkNode = {
    id: 'root',
    layoutOptions,
    children: elkNodes,
    edges: elkEdges,
  };

  try {
    const layoutedGraph = await elk.layout(elkGraph);

    // Map ELK positions back to ReactFlow nodes
    const positionMap = new Map<string, { x: number; y: number }>();
    if (layoutedGraph.children) {
      for (const elkNode of layoutedGraph.children) {
        if (elkNode.x !== undefined && elkNode.y !== undefined) {
          positionMap.set(elkNode.id, { x: elkNode.x, y: elkNode.y });
        }
      }
    }

    // Return nodes with updated positions
    return nodes.map((node) => {
      const newPos = positionMap.get(node.id);
      if (newPos) {
        return {
          ...node,
          position: newPos,
        };
      }
      return node;
    });
  } catch (error) {
    console.error('[ELK] Layout failed:', error);
    return nodes; // Return original nodes on error
  }
}

/**
 * Layout only new nodes while keeping existing nodes fixed
 * Uses a smarter approach that:
 * 1. Respects existing node positions (no overlap)
 * 2. Wraps new nodes into multiple rows
 * 3. Places children logically below parent
 *
 * @param existingNodes - Nodes already on the canvas (keep positions)
 * @param newNodes - New nodes to position
 * @param edges - All edges (existing + new)
 * @param parentNodeId - ID of the parent node (for positioning near it)
 * @param options - Layout options
 * @returns All nodes with positions (existing unchanged, new positioned)
 */
export async function layoutNewNodesAroundParent<T = any>(
  existingNodes: Node<T>[],
  newNodes: Node<T>[],
  edges: Edge[],
  parentNodeId: string,
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  if (newNodes.length === 0) return existingNodes;

  const parentNode = existingNodes.find((n) => n.id === parentNodeId);
  if (!parentNode) {
    console.warn('[ELK] Parent node not found:', parentNodeId);
    return [...existingNodes, ...newNodes];
  }

  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 60,
    layerSpacing = 150, // Extra space for incoming/outgoing children
    maxNodesPerRow = DEFAULT_MAX_NODES_PER_ROW,
    maxChildRows = MAX_CHILD_ROWS,
  } = options;

  // Build occupancy grid to avoid overlaps with existing nodes
  const cellSize = 40; // Grid cell size for occupancy checking
  const occupied = new Map<string, boolean>();
  
  const gridKey = (x: number, y: number) => {
    const gx = Math.floor(x / cellSize);
    const gy = Math.floor(y / cellSize);
    return `${gx},${gy}`;
  };
  
  // Mark cells occupied by existing nodes (with padding)
  const padding = 20;
  for (const node of existingNodes) {
    const left = node.position.x - padding;
    const right = node.position.x + nodeWidth + padding;
    const top = node.position.y - padding;
    const bottom = node.position.y + nodeHeight + padding;
    
    for (let x = left; x <= right; x += cellSize) {
      for (let y = top; y <= bottom; y += cellSize) {
        occupied.set(gridKey(x, y), true);
      }
    }
  }
  
  // Check if a position is free (no overlaps)
  const isFree = (x: number, y: number): boolean => {
    for (let dx = 0; dx <= nodeWidth; dx += cellSize) {
      for (let dy = 0; dy <= nodeHeight; dy += cellSize) {
        if (occupied.get(gridKey(x + dx, y + dy))) {
          return false;
        }
      }
    }
    return true;
  };
  
  // Mark a position as occupied
  const markOccupied = (x: number, y: number): void => {
    for (let dx = -padding; dx <= nodeWidth + padding; dx += cellSize) {
      for (let dy = -padding; dy <= nodeHeight + padding; dy += cellSize) {
        occupied.set(gridKey(x + dx, y + dy), true);
      }
    }
  };
  
  // Find nearest free position
  const findFreePosition = (desiredX: number, desiredY: number): { x: number; y: number } => {
    // Try desired position first
    if (isFree(desiredX, desiredY)) {
      markOccupied(desiredX, desiredY);
      return { x: desiredX, y: desiredY };
    }
    
    // Spiral search for free position
    const stepX = nodeWidth + nodeSpacing;
    const stepY = nodeHeight + nodeSpacing / 2;
    
    for (let ring = 1; ring <= 10; ring++) {
      // Try positions in expanding rings
      for (let dx = -ring; dx <= ring; dx++) {
        for (let dy = 0; dy <= ring; dy++) { // Prefer going down
          const x = desiredX + dx * stepX;
          const y = desiredY + dy * stepY;
          if (isFree(x, y)) {
            markOccupied(x, y);
            return { x, y };
          }
        }
      }
    }
    
    // Fallback: just use desired position (will overlap, but better than nothing)
    markOccupied(desiredX, desiredY);
    return { x: desiredX, y: desiredY };
  };

  // Separate nodes by direction: incoming (above parent) vs outgoing (below parent)
  const incomingNodes: Node<T>[] = [];
  const outgoingNodes: Node<T>[] = [];
  
  for (const node of newNodes) {
    const nodeData = node.data as any;
    const direction = nodeData?.directionFromParent || 'outgoing';
    if (direction === 'incoming') {
      incomingNodes.push(node);
    } else {
      outgoingNodes.push(node);
    }
  }
  
  console.log('[ELK] Nodes by direction:', { incoming: incomingNodes.length, outgoing: outgoingNodes.length });
  
  const parentX = parentNode.position.x;
  const parentY = parentNode.position.y;
  
  // Helper to layout a group of nodes in a grid (max 2 rows)
  const layoutGroup = (
    nodes: Node<T>[], 
    startY: number, 
    goingUp: boolean
  ): Node<T>[] => {
    if (nodes.length === 0) return [];
    
    const numNodes = nodes.length;
    // Calculate nodesPerRow to fit within maxChildRows (default 2)
    const nodesPerRow = Math.max(1, Math.ceil(numNodes / maxChildRows));
    const numRows = Math.min(maxChildRows, Math.ceil(numNodes / nodesPerRow));
    
    const totalWidth = nodesPerRow * nodeWidth + (nodesPerRow - 1) * nodeSpacing;
    const startX = parentX + (nodeWidth / 2) - (totalWidth / 2);
    
    return nodes.map((node, idx) => {
      const row = Math.floor(idx / nodesPerRow);
      const col = idx % nodesPerRow;
      
      const nodesInThisRow = Math.min(nodesPerRow, numNodes - row * nodesPerRow);
      const rowOffset = (nodesPerRow - nodesInThisRow) * (nodeWidth + nodeSpacing) / 2;
      
      const desiredX = startX + col * (nodeWidth + nodeSpacing) + rowOffset;
      // For incoming (above), rows go up; for outgoing (below), rows go down
      const rowY = goingUp 
        ? startY - row * (nodeHeight + layerSpacing / 2)
        : startY + row * (nodeHeight + layerSpacing / 2);
      
      const position = findFreePosition(desiredX, rowY);
      
      return { ...node, position };
    });
  };
  
  // Layout incoming nodes ABOVE parent
  const incomingStartY = parentY - nodeHeight - layerSpacing;
  const positionedIncoming = layoutGroup(incomingNodes, incomingStartY, true);
  
  // Layout outgoing nodes BELOW parent  
  const outgoingStartY = parentY + nodeHeight + layerSpacing;
  const positionedOutgoing = layoutGroup(outgoingNodes, outgoingStartY, false);
  
  return [...existingNodes, ...positionedIncoming, ...positionedOutgoing];
}

/**
 * Variant C (local re-layout): group children by (incoming/outgoing × edgeType) and layout
 * only the expanded neighborhood (its children + their group containers). All other nodes stay fixed.
 *
 * This drastically reduces "spaghetti" on hub expansions while preserving mental map.
 */
export async function layoutExpansionWithGroups<T = any>(
  existingNodes: Node<T>[],
  newNodes: Node<T>[],
  parentNodeId: string,
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  if (newNodes.length === 0) return existingNodes;

  // `parentNodeId` is the logical fragment id. In some flows ReactFlow `node.id` can be a lensItemId,
  // while `data.fragmentId` holds the fragment id. Match both to avoid silently skipping group creation.
  const parentNode = existingNodes.find((n) => {
    if (n.id === parentNodeId) return true;
    const fid = String((n.data as any)?.fragmentId || '').trim();
    return fid === parentNodeId;
  });
  if (!parentNode) {
    console.warn('[ELK] Parent node not found:', parentNodeId);
    return [...existingNodes, ...newNodes];
  }

  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 60,
    layerSpacing = 150,
    maxChildRows = MAX_CHILD_ROWS,
  } = options;

  // Neighborhood to re-layout: all existing children of the parent + newly added children.
  const existingChildren = existingNodes.filter((n) => {
    if (isGroupNode(n as any)) return false;
    const d: any = n.data;
    return d?.parentFragmentId === parentNodeId;
  });

  // Remove any old group containers for this parent; we'll rebuild them.
  const fixedNodes = existingNodes.filter((n) => {
    const d: any = n.data;
    if (d?.isGroup && d?.groupParentId === parentNodeId) return false;
    if (!isGroupNode(n as any) && d?.parentFragmentId === parentNodeId) return false;
    return true;
  });

  const neighborhoodChildren: Node<T>[] = [...existingChildren, ...newNodes];

  // Build occupancy grid for fixed nodes only (avoid overlaps while placing containers)
  const cellSize = 40;
  const occupied = new Map<string, boolean>();
  const padding = 20;

  const gridKey = (x: number, y: number) => `${Math.floor(x / cellSize)},${Math.floor(y / cellSize)}`;

  const markRectOccupied = (x: number, y: number, w: number, h: number) => {
    const left = x - padding;
    const right = x + w + padding;
    const top = y - padding;
    const bottom = y + h + padding;
    for (let xx = left; xx <= right; xx += cellSize) {
      for (let yy = top; yy <= bottom; yy += cellSize) {
        occupied.set(gridKey(xx, yy), true);
      }
    }
  };

  const isRectFree = (x: number, y: number, w: number, h: number): boolean => {
    for (let dx = 0; dx <= w; dx += cellSize) {
      for (let dy = 0; dy <= h; dy += cellSize) {
        if (occupied.get(gridKey(x + dx, y + dy))) return false;
      }
    }
    return true;
  };

  for (const node of fixedNodes) {
    // Group nodes for other parents are treated as fixed obstacles too
    const rawW: any = (node.style as any)?.width;
    const rawH: any = (node.style as any)?.height;
    const measuredW: any = (node as any)?.width;
    const measuredH: any = (node as any)?.height;
    const w =
      typeof measuredW === 'number'
        ? measuredW
        : typeof rawW === 'number'
          ? rawW
          : typeof rawW === 'string'
            ? parseFloat(rawW)
            : nodeWidth;
    const h =
      typeof measuredH === 'number'
        ? measuredH
        : typeof rawH === 'number'
          ? rawH
          : typeof rawH === 'string'
            ? parseFloat(rawH)
            : nodeHeight;

    const ww = isGroupNode(node as any) && Number.isFinite(w) ? w : nodeWidth;
    const hh = isGroupNode(node as any) && Number.isFinite(h) ? h : nodeHeight;
    markRectOccupied(node.position.x, node.position.y, ww, hh);
  }

  const findFreeRect = (
    desiredX: number,
    desiredY: number,
    w: number,
    h: number,
    preferDown: boolean,
  ): { x: number; y: number } => {
    if (isRectFree(desiredX, desiredY, w, h)) {
      markRectOccupied(desiredX, desiredY, w, h);
      return { x: desiredX, y: desiredY };
    }

    const stepX = Math.max(nodeWidth + nodeSpacing, Math.floor(w / 2));
    const stepY = Math.max(nodeHeight + Math.floor(nodeSpacing / 2), Math.floor(h / 2));

    for (let ring = 1; ring <= 10; ring++) {
      for (let dx = -ring; dx <= ring; dx++) {
        // Prefer down (outgoing) or up (incoming)
        for (let dy = 0; dy <= ring; dy++) {
          const ddy = preferDown ? dy : -dy;
          const x = desiredX + dx * stepX;
          const y = desiredY + ddy * stepY;
          if (isRectFree(x, y, w, h)) {
            markRectOccupied(x, y, w, h);
            return { x, y };
          }
        }
      }
    }

    markRectOccupied(desiredX, desiredY, w, h);
    return { x: desiredX, y: desiredY };
  };

  // Group children by semantic group (4 groups max: supply-chain, customers, market, context)
  type SemanticGroupData = {
    semanticGroup: SemanticGroup;
    edgeTypes: Set<string>;
    nodes: Node<T>[];
  };
  const groupsMap = new Map<SemanticGroup, SemanticGroupData>();
  
  for (const child of neighborhoodChildren) {
    const d: any = child.data;
    const isOutgoing = d?.directionFromParent !== 'incoming';
    const edgeType = normalizeEdgeType(d?.edgeTypeFromParent);
    const sg = getSemanticGroup(edgeType, isOutgoing);
    
    const g = groupsMap.get(sg) || { semanticGroup: sg, edgeTypes: new Set<string>(), nodes: [] };
    g.nodes.push(child);
    g.edgeTypes.add(edgeType);
    groupsMap.set(sg, g);
  }

  // Convert to array and sort children within each group
  const groups = Array.from(groupsMap.values())
    .map((g) => {
      const sorted = [...g.nodes].sort((a, b) => {
        const ap = getEdgePriority((a.data as any)?.edgeTypeFromParent);
        const bp = getEdgePriority((b.data as any)?.edgeTypeFromParent);
        if (ap !== bp) return ap - bp;
        const al = String((a.data as any)?.label || a.id);
        const bl = String((b.data as any)?.label || b.id);
        return al.localeCompare(bl);
      });
      return { ...g, nodes: sorted, edgeTypes: Array.from(g.edgeTypes) };
    });

  const parentX = parentNode.position.x;
  const parentY = parentNode.position.y;
  const parentCenterX = parentX + nodeWidth / 2;
  const parentCenterY = parentY + nodeHeight / 2;

  // Precompute group bbox sizes from child grid
  const childGapX = nodeSpacing;
  const childGapY = Math.max(48, Math.floor(nodeSpacing * 1.05));

  // Calculate group dimensions based on item count
  // If pagination needed (>GROUP_PAGE_SIZE items): fixed 6x3 grid
  // If no pagination: size matches actual item count
  const groupDims = (count: number) => {
    const totalPages = Math.ceil(count / GROUP_PAGE_SIZE);
    const hasPagination = totalPages > 1;

    let nodesPerRow: number;
    let rows: number;

    if (hasPagination) {
      // Fixed 6x3 grid for paginated groups
      nodesPerRow = GROUP_MAX_COLS;
      rows = GROUP_MAX_ROWS;
    } else {
      // Size matches actual item count (max 6 columns)
      nodesPerRow = Math.min(count, GROUP_MAX_COLS);
      // Cap vertical stacking: never more than GROUP_MAX_ROWS per column.
      rows = Math.min(GROUP_MAX_ROWS, Math.ceil(count / Math.max(1, nodesPerRow)));
    }

    const innerW = nodesPerRow * nodeWidth + Math.max(0, nodesPerRow - 1) * childGapX;
    const innerH = rows * nodeHeight + Math.max(0, rows - 1) * childGapY;
    const w = innerW + GROUP_PADDING_X * 2;
    // Add pagination footer height if multiple pages
    const h = innerH + GROUP_HEADER_HEIGHT + GROUP_PADDING_BOTTOM + (hasPagination ? GROUP_PAGINATION_HEIGHT : 0);
    return { w, h, nodesPerRow, rows, totalPages, hasPagination };
  };

  const positionedGroupNodes: Node<T>[] = [];
  const positionedChildren: Node<T>[] = [];

  // Layout each semantic group at its designated position
  for (const g of groups) {
    const position = SEMANTIC_GROUP_POSITION[g.semanticGroup];
    const dim = groupDims(g.nodes.length);

    let desiredX: number;
    let desiredY: number;
    let preferDown = false;

    switch (position) {
      case 'top':
        // Center horizontally above parent
        desiredX = parentCenterX - dim.w / 2;
        desiredY = parentY - layerSpacing - dim.h;
        preferDown = false;
        break;
      case 'bottom':
        // Center horizontally below parent
        desiredX = parentCenterX - dim.w / 2;
        desiredY = parentY + nodeHeight + layerSpacing;
        preferDown = true;
        break;
      case 'left':
        // Center vertically to the left of parent
        desiredX = parentX - layerSpacing - dim.w;
        desiredY = parentCenterY - dim.h / 2;
        preferDown = true;
        break;
      case 'right':
        // Center vertically to the right of parent
        desiredX = parentX + nodeWidth + layerSpacing;
        desiredY = parentCenterY - dim.h / 2;
        preferDown = true;
        break;
    }

    const pos = findFreeRect(desiredX, desiredY, dim.w, dim.h, preferDown);

    const groupId = defaultGroupId(parentNodeId, g.semanticGroup);
    const allChildIds = g.nodes.map((n) => n.id);
    const currentPage = 0; // Start on first page

    const groupNode: Node<T> = makeGroupParentNode({
      id: groupId,
      position: pos,
      parentId: parentNodeId,
      semanticGroup: g.semanticGroup,
      edgeTypes: g.edgeTypes,
      count: g.nodes.length,
      containerWidth: dim.w,
      containerHeight: dim.h,
      nodeWidth,
      nodeHeight,
      exemplarData: g.nodes[0]?.data,
      allChildIds,
      currentPage,
    });
    positionedGroupNodes.push(groupNode);

    // Layout children inside group with pagination (right-to-left, top-to-bottom)
    const padTop = GROUP_HEADER_HEIGHT;
    const startIdx = currentPage * GROUP_PAGE_SIZE;
    const endIdx = Math.min(startIdx + GROUP_PAGE_SIZE, g.nodes.length);

    for (let idx = 0; idx < g.nodes.length; idx++) {
      const child = g.nodes[idx];
      const isVisible = idx >= startIdx && idx < endIdx;
      // Position relative to current page (visible items only)
      const pageIdx = idx - startIdx;
      const rowIdx = Math.floor(pageIdx / dim.nodesPerRow);
      const colIdx = pageIdx % dim.nodesPerRow;
      // Right-to-left fill: col 0 is the RIGHTMOST slot.
      const rtlColIdx = dim.nodesPerRow - 1 - colIdx;
      const cx = GROUP_PADDING_X + rtlColIdx * (nodeWidth + childGapX);
      const cy = padTop + rowIdx * (nodeHeight + childGapY);

      const positioned = attachChildToGroup(child, groupId, { x: cx, y: cy });
      // Hide nodes that are not on the current page
      positionedChildren.push({
        ...positioned,
        hidden: !isVisible,
      });
    }
  }

  // Ensure group containers render behind fragments (and don't steal clicks).
  // ReactFlow's DOM order matters; groups must be earlier in the `nodes` array.
  const out = [...fixedNodes, ...positionedGroupNodes, ...positionedChildren];
  const groupContainers = out.filter((n) => isGroupNode(n as any));
  const rest = out.filter((n) => !isGroupNode(n as any));
  return [...groupContainers, ...rest];
}

/**
 * Rebuild visual group containers for all parents based on current child positions.
 * Converts groups into true parent containers (`parentId`) and converts child positions to be relative.
 * Does NOT re-layout fragments; only wraps existing positions into containers.
 * Groups by semantic category (supply-chain, customers, market, context).
 */
export function rebuildGroupContainers<T = any>(
  nodes: Node<T>[],
  options: LayoutOptions_Custom = {},
): Node<T>[] {
  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
  } = options;

  const fragments = nodes.filter((n) => !isGroupNode(n as any)).map((n) => {
    // ensure we drop any previous grouping so we can rebuild deterministically
    const out: any = { ...n };
    delete out.parentId;
    delete out.parentNode;
    delete out.extent;
    delete out.expandParent;
    return out as Node<T>;
  });

  // Group by (parentId, semanticGroup)
  type GroupData = {
    parentId: string;
    semanticGroup: SemanticGroup;
    edgeTypes: Set<string>;
    kids: Node<T>[];
  };
  type Key = string;
  const groups = new Map<Key, GroupData>();
  
  for (const n of fragments) {
    const d: any = n.data;
    const parentId = d?.parentFragmentId;
    if (!parentId) continue;
    
    const edgeType = normalizeEdgeType(d?.edgeTypeFromParent);
    const isOutgoing = d?.directionFromParent !== 'incoming';
    const sg = getSemanticGroup(edgeType, isOutgoing);
    
    const key = `${parentId}::${sg}`;
    const existing = groups.get(key);
    if (existing) {
      existing.kids.push(n);
      existing.edgeTypes.add(edgeType);
    } else {
      groups.set(key, { parentId, semanticGroup: sg, edgeTypes: new Set<string>([edgeType]), kids: [n] });
    }
  }

  // Find existing group nodes to preserve current page
  const existingGroups = new Map<string, Node<T>>();
  for (const n of nodes) {
    if (isGroupNode(n as any)) {
      existingGroups.set(n.id, n);
    }
  }

  const groupNodes: Node<T>[] = [];
  const updatedChildren: Node<T>[] = [];

  // Spacing for child layout
  const childGapX = 60; // nodeSpacing
  const childGapY = Math.max(48, Math.floor(60 * 1.05));

  for (const [, g] of groups.entries()) {
    if (g.kids.length === 0) continue;

    const gid = defaultGroupId(g.parentId, g.semanticGroup);

    // Get current page from existing group (preserve pagination state)
    const existingGroup = existingGroups.get(gid);
    const currentPage = (existingGroup?.data as any)?.groupCurrentPage || 0;
    const totalPages = Math.ceil(g.kids.length / GROUP_PAGE_SIZE);
    const hasPagination = totalPages > 1;
    const allChildIds = g.kids.map((k) => k.id);

    // Calculate size based on item count
    // If pagination needed: fixed 6x3 grid
    // If no pagination: size matches actual item count
    let nodesPerRow: number;
    let rows: number;

    if (hasPagination) {
      nodesPerRow = GROUP_MAX_COLS;
      rows = GROUP_MAX_ROWS;
    } else {
      nodesPerRow = Math.min(g.kids.length, GROUP_MAX_COLS);
      // Cap vertical stacking: never more than GROUP_MAX_ROWS per column.
      rows = Math.min(GROUP_MAX_ROWS, Math.ceil(g.kids.length / Math.max(1, nodesPerRow)));
    }

    const innerW = nodesPerRow * nodeWidth + Math.max(0, nodesPerRow - 1) * childGapX;
    const innerH = rows * nodeHeight + Math.max(0, rows - 1) * childGapY;
    const w = innerW + GROUP_PADDING_X * 2;
    const h = innerH + GROUP_HEADER_HEIGHT + GROUP_PADDING_BOTTOM + (hasPagination ? GROUP_PAGINATION_HEIGHT : 0);

    // Find parent node to center groups properly
    const parentNode = fragments.find((n) => n.id === g.parentId || (n.data as any)?.fragmentId === g.parentId);
    const ownerLabel =
      String((parentNode as any)?.data?.label || '').trim() ||
      String((parentNode as any)?.data?.canonicalPath || '').trim().split('/').filter(Boolean).slice(-1)[0] ||
      '';
    const ownerType = String((parentNode as any)?.data?.type || '').trim() || undefined;
    const ownerCanonicalPath = String((parentNode as any)?.data?.canonicalPath || '').trim() || undefined;
    const parentX = parentNode?.position.x ?? 0;
    const parentY = parentNode?.position.y ?? 0;
    const parentCenterX = parentX + nodeWidth / 2;
    const parentCenterY = parentY + nodeHeight / 2;

    // Determine position based on semantic group
    const position = SEMANTIC_GROUP_POSITION[g.semanticGroup];
    const layerSpacing = 150;

    let x: number;
    let y: number;

    const startIdx = currentPage * GROUP_PAGE_SIZE;
    const endIdx = Math.min(startIdx + GROUP_PAGE_SIZE, g.kids.length);

    switch (position) {
      case 'top':
        // Center horizontally above parent
        x = parentCenterX - w / 2;
        y = parentY - layerSpacing - h;
        break;
      case 'bottom':
        // Center horizontally below parent
        x = parentCenterX - w / 2;
        y = parentY + nodeHeight + layerSpacing;
        break;
      case 'left':
        // Center vertically to the left of parent
        x = parentX - layerSpacing - w;
        y = parentCenterY - h / 2;
        break;
      case 'right':
        // Center vertically to the right of parent
        x = parentX + nodeWidth + layerSpacing;
        y = parentCenterY - h / 2;
        break;
      default:
        // Fallback: use first child position
        x = (g.kids[0]?.position.x ?? 0) - GROUP_PADDING_X;
        y = (g.kids[0]?.position.y ?? 0) - GROUP_HEADER_HEIGHT;
    }

    groupNodes.push(
      makeGroupParentNode({
        id: gid,
        position: { x, y },
        parentId: g.parentId,
        ownerLabel,
        ownerType,
        ownerCanonicalPath,
        semanticGroup: g.semanticGroup,
        edgeTypes: Array.from(g.edgeTypes),
        count: g.kids.length,
        containerWidth: w,
        containerHeight: h,
        nodeWidth,
        nodeHeight,
        exemplarData: g.kids[0]?.data,
        allChildIds,
        currentPage,
      }),
    );

    // Layout children with pagination (horizontal-first: left-to-right rows)
    const padTop = GROUP_HEADER_HEIGHT;
    for (let idx = 0; idx < g.kids.length; idx++) {
      const child = g.kids[idx];
      const isVisible = idx >= startIdx && idx < endIdx;
      const pageIdx = idx - startIdx;
      // Horizontal fill:
      // row = floor(index / cols)
      // col = index % cols
      const rowIdx = Math.floor(pageIdx / nodesPerRow);
      const colIdx = pageIdx % nodesPerRow;
      
      const cx = GROUP_PADDING_X + colIdx * (nodeWidth + childGapX);
      const cy = padTop + rowIdx * (nodeHeight + childGapY);

      const positioned = attachChildToGroup(child, gid, { x: cx, y: cy });
      updatedChildren.push({
        ...positioned,
        hidden: !isVisible,
      });
    }
  }

  // Include fragments that weren't part of any group (roots, etc.)
  const groupedChildIds = new Set(updatedChildren.map((n) => n.id));
  const passthrough = fragments.filter((n) => !groupedChildIds.has(n.id));
  // Critical: render group nodes FIRST so they are visually behind and don't block pointer events.
  // Children/roots should sit above group backgrounds.
  return [...groupNodes, ...passthrough, ...updatedChildren];
}

/**
 * Full layout for initial graph load
 * Uses hierarchical layout that respects expansion parent relationships
 */
export async function layoutFullGraph<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  console.log('[ELK] layoutFullGraph called with', nodes.length, 'nodes and', edges.length, 'edges');
  
  // Use hierarchical layout that groups nodes by their expansion parent
  const result = await layoutWithHierarchy(nodes, edges, options);
  console.log('[ELK] layoutFullGraph completed');
  return result;
}

/**
 * Layout nodes respecting the expansion hierarchy (parentFragmentId)
 * Nodes are grouped by their expansion parent and laid out in levels
 */
async function layoutWithHierarchy<T = any>(
  nodes: Node<T>[],
  edges: Edge[],
  options: LayoutOptions_Custom = {},
): Promise<Node<T>[]> {
  if (nodes.length === 0) return nodes;

  const {
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodeSpacing = 60,
    layerSpacing = 150, // Extra space for incoming/outgoing children
    maxNodesPerRow = DEFAULT_MAX_NODES_PER_ROW,
    maxChildRows = MAX_CHILD_ROWS,
  } = options;

  // First pass: build node map
  const nodeMap = new Map<string, Node<T>>();
  for (const node of nodes) {
    nodeMap.set(node.id, node);
  }

  // Second pass: build hierarchy
  const childrenMap = new Map<string, Node<T>[]>(); // parentId -> children
  const rootNodes: Node<T>[] = [];
  const processedIds = new Set<string>();

  for (const node of nodes) {
    const nodeData = node.data as any;
    const parentId = nodeData?.parentFragmentId;
    const depth = nodeData?.depth;
    
    // Node is root if: no parentId, or parentId doesn't exist, or depth=0
    const isRoot = !parentId || !nodeMap.has(parentId) || depth === 0;
    
    if (isRoot) {
      rootNodes.push(node);
      processedIds.add(node.id);
    } else {
      // Add to parent's children
      const siblings = childrenMap.get(parentId) || [];
      siblings.push(node);
      childrenMap.set(parentId, siblings);
      processedIds.add(node.id);
    }
  }

  // Sort children by edge type priority (important = left)
  for (const [parentId, children] of childrenMap.entries()) {
    children.sort((a, b) => {
      const aData = a.data as any;
      const bData = b.data as any;
      const aPriority = getEdgePriority(aData?.edgeTypeFromParent);
      const bPriority = getEdgePriority(bData?.edgeTypeFromParent);
      return aPriority - bPriority;
    });
    childrenMap.set(parentId, children);
  }

  console.log('[ELK] Hierarchy:', {
    roots: rootNodes.length,
    parentsWithChildren: childrenMap.size,
    totalNodes: nodes.length,
  });

  // Calculate subtree widths for proper spacing
  const subtreeWidths = new Map<string, number>();
  
  const calculateSubtreeWidth = (nodeId: string): number => {
    const children = childrenMap.get(nodeId) || [];
    if (children.length === 0) {
      subtreeWidths.set(nodeId, nodeWidth);
      return nodeWidth;
    }
    
    // Width is sum of children's subtree widths + spacing
    // Calculate nodesPerRow to fit within maxChildRows (default 2)
    let totalWidth = 0;
    const nodesPerRow = Math.max(1, Math.ceil(children.length / maxChildRows));
    const numRows = Math.min(maxChildRows, Math.ceil(children.length / nodesPerRow));
    
    // For each row, calculate max width
    for (let row = 0; row < numRows; row++) {
      let rowWidth = 0;
      const startIdx = row * nodesPerRow;
      const endIdx = Math.min(startIdx + nodesPerRow, children.length);
      
      for (let i = startIdx; i < endIdx; i++) {
        const childWidth = calculateSubtreeWidth(children[i].id);
        rowWidth += childWidth + nodeSpacing;
      }
      rowWidth -= nodeSpacing; // Remove last spacing
      totalWidth = Math.max(totalWidth, rowWidth);
    }
    
    // Ensure at least node width
    totalWidth = Math.max(totalWidth, nodeWidth);
    subtreeWidths.set(nodeId, totalWidth);
    return totalWidth;
  };

  // Calculate widths for all roots and their subtrees
  for (const root of rootNodes) {
    calculateSubtreeWidth(root.id);
  }

  // Position nodes
  const positionedNodes = new Map<string, { x: number; y: number }>();
  
  // Layout roots with spacing based on their subtree widths
  let currentX = 0;
  for (const root of rootNodes) {
    const subtreeWidth = subtreeWidths.get(root.id) || nodeWidth;
    const rootX = currentX + subtreeWidth / 2 - nodeWidth / 2;
    const rootPos = { x: rootX, y: 0 };
    positionedNodes.set(root.id, rootPos);
    currentX += subtreeWidth + nodeSpacing * 2; // Extra spacing between subtrees
  }

  // Helper to layout a group of children in rows (max 2 rows)
  const layoutChildGroup = (
    childGroup: Node<T>[],
    parentPos: { x: number; y: number },
    goingUp: boolean, // true = above parent, false = below parent
  ) => {
    if (childGroup.length === 0) return;
    
    const numChildren = childGroup.length;
    // Calculate nodesPerRow to fit within maxChildRows (default 2)
    const nodesPerRow = Math.max(1, Math.ceil(numChildren / maxChildRows));
    const numRows = Math.min(maxChildRows, Math.ceil(numChildren / nodesPerRow));
    const parentCenterX = parentPos.x + nodeWidth / 2;
    
    // Start Y: above or below parent
    const baseY = goingUp
      ? parentPos.y - nodeHeight - layerSpacing
      : parentPos.y + nodeHeight + layerSpacing;
    
    for (let row = 0; row < numRows; row++) {
      const startIdx = row * nodesPerRow;
      const endIdx = Math.min(startIdx + nodesPerRow, numChildren);
      const rowChildren = childGroup.slice(startIdx, endIdx);
      
      // Calculate row width
      let rowWidth = 0;
      for (const child of rowChildren) {
        rowWidth += (subtreeWidths.get(child.id) || nodeWidth) + nodeSpacing;
      }
      rowWidth -= nodeSpacing;
      
      // Start X for this row (centered under/above parent)
      let rowX = parentCenterX - rowWidth / 2;
      
      // Y offset: going up means subtracting, going down means adding
      const rowY = goingUp
        ? baseY - row * (nodeHeight + layerSpacing / 2)
        : baseY + row * (nodeHeight + layerSpacing / 2);
      
      for (const child of rowChildren) {
        const childSubtreeWidth = subtreeWidths.get(child.id) || nodeWidth;
        const childX = rowX + childSubtreeWidth / 2 - nodeWidth / 2;
        const childPos = { x: childX, y: rowY };
        
        positionedNodes.set(child.id, childPos);
        // Recursively layout this child's children
        layoutChildren(child.id, childPos);
        
        rowX += childSubtreeWidth + nodeSpacing;
      }
    }
  };

  // Recursive function to layout children around their parent
  const layoutChildren = (parentId: string, parentPos: { x: number; y: number }) => {
    const children = childrenMap.get(parentId) || [];
    if (children.length === 0) return;

    // Separate children by direction
    const incomingChildren: Node<T>[] = [];
    const outgoingChildren: Node<T>[] = [];
    
    for (const child of children) {
      const childData = child.data as any;
      const direction = childData?.directionFromParent || 'outgoing';
      if (direction === 'incoming') {
        incomingChildren.push(child);
      } else {
        outgoingChildren.push(child);
      }
    }
    
    // Layout incoming children ABOVE parent
    layoutChildGroup(incomingChildren, parentPos, true);
    
    // Layout outgoing children BELOW parent
    layoutChildGroup(outgoingChildren, parentPos, false);
  };

  // Layout children for each root
  for (const root of rootNodes) {
    const rootPos = positionedNodes.get(root.id)!;
    layoutChildren(root.id, rootPos);
  }

  // Apply positions to nodes
  return nodes.map((node) => {
    const pos = positionedNodes.get(node.id);
    if (pos) {
      return { ...node, position: pos };
    }
    // Fallback for unpositioned nodes
    console.warn('[ELK] Node not positioned:', node.id);
    return { ...node, position: { x: 0, y: 0 } };
  });
}

