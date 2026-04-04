import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  api,
  type HorizonHomeWorkspaceEntity,
  type HorizonHomeWorkspaceRunResponse,
} from '../lib/api';
import { useProjectsStore } from '../stores/projectsStore';
import type { LensChatMapFocus } from './LensChatDockPlaceholder';

interface HorizonHomeWorkspaceProps {
  currentProjectId?: string | null;
  onOpenLens?: (lensId: string) => void;
  onMapFocusChange?: (focus: LensChatMapFocus | null) => void;
}

type SaveState = 'idle' | 'saving' | 'saved' | 'error';

const PANEL_STYLE: React.CSSProperties = {
  border: '1px solid var(--border-subtle)',
  borderRadius: 20,
  background: 'color-mix(in srgb, var(--bg-elevated) 92%, black 8%)',
  boxShadow: '0 24px 60px rgba(0, 0, 0, 0.28)',
  backdropFilter: 'blur(14px)',
};

function buildHomeMapFocus(result: HorizonHomeWorkspaceRunResponse | null): LensChatMapFocus | null {
  if (!result?.runId) return null;
  const set = new Set<string>();
  for (const path of result.highlightedPaths || []) {
    const value = String(path || '').trim();
    if (value.startsWith('/')) set.add(value);
  }
  for (const path of result.eventCorePaths || []) {
    const value = String(path || '').trim();
    if (value.startsWith('/')) set.add(value);
  }
  for (const path of result.affectedZoneRefs || []) {
    const value = String(path || '').trim();
    if (value.startsWith('/')) set.add(value);
  }
  const canonicalPaths = Array.from(set);
  if (canonicalPaths.length === 0) return null;
  return { sourceMessageId: result.runId, canonicalPaths };
}

function humanizeCanonicalPath(path: string): string {
  const normalized = String(path || '').trim().toLowerCase();
  if (!normalized.startsWith('/')) return normalized;
  const segments = normalized.split('/').filter(Boolean);
  if (segments[0] === 'loc' && segments[1] && segments[1].length === 2) {
    try {
      const regionNames = new Intl.DisplayNames(['en'], { type: 'region' });
      const label = regionNames.of(segments[1].toUpperCase());
      if (label) return label;
    } catch {}
  }
  let start = 1;
  if (segments.length > 1 && segments[1].length === 2) start = 2;
  if (start >= segments.length) start = Math.max(0, segments.length - 1);
  return segments
    .slice(start)
    .flatMap((segment) => segment.split('-'))
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function formatEntityType(value?: string): string {
  const normalized = String(value || '').trim();
  if (!normalized) return '';
  return normalized
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
    .join(' ');
}

function compactCanonicalPath(path: string): string {
  const value = String(path || '').trim();
  if (!value) return '';
  if (value.length <= 52) return value;
  return `${value.slice(0, 30)}…${value.slice(-16)}`;
}

function buildSignalChipLabel(path: string): string {
  const label = humanizeCanonicalPath(path);
  if (!label) return path;
  if (label.length <= 28) return label;
  return `${label.slice(0, 26)}…`;
}

function formatWorkspaceWarning(value: string): string | null {
  const warning = String(value || '').trim();
  if (!warning) return null;
  const normalized = warning.toLowerCase();
  if (normalized.includes('final synthesis failed') && normalized.includes('deadline exceeded')) {
    return 'The final explanation timed out. Grounded results below are still available.';
  }
  if (normalized.includes('final synthesis returned empty content')) {
    return 'The final explanation came back empty, so the workspace is showing grounded fallback results.';
  }
  if (normalized.includes('vertex search client is not configured')) {
    return 'Workspace search is temporarily unavailable.';
  }
  return warning;
}

function buildEvidenceTitle(hit: { title?: string; canonicalPaths?: string[]; kind?: string }): string {
  const rawTitle = String(hit.title || '').trim();
  const canonicalPaths = Array.isArray(hit.canonicalPaths) ? hit.canonicalPaths.filter(Boolean) : [];
  const nonEventPaths = canonicalPaths.filter((path) => !String(path || '').trim().startsWith('/event/'));
  const primaryPath = nonEventPaths[0] || canonicalPaths[0] || '';
  const primaryLabel = primaryPath ? humanizeCanonicalPath(primaryPath) : '';

  const normalizedTitle = rawTitle.toLowerCase();
  if (normalizedTitle.startsWith('fresh signal around /event/')) {
    return primaryLabel ? `Fresh signal: ${primaryLabel}` : 'Fresh signal';
  }
  if (normalizedTitle.startsWith('horizon monitor')) {
    return primaryLabel ? `Signal landing: ${primaryLabel}` : 'Signal landing';
  }
  if (rawTitle.includes('/event/')) {
    return primaryLabel ? `Event evidence: ${primaryLabel}` : 'Event evidence';
  }
  return rawTitle || primaryLabel || 'Grounded evidence';
}

function buildEvidenceMetaLabel(kind?: string): string {
  const normalized = String(kind || '').trim().toLowerCase();
  switch (normalized) {
    case 'signal_landing_summary':
      return 'Fresh landed signal';
    case 'analytical_aggregate':
      return 'Analytical summary';
    case 'analytical_primitive':
      return 'Analytical primitive';
    case 'relation':
      return 'Grounded relation';
    case 'object':
      return 'Grounded object';
    default:
      return normalized
        ? normalized
            .split('_')
            .filter(Boolean)
            .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
            .join(' ')
        : 'Grounded evidence';
  }
}

function buildEvidenceWhy(
  hit: { kind?: string; canonicalPaths?: string[]; title?: string },
  eventCoreSet: Set<string>,
  affectedZoneSet: Set<string>,
  haloSet: Set<string>,
): string {
  const rawPaths = Array.isArray(hit.canonicalPaths) ? hit.canonicalPaths.map((value) => String(value || '').trim()).filter(Boolean) : [];
  const core = rawPaths.filter((path) => eventCoreSet.has(path));
  const zones = rawPaths.filter((path) => affectedZoneSet.has(path));
  const halo = rawPaths.filter((path) => haloSet.has(path));

  if (String(hit.kind || '').trim() === 'signal_landing_summary') {
    if (core.length > 0) {
      return `Shown because the fresh signal lands on ${joinHumanLabels(core)}.`;
    }
    if (zones.length > 0) {
      return `Shown because the signal affects ${joinHumanLabels(zones)}.`;
    }
    if (halo.length > 0) {
      return `Shown because it extends the signal halo through ${joinHumanLabels(halo)}.`;
    }
    return 'Shown because it captures the freshest landed signal for this workspace.';
  }
  if (String(hit.kind || '').trim() === 'relation') {
    return 'Shown because this relation helps explain how the impact can propagate through connected products or entities.';
  }
  if (core.length > 0 || zones.length > 0 || halo.length > 0) {
    return `Shown because it grounds the current signal on ${joinHumanLabels([...core, ...zones, ...halo])}.`;
  }
  return 'Shown because it adds grounded context for the current workspace question.';
}

function joinHumanLabels(paths: string[]): string {
  const labels = makeUniqueStrings(paths.map((path) => humanizeCanonicalPath(path)).filter(Boolean));
  if (labels.length === 0) return '';
  if (labels.length === 1) return labels[0];
  if (labels.length === 2) return `${labels[0]} and ${labels[1]}`;
  return `${labels.slice(0, -1).join(', ')}, and ${labels[labels.length - 1]}`;
}

function makeUniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const normalized = String(value || '').trim();
    if (!normalized) continue;
    const key = normalized.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(normalized);
  }
  return out;
}

function whySurfacedLabel(
  canonicalPath: string,
  eventCoreSet: Set<string>,
  affectedZoneSet: Set<string>,
  haloSet: Set<string>,
  scopeSet: Set<string>,
): string {
  if (eventCoreSet.has(canonicalPath)) return 'Fresh signal core';
  if (affectedZoneSet.has(canonicalPath)) return 'Affected zone';
  if (haloSet.has(canonicalPath)) return 'Signal halo';
  if (scopeSet.has(canonicalPath)) return 'Suggested scope';
  return 'Grounded object';
}

function reasonPillStyle(kind: 'core' | 'zone' | 'halo' | 'scope' | 'neutral'): React.CSSProperties {
  const palette =
    kind === 'core'
      ? { border: 'rgba(255, 171, 84, 0.55)', bg: 'rgba(255, 171, 84, 0.12)', color: 'var(--text-heading)' }
      : kind === 'zone'
        ? { border: 'rgba(102, 153, 153, 0.45)', bg: 'rgba(102, 153, 153, 0.12)', color: 'var(--text-heading)' }
        : kind === 'halo'
          ? { border: 'var(--border-subtle)', bg: 'rgba(255,255,255,0.03)', color: 'var(--text-secondary-strong)' }
          : kind === 'scope'
            ? { border: 'rgba(255, 171, 84, 0.55)', bg: 'rgba(255, 171, 84, 0.08)', color: 'var(--text-heading)' }
            : { border: 'var(--border-subtle)', bg: 'rgba(255,255,255,0.03)', color: 'var(--text-secondary-strong)' };
  return {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    height: 28,
    padding: '0 10px',
    borderRadius: 999,
    border: `1px solid ${palette.border}`,
    background: palette.bg,
    color: palette.color,
    fontSize: 11,
    fontWeight: 700,
    whiteSpace: 'nowrap',
  };
}

function chipButtonStyle(active: boolean): React.CSSProperties {
  return {
    padding: '7px 10px',
    borderRadius: 999,
    border: `1px solid ${active ? 'var(--accent-primary)' : 'var(--border-subtle)'}`,
    background: active ? 'rgba(17, 153, 142, 0.14)' : 'var(--bg-base)',
    color: 'var(--text-heading)',
    fontSize: 11,
    fontWeight: 600,
    cursor: 'pointer',
  };
}

export const HorizonHomeWorkspace: React.FC<HorizonHomeWorkspaceProps> = ({
  currentProjectId,
  onOpenLens,
  onMapFocusChange,
}) => {
  const createProject = useProjectsStore((s) => s.createProject);
  const updateProject = useProjectsStore((s) => s.updateProject);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const [query, setQuery] = React.useState('');
  const [result, setResult] = React.useState<HorizonHomeWorkspaceRunResponse | null>(null);
  const [isRunning, setIsRunning] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [selectedPaths, setSelectedPaths] = React.useState<string[]>([]);
  const [creatingLens, setCreatingLens] = React.useState(false);
  const [watchStates, setWatchStates] = React.useState<Record<string, SaveState>>({});
  const [portfolioStates, setPortfolioStates] = React.useState<Record<string, SaveState>>({});

  React.useEffect(() => {
    const next = result?.suggestedLensScope && result.suggestedLensScope.length > 0
      ? result.suggestedLensScope
      : result?.highlightedPaths || [];
    setSelectedPaths(Array.from(new Set(next.map((value) => String(value || '').trim()).filter(Boolean))));
    onMapFocusChange?.(buildHomeMapFocus(result));
  }, [result, onMapFocusChange]);

  const runWorkspace = React.useCallback(async () => {
    const trimmed = query.trim();
    if (!trimmed || isRunning) return;
    setIsRunning(true);
    setError(null);
    try {
      const response = await api.runHorizonHomeWorkspace(trimmed);
      setResult(response);
    } catch (err: any) {
      console.error('[HorizonHomeWorkspace] Failed to run workspace', err);
      setError(err?.response?.data?.error?.message || err?.message || 'Failed to run workspace');
    } finally {
      setIsRunning(false);
    }
  }, [query, isRunning]);

  const handleCreateLens = React.useCallback(async () => {
    if (!result || creatingLens) return;
    const scope = Array.from(new Set(selectedPaths.map((value) => String(value || '').trim()).filter(Boolean)));
    if (scope.length === 0) return;
    setCreatingLens(true);
    setError(null);
    try {
      let projectId = String(currentProjectId || '').trim();
      if (!projectId) {
        const created = await createProject({
          displayName: (result.suggestedLensName || result.query || 'Home workspace').trim(),
          view: 'horizon',
        });
        const updated = await updateProject(created.id, { projectType: 'horizon' });
        setCurrentProject(updated);
        projectId = updated.id;
      }
      const lens = await api.createHorizonLens(
        {
          projectId,
          name: (result.suggestedLensName || result.query || 'Home workspace').trim(),
          scope,
          viewConfig: {
            homeWorkspace: {
              runId: result.runId,
              query: result.query,
              highlightedPaths: result.highlightedPaths || [],
              eventCorePaths: result.eventCorePaths || [],
              haloPaths: result.haloPaths || [],
            },
          },
        },
        { skipScouting: false },
      );
      if (lens?.id) {
        onOpenLens?.(lens.id);
      }
    } catch (err: any) {
      console.error('[HorizonHomeWorkspace] Failed to create lens', err);
      setError(err?.response?.data?.error?.message || err?.message || 'Failed to create lens');
    } finally {
      setCreatingLens(false);
    }
  }, [createProject, creatingLens, currentProjectId, onOpenLens, result, selectedPaths, setCurrentProject, updateProject]);

  const handleAddToWatch = React.useCallback(async (entity: HorizonHomeWorkspaceEntity) => {
    const canonicalPath = String(entity.canonicalPath || '').trim();
    if (!canonicalPath) return;
    setWatchStates((current) => ({ ...current, [canonicalPath]: 'saving' }));
    try {
      await api.createHorizonWatchTarget({
        kind: 'focus_object',
        label: entity.name || humanizeCanonicalPath(canonicalPath),
        canonicalPath,
        interestTitle: result?.suggestedLensName || result?.query || entity.name,
        interestQuery: result?.query || entity.name,
        priority: 70,
      });
      setWatchStates((current) => ({ ...current, [canonicalPath]: 'saved' }));
    } catch (err) {
      console.error('[HorizonHomeWorkspace] Failed to add watch target', err);
      setWatchStates((current) => ({ ...current, [canonicalPath]: 'error' }));
    }
  }, [result]);

  const handleAddToPortfolio = React.useCallback(async (entity: HorizonHomeWorkspaceEntity) => {
    const canonicalPath = String(entity.canonicalPath || '').trim();
    if (!canonicalPath) return;
    setPortfolioStates((current) => ({ ...current, [canonicalPath]: 'saving' }));
    try {
      const portfolios = await api.getHorizonWatchPortfolios();
      const existing = portfolios.find((item) => String(item?.name || '').trim().toLowerCase() === 'home workspace');
      const existingView = existing?.id ? await api.getHorizonWatchPortfolio(existing.id) : null;
      const existingEntries = Array.isArray(existingView?.portfolio?.entries) ? existingView.portfolio.entries : [];
      const alreadyExists = existingEntries.some((item: any) => String(item?.canonicalPath || '').trim() === canonicalPath);
      const nextEntries = alreadyExists
        ? existingEntries
        : [
            ...existingEntries,
            {
              label: entity.name || humanizeCanonicalPath(canonicalPath),
              canonicalPath,
              entryType: 'object',
              autoEnsureFocus: true,
              metadata: {
                source: 'horizon_home_workspace',
                query: result?.query || '',
              },
            },
          ];
      await api.upsertHorizonWatchPortfolio({
        id: existing?.id,
        name: 'Home workspace',
        kind: 'research',
        description: 'Saved objects from Horizon Home workspace',
        entries: nextEntries.map((item: any) => ({
          id: item?.id,
          label: item?.label,
          canonicalPath: item?.canonicalPath,
          entryType: item?.entryType,
          subjectId: item?.subjectId,
          sourceTenantId: item?.sourceTenantId,
          sourceLensId: item?.sourceLensId,
          relatedCanonicalPaths: item?.relatedCanonicalPaths,
          autoEnsureFocus: Boolean(item?.autoEnsureFocus),
          metadata: item?.metadata,
        })),
        metadata: {
          source: 'horizon_home_workspace',
        },
      });
      setPortfolioStates((current) => ({ ...current, [canonicalPath]: 'saved' }));
    } catch (err) {
      console.error('[HorizonHomeWorkspace] Failed to add portfolio entry', err);
      setPortfolioStates((current) => ({ ...current, [canonicalPath]: 'error' }));
    }
  }, [result]);

  const displayedEntities = React.useMemo(() => {
    if (result?.entities && result.entities.length > 0) {
      return result.entities;
    }
    return (result?.highlightedPaths || []).map((path): HorizonHomeWorkspaceEntity => ({
      name: humanizeCanonicalPath(path),
      canonicalPath: path,
      selected: selectedPaths.includes(path),
    }));
  }, [result, selectedPaths]);

  const centerStack = !result;
  const eventCoreSet = React.useMemo(() => new Set((result?.eventCorePaths || []).map((value) => String(value || '').trim()).filter(Boolean)), [result]);
  const affectedZoneSet = React.useMemo(() => new Set((result?.affectedZoneRefs || []).map((value) => String(value || '').trim()).filter(Boolean)), [result]);
  const haloSet = React.useMemo(() => new Set((result?.haloPaths || []).map((value) => String(value || '').trim()).filter(Boolean)), [result]);
  const suggestedScopeSet = React.useMemo(() => new Set((result?.suggestedLensScope || selectedPaths).map((value) => String(value || '').trim()).filter(Boolean)), [result, selectedPaths]);
  const displayedCorePaths = React.useMemo(
    () => Array.from(eventCoreSet).slice(0, 6),
    [eventCoreSet],
  );
  const displayedHaloPaths = React.useMemo(
    () =>
      Array.from(haloSet)
        .filter((value) => !eventCoreSet.has(value) && !affectedZoneSet.has(value))
        .slice(0, 8),
    [affectedZoneSet, eventCoreSet, haloSet],
  );
  const displayedAffectedZones = React.useMemo(
    () => Array.from(affectedZoneSet).filter((value) => !eventCoreSet.has(value)).slice(0, 6),
    [affectedZoneSet, eventCoreSet],
  );
  const displayedEvidenceHits = React.useMemo(
    () =>
      (result?.hits || []).slice(0, 6).map((hit) => {
        const rawPaths = Array.isArray(hit.canonicalPaths) ? hit.canonicalPaths : [];
        const novelPaths = rawPaths.filter((path) => {
          const normalized = String(path || '').trim();
          return normalized && !eventCoreSet.has(normalized) && !haloSet.has(normalized) && !affectedZoneSet.has(normalized);
        });
        return {
          ...hit,
          displayPaths: novelPaths.slice(0, 4),
          evidenceMetaLabel: buildEvidenceMetaLabel(hit.kind),
          evidenceWhy: buildEvidenceWhy(hit, eventCoreSet, affectedZoneSet, haloSet),
        };
      }),
    [affectedZoneSet, eventCoreSet, haloSet, result],
  );
  const displayedWarnings = React.useMemo(
    () =>
      Array.from(
        new Set(
          (result?.warnings || [])
            .map(formatWorkspaceWarning)
            .filter((value): value is string => Boolean(value)),
        ),
      ),
    [result],
  );

  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        gap: 16,
        padding: '82px 18px 18px 18px',
        overflow: 'auto',
      }}
    >
      <div
        style={{
          width: '100%',
          maxWidth: 760,
          minHeight: '100%',
          margin: '0 auto',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: centerStack ? 'center' : 'flex-start',
          gap: 16,
        }}
      >
        <div style={{ ...PANEL_STYLE, overflow: 'hidden' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 12,
              padding: '18px 18px 14px 18px',
              background:
                'radial-gradient(circle at top left, rgba(102, 153, 153, 0.14), transparent 42%), linear-gradient(180deg, rgba(255,255,255,0.025), rgba(255,255,255,0))',
            }}
          >
            <div
              style={{
                width: 32,
                height: 32,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: 'var(--text-secondary-strong)',
                flexShrink: 0,
              }}
            >
              <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="m12 3 1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9L12 3Z" />
              </svg>
            </div>
            <textarea
              className="horizon-home-workspace-input"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                  event.preventDefault();
                  void runWorkspace();
                }
              }}
              placeholder="Ask about a disruption, weak point, dependency, route, product, company, or geography..."
              style={{
                flex: 1,
                minHeight: 54,
                maxHeight: 140,
                resize: 'none',
                border: 'none',
                background: 'transparent',
                color: 'var(--text-heading)',
                padding: '14px 16px',
                borderRadius: 18,
                fontSize: 15,
                lineHeight: 1.45,
                outline: 'none',
              }}
            />
            <style>
              {`
                .horizon-home-workspace-input::placeholder {
                  color: rgba(255, 255, 255, 0.52);
                }
              `}
            </style>
            <button
              type="button"
              aria-label="Run workspace"
              onClick={() => void runWorkspace()}
              disabled={!query.trim() || isRunning}
              style={{
                width: 32,
                height: 32,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                borderRadius: 8,
                background: !query.trim() || isRunning ? 'transparent' : 'var(--accent-primary)',
                color: !query.trim() || isRunning ? 'var(--text-disabled)' : '#ffffff',
                border: 'none',
                cursor: !query.trim() || isRunning ? 'not-allowed' : 'pointer',
                transition: 'all 0.2s',
                opacity: !query.trim() ? 0.5 : 1,
                flexShrink: 0,
                boxShadow: !query.trim() || isRunning ? 'none' : '0 10px 24px rgba(102, 153, 153, 0.26)',
              }}
            >
              {isRunning ? (
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M21 12a9 9 0 1 1-6.22-8.56" />
                </svg>
              ) : (
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M5 12h14" />
                  <path d="m12 5 7 7-7 7" />
                </svg>
              )}
            </button>
          </div>

          <div
            style={{
              width: '100%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '10px 14px 12px',
              borderTop: '1px solid var(--border-subtle)',
              flexWrap: 'wrap',
            }}
          >
            <div
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 8,
                color: 'var(--text-secondary-strong)',
                padding: '4px 8px',
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M4 7h16" />
                <path d="M7 12h10" />
                <path d="M10 17h4" />
              </svg>
              <span>Research</span>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
              <div style={{ fontSize: 12, color: 'var(--text-tertiary-weak)' }}>
                Cmd/Ctrl + Enter
              </div>
              <button
                type="button"
                onClick={handleCreateLens}
                disabled={!result || selectedPaths.length === 0 || creatingLens}
                style={{
                  height: 30,
                  padding: '0 12px',
                  borderRadius: 10,
                  border: `1px solid ${!result || selectedPaths.length === 0 ? 'var(--border-subtle)' : 'var(--accent-primary)'}`,
                  background: creatingLens ? 'rgba(17,153,142,0.14)' : !result || selectedPaths.length === 0 ? 'transparent' : 'rgba(17,153,142,0.12)',
                  color: !result || selectedPaths.length === 0 ? 'var(--text-disabled)' : 'var(--text-heading)',
                  fontSize: 12,
                  fontWeight: 700,
                  cursor: !result || selectedPaths.length === 0 || creatingLens ? 'default' : 'pointer',
                }}
              >
                {creatingLens ? 'Creating…' : 'Create lens'}
              </button>
            </div>
          </div>
        </div>

        {error && (
          <div style={{ marginTop: -4, fontSize: 13, color: '#f87171', textAlign: 'center' }}>
            {error}
          </div>
        )}

        {result && (
          <>
          <div style={{ ...PANEL_STYLE, padding: 18 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 16, alignItems: 'center', marginBottom: 12 }}>
              <div>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)' }}>
                  Explanation
                </div>
                <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)', marginTop: 4 }}>
                  {result.query}
                </div>
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-tertiary-weak)' }}>
                {result.runId}
              </div>
            </div>
            <div className="markdown-body" style={{ color: 'var(--text-heading)', fontSize: 14, lineHeight: 1.6 }}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{result.answer}</ReactMarkdown>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.2fr) minmax(300px, 0.8fr)', gap: 14 }}>
            <div style={{ ...PANEL_STYLE, padding: 18 }}>
              <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 12 }}>
                Workspace objects
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {displayedEntities.map((entity) => {
                  const canonicalPath = String(entity.canonicalPath || '').trim();
                  const selected = selectedPaths.includes(canonicalPath);
                  const watchState = watchStates[canonicalPath] || 'idle';
                  const portfolioState = portfolioStates[canonicalPath] || 'idle';
                  const metadataParts = [entity.location, entity.industry].filter(Boolean);
                  const whyLabel = whySurfacedLabel(canonicalPath, eventCoreSet, affectedZoneSet, haloSet, suggestedScopeSet);
                  const whyKind =
                    whyLabel === 'Fresh signal core'
                      ? 'core'
                      : whyLabel === 'Affected zone'
                        ? 'zone'
                        : whyLabel === 'Signal halo'
                          ? 'halo'
                          : whyLabel === 'Suggested scope'
                            ? 'scope'
                            : 'neutral';
                  return (
                    <div
                      key={canonicalPath}
                      style={{
                        borderRadius: 14,
                        border: '1px solid var(--border-subtle)',
                        background: selected ? 'rgba(17,153,142,0.08)' : 'var(--bg-base)',
                        padding: 12,
                      }}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                        <div style={{ minWidth: 0 }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', marginBottom: 8 }}>
                            {entity.type ? (
                              <span style={reasonPillStyle('neutral')}>
                                {formatEntityType(entity.type)}
                              </span>
                            ) : null}
                            <span style={reasonPillStyle(whyKind)}>
                              {whyLabel}
                            </span>
                          </div>
                          <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-heading)' }}>
                            {entity.name || humanizeCanonicalPath(canonicalPath)}
                          </div>
                          {metadataParts.length > 0 && (
                            <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                              {metadataParts.join(' • ')}
                            </div>
                          )}
                          <div
                            title={canonicalPath}
                            style={{
                              marginTop: 8,
                              fontSize: 11,
                              color: 'var(--text-tertiary-weak)',
                              wordBreak: 'break-word',
                              fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                            }}
                          >
                            {compactCanonicalPath(canonicalPath)}
                          </div>
                          {entity.description && (
                            <div
                              style={{
                                marginTop: 10,
                                fontSize: 12,
                                color: 'var(--text-secondary-strong)',
                                display: '-webkit-box',
                                WebkitLineClamp: 3,
                                WebkitBoxOrient: 'vertical',
                                overflow: 'hidden',
                              }}
                            >
                              {entity.description}
                            </div>
                          )}
                        </div>
                        <div style={{ flexShrink: 0 }}>
                          <button
                            type="button"
                            onClick={() => {
                              setSelectedPaths((current) => selected ? current.filter((item) => item !== canonicalPath) : [...current, canonicalPath]);
                            }}
                            style={reasonPillStyle(selected ? 'scope' : 'neutral')}
                          >
                            {selected ? 'In lens scope' : 'Add to scope'}
                          </button>
                        </div>
                      </div>
                      <div style={{ display: 'flex', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
                        <button
                          type="button"
                          onClick={() => void handleAddToWatch(entity)}
                          disabled={watchState === 'saving'}
                          style={chipButtonStyle(false)}
                        >
                          {watchState === 'saving' ? 'Saving…' : watchState === 'saved' ? 'Saved to watch' : watchState === 'error' ? 'Retry watch' : 'Add to watch'}
                        </button>
                        <button
                          type="button"
                          onClick={() => void handleAddToPortfolio(entity)}
                          disabled={portfolioState === 'saving'}
                          style={chipButtonStyle(false)}
                        >
                          {portfolioState === 'saving' ? 'Saving…' : portfolioState === 'saved' ? 'Saved to portfolio' : portfolioState === 'error' ? 'Retry portfolio' : 'Add to portfolio'}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              <div style={{ ...PANEL_STYLE, padding: 16 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                  Fresh signal core
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {displayedCorePaths.length > 0 ? displayedCorePaths.map((path) => (
                    <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(true)}>
                      {buildSignalChipLabel(path)}
                    </button>
                  )) : (
                    <div style={{ fontSize: 12, color: 'var(--text-tertiary-weak)' }}>No fresh event-core refs in this run.</div>
                  )}
                </div>
              </div>

              <div style={{ ...PANEL_STYLE, padding: 16 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                  Halo and affected zones
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: displayedAffectedZones.length > 0 ? 12 : 0 }}>
                  {displayedHaloPaths.length > 0 ? displayedHaloPaths.map((path) => (
                    <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(false)}>
                      {buildSignalChipLabel(path)}
                    </button>
                  )) : (
                    <div style={{ fontSize: 12, color: 'var(--text-tertiary-weak)' }}>No additional halo refs beyond the event core.</div>
                  )}
                </div>
                {displayedAffectedZones.length > 0 && (
                  <>
                    <div style={{ fontSize: 11, color: 'var(--text-tertiary-weak)', marginBottom: 8 }}>
                      Affected zones
                    </div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {displayedAffectedZones.map((path) => (
                        <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(true)}>
                          {buildSignalChipLabel(path)}
                        </button>
                      ))}
                    </div>
                  </>
                )}
              </div>

              <div style={{ ...PANEL_STYLE, padding: 16 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                  Grounded evidence
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {displayedEvidenceHits.map((hit) => (
                    <div key={hit.id} style={{ borderRadius: 14, border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', padding: 12 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', marginBottom: 8 }}>
                        <span style={reasonPillStyle('neutral')}>{hit.evidenceMetaLabel}</span>
                      </div>
                      <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>{buildEvidenceTitle(hit)}</div>
                      <div style={{ marginTop: 6, fontSize: 12, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                        {hit.evidenceWhy}
                      </div>
                      <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-tertiary-weak)' }}>
                        relevance score {Number(hit.score || 0).toFixed(2)}
                      </div>
                      {(hit.displayPaths || []).length > 0 && (
                        <div style={{ marginTop: 8, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                          {(hit.displayPaths || []).map((path) => (
                            <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(false)}>
                              {buildSignalChipLabel(path)}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>

              {displayedWarnings.length > 0 && (
                <div style={{ ...PANEL_STYLE, padding: 16 }}>
                  <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                    Warnings
                  </div>
                  <ul style={{ margin: 0, paddingLeft: 18, color: 'var(--text-secondary-strong)', fontSize: 12, lineHeight: 1.5 }}>
                    {displayedWarnings.slice(0, 6).map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          </div>
          </>
        )}
      </div>
    </div>
  );
};
