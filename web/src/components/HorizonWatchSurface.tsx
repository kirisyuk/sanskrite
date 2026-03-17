import React from 'react';
import { api } from '../lib/api';
import type {
  HorizonWatchBrief,
  HorizonWatchDashboardWidget,
  HorizonWatchMapFragment,
  HorizonWatchMonitoringSnapshot,
  HorizonWatchScenarioPreset,
  HorizonWatchScenarioWorkspace,
  HorizonWatchSnapshot,
} from '../types/horizonWatch';

type HorizonWatchSurfaceProps = {
  lensId?: string | null;
  projectName?: string | null;
  onOpenWorldMap?: () => void;
};

type BriefEvidenceState = {
  brief: HorizonWatchBrief | null;
  evidence: any[];
  paths: string[];
  sources: string[];
};

type ScenarioComparisonBranch = {
  contract: HorizonWatchScenarioPreset;
  result?: any;
  error?: string;
};

type ServiceLine = 'geo' | 'monitoring' | 'scenario';
type WatchMode = 'global' | 'my_watch' | 'combined';

const pillTone: Record<string, string> = {
  critical: '#ef4444',
  elevated: '#f59e0b',
  watch: '#facc15',
  calm: '#34d399',
  neutral: '#94a3b8',
  accent: '#60a5fa',
};

const serviceLineTheme: Record<ServiceLine, {
  title: string;
  eyebrow: string;
  description: string;
  badgeColor: string;
  badgeBg: string;
  panelGlow: string;
  background: string;
}> = {
  geo: {
    title: 'Geopolitical Risks Overview',
    eyebrow: 'Executive briefing mode',
    description:
      'A panoramic risk picture with brief-led interpretation, corridor pressure, geography, and exposed targets. This mode should read like a board-grade intelligence briefing, not a feed.',
    badgeColor: '#fde68a',
    badgeBg: 'rgba(250,204,21,0.14)',
    panelGlow: 'radial-gradient(circle at top right, rgba(250,204,21,0.14), transparent 32%)',
    background:
      'radial-gradient(circle at top left, rgba(96,165,250,0.16), transparent 24%), radial-gradient(circle at top right, rgba(251,191,36,0.14), transparent 24%), linear-gradient(180deg, rgba(2,6,23,0.96) 0%, rgba(3,7,18,1) 100%)',
  },
  monitoring: {
    title: 'Deep Monitoring',
    eyebrow: 'Persistent intelligence watch',
    description:
      'A dense interpreted monitoring surface for watched scopes, attention subjects, new deltas, propagation pressure, and live movement inside the watch layer.',
    badgeColor: '#99f6e4',
    badgeBg: 'rgba(45,212,191,0.14)',
    panelGlow: 'radial-gradient(circle at top right, rgba(45,212,191,0.14), transparent 32%)',
    background:
      'radial-gradient(circle at top left, rgba(20,184,166,0.16), transparent 24%), radial-gradient(circle at top right, rgba(96,165,250,0.14), transparent 24%), linear-gradient(180deg, rgba(2,6,23,0.96) 0%, rgba(4,12,20,1) 100%)',
  },
  scenario: {
    title: 'Scenario Planning',
    eyebrow: 'What-if workspace',
    description:
      'A scenario workspace over the same Horizon semantics: baseline, branches, triggers, disconfirmers, and projected impact surfaces across supply, governance, and regulatory pressure.',
    badgeColor: '#fdba74',
    badgeBg: 'rgba(251,146,60,0.14)',
    panelGlow: 'radial-gradient(circle at top right, rgba(251,146,60,0.14), transparent 32%)',
    background:
      'radial-gradient(circle at top left, rgba(251,146,60,0.16), transparent 24%), radial-gradient(circle at top right, rgba(248,113,113,0.14), transparent 24%), linear-gradient(180deg, rgba(8,7,18,0.96) 0%, rgba(10,8,15,1) 100%)',
  },
};

const basePanelStyle: React.CSSProperties = {
  borderRadius: 24,
  border: '1px solid rgba(255,255,255,0.08)',
  background: 'linear-gradient(180deg, rgba(17,24,39,0.88) 0%, rgba(9,14,27,0.94) 100%)',
  boxShadow: '0 20px 60px rgba(2,6,23,0.28)',
  backdropFilter: 'blur(10px)',
};

function formatPct(value?: number): string {
  return `${Math.round((value || 0) * 100)}%`;
}

function formatDate(value?: string): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleString();
}

function pickVisibleBriefs(snapshot: HorizonWatchSnapshot | null): HorizonWatchBrief[] {
  if (!snapshot) return [];
  const source = snapshot.mode === 'global'
    ? (snapshot.globalHotSlices || [])
    : snapshot.mode === 'my_watch'
      ? (snapshot.myWatch || [])
      : [...(snapshot.myWatch || []), ...(snapshot.globalHotSlices || [])];
  const seen = new Set<string>();
  return source.filter((brief) => {
    const key = String(brief?.id || brief?.targetId || '').trim();
    if (!key || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function buildSyntheticBriefEvidence(brief: HorizonWatchBrief): BriefEvidenceState {
  const metadata = brief.metadata || {};
  return {
    brief,
    evidence: Array.isArray(metadata.localEvidence) ? metadata.localEvidence : [],
    paths: Array.isArray(metadata.artifactPaths) ? metadata.artifactPaths.filter(Boolean) : [brief.canonicalPath].filter(Boolean),
    sources: Array.isArray(metadata.localSources) ? metadata.localSources.filter(Boolean) : [],
  };
}

function activateByKeyboard(
  event: React.KeyboardEvent<HTMLElement>,
  handler: () => void,
): void {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    handler();
  }
}

function renderWidgetTile(widget: HorizonWatchDashboardWidget): React.ReactNode {
  return (
    <div
      key={widget.id}
      style={{
        borderRadius: 18,
        padding: 14,
        background: 'rgba(255,255,255,0.04)',
        border: '1px solid rgba(255,255,255,0.08)',
      }}
    >
      <div style={{ fontSize: 11, color: 'rgba(148,163,184,0.9)', textTransform: 'uppercase', letterSpacing: 0.5 }}>
        {widget.title}
      </div>
      <div style={{ fontSize: 28, fontWeight: 800, marginTop: 8, color: pillTone[widget.tone || 'neutral'] || 'var(--text-heading)' }}>
        {widget.value}
      </div>
      <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginTop: 6 }}>{widget.subtitle}</div>
    </div>
  );
}

function renderMapCard(
  fragment: HorizonWatchMapFragment,
  onOpenWorldMap?: () => void,
): React.ReactNode {
  return (
    <div
      key={fragment.id}
      style={{
        borderRadius: 20,
        border: '1px solid rgba(255,255,255,0.08)',
        background:
          'linear-gradient(180deg, rgba(30,41,59,0.72), rgba(15,23,42,0.84)), radial-gradient(circle at top right, rgba(250,204,21,0.10), transparent 40%)',
        padding: 16,
        minHeight: 192,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'space-between',
      }}
    >
      <div>
        <div style={{ fontSize: 11, color: 'rgba(148,163,184,0.9)', textTransform: 'uppercase' }}>
          {(fragment.layerHints || []).join(' · ') || 'map fragment'}
        </div>
        <div style={{ fontSize: 18, fontWeight: 800, marginTop: 8 }}>{fragment.title}</div>
        <div style={{ fontSize: 13, lineHeight: 1.6, color: 'rgba(226,232,240,0.78)', marginTop: 8 }}>
          {fragment.caption || 'Geographic context for exposed routes, zones, and pressure propagation.'}
        </div>
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', marginTop: 16 }}>
        <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
          {(fragment.canonicalPaths || []).slice(0, 2).join(' • ')}
        </span>
        {onOpenWorldMap ? (
          <button
            type="button"
            onClick={onOpenWorldMap}
            style={{
              borderRadius: 12,
              border: '1px solid rgba(96,165,250,0.18)',
              background: 'rgba(96,165,250,0.12)',
              color: '#dbeafe',
              padding: '8px 10px',
              fontSize: 12,
              fontWeight: 700,
            }}
          >
            Open map
          </button>
        ) : null}
      </div>
    </div>
  );
}

export const HorizonWatchSurface: React.FC<HorizonWatchSurfaceProps> = ({
  lensId,
  projectName,
  onOpenWorldMap,
}) => {
  const [snapshot, setSnapshot] = React.useState<HorizonWatchSnapshot | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [mode, setMode] = React.useState<WatchMode>('combined');
  const [serviceLine, setServiceLine] = React.useState<ServiceLine>('geo');
  const [timeHorizon, setTimeHorizon] = React.useState('7d');
  const [watchQuery, setWatchQuery] = React.useState('');
  const [searchResults, setSearchResults] = React.useState<Array<{ kind: string; label: string; canonicalPath: string; fragmentId?: string }>>([]);
  const [isSearchBusy, setIsSearchBusy] = React.useState(false);
  const [selectedBriefId, setSelectedBriefId] = React.useState<string | null>(null);
  const [detailTab, setDetailTab] = React.useState<'summary' | 'evidence' | 'paths' | 'sources'>('summary');
  const [briefEvidence, setBriefEvidence] = React.useState<BriefEvidenceState | null>(null);
  const [scenarioBusyId, setScenarioBusyId] = React.useState<string | null>(null);
  const [scenarioResults, setScenarioResults] = React.useState<Record<string, any>>({});
  const [scenarioComparison, setScenarioComparison] = React.useState<ScenarioComparisonBranch[] | null>(null);
  const [scenarioCompareBusy, setScenarioCompareBusy] = React.useState(false);
  const [scenarioWorkspaceOverride, setScenarioWorkspaceOverride] = React.useState<HorizonWatchScenarioWorkspace | null>(null);
  const [scenarioWorkspaceBusy, setScenarioWorkspaceBusy] = React.useState(false);
  const [selectedScenarioId, setSelectedScenarioId] = React.useState<string | null>(null);
  const [selectedScenarioDetail, setSelectedScenarioDetail] = React.useState<HorizonWatchScenarioPreset | null>(null);
  const [scenarioDetailBusy, setScenarioDetailBusy] = React.useState(false);
  const prefsHydratedRef = React.useRef(false);
  const prefsSaveTimerRef = React.useRef<number | null>(null);
  const autoComparedWorkspaceRef = React.useRef<string>('');

  const refreshSnapshot = React.useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const next = await api.getHorizonWatchSnapshot({
        mode,
        lensId,
        serviceLine,
        timeHorizon,
      });
      setSnapshot(next);
      if (!prefsHydratedRef.current) {
        if (next.preferences) {
          const nextMode = next.preferences.mode as WatchMode | undefined;
          if (nextMode === 'global' || nextMode === 'my_watch' || nextMode === 'combined') {
            setMode(nextMode);
          }
          if (next.preferences.timeHorizon) {
            setTimeHorizon(next.preferences.timeHorizon);
          }
          const preferredServiceLine = String(next.preferences.filters?.serviceLine || '').trim();
          if (preferredServiceLine === 'geo' || preferredServiceLine === 'monitoring' || preferredServiceLine === 'scenario') {
            setServiceLine(preferredServiceLine);
          }
        }
        prefsHydratedRef.current = true;
      }
    } catch (err: any) {
      console.error('[HorizonWatchSurface] Failed to load watch snapshot', err);
      setError(err?.response?.data || err?.message || 'Failed to load Horizon Watch');
    } finally {
      setLoading(false);
    }
  }, [lensId, mode, serviceLine, timeHorizon]);

  React.useEffect(() => {
    void refreshSnapshot();
  }, [refreshSnapshot]);

  React.useEffect(() => {
    const intervalId = window.setInterval(() => {
      void refreshSnapshot();
    }, 60000);
    const handleVisibility = () => {
      if (document.visibilityState === 'visible') {
        void refreshSnapshot();
      }
    };
    document.addEventListener('visibilitychange', handleVisibility);
    return () => {
      window.clearInterval(intervalId);
      document.removeEventListener('visibilitychange', handleVisibility);
    };
  }, [refreshSnapshot]);

  React.useEffect(() => {
    if (!prefsHydratedRef.current) return;
    if (prefsSaveTimerRef.current) {
      window.clearTimeout(prefsSaveTimerRef.current);
    }
    prefsSaveTimerRef.current = window.setTimeout(() => {
      void api.updateHorizonWatchPreferences({
        mode,
        timeHorizon,
        filters: { serviceLine },
      }).catch((err) => {
        console.error('[HorizonWatchSurface] Failed to save watch preferences', err);
      });
    }, 250);
    return () => {
      if (prefsSaveTimerRef.current) {
        window.clearTimeout(prefsSaveTimerRef.current);
      }
    };
  }, [mode, serviceLine, timeHorizon]);

  React.useEffect(() => {
    if (watchQuery.trim().length < 2) {
      setSearchResults([]);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      setIsSearchBusy(true);
      try {
        const results = await api.searchHorizonWatchTargets(watchQuery.trim(), 8);
        if (!cancelled) {
          setSearchResults(results);
        }
      } catch (err) {
        console.error('[HorizonWatchSurface] Failed to search watch targets', err);
      } finally {
        if (!cancelled) {
          setIsSearchBusy(false);
        }
      }
    }, 220);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [watchQuery]);

  const briefIndex = React.useMemo(() => {
    const index = new Map<string, HorizonWatchBrief>();
    [...(snapshot?.myWatch || []), ...(snapshot?.globalHotSlices || [])].forEach((brief) => {
      const id = String(brief?.id || '').trim();
      if (id && !index.has(id)) {
        index.set(id, brief);
      }
    });
    return index;
  }, [snapshot?.globalHotSlices, snapshot?.myWatch]);

  React.useEffect(() => {
    if (!selectedBriefId) {
      setBriefEvidence(null);
      return;
    }
    const localBrief = briefIndex.get(selectedBriefId) || null;
    if (localBrief?.metadata?.syntheticArtifact) {
      setBriefEvidence(buildSyntheticBriefEvidence(localBrief));
      return;
    }
    let cancelled = false;
    const loadDetail = async () => {
      try {
        const [brief, evidencePayload, pathsPayload, sourcesPayload] = await Promise.all([
          api.getHorizonWatchBrief(selectedBriefId),
          api.getHorizonWatchBriefEvidence(selectedBriefId),
          api.getHorizonWatchBriefPaths(selectedBriefId),
          api.getHorizonWatchBriefSources(selectedBriefId),
        ]);
        if (!cancelled) {
          setBriefEvidence({
            brief,
            evidence: evidencePayload.evidence || [],
            paths: pathsPayload.canonicalPaths || [],
            sources: sourcesPayload.sources || [],
          });
        }
      } catch (err) {
        console.error('[HorizonWatchSurface] Failed to load brief detail', err);
      }
    };
    void loadDetail();
    return () => {
      cancelled = true;
    };
  }, [briefIndex, selectedBriefId]);

  React.useEffect(() => {
    if (serviceLine !== 'scenario') return;
    let cancelled = false;
    setScenarioWorkspaceBusy(true);
    void api.getHorizonWatchScenarioWorkspace({
      mode,
      lensId,
      timeHorizon,
    }).then((workspace) => {
      if (!cancelled) {
        setScenarioWorkspaceOverride(workspace);
      }
    }).catch((err) => {
      console.error('[HorizonWatchSurface] Failed to load scenario workspace', err);
      if (!cancelled) {
        setScenarioWorkspaceOverride(null);
      }
    }).finally(() => {
      if (!cancelled) {
        setScenarioWorkspaceBusy(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [lensId, mode, serviceLine, timeHorizon]);

  const visibleBriefs = React.useMemo(() => pickVisibleBriefs(snapshot), [snapshot]);
  const liveFeed = React.useMemo(() => {
    if (snapshot?.lensAttention?.feed?.length) {
      return snapshot.lensAttention.feed;
    }
    return snapshot?.feed || [];
  }, [snapshot]);
  const myWatchTargetIds = React.useMemo(() => {
    return new Set((snapshot?.myWatch || []).map((brief) => String(brief.targetId || brief.id || '').trim()).filter(Boolean));
  }, [snapshot]);
  const scenarioWorkspace = React.useMemo(
    () => scenarioWorkspaceOverride || snapshot?.scenarioWorkspace || null,
    [scenarioWorkspaceOverride, snapshot?.scenarioWorkspace],
  );
  const selectedScenarioPreset = React.useMemo(() => {
    const contracts = scenarioWorkspace?.contracts || [];
    if (selectedScenarioDetail?.id) {
      const detailed = contracts.find((contract) => contract.id === selectedScenarioDetail.id);
      if (detailed) {
        return { ...detailed, ...selectedScenarioDetail };
      }
    }
    return contracts.find((contract) => contract.id === selectedScenarioId) || selectedScenarioDetail;
  }, [scenarioWorkspace?.contracts, selectedScenarioDetail, selectedScenarioId]);
  const primaryBrief = visibleBriefs[0] || null;
  const theme = serviceLineTheme[serviceLine];
  const scopeTitle = lensId
    ? `Lens Watch${snapshot?.lensName ? ` · ${snapshot.lensName}` : ''}`
    : 'Global Watch';

  React.useEffect(() => {
    const contracts = scenarioWorkspace?.contracts || [];
    if (contracts.length === 0) {
      setSelectedScenarioId(null);
      setSelectedScenarioDetail(null);
      return;
    }
    setSelectedScenarioId((current) => {
      if (current && contracts.some((contract) => contract.id === current)) {
        return current;
      }
      return contracts[0].id;
    });
  }, [scenarioWorkspace]);

  React.useEffect(() => {
    if (serviceLine !== 'scenario' || !selectedScenarioId) return;
    let cancelled = false;
    setScenarioDetailBusy(true);
    void api.getHorizonWatchScenarioContractDetail(selectedScenarioId, {
      mode,
      lensId,
      timeHorizon,
    }).then((payload) => {
      if (!cancelled) {
        setSelectedScenarioDetail(payload.contract);
        setScenarioWorkspaceOverride(payload.workspace);
      }
    }).catch((err) => {
      console.error('[HorizonWatchSurface] Failed to load scenario contract detail', err);
      if (!cancelled) {
        const fallback = scenarioWorkspace?.contracts.find((contract) => contract.id === selectedScenarioId) || null;
        setSelectedScenarioDetail(fallback);
      }
    }).finally(() => {
      if (!cancelled) {
        setScenarioDetailBusy(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [lensId, mode, scenarioWorkspace?.contracts, selectedScenarioId, serviceLine, timeHorizon]);

  const handleAddWatchTarget = React.useCallback(async (result: { kind: string; label: string; canonicalPath: string; fragmentId?: string }) => {
    await api.createHorizonWatchTarget({
      kind: result.kind,
      label: result.label,
      canonicalPath: result.canonicalPath,
      fragmentId: result.fragmentId,
      priority: 70,
    });
    setWatchQuery('');
    setSearchResults([]);
    setMode('my_watch');
    await refreshSnapshot();
  }, [refreshSnapshot]);

  const handleRemoveWatchTarget = React.useCallback(async (targetId: string) => {
    if (!targetId) return;
    await api.deleteHorizonWatchTarget(targetId);
    const selectedBriefTargetId = String(briefEvidence?.brief?.targetId || '').trim();
    if (selectedBriefId === targetId || selectedBriefTargetId === targetId) {
      setSelectedBriefId(null);
    }
    await refreshSnapshot();
  }, [briefEvidence?.brief?.targetId, refreshSnapshot, selectedBriefId]);

  const handleRunScenario = React.useCallback(async (preset: HorizonWatchScenarioPreset) => {
    setScenarioBusyId(preset.id);
    try {
      const result = await api.runHorizonScenario({
        scenarioId: preset.id,
        contractType: preset.contractType,
        domainModes: preset.domainModes,
        mode: 'preview',
        parameters: preset.parameters || {},
        targetPaths: preset.targetPaths || [],
        seedFragmentIds: preset.seedFragmentIds || [],
        maxTraceSteps: 60,
      });
      setScenarioResults((current) => ({ ...current, [preset.id]: result }));
    } catch (err) {
      console.error('[HorizonWatchSurface] Failed to run scenario', err);
      setScenarioResults((current) => ({
        ...current,
        [preset.id]: { error: 'Scenario run failed. Check backend logs.' },
      }));
    } finally {
      setScenarioBusyId(null);
    }
  }, []);

  const handleCompareScenarios = React.useCallback(async () => {
    if (!scenarioWorkspace?.contracts?.length) return;
    setScenarioCompareBusy(true);
    try {
      const comparison = await api.compareHorizonWatchScenarios({
        mode,
        lensId,
        timeHorizon,
        contractIds: scenarioWorkspace.contracts.map((contract) => contract.id),
      });
      setScenarioComparison(comparison.branches || []);
      if (comparison.workspace) {
        setScenarioWorkspaceOverride(comparison.workspace);
      }
    } catch (err) {
      console.error('[HorizonWatchSurface] Failed to compare scenarios', err);
      setScenarioComparison([{
        contract: {
          id: 'compare',
          title: 'Scenario comparison failed',
          contractType: 'policy-impact',
        },
        error: 'Comparison failed. Check backend logs.',
      }]);
    } finally {
      setScenarioCompareBusy(false);
    }
  }, [lensId, mode, scenarioWorkspace, timeHorizon]);

  React.useEffect(() => {
    if (serviceLine !== 'scenario' || !scenarioWorkspace?.contracts?.length) return;
    const workspaceKey = [
      lensId || 'global',
      mode,
      timeHorizon,
      scenarioWorkspace.generatedAt,
      scenarioWorkspace.contracts.map((contract) => contract.id).join(','),
    ].join(':');
    if (autoComparedWorkspaceRef.current === workspaceKey) return;
    autoComparedWorkspaceRef.current = workspaceKey;
    void handleCompareScenarios();
  }, [handleCompareScenarios, lensId, mode, scenarioWorkspace, serviceLine, timeHorizon]);

  React.useEffect(() => {
    if (serviceLine !== 'scenario' || !selectedScenarioPreset) return;
    if (scenarioResults[selectedScenarioPreset.id] || scenarioBusyId === selectedScenarioPreset.id) return;
    void handleRunScenario(selectedScenarioPreset);
  }, [handleRunScenario, scenarioBusyId, scenarioResults, selectedScenarioPreset, serviceLine]);

  const openBrief = React.useCallback((briefId: string) => {
    setSelectedBriefId(briefId);
    setDetailTab('summary');
  }, []);

  const renderSearchPanel = () => (
    <section
      style={{
        ...basePanelStyle,
        ...({ background: `${theme.panelGlow}, ${basePanelStyle.background}` } as React.CSSProperties),
        padding: 20,
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', marginBottom: 12 }}>
        <div>
          <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: 0.6, color: 'rgba(148,163,184,0.9)' }}>
            Watch picker
          </div>
          <div style={{ fontSize: 16, fontWeight: 700 }}>Add countries, markets, corridors, or scoped fragments</div>
        </div>
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          {(snapshot?.myWatch || []).length} tracked
        </div>
      </div>
      <div style={{ position: 'relative' }}>
        <input
          value={watchQuery}
          onChange={(event) => setWatchQuery(event.target.value)}
          placeholder="Search countries, markets, corridors, fragments"
          style={{
            width: '100%',
            borderRadius: 18,
            border: '1px solid rgba(255,255,255,0.08)',
            background: 'rgba(15,23,42,0.9)',
            color: 'var(--text-heading)',
            padding: '14px 16px',
            fontSize: 14,
          }}
        />
        {watchQuery.trim().length >= 2 ? (
          <div
            style={{
              position: 'absolute',
              left: 0,
              right: 0,
              top: 'calc(100% + 10px)',
              zIndex: 20,
              ...basePanelStyle,
              padding: 10,
              display: 'flex',
              flexDirection: 'column',
              gap: 8,
            }}
          >
            {isSearchBusy ? (
              <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>Searching…</div>
            ) : searchResults.length === 0 ? (
              <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>No matching scope yet.</div>
            ) : (
              searchResults.map((result) => (
                <button
                  key={`${result.kind}:${result.canonicalPath}`}
                  type="button"
                  onClick={() => void handleAddWatchTarget(result)}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    gap: 12,
                    borderRadius: 14,
                    border: '1px solid rgba(255,255,255,0.06)',
                    background: 'rgba(255,255,255,0.03)',
                    color: 'var(--text-heading)',
                    padding: '10px 12px',
                    textAlign: 'left',
                  }}
                >
                  <span style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                    <span style={{ fontWeight: 700 }}>{result.label}</span>
                    <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{result.canonicalPath}</span>
                  </span>
                  <span style={{ fontSize: 11, color: theme.badgeColor, textTransform: 'uppercase' }}>{result.kind}</span>
                </button>
              ))
            )}
          </div>
        ) : null}
      </div>
      {(snapshot?.myWatch || []).length > 0 ? (
        <div style={{ marginTop: 14, display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {(snapshot?.myWatch || []).slice(0, 10).map((brief) => (
            <div
              key={`watch-chip-${brief.id}`}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                borderRadius: 999,
                border: '1px solid rgba(255,255,255,0.08)',
                background: 'rgba(255,255,255,0.04)',
                color: 'var(--text-heading)',
                padding: '8px 10px',
                fontSize: 12,
              }}
            >
              <button
                type="button"
                onClick={() => openBrief(brief.id)}
                style={{
                  border: 'none',
                  background: 'transparent',
                  color: 'inherit',
                  padding: 0,
                  fontSize: 12,
                  cursor: 'pointer',
                }}
              >
                {brief.label}
              </button>
              <button
                type="button"
                onClick={() => void handleRemoveWatchTarget(String(brief.targetId || brief.id || '').trim())}
                style={{
                  borderRadius: 999,
                  border: '1px solid rgba(239,68,68,0.16)',
                  background: 'rgba(239,68,68,0.10)',
                  color: '#fca5a5',
                  padding: '2px 7px',
                  fontSize: 11,
                  fontWeight: 700,
                }}
              >
                Remove
              </button>
            </div>
          ))}
        </div>
      ) : null}
    </section>
  );

  const renderBriefDetail = () => {
    if (!briefEvidence?.brief) return null;
    return (
      <section style={{ ...basePanelStyle, padding: 18 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', marginBottom: 12 }}>
          <div>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Drill-down
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>{briefEvidence.brief.headline}</div>
          </div>
          <button
            type="button"
            onClick={() => setSelectedBriefId(null)}
            style={{
              borderRadius: 12,
              border: '1px solid rgba(255,255,255,0.08)',
              background: 'rgba(255,255,255,0.04)',
              color: 'var(--text-secondary-strong)',
              padding: '8px 10px',
            }}
          >
            Close
          </button>
        </div>

        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
          {(['summary', 'evidence', 'paths', 'sources'] as const).map((tab) => {
            const active = detailTab === tab;
            return (
              <button
                key={tab}
                type="button"
                onClick={() => setDetailTab(tab)}
                style={{
                  borderRadius: 12,
                  border: active ? '1px solid rgba(96,165,250,0.22)' : '1px solid rgba(255,255,255,0.08)',
                  background: active ? 'rgba(96,165,250,0.12)' : 'rgba(255,255,255,0.03)',
                  color: active ? '#dbeafe' : 'var(--text-secondary-strong)',
                  padding: '8px 10px',
                  fontSize: 12,
                  fontWeight: 700,
                  textTransform: 'capitalize',
                }}
              >
                {tab}
              </button>
            );
          })}
        </div>

        {detailTab === 'summary' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, fontSize: 13, lineHeight: 1.7, color: 'rgba(226,232,240,0.82)' }}>
            <div>{briefEvidence.brief.summary}</div>
            <div>{briefEvidence.brief.explanation}</div>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', color: 'var(--text-secondary-strong)' }}>
              <span>Hotness {formatPct(briefEvidence.brief.hotness)}</span>
              <span>Confidence {formatPct(briefEvidence.brief.confidence)}</span>
              <span>{briefEvidence.brief.canonicalPath}</span>
            </div>
          </div>
        ) : null}

        {detailTab === 'evidence' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {briefEvidence.evidence.map((entry, index) => (
              <div
                key={`${entry.id || entry.type}-${index}`}
                style={{
                  borderRadius: 14,
                  border: '1px solid rgba(255,255,255,0.08)',
                  background: 'rgba(255,255,255,0.03)',
                  padding: 12,
                  fontSize: 12,
                  color: 'rgba(226,232,240,0.8)',
                }}
              >
                <div style={{ fontWeight: 700 }}>{entry.type || entry.proposalType || entry.kind}</div>
                <div style={{ marginTop: 6 }}>{entry.summary || entry.explanation || entry.title || entry.id}</div>
              </div>
            ))}
          </div>
        ) : null}

        {detailTab === 'paths' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {briefEvidence.paths.map((path) => (
              <div
                key={path}
                style={{
                  borderRadius: 12,
                  padding: '10px 12px',
                  background: 'rgba(255,255,255,0.03)',
                  border: '1px solid rgba(255,255,255,0.08)',
                  fontSize: 12,
                  color: '#bfdbfe',
                }}
              >
                {path}
              </div>
            ))}
          </div>
        ) : null}

        {detailTab === 'sources' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {briefEvidence.sources.map((source) => (
              <div
                key={source}
                style={{
                  borderRadius: 12,
                  padding: '10px 12px',
                  background: 'rgba(255,255,255,0.03)',
                  border: '1px solid rgba(255,255,255,0.08)',
                  fontSize: 12,
                  color: '#fde68a',
                }}
              >
                {source}
              </div>
            ))}
          </div>
        ) : null}
      </section>
    );
  };

  const renderBriefCards = (briefs: HorizonWatchBrief[], options?: { compact?: boolean; limit?: number }) => {
    const compact = options?.compact || false;
    const items = briefs.slice(0, options?.limit || briefs.length);
    return (
      <div style={{ display: 'grid', gridTemplateColumns: compact ? '1fr' : 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>
        {items.map((brief) => {
          const active = selectedBriefId === brief.id;
          const targetId = String(brief.targetId || brief.id || '').trim();
          return (
            <div
              key={brief.id}
              role="button"
              tabIndex={0}
              onClick={() => openBrief(brief.id)}
              onKeyDown={(event) => activateByKeyboard(event, () => openBrief(brief.id))}
              style={{
                textAlign: 'left',
                borderRadius: 20,
                border: active ? '1px solid rgba(96,165,250,0.28)' : '1px solid rgba(255,255,255,0.08)',
                background: active ? 'rgba(96,165,250,0.10)' : 'rgba(255,255,255,0.035)',
                padding: 16,
                color: 'var(--text-heading)',
                cursor: 'pointer',
                outline: 'none',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, marginBottom: 10, alignItems: 'center' }}>
                <span style={{ fontSize: 11, color: 'rgba(148,163,184,0.9)', textTransform: 'uppercase' }}>{brief.scopeType}</span>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ fontSize: 11, color: pillTone[brief.riskPosture || 'watch'] || '#fde68a' }}>
                    {brief.riskPosture || 'watch'}
                  </span>
                  {myWatchTargetIds.has(targetId) ? (
                    <button
                      type="button"
                      onClick={(event) => {
                        event.stopPropagation();
                        void handleRemoveWatchTarget(targetId);
                      }}
                      style={{
                        borderRadius: 10,
                        border: '1px solid rgba(239,68,68,0.16)',
                        background: 'rgba(239,68,68,0.10)',
                        color: '#fca5a5',
                        padding: '4px 8px',
                        fontSize: 11,
                        fontWeight: 700,
                      }}
                    >
                      Remove
                    </button>
                  ) : null}
                </div>
              </div>
              <div style={{ fontSize: compact ? 18 : 20, fontWeight: 800, lineHeight: 1.08, letterSpacing: -0.6, marginBottom: 8 }}>
                {brief.headline}
              </div>
              <div style={{ fontSize: 13, lineHeight: 1.6, color: 'rgba(226,232,240,0.82)', minHeight: compact ? 0 : 62 }}>
                {brief.summary}
              </div>
              {(brief.topAffectedTargets || []).length > 0 ? (
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 12 }}>
                  {(brief.topAffectedTargets || []).slice(0, compact ? 2 : 3).map((target) => (
                    <span
                      key={`${brief.id}-${target}`}
                      style={{
                        borderRadius: 999,
                        padding: '4px 8px',
                        fontSize: 11,
                        background: 'rgba(255,255,255,0.05)',
                        color: 'var(--text-secondary-strong)',
                      }}
                    >
                      {target}
                    </span>
                  ))}
                </div>
              ) : null}
              <div style={{ display: 'flex', gap: 14, marginTop: 14, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                <span>Hotness {formatPct(brief.hotness)}</span>
                <span>Confidence {formatPct(brief.confidence)}</span>
              </div>
            </div>
          );
        })}
      </div>
    );
  };

  const renderGeoLayout = () => (
    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.45fr) minmax(320px, 0.95fr)', gap: 18, alignItems: 'start' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18, minWidth: 0 }}>
        {primaryBrief ? (
          <section
            style={{
              ...basePanelStyle,
              padding: 24,
              background:
                'linear-gradient(180deg, rgba(17,24,39,0.92) 0%, rgba(9,14,27,0.96) 100%), radial-gradient(circle at top right, rgba(250,204,21,0.12), transparent 35%)',
            }}
          >
            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.25fr) minmax(260px, 0.75fr)', gap: 18 }}>
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: 0.6, color: theme.badgeColor, marginBottom: 10 }}>
                  Executive headline
                </div>
                <div style={{ fontSize: 34, lineHeight: 1.02, fontWeight: 900, letterSpacing: -1.4, marginBottom: 12 }}>
                  {primaryBrief.headline}
                </div>
                <div style={{ fontSize: 15, lineHeight: 1.8, color: 'rgba(226,232,240,0.86)' }}>
                  {primaryBrief.summary}
                </div>
                <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 16 }}>
                  <span style={{ fontSize: 12, color: pillTone[primaryBrief.riskPosture || 'watch'] || theme.badgeColor }}>
                    {primaryBrief.riskPosture || 'watch'}
                  </span>
                  <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    Hotness {formatPct(primaryBrief.hotness)}
                  </span>
                  <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    Confidence {formatPct(primaryBrief.confidence)}
                  </span>
                </div>
                <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', marginTop: 16 }}>
                  <button
                    type="button"
                    onClick={() => openBrief(primaryBrief.id)}
                    style={{
                      borderRadius: 14,
                      border: '1px solid rgba(96,165,250,0.18)',
                      background: 'rgba(96,165,250,0.12)',
                      color: '#dbeafe',
                      padding: '10px 12px',
                      fontWeight: 700,
                    }}
                  >
                    Open explanation
                  </button>
                  {onOpenWorldMap ? (
                    <button
                      type="button"
                      onClick={onOpenWorldMap}
                      style={{
                        borderRadius: 14,
                        border: '1px solid rgba(250,204,21,0.18)',
                        background: 'rgba(250,204,21,0.12)',
                        color: '#fde68a',
                        padding: '10px 12px',
                        fontWeight: 700,
                      }}
                    >
                      See map context
                    </button>
                  ) : null}
                </div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 10 }}>
                {(snapshot?.dashboards || []).slice(0, 4).map((widget) => renderWidgetTile(widget))}
              </div>
            </div>
          </section>
        ) : null}

        <section style={{ ...basePanelStyle, padding: 20 }}>
          <div style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Regional brief board
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>Top risk slices and exposed narratives</div>
          </div>
          {renderBriefCards(visibleBriefs.slice(0, 6))}
        </section>

        {(snapshot?.mapFragments || []).length > 0 ? (
          <section style={{ ...basePanelStyle, padding: 20 }}>
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                Risk map fragments
              </div>
              <div style={{ fontSize: 16, fontWeight: 700 }}>Where pressure becomes geographically legible</div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: 12 }}>
              {(snapshot?.mapFragments || []).slice(0, 4).map((fragment) => renderMapCard(fragment, onOpenWorldMap))}
            </div>
          </section>
        ) : null}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        {renderSearchPanel()}

        <section style={{ ...basePanelStyle, padding: 18 }}>
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Executive dashboards
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>Macro pressure, posture, and watch velocity</div>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 10 }}>
            {(snapshot?.dashboards || []).map((widget) => renderWidgetTile(widget))}
          </div>
        </section>

        <section style={{ ...basePanelStyle, padding: 18 }}>
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Impact rail
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>Who gets hit and through which dominant themes</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {visibleBriefs.slice(0, 5).map((brief) => (
              <div
                key={`impact-${brief.id}`}
                style={{
                  borderRadius: 16,
                  border: '1px solid rgba(255,255,255,0.08)',
                  background: 'rgba(255,255,255,0.03)',
                  padding: 14,
                }}
              >
                <div style={{ fontWeight: 700, fontSize: 13 }}>{brief.label}</div>
                <div style={{ marginTop: 8, fontSize: 12, color: 'rgba(226,232,240,0.8)', lineHeight: 1.6 }}>
                  {(brief.dominantThemes || []).slice(0, 3).join(' • ') || brief.summary}
                </div>
                {(brief.topAffectedTargets || []).length > 0 ? (
                  <div style={{ marginTop: 8, fontSize: 11, color: '#bfdbfe' }}>
                    {(brief.topAffectedTargets || []).slice(0, 3).join(' • ')}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        </section>

        {renderBriefDetail()}
      </div>
    </div>
  );

  const renderMonitoringLayout = () => (
    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(320px, 0.92fr) minmax(0, 1.08fr)', gap: 18, alignItems: 'start' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        {renderSearchPanel()}

        <section style={{ ...basePanelStyle, padding: 18 }}>
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Watch dashboards
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>Coverage, status, and interpreted movement</div>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 10 }}>
            {(snapshot?.dashboards || []).map((widget) => renderWidgetTile(widget))}
          </div>
        </section>

        {(snapshot?.lensAttention?.subjects || []).length > 0 ? (
          <section style={{ ...basePanelStyle, padding: 18 }}>
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                Lens attention
              </div>
              <div style={{ fontSize: 16, fontWeight: 700 }}>Subjects, trigger state, and coverage gaps</div>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {(snapshot?.lensAttention?.subjects || []).slice(0, 6).map((subject) => (
                <div
                  key={subject.subjectId}
                  style={{
                    borderRadius: 16,
                    border: '1px solid rgba(255,255,255,0.08)',
                    background: 'rgba(255,255,255,0.03)',
                    padding: 14,
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
                    <div>
                      <div style={{ fontWeight: 700 }}>{subject.displayLabel}</div>
                      <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                        {subject.subjectKind} • {subject.anchorCanonicalPath}
                      </div>
                    </div>
                    <div style={{ fontSize: 11, color: pillTone[subject.status || 'neutral'] || 'var(--text-secondary-strong)' }}>
                      {subject.status}
                    </div>
                  </div>
                  {(subject.domainModes || []).length > 0 ? (
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 10 }}>
                      {(subject.domainModes || []).slice(0, 4).map((modeValue) => (
                        <span
                          key={`${subject.subjectId}-${modeValue}`}
                          style={{
                            borderRadius: 999,
                            padding: '4px 8px',
                            fontSize: 11,
                            background: 'rgba(45,212,191,0.12)',
                            color: '#99f6e4',
                          }}
                        >
                          {modeValue}
                        </span>
                      ))}
                    </div>
                  ) : null}
                  {(subject.coverageGaps || []).length > 0 ? (
                    <div style={{ marginTop: 10, fontSize: 11, color: '#fca5a5' }}>
                      Coverage gaps: {(subject.coverageGaps || []).slice(0, 2).join(' • ')}
                    </div>
                  ) : null}
                </div>
              ))}
            </div>
          </section>
        ) : null}

        {renderBriefDetail()}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 18, minWidth: 0 }}>
        <section style={{ ...basePanelStyle, padding: 20 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', marginBottom: 14 }}>
            <div>
              <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                Monitoring board
              </div>
              <div style={{ fontSize: 16, fontWeight: 700 }}>
                {lensId ? 'Lens subjects and propagation deltas' : 'Watched scopes and persistent pressure'}
              </div>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
              0 clicks status • 1 click interpretation • 2 clicks proof
            </div>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: 12 }}>
            {(snapshot?.monitoring || []).slice(0, 8).map((item: HorizonWatchMonitoringSnapshot) => (
              <div
                key={item.id}
                style={{
                  borderRadius: 18,
                  border: '1px solid rgba(255,255,255,0.08)',
                  background: 'rgba(255,255,255,0.03)',
                  padding: 15,
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                  <div>
                    <div style={{ fontWeight: 700 }}>{item.label}</div>
                    <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>{item.canonicalPath}</div>
                  </div>
                  <div style={{ fontSize: 11, color: pillTone[item.riskPosture || 'neutral'] || 'var(--text-secondary-strong)' }}>
                    {item.riskPosture || item.status || 'watch'}
                  </div>
                </div>
                <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.6, color: 'rgba(226,232,240,0.78)' }}>
                  {item.summary}
                </div>
                {(item.topAffectedTargets || []).length > 0 ? (
                  <div style={{ marginTop: 10, fontSize: 11, color: '#bfdbfe' }}>
                    {(item.topAffectedTargets || []).slice(0, 3).join(' • ')}
                  </div>
                ) : null}
                <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 10, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {item.hotness !== undefined ? <span>Hotness {formatPct(item.hotness)}</span> : null}
                  {item.confidence !== undefined ? <span>Confidence {formatPct(item.confidence)}</span> : null}
                  {item.updatedAt ? <span>{formatDate(item.updatedAt)}</span> : null}
                </div>
              </div>
            ))}
          </div>
        </section>

        <section style={{ ...basePanelStyle, padding: 18 }}>
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Live signal stream
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>What changed in the watch layer</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, maxHeight: 520, overflow: 'auto', paddingRight: 4 }}>
            {liveFeed.map((entry: any) => (
              <div
                key={`${entry.targetId || entry.subjectId || 'feed'}-${entry.title}-${entry.createdAt}`}
                style={{
                  borderRadius: 16,
                  border: '1px solid rgba(255,255,255,0.08)',
                  background: 'rgba(255,255,255,0.03)',
                  padding: 14,
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10 }}>
                  <div style={{ fontWeight: 700, fontSize: 13 }}>{entry.title}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{formatDate(entry.createdAt)}</div>
                </div>
                <div style={{ marginTop: 6, fontSize: 12, lineHeight: 1.6, color: 'rgba(226,232,240,0.76)' }}>
                  {entry.summary || entry.entryType}
                </div>
                {(entry.canonicalPaths || []).length > 0 ? (
                  <div style={{ marginTop: 8, fontSize: 11, color: '#bfdbfe' }}>
                    {(entry.canonicalPaths || []).slice(0, 2).join(' • ')}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        </section>

        <section style={{ ...basePanelStyle, padding: 18 }}>
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
              Interpreted briefs
            </div>
            <div style={{ fontSize: 16, fontWeight: 700 }}>High-signal summaries for active watched scopes</div>
          </div>
          {renderBriefCards(visibleBriefs.slice(0, 4), { compact: true })}
        </section>
      </div>
    </div>
  );

  const renderScenarioLayout = () => {
    const previewResult = selectedScenarioPreset ? scenarioResults[selectedScenarioPreset.id] : null;
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <section
          style={{
            ...basePanelStyle,
            padding: 22,
            background:
              'linear-gradient(180deg, rgba(22,17,14,0.92) 0%, rgba(14,11,20,0.96) 100%), radial-gradient(circle at top right, rgba(251,146,60,0.14), transparent 36%)',
          }}
        >
          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.2fr) minmax(280px, 0.8fr)', gap: 18 }}>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: 0.6, color: theme.badgeColor, marginBottom: 10 }}>
                Scenario workspace
              </div>
              <div style={{ fontSize: 30, lineHeight: 1.04, fontWeight: 900, letterSpacing: -1.2, marginBottom: 10 }}>
                {scenarioWorkspace?.baselineRef || 'Baseline watch scope'}
              </div>
              <div style={{ fontSize: 14, lineHeight: 1.75, color: 'rgba(226,232,240,0.86)' }}>
                Build branches over the current watch scope, compare projected impact, and stress-test assumptions with triggers and disconfirmers before digging into proof.
              </div>
              <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', marginTop: 14 }}>
                <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  Scope {scenarioWorkspace?.scope || (lensId ? 'lens' : 'global')}
                </span>
                <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  Targets {(scenarioWorkspace?.targetPaths || []).length}
                </span>
                <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  Generated {formatDate(scenarioWorkspace?.generatedAt)}
                </span>
              </div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 10 }}>
              <div style={{ borderRadius: 18, padding: 14, border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.04)' }}>
                <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>Branches</div>
                <div style={{ fontSize: 28, fontWeight: 800, marginTop: 8 }}>{scenarioWorkspace?.contracts?.length || 0}</div>
              </div>
              <div style={{ borderRadius: 18, padding: 14, border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.04)' }}>
                <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>Compared</div>
                <div style={{ fontSize: 28, fontWeight: 800, marginTop: 8 }}>{scenarioComparison?.length || 0}</div>
              </div>
              <div style={{ borderRadius: 18, padding: 14, border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.04)' }}>
                <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>Previewed</div>
                <div style={{ fontSize: 28, fontWeight: 800, marginTop: 8 }}>{Object.keys(scenarioResults).length}</div>
              </div>
              <button
                type="button"
                onClick={() => void handleCompareScenarios()}
                disabled={scenarioCompareBusy || !scenarioWorkspace?.contracts?.length}
                style={{
                  borderRadius: 18,
                  border: '1px solid rgba(251,146,60,0.18)',
                  background: 'rgba(251,146,60,0.12)',
                  color: '#fdba74',
                  padding: '14px 12px',
                  fontWeight: 700,
                  opacity: scenarioCompareBusy ? 0.75 : 1,
                }}
              >
                {scenarioCompareBusy ? 'Comparing…' : 'Refresh compare'}
              </button>
            </div>
          </div>
        </section>

        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(320px, 0.82fr) minmax(0, 1.18fr)', gap: 18, alignItems: 'start' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
            <section style={{ ...basePanelStyle, padding: 18 }}>
              <div style={{ marginBottom: 12 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                  Scenario branches
                </div>
                <div style={{ fontSize: 16, fontWeight: 700 }}>Choose the branch to stress-test</div>
              </div>
              {scenarioWorkspaceBusy ? (
                <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>Loading scenario workspace…</div>
              ) : !(scenarioWorkspace?.contracts || []).length ? (
                <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                  No scenario presets are available for this scope yet.
                </div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {(scenarioWorkspace?.contracts || []).map((preset) => {
                    const active = preset.id === selectedScenarioId;
                    return (
                      <div
                        key={preset.id}
                        role="button"
                        tabIndex={0}
                        onClick={() => setSelectedScenarioId(preset.id)}
                        onKeyDown={(event) => activateByKeyboard(event, () => setSelectedScenarioId(preset.id))}
                        style={{
                          borderRadius: 18,
                          border: active ? '1px solid rgba(251,146,60,0.24)' : '1px solid rgba(255,255,255,0.08)',
                          background: active ? 'rgba(251,146,60,0.10)' : 'rgba(255,255,255,0.03)',
                          padding: 15,
                          cursor: 'pointer',
                          outline: 'none',
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
                          <div>
                            <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>
                              {preset.contractType}
                            </div>
                            <div style={{ fontSize: 17, fontWeight: 800, marginTop: 6 }}>{preset.title}</div>
                          </div>
                          <button
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation();
                              void handleRunScenario(preset);
                            }}
                            disabled={scenarioBusyId === preset.id}
                            style={{
                              borderRadius: 12,
                              border: '1px solid rgba(255,255,255,0.08)',
                              background: 'rgba(255,255,255,0.05)',
                              color: 'var(--text-heading)',
                              padding: '8px 10px',
                              fontSize: 12,
                              fontWeight: 700,
                              opacity: scenarioBusyId === preset.id ? 0.7 : 1,
                            }}
                          >
                            {scenarioBusyId === preset.id ? 'Running…' : 'Preview'}
                          </button>
                        </div>
                        <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.6, color: 'rgba(226,232,240,0.8)' }}>
                          {preset.description}
                        </div>
                        {(preset.domainModes || []).length > 0 ? (
                          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 10 }}>
                            {(preset.domainModes || []).map((modeValue) => (
                              <span
                                key={`${preset.id}-${modeValue}`}
                                style={{
                                  borderRadius: 999,
                                  padding: '4px 8px',
                                  fontSize: 11,
                                  background: 'rgba(251,146,60,0.12)',
                                  color: '#fdba74',
                                }}
                              >
                                {modeValue}
                              </span>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              )}
            </section>

            {renderSearchPanel()}
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
            <section style={{ ...basePanelStyle, padding: 20 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', marginBottom: 12 }}>
                <div>
                  <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                    Planner board
                  </div>
                  <div style={{ fontSize: 16, fontWeight: 700 }}>
                    {selectedScenarioPreset?.title || 'Select a scenario branch'}
                  </div>
                </div>
                {selectedScenarioPreset ? (
                  <button
                    type="button"
                    onClick={() => void handleRunScenario(selectedScenarioPreset)}
                    disabled={scenarioBusyId === selectedScenarioPreset.id}
                    style={{
                      borderRadius: 14,
                      border: '1px solid rgba(251,146,60,0.18)',
                      background: 'rgba(251,146,60,0.12)',
                      color: '#fdba74',
                      padding: '10px 12px',
                      fontWeight: 700,
                      opacity: scenarioBusyId === selectedScenarioPreset.id ? 0.7 : 1,
                    }}
                  >
                    {scenarioBusyId === selectedScenarioPreset?.id ? 'Running preview…' : 'Run preview'}
                  </button>
                ) : null}
              </div>

              {scenarioDetailBusy ? (
                <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>Loading branch detail…</div>
              ) : selectedScenarioPreset ? (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                  <div style={{ fontSize: 14, lineHeight: 1.75, color: 'rgba(226,232,240,0.84)' }}>
                    {selectedScenarioPreset.description || 'Branch description is being prepared.'}
                  </div>

                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 10 }}>
                    <div style={{ borderRadius: 16, padding: 14, border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.03)' }}>
                      <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>Contract</div>
                      <div style={{ fontSize: 18, fontWeight: 800, marginTop: 8 }}>{selectedScenarioPreset.contractType}</div>
                    </div>
                    <div style={{ borderRadius: 16, padding: 14, border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.03)' }}>
                      <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>Targets</div>
                      <div style={{ fontSize: 18, fontWeight: 800, marginTop: 8 }}>{(selectedScenarioPreset.targetPaths || []).length}</div>
                    </div>
                    <div style={{ borderRadius: 16, padding: 14, border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.03)' }}>
                      <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)' }}>Seed fragments</div>
                      <div style={{ fontSize: 18, fontWeight: 800, marginTop: 8 }}>{(selectedScenarioPreset.seedFragmentIds || []).length}</div>
                    </div>
                  </div>

                  {(selectedScenarioPreset.triggerConditions || []).length > 0 ? (
                    <div>
                      <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6, marginBottom: 8 }}>
                        Trigger conditions
                      </div>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                        {(selectedScenarioPreset.triggerConditions || []).map((trigger) => (
                          <span
                            key={`${selectedScenarioPreset.id}-trigger-${trigger}`}
                            style={{
                              borderRadius: 999,
                              padding: '6px 10px',
                              background: 'rgba(96,165,250,0.10)',
                              color: '#bfdbfe',
                              fontSize: 12,
                            }}
                          >
                            {trigger}
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  {(selectedScenarioPreset.disconfirmers || []).length > 0 ? (
                    <div>
                      <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6, marginBottom: 8 }}>
                        Disconfirmers
                      </div>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                        {(selectedScenarioPreset.disconfirmers || []).map((disconfirmer) => (
                          <span
                            key={`${selectedScenarioPreset.id}-disconfirmer-${disconfirmer}`}
                            style={{
                              borderRadius: 999,
                              padding: '6px 10px',
                              background: 'rgba(239,68,68,0.10)',
                              color: '#fca5a5',
                              fontSize: 12,
                            }}
                          >
                            {disconfirmer}
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  {(selectedScenarioPreset.targetPaths || []).length > 0 ? (
                    <div>
                      <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6, marginBottom: 8 }}>
                        Projected target paths
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                        {(selectedScenarioPreset.targetPaths || []).slice(0, 6).map((path) => (
                          <div
                            key={`${selectedScenarioPreset.id}-path-${path}`}
                            style={{
                              borderRadius: 12,
                              padding: '10px 12px',
                              background: 'rgba(255,255,255,0.03)',
                              border: '1px solid rgba(255,255,255,0.08)',
                              fontSize: 12,
                              color: '#bfdbfe',
                            }}
                          >
                            {path}
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  {previewResult ? (
                    <div
                      style={{
                        borderRadius: 18,
                        border: '1px solid rgba(251,146,60,0.14)',
                        background: 'rgba(251,146,60,0.08)',
                        padding: 16,
                      }}
                    >
                      <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(254,215,170,0.92)', letterSpacing: 0.6, marginBottom: 10 }}>
                        Preview result
                      </div>
                      {'error' in previewResult ? (
                        <div style={{ fontSize: 13, color: '#fca5a5' }}>{previewResult.error}</div>
                      ) : (
                        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 10 }}>
                          <div>
                            <div style={{ fontSize: 11, color: 'rgba(226,232,240,0.7)' }}>Affected fragments</div>
                            <div style={{ fontSize: 24, fontWeight: 800, marginTop: 6 }}>{previewResult.affectedFragments?.length || 0}</div>
                          </div>
                          <div>
                            <div style={{ fontSize: 11, color: 'rgba(226,232,240,0.7)' }}>Domain metrics</div>
                            <div style={{ fontSize: 24, fontWeight: 800, marginTop: 6 }}>{previewResult.domainMetrics?.length || 0}</div>
                          </div>
                          <div>
                            <div style={{ fontSize: 11, color: 'rgba(226,232,240,0.7)' }}>Signals</div>
                            <div style={{ fontSize: 24, fontWeight: 800, marginTop: 6 }}>{previewResult.encodedSignals?.length || 0}</div>
                          </div>
                        </div>
                      )}
                    </div>
                  ) : null}
                </div>
              ) : (
                <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                  Select a scenario branch to inspect triggers, disconfirmers, and projected impact.
                </div>
              )}
            </section>

            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 0.95fr) minmax(320px, 1.05fr)', gap: 18, alignItems: 'start' }}>
              <section style={{ ...basePanelStyle, padding: 18 }}>
                <div style={{ marginBottom: 12 }}>
                  <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                    Compare branches
                  </div>
                  <div style={{ fontSize: 16, fontWeight: 700 }}>Side-by-side pressure and impact preview</div>
                </div>
                {scenarioCompareBusy ? (
                  <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>Comparing scenario branches…</div>
                ) : !(scenarioComparison || []).length ? (
                  <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>Comparison results will appear here.</div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {(scenarioComparison || []).map((branch) => (
                      <div
                        key={`compare-${branch.contract.id}`}
                        style={{
                          borderRadius: 16,
                          border: '1px solid rgba(255,255,255,0.08)',
                          background: 'rgba(255,255,255,0.03)',
                          padding: 14,
                        }}
                      >
                        <div style={{ fontWeight: 700 }}>{branch.contract.title}</div>
                        <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                          {branch.contract.contractType}
                        </div>
                        {branch.error ? (
                          <div style={{ marginTop: 8, fontSize: 12, color: '#fca5a5' }}>{branch.error}</div>
                        ) : (
                          <div style={{ marginTop: 8, display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 8, fontSize: 12, color: 'rgba(226,232,240,0.82)' }}>
                            <div>
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Affected</div>
                              <div style={{ marginTop: 4, fontWeight: 700 }}>{branch.result?.affectedFragments?.length || 0}</div>
                            </div>
                            <div>
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Signals</div>
                              <div style={{ marginTop: 4, fontWeight: 700 }}>{branch.result?.encodedSignals?.length || 0}</div>
                            </div>
                            <div>
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Metrics</div>
                              <div style={{ marginTop: 4, fontWeight: 700 }}>{branch.result?.domainMetrics?.length || 0}</div>
                            </div>
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </section>

              <section style={{ ...basePanelStyle, padding: 18 }}>
                <div style={{ marginBottom: 12 }}>
                  <div style={{ fontSize: 12, textTransform: 'uppercase', color: 'rgba(148,163,184,0.9)', letterSpacing: 0.6 }}>
                    Baseline context
                  </div>
                  <div style={{ fontSize: 16, fontWeight: 700 }}>Supporting briefs and map context for the current baseline</div>
                </div>
                {primaryBrief ? (
                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => openBrief(primaryBrief.id)}
                    onKeyDown={(event) => activateByKeyboard(event, () => openBrief(primaryBrief.id))}
                    style={{
                      borderRadius: 18,
                      border: '1px solid rgba(255,255,255,0.08)',
                      background: 'rgba(255,255,255,0.03)',
                      padding: 16,
                      cursor: 'pointer',
                    }}
                  >
                    <div style={{ fontSize: 11, textTransform: 'uppercase', color: theme.badgeColor }}>Baseline thesis</div>
                    <div style={{ fontSize: 18, fontWeight: 800, marginTop: 8 }}>{primaryBrief.headline}</div>
                    <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.7, color: 'rgba(226,232,240,0.8)' }}>
                      {primaryBrief.summary}
                    </div>
                  </div>
                ) : null}
                {(snapshot?.mapFragments || []).length > 0 ? (
                  <div style={{ marginTop: 12, display: 'grid', gridTemplateColumns: '1fr', gap: 12 }}>
                    {(snapshot?.mapFragments || []).slice(0, 2).map((fragment) => renderMapCard(fragment, onOpenWorldMap))}
                  </div>
                ) : null}
              </section>
            </div>

            {renderBriefDetail()}
          </div>
        </div>
      </div>
    );
  };

  return (
    <div
      style={{
        minHeight: '100%',
        overflow: 'auto',
        padding: 20,
        background: theme.background,
        color: 'var(--text-heading)',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18, maxWidth: 1540, margin: '0 auto' }}>
        <section
          style={{
            ...basePanelStyle,
            ...({ background: `${theme.panelGlow}, ${basePanelStyle.background}` } as React.CSSProperties),
            padding: 24,
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))',
            gap: 20,
          }}
        >
          <div style={{ minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap', marginBottom: 14 }}>
              <span
                style={{
                  padding: '6px 10px',
                  borderRadius: 999,
                  fontSize: 11,
                  letterSpacing: 0.6,
                  textTransform: 'uppercase',
                  background: theme.badgeBg,
                  color: theme.badgeColor,
                  border: '1px solid rgba(255,255,255,0.08)',
                }}
              >
                Horizon Watch
              </span>
              <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                {theme.eyebrow}
              </span>
              {snapshot?.cached ? (
                <span style={{ fontSize: 12, color: 'rgba(148,163,184,0.9)' }}>cached snapshot</span>
              ) : null}
            </div>
            <div style={{ fontSize: 36, lineHeight: 1.05, fontWeight: 800, letterSpacing: -1.4, marginBottom: 8 }}>
              {theme.title}
            </div>
            <div style={{ maxWidth: 880, fontSize: 15, lineHeight: 1.7, color: 'rgba(226,232,240,0.86)' }}>
              {theme.description}
            </div>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 14, color: 'rgba(148,163,184,0.9)', fontSize: 12 }}>
              <span>{scopeTitle}</span>
              {projectName ? <span>Project {projectName}</span> : null}
              <span>{lensId ? 'Lens-scoped watch surface' : 'Tenant and global-wide watch surface'}</span>
              <span>{serviceLine === 'scenario' ? '0 clicks baseline • 1 click branch • 2 clicks proof' : '0 clicks insight • 1 click explanation • 2 clicks proof'}</span>
            </div>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              {[
                { id: 'geo', label: 'Geopolitical Risks' },
                { id: 'monitoring', label: 'Deep Monitoring' },
                { id: 'scenario', label: 'Scenario Planning' },
              ].map((item) => {
                const active = serviceLine === item.id;
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => setServiceLine(item.id as ServiceLine)}
                    style={{
                      borderRadius: 14,
                      padding: '10px 12px',
                      border: active ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(255,255,255,0.08)',
                      background: active ? theme.badgeBg : 'rgba(255,255,255,0.04)',
                      color: active ? theme.badgeColor : 'var(--text-secondary-strong)',
                      fontSize: 12,
                      fontWeight: 700,
                    }}
                  >
                    {item.label}
                  </button>
                );
              })}
            </div>

            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
                gap: 10,
              }}
            >
              <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                <span style={{ fontSize: 11, color: 'rgba(148,163,184,0.9)', textTransform: 'uppercase', letterSpacing: 0.5 }}>
                  Mode
                </span>
                <select
                  value={mode}
                  onChange={(event) => setMode(event.target.value as WatchMode)}
                  style={{
                    borderRadius: 14,
                    padding: '12px 14px',
                    background: 'rgba(15,23,42,0.88)',
                    color: 'var(--text-heading)',
                    border: '1px solid rgba(255,255,255,0.08)',
                  }}
                >
                  <option value="combined">Combined</option>
                  <option value="global">Global</option>
                  <option value="my_watch">My watch</option>
                </select>
              </label>

              <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                <span style={{ fontSize: 11, color: 'rgba(148,163,184,0.9)', textTransform: 'uppercase', letterSpacing: 0.5 }}>
                  Horizon
                </span>
                <select
                  value={timeHorizon}
                  onChange={(event) => setTimeHorizon(event.target.value)}
                  style={{
                    borderRadius: 14,
                    padding: '12px 14px',
                    background: 'rgba(15,23,42,0.88)',
                    color: 'var(--text-heading)',
                    border: '1px solid rgba(255,255,255,0.08)',
                  }}
                >
                  <option value="24h">24h</option>
                  <option value="7d">7d</option>
                  <option value="30d">30d</option>
                </select>
              </label>
            </div>

            <div style={{ display: 'flex', gap: 10 }}>
              <button
                type="button"
                onClick={() => void refreshSnapshot()}
                style={{
                  flex: 1,
                  borderRadius: 14,
                  border: '1px solid rgba(255,255,255,0.08)',
                  background: 'rgba(255,255,255,0.05)',
                  color: 'var(--text-heading)',
                  padding: '12px 14px',
                  fontWeight: 700,
                }}
              >
                Refresh snapshot
              </button>
              {onOpenWorldMap ? (
                <button
                  type="button"
                  onClick={onOpenWorldMap}
                  style={{
                    flex: 1,
                    borderRadius: 14,
                    border: '1px solid rgba(96,165,250,0.18)',
                    background: 'rgba(96,165,250,0.12)',
                    color: '#dbeafe',
                    padding: '12px 14px',
                    fontWeight: 700,
                  }}
                >
                  Open world map
                </button>
              ) : null}
            </div>
          </div>
        </section>

        {loading ? (
          <div style={{ ...basePanelStyle, padding: 24, fontSize: 14, color: 'var(--text-secondary-strong)' }}>Loading watch snapshot…</div>
        ) : error ? (
          <div style={{ ...basePanelStyle, padding: 24, fontSize: 14, color: '#fca5a5' }}>{error}</div>
        ) : null}

        {!loading && !error ? (
          <>
            {serviceLine === 'geo' ? renderGeoLayout() : null}
            {serviceLine === 'monitoring' ? renderMonitoringLayout() : null}
            {serviceLine === 'scenario' ? renderScenarioLayout() : null}
          </>
        ) : null}
      </div>
    </div>
  );
};

export default HorizonWatchSurface;
