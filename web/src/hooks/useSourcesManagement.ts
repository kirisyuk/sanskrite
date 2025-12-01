import { useState, useRef, useEffect } from 'react';
import { useDiscoveryStore } from '../stores/discoveryStore';
import { useProjectsStore } from '../stores/projectsStore';
import { getPreferredLanguage } from '../lib/uiUtils';
import { api } from '../lib/api';

export function useSourcesManagement() {
  const [previewSource, setPreviewSource] = useState<any | null>(null);
  const [showAddSourceMenu, setShowAddSourceMenu] = useState(false);
  const addMenuRef = useRef<HTMLDivElement | null>(null);
  const [showAddTelegramModal, setShowAddTelegramModal] = useState(false);
  const [showAddRSSModal, setShowAddRSSModal] = useState(false);
  const [tgInput, setTgInput] = useState('');
  const [rssInput, setRssInput] = useState('');
  const [tgSubmitting, setTgSubmitting] = useState(false);
  const [rssSubmitting, setRssSubmitting] = useState(false);
  const [tgError, setTgError] = useState<string | null>(null);
  const [rssError, setRssError] = useState<string | null>(null);
  
  const { executeDiscovery, fetchProjectSources } = useDiscoveryStore();
  const currentProject = useProjectsStore((s) => s.currentProject);

  // Close add source menu on outside click or Escape
  useEffect(() => {
    function onDocMouseDown(e: MouseEvent) {
      if (!showAddSourceMenu) return;
      if (
        addMenuRef.current &&
        !addMenuRef.current.contains(e.target as Node)
      ) {
        setShowAddSourceMenu(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setShowAddSourceMenu(false);
    }
    document.addEventListener('mousedown', onDocMouseDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocMouseDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [showAddSourceMenu]);

  const normalizeTelegramInput = (value: string): string | null => {
    const v = (value || '').trim();
    if (!v) return null;
    const linkMatch = v.match(
      /(?:https?:\/\/)?(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/i,
    );
    if (linkMatch && linkMatch[1]) return '@' + linkMatch[1];
    const atMatch = v.match(/^@([A-Za-z0-9_]{3,})$/);
    if (atMatch && atMatch[1]) return '@' + atMatch[1];
    const bare = v.match(/^([A-Za-z0-9_]{3,})$/);
    if (bare && bare[1]) return '@' + bare[1];
    return null;
  };

  const addTelegramSource = async () => {
    setTgError(null);
    const handle = normalizeTelegramInput(tgInput);
    if (!handle) {
      setTgError('Enter @username or t.me/username');
      return;
    }
    // Pre-seed skeleton (pending source)
    const pending = {
      id: handle,
      kind: 'telegram' as const,
      status: 'fetching',
    };
    const store = useDiscoveryStore.getState();
    store.setPendingSources([...(store as any).pendingSources, pending]);
    if (currentProject?.id) {
      // Merge pending skeleton into visible list right away
      fetchProjectSources(currentProject.id);
    }

    setTgSubmitting(true);
    try {
      const lang = getPreferredLanguage();
      await executeDiscovery({
        projectId: currentProject?.id || '',
        queryText: handle, // minimal context; backend ignores for existing project
        language: lang,
        sources: { web: [], telegram: [handle] },
      });
      setShowAddTelegramModal(false);
      setTgInput('');
      
      // Notify about source update
      window.dispatchEvent(new CustomEvent('sources.updated', { 
        detail: { projectId: currentProject?.id } 
      }));
    } catch (e: any) {
      setTgError(e?.message || 'Failed to add channel');
    } finally {
      setTgSubmitting(false);
    }
  };

  const addRSSSource = async () => {
    setRssError(null);
    const url = rssInput.trim();
    if (!url) {
      setRssError('Enter RSS feed URL');
      return;
    }
    
    // Pre-seed skeleton (pending source)
    const pending = {
      id: url,
      kind: 'rss' as const,
      status: 'fetching',
    };
    const store = useDiscoveryStore.getState();
    store.setPendingSources([...(store as any).pendingSources, pending]);
    if (currentProject?.id) {
      // Merge pending skeleton into visible list right away
      fetchProjectSources(currentProject.id);
    }

    setRssSubmitting(true);
    try {
      const lang = getPreferredLanguage();
      await executeDiscovery({
        projectId: currentProject?.id || '',
        queryText: url, 
        language: lang,
        sources: { web: [], telegram: [], rss: [url] },
      });
      setShowAddRSSModal(false);
      setRssInput('');
      
      // Notify about source update
      window.dispatchEvent(new CustomEvent('sources.updated', { 
        detail: { projectId: currentProject?.id } 
      }));
    } catch (e: any) {
      setRssError(e?.message || 'Failed to add RSS feed');
    } finally {
      setRssSubmitting(false);
    }
  };

  const parseSourcesFromQuery = (text: string) => {
    // Normalize accidental '@' before URLs (e.g., @https://...)
    const normalized = text.trim().replace(/@(?=https?:\/\/)/g, '');
    
    // Extract URLs
    const rawUrls = [
      ...(normalized.match(
        /https?:\/\/[\w.-]+(?:\/[\w\-._~:\/?#[\]@!$&'()*+,;=%]+)?/gi,
      ) || []),
      ...(normalized.match(
        /\bwww\.[\w.-]+(?:\/[\w\-._~:\/?#[\]@!$&'()*+,;=%]+)?/gi,
      ) || []),
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
    const tgLinks =
      normalized.match(
        /(?:https?:\/\/)?(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/gi,
      ) || [];
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

  return {
    previewSource,
    setPreviewSource,
    showAddSourceMenu,
    setShowAddSourceMenu,
    addMenuRef,
    showAddTelegramModal,
    setShowAddTelegramModal,
    showAddRSSModal,
    setShowAddRSSModal,
    tgInput,
    setTgInput,
    rssInput,
    setRssInput,
    tgSubmitting,
    rssSubmitting,
    tgError,
    rssError,
    addTelegramSource,
    addRSSSource,
    parseSourcesFromQuery,
  };
}

