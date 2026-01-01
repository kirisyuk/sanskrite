import React from 'react';
import { api } from '../lib/api';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import { ThemeContext } from './ThemeProvider';
import { HorizonNodeIcon, getNodeColors, formatHorizonFragmentType, getMetricColor } from './HorizonGraphNodeTypes';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface ObjectInspectorProps {
  nodeId: string | null;
  nodeData: HorizonNodeData | null;
  edgeId: string | null;
  edgeData: any | null;
  lensId: string | null;
}

export const ObjectInspector: React.FC<ObjectInspectorProps> = ({
  nodeId,
  nodeData,
  edgeId,
  edgeData,
  lensId,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const mono = React.useMemo(
    () => ({
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
    }),
    [],
  );

  const [fragment, setFragment] = React.useState<any>(null);
  const [entity, setEntity] = React.useState<any>(null);
  const [edge, setEdge] = React.useState<any>(null);
  const [sourceFragment, setSourceFragment] = React.useState<any>(null);
  const [targetFragment, setTargetFragment] = React.useState<any>(null);
  const [edgeExplainLoading, setEdgeExplainLoading] = React.useState(false);
  const [edgeExplainError, setEdgeExplainError] = React.useState<string | null>(null);
  const [edgeExplainText, setEdgeExplainText] = React.useState<string | null>(null);
  const [edgeExplainCachedAt, setEdgeExplainCachedAt] = React.useState<string | null>(null);
  const [edgeExplainMissing, setEdgeExplainMissing] = React.useState(false);
  const edgeExplainAutoRequestedRef = React.useRef<string | null>(null);
  const [nodeExplainLoading, setNodeExplainLoading] = React.useState(false);
  const [nodeExplainError, setNodeExplainError] = React.useState<string | null>(null);
  const [nodeExplainText, setNodeExplainText] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [ensuring, setEnsuring] = React.useState(false);
  const [enriching, setEnriching] = React.useState(false);
  const [productIOLoading, setProductIOLoading] = React.useState(false);
  const [productIOError, setProductIOError] = React.useState<string | null>(null);
  const [productIOResult, setProductIOResult] = React.useState<{ nodes: number; edges: number } | null>(null);

  const hexToRgba = React.useCallback((hex: string, alpha: number) => {
    const h = String(hex || '').trim();
    if (!h.startsWith('#')) return `rgba(0,0,0,${alpha})`;
    const v = h.slice(1);
    if (v.length !== 6) return `rgba(0,0,0,${alpha})`;
    const r = parseInt(v.slice(0, 2), 16);
    const g = parseInt(v.slice(2, 4), 16);
    const b = parseInt(v.slice(4, 6), 16);
    if (!Number.isFinite(r) || !Number.isFinite(g) || !Number.isFinite(b)) return `rgba(0,0,0,${alpha})`;
    const a = Math.max(0, Math.min(1, alpha));
    return `rgba(${r},${g},${b},${a})`;
  }, []);

  // Load fragment and entity data when nodeId changes
  React.useEffect(() => {
    if (!nodeId) {
      setFragment(null);
      setEntity(null);
      setError(null);
      setNodeExplainError(null);
      setNodeExplainText(null);
      return;
    }

    const loadData = async () => {
      setLoading(true);
      setError(null);
      try {
        // Use fragmentId from nodeData, or fallback to nodeId if fragmentId is not available
        const fragmentId = nodeData?.fragmentId || nodeId;
        
        if (!fragmentId) {
          setError('Fragment ID is missing');
          setLoading(false);
          return;
        }
        
        // Load fragment
        const fragmentData = await api.getHorizonFragment(fragmentId);
        setFragment(fragmentData);

        // Try to load linked entity
        try {
          const entityData = await api.getFragmentEntity(fragmentId);
          setEntity(entityData);
        } catch (err) {
          // Entity might not exist, that's okay
          setEntity(null);
        }
      } catch (err: any) {
        console.error('[ObjectInspector] Failed to load data', err);
        setError(err.response?.data?.error || err.message || 'Failed to load data');
      } finally {
        setLoading(false);
      }
    };

    loadData();
  }, [nodeId, nodeData]);

  const handleExplainNode = React.useCallback(async () => {
    if (!nodeId) return;
    setNodeExplainLoading(true);
    setNodeExplainError(null);
    setNodeExplainText(null);

    try {
      const preferredLanguage =
        (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en';

      const fragmentId = nodeData?.fragmentId || nodeId;

      const payload = {
        preferredLanguage,
        node: {
          nodeId,
          fragmentId,
          label:
            nodeData?.label ||
            fragment?.labels?.en ||
            fragment?.canonicalPath?.split('/')?.pop() ||
            null,
          canonicalPath: nodeData?.canonicalPath || fragment?.canonicalPath || null,
          type: nodeData?.type || fragment?.type || null,
          status: nodeData?.status || fragment?.status || null,
        },
        visibleIncomingOutgoingEdges: (nodeData as any)?.visibleIncidentEdges || [],
        visibleNeighborNodes: (nodeData as any)?.visibleNeighborNodes || [],
      };

      const prompt =
        `Explain this node and the visible incoming/outgoing relationships.\n` +
        `- Output language: ${preferredLanguage}\n` +
        `- Use Google Search grounding.\n` +
        `- Output: plain text only.\n` +
        `- Be detailed (but structured): 2-4 short paragraphs.\n` +
        `- If you are unsure about anything, say so explicitly.\n\n` +
        `Graph context (JSON):\n` +
        `${JSON.stringify(payload, null, 2)}\n`;

      const resp: any = await api.genaiComplete({
        prompt,
        options: {
          profile: 'fast',
          modelOverride: 'gemini-3-flash-preview',
          groundingEnabled: true,
          temperature: 0.2,
          maxOutputTokens: 8192,
          systemInstruction:
            'You are a careful analyst. Explain the node and relationships in plain language. Do not invent facts. Prefer grounded statements.',
        },
      });

      const text = String(resp?.content || '').trim();
      if (!text) {
        setNodeExplainError('Empty response from model');
      } else {
        setNodeExplainText(text);
      }
    } catch (err: any) {
      console.error('[ObjectInspector] Explain node failed', err);
      setNodeExplainError(err.response?.data?.error || err.message || 'Failed to explain node');
    } finally {
      setNodeExplainLoading(false);
    }
  }, [nodeId, nodeData, fragment]);

  // Load edge data when edgeId changes
  React.useEffect(() => {
    if (!edgeId) {
      setEdge(null);
      setSourceFragment(null);
      setTargetFragment(null);
      setEdgeExplainError(null);
      setEdgeExplainText(null);
      setEdgeExplainCachedAt(null);
      setEdgeExplainMissing(false);
      edgeExplainAutoRequestedRef.current = null;
      setError(null);
      return;
    }

    const loadEdgeData = async () => {
      setLoading(true);
      setError(null);
      try {
        let loadedEdge: any = null;
        // Try to load edge from API if we have lensItemId
        if (edgeData?.lensItemId) {
          try {
            const edgeDataFromApi = await api.getHorizonEdge(edgeData.lensItemId);
            loadedEdge = edgeDataFromApi;
            setEdge(edgeDataFromApi);
          } catch (err) {
            // If API call fails, use the data from edgeData
            console.warn('[ObjectInspector] Failed to load edge from API, using provided data', err);
            loadedEdge = edgeData;
            setEdge(edgeData);
          }
        } else {
          // Use the data from edgeData directly
          loadedEdge = edgeData;
          setEdge(edgeData);
        }

        // Load source and target fragments if we have their IDs
        // Try to get from loadedEdge first, then from edgeData (passed from click handler)
        const sourceId = loadedEdge?.sourceFragmentId || edgeData?.sourceFragmentId;
        const targetId = loadedEdge?.targetFragmentId || edgeData?.targetFragmentId;

        if (sourceId) {
          try {
            const sourceFrag = await api.getHorizonFragment(sourceId);
            setSourceFragment(sourceFrag);
          } catch (err) {
            console.warn('[ObjectInspector] Failed to load source fragment', err);
            setSourceFragment(null);
          }
        } else {
          setSourceFragment(null);
        }

        if (targetId) {
          try {
            const targetFrag = await api.getHorizonFragment(targetId);
            setTargetFragment(targetFrag);
          } catch (err) {
            console.warn('[ObjectInspector] Failed to load target fragment', err);
            setTargetFragment(null);
          }
        } else {
          setTargetFragment(null);
        }
      } catch (err: any) {
        console.error('[ObjectInspector] Failed to load edge data', err);
        setError(err.response?.data?.error || err.message || 'Failed to load edge data');
      } finally {
        setLoading(false);
      }
    };

    loadEdgeData();
  }, [edgeId, edgeData]);

  // Load cached AI explanation (if exists).
  React.useEffect(() => {
    if (!edgeId) return;
    let cancelled = false;

    const preferredLanguage =
      (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en';
    const lang = (String(preferredLanguage || 'en').split('-')[0] || 'en').toLowerCase();

    const run = async () => {
      try {
        const cached = await api.getHorizonAIExplanation('edge', edgeId, { lang });
        if (cancelled) return;
        if (cached?.markdown) {
          setEdgeExplainText(String(cached.markdown));
          setEdgeExplainCachedAt(String(cached.updatedAt || ''));
          setEdgeExplainError(null);
          setEdgeExplainMissing(false);
          return;
        }
      } catch (err: any) {
        if (cancelled) return;
        // Not found => generate below. Other errors => show but still allow manual explain.
        if (err?.response?.status !== 404) {
          setEdgeExplainError(err.response?.data?.error || err.message || 'Failed to load cached explanation');
          return;
        }
        setEdgeExplainMissing(true);
      }
    };

    run();
    return () => {
      cancelled = true;
    };
  }, [edgeId]);

  const handleExplainEdge = React.useCallback(async () => {
    if (!edgeId) return;
    setEdgeExplainLoading(true);
    setEdgeExplainError(null);
    setEdgeExplainText(null);
    setEdgeExplainCachedAt(null);

    try {
      const preferredLanguage =
        (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en';
      const lang = (String(preferredLanguage || 'en').split('-')[0] || 'en').toLowerCase();

      const edgeType = String(edgeData?.edgeType || edge?.type || 'UNKNOWN');
      const sourceId = String(edgeData?.sourceFragmentId || edge?.sourceFragmentId || '').trim();
      const targetId = String(edgeData?.targetFragmentId || edge?.targetFragmentId || '').trim();

      const payload = {
        preferredLanguage,
        edge: {
          id: edgeId,
          edgeType,
          criticality: edgeData?.criticality ?? edge?.properties?.criticality,
          amplification: edgeData?.amplification ?? edge?.properties?.amplification,
          latencyHours: edgeData?.latencyHours ?? edge?.properties?.latencyHours,
        },
        source: {
          fragmentId: sourceId,
          label:
            sourceFragment?.labels?.en ||
            sourceFragment?.labels?.[Object.keys(sourceFragment?.labels || {})[0]] ||
            sourceFragment?.canonicalPath?.split('/')?.pop() ||
            null,
          canonicalPath: sourceFragment?.canonicalPath || null,
          type: sourceFragment?.type || null,
        },
        target: {
          fragmentId: targetId,
          label:
            targetFragment?.labels?.en ||
            targetFragment?.labels?.[Object.keys(targetFragment?.labels || {})[0]] ||
            targetFragment?.canonicalPath?.split('/')?.pop() ||
            null,
          canonicalPath: targetFragment?.canonicalPath || null,
          type: targetFragment?.type || null,
        },
      };

      const prompt =
        `Explain why this graph relationship exists.\n` +
        `- Output language: ${preferredLanguage}\n` +
        `- Use Google Search grounding.\n` +
        `- Output: Markdown.\n` +
        `- Keep it concise (3-6 sentences). You may use bullet points.\n` +
        `- If you are unsure, say so explicitly.\n\n` +
        `Graph edge context (JSON):\n` +
        `${JSON.stringify(payload, null, 2)}\n`;

      const resp: any = await api.genaiComplete({
        prompt,
        options: {
          profile: 'fast',
          modelOverride: 'gemini-3-flash-preview',
          groundingEnabled: true,
          temperature: 0.2,
          maxOutputTokens: 2048,
          systemInstruction:
            'You are a careful analyst. Explain the edge in plain language. Do not invent facts. Prefer grounded statements.',
        },
      });

      const text = String(resp?.content || '').trim();
      if (!text) {
        setEdgeExplainError('Empty response from model');
      } else {
        setEdgeExplainText(text);
        // Persist best-effort
        try {
          await api.upsertHorizonAIExplanation('edge', edgeId, {
            markdown: text,
            language: lang,
            lensId: lensId || undefined,
            edgeType,
            sourceFragmentId: sourceId || undefined,
            targetFragmentId: targetId || undefined,
          }, { lang });
          setEdgeExplainCachedAt(new Date().toISOString());
        } catch {
          // ignore
        }
      }
    } catch (err: any) {
      console.error('[ObjectInspector] Explain edge failed', err);
      setEdgeExplainError(err.response?.data?.error || err.message || 'Failed to explain edge');
    } finally {
      setEdgeExplainLoading(false);
    }
  }, [edgeId, lensId, edgeData, edge, sourceFragment, targetFragment]);

  // If missing, auto-generate once per edgeId when we have enough IDs for a meaningful prompt.
  React.useEffect(() => {
    if (!edgeId) return;
    if (!edgeExplainMissing) return;
    if (edgeExplainLoading) return;
    if (edgeExplainAutoRequestedRef.current === edgeId) return;
    const sourceId = String(edgeData?.sourceFragmentId || edge?.sourceFragmentId || '').trim();
    const targetId = String(edgeData?.targetFragmentId || edge?.targetFragmentId || '').trim();
    if (!sourceId || !targetId) return;
    edgeExplainAutoRequestedRef.current = edgeId;
    handleExplainEdge();
  }, [edgeId, edgeExplainMissing, edgeExplainLoading, edgeData, edge, handleExplainEdge]);

  const handleMaterializeProductIO = React.useCallback(async () => {
    const fragmentId = nodeData?.fragmentId || nodeId;
    if (!lensId) {
      setProductIOError('Lens ID is missing (open this inspector from a Lens cockpit)');
      return;
    }
    if (!fragmentId) {
      setProductIOError('Fragment ID is missing');
      return;
    }

    setProductIOLoading(true);
    setProductIOError(null);
    setProductIOResult(null);

    try {
      const extracted = await api.extractProductIO(fragmentId, { direction: 'both', lang: 'en' });
      const applied = await api.applyProductIO(lensId, {
        objectiveFragmentId: fragmentId,
        produces: extracted.produces || [],
        consumes: extracted.consumes || [],
      });
      const nodesCount = Array.isArray(applied?.delta?.fragmentsUpserted) ? applied.delta.fragmentsUpserted.length : 0;
      const edgesCount = Array.isArray(applied?.delta?.edgesUpserted) ? applied.delta.edgesUpserted.length : 0;
      setProductIOResult({ nodes: nodesCount, edges: edgesCount });

      // Best-effort: expand the selected node to show newly materialized neighbors.
      const expand = (nodeData as any)?.onLoadRelated as undefined | ((id: string) => void);
      if (typeof expand === 'function') {
        expand(fragmentId);
      }
    } catch (err: any) {
      console.error('[ObjectInspector] Product IO materialization failed', err);
      setProductIOError(err.response?.data?.error || err.message || 'Failed to materialize Product IO');
    } finally {
      setProductIOLoading(false);
    }
  }, [lensId, nodeData]);

  const copyToClipboard = React.useCallback(async (text: string) => {
    const t = String(text || '');
    if (!t) return;
    try {
      if (typeof navigator !== 'undefined' && (navigator as any).clipboard?.writeText) {
        await (navigator as any).clipboard.writeText(t);
        return;
      }
    } catch {
      // fallback below
    }
    try {
      const ta = document.createElement('textarea');
      ta.value = t;
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      ta.style.top = '0';
      document.body.appendChild(ta);
      ta.focus();
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    } catch {
      // ignore
    }
  }, []);

  const humanizeEdgeType = React.useCallback((raw: any): string => {
    const t = String(raw || '').trim();
    if (!t) return 'Edge';
    const u = t.toUpperCase();
    const MAP: Record<string, string> = {
      DEPENDS_ON: 'Depends',
      REQUIRES: 'Requires',
      NEEDS: 'Needs',
      USES: 'Uses',
      CONSUMES: 'Consumes',
      OWNS: 'Owns',
      MANAGES: 'Manages',
      CONTROLS: 'Controls',
      INVESTS_IN: 'Invests',
      PARENT_OF: 'Parent',
      SUBSIDIARY_OF: 'Subsidiary',
      EMPLOYED_BY: 'Employed',
      BOARD_MEMBER_OF: 'Board',
      FOUNDED: 'Founded',
      PARTNERS_WITH: 'Partners',
      COOPERATES_WITH: 'Cooperates',
      SUPPORTS: 'Supports',
      SUPPLIES_TO: 'Supplies',
      PRODUCES: 'Produces',
      LOCATED_IN: 'Located',
      OPERATES_IN: 'Operates',
      BASED_IN: 'Based',
      HEADQUARTERED_IN: 'HQ',
      COMPETES_WITH: 'Competes',
      CONFLICTS_WITH: 'Conflicts',
      OPPOSES: 'Opposes',
      INFLUENCES: 'Influences',
      AFFECTS: 'Affects',
      REGULATES: 'Regulates',
      IMPACTS: 'Impacts',
    };
    if (MAP[u]) return MAP[u];
    const human = u
      .replace(/_WITH$/g, '')
      .replace(/_TO$/g, '')
      .replace(/_OF$/g, '')
      .replace(/_/g, ' ')
      .toLowerCase()
      .replace(/\b\w/g, (c) => c.toUpperCase());
    return human.length > 22 ? `${human.slice(0, 22)}…` : human;
  }, []);

  const SectionCard: React.FC<{
    title: string;
    right?: React.ReactNode;
    children: React.ReactNode;
  }> = React.useCallback(({ title, right, children }) => {
    return (
      <div
        style={{
          borderRadius: 14,
          border: '1px solid var(--border-subtle)',
          background: 'var(--bg-elevated)',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 10,
            padding: '10px 12px',
            borderBottom: '1px solid var(--border-subtle)',
            background: 'var(--bg-elevated)',
          }}
        >
          <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', letterSpacing: '0.2px' }}>
            {title}
          </div>
          {right && <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>{right}</div>}
        </div>
        <div style={{ padding: 12 }}>{children}</div>
      </div>
    );
  }, []);

  const KV: React.FC<{ k: string; v?: React.ReactNode; copyText?: string }> = React.useCallback(
    ({ k, v, copyText }) => {
      return (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '110px 1fr',
            gap: 10,
            alignItems: 'start',
            padding: '6px 0',
          }}
        >
          <div style={{ fontSize: 11, color: 'var(--text-secondary)', paddingTop: 2 }}>{k}</div>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8, minWidth: 0 }}>
            <div style={{ fontSize: 13, color: 'var(--text-heading)', minWidth: 0, overflowWrap: 'anywhere' }}>
              {v}
            </div>
            {copyText && (
              <button
                type="button"
                onClick={() => void copyToClipboard(copyText)}
                title="Copy"
                aria-label="Copy"
                style={{
                  flexShrink: 0,
                  width: 28,
                  height: 28,
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-tertiary)',
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M9 9h10v10H9z" />
                  <path strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                </svg>
              </button>
            )}
          </div>
        </div>
      );
    },
    [copyToClipboard],
  );

  const Chip: React.FC<{ children: React.ReactNode; tone?: 'neutral' | 'info' | 'danger' }> = React.useCallback(
    ({ children, tone = 'neutral' }) => {
      const bg =
        tone === 'danger'
          ? 'rgba(239, 68, 68, 0.14)'
          : tone === 'info'
            ? 'rgba(59, 130, 246, 0.14)'
            : 'var(--white-10)';
      const border =
        tone === 'danger'
          ? 'rgba(239, 68, 68, 0.28)'
          : tone === 'info'
            ? 'rgba(59, 130, 246, 0.22)'
            : 'var(--border-subtle)';
      const fg =
        tone === 'danger'
          ? 'rgba(248, 113, 113, 0.95)'
          : tone === 'info'
            ? 'rgba(147, 197, 253, 0.95)'
            : 'var(--text-secondary-strong)';
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 8px',
            borderRadius: 999,
            border: `1px solid ${border}`,
            background: bg,
            color: fg,
            fontSize: 11,
            fontWeight: 750,
            whiteSpace: 'nowrap',
          }}
        >
          {children}
        </span>
      );
    },
    [],
  );

  const AccentChip: React.FC<{ accent: string; children: React.ReactNode }> = React.useCallback(
    ({ accent, children }) => {
      const bg = hexToRgba(accent, theme === 'dark' ? 0.18 : 0.14);
      const border = hexToRgba(accent, theme === 'dark' ? 0.40 : 0.28);
      const fg = theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.88)';
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 8px',
            borderRadius: 999,
            border: `1px solid ${border}`,
            background: bg,
            color: fg,
            fontSize: 11,
            fontWeight: 800,
            whiteSpace: 'nowrap',
          }}
        >
          {children}
        </span>
      );
    },
    [hexToRgba, theme],
  );

  const Disclosure: React.FC<{ title: string; subtitle?: string; children: React.ReactNode; defaultOpen?: boolean }> =
    React.useCallback(({ title, subtitle, children, defaultOpen }) => {
      return (
        <details
          open={defaultOpen}
          style={{
            borderRadius: 14,
            border: '1px solid var(--border-subtle)',
            background: 'var(--bg-elevated)',
            overflow: 'hidden',
          }}
        >
          <summary
            style={{
              listStyle: 'none',
              cursor: 'pointer',
              padding: '10px 12px',
              display: 'flex',
              alignItems: 'baseline',
              justifyContent: 'space-between',
              gap: 10,
              borderBottom: '1px solid var(--border-subtle)',
              userSelect: 'none',
            }}
          >
            <span style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>{title}</span>
            {subtitle && <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{subtitle}</span>}
          </summary>
          <div style={{ padding: 12 }}>{children}</div>
        </details>
      );
    }, []);

  const nodeDisplayName = React.useMemo(() => {
    const fromNode = String(nodeData?.label || '').trim();
    if (fromNode) return fromNode;
    const fromFrag =
      String(fragment?.labels?.en || '').trim() ||
      String(fragment?.canonicalPath || '').trim().split('/').pop() ||
      '';
    if (fromFrag) return fromFrag;
    const fromPath = String(nodeData?.canonicalPath || '').trim().split('/').pop() || '';
    return fromPath || 'Node';
  }, [nodeData, fragment]);

  const nodeType = React.useMemo(() => String(nodeData?.type || fragment?.type || '').trim(), [nodeData, fragment]);
  const nodeStatus = React.useMemo(() => String(nodeData?.status || fragment?.status || '').trim(), [nodeData, fragment]);
  const nodeCanonicalPath = React.useMemo(
    () => String(nodeData?.canonicalPath || fragment?.canonicalPath || '').trim(),
    [nodeData, fragment],
  );
  const fragmentIdForNode = React.useMemo(() => String(nodeData?.fragmentId || nodeId || '').trim(), [nodeData, nodeId]);

  const edgeTypeRaw = React.useMemo(
    () => String(edgeData?.edgeType || edge?.type || '').trim(),
    [edgeData, edge],
  );
  const edgeCaption = React.useMemo(
    () => String(edgeData?.edgeCaption || humanizeEdgeType(edgeTypeRaw) || '').trim(),
    [edgeData, humanizeEdgeType, edgeTypeRaw],
  );
  const edgeStroke = React.useMemo(() => {
    const s =
      (edgeData as any)?.baseStyle?.stroke ||
      (edgeData as any)?.style?.stroke ||
      (edgeData as any)?.stroke ||
      (edge as any)?.style?.stroke ||
      (edge as any)?.data?.baseStyle?.stroke;
    return (typeof s === 'string' && s.trim()) ? s.trim() : 'var(--text-tertiary-weak)';
  }, [edgeData, edge]);

  const nodeAccent = React.useMemo(() => {
    const t = nodeType || fragment?.type;
    return getNodeColors(t, theme === 'dark' ? 'dark' : 'light').accent;
  }, [nodeType, fragment, theme]);

  const nodeTypeHuman = React.useMemo(() => formatHorizonFragmentType(nodeType), [nodeType]);

  const METRIC_META: Record<
    string,
    {
      label: string;
      good: 'high' | 'low' | 'neutral';
      group: 'sentiment' | 'ops' | 'risk' | 'other';
      scale?: 'unit' | 'bipolar';
    }
  > = React.useMemo(
    () => ({
      public_perception: { label: 'Public perception', good: 'high', group: 'sentiment' },
      // Often -1..+1 in practice
      market_sentiment: { label: 'Market sentiment', good: 'high', group: 'sentiment', scale: 'bipolar' },
      geopolitical_alignment: { label: 'Geopolitical alignment', good: 'neutral', group: 'sentiment', scale: 'bipolar' },
      operational_health: { label: 'Operational health', good: 'high', group: 'ops' },
      capacity_utilization: { label: 'Capacity utilization', good: 'high', group: 'ops' },
      stability_index: { label: 'Stability', good: 'high', group: 'ops' },
      corruption_vulnerability: { label: 'Corruption vulnerability', good: 'low', group: 'risk' },
      sanction_exposure: { label: 'Sanctions exposure', good: 'low', group: 'risk' },
      risk: { label: 'Risk', good: 'low', group: 'risk' },
    }),
    [],
  );

  if (!nodeId && !edgeId) {
    return (
      <div
        style={{
          padding: '18px 14px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          borderRadius: 14,
          border: '1px dashed var(--border-subtle)',
          background: 'var(--bg-elevated)',
        }}
      >
        Select a node or edge to inspect.
      </div>
    );
  }

  return (
    <div style={{ padding: '8px 0', display: 'flex', flexDirection: 'column', gap: 12 }}>
      {/* Top summary */}
      <div
        style={{
          borderRadius: 16,
          border: '1px solid var(--border-subtle)',
          background: 'var(--bg-elevated)',
          padding: 12,
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 10 }}>
          <div style={{ minWidth: 0, display: 'flex', gap: 10, alignItems: 'flex-start' }}>
            {!edgeId && (
              <div
                style={{
                  width: 34,
                  height: 34,
                  borderRadius: 12,
                  border: `1px solid ${hexToRgba(nodeAccent, theme === 'dark' ? 0.38 : 0.22)}`,
                  background: hexToRgba(nodeAccent, theme === 'dark' ? 0.16 : 0.10),
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0,
                }}
              >
                <HorizonNodeIcon type={nodeType} color={nodeAccent} size={18} />
              </div>
            )}
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)', lineHeight: 1.25 }}>
                {edgeId ? 'Relationship' : nodeDisplayName}
              </div>
              <div style={{ marginTop: 4, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {edgeId ? (
                  <>
                    <Chip tone="info">
                      <span style={{ width: 8, height: 8, borderRadius: 99, background: edgeStroke }} />
                      {edgeCaption}
                    </Chip>
                    {edgeTypeRaw && <Chip>{edgeTypeRaw}</Chip>}
                  </>
                ) : (
                  <>
                    {nodeType && (
                      <AccentChip accent={nodeAccent}>
                        <HorizonNodeIcon type={nodeType} color={nodeAccent} size={14} />
                        {nodeTypeHuman}
                      </AccentChip>
                    )}
                    {nodeStatus && <Chip>{nodeStatus}</Chip>}
                  </>
                )}
              </div>
              {!edgeId && nodeCanonicalPath && (
                <div style={{ marginTop: 8 }}>
                  <KV k="Canonical path" v={<span style={mono}>{nodeCanonicalPath}</span>} copyText={nodeCanonicalPath} />
                </div>
              )}
            </div>
          </div>
          {loading && <Chip>Loading…</Chip>}
        </div>
        {error && (
          <div
            style={{
              padding: '10px 12px',
              borderRadius: 12,
              backgroundColor: 'rgba(239, 68, 68, 0.10)',
              border: '1px solid rgba(239, 68, 68, 0.25)',
              color: '#f87171',
              fontSize: 12,
              fontWeight: 650,
            }}
          >
            {error}
          </div>
        )}
      </div>

      {/* Edge */}
      {edgeId && (
        <>
          <SectionCard title="Summary">
            {edgeTypeRaw && (
              <KV
                k="Type"
                v={
                  <span title={edgeTypeRaw}>
                    <b>{edgeCaption}</b>
                    <span style={{ marginLeft: 8, color: 'var(--text-secondary)', ...mono }}>{edgeTypeRaw}</span>
                  </span>
                }
              />
            )}
            <KV
              k="From"
              v={
                sourceFragment ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                    <div style={{ fontWeight: 750 }}>
                      {sourceFragment.labels?.en ||
                        sourceFragment.labels?.[Object.keys(sourceFragment.labels || {})[0]] ||
                        sourceFragment.canonicalPath?.split('/').pop() ||
                        'Unknown'}
                    </div>
                    {sourceFragment?.type && (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        {(() => {
                          const accent = getNodeColors(sourceFragment.type, theme === 'dark' ? 'dark' : 'light').accent;
                          return (
                            <AccentChip accent={accent}>
                              <HorizonNodeIcon type={sourceFragment.type} color={accent} size={14} />
                              {formatHorizonFragmentType(sourceFragment.type)}
                            </AccentChip>
                          );
                        })()}
                      </div>
                    )}
                    <div style={{ fontSize: 11, color: 'var(--text-secondary)', ...mono }}>
                      {sourceFragment?.canonicalPath ? sourceFragment.canonicalPath : (edge?.sourceFragmentId || edgeData?.sourceFragmentId)}
                    </div>
                  </div>
                ) : (
                  <span style={mono}>{edge?.sourceFragmentId || edgeData?.sourceFragmentId}</span>
                )
              }
              copyText={String(sourceFragment?.canonicalPath || edge?.sourceFragmentId || edgeData?.sourceFragmentId || '').trim() || undefined}
            />
            <KV
              k="To"
              v={
                targetFragment ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                    <div style={{ fontWeight: 750 }}>
                      {targetFragment.labels?.en ||
                        targetFragment.labels?.[Object.keys(targetFragment.labels || {})[0]] ||
                        targetFragment.canonicalPath?.split('/').pop() ||
                        'Unknown'}
                    </div>
                    {targetFragment?.type && (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        {(() => {
                          const accent = getNodeColors(targetFragment.type, theme === 'dark' ? 'dark' : 'light').accent;
                          return (
                            <AccentChip accent={accent}>
                              <HorizonNodeIcon type={targetFragment.type} color={accent} size={14} />
                              {formatHorizonFragmentType(targetFragment.type)}
                            </AccentChip>
                          );
                        })()}
                      </div>
                    )}
                    <div style={{ fontSize: 11, color: 'var(--text-secondary)', ...mono }}>
                      {targetFragment?.canonicalPath ? targetFragment.canonicalPath : (edge?.targetFragmentId || edgeData?.targetFragmentId)}
                    </div>
                  </div>
                ) : (
                  <span style={mono}>{edge?.targetFragmentId || edgeData?.targetFragmentId}</span>
                )
              }
              copyText={String(targetFragment?.canonicalPath || edge?.targetFragmentId || edgeData?.targetFragmentId || '').trim() || undefined}
            />
          </SectionCard>

          <Disclosure title="AI explanation" subtitle="Grounded, concise">
            <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
              <button
                type="button"
                onClick={handleExplainEdge}
                disabled={edgeExplainLoading}
                style={{
                  padding: '8px 10px',
                  fontSize: 12,
                  fontWeight: 800,
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: edgeExplainLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                  color: 'var(--text-heading)',
                  cursor: edgeExplainLoading ? 'not-allowed' : 'pointer',
                  opacity: edgeExplainLoading ? 0.7 : 1,
                  flex: 1,
                }}
              >
                {edgeExplainLoading ? 'Generating…' : (edgeExplainText ? 'Reload' : 'Generate')}
              </button>
              {edgeExplainCachedAt && (
                <span style={{ fontSize: 11, color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                  Cached
                </span>
              )}
            </div>
            {edgeExplainError && <div style={{ fontSize: 12, color: '#f87171', marginTop: 10 }}>{edgeExplainError}</div>}
            {edgeExplainText && (
              <div
                style={{
                  marginTop: 10,
                  fontSize: 12,
                  color: 'var(--text-heading)',
                  lineHeight: 1.5,
                  padding: 10,
                  borderRadius: 12,
                  backgroundColor: 'var(--bg-secondary)',
                  border: '1px solid var(--border-subtle)',
                }}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{edgeExplainText}</ReactMarkdown>
              </div>
            )}
          </Disclosure>

          {(edgeData?.criticality !== undefined ||
            edgeData?.amplification !== undefined ||
            edgeData?.latencyHours !== undefined ||
            edge?.properties) && (
            <SectionCard title="Properties">
              {(edgeData?.criticality !== undefined || edge?.properties?.criticality !== undefined) && (
                <KV k="Criticality" v={<span style={mono}>{edge?.properties?.criticality ?? edgeData?.criticality}</span>} />
              )}
              {(edgeData?.amplification !== undefined || edge?.properties?.amplification !== undefined) && (
                <KV k="Amplification" v={<span style={mono}>{edge?.properties?.amplification ?? edgeData?.amplification}</span>} />
              )}
              {(edgeData?.latencyHours !== undefined || edge?.properties?.latencyHours !== undefined) && (
                <KV k="Latency (h)" v={<span style={mono}>{edge?.properties?.latencyHours ?? edgeData?.latencyHours}</span>} />
              )}
              {edge?.direction && <KV k="Direction" v={<span>{edge.direction}</span>} />}
            </SectionCard>
          )}

          {edge?.qualifiers && Object.keys(edge.qualifiers).length > 0 && (
            <SectionCard title="Qualifiers">
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {Object.entries(edge.qualifiers).map(([k, v]) => (
                  <Chip key={k}>
                    {k}: {String(v)}
                  </Chip>
                ))}
              </div>
            </SectionCard>
          )}

          {edge?.metadata && (
            <Disclosure title="Metadata" subtitle="Technical details">
              {edge.metadata.sourceFrameId && (
                <KV k="Source frame" v={<span style={mono}>{edge.metadata.sourceFrameId}</span>} copyText={edge.metadata.sourceFrameId} />
              )}
              {edge.metadata.createdAt && <KV k="Created" v={<span>{new Date(edge.metadata.createdAt).toLocaleString()}</span>} />}
              {edge.metadata.lastUpdatedAt && <KV k="Updated" v={<span>{new Date(edge.metadata.lastUpdatedAt).toLocaleString()}</span>} />}
            </Disclosure>
          )}

          <Disclosure title="Technical" subtitle="IDs & raw fields">
            <KV k="Edge ID" v={<span style={mono}>{edgeId}</span>} copyText={edgeId} />
            {edgeData?.lensItemId && <KV k="Lens item" v={<span style={mono}>{edgeData.lensItemId}</span>} copyText={edgeData.lensItemId} />}
            {(edge?.id && edge?.id !== edgeId) && <KV k="Dependency edge" v={<span style={mono}>{edge.id}</span>} copyText={edge.id} />}
            {edgeTypeRaw && <KV k="Raw type" v={<span style={mono}>{edgeTypeRaw}</span>} copyText={edgeTypeRaw} />}
            {(edge?.sourceFragmentId || edgeData?.sourceFragmentId) && (
              <KV
                k="Source ID"
                v={<span style={mono}>{edge?.sourceFragmentId || edgeData?.sourceFragmentId}</span>}
                copyText={String(edge?.sourceFragmentId || edgeData?.sourceFragmentId || '').trim() || undefined}
              />
            )}
            {(edge?.targetFragmentId || edgeData?.targetFragmentId) && (
              <KV
                k="Target ID"
                v={<span style={mono}>{edge?.targetFragmentId || edgeData?.targetFragmentId}</span>}
                copyText={String(edge?.targetFragmentId || edgeData?.targetFragmentId || '').trim() || undefined}
              />
            )}
          </Disclosure>
        </>
      )}

      {/* Node */}
      {nodeId && (
        <>
          <SectionCard title="Context">
            {(nodeData as any)?.visibleNeighborNodes && (
              <KV
                k="Visible context"
                v={
                  <span style={{ color: 'var(--text-secondary-strong)', fontSize: 12, fontWeight: 650 }}>
                    {(nodeData as any)?.visibleNeighborNodes?.length || 0} neighbors, {(nodeData as any)?.visibleIncidentEdges?.length || 0} edges
                  </span>
                }
              />
            )}
          </SectionCard>

          <Disclosure title="AI explanation" subtitle="Grounded + structured">
            <button
              type="button"
              onClick={handleExplainNode}
              disabled={nodeExplainLoading}
              style={{
                padding: '8px 10px',
                fontSize: 12,
                fontWeight: 800,
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: nodeExplainLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                color: 'var(--text-heading)',
                cursor: nodeExplainLoading ? 'not-allowed' : 'pointer',
                opacity: nodeExplainLoading ? 0.7 : 1,
                width: '100%',
              }}
            >
              {nodeExplainLoading ? 'Explaining…' : 'Explain node + visible relationships'}
            </button>
            {nodeExplainError && <div style={{ fontSize: 12, color: '#f87171', marginTop: 10 }}>{nodeExplainError}</div>}
            {nodeExplainText && (
              <div
                style={{
                  marginTop: 10,
                  fontSize: 12,
                  color: 'var(--text-heading)',
                  lineHeight: 1.5,
                  whiteSpace: 'pre-wrap',
                  padding: 10,
                  borderRadius: 12,
                  backgroundColor: 'var(--bg-secondary)',
                  border: '1px solid var(--border-subtle)',
                }}
              >
                {nodeExplainText}
              </div>
            )}
          </Disclosure>

          {nodeData?.state && Object.keys(nodeData.state).length > 0 && (
            <SectionCard
              title="Signals"
              right={
                <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                  0–1 and −1…+1
                </span>
              }
            >
              {(() => {
                const colorFrom01 = (x: number) => {
                  const v = Math.max(0, Math.min(1, x));
                  if (v >= 0.7) return '#22C55E';
                  if (v >= 0.4) return '#F59E0B';
                  return '#EF4444';
                };

                const entries = Object.entries(nodeData.state || {})
                  .map(([k, v]) => [String(k || ''), typeof v === 'number' ? v : Number(v)] as const)
                  .filter(([k, v]) => k && Number.isFinite(v))
                  .map(([k, v]) => {
                    const key = k.toLowerCase().split('/').pop() || k.toLowerCase();
                    const meta = METRIC_META[key];
                    const label = meta?.label || key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
                    const scale: 'unit' | 'bipolar' = meta?.scale || 'unit';

                    // Values may be either 0..1 or -1..+1 (bipolar). Do not clamp negatives to 0.
                    const vRaw = Number(v);
                    const vUnit = Math.max(0, Math.min(1, vRaw));
                    const vBipolar = Math.max(-1, Math.min(1, vRaw));
                    const v01 = scale === 'bipolar' ? (vBipolar + 1) / 2 : vUnit;

                    // Scoring for coloring:
                    // - neutral: best at 0 for bipolar, best at 0.5 for unit (rare)
                    // - low: best when lower (invert)
                    // - high: best when higher
                    const score01 =
                      meta?.good === 'neutral'
                        ? (scale === 'bipolar' ? (1 - Math.abs(vBipolar)) : (1 - Math.abs(vUnit - 0.5) * 2))
                        : meta?.good === 'low'
                          ? (1 - v01)
                          : v01;

                    const dot =
                      meta?.good === 'neutral'
                        ? colorFrom01(score01)
                        : getMetricColor(key, v01);

                    const pct = Math.round(v01 * 100);
                    const pctSigned = Math.round(vBipolar * 100);

                    return {
                      rawKey: k,
                      key,
                      label,
                      group: meta?.group || 'other',
                      scale,
                      v01,
                      vBipolar,
                      dot,
                      pct,
                      pctSigned,
                    };
                  });

                // Known metrics first, then rest alphabetically
                const order = ['sentiment', 'ops', 'risk', 'other'] as const;
                entries.sort((a, b) => {
                  const ga = order.indexOf(a.group as any);
                  const gb = order.indexOf(b.group as any);
                  if (ga !== gb) return ga - gb;
                  const ak = Boolean(METRIC_META[a.key]);
                  const bk = Boolean(METRIC_META[b.key]);
                  if (ak !== bk) return ak ? -1 : 1;
                  return a.label.localeCompare(b.label);
                });

                return (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {entries.slice(0, 12).map((m) => (
                      <div key={m.rawKey} title={m.rawKey} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        <div style={{ width: 12, height: 12, borderRadius: 99, background: m.dot, flexShrink: 0 }} />
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                            <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {m.label}
                            </div>
                            <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-secondary-strong)', ...mono }}>
                              {m.scale === 'bipolar' ? (
                                <>
                                  {m.vBipolar.toFixed(2)}
                                  <span style={{ marginLeft: 6, color: 'var(--text-tertiary-weak)', fontWeight: 700 }}>
                                    {m.pctSigned >= 0 ? `+${m.pctSigned}%` : `${m.pctSigned}%`}
                                  </span>
                                </>
                              ) : (
                                <>
                                  {m.v01.toFixed(2)}
                                  <span style={{ marginLeft: 6, color: 'var(--text-tertiary-weak)', fontWeight: 700 }}>{m.pct}%</span>
                                </>
                              )}
                            </div>
                          </div>
                          <div
                            style={{
                              marginTop: 6,
                              height: 6,
                              borderRadius: 999,
                              background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)',
                              overflow: 'hidden',
                              position: 'relative',
                            }}
                          >
                            {m.scale === 'bipolar' ? (
                              <>
                                {/* Midline */}
                                <div
                                  style={{
                                    position: 'absolute',
                                    left: '50%',
                                    top: 0,
                                    bottom: 0,
                                    width: 1,
                                    background: theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)',
                                  }}
                                />
                                {/* Fill from center */}
                                {m.vBipolar >= 0 ? (
                                  <div
                                    style={{
                                      position: 'absolute',
                                      left: '50%',
                                      top: 0,
                                      bottom: 0,
                                      width: `${Math.round(m.vBipolar * 50)}%`,
                                      background: m.dot,
                                    }}
                                  />
                                ) : (
                                  <div
                                    style={{
                                      position: 'absolute',
                                      right: '50%',
                                      top: 0,
                                      bottom: 0,
                                      width: `${Math.round(Math.abs(m.vBipolar) * 50)}%`,
                                      background: m.dot,
                                    }}
                                  />
                                )}
                              </>
                            ) : (
                              <div style={{ width: `${m.pct}%`, height: '100%', background: m.dot }} />
                            )}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                );
              })()}
            </SectionCard>
          )}

          {fragment && (
            <Disclosure title="Fragment details" subtitle="Source object metadata">
              {fragment.labels && Object.keys(fragment.labels).length > 0 && (
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: 10 }}>
                  {Object.entries(fragment.labels).map(([lang, label]) => (
                    <Chip key={lang}>
                      {lang}: {String(label)}
                    </Chip>
                  ))}
                </div>
              )}
              {fragment.canonicalPath && <KV k="Canonical path" v={<span style={mono}>{fragment.canonicalPath}</span>} copyText={fragment.canonicalPath} />}
              {fragment.type && <KV k="Fragment type" v={<span>{String(fragment.type)}</span>} />}
              {fragment.identifiers && Object.keys(fragment.identifiers).length > 0 && (
                <div style={{ marginTop: 6 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginBottom: 6 }}>Identifiers</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {Object.entries(fragment.identifiers).map(([k, v]) => (
                      <KV key={k} k={k} v={<span style={mono}>{String(v)}</span>} copyText={String(v)} />
                    ))}
                  </div>
                </div>
              )}
            </Disclosure>
          )}

          {entity && (
            <Disclosure title="Linked entity" subtitle="Enriched business object">
              {entity.canonicalName && <KV k="Name" v={<span>{entity.canonicalName}</span>} />}
              {entity.type && <KV k="Type" v={<span>{entity.type}</span>} />}
              {entity.locationPath && <KV k="Location path" v={<span style={mono}>{entity.locationPath}</span>} copyText={entity.locationPath} />}
              {entity.homeLocation?.iso6709 && <KV k="Coordinates" v={<span style={mono}>{entity.homeLocation.iso6709}</span>} copyText={entity.homeLocation.iso6709} />}
              {entity.kbRefs && entity.kbRefs.length > 0 && (
                <div style={{ marginTop: 6 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginBottom: 6 }}>KB refs</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {entity.kbRefs.map((ref: any, idx: number) => (
                      <KV key={idx} k={String(ref.kb || 'KB')} v={<span style={mono}>{String(ref.externalId || 'N/A')}</span>} />
                    ))}
                  </div>
                </div>
              )}
            </Disclosure>
          )}

          {(nodeData?.fragmentId || nodeId) && (
            <Disclosure title="Actions" subtitle="Operational / experimental">
              {/* Product IO */}
              {nodeData?.fragmentId && (
                <div style={{ marginBottom: 12 }}>
                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 8 }}>
                    Hypothesis: Product IO (v1)
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginBottom: 10, lineHeight: 1.35 }}>
                    Extract products this object <b>produces</b> and <b>consumes</b>, then materialize them into the global Horizon graph.
                    <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-tertiary-weak)' }}>
                      v1: PRODUCT-only. Fragment is primary; entity link is enrichment.
                    </div>
                  </div>
                  {productIOError && <div style={{ fontSize: 12, color: '#f87171', marginBottom: 8 }}>{productIOError}</div>}
                  {productIOResult && (
                    <div style={{ fontSize: 12, color: 'var(--text-heading)', marginBottom: 8 }}>
                      Materialized: <b>{productIOResult.nodes}</b> nodes, <b>{productIOResult.edges}</b> edges
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={handleMaterializeProductIO}
                    disabled={productIOLoading || !lensId}
                    style={{
                      padding: '8px 10px',
                      fontSize: 12,
                      fontWeight: 800,
                      borderRadius: 12,
                      border: '1px solid var(--border-subtle)',
                      background: productIOLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                      color: 'var(--text-heading)',
                      cursor: productIOLoading || !lensId ? 'not-allowed' : 'pointer',
                      opacity: productIOLoading || !lensId ? 0.7 : 1,
                      width: '100%',
                    }}
                  >
                    {productIOLoading ? 'Materializing…' : 'Materialize Product IO'}
                  </button>
                </div>
              )}

              {/* Ensure / Enrich */}
              {nodeId && nodeData && fragment && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <button
                    type="button"
                    onClick={async () => {
                      if (!fragment.id || ensuring) return;
                      setEnsuring(true);
                      setError(null);
                      try {
                        await api.ensureFragmentEntity(fragment.id);
                        const fragmentData = await api.getHorizonFragment(fragment.id);
                        setFragment(fragmentData);
                        try {
                          const entityData = await api.getFragmentEntity(fragment.id);
                          setEntity(entityData);
                        } catch {
                          setEntity(null);
                        }
                      } catch (err: any) {
                        console.error('[ObjectInspector] Failed to ensure entity', err);
                        setError(err.response?.data?.error || err.message || 'Failed to ensure entity');
                      } finally {
                        setEnsuring(false);
                      }
                    }}
                    disabled={ensuring || enriching}
                    style={{
                      padding: '10px 12px',
                      borderRadius: 12,
                      backgroundColor: ensuring ? 'var(--bg-secondary)' : 'var(--accent-primary)',
                      color: 'var(--text-on-accent)',
                      border: 'none',
                      cursor: ensuring || enriching ? 'not-allowed' : 'pointer',
                      fontSize: 12,
                      fontWeight: 850,
                      opacity: ensuring || enriching ? 0.6 : 1,
                    }}
                  >
                    {ensuring ? 'Updating…' : 'Ensure entity'}
                  </button>
                  {entity && (
                    <button
                      type="button"
                      onClick={async () => {
                        if (!fragment.id || enriching) return;
                        setEnriching(true);
                        setError(null);
                        try {
                          await api.enrichFragmentEntity(fragment.id, true);
                          const entityData = await api.getFragmentEntity(fragment.id);
                          setEntity(entityData);
                          const fragmentData = await api.getHorizonFragment(fragment.id);
                          setFragment(fragmentData);
                        } catch (err: any) {
                          console.error('[ObjectInspector] Failed to enrich entity', err);
                          setError(err.response?.data?.error || err.message || 'Failed to enrich entity');
                        } finally {
                          setEnriching(false);
                        }
                      }}
                      disabled={ensuring || enriching}
                      style={{
                        padding: '10px 12px',
                        borderRadius: 12,
                        backgroundColor: enriching ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                        color: 'var(--text-heading)',
                        border: '1px solid var(--border-subtle)',
                        cursor: ensuring || enriching ? 'not-allowed' : 'pointer',
                        fontSize: 12,
                        fontWeight: 850,
                        opacity: ensuring || enriching ? 0.6 : 1,
                      }}
                    >
                      {enriching ? 'Updating…' : 'Enrich entity'}
                    </button>
                  )}
                </div>
              )}
            </Disclosure>
          )}

          <Disclosure title="Technical" subtitle="IDs & raw fields">
            <KV k="Node ID" v={<span style={mono}>{nodeId}</span>} copyText={nodeId} />
            {fragmentIdForNode && <KV k="Fragment ID" v={<span style={mono}>{fragmentIdForNode}</span>} copyText={fragmentIdForNode} />}
            {(nodeData as any)?.lensItemId && (
              <KV k="Lens item" v={<span style={mono}>{String((nodeData as any).lensItemId)}</span>} copyText={String((nodeData as any).lensItemId)} />
            )}
            {nodeType && <KV k="Raw type" v={<span style={mono}>{nodeType}</span>} copyText={nodeType} />}
            {nodeStatus && <KV k="Raw status" v={<span style={mono}>{nodeStatus}</span>} copyText={nodeStatus} />}
            {typeof (nodeData as any)?.depth === 'number' && <KV k="Depth" v={<span style={mono}>{String((nodeData as any).depth)}</span>} />}
            {Boolean((nodeData as any)?.rootFragmentId) && (
              <KV k="Root" v={<span style={mono}>{String((nodeData as any).rootFragmentId)}</span>} copyText={String((nodeData as any).rootFragmentId)} />
            )}
            {Boolean((nodeData as any)?.parentFragmentId) && (
              <KV
                k="Parent"
                v={<span style={mono}>{String((nodeData as any).parentFragmentId)}</span>}
                copyText={String((nodeData as any).parentFragmentId)}
              />
            )}
            {typeof (nodeData as any)?.expanded === 'boolean' && <KV k="Expanded" v={<span style={mono}>{String((nodeData as any).expanded)}</span>} />}
            {typeof (nodeData as any)?.collapsed === 'boolean' && <KV k="Collapsed" v={<span style={mono}>{String((nodeData as any).collapsed)}</span>} />}
            {typeof (nodeData as any)?.critical === 'boolean' && <KV k="Critical" v={<span style={mono}>{String((nodeData as any).critical)}</span>} />}
          </Disclosure>
        </>
      )}
    </div>
  );
};

