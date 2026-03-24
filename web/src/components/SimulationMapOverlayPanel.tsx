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

const USABLE_RUN_STATUSES = new Set(['SUCCEEDED', 'DEGRADED', 'REVIEWING', 'AGGREGATING']);

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

const pickLatestRun = (runs: HorizonSimulationRunSummary[], lensId: string): HorizonSimulationRunSummary | null => {
  const matching = runs
    .filter((run) => String(run.request?.lensId || '').trim() === lensId)
    .sort((a, b) => {
      const aTs = Date.parse(String(a.createdAt || a.updatedAt || '')) || 0;
      const bTs = Date.parse(String(b.createdAt || b.updatedAt || '')) || 0;
      return bTs - aTs;
    });
  if (matching.length === 0) return null;
  return matching.find((run) => USABLE_RUN_STATUSES.has(String(run.status || '').trim().toUpperCase())) || matching[0];
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

type RouteMatcher = {
  artifact: MaterializedArtifact;
  refs: Set<string>;
  countries: Set<string>;
  products: Set<string>;
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

interface SimulationMapOverlayPanelProps {
  lensId: string | null;
  routeCatalog: MaterializedArtifact[];
  selectedRouteArtifactIds: string[];
  onChangeSelectedRouteArtifactIds: (ids: string[]) => void;
  isDark: boolean;
}

export function SimulationMapOverlayPanel({
  lensId,
  routeCatalog,
  selectedRouteArtifactIds,
  onChangeSelectedRouteArtifactIds,
  isDark,
}: SimulationMapOverlayPanelProps) {
  const [latestRun, setLatestRun] = React.useState<HorizonSimulationRunSummary | null>(null);
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

  React.useEffect(() => {
    if (!lensId) return;
    let cancelled = false;
    setLoadingRun(true);
    setError(null);
    api
      .listHorizonSimulationRuns()
      .then((response) => {
        if (cancelled) return;
        setLatestRun(pickLatestRun(response.runs || [], lensId));
      })
      .catch((err: any) => {
        if (cancelled) return;
        setError(err?.message || 'Failed to load simulation runs');
        setLatestRun(null);
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
    if (!latestRun?.id) return;
    let cancelled = false;
    setLoadingOverlays(true);
    setError(null);
    api
      .getHorizonSimulationOverlays(latestRun.id, backendFilters)
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
  }, [backendFilters, latestRun?.id]);

  const matchMap = React.useMemo(() => {
    const byOverlayId = new Map<string, string[]>();
    const overlayLists: HorizonSimulationOverlayItem[] = [
      ...rawOverlays.shoulders,
      ...rawOverlays.products,
      ...rawOverlays.pathChains,
      ...rawOverlays.cascades,
    ];
    for (const item of overlayLists) {
      const overlayId = String(item.id || '').trim();
      if (!overlayId) continue;
      const matched = routeMatchers
        .map((matcher) => ({
          artifactId: String(matcher.artifact.id || '').trim(),
          weight: overlayMatchCount(item, matcher) + (typeof matcher.artifact.pathStressScore === 'number' ? matcher.artifact.pathStressScore : 0),
        }))
        .filter((entry) => entry.artifactId && entry.weight > 0)
        .sort((a, b) => b.weight - a.weight)
        .map((entry) => entry.artifactId);
      byOverlayId.set(overlayId, matched);
    }
    return byOverlayId;
  }, [rawOverlays, routeMatchers]);

  const filteredOverlays = React.useMemo<HorizonSimulationResultOverlays>(() => {
    const filterItems = (items: HorizonSimulationOverlayItem[]) => {
      const base = mappedOnly
        ? items.filter((item) => (matchMap.get(String(item.id || '').trim()) || []).length > 0)
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
        return allTargets.some((target) => (matchMap.get(String(target || '').trim()) || []).length > 0);
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
      for (const artifactId of matchMap.get(String(overlayId || '').trim()) || []) {
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
        const set = new Set(current.map((value) => String(value || '').trim()).filter(Boolean));
        if (set.has(id)) set.delete(id);
        else set.add(id);
        return Array.from(set);
      });
    });
  }, []);

  const cardStyle: React.CSSProperties = {
    borderRadius: 12,
    border: '1px solid var(--border-subtle)',
    background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)',
    padding: '10px 12px',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
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
          const matchedArtifacts = matchMap.get(overlayId) || [];
          const active = activeOverlayIds.includes(overlayId);
          const activeMatchCount = matchedArtifacts.filter((artifactId) => selectedRouteArtifactIdSet.has(artifactId)).length;
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
                border: active ? '1px solid rgba(37,99,235,0.45)' : cardStyle.border,
                background: active
                  ? (isDark ? 'rgba(59,130,246,0.14)' : 'rgba(59,130,246,0.08)')
                  : cardStyle.background,
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'flex-start' }}>
                <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>{item.title || overlayId}</div>
                <div style={{ fontSize: 11, fontWeight: 900, color: '#ef4444' }}>{(typeof item.score === 'number' ? item.score : 0).toFixed(2)}</div>
              </div>
              {item.explanation ? (
                <div style={{ fontSize: 11, lineHeight: 1.4, color: 'var(--text-secondary)' }}>{item.explanation}</div>
              ) : null}
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                {item.riskCategory ? <span>{item.riskCategory}</span> : null}
                <span>{matchedArtifacts.length} routes</span>
                {active ? <span>{activeMatchCount} active</span> : null}
              </div>
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
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
        <div>
          <div style={{ fontSize: 14, fontWeight: 800, color: 'var(--text-heading)' }}>Simulation stress</div>
          <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
            {latestRun ? `${latestRun.status} · ${formatDate(latestRun.createdAt || latestRun.updatedAt)}` : 'No simulation run found for this lens'}
          </div>
        </div>
        <button
          type="button"
          onClick={() => setRefreshNonce((value) => value + 1)}
          style={{
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            padding: '7px 10px',
            fontSize: 11,
            fontWeight: 700,
            cursor: 'pointer',
          }}
        >
          Refresh
        </button>
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
                borderRadius: 10,
                border: active ? '1px solid rgba(14,165,233,0.35)' : '1px solid var(--border-subtle)',
                background: active ? (isDark ? 'rgba(14,165,233,0.16)' : 'rgba(14,165,233,0.10)') : 'transparent',
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
          borderRadius: 10,
          border: '1px solid var(--border-subtle)',
          background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
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
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
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
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
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
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
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
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
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
            marginLeft: 'auto',
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: 'transparent',
            color: activeOverlayIds.length > 0 ? 'var(--text-secondary-strong)' : 'var(--text-secondary)',
            padding: '7px 10px',
            fontSize: 11,
            fontWeight: 700,
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
