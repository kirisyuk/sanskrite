import React from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { ThemeContext } from './ThemeProvider';
import { generatePerspectiveAnalysis, PerspectiveAnalysis } from '../lib/perspectiveAnalysis';
import { api } from '../lib/api';

interface PerspectiveAnalysisPanelProps {
  preset: string;
  perspectiveId: string | null;
  perspectiveContent: string | null;
  onClose?: () => void;
  onApplyKeyPoint?: (keyPoint: string) => void;
}

const PerspectiveAnalysisPanel: React.FC<PerspectiveAnalysisPanelProps> = ({ preset, perspectiveId, perspectiveContent, onClose, onApplyKeyPoint }) => {
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const [analysis, setAnalysis] = React.useState<PerspectiveAnalysis | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  // PERFORMANCE FIX: Subscribe only to interactions.length, not entire array
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const persistedByLocalId = useGenaiChatStore((s) => (s as any).persistedByLocalId);
  const projectId = useGenaiChatStore((s) => s.projectId);
  
  // Get interactions only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);

  // Load existing analysis when perspective changes
  React.useEffect(() => {
    if (!perspectiveId || !preset) {
      setAnalysis(null);
      return;
    }

    const currentInteractions = getInteractions();
    const map = persistedByLocalId || {};
    const serverId = map[perspectiveId] || perspectiveId;
    
    // Build reverse map for bidirectional lookup
    const reverseMap: Record<string, string[]> = {};
    Object.entries(map).forEach(([local, server]) => {
      if (!reverseMap[server as string]) reverseMap[server as string] = [];
      reverseMap[server as string].push(local);
    });
    const localIds = reverseMap[perspectiveId] || [];

    const analysisSubkind = `perspective.${preset}.analysis`;
    const existingAnalysis = currentInteractions.find((it) => 
      it.subkind === analysisSubkind && 
      (it.replyTo === perspectiveId || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
       (it.relations && it.relations.some((r) => r.targetId === perspectiveId || r.targetId === serverId)))
    );

    if (existingAnalysis && existingAnalysis.content) {
      try {
        const data = JSON.parse(existingAnalysis.content || existingAnalysis.contentSanitized || '{}');
        if (data.title && Array.isArray(data.keyPoints) && Array.isArray(data.blindSpots)) {
          setAnalysis(data as PerspectiveAnalysis);
        } else {
          setAnalysis(null);
        }
      } catch (e) {
        console.warn('[PerspectiveAnalysisPanel] Failed to parse analysis', e);
        setAnalysis(null);
      }
    } else {
      setAnalysis(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [perspectiveId, preset, interactionsLength, getInteractions, persistedByLocalId]);

  const handleGenerate = React.useCallback(async () => {
    if (!perspectiveId || !perspectiveContent || !preset) {
      setError('No perspective content available');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      await generatePerspectiveAnalysis(perspectiveId, perspectiveContent, preset);
      // Analysis will be loaded via useEffect when interactions update
    } catch (e: any) {
      setError('Failed to generate analysis');
      console.error('[PerspectiveAnalysisPanel] Generation failed', e);
    } finally {
      setLoading(false);
    }
  }, [perspectiveId, perspectiveContent, preset]);

  const handleRefresh = React.useCallback(async () => {
    if (!perspectiveId || !preset) return;

    const map = persistedByLocalId || {};
    const serverId = map[perspectiveId] || perspectiveId;
    
    // Build reverse map for bidirectional lookup
    const reverseMap: Record<string, string[]> = {};
    Object.entries(map).forEach(([local, server]) => {
      if (!reverseMap[server as string]) reverseMap[server as string] = [];
      reverseMap[server as string].push(local);
    });
    const localIds = reverseMap[perspectiveId] || [];
    
    const analysisSubkind = `perspective.${preset}.analysis`;
    const interactions = getInteractions();
    const existingAnalysis = interactions.find((it) => 
      it.subkind === analysisSubkind && 
      (it.replyTo === perspectiveId || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
       (it.relations && it.relations.some((r: any) => r.targetId === perspectiveId || r.targetId === serverId)))
    );

    // Delete old analysis before generating new one
    if (existingAnalysis && existingAnalysis.id) {
      const currentProjectId = projectId;
      if (currentProjectId && currentProjectId !== 'local-unfold') {
        try {
          await api.deleteProjectInteraction(currentProjectId, existingAnalysis.id);
          // Remove from local state
          useGenaiChatStore.setState((s) => ({
            interactions: s.interactions.filter((it) => it.id !== existingAnalysis.id)
          }));
        } catch (e) {
          console.warn('[PerspectiveAnalysisPanel] Failed to delete old analysis', e);
        }
      }
    }

    await handleGenerate();
  }, [perspectiveId, preset, interactionsLength, handleGenerate]);

  const containerStyle: React.CSSProperties = React.useMemo(() => ({
    borderColor: 'var(--overlay-line-90)',
    backgroundColor: 'var(--bg-elevated)',
    boxShadow: theme === 'light' ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
  }), [theme]);

  if (!perspectiveId || !perspectiveContent) {
    return (
      <div className="flex flex-col flex-1" style={{ minHeight: 0 }}>
        <div className="rounded-xl border p-4" style={containerStyle}>
          <div className="text-12 text-center" style={{ color: 'var(--text-secondary-strong)' }}>
            {t('unfold.perspectives.analysis.empty', { defaultValue: 'Generate a perspective to see analysis' })}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col flex-1" style={{ minHeight: 0 }}>
      <div className="rounded-xl border p-4 flex-1 flex flex-col" style={containerStyle}>
        {/* Header with close button */}
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>
            {t('unfold.perspectives.analysis.title', { defaultValue: 'Analysis' })}
          </h3>
          <button
            type="button"
            onClick={() => onClose?.()}
            className="p-1 rounded"
            style={{ 
              background: 'transparent', 
              color: 'var(--text-secondary-strong)', 
              cursor: 'pointer',
            }}
            title={t('unfold.perspectives.analysis.close', { defaultValue: 'Close' })}
            onMouseEnter={(e) => { 
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; 
            }}
            onMouseLeave={(e) => { 
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; 
            }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18"></line>
              <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
          </button>
        </div>

        {/* Scrollable content area */}
        <div className="flex-1 overflow-y-auto mb-3" style={{ minHeight: 0 }}>
          {/* Loading state */}
          {loading && (
            <div className="space-y-2">
              <div style={{ height: 14, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 10, width: '90%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
          )}

          {/* Error state */}
          {error && (
            <div className="text-12 mb-2" style={{ color: 'var(--danger-50)' }}>{error}</div>
          )}

          {/* Analysis content */}
          {!loading && analysis && (
            <div className="space-y-4">
              {/* Title */}
              {analysis.title && (
                <div>
                  <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {t('unfold.perspectives.analysis.sections.title', { defaultValue: 'Title' })}
                  </div>
                  <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>
                    {analysis.title}
                  </div>
                </div>
              )}

              {/* Key Points */}
              {analysis.keyPoints && analysis.keyPoints.length > 0 && (
                <div>
                  <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {t('unfold.perspectives.analysis.sections.keyPoints', { defaultValue: 'Key Points' })}
                  </div>
                  <ul className="space-y-1.5" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
                    {analysis.keyPoints.map((point, idx) => (
                      <li key={idx} className="flex items-start gap-2">
                        <span className="text-12 mt-0.5" style={{ color: 'var(--text-secondary-strong)' }}>•</span>
                        <span className="text-12 flex-1" style={{ color: 'var(--text-heading)', lineHeight: '1.5' }}>
                          {point}
                        </span>
                        {onApplyKeyPoint && (
                          <button
                            type="button"
                            onClick={() => onApplyKeyPoint(point)}
                            className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0"
                            style={{
                              background: 'var(--accent-primary)',
                              color: 'var(--text-on-accent)',
                              border: 'none',
                              cursor: 'pointer',
                            }}
                            title={t('unfold.perspectives.analysis.apply', { defaultValue: 'Apply to chat' })}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.opacity = '0.85';
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                            }}
                          >
                            {t('unfold.perspectives.analysis.applyButton', { defaultValue: 'Apply' })}
                          </button>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {/* Blind Spots */}
              {analysis.blindSpots && analysis.blindSpots.length > 0 && (
                <div>
                  <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {t('unfold.perspectives.analysis.sections.blindSpots', { defaultValue: 'Blind Spots' })}
                  </div>
                  <ul className="space-y-1.5" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
                    {analysis.blindSpots.map((spot, idx) => (
                      <li key={idx} className="flex items-start gap-2">
                        <span className="text-12 mt-0.5" style={{ color: 'var(--danger-50)' }}>⚠</span>
                        <span className="text-12 flex-1" style={{ color: 'var(--text-heading)', lineHeight: '1.5' }}>
                          {spot}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          {/* Empty state - no analysis yet */}
          {!loading && !analysis && !error && (
            <div className="text-center py-6">
              <div className="text-12 mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
                {t('unfold.perspectives.analysis.generate', { defaultValue: 'Click refresh to generate analysis' })}
              </div>
            </div>
          )}
        </div>

        {/* Refresh button at the bottom */}
        <div className="mt-auto pt-3 border-t" style={{ borderColor: 'var(--overlay-line-90)' }}>
          <button
            type="button"
            onClick={handleRefresh}
            disabled={loading || !perspectiveContent}
            className="w-full py-2 px-3 rounded text-12 font-medium transition-colors"
            style={{ 
              background: (loading || !perspectiveContent) ? 'var(--white-06)' : 'var(--white-10)', 
              color: (loading || !perspectiveContent) ? 'var(--text-tertiary-weak)' : 'var(--text-heading)', 
              cursor: (loading || !perspectiveContent) ? 'not-allowed' : 'pointer',
              border: 'none',
            }}
            onMouseEnter={(e) => { 
              if (!loading && perspectiveContent) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)'; 
              }
            }}
            onMouseLeave={(e) => { 
              if (!loading && perspectiveContent) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; 
              }
            }}
          >
            {loading ? (
              t('unfold.perspectives.analysis.refreshing', { defaultValue: 'Refreshing...' })
            ) : (
              t('unfold.perspectives.analysis.refresh', { defaultValue: 'Refresh Analysis' })
            )}
          </button>
        </div>
      </div>
    </div>
  );
};

export default PerspectiveAnalysisPanel;

