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
const DEFAULT_NODE_WIDTH = 180;
// Layout height includes the "+" button protruding half outside the visual card (26px button => +13px).
const DEFAULT_NODE_HEIGHT = 161;

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
  // 1) Ownership / control (strongest)
  'OWNS': 1,
  'PARENT_OF': 1,
  'SUBSIDIARY_OF': 1,
  'MANAGES': 1,
  'CONTROLS': 1,

  // 2) Products & dependency (inputs/outputs)
  'PRODUCES': 2,
  'SUPPLIES_TO': 2,
  'CONSUMES': 2,
  'DEPENDS_ON': 2,
  'REQUIRES': 2,
  'USES': 2,
  'NEEDS': 2,

  // 3) Market (peers)
  'COMPETES_WITH': 3,
  'CONFLICTS_WITH': 3,
  'OPPOSES': 3,
  'PARTNERS_WITH': 3,
  'COOPERATES_WITH': 3,

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

const normalizeEdgeType = (t: string | undefined) => (t || '').toUpperCase().trim() || 'OTHER';

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
    // Supply Chain vs Customers depends on direction
    case 'SUPPLIES_TO':
      return isOutgoing ? 'customers' : 'supply-chain';  // outgoing = we supply to them (customer), incoming = they supply to us (supplier)
    case 'DEPENDS_ON':
      return isOutgoing ? 'supply-chain' : 'customers';  // outgoing = we depend on them (upstream), incoming = they depend on us (downstream)
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
    
    // Production always downstream
    case 'PRODUCES':
      return 'customers';
    
    // Symmetric / Peers — market position
    case 'COMPETES_WITH':
    case 'CONFLICTS_WITH':
    case 'OPPOSES':
    case 'PARTNERS_WITH':
    case 'COOPERATES_WITH':
    case 'RELATED_TO':
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

  // Direction-agnostic relations: direction does NOT define top/bottom.
  // These are typically "peer" or symmetric relations.
  const SYMMETRIC = new Set<string>([
    'COMPETES_WITH',
    'CONFLICTS_WITH',
    'OPPOSES',
    'PARTNERS_WITH',
    'COOPERATES_WITH',
    'RELATED_TO',
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
    // Products / supply / dependencies (inputs+outputs)
    'PRODUCES',
    'SUPPLIES_TO',
    'CONSUMES',
    'DEPENDS_ON',
    'REQUIRES',
    'USES',
    'NEEDS',
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
      rows = Math.ceil(count / nodesPerRow);
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
      rows = Math.ceil(g.kids.length / nodesPerRow);
    }

    const innerW = nodesPerRow * nodeWidth + Math.max(0, nodesPerRow - 1) * childGapX;
    const innerH = rows * nodeHeight + Math.max(0, rows - 1) * childGapY;
    const w = innerW + GROUP_PADDING_X * 2;
    const h = innerH + GROUP_HEADER_HEIGHT + GROUP_PADDING_BOTTOM + (hasPagination ? GROUP_PAGINATION_HEIGHT : 0);

    // Find parent node to center groups properly
    const parentNode = fragments.find((n) => n.id === g.parentId || (n.data as any)?.fragmentId === g.parentId);
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

