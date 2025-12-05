import React from 'react';
import { useParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import ChatGraphMessage from '../components/ChatGraphMessage';
import ChatMessage from '../components/ChatMessage';
import { api } from '../lib/api';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from '../components/ThemeProvider';
import logoDarkTheme from '../assets/hatl-dark-theme.png';
import logoLightTheme from '../assets/hatl-light-theme.png';

export default function ShareViewPage() {
  const { token } = useParams<{ token: string }>();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [data, setData] = React.useState<any | null>(null);
  const { theme, setTheme } = React.useContext(ThemeContext);
  const { t } = useTranslation();

  React.useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        if (!token) { setError('Missing token'); setLoading(false); return; }
        const res = await api.shareGetPublic(token);
        if (mounted) { setData(res); setLoading(false); }
      } catch (e: any) {
        if (mounted) { setError('Unavailable or expired'); setLoading(false); }
      }
    })();
    return () => { mounted = false; };
  }, [token]);

  const toggleTheme = () => {
    setTheme(theme === 'dark' ? 'light' : 'dark');
  };

  // Build map of compliance warnings by assistant message ID (via replyTo or relations)
  // Must be called before early returns to follow React hooks rules
  const items = React.useMemo(() => {
    return Array.isArray(data?.interactions) ? data.interactions : [];
  }, [data]);

  const complianceWarnings = React.useMemo(() => {
    const map: Record<string, { confidence: number; explanation: string }> = {};
    items.forEach((it: any) => {
      if (it.kind === 'system' && it.subkind === 'eval.frame_compliance') {
        try {
          const content = it.contentSanitized || it.content || '{}';
          const parsed = JSON.parse(content);
          if (parsed.confidence >= 0.5 && parsed.explanation) {
            // Link via replyTo
            if (it.replyTo) {
              map[it.replyTo] = { confidence: parsed.confidence, explanation: parsed.explanation };
            }
            // Also check relations for targetId
            if (it.relations && Array.isArray(it.relations)) {
              it.relations.forEach((rel: any) => {
                if (rel.targetId && (rel.type === 'compliance_check' || rel.type === 'evaluates')) {
                  map[rel.targetId] = { confidence: parsed.confidence, explanation: parsed.explanation };
                }
              });
            }
          }
        } catch (e) {
          // Ignore parse errors
        }
      }
    });
    return map;
  }, [items]);

  // Extract Discussion Frame from note interaction with discussion.flow.analysis subkind
  const discussionFrame = React.useMemo(() => {
    const analysisInteraction = items.find(
      (it: any) => it.kind === 'note' && it.subkind === 'discussion.flow.analysis'
    );
    if (analysisInteraction?.content || analysisInteraction?.contentSanitized) {
      try {
        const content = analysisInteraction.contentSanitized || analysisInteraction.content || '{}';
        const parsed = JSON.parse(content);
        if (parsed.frame && typeof parsed.frame === 'string' && parsed.frame.trim()) {
          return parsed.frame.trim();
        }
      } catch (e) {
        // Ignore parse errors
      }
    }
    return null;
  }, [items]);

  // State for Discussion Frame collapse
  const [isFrameExpanded, setIsFrameExpanded] = React.useState(true);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={{ background: 'var(--bg-root)', color: 'var(--text-secondary-strong)' }}>
        Loading…
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center p-6" style={{ background: 'var(--bg-root)' }}>
        <div className="w-full max-w-lg text-center">
          <h1 className="text-2xl font-semibold mb-3" style={{ color: 'var(--text-heading)' }}>
            Link Unavailable or Expired
          </h1>
          <p className="text-base mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            The public link to this project is no longer valid or has expired.
          </p>
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
            Please contact the project owner to request an updated link.
          </p>
        </div>
      </div>
    );
  }

  const proj = data.project || {};

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-root)' }}>
      <div className="max-w-3xl mx-auto px-4 py-4">
        {/* Header with theme toggle */}
        <div className="mb-3 flex items-start justify-between">
          <div className="flex-1">
            <div className="text-[13px] font-semibold" style={{ color: 'var(--text-heading)' }}>
              {proj.displayName || 'Shared chat'}
            </div>
            {proj.description && (
              <div className="text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {proj.description}
              </div>
            )}
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Public read‑only · available until {new Date(data.share.expiresAt).toLocaleString()}
            </div>
          </div>
          <button
            onClick={toggleTheme}
            className="ml-3 rounded-lg p-2"
            style={{ 
              backgroundColor: 'var(--bg-elevated)', 
              border: '1px solid var(--overlay-line-90)',
              color: 'var(--text-secondary-strong)',
              cursor: 'pointer'
            }}
            title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
          >
            {theme === 'dark' ? (
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <circle cx="12" cy="12" r="5" strokeWidth="2"/>
                <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" strokeWidth="2" strokeLinecap="round"/>
              </svg>
            ) : (
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
              </svg>
            )}
          </button>
        </div>

        {/* Discussion Frame (collapsible) */}
        {discussionFrame && (
          <div className="mb-4 rounded-lg" style={{ 
            backgroundColor: 'var(--bg-elevated)', 
            border: '1px solid var(--overlay-line-90)',
            overflow: 'hidden'
          }}>
            <button
              onClick={() => setIsFrameExpanded(!isFrameExpanded)}
              className="w-full flex items-center justify-between p-3 text-left"
              style={{ 
                color: 'var(--text-heading)',
                cursor: 'pointer',
                backgroundColor: 'transparent',
                border: 'none'
              }}
            >
              <span className="text-[13px] font-semibold" style={{ color: '#669999' }}>
                Discussion Frame
              </span>
              <svg 
                className="w-4 h-4 transition-transform" 
                style={{ 
                  transform: isFrameExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                  color: 'var(--text-secondary-strong)'
                }}
                viewBox="0 0 24 24" 
                fill="none" 
                stroke="currentColor" 
                strokeWidth="2"
              >
                <path d="M6 9l6 6 6-6" strokeLinecap="round" strokeLinejoin="round"/>
              </svg>
            </button>
            {isFrameExpanded && (
              <div className="px-3 pb-3" style={{ color: 'var(--text-secondary-strong)' }}>
                <div className="text-[12px] leading-relaxed" style={{ 
                  fontStyle: 'italic',
                  color: 'var(--text-secondary-strong)'
                }}>
                  {discussionFrame}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Divider after Discussion Frame */}
        {discussionFrame && (
          <div className="mt-4 mb-4" style={{ borderTop: '1px solid var(--overlay-line-90)' }} />
        )}

        {/* Messages */}
        <div className="space-y-2">
          {items.map((it: any) => {
            const isUser = it.kind === 'user';
            const isSystem = it.kind === 'system';
            const content = (it.contentSanitized || it.content || '').toString();
            const gd = it.graphData;

            // Skip note interactions with discussion.flow.analysis (shown separately as Discussion Frame)
            if (it.kind === 'note' && it.subkind === 'discussion.flow.analysis') {
              return null;
            }

            // Skip all system interactions with eval.* subkind (except eval.frame_compliance which is shown as compliance warning)
            // These are metadata evaluations (bias, stance, etc.) and should not be displayed as messages
            if (isSystem && it.subkind && it.subkind.startsWith('eval.')) {
              if (it.subkind === 'eval.frame_compliance') {
                // This will be shown as compliance warning after assistant message
                return null;
              }
              // Skip all other eval.* system interactions (eval.bias, eval.stance, etc.)
              return null;
            }

            // Skip assistant interactions that are pure metadata (empty content or only JSON evaluations)
            // These might be evaluation metadata stored as assistant interactions
            if (!isUser && !isSystem && (!content || content.trim() === '' || content.trim().startsWith('{'))) {
              // Check if this looks like a JSON-only interaction (evaluation metadata)
              try {
                const parsed = JSON.parse(content);
                // If it's a valid JSON object with evaluation-like structure, skip it
                if (parsed && typeof parsed === 'object' && (parsed.biasLevel !== undefined || parsed.thoughtfulnessLevel !== undefined || parsed.expansions !== undefined)) {
                  return null;
                }
              } catch {
                // Not JSON, continue rendering
              }
            }

            // Special rendering for action.bring_in indicator messages
            if (isUser && it.subkind === 'action.bring_in') {
              return (
                <div key={it.id} className="my-1.5 flex justify-end">
                  <div className="px-3 py-1.5 rounded-lg text-[12px] inline-flex items-center gap-2" style={{ 
                    background: 'var(--accent-research-10)', 
                    color: 'var(--accent-research)',
                    border: '1px solid var(--accent-research-20)',
                    maxWidth: '80%'
                  }}>
                    <svg className="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6" strokeWidth="2" />
                      <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth="2" strokeLinecap="round" />
                      <path d="M18 7v4M16 9h4" strokeWidth="2" strokeLinecap="round" />
                    </svg>
                    <span>{content}</span>
                  </div>
                </div>
              );
            }

            // Determine display name
            let displayName = isUser ? 'you' : 'assistant';
            if (it.author?.displayName) {
              displayName = it.author.displayName;
            } else if (it.subkind === 'agent.bring_in' || it.subkind === 'agent.ask') {
              displayName = 'agent';
            }

            // Agent accent (if agent contribution)
            const isAgent = it.subkind?.startsWith('agent.');
            const isAssistant = !isUser && !isSystem;

            // Check for compliance warning for this assistant message
            const complianceWarning = isAssistant ? complianceWarnings[it.id] : null;

            return (
              <React.Fragment key={it.id}>
                <ChatMessage
                  role={isUser ? 'user' : 'assistant'}
                  align={isUser ? 'right' : 'left'}
                  headerText={displayName}
                  leftAccent={isAgent}
                >
                  {gd ? (
                    <ChatGraphMessage
                      chartTitle={gd.chartTitle}
                      chartType={gd.chartType || 'line'}
                      table={gd.table}
                      sources={gd.sources}
                    />
                  ) : (
                    <div className="markdown">
                      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
                        {content}
                      </ReactMarkdown>
                    </div>
                  )}
                </ChatMessage>
                {/* Show compliance warning after assistant message */}
                {isAssistant && complianceWarning && (
                  <div className="mt-2" style={{ width: '100%' }}>
                    <div className="p-2" style={{ backgroundColor: 'var(--warning-10)', borderLeft: '2px solid var(--accent-warning)', borderRadius: '4px' }}>
                      <div className="text-[11px]" style={{ color: 'var(--text-heading)', lineHeight: '1.4' }}>
                        {complianceWarning.explanation}
                      </div>
                    </div>
                  </div>
                )}
              </React.Fragment>
            );
          })}
        </div>

        {/* Footer with logo and tagline */}
        <div className="mt-8 pt-6" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
          <div className="flex flex-col items-center gap-3">
            {/* Dark theme logo */}
            <img
              src={logoDarkTheme}
              alt="hatl.one"
              className="h-4 w-auto"
              style={{
                display: theme === 'dark' ? 'block' : 'none',
                filter: 'drop-shadow(0 0 2px rgba(0,0,0,0.6))',
                opacity: 0.7,
              }}
            />
            {/* Light theme logo */}
            <img
              src={logoLightTheme}
              alt="hatl.one"
              className="h-4 w-auto"
              style={{
                display: theme === 'light' ? 'block' : 'none',
                opacity: 0.7,
              }}
            />
            <div className="text-[11px]" style={{ color: 'var(--text-secondary)', textAlign: 'center' }}>
              {t('unfold.share.footer.tagline')}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}


