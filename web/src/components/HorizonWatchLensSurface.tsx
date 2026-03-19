import React from 'react';
import { api } from '../lib/api';
import type {
  HorizonWatchPageAffectedItem,
  HorizonWatchPageDashboardMetric,
  HorizonWatchPageEvidenceItem,
  HorizonWatchPageMapFragment,
  HorizonWatchPageRiskItem,
  HorizonWatchPageScenarioItem,
  HorizonWatchPageSnapshot,
  HorizonWatchPageScopeItem,
  HorizonWatchPageSignalItem,
  HorizonWatchPageSupplyPath,
  HorizonWatchSearchResult,
} from '../types/horizonWatch';

type HorizonWatchLensSurfaceProps = {
  lensId: string;
  projectName?: string | null;
  onOpenWorldMap?: () => void;
  isAdmin?: boolean;
};

const panelStyle: React.CSSProperties = {
  borderRadius: 24,
  border: '1px solid rgba(148, 163, 184, 0.18)',
  background: 'linear-gradient(180deg, rgba(15,23,42,0.96) 0%, rgba(2,6,23,0.98) 100%)',
  boxShadow: '0 30px 90px rgba(2,6,23,0.35)',
  backdropFilter: 'blur(14px)',
};

const sectionTitleStyle: React.CSSProperties = {
  fontSize: 12,
  letterSpacing: '0.16em',
  textTransform: 'uppercase',
  color: 'rgba(148, 163, 184, 0.86)',
  marginBottom: 14,
};

const timeHorizonOptions = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
  { value: '90d', label: '90d' },
];

function formatTimestamp(value?: string): string {
  if (!value) {
    return 'n/a';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 'n/a';
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}

function statusTone(status?: HorizonWatchPageSnapshot['buildStatus']): { label: string; color: string; bg: string } {
  switch (status) {
    case 'READY':
      return { label: 'Ready', color: '#86efac', bg: 'rgba(34,197,94,0.14)' };
    case 'STALE':
      return { label: 'Stale', color: '#fcd34d', bg: 'rgba(245,158,11,0.14)' };
    case 'FAILED':
      return { label: 'Failed', color: '#fca5a5', bg: 'rgba(239,68,68,0.14)' };
    default:
      return { label: 'Not built', color: '#93c5fd', bg: 'rgba(59,130,246,0.14)' };
  }
}

function deltaTone(direction?: string): string {
  switch (direction) {
    case 'up':
      return '#fca5a5';
    case 'down':
      return '#86efac';
    default:
      return '#cbd5e1';
  }
}

function renderListChips(items?: string[], emptyLabel?: string): React.ReactNode {
  if (!Array.isArray(items) || items.length === 0) {
    return emptyLabel ? <span style={{ color: 'rgba(148, 163, 184, 0.85)' }}>{emptyLabel}</span> : null;
  }
  return (
    <div className="flex flex-wrap gap-2">
      {items.map((item) => (
        <span
          key={item}
          className="rounded-full px-3 py-1 text-xs"
          style={{
            border: '1px solid rgba(148, 163, 184, 0.2)',
            background: 'rgba(15, 23, 42, 0.7)',
            color: 'rgba(226, 232, 240, 0.94)',
          }}
        >
          {item}
        </span>
      ))}
    </div>
  );
}

function RiskCard({ item }: { item: HorizonWatchPageRiskItem }) {
  return (
    <div
      className="rounded-3xl p-5"
      style={{
        border: '1px solid rgba(248, 113, 113, 0.18)',
        background: 'linear-gradient(180deg, rgba(30, 41, 59, 0.88) 0%, rgba(15, 23, 42, 0.96) 100%)',
      }}
    >
      <div className="mb-2 flex items-start justify-between gap-3">
        <div>
          <div className="text-lg font-semibold text-slate-100">{item.title}</div>
          {item.canonicalPath ? (
            <div className="mt-1 text-xs text-slate-400">{item.canonicalPath}</div>
          ) : null}
        </div>
        {item.riskPosture ? (
          <span
            className="rounded-full px-3 py-1 text-xs font-medium uppercase"
            style={{
              background: 'rgba(239, 68, 68, 0.14)',
              color: '#fca5a5',
            }}
          >
            {item.riskPosture}
          </span>
        ) : null}
      </div>
      {item.summary ? <p className="text-sm leading-6 text-slate-300">{item.summary}</p> : null}
      {item.mechanisms?.length ? (
        <div className="mt-4">
          {renderListChips(item.mechanisms)}
        </div>
      ) : null}
    </div>
  );
}

function AffectedCard({ item }: { item: HorizonWatchPageAffectedItem }) {
  return (
    <div
      className="rounded-3xl p-4"
      style={{
        border: '1px solid rgba(94, 234, 212, 0.14)',
        background: 'rgba(15, 23, 42, 0.84)',
      }}
    >
      <div className="mb-2 flex items-start justify-between gap-3">
        <div>
          <div className="text-base font-semibold text-slate-100">{item.label}</div>
          <div className="mt-1 text-xs text-slate-400">{item.canonicalPath}</div>
        </div>
        {item.rank ? (
          <span className="text-xs font-semibold uppercase tracking-[0.14em] text-teal-300">#{item.rank}</span>
        ) : null}
      </div>
      {item.reason ? <p className="text-sm leading-6 text-slate-300">{item.reason}</p> : null}
    </div>
  );
}

function ScopeCard({ item }: { item: HorizonWatchPageScopeItem }) {
  return (
    <div
      className="rounded-3xl p-4"
      style={{
        border: '1px solid rgba(148, 163, 184, 0.16)',
        background: 'rgba(15, 23, 42, 0.74)',
      }}
    >
      <div className="text-base font-semibold text-slate-100">{item.label}</div>
      <div className="mt-1 text-xs uppercase tracking-[0.12em] text-slate-500">{item.kind || 'scope'}</div>
      <div className="mt-2 text-xs text-slate-400">{item.canonicalPath}</div>
      {item.summary ? <p className="mt-3 text-sm leading-6 text-slate-300">{item.summary}</p> : null}
    </div>
  );
}

function SupplyPathCard({ item }: { item: HorizonWatchPageSupplyPath }) {
  return (
    <div
      className="rounded-3xl p-5"
      style={{
        border: '1px solid rgba(96, 165, 250, 0.16)',
        background: 'rgba(15, 23, 42, 0.82)',
      }}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-base font-semibold text-slate-50">{item.title}</div>
          {item.productAxis ? <div className="mt-1 text-xs text-slate-400">{item.productAxis}</div> : null}
        </div>
        <div className="text-right">
          {typeof item.stressScore === 'number' ? <div className="text-sm font-semibold text-sky-300">{item.stressScore.toFixed(2)} stress</div> : null}
          {typeof item.bindingConfidence === 'number' ? <div className="mt-1 text-xs text-slate-500">{Math.round(item.bindingConfidence * 100)}% binding</div> : null}
        </div>
      </div>
      {item.summary ? <p className="mt-3 text-sm leading-6 text-slate-300">{item.summary}</p> : null}
      <div className="mt-4 grid gap-3 md:grid-cols-2">
        {item.anchorSupplier ? (
          <div>
            <div className="mb-1 text-xs uppercase tracking-[0.12em] text-slate-500">Supplier</div>
            <div className="text-sm text-slate-200">{item.anchorSupplier}</div>
          </div>
        ) : null}
        {item.anchorCustomer ? (
          <div>
            <div className="mb-1 text-xs uppercase tracking-[0.12em] text-slate-500">Customer</div>
            <div className="text-sm text-slate-200">{item.anchorCustomer}</div>
          </div>
        ) : null}
      </div>
      {item.routeRefs?.length ? (
        <div className="mt-4">
          <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Routes</div>
          {renderListChips(item.routeRefs)}
        </div>
      ) : null}
    </div>
  );
}

function SignalCard({ item }: { item: HorizonWatchPageSignalItem }) {
  return (
    <div
      className="rounded-3xl p-4"
      style={{
        border: '1px solid rgba(251, 191, 36, 0.16)',
        background: 'rgba(15, 23, 42, 0.72)',
      }}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-slate-100">{item.title}</div>
          <div className="mt-1 text-xs uppercase tracking-[0.12em] text-amber-200">{item.kind}</div>
        </div>
        {item.sourceLabel ? <div className="text-xs text-slate-500">{item.sourceLabel}</div> : null}
      </div>
      {item.summary ? <p className="mt-3 text-sm leading-6 text-slate-300">{item.summary}</p> : null}
      {item.canonicalPaths?.length ? <div className="mt-4">{renderListChips(item.canonicalPaths)}</div> : null}
    </div>
  );
}

function ScenarioCard({ item }: { item: HorizonWatchPageScenarioItem }) {
  return (
    <div
      className="rounded-3xl p-5"
      style={{
        border: '1px solid rgba(192, 132, 252, 0.16)',
        background: 'rgba(15, 23, 42, 0.78)',
      }}
    >
      <div className="text-base font-semibold text-slate-100">{item.title}</div>
      {item.description ? <p className="mt-3 text-sm leading-6 text-slate-300">{item.description}</p> : null}
      {item.summary ? <p className="mt-3 text-sm leading-6 text-violet-200">{item.summary}</p> : null}
      {item.triggerConditions?.length ? (
        <div className="mt-4">
          <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Triggers</div>
          {renderListChips(item.triggerConditions)}
        </div>
      ) : null}
      {item.targetPaths?.length ? (
        <div className="mt-4">
          <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Scope</div>
          {renderListChips(item.targetPaths)}
        </div>
      ) : null}
    </div>
  );
}

function MetricCard({ metric }: { metric: HorizonWatchPageDashboardMetric }) {
  return (
    <div
      className="rounded-3xl p-5"
      style={{
        border: '1px solid rgba(96, 165, 250, 0.18)',
        background: 'linear-gradient(180deg, rgba(14, 22, 36, 0.86) 0%, rgba(10, 15, 28, 0.96) 100%)',
      }}
    >
      <div className="text-xs uppercase tracking-[0.14em] text-slate-400">{metric.title}</div>
      <div className="mt-3 flex items-end justify-between gap-3">
        <div className="text-3xl font-semibold text-slate-50">{metric.value}</div>
        {metric.deltaValue ? (
          <div className="text-sm font-semibold" style={{ color: deltaTone(metric.deltaDirection) }}>
            {metric.deltaValue}
          </div>
        ) : null}
      </div>
      {metric.subtitle ? <div className="mt-2 text-sm text-slate-400">{metric.subtitle}</div> : null}
      {metric.caption ? <p className="mt-4 text-sm leading-6 text-slate-300">{metric.caption}</p> : null}
    </div>
  );
}

function MapFragmentCard({
  fragment,
  onOpenWorldMap,
}: {
  fragment: HorizonWatchPageMapFragment;
  onOpenWorldMap?: () => void;
}) {
  return (
    <div
      className="rounded-3xl p-5"
      style={{
        border: '1px solid rgba(251, 191, 36, 0.18)',
        background:
          'radial-gradient(circle at top right, rgba(251, 191, 36, 0.12), transparent 34%), linear-gradient(180deg, rgba(30, 41, 59, 0.92) 0%, rgba(15, 23, 42, 0.96) 100%)',
      }}
    >
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <div className="text-base font-semibold text-slate-50">{fragment.title}</div>
          {fragment.scopeType ? (
            <div className="mt-1 text-xs uppercase tracking-[0.12em] text-amber-200">{fragment.scopeType}</div>
          ) : null}
        </div>
        {onOpenWorldMap ? (
          <button
            type="button"
            onClick={onOpenWorldMap}
            className="rounded-full px-3 py-1.5 text-xs font-medium"
            style={{
              border: '1px solid rgba(251, 191, 36, 0.24)',
              color: '#fde68a',
              background: 'rgba(120, 53, 15, 0.16)',
            }}
          >
            Open map
          </button>
        ) : null}
      </div>
      {fragment.caption ? <p className="text-sm leading-6 text-slate-300">{fragment.caption}</p> : null}
      <div className="mt-4 space-y-3">
        {fragment.canonicalPaths?.length ? (
          <div>
            <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Concentrated around</div>
            {renderListChips(fragment.canonicalPaths)}
          </div>
        ) : null}
        {fragment.layerHints?.length ? (
          <div>
            <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Layers</div>
            {renderListChips(fragment.layerHints)}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function EvidenceCard({ item }: { item: HorizonWatchPageEvidenceItem }) {
  return (
    <div
      className="rounded-3xl p-4"
      style={{
        border: '1px solid rgba(148, 163, 184, 0.14)',
        background: 'rgba(15, 23, 42, 0.72)',
      }}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-slate-100">{item.title}</div>
          {item.summary ? <p className="mt-2 text-sm leading-6 text-slate-300">{item.summary}</p> : null}
        </div>
        <span className="rounded-full bg-slate-800/80 px-2.5 py-1 text-[11px] uppercase tracking-[0.12em] text-slate-300">
          {item.kind}
        </span>
      </div>
      {item.canonicalPaths?.length ? <div className="mt-4">{renderListChips(item.canonicalPaths)}</div> : null}
    </div>
  );
}

export const HorizonWatchLensSurface: React.FC<HorizonWatchLensSurfaceProps> = ({
  lensId,
  projectName,
  onOpenWorldMap,
  isAdmin = false,
}) => {
  const [snapshot, setSnapshot] = React.useState<HorizonWatchPageSnapshot | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [refreshing, setRefreshing] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [timeHorizon, setTimeHorizon] = React.useState('7d');
  const [watchQuery, setWatchQuery] = React.useState('');
  const [interestTitle, setInterestTitle] = React.useState('');
  const [interestQuery, setInterestQuery] = React.useState('');
  const [searchBusy, setSearchBusy] = React.useState(false);
  const [addingTarget, setAddingTarget] = React.useState<string | null>(null);
  const [searchResults, setSearchResults] = React.useState<HorizonWatchSearchResult[]>([]);

  const loadSnapshot = React.useCallback(async () => {
    if (!lensId) {
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const next = await api.getHorizonWatchPage({ lensId });
      setSnapshot(next);
      if (next.timeHorizon) {
        setTimeHorizon(next.timeHorizon);
      }
      setInterestTitle((current) => current || next.interestTitle || '');
      setInterestQuery((current) => current || next.interestQuery || '');
    } catch (err: any) {
      console.error('[HorizonWatchLensSurface] Failed to load watch page', err);
      setError(err?.response?.data || err?.message || 'Failed to load watch page');
    } finally {
      setLoading(false);
    }
  }, [lensId]);

  React.useEffect(() => {
    void loadSnapshot();
  }, [loadSnapshot]);

  const handleRefresh = React.useCallback(async () => {
    if (!lensId) {
      return;
    }
    setRefreshing(true);
    setError(null);
    try {
      const next = await api.refreshHorizonWatchPage({
        lensId,
        timeHorizon,
      });
      setSnapshot(next);
    } catch (err: any) {
      console.error('[HorizonWatchLensSurface] Failed to refresh watch page', err);
      setError(err?.response?.data || err?.message || 'Failed to refresh lens watch surface');
    } finally {
      setRefreshing(false);
    }
  }, [lensId, timeHorizon]);

  const handleSearch = React.useCallback(async () => {
    const query = watchQuery.trim();
    if (!query) {
      setSearchResults([]);
      return;
    }
    setSearchBusy(true);
    try {
      const results = await api.searchHorizonWatchTargets(query, 8);
      setSearchResults(results);
    } catch (err) {
      console.error('[HorizonWatchLensSurface] Failed to search watch targets', err);
      setSearchResults([]);
    } finally {
      setSearchBusy(false);
    }
  }, [watchQuery]);

  const handleAddTarget = React.useCallback(async (result: HorizonWatchSearchResult) => {
    const resolvedInterestQuery = interestQuery.trim();
    setAddingTarget(result.canonicalPath);
    setError(null);
    try {
      await api.createHorizonWatchTarget({
        kind: result.kind,
        label: result.label,
        canonicalPath: result.canonicalPath,
        fragmentId: result.fragmentId,
        interestTitle: interestTitle.trim() || undefined,
        interestQuery: resolvedInterestQuery || undefined,
        queryOverrides: resolvedInterestQuery ? [resolvedInterestQuery] : undefined,
        priority: 70,
      });
      setWatchQuery('');
      setSearchResults([]);
      await handleRefresh();
    } catch (err: any) {
      console.error('[HorizonWatchLensSurface] Failed to create watch target', err);
      setError(err?.response?.data || err?.message || 'Failed to add watch target');
    } finally {
      setAddingTarget(null);
    }
  }, [handleRefresh, interestQuery, interestTitle]);

  const tone = statusTone(snapshot?.buildStatus);
  const lensLabel = snapshot?.lensName || projectName || lensId;
  const scopeSummary = snapshot?.sectionEnrichments?.scope?.content;
  const productSummary = snapshot?.sectionEnrichments?.products?.content;
  const supplyPathSummary = snapshot?.sectionEnrichments?.supply_paths?.content;
  const riskSummary = snapshot?.sectionEnrichments?.what_is_at_risk?.content;
  const affectedSummary = snapshot?.sectionEnrichments?.affected?.content;
  const directSummary = snapshot?.sectionEnrichments?.direct_effects?.content;
  const indirectSummary = snapshot?.sectionEnrichments?.indirect_effects?.content;
  const signalSummary = snapshot?.sectionEnrichments?.signals?.content;
  const scenarioSummary = snapshot?.sectionEnrichments?.scenarios?.content;
  const isNotBuilt = snapshot?.buildStatus === 'NOT_BUILT';

  if (loading && !snapshot) {
    return (
      <div
        className="flex h-full items-center justify-center px-6 py-6 text-sm text-slate-400"
        style={{
          background:
            'radial-gradient(circle at top left, rgba(250, 204, 21, 0.12), transparent 24%), radial-gradient(circle at top right, rgba(45, 212, 191, 0.12), transparent 24%), linear-gradient(180deg, #020617 0%, #020817 100%)',
        }}
      >
        Loading lens watch surface...
      </div>
    );
  }

  return (
    <div
      className="h-full overflow-y-auto px-6 py-6 text-slate-100"
      style={{
        background:
          'radial-gradient(circle at top left, rgba(250, 204, 21, 0.12), transparent 24%), radial-gradient(circle at top right, rgba(45, 212, 191, 0.12), transparent 24%), linear-gradient(180deg, #020617 0%, #020817 100%)',
      }}
    >
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 pb-10">
        <section className="overflow-hidden p-7" style={panelStyle}>
          <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
            <div className="max-w-3xl">
              <div className="mb-3 flex flex-wrap items-center gap-3">
                <span
                  className="rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em]"
                  style={{ color: tone.color, background: tone.bg }}
                >
                  {tone.label}
                </span>
                <span className="text-xs uppercase tracking-[0.14em] text-slate-400">Lens teaser surface</span>
                <span className="text-xs text-slate-500">Fresh {formatTimestamp(snapshot?.freshAt)}</span>
                <span className="text-xs text-slate-500">Stale after {formatTimestamp(snapshot?.staleAfterAt)}</span>
              </div>
              <div className="text-sm uppercase tracking-[0.18em] text-amber-200">
                {snapshot?.hero?.eyebrow || 'External teaser surface'}
              </div>
              <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-50 lg:text-5xl">
                {snapshot?.hero?.title || lensLabel}
              </h1>
              <p className="mt-4 max-w-3xl text-base leading-7 text-slate-300 lg:text-lg">
                {snapshot?.hero?.summary || 'Build this lens watch page to expose the current risk picture.'}
              </p>
              {snapshot?.hero?.whyNow ? (
                <p className="mt-4 max-w-3xl text-sm leading-7 text-slate-400">{snapshot.hero.whyNow}</p>
              ) : null}
              <div className="mt-5 flex flex-wrap gap-3">
                <span className="rounded-full border border-slate-700/80 bg-slate-950/40 px-3 py-1.5 text-xs text-slate-300">
                  Lens: {lensLabel}
                </span>
                <span className="rounded-full border border-slate-700/80 bg-slate-950/40 px-3 py-1.5 text-xs text-slate-300">
                  Horizon: {snapshot?.hero?.timeHorizon || timeHorizon}
                </span>
                {snapshot?.interestQuery ? (
                  <span className="rounded-full border border-slate-700/80 bg-slate-950/40 px-3 py-1.5 text-xs text-slate-300">
                    Focus: {snapshot.interestQuery}
                  </span>
                ) : null}
              </div>
            </div>

            <div
              className="w-full max-w-md rounded-3xl p-5"
              style={{
                border: '1px solid rgba(148, 163, 184, 0.18)',
                background: 'rgba(15, 23, 42, 0.72)',
              }}
            >
              <div style={sectionTitleStyle}>Refresh</div>
              {isAdmin ? (
                <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
                  <label className="flex flex-col gap-2 text-sm text-slate-300">
                    <span>Time horizon</span>
                    <select
                      value={timeHorizon}
                      onChange={(event) => setTimeHorizon(event.target.value)}
                      className="rounded-2xl border border-slate-700 bg-slate-950/70 px-4 py-3 text-sm text-slate-100 outline-none"
                    >
                      {timeHorizonOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <button
                    type="button"
                    onClick={() => void handleRefresh()}
                    disabled={refreshing}
                    className="rounded-2xl px-5 py-3 text-sm font-semibold"
                    style={{
                      alignSelf: 'end',
                      background: refreshing ? 'rgba(71, 85, 105, 0.6)' : 'linear-gradient(135deg, #f59e0b 0%, #f97316 100%)',
                      color: '#fff7ed',
                      opacity: refreshing ? 0.75 : 1,
                    }}
                  >
                    {refreshing ? 'Refreshing...' : isNotBuilt ? 'Build surface' : 'Refresh'}
                  </button>
                </div>
              ) : (
                <div className="rounded-2xl border border-slate-700 bg-slate-950/45 px-4 py-4 text-sm leading-6 text-slate-300">
                  {isNotBuilt
                    ? 'This lens brief has not been built yet. An admin can build the first persisted brief from this tab.'
                    : 'This brief is rendered from a persisted lens snapshot. Refresh is limited to admins.'}
                </div>
              )}
              {snapshot?.buildError ? <p className="mt-3 text-sm text-rose-300">{snapshot.buildError}</p> : null}
              {error ? <p className="mt-3 text-sm text-rose-300">{error}</p> : null}
            </div>
          </div>
        </section>

        <section className="grid gap-6 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.9fr)]">
          <div className="space-y-6">
            {isAdmin ? (
              <div className="p-6" style={panelStyle}>
                <div style={sectionTitleStyle}>Watch Subjects</div>
                <div className="grid gap-4 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
                  <div className="space-y-3">
                    <label className="block text-sm text-slate-300">
                      <span className="mb-2 block">What object or geography should be watched?</span>
                      <input
                        value={watchQuery}
                        onChange={(event) => setWatchQuery(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') {
                            event.preventDefault();
                            void handleSearch();
                          }
                        }}
                        placeholder="Search an entity, corridor, geography, or market"
                        className="w-full rounded-2xl border border-slate-700 bg-slate-950/70 px-4 py-3 text-sm text-slate-100 outline-none"
                      />
                    </label>
                    <button
                      type="button"
                      onClick={() => void handleSearch()}
                      disabled={searchBusy}
                      className="rounded-2xl border border-slate-700 bg-slate-900/80 px-4 py-2.5 text-sm font-medium text-slate-100"
                    >
                      {searchBusy ? 'Searching...' : 'Find watch subject'}
                    </button>
                  </div>

                  <div className="space-y-3">
                    <label className="block text-sm text-slate-300">
                      <span className="mb-2 block">Interest title</span>
                      <input
                        value={interestTitle}
                        onChange={(event) => setInterestTitle(event.target.value)}
                        placeholder="Optional short title for the teaser framing"
                        className="w-full rounded-2xl border border-slate-700 bg-slate-950/70 px-4 py-3 text-sm text-slate-100 outline-none"
                      />
                    </label>
                    <label className="block text-sm text-slate-300">
                      <span className="mb-2 block">What exactly are we watching for?</span>
                      <textarea
                        value={interestQuery}
                        onChange={(event) => setInterestQuery(event.target.value)}
                        rows={4}
                        placeholder="Example: export controls, shipping delays, insurance pressure, and the entities most exposed"
                        className="w-full rounded-2xl border border-slate-700 bg-slate-950/70 px-4 py-3 text-sm text-slate-100 outline-none"
                      />
                    </label>
                  </div>
                </div>
                <div className="mt-5 grid gap-3">
                  {searchResults.length === 0 ? (
                    <div className="rounded-3xl border border-dashed border-slate-700 bg-slate-950/30 px-4 py-5 text-sm text-slate-400">
                      Search results will appear here. Creating a watch subject from this panel will use the interest framing above and then rebuild the lens surface.
                    </div>
                  ) : (
                    searchResults.map((result) => (
                      <div
                        key={`${result.kind}:${result.canonicalPath}`}
                        className="flex flex-col gap-3 rounded-3xl p-4 lg:flex-row lg:items-center lg:justify-between"
                        style={{
                          border: '1px solid rgba(148, 163, 184, 0.16)',
                          background: 'rgba(15, 23, 42, 0.72)',
                        }}
                      >
                        <div>
                          <div className="text-sm font-semibold text-slate-100">{result.label}</div>
                          <div className="mt-1 text-xs uppercase tracking-[0.12em] text-slate-500">{result.kind}</div>
                          <div className="mt-2 text-xs text-slate-400">{result.canonicalPath}</div>
                        </div>
                        <button
                          type="button"
                          onClick={() => void handleAddTarget(result)}
                          disabled={addingTarget === result.canonicalPath}
                          className="rounded-2xl px-4 py-2.5 text-sm font-semibold"
                          style={{
                            background:
                              addingTarget === result.canonicalPath
                                ? 'rgba(71, 85, 105, 0.6)'
                                : 'linear-gradient(135deg, #14b8a6 0%, #0f766e 100%)',
                            color: '#f0fdfa',
                          }}
                        >
                          {addingTarget === result.canonicalPath ? 'Adding...' : 'Add to watch'}
                        </button>
                      </div>
                    ))
                  )}
                </div>
              </div>
            ) : null}

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Anchors And Reach</div>
              {scopeSummary ? <p className="mb-5 text-sm leading-7 text-slate-300">{scopeSummary}</p> : null}
              <div className="grid gap-6 xl:grid-cols-2">
                <div>
                  <div className="mb-3 text-xs uppercase tracking-[0.14em] text-slate-500">Anchors</div>
                  <div className="grid gap-3">
                    {(snapshot?.anchors || []).map((item) => (
                      <ScopeCard key={item.id} item={item} />
                    ))}
                  </div>
                </div>
                <div>
                  <div className="mb-3 text-xs uppercase tracking-[0.14em] text-slate-500">Reach</div>
                  <div className="grid gap-3">
                    {(snapshot?.reach || []).map((item) => (
                      <ScopeCard key={item.id} item={item} />
                    ))}
                  </div>
                </div>
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Products And Paths</div>
              {productSummary ? <p className="mb-4 text-sm leading-7 text-slate-300">{productSummary}</p> : null}
              {supplyPathSummary ? <p className="mb-5 text-sm leading-7 text-slate-400">{supplyPathSummary}</p> : null}
              <div className="grid gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
                <div>
                  <div className="mb-3 text-xs uppercase tracking-[0.14em] text-slate-500">Products</div>
                  <div className="grid gap-3">
                    {(snapshot?.products || []).map((item) => (
                      <ScopeCard key={item.id} item={item} />
                    ))}
                  </div>
                </div>
                <div>
                  <div className="mb-3 text-xs uppercase tracking-[0.14em] text-slate-500">Paths</div>
                  <div className="grid gap-3">
                    {(snapshot?.supplyPaths || []).map((item) => (
                      <SupplyPathCard key={item.id} item={item} />
                    ))}
                  </div>
                </div>
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>What Is At Risk</div>
              {riskSummary ? <p className="mb-5 text-sm leading-7 text-slate-300">{riskSummary}</p> : null}
              <div className="grid gap-4 xl:grid-cols-2">
                {(snapshot?.whatIsAtRisk || []).map((item) => (
                  <RiskCard key={item.id} item={item} />
                ))}
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Dashboard Board</div>
              <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                {(snapshot?.dashboards || []).map((metric) => (
                  <MetricCard key={metric.id} metric={metric} />
                ))}
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Map Fragments</div>
              <div className="grid gap-4 xl:grid-cols-2">
                {(snapshot?.mapFragments || []).map((fragment) => (
                  <MapFragmentCard key={fragment.id} fragment={fragment} onOpenWorldMap={onOpenWorldMap} />
                ))}
              </div>
            </div>
          </div>

          <div className="space-y-6">
            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Who Or What Is Affected</div>
              {affectedSummary ? <p className="mb-5 text-sm leading-7 text-slate-300">{affectedSummary}</p> : null}
              <div className="grid gap-6 xl:grid-cols-2">
                <div>
                  <div className="mb-3 text-xs uppercase tracking-[0.14em] text-slate-500">Directly affected</div>
                  {directSummary ? <p className="mb-4 text-sm leading-7 text-slate-300">{directSummary}</p> : null}
                  <div className="space-y-3">
                    {(snapshot?.directlyAffected || snapshot?.affected || []).map((item) => (
                      <AffectedCard key={item.id} item={item} />
                    ))}
                  </div>
                </div>
                <div>
                  <div className="mb-3 text-xs uppercase tracking-[0.14em] text-slate-500">Indirectly affected</div>
                  {indirectSummary ? <p className="mb-4 text-sm leading-7 text-slate-300">{indirectSummary}</p> : null}
                  <div className="space-y-3">
                    {(snapshot?.indirectlyAffected || []).map((item) => (
                      <AffectedCard key={item.id} item={item} />
                    ))}
                  </div>
                </div>
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Signals And Events</div>
              {signalSummary ? <p className="mb-5 text-sm leading-7 text-slate-300">{signalSummary}</p> : null}
              <div className="space-y-3">
                {(snapshot?.signals || []).map((item) => (
                  <SignalCard key={item.id} item={item} />
                ))}
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Scenarios</div>
              {scenarioSummary ? <p className="mb-5 text-sm leading-7 text-slate-300">{scenarioSummary}</p> : null}
              <div className="space-y-3">
                {(snapshot?.scenarios || []).map((item) => (
                  <ScenarioCard key={item.id} item={item} />
                ))}
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Evidence Rail</div>
              <div className="space-y-3">
                {(snapshot?.evidence || []).map((item) => (
                  <EvidenceCard key={item.id} item={item} />
                ))}
              </div>
            </div>

            <div className="p-6" style={panelStyle}>
              <div style={sectionTitleStyle}>Framing</div>
              <div className="space-y-4 text-sm leading-7 text-slate-300">
                <p>
                  This page is rendered from a persisted lens snapshot, so it stays readable and public-safe later without live access to internal watch runtime state.
                </p>
                <div>
                  <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Current query overrides</div>
                  {renderListChips(snapshot?.queryOverrides, 'No custom query overrides yet.')}
                </div>
                <div>
                  <div className="mb-2 text-xs uppercase tracking-[0.12em] text-slate-500">Build status</div>
                  <p className="text-sm text-slate-300">
                    {snapshot?.buildStatus === 'NOT_BUILT'
                      ? 'Refresh will assemble the first teaser snapshot for this lens.'
                      : snapshot?.buildStatus === 'STALE'
                        ? 'The snapshot is readable but should be rebuilt to stay aligned with the latest watch data.'
                        : 'The page is serving a persisted lens snapshot and can be refreshed on demand.'}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
};

export default HorizonWatchLensSurface;
