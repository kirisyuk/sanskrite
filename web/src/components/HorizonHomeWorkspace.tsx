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
  background: 'var(--bg-elevated)',
  boxShadow: '0 18px 40px rgba(0,0,0,0.18)',
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

function chipButtonStyle(active: boolean): React.CSSProperties {
  return {
    padding: '8px 10px',
    borderRadius: 999,
    border: `1px solid ${active ? 'var(--accent-primary)' : 'var(--border-subtle)'}`,
    background: active ? 'rgba(17, 153, 142, 0.14)' : 'var(--bg-base)',
    color: 'var(--text-heading)',
    fontSize: 12,
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

  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        gap: 16,
        padding: 16,
        overflow: 'auto',
      }}
    >
      <div style={{ ...PANEL_STYLE, padding: 22 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 16, alignItems: 'flex-start', marginBottom: 14 }}>
          <div>
            <div style={{ fontSize: 12, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-tertiary-weak)', marginBottom: 8 }}>
              Horizon Home
            </div>
            <h1 style={{ margin: 0, fontSize: 26, lineHeight: 1.05, color: 'var(--text-heading)' }}>
              Explore first. Promote later.
            </h1>
            <p style={{ margin: '10px 0 0 0', maxWidth: 760, fontSize: 14, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
              Ask from Home, land fresh signals and structural context on real entities, review the explanation and map, then turn the result into a lens or save objects to watch and portfolio.
            </p>
          </div>
          <button
            type="button"
            onClick={handleCreateLens}
            disabled={!result || selectedPaths.length === 0 || creatingLens}
            style={{
              padding: '10px 14px',
              borderRadius: 12,
              border: '1px solid var(--accent-primary)',
              background: creatingLens ? 'rgba(17,153,142,0.16)' : 'var(--accent-primary)',
              color: creatingLens ? 'var(--text-heading)' : '#fff',
              fontSize: 13,
              fontWeight: 700,
              cursor: !result || selectedPaths.length === 0 || creatingLens ? 'default' : 'pointer',
              opacity: !result || selectedPaths.length === 0 ? 0.45 : 1,
            }}
          >
            {creatingLens ? 'Creating lens…' : 'Create lens'}
          </button>
        </div>

        <div style={{ display: 'flex', gap: 10, alignItems: 'stretch' }}>
          <textarea
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                event.preventDefault();
                void runWorkspace();
              }
            }}
            placeholder="Ask about a disruption, weak point, dependency, route, geography, product, or company…"
            style={{
              flex: 1,
              minHeight: 110,
              resize: 'vertical',
              borderRadius: 16,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-base)',
              color: 'var(--text-heading)',
              padding: 14,
              fontSize: 15,
              lineHeight: 1.45,
              outline: 'none',
            }}
          />
          <button
            type="button"
            onClick={() => void runWorkspace()}
            disabled={!query.trim() || isRunning}
            style={{
              width: 144,
              borderRadius: 16,
              border: '1px solid var(--accent-primary)',
              background: isRunning ? 'rgba(17,153,142,0.16)' : 'var(--accent-primary)',
              color: isRunning ? 'var(--text-heading)' : '#fff',
              fontSize: 14,
              fontWeight: 700,
              cursor: !query.trim() || isRunning ? 'default' : 'pointer',
              opacity: !query.trim() ? 0.45 : 1,
            }}
          >
            {isRunning ? 'Running…' : 'Run workspace'}
          </button>
        </div>
        <div style={{ marginTop: 10, fontSize: 12, color: 'var(--text-tertiary-weak)' }}>
          `Cmd/Ctrl + Enter` to run. Home uses the same signal landing, analytical retrieval, and fresh-signal prioritization as lens chat.
        </div>
        {error && (
          <div style={{ marginTop: 12, fontSize: 13, color: '#f87171' }}>
            {error}
          </div>
        )}
      </div>

      {result && (
        <>
          <div style={{ ...PANEL_STYLE, padding: 22 }}>
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

          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1.2fr) minmax(320px, 0.8fr)', gap: 16 }}>
            <div style={{ ...PANEL_STYLE, padding: 20 }}>
              <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 12 }}>
                Workspace objects
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {displayedEntities.map((entity) => {
                  const canonicalPath = String(entity.canonicalPath || '').trim();
                  const selected = selectedPaths.includes(canonicalPath);
                  const watchState = watchStates[canonicalPath] || 'idle';
                  const portfolioState = portfolioStates[canonicalPath] || 'idle';
                  return (
                    <div
                      key={canonicalPath}
                      style={{
                        borderRadius: 16,
                        border: '1px solid var(--border-subtle)',
                        background: selected ? 'rgba(17,153,142,0.08)' : 'var(--bg-base)',
                        padding: 14,
                      }}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                        <div style={{ minWidth: 0 }}>
                          <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-heading)' }}>
                            {entity.name || humanizeCanonicalPath(canonicalPath)}
                          </div>
                          <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-tertiary-weak)', wordBreak: 'break-word' }}>
                            {canonicalPath}
                          </div>
                          {(entity.type || entity.location || entity.industry) && (
                            <div style={{ marginTop: 6, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                              {[entity.type, entity.location, entity.industry].filter(Boolean).join(' • ')}
                            </div>
                          )}
                          {entity.description && (
                            <div style={{ marginTop: 8, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                              {entity.description}
                            </div>
                          )}
                        </div>
                        <button
                          type="button"
                          onClick={() => {
                            setSelectedPaths((current) => selected ? current.filter((item) => item !== canonicalPath) : [...current, canonicalPath]);
                          }}
                          style={chipButtonStyle(selected)}
                        >
                          {selected ? 'In lens scope' : 'Select'}
                        </button>
                      </div>
                      <div style={{ display: 'flex', gap: 8, marginTop: 12, flexWrap: 'wrap' }}>
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
              <div style={{ ...PANEL_STYLE, padding: 18 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                  Fresh signal core
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {(result.eventCorePaths || []).length > 0 ? (result.eventCorePaths || []).map((path) => (
                    <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(true)}>
                      {humanizeCanonicalPath(path)}
                    </button>
                  )) : (
                    <div style={{ fontSize: 12, color: 'var(--text-tertiary-weak)' }}>No fresh event-core refs in this run.</div>
                  )}
                </div>
              </div>

              <div style={{ ...PANEL_STYLE, padding: 18 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                  Halo and affected zones
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: (result.affectedZoneRefs || []).length > 0 ? 12 : 0 }}>
                  {(result.haloPaths || []).slice(0, 12).map((path) => (
                    <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(false)}>
                      {humanizeCanonicalPath(path)}
                    </button>
                  ))}
                </div>
                {(result.affectedZoneRefs || []).length > 0 && (
                  <>
                    <div style={{ fontSize: 11, color: 'var(--text-tertiary-weak)', marginBottom: 8 }}>
                      Affected zones
                    </div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {(result.affectedZoneRefs || []).map((path) => (
                        <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(true)}>
                          {humanizeCanonicalPath(path)}
                        </button>
                      ))}
                    </div>
                  </>
                )}
              </div>

              <div style={{ ...PANEL_STYLE, padding: 18 }}>
                <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                  Grounded evidence
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {(result.hits || []).slice(0, 6).map((hit) => (
                    <div key={hit.id} style={{ borderRadius: 14, border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', padding: 12 }}>
                      <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>{hit.title}</div>
                      <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-tertiary-weak)' }}>
                        {hit.kind} • score {Number(hit.score || 0).toFixed(2)}
                      </div>
                      {(hit.canonicalPaths || []).length > 0 && (
                        <div style={{ marginTop: 8, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                          {(hit.canonicalPaths || []).slice(0, 4).map((path) => (
                            <button key={path} type="button" onClick={() => onMapFocusChange?.({ sourceMessageId: result.runId, canonicalPaths: [path] })} style={chipButtonStyle(false)}>
                              {humanizeCanonicalPath(path)}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>

              {(result.warnings || []).length > 0 && (
                <div style={{ ...PANEL_STYLE, padding: 18 }}>
                  <div style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-tertiary-weak)', marginBottom: 10 }}>
                    Warnings
                  </div>
                  <ul style={{ margin: 0, paddingLeft: 18, color: 'var(--text-secondary-strong)', fontSize: 12, lineHeight: 1.5 }}>
                    {(result.warnings || []).slice(0, 6).map((warning) => (
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
  );
};
