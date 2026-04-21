import React from 'react';
import { api } from '../lib/api';
import { auth } from '../lib/firebase';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import { ThemeContext } from './ThemeProvider';
import { HorizonNodeIcon, getNodeColors, formatHorizonFragmentType, getMetricColor, getHorizonNodeVisualType } from './HorizonGraphNodeTypes';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { humanizeCanonicalPath, preferDisplayLabel } from '../lib/uiUtils';
import { describeRouteOverlay, readLegRouteOverlays, selectPrimaryLegRouteOverlay } from '../lib/supplyRouteOverlays';
import { normalizeEdgeType as normalizeWireRelation, tryParseFragmentClass } from '../lib/taxonomy';
import { FRAGMENT_CLASS, RELATION } from '../lib/taxonomy.generated';
import { HorizonOnDemandDependencyResolverModal, type HorizonOnDemandContext } from './HorizonOnDemandDependencyResolverModal';
import { RISK_SEGMENTS, RiskSegmentIcon, segmentForMetricBaseKey } from '../lib/riskShield';

/** One row from `attributes.site_transforms` on a horizon fragment (backend: models.SiteTransform). */
type HorizonSiteTransformRow = {
  kind: string;
  inputProducts: string[];
  outputProducts: string[];
  yieldRatio?: number;
  confidence?: number;
  source?: string;
};

function normalizeProductPathList(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  return v.map((x) => String(x || '').trim()).filter(Boolean);
}

function parseSiteTransformsFromFragment(fragment: unknown): HorizonSiteTransformRow[] {
  const attrs = (fragment as any)?.attributes;
  if (!attrs || typeof attrs !== 'object') return [];
  const raw = (attrs as any).site_transforms ?? (attrs as any).siteTransforms;
  if (raw == null) return [];
  const arr = Array.isArray(raw) ? raw : [];
  const out: HorizonSiteTransformRow[] = [];
  for (const item of arr) {
    if (!item || typeof item !== 'object') continue;
    const rec = item as Record<string, unknown>;
    const kind = String(rec.kind ?? '').trim();
    const inputProducts = normalizeProductPathList(rec.inputProducts);
    const outputProducts = normalizeProductPathList(rec.outputProducts);
    const yieldRatio = typeof rec.yieldRatio === 'number' && Number.isFinite(rec.yieldRatio) ? rec.yieldRatio : undefined;
    const confidence =
      typeof rec.confidence === 'number' && Number.isFinite(rec.confidence) ? rec.confidence : undefined;
    const source = String(rec.source ?? '').trim() || undefined;
    if (!kind && inputProducts.length === 0 && outputProducts.length === 0) continue;
    out.push({ kind, inputProducts, outputProducts, yieldRatio, confidence, source });
  }
  return out;
}

function formatSiteTransformConfidence(c: number): string {
  if (c >= 0 && c <= 1) return `${Math.round(c * 100)}%`;
  return c.toFixed(2);
}

interface HorizonEntityInspectorProps {
  nodeId: string | null;
  nodeData: HorizonNodeData | null;
  edgeId: string | null;
  edgeData: any | null;
  lensId: string | null;
}

export const HorizonEntityInspector: React.FC<HorizonEntityInspectorProps> = ({
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

  const normalizeEdgeType = React.useCallback((raw: any): string => {
    const t = String(raw || '').trim();
    if (!t) return '';
    // Defensive normalization: "Located in" -> "LOCATED_IN"
    return t.toUpperCase().replace(/\s+/g, '_');
  }, []);

  const readFacilityKind = React.useCallback((obj: any): string => {
    return String(
      obj?.facilityKind ||
      obj?.identifiers?.facilityKind ||
      obj?.identifiers?.facility_kind ||
      '',
    ).trim();
  }, []);

  const [fragment, setFragment] = React.useState<any>(null);
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
  const [productIOLoading, setProductIOLoading] = React.useState(false);
  const [productIOError, setProductIOError] = React.useState<string | null>(null);
  const [productIOResult, setProductIOResult] = React.useState<{ nodes: number; edges: number } | null>(null);
  const [newsLoading, setNewsLoading] = React.useState(false);
  const [newsError, setNewsError] = React.useState<string | null>(null);
  const [onDemandOpen, setOnDemandOpen] = React.useState(false);
  const [onDemandCtx, setOnDemandCtx] = React.useState<HorizonOnDemandContext | null>(null);

  const formatMetricValue = React.useCallback((mv: any): string => {
    if (!mv || typeof mv !== 'object') return String(mv ?? '');
    const v = (mv as any).value;
    const unit = String((mv as any).unit || '').trim();
    const conf = typeof (mv as any).confidence === 'number' ? (mv as any).confidence : undefined;
    const observedAt = String((mv as any).observedAt || '').trim();
    const parts: string[] = [];
    if (v !== undefined && v !== null && v !== '') parts.push(String(v));
    if (unit) parts.push(unit);
    if (typeof conf === 'number' && Number.isFinite(conf)) parts.push(`conf=${Math.round(Math.max(0, Math.min(1, conf)) * 100)}%`);
    if (observedAt) parts.push(`asOf=${observedAt}`);
    return parts.join(' ');
  }, []);

  // Lens-scope pin state (best-effort): derived from lens.scope to avoid stale nodeData snapshots.
  const [lensScopePinned, setLensScopePinned] = React.useState<boolean | null>(null);
  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      const cp = String(nodeData?.canonicalPath || fragment?.canonicalPath || '').trim();
      if (!lensId || !cp || edgeId) {
        if (!cancelled) setLensScopePinned(null);
        return;
      }
      try {
        const lens = await api.getHorizonLens(lensId);
        const scope = Array.isArray(lens?.scope) ? lens.scope.map((s: any) => String(s || '').trim()).filter(Boolean) : [];
        const pinned = scope.includes(cp);
        if (!cancelled) setLensScopePinned(pinned);
      } catch {
        if (!cancelled) setLensScopePinned(null);
      }
    };
    void run();
    return () => {
      cancelled = true;
    };
  }, [lensId, edgeId, nodeData?.canonicalPath, fragment?.canonicalPath]);

  // --- Human-in-the-Loop (HITL) edge curation UI (admin only) ---
  const [hitlIsAdmin, setHitlIsAdmin] = React.useState<boolean>(false);
  const [hitlAdminChecked, setHitlAdminChecked] = React.useState<boolean>(false);
  const [hitlOpen, setHitlOpen] = React.useState<boolean>(false);
  const [hitlType, setHitlType] = React.useState<string>('');
  const [hitlSwap, setHitlSwap] = React.useState<boolean>(false);
  const [hitlDelete, setHitlDelete] = React.useState<boolean>(false);
  const [hitlBusy, setHitlBusy] = React.useState<boolean>(false);
  const [hitlError, setHitlError] = React.useState<string | null>(null);

  // Determine admin role (best-effort).
  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      try {
        const u = auth?.currentUser;
        if (!u?.uid) {
          if (!cancelled) {
            setHitlIsAdmin(false);
            setHitlAdminChecked(true);
          }
          return;
        }
        const profile: any = await api.getUser(u.uid);
        const role = String(profile?.role || '').trim().toLowerCase();
        if (!cancelled) {
          setHitlIsAdmin(role === 'admin');
          setHitlAdminChecked(true);
        }
      } catch {
        if (!cancelled) {
          setHitlIsAdmin(false);
          setHitlAdminChecked(true);
        }
      }
    };
    run();
    return () => {
      cancelled = true;
    };
  }, []);

  // Reset HITL modal state when a new edge is selected
  React.useEffect(() => {
    if (!edgeId) {
      setHitlOpen(false);
      setHitlError(null);
      setHitlBusy(false);
      setHitlSwap(false);
      setHitlDelete(false);
      setHitlType('');
      return;
    }
    setHitlError(null);
    setHitlBusy(false);
    setHitlSwap(false);
    setHitlDelete(false);
    const t = String(edgeData?.edgeType || edge?.type || '').trim();
    setHitlType(t);
  }, [edgeId, edgeData, edge]);

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

  // Load fragment data when nodeId changes
  React.useEffect(() => {
    if (!nodeId) {
      setFragment(null);
      setError(null);
      setNodeExplainError(null);
      setNodeExplainText(null);
      return;
    }

    // Reset node-scoped UI state immediately on node switch,
    // so we don't show the previous node's details while the new one loads.
    setFragment(null);
    setError(null);
    setNodeExplainError(null);
    setNodeExplainText(null);
    setNodeExplainLoading(false);
    setProductIOError(null);
    setProductIOResult(null);
    setProductIOLoading(false);
    setNewsError(null);

    const loadData = async () => {
      setLoading(true);
      setError(null);
      try {
        const fragmentId = String(nodeData?.fragmentId || '').trim();
        const canonicalPath = String(nodeData?.canonicalPath || '').trim();

        if (fragmentId) {
          const fragmentData = await api.getHorizonFragment(fragmentId);
          setFragment(fragmentData);
          return;
        }

        if (canonicalPath && canonicalPath.startsWith('/')) {
          const fragmentData = await api.getHorizonFragmentByPath(canonicalPath);
          setFragment(fragmentData);
          return;
        }

        const fallbackNodeId = String(nodeId || '').trim();
        if (!fallbackNodeId) {
          setError('Fragment ID is missing');
          setLoading(false);
          return;
        }

        const fragmentData = await api.getHorizonFragment(fallbackNodeId);
        setFragment(fragmentData);
      } catch (err: any) {
        console.error('[HorizonEntityInspector] Failed to load data', err);
        setError(err.response?.data?.error || err.message || 'Failed to load data');
      } finally {
        setLoading(false);
      }
    };

    loadData();
  }, [nodeId, nodeData]);

  const domainContract = React.useMemo(() => {
    const cp = String(fragment?.canonicalPath || nodeData?.canonicalPath || '').trim();
    const t = String(fragment?.type || nodeData?.type || '').trim().toUpperCase();
    const isDomain = t === 'DOMAIN' || cp.startsWith('/domain/');
    if (!isDomain || !cp) return null;

    const identifiers: any = fragment?.identifiers && typeof fragment.identifiers === 'object' ? fragment.identifiers : {};
    const fromId = (k: string) => String(identifiers?.[k] || '').trim();

    const parsedFromPath = (() => {
      if (!cp.startsWith('/domain/v1/')) return { contractType: '', facets: {} as Record<string, string[]> };
      const parts = cp.split('/').filter(Boolean);
      if (parts.length < 3) return { contractType: '', facets: {} as Record<string, string[]> };
      const contractType = String(parts[2] || '').trim();
      const rest = parts.slice(3);
      const facets: Record<string, string[]> = {};
      if (rest.length % 2 !== 0) return { contractType, facets };
      for (let i = 0; i + 1 < rest.length; i += 2) {
        const k = String(rest[i] || '').trim();
        const v = String(rest[i + 1] || '').trim();
        if (!k || !v) continue;
        if (!facets[k]) facets[k] = [];
        facets[k].push(v);
      }
      return { contractType, facets };
    })();

    const facetsFromIdentifiers = (() => {
      const raw = fromId('domainFacetsJson');
      if (!raw) return null as Record<string, string[]> | null;
      try {
        const obj = JSON.parse(raw);
        if (!obj || typeof obj !== 'object') return null;
        const out: Record<string, string[]> = {};
        for (const [k, v] of Object.entries(obj as any)) {
          const kk = String(k || '').trim();
          if (!kk) continue;
          if (Array.isArray(v)) {
            out[kk] = v.map((x) => String(x || '').trim()).filter(Boolean);
          }
        }
        return out;
      } catch {
        return null;
      }
    })();

    const contractType = fromId('domainContractType') || parsedFromPath.contractType;
    const facets = facetsFromIdentifiers || parsedFromPath.facets;

    return {
      canonicalPath: cp,
      title: String(fragment?.labels?.en || '').trim() || '',
      description: String(fragment?.description || '').trim() || '',
      contractType,
      facets,
      source: {
        contractType: fromId('domainContractType') ? 'identifiers.domainContractType' : 'canonicalPath',
        facets: facetsFromIdentifiers ? 'identifiers.domainFacetsJson' : 'canonicalPath',
      },
    };
  }, [fragment, nodeData?.canonicalPath, nodeData?.type]);

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
            preferDisplayLabel(nodeData?.label, nodeData?.canonicalPath, nodeData?.type) ||
            preferDisplayLabel(fragment?.labels?.en, fragment?.canonicalPath, fragment?.type) ||
            humanizeCanonicalPath(nodeData?.canonicalPath || fragment?.canonicalPath) ||
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
      console.error('[HorizonEntityInspector] Explain node failed', err);
      setNodeExplainError(err.response?.data?.error || err.message || 'Failed to explain node');
    } finally {
      setNodeExplainLoading(false);
    }
  }, [nodeId, nodeData, fragment]);

  const handleLoadNewsForNode = React.useCallback(async () => {
    const cp = String(nodeData?.canonicalPath || fragment?.canonicalPath || '').trim();
    if (!cp || !cp.startsWith('/')) {
      setNewsError('Node has no canonical path');
      return;
    }
    setNewsLoading(true);
    setNewsError(null);
    try {
      await api.fetchNewsByFragments({
        fragments: [{
          canonicalPath: cp,
          canonicalName: preferDisplayLabel(nodeData?.label, cp, nodeData?.type) || preferDisplayLabel(fragment?.labels?.en, cp, fragment?.type) || undefined,
        }],
        daysBack: 7,
      });
    } catch (err: any) {
      setNewsError(err?.response?.data?.error || err?.message || 'Failed to load news');
    } finally {
      setNewsLoading(false);
    }
  }, [nodeData, fragment]);

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
            console.warn('[HorizonEntityInspector] Failed to load edge from API, using provided data', err);
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
            console.warn('[HorizonEntityInspector] Failed to load source fragment', err);
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
            console.warn('[HorizonEntityInspector] Failed to load target fragment', err);
            setTargetFragment(null);
          }
        } else {
          setTargetFragment(null);
        }
      } catch (err: any) {
        console.error('[HorizonEntityInspector] Failed to load edge data', err);
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
      console.error('[HorizonEntityInspector] Explain edge failed', err);
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
      console.error('[HorizonEntityInspector] Product IO materialization failed', err);
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

  const safeJSONStringify = React.useCallback((value: any): string => {
    const seen = new WeakSet<object>();
    return JSON.stringify(
      value,
      (_k, v) => {
        if (typeof v === 'bigint') return String(v);
        if (typeof v === 'object' && v !== null) {
          if (seen.has(v as object)) return '[Circular]';
          seen.add(v as object);
        }
        return v;
      },
      2,
    );
  }, []);

  const [techCopyState, setTechCopyState] = React.useState<'idle' | 'copying' | 'copied'>('idle');
  const techCopyTimeoutRef = React.useRef<number | null>(null);
  React.useEffect(() => {
    return () => {
      if (techCopyTimeoutRef.current !== null) {
        window.clearTimeout(techCopyTimeoutRef.current);
        techCopyTimeoutRef.current = null;
      }
    };
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
      EXTRACTS_FROM: 'Extracts',
      TRADED_IN: 'Traded In',
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

  const hitlKnownEdgeTypes = React.useMemo(() => {
    // Keep this list aligned with backend `internal/horizon/models/dependency_edge.go` plus a few UI-only aliases.
    const all = [
      'OWNS',
      'SUBSIDIARY_OF',
      'CONTROLS',
      'MANAGES',
      'INVESTS_IN',
      'EMPLOYED_BY',
      'BOARD_MEMBER_OF',
      'FOUNDED',
      'SUPPLIES_TO',
      'PRODUCES',
      'CONSUMES',
      'TRANSFORMS_INTO',
      'DEPENDS_ON',
      'REQUIRES',
      'NEEDS',
      'USES',
      'REGULATES',
      'INFLUENCES',
      'LOCATED_IN',
      'OPERATES_IN',
      'BASED_IN',
      'HEADQUARTERED_IN',
      'COMPETES_WITH',
      'PARTNERS_WITH',
      'COOPERATES_WITH',
      'CONFLICTS_WITH',
      'OPPOSES',
      // OSINT-lite / governance extensions
      'ASSURES',
      'AUDITED_BY',
      'RELATIVE_OF',
      'RELATED_TO',
      'SIMILAR_TO',
      'LINKED_TO',
      'AFFECTS',
      'IMPACTS',
      'SUPPORTS',
      'EXTRACTS_FROM',
      // Strategic overlay relations
      'CONTROLS_ACCESS',
      'IMPOSES_CONDITION',
      'GATEKEEPS_DECISION',
      'USES_MECHANISM',
      'APPLIES_TO',
      'SURFACES',
    ];
    // De-dupe + stable sort
    return Array.from(new Set(all)).sort((a, b) => a.localeCompare(b));
  }, []);

  const hitlValidate = React.useCallback((): { ok: boolean; message?: string } => {
    // Minimal front-end validation (no fanatism).
    // We only block obvious mismatches for a few strict rules.
    if (hitlDelete) return { ok: true };
    const t = String(hitlType || '').trim().toUpperCase();
    if (!t) return { ok: false, message: 'Select edge type' };
    const sType = String(sourceFragment?.type || '').trim().toUpperCase();
    const tgtType = String(targetFragment?.type || '').trim().toUpperCase();
    if (!sType || !tgtType) return { ok: true };

    // Apply swap to the displayed orientation
    const fromType = hitlSwap ? tgtType : sType;
    const toType = hitlSwap ? sType : tgtType;

    if (t === 'EMPLOYED_BY' || t === 'BOARD_MEMBER_OF') {
      if (fromType !== 'PERSON') return { ok: false, message: `${t}: From должен быть PERSON` };
      if (toType !== 'ORGANIZATION') return { ok: false, message: `${t}: To должен быть ORGANIZATION` };
    }
    if (t === 'LOCATED_IN') {
      if (toType !== 'GEO_REGION') return { ok: false, message: 'LOCATED_IN: To должен быть GEO_REGION' };
    }
    if (t === 'REGULATES') {
      if (fromType !== 'ORGANIZATION') return { ok: false, message: 'REGULATES: From должен быть ORGANIZATION' };
    }
    if (t === 'ASSURES') {
      if (fromType !== 'ORGANIZATION') return { ok: false, message: 'ASSURES: From должен быть ORGANIZATION (assurer)' };
      if (toType !== 'ORGANIZATION' && toType !== 'FACILITY' && toType !== 'PROGRAM') return { ok: false, message: 'ASSURES: To должен быть ORGANIZATION, FACILITY или PROGRAM' };
    }
    if (t === 'AUDITED_BY') {
      if (fromType !== 'ORGANIZATION' && fromType !== 'FACILITY') return { ok: false, message: 'AUDITED_BY: From должен быть ORGANIZATION или FACILITY' };
      if (toType !== 'ORGANIZATION') return { ok: false, message: 'AUDITED_BY: To должен быть ORGANIZATION' };
    }
    if (t === 'EXTRACTS_FROM') {
      if (fromType !== 'FACILITY') return { ok: false, message: 'EXTRACTS_FROM: From должен быть FACILITY' };
      if (toType !== 'DEPOSIT') return { ok: false, message: 'EXTRACTS_FROM: To должен быть DEPOSIT' };
    }
    if (t === 'RELATIVE_OF' || t === 'RELATED_TO') {
      if (fromType !== 'PERSON') return { ok: false, message: `${t}: From должен быть PERSON` };
      if (toType !== 'PERSON') return { ok: false, message: `${t}: To должен быть PERSON` };
    }
    return { ok: true };
  }, [hitlDelete, hitlType, hitlSwap, sourceFragment, targetFragment]);

  const handleHitlApply = React.useCallback(async () => {
    if (!edgeId) return;
    setHitlBusy(true);
    setHitlError(null);
    try {
      const v = hitlValidate();
      if (!v.ok) {
        setHitlError(v.message || 'Invalid edge edit');
        return;
      }

      // Deletion: remove edge + best-effort remove lens item so it disappears from the current lens.
      if (hitlDelete) {
        await api.deleteHorizonEdge(edgeId);
        if (lensId && (edgeData?.lensItemId || edgeId)) {
          const itemId = String(edgeData?.lensItemId || edgeId).trim();
          if (itemId) {
            try {
              await api.deleteLensItem(lensId, itemId);
            } catch {
              // ignore (edge is already gone globally)
            }
          }
        }
        setHitlOpen(false);
        return;
      }

      const desiredType = String(hitlType || '').trim().toUpperCase();
      const shouldSwap = !!hitlSwap;

      // 1) Update global dependency edge via HITL endpoint (admin-only).
      await api.humanInTheLoopUpdateHorizonEdge(edgeId, { type: desiredType, swap: shouldSwap });

      // 2) Update current lens item (so graph reflects new type/direction without a full reload).
      if (lensId) {
        const itemId = String(edgeData?.lensItemId || edgeId).trim();
        if (itemId) {
          const srcId = String(edgeData?.sourceFragmentId || edge?.sourceFragmentId || '').trim();
          const tgtId = String(edgeData?.targetFragmentId || edge?.targetFragmentId || '').trim();
          const nextSrc = shouldSwap ? tgtId : srcId;
          const nextTgt = shouldSwap ? srcId : tgtId;
          await api.updateLensItem(lensId, itemId, {
            edgeType: desiredType as any,
            subkind: desiredType as any,
            sourceFragmentId: nextSrc || undefined,
            targetFragmentId: nextTgt || undefined,
          } as any);
        }
      }

      // Refresh inspector edge snapshot (best-effort)
      try {
        const refreshed = await api.getHorizonEdge(edgeId);
        setEdge(refreshed);
      } catch {
        // ignore
      }

      setHitlOpen(false);
    } catch (err: any) {
      const respData = err?.response?.data;
      const serverMsg =
        (typeof respData === 'string' && respData.trim()) ? respData.trim()
          : (respData?.error ? String(respData.error).trim() : '');
      setHitlError(serverMsg || err?.message || 'Failed to apply edge edit');
    } finally {
      setHitlBusy(false);
    }
  }, [edgeId, lensId, edgeData, edge, hitlDelete, hitlSwap, hitlType, hitlValidate]);

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

  const FieldBlock: React.FC<{ label: string; value?: React.ReactNode; copyText?: string }> = React.useCallback(
    ({ label, value, copyText }) => {
      if (!value) return null;
      return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <div style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{label}</div>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8, minWidth: 0 }}>
            <div style={{ fontSize: 13, color: 'var(--text-heading)', minWidth: 0, overflowWrap: 'anywhere' }}>
              {value}
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
    const fromNodePreferred = preferDisplayLabel(
      nodeData?.label,
      nodeData?.canonicalPath,
      nodeData?.type,
    );
    if (fromNodePreferred) return fromNodePreferred;
    const wantId = String(nodeData?.fragmentId || nodeId || '').trim();
    const loadedId = String((fragment as any)?.id || (fragment as any)?.ID || '').trim();
    const canUseFragment = Boolean(!edgeId && wantId && loadedId && loadedId === wantId);
    const fromFrag = canUseFragment
      ? (() => {
          const preferred = preferDisplayLabel(
            fragment?.labels?.en,
            fragment?.canonicalPath,
            fragment?.type,
          );
          return preferred || humanizeCanonicalPath(fragment?.canonicalPath) || '';
        })()
      : '';
    if (fromFrag) return fromFrag;
    const fromPath = humanizeCanonicalPath(nodeData?.canonicalPath) || '';
    return fromPath || 'Node';
  }, [nodeData, nodeId, edgeId, fragment]);

  const nodeType = React.useMemo(() => {
    const fromNode = String(nodeData?.type || '').trim();
    if (fromNode) return fromNode;
    const wantId = String(nodeData?.fragmentId || nodeId || '').trim();
    const loadedId = String((fragment as any)?.id || (fragment as any)?.ID || '').trim();
    const canUseFragment = Boolean(!edgeId && wantId && loadedId && loadedId === wantId);
    if (!canUseFragment) return '';
    return String(fragment?.type || '').trim();
  }, [nodeData, nodeId, edgeId, fragment]);

  const nodeStatus = React.useMemo(() => {
    const fromNode = String(nodeData?.status || '').trim();
    if (fromNode) return fromNode;
    const wantId = String(nodeData?.fragmentId || nodeId || '').trim();
    const loadedId = String((fragment as any)?.id || (fragment as any)?.ID || '').trim();
    const canUseFragment = Boolean(!edgeId && wantId && loadedId && loadedId === wantId);
    if (!canUseFragment) return '';
    return String(fragment?.status || '').trim();
  }, [nodeData, nodeId, edgeId, fragment]);
  const nodeCanonicalPath = React.useMemo(
    () => {
      const fromNode = String(nodeData?.canonicalPath || '').trim();
      if (fromNode) return fromNode;
      const wantId = String(nodeData?.fragmentId || nodeId || '').trim();
      const loadedId = String((fragment as any)?.id || (fragment as any)?.ID || '').trim();
      const canUseFragment = Boolean(!edgeId && wantId && loadedId && loadedId === wantId);
      if (!canUseFragment) return '';
      return String(fragment?.canonicalPath || '').trim();
    },
    [nodeData, nodeId, edgeId, fragment],
  );
  const fragmentIdForNode = React.useMemo(() => String(nodeData?.fragmentId || nodeId || '').trim(), [nodeData, nodeId]);

  const nodeVisualType = React.useMemo(() => {
    const t = nodeType || fragment?.type;
    const cp = nodeCanonicalPath || fragment?.canonicalPath;
    const lbl = nodeData?.label ||
      fragment?.labels?.en ||
      fragment?.labels?.[Object.keys(fragment?.labels || {})[0]] ||
      fragment?.label;
    const facilityKind = readFacilityKind(nodeData) || readFacilityKind(fragment);
    const fClass = String((fragment as any)?.class || (nodeData as any)?.fragmentClass || '').trim() || null;
    const fSub = String((fragment as any)?.subtype || (nodeData as any)?.fragmentSubtype || '').trim() || null;
    return getHorizonNodeVisualType(t, cp, lbl, facilityKind, fClass, fSub);
  }, [nodeType, nodeCanonicalPath, fragment, nodeData, readFacilityKind]);

  const canUseLoadedFragment = React.useMemo(() => {
    const wantId = String(nodeData?.fragmentId || nodeId || '').trim();
    const loadedId = String((fragment as any)?.id || (fragment as any)?.ID || '').trim();
    return Boolean(!edgeId && wantId && loadedId && loadedId === wantId);
  }, [edgeId, nodeData?.fragmentId, nodeId, fragment]);

  const siteTransforms = React.useMemo(() => {
    if (!canUseLoadedFragment || !fragment) return [];
    return parseSiteTransformsFromFragment(fragment);
  }, [canUseLoadedFragment, fragment]);

  const [directRelationsLoading, setDirectRelationsLoading] = React.useState(false);
  const [directRelationsError, setDirectRelationsError] = React.useState<string | null>(null);
  const directRelationsErrorForRef = React.useRef<string>('');
  const [govLocExpanded, setGovLocExpanded] = React.useState<Record<string, boolean>>({});
  React.useEffect(() => {
    // Reset per-node expansion state when switching nodes.
    setGovLocExpanded({});
  }, [fragmentIdForNode, edgeId]);
  const [directRelations, setDirectRelations] = React.useState<{
    managesOut: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    managesIn: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    boardOut: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    boardIn: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    assuredBy: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    assures: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    locatedOut: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    locatedIn: Array<{ id: string; label: string; canonicalPath: string; type: string }>;
    source: {
      centerFragmentId: string;
      edgesReturned: number;
      neighborFragmentsReturned: number;
    };
  } | null>(null);
  const directRelationsReqKeyRef = React.useRef<string>('');

  React.useEffect(() => {
    const centerFragmentId = String(fragmentIdForNode || '').trim();
    if (!nodeId || edgeId || !centerFragmentId) {
      setDirectRelations(null);
      setDirectRelationsError(null);
      setDirectRelationsLoading(false);
      directRelationsErrorForRef.current = '';
      directRelationsReqKeyRef.current = '';
      return;
    }

    const edgeTypes = ['MANAGES', 'BOARD_MEMBER_OF', 'ASSURES', 'AUDITED_BY', 'REGULATES', 'LOCATED_IN'];
    const reqKey = `${centerFragmentId}|${edgeTypes.join(',')}`;
    if (directRelationsReqKeyRef.current === reqKey) return;
    directRelationsReqKeyRef.current = reqKey;

    let cancelled = false;
    const run = async () => {
      setDirectRelationsLoading(true);
      setDirectRelationsError(null);
      directRelationsErrorForRef.current = centerFragmentId;
      try {
        const resp = await api.getFragmentDependencies(centerFragmentId, {
          direction: 'both',
          edgeTypes,
          includeFragments: true,
          includeUnindexed: true,
          maxPerType: 25,
          maxEdges: 120,
        });
        if (cancelled) return;

        const edges = Array.isArray(resp?.edges) ? resp.edges : [];
        const fragments = Array.isArray(resp?.fragments) ? resp.fragments : [];
        const byId: Record<string, any> = {};
        for (const f of fragments) {
          const id = String(f?.id || '').trim();
          if (id) byId[id] = f;
        }

        const labelFor = (fragId: string): string => {
          const id = String(fragId || '').trim();
          const f = byId[id];
          if (!f) return id;
          const labels = f?.labels;
          const fromLabels =
            labels && typeof labels === 'object'
              ? String(labels.en || labels[Object.keys(labels)[0]] || '').trim()
              : '';
          return fromLabels || humanizeCanonicalPath(String(f?.canonicalPath || '').trim()) || id;
        };

        const entryFor = (fragId: string) => {
          const id = String(fragId || '').trim();
          const f = byId[id];
          return {
            id,
            label: labelFor(id),
            canonicalPath: String(f?.canonicalPath || '').trim(),
            type: String(f?.type || '').trim(),
          };
        };

        const dedupePush = (arr: any[], item: any) => {
          if (!item?.id) return;
          if (arr.some((x) => String(x?.id || '') === item.id)) return;
          arr.push(item);
        };

        const managesOut: any[] = [];
        const managesIn: any[] = [];
        const boardOut: any[] = [];
        const boardIn: any[] = [];
        const assuredBy: any[] = [];
        const assures: any[] = [];
        const locatedOut: any[] = [];
        const locatedIn: any[] = [];

        for (const e of edges) {
          const t = normalizeEdgeType((e as any)?.type);
          const src = String((e as any)?.sourceFragmentId || '').trim();
          const tgt = String((e as any)?.targetFragmentId || '').trim();
          if (!t || !src || !tgt) continue;
          if (src !== centerFragmentId && tgt !== centerFragmentId) continue;

          const outgoing = src === centerFragmentId;
          const otherId = outgoing ? tgt : src;
          const other = entryFor(otherId);

          if (t === 'MANAGES') {
            if (outgoing) dedupePush(managesOut, other);
            else dedupePush(managesIn, other);
          }
          if (t === 'BOARD_MEMBER_OF') {
            if (outgoing) dedupePush(boardOut, other);
            else dedupePush(boardIn, other);
          }
          if (t === 'ASSURES' || t === 'AUDITED_BY' || t === 'REGULATES') {
            // assuredBy: who provides assurance to us (ASSURES incoming, AUDITED_BY outgoing, REGULATES incoming)
            if ((t === 'ASSURES' && !outgoing) || (t === 'AUDITED_BY' && outgoing) || (t === 'REGULATES' && !outgoing)) {
              dedupePush(assuredBy, other);
            }
            // assures: who we provide assurance to (ASSURES outgoing, AUDITED_BY incoming, REGULATES outgoing)
            if ((t === 'ASSURES' && outgoing) || (t === 'AUDITED_BY' && !outgoing) || (t === 'REGULATES' && outgoing)) {
              dedupePush(assures, other);
            }
          }
          if (t === 'LOCATED_IN') {
            if (outgoing) dedupePush(locatedOut, other);
            else dedupePush(locatedIn, other);
          }
        }

        setDirectRelations({
          managesOut,
          managesIn,
          boardOut,
          boardIn,
          assuredBy,
          assures,
          locatedOut,
          locatedIn,
          source: {
            centerFragmentId,
            edgesReturned: edges.length,
            neighborFragmentsReturned: Object.keys(byId).length,
          },
        });
      } catch (err: any) {
        if (cancelled) return;
        setDirectRelations(null);
        setDirectRelationsError(err?.response?.data?.error || err?.message || 'Failed to load direct relationships');
        directRelationsErrorForRef.current = centerFragmentId;
      } finally {
        if (!cancelled) setDirectRelationsLoading(false);
      }
    };

    void run();
    return () => {
      cancelled = true;
    };
  }, [edgeId, fragmentIdForNode, nodeId, normalizeEdgeType]);

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

  const edgePathLegMeta = React.useMemo(() => {
    const src: any = edgeData || edge || {};
    const readStringArray = (value: any): string[] => {
      if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean);
      return [];
    };
    const pathArtifactId = String(src?.pathArtifactId || '').trim();
    const pathRole = String(src?.pathRole || '').trim();
    const pathProductRef = String(src?.pathProductRef || '').trim();
    const pathSupplyLinkId = String(src?.pathSupplyLinkId || '').trim();
    const pathFamilyId = String(src?.pathFamilyId || '').trim();
    const pathVesselType = String(src?.pathVesselType || src?.pathQualifiers?.vesselType || '').trim();
    const dominantFactors = readStringArray(src?.pathDominantFactors);
    const flowRefs = readStringArray(src?.pathFlowRefs);
    const routeOverlays = readLegRouteOverlays({
      routeOverlays: src?.pathRouteOverlays || [],
      routeVariantKey: src?.pathRouteVariantKey,
      transportViaRefs: src?.pathTransportViaRefs,
      routeGeometrySource: src?.pathRouteGeometrySource,
      mode: src?.pathMode,
      flowRefs: src?.pathFlowRefs,
      zoneRefs: src?.pathZoneRefs,
      affectedZoneRefs: src?.pathAffectedZoneRefs,
      spatialSemantics: src?.pathSpatialSemantics,
      vesselType: pathVesselType,
      bindingConfidence: src?.pathBindingConfidence,
    });
    const zoneRefs = readStringArray(src?.pathZoneRefs);
    const affectedZoneRefs = readStringArray(src?.pathAffectedZoneRefs);
    const spatialSemantics = readStringArray(src?.pathSpatialSemantics);
    const hardFailures = readStringArray(src?.pathHardFailures);
    const openGaps = readStringArray(src?.pathOpenGaps);
    const warnings = readStringArray(src?.pathWarnings);
    const joinAssessments = Array.isArray(src?.pathJoinAssessments) ? src.pathJoinAssessments : [];
    const hasMeta =
      !!pathArtifactId ||
      !!pathFamilyId ||
      !!pathRole ||
      typeof src?.pathStressScore === 'number' ||
      typeof src?.pathSegmentIndex === 'number' ||
      !!pathVesselType ||
      !!src?.pathSufficiencyStatus ||
      flowRefs.length > 0 ||
      routeOverlays.length > 0 ||
      zoneRefs.length > 0 ||
      affectedZoneRefs.length > 0 ||
      spatialSemantics.length > 0;
    if (!hasMeta) return null;
    return {
      pathArtifactId,
      pathRole,
      pathProductRef,
      pathSupplyLinkId,
      pathVesselType,
      pathFamilyId,
      pathFamilyPathCount: typeof src?.pathFamilyPathCount === 'number' ? src.pathFamilyPathCount : undefined,
      pathFamilyValidatedCount: typeof src?.pathFamilyValidatedCount === 'number' ? src.pathFamilyValidatedCount : undefined,
      pathFamilyGapCount: typeof src?.pathFamilyGapCount === 'number' ? src.pathFamilyGapCount : undefined,
      pathSufficiencyStatus: String(src?.pathSufficiencyStatus || '').trim() || undefined,
      pathSufficiencyScore: typeof src?.pathSufficiencyScore === 'number' ? src.pathSufficiencyScore : undefined,
      pathGapCount: typeof src?.pathGapCount === 'number' ? src.pathGapCount : undefined,
      pathSufficiencySummary: String(src?.pathSufficiencySummary || '').trim() || undefined,
      pathValidatedJoinCount: typeof src?.pathValidatedJoinCount === 'number' ? src.pathValidatedJoinCount : undefined,
      pathUnresolvedJoinCount: typeof src?.pathUnresolvedJoinCount === 'number' ? src.pathUnresolvedJoinCount : undefined,
      pathSegmentIndex: typeof src?.pathSegmentIndex === 'number' ? src.pathSegmentIndex : undefined,
      pathCarriesProduct: Boolean(src?.pathCarriesProduct),
      pathStressScore: typeof src?.pathStressScore === 'number' ? src.pathStressScore : undefined,
      pathFragility: typeof src?.pathFragility === 'number' ? src.pathFragility : undefined,
      pathRerouteability: typeof src?.pathRerouteability === 'number' ? src.pathRerouteability : undefined,
      pathZonePressure: typeof src?.pathZonePressure === 'number' ? src.pathZonePressure : undefined,
      pathAffectedZonePressure: typeof src?.pathAffectedZonePressure === 'number' ? src.pathAffectedZonePressure : undefined,
      pathRecoveryLatencyHours: typeof src?.pathRecoveryLatencyHours === 'number' ? src.pathRecoveryLatencyHours : undefined,
      pathBindingConfidence: typeof src?.pathBindingConfidence === 'number' ? src.pathBindingConfidence : undefined,
      pathBottleneck: Boolean(src?.pathBottleneck),
      dominantFactors,
      flowRefs,
      routeOverlays,
      zoneRefs,
      affectedZoneRefs,
      spatialSemantics,
      hardFailures,
      openGaps,
      warnings,
      joinAssessments,
    };
  }, [edgeData, edge]);

  const edgeCriticalityMeta = React.useMemo(() => {
    const src: any = edgeData || edge || {};
    const qualifiers: Record<string, any> = {
      ...((edge as any)?.qualifiers || {}),
      ...((edge as any)?.properties?.qualifiers || {}),
      ...((src?.qualifiers && typeof src.qualifiers === 'object') ? src.qualifiers : {}),
    };
    const readStringArray = (value: any): string[] => {
      if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean);
      return [];
    };
    const summary =
      String(src?.criticalSummary || qualifiers?.criticalSummary || '').trim() ||
      (
        edgePathLegMeta && typeof edgePathLegMeta.pathStressScore === 'number'
          ? `${edgePathLegMeta.pathBottleneck ? 'Bottleneck leg' : 'Route leg'} stress ${edgePathLegMeta.pathStressScore.toFixed(2)}`
          : ''
      );
    const primaryMechanism = String(src?.primaryRiskMechanism || qualifiers?.primaryRiskMechanism || '').trim();
    const primaryMetricKey = String(src?.primaryCriticalMetricKey || qualifiers?.primaryCriticalMetricKey || '').trim();
    const primaryMetricLabel = String(src?.primaryCriticalMetricLabel || qualifiers?.primaryCriticalMetricLabel || '').trim();
    const primaryMetricUnit = String(src?.primaryCriticalMetricUnit || qualifiers?.primaryCriticalMetricUnit || '').trim();
    const primaryMetricValueRaw =
      src?.primaryCriticalMetricValue ??
      qualifiers?.primaryCriticalMetricValue ??
      (edgePathLegMeta && typeof edgePathLegMeta.pathStressScore === 'number' ? edgePathLegMeta.pathStressScore : undefined);
    const riskFlags = readStringArray(src?.riskFlags || qualifiers?.riskFlags);
    const gateStatus = String(src?.riskGateStatus || qualifiers?.riskGateStatus || '').trim();
    const coverageStatus = String(src?.riskCoverageStatus || qualifiers?.riskCoverageStatus || '').trim();
    const explicitCriticality =
      typeof edgeData?.criticality === 'number'
        ? edgeData.criticality
        : (typeof edge?.properties?.criticality === 'number' ? edge.properties.criticality : undefined);
    const criticalityScore =
      typeof explicitCriticality === 'number'
        ? explicitCriticality
        : (edgePathLegMeta && typeof edgePathLegMeta.pathStressScore === 'number' ? edgePathLegMeta.pathStressScore : undefined);
    if (!summary && !primaryMechanism && !primaryMetricKey && riskFlags.length === 0 && typeof criticalityScore !== 'number') {
      return null;
    }
    return {
      summary,
      primaryMechanism,
      primaryMetricKey,
      primaryMetricLabel,
      primaryMetricUnit,
      primaryMetricValue: typeof primaryMetricValueRaw === 'number' ? primaryMetricValueRaw : undefined,
      riskFlags,
      gateStatus,
      coverageStatus,
      criticalityScore,
    };
  }, [edgeData, edge, edgePathLegMeta]);

  const nodeAccent = React.useMemo(() => {
    return getNodeColors(nodeVisualType || nodeType || fragment?.type, theme === 'dark' ? 'dark' : 'light').accent;
  }, [nodeVisualType, nodeType, fragment, theme]);

  const nodeTypeHuman = React.useMemo(() => {
    const lbl =
      nodeData?.label ||
      fragment?.labels?.en ||
      fragment?.labels?.[Object.keys(fragment?.labels || {})[0]] ||
      fragment?.label;
    return formatHorizonFragmentType(nodeType, nodeCanonicalPath, lbl, {
      fragmentClass: (fragment as any)?.class || (nodeData as any)?.fragmentClass,
      fragmentSubtype: undefined,
    });
  }, [nodeType, nodeCanonicalPath, nodeData, fragment]);

  const nodeSubtypeHuman = React.useMemo(() => {
    const raw = String((fragment as any)?.subtype || (nodeData as any)?.fragmentSubtype || '').trim();
    if (!raw) return '';
    return formatHorizonFragmentType(raw, '', '');
  }, [fragment, nodeData]);

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
      {/* On-demand resolver modal (no writes) */}
      {onDemandOpen && onDemandCtx && (
        <HorizonOnDemandDependencyResolverModal
          open={onDemandOpen}
          onClose={() => setOnDemandOpen(false)}
          context={onDemandCtx}
        />
      )}

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
                <HorizonNodeIcon type={nodeVisualType || nodeType} color={nodeAccent} size={18} />
        </div>
      )}
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)', lineHeight: 1.25 }}>
                {edgeId ? 'Relationship' : nodeDisplayName}
              </div>
              {edgeId && (
                <div style={{ marginTop: 4, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  <Chip tone="info">
                    <span style={{ width: 8, height: 8, borderRadius: 99, background: edgeStroke }} />
                    {edgeCaption}
                  </Chip>
                  {edgeTypeRaw && <Chip>{edgeTypeRaw}</Chip>}
                </div>
              )}
              {!edgeId && (
                <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {nodeCanonicalPath && (
                    <FieldBlock label="Canonical path" value={<span style={mono}>{nodeCanonicalPath}</span>} copyText={nodeCanonicalPath} />
                  )}
                  {(nodeType ||
                    String((fragment as any)?.class || (nodeData as any)?.fragmentClass || '').trim()) && (
                    <FieldBlock
                      label="Class"
                      value={
                        <AccentChip accent={nodeAccent}>
                          <HorizonNodeIcon type={nodeVisualType || nodeType} color={nodeAccent} size={14} />
                          {nodeTypeHuman}
                        </AccentChip>
                      }
                    />
                  )}
                  {nodeSubtypeHuman && (
                    <FieldBlock
                      label="Subtype"
                      value={<span style={{ fontWeight: 650, color: 'var(--text-heading)' }}>{nodeSubtypeHuman}</span>}
                      copyText={String(
                        (fragment as any)?.subtype || (nodeData as any)?.fragmentSubtype || '',
                      ).trim()}
                    />
                  )}
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
                        humanizeCanonicalPath(sourceFragment.canonicalPath) ||
                        'Unknown'}
                    </div>
                    {(sourceFragment?.type || sourceFragment?.class) && (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        {(() => {
                          const vt = getHorizonNodeVisualType(
                            sourceFragment.type,
                            sourceFragment.canonicalPath,
                            sourceFragment.labels?.en || sourceFragment.labels?.[Object.keys(sourceFragment.labels || {})[0]],
                            readFacilityKind(sourceFragment),
                            sourceFragment.class,
                            sourceFragment.subtype,
                          );
                          const accent = getNodeColors(vt || sourceFragment.type, theme === 'dark' ? 'dark' : 'light').accent;
                          return (
                            <AccentChip accent={accent}>
                              <HorizonNodeIcon type={vt || sourceFragment.type} color={accent} size={14} />
                              {formatHorizonFragmentType(
                                sourceFragment.type,
                                sourceFragment.canonicalPath,
                                sourceFragment.labels?.en,
                                { fragmentClass: sourceFragment.class, fragmentSubtype: sourceFragment.subtype },
                              )}
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
                        humanizeCanonicalPath(targetFragment.canonicalPath) ||
                        'Unknown'}
                    </div>
                    {(targetFragment?.type || targetFragment?.class) && (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        {(() => {
                          const vt = getHorizonNodeVisualType(
                            targetFragment.type,
                            targetFragment.canonicalPath,
                            targetFragment.labels?.en || targetFragment.labels?.[Object.keys(targetFragment.labels || {})[0]],
                            readFacilityKind(targetFragment),
                            targetFragment.class,
                            targetFragment.subtype,
                          );
                          const accent = getNodeColors(vt || targetFragment.type, theme === 'dark' ? 'dark' : 'light').accent;
                          return (
                            <AccentChip accent={accent}>
                              <HorizonNodeIcon type={vt || targetFragment.type} color={accent} size={14} />
                              {formatHorizonFragmentType(
                                targetFragment.type,
                                targetFragment.canonicalPath,
                                targetFragment.labels?.en,
                                { fragmentClass: targetFragment.class, fragmentSubtype: targetFragment.subtype },
                              )}
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

          {edgePathLegMeta && (
            <SectionCard
              title="Path leg"
              right={
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                  {typeof edgePathLegMeta.pathStressScore === 'number' && (
                    <Chip tone={edgePathLegMeta.pathStressScore >= 0.58 ? 'danger' : 'info'}>
                      stress {edgePathLegMeta.pathStressScore.toFixed(2)}
                    </Chip>
                  )}
                  {edgePathLegMeta.pathBottleneck && <Chip tone="danger">bottleneck</Chip>}
                  {typeof edgePathLegMeta.pathBindingConfidence === 'number' && (
                    <Chip>bind {Math.round(edgePathLegMeta.pathBindingConfidence * 100)}%</Chip>
                  )}
                  {edgePathLegMeta.pathVesselType && <Chip>{edgePathLegMeta.pathVesselType}</Chip>}
                </div>
              }
            >
              {typeof edgePathLegMeta.pathSegmentIndex === 'number' && (
                <KV k="Segment" v={<span style={mono}>{edgePathLegMeta.pathSegmentIndex}</span>} />
              )}
              {edgePathLegMeta.pathSufficiencyStatus && (
                <KV
                  k="Continuity"
                  v={
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                      <Chip
                        tone={
                          edgePathLegMeta.pathSufficiencyStatus === 'validated'
                            ? 'info'
                            : edgePathLegMeta.pathSufficiencyStatus === 'forensic_gap'
                              ? 'danger'
                              : 'info'
                        }
                      >
                        {edgePathLegMeta.pathSufficiencyStatus === 'forensic_gap'
                          ? 'gap'
                          : edgePathLegMeta.pathSufficiencyStatus}
                      </Chip>
                      {typeof edgePathLegMeta.pathSufficiencyScore === 'number' && (
                        <Chip>suff {Math.round(edgePathLegMeta.pathSufficiencyScore * 100)}%</Chip>
                      )}
                      {typeof edgePathLegMeta.pathValidatedJoinCount === 'number' && (
                        <Chip>
                          joins {edgePathLegMeta.pathValidatedJoinCount}
                          {typeof edgePathLegMeta.pathUnresolvedJoinCount === 'number'
                            ? ` • unresolved ${edgePathLegMeta.pathUnresolvedJoinCount}`
                            : ''}
                        </Chip>
                      )}
                      {typeof edgePathLegMeta.pathGapCount === 'number' && edgePathLegMeta.pathGapCount > 0 && (
                        <Chip tone="info">gaps {edgePathLegMeta.pathGapCount}</Chip>
                      )}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.pathSufficiencySummary && (
                <KV k="Summary" v={<span>{edgePathLegMeta.pathSufficiencySummary}</span>} />
              )}
              {edgePathLegMeta.pathRole && <KV k="Role" v={<span>{edgePathLegMeta.pathRole}</span>} />}
              {edgePathLegMeta.pathVesselType && <KV k="Vessel type" v={<span>{edgePathLegMeta.pathVesselType}</span>} />}
              {edgePathLegMeta.pathArtifactId && (
                <KV k="Path artifact" v={<span style={mono}>{edgePathLegMeta.pathArtifactId}</span>} copyText={edgePathLegMeta.pathArtifactId} />
              )}
              {edgePathLegMeta.pathFamilyId && (
                <KV
                  k="Family"
                  v={
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                      <span style={mono}>{edgePathLegMeta.pathFamilyId}</span>
                      {typeof edgePathLegMeta.pathFamilyPathCount === 'number' && (
                        <Chip>
                          {typeof edgePathLegMeta.pathFamilyValidatedCount === 'number'
                            ? `${edgePathLegMeta.pathFamilyValidatedCount}/${edgePathLegMeta.pathFamilyPathCount}`
                            : `${edgePathLegMeta.pathFamilyPathCount} paths`}
                        </Chip>
                      )}
                      {typeof edgePathLegMeta.pathFamilyGapCount === 'number' && edgePathLegMeta.pathFamilyGapCount > 0 && (
                        <Chip tone="info">family gaps {edgePathLegMeta.pathFamilyGapCount}</Chip>
                      )}
                    </div>
                  }
                  copyText={edgePathLegMeta.pathFamilyId}
                />
              )}
              {edgePathLegMeta.pathSupplyLinkId && (
                <KV k="Supply link" v={<span style={mono}>{edgePathLegMeta.pathSupplyLinkId}</span>} copyText={edgePathLegMeta.pathSupplyLinkId} />
              )}
              {edgePathLegMeta.pathProductRef && (
                <KV k="Product" v={<span style={mono}>{edgePathLegMeta.pathProductRef}</span>} copyText={edgePathLegMeta.pathProductRef} />
              )}
              <KV
                k="Carries product"
                v={<span>{edgePathLegMeta.pathCarriesProduct ? 'Yes' : 'No'}</span>}
              />
              {typeof edgePathLegMeta.pathRecoveryLatencyHours === 'number' && (
                <KV k="Recovery (h)" v={<span style={mono}>{edgePathLegMeta.pathRecoveryLatencyHours}</span>} />
              )}
              {(typeof edgePathLegMeta.pathFragility === 'number' ||
                typeof edgePathLegMeta.pathRerouteability === 'number' ||
                typeof edgePathLegMeta.pathZonePressure === 'number' ||
                typeof edgePathLegMeta.pathAffectedZonePressure === 'number') && (
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {typeof edgePathLegMeta.pathFragility === 'number' && <Chip>fragility {edgePathLegMeta.pathFragility.toFixed(2)}</Chip>}
                  {typeof edgePathLegMeta.pathRerouteability === 'number' && <Chip>reroute {edgePathLegMeta.pathRerouteability.toFixed(2)}</Chip>}
                  {typeof edgePathLegMeta.pathZonePressure === 'number' && <Chip>zone {edgePathLegMeta.pathZonePressure.toFixed(2)}</Chip>}
                  {typeof edgePathLegMeta.pathAffectedZonePressure === 'number' && <Chip>affected {edgePathLegMeta.pathAffectedZonePressure.toFixed(2)}</Chip>}
                </div>
              )}
              {edgePathLegMeta.spatialSemantics.length > 0 && (
                <KV
                  k="Spatial semantics"
                  v={
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {edgePathLegMeta.spatialSemantics.map((item) => <Chip key={item}>{item}</Chip>)}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.flowRefs.length > 0 && (
                <KV
                  k="Flows"
                  v={
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {edgePathLegMeta.flowRefs.map((item) => <Chip key={item}>{item}</Chip>)}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.routeOverlays.length > 0 && (
                <KV
                  k="Route overlays"
                  v={
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      {(() => {
                        const primary = selectPrimaryLegRouteOverlay(edgePathLegMeta.routeOverlays);
                        return edgePathLegMeta.routeOverlays.map((overlay) => {
                        const label = describeRouteOverlay(overlay) || overlay.routeGeometrySource || overlay.routeVariantKey || overlay.overlayKey;
                        return (
                          <div key={overlay.overlayKey} style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                            <Chip tone={primary?.overlayKey === overlay.overlayKey ? 'info' : undefined}>
                              {primary?.overlayKey === overlay.overlayKey ? `primary: ${label}` : label}
                            </Chip>
                            {overlay.routeVariantKey ? <Chip>{overlay.routeVariantKey}</Chip> : null}
                          </div>
                        );
                        });
                      })()}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.zoneRefs.length > 0 && (
                <KV
                  k="Zones"
                  v={
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {edgePathLegMeta.zoneRefs.map((item) => <Chip key={item}>{item}</Chip>)}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.affectedZoneRefs.length > 0 && (
                <KV
                  k="Affected"
                  v={
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {edgePathLegMeta.affectedZoneRefs.map((item) => <Chip key={item} tone="danger">{item}</Chip>)}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.dominantFactors.length > 0 && (
                <KV
                  k="Dominant factors"
                  v={
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {edgePathLegMeta.dominantFactors.map((item) => <Chip key={item} tone="info">{item}</Chip>)}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.joinAssessments.length > 0 && (
                <KV
                  k="Join checks"
                  v={
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      {edgePathLegMeta.joinAssessments.slice(0, 4).map((item: any) => (
                        <div key={`${item?.index}-${item?.leftLegIndex}-${item?.rightLegIndex}`} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                          <strong>{String(item?.status || 'unknown')}</strong>
                          {` • ${String(item?.actorContinuity || 'n/a')}`}
                          {` • ${String(item?.productContinuity || 'n/a')}`}
                          {` • ${String(item?.temporalContinuity || 'n/a')}`}
                          {Array.isArray(item?.reasons) && item.reasons.length > 0 ? ` • ${item.reasons.join('; ')}` : ''}
                        </div>
                      ))}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.hardFailures.length > 0 && (
                <KV
                  k="Hard failures"
                  v={
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      {edgePathLegMeta.hardFailures.map((item: string) => (
                        <div key={item} style={{ fontSize: 12, color: 'var(--text-error)' }}>{item}</div>
                      ))}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.openGaps.length > 0 && (
                <KV
                  k="Open gaps"
                  v={
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      {edgePathLegMeta.openGaps.map((item: string) => (
                        <div key={item} style={{ fontSize: 12, color: 'var(--text-warning)' }}>{item}</div>
                      ))}
                    </div>
                  }
                />
              )}
              {edgePathLegMeta.warnings.length > 0 && (
                <KV
                  k="Warnings"
                  v={
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      {edgePathLegMeta.warnings.map((item: string) => (
                        <div key={item} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{item}</div>
                      ))}
                    </div>
                  }
                />
              )}
            </SectionCard>
          )}

          {edgeCriticalityMeta && (
            <SectionCard title="Criticality">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {edgeCriticalityMeta.summary && (
                  <div style={{ fontSize: 12, lineHeight: 1.5, color: 'var(--text-heading)' }}>{edgeCriticalityMeta.summary}</div>
                )}
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {typeof edgeCriticalityMeta.criticalityScore === 'number' && (
                    <Chip tone={edgeCriticalityMeta.criticalityScore >= 0.58 ? 'danger' : 'info'}>
                      criticality {edgeCriticalityMeta.criticalityScore.toFixed(2)}
                    </Chip>
                  )}
                  {edgeCriticalityMeta.primaryMechanism && <Chip>{edgeCriticalityMeta.primaryMechanism}</Chip>}
                  {edgeCriticalityMeta.gateStatus && <Chip>{edgeCriticalityMeta.gateStatus}</Chip>}
                  {edgeCriticalityMeta.coverageStatus && <Chip>{edgeCriticalityMeta.coverageStatus}</Chip>}
                  {edgeCriticalityMeta.riskFlags.map((flag: string) => (
                    <Chip key={flag} tone="danger">{flag}</Chip>
                  ))}
                </div>
                {(edgeCriticalityMeta.primaryMetricKey || edgeCriticalityMeta.primaryMetricLabel) && (
                  <KV
                    k={edgeCriticalityMeta.primaryMetricLabel || edgeCriticalityMeta.primaryMetricKey}
                    v={
                      <span style={mono}>
                        {typeof edgeCriticalityMeta.primaryMetricValue === 'number'
                          ? `${edgeCriticalityMeta.primaryMetricValue}${edgeCriticalityMeta.primaryMetricUnit ? ` ${edgeCriticalityMeta.primaryMetricUnit}` : ''}`
                          : 'n/a'}
                      </span>
                    }
                  />
                )}
              </div>
            </SectionCard>
          )}

          {/* Admin-only: Human-in-the-Loop edge curation */}
          {hitlAdminChecked && hitlIsAdmin && (
            <div style={{ position: 'sticky', bottom: 12, zIndex: 5, marginTop: 12 }}>
              <div style={{ display: 'flex', justifyContent: 'flex-start' }}>
              <button
                type="button"
                  onClick={() => {
                    setHitlError(null);
                    setHitlOpen(true);
                  }}
                style={{
                  padding: '8px 10px',
                    fontSize: 12,
                    fontWeight: 900,
                    borderRadius: 12,
                    border: '1px solid rgba(239, 68, 68, 0.35)',
                    background: 'rgba(239, 68, 68, 0.10)',
                    color: theme === 'dark' ? 'rgba(248,113,113,0.95)' : 'rgba(185,28,28,0.95)',
                    cursor: 'pointer',
                  }}
                  title="Admin: correct or delete this edge"
                >
                  Wrong edge
              </button>
              </div>
                </div>
              )}

          {hitlOpen && (
            <>
                <div
                  style={{
                  position: 'fixed',
                  inset: 0,
                  zIndex: 2100,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.45)' : 'rgba(15,23,42,0.22)',
                  backdropFilter: 'blur(10px)',
                }}
                onMouseDown={() => {
                  if (!hitlBusy) setHitlOpen(false);
                }}
              />
              <div
                style={{
                  position: 'fixed',
                  left: '50%',
                  top: '50%',
                  transform: 'translate(-50%, -50%)',
                  width: 560,
                  maxWidth: 'calc(100vw - 24px)',
                  zIndex: 2150,
                  borderRadius: 16,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(15,23,42,0.10)'}`,
                  background: theme === 'dark' ? 'rgba(10,10,12,0.78)' : 'rgba(255,255,255,0.80)',
                  backdropFilter: 'blur(12px)',
                  boxShadow: theme === 'dark' ? '0 22px 70px rgba(0,0,0,0.60)' : '0 22px 70px rgba(0,0,0,0.18)',
                  overflow: 'hidden',
                }}
                onMouseDown={(e) => e.stopPropagation()}
              >
                <div
                  style={{
                    padding: '12px 14px',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    borderBottom: '1px solid var(--border-subtle)',
                  }}
                >
                  <div style={{ fontSize: 13, fontWeight: 900, color: 'var(--text-heading)' }}>Wrong edge</div>
                  <button
                    type="button"
                    disabled={hitlBusy}
                    onClick={() => setHitlOpen(false)}
                    style={{
                      width: 30,
                      height: 30,
                      borderRadius: 10,
                      border: '1px solid var(--border-subtle)',
                      background: 'transparent',
                    color: 'var(--text-heading)',
                      cursor: hitlBusy ? 'not-allowed' : 'pointer',
                      opacity: hitlBusy ? 0.4 : 0.8,
                      fontSize: 16,
                      fontWeight: 900,
                      lineHeight: 1,
                    }}
                    aria-label="Close"
                    title="Close"
                  >
                    ×
                  </button>
                </div>

                <div style={{ padding: 14, display: 'flex', flexDirection: 'column', gap: 12 }}>
                  <div style={{ display: 'grid', gridTemplateColumns: '120px 1fr', gap: 12, alignItems: 'center' }}>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary)', fontWeight: 750 }}>Type</div>
                    <select
                      value={String(hitlType || '').trim()}
                      disabled={hitlBusy || hitlDelete}
                      onChange={(e) => setHitlType(e.target.value)}
                      style={{
                        width: '100%',
                        padding: '10px 10px',
                        borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-elevated)',
                        color: 'var(--text-heading)',
                        fontSize: 12,
                        fontWeight: 750,
                      }}
                    >
                      <option value="">Select…</option>
                      {hitlKnownEdgeTypes.map((t) => (
                        <option key={t} value={t}>
                          {humanizeEdgeType(t)} ({t})
                        </option>
                      ))}
                    </select>
                </div>

                  <div style={{ display: 'grid', gridTemplateColumns: '120px 1fr', gap: 12, alignItems: 'center' }}>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary)', fontWeight: 750 }}>Direction</div>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <input
                        type="checkbox"
                        checked={hitlSwap}
                        disabled={hitlBusy || hitlDelete}
                        onChange={(e) => setHitlSwap(e.target.checked)}
                      />
                      <span style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 750 }}>
                        Swap From/To
                </span>
                    </label>
              </div>

                  <div style={{ padding: 10, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary)', fontWeight: 800, marginBottom: 6 }}>
                      Effective
                </div>
                    <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 800, display: 'flex', gap: 8, alignItems: 'center' }}>
                      <span style={{ maxWidth: 240, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {hitlSwap ? (targetFragment?.labels?.en || targetFragment?.canonicalPath?.split('/')?.pop() || (edge?.targetFragmentId || edgeData?.targetFragmentId) || 'Unknown')
                          : (sourceFragment?.labels?.en || sourceFragment?.canonicalPath?.split('/')?.pop() || (edge?.sourceFragmentId || edgeData?.sourceFragmentId) || 'Unknown')}
                  </span>
                      <span style={{ color: 'var(--text-secondary-strong)', fontWeight: 900 }}>→</span>
                      <span style={{ maxWidth: 240, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {hitlSwap ? (sourceFragment?.labels?.en || sourceFragment?.canonicalPath?.split('/')?.pop() || (edge?.sourceFragmentId || edgeData?.sourceFragmentId) || 'Unknown')
                          : (targetFragment?.labels?.en || targetFragment?.canonicalPath?.split('/')?.pop() || (edge?.targetFragmentId || edgeData?.targetFragmentId) || 'Unknown')}
                  </span>
                </div>
                </div>

                  <div style={{ display: 'flex', gap: 10, alignItems: 'center', justifyContent: 'space-between' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <input
                        type="checkbox"
                        checked={hitlDelete}
                        disabled={hitlBusy}
                        onChange={(e) => setHitlDelete(e.target.checked)}
                      />
                      <span style={{ fontSize: 12, fontWeight: 800, color: theme === 'dark' ? 'rgba(248,113,113,0.95)' : 'rgba(185,28,28,0.95)' }}>
                        Delete this edge
                  </span>
                    </label>

                    <button
                      type="button"
                      onClick={handleHitlApply}
                      disabled={hitlBusy || !hitlValidate().ok}
                      style={{
                        padding: '10px 14px',
                        fontSize: 12,
                        fontWeight: 900,
                        borderRadius: 12,
                        border: '1px solid var(--border-subtle)',
                        background: hitlBusy ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                        color: 'var(--text-heading)',
                        cursor: hitlBusy ? 'not-allowed' : 'pointer',
                        opacity: hitlBusy ? 0.7 : 1,
                        minWidth: 120,
                      }}
                    >
                      {hitlBusy ? 'Applying…' : 'Apply'}
                    </button>
                        </div>

                  {hitlError && (
                    <div style={{ fontSize: 12, color: '#f87171', fontWeight: 650 }}>
                      {hitlError}
                        </div>
                    )}
                  </div>
                </div>
            </>
          )}

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
            edge?.properties ||
            edge?.direction ||
            edge?.validFrom ||
            edge?.validTo) && (
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
              {edge?.validFrom && <KV k="Valid from" v={<span style={mono}>{String(edge.validFrom)}</span>} copyText={String(edge.validFrom)} />}
              {edge?.validTo && <KV k="Valid to" v={<span style={mono}>{String(edge.validTo)}</span>} copyText={String(edge.validTo)} />}
            </SectionCard>
          )}

          {edge?.metrics && Object.keys(edge.metrics).length > 0 && (
            <SectionCard title="Metrics">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {Object.entries(edge.metrics).map(([k, mv]) => (
                  <KV key={k} k={k} v={<span style={mono}>{formatMetricValue(mv)}</span>} />
                ))}
              </div>
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

          <Disclosure title="Technical" subtitle="IDs">
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
          {/* Overview: primary business classification should be visible immediately (not hidden in disclosures). */}
          {canUseLoadedFragment && fragment && (String((fragment as any)?.type || '').toUpperCase() === 'ORGANIZATION' || String((fragment as any)?.type || '').toUpperCase() === 'FACILITY') && (
            <SectionCard title="Overview">
              <KV
                k="Org size"
                v={<span style={mono}>{(fragment as any)?.orgSize ? String((fragment as any).orgSize) : '-'}</span>}
                copyText={(fragment as any)?.orgSize ? String((fragment as any).orgSize) : undefined}
              />
              <KV
                k="Market segments"
                v={(() => {
                  const segsRaw = (fragment as any)?.marketSegments;
                  const segs = Array.isArray(segsRaw) ? segsRaw.map((p: any) => String(p || '').trim()).filter(Boolean) : [];
                  if (segs.length === 0) return <span style={mono}>-</span>;

                  const labelFor = (p: string) => {
                    const parts = p.split('/').filter(Boolean);
                    // /segment/{domain}/{vertical}/{geo}
                    if (parts.length >= 4 && parts[0] === 'segment') {
                      const domain = parts[1] || '';
                      const vertical = parts[2] || '';
                      const geo = parts[3] || '';
                      if (vertical && geo) return `${vertical} · ${geo}`;
                      if (vertical) return vertical;
                      if (domain && geo) return `${domain} · ${geo}`;
                      return domain || p;
                    }
                    return p;
                  };

                  // Keep the overview compact: show a few chips, but keep all accessible.
                  const PREVIEW = 4;
                  const preview = segs.slice(0, PREVIEW);
                  const hiddenN = Math.max(0, segs.length - preview.length);

                  return (
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {preview.map((p) => (
                        <span key={p} title={p}>
                          <Chip>{labelFor(p)}</Chip>
                        </span>
                      ))}
                      {hiddenN > 0 && (
                        <span title={segs.join('\n')}>
                          <Chip>+{hiddenN} more</Chip>
                        </span>
                      )}
                    </div>
                  );
                })()}
                copyText={(() => {
                  const segsRaw = (fragment as any)?.marketSegments;
                  const segs = Array.isArray(segsRaw) ? segsRaw.map((p: any) => String(p || '').trim()).filter(Boolean) : [];
                  return segs.length > 0 ? segs.join('\n') : undefined;
                })()}
              />
            </SectionCard>
          )}

          {canUseLoadedFragment && siteTransforms.length > 0 && (
            <SectionCard
              title="Site transforms"
              right={<Chip tone="info">{siteTransforms.length}</Chip>}
            >
              <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginBottom: 10, lineHeight: 1.45 }}>
                Product transitions recorded on this fragment (<span style={mono}>attributes.site_transforms</span>).
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {siteTransforms.map((st, idx) => {
                  const key = `${st.kind}|${st.inputProducts.join(',')}->${st.outputProducts.join(',')}|${idx}`;
                  const copyLine = [
                    st.kind ? `kind=${st.kind}` : '',
                    st.inputProducts.length ? `in=${st.inputProducts.join(';')}` : '',
                    st.outputProducts.length ? `out=${st.outputProducts.join(';')}` : '',
                    typeof st.yieldRatio === 'number' ? `yield=${st.yieldRatio}` : '',
                    typeof st.confidence === 'number' ? `confidence=${st.confidence}` : '',
                    st.source ? `source=${st.source}` : '',
                  ]
                    .filter(Boolean)
                    .join(' ');
                  return (
                    <div
                      key={key}
                      style={{
                        borderRadius: 12,
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-secondary)',
                        padding: 10,
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 8,
                      }}
                    >
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
                        {st.kind ? <Chip>{formatHorizonFragmentType(st.kind, '', '')}</Chip> : <Chip tone="neutral">unknown kind</Chip>}
                        {typeof st.yieldRatio === 'number' && (
                          <Chip tone="info">yield {st.yieldRatio}</Chip>
                        )}
                        {typeof st.confidence === 'number' && (
                          <Chip>conf {formatSiteTransformConfidence(st.confidence)}</Chip>
                        )}
                      </div>
                      <KV
                        k="Inputs"
                        v={
                          <span style={mono}>
                            {st.inputProducts.length > 0 ? st.inputProducts.join(', ') : '—'}
                          </span>
                        }
                        copyText={st.inputProducts.length > 0 ? st.inputProducts.join('\n') : undefined}
                      />
                      <KV
                        k="Outputs"
                        v={
                          <span style={mono}>
                            {st.outputProducts.length > 0 ? st.outputProducts.join(', ') : '—'}
                          </span>
                        }
                        copyText={st.outputProducts.length > 0 ? st.outputProducts.join('\n') : undefined}
                      />
                      {st.source ? (
                        <KV k="Source" v={<span style={mono}>{st.source}</span>} copyText={st.source} />
                      ) : null}
                      {copyLine ? (
                        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                          <button
                            type="button"
                            onClick={() => void copyToClipboard(copyLine)}
                            title="Copy transform summary"
                            style={{
                              padding: '4px 8px',
                              fontSize: 11,
                              fontWeight: 650,
                              borderRadius: 8,
                              border: '1px solid var(--border-subtle)',
                              background: 'var(--bg-tertiary)',
                              color: 'var(--text-secondary-strong)',
                              cursor: 'pointer',
                            }}
                          >
                            Copy
                          </button>
                        </div>
                      ) : null}
                    </div>
                  );
                })}
              </div>
            </SectionCard>
          )}

          {(() => {
            const currentFid = String(fragmentIdForNode || '').trim();
            if (!currentFid) return null;

            const showErr = Boolean(directRelationsError && directRelationsErrorForRef.current === currentFid);
            const d =
              currentFid && directRelations?.source?.centerFragmentId === currentFid
                ? directRelations
                : null;
            const showSkeleton = Boolean(!showErr && currentFid && !d);
            const PREVIEW_N = 3;

            const renderList = (items: any[]) => {
              return (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {items.map((n) => {
                    const accent = getNodeColors(n.type || 'UNKNOWN', theme === 'dark' ? 'dark' : 'light').accent;
                    return (
                      <div key={n.id} style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                          {n.type ? (
                            <div
                              title={formatHorizonFragmentType(n.type)}
                              style={{
                                width: 28,
                                height: 28,
                                borderRadius: 999,
                                border: `1px solid ${hexToRgba(accent, theme === 'dark' ? 0.40 : 0.28)}`,
                                background: hexToRgba(accent, theme === 'dark' ? 0.16 : 0.10),
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                flexShrink: 0,
                              }}
                            >
                              <HorizonNodeIcon type={n.type} color={accent} size={16} />
                            </div>
                          ) : (
                            <div style={{ width: 28, height: 28, flexShrink: 0 }} />
                          )}
                          <span style={{ color: 'var(--text-heading)', fontSize: 13, fontWeight: 800, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {n.label}
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              );
            };

            const t = String(nodeType || '').trim().toUpperCase();
            const isPerson = t === 'PERSON';
            const isOrg = t === 'ORGANIZATION';
            const isGeo = t === 'GEO_REGION';

            const Section: React.FC<{ title: string; items: any[] }> = ({ title, items }) => {
              if (!items || items.length === 0) return null;
              const key = `${currentFid}:${title}`;
              const expanded = Boolean(govLocExpanded[key]);
              const shown = expanded ? items : items.slice(0, PREVIEW_N);
              return (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>{title}</div>
                  <div>{renderList(shown)}</div>
                  {!expanded && items.length > PREVIEW_N && (
                    <button
                      type="button"
                      onClick={() => setGovLocExpanded((prev) => ({ ...prev, [key]: true }))}
                      style={{
                        alignSelf: 'flex-start',
                        padding: '6px 10px',
                        borderRadius: 10,
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-tertiary)',
                        color: 'var(--text-heading)',
                        cursor: 'pointer',
                        fontSize: 12,
                        fontWeight: 850,
                      }}
                    >
                      Show more
                    </button>
                  )}
                </div>
              );
            };

            const SkeletonBar: React.FC<{ w: string }> = ({ w }) => (
              <div
                style={{
                  width: w,
                  height: 12,
                  borderRadius: 999,
                  background: theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)',
                }}
              />
            );

            const hasAny =
              (d?.managesIn?.length || 0) > 0 ||
              (d?.managesOut?.length || 0) > 0 ||
              (d?.boardIn?.length || 0) > 0 ||
              (d?.boardOut?.length || 0) > 0 ||
              (d?.assuredBy?.length || 0) > 0 ||
              (d?.assures?.length || 0) > 0 ||
              (d?.locatedOut?.length || 0) > 0 ||
              (d?.locatedIn?.length || 0) > 0;

            // UX: hide the entire panel when there's no content.
            // Still show while loading or if there's an error.
            const shouldShow = Boolean(showErr || directRelationsLoading || showSkeleton || hasAny);
            if (!shouldShow) return null;

            return (
              <SectionCard
                title="Governance, assurance & location"
                right={directRelationsLoading ? <Chip>Loading…</Chip> : undefined}
              >
                {showErr ? (
                  <div style={{ fontSize: 12, color: '#f87171', fontWeight: 650 }}>
                    {directRelationsError}
                  </div>
                ) : showSkeleton ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>Managed by</div>
                      <SkeletonBar w="72%" />
                      <SkeletonBar w="60%" />
                    </div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>Located in</div>
                      <SkeletonBar w="68%" />
                    </div>
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                    {/* Management */}
                    {!isGeo && (
                      <>
                        <Section title="Managed by" items={d?.managesIn || []} />
                        <Section title="Manages" items={d?.managesOut || []} />
                      </>
                    )}

                    {/* Governance: show only the direction that makes sense for the node type */}
                    {isOrg && <Section title="Board members" items={d?.boardIn || []} />}
                    {isPerson && <Section title="Board of" items={d?.boardOut || []} />}

                    {/* Assurance & Oversight (Third Line) */}
                    {((d?.assuredBy?.length || 0) > 0 || (d?.assures?.length || 0) > 0) && (
                      <>
                        <Section title="Assured by" items={d?.assuredBy || []} />
                        <Section title="Assures" items={d?.assures || []} />
                      </>
                    )}

                    {/* Location */}
                    {!isGeo && <Section title="Located in" items={d?.locatedOut || []} />}
                    {isGeo && <Section title="Located here" items={d?.locatedIn || []} />}
                  </div>
                )}
              </SectionCard>
            );
          })()}

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

          <Disclosure title="News & ontology facts" subtitle="Fetch news, create events and signals">
            <button
              type="button"
              onClick={handleLoadNewsForNode}
              disabled={newsLoading}
              style={{
                padding: '8px 10px',
                fontSize: 12,
                fontWeight: 800,
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: newsLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                color: 'var(--text-heading)',
                cursor: newsLoading ? 'not-allowed' : 'pointer',
                opacity: newsLoading ? 0.7 : 1,
                width: '100%',
              }}
            >
              {newsLoading ? 'Loading…' : 'Load news for this node'}
            </button>
            {newsError && <div style={{ fontSize: 12, color: '#f87171', marginTop: 10 }}>{newsError}</div>}
            <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 8 }}>
              Uses Gemini search grounding. Creates ontology facts (events), 12-axis signals, propagation.
            </div>
          </Disclosure>

          {(() => {
            const nodeState = (nodeData as any)?.state;
            const fragState = (fragment as any)?.state;
            const hasNodeState = nodeState && typeof nodeState === 'object' && Object.keys(nodeState).length > 0;
            const hasFragState = fragState && typeof fragState === 'object' && Object.keys(fragState).length > 0;
            return hasNodeState || hasFragState;
          })() && (
            <Disclosure title="Metrics" subtitle="KPIs + signals" defaultOpen={false}>
              {(() => {
                const colorFrom01 = (x: number) => {
                  const v = Math.max(0, Math.min(1, x));
                  if (v >= 0.7) return '#22C55E';
                  if (v >= 0.4) return '#F59E0B';
                  return '#EF4444';
                };

                // Merge node state (lens item) with fragment.state (authoritative MetricValue objects).
                // We keep BOTH:
                // - `mergedValues`: numeric values for scoring/visuals (signals)
                // - `mergedMetricObjects`: rich objects for KPI-style display (value + unit + confidence + asOf)
                const mergedValues: Record<string, number> = {};
                const mergedMetricObjects: Record<string, any> = {};

                const ns = (nodeData as any)?.state;
                if (ns && typeof ns === 'object') {
                  for (const [k, v] of Object.entries(ns)) {
                    const kk = String(k || '').trim();
                    const vv = typeof v === 'number' ? v : Number(v);
                    if (!kk || !Number.isFinite(vv)) continue;
                    mergedValues[kk] = vv;
                    if (!mergedMetricObjects[kk]) mergedMetricObjects[kk] = { value: vv };
                  }
                }
                if (fragment && (fragment as any)?.state && typeof (fragment as any).state === 'object') {
                  for (const [k, mv] of Object.entries((fragment as any).state)) {
                    const kk = String(k || '').trim();
                    if (!kk) continue;
                    if (mv && typeof mv === 'object' && 'value' in (mv as any)) {
                      const vv = Number((mv as any).value);
                      if (!Number.isFinite(vv)) continue;
                      // Prefer fragment as source of truth (more precise than lens summaries).
                      mergedValues[kk] = vv;
                      mergedMetricObjects[kk] = mv;
                    }
                  }
                }

                // Market nodes: show domain-specific KPIs prominently (TAM/CAGR/etc).
                // Heuristic: canonicalPath starts with /market (primary), with /segment as a fallback.
                const cp = String(nodeCanonicalPath || (fragment as any)?.canonicalPath || '').trim().toLowerCase();
                const isMarketNode = cp.startsWith('/market/') || cp === '/market' || cp.startsWith('/segment/') || cp === '/segment';
                const normalizeBaseKey = (k: string) => {
                  const raw = String(k || '').trim().toLowerCase();
                  const base = raw.split('/').pop() || raw;
                  return base.replace(/\s+/g, '_');
                };
                const isMarketMetricKey = (baseKey: string) => {
                  // Conservative allow-list-ish heuristic to avoid misclassifying generic metrics.
                  return (
                    baseKey === 'tam' ||
                    baseKey === 'tam_usd' ||
                    baseKey === 'sam' ||
                    baseKey === 'sam_usd' ||
                    baseKey === 'som' ||
                    baseKey === 'som_usd' ||
                    baseKey === 'cagr' ||
                    baseKey === 'cagr_percent' ||
                    baseKey === 'growth' ||
                    baseKey === 'growth_rate' ||
                    baseKey === 'growth_rate_percent' ||
                    baseKey === 'market_size' ||
                    baseKey === 'market_size_usd'
                  );
                };
                const marketLabelFor = (baseKey: string, fallback: string) => {
                  if (baseKey === 'tam' || baseKey === 'tam_usd') return 'TAM';
                  if (baseKey === 'sam' || baseKey === 'sam_usd') return 'SAM';
                  if (baseKey === 'som' || baseKey === 'som_usd') return 'SOM';
                  if (baseKey === 'cagr' || baseKey === 'cagr_percent') return 'CAGR';
                  if (baseKey.startsWith('growth')) return 'Growth';
                  if (baseKey.startsWith('market_size')) return 'Market size';
                  return fallback;
                };
                const formatKpiValue = (mv: any): string => {
                  const v0 = mv && typeof mv === 'object' && 'value' in mv ? Number((mv as any).value) : Number(mv);
                  if (!Number.isFinite(v0)) return '-';
                  const unitRaw = mv && typeof mv === 'object' ? String((mv as any).unit || (mv as any).uom || '').trim() : '';
                  const unit = unitRaw.toLowerCase();
                  // Currency (usd)
                  if (unit === 'usd' || unit === '$' || unit.includes('usd')) {
                    try {
                      return new Intl.NumberFormat('en-US', {
                        style: 'currency',
                        currency: 'USD',
                        notation: 'compact',
                        compactDisplay: 'short',
                        maximumFractionDigits: 1,
                      }).format(v0);
                    } catch {
                      // fallback
                      return `${v0} USD`;
                    }
                  }
                  // Percent
                  if (unit.includes('percent') || unit === '%' || unit === 'pct') {
                    const pct = Math.abs(v0) <= 1 ? v0 * 100 : v0;
                    const digits = pct >= 10 ? 0 : 1;
                    return `${pct.toFixed(digits)}%`;
                  }
                  // Generic compact numbers
                  if (Math.abs(v0) >= 1_000_000) {
                    try {
                      return new Intl.NumberFormat('en-US', { notation: 'compact', compactDisplay: 'short', maximumFractionDigits: 2 }).format(v0);
                    } catch {
                      // ignore
                    }
                  }
                  return unitRaw ? `${v0} ${unitRaw}` : String(v0);
                };
                const formatKpiMeta = (mv: any): string => {
                  if (!mv || typeof mv !== 'object') return '';
                  const parts: string[] = [];
                  const conf = (mv as any).conf;
                  if (typeof conf === 'number' && Number.isFinite(conf)) {
                    parts.push(`Confidence ${Math.round(Math.max(0, Math.min(1, conf)) * 100)}%`);
                  }
                  const asOf = String((mv as any).observedAt || '').trim();
                  if (asOf) {
                    try {
                      const d = new Date(asOf);
                      if (!Number.isNaN(d.getTime())) parts.push(`As of ${d.toLocaleDateString()}`);
                      else parts.push(`As of ${asOf}`);
                    } catch {
                      parts.push(`As of ${asOf}`);
                    }
                  }
                  return parts.join(' · ');
                };
                const MarketMetricIcon: React.FC<{ baseKey: string }> = ({ baseKey }) => {
                  const k = String(baseKey || '').trim().toLowerCase();
                  const color = theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.92)';
                  const stroke = color;
                  const common = {
                    width: 16,
                    height: 16,
                    viewBox: '0 0 24 24',
                    fill: 'none' as const,
                    stroke,
                    strokeWidth: 2,
                    strokeLinecap: 'round' as const,
                    strokeLinejoin: 'round' as const,
                  };
                  if (k.startsWith('tam') || k.startsWith('sam') || k.startsWith('som') || k.includes('market_size')) {
                    // Coins / market size
                    return (
                      <svg {...common}>
                        <ellipse cx="12" cy="7" rx="6.5" ry="3" />
                        <path d="M5.5 7v4c0 1.7 2.9 3 6.5 3s6.5-1.3 6.5-3V7" />
                        <path d="M5.5 11v4c0 1.7 2.9 3 6.5 3s6.5-1.3 6.5-3v-4" />
                      </svg>
                    );
                  }
                  if (k.startsWith('cagr') || k.startsWith('growth')) {
                    // Trend up
                    return (
                      <svg {...common}>
                        <path d="M3 17l6-6 4 4 8-8" />
                        <path d="M14 7h7v7" />
                      </svg>
                    );
                  }
                  // Default: small dot (neutral)
                  return (
                    <svg {...common}>
                      <circle cx="12" cy="12" r="2.5" />
                    </svg>
                  );
                };

                const entries = Object.entries(mergedValues || {})
                  .map(([k, v]) => [String(k || ''), typeof v === 'number' ? v : Number(v)] as const)
                  .filter(([k, v]) => k && Number.isFinite(v))
                  .map(([k, v]) => {
                    const key = normalizeBaseKey(k);
                    const meta = METRIC_META[key];
                    const label = meta?.label || key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
                    const scale: 'unit' | 'bipolar' = meta?.scale || 'unit';

                    // Values may be either 0..1 or -1..+1 (bipolar). Do not clamp negatives to 0.
                    const vRaw = Number(v);
                    const isMarket = isMarketNode && isMarketMetricKey(key);
                    const isSignal = Boolean(meta) || Math.abs(vRaw) <= 1.0001; // heuristic: raw metrics (e.g. reserves) are typically >> 1
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
                      kind: isMarket ? ('market' as const) : (isSignal ? ('signal' as const) : ('raw' as const)),
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

                const marketEntries = entries.filter((e) => e.kind === 'market');
                const signalEntries = entries.filter((e) => e.kind === 'signal');
                const rawEntries = entries.filter((e) => e.kind === 'raw');

                // Sort market KPIs in a product-first order.
                const marketOrder = ['tam_usd', 'tam', 'sam_usd', 'sam', 'som_usd', 'som', 'cagr_percent', 'cagr', 'growth_rate_percent', 'growth_rate', 'growth', 'market_size_usd', 'market_size'];
                marketEntries.sort((a, b) => {
                  const ia = marketOrder.indexOf(a.key);
                  const ib = marketOrder.indexOf(b.key);
                  if (ia !== -1 || ib !== -1) return (ia === -1 ? 999 : ia) - (ib === -1 ? 999 : ib);
                  return a.label.localeCompare(b.label);
                });

                return (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {/* Market KPIs: own subsection, open by default. */}
                    {marketEntries.length > 0 && (
                      <Disclosure
                        key={`market-kpis:${String(nodeId || '')}:${String(nodeCanonicalPath || '')}`}
                        title="Market KPIs"
                        subtitle={`${Math.min(6, marketEntries.length)} shown`}
                        defaultOpen
                      >
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
                          {marketEntries.slice(0, 6).map((m) => {
                            const mv = mergedMetricObjects[m.rawKey];
                            const title = String(m.rawKey || '').trim();
                            const label = marketLabelFor(m.key, m.label);
                            const val = formatKpiValue(mv);
                            const meta = formatKpiMeta(mv);
                            return (
                              <div
                                key={`market-kpi:${m.rawKey}`}
                                title={title}
                                style={{
                                  borderRadius: 14,
                                  border: '1px solid var(--border-subtle)',
                                  background: theme === 'dark' ? 'rgba(0,0,0,0.14)' : 'rgba(255,255,255,0.55)',
                                  padding: 10,
                                  minWidth: 0,
                                }}
                              >
                                <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                                  <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-secondary)', letterSpacing: 0.2 }}>
                                    {label}
                                  </div>
                                  <div style={{ width: 16, height: 16, flexShrink: 0, opacity: 0.95 }}>
                                    <MarketMetricIcon baseKey={m.key} />
                                  </div>
                                </div>
                                <div style={{ marginTop: 6, fontSize: 18, fontWeight: 950, color: 'var(--text-heading)', lineHeight: 1.1 }}>
                                  {val}
                                </div>
                                {meta && (
                                  <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-secondary)', fontWeight: 650 }}>
                                    {meta}
                                  </div>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </Disclosure>
                    )}

                    {/* Signals: inline (no subgroup). */}
                    {signalEntries.length > 0 && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                        {signalEntries.slice(0, 18).map((m) => (
                          <div key={m.rawKey} title={m.rawKey} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        {/* Segment icon (synced with Risk Shield). */}
                        {(() => {
                          const segId = segmentForMetricBaseKey(m.key);
                          if (!segId) {
                            return <div style={{ width: 18, height: 18, flexShrink: 0 }} />;
                          }
                          const spec = RISK_SEGMENTS.find((s) => s.id === segId);
                          const color = spec?.color || 'var(--text-tertiary-weak)';
                          const title = `${spec?.label || segId}: ${m.label}`;
                          return (
                            <div title={title} style={{ width: 18, height: 18, flexShrink: 0 }}>
                                    <RiskSegmentIcon id={segId} size={18} color={color} opacity={1} />
                            </div>
                          );
                        })()}
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
                        {signalEntries.length > 18 && (
                          <div style={{ fontSize: 11, color: 'var(--text-secondary)', fontWeight: 650 }}>
                            Showing 18 of {signalEntries.length}. (More can be added later if needed.)
                          </div>
                        )}
                      </div>
                    )}

                    {/* Raw metrics: keep accessible, but collapsed by default (technical). */}
                    {rawEntries.length > 0 && (
                      <Disclosure
                        key={`raw:${String(nodeId || '')}:${String(nodeCanonicalPath || '')}`}
                        title="Raw metrics"
                        subtitle={`${rawEntries.length} ${rawEntries.length === 1 ? 'field' : 'fields'}`}
                      >
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                          {rawEntries.slice(0, 24).map((m) => {
                            const mv = (fragment as any)?.state?.[m.rawKey];
                            const valueText = mv && typeof mv === 'object' ? formatMetricValue(mv) : String((nodeData as any)?.state?.[m.rawKey]);
                            return <KV key={m.rawKey} k={m.label} v={<span style={mono}>{valueText}</span>} />;
                          })}
                          {rawEntries.length > 24 && (
                            <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-secondary)', fontWeight: 650 }}>
                              Showing 24 of {rawEntries.length}.
                        </div>
                          )}
                      </div>
                      </Disclosure>
                    )}
                  </div>
                );
              })()}
            </Disclosure>
          )}

          {(nodeData?.fragmentId || nodeId) && (
            <Disclosure title="Technical" subtitle="IDs & tools">
              {(() => {
                const cp = String(nodeCanonicalPath || '').trim();
                const canPin = Boolean(lensId && cp);
                const pinned = lensScopePinned === null ? Boolean((nodeData as any)?.pinnedToScope) : lensScopePinned;
                if (!canPin) return null;
                return (
                  <div style={{ marginBottom: 12 }}>
                    <button
                      type="button"
                      onClick={async () => {
                        // Prefer node-provided callback (keeps graph + scope preview in sync).
                        const cb = (nodeData as any)?.onTogglePin as
                          | ((args: { fragmentId?: string; canonicalPath?: string; nextPinned: boolean }) => void)
                          | undefined;
                        if (cb) {
                          cb({ fragmentId: nodeData?.fragmentId, canonicalPath: cp, nextPinned: !pinned });
                          setLensScopePinned(!pinned);
                          return;
                        }

                        // Fallback: update lens scope directly.
                        if (!lensId) return;
                        setLensScopePinned(!pinned);
                        try {
                          const lens = await api.getHorizonLens(lensId);
                          const current: string[] = Array.isArray(lens?.scope)
                            ? lens.scope.map((s: any) => String(s || '').trim()).filter(Boolean)
                            : [];
                          const has = current.includes(cp);
                          const next = !pinned ? (has ? current : [...current, cp]) : current.filter((x) => x !== cp);
                          await api.updateHorizonLens(lensId, {
                            tenantId: lens.tenantId,
                            projectId: lens.projectId,
                            name: lens.name,
                            scope: next,
                            viewConfig: lens.viewConfig,
                            alertConfig: lens.alertConfig,
                            status: lens.status,
                            scopeFragments: lens.scopeFragments,
                            resolvedQueries: lens.resolvedQueries,
                            lastResolved: lens.lastResolved,
                          });
                        } catch {
                          // Re-sync (best-effort)
                          try {
                            const lens = await api.getHorizonLens(lensId);
                            const scope = Array.isArray(lens?.scope)
                              ? lens.scope.map((s: any) => String(s || '').trim()).filter(Boolean)
                              : [];
                            setLensScopePinned(scope.includes(cp));
                          } catch {
                            // ignore
                          }
                        }
                      }}
                      title={pinned ? 'Pinned to scope (click to unpin)' : 'Pin to scope'}
                      aria-label={pinned ? 'Unpin from scope' : 'Pin to scope'}
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        gap: 6,
                        padding: '8px 10px',
                        borderRadius: 12,
                        border: `1px solid ${pinned ? hexToRgba(nodeAccent, theme === 'dark' ? 0.42 : 0.28) : 'var(--border-subtle)'}`,
                        background: pinned ? hexToRgba(nodeAccent, theme === 'dark' ? 0.18 : 0.12) : 'var(--bg-tertiary)',
                        color: 'var(--text-heading)',
                        fontSize: 12,
                        fontWeight: 850,
                        cursor: 'pointer',
                        width: '100%',
                      }}
                    >
                      {pinned ? 'Unpin from scope' : 'Pin to scope'}
                    </button>
                  </div>
                );
              })()}

              {canUseLoadedFragment && fragment && (
                <div style={{ marginBottom: 14 }}>
                  <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)', marginBottom: 10 }}>
                    Fragment
                  </div>
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
                  {fragment.type && <KV k="Type" v={<span>{String(fragment.type)}</span>} />}
                  {fragment.lifecycleStage && <KV k="Lifecycle" v={<span style={mono}>{String(fragment.lifecycleStage)}</span>} />}
                  {fragment.expectedStartDate && <KV k="Expected start" v={<span style={mono}>{String(fragment.expectedStartDate)}</span>} />}
                  {fragment.actualStartDate && <KV k="Actual start" v={<span style={mono}>{String(fragment.actualStartDate)}</span>} />}
                  {fragment.expectedEndDate && <KV k="Expected end" v={<span style={mono}>{String(fragment.expectedEndDate)}</span>} />}
                  {fragment.actualEndDate && <KV k="Actual end" v={<span style={mono}>{String(fragment.actualEndDate)}</span>} />}
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

                  {/* DOMAIN contract (best-effort): show what we actually have on the fragment */}
                  {domainContract && (
                    <div style={{ marginTop: 12 }}>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)', marginBottom: 10 }}>
                        Domain contract
                      </div>
                      {domainContract.contractType && (
                        <KV
                          k="Contract type"
                          v={
                            <span style={mono} title={domainContract.source.contractType}>
                              {domainContract.contractType}
                            </span>
                          }
                          copyText={domainContract.contractType}
                        />
                      )}
                      {domainContract.title && <KV k="Title (labels.en)" v={<span>{domainContract.title}</span>} />}
                      {domainContract.description && (
                        <KV
                          k="Description"
                          v={<span style={{ whiteSpace: 'pre-wrap' }}>{domainContract.description}</span>}
                        />
                      )}
                      {domainContract.facets && Object.keys(domainContract.facets).length > 0 && (
                        <KV
                          k="Facets"
                          v={
                            <span style={mono} title={domainContract.source.facets}>
                              {JSON.stringify(domainContract.facets)}
                            </span>
                          }
                          copyText={JSON.stringify(domainContract.facets)}
                        />
                      )}
                      {!domainContract.title && !domainContract.description && (
                        <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-secondary)', fontWeight: 650 }}>
                          Note: title/description are not stored on this fragment yet. UI will use a deterministic fallback from canonicalPath.
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* On-demand resolver (no writes) */}
              {nodeId && nodeData && (
                <div style={{ marginBottom: 12 }}>
                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 8 }}>
                    On-demand: resolve dependency via product
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginBottom: 10, lineHeight: 1.35 }}>
                    Runs a grounded LLM request and shows JSON request/response. Does not write anything to the graph.
                  </div>
                  {(() => {
                    const selectedType = String((nodeData as any)?.type || (fragment as any)?.type || '').trim().toUpperCase();
                    const canOpen = !Boolean(edgeId) && (selectedType === 'ORGANIZATION' || selectedType === 'FACILITY');

                    const openResolver = () => {
                      try {
                        const incidentEdgesRaw = (nodeData as any)?.visibleIncidentEdges;
                        const neighborNodesRaw = (nodeData as any)?.visibleNeighborNodes;
                        const incidentEdgesAll = Array.isArray(incidentEdgesRaw) ? incidentEdgesRaw : [];
                        const neighborNodesAll = Array.isArray(neighborNodesRaw) ? neighborNodesRaw : [];

                        const selectedNodeId = String(nodeId);
                        const selectedPath = String((nodeData as any)?.canonicalPath || (fragment as any)?.canonicalPath || '');

                        const orgLike = (t: unknown) => {
                          const u = String(t || '').trim().toUpperCase();
                          if (['ORGANIZATION', 'FACILITY', 'ORG', 'SITE'].includes(u)) return true;
                          const cls = tryParseFragmentClass(String(t || ''));
                          return cls === FRAGMENT_CLASS.Org || cls === FRAGMENT_CLASS.Site;
                        };

                        // Supply-like edges (AH `supplies` covers legacy SUPPLIES_TO / DEPENDS_ON)
                        const supplyChainNorm = new Set([
                          normalizeWireRelation('SUPPLIES_TO'),
                          normalizeWireRelation('DEPENDS_ON'),
                        ]);
                        const incidentEdges = incidentEdgesAll.filter((e: any) => {
                          const et = normalizeWireRelation(e?.edgeType);
                          if (!supplyChainNorm.has(et)) return false;
                          return orgLike(e?.sourceType) && orgLike(e?.targetType);
                        });

                        // Build node lookup including selected node
                        const nodeById = new Map<string, any>();
                        nodeById.set(String(nodeId), {
                          nodeId: String(nodeId),
                          fragmentId: (nodeData as any)?.fragmentId || nodeId,
                          canonicalPath: selectedPath || null,
                          type: (nodeData as any)?.type || (fragment as any)?.type || null,
                          label: (nodeData as any)?.label || nodeDisplayName || null,
                        });
                        for (const n of neighborNodesAll) {
                          nodeById.set(String(n?.nodeId || ''), n);
                        }

                        // Get neighbor org nodes (other side of incidentEdges)
                        const neighborNodeIds = new Set<string>();
                        for (const e of incidentEdges) {
                          const srcId = String(e?.sourceNodeId || '');
                          const tgtId = String(e?.targetNodeId || '');
                          if (srcId !== selectedNodeId) neighborNodeIds.add(srcId);
                          if (tgtId !== selectedNodeId) neighborNodeIds.add(tgtId);
                        }
                        const neighborNodes = neighborNodesAll.filter((n: any) => neighborNodeIds.has(String(n?.nodeId || '')));

                        // Build edges with labels
                        const edgesOut = incidentEdges.map((e: any) => {
                          const src = nodeById.get(String(e?.sourceNodeId));
                          const tgt = nodeById.get(String(e?.targetNodeId));
                          return {
                            id: String(e?.id || ''),
                            edgeType: String(e?.edgeType || ''),
                            sourceNodeId: String(e?.sourceNodeId || ''),
                            targetNodeId: String(e?.targetNodeId || ''),
                            sourceFragmentId: src?.fragmentId || e?.sourceFragmentId || null,
                            targetFragmentId: tgt?.fragmentId || e?.targetFragmentId || null,
                            sourceCanonicalPath: src?.canonicalPath || null,
                            targetCanonicalPath: tgt?.canonicalPath || null,
                            sourceType: src?.type || null,
                            targetType: tgt?.type || null,
                            sourceLabel: src?.label || null,
                            targetLabel: tgt?.label || null,
                            products: Array.isArray(e?.products) ? e.products : [],
                            brandNames: Array.isArray(e?.brandNames) ? e.brandNames : [],
                            criticality: e?.criticality,
                          };
                        });

                        // Build neighborContext: what each neighbor produces/supplies
                        // For now, we extract from PRODUCES edges in neighborNodesAll
                        const neighborContext: Record<string, { produces?: string[]; suppliesTo?: string[]; dependsOn?: string[] }> = {};
                        for (const n of neighborNodes) {
                          const nPath = String(n?.canonicalPath || '');
                          if (!nPath) continue;
                          neighborContext[nPath] = { produces: [], suppliesTo: [], dependsOn: [] };
                        }
                        // Scan all edges to find PRODUCES edges from neighbors
                        for (const e of incidentEdgesAll) {
                          const etNorm = normalizeWireRelation(e?.edgeType);
                          const etLegacy = String(e?.edgeType || '')
                            .trim()
                            .toUpperCase()
                            .replace(/-/g, '_');
                          const srcPath = String(e?.sourceCanonicalPath || '');
                          const tgtPath = String(e?.targetCanonicalPath || '');
                          if (etNorm === RELATION.Produces && neighborContext[srcPath] && tgtPath.startsWith('/product/')) {
                            neighborContext[srcPath].produces?.push(tgtPath);
                          }
                          if (etNorm === RELATION.Supplies && neighborContext[srcPath]) {
                            if (etLegacy === 'DEPENDS_ON') {
                              neighborContext[srcPath].dependsOn?.push(tgtPath);
                            } else {
                              neighborContext[srcPath].suppliesTo?.push(tgtPath);
                            }
                          }
                        }

                        // Collect known products from graph (PRODUCES targets, product-type neighbors)
                        const knownProductPaths = new Set<string>();
                        for (const e of incidentEdgesAll) {
                          const tgtPath = String(e?.targetCanonicalPath || '');
                          const srcPath = String(e?.sourceCanonicalPath || '');
                          if (tgtPath.startsWith('/product/')) knownProductPaths.add(tgtPath);
                          if (srcPath.startsWith('/product/')) knownProductPaths.add(srcPath);
                        }
                        for (const n of neighborNodesAll) {
                          const nPath = String(n?.canonicalPath || '');
                          if (nPath.startsWith('/product/')) knownProductPaths.add(nPath);
                        }
                        const knownProducts = Array.from(knownProductPaths).map((p) => {
                          const n = neighborNodesAll.find((nn: any) => String(nn?.canonicalPath || '') === p);
                          return { canonicalPath: p, label: n?.label || undefined };
                        });

                        const ctx: HorizonOnDemandContext = {
                          v: 2,
                          scenario: 'org_supply_chain',
                          selected: {
                            nodeId: String(nodeId),
                            fragmentId: String((nodeData as any)?.fragmentId || nodeId),
                            canonicalPath: selectedPath || null,
                            type: String((nodeData as any)?.type || (fragment as any)?.type || '') || null,
                            label: String((nodeData as any)?.label || nodeDisplayName || '') || null,
                          },
                          incidentEdges: edgesOut,
                          neighborNodes: neighborNodes.map((n: any) => ({
                            nodeId: String(n?.nodeId || ''),
                            fragmentId: n?.fragmentId || null,
                            canonicalPath: n?.canonicalPath || null,
                            type: n?.type || null,
                            label: n?.label || null,
                          })),
                          neighborContext,
                          knownProducts,
                        };
                        setOnDemandCtx(ctx);
                        setOnDemandOpen(true);
                      } catch (e) {
                        console.warn('[HorizonEntityInspector] failed to open on-demand resolver', e);
                      }
                    };

                    if (!canOpen) {
                      return (
                        <div style={{ fontSize: 11, color: 'var(--text-tertiary-weak)' }}>
                          Select an organization to resolve supply-chain products.
                        </div>
                      );
                    }

                    return (
                      <button
                        type="button"
                        onClick={openResolver}
                        style={{
                          padding: '8px 10px',
                          fontSize: 12,
                          fontWeight: 800,
                          borderRadius: 12,
                          border: '1px solid var(--border-subtle)',
                          background: 'var(--bg-tertiary)',
                          color: 'var(--text-heading)',
                          cursor: 'pointer',
                          width: '100%',
                        }}
                      >
                        Resolve supply-chain products
                      </button>
                    );
                  })()}
                </div>
              )}

              {/* Product IO */}
      {nodeData?.fragmentId && (
                <div style={{ marginBottom: 12 }}>
                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 8 }}>
            Hypothesis: Product IO (v1)
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginBottom: 10, lineHeight: 1.35 }}>
              Extract products this object <b>produces</b> and <b>consumes</b>, then materialize them into the global Horizon graph.
                    <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-tertiary-weak)' }}>
                v1: PRODUCT-only. Fragment is primary.
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

              <div style={{ marginTop: 14 }}>
                <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)', marginBottom: 10 }}>
                  IDs
                </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 10 }}>
              <button
                type="button"
                onClick={async () => {
                  if (edgeId) return; // Node-only
                  const fid = String(fragmentIdForNode || '').trim();
                  setTechCopyState('copying');
                  try {
                    // Best-effort: include latest fragment.
                    let frag: any = fragment;
                    if (fid) {
                      try {
                        frag = await api.getHorizonFragment(fid);
                      } catch {
                        // keep existing
                      }
                    }

                    const payload = {
                      kind: 'horizon_fragment_tech_dump',
                      capturedAt: new Date().toISOString(),
                      lensId: lensId || null,
                      node: {
                        nodeId: nodeId || null,
                        fragmentId: fid || null,
                        canonicalPath: String(nodeCanonicalPath || frag?.canonicalPath || '').trim() || null,
                        type: nodeType || frag?.type || null,
                        status: nodeStatus || frag?.status || null,
                      },
                      fragment: frag || null,
                      nodeData: nodeData || null,
                    };

                    await copyToClipboard(safeJSONStringify(payload));
                    setTechCopyState('copied');
                    if (techCopyTimeoutRef.current !== null) window.clearTimeout(techCopyTimeoutRef.current);
                    techCopyTimeoutRef.current = window.setTimeout(() => {
                      setTechCopyState('idle');
                      techCopyTimeoutRef.current = null;
                    }, 1200);
                  } catch (e) {
                    console.warn('[HorizonEntityInspector] tech copy failed', e);
                    setTechCopyState('idle');
                  }
                }}
                disabled={techCopyState === 'copying' || Boolean(edgeId)}
                title="Copy all technical data (fragment) as JSON"
                style={{
                  padding: '6px 10px',
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--white-10)',
                  color: 'var(--text-heading)',
                  cursor: techCopyState === 'copying' || Boolean(edgeId) ? 'not-allowed' : 'pointer',
                  fontSize: 12,
                  fontWeight: 850,
                  opacity: techCopyState === 'copying' || Boolean(edgeId) ? 0.55 : 1,
                }}
              >
                {techCopyState === 'copying' ? 'Copying…' : techCopyState === 'copied' ? 'Copied' : 'Copy JSON'}
              </button>
            </div>
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
              </div>
            </Disclosure>
          )}
        </>
      )}
    </div>
  );
};
