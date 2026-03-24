import React from 'react';
import { ThemeContext } from './ThemeProvider';
import {
  api,
  type HorizonSimulationOverlayFilters,
  type HorizonSimulationOverlayItem,
  type HorizonSimulationResultOverlays,
  type HorizonSimulationRoleOverlay,
  type HorizonSimulationRunSummary,
} from '../lib/api';

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

const overlayMatchesVisibleRefs = (
  item: HorizonSimulationOverlayItem,
  visibleRefs: Set<string>,
  selectedTargetRef: string,
  visibleOnly: boolean,
  selectedOnly: boolean,
): boolean => {
  const refs = Array.isArray(item.targetRefs)
    ? item.targetRefs.map((ref) => String(ref || '').trim()).filter(Boolean)
    : [];
  const countryCode = String(item.targetCountryCode || '').trim().toLowerCase();
  if (!visibleOnly && !selectedOnly) return true;
  if (refs.length === 0 && !countryCode) return !selectedOnly;
  if (selectedOnly && selectedTargetRef) {
    return refs.some((ref) => ref === selectedTargetRef);
  }
  if (visibleOnly) {
    return refs.some((ref) => visibleRefs.has(ref));
  }
  return true;
};

const roleMatchesVisibleRefs = (
  role: HorizonSimulationRoleOverlay,
  activeOverlayIDs: Set<string>,
  visibleOnly: boolean,
  selectedOnly: boolean,
): boolean => {
  if (!visibleOnly && !selectedOnly) return true;
  const targets = role.targets || {};
  const allTargets = [
    ...(targets.shoulders || []),
    ...(targets.products || []),
    ...(targets.pathChains || []),
    ...(targets.cascades || []),
  ];
  if (allTargets.length === 0) return !selectedOnly;
  return allTargets.some((target) => activeOverlayIDs.has(String(target || '').trim()));
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

interface SimulationOverlayPanelProps {
  lensId: string | null;
  isVisible: boolean;
  visibleTargetRefs?: string[];
  selectedTargetRef?: string | null;
}

export function SimulationOverlayPanel({
  lensId,
  isVisible,
  visibleTargetRefs = [],
  selectedTargetRef = null,
}: SimulationOverlayPanelProps) {
  const { theme } = React.useContext(ThemeContext);
  const [latestRun, setLatestRun] = React.useState<HorizonSimulationRunSummary | null>(null);
  const [loadingRun, setLoadingRun] = React.useState(false);
  const [loadingOverlays, setLoadingOverlays] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = React.useState(0);
  const [query, setQuery] = React.useState('');
  const [functionsInput, setFunctionsInput] = React.useState('');
  const [disciplinesInput, setDisciplinesInput] = React.useState('');
  const [rolesInput, setRolesInput] = React.useState('');
  const [targetsInput, setTargetsInput] = React.useState('');
  const [categoriesInput, setCategoriesInput] = React.useState('');
  const [minScore, setMinScore] = React.useState(0.35);
  const [visibleOnly, setVisibleOnly] = React.useState(true);
  const [selectedOnly, setSelectedOnly] = React.useState(false);
  const [kindEnabled, setKindEnabled] = React.useState<Record<OverlayKind, boolean>>({
    shoulders: true,
    products: true,
    pathChains: true,
    cascades: true,
    roles: true,
  });
  const [rawOverlays, setRawOverlays] = React.useState<HorizonSimulationResultOverlays>(emptyOverlays);

  const deferredQuery = React.useDeferredValue(query);
  const deferredFunctionsInput = React.useDeferredValue(functionsInput);
  const deferredDisciplinesInput = React.useDeferredValue(disciplinesInput);
  const deferredRolesInput = React.useDeferredValue(rolesInput);
  const deferredTargetsInput = React.useDeferredValue(targetsInput);
  const deferredCategoriesInput = React.useDeferredValue(categoriesInput);

  const visibleRefSet = React.useMemo(
    () =>
      new Set(
        visibleTargetRefs
          .map((ref) => String(ref || '').trim())
          .filter(Boolean),
      ),
    [visibleTargetRefs],
  );
  const selectedTargetRefNormalized = String(selectedTargetRef || '').trim();

  React.useEffect(() => {
    if (!isVisible || !lensId) return;
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
  }, [isVisible, lensId, refreshNonce]);

  const backendFilters = React.useMemo<HorizonSimulationOverlayFilters>(() => {
    const kinds = OVERLAY_KINDS.filter(({ key }) => kindEnabled[key]).map(({ key }) => key);
    return {
      kinds,
      functions: parseCsv(deferredFunctionsInput),
      disciplines: parseCsv(deferredDisciplinesInput),
      roles: parseCsv(deferredRolesInput),
      targets: parseCsv(deferredTargetsInput),
      categories: parseCsv(deferredCategoriesInput),
      q: deferredQuery.trim(),
      minScore,
    };
  }, [
    deferredCategoriesInput,
    deferredDisciplinesInput,
    deferredFunctionsInput,
    deferredQuery,
    deferredRolesInput,
    deferredTargetsInput,
    kindEnabled,
    minScore,
  ]);

  React.useEffect(() => {
    if (!isVisible || !latestRun?.id) return;
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
  }, [backendFilters, isVisible, latestRun?.id]);

  const filteredOverlays = React.useMemo<HorizonSimulationResultOverlays>(() => {
    const shoulders = rawOverlays.shoulders.filter((item) =>
      overlayMatchesVisibleRefs(item, visibleRefSet, selectedTargetRefNormalized, visibleOnly, selectedOnly),
    );
    const products = rawOverlays.products.filter((item) =>
      overlayMatchesVisibleRefs(item, visibleRefSet, selectedTargetRefNormalized, visibleOnly, selectedOnly),
    );
    const pathChains = rawOverlays.pathChains.filter((item) =>
      overlayMatchesVisibleRefs(item, visibleRefSet, selectedTargetRefNormalized, visibleOnly, selectedOnly),
    );
    const cascades = rawOverlays.cascades.filter((item) =>
      overlayMatchesVisibleRefs(item, visibleRefSet, selectedTargetRefNormalized, visibleOnly, selectedOnly),
    );
    const activeOverlayIDs = new Set(
      [...shoulders, ...products, ...pathChains, ...cascades]
        .map((item) => String(item.id || '').trim())
        .filter(Boolean),
    );
    const roles = rawOverlays.roles.filter((role) => roleMatchesVisibleRefs(role, activeOverlayIDs, visibleOnly, selectedOnly));
    return { shoulders, products, pathChains, cascades, roles };
  }, [rawOverlays, selectedOnly, selectedTargetRefNormalized, visibleOnly, visibleRefSet]);

  const totalItems = React.useMemo(
    () =>
      filteredOverlays.shoulders.length +
      filteredOverlays.products.length +
      filteredOverlays.pathChains.length +
      filteredOverlays.cascades.length +
      filteredOverlays.roles.length,
    [filteredOverlays],
  );

  const panelStyle: React.CSSProperties = {
    width: 440,
    maxWidth: 'min(440px, calc(100vw - 96px))',
    maxHeight: 'calc(100vh - 120px)',
    overflowY: 'auto',
    padding: 14,
    borderRadius: 18,
    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(15,23,42,0.10)'}`,
    background: theme === 'dark' ? 'rgba(10,10,12,0.78)' : 'rgba(255,255,255,0.82)',
    backdropFilter: 'blur(18px)',
    boxShadow: theme === 'dark' ? '0 24px 70px rgba(0,0,0,0.62)' : '0 24px 70px rgba(15,23,42,0.16)',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    color: 'var(--text-heading)',
  };

  const filterInputStyle: React.CSSProperties = {
    width: '100%',
    borderRadius: 10,
    border: '1px solid var(--border-subtle)',
    background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
    color: 'var(--text-heading)',
    padding: '8px 10px',
    fontSize: 12,
  };

  const renderOverlayItems = (title: string, items: HorizonSimulationOverlayItem[]) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ fontSize: 12, fontWeight: 900, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
          {title}
        </div>
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{items.length}</div>
      </div>
      {items.length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', padding: '8px 10px', borderRadius: 12, border: '1px dashed var(--border-subtle)' }}>
          No matching overlays.
        </div>
      ) : (
        items.map((item, index) => (
          <div
            key={String(item.id || item.title || `${title}-${index}`)}
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 8,
              padding: '10px 12px',
              borderRadius: 14,
              border: '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)',
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
              <div style={{ fontSize: 13, fontWeight: 800, lineHeight: 1.35 }}>{item.title || item.id || 'Untitled overlay'}</div>
              <div style={{ fontSize: 12, fontWeight: 900, color: '#ef4444', whiteSpace: 'nowrap' }}>
                {(typeof item.score === 'number' ? item.score : 0).toFixed(2)}
              </div>
            </div>
            {item.explanation ? (
              <div style={{ fontSize: 12, lineHeight: 1.45, color: 'var(--text-secondary)' }}>{item.explanation}</div>
            ) : null}
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {item.riskCategory ? (
                <span style={{ padding: '3px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', fontSize: 11, fontWeight: 800 }}>
                  {item.riskCategory}
                </span>
              ) : null}
              {(item.functions || []).map((value) => (
                <span key={`fn:${value}`} style={{ padding: '3px 8px', borderRadius: 999, background: theme === 'dark' ? 'rgba(59,130,246,0.18)' : 'rgba(59,130,246,0.10)', fontSize: 11, fontWeight: 700 }}>
                  {value}
                </span>
              ))}
              {(item.disciplines || []).map((value) => (
                <span key={`disc:${value}`} style={{ padding: '3px 8px', borderRadius: 999, background: theme === 'dark' ? 'rgba(16,185,129,0.18)' : 'rgba(16,185,129,0.10)', fontSize: 11, fontWeight: 700 }}>
                  {value}
                </span>
              ))}
            </div>
            {(item.dominantMechanisms || []).length > 0 ? (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                Mechanisms: {(item.dominantMechanisms || []).join(', ')}
              </div>
            ) : null}
            {(item.recommendedActions || []).length > 0 ? (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                Actions: {(item.recommendedActions || []).join(', ')}
              </div>
            ) : null}
            {(item.targetRefs || []).length > 0 ? (
              <div style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                Targets: {(item.targetRefs || []).slice(0, 4).join(', ')}
                {(item.targetRefs || []).length > 4 ? ` +${(item.targetRefs || []).length - 4}` : ''}
              </div>
            ) : null}
          </div>
        ))
      )}
    </div>
  );

  const renderRoleItems = (items: HorizonSimulationRoleOverlay[]) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ fontSize: 12, fontWeight: 900, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
          Role Views
        </div>
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{items.length}</div>
      </div>
      {items.length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', padding: '8px 10px', borderRadius: 12, border: '1px dashed var(--border-subtle)' }}>
          No matching role overlays.
        </div>
      ) : (
        items.map((role) => {
          const targetCount =
            (role.targets?.shoulders?.length || 0) +
            (role.targets?.products?.length || 0) +
            (role.targets?.pathChains?.length || 0) +
            (role.targets?.cascades?.length || 0);
          return (
            <div
              key={role.role}
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
                padding: '10px 12px',
                borderRadius: 14,
                border: '1px solid var(--border-subtle)',
                background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ fontSize: 13, fontWeight: 800 }}>{role.title || role.role}</div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{targetCount} targets</div>
              </div>
              {(role.summary || []).length > 0 ? (
                <div style={{ fontSize: 12, lineHeight: 1.45, color: 'var(--text-secondary)' }}>
                  {(role.summary || []).slice(0, 2).join(' ')}
                </div>
              ) : null}
              {(role.priorityFunctions || []).length > 0 ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  Functions: {(role.priorityFunctions || []).join(', ')}
                </div>
              ) : null}
              {(role.immediateActions || []).length > 0 ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  Immediate: {(role.immediateActions || []).slice(0, 3).join(', ')}
                </div>
              ) : null}
            </div>
          );
        })
      )}
    </div>
  );

  return (
    <div style={panelStyle}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <div style={{ fontSize: 12, fontWeight: 900, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
            Simulation Overlays
          </div>
          <div style={{ fontSize: 15, fontWeight: 900 }}>
            {latestRun ? `Run ${latestRun.id.slice(0, 8)}` : 'No simulation run'}
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
            {latestRun ? `${latestRun.status} · ${formatDate(latestRun.createdAt || latestRun.updatedAt)}` : 'Open a lens with a completed simulation run.'}
          </div>
          {latestRun?.request?.question ? (
            <div style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.4 }}>
              {latestRun.request.question}
            </div>
          ) : null}
        </div>
        <button
          type="button"
          onClick={() => setRefreshNonce((value) => value + 1)}
          style={{
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: 'transparent',
            color: 'var(--text-heading)',
            padding: '7px 10px',
            fontSize: 12,
            fontWeight: 800,
            cursor: 'pointer',
          }}
        >
          Refresh
        </button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
        {OVERLAY_KINDS.map(({ key, label }) => {
          const active = kindEnabled[key];
          return (
            <button
              key={key}
              type="button"
              onClick={() => setKindEnabled((current) => ({ ...current, [key]: !current[key] }))}
              style={{
                padding: '8px 10px',
                borderRadius: 12,
                border: active ? '1px solid rgba(37,99,235,0.50)' : '1px solid var(--border-subtle)',
                background: active
                  ? (theme === 'dark' ? 'rgba(59,130,246,0.16)' : 'rgba(59,130,246,0.10)')
                  : 'transparent',
                color: 'var(--text-heading)',
                fontSize: 12,
                fontWeight: 800,
                cursor: 'pointer',
              }}
            >
              {label}
            </button>
          );
        })}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search overlays"
          style={filterInputStyle}
        />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
          <input
            value={functionsInput}
            onChange={(event) => setFunctionsInput(event.target.value)}
            placeholder="Functions: operations, finance"
            style={filterInputStyle}
          />
          <input
            value={disciplinesInput}
            onChange={(event) => setDisciplinesInput(event.target.value)}
            placeholder="Disciplines: supply_operations"
            style={filterInputStyle}
          />
          <input
            value={rolesInput}
            onChange={(event) => setRolesInput(event.target.value)}
            placeholder="Roles: coo, cfo"
            style={filterInputStyle}
          />
          <input
            value={categoriesInput}
            onChange={(event) => setCategoriesInput(event.target.value)}
            placeholder="Categories: capacity_pressure"
            style={filterInputStyle}
          />
          <input
            value={targetsInput}
            onChange={(event) => setTargetsInput(event.target.value)}
            placeholder="Targets: /fac/..., shoulder_load_ae"
            style={{ ...filterInputStyle, gridColumn: '1 / -1' }}
          />
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
            <input type="checkbox" checked={visibleOnly} onChange={(event) => setVisibleOnly(event.target.checked)} />
            Visible graph only
          </label>
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
            <input
              type="checkbox"
              checked={selectedOnly}
              disabled={!selectedTargetRefNormalized}
              onChange={(event) => setSelectedOnly(event.target.checked)}
            />
            Selected node only
          </label>
          <div style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-secondary-strong)' }}>
            Min score {(minScore || 0).toFixed(2)}
          </div>
        </div>
        <input
          type="range"
          min={0}
          max={1}
          step={0.05}
          value={minScore}
          onChange={(event) => setMinScore(Number(event.target.value))}
        />
      </div>

      {loadingRun || loadingOverlays ? (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>Loading overlays…</div>
      ) : null}
      {error ? (
        <div style={{ padding: '10px 12px', borderRadius: 12, border: '1px solid rgba(239,68,68,0.24)', background: theme === 'dark' ? 'rgba(239,68,68,0.12)' : 'rgba(239,68,68,0.08)', color: '#ef4444', fontSize: 12 }}>
          {error}
        </div>
      ) : null}
      {!loadingRun && !loadingOverlays && !error ? (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          Showing {totalItems} overlays
          {selectedOnly && selectedTargetRefNormalized ? ` for ${selectedTargetRefNormalized}` : ''}
          {visibleOnly ? ' within the visible graph' : ''}.
        </div>
      ) : null}

      {renderOverlayItems('Shoulders', filteredOverlays.shoulders)}
      {renderOverlayItems('Products', filteredOverlays.products)}
      {renderOverlayItems('Path Chains', filteredOverlays.pathChains)}
      {renderOverlayItems('Cascades', filteredOverlays.cascades)}
      {renderRoleItems(filteredOverlays.roles)}
    </div>
  );
}
