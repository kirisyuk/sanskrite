import React from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { ThemeContext } from './ThemeProvider';
import { useAuth } from '../contexts/AuthContext';
import { UserProfile } from './UserProfile';
import { useTranslation } from 'react-i18next';
import { useUIStore } from '../stores/uiStore';
import { useIsAdmin } from '../hooks/useIsAdmin';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useProjectsStore } from '../stores/projectsStore';
import { useStrategicRecommendationsStore } from '../stores/strategicRecommendationsStore';
import { api } from '../lib/api';

export const TopBar: React.FC<{ showUser?: boolean }> = ({ showUser = true }) => {
  const { theme, setTheme } = React.useContext(ThemeContext);
  const { currentUser } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const { i18n } = useTranslation();
  const uiLanguage = useUIStore((s) => s.uiLanguage);
  const setUiLanguage = useUIStore((s) => s.setUiLanguage);
  const isAdmin = useIsAdmin();
  const openStrategicInbox = useUIStore((s) => s.openStrategicInbox);
  const pendingRecommendations = useStrategicRecommendationsStore((s) => s.pendingCount);
  const tenantPendingRecommendations = useStrategicRecommendationsStore((s) => s.tenantPendingCount);
  const globalPendingRecommendations = useStrategicRecommendationsStore((s) => s.globalPendingCount);
  const fetchRecommendationsCount = useStrategicRecommendationsStore((s) => s.fetchCount);
  // PERFORMANCE FIX: Subscribe only to specific values, not entire store
  const genaiChatProjectId = useGenaiChatStore((s) => s.projectId);
  const genaiChatInteractionsLength = useGenaiChatStore((s) => s.interactions.length);
  const genaiChatMessagesLength = useGenaiChatStore((s) => s.messages.length);
  const { currentProject } = useProjectsStore();
  const [shareBusy, setShareBusy] = React.useState(false);
  const [shareOpen, setShareOpen] = React.useState(false);
  const [shareUrl, setShareUrl] = React.useState<string | null>(null);
  const [shareUrlProjectId, setShareUrlProjectId] = React.useState<string | null>(null); // Track which project the share URL is for
  const [copied, setCopied] = React.useState(false);
  const [downloadBusy, setDownloadBusy] = React.useState(false);
  const [modeOpen, setModeOpen] = React.useState(false);
  const shareRef = React.useRef<HTMLDivElement | null>(null);
  const modeRef = React.useRef<HTMLDivElement | null>(null);
  const currentMode = React.useMemo<'Structure' | 'Discuss' | 'CriticalChain'>(() => {
    if (location.pathname.startsWith('/criticalchain')) return 'CriticalChain';
    if (location.pathname.startsWith('/horizon')) return 'Structure';
    return 'Discuss';
  }, [location.pathname]);
  const toggleTheme = React.useCallback(() => {
    const next = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
  }, [theme, setTheme]);

  // Keep UI store in sync with i18n (initial mount and external changes)
  React.useEffect(() => {
    const current = (i18n.language || 'en').toLowerCase();
    setUiLanguage(current);
    try {
      document.documentElement.setAttribute('lang', current);
    } catch {}
  }, [i18n.language, setUiLanguage]);

  const handleLanguageChange = React.useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    const raw = (e.target.value || 'en').toLowerCase();
    const mapped = raw === 'kz' ? 'kk' : raw;
    setUiLanguage(mapped);
    try { document.documentElement.setAttribute('lang', mapped); } catch {}
    i18n.changeLanguage(mapped);
  }, [i18n, setUiLanguage]);

  const selectedUi = uiLanguage === 'kk' ? 'kz' : (uiLanguage || 'en');
  
  // Determine which project ID to use for sharing
  // Always prioritize currentProject?.id (the project explicitly selected in sidebar)
  // This ensures that when user switches projects in sidebar, the correct project is shared
  // Fallback to genaiChatProjectId only if currentProject is not set
  const shareProjectId = React.useMemo(() => {
    if (currentProject?.id && currentProject.id !== 'local-unfold') {
      return currentProject.id;
    }
    // Otherwise, fallback to genaiChatProjectId (the active chat project)
    return genaiChatProjectId;
  }, [currentProject?.id, genaiChatProjectId]);
  
  const canShare = React.useMemo(() => {
    try {
      if (!currentUser) return false;
      if (!shareProjectId || shareProjectId === 'local-unfold') return false;
      return genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0;
    } catch { return false; }
  }, [shareProjectId, genaiChatInteractionsLength, genaiChatMessagesLength, currentUser]);

  const createShare = React.useCallback(async () => {
    try {
      if (shareBusy) return;
      setShareBusy(true);
      // Get currentProject directly from store at call time (not from closure)
      // This ensures we always use the latest selected project
      const projectsStore = useProjectsStore.getState();
      const currentProj = projectsStore.currentProject;
      const genaiChatStore = useGenaiChatStore.getState();
      const genaiChatPid = genaiChatStore.projectId;
      
      // Determine project ID: prioritize currentProject?.id if set, otherwise use genaiChatProjectId
      let pid: string | null = null;
      if (currentProj?.id && currentProj.id !== 'local-unfold') {
        pid = typeof currentProj.id === 'string' ? currentProj.id : String(currentProj.id);
      } else if (genaiChatPid && genaiChatPid !== 'local-unfold') {
        pid = typeof genaiChatPid === 'string' ? genaiChatPid : String(genaiChatPid);
      }
      
      if (!pid) { 
        setShareBusy(false); 
        return; 
      }
      
      console.log('[TopBar] Creating share for project', { 
        projectId: pid, 
        currentProjectId: currentProj?.id,
        genaiChatProjectId: genaiChatPid
      });
      
      // default: 7 days from now
      const exp = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString();
      const res = await api.shareCreate(pid, exp);
      const rel = res?.shareToken ? `/shared/${res.shareToken}` : null;
      setShareUrl(rel);
      setShareUrlProjectId(pid); // Store which project this URL is for
      setShareOpen(true);
    } catch {
      setShareUrl(null);
      setShareUrlProjectId(null);
      setShareOpen(true);
    } finally {
      setShareBusy(false);
    }
  }, [shareBusy]);
  
  // Clear share URL when project changes (so we don't show old URL for new project)
  React.useEffect(() => {
    const projectsStore = useProjectsStore.getState();
    const currentProj = projectsStore.currentProject;
    const genaiChatStore = useGenaiChatStore.getState();
    const genaiChatPid = genaiChatStore.projectId;
    
    // Determine current project ID
    let currentPid: string | null = null;
    if (currentProj?.id && currentProj.id !== 'local-unfold') {
      currentPid = typeof currentProj.id === 'string' ? currentProj.id : String(currentProj.id);
    } else if (genaiChatPid && genaiChatPid !== 'local-unfold') {
      currentPid = typeof genaiChatPid === 'string' ? genaiChatPid : String(genaiChatPid);
    }
    
    // If project changed and we have a share URL for a different project, clear it
    if (shareUrlProjectId && currentPid && shareUrlProjectId !== currentPid) {
      console.log('[TopBar] Project changed, clearing share URL', { 
        oldProjectId: shareUrlProjectId, 
        newProjectId: currentPid 
      });
      setShareUrl(null);
      setShareUrlProjectId(null);
      setShareOpen(false);
    }
  }, [currentProject?.id, genaiChatProjectId, shareUrlProjectId]);

  React.useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      const target = e.target as Node;
      if (shareOpen) {
        const shareEl = shareRef.current;
        if (shareEl && !shareEl.contains(target)) setShareOpen(false);
      }
      if (modeOpen) {
        const modeEl = modeRef.current;
        if (modeEl && !modeEl.contains(target)) setModeOpen(false);
      }
    };
    document.addEventListener('mousedown', onDocClick, { capture: true });
    return () => document.removeEventListener('mousedown', onDocClick, { capture: true } as any);
  }, [shareOpen, modeOpen]);

  React.useEffect(() => {
    setModeOpen(false);
  }, [location.pathname, location.search]);

  React.useEffect(() => {
    if (!currentUser) return;
    void fetchRecommendationsCount({ scope: isAdmin ? 'all' : 'tenant' });
  }, [currentUser, fetchRecommendationsCount, isAdmin]);

  // Export chat to markdown
  const exportToMarkdown = React.useCallback(() => {
    try {
      if (downloadBusy) return;
      setDownloadBusy(true);

      const genaiChatStore = useGenaiChatStore.getState();
      const interactions = genaiChatStore.interactions || [];
      
      // Filter interactions similar to useMergedMessages
      const filteredInteractions = interactions.filter((it) => {
        // Exclude perspective interactions
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        // Exclude queued tasks interactions
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind === 'queued.tasks') {
          return false;
        }
        // Exclude perspective analysis interactions
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
          return false;
        }
        // Exclude document attachments
        if (it.subkind === 'document.attach') {
          return false;
        }
        // Exclude layout interactions
        if (it.kind === 'ontology' && it.subkind === 'layout') {
          return false;
        }
        // Exclude agent execution interactions
        if (it.kind === 'agent.execution') {
          return false;
        }
        // Exclude Model chat interactions
        if (it.subkind === 'graph.chat') {
          return false;
        }
        // Exclude ontology interactions (they belong to Graph view)
        if (it.kind === 'ontology') {
          return false;
        }
        // Include user, assistant, action, research, section markers, bring in
        return (
          it.kind === 'user' ||
          it.kind === 'assistant' ||
          it.kind === 'action' ||
          it.kind === 'research' ||
          it.kind === 'section_start' ||
          it.kind === 'section_end'
        );
      });

      // Sort by timestamp
      const sortedInteractions = [...filteredInteractions].sort((a, b) => {
        const getTimestamp = (it: any): number => {
          try {
            const originalMsgId = it.originalMessageId;
            if (originalMsgId && typeof originalMsgId === 'string') {
              const parsedTs = parseInt(originalMsgId.split('-')[0]);
              if (!Number.isNaN(parsedTs) && parsedTs > 0) {
                return parsedTs;
              }
            }
          } catch {}
          try {
            const ca: any = it.createdAt;
            if (ca && typeof ca.seconds === 'number') {
              return ca.seconds * 1000;
            } else if (typeof ca === 'string') {
              const d = Date.parse(ca);
              if (!Number.isNaN(d)) return d;
            } else if (typeof ca === 'number') {
              return ca;
            }
          } catch {}
          return 0;
        };
        const tsA = getTimestamp(a);
        const tsB = getTimestamp(b);
        if (tsA !== tsB) return tsA - tsB;
        // On ties, user before assistant
        if (a.kind === 'user' && b.kind !== 'user') return -1;
        if (a.kind !== 'user' && b.kind === 'user') return 1;
        return 0;
      });

      // Build markdown content
      const lines: string[] = [];
      
      // Add title from project name
      const projectName = currentProject?.displayName || 'Chat';
      const title = projectName.length > 50 ? projectName.substring(0, 50) : projectName;
      lines.push(`# ${title}\n`);

      // Extract "How you engage" from eval.stance interaction (project-level, without replyTo)
      const stanceInteraction = interactions.find(
        (it) => it.subkind === 'eval.stance' && !it.replyTo
      );
      if (stanceInteraction?.content || stanceInteraction?.contentSanitized) {
        const stanceContent = stanceInteraction.contentSanitized || stanceInteraction.content || '';
        if (stanceContent.trim()) {
          // Sanitize SX instructions
          const sanitizedContent = stanceContent.replace(/<!--\s*@sx:[\s\S]*?-->/g, '').trim();
          if (sanitizedContent) {
            lines.push(`## How you engage\n\n${sanitizedContent}\n`);
          }
        }
      }

      // Extract Discussion Frame and Blind Spots from discussion.flow.analysis interaction
      const analysisInteraction = interactions.find(
        (it) => it.subkind === 'discussion.flow.analysis'
      );
      if (analysisInteraction?.content || analysisInteraction?.contentSanitized) {
        try {
          const content = analysisInteraction.contentSanitized || analysisInteraction.content || '{}';
          const parsed = JSON.parse(content);
          
          // Add Discussion Frame
          if (parsed.frame && typeof parsed.frame === 'string' && parsed.frame.trim()) {
            lines.push(`## Discussion Frame\n\n${parsed.frame.trim()}\n`);
          }
          
          // Add Blind Spots if they exist
          if (parsed.blindSpots && Array.isArray(parsed.blindSpots) && parsed.blindSpots.length > 0) {
            const normalizedBlindSpots = parsed.blindSpots
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
            
            if (normalizedBlindSpots.length > 0) {
              lines.push(`\n## Blind Spots\n`);
              normalizedBlindSpots.forEach((blindSpot: any) => {
                lines.push(`- ${blindSpot.text}`);
              });
              lines.push('');
            }
          }
          
          lines.push('---\n'); // Separator before chat content
        } catch (e) {
          // Ignore parse errors
        }
      }

      let currentSection: string | null = null;
      
      // Build a map of reframes (frame expansions) by target message ID
      // Include eval.frame_expansions interactions for reframe mapping
      const allInteractionsForReframes = interactions.filter((it) => it.subkind === 'eval.frame_expansions');
      const reframesMap = new Map<string, any[]>();
      const persistedByLocalId = genaiChatStore.persistedByLocalId || {};
      
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(persistedByLocalId).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      
      allInteractionsForReframes.forEach((it) => {
        if (it.relations) {
          it.relations.forEach((rel: any) => {
            if (rel.type === 'refines' && rel.targetId) {
              // Get all possible IDs for this target (local, server, mapped)
              const targetIds = new Set<string>();
              targetIds.add(rel.targetId);
              const serverId = persistedByLocalId[rel.targetId] || rel.targetId;
              targetIds.add(serverId);
              if (reverseMap[serverId]) {
                reverseMap[serverId].forEach((localId) => targetIds.add(localId));
              }
              
              // Parse expansions from content
              try {
                const content = it.content || it.contentSanitized || '{}';
                const parsed = JSON.parse(content);
                if (parsed.expansions && Array.isArray(parsed.expansions)) {
                  parsed.expansions.forEach((exp: any) => {
                    if (exp.question) {
                      // Add reframes to all possible IDs
                      targetIds.forEach((targetId) => {
                        if (!reframesMap.has(targetId)) {
                          reframesMap.set(targetId, []);
                        }
                        reframesMap.get(targetId)!.push(exp.question);
                      });
                    }
                  });
                }
              } catch {}
            }
          });
        }
      });

      sortedInteractions.forEach((it) => {
        // Handle section markers
        if (it.kind === 'section_start') {
          const sectionName = it.content || 'Section';
          currentSection = sectionName;
          lines.push(`\n## ${sectionName}\n`);
          return;
        }
        if (it.kind === 'section_end') {
          currentSection = null;
          lines.push('');
          return;
        }

        // Handle action interactions (like bring in)
        if (it.kind === 'action') {
          if (it.subkind === 'action.bring_in') {
            const agentProfile = it.agentProfile;
            const modelName = agentProfile?.model || 'Unknown';
            const provider = agentProfile?.provider || '';
            lines.push(`\n*Bring in: ${provider} ${modelName}*\n`);
          } else {
            lines.push(`\n*${it.content || 'Action'}*\n`);
          }
          return;
        }

        // Handle research interactions
        if (it.kind === 'research') {
          const query = it.researchQuery || it.content || 'Research';
          lines.push(`\n### Research: ${query}\n`);
          if (it.researchSources && it.researchSources.length > 0) {
            lines.push('Sources:');
            it.researchSources.forEach((source: any) => {
              lines.push(`- [${source.title || 'Untitled'}](${source.url || ''})`);
            });
            lines.push('');
          }
          return;
        }

        // Handle user messages
        if (it.kind === 'user') {
          const content = it.contentSanitized || it.content || '';
          if (content.trim()) {
            // Skip bring in indicator messages (they're already shown as action)
            if (it.subkind !== 'action.bring_in') {
              lines.push(`### User\n\n${content}\n`);
            }
          }
          return;
        }

        // Handle assistant messages
        if (it.kind === 'assistant') {
          const content = it.contentSanitized || it.content || '';
          if (content.trim()) {
            const authorName = it.author?.displayName || 
                              (it.agentProfile ? `${it.agentProfile.provider} ${it.agentProfile.model}` : 'Assistant');
            lines.push(`### ${authorName}\n\n${content}\n`);
            
            // Add reframes if available
            const map = genaiChatStore.persistedByLocalId || {};
            const serverId = map[it.id] || it.id;
            const reframes = reframesMap.get(it.id) || reframesMap.get(serverId) || [];
            if (reframes.length > 0) {
              lines.push('\n**Reframes:**');
              reframes.forEach((question: string) => {
                lines.push(`- ${question}`);
              });
              lines.push('');
            }
            
            // Add sources if available
            if (it.sources && it.sources.length > 0) {
              lines.push('\n**Sources:**');
              it.sources.forEach((url: string) => {
                lines.push(`- ${url}`);
              });
              lines.push('');
            }
          }
          return;
        }
      });

      const markdownContent = lines.join('\n');

      // Create filename from project name (use full project name, not truncated title)
      const sanitizeFilename = (name: string): string => {
        return name
          .replace(/[^a-z0-9]/gi, '_')
          .replace(/_+/g, '_')
          .replace(/^_|_$/g, '')
          .substring(0, 50) || 'chat';
      };
      // Use full project name for filename, not truncated title
      const filename = `${sanitizeFilename(projectName)}.md`;

      // Download file
      const blob = new Blob([markdownContent], { type: 'text/markdown' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (error) {
      console.error('[TopBar] Failed to export chat', error);
    } finally {
      setDownloadBusy(false);
    }
  }, [downloadBusy, currentProject?.displayName]);

  return (
    <div className="topbar">
      {/* Left side - Logo (aligned to sidebar toggle spacing) */}
      <div className="flex items-center gap-3 px-3">
        <div className="hzn-brand-mark" aria-label="Horizon navigation" ref={modeRef}>
          <span className="hzn-brand-primary">Horizon</span>
          <span className="hzn-brand-divider" aria-hidden="true">|</span>
          {modeOpen ? (
            <div className="hzn-brand-inline-choices hzn-brand-inline-choices-open" role="menu" aria-label="Mode selection">
              <button
                type="button"
                role="menuitem"
                className="hzn-brand-inline-choice"
                onClick={() => { setModeOpen(false); navigate('/horizon'); }}
              >
                Structure
              </button>
              <span className="hzn-brand-divider" aria-hidden="true">|</span>
              <button
                type="button"
                role="menuitem"
                className="hzn-brand-inline-choice"
                onClick={() => { setModeOpen(false); navigate('/'); }}
              >
                Discuss
              </button>
              <span className="hzn-brand-divider" aria-hidden="true">|</span>
              <button
                type="button"
                role="menuitem"
                className="hzn-brand-inline-choice"
                onClick={() => { setModeOpen(false); navigate('/criticalchain'); }}
              >
                CriticalChain
              </button>
            </div>
          ) : (
            <button
              type="button"
              className="hzn-brand-option hzn-brand-option-trigger"
              aria-label="Select mode"
              aria-haspopup="menu"
              aria-expanded={false}
              onClick={() => setModeOpen(true)}
            >
              {currentMode}
            </button>
          )}
        </div>
      </div>

      {/* Center - View mode toggle temporarily disabled */}
      <div />

      {/* Right - Download, Share, Language switcher, Theme toggle and User profile */}
      <div className="flex items-center gap-3 px-2">
        {/* Download (left of Share), only when user is authenticated and chat is not new/empty) */}
        {canShare && (
          <button
            type="button"
            className="rounded-lg text-12 px-2 py-1"
            aria-label="Download chat as markdown"
            title="Download chat as markdown"
            onClick={exportToMarkdown}
            disabled={downloadBusy}
            style={{ backgroundColor: 'transparent', color: 'var(--text-heading)', border: '1px solid var(--white-10)' }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
          >
            {downloadBusy ? 'Downloading…' : 'Download'}
          </button>
        )}
        {/* Share (left of language), only when user is authenticated and chat is not new/empty) */}
        {canShare && (
          <div ref={shareRef} style={{ position: 'relative' }}>
            <button
              type="button"
              className="rounded-lg text-12 px-2 py-1"
              aria-label="Share project"
              title="Share project"
              onClick={() => { if (!shareUrl) { createShare(); } else { setShareOpen((v) => !v); } }}
              disabled={shareBusy}
              style={{ backgroundColor: 'transparent', color: 'var(--text-heading)', border: '1px solid var(--white-10)' }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
            >
              {shareBusy ? 'Sharing…' : 'Share'}
            </button>
            {shareOpen && (
              <div className="rounded-lg p-2 mt-1" style={{ position: 'absolute', right: 0, top: '100%', zIndex: 60, minWidth: 280, background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', boxShadow: '0 4px 14px rgba(0,0,0,0.2)' }}>
                {shareUrl ? (
                  <div>
                    <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Public read‑only link</div>
                    <div className="flex items-center gap-2">
                      {(() => {
                        const origin = (typeof window !== 'undefined' ? window.location.origin : '');
                        const abs = (shareUrl || '').startsWith('http') ? (shareUrl as string) : (origin + (shareUrl || ''));
                        return (
                          <>
                            <input readOnly value={abs} className="text-12 flex-1" style={{ background: 'transparent', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)', borderRadius: 6, padding: '4px 6px' }} />
                            <button
                              className="text-12 px-2 py-1 rounded"
                              style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
                              onClick={() => { try { navigator.clipboard.writeText(abs); setCopied(true); setTimeout(() => setCopied(false), 1200); } catch {} }}
                            >{copied ? 'Copied' : 'Copy'}</button>
                          </>
                        );
                      })()}
                    </div>
                    {copied && (
                      <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>Copied to clipboard</div>
                    )}
                    <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>Default expiry: 7 days</div>
                  </div>
                ) : (
                  <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Failed to create link. Try again.</div>
                )}
              </div>
            )}
          </div>
        )}
        {/* Language switcher */}
        <select
          aria-label="Select language"
          className="lang-select text-12 px-2 py-1"
          value={selectedUi}
          onChange={handleLanguageChange}
        >
          <option value="en">EN</option>
          <option value="de">DE</option>
          <option value="es">ES</option>
          <option value="ru">RU</option>
          <option value="kz">KZ</option>
        </select>
        {/* Admin (only when admin) */}
        {currentUser && (
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="p-2 rounded"
              aria-label="Strategic inbox"
              title="Strategic inbox"
              onClick={() => openStrategicInbox()}
              style={{ backgroundColor: 'transparent', transition: 'background-color 150ms ease', position: 'relative' }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }}
            >
              <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} style={{ color: 'var(--text-heading)' }}>
                <path d="M15 17h5l-1.4-1.4A2 2 0 0118 14.2V11a6 6 0 10-12 0v3.2a2 2 0 01-.6 1.4L4 17h5" />
                <path d="M10 17a2 2 0 004 0" />
              </svg>
              {pendingRecommendations > 0 && (
                <span
                  style={{
                    position: 'absolute',
                    top: 0,
                    right: 0,
                    minWidth: 16,
                    height: 16,
                    borderRadius: 999,
                    background: '#d14b3f',
                    color: '#fff',
                    fontSize: 10,
                    lineHeight: '16px',
                    textAlign: 'center',
                    padding: '0 4px',
                  }}
                >
                  {pendingRecommendations > 99 ? '99+' : pendingRecommendations}
                </span>
              )}
            </button>
            {isAdmin && (
              <div className="flex items-center gap-1 text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                <span>T {tenantPendingRecommendations}</span>
                <span>G {globalPendingRecommendations}</span>
              </div>
            )}
          </div>
        )}
        {currentUser && isAdmin && (
          <button
            type="button"
            className="p-2 rounded"
            aria-label="Admin panel"
            title="Admin panel"
            onClick={() => useUIStore.getState().openAdminModal()}
            style={{ backgroundColor: 'transparent', transition: 'background-color 150ms ease' }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
            }}
          >
            <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} style={{ color: 'var(--text-heading)' }}>
              <path d="M12 3l7 3v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-3z" />
              <path d="M9.5 12.5l1.8 1.8 3.2-3.3" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        )}
        {/* Theme toggle */}
        <button
          className="p-2 rounded"
          onClick={toggleTheme}
          title={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
          aria-label="Toggle theme"
          style={{ backgroundColor: 'transparent', transition: 'background-color 150ms ease' }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
          }}
        >
          {theme === 'dark' ? (
            <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" style={{ color: 'var(--text-heading)' }}>
              <path d="M21.64 13.65A9 9 0 1110.35 2.36 7 7 0 0021.64 13.65z" />
            </svg>
          ) : (
            <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" style={{ color: 'var(--text-heading)' }}>
              <path d="M6.76 4.84l-1.8-1.79L3.17 4.84 4.96 6.63l1.8-1.79zM1 13h3v-2H1v2zm10 10h2v-3h-2v3zm9-10v-2h-3v2h3zm-3.76 7.16l1.79 1.8 1.79-1.8-1.79-1.79-1.79 1.79zM13 1h-2v3h2V1zm-8.03 14.66l-1.79 1.79 1.79 1.8 1.8-1.8-1.8-1.79zM12 6a6 6 0 100 12 6 6 0 000-12z" />
            </svg>
          )}
        </button>

        {/* User profile */}
        {showUser && currentUser && <UserProfile />}
      </div>
    </div>
  );
};
