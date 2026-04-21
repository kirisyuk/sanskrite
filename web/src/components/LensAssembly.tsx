import React from 'react';
import ReactFlow, {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  type Edge,
  type Node as FlowNode,
  type NodeProps,
  type ReactFlowInstance,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { ThemeContext } from './ThemeProvider';
import type { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';
import {
  api,
  type HorizonProductProducer,
  type HorizonProductProducersResponse,
  type HorizonProductPrefetchSuggestion,
  type HorizonProductStreamEdge,
  type HorizonProductStreamNode,
  type HorizonProductStreamResponse,
} from '../lib/api';
import { productStreamEdgeVisualKind } from '../lib/taxonomy';

type AssemblyMode = 'freeform' | 'product' | 'entity';

interface LensAssemblyProps {
  lens: any;
  projectId?: string | null;
  onApplyScope: (nextScope: string[]) => Promise<void> | void;
  onOpenGraph: () => void;
  onOpenMap: () => void;
  onAssemble?: (request: AssembleLensRequest) => void;
}

type SelectedProduct = {
  canonicalPath: string;
  label: string;
  sourceScope?: string;
};

type ProductStreamPair = {
  upstream: HorizonProductStreamResponse | null;
  downstream: HorizonProductStreamResponse | null;
};

type ProductGraphNodeData = {
  id: string;
  label: string;
  kind: 'product' | 'organization' | 'facility' | 'deposit' | 'program';
  active: boolean;
  center: boolean;
  onSelect?: (path: string) => void;
};

const MAX_HOPS = 3;
const MAX_PER_HOP = 7;
const HOP_X_GAP = 320;
const NODE_Y_GAP = 104;

const productIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M16.5 9.4 7.55 4.24" />
    <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
    <polyline points="3.29 7 12 12 20.71 7" />
    <line x1="12" y1="22" x2="12" y2="12" />
  </svg>
);

const assemblyModeIcons: Record<AssemblyMode, React.ReactNode> = {
  freeform: (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 7h16" />
      <path d="M7 12h10" />
      <path d="M10 17h4" />
    </svg>
  ),
  product: (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3 4 7v10l8 4 8-4V7l-8-4Z" />
      <path d="m4 7 8 4 8-4" />
      <path d="v10" />
    </svg>
  ),
  entity: (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 21h18" />
      <path d="M5 21V7l7-4 7 4v14" />
      <path d="M9 11h.01" />
      <path d="M15 11h.01" />
      <path d="M9 15h.01" />
      <path d="M15 15h.01" />
    </svg>
  ),
};

const organizationIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M3 21h18" />
    <path d="M5 21V7l7-4 7 4v14" />
    <path d="M9 11h.01" />
    <path d="M15 11h.01" />
    <path d="M9 15h.01" />
    <path d="M15 15h.01" />
  </svg>
);

const facilityIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M2 20h20" />
    <path d="M5 20V8l6-3v15" />
    <path d="M11 12h5l2 2v6" />
    <path d="M7 11h.01" />
    <path d="M7 15h.01" />
    <path d="M14 15h.01" />
  </svg>
);

const depositIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M3 20h18" />
    <path d="M5 20 10 8l4 6 2-3 3 9" />
  </svg>
);

const programIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="4" y="4" width="16" height="16" rx="2" />
    <path d="M9 9h6" />
    <path d="M9 13h6" />
    <path d="M9 17h3" />
  </svg>
);

const producerKindFromType = (fragmentType?: string): ProductGraphNodeData['kind'] => {
  switch ((fragmentType || '').toUpperCase()) {
    case 'FACILITY':
      return 'facility';
    case 'ORGANIZATION':
      return 'organization';
    case 'deposit':
      return 'deposit';
    case 'PROGRAM':
      return 'program';
    default:
      return 'organization';
  }
};

const iconForNodeKind = (kind: ProductGraphNodeData['kind']) => {
  switch (kind) {
    case 'facility':
      return facilityIcon;
    case 'organization':
      return organizationIcon;
    case 'deposit':
      return depositIcon;
    case 'program':
      return programIcon;
    default:
      return productIcon;
  }
};

const byHop = (nodes: HorizonProductStreamNode[] | undefined, maxHops: number) => {
  const out = new Map<number, HorizonProductStreamNode[]>();
  for (let hop = 1; hop <= maxHops; hop += 1) {
    out.set(hop, []);
  }
  for (const node of nodes || []) {
    if (!node || node.hop <= 0) continue;
    const bucket = out.get(node.hop);
    if (!bucket) continue;
    bucket.push(node);
  }
  for (const [hop, bucket] of out.entries()) {
    bucket.sort((a, b) => (a.label || a.canonicalPath).localeCompare(b.label || b.canonicalPath));
    out.set(hop, bucket);
  }
  return out;
};

const productLabelFromPath = (path: string) => {
  const segment = path.split('/').filter(Boolean).slice(-1)[0] || path;
  return segment
    .split('-')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
};

const ProductGraphNode: React.FC<NodeProps<ProductGraphNodeData>> = ({ data }) => (
  <button
    type="button"
    onClick={() => data.onSelect?.(data.id)}
    style={{
      minWidth: data.kind === 'product' ? 170 : 150,
      maxWidth: data.kind === 'product' ? 190 : 170,
      textAlign: 'left',
      border: data.center ? '1px solid rgba(245,158,11,0.7)' : (data.active ? '1px solid rgba(59,130,246,0.7)' : '1px solid var(--border-subtle)'),
      background: data.center
        ? 'rgba(30,41,59,0.78)'
        : (data.active ? 'rgba(59,130,246,0.12)' : 'rgba(17,17,19,0.74)'),
      backdropFilter: 'blur(4px)',
      borderRadius: 14,
      padding: '10px 12px',
      color: 'var(--text-heading)',
      boxShadow: data.center ? '0 10px 30px rgba(0,0,0,0.24)' : '0 8px 24px rgba(0,0,0,0.18)',
      cursor: data.onSelect ? 'pointer' : 'default',
    }}
  >
    <Handle type="target" position={Position.Top} id="target-top" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="source" position={Position.Top} id="source-top" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="target" position={Position.Bottom} id="target-bottom" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="source" position={Position.Bottom} id="source-bottom" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="target" position={Position.Left} id="target-left" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="target" position={Position.Right} id="target-right" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="source" position={Position.Left} id="source-left" style={{ opacity: 0, width: 8, height: 8 }} />
    <Handle type="source" position={Position.Right} id="source-right" style={{ opacity: 0, width: 8, height: 8 }} />
    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
      <span style={{ display: 'inline-flex', color: 'var(--text-secondary-strong)', flexShrink: 0 }}>{iconForNodeKind(data.kind)}</span>
      <div style={{ fontSize: 13, fontWeight: 700, lineHeight: 1.3, color: 'var(--text-heading)' }}>
        {data.label}
      </div>
    </div>
    {data.center && (
      <div style={{ marginTop: 6, fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
        Selected
      </div>
    )}
    {!data.center && data.kind !== 'product' && (
      <div style={{ marginTop: 6, fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
        {data.kind}
      </div>
    )}
    {!data.center && data.kind === 'product' && (
      <div style={{ marginTop: 6, fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
        Product
      </div>
    )}
  </button>
);

const productGraphNodeTypes = { productAssembly: ProductGraphNode };

export const LensAssembly: React.FC<LensAssemblyProps> = ({
  lens,
  projectId,
  onApplyScope,
  onOpenGraph,
  onOpenMap,
  onAssemble,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [query, setQuery] = React.useState('');
  const deferredQuery = React.useDeferredValue(query);
  const [suggestions, setSuggestions] = React.useState<HorizonProductPrefetchSuggestion[]>([]);
  const [loadingSuggestions, setLoadingSuggestions] = React.useState(false);
  const [showSuggestions, setShowSuggestions] = React.useState(false);
  const [selectedProducts, setSelectedProducts] = React.useState<SelectedProduct[]>([]);
  const [activePath, setActivePath] = React.useState<string | null>(null);
  const [streamCache, setStreamCache] = React.useState<Record<string, ProductStreamPair>>({});
  const [producerCache, setProducerCache] = React.useState<Record<string, HorizonProductProducersResponse>>({});
  const [loadingStream, setLoadingStream] = React.useState(false);
  const [streamError, setStreamError] = React.useState<string | null>(null);
  const [loadingProducers, setLoadingProducers] = React.useState(false);
  const [producerError, setProducerError] = React.useState<string | null>(null);
  const [applyState, setApplyState] = React.useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
  const [applyError, setApplyError] = React.useState<string | null>(null);
  const inputRef = React.useRef<HTMLInputElement | null>(null);
  const dropdownAbortRef = React.useRef<AbortController | null>(null);
  const productRootRef = React.useRef<HTMLDivElement | null>(null);
  const reactFlowRef = React.useRef<ReactFlowInstance | null>(null);
  const activeMode: AssemblyMode = 'product';

  const scopePaths = React.useMemo(
    () => new Set(((lens?.scope as string[] | undefined) || []).map((item) => String(item || '').trim()).filter(Boolean)),
    [lens?.scope],
  );
  const hasSelection = selectedProducts.length > 0;

  const activeStream = activePath ? streamCache[activePath] || null : null;
  const activeProducers = activePath ? producerCache[activePath] || null : null;
  const upstreamByHop = React.useMemo(() => byHop(activeStream?.upstream?.nodes, MAX_HOPS), [activeStream?.upstream]);
  const downstreamByHop = React.useMemo(() => byHop(activeStream?.downstream?.nodes, MAX_HOPS), [activeStream?.downstream]);
  const activeLabel = React.useMemo(() => {
    const chip = selectedProducts.find((item) => item.canonicalPath === activePath);
    if (chip?.label) return chip.label;
    return activeStream?.downstream?.resolvedLabel || activeStream?.upstream?.resolvedLabel || '';
  }, [activePath, activeStream, selectedProducts]);

  const productGraph = React.useMemo(() => {
    if (!activePath) return { nodes: [] as FlowNode<ProductGraphNodeData>[], edges: [] as Edge[] };

    const pathToLabel = new Map<string, string>();
    pathToLabel.set(activePath, activeLabel || productLabelFromPath(activePath));
    for (const node of activeStream?.upstream?.nodes || []) {
      pathToLabel.set(node.canonicalPath, node.label || productLabelFromPath(node.canonicalPath));
    }
    for (const node of activeStream?.downstream?.nodes || []) {
      pathToLabel.set(node.canonicalPath, node.label || productLabelFromPath(node.canonicalPath));
    }

    const positionsById = new Map<string, { x: number; y: number }>();
    positionsById.set(activePath, { x: 0, y: 0 });

    const nodes: FlowNode<ProductGraphNodeData>[] = [
      {
        id: activePath,
        type: 'productAssembly',
        position: { x: 0, y: 0 },
        draggable: false,
        data: {
          id: activePath,
          label: pathToLabel.get(activePath) || productLabelFromPath(activePath),
          kind: 'product',
          active: true,
          center: true,
          onSelect: setActivePath,
        },
      },
    ];

    const placeHopNodes = (hop: number, side: 'upstream' | 'downstream', items: HorizonProductStreamNode[]) => {
      const count = items.length;
      const totalHeight = Math.max(0, (count - 1) * NODE_Y_GAP);
      const startY = -totalHeight / 2;
      items.forEach((node, index) => {
        const position = {
          x: side === 'upstream' ? hop * HOP_X_GAP : -hop * HOP_X_GAP,
          y: startY + index * NODE_Y_GAP,
        };
        positionsById.set(node.canonicalPath, position);
        nodes.push({
          id: node.canonicalPath,
          type: 'productAssembly',
          position,
          draggable: false,
          data: {
            id: node.canonicalPath,
            label: pathToLabel.get(node.canonicalPath) || productLabelFromPath(node.canonicalPath),
            kind: 'product',
            active: node.canonicalPath === activePath,
            center: false,
            onSelect: setActivePath,
          },
        });
      });
    };

    for (let hop = 1; hop <= MAX_HOPS; hop += 1) {
      placeHopNodes(hop, 'downstream', downstreamByHop.get(hop) || []);
      placeHopNodes(hop, 'upstream', upstreamByHop.get(hop) || []);
    }

    const producers = (activeProducers?.producers || []).slice(0, MAX_PER_HOP);
    if (producers.length > 0) {
      const producerY = Array.from(positionsById.values()).reduce((maxY, position) => Math.max(maxY, position.y), 0) + 220;
      const actorGap = 220;
      const startX = -((producers.length - 1) * actorGap) / 2;
      producers.forEach((producer: HorizonProductProducer, index: number) => {
        const producerId = `producer:${producer.fragmentId || producer.canonicalPath || String(index)}`;
        positionsById.set(producerId, { x: startX + index * actorGap, y: producerY });
        nodes.push({
          id: producerId,
          type: 'productAssembly',
          position: { x: startX + index * actorGap, y: producerY },
          draggable: false,
          data: {
            id: producerId,
            label: producer.label || productLabelFromPath(producer.canonicalPath || producer.fragmentId || producerId),
            kind: producerKindFromType(producer.fragmentType),
            active: false,
            center: false,
          },
        });
      });
    }

    const pushEdge = (edge: HorizonProductStreamEdge, source: string, target: string) => {
      const visual = productStreamEdgeVisualKind(edge.relationKind);
      const stroke =
        visual === 'assembly' ? '#f59e0b' : visual === 'usage' ? '#3b82f6' : '#10b981';
      const strokeWidth = visual === 'assembly' ? 1.8 : visual === 'usage' ? 1.65 : 1.6;
      const sourcePosition = positionsById.get(source);
      const targetPosition = positionsById.get(target);
      if (!sourcePosition || !targetPosition) return;
      const sourceOnRight = sourcePosition.x < targetPosition.x;
      productEdges.push({
        id: `${source}->${target}:${edge.relationKind || 'transformation'}`,
        source,
        target,
        sourceHandle: sourceOnRight ? 'source-right' : 'source-left',
        targetHandle: sourceOnRight ? 'target-left' : 'target-right',
        type: 'default',
        animated: false,
        interactionWidth: 18,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: stroke,
        },
        style: {
          stroke,
          strokeWidth,
          opacity: 0.9,
        },
      });
    };

    const productEdges: Edge[] = [];
    for (const edge of activeStream?.downstream?.edges || []) {
      pushEdge(edge, edge.fromPath, edge.toPath);
    }
    for (const edge of activeStream?.upstream?.edges || []) {
      pushEdge(edge, edge.fromPath, edge.toPath);
    }
    for (const [index, producer] of (activeProducers?.producers || []).slice(0, MAX_PER_HOP).entries()) {
      const producerId = `producer:${producer.fragmentId || producer.canonicalPath || String(index)}`;
      if (!positionsById.has(producerId) || !positionsById.has(activePath)) continue;
      productEdges.push({
        id: `${producerId}->${activePath}:produces`,
        source: producerId,
        target: activePath,
        sourceHandle: 'source-top',
        targetHandle: 'target-bottom',
        type: 'default',
        animated: false,
        interactionWidth: 18,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: '#94a3b8',
        },
        style: {
          stroke: '#94a3b8',
          strokeWidth: 1.45,
          opacity: 0.84,
        },
      });
    }

    return { nodes, edges: productEdges };
  }, [activeLabel, activePath, activeProducers, activeStream, downstreamByHop, upstreamByHop]);

  React.useEffect(() => {
    if (!hasSelection || !reactFlowRef.current || productGraph.nodes.length === 0) return;
    const handle = window.requestAnimationFrame(() => {
      reactFlowRef.current?.fitView({ padding: 0.22, duration: 300 });
    });
    return () => window.cancelAnimationFrame(handle);
  }, [hasSelection, productGraph]);

  React.useEffect(() => {
    const q = deferredQuery.trim();
    if (q.length < 2) {
      setSuggestions([]);
      setShowSuggestions(false);
      setLoadingSuggestions(false);
      try { dropdownAbortRef.current?.abort(); } catch {}
      return;
    }
    const handle = window.setTimeout(async () => {
      try {
        try { dropdownAbortRef.current?.abort(); } catch {}
        const abort = new AbortController();
        dropdownAbortRef.current = abort;
        setLoadingSuggestions(true);
        const response = await api.prefetchHorizonProducts({ query: q, limit: 8 }, abort.signal);
        const selected = new Set(selectedProducts.map((item) => item.canonicalPath));
        setSuggestions((response.suggestions || []).filter((item) => !selected.has(item.canonicalPath)));
        setShowSuggestions(true);
      } catch (error: any) {
        if (error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') return;
        setSuggestions([]);
        setShowSuggestions(false);
      } finally {
        setLoadingSuggestions(false);
      }
    }, 160);
    return () => window.clearTimeout(handle);
  }, [deferredQuery, selectedProducts]);

  React.useEffect(() => {
    const onDown = (event: MouseEvent) => {
      const root = productRootRef.current;
      if (!root || root.contains(event.target as Node)) return;
      setShowSuggestions(false);
    };
    window.addEventListener('mousedown', onDown);
    return () => window.removeEventListener('mousedown', onDown);
  }, []);

  React.useEffect(() => {
    if (selectedProducts.length === 0) {
      if (activePath !== null) setActivePath(null);
      return;
    }
    if (!activePath || !selectedProducts.some((item) => item.canonicalPath === activePath)) {
      setActivePath(selectedProducts[selectedProducts.length - 1].canonicalPath);
    }
  }, [activePath, selectedProducts]);

  React.useEffect(() => {
    if (!activePath) return;
    if (streamCache[activePath]?.upstream && streamCache[activePath]?.downstream) return;
    let cancelled = false;
    const load = async () => {
      setLoadingStream(true);
      setStreamError(null);
      try {
        const [upstream, downstream] = await Promise.all([
          api.getHorizonProductStream({ productRef: activePath, direction: 'upstream', hops: MAX_HOPS, limitPerHop: MAX_PER_HOP }),
          api.getHorizonProductStream({ productRef: activePath, direction: 'downstream', hops: MAX_HOPS, limitPerHop: MAX_PER_HOP }),
        ]);
        if (cancelled) return;
        setStreamCache((prev) => ({
          ...prev,
          [activePath]: { upstream, downstream },
        }));
      } catch (error: any) {
        if (cancelled) return;
        setStreamError(error?.response?.data?.error || error?.message || 'Failed to load product stream');
      } finally {
        if (!cancelled) setLoadingStream(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [activePath, streamCache]);

  React.useEffect(() => {
    if (!activePath) return;
    if (producerCache[activePath]?.resolvedPath) return;
    let cancelled = false;
    const load = async () => {
      setLoadingProducers(true);
      setProducerError(null);
      try {
        const producers = await api.getHorizonProductProducers({ productRef: activePath, limit: MAX_PER_HOP });
        if (cancelled) return;
        setProducerCache((prev) => ({
          ...prev,
          [activePath]: producers,
        }));
      } catch (error: any) {
        if (cancelled) return;
        setProducerError(error?.response?.data?.error || error?.message || 'Failed to load product producers');
      } finally {
        if (!cancelled) setLoadingProducers(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [activePath, producerCache]);

  const addProduct = React.useCallback((suggestion: HorizonProductPrefetchSuggestion) => {
    const canonicalPath = String(suggestion.canonicalPath || '').trim();
    if (!canonicalPath) return;
    setSelectedProducts((prev) => {
      if (prev.some((item) => item.canonicalPath === canonicalPath)) return prev;
      return [
        ...prev,
        {
          canonicalPath,
          label: suggestion.label || suggestion.matchedLabel || canonicalPath.split('/').filter(Boolean).slice(-1)[0] || canonicalPath,
          sourceScope: suggestion.sourceScope,
        },
      ];
    });
    setActivePath(canonicalPath);
    setQuery('');
    setShowSuggestions(false);
    setApplyState('idle');
    setApplyError(null);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, []);

  const removeProduct = React.useCallback((canonicalPath: string) => {
    setSelectedProducts((prev) => prev.filter((item) => item.canonicalPath !== canonicalPath));
    setApplyState('idle');
    setApplyError(null);
  }, []);

  const handleApply = React.useCallback(async () => {
    const selectedPaths = selectedProducts.map((item) => item.canonicalPath).filter(Boolean);
    if (selectedPaths.length === 0) return;
    const nextScope = Array.from(new Set([...(lens?.scope || []), ...selectedPaths]));
    setApplyState('saving');
    setApplyError(null);
    try {
      await onApplyScope(nextScope);
      setApplyState('saved');
    } catch (error: any) {
      setApplyState('error');
      setApplyError(error?.response?.data?.error || error?.message || 'Failed to apply scope');
    }
  }, [lens?.scope, onApplyScope, selectedProducts]);

  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        padding: '72px 20px 20px',
      }}
    >
      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: hasSelection ? 'flex-start' : 'center',
          gap: hasSelection ? 24 : 20,
          transition: 'all 240ms ease',
          overflow: 'hidden',
        }}
      >
        <div
          ref={productRootRef}
          style={{
            position: 'relative',
            width: '100%',
            maxWidth: 920,
            transform: hasSelection ? 'translateY(0)' : 'translateY(-14px)',
            transition: 'transform 240ms ease, max-width 240ms ease',
            flexShrink: 0,
          }}
        >
          <div
            style={{
              borderRadius: 18,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: '0 20px 70px rgba(0,0,0,0.45)',
              overflow: 'hidden',
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'flex-start',
                gap: 12,
                padding: '14px 16px',
                borderBottom: '1px solid var(--border-subtle)',
              }}
            >
              <div
                style={{
                  marginTop: 3,
                  color: 'var(--text-secondary-strong)',
                  display: 'flex',
                  flexShrink: 0,
                }}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="11" cy="11" r="8" />
                  <path d="m21 21-4.3-4.3" />
                </svg>
              </div>
              <div
                style={{
                  flex: 1,
                  minWidth: 0,
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'center',
                  gap: 8,
                  minHeight: 32,
                }}
              >
                {selectedProducts.map((item) => {
                  const inScope = scopePaths.has(item.canonicalPath);
                  const active = activePath === item.canonicalPath;
                  return (
                    <button
                      key={item.canonicalPath}
                      type="button"
                      onClick={() => setActivePath(item.canonicalPath)}
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 8,
                        maxWidth: '100%',
                        borderRadius: 999,
                        border: active ? '1px solid var(--accent-primary)' : '1px solid var(--border-subtle)',
                        background: active
                          ? (theme === 'dark' ? 'rgba(59,130,246,0.14)' : 'rgba(59,130,246,0.10)')
                          : (theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(15,23,42,0.04)'),
                        color: 'var(--text-heading)',
                        padding: '7px 11px',
                        cursor: 'pointer',
                      }}
                    >
                      <span style={{ fontSize: 12, fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {item.label}
                      </span>
                      {inScope && <span style={{ fontSize: 10, color: 'var(--text-tertiary)' }}>in scope</span>}
                      <span
                        onClick={(event) => {
                          event.stopPropagation();
                          removeProduct(item.canonicalPath);
                        }}
                        style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}
                      >
                        ×
                      </span>
                    </button>
                  );
                })}

                <input
                  ref={inputRef}
                  type="text"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                      event.preventDefault();
                      setShowSuggestions(false);
                      return;
                    }
                    if (event.key === 'Enter' && suggestions.length > 0) {
                      event.preventDefault();
                      addProduct(suggestions[0]);
                    }
                  }}
                  onFocus={() => {
                    if (suggestions.length > 0) setShowSuggestions(true);
                  }}
                  placeholder="Search products by canonical name or synonym"
                  style={{
                    flex: 1,
                    minWidth: 220,
                    border: 'none',
                    outline: 'none',
                    background: 'transparent',
                    fontSize: 16,
                    fontWeight: 500,
                    color: 'var(--text-heading)',
                  }}
                />
              </div>

              {loadingSuggestions && (
                <span style={{ marginTop: 3, fontSize: 12, color: 'var(--text-tertiary)', flexShrink: 0 }}>
                  Loading…
                </span>
              )}
            </div>

            <div
              style={{
                display: 'flex',
                justifyContent: 'flex-start',
                padding: '12px 16px 14px',
              }}
            >
              <div
                role="tablist"
                aria-label="Assembly mode"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 4,
                  padding: 0,
                  borderRadius: 12,
                  background: 'transparent',
                }}
              >
                {[
                  { key: 'freeform', label: 'Freeform', disabled: true },
                  { key: 'product', label: 'By Product', disabled: false },
                  { key: 'entity', label: 'By Entity', disabled: true },
                ].map((item) => {
                  const active = activeMode === item.key;
                  return (
                    <button
                      key={item.key}
                      type="button"
                      aria-pressed={active}
                      disabled={item.disabled}
                      style={{
                        border: 'none',
                        borderRadius: 10,
                        padding: '9px 12px',
                        background: active ? (theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(0,0,0,0.08)') : 'transparent',
                        color: item.disabled ? 'var(--text-disabled)' : 'var(--text-heading)',
                        fontSize: 12,
                        fontWeight: active ? 700 : 600,
                        cursor: item.disabled ? 'not-allowed' : 'default',
                        opacity: item.disabled ? 0.56 : 1,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 8,
                      }}
                      title={item.disabled ? 'Coming next' : item.label}
                    >
                      <span style={{ display: 'inline-flex', color: item.disabled ? 'var(--text-disabled)' : (active ? 'var(--text-heading)' : 'var(--text-secondary-strong)') }}>
                        {assemblyModeIcons[item.key as AssemblyMode]}
                      </span>
                      {item.label}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>

          {showSuggestions && suggestions.length > 0 && (
            <div
              style={{
                position: 'absolute',
                top: '100%',
                left: 0,
                right: 0,
                marginTop: 12,
                borderRadius: 16,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                boxShadow: '0 20px 60px rgba(0,0,0,0.45)',
                overflow: 'hidden',
                zIndex: 30,
              }}
            >
              <div style={{ maxHeight: 'min(420px, 44vh)', overflowY: 'auto', padding: '8px 0' }}>
                {suggestions.map((item) => (
                  <button
                    key={item.canonicalPath}
                    type="button"
                    onClick={() => addProduct(item)}
                    style={{
                      width: '100%',
                      textAlign: 'left',
                      border: 'none',
                      background: 'transparent',
                      cursor: 'pointer',
                      padding: '11px 16px',
                      display: 'flex',
                      justifyContent: 'space-between',
                      gap: 14,
                      transition: 'background 0.12s ease',
                    }}
                    onMouseEnter={(event) => {
                      event.currentTarget.style.background = theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)';
                    }}
                    onMouseLeave={(event) => { event.currentTarget.style.background = 'transparent'; }}
                  >
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-heading)' }}>{item.label}</div>
                      <div style={{ marginTop: 2, fontSize: 12, color: 'var(--text-tertiary)', wordBreak: 'break-all' }}>{item.canonicalPath}</div>
                      {(item.matchedLabel || item.canonicalizedFrom) && (
                        <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                          matched: {item.matchedLabel || item.canonicalizedFrom}
                        </div>
                      )}
                    </div>
                    <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
                      {item.sourceScope || 'global'}
                    </div>
                  </button>
                ))}
              </div>
              <div
                style={{
                  padding: '8px 12px',
                  borderTop: '1px solid var(--border-subtle)',
                  background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  fontSize: 11,
                  color: 'var(--text-secondary-strong)',
                }}
              >
                <div>{suggestions.length} match{suggestions.length === 1 ? '' : 'es'}</div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    <kbd style={{ background: 'var(--bg-base)', border: '1px solid var(--border-subtle)', borderRadius: 4, padding: '1px 4px', fontSize: 10 }}>↵</kbd>
                    <span>to add</span>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    <kbd style={{ background: 'var(--bg-base)', border: '1px solid var(--border-subtle)', borderRadius: 4, padding: '1px 4px', fontSize: 10 }}>esc</kbd>
                    <span>to close</span>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>

        {!hasSelection && (
          <div
            style={{
              width: '100%',
              maxWidth: 540,
              textAlign: 'center',
              color: 'var(--text-secondary-strong)',
            }}
          >
            <div style={{ fontSize: 15, fontWeight: 800, color: 'var(--text-heading)' }}>
              Start with a product
            </div>
            <div style={{ marginTop: 8, fontSize: 13 }}>
              Pick a product to explore its chain.
            </div>
          </div>
        )}

        {hasSelection && (
          <div
            style={{
              width: '100%',
              flex: 1,
              minHeight: 0,
              display: 'flex',
              flexDirection: 'column',
              gap: 10,
            }}
          >
            <div
              style={{
                flex: 1,
                minHeight: 420,
                display: 'flex',
                flexDirection: 'column',
                gap: 12,
                position: 'relative',
                overflow: 'hidden',
              }}
            >
              {(streamError || loadingStream || producerError || loadingProducers) && (
                <div
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    zIndex: 5,
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 8,
                    padding: '8px 10px',
                    borderRadius: 12,
                    background: theme === 'dark' ? 'rgba(15,15,18,0.72)' : 'rgba(255,255,255,0.82)',
                    backdropFilter: 'blur(8px)',
                    border: '1px solid var(--border-subtle)',
                    fontSize: 12,
                    color: (streamError || producerError) ? '#ef4444' : 'var(--text-secondary-strong)',
                  }}
                >
                  {streamError || producerError || (loadingProducers ? 'Loading exact producers…' : 'Loading product graph…')}
                </div>
              )}

              {activePath && (
                <div
                  style={{
                    flex: 1,
                    minHeight: 0,
                    borderRadius: 18,
                    background: 'transparent',
                    overflow: 'hidden',
                  }}
                >
                  <ReactFlow
                    nodes={productGraph.nodes}
                    edges={productGraph.edges}
                    nodeTypes={productGraphNodeTypes}
                    onInit={(instance) => { reactFlowRef.current = instance; }}
                    fitView
                    nodesDraggable={false}
                    nodesConnectable={false}
                    elementsSelectable={false}
                    panOnDrag
                    zoomOnScroll
                    zoomOnPinch
                    zoomOnDoubleClick={false}
                    proOptions={{ hideAttribution: true }}
                    style={{ background: 'transparent' }}
                    defaultEdgeOptions={{
                      type: 'default',
                    }}
                  >
                    <Controls
                      position="bottom-left"
                      showInteractive={false}
                      style={{
                        background: theme === 'dark' ? 'rgba(17,17,19,0.72)' : 'rgba(255,255,255,0.84)',
                        border: '1px solid var(--border-subtle)',
                        borderRadius: 12,
                        backdropFilter: 'blur(8px)',
                      }}
                    />
                    <Background color="transparent" gap={24} />
                  </ReactFlow>
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {hasSelection && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 12,
            flexWrap: 'wrap',
            padding: 14,
            marginTop: 12,
            borderRadius: 16,
            border: '1px solid var(--border-subtle)',
            background: 'var(--bg-elevated)',
            flexShrink: 0,
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
              {selectedProducts.length} selected product{selectedProducts.length === 1 ? '' : 's'}
            </div>
            <div style={{ fontSize: 12, color: applyState === 'error' ? '#ef4444' : 'var(--text-tertiary)' }}>
              {applyState === 'saved'
                ? 'Scope updated. Stay here or move to Graph/Map.'
                : applyError || 'Apply adds only the selected canonical chips to lens scope.'}
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            <button
              type="button"
              onClick={() => {
                setSelectedProducts([]);
                setActivePath(null);
                setApplyState('idle');
                setApplyError(null);
              }}
              style={{
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: 'transparent',
                color: 'var(--text-heading)',
                padding: '10px 12px',
                cursor: 'pointer',
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              Clear
            </button>
            <button
              type="button"
              onClick={handleApply}
              disabled={selectedProducts.length === 0 || applyState === 'saving'}
              style={{
                borderRadius: 12,
                border: 'none',
                background: selectedProducts.length === 0 ? 'var(--surface-muted)' : 'var(--accent-primary)',
                color: selectedProducts.length === 0 ? 'var(--text-disabled)' : '#fff',
                padding: '10px 14px',
                cursor: selectedProducts.length === 0 ? 'not-allowed' : 'pointer',
                fontSize: 12,
                fontWeight: 800,
              }}
            >
              {applyState === 'saving' ? 'Applying…' : 'Apply'}
            </button>
            <button
              type="button"
              onClick={onOpenGraph}
              style={{
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: 'transparent',
                color: 'var(--text-heading)',
                padding: '10px 12px',
                cursor: 'pointer',
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              Graph
            </button>
            <button
              type="button"
              onClick={onOpenMap}
              style={{
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: 'transparent',
                color: 'var(--text-heading)',
                padding: '10px 12px',
                cursor: 'pointer',
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              Map
            </button>
          </div>
        </div>
      )}
    </div>
  );
};

export default LensAssembly;
