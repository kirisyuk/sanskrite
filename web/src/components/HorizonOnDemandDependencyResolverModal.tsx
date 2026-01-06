import React from 'react';
import { api } from '../lib/api';
import { ThemeContext } from './ThemeProvider';

type Scenario = 'org_supply_chain';

// What a neighbor org produces/supplies (for context)
type NeighborOrgContext = {
  produces?: string[]; // product canonical paths
  suppliesTo?: string[]; // org canonical paths
  dependsOn?: string[]; // org canonical paths
};

export type HorizonOnDemandContext = {
  v: 2;
  scenario: Scenario;
  selected: {
    nodeId: string;
    fragmentId?: string | null;
    canonicalPath?: string | null;
    type?: string | null;
    label?: string | null;
  };
  // Edges incident to selected node (SUPPLIES_TO, DEPENDS_ON between orgs)
  incidentEdges: Array<{
    id: string;
    edgeType: string; // SUPPLIES_TO | DEPENDS_ON
    sourceNodeId: string;
    targetNodeId: string;
    sourceFragmentId?: string | null;
    targetFragmentId?: string | null;
    sourceCanonicalPath?: string | null;
    targetCanonicalPath?: string | null;
    sourceType?: string | null;
    targetType?: string | null;
    sourceLabel?: string | null;
    targetLabel?: string | null;
    // Existing products[] on edge (may be empty — that's what we want to fill)
    products?: string[];
    brandNames?: string[];
    criticality?: number;
  }>;
  // Neighbor orgs (the other side of incidentEdges)
  neighborNodes: Array<{
    nodeId: string;
    fragmentId?: string | null;
    canonicalPath?: string | null;
    type?: string | null;
    label?: string | null;
  }>;
  // НОВОЕ: контекст окрестности — что производят/поставляют соседи
  neighborContext?: Record<string, NeighborOrgContext>;
  // НОВОЕ: известные продукты в графе (чтобы LLM переиспользовал, а не выдумывал)
  knownProducts?: Array<{
    canonicalPath: string;
    label?: string;
  }>;
};

function safeJSONStringify(v: any) {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

type IdAliasMap = {
  forward: Record<string, string>; // original -> alias
  reverse: Record<string, string>; // alias -> original
};

function aliasContextIds(ctx: HorizonOnDemandContext): { aliased: HorizonOnDemandContext; idMap: IdAliasMap } {
  const forward = new Map<string, string>();
  const reverse = new Map<string, string>();
  let next = 1;

  const alloc = (orig: any) => {
    if (typeof orig !== 'string') return orig;
    if (!orig) return orig;
    const existing = forward.get(orig);
    if (existing) return existing;
    const alias = String(next++);
    forward.set(orig, alias);
    reverse.set(alias, orig);
    return alias;
  };

  const aliased: HorizonOnDemandContext = {
    ...ctx,
    selected: {
      ...ctx.selected,
      nodeId: alloc(ctx.selected.nodeId),
      fragmentId: ctx.selected.fragmentId ? alloc(ctx.selected.fragmentId) : ctx.selected.fragmentId,
    },
    incidentEdges: (ctx.incidentEdges || []).map((e) => ({
      ...e,
      id: alloc(e.id),
      sourceNodeId: alloc(e.sourceNodeId),
      targetNodeId: alloc(e.targetNodeId),
      sourceFragmentId: e.sourceFragmentId ? alloc(e.sourceFragmentId) : e.sourceFragmentId,
      targetFragmentId: e.targetFragmentId ? alloc(e.targetFragmentId) : e.targetFragmentId,
    })),
    neighborNodes: (ctx.neighborNodes || []).map((n) => ({
      ...n,
      nodeId: alloc(n.nodeId),
      fragmentId: n.fragmentId ? alloc(n.fragmentId) : n.fragmentId,
    })),
  };

  return {
    aliased,
    idMap: {
      forward: Object.fromEntries(forward.entries()),
      reverse: Object.fromEntries(reverse.entries()),
    },
  };
}

function formatErrorForDisplay(e: any): string {
  try {
    // Axios-style error
    const status = e?.response?.status;
    const data = e?.response?.data;
    const msgFromData = data?.error || data?.message;

    if (status === 502) {
      // Bad Gateway: backend/proxy couldn't reach upstream (often LLM/grounding provider) or timed out
      return `HTTP 502 (Bad Gateway): upstream service failed or timed out. Retry in a moment. Details: ${safeJSONStringify(msgFromData || data || e?.message || e)}`;
    }

    if (typeof msgFromData === 'string') return msgFromData;
    if (msgFromData && typeof msgFromData === 'object') return safeJSONStringify(msgFromData);

    if (typeof data === 'string') return data;
    if (data && typeof data === 'object') return safeJSONStringify(data);

    if (typeof e?.message === 'string') return e.message;
    return safeJSONStringify(e);
  } catch {
    return 'LLM call failed';
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // best-effort fallback
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    try {
      document.execCommand('copy');
    } catch {}
    document.body.removeChild(ta);
  }
}

function buildDefaultPrompt(ctx: HorizonOnDemandContext) {
  const selectedLabel = ctx.selected.label || ctx.selected.canonicalPath || 'the organization';
  const selectedPath = ctx.selected.canonicalPath || '';

  // Build human-readable edge list
  const edgeList = (ctx.incidentEdges || [])
    .map((e, i) => {
      const isOutgoing = e.sourceCanonicalPath === selectedPath;
      const otherPath = isOutgoing ? e.targetCanonicalPath : e.sourceCanonicalPath;
      const otherLabel = isOutgoing ? e.targetLabel : e.sourceLabel;
      const direction = isOutgoing ? '→' : '←';
      const existingProducts = (e.products || []).length > 0 ? ` [products: ${e.products?.join(', ')}]` : ' [products: ???]';
      return `  ${i + 1}. ${e.edgeType} ${direction} ${otherLabel || otherPath}${existingProducts}`;
    })
    .join('\n');

  // Build known products list
  const knownProductsList = (ctx.knownProducts || [])
    .map((p) => `  - ${p.canonicalPath}${p.label ? ` (${p.label})` : ''}`)
    .join('\n');

  // Build neighbor context summary
  const neighborContextSummary = Object.entries(ctx.neighborContext || {})
    .map(([path, nc]) => {
      const parts: string[] = [];
      if (nc.produces?.length) parts.push(`produces: ${nc.produces.join(', ')}`);
      if (nc.suppliesTo?.length) parts.push(`supplies to: ${nc.suppliesTo.join(', ')}`);
      if (nc.dependsOn?.length) parts.push(`depends on: ${nc.dependsOn.join(', ')}`);
      return `  ${path}: ${parts.join('; ') || '(no data)'}`;
    })
    .join('\n');

  const contextJSON = safeJSONStringify(ctx);

  return (
    `You are a domain expert assistant for supply-chain and industrial knowledge.\n` +
    `Your task is to use YOUR OWN KNOWLEDGE to ENRICH existing supply-chain relationships with specific products.\n\n` +
    `Output MUST be valid JSON only (no markdown, no commentary outside JSON).\n\n` +
    `TASK (org_supply_chain):\n` +
    `We have an ORGANIZATION in the center: "${selectedLabel}" (${selectedPath}).\n` +
    `We have supply-chain EDGES connecting it to other organizations.\n` +
    `For EACH edge, your job is to determine: WHAT PRODUCTS are being supplied?\n\n` +
    `EDGES TO ENRICH:\n${edgeList}\n\n` +
    `KEY PRINCIPLE:\n` +
    `- SUPPLIES_TO(A → B) means A supplies products to B.\n` +
    `- DEPENDS_ON(A → B) means A depends on B, which typically means B supplies products to A (reverse SUPPLIES_TO).\n` +
    `- Your job: identify the products[] that should be attached to each edge.\n\n` +
    `CONTEXT - WHAT NEIGHBORS PRODUCE/SUPPLY:\n${neighborContextSummary || '(none)'}\n\n` +
    `KNOWN PRODUCTS IN GRAPH (prefer these over inventing new ones):\n${knownProductsList || '(none)'}\n\n` +
    `RULES:\n` +
    `1. USE YOUR OWN KNOWLEDGE about what these companies actually produce and supply to each other.\n` +
    `2. PREFER existing products from knownProducts. Only propose new /product/... paths if truly needed.\n` +
    `3. New /product/... paths MUST be short, generic class names (e.g. "/product/euv-optics", not "/product/carl-zeiss-euv-optical-systems-v3").\n` +
    `4. Do NOT invent /org/, /fac/, /person/, /loc/ paths. Use ONLY what appears in context.\n` +
    `5. Brand/model names are OPTIONAL attributes of the edge, not separate products.\n` +
    `6. If you don't know what products are supplied, set verdict="unknown".\n\n` +
    `CONFIDENCE RULES:\n` +
    `- NEVER output confidence=1.0.\n` +
    `- No sources cited: confidence <= 0.70.\n` +
    `- 1 source: confidence <= 0.80.\n` +
    `- 2 sources: confidence <= 0.90.\n` +
    `- verdict="unknown": confidence 0.50-0.65.\n` +
    `- sources.length MUST be <= 2.\n\n` +
    `OUTPUT SCHEMA:\n` +
    `{\n` +
    `  "v": 2,\n` +
    `  "scenario": "org_supply_chain",\n` +
    `  "items": [\n` +
    `    {\n` +
    `      "edgeIndex": 0,\n` +
    `      "edgeId": "original edge id from incidentEdges",\n` +
    `      "verdict": "resolved" | "unknown",\n` +
    `      "supplierCanonicalPath": "/org/...",\n` +
    `      "consumerCanonicalPath": "/org/...",\n` +
    `      "products": [\n` +
    `        {\n` +
    `          "canonicalPath": "/product/...",\n` +
    `          "brandNames": ["string"],\n` +
    `          "modelNames": ["string"]\n` +
    `        }\n` +
    `      ],\n` +
    `      "evidence": "1-2 sentence explanation",\n` +
    `      "sources": [{"title":"", "url":""}],\n` +
    `      "confidence": 0.0,\n` +
    `      "notes": "optional"\n` +
    `    }\n` +
    `  ]\n` +
    `}\n\n` +
    `SCHEMA RULES:\n` +
    `- items.length MUST equal incidentEdges.length. One item per edge, same order.\n` +
    `- edgeIndex = index in incidentEdges array (0-based).\n` +
    `- edgeId = copy of incidentEdges[i].id.\n` +
    `- For SUPPLIES_TO(A → B): supplierCanonicalPath=A, consumerCanonicalPath=B.\n` +
    `- For DEPENDS_ON(A → B): this means B supplies to A, so supplierCanonicalPath=B, consumerCanonicalPath=A.\n` +
    `- products[] = array of products being supplied. Each has canonicalPath (required) and optional brandNames/modelNames.\n` +
    `- If verdict="unknown", products[] should be empty.\n` +
    `- If verdict="resolved", products[] MUST have at least one entry.\n\n` +
    `GRAPH CONTEXT (full JSON for reference):\n` +
    `${contextJSON}\n`
  );
}

function hostFromURL(url: string): string | null {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return null;
  }
}

function isValidCanonicalPath(p: string): boolean {
  if (!p || typeof p !== 'string') return false;
  if (!p.startsWith('/')) return false;
  if (p !== p.toLowerCase()) return false;
  return /^\/[a-z0-9][a-z0-9/_-]*$/.test(p);
}

function buildResultWarnings(resultJSON: any, ctx: HorizonOnDemandContext): string[] {
  const warnings: string[] = [];
  const items = Array.isArray(resultJSON?.items) ? resultJSON.items : [];

  const isWeakHost = (h: string) =>
    h.includes('wikipedia.org') || h.includes('medium.com') || h.includes('substack.com') || h.includes('blog') || h.includes('insight');

  // Collect known canonical paths from context
  const knownOrgPaths = new Set<string>();
  const knownProductPaths = new Set<string>();
  (ctx?.neighborNodes || []).forEach((n) => {
    const p = String((n as any)?.canonicalPath || '');
    if (p.startsWith('/org/') || p.startsWith('/fac/')) knownOrgPaths.add(p);
  });
  (ctx?.incidentEdges || []).forEach((e) => {
    const sp = String((e as any)?.sourceCanonicalPath || '');
    const tp = String((e as any)?.targetCanonicalPath || '');
    if (sp.startsWith('/org/') || sp.startsWith('/fac/')) knownOrgPaths.add(sp);
    if (tp.startsWith('/org/') || tp.startsWith('/fac/')) knownOrgPaths.add(tp);
  });
  const selectedCP = String(ctx?.selected?.canonicalPath || '');
  if (selectedCP) knownOrgPaths.add(selectedCP);
  (ctx?.knownProducts || []).forEach((p) => {
    if (p.canonicalPath) knownProductPaths.add(p.canonicalPath);
  });

  // items.length must equal incidentEdges.length
  if (Array.isArray(ctx?.incidentEdges) && items.length !== ctx.incidentEdges.length) {
    warnings.push(`items.length (${items.length}) must equal incidentEdges.length (${ctx.incidentEdges.length})`);
  }

  items.forEach((it: any, idx: number) => {
    const verdict = String(it?.verdict || '');
    const sources = Array.isArray(it?.sources) ? it.sources : [];
    const confidence = typeof it?.confidence === 'number' ? it.confidence : NaN;
    const products = Array.isArray(it?.products) ? it.products : [];

    // Basic structure
    if (!verdict) warnings.push(`items[${idx}]: missing verdict`);
    if (verdict !== 'resolved' && verdict !== 'unknown') {
      warnings.push(`items[${idx}]: verdict must be "resolved" or "unknown" (got: ${verdict})`);
    }

    // edgeIndex and edgeId
    if (typeof it?.edgeIndex !== 'number') warnings.push(`items[${idx}]: missing edgeIndex`);
    if (typeof it?.edgeIndex === 'number' && it.edgeIndex !== idx) {
      warnings.push(`items[${idx}]: edgeIndex (${it.edgeIndex}) does not match array index (${idx})`);
    }
    const expectedEdgeId = ctx?.incidentEdges?.[idx]?.id;
    if (expectedEdgeId && String(it?.edgeId || '') !== expectedEdgeId) {
      warnings.push(`items[${idx}]: edgeId mismatch (expected: ${expectedEdgeId})`);
    }

    // supplierCanonicalPath and consumerCanonicalPath
    const supplierCP = String(it?.supplierCanonicalPath || '');
    const consumerCP = String(it?.consumerCanonicalPath || '');
    if (!supplierCP) warnings.push(`items[${idx}]: missing supplierCanonicalPath`);
    if (!consumerCP) warnings.push(`items[${idx}]: missing consumerCanonicalPath`);
    if (supplierCP && !isValidCanonicalPath(supplierCP)) warnings.push(`items[${idx}]: invalid supplierCanonicalPath: ${supplierCP}`);
    if (consumerCP && !isValidCanonicalPath(consumerCP)) warnings.push(`items[${idx}]: invalid consumerCanonicalPath: ${consumerCP}`);
    if (supplierCP && !knownOrgPaths.has(supplierCP)) warnings.push(`items[${idx}]: supplierCanonicalPath not in context: ${supplierCP}`);
    if (consumerCP && !knownOrgPaths.has(consumerCP)) warnings.push(`items[${idx}]: consumerCanonicalPath not in context: ${consumerCP}`);

    // Verdict + products consistency
    if (verdict === 'resolved' && products.length === 0) {
      warnings.push(`items[${idx}]: verdict="resolved" requires at least one product`);
    }
    if (verdict === 'unknown' && products.length > 0) {
      warnings.push(`items[${idx}]: verdict="unknown" should have empty products[]`);
    }

    // Validate each product
    products.forEach((prod: any, pIdx: number) => {
      const prodPath = String(prod?.canonicalPath || '');
      if (!prodPath) {
        warnings.push(`items[${idx}].products[${pIdx}]: missing canonicalPath`);
      } else {
        if (!isValidCanonicalPath(prodPath)) warnings.push(`items[${idx}].products[${pIdx}]: invalid canonicalPath: ${prodPath}`);
        if (!prodPath.startsWith('/product/')) warnings.push(`items[${idx}].products[${pIdx}]: must be /product/...: ${prodPath}`);
        // Warn if inventing a new product (but allow it)
        if (prodPath.startsWith('/product/') && !knownProductPaths.has(prodPath)) {
          // Soft warning only if multi-segment
          const rest = prodPath.slice('/product/'.length);
          if (rest.includes('/')) {
            warnings.push(`items[${idx}].products[${pIdx}]: new product path should be single-segment: ${prodPath}`);
          }
        }
      }
    });

    // Sources validation
    if (sources.length > 2) warnings.push(`items[${idx}]: sources.length must be <= 2 (got ${sources.length})`);
    for (const s of sources) {
      const url = String(s?.url || '');
      if (/\s/.test(url)) warnings.push(`items[${idx}]: source url contains whitespace: ${url}`);
      const host = hostFromURL(url);
      if (!host) warnings.push(`items[${idx}]: invalid source url: ${url || '(empty)'}`);
      if (url.toLowerCase().includes('example')) warnings.push(`items[${idx}]: placeholder url: ${url}`);
    }

    // Confidence validation
    if (Number.isFinite(confidence)) {
      if (confidence >= 1) warnings.push(`items[${idx}]: confidence must never be 1.0`);
      if (sources.length === 0 && confidence > 0.7) warnings.push(`items[${idx}]: confidence > 0.70 with 0 sources`);
      if (sources.length === 1 && confidence > 0.8) warnings.push(`items[${idx}]: confidence > 0.80 with 1 source`);
      if (sources.length === 2 && confidence > 0.9) warnings.push(`items[${idx}]: confidence > 0.90 with 2 sources`);
      if (verdict === 'unknown' && confidence > 0.65) warnings.push(`items[${idx}]: verdict="unknown" should have confidence <= 0.65`);
      const hosts = sources.map((s: any) => hostFromURL(String(s?.url || ''))).filter(Boolean) as string[];
      if (hosts.some(isWeakHost) && confidence > 0.8) warnings.push(`items[${idx}]: weak source with confidence > 0.80`);
    }
  });

  return warnings;
}

function normalizeResultForDisplay(resultJSON: any, ctx: HorizonOnDemandContext): any {
  if (!resultJSON || typeof resultJSON !== 'object') return resultJSON;
  const rawItems = Array.isArray((resultJSON as any).items) ? (resultJSON as any).items : [];

  const clamp = (x: number, cap: number) => (Number.isFinite(x) ? Math.min(x, cap) : x);

  const isWeakHost = (h: string) =>
    h.includes('wikipedia.org') || h.includes('medium.com') || h.includes('substack.com') || h.includes('blog') || h.includes('insight');

  const normalizedItems = rawItems.map((it: any, idx: number) => {
    const out: any = { ...it };
    const sources = Array.isArray(out?.sources) ? out.sources : [];
    const hosts = sources.map((s: any) => hostFromURL(String(s?.url || ''))).filter(Boolean) as string[];
    const verdict = String(out?.verdict || '');

    // Ensure edgeIndex matches array index
    out.edgeIndex = idx;

    // Ensure edgeId is set from context if missing
    if (!out.edgeId && ctx?.incidentEdges?.[idx]?.id) {
      out.edgeId = ctx.incidentEdges[idx].id;
    }

    // Confidence caps (display-safety)
    let cap = 0.95;
    if (sources.length === 0) cap = Math.min(cap, 0.7);
    if (sources.length === 1) cap = Math.min(cap, 0.8);
    if (sources.length === 2) cap = Math.min(cap, 0.9);
    if (hosts.some(isWeakHost)) cap = Math.min(cap, 0.8);
    if (verdict === 'unknown') cap = Math.min(cap, 0.65);

    if (typeof out?.confidence === 'number') out.confidence = clamp(out.confidence, cap);

    return out;
  });

  return { ...resultJSON, items: normalizedItems };
}

export const HorizonOnDemandDependencyResolverModal: React.FC<{
  open: boolean;
  onClose: () => void;
  context: HorizonOnDemandContext;
}> = ({ open, onClose, context }) => {
  const { theme } = React.useContext(ThemeContext);
  const { aliased: aliasedContext, idMap } = React.useMemo(() => aliasContextIds(context), [context]);
  const [prompt, setPrompt] = React.useState(() => buildDefaultPrompt(aliasedContext));
  const [running, setRunning] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [result, setResult] = React.useState<any>(null);

  React.useEffect(() => {
    if (!open) return;
    setPrompt(buildDefaultPrompt(aliasedContext));
    setError(null);
    setResult(null);
  }, [open, aliasedContext]);

  if (!open) return null;

  const title = 'On-demand: resolve supply-chain products';

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-[90] flex items-center justify-center"
      style={{
        background: 'rgba(0,0,0,0.38)',
        backdropFilter: 'blur(6px)',
        padding: 18,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className="w-full max-w-4xl rounded-2xl border overflow-hidden"
        style={{
          background: 'var(--bg-elevated)',
          borderColor: 'var(--border-subtle)',
          boxShadow: theme === 'dark' ? '0 30px 90px rgba(0,0,0,0.55)' : '0 30px 90px rgba(0,0,0,0.18)',
        }}
      >
        <div
          className="px-5 py-4 flex items-start justify-between gap-4"
          style={{ borderBottom: '1px solid var(--border-subtle)' }}
        >
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)' }}>{title}</div>
            <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
              This does not write anything to the graph. It only runs a grounded LLM call and shows JSON request/response.
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded-lg border px-3 py-2 text-[12px]"
            style={{
              borderColor: 'var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-heading)',
              cursor: 'pointer',
              flexShrink: 0,
            }}
          >
            Close
          </button>
        </div>

        <div className="p-5 grid grid-cols-1 lg:grid-cols-2 gap-4">
          {/* Left: task + context */}
          <div className="rounded-xl border p-4" style={{ borderColor: 'var(--border-subtle)', background: 'var(--bg-primary)' }}>
            <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>Request context (JSON)</div>
            <div style={{ marginTop: 8, display: 'flex', gap: 8 }}>
              <button
                type="button"
                className="px-3 py-2 rounded-lg border text-[12px]"
                style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                onClick={() => void copyText(safeJSONStringify(context))}
              >
                Copy context
              </button>
              <button
                type="button"
                className="px-3 py-2 rounded-lg border text-[12px]"
                style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                onClick={() => void copyText(safeJSONStringify(aliasedContext))}
              >
                Copy aliased context
              </button>
              <button
                type="button"
                className="px-3 py-2 rounded-lg border text-[12px]"
                style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                onClick={() => void copyText(safeJSONStringify(idMap))}
              >
                Copy id map
              </button>
              <button
                type="button"
                className="px-3 py-2 rounded-lg border text-[12px]"
                style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                onClick={() =>
                  void copyText(
                    safeJSONStringify({
                      prompt,
                      endpoint: '/api/v1/ai/structured',
                      body: {
                        prompt,
                        minCapabilityScore: 6,
                        maxOutputSize: 16384,
                      },
                    }),
                  )
                }
              >
                Copy LLM request
              </button>
            </div>
            <pre
              className="mt-3 text-[11px] rounded-lg border p-3 overflow-auto max-h-[340px]"
              style={{
                borderColor: 'var(--border-subtle)',
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.75)',
                color: 'var(--text-heading)',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {safeJSONStringify(context)}
            </pre>
            <div className="mt-2 text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              LLM sees aliased ids: <b>{Object.keys(idMap.forward || {}).length}</b> ids mapped.
            </div>
          </div>

          {/* Right: prompt + run */}
          <div className="rounded-xl border p-4" style={{ borderColor: 'var(--border-subtle)', background: 'var(--bg-primary)' }}>
            <div className="flex items-start justify-between gap-3">
              <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>Prompt</div>
              <button
                type="button"
                disabled={running}
                className="px-3 py-2 rounded-lg text-[12px]"
                style={{
                  background: running ? 'var(--bg-secondary)' : 'var(--accent-primary)',
                  color: 'var(--text-on-accent)',
                  border: 'none',
                  cursor: running ? 'not-allowed' : 'pointer',
                  opacity: running ? 0.7 : 1,
                  flexShrink: 0,
                  fontWeight: 850,
                }}
                onClick={async () => {
                  setRunning(true);
                  setError(null);
                  setResult(null);
                  try {
                    const res = await api.aiStructured({
                      prompt,
                      minCapabilityScore: 6,
                      maxOutputSize: 16384,
                    });
                    // Normalize shape to match existing UI expectations
                    const normalized = {
                      ok: Boolean(res?.ok),
                      model: 'model-manager',
                      usage: undefined,
                      validJson: true,
                      json: res?.result ?? null,
                      raw: safeJSONStringify(res?.result ?? null),
                    };
                    setResult(normalized);
                    if (!normalized.ok) setError('LLM call failed (ok=false)');
                  } catch (e: any) {
                    setError(formatErrorForDisplay(e));
                  } finally {
                    setRunning(false);
                  }
                }}
              >
                {running ? 'Running…' : 'Run'}
              </button>
            </div>

            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              className="mt-3 w-full rounded-lg border p-3 text-[12px]"
              style={{
                minHeight: 260,
                borderColor: 'var(--border-subtle)',
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.75)',
                color: 'var(--text-heading)',
                outline: 'none',
                fontFamily:
                  'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
              }}
            />

            {error && (
              <div className="mt-3 text-[12px]" style={{ color: '#f87171', whiteSpace: 'pre-wrap' }}>
                {error}
              </div>
            )}

            {result && (
              <div className="mt-4">
                <div className="flex items-center justify-between gap-2">
                  <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>Response</div>
                  <div className="flex items-center gap-8">
                    <button
                      type="button"
                      className="px-3 py-2 rounded-lg border text-[12px]"
                      style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                      onClick={() => void copyText(safeJSONStringify(result?.json ?? null))}
                    >
                      Copy JSON
                    </button>
                    <button
                      type="button"
                      className="px-3 py-2 rounded-lg border text-[12px]"
                      style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                      onClick={() => void copyText(safeJSONStringify(normalizeResultForDisplay(result?.json, context) ?? null))}
                    >
                      Copy normalized JSON
                    </button>
                    <button
                      type="button"
                      className="px-3 py-2 rounded-lg border text-[12px]"
                      style={{ borderColor: 'var(--border-subtle)', background: 'transparent', color: 'var(--text-heading)', cursor: 'pointer' }}
                      onClick={() => void copyText(String(result?.raw ?? ''))}
                    >
                      Copy raw
                    </button>
                  </div>
                </div>

                <div className="mt-2 text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                  model: <b>{String(result?.model || 'unknown')}</b>, validJson: <b>{String(Boolean(result?.validJson))}</b>, tokens: <b>{String(result?.usage?.totalTokens ?? '')}</b>
                </div>

                {(() => {
                  const warnings = buildResultWarnings(result?.json, context);
                  if (!warnings.length) return null;
                  return (
                    <div className="mt-3 rounded-lg border p-3" style={{ borderColor: 'rgba(248,113,113,0.6)', background: 'rgba(248,113,113,0.08)' }}>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>Lint warnings</div>
                      <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                        These indicate violations of the prompt’s non-negotiable rules (confidence caps, scopeTag semantics, schema constraints).
                      </div>
                      <ul className="mt-2 text-[11px]" style={{ color: 'var(--text-heading)', paddingLeft: 18, listStyle: 'disc' }}>
                        {warnings.slice(0, 30).map((w) => (
                          <li key={w}>{w}</li>
                        ))}
                      </ul>
                      {warnings.length > 30 && (
                        <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                          Showing first 30 of {warnings.length}.
                        </div>
                      )}
                    </div>
                  );
                })()}

                <pre
                  className="mt-3 text-[11px] rounded-lg border p-3 overflow-auto max-h-[300px]"
                  style={{
                    borderColor: 'var(--border-subtle)',
                    background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.75)',
                    color: 'var(--text-heading)',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                  }}
                >
                  {safeJSONStringify(result?.json ?? { raw: result?.raw })}
                </pre>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};


