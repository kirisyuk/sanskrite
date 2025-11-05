import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { DiscussionFlowAnalysis, isGeneratingDiscussionFlowAnalysis, generateDiscussionFlowAnalysis } from '../lib/discussionFlowAnalysis';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { getPreferredLanguage, getLanguageName } from '../lib/uiUtils';
import { TextTask } from './TextTasksPanel';

interface DiscussionFlowProps {
  onFirstResult?: () => void;
  onAddTextTask?: (task: TextTask) => void;
  onRemoveTextTask?: (taskId: string) => void;
  textTasks?: TextTask[];
}

export const DiscussionFlow: React.FC<DiscussionFlowProps> = ({ onFirstResult, onAddTextTask, onRemoveTextTask, textTasks = [] }) => {
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
  const [isRefreshing, setIsRefreshing] = React.useState(false);
  const [applyingIndex, setApplyingIndex] = React.useState<number | null>(null);
  // Track which blind spots have prompts generated
  const [blindSpotPrompts, setBlindSpotPrompts] = React.useState<Record<string, string>>({});
  const [generatingPrompts, setGeneratingPrompts] = React.useState<Record<string, boolean>>({});
  
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
          data.frameShiftRecommendations && Array.isArray(data.frameShiftRecommendations) &&
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
    if (analysis && !isRefreshing) return false;
    return isGeneratingDiscussionFlowAnalysis(projectId, chatMessageCount) || isRefreshing;
  }, [projectId, chatMessageCount, analysis, isRefreshing]);

  // Memoize empty message translation
  const emptyMessage = React.useMemo(
    () => t('unfold.discussionFlow.empty', { defaultValue: 'Nothing to analyze yet. Start your discussion and we\'ll examine how it flows.' }),
    [t]
  );

  // Handle refresh - regenerate analysis from full discussion
  const handleRefresh = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    if (chatMessageCount < 2) return;
    if (isRefreshing) return;

    setIsRefreshing(true);
    try {
      const state = useGenaiChatStore.getState();
      const interactions = state.interactions;
      
      // Delete existing analysis if it exists
      if (analysisInteraction && analysisInteraction.id) {
        try {
          await api.deleteProjectInteraction(projectId, analysisInteraction.id);
          // Remove from local state
          useGenaiChatStore.setState((s) => ({
            interactions: s.interactions.filter((it) => it.id !== analysisInteraction.id)
          }));
        } catch (e) {
          console.warn('[DiscussionFlow] Failed to delete old analysis', e);
          // Continue anyway - will overwrite
        }
      }

      // Get all chat messages (user, assistant, agent)
      const chatMessages: Array<{ role: string; content: string }> = interactions
        .filter((it) => 
          it.kind === 'user' || it.kind === 'assistant'
        )
        .map((it) => ({
          role: it.kind === 'user' ? 'user' : 'assistant',
          content: it.content || '',
          ...((it as any).agentProfile ? { agentProfile: (it as any).agentProfile } : {}),
        }));

      // Fallback to messages array if no interactions
      let finalMessages = chatMessages;
      if (finalMessages.length === 0 && state.messages.length > 0) {
        finalMessages = state.messages.map((m) => ({
          role: m.role,
          content: m.content || '',
        }));
      }

      if (finalMessages.length === 0) {
        setIsRefreshing(false);
        return;
      }

      // Generate full analysis (no existingAnalysis, no lastPair = full recompute)
      // generateDiscussionFlowAnalysis will handle context window limitation (70%) and filtering
      await generateDiscussionFlowAnalysis(
        projectId,
        finalMessages,
        null, // no existing analysis
        undefined // no last pair - full recompute
      );
    } catch (e) {
      console.error('[DiscussionFlow] Failed to refresh analysis', e);
    } finally {
      setIsRefreshing(false);
    }
  }, [projectId, chatMessageCount, analysisInteraction, isRefreshing]);

  // Memoize section labels
  const sectionLabels = React.useMemo(() => ({
    frame: t('unfold.discussionFlow.frame', { defaultValue: 'Discussion Frame' }),
    confidence: t('unfold.discussionFlow.confidence.label', { defaultValue: 'Confidence' }),
    blindSpots: t('unfold.discussionFlow.blindSpots', { defaultValue: 'Blind Spots' }),
    frameShiftRecommendations: t('unfold.discussionFlow.frameShiftRecommendations', { defaultValue: 'You can refine the discussion focus' }),
  }), [t]);

  // Handle blind spot checkbox toggle - generate prompt and add/remove task
  const handleBlindSpotToggle = React.useCallback(async (blindSpot: string, index: number) => {
    const taskId = `blind-spot-${index}`;
    const existingTask = textTasks?.find((t) => t.id === taskId);
    
    // If task exists, remove it
    if (existingTask) {
      onRemoveTextTask?.(taskId);
      return;
    }

    // Check if prompt already generated
    if (blindSpotPrompts[taskId]) {
      // Add task with existing prompt
      onAddTextTask?.({
        id: taskId,
        title: blindSpot,
        prompt: blindSpotPrompts[taskId],
      });
      return;
    }

    // Generate prompt
    if (generatingPrompts[taskId]) return; // Already generating

    setGeneratingPrompts((prev) => ({ ...prev, [taskId]: true }));
    try {
      const settingsState = useGenAISettings.getState();
      const evalModel = (settingsState.stanceModel && settingsState.stanceModel.trim()) 
        ? settingsState.stanceModel 
        : 'gemini_fast';
      const preferredLang = getPreferredLanguage();
      const langName = getLanguageName(preferredLang);

      const prompt = `You are helping to convert a blind spot (gap or missing consideration) into a well-formulated prompt for a research discussion.

The blind spot is: "${blindSpot}"

Task: Reformulate this blind spot as a clear, actionable prompt that can be used to continue the discussion. The prompt should:
- Be concise and direct
- Be formulated as a question or request that addresses the identified gap
- Guide the discussion to explore or resolve this blind spot
- Be ready to use immediately in a chat interface

Output only the reformulated prompt, without any additional explanation or commentary.

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.`;

      const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

      const res = await api.genaiGenerate({
        model: evalModel,
        prompt,
        systemInstruction: langDirective,
        maxOutputTokens: 500,
        webSearchEnabled: false,
        strictJSONEnabled: false,
      });

      const reformulatedPrompt = (res?.content || '').trim();
      
      if (!reformulatedPrompt) {
        console.warn('[DiscussionFlow] Empty response from LLM for blind spot');
        return;
      }

      // Store prompt for reuse
      setBlindSpotPrompts((prev) => ({ ...prev, [taskId]: reformulatedPrompt }));

      // Add task
      onAddTextTask?.({
        id: taskId,
        title: blindSpot,
        prompt: reformulatedPrompt,
      });
    } catch (e) {
      console.error('[DiscussionFlow] Failed to generate prompt for blind spot', e);
    } finally {
      setGeneratingPrompts((prev) => {
        const next = { ...prev };
        delete next[taskId];
        return next;
      });
    }
  }, [textTasks, blindSpotPrompts, generatingPrompts, onAddTextTask, onRemoveTextTask]);

  // Handle applying recommendation - convert to prompt and insert into input
  const handleApplyRecommendation = React.useCallback(async (recommendation: string, index: number) => {
    if (applyingIndex !== null) return; // Already processing

    setApplyingIndex(index);
    try {
      const settingsState = useGenAISettings.getState();
      const evalModel = (settingsState.stanceModel && settingsState.stanceModel.trim()) 
        ? settingsState.stanceModel 
        : 'gemini_fast';
      const preferredLang = getPreferredLanguage();
      const langName = getLanguageName(preferredLang);

      const prompt = `You are helping to convert a frame shift recommendation into a well-formulated prompt for a research discussion.

The recommendation is: "${recommendation}"

Task: Reformulate this recommendation as a clear, actionable prompt that can be used to continue the discussion. The prompt should:
- Be concise and direct
- Be formulated as a question or request that guides the discussion
- Maintain the intent and value of the original recommendation
- Be ready to use immediately in a chat interface

Output only the reformulated prompt, without any additional explanation or commentary.

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.`;

      const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

      const res = await api.genaiGenerate({
        model: evalModel,
        prompt,
        systemInstruction: langDirective,
        maxOutputTokens: 500,
        webSearchEnabled: false,
        strictJSONEnabled: false,
      });

      const reformulatedPrompt = (res?.content || '').trim();
      
      if (!reformulatedPrompt) {
        console.warn('[DiscussionFlow] Empty response from LLM');
        return;
      }

      // Insert into textarea (same approach as ActionRow)
      try {
        const container = document.getElementById('unfold-request-container');
        if (container) {
          const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
          if (textarea) {
            // Set value using React-friendly method
            const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value')?.set;
            if (nativeInputValueSetter) {
              nativeInputValueSetter.call(textarea, reformulatedPrompt);
            } else {
              textarea.value = reformulatedPrompt;
            }
            // Trigger React's onChange handler by dispatching input event
            const inputEvent = new Event('input', { bubbles: true });
            textarea.dispatchEvent(inputEvent);
            // Focus the textarea and move cursor to end
            textarea.focus();
            setTimeout(() => {
              textarea.setSelectionRange(textarea.value.length, textarea.value.length);
            }, 0);
          }
        }
      } catch (e) {
        console.warn('[DiscussionFlow] Failed to set prompt in input', e);
      }
    } catch (e) {
      console.error('[DiscussionFlow] Failed to apply recommendation', e);
    } finally {
      setApplyingIndex(null);
    }
  }, [applyingIndex, t]);

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
        <div className="text-12 text-center" style={{ ...textSecondaryStrongStyle, whiteSpace: 'pre-line' }}>
          {emptyMessage}
        </div>
      </div>
    );
  }

  // Show skeleton if loading
  if (isLoading) {
    return (
      <div className="mt-3 p-3">
        {/* Header with refresh button */}
        <div className="flex items-center justify-between mb-3">
          <div />
          <button
            type="button"
            onClick={handleRefresh}
            disabled={!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading}
            className="p-1.5 rounded transition-colors"
            style={{
              background: 'transparent',
              color: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) 
                ? 'var(--text-tertiary-weak)' 
                : 'var(--text-secondary-strong)',
              cursor: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) 
                ? 'not-allowed' 
                : 'pointer',
              opacity: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) ? 0.6 : 1
            }}
            title={t('unfold.discussionFlow.refresh', { defaultValue: 'Refresh Analysis' })}
            onMouseEnter={(e) => {
              if (!(!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading)) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
            }}
          >
            <svg 
              className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} 
              viewBox="0 0 24 24" 
              fill="none" 
              stroke="currentColor"
            >
              <path d="M20 7h-5M20 7v5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              <path d="M20 12a8 8 0 1 1-2.343-5.657" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </button>
        </div>
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
            <div className="flex items-center gap-2">
              <div style={{ width: 12, height: 12, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 14, flex: 1, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ width: 16, height: 16, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden', flexShrink: 0 }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
            <div className="flex items-center gap-2">
              <div style={{ width: 12, height: 12, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 14, flex: 1, width: '85%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ width: 16, height: 16, borderRadius: 2, background: 'var(--white-06)', position: 'relative', overflow: 'hidden', flexShrink: 0 }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
          </div>
          {/* Frame Shift Recommendations skeleton */}
          {analysis?.frameShiftRecommendations && analysis.frameShiftRecommendations.length > 0 && (
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
          )}
        </div>
      </div>
    );
  }

  // If no analysis and not loading, show empty (shouldn't happen if chat has messages, but safety check)
  if (!analysis) {
    return (
      <div className="mt-3 p-3">
        {/* Header with refresh button */}
        {chatMessageCount >= 2 && (
          <div className="flex items-center justify-between mb-3">
            <div />
            <button
              type="button"
              onClick={handleRefresh}
              disabled={!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading}
              className="p-1.5 rounded transition-colors"
              style={{
                background: 'transparent',
                color: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) 
                  ? 'var(--text-tertiary-weak)' 
                  : 'var(--text-secondary-strong)',
                cursor: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) 
                  ? 'not-allowed' 
                  : 'pointer',
                opacity: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) ? 0.6 : 1
              }}
              title={t('unfold.discussionFlow.refresh', { defaultValue: 'Refresh Analysis' })}
              onMouseEnter={(e) => {
                if (!(!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading)) {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                }
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }}
            >
              <svg 
                className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} 
                viewBox="0 0 24 24" 
                fill="none" 
                stroke="currentColor"
              >
                <path d="M20 7h-5M20 7v5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M20 12a8 8 0 1 1-2.343-5.657" strokeWidth="2" strokeLinecap="round" />
              </svg>
            </button>
          </div>
        )}
        <div className="text-12 text-center" style={{ ...textSecondaryStrongStyle, whiteSpace: 'pre-line' }}>
          {emptyMessage}
        </div>
      </div>
    );
  }

  return (
    <div className="mt-3 p-3">
      {/* Header with refresh button */}
      <div className="flex items-center justify-between mb-3">
        <div />
        <button
          type="button"
          onClick={handleRefresh}
          disabled={!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading}
          className="p-1.5 rounded transition-colors"
          style={{
            background: 'transparent',
            color: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) 
              ? 'var(--text-tertiary-weak)' 
              : 'var(--text-secondary-strong)',
            cursor: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) 
              ? 'not-allowed' 
              : 'pointer',
            opacity: (!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading) ? 0.6 : 1
          }}
          title={t('unfold.discussionFlow.refresh', { defaultValue: 'Refresh Analysis' })}
          onMouseEnter={(e) => {
            if (!(!projectId || projectId === 'local-unfold' || chatMessageCount < 2 || isLoading)) {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
            }
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
          }}
        >
          <svg 
            className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} 
            viewBox="0 0 24 24" 
            fill="none" 
            stroke="currentColor"
          >
            <path d="M20 7h-5M20 7v5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M20 12a8 8 0 1 1-2.343-5.657" strokeWidth="2" strokeLinecap="round" />
          </svg>
        </button>
      </div>

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
            {analysis.blindSpots.map((spot, idx) => {
              const taskId = `blind-spot-${idx}`;
              const isChecked = textTasks?.some((t) => t.id === taskId) || false;
              const isGenerating = generatingPrompts[taskId] || false;
              
              return (
                <li key={idx} className="flex items-center gap-2">
                  {isGenerating ? (
                    <svg 
                      className="w-3 h-3 animate-spin shrink-0" 
                      viewBox="0 0 24 24" 
                      fill="none" 
                      stroke="currentColor"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      <circle cx="12" cy="12" r="9" strokeWidth="2" opacity="0.25" />
                      <path d="M21 12a9 9 0 0 0-9-9" strokeWidth="2" strokeLinecap="round" />
                    </svg>
                  ) : (
                    <span className="text-12 shrink-0" style={dangerColorStyle}>⚠</span>
                  )}
                  <span className="text-12 flex-1" style={textHeadingStyle}>
                    {spot}
                  </span>
                  <input
                    type="checkbox"
                    checked={isChecked}
                    onChange={() => handleBlindSpotToggle(spot, idx)}
                    disabled={isGenerating}
                    className="shrink-0"
                    style={{
                      width: '16px',
                      height: '16px',
                      cursor: isGenerating ? 'not-allowed' : 'pointer',
                      opacity: isGenerating ? 0.6 : 1
                    }}
                  />
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {/* Frame Shift Recommendations */}
      {analysis.frameShiftRecommendations && analysis.frameShiftRecommendations.length > 0 && (
        <div className="mb-4">
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {sectionLabels.frameShiftRecommendations}
          </div>
          <ul className="space-y-1.5" style={listStyle}>
            {analysis.frameShiftRecommendations.map((rec, idx) => (
              <li key={idx} className="flex items-center gap-2">
                <span className="text-12" style={recommendationColorStyle}>→</span>
                <span className="text-12 flex-1" style={textHeadingStyle}>
                  {rec}
                </span>
                <button
                  type="button"
                  onClick={() => handleApplyRecommendation(rec, idx)}
                  disabled={applyingIndex !== null}
                  className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0 flex items-center gap-1"
                  style={{
                    background: applyingIndex === idx ? 'var(--white-06)' : 'var(--accent-primary)',
                    color: applyingIndex === idx ? 'var(--text-tertiary-weak)' : 'var(--text-on-accent)',
                    border: '1px solid var(--overlay-line-90)',
                    cursor: applyingIndex !== null ? 'not-allowed' : 'pointer',
                    opacity: applyingIndex !== null && applyingIndex !== idx ? 0.6 : 1
                  }}
                  title={t('unfold.discussionFlow.apply', { defaultValue: 'Apply to chat' })}
                  onMouseEnter={(e) => {
                    if (applyingIndex === null) {
                      (e.currentTarget as HTMLButtonElement).style.opacity = '0.85';
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (applyingIndex === null) {
                      (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                    }
                  }}
                >
                  {applyingIndex === idx ? (
                    <>
                      <svg 
                        className="w-3 h-3 animate-spin" 
                        viewBox="0 0 24 24" 
                        fill="none" 
                        stroke="currentColor"
                      >
                        <circle cx="12" cy="12" r="9" strokeWidth="2" opacity="0.25" />
                        <path d="M21 12a9 9 0 0 0-9-9" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                      <span>{t('unfold.discussionFlow.applying', { defaultValue: 'Applying...' })}</span>
                    </>
                  ) : (
                    <span>{t('unfold.discussionFlow.applyButton', { defaultValue: 'Apply' })}</span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};
