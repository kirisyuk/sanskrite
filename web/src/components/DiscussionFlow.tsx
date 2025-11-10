import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { DiscussionFlowAnalysis, BlindSpot, ThinkingType, isGeneratingDiscussionFlowAnalysis, generateDiscussionFlowAnalysis } from '../lib/discussionFlowAnalysis';
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
  
  // PERF: Track mount time
  React.useEffect(() => {
    const mountTime = performance.now();
    console.log('[DiscussionFlow] Component mounted at', mountTime);
    return () => {
      const unmountTime = performance.now();
      console.log('[DiscussionFlow] Component unmounted at', unmountTime, 'lifetime:', unmountTime - mountTime, 'ms');
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
  // Track expanded state for blind spots
  const [blindSpotsExpanded, setBlindSpotsExpanded] = React.useState(false);
  const [showRawJSON, setShowRawJSON] = React.useState(false);
  // Track expanded state for thinking types
  const [expandedThinkingType, setExpandedThinkingType] = React.useState<number | null>(null);
  
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
    
    // PERF: Use early return if no interactions
    if (interactions.length === 0) {
      setAnalysisInteraction(null);
      setChatMessageCount(0);
      return;
    }
    
    // PERF: Cache projectId to avoid repeated access
    const currentProjectId = state.projectId;
    
    // Find analysis interaction - use simple loop instead of find for better performance
    let found = null;
    for (let i = interactions.length - 1; i >= 0; i--) {
      const it = interactions[i];
      if (it.subkind === 'discussion.flow.analysis' && it.projectId === currentProjectId) {
        found = it;
        break; // Found it, stop searching
      }
    }
    
    // Count chat messages - optimized: single pass with counter
    let count = 0;
    for (let i = 0; i < interactions.length; i++) {
      const kind = interactions[i].kind;
      if (kind === 'user' || kind === 'assistant') {
        count++;
      }
    }
    
    // Only update state if still mounted
    if (mountedRef.current) {
      setAnalysisInteraction(found || null);
      setChatMessageCount(count);
    }
  }, [projectId, interactionsLength]);

  const analysis: DiscussionFlowAnalysis | null = React.useMemo(() => {
    if (!analysisInteraction) return null;
    
    // Check if this is a failed parse (has rawResponse but no content)
    const hasRawResponse = !!(analysisInteraction as any).rawResponse;
    const hasContent = !!(analysisInteraction.content || analysisInteraction.contentSanitized);
    const hasParseError = !!(analysisInteraction as any).parseError;
    
    // If we have raw response but no valid content, return null but keep rawResponse available
    if (hasRawResponse && !hasContent && hasParseError) {
      return null;
    }
    
    if (!hasContent) return null;
    
      try {
      const data = JSON.parse(analysisInteraction.content || analysisInteraction.contentSanitized || '{}');
      if (data.blindSpots && Array.isArray(data.blindSpots) && 
          data.frameShiftRecommendations && Array.isArray(data.frameShiftRecommendations) &&
          data.frame && typeof data.frame === 'string' &&
          data.confidence && ['low', 'medium', 'high'].includes(data.confidence)) {
        // Normalize blindSpots to new format if needed
        const normalizedBlindSpots = data.blindSpots
          .map((b: any) => {
            if (typeof b === 'string') {
              return { text: b, practical_relevance: 0.5 };
            }
            if (typeof b === 'object' && b !== null) {
              const text = b.text || b.textValue || b.value || b.description || b.content || String(b);
              const practical_relevance = typeof b.practical_relevance === 'number' 
                ? Math.max(0, Math.min(1, b.practical_relevance))
                : 0.5;
              return {
                text: typeof text === 'string' ? text.trim() : String(text).trim(),
                practical_relevance
              };
            }
            return { text: String(b || ''), practical_relevance: 0.5 };
          })
          .filter((b: any) => b && typeof b === 'object' && b.text && typeof b.text === 'string' && b.text.trim().length > 0);
        
        // Normalize thinkingTypes if present
        const normalizedThinkingTypes = data.thinkingTypes && Array.isArray(data.thinkingTypes)
          ? data.thinkingTypes
              .filter((tt: any) => 
                tt && 
                typeof tt === 'object' && 
                typeof tt.type === 'string' && 
                typeof tt.score === 'number' && 
                !isNaN(tt.score) &&
                ['exploratory', 'decision-making', 'explanatory', 'validating', 'synthesizing', 'planning', 'argumentative', 'modeling', 'communication', 'tool-making'].includes(tt.type)
              )
              .slice(0, 2)
              .map((tt: any) => ({
                type: tt.type,
                score: Math.max(0, Math.min(1, tt.score)),
                manifestation: tt.manifestation && typeof tt.manifestation === 'string' 
                  ? tt.manifestation.trim().slice(0, 300) 
                  : undefined
              }))
              .sort((a: any, b: any) => b.score - a.score)
          : undefined;
        
        return {
          ...data,
          blindSpots: normalizedBlindSpots,
          thinkingTypes: normalizedThinkingTypes
        } as DiscussionFlowAnalysis;
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
  const getConfidenceBadgeStyle = React.useCallback((confidence: string) => {
    switch (confidence) {
      case 'low':
        return {
          backgroundColor: 'rgba(156, 163, 175, 0.1)', // gray
          borderColor: 'rgba(156, 163, 175, 0.3)',
          color: 'rgb(156, 163, 175)',
        };
      case 'medium':
        return {
          backgroundColor: 'rgba(251, 191, 36, 0.1)', // yellow/orange
          borderColor: 'rgba(251, 191, 36, 0.3)',
          color: 'rgb(251, 191, 36)',
        };
      case 'high':
        return {
          backgroundColor: 'rgba(34, 197, 94, 0.1)', // green
          borderColor: 'rgba(34, 197, 94, 0.3)',
          color: 'rgb(34, 197, 94)',
        };
      default:
        return {
          backgroundColor: 'rgba(156, 163, 175, 0.1)',
          borderColor: 'rgba(156, 163, 175, 0.3)',
          color: 'rgb(156, 163, 175)',
        };
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
      
      // Delete existing analysis if it exists (including failed parse ones)
      const analysisSubkind = 'discussion.flow.analysis';
      const existingAnalyses = interactions.filter((it) => 
        it.subkind === analysisSubkind && it.projectId === projectId
      );
      
      for (const existing of existingAnalyses) {
        if (existing.id) {
          try {
            await api.deleteProjectInteraction(projectId, existing.id);
          } catch (e) {
            console.warn('[DiscussionFlow] Failed to delete old analysis', e);
          }
        }
      }
      
      // Remove from local state
      useGenaiChatStore.setState((s) => ({
        interactions: s.interactions.filter((it) => 
          !(it.subkind === analysisSubkind && it.projectId === projectId)
        )
      }));

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

      // Generate full analysis (pass null explicitly to force regeneration)
      // generateDiscussionFlowAnalysis will handle context window limitation (70%) and filtering
      await generateDiscussionFlowAnalysis(
        projectId,
        finalMessages,
        null, // explicitly null to force regeneration
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
  const handleBlindSpotToggle = React.useCallback(async (blindSpot: BlindSpot, index: number) => {
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
        title: blindSpot.text,
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

The blind spot is: "${blindSpot.text}"

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
        title: blindSpot.text,
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

  // Check if we have raw response even without valid analysis (parse error case)
  const hasRawResponse = analysisInteraction && !!(analysisInteraction as any).rawResponse;
  const hasParseError = analysisInteraction && !!(analysisInteraction as any).parseError;

  // Show empty message only if chat is empty (less than 2 messages) AND no raw response to show
  if (!analysis && !isLoading && chatMessageCount < 2 && !hasRawResponse) {
    return (
      <div>
        <div className="text-12 text-center" style={{ ...textSecondaryStrongStyle, whiteSpace: 'pre-line' }}>
          {emptyMessage}
        </div>
      </div>
    );
  }
  
  // If we have raw response but no analysis, show debug section
  if (!analysis && !isLoading && hasRawResponse) {
    return (
      <div>
        {/* Header with refresh button */}
        <div className="flex items-center justify-between mb-2">
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
        
        {/* Raw JSON Debug Section - Show when parse failed */}
        {hasRawResponse && (
          <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
            <div className="flex items-center justify-between mb-2">
              <div className="text-[11px] uppercase tracking-wide" style={textTertiaryWeakStyle}>
                {hasParseError ? '⚠️ Raw API Response (Parse Failed)' : 'Raw API Response (Debug)'}
              </div>
              <button
                type="button"
                onClick={() => setShowRawJSON(!showRawJSON)}
                className="text-[11px] px-2 py-1 rounded transition-colors"
                style={{
                  background: 'var(--white-10)',
                  color: 'var(--text-secondary-strong)',
                  border: '1px solid var(--border-input)',
                  cursor: 'pointer',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                }}
              >
                {showRawJSON ? 'Hide' : 'Show'}
              </button>
            </div>
            {showRawJSON && (
              <div className="mt-2">
                <pre
                  className="text-[10px] overflow-auto p-2 rounded"
                  style={{
                    backgroundColor: 'var(--bg-input)',
                    color: 'var(--text-secondary-strong)',
                    border: '1px solid var(--border-input)',
                    maxHeight: '400px',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                  }}
                >
                  {JSON.stringify((analysisInteraction as any).rawResponse, null, 2)}
                </pre>
              </div>
            )}
          </div>
        )}
      </div>
    );
  }

  // Show skeleton if loading
  if (isLoading) {
    return (
      <div>
        {/* Header with refresh button */}
        <div className="flex items-center justify-between mb-2">
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
      <div>
        {/* Header with refresh button */}
        {chatMessageCount >= 2 && (
          <div className="flex items-center justify-between mb-2">
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
    <div>
      {/* Header with refresh button */}
      <div className="flex items-center justify-between mb-2">
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
        <>
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {sectionLabels.frame}
          </div>
          <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
            <div className="text-12" style={textHeadingStyle}>
              {analysis.frame}
            </div>
          </div>
        </>
      )}

      {/* Thinking Types */}
      {analysis.thinkingTypes && analysis.thinkingTypes.length > 0 && (
        <>
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {t('unfold.discussionFlow.thinkingTypes', { defaultValue: 'Thinking Types' })}
          </div>
          <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
            <div className="space-y-2">
              {analysis.thinkingTypes.map((thinkingType, idx) => {
                const scorePercent = Math.round(thinkingType.score * 100);
                const typeLabels: Record<string, string> = {
                  'exploratory': t('unfold.discussionFlow.thinkingTypes.exploratory', { defaultValue: 'Exploratory' }),
                  'decision-making': t('unfold.discussionFlow.thinkingTypes.decisionMaking', { defaultValue: 'Decision-Making' }),
                  'explanatory': t('unfold.discussionFlow.thinkingTypes.explanatory', { defaultValue: 'Explanatory' }),
                  'validating': t('unfold.discussionFlow.thinkingTypes.validating', { defaultValue: 'Validating' }),
                  'synthesizing': t('unfold.discussionFlow.thinkingTypes.synthesizing', { defaultValue: 'Synthesizing' }),
                  'planning': t('unfold.discussionFlow.thinkingTypes.planning', { defaultValue: 'Planning' }),
                  'argumentative': t('unfold.discussionFlow.thinkingTypes.argumentative', { defaultValue: 'Argumentative' }),
                  'modeling': t('unfold.discussionFlow.thinkingTypes.modeling', { defaultValue: 'Modeling' }),
                  'communication': t('unfold.discussionFlow.thinkingTypes.communication', { defaultValue: 'Communication' }),
                  'tool-making': t('unfold.discussionFlow.thinkingTypes.toolMaking', { defaultValue: 'Tool-Making' }),
                };
                const typeLabel = typeLabels[thinkingType.type] || thinkingType.type;
                const isExpanded = expandedThinkingType === idx;
                const hasManifestation = thinkingType.manifestation && thinkingType.manifestation.trim().length > 0;
                
                return (
                  <div key={idx}>
                    <div 
                      className={`flex items-center justify-between ${hasManifestation ? 'cursor-pointer hover:opacity-80' : ''}`}
                      onClick={() => hasManifestation && setExpandedThinkingType(isExpanded ? null : idx)}
                      style={hasManifestation ? { transition: 'opacity 0.2s' } : {}}
                    >
                      <span className="text-12" style={textHeadingStyle}>
                        {typeLabel}
                      </span>
                      <span className="text-[11px] shrink-0 ml-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                        {scorePercent}%
                      </span>
                    </div>
                    {isExpanded && hasManifestation && (
                      <div className="mt-2 pt-2 border-t" style={{ borderColor: 'var(--border-subtle)' }}>
                        <div className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>
                          {thinkingType.manifestation}
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </>
      )}

      {/* Confidence */}
      <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
        <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
          {sectionLabels.confidence}
        </div>
        <div className="inline-flex items-center">
          {(() => {
            const badgeStyle = getConfidenceBadgeStyle(analysis.confidence);
            return (
              <span
                className="inline-flex items-center px-2 py-1 rounded text-[11px] font-medium"
                style={{
                  ...badgeStyle,
                  border: `1px solid ${badgeStyle.borderColor}`,
                }}
              >
                {getConfidenceLabel(analysis.confidence)}
              </span>
            );
          })()}
        </div>
      </div>

      {/* Blind Spots */}
      {analysis.blindSpots && analysis.blindSpots.length > 0 && (() => {
        // Ensure all blindSpots are properly normalized
        const safeBlindSpots = analysis.blindSpots
          .filter((spot: any) => spot && typeof spot === 'object' && spot.text && typeof spot.text === 'string')
          .map((spot: any) => ({
            text: String(spot.text || '').trim(),
            practical_relevance: typeof spot.practical_relevance === 'number' 
              ? Math.max(0, Math.min(1, spot.practical_relevance))
              : 0.5
          }))
          .filter((spot: BlindSpot) => spot.text.length > 0);
        
        if (safeBlindSpots.length === 0) return null;
        
        // Sort by practical_relevance descending, preserving original index
        const sortedBlindSpots = safeBlindSpots
          .map((spot, idx) => ({ spot, originalIdx: idx }))
          .sort((a, b) => b.spot.practical_relevance - a.spot.practical_relevance);
        const visibleBlindSpots = blindSpotsExpanded ? sortedBlindSpots : sortedBlindSpots.slice(0, 3);
        const hasMore = sortedBlindSpots.length > 3;
        
        return (
          <>
            <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
              {sectionLabels.blindSpots}
            </div>
            <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
              <ul className="space-y-1.5" style={listStyle}>
              {visibleBlindSpots
                .map(({ spot, originalIdx }) => {
                  // Additional safety check
                  if (!spot || typeof spot !== 'object' || !spot.text || typeof spot.text !== 'string') {
                    console.warn('[DiscussionFlow] Invalid blind spot format', spot);
                    return null;
                  }
                  
                  const taskId = `blind-spot-${originalIdx}`;
                  const isChecked = textTasks?.some((t) => t.id === taskId) || false;
                  const isGenerating = generatingPrompts[taskId] || false;
                  const relevancePercent = Math.round((spot.practical_relevance || 0) * 100);
                  
                  return (
                    <li key={originalIdx} className="flex items-center gap-2">
                      {isGenerating && (
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
                      )}
                      <span className="text-12 flex-1" style={textHeadingStyle}>
                        {spot.text}
                      </span>
                      <span className="text-[11px] shrink-0" style={{ color: 'var(--text-tertiary-weak)' }}>
                        {relevancePercent}%
                      </span>
                      <label 
                        className="shrink-0 relative inline-block"
                        style={{ cursor: isGenerating ? 'not-allowed' : 'pointer' }}
                      >
                        <input
                          type="checkbox"
                          checked={isChecked}
                          onChange={() => handleBlindSpotToggle(spot, originalIdx)}
                          disabled={isGenerating}
                          style={{
                            width: '16px',
                            height: '16px',
                            cursor: isGenerating ? 'not-allowed' : 'pointer',
                            opacity: isGenerating ? 0.6 : 1,
                            backgroundColor: isChecked ? '#E9B27E' : 'var(--bg-input)',
                            border: `1px solid ${isChecked ? '#E9B27E' : 'var(--border-input)'}`,
                            borderRadius: '3px',
                            appearance: 'none',
                            WebkitAppearance: 'none',
                            MozAppearance: 'none',
                            position: 'relative',
                            margin: 0,
                          }}
                          onMouseEnter={(e) => {
                            if (!isGenerating && !isChecked) {
                              (e.currentTarget as HTMLInputElement).style.borderColor = 'var(--text-secondary-strong)';
                            }
                          }}
                          onMouseLeave={(e) => {
                            if (!isChecked) {
                              (e.currentTarget as HTMLInputElement).style.borderColor = 'var(--border-input)';
                            }
                          }}
                        />
                        {isChecked && (
                          <svg
                            width="10"
                            height="10"
                            viewBox="0 0 10 10"
                            fill="none"
                            style={{
                              position: 'absolute',
                              left: '3px',
                              top: '3px',
                              pointerEvents: 'none',
                            }}
                          >
                            <path
                              d="M1.5 5L4 7.5L8.5 2.5"
                              stroke="white"
                              strokeWidth="1.5"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                            />
                          </svg>
                        )}
                      </label>
                    </li>
                  );
                })
                .filter((item): item is React.ReactElement => item !== null)}
            </ul>
            {hasMore && (
              <button
                type="button"
                onClick={() => setBlindSpotsExpanded(!blindSpotsExpanded)}
                className="mt-2 text-[11px] text-left transition-colors"
                style={{
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                  background: 'transparent',
                  border: 'none',
                  padding: 0
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.color = 'var(--accent-primary)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.color = 'var(--text-secondary-strong)';
                }}
              >
                {blindSpotsExpanded 
                  ? t('unfold.discussionFlow.showLess', { defaultValue: 'Show less' })
                  : t('unfold.discussionFlow.showMore', { defaultValue: `Show more (${sortedBlindSpots.length - 3})` })
                }
              </button>
            )}
          </div>
          </>
        );
      })()}

      {/* Frame Shift Recommendations */}
      {analysis.frameShiftRecommendations && analysis.frameShiftRecommendations.length > 0 && (
        <>
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={textTertiaryWeakStyle}>
            {sectionLabels.frameShiftRecommendations}
          </div>
          <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
            <ul className="space-y-1.5" style={listStyle}>
            {analysis.frameShiftRecommendations.map((rec, idx) => (
              <li key={idx} className="flex items-center gap-2">
                <span className="text-12 flex-1" style={textHeadingStyle}>
                  {rec}
                </span>
                <button
                  type="button"
                  onClick={() => handleApplyRecommendation(rec, idx)}
                  disabled={applyingIndex !== null}
                  className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0 flex items-center gap-1"
                  style={{
                    background: applyingIndex === idx ? 'var(--white-06)' : 'var(--white-10)',
                    color: applyingIndex === idx ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
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
        </>
      )}

      {/* Raw JSON Debug Section - Show even if parsing failed */}
      {hasRawResponse && (
        <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
          <div className="flex items-center justify-between mb-2">
            <div className="text-[11px] uppercase tracking-wide" style={textTertiaryWeakStyle}>
              {hasParseError ? '⚠️ Raw API Response (Parse Failed)' : 'Raw API Response (Debug)'}
            </div>
            <button
              type="button"
              onClick={() => setShowRawJSON(!showRawJSON)}
              className="text-[11px] px-2 py-1 rounded transition-colors"
              style={{
                background: 'var(--white-10)',
                color: 'var(--text-secondary-strong)',
                border: '1px solid var(--border-input)',
                cursor: 'pointer',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }}
            >
              {showRawJSON ? 'Hide' : 'Show'}
            </button>
          </div>
          {showRawJSON && (
            <div className="mt-2">
              <pre
                className="text-[10px] overflow-auto p-2 rounded"
                style={{
                  backgroundColor: 'var(--bg-input)',
                  color: 'var(--text-secondary-strong)',
                  border: '1px solid var(--border-input)',
                  maxHeight: '400px',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                }}
              >
                {JSON.stringify((analysisInteraction as any).rawResponse, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
