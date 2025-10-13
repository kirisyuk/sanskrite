import React, { useState, useEffect, useRef } from 'react';
import Settings from '../components/Settings';
import { useProjectsStore } from '../stores/projectsStore';
import { useDiscoveryStore } from '../stores/discoveryStore';
import { useAuth } from '../contexts/AuthContext';

const DiscoveryPage: React.FC = () => {
  const { currentUser } = useAuth();
  const { projects, currentProject, fetchProjects, deleteProject, setCurrentProject } = useProjectsStore();
  const { sources, loading, executeDiscovery, fetchProjectSources, fetchProjectInteractions, clearSources, connectWs, answer, correlationId, interactions, sendChatMessage } = useDiscoveryStore();
  
  const [queryText, setQueryText] = useState('');
  const [sideOpen, setSideOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [thinkingSeconds, setThinkingSeconds] = useState(0);
  const [showSettings, setShowSettings] = useState(false);
  const correlationRef = useRef<string | null>(null);
  const wsInitialized = useRef(false);

  useEffect(() => {
    fetchProjects('discovery');
  }, [fetchProjects]);

  // Separate effect for WebSocket to prevent duplicate connections
  useEffect(() => {
    if (wsInitialized.current) return;
    
    const tenantId = currentUser?.email || '';
    if (tenantId) {
      wsInitialized.current = true;
      connectWs(tenantId);
    }
  }, [currentUser?.email, connectWs]);

  useEffect(() => {
    if (currentProject) {
      fetchProjectSources(currentProject.id);
      fetchProjectInteractions(currentProject.id);
    } else {
      clearSources();
    }
  }, [currentProject]);

  // Timer for chat "Thinking..." state
  useEffect(() => {
    if (running && currentProject?.id) {
      setThinkingSeconds(0);
      const id = window.setInterval(() => {
        setThinkingSeconds((s) => s + 1);
      }, 1000);
      return () => {
        window.clearInterval(id);
      };
    } else {
      setThinkingSeconds(0);
    }
  }, [running, currentProject?.id]);

  const handleDeleteChat = async (projectId: string, projectName: string) => {
    if (!confirm(`Delete "${projectName}"?`)) return;
    
    try {
      await deleteProject(projectId);
    } catch (error) {
      console.error('Failed to delete chat:', error);
    }
  };

  const parseSourcesFromQuery = (text: string) => {
    // Normalize accidental '@' before URLs (e.g., @https://...)
    const normalized = text.trim().replace(/@(?=https?:\/\/)/g, '');
    
    // Extract URLs
    const rawUrls = [
      ...(normalized.match(/https?:\/\/[\w.-]+(?:\/[\w\-._~:\/?#[\]@!$&'()*+,;=%]+)?/gi) || []),
      ...(normalized.match(/\bwww\.[\w.-]+(?:\/[\w\-._~:\/?#[\]@!$&'()*+,;=%]+)?/gi) || [])
    ];
    
    const normalizeUrl = (u: string) => {
      let v = u.trim();
      if (/^www\./i.test(v)) v = `https://${v}`;
      v = v.replace(/[)\],.;]+$/g, ''); // Strip trailing punctuation
      return v;
    };
    
    const webSources = Array.from(new Set(rawUrls.map(normalizeUrl)));

    // Extract Telegram handles
    const tgHandles = new Set<string>();
    
    // t.me / telegram.me links
    const tgLinks = normalized.match(/(?:https?:\/\/)?(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/gi) || [];
    tgLinks.forEach((m) => {
      const grp = m.match(/(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/i);
      if (grp && grp[1]) tgHandles.add(`@${grp[1]}`);
    });
    
    // @username tokens
    const ats = normalized.match(/(^|\s)@([A-Za-z0-9_]{3,})\b/g) || [];
    ats.forEach((m) => {
      const grp = m.match(/@([A-Za-z0-9_]{3,})/);
      if (grp && grp[1]) tgHandles.add(`@${grp[1]}`);
    });

    return {
      web: webSources,
      telegram: Array.from(tgHandles),
    };
  };

  const handleExecuteDiscovery = async () => {
    if (running || !queryText.trim()) {
      console.log('[Discovery] Blocked:', { running, hasQuery: !!queryText.trim() });
      return;
    }

    const parsed = parseSourcesFromQuery(queryText);
    console.log('[Discovery] Parsed sources:', parsed);
    
    if (parsed.web.length === 0 && parsed.telegram.length === 0) {
      alert('Please include at least one source (URL or @telegram handle) in your query');
      return;
    }

    // Pre-seed pending sources for immediate visual feedback
    const pending = [
      ...parsed.web.map((url) => ({ id: url, kind: 'web' as const, status: 'fetching' })),
      ...parsed.telegram.map((handle) => ({ id: handle, kind: 'telegram' as const, status: 'fetching' })),
    ];
    useDiscoveryStore.getState().setPendingSources(pending);

    setRunning(true);
    
    try {
      // If no currentProject, backend will create one automatically
      const projectId = currentProject?.id || '';
      
      console.log('[Discovery] Executing with projectId:', projectId);
      const result = await executeDiscovery({
        projectId,
        queryText,
        sources: parsed,
      });
      console.log('[Discovery] Result:', result);
      
      correlationRef.current = correlationId;
      // Optimistically append user query interaction (typed as discovery.query)
      const createdAt = new Date().toISOString();
      useDiscoveryStore.setState((state) => ({
        interactions: [
          ...state.interactions,
          { id: createdAt, kind: 'discovery.query', content: queryText, createdAt },
        ],
      }));
      
      // Reload projects after discovery starts (to get newly created project)
      if (!currentProject) {
        setTimeout(() => fetchProjects('discovery'), 1000);
      }
      
      // Reset running state after successful execution
      setRunning(false);
    } catch (error) {
      console.error('[Discovery] Failed to execute:', error);
      setRunning(false);
    }
  };

  const getSourceIcon = (kind?: string) => {
    if (kind === 'web') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 5c7.18 0 13 5.82 13 13" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 11a7 7 0 017 7" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 18a1 1 0 11-2 0 1 1 0 012 0z" />
        </svg>
      );
    } else if (kind === 'telegram') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 11l18-7-7 18-4-8-7-3z" />
        </svg>
      );
    }
    return (
      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
        <circle cx="12" cy="12" r="9" strokeWidth={2} />
      </svg>
    );
  };

  const renderSourceCard = (source: any) => {
    const isFetched = source.status === 'completed' || !!source.profile;
    const inferredKind = source.kind || (typeof source.id === 'string' && source.id.startsWith('@') ? 'telegram' : undefined);

    return (
      <div
        key={source.id}
        className="text-xs rounded-2xl"
        style={{
          backgroundColor: 'var(--bg-sidebar)',
          border: '1px solid var(--overlay-line-90)',
        }}
      >
        <div className="flex items-center gap-2 px-3 py-2">
          {isFetched ? (
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" style={{ color: 'var(--accent-success)' }}>
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 22C6.477 22 2 17.523 2 12S6.477 2 12 2s10 4.477 10 10-4.477 10-10 10z" />
            </svg>
          ) : (
            getSourceIcon(inferredKind)
          )}
          <span>
            <strong style={{ fontWeight: 500 }}>{inferredKind || 'source'}</strong> {source.displayName || source.id}
          </span>
          <span className="ml-auto">
            {source.status === 'processing' && (
              <span className="animate-pulse" style={{ color: 'var(--text-secondary-strong)' }}>...</span>
            )}
            {source.status === 'failed' && (
              <span style={{ color: 'var(--accent-danger)' }}>failed</span>
            )}
          </span>
        </div>

        <div className="px-3 pb-2" style={{ color: 'var(--text-secondary-strong)' }}>
          {source.profile?.motivation ? (
            <span>{source.profile.motivation}</span>
          ) : source.profile?.agenda?.description ? (
            <span>{source.profile.agenda.description}</span>
          ) : source.status === 'processing' ? (
            <span className="animate-pulse" style={{ color: 'var(--text-secondary-strong)' }}>...</span>
          ) : source.description ? (
            <span>{source.description}</span>
          ) : null}
        </div>

        {source.profile?.agenda?.primaryGoal && (
          <div className="ml-5 mt-1 rounded mb-1" style={{ color: 'var(--text-heading)' }}>
            <div>
              <span style={{ color: 'var(--text-secondary-strong)' }}>primaryGoal:</span> {source.profile.agenda.primaryGoal}
            </div>
          </div>
        )}

        {source.profile && (
          <div className="px-3 pb-3 space-y-2 mt-2">
            {source.profile.topics && source.profile.topics.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {source.profile.topics.slice(0, 6).map((t: any, i: number) => (
                  <span
                    key={i}
                    className="px-2 py-0.5 rounded-full text-[10px]"
                    style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}
                  >
                    {t.name}
                    {typeof t.share === 'number' ? ` ${Math.round(t.share * 100)}%` : ''}
                  </span>
                ))}
              </div>
            )}

            {(typeof source.profile.trustScore === 'number' || typeof source.profile.goalAlignmentScore === 'number') && (
              <div className="flex items-center gap-3 text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {typeof source.profile.goalAlignmentScore === 'number' && (
                  <div className="flex items-center gap-1">
                    <span>alignment</span>
                    <div className="w-24 h-1.5 rounded" style={{ backgroundColor: 'var(--overlay-line-90)' }}>
                      <div
                        className="h-1.5 rounded"
                        style={{
                          width: `${Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100}%`,
                          backgroundColor: 'var(--accent-success)',
                        }}
                      />
                    </div>
                    <span>{Math.round(Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100)}%</span>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="flex h-full" style={{ padding: '16px 0' }}>
      {/* Side panel (hidden by default) - LEFT */}
      {sideOpen && (
        <div
          className="flex-shrink-0 rounded-xl mr-4 overflow-hidden relative sidebar-panel"
          style={{ backgroundColor: 'var(--bg-sidebar)', width: '320px', minWidth: '260px', maxWidth: '420px' }}
        >
          <div className="p-6 h-full flex flex-col">
            <div className="flex items-center justify-between mb-6">
              <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Navigation</h3>
              <button
                onClick={() => setSideOpen(false)}
                className="p-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'var(--white-10)', color: '#F1F3F5' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
            </div>

            {/* New Chat action */}
            <div className="mb-3">
              <button
                type="button"
                aria-label="New Chat"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => {
                  // Clear right side: sources, status, input, current chat, interactions
                  clearSources();
                  setCurrentProject(null);
                  setQueryText('');
                }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  {/* Outlined plus in rounded rect (new chat) */}
                  <rect x="3.5" y="3.5" width="17" height="17" rx="3" strokeWidth={2} />
                  <path strokeLinecap="round" strokeWidth={2} d="M12 8v8M8 12h8" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>New Chat</span>
              </button>
            </div>

            {/* Chats section */}
            <div className="mb-4">
              <div className="text-[11px] uppercase tracking-wide mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                Chats
              </div>

              {projects.length === 0 ? (
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>No chats yet</div>
              ) : (
                <div className="space-y-1">
                  {projects.map((project) => (
                    <div
                      key={project.id}
                      className="px-2 py-1 rounded text-xs flex items-center gap-2 group cursor-pointer"
                      title={project.displayName}
                      onClick={() => setCurrentProject(project)}
                      style={{
                        backgroundColor: currentProject?.id === project.id ? 'var(--white-10)' : 'transparent',
                      }}
                      onMouseEnter={(e) => { if (currentProject?.id !== project.id) (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { if (currentProject?.id !== project.id) (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent'; }}
                    >
                      <span className="flex-1 truncate" style={{ color: 'var(--text-heading)' }}>
                        {project.displayName}
                      </span>
                      <button
                        className="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded"
                        title="Delete chat"
                        style={{ backgroundColor: 'transparent' }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDeleteChat(project.id, project.displayName);
                        }}
                      >
                        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12" />
                        </svg>
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Bottom settings button */}
            <div className="mt-auto pt-4">
              <button
                type="button"
                aria-label="Settings"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setShowSettings(true)}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  {/* Outlined gear (non-filled), simple and monotone */}
                  <circle cx="12" cy="12" r="4.5" strokeWidth={2} />
                  <path strokeLinecap="round" strokeWidth={2} d="M12 3v2M12 19v2M3 12h2M19 12h2M5.636 5.636l1.414 1.414M16.95 7.05l1.414-1.414M18.364 18.364l-1.414-1.414M7.05 16.95l-1.414 1.414" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Settings</span>
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Toggle button (when panel hidden) */}
      {!sideOpen && (
        <button
          onClick={() => setSideOpen(true)}
          className="flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center mr-2 transition-all duration-300"
          style={{ backgroundColor: '#F4A261', color: 'white', alignSelf: 'flex-start', marginTop: '20px' }}
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M9 5l7 7-7 7" />
          </svg>
        </button>
      )}

      {/* Grid wrapper: [left free area for sources] [fixed-width center] [right filler] */}
      <div className="flex-1 min-h-0" style={{ display: 'grid', gridTemplateColumns: '1fr minmax(0, 768px) 1fr', gap: '16px' }}>
        {/* Left area: sources take whatever space is available without pushing center */}
        <div className="min-h-0 mr-4" style={{ background: 'transparent' }}>
          {sources.length > 0 && (
            <div className="space-y-2">
              {sources.map((source) => renderSourceCard(source))}
            </div>
          )}
        </div>

        {/* Main center column (fixed max width) */}
        <div className="min-h-0 flex flex-col">
        {showSettings ? (
          <div className="w-full px-4 mt-2 mb-2">
            <div className="mb-2 flex items-center">
              <button
                onClick={() => setShowSettings(false)}
                className="p-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                aria-label="Back"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
              <div className="ml-2 text-13" style={{ color: 'var(--text-heading)' }}>Settings</div>
            </div>
            <Settings />
          </div>
        ) : (
        <>
        {/* Sources moved to the left grid column */}

        {/* Input box anchored to bottom */}
        <div className="w-full max-w-3xl px-4 mt-auto mb-2">
          {/* Interactions history (LLM answers etc.) */}
          {interactions && interactions.length > 0 && (
            <div className="px-1 pb-2">
              <div className="space-y-2">
                {interactions.map((it) => {
                  const isUser = it.kind === 'discovery.query';
                  const containerStyle: React.CSSProperties = {
                    backgroundColor: isUser ? 'var(--bg-elevated)' : 'var(--bg-sidebar)',
                    border: '1px solid var(--overlay-line-90)',
                    maxWidth: '75%',
                  };
                  return (
                    <div key={it.id} className={`rounded-2xl px-3 py-2 ${isUser ? 'ml-auto' : ''}`} style={containerStyle}>
                      <div className="text-[10px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>{isUser ? 'you' : 'assistant'}</div>
                      <div className="text-xs" style={{ color: 'var(--text-heading)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                        {it.content}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Status indicator */}
          {running && (
            <div className="px-1 pb-2">
              <div className="text-xs" style={{ textAlign: 'left', color: 'var(--text-secondary-strong)' }}>
                Thinking... {thinkingSeconds}s
              </div>
            </div>
          )}

          {/* Input box */}
          <div className="rounded-xl border overflow-hidden" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
            <div className="p-3">
              <div className="flex items-end gap-3">
                <div className="flex-1">
                  <textarea
                    rows={3}
                    value={queryText}
                    onChange={(e) => setQueryText(e.target.value)}
                    placeholder="What would you like to discover? Include URLs or @telegram handles..."
                    className="w-full resize-none bg-transparent outline-none"
                    style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)' }}
                    aria-label="Discovery query"
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                        e.preventDefault();
                        handleExecuteDiscovery();
                      }
                    }}
                  />
                </div>
                <button
                  type="button"
                  aria-label="Send"
                  className="flex items-center justify-center rounded-full"
                  onClick={async () => {
                    if (running || !queryText.trim()) return;
                    // If there is a current project, send chat; otherwise, run discovery
                    if (currentProject?.id) {
                      setRunning(true);
                      try {
                        await sendChatMessage(currentProject.id, queryText);
                        setQueryText('');
                      } finally {
                        setRunning(false);
                      }
                    } else {
                      handleExecuteDiscovery();
                    }
                  }}
                  disabled={running || !queryText.trim()}
                  style={{
                    width: '32px',
                    height: '32px',
                    backgroundColor: 'transparent',
                    opacity: running || !queryText.trim() ? 0.6 : 1,
                  }}
                >
                  {running ? (
                    <div className="animate-spin w-6 h-6 border-2 border-t-transparent rounded-full" style={{ borderColor: 'var(--text-secondary-strong)' }} />
                  ) : (
                    <svg className="w-6 h-6" viewBox="0 0 24 24" aria-hidden="true">
                      <path d="M8 5v14l11-7z" fill="var(--text-secondary-strong)" />
                    </svg>
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
        </>
        )}
        </div>

        {/* Right filler column (keeps center truly centered) */}
        <div />
      </div>
    </div>
  );
};

export default DiscoveryPage;
