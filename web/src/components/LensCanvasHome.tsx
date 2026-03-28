import React from 'react';
import { api } from '../lib/api';
import { geoCodeToDisplayName, getDomainRegionLabel, getScopeGeoLabel, humanizeCanonicalPath, preferDisplayLabel } from '../lib/uiUtils';
import { formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors, HorizonNodeIcon } from './HorizonGraphNodeTypes';
import AssembleLensRequestComponent, { type AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';
import { useProjectsStore } from '../stores/projectsStore';

function pruneMapToMax<K, V>(m: Map<K, V>, maxSize: number) {
  if (maxSize <= 0) {
    m.clear();
    return;
  }
  while (m.size > maxSize) {
    const first = m.keys().next();
    if (first.done) break;
    m.delete(first.value);
  }
}

function boundedMapSet<K, V>(m: Map<K, V>, key: K, value: V, maxSize: number) {
  m.set(key, value);
  pruneMapToMax(m, maxSize);
}

export interface LensCanvasHomeProps {
  lens?: any | null;
  projectId?: string | null;
  showInput?: boolean;
  onAssemble?: (request: AssembleLensRequest) => void;
  onOpenGraph?: () => void;
  onOpenMap?: () => void;
  isCreatingLens?: boolean;
  assemblyStatus: any;
  scoutingJobs: any[];
  loadingJobs: boolean;
  onRefreshJobs: () => void;
  onUpdateScope?: (nextScope: string[]) => Promise<void>;
}

export const LensCanvasHome: React.FC<LensCanvasHomeProps> = ({
  lens,
  projectId,
  showInput = false,
  onAssemble,
  onOpenGraph,
  onOpenMap,
  isCreatingLens = false,
  assemblyStatus,
  scoutingJobs,
  loadingJobs,
  onRefreshJobs,
  onUpdateScope,
}) => {
  const currentProject = useProjectsStore((s) => s.currentProject);
  const formatFragmentTypeLabel = (value?: string, canonicalPath?: string, label?: string) => {
    const raw = String(value || '').trim();
    if (raw) {
      return formatHorizonFragmentType(raw, canonicalPath, label);
    }
    const path = String(canonicalPath || '').trim().toLowerCase();
    if (path.startsWith('/org/')) return 'Organization';
    if (path.startsWith('/gov/')) return 'Government';
    if (path.startsWith('/fac/')) return 'Facility';
    if (path.startsWith('/loc/')) return 'Location';
    if (path.startsWith('/product/')) return 'Product';
    if (path.startsWith('/market/')) return 'Market';
    if (path.startsWith('/community/')) return 'Community';
    if (path.startsWith('/person/')) return 'Person';
    if (path.startsWith('/program/')) return 'Program';
    if (path.startsWith('/deposit/')) return 'Deposit';
    if (path.startsWith('/zone/')) return 'Affected Zone';
    if (path.startsWith('/domain/')) return 'Domain';
    if (path.startsWith('/query/')) return 'Query';
    if (path.startsWith('/artifact/')) return 'Artifact';
    return label ? 'Object' : 'Unknown';
  };
  const isSupplyChainContractType = (value?: string) => /^(supply-chain|value-chain)$/i.test(String(value || '').trim());
  const getSupplyChainGraphBuildModeLabel = (value?: string) => {
    const mode = String(value || '').trim().toLowerCase();
    if (mode === 'hot_path_only') return 'HOT PATH ONLY';
    if (mode === 'hot_path_plus_graph') return 'HOT PATH + GRAPH';
    return '';
  };
  type SearchEntity = {
    id: string;
    canonicalPath: string;
    name: string;
    type: string;
    description?: string;
    location?: string;
    sector?: string;
  };

  const [newScopePath, setNewScopePath] = React.useState('');
  const [savingScope, setSavingScope] = React.useState(false);
  const [scopeError, setScopeError] = React.useState<string | null>(null);
  const scopeInputRef = React.useRef<HTMLInputElement | null>(null);
  const scopeSuggestRef = React.useRef<HTMLDivElement | null>(null);
  const [scopeSuggestOpen, setScopeSuggestOpen] = React.useState(false);
  const [scopeSuggestLoading, setScopeSuggestLoading] = React.useState(false);
  const [scopeSuggestError, setScopeSuggestError] = React.useState<string | null>(null);
  const [scopeSuggestResults, setScopeSuggestResults] = React.useState<SearchEntity[]>([]);
  const [scopeSuggestIndex, setScopeSuggestIndex] = React.useState(0);
  const scopeSuggestAbortRef = React.useRef<AbortController | null>(null);
  const scopeSuggestCacheRef = React.useRef<Map<string, SearchEntity[]>>(new Map());
  const [newsLoading, setNewsLoading] = React.useState(false);
  const [newsError, setNewsError] = React.useState<string | null>(null);
  const scope: string[] = Array.isArray(lens?.scope) ? lens.scope : [];
  const scopeFragments: Record<string, string> = (lens?.scopeFragments && typeof lens.scopeFragments === 'object') ? lens.scopeFragments : {};
  const scopeArtifacts: Record<string, {
    kind?: string;
    displayName?: string;
    anchorSupplier?: string;
    anchorCustomer?: string;
    productAxis?: string;
    nodeRefs?: string[];
    status?: string;
    flowRefs?: string[];
    zoneRefs?: string[];
    affectedZoneRefs?: string[];
    spatialSemantics?: string[];
    legCount?: number;
    bindingConfidence?: number;
    pathStressScore?: number;
    bottleneckLegIndex?: number;
    recoveryLatencyHours?: number;
  }> =
    (lens?.scopeArtifacts && typeof lens.scopeArtifacts === 'object') ? lens.scopeArtifacts : {};
  const resolvedQueries: Record<string, string[]> = (lens?.resolvedQueries && typeof lens.resolvedQueries === 'object') ? lens.resolvedQueries : {};
  const intents = Array.isArray(lens?.intents) ? lens.intents : [];
  const domains = Array.isArray(lens?.viewConfig?.domains) ? lens.viewConfig.domains : [];
  const domainModes = Array.isArray(lens?.domainModes) ? lens.domainModes : [];
  const domainModesOnly = domainModes.length > 0 && domains.length === 0;
  const totalObjects = scope.length + intents.length + domains.length + (domainModesOnly ? domainModes.length : 0);

  type ScopeResolved = { name?: string; type?: string; fragmentId?: string; status: 'resolved' | 'error' };
  const [scopeResolved, setScopeResolved] = React.useState<Record<string, ScopeResolved>>({});
  const scopeResolveInFlightRef = React.useRef<Set<string>>(new Set());

  const dedupe = (arr: string[]) => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const v of arr) {
      const s = (v || '').trim();
      if (!s) continue;
      if (seen.has(s)) continue;
      seen.add(s);
      out.push(s);
    }
    return out;
  };

  const isValidScopeEntry = (v: string) => {
    const s = (v || '').trim();
    if (!s) return false;
    // Allow canonical paths and query addresses.
    if (s.startsWith('/query/')) return true;
    return s.startsWith('/');
  };

  const isStaticCanonicalPath = (v: string) => {
    const s = (v || '').trim();
    return s.startsWith('/') && !s.startsWith('/query/');
  };

  const closeScopeSuggest = React.useCallback(() => {
    setScopeSuggestOpen(false);
    setScopeSuggestError(null);
    setScopeSuggestLoading(false);
    setScopeSuggestResults([]);
    setScopeSuggestIndex(0);
    if (scopeSuggestAbortRef.current) {
      try { scopeSuggestAbortRef.current.abort(); } catch { /* ignore */ }
    }
    scopeSuggestAbortRef.current = null;
  }, []);

  // Memory safety: clear per-lens caches when switching lenses / unmounting.
  React.useEffect(() => {
    scopeSuggestCacheRef.current.clear();
    scopeResolveInFlightRef.current.clear();
    setScopeResolved({});
    closeScopeSuggest();
  }, [lens?.id, closeScopeSuggest]);
  React.useEffect(() => {
    return () => {
      try { scopeSuggestAbortRef.current?.abort(); } catch { /* ignore */ }
      scopeSuggestAbortRef.current = null;
      scopeSuggestCacheRef.current.clear();
      scopeResolveInFlightRef.current.clear();
    };
  }, []);

  const pickScopeSuggestion = React.useCallback((r: SearchEntity) => {
    setNewScopePath(r.canonicalPath);
    closeScopeSuggest();
    // Keep focus to allow quick Enter -> Add
    window.setTimeout(() => scopeInputRef.current?.focus(), 0);
  }, [closeScopeSuggest]);

  // Scope input: if user types a name (not starting with "/"), prefetch entities and allow picking a canonical path.
  React.useEffect(() => {
    const q = (newScopePath || '').trim();
    if (!q) {
      closeScopeSuggest();
      return;
    }
    if (q.startsWith('/')) {
      // Canonical paths / query addresses are entered directly.
      closeScopeSuggest();
      return;
    }
    if (q.length < 2) {
      setScopeSuggestOpen(true);
      setScopeSuggestError(null);
      setScopeSuggestLoading(false);
      setScopeSuggestResults([]);
      setScopeSuggestIndex(0);
      return;
    }

    setScopeSuggestOpen(true);
    setScopeSuggestError(null);

    const key = q.toLowerCase();
    const cached = scopeSuggestCacheRef.current.get(key);
    if (cached) {
      setScopeSuggestResults(cached);
      setScopeSuggestLoading(false);
      setScopeSuggestIndex(0);
      return;
    }

    const t = window.setTimeout(async () => {
      if (scopeSuggestAbortRef.current) {
        try { scopeSuggestAbortRef.current.abort(); } catch { /* ignore */ }
      }
      const ac = new AbortController();
      scopeSuggestAbortRef.current = ac;
      setScopeSuggestLoading(true);
      try {
        const res = await api.searchHorizonEntities(q, 8, ac.signal);
        const list = Array.isArray(res?.entities) ? (res.entities as SearchEntity[]) : [];
        boundedMapSet(scopeSuggestCacheRef.current, key, list, 80);
        setScopeSuggestResults(list);
        setScopeSuggestIndex(0);
        setScopeSuggestLoading(false);
      } catch (e: any) {
        if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return;
        setScopeSuggestLoading(false);
        setScopeSuggestError('Search failed');
      }
    }, 180);

    return () => window.clearTimeout(t);
  }, [newScopePath, closeScopeSuggest]);

  // Close suggestions on Esc (do NOT use global mousedown capture here:
  // on some devices it can steal focus while typing).
  React.useEffect(() => {
    if (!scopeSuggestOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        closeScopeSuggest();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [scopeSuggestOpen, closeScopeSuggest]);

  // Progressive enhancement: resolve canonical scope paths -> fragment name/type (cached).
  React.useEffect(() => {
    const lensId = String(lens?.id || '').trim();
    if (!lensId) return;

    const paths = scope.map((s) => (s || '').trim()).filter((p) => isStaticCanonicalPath(p) && !p.startsWith('/artifact/') && !p.startsWith('/domain/'));
    if (paths.length === 0) return;

    let cancelled = false;
    const run = async () => {
      // Resolve in small batches to avoid request storms.
      const unresolved = paths.filter((p) => !scopeResolved[p] && !scopeResolveInFlightRef.current.has(p));
      if (unresolved.length === 0) return;

      const batchSize = 4;
      for (let i = 0; i < unresolved.length; i += batchSize) {
        const batch = unresolved.slice(i, i + batchSize);
        batch.forEach((p) => scopeResolveInFlightRef.current.add(p));
        try {
          const results = await Promise.all(
            batch.map(async (path) => {
              try {
                const cachedId = scopeFragments?.[path];
                const frag = cachedId ? await api.getHorizonFragment(cachedId) : await api.getHorizonFragmentByPath(path);
                const rawName = frag?.name || frag?.displayLabel || frag?.label || undefined;
                const preferred = preferDisplayLabel(rawName, frag?.canonicalPath, frag?.type);
                const name = preferred || humanizeCanonicalPath(frag?.canonicalPath) || rawName || undefined;
                const type = frag?.type || frag?.displayType || undefined;
                const fragmentId = frag?.id || cachedId || undefined;
                return { path, data: { name, type, fragmentId, status: 'resolved' as const } };
              } catch {
                return { path, data: { status: 'error' as const } };
              }
            })
          );
          if (cancelled) return;
          setScopeResolved((prev) => {
            const next = { ...prev };
            for (const r of results) {
              next[r.path] = r.data;
            }
            return next;
          });
        } finally {
          batch.forEach((p) => scopeResolveInFlightRef.current.delete(p));
        }
      }
    };

    void run();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lens?.id, scope.join('\n'), JSON.stringify(scopeFragments)]);

  const handleAddScope = async () => {
    if (!onUpdateScope) return;
    setScopeError(null);
    const value = newScopePath.trim();
    if (!isValidScopeEntry(value)) {
      setScopeError('Enter a canonical path (starts with "/") or a query address (starts with "/query/").');
      return;
    }
    const next = dedupe([...scope, value]);
    if (next.length === scope.length) {
      setNewScopePath('');
      return;
    }
    setSavingScope(true);
    try {
      await onUpdateScope(next);
      setNewScopePath('');
    } catch (e: any) {
      setScopeError(e?.response?.data?.error || e?.message || 'Failed to update scope');
    } finally {
      setSavingScope(false);
    }
  };

  const handleRemoveScope = async (path: string) => {
    if (!onUpdateScope) return;
    setScopeError(null);
    const p = (path || '').trim();
    if (!p) return;
    if (!window.confirm(`Remove scope entry?\n\n${p}`)) return;
    const next = scope.filter((x) => (x || '').trim() !== p);
    if (next.length === 0) {
      setScopeError('Scope must contain at least one entry.');
      return;
    }
    setSavingScope(true);
    try {
      await onUpdateScope(next);
    } catch (e: any) {
      setScopeError(e?.response?.data?.error || e?.message || 'Failed to update scope');
    } finally {
      setSavingScope(false);
    }
  };

  const handleLoadNews = React.useCallback(async () => {
    const paths = scope.filter((p) => {
      const s = (p || '').trim();
      return s.startsWith('/') && !s.startsWith('/query/');
    });
    if (paths.length === 0) {
      setNewsError('Add at least one canonical path to scope (e.g. /org/ae/deloitte)');
      return;
    }
    setNewsLoading(true);
    setNewsError(null);
    try {
      await api.fetchNewsByFragments({
        fragments: paths.map((cp) => ({
          canonicalPath: cp,
          canonicalName: scopeFragments[cp] || undefined,
        })),
        daysBack: 7,
      });
    } catch (e: any) {
      setNewsError(e?.response?.data?.error || e?.message || 'Failed to load news');
    } finally {
      setNewsLoading(false);
    }
  }, [scope, scopeFragments]);

  const Pill: React.FC<{ text: string; tone?: 'neutral' | 'success' | 'danger' | 'amber' }> = ({ text, tone = 'neutral' }) => {
    const styles =
      tone === 'success'
        ? { bg: 'var(--success-20)', fg: 'var(--accent-success)' }
        : tone === 'danger'
          ? { bg: 'rgba(248, 113, 113, 0.18)', fg: '#f87171' }
          : tone === 'amber'
            ? { bg: 'rgba(251, 191, 36, 0.18)', fg: 'rgba(251, 191, 36, 0.92)' }
            : { bg: 'var(--white-10)', fg: 'var(--text-secondary-strong)' };
    return (
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          padding: '3px 8px',
          borderRadius: 999,
          fontSize: 11,
          fontWeight: 750,
          background: styles.bg,
          color: styles.fg,
          whiteSpace: 'nowrap',
        }}
      >
        {text}
      </span>
    );
  };

  const SummaryChip: React.FC<{
    label: string;
    type?: string;
    canonicalPath?: string;
    facilityKind?: string | null;
  }> = ({ label, type, canonicalPath, facilityKind }) => {
    const visualType = getHorizonNodeVisualType(type, canonicalPath, label, facilityKind);
    const colors = getNodeColors(visualType, 'dark');
    return (
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          padding: '4px 10px',
          borderRadius: 999,
          border: `1px solid ${colors.border}`,
          background: colors.bg,
          color: colors.text,
          fontSize: 12,
          fontWeight: 650,
          lineHeight: 1.2,
          whiteSpace: 'nowrap',
        }}
      >
        <HorizonNodeIcon type={visualType} color={colors.accent} size={12} />
        <span>{label}</span>
      </span>
    );
  };

  const mono: React.CSSProperties = {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
  };

  const CollapseSection: React.FC<{
    title: string;
    subtitle?: string;
    defaultOpen?: boolean;
    children: React.ReactNode;
    right?: React.ReactNode;
  }> = ({ title, subtitle, defaultOpen = false, children, right }) => (
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
          padding: '14px 16px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
        }}
      >
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 13, fontWeight: 800, color: 'var(--text-heading)' }}>{title}</div>
          {subtitle ? (
            <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>{subtitle}</div>
          ) : null}
        </div>
        {right}
      </summary>
      <div style={{ padding: '0 16px 16px 16px', borderTop: '1px solid var(--border-subtle)' }}>{children}</div>
    </details>
  );

  const assemblyStage = String((assemblyStatus as any)?.stage || assemblyStatus?.phase || '').trim();
  const artifactEntries = Object.entries(scopeArtifacts);
  const uniqueNodeRefs = new Set<string>();
  const uniqueFlowRefs = new Set<string>();
  const uniqueZoneRefs = new Set<string>();
  const kindCounts = new Map<string, number>();
  for (const [, artifact] of artifactEntries) {
    const kind = String(artifact?.kind || 'artifact').trim() || 'artifact';
    kindCounts.set(kind, (kindCounts.get(kind) || 0) + 1);
    for (const ref of artifact?.nodeRefs || []) {
      const value = String(ref || '').trim();
      if (value) uniqueNodeRefs.add(value);
    }
    for (const ref of artifact?.flowRefs || []) {
      const value = String(ref || '').trim();
      if (value) uniqueFlowRefs.add(value);
    }
    for (const ref of [...(artifact?.zoneRefs || []), ...(artifact?.affectedZoneRefs || [])]) {
      const value = String(ref || '').trim();
      if (value) uniqueZoneRefs.add(value);
    }
  }
  const workscopeKinds = Array.from(kindCounts.entries()).sort((a, b) => b[1] - a[1]);
  const supplyPathCount = artifactEntries.filter(([, artifact]) => String(artifact?.kind || '').trim().toUpperCase() === 'SUPPLY_CHAIN_PATH').length;
  const recentJobs = scoutingJobs.slice(0, 8);
  const showCommandPalette = Boolean(showInput && projectId && onAssemble);
  const homeCommandPlaceholder = totalObjects > 0 || artifactEntries.length > 0
    ? 'What would you like to extend or explore next?'
    : undefined;
  const showHomeViewActions = Boolean((totalObjects > 0 || artifactEntries.length > 0) && (onOpenGraph || onOpenMap));
  const projectDisplayName = (
    (currentProject?.id && projectId && currentProject.id === projectId ? currentProject.displayName : '')
    || String(lens?.projectName || '').trim()
    || String(currentProject?.displayName || '').trim()
    || String(projectId || '').trim()
  );
  const hasHomeSummary = Boolean(
    String(lens?.name || '').trim()
    || String(lens?.id || '').trim()
    || totalObjects > 0
    || artifactEntries.length > 0
    || String(assemblyStage || '').trim()
    || scoutingJobs.length > 0,
  );
  const domainCanonicalPaths = new Set(
    domains
      .map((domain: any) => String(domain?.canonicalPath || '').trim())
      .filter(Boolean),
  );
  const scopeObjectItems: Array<{
    id: string;
    label: string;
    sublabel?: string;
    badges: Array<{
      text: string;
      tone?: 'neutral' | 'success' | 'danger' | 'amber';
      kind?: 'nodeType';
      objectType?: string;
      canonicalPath?: string;
      objectLabel?: string;
    }>;
  }> = [
    ...intents.map((entry: any, idx: number) => ({
      id: `intent-${entry?.id || idx}`,
      label: String(entry?.question || 'Intent').trim() || 'Intent',
      sublabel: undefined,
      badges: [{ text: 'INTENT', tone: entry?.status === 'answered' ? 'success' : 'neutral' }],
    })),
    ...domains.map((domain: any, idx: number) => {
      const path = String(domain?.canonicalPath || '').trim();
      const contractType = String(domain?.contractType || 'DOMAIN').trim();
      const regionLabel = getDomainRegionLabel(path);
      const modeLabel = isSupplyChainContractType(contractType) ? getSupplyChainGraphBuildModeLabel(domain?.supplyChainExecutionPlan?.graphBuildMode) : '';
      return {
        id: `domain-${path || idx}`,
        label: String(domain?.title || domain?.description || humanizeCanonicalPath(path) || contractType).trim(),
        sublabel: path || undefined,
        badges: [
          { text: contractType, tone: 'amber' as const },
          ...(regionLabel ? [{ text: regionLabel, tone: 'amber' as const }] : []),
          ...(modeLabel ? [{ text: modeLabel, tone: 'success' as const }] : []),
        ],
      };
    }),
    ...scope
      .filter((entry: string) => {
        const path = String(entry || '').trim();
        if (!path) return false;
        if (domainCanonicalPaths.has(path)) return false;
        return true;
      })
      .map((entry: string, index: number) => {
      const path = (entry || '').trim();
      const isQuery = path.startsWith('/query/');
      const isArtifact = path.startsWith('/artifact/');
      const resolved = scopeResolved[path];
      const artifactKey = path.replace(/^\/artifact\//, '');
      const artifact = scopeArtifacts[artifactKey];
      const label = isArtifact
        ? String(artifact?.displayName || humanizeCanonicalPath(path) || path).trim()
        : isQuery
          ? String(path.replace(/^\/query\//, '') || path).trim()
          : String(resolved?.name || humanizeCanonicalPath(path) || path).trim();
      const typeLabel = isArtifact
        ? String(artifact?.kind || 'ARTIFACT').trim().replace(/_/g, ' ')
        : isQuery
          ? 'Query'
          : formatFragmentTypeLabel(resolved?.type, path, label);
      const badges = isArtifact
        ? [
            { text: typeLabel, tone: 'amber' as const },
            ...(artifact?.status ? [{ text: String(artifact.status), tone: artifact.status === 'validated' ? 'success' as const : 'neutral' as const }] : []),
          ]
        : [{
            text: typeLabel,
            kind: 'nodeType' as const,
            objectType: resolved?.type,
            canonicalPath: path,
            objectLabel: label,
          }];
      return {
        id: `scope-${path}-${index}`,
        label,
        sublabel: path || undefined,
        badges,
      };
      }),
  ];

  const scopedEntities = scopeObjectItems
    .filter((item) => item.badges.some((badge) => badge.kind === 'nodeType'))
    .map((item) => ({
      label: item.label,
      type: String(item.badges.find((badge) => badge.kind === 'nodeType')?.objectType || '').trim(),
      canonicalPath: String(item.badges.find((badge) => badge.kind === 'nodeType')?.canonicalPath || '').trim(),
    }));

  const entityItems = scopedEntities
    .filter((item) => {
      const path = item.canonicalPath.toLowerCase();
      return path.startsWith('/org/') || path.startsWith('/gov/') || path.startsWith('/fac/') || path.startsWith('/person/');
    })
    .filter((item, index, arr) => arr.findIndex((candidate) => candidate.label === item.label && candidate.canonicalPath === item.canonicalPath) === index)
    .slice(0, 6);
  const productItems = scopedEntities
    .filter((item) => item.canonicalPath.toLowerCase().startsWith('/product/'))
    .filter((item, index, arr) => arr.findIndex((candidate) => candidate.label === item.label && candidate.canonicalPath === item.canonicalPath) === index)
    .slice(0, 6);
  const marketItems = scopedEntities
    .filter((item) => item.canonicalPath.toLowerCase().startsWith('/market/'))
    .filter((item, index, arr) => arr.findIndex((candidate) => candidate.label === item.label && candidate.canonicalPath === item.canonicalPath) === index)
    .slice(0, 4);
  const countryItems: Array<{ code: string; label: string }> = Array.from(new Map<string, string>([
    ...scope
      .map((entry) => {
        const path = String(entry || '').trim().toLowerCase();
        const parts = path.split('/').filter(Boolean);
        if (parts.length >= 2 && ['org', 'gov', 'fac', 'market', 'community', 'loc'].includes(parts[0])) {
          const code = String(parts[1] || '').trim().toLowerCase();
          if (/^[a-z]{2}$/.test(code)) return [code, geoCodeToDisplayName(code)] as const;
        }
        return null;
      })
      .filter((value): value is readonly [string, string] => Boolean(value)),
    ...domains.flatMap((domain: any) => {
      const path = String(domain?.canonicalPath || '').trim().toLowerCase();
      const parts = path.split('/').filter(Boolean);
      const out: Array<readonly [string, string]> = [];
      for (let i = 0; i + 1 < parts.length; i += 1) {
        if (parts[i] === 'geo') {
          const token = String(parts[i + 1] || '').trim();
          for (const piece of token.split('-')) {
            const cc = piece.toLowerCase();
            if (/^[a-z]{2}$/.test(cc)) out.push([cc, geoCodeToDisplayName(cc)] as const);
          }
        }
      }
      return out;
    }),
  ]).entries())
    .map(([code, label]) => ({ code, label }))
    .slice(0, 8);
  const centerHomeStack = Boolean(showCommandPalette || hasHomeSummary);

  return (
    <div style={{ padding: '82px 18px 18px 18px', width: '100%', height: '100%', overflow: 'auto' }}>
      <div
        style={{
          width: '100%',
          maxWidth: 760,
          minHeight: '100%',
          margin: '0 auto',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: centerHomeStack ? 'center' : 'flex-start',
          gap: 14,
        }}
      >
        {(hasHomeSummary || showCommandPalette) ? (
          <div style={{ width: '100%', maxWidth: 760, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 14 }}>
            {hasHomeSummary ? (
              <div style={{ width: '100%', paddingLeft: 24, display: 'flex', flexDirection: 'column', gap: 8, alignItems: 'flex-start', textAlign: 'left' }}>
                {projectDisplayName ? (
                  <div
                    style={{
                      fontSize: 15,
                      lineHeight: 1.4,
                      color: 'var(--text-heading)',
                      fontWeight: 650,
                    }}
                  >
                    {projectDisplayName} project
                  </div>
                ) : null}
                <div style={{ width: '100%', maxWidth: 640, display: 'flex', flexDirection: 'column', gap: 10, alignItems: 'flex-start' }}>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-start', gap: 8, flexWrap: 'wrap' }}>
                    <span style={{ fontSize: 13, lineHeight: 1.45, color: 'var(--text-secondary-strong)', fontWeight: 600 }}>
                      Topic:
                    </span>
                    <span style={{ fontSize: 14, lineHeight: 1.45, color: 'var(--text-tertiary)', fontWeight: 430 }}>
                      {String(lens?.name || 'Lens').trim() || 'Lens'}
                    </span>
                    <span style={{ fontSize: 13, lineHeight: 1.45, color: 'var(--text-secondary-strong)', fontWeight: 600 }}>
                      , this lens is
                    </span>
                    <Pill
                      text={String(lens?.status || 'UNKNOWN')}
                      tone={String(lens?.status || '').toUpperCase() === 'ACTIVE' ? 'success' : 'neutral'}
                    />
                  </div>
                  <div style={{ width: '100%', borderTop: '1px solid var(--border-subtle)' }} />
                </div>
                <div style={{ display: 'flex', width: '100%', flexDirection: 'column', gap: 6, alignItems: 'flex-start' }}>
                  {entityItems.length > 0 ? (
                    <div style={{ maxWidth: 640, fontSize: 13, lineHeight: 1.55, color: 'var(--text-secondary-strong)', fontWeight: 450 }}>
                      <span>This lens focuses on the following entities: </span>
                      <span style={{ display: 'inline-flex', flexWrap: 'wrap', justifyContent: 'flex-start', gap: 6, verticalAlign: 'middle' }}>
                        {entityItems.map((item) => (
                          <SummaryChip
                            key={`entity-${item.canonicalPath || item.label}`}
                            label={item.label}
                            type={item.type}
                            canonicalPath={item.canonicalPath}
                          />
                        ))}
                      </span>
                    </div>
                  ) : null}
                  {productItems.length > 0 ? (
                    <div style={{ maxWidth: 640, fontSize: 13, lineHeight: 1.55, color: 'var(--text-secondary-strong)', fontWeight: 450 }}>
                      <span>Products in focus: </span>
                      <span style={{ display: 'inline-flex', flexWrap: 'wrap', justifyContent: 'flex-start', gap: 6, verticalAlign: 'middle' }}>
                        {productItems.map((item) => (
                          <SummaryChip
                            key={`product-${item.canonicalPath || item.label}`}
                            label={item.label}
                            type="PRODUCT"
                            canonicalPath={item.canonicalPath}
                          />
                        ))}
                      </span>
                    </div>
                  ) : null}
                  {marketItems.length > 0 ? (
                    <div style={{ maxWidth: 640, fontSize: 13, lineHeight: 1.55, color: 'var(--text-secondary-strong)', fontWeight: 450 }}>
                      <span>Markets covered: </span>
                      <span style={{ display: 'inline-flex', flexWrap: 'wrap', justifyContent: 'flex-start', gap: 6, verticalAlign: 'middle' }}>
                        {marketItems.map((item) => (
                          <SummaryChip
                            key={`market-${item.canonicalPath || item.label}`}
                            label={item.label}
                            type="MARKET"
                            canonicalPath={item.canonicalPath}
                          />
                        ))}
                      </span>
                    </div>
                  ) : null}
                  {countryItems.length > 0 ? (
                    <div style={{ maxWidth: 640, fontSize: 13, lineHeight: 1.55, color: 'var(--text-secondary-strong)', fontWeight: 450 }}>
                      <span>Countries covered: </span>
                      <span style={{ display: 'inline-flex', flexWrap: 'wrap', justifyContent: 'flex-start', gap: 6, verticalAlign: 'middle' }}>
                        {countryItems.map((country) => (
                          <SummaryChip
                            key={`country-${country.code}`}
                            label={country.label}
                            type="LOCATION"
                            canonicalPath={`/loc/${country.code}`}
                          />
                        ))}
                      </span>
                    </div>
                  ) : null}
                </div>
              </div>
            ) : null}

            {showCommandPalette ? (
              <div style={{ width: '100%' }}>
                <AssembleLensRequestComponent
                  projectId={projectId!}
                  onAssemble={onAssemble!}
                  disabled={isCreatingLens}
                  pinnedBottom={false}
                  placeholderOverride={homeCommandPlaceholder}
                  hideProjectIndicator={true}
                  hideSuggestions={showHomeViewActions}
                />
              </div>
            ) : null}

            {showHomeViewActions ? (
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 10, flexWrap: 'wrap' }}>
                {onOpenGraph ? (
                  <button
                    type="button"
                    onClick={onOpenGraph}
                    style={{
                      height: 34,
                      padding: '0 14px',
                      borderRadius: 10,
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--white-06)',
                      color: 'var(--text-secondary-strong)',
                      fontSize: 12,
                      fontWeight: 700,
                      cursor: 'pointer',
                    }}
                  >
                    Graph View
                  </button>
                ) : null}
                {onOpenMap ? (
                  <button
                    type="button"
                    onClick={onOpenMap}
                    style={{
                      height: 34,
                      padding: '0 14px',
                      borderRadius: 10,
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--white-06)',
                      color: 'var(--text-secondary-strong)',
                      fontSize: 12,
                      fontWeight: 700,
                      cursor: 'pointer',
                    }}
                  >
                    View on Map
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : null}

        {/* Hidden temporarily for readability of the Home tab. Keep the sections in code for debug/operator work. */}
        {false ? (
          <>
            <CollapseSection
              title="Workscope reach"
              subtitle="How far the current materialized substrate already reaches."
              right={supplyPathCount > 0 ? <Pill text={`${supplyPathCount} supply paths`} tone="success" /> : undefined}
            >
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 10 }}>
                <div style={{ padding: '10px 12px', borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Artifacts</div>
                  <div style={{ marginTop: 4, fontSize: 16, fontWeight: 800, color: 'var(--text-heading)' }}>{artifactEntries.length}</div>
                </div>
                <div style={{ padding: '10px 12px', borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Entities reached</div>
                  <div style={{ marginTop: 4, fontSize: 16, fontWeight: 800, color: 'var(--text-heading)' }}>{uniqueNodeRefs.size}</div>
                </div>
                <div style={{ padding: '10px 12px', borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Flows</div>
                  <div style={{ marginTop: 4, fontSize: 16, fontWeight: 800, color: 'var(--text-heading)' }}>{uniqueFlowRefs.size}</div>
                </div>
                <div style={{ padding: '10px 12px', borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Zones</div>
                  <div style={{ marginTop: 4, fontSize: 16, fontWeight: 800, color: 'var(--text-heading)' }}>{uniqueZoneRefs.size}</div>
                </div>
              </div>
              {workscopeKinds.length > 0 ? (
                <div style={{ marginTop: 12, display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {workscopeKinds.slice(0, 6).map(([kind, count]) => (
                    <Pill key={kind} text={`${kind.replace(/_/g, ' ')} · ${count}`} tone="amber" />
                  ))}
                </div>
              ) : null}
            </CollapseSection>

            <CollapseSection
              title="Technical description"
              subtitle="Collapsed by default for debug and operator work."
            >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
              Contains scope editing, manual grounding, assembly details, raw configuration and recent scouting jobs.
            </div>

            <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
              <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                Scope editor
              </summary>
              <div style={{ padding: '12px', borderTop: '1px solid var(--border-subtle)' }}>
            {/* Edit controls (not scrollable) */}
            <div
              ref={scopeSuggestRef}
              style={{ position: 'relative' }}
              onBlurCapture={(e) => {
                // Close only when focus truly leaves this scope editor (input + dropdown).
                const next = (e.relatedTarget as globalThis.Node | null);
                if (next && (e.currentTarget as any)?.contains?.(next)) return;
                closeScopeSuggest();
              }}
            >
              <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 10 }}>
                <input
                  ref={scopeInputRef}
                  value={newScopePath}
                  onChange={(e) => {
                    setNewScopePath(e.target.value);
                    setScopeSuggestIndex(0);
                  }}
                  placeholder='Add path (e.g., "/org/acme") or type a name…'
                  style={{
                    flex: 1,
                    minWidth: 0,
                    padding: '10px 12px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    ...mono,
                    opacity: savingScope ? 0.7 : 1,
                  }}
                  disabled={!onUpdateScope || savingScope}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') {
                      e.preventDefault();
                      closeScopeSuggest();
                      return;
                    }
                    if (scopeSuggestOpen && scopeSuggestResults.length > 0) {
                      if (e.key === 'ArrowDown') {
                        e.preventDefault();
                        setScopeSuggestIndex((i) => Math.min(i + 1, Math.max(0, scopeSuggestResults.length - 1)));
                        return;
                      }
                      if (e.key === 'ArrowUp') {
                        e.preventDefault();
                        setScopeSuggestIndex((i) => Math.max(0, i - 1));
                        return;
                      }
                      if (e.key === 'Enter' && !newScopePath.trim().startsWith('/')) {
                        // In "search by name" mode: Enter picks a suggestion first.
                        e.preventDefault();
                        const picked = scopeSuggestResults[scopeSuggestIndex];
                        if (picked) pickScopeSuggestion(picked);
                        return;
                      }
                    }
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      void handleAddScope();
                    }
                  }}
                  onFocus={() => {
                    const q = (newScopePath || '').trim();
                    if (q && !q.startsWith('/')) setScopeSuggestOpen(true);
                  }}
                />
                <button
                  type="button"
                  onClick={() => void handleAddScope()}
                  disabled={!onUpdateScope || savingScope || !newScopePath.trim()}
                  style={{
                    padding: '10px 12px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--white-10)',
                    color: 'var(--text-heading)',
                    fontSize: 12,
                    fontWeight: 800,
                    cursor: !onUpdateScope || savingScope || !newScopePath.trim() ? 'not-allowed' : 'pointer',
                    opacity: !onUpdateScope || savingScope || !newScopePath.trim() ? 0.5 : 1,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {savingScope ? 'Saving…' : 'Add'}
                </button>
              </div>

              {/* Suggestions dropdown */}
              {scopeSuggestOpen && !newScopePath.trim().startsWith('/') && (
                <div
                  style={{
                    position: 'absolute',
                    top: 44,
                    left: 0,
                    right: 82,
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-elevated)',
                    boxShadow: '0 18px 60px rgba(0,0,0,0.35)',
                    overflow: 'hidden',
                    zIndex: 20,
                  }}
                >
                  <div style={{ maxHeight: 240, overflowY: 'auto' }}>
                    {scopeSuggestLoading && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>Searching…</div>
                    )}
                    {scopeSuggestError && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: '#f87171' }}>{scopeSuggestError}</div>
                    )}
                    {!scopeSuggestLoading && !scopeSuggestError && newScopePath.trim().length < 2 && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>Type 2+ characters to search</div>
                    )}
                    {!scopeSuggestLoading && !scopeSuggestError && newScopePath.trim().length >= 2 && scopeSuggestResults.length === 0 && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>No matches</div>
                    )}
                    {!scopeSuggestLoading && !scopeSuggestError && scopeSuggestResults.map((r, idx) => {
                      const active = idx === scopeSuggestIndex;
                      return (
                        <button
                          key={`${r.id}-${idx}`}
                          type="button"
                          onClick={() => pickScopeSuggestion(r)}
                          onMouseEnter={() => setScopeSuggestIndex(idx)}
                          style={{
                            width: '100%',
                            textAlign: 'left',
                            padding: '10px 12px',
                            border: 'none',
                            background: active ? 'var(--white-10)' : 'transparent',
                            cursor: 'pointer',
                            display: 'flex',
                            flexDirection: 'column',
                            gap: 3,
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                            <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {r.name}
                            </div>
                            {(() => {
                              const t = String(r.type || '').trim();
                              const isNonFragment = /^(supply-chain|domain|intent|artifact|SUPPLY_CHAIN|GOVERNANCE|REGULATORY|ECOSYSTEM)/i.test(t) || (r.canonicalPath || '').startsWith('/domain/') || (r.canonicalPath || '').startsWith('/artifact/');
                              return <Pill text={t ? t.toUpperCase() : '—'} tone={isNonFragment ? 'amber' : 'neutral'} />;
                            })()}
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {r.canonicalPath}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>

          {scopeError && (
            <div style={{ marginBottom: 10, fontSize: 12, color: '#f87171' }}>
              {scopeError}
            </div>
          )}

          {totalObjects > 0 ? (
            <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden', maxHeight: 320 }}>
              <div style={{ maxHeight: 320, overflowY: 'auto' }}>
                {intents.map((entry: { id?: string; question?: string; status?: string }, idx: number) => {
                  const q = (entry?.question || '').trim();
                  const status = (entry?.status || '').trim();
                  return (
                    <div key={`intent-${entry?.id || idx}`} style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                        <Pill text="INTENT" tone={status === 'answered' ? 'success' : 'neutral'} />
                        <div style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                          {q || '—'}
                        </div>
                      </div>
                    </div>
                  );
                })}
                {domains.map((d: { canonicalPath?: string; contractType?: string; title?: string; description?: string; supplyChainExecutionPlan?: { graphBuildMode?: string } }, idx: number) => {
                  const path = (d?.canonicalPath || '').trim();
                  const ct = (d?.contractType || '').trim();
                  const title = (d?.title || d?.description || '').trim();
                  const regionLabel = getDomainRegionLabel(path);
                  const graphBuildModeLabel = isSupplyChainContractType(ct) ? getSupplyChainGraphBuildModeLabel(d?.supplyChainExecutionPlan?.graphBuildMode) : '';
                  return (
                    <div key={`domain-${path || idx}`} style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                        <Pill text={ct || 'DOMAIN'} tone="amber" />
                        {regionLabel ? <Pill text={regionLabel} tone="amber" /> : null}
                        {graphBuildModeLabel ? <Pill text={graphBuildModeLabel} tone="success" /> : null}
                        <div style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {title || humanizeCanonicalPath(path) || '—'}
                        </div>
                      </div>
                      {path ? (
                        <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={path}>
                          {path}
                        </div>
                      ) : null}
                    </div>
                  );
                })}
                {domainModesOnly && domainModes.map((mode: string, idx: number) => (
                  <div key={`domainMode-${idx}`} style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <Pill text="DOMAIN_MODE" />
                      <div style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)' }}>
                        {mode}
                      </div>
                    </div>
                  </div>
                ))}
                {scope.map((entry: string, index: number) => {
                  const path = (entry || '').trim();
                  const isQuery = path.startsWith('/query/');
                  const isArtifact = path.startsWith('/artifact/');
                  const isDomain = path.startsWith('/domain/');
                  const resolved = scopeResolved[path];
                  const resolvedList = isQuery ? (resolvedQueries?.[path] || []) : [];

                  const removeBtnStyle = {
                    padding: '6px 10px',
                    borderRadius: 10,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    fontSize: 12,
                    fontWeight: 700,
                    cursor: !onUpdateScope || savingScope ? 'not-allowed' : 'pointer',
                    opacity: !onUpdateScope || savingScope ? 0.5 : 1,
                  } as const;

                  if (isDomain) {
                    const segments = path.split('/').filter(Boolean);
                    const contractType = segments.length >= 3 ? segments[2] : 'domain';
                    const domainName = humanizeCanonicalPath(path) || contractType;
                    const matchedDomain = domains.find((d: any) => (d?.canonicalPath || '').trim() === path);
                    const displayTitle = matchedDomain?.title || matchedDomain?.description || domainName;
                    const regionLabel = getDomainRegionLabel(path);
                    const graphBuildModeLabel = isSupplyChainContractType(contractType) ? getSupplyChainGraphBuildModeLabel(matchedDomain?.supplyChainExecutionPlan?.graphBuildMode) : '';
                    return (
                      <div key={`scope-${path}-${index}`} style={{ padding: '10px 12px', background: 'transparent', borderBottom: '1px solid var(--border-subtle)' }}>
                        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                              <Pill text={contractType} tone="amber" />
                              {regionLabel ? <Pill text={regionLabel} tone="amber" /> : null}
                              {graphBuildModeLabel ? <Pill text={graphBuildModeLabel} tone="success" /> : null}
                              <div style={{ minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {displayTitle}
                              </div>
                            </div>
                            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={path}>
                              {path}
                            </div>
                          </div>
                          <button type="button" onClick={() => void handleRemoveScope(path)} disabled={!onUpdateScope || savingScope} style={removeBtnStyle} title="Remove">Remove</button>
                        </div>
                      </div>
                    );
                  }

                  if (isArtifact) {
                    const artKey = path.replace(/^\/artifact\//, '');
                    const artMeta = scopeArtifacts[artKey];
                    const kindLabel = artMeta?.kind
                      ? artMeta.kind.replace(/_/g, ' ').replace(/\b\w/g, (c: string) => c.toUpperCase())
                      : 'Supply Chain';
                    const fullChain = String(artMeta?.kind || '').trim().toUpperCase() === 'SUPPLY_CHAIN_PATH';
                    const anchorLine = [artMeta?.anchorSupplier, artMeta?.anchorCustomer].filter(Boolean).join(' → ');
                    const metricsLine = [
                      typeof artMeta?.legCount === 'number' && artMeta.legCount > 0 ? `${artMeta.legCount} legs` : '',
                      typeof artMeta?.bindingConfidence === 'number' && artMeta.bindingConfidence > 0
                        ? `bind ${Math.round(artMeta.bindingConfidence * 100)}%`
                        : '',
                      typeof artMeta?.pathStressScore === 'number' && artMeta.pathStressScore > 0
                        ? `stress ${artMeta.pathStressScore.toFixed(2)}`
                        : '',
                    ].filter(Boolean).join(' • ');
                    const footprintLine = [
                      artMeta?.flowRefs && artMeta.flowRefs.length > 0 ? `${artMeta.flowRefs.length} flows` : '',
                      artMeta?.zoneRefs && artMeta.zoneRefs.length > 0 ? `${artMeta.zoneRefs.length} zones` : '',
                      artMeta?.affectedZoneRefs && artMeta.affectedZoneRefs.length > 0 ? `${artMeta.affectedZoneRefs.length} affected` : '',
                    ].filter(Boolean).join(' • ');
                    return (
                      <div key={`scope-${path}-${index}`} style={{ padding: '10px 12px', background: 'transparent', borderBottom: '1px solid var(--border-subtle)' }}>
                        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                              <Pill text={kindLabel} tone="amber" />
                              {fullChain ? <Pill text="Full chain" tone="success" /> : null}
                              <div style={{ minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {artMeta?.displayName || humanizeCanonicalPath(path) || '—'}
                              </div>
                              {artMeta?.status && <Pill text={artMeta.status} tone={artMeta.status === 'validated' ? 'success' : 'neutral'} />}
                            </div>
                            {anchorLine ? (
                              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary-strong)' }}>
                                {anchorLine}
                                {artMeta?.productAxis ? ` • ${artMeta.productAxis}` : ''}
                              </div>
                            ) : artMeta?.productAxis ? (
                              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary-strong)' }}>
                                {artMeta.productAxis}
                              </div>
                            ) : null}
                            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={path}>
                              {path}
                            </div>
                            {artMeta?.nodeRefs && artMeta.nodeRefs.length > 0 && (
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                {artMeta.nodeRefs.length} entities in chain
                              </div>
                            )}
                            {metricsLine && (
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                {metricsLine}
                              </div>
                            )}
                            {footprintLine && (
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                {footprintLine}
                              </div>
                            )}
                            {artMeta?.spatialSemantics && artMeta.spatialSemantics.length > 0 && (
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                {artMeta.spatialSemantics.slice(0, 3).join(' • ')}
                              </div>
                            )}
                          </div>
                          <button type="button" onClick={() => void handleRemoveScope(path)} disabled={!onUpdateScope || savingScope} style={removeBtnStyle} title="Remove">Remove</button>
                        </div>
                      </div>
                    );
                  }

                  const scopeGeoLabel = !isQuery && !path.startsWith('/domain/') ? getScopeGeoLabel(path) : null;
                  return (
                    <div key={`scope-${path}-${index}`} style={{ padding: '10px 12px', background: 'transparent', borderBottom: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                        <div style={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                            {isQuery ? <Pill text="Query" /> : resolved?.type ? <Pill text={String(resolved.type)} /> : null}
                            {scopeGeoLabel ? <Pill text={scopeGeoLabel} tone="amber" /> : null}
                            <div style={{ minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {!isQuery ? (resolved?.name ? resolved.name : humanizeCanonicalPath(path) || '—') : '—'}
                            </div>
                            {isQuery && resolvedList.length > 0 && (
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', flexShrink: 0 }}>{resolvedList.length} resolved</div>
                            )}
                          </div>
                          <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={path}>
                            {path}
                          </div>
                        </div>
                        <button type="button" onClick={() => void handleRemoveScope(path)} disabled={!onUpdateScope || savingScope} style={removeBtnStyle} title="Remove">Remove</button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : (
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>No objects defined</div>
          )}
              </div>
            </details>

            <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
              <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                Live signals
              </summary>
              <div style={{ padding: '12px', borderTop: '1px solid var(--border-subtle)' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 8 }}>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    Fetch recent news and ontology facts for current scope.
                  </div>
                  <button
                    type="button"
                    onClick={() => void handleLoadNews()}
                    disabled={newsLoading}
                    style={{
                      padding: '8px 10px',
                      borderRadius: 12,
                      border: '1px solid var(--border-subtle)',
                      background: 'transparent',
                      color: 'var(--text-heading)',
                      fontSize: 12,
                      fontWeight: 800,
                      cursor: newsLoading ? 'not-allowed' : 'pointer',
                      opacity: newsLoading ? 0.55 : 1,
                    }}
                  >
                    {newsLoading ? 'Loading…' : 'Load news for scope'}
                  </button>
                </div>
                {newsError ? <div style={{ fontSize: 12, color: '#f87171' }}>{newsError}</div> : null}
              </div>
            </details>

            {assemblyStatus ? (
              <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
                <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                  Assembly status
                </summary>
                <div style={{ padding: '12px', borderTop: '1px solid var(--border-subtle)' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Stage</div>
                  <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                    {String((assemblyStatus as any)?.stage || assemblyStatus?.phase || '—')}
                  </div>
                </div>
                {assemblyStatus?.progress !== undefined && (
                  <div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, marginBottom: 6 }}>
                      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Progress</div>
                      <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                        {Math.round(Number(assemblyStatus.progress) * 100)}%
                      </div>
                    </div>
                    <div style={{ height: 8, borderRadius: 999, overflow: 'hidden', background: 'var(--white-10)' }}>
                      <div style={{ width: `${Math.max(0, Math.min(1, Number(assemblyStatus.progress))) * 100}%`, height: '100%', background: 'var(--accent-primary, #3B82F6)' }} />
                    </div>
                  </div>
                )}
                {((assemblyStatus as any)?.stages?.scouting || assemblyStatus?.phases?.scouting) && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, padding: '8px 10px', borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Scouting</div>
                    <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 750 }}>
                      {((assemblyStatus as any)?.stages?.scouting?.completed ?? assemblyStatus.phases.scouting.completed)} / {((assemblyStatus as any)?.stages?.scouting?.total ?? assemblyStatus.phases.scouting.total)}
                    </div>
                  </div>
                )}
              </div>
                </div>
              </details>
            ) : null}

            {(lens?.viewConfig && Object.keys(lens.viewConfig).length > 0) || (lens?.alertConfig && Object.keys(lens.alertConfig).length > 0) ? (
              <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
                <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                  Configuration
                </summary>
                <div style={{ padding: '12px', borderTop: '1px solid var(--border-subtle)' }}>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10, maxHeight: 240, overflowY: 'auto' }}>
                {lens?.viewConfig && Object.keys(lens.viewConfig).length > 0 && (
                  <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
                    <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                      View configuration
                    </summary>
                    <div style={{ padding: '10px 12px', borderTop: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                      <pre style={{ margin: 0, fontSize: 11, color: 'var(--text-heading)', overflow: 'auto', ...mono }}>
                        {JSON.stringify(lens.viewConfig, null, 2)}
                      </pre>
                    </div>
                  </details>
                )}
                {lens?.alertConfig && Object.keys(lens.alertConfig).length > 0 && (
                  <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
                    <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                      Alert configuration
                    </summary>
                    <div style={{ padding: '10px 12px', borderTop: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                      <pre style={{ margin: 0, fontSize: 11, color: 'var(--text-heading)', overflow: 'auto', ...mono }}>
                        {JSON.stringify(lens.alertConfig, null, 2)}
                      </pre>
                    </div>
                  </details>
                )}
                  </div>
                </div>
              </details>
            ) : null}

            <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
              <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                Recent scouting jobs ({scoutingJobs.length})
              </summary>
              <div style={{ padding: '12px', borderTop: '1px solid var(--border-subtle)' }}>
                <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 10 }}>
                  <button
                    type="button"
                    onClick={onRefreshJobs}
                    disabled={loadingJobs}
                    style={{
                      padding: '8px 10px',
                      borderRadius: 12,
                      border: '1px solid var(--border-subtle)',
                      background: 'transparent',
                      color: 'var(--text-heading)',
                      fontSize: 12,
                      fontWeight: 800,
                      cursor: loadingJobs ? 'not-allowed' : 'pointer',
                      opacity: loadingJobs ? 0.55 : 1,
                    }}
                  >
                    {loadingJobs ? 'Loading…' : 'Refresh'}
                  </button>
                </div>
            {scoutingJobs.length > 0 ? (
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden', maxHeight: 260 }}>
                <div style={{ maxHeight: 260, overflowY: 'auto' }}>
                  {recentJobs.map((job: any, idx: number) => {
                    const status = String(job?.status || '').toUpperCase();
                    const tone = status === 'VERIFIED' || status === 'COMPLETED' ? 'success' : (status === 'FAILED' || status === 'ERROR' ? 'danger' : 'neutral');
                    const showDivider = idx !== recentJobs.length - 1;
                    return (
                      <div key={job.id} style={{ padding: '10px 12px', borderBottom: showDivider ? '1px solid var(--border-subtle)' : 'none' }}>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                            <Pill text={status || 'UNKNOWN'} tone={tone as any} />
                            <div style={{ minWidth: 0 }}>
                              <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 700, ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={job.targetCanonicalPath}>
                                {job.targetCanonicalPath}
                              </div>
                            </div>
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-tertiary-weak)', flexShrink: 0 }}>
                            {job?.createdAt ? new Date(job.createdAt).toLocaleDateString() : '—'}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ) : (
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>No scouting jobs found</div>
            )}
              </div>
            </details>
          </div>
            </CollapseSection>
          </>
        ) : null}
      </div>
    </div>
  );
};
