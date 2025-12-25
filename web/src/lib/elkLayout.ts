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
const DEFAULT_NODE_HEIGHT = 80;

// Maximum rows for children (2 = spread wider, fewer rows)
const MAX_CHILD_ROWS = 2;

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

// Приоритет типов связей для сортировки (меньше = важнее = левее)
const EDGE_TYPE_PRIORITY: Record<string, number> = {
  'COMPETES_WITH': 1, 'CONFLICTS_WITH': 1, 'OPPOSES': 1,
  'INFLUENCES': 2, 'AFFECTS': 2, 'REGULATES': 2, 'IMPACTS': 2,
  'OWNS': 3, 'MANAGES': 3, 'CONTROLS': 3, 'PARENT_OF': 3, 'SUBSIDIARY_OF': 3,
  'PARTNERS_WITH': 4, 'COOPERATES_WITH': 4, 'SUPPLIES_TO': 4, 'SUPPORTS': 4,
  'LOCATED_IN': 5, 'OPERATES_IN': 5, 'BASED_IN': 5, 'HEADQUARTERED_IN': 5,
  'DEPENDS_ON': 6, 'REQUIRES': 6, 'USES': 6, 'NEEDS': 6,
};
const DEFAULT_EDGE_PRIORITY = 7;

const getEdgePriority = (edgeType: string | undefined): number => {
  const type = (edgeType || '').toUpperCase();
  return EDGE_TYPE_PRIORITY[type] ?? DEFAULT_EDGE_PRIORITY;
};

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
  /** Maximum number of rows for children (default 2) */
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

