import React from 'react';
import {
  api,
  type HorizonSimulationOverlayFilters,
  type HorizonSimulationOverlayItem,
  type HorizonSimulationResultOverlays,
  type HorizonSimulationRoleOverlay,
  type HorizonSimulationRunSummary,
} from '../lib/api';
import type { MaterializedArtifact } from './SupplyChainOverlay';

type OverlayKind = 'shoulders' | 'products' | 'pathChains' | 'cascades' | 'roles';

const OVERLAY_KINDS: Array<{ key: OverlayKind; label: string }> = [
  { key: 'shoulders', label: 'Shoulders' },
  { key: 'products', label: 'Products' },
  { key: 'pathChains', label: 'Chains' },
  { key: 'cascades', label: 'Cascades' },
  { key: 'roles', label: 'Roles' },
];

const emptyOverlays = (): HorizonSimulationResultOverlays => ({
  shoulders: [],
  products: [],
  pathChains: [],
  cascades: [],
  roles: [],
});

const parseCsv = (value: string): string[] =>
  value
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean);

const normalizeCountryFromPath = (value: string): string => {
  const match = String(value || '').trim().toLowerCase().match(/^\/(?:fac|org|loc|gov|deposit|market)\/([a-z]{2})(?:\/|$)/);
  return match?.[1] || '';
};

const routeProductPaths = (artifact: MaterializedArtifact): string[] => {
  const set = new Set<string>();
  const direct = String(artifact.productAxis || '').trim();
  if (direct.startsWith('/product/')) set.add(direct);
  const productNodes = Array.isArray(artifact.payload?.productNodes) ? artifact.payload!.productNodes! : [];
  for (const node of productNodes) {
    const ref = String(node?.ref || '').trim();
    if (ref.startsWith('/product/')) set.add(ref);
  }
  const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload!.legs! : [];
  for (const leg of legs) {
    const ref = String(leg?.productRef || '').trim();
    if (ref.startsWith('/product/')) set.add(ref);
  }
  return Array.from(set);
};

const routeRefs = (artifact: MaterializedArtifact): string[] => {
  const set = new Set<string>();
  const push = (value: unknown) => {
    const ref = String(value || '').trim();
    if (ref) set.add(ref);
  };
  push(artifact.anchorSupplier);
  push(artifact.anchorCustomer);
  for (const ref of artifact.nodeRefs || []) push(ref);
  for (const ref of artifact.flowRefs || []) push(ref);
  for (const ref of artifact.zoneRefs || []) push(ref);
  for (const ref of artifact.affectedZoneRefs || []) push(ref);
  const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload!.legs! : [];
  for (const leg of legs) {
    push(leg?.supplierRef);
    push(leg?.customerRef);
    push(leg?.productRef);
    for (const ref of leg?.transportViaRefs || []) push(ref);
    for (const ref of leg?.flowRefs || []) push(ref);
    for (const ref of leg?.zoneRefs || []) push(ref);
    for (const ref of leg?.affectedZoneRefs || []) push(ref);
  }
  for (const ref of routeProductPaths(artifact)) push(ref);
  return Array.from(set);
};

const routeCountryCodes = (artifact: MaterializedArtifact): string[] => {
  const codes = new Set<string>();
  for (const ref of routeRefs(artifact)) {
    const code = normalizeCountryFromPath(ref);
    if (code) codes.add(code);
  }
  return Array.from(codes);
};

const formatDate = (value?: string): string => {
  const ts = Date.parse(String(value || ''));
  if (!Number.isFinite(ts)) return 'unknown';
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(ts));
};

const formatPriorityLabel = (value?: string): string => {
  const label = String(value || '').trim().toLowerCase();
  if (!label) return 'Watch';
  return label.charAt(0).toUpperCase() + label.slice(1);
};

type RouteMatcher = {
  artifact: MaterializedArtifact;
  refs: Set<string>;
  countries: Set<string>;
  products: Set<string>;
};

type MatchedArtifactSelection = {
  artifactId: string;
  status: 'validated' | 'hypothesis';
};

type OverlayMatchMeta = {
  matchedArtifactIds: string[];
  validatedArtifactIds: string[];
  hypothesisArtifactIds: string[];
};

const inferArtifactPathStatus = (artifact: MaterializedArtifact): 'validated' | 'hypothesis' => {
  const explicit = String(artifact.pathStatus || '').trim().toLowerCase();
  if (explicit === 'hypothesis') return 'hypothesis';
  if (explicit === 'validated') return 'validated';
  if (artifact.isConfirmedPath) return 'validated';
  const sufficiency = String(artifact.sufficiencyStatus || artifact.payload?.sufficiency?.status || '').trim().toLowerCase();
  if (sufficiency === 'validated') return 'validated';
  return 'hypothesis';
};

const overlayMatchCount = (item: HorizonSimulationOverlayItem, matcher: RouteMatcher): number => {
  let score = 0;
  const targetRefs = Array.isArray(item.targetRefs)
    ? item.targetRefs.map((ref) => String(ref || '').trim()).filter(Boolean)
    : [];
  for (const ref of targetRefs) {
    if (matcher.refs.has(ref)) score += 3;
  }
  const countryCode = String(item.targetCountryCode || '').trim().toLowerCase();
  if (countryCode && matcher.countries.has(countryCode)) score += 2;
  const productPath = String(item.productPath || '').trim();
  if (productPath && matcher.products.has(productPath)) score += 3;
  if (String(item.kind || '').trim() === 'path_chain' && targetRefs.length > 0) {
    const facilityHits = targetRefs.filter((ref) => ref.startsWith('/fac/') && matcher.refs.has(ref)).length;
    const productHits = targetRefs.filter((ref) => ref.startsWith('/product/') && matcher.products.has(ref)).length;
    if (facilityHits > 0 && productHits > 0) score += 4;
  }
  return score;
};

const matchedArtifactsForOverlay = (item: HorizonSimulationOverlayItem, routeMatchers: RouteMatcher[]): MatchedArtifactSelection[] => {
  const ranked = routeMatchers
    .map((matcher) => {
      const directWeight = overlayMatchCount(item, matcher);
      const stressWeight = typeof matcher.artifact.pathStressScore === 'number' ? matcher.artifact.pathStressScore : 0;
      return {
        artifactId: String(matcher.artifact.id || '').trim(),
        status: inferArtifactPathStatus(matcher.artifact),
        directWeight,
        stressWeight,
      };
    })
    .filter((entry) => entry.artifactId && entry.directWeight > 0)
    .sort((a, b) => b.directWeight - a.directWeight || b.stressWeight - a.stressWeight || a.artifactId.localeCompare(b.artifactId));

  if (ranked.length === 0) return [];

  const bestDirectWeight = ranked[0]?.directWeight || 0;
  const kind = String(item.kind || '').trim();
  const cap =
    kind === 'path_chain' || kind === 'derived_cascade'
      ? 6
      : kind === 'regional_shoulder' || kind === 'product_family'
        ? 8
        : 8;
  return ranked
    .filter((entry) => entry.directWeight === bestDirectWeight)
    .slice(0, cap)
    .map((entry) => ({
      artifactId: entry.artifactId,
      status: entry.status,
    }));
};

interface SimulationMapOverlayPanelProps {
  lensId: string | null;
  routeCatalog: MaterializedArtifact[];
  selectedRouteArtifactIds: string[];
  onChangeSelectedRouteArtifactIds: (ids: string[]) => void;
  onSelectionChange?: (selection: SimulationMapOverlaySelection) => void;
  isDark: boolean;
}

export interface SimulationMapOverlaySelection {
  lensId: string | null;
  runId: string;
  activeOverlayIds: string[];
  activeItems: HorizonSimulationOverlayItem[];
  matchedArtifactIds: string[];
  validatedArtifactIds: string[];
  hypothesisArtifactIds: string[];
  matchedArtifactIdsByOverlay: Record<string, string[]>;
  validatedArtifactIdsByOverlay: Record<string, string[]>;
  hypothesisArtifactIdsByOverlay: Record<string, string[]>;
  targetRefs: string[];
  targetCountryCodes: string[];
  productPaths: string[];
}

export function SimulationMapOverlayPanel({
  lensId,
  routeCatalog,
  selectedRouteArtifactIds,
  onChangeSelectedRouteArtifactIds,
  onSelectionChange,
  isDark,
}: SimulationMapOverlayPanelProps) {
  const [runs, setRuns] = React.useState<HorizonSimulationRunSummary[]>([]);
  const [selectedRunId, setSelectedRunId] = React.useState('');
  const [loadingRun, setLoadingRun] = React.useState(false);
  const [loadingOverlays, setLoadingOverlays] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = React.useState(0);
  const [query, setQuery] = React.useState('');
  const [functionsInput, setFunctionsInput] = React.useState('');
  const [disciplinesInput, setDisciplinesInput] = React.useState('');
  const [rolesInput, setRolesInput] = React.useState('');
  const [categoriesInput, setCategoriesInput] = React.useState('');
  const [minScore, setMinScore] = React.useState(0.35);
  const [mappedOnly, setMappedOnly] = React.useState(true);
  const [kindEnabled, setKindEnabled] = React.useState<Record<OverlayKind, boolean>>({
    shoulders: true,
    products: true,
    pathChains: true,
    cascades: true,
    roles: false,
  });
  const [activeOverlayIds, setActiveOverlayIds] = React.useState<string[]>([]);
  const [rawOverlays, setRawOverlays] = React.useState<HorizonSimulationResultOverlays>(emptyOverlays);

  const deferredQuery = React.useDeferredValue(query);
  const deferredFunctionsInput = React.useDeferredValue(functionsInput);
  const deferredDisciplinesInput = React.useDeferredValue(disciplinesInput);
  const deferredRolesInput = React.useDeferredValue(rolesInput);
  const deferredCategoriesInput = React.useDeferredValue(categoriesInput);

  const routeMatchers = React.useMemo<RouteMatcher[]>(
    () =>
      routeCatalog.map((artifact) => ({
        artifact,
        refs: new Set(routeRefs(artifact)),
        countries: new Set(routeCountryCodes(artifact)),
        products: new Set(routeProductPaths(artifact)),
      })),
    [routeCatalog],
  );

  const selectedRun = React.useMemo(
    () => runs.find((run) => String(run.id || '').trim() === selectedRunId) || null,
    [runs, selectedRunId],
  );

  React.useEffect(() => {
    if (!lensId) return;
    let cancelled = false;
    setLoadingRun(true);
    setError(null);
    api
      .listHorizonSimulationRunsForLens(lensId, 50)
      .then((response) => {
        if (cancelled) return;
        const nextRuns = Array.isArray(response.runs) ? response.runs : [];
        setRuns(nextRuns);
        setSelectedRunId((current) => {
          const currentId = String(current || '').trim();
          if (currentId && nextRuns.some((run) => String(run.id || '').trim() === currentId)) return currentId;
          return String(nextRuns[0]?.id || '').trim();
        });
      })
      .catch((err: any) => {
        if (cancelled) return;
        setError(err?.message || 'Failed to load simulation runs');
        setRuns([]);
        setSelectedRunId('');
      })
      .finally(() => {
        if (!cancelled) setLoadingRun(false);
      });
    return () => {
      cancelled = true;
    };
  }, [lensId, refreshNonce]);

  const backendFilters = React.useMemo<HorizonSimulationOverlayFilters>(() => ({
    kinds: OVERLAY_KINDS.filter(({ key }) => kindEnabled[key]).map(({ key }) => key),
    functions: parseCsv(deferredFunctionsInput),
    disciplines: parseCsv(deferredDisciplinesInput),
    roles: parseCsv(deferredRolesInput),
    categories: parseCsv(deferredCategoriesInput),
    q: deferredQuery.trim(),
    minScore,
  }), [
    deferredCategoriesInput,
    deferredDisciplinesInput,
    deferredFunctionsInput,
    deferredQuery,
    deferredRolesInput,
    kindEnabled,
    minScore,
  ]);

  React.useEffect(() => {
    if (!selectedRunId) {
      setRawOverlays(emptyOverlays());
      return;
    }
    let cancelled = false;
    setLoadingOverlays(true);
    setError(null);
    api
      .getHorizonSimulationOverlays(selectedRunId, backendFilters)
      .then((response) => {
        if (cancelled) return;
        setRawOverlays(response.overlays || emptyOverlays());
      })
      .catch((err: any) => {
        if (cancelled) return;
        setError(err?.message || 'Failed to load simulation overlays');
        setRawOverlays(emptyOverlays());
      })
      .finally(() => {
        if (!cancelled) setLoadingOverlays(false);
      });
    return () => {
      cancelled = true;
    };
  }, [backendFilters, selectedRunId]);

  const matchMap = React.useMemo(() => {
    const byOverlayId = new Map<string, OverlayMatchMeta>();
    const overlayLists: HorizonSimulationOverlayItem[] = [
      ...rawOverlays.shoulders,
      ...rawOverlays.products,
      ...rawOverlays.pathChains,
      ...rawOverlays.cascades,
    ];
    for (const item of overlayLists) {
      const overlayId = String(item.id || '').trim();
      if (!overlayId) continue;
      const matches = matchedArtifactsForOverlay(item, routeMatchers);
      byOverlayId.set(overlayId, {
        matchedArtifactIds: matches.map((entry) => entry.artifactId),
        validatedArtifactIds: matches.filter((entry) => entry.status === 'validated').map((entry) => entry.artifactId),
        hypothesisArtifactIds: matches.filter((entry) => entry.status === 'hypothesis').map((entry) => entry.artifactId),
      });
    }
    return byOverlayId;
  }, [rawOverlays, routeMatchers]);

  const filteredOverlays = React.useMemo<HorizonSimulationResultOverlays>(() => {
    const filterItems = (items: HorizonSimulationOverlayItem[]) => {
      const base = mappedOnly
        ? items.filter((item) => (matchMap.get(String(item.id || '').trim())?.matchedArtifactIds || []).length > 0)
        : items;
      return base;
    };
    return {
      shoulders: filterItems(rawOverlays.shoulders),
      products: filterItems(rawOverlays.products),
      pathChains: filterItems(rawOverlays.pathChains),
      cascades: filterItems(rawOverlays.cascades),
      roles: rawOverlays.roles.filter((role) => {
        if (!mappedOnly) return true;
        const targets = role.targets || {};
        const allTargets = [
          ...(targets.shoulders || []),
          ...(targets.products || []),
          ...(targets.pathChains || []),
          ...(targets.cascades || []),
        ];
        return allTargets.some((target) => (matchMap.get(String(target || '').trim())?.matchedArtifactIds || []).length > 0);
      }),
    };
  }, [mappedOnly, matchMap, rawOverlays]);

  const selectedRouteArtifactIdSet = React.useMemo(
    () => new Set(selectedRouteArtifactIds.map((value) => String(value || '').trim()).filter(Boolean)),
    [selectedRouteArtifactIds],
  );

  React.useEffect(() => {
    const next = new Set<string>();
    for (const overlayId of activeOverlayIds) {
      for (const artifactId of matchMap.get(String(overlayId || '').trim())?.matchedArtifactIds || []) {
        next.add(artifactId);
      }
    }
    const nextIds = Array.from(next);
    const currentIds = Array.from(selectedRouteArtifactIdSet);
    const same =
      nextIds.length === currentIds.length
      && nextIds.every((value) => selectedRouteArtifactIdSet.has(value));
    if (!same) onChangeSelectedRouteArtifactIds(nextIds);
  }, [activeOverlayIds, matchMap, onChangeSelectedRouteArtifactIds, selectedRouteArtifactIdSet]);

  const toggleOverlay = React.useCallback((overlayId: string) => {
    const id = String(overlayId || '').trim();
    if (!id) return;
    React.startTransition(() => {
      setActiveOverlayIds((current) => {
        const currentSet = new Set(current.map((value) => String(value || '').trim()).filter(Boolean));
        if (currentSet.has(id)) currentSet.delete(id);
        else currentSet.add(id);
        return Array.from(currentSet);
      });
    });
  }, []);

  const activeOverlayItems = React.useMemo(() => {
    const byId = new Map<string, HorizonSimulationOverlayItem>();
    for (const item of [
      ...filteredOverlays.shoulders,
      ...filteredOverlays.products,
      ...filteredOverlays.pathChains,
      ...filteredOverlays.cascades,
    ]) {
      const id = String(item.id || '').trim();
      if (!id) continue;
      byId.set(id, item);
    }
    return activeOverlayIds
      .map((id) => byId.get(String(id || '').trim()) || null)
      .filter((item): item is HorizonSimulationOverlayItem => Boolean(item));
  }, [activeOverlayIds, filteredOverlays]);

  const activeSelection = React.useMemo<SimulationMapOverlaySelection>(() => {
    const matchedArtifactSet = new Set<string>();
    const validatedArtifactSet = new Set<string>();
    const hypothesisArtifactSet = new Set<string>();
    const targetRefSet = new Set<string>();
    const targetCountrySet = new Set<string>();
    const productPathSet = new Set<string>();
    for (const item of activeOverlayItems) {
      const overlayId = String(item.id || '').trim();
      const matchMeta = matchMap.get(overlayId);
      for (const artifactId of matchMeta?.matchedArtifactIds || []) {
        if (artifactId) matchedArtifactSet.add(artifactId);
      }
      for (const artifactId of matchMeta?.validatedArtifactIds || []) {
        if (artifactId) validatedArtifactSet.add(artifactId);
      }
      for (const artifactId of matchMeta?.hypothesisArtifactIds || []) {
        if (artifactId) hypothesisArtifactSet.add(artifactId);
      }
      for (const ref of item.targetRefs || []) {
        const trimmed = String(ref || '').trim();
        if (trimmed) targetRefSet.add(trimmed);
      }
      const countryCode = String(item.targetCountryCode || '').trim().toLowerCase();
      if (countryCode) targetCountrySet.add(countryCode);
      const productPath = String(item.productPath || '').trim();
      if (productPath) productPathSet.add(productPath);
    }
    return {
      lensId: lensId || null,
      runId: selectedRunId,
      activeOverlayIds: [...activeOverlayIds],
      activeItems: activeOverlayItems,
      matchedArtifactIds: Array.from(matchedArtifactSet),
      validatedArtifactIds: Array.from(validatedArtifactSet),
      hypothesisArtifactIds: Array.from(hypothesisArtifactSet),
      matchedArtifactIdsByOverlay: Object.fromEntries(
        activeOverlayItems.map((item) => {
          const overlayId = String(item.id || '').trim();
          return [overlayId, overlayId ? [...(matchMap.get(overlayId)?.matchedArtifactIds || [])] : []];
        }),
      ),
      validatedArtifactIdsByOverlay: Object.fromEntries(
        activeOverlayItems.map((item) => {
          const overlayId = String(item.id || '').trim();
          return [overlayId, overlayId ? [...(matchMap.get(overlayId)?.validatedArtifactIds || [])] : []];
        }),
      ),
      hypothesisArtifactIdsByOverlay: Object.fromEntries(
        activeOverlayItems.map((item) => {
          const overlayId = String(item.id || '').trim();
          return [overlayId, overlayId ? [...(matchMap.get(overlayId)?.hypothesisArtifactIds || [])] : []];
        }),
      ),
      targetRefs: Array.from(targetRefSet),
      targetCountryCodes: Array.from(targetCountrySet),
      productPaths: Array.from(productPathSet),
    };
  }, [activeOverlayIds, activeOverlayItems, lensId, matchMap, selectedRunId]);

  React.useEffect(() => {
    onSelectionChange?.(activeSelection);
  }, [activeSelection, onSelectionChange]);

  const shellButtonStyle = (accent = false): React.CSSProperties => ({
    borderRadius: 12,
    border: accent
      ? (isDark ? '1px solid rgba(102,153,153,0.28)' : '1px solid rgba(102,153,153,0.22)')
      : '1px solid var(--border-subtle)',
    background: accent
      ? (isDark ? 'rgba(102,153,153,0.12)' : 'rgba(102,153,153,0.08)')
      : 'rgba(255,255,255,0.02)',
    color: 'var(--text-heading)',
    padding: '8px 10px',
    fontSize: 11,
    fontWeight: 700,
    cursor: 'pointer',
  });

  const cardStyle: React.CSSProperties = {
    borderRadius: 14,
    border: '1px solid var(--border-subtle)',
    background: isDark ? 'rgba(255,255,255,0.024)' : 'rgba(15,23,42,0.024)',
    padding: '11px 12px',
    display: 'flex',
    flexDirection: 'column',
    gap: 7,
  };

  const renderOverlayItems = (title: string, items: HorizonSimulationOverlayItem[]) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10 }}>
        <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>{title}</div>
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{items.length}</div>
      </div>
      {items.length === 0 ? (
        <div style={{ ...cardStyle, fontSize: 12, color: 'var(--text-secondary-strong)' }}>No matching overlays.</div>
      ) : (
        items.map((item, index) => {
          const overlayId = String(item.id || `${title}-${index}`).trim();
          const matchMeta = matchMap.get(overlayId) || { matchedArtifactIds: [], validatedArtifactIds: [], hypothesisArtifactIds: [] };
          const matchedArtifacts = matchMeta.matchedArtifactIds;
          const active = activeOverlayIds.includes(overlayId);
          const activeMatchCount = matchedArtifacts.filter((artifactId) => selectedRouteArtifactIdSet.has(artifactId)).length;
          const validatedVisibleCount = matchMeta.validatedArtifactIds.length;
          const hypothesisVisibleCount = matchMeta.hypothesisArtifactIds.length;
          const supportingPathCount = Number.isFinite(Number(item.supportingPathCount)) ? Number(item.supportingPathCount) : 0;
          const visibleRouteCount = matchedArtifacts.length;
          const priorityScore = typeof item.priorityScore === 'number' ? item.priorityScore : (typeof item.score === 'number' ? item.score : 0);
          const priorityLabel = formatPriorityLabel(item.priorityLabel);
          const maxStress = typeof item.maxStress === 'number' ? item.maxStress : null;
          const executionScore = typeof item.executionScore === 'number' ? item.executionScore : null;
          return (
            <button
              key={overlayId}
              type="button"
              onClick={() => toggleOverlay(overlayId)}
              style={{
                ...cardStyle,
                width: '100%',
                textAlign: 'left',
                cursor: 'pointer',
                border: active ? (isDark ? '1px solid rgba(102,153,153,0.34)' : '1px solid rgba(102,153,153,0.24)') : cardStyle.border,
                background: active
                  ? (isDark ? 'rgba(102,153,153,0.12)' : 'rgba(102,153,153,0.08)')
                  : cardStyle.background,
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'flex-start' }}>
                <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>{item.title || overlayId}</div>
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 2 }}>
                  <div style={{ fontSize: 11, fontWeight: 900, color: isDark ? '#cbd5e1' : '#334155' }}>{priorityScore.toFixed(2)}</div>
                  <div style={{ fontSize: 10, fontWeight: 800, color: 'var(--text-secondary-strong)', textTransform: 'uppercase' }}>{priorityLabel}</div>
                </div>
              </div>
              {item.explanation ? (
                <div style={{ fontSize: 11, lineHeight: 1.4, color: 'var(--text-secondary)' }}>{item.explanation}</div>
              ) : null}
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                {item.riskCategory ? <span>{item.riskCategory}</span> : null}
                {supportingPathCount > 0 ? <span>{supportingPathCount} supporting paths</span> : null}
                <span>{visibleRouteCount} visible routes</span>
                {validatedVisibleCount > 0 ? <span>{validatedVisibleCount} validated</span> : null}
                {hypothesisVisibleCount > 0 ? <span>{hypothesisVisibleCount} inferred</span> : null}
                {active ? <span>{activeMatchCount} active</span> : null}
              </div>
              {(maxStress !== null || executionScore !== null) ? (
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {maxStress !== null ? <span>max stress {maxStress.toFixed(3)}</span> : null}
                  {executionScore !== null ? <span>execution {executionScore.toFixed(2)}</span> : null}
                </div>
              ) : null}
              {item.attentionFocus ? (
                <div style={{ fontSize: 11, lineHeight: 1.4, color: 'var(--text-secondary-strong)' }}>
                  <span style={{ fontWeight: 800, color: 'var(--text-heading)' }}>Watch:</span> {item.attentionFocus}
                </div>
              ) : null}
              {(item.dominantMechanisms || []).length > 0 ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {(item.dominantMechanisms || []).slice(0, 4).join(', ')}
                </div>
              ) : null}
            </button>
          );
        })
      )}
    </div>
  );

  const renderRoleItems = (items: HorizonSimulationRoleOverlay[]) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10 }}>
        <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>Role views</div>
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{items.length}</div>
      </div>
      {items.length === 0 ? (
        <div style={{ ...cardStyle, fontSize: 12, color: 'var(--text-secondary-strong)' }}>No matching role overlays.</div>
      ) : (
        items.map((role) => (
          <div key={role.role} style={cardStyle}>
            <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>{role.title || role.role}</div>
            {(role.summary || []).length > 0 ? (
              <div style={{ fontSize: 11, lineHeight: 1.4, color: 'var(--text-secondary)' }}>
                {(role.summary || []).slice(0, 2).join(' ')}
              </div>
            ) : null}
            {(role.priorityFunctions || []).length > 0 ? (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                {(role.priorityFunctions || []).join(', ')}
              </div>
            ) : null}
          </div>
        ))
      )}
    </div>
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxHeight: 'min(68vh, 720px)', overflowY: 'auto', paddingRight: 2 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
        <div>
          <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-heading)' }}>Simulation stress</div>
          <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
            Lens ID: {lensId || 'none'}
          </div>
          <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
            {selectedRun ? `${selectedRun.status} · ${formatDate(selectedRun.createdAt || selectedRun.updatedAt)}` : 'No simulation run found for this lens'}
          </div>
        </div>
        <button
          type="button"
          onClick={() => setRefreshNonce((value) => value + 1)}
          style={shellButtonStyle(false)}
        >
          Refresh
        </button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>Simulation run</div>
        <select
          value={selectedRunId}
          onChange={(event) => setSelectedRunId(String(event.target.value || '').trim())}
          style={{
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
            color: 'var(--text-heading)',
            padding: '9px 11px',
            fontSize: 12,
          }}
        >
          {runs.length === 0 ? <option value="">No runs</option> : null}
          {runs.map((run) => {
            const runId = String(run.id || '').trim();
            const question = String(run.request?.question || '').trim();
            const preview = question ? ` · ${question}` : '';
            return (
              <option key={runId} value={runId}>
                {`${run.status} · ${formatDate(run.createdAt || run.updatedAt)} · ${runId.slice(0, 8)}${preview}`.slice(0, 160)}
              </option>
            );
          })}
        </select>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 6 }}>
        {OVERLAY_KINDS.map(({ key, label }) => {
          const active = kindEnabled[key];
          return (
            <button
              key={key}
              type="button"
              onClick={() => setKindEnabled((current) => ({ ...current, [key]: !current[key] }))}
              style={{
                borderRadius: 12,
                border: active
                  ? (isDark ? '1px solid rgba(102,153,153,0.30)' : '1px solid rgba(102,153,153,0.22)')
                  : '1px solid var(--border-subtle)',
                background: active
                  ? (isDark ? 'rgba(102,153,153,0.14)' : 'rgba(102,153,153,0.08)')
                  : 'transparent',
                color: 'var(--text-heading)',
                padding: '7px 8px',
                fontSize: 11,
                fontWeight: 800,
                cursor: 'pointer',
              }}
            >
              {label}
            </button>
          );
        })}
      </div>

      <input
        type="search"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder="Search simulation overlays"
        style={{
          borderRadius: 12,
          border: '1px solid var(--border-subtle)',
          background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
          color: 'var(--text-heading)',
          padding: '9px 11px',
          fontSize: 12,
        }}
      />

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
        <input
          value={functionsInput}
          onChange={(event) => setFunctionsInput(event.target.value)}
          placeholder="Functions"
          style={{
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
            color: 'var(--text-heading)',
            padding: '8px 10px',
            fontSize: 11,
          }}
        />
        <input
          value={disciplinesInput}
          onChange={(event) => setDisciplinesInput(event.target.value)}
          placeholder="Disciplines"
          style={{
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
            color: 'var(--text-heading)',
            padding: '8px 10px',
            fontSize: 11,
          }}
        />
        <input
          value={rolesInput}
          onChange={(event) => setRolesInput(event.target.value)}
          placeholder="Roles"
          style={{
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
            color: 'var(--text-heading)',
            padding: '8px 10px',
            fontSize: 11,
          }}
        />
        <input
          value={categoriesInput}
          onChange={(event) => setCategoriesInput(event.target.value)}
          placeholder="Categories"
          style={{
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.024)',
            color: 'var(--text-heading)',
            padding: '8px 10px',
            fontSize: 11,
          }}
        />
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
          <input type="checkbox" checked={mappedOnly} onChange={(event) => setMappedOnly(event.target.checked)} />
          Only mapped to visible routes
        </label>
        <button
          type="button"
          onClick={() => setActiveOverlayIds([])}
          disabled={activeOverlayIds.length === 0}
          style={{
            ...shellButtonStyle(false),
            marginLeft: 'auto',
            color: activeOverlayIds.length > 0 ? 'var(--text-secondary-strong)' : 'var(--text-secondary)',
            cursor: activeOverlayIds.length > 0 ? 'pointer' : 'not-allowed',
            opacity: activeOverlayIds.length > 0 ? 1 : 0.55,
          }}
        >
          Clear map selection
        </button>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Min score {minScore.toFixed(2)}</div>
        <input
          type="range"
          min={0}
          max={1}
          step={0.05}
          value={minScore}
          onChange={(event) => setMinScore(Number(event.target.value))}
          style={{ flex: 1 }}
        />
      </div>

      {loadingRun || loadingOverlays ? (
        <div style={{ ...cardStyle, fontSize: 12, color: 'var(--text-secondary-strong)' }}>Loading simulation overlays…</div>
      ) : null}
      {error ? (
        <div style={{ ...cardStyle, border: '1px solid rgba(239,68,68,0.24)', background: isDark ? 'rgba(239,68,68,0.12)' : 'rgba(239,68,68,0.08)', color: '#ef4444', fontSize: 12 }}>
          {error}
        </div>
      ) : null}

      {renderOverlayItems('Shoulders', filteredOverlays.shoulders)}
      {renderOverlayItems('Products', filteredOverlays.products)}
      {renderOverlayItems('Path chains', filteredOverlays.pathChains)}
      {renderOverlayItems('Cascades', filteredOverlays.cascades)}
      {renderRoleItems(filteredOverlays.roles)}
    </div>
  );
}
