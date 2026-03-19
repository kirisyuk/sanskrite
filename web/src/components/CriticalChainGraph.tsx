/**
 * CriticalChain Graph — whitepaper 4.5
 * Materials = circles, Facilities = rectangles
 * Edge labels = processing type, Edge thickness = volume (yieldRatio/geographicConcentration)
 */
import React, { useMemo } from 'react';
import ReactFlow, {
  Node,
  Edge,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  Position,
  MarkerType,
  NodeProps,
  EdgeLabelRenderer,
  BaseEdge,
  getSmoothStepPath,
  EdgeProps,
} from 'reactflow';
import 'reactflow/dist/style.css';
import type { CriticalChainGraph as GraphData } from '../lib/criticalchain-api';

const RISK_COLORS: Record<number, string> = {
  1: '#00C853',
  2: '#00C853',
  3: '#FFD700',
  4: '#FF8C00',
  5: '#FF4444',
};

function getRiskColor(score: number): string {
  const level = Math.min(5, Math.max(1, Math.round(score)));
  return RISK_COLORS[level] ?? '#484F58';
}

// Material node = circle (whitepaper 4.5)
function MaterialNode({ data, selected }: NodeProps) {
  const score = (data?.riskScore as number) ?? 1;
  const color = getRiskColor(score);
  return (
    <div
      className={`flex items-center justify-center rounded-full border-2 ${
        selected ? 'border-[#4FC3F7] ring-2 ring-[#4FC3F7]/30' : 'border-[#21262D]'
      }`}
      style={{
        width: 56,
        height: 56,
        backgroundColor: color,
        color: '#0D1117',
      }}
      title={`${data?.label ?? ''} — Risk: ${score.toFixed(1)}/5`}
    >
      <span className="text-[10px] font-medium truncate max-w-[44px] px-1" style={{ fontFamily: 'JetBrains Mono, monospace' }}>
        {String(data?.label ?? '').slice(0, 6)}
      </span>
    </div>
  );
}

// Facility node = rectangle (whitepaper 4.5)
function FacilityNode({ data, selected }: NodeProps) {
  const score = (data?.riskScore as number) ?? 1;
  const color = getRiskColor(score);
  return (
    <div
      className={`flex items-center justify-center rounded px-2 py-1 border-2 ${
        selected ? 'border-[#4FC3F7] ring-2 ring-[#4FC3F7]/30' : 'border-[#21262D]'
      }`}
      style={{
        minWidth: 80,
        maxWidth: 120,
        minHeight: 36,
        backgroundColor: color,
        color: '#0D1117',
      }}
      title={`${data?.label ?? ''} — Risk: ${score.toFixed(1)}/5`}
    >
      <span className="text-[10px] font-medium truncate" style={{ fontFamily: 'JetBrains Mono, monospace' }}>
        {String(data?.label ?? '').slice(0, 14)}
      </span>
    </div>
  );
}

// Edge with label (processing type) and thickness
function LabeledEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  style,
  markerEnd,
}: EdgeProps) {
  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const strokeWidth = Math.max(1.5, Math.min(6, ((data?.weight as number) ?? 1) * 2 + 1));
  return (
    <>
      <BaseEdge
        path={edgePath}
        style={{ ...style, strokeWidth }}
        markerEnd={markerEnd}
      />
      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            pointerEvents: 'all',
          }}
          className="nodrag nopan"
        >
          <div
            className="px-1.5 py-0.5 rounded text-[9px] font-medium"
            style={{
              backgroundColor: '#21262D',
              color: '#8B949E',
              border: '1px solid #484F58',
              fontFamily: 'JetBrains Mono, monospace',
            }}
          >
            {String(data?.label ?? '').toLowerCase().replace(/_/g, ' ')}
          </div>
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

const nodeTypes = { material: MaterialNode, facility: FacilityNode };
const edgeTypes = { labeled: LabeledEdge };

function graphToFlowData(graph: GraphData): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const seen = new Set<string>();
  const edgeKeys = new Set<string>();

  const materialNodes = (graph.materials && graph.materials.length > 0)
    ? [...graph.materials]
    : (graph.material ? [graph.material] : []);

  materialNodes.sort((a, b) => getGraphStage(a.id) - getGraphStage(b.id));

  materialNodes.forEach((material, index) => {
    nodes.push({
      id: material.id,
      type: 'material',
      data: {
        label: material.name,
        riskScore: material.riskScore,
      },
      position: { x: index * 190, y: 32 },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    });
    seen.add(material.id);
  });

  const stageCounts = new Map<number, number>();

  graph.facilities.forEach((f) => {
    if (!seen.has(f.id)) {
      const offset = stageCounts.get(f.stage) ?? 0;
      stageCounts.set(f.stage, offset + 1);
      nodes.push({
        id: f.id,
        type: 'facility',
        data: {
          label: f.name || f.id,
          riskScore: f.riskScore,
        },
        position: {
          x: Math.max(190, (f.stage + 1) * 190),
          y: 132 + offset * 88,
        },
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      });
      seen.add(f.id);
    }
  });

  graph.processingSteps.forEach((step) => {
    const key = `${step.from}-${step.to}`;
    if (seen.has(step.from) && seen.has(step.to) && !edgeKeys.has(key)) {
      edgeKeys.add(key);
      const weight = step.yieldRatio || step.geographicConcentration || 1;
      edges.push({
        id: key,
        source: step.from,
        target: step.to,
        type: 'labeled',
        data: { label: step.type, weight },
        markerEnd: { type: MarkerType.ArrowClosed },
        style: {
          stroke: '#4FC3F7',
          strokeWidth: Math.max(1.5, Math.min(6, weight * 2 + 1)),
        },
      });
    }
  });

  return { nodes, edges };
}

function getGraphStage(id: string): number {
  const match = id.match(/:stage:(\d+)$/);
  if (!match) return -1;
  return Number(match[1]);
}

export const CriticalChainGraph: React.FC<{ graph: GraphData | null }> = ({
  graph,
}) => {
  const { nodes: initialNodes, edges: initialEdges } = useMemo(() => {
    if (!graph) return { nodes: [], edges: [] };
    return graphToFlowData(graph);
  }, [graph]);

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  React.useEffect(() => {
    setNodes(initialNodes);
    setEdges(initialEdges);
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  if (!graph) {
    return (
      <div className="flex items-center justify-center h-full text-[#8B949E] text-sm">
        Select a material to view supply chain graph
      </div>
    );
  }

  return (
    <div className="w-full h-full min-h-[200px]">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        className="bg-[#0D1117]"
      >
        <Controls className="!bg-[#21262D] !border-[#484F58]" />
        <MiniMap
          className="!bg-[#21262D] !border-[#484F58]"
          nodeColor={(n) => getRiskColor((n.data?.riskScore as number) ?? 1)}
        />
      </ReactFlow>
    </div>
  );
};
