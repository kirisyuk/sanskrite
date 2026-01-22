import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownRenderer } from '../lib/markdownComponents';
import type { LocalInteraction } from '../stores/genaiChatStore';
import { ActionRow } from './ActionRow';
import ChatMessage from './ChatMessage';
import { MessageActions } from './MessageActions';

interface ResearchInteractionProps {
  interaction: LocalInteraction;
  collapsedSections?: Record<string, boolean>;
}

export const ResearchInteraction: React.FC<ResearchInteractionProps> = ({
  interaction,
  collapsedSections,
}) => {
  // Respect collapsed sections
  try {
    const sid = (interaction as any).sectionId as string | undefined;
    if (sid && collapsedSections?.[sid]) return null;
  } catch {}

  if (interaction.subkind === 'research.progress') {
    return <ResearchProgressView interaction={interaction} />;
  }
  
  if (interaction.subkind === 'research.result') {
    return (
      <div className="group relative">
        <MessageActions
          interactionId={interaction.id}
          isUser={false}
          interaction={interaction}
        />
        <ChatMessage
          role="assistant"
          align="left"
          isThinking={false}
          thinkingSeconds={0}
          isError={false}
          headerText={undefined}
          leftAccent={false}
          footer={undefined}
        >
          <ResearchResultView interaction={interaction} />
        </ChatMessage>
      </div>
    );
  }

  return null;
};

const ResearchProgressView: React.FC<{ interaction: LocalInteraction }> = ({ interaction }) => {
  const { t } = useTranslation();
  const progress = interaction.researchProgress;
  const query = interaction.researchQuery || '';
  const stageStartTimeRef = React.useRef<Record<string, number>>({});
  const [elapsedTime, setElapsedTime] = React.useState(0);

  // Type assertion to ensure TypeScript knows the full type after null check
  // Use ResearchProgressInteraction type to get the correct stage union type
  const typedProgress: {
    stage: 'planning' | 'searching' | 'generating' | 'balancing' | 'synthesizing';
    status: 'in_progress' | 'completed';
    subQueries?: string[];
    sourcesCount?: number;
  } | null = progress || null;

  // Track stage start time for timer (using ref to persist across renders)
  React.useEffect(() => {
    if (!typedProgress) return;
    const currentStage = typedProgress.stage;
    if (currentStage && !stageStartTimeRef.current[currentStage]) {
      stageStartTimeRef.current[currentStage] = Date.now();
    }
  }, [typedProgress?.stage]);

  // Update elapsed time every second
  React.useEffect(() => {
    if (!typedProgress) {
      setElapsedTime(0);
      return;
    }
    if (typedProgress.status === 'in_progress' && typedProgress.stage) {
      const startTime = stageStartTimeRef.current[typedProgress.stage] || Date.now();
      // Set initial time immediately
      setElapsedTime(Math.floor((Date.now() - startTime) / 1000));
      
      const interval = setInterval(() => {
        setElapsedTime(Math.floor((Date.now() - startTime) / 1000));
      }, 1000);
      return () => clearInterval(interval);
    } else {
      setElapsedTime(0);
    }
  }, [typedProgress?.stage, typedProgress?.status]);

  if (!progress || !typedProgress) return null;

  const formatTime = (seconds: number) => {
    if (seconds < 60) return `${seconds}s`;
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}m ${secs}s`;
  };

  // Determine completed stages (for badges)
  const completedStages: string[] = [];
  if (typedProgress.stage === 'searching' || typedProgress.stage === 'generating' || typedProgress.stage === 'balancing' || typedProgress.stage === 'synthesizing') {
    completedStages.push('planning');
  }
  if (typedProgress.stage === 'generating' || typedProgress.stage === 'balancing' || typedProgress.stage === 'synthesizing') {
    completedStages.push('searching');
  }
  if (typedProgress.stage === 'balancing' || typedProgress.stage === 'synthesizing') {
    completedStages.push('generating');
  }
  if (typedProgress.stage === 'synthesizing') {
    completedStages.push('balancing');
  }

  const getStageInfo = (stage: string) => {
    switch (stage) {
      case 'planning':
        return {
          label: 'Planning sub-queries',
          icon: (
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          ),
        };
      case 'searching':
        return {
          label: 'Searching sources',
          icon: (
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="11" cy="11" r="8" />
              <path d="M21 21l-4.35-4.35" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          ),
        };
      case 'generating':
        return {
          label: 'Generating report',
          icon: (
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
              <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          ),
        };
      case 'balancing':
        return {
          label: 'Balancing position',
          icon: (
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M12 2v20M2 12h20" strokeLinecap="round" strokeLinejoin="round" />
              <circle cx="12" cy="12" r="3" />
            </svg>
          ),
        };
      case 'synthesizing':
        return {
          label: 'Synthesizing result',
          icon: (
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M12 2L2 7l10 5 10-5-10-5z" />
              <path d="M2 17l10 5 10-5M2 12l10 5 10-5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          ),
        };
      default:
        return { label: stage, icon: null };
    }
  };

  const currentStageInfo = getStageInfo(typedProgress.stage);
  const isActive = typedProgress.status === 'in_progress';

  return (
    <div
      className="my-2 p-4 rounded"
      style={{
        background: 'var(--overlay-line-10, rgba(255, 255, 255, 0.05))',
        border: '1px solid var(--overlay-line-20, rgba(255, 255, 255, 0.1))',
      }}
    >
      {/* Header with completed badges */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2" style={{ color: 'var(--text-heading, #ffffff)' }}>
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span className="text-sm font-medium">Research</span>
        </div>
        
        {/* Completed stages badges */}
        {completedStages.length > 0 && (
          <div className="flex items-center gap-1.5">
            {completedStages.map((stage) => {
              const info = getStageInfo(stage);
              return (
                <div
                  key={stage}
                  className="flex items-center gap-1 px-2 py-0.5 rounded text-[10px]"
                  style={{
                    background: 'var(--white-06, rgba(255, 255, 255, 0.06))',
                    color: 'var(--text-secondary-strong, rgba(255, 255, 255, 0.8))',
                  }}
                >
                  <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                    <path d="M5 13l4 4L19 7" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span>{info.label}</span>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Query */}
      {query && (
        <div className="mb-4 text-xs" style={{ color: 'var(--text-secondary, rgba(255, 255, 255, 0.7))' }}>
          {query}
        </div>
      )}

      {/* Current stage with fade transition - always visible when active */}
      <div
        key={typedProgress.stage}
        className="transition-all duration-300 ease-in-out"
        style={{ 
          opacity: 1,
          transform: 'translateY(0)',
        }}
      >
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2" style={{ color: 'var(--text-heading, #ffffff)' }}>
            {isActive ? (
              <div className="w-4 h-4 animate-spin" style={{ color: 'var(--text-secondary)' }}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <circle cx="12" cy="12" r="10" strokeDasharray="60" strokeDashoffset="15" opacity="0.3" />
                  <circle cx="12" cy="12" r="10" strokeDasharray="60" strokeDashoffset="45" strokeLinecap="round" />
                </svg>
              </div>
            ) : (
              <div className="w-4 h-4" style={{ color: 'var(--text-secondary)' }}>
                {currentStageInfo.icon}
              </div>
            )}
            <span className="text-xs font-medium">{currentStageInfo.label}</span>
          </div>
          
          {/* Timer */}
          {isActive && elapsedTime > 0 && (
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak, rgba(255, 255, 255, 0.4))' }}>
              {formatTime(elapsedTime)}
            </span>
          )}
        </div>

        {/* Stage details (only for active stage) */}
        {isActive && (
          <div className="mt-3 ml-6 text-xs" style={{ color: 'var(--text-secondary, rgba(255, 255, 255, 0.7))' }}>
            {typedProgress.stage === 'planning' && typedProgress.subQueries && typedProgress.subQueries.length > 0 && (
              <div>
                <div className="mb-1.5 font-medium">Generated {typedProgress.subQueries.length} sub-queries:</div>
                <ul className="list-disc list-inside space-y-1 ml-1">
                  {typedProgress.subQueries.map((q, idx) => (
                    <li key={idx}>{q}</li>
                  ))}
                </ul>
              </div>
            )}
            
            {typedProgress.stage === 'searching' && typedProgress.sourcesCount !== undefined && (
              <div>Found {typedProgress.sourcesCount} sources</div>
            )}
            
                {typedProgress.stage === 'generating' && (
                  <div>Generating report from sources...</div>
                )}
                
                {typedProgress.stage === 'balancing' && (
                  <div>Generating counter-position analysis...</div>
                )}
                
                {typedProgress.stage === 'synthesizing' && (
                  <div>Synthesizing reports into final result...</div>
                )}
          </div>
        )}
      </div>
    </div>
  );
};

const ResearchResultView: React.FC<{ interaction: LocalInteraction }> = ({ interaction }) => {
  const { t } = useTranslation();
  const [sourcesExpanded, setSourcesExpanded] = useState(false);
  
  const query = interaction.researchQuery || '';
  const subQueries = Array.isArray(interaction.subQueries) 
    ? interaction.subQueries.filter((q): q is string => typeof q === 'string')
    : [];
  
  // Normalize researchSources - ensure they're objects with title/url
  const sources = React.useMemo(() => {
    const rawSources = interaction.researchSources || [];
    return rawSources.map((src: any) => {
      // If it's already an object with title/url, use it
      if (src && typeof src === 'object' && 'title' in src && 'url' in src) {
        return {
          title: String(src.title || ''),
          url: String(src.url || ''),
          score: typeof src.score === 'number' ? src.score : undefined,
        };
      }
      // If it's a string (URL), convert to object
      if (typeof src === 'string') {
        return {
          title: src,
          url: src,
          score: undefined,
        };
      }
      // Fallback
      return {
        title: 'Unknown source',
        url: '',
        score: undefined,
      };
    });
  }, [interaction.researchSources]);
  
  const usage = interaction.usage;
  const model = interaction.model;

  // Sort sources by score (if available)
  const sortedSources = [...sources].sort((a, b) => (b.score || 0) - (a.score || 0));
  const visibleSources = sourcesExpanded ? sortedSources : sortedSources.slice(0, 5);

  const content = interaction.content || '';
  const contentText = typeof content === 'string' ? content : '';

  return (
    <div
      className="p-4 rounded"
      style={{
        background: 'var(--overlay-line-10, rgba(255, 255, 255, 0.05))',
        border: '1px solid var(--overlay-line-20, rgba(255, 255, 255, 0.1))',
      }}
    >
      <div className="flex items-center gap-2 mb-3" style={{ color: 'var(--text-heading, #ffffff)' }}>
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span className="text-sm font-medium">Research Report</span>
      </div>

      {/* Main report content */}
      {contentText && (
        <div className="mb-4">
          <MarkdownRenderer content={contentText} />
        </div>
      )}

      {/* Divider */}
      <div className="my-4 border-t" style={{ borderColor: 'var(--overlay-line-20, rgba(255, 255, 255, 0.1))' }} />

      {/* Sub-queries */}
      {subQueries.length > 0 && (
        <div className="mb-4">
          <div className="flex items-center gap-1.5 text-xs font-medium mb-2" style={{ color: 'var(--text-heading, #ffffff)' }}>
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span>Sub-queries explored ({subQueries.length})</span>
          </div>
          <ul className="list-disc list-inside space-y-1 ml-2">
            {subQueries.map((q, idx) => (
              <li key={idx} className="text-xs" style={{ color: 'var(--text-secondary, rgba(255, 255, 255, 0.7))' }}>
                {q}
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Sources */}
      {sources.length > 0 && (
        <div className="mb-4">
          <div className="flex items-center gap-1.5 text-xs font-medium mb-2" style={{ color: 'var(--text-heading, #ffffff)' }}>
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M4 19.5A2.5 2.5 0 016.5 17H20" />
              <path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span>Sources ({sources.length})</span>
          </div>
          <div className="space-y-1">
            {visibleSources.map((source, idx) => (
              <div key={idx} className="text-xs">
                <a
                  href={source.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline"
                  style={{ color: 'var(--accent-blue, #3b82f6)' }}
                >
                  {source.title || source.url}
                </a>
                {source.score !== undefined && (
                  <span className="ml-2 opacity-60">
                    ({Math.round(source.score * 100)}%)
                  </span>
                )}
              </div>
            ))}
          </div>
          {sources.length > 5 && (
            <button
              onClick={() => setSourcesExpanded(!sourcesExpanded)}
              className="mt-2 text-xs underline"
              style={{ color: 'var(--accent-blue, #3b82f6)' }}
            >
              {sourcesExpanded ? '▲ Show less' : `▼ Show all ${sources.length} sources`}
            </button>
          )}
        </div>
      )}

      {/* ActionRow for research result */}
      <ActionRow
        messageId={interaction.id}
        isAssistant={true}
        content={contentText}
      />
    </div>
  );
};


