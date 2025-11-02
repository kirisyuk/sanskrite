import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useAuth } from '../contexts/AuthContext';
import { useProjectsStore } from '../stores/projectsStore';
import { api } from '../lib/api';

// Inline lightweight component to fetch and display title suggestions
export const TitleSuggestions: React.FC<{ visible: boolean; onClose: () => void; onAvailableChange?: (available: boolean) => void }> = ({ visible, onClose, onAvailableChange }) => {
  const [titles, setTitles] = React.useState<string[] | null>(null);
  const [loading, setLoading] = React.useState(false);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
  const { currentProject } = useProjectsStore();
  const appliedProjectRef = React.useRef<string | null>(null);
  const finalizedRef = React.useRef(false);
  const fetchedForProjectRef = React.useRef<string | null>(null);
  const hasAssistant = React.useMemo(() =>
    genaiChat.messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0),
    [genaiChat.messages]
  );

  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      // Require authenticated user and at least first assistant answer
      if (!currentUser?.uid) return;
      if (!hasAssistant) return;
      // Guard: operate only for the active chat project id
      let pid: string | null = null;
      try {
        pid = useGenaiChatStore.getState().projectId;
        const cpid = useProjectsStore.getState().currentProject?.id;
        if (!pid || pid === 'local-unfold' || !cpid || pid !== cpid) return;
      } catch {}
      // If user has already finalized a title or project already named (not Draft), skip fetching
      try {
        const projectId = useGenaiChatStore.getState().projectId || currentProject?.id;
        if (projectId && projectId !== 'local-unfold' && typeof window !== 'undefined') {
          finalizedRef.current = window.localStorage.getItem(`chatTitleFinal:${projectId}`) === '1';
        }
        const name = (useProjectsStore.getState().currentProject?.displayName || '').trim();
        if (finalizedRef.current || (name && name.toLowerCase() !== 'draft')) return;
      } catch {}
      // If we already fetched titles for this project, don't fetch again
      if (fetchedForProjectRef.current === pid) return;
      setLoading(true);
      try {
        // Build messages from local interactions for better signal (summary if available)
        const st = useGenaiChatStore.getState();
        // Take up to 5 latest USER interactions (oldest->newest)
        const userInteractions = st.interactions.filter((it) => it.kind === 'user');
        const last5 = userInteractions.slice(Math.max(0, userInteractions.length - 5));
        const messages = last5.map((it) => ({
          role: 'user' as const,
          text: (it.summary && it.summary.trim()) ? it.summary : it.content,
        }));
        // If empty, fallback to chat messages
        if (messages.length === 0) {
          const fallbackAll = genaiChat.messages.filter((m) => m.role === 'user');
          const fallbackLast5 = fallbackAll.slice(Math.max(0, fallbackAll.length - 5));
          const fallback: Array<{ role: 'user'; text: string }> = fallbackLast5.map((m) => ({ role: 'user', text: m.content }));
          fallback.forEach((mm) => messages.push(mm));
        }
        const res = await api.suggestChatTitles(messages as Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>);
        if (!cancelled) {
          setTitles(res);
          // Mark this project as having titles fetched
          if (pid) fetchedForProjectRef.current = pid;
        }
      } catch {
        if (!cancelled) setTitles([]);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.messages.length, currentUser?.uid, hasAssistant, currentProject?.id]);

  React.useEffect(() => {
    try { onAvailableChange && onAvailableChange(!!(titles && titles.length > 0)); } catch {}
  }, [titles?.length]);

  // When title suggestions appear, set the first suggestion as chat title, switch view to discovery and persist
  React.useEffect(() => {
    try {
      if (!titles || titles.length === 0) return;
      const first = (titles[0] || '').trim();
      if (!first) return;
      try {
        const activePid = useGenaiChatStore.getState().projectId;
        const sidePid = useProjectsStore.getState().currentProject?.id;
        console.log('[Titles] apply candidate', { first, activePid, sidePid });
      } catch {}
      // Guard: only auto-apply for the exact active chat project id
      const activePid = ((): string | null => { try { return useGenaiChatStore.getState().projectId || null; } catch { return null; } })();
      if (!activePid || !currentProject?.id || activePid !== currentProject.id) return;
      const proj = currentProject;
      if (!proj?.id) {
        // Hydrate currentProject from genaiChat if it was created via ensureProject()
        (async () => {
          try {
            const pid = activePid;
            if (pid && pid !== 'local-unfold') {
              const p = await api.getProject(pid);
              useProjectsStore.setState((prev) => ({
                projects: [p as any, ...prev.projects.filter((x) => x.id !== (p as any).id)],
                currentProject: p as any,
              }));
            }
          } catch {}
        })();
        return;
      }
      if (appliedProjectRef.current === proj.id) return; // already applied for this project instance
      if ((proj.displayName || '').trim() === first && (proj.view || '') === 'discovery') { appliedProjectRef.current = proj.id; return; }
      (async () => {
        try {
          await api.updateProject(activePid, { displayName: first, view: 'discovery' } as any);
          console.log('[Titles] auto-applied to project', { projectId: activePid, name: first });
          useProjectsStore.setState((prev) => {
            const exists = prev.projects.some((p) => p.id === activePid);
            const updated = { ...(prev.currentProject || proj), id: activePid, displayName: first, view: 'discovery' } as any;
            return {
              projects: exists
                ? prev.projects.map((p) => (p.id === activePid ? { ...p, displayName: first, view: 'discovery' } : p))
                : [{ id: activePid, displayName: first, view: 'discovery' } as any, ...prev.projects],
              currentProject: updated,
            } as any;
          });
          appliedProjectRef.current = activePid;
          // Finalize: suppress future suggestions and hide component permanently
          try { if (typeof window !== 'undefined') window.localStorage.setItem(`chatTitleFinal:${activePid}`, '1'); } catch {}
          finalizedRef.current = true;
          try { onAvailableChange && onAvailableChange(false); } catch {}
          try { onClose && onClose(); } catch {}
          setTitles(null);
        } catch {}
      })();
    } catch {}
  }, [titles && titles[0], currentProject?.id]);
  // Never render UI; only background auto-apply logic is kept
  return null;
};

