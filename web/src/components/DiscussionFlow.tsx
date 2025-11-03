import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { DiscussionFlowAnalysis, isGeneratingDiscussionFlowAnalysis } from '../lib/discussionFlowAnalysis';
import { useTranslation } from 'react-i18next';

export const DiscussionFlow: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const { t } = useTranslation();
  const notifiedRef = React.useRef(false);
  
  // Reset notifiedRef on unmount to allow re-notification if component is reopened
  React.useEffect(() => {
    return () => {
      notifiedRef.current = false;
    };
  }, []);

  // CRITICAL PERFORMANCE FIX: Subscribe only to interactions.length, not the entire array
  // This prevents re-renders when interactions are updated (text selection, highlighting, etc.)
  const projectId = useGenaiChatStore((state) => state.projectId);
  const interactionsLength = useGenaiChatStore((state) => state.interactions.length);
  
  // Store analysis data in state, update only when interactions.length changes
  const [analysisInteraction, setAnalysisInteraction] = React.useState<any>(null);
  const [chatMessageCount, setChatMessageCount] = React.useState(0);
  
  // Track mounted state to prevent updates after unmount
  const mountedRef = React.useRef(true);
  
  React.useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Update analysis interaction only when interactions array length changes
  React.useEffect(() => {
    // Skip if component is unmounted
    if (!mountedRef.current) return;
    
    const state = useGenaiChatStore.getState();
    const interactions = state.interactions;
    
    // Find analysis interaction
    const found = interactions.find((it: any) => 
      it.subkind === 'discussion.flow.analysis' && it.projectId === state.projectId
    );
    
    // Count chat messages
    const count = interactions.filter((it: any) => 
      it.kind === 'user' || it.kind === 'assistant'
    ).length;
    
    // Only update state if still mounted
    if (mountedRef.current) {
      setAnalysisInteraction(found || null);
      setChatMessageCount(count);
    }
  }, [projectId, interactionsLength]);

  const analysis: DiscussionFlowAnalysis | null = React.useMemo(() => {
    if (!analysisInteraction || !analysisInteraction.content) return null;
    try {
      const data = JSON.parse(analysisInteraction.content || analysisInteraction.contentSanitized || '{}');
      if (data.blindSpots && Array.isArray(data.blindSpots) && 
          data.recommendations && Array.isArray(data.recommendations) &&
          data.frame && typeof data.frame === 'string' &&
          data.confidence && ['low', 'medium', 'high'].includes(data.confidence)) {
        return data as DiscussionFlowAnalysis;
      }
    } catch (e) {
      console.warn('[DiscussionFlow] Failed to parse analysis', e);
    }
    return null;
  }, [analysisInteraction]);

  React.useEffect(() => {
    // Notify availability once analysis exists
    if (analysis && !notifiedRef.current && onFirstResult) {
      notifiedRef.current = true;
      // Use setTimeout to avoid calling during render
      const timeoutId = setTimeout(() => {
        // Check if component is still mounted before calling callback
        if (mountedRef.current) {
          try {
            onFirstResult();
          } catch (e) {
            console.warn('[DiscussionFlow] onFirstResult callback error', e);
          }
        }
      }, 0);
      
      // Cleanup timeout on unmount
      return () => {
        clearTimeout(timeoutId);
      };
    }
  }, [analysis, onFirstResult]);

  // Memoize helper functions to prevent recreation on every render
  // MUST be called before any early returns to follow Rules of Hooks
  const getConfidenceColor = React.useCallback((confidence: string) => {
    switch (confidence) {
      case 'low': return 'var(--text-tertiary-weak)';
      case 'medium': return 'var(--text-secondary-strong)';
      case 'high': return '#669999';
      default: return 'var(--text-secondary-strong)';
    }
  }, []);

  // Memoize translation strings to avoid calling t() on every render
  const confidenceLabels = React.useMemo(() => ({
    low: t('unfold.discussionFlow.confidence.low', { defaultValue: 'Low' }),
    medium: t('unfold.discussionFlow.confidence.medium', { defaultValue: 'Medium' }),
    high: t('unfold.discussionFlow.confidence.high', { defaultValue: 'High' }),
  }), [t]);

  const getConfidenceLabel = React.useCallback((confidence: string) => {
    return confidenceLabels[confidence as keyof typeof confidenceLabels] || confidence;
  }, [confidenceLabels]);

  // Check if analysis is being generated
  // Memoize to prevent infinite re-renders
  const isLoading = React.useMemo(() => {
    if (!projectId || projectId === 'local-unfold') return false;
    if (chatMessageCount < 2) return false;
    if (analysis) return false;
    return isGeneratingDiscussionFlowAnalysis(projectId, chatMessageCount);
  }, [projectId, chatMessageCount, analysis]);

  // Memoize empty message translation
  const emptyMessage = React.useMemo(
    () => t('unfold.discussionFlow.empty', { defaultValue: 'Analysis will appear here after a few messages' }),
    [t]
  );

  // Memoize section labels
  const sectionLabels = React.useMemo(() => ({
    frame: t('unfold.discussionFlow.frame', { defaultValue: 'Discussion Frame' }),
    confidence: t('unfold.discussionFlow.confidence.label', { defaultValue: 'Confidence' }),
    blindSpots: t('unfold.discussionFlow.blindSpots', { defaultValue: 'Blind Spots' }),
    recommendations: t('unfold.discussionFlow.recommendations', { defaultValue: 'Recommendations' }),
  }), [t]);

  // Memoize style objects to prevent creating new objects on every render
  const textSecondaryStrongStyle = React.useMemo(() => ({ color: 'var(--text-secondary-strong)' }), []);
  const textTertiaryWeakStyle = React.useMemo(() => ({ color: 'var(--text-tertiary-weak)' }), []);
  const textHeadingStyle = React.useMemo(() => ({ color: 'var(--text-heading)', lineHeight: '1.5' }), []);
  const listStyle = React.useMemo(() => ({ listStyle: 'none', padding: 0, margin: 0 }), []);
  const dangerColorStyle = React.useMemo(() => ({ color: 'var(--danger-50)' }), []);
  const recommendationColorStyle = React.useMemo(() => ({ color: '#669999' }), []);

  // Show empty message only if chat is empty (less than 2 messages)
  if (!analysis && !isLoading && chatMessageCount < 2) {
    return (
      <div className="mt-3 p-3">
        <div className="text-12" style={textSecondaryStrongStyle}>
          {emptyMessage}
        </div>
      </div>
    );
  }

  // Show skeleton if loading
  if (isLoading) {
    return (
      <div className="mt-3 p-3">
        <div className="space-y-4">
          {/* Frame skeleton */}
          <div className="space-y-2">
            <div style={{ height: 10, width: '40%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
            <div style={{ height: 14, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
            <div style={{ height: 14, width: '90%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
          </div>
          {/* Confidence skeleton */}
          <div className="space-y-2">
            <div style={{ height: 10, width: '30%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
            <div style={{ height: 12, width: '20%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
          </div>
          {/* Blind Spots skeleton */}
          <div className="space-y-2">
            <div style={{ height: 10, width: '35%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
            <div className="flex items-start gap-2">
              <div style={{ width: 12, height: 12, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden', marginTop: 2 }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 14, flex: 1, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
            <div className="flex items-start gap-2">
              <div style={{ width: 12, height: 12, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden', marginTop: 2 }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 14, flex: 1, width: '85%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
          </div>
          {/* Recommendations skeleton */}
          <div className="space-y-2">
            <div style={{ height: 10, width: '40%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
            </div>
            <div className="flex items-start gap-2">
              <div style={{ width: 12, height: 12, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden', marginTop: 2 }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 14, flex: 1, width: '80%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // If no analysis and not loading, show empty (shouldn't happen if chat has messages, but safety check)
  if (!analysis) {
    return (
      <div className="mt-3 p-3">
        <div className="text-12" style={textSecondaryStrongStyle}>
          {emptyMessage}
        </div>
      </div>
    );
  }

  return (
    <div className="mt-3 p-3">
        {/* Frame */}
      {analysis.frame && (
        <div className="mb-4">
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {sectionLabels.frame}
          </div>
          <div className="text-12" style={textHeadingStyle}>
            {analysis.frame}
          </div>
        </div>
      )}

      {/* Confidence */}
      <div className="mb-4">
        <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
          {sectionLabels.confidence}
        </div>
        <div className="text-12" style={{ color: getConfidenceColor(analysis.confidence) } as React.CSSProperties}>
          {getConfidenceLabel(analysis.confidence)}
        </div>
      </div>

      {/* Blind Spots */}
      {analysis.blindSpots && analysis.blindSpots.length > 0 && (
        <div className="mb-4">
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {sectionLabels.blindSpots}
          </div>
          <ul className="space-y-1.5" style={listStyle}>
            {analysis.blindSpots.map((spot, idx) => (
              <li key={idx} className="flex items-start gap-2">
                <span className="text-12 mt-0.5" style={dangerColorStyle}>⚠</span>
                <span className="text-12 flex-1" style={textHeadingStyle}>
                  {spot}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Recommendations */}
      {analysis.recommendations && analysis.recommendations.length > 0 && (
        <div className="mb-4">
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {sectionLabels.recommendations}
          </div>
          <ul className="space-y-1.5" style={listStyle}>
            {analysis.recommendations.map((rec, idx) => (
              <li key={idx} className="flex items-start gap-2">
                <span className="text-12 mt-0.5" style={recommendationColorStyle}>→</span>
                <span className="text-12 flex-1" style={textHeadingStyle}>
                  {rec}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};
