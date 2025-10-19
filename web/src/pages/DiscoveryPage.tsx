import React, { useState, useEffect, useRef } from 'react';
import ReactMarkdown, { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import Settings from '../components/Settings';
import { useProjectsStore } from '../stores/projectsStore';
import { useDiscoveryStore } from '../stores/discoveryStore';
import { useAuth } from '../contexts/AuthContext';
import { api } from '../lib/api';
import SourcePreview from '../components/SourcePreview';
import { useUIStore } from '../stores/uiStore';
import DiscoveryRequest from '../components/DiscoveryRequest';
import UnfoldRequest from '../components/UnfoldRequest';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';

// Component to analyze user's position based on their interactions
const UserPositionAnalysis: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const [position, setPosition] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
  const hasAssistant = React.useMemo(() =>
    genaiChat.messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0),
    [genaiChat.messages]
  );
  const notifiedRef = React.useRef(false);

  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      // Require authenticated user and at least first assistant answer
      if (!currentUser?.uid) return;
      if (!hasAssistant) return;
      setLoading(true);
      setError(null);
      try {
        const st = useGenaiChatStore.getState();
        // Get all user interactions
        const userInteractions = st.interactions.filter((it) => it.kind === 'user');
        if (userInteractions.length === 0) {
          setError('No user queries to analyze yet.');
          return;
        }
        // Build transcript of user messages
        const transcript = userInteractions.map((it) => 
          (it.summary && it.summary.trim()) ? it.summary : it.content
        ).join('\n');

        // Determine preferred language (fallback to EN)
        const lang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();

        const prompt = `You are analysing the user's stance from the queries below. Language: ${lang}. Address the end user as "you".

User queries:
${transcript}

Task: In 1–2 concise sentences (<=100 words), neutrally state what the user seems to be pursuing, their perspective and intent. Avoid moral judgement and any assumptions not evidenced by the queries.

Respond in plain text in ${lang}.`;

        const response = await api.genaiComplete({
          prompt,
          options: {
            modelOverride: 'gpt_quality',
            groundingEnabled: false,
            temperature: 0.3,
            maxOutputTokens: 1024,
          },
        });

        if (!cancelled) {
          const content = (response?.content || '').trim();
          if (content) {
            setPosition(content);
            setError(null);
            if (!notifiedRef.current) {
              notifiedRef.current = true;
              onFirstResult && onFirstResult();
            }
          } else {
            setError('The model returned an empty result.');
          }
        }
      } catch (err: any) {
        console.error('[UserPositionAnalysis] Error:', err);
        if (!cancelled) setError(err?.message || 'Failed to analyze position.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.messages.length, currentUser?.uid, hasAssistant]);

  // Do not render until first assistant answer is present
  if (!hasAssistant) return null;

  return (
    <div className="mt-3 p-3">
      <div className="text-13 mb-2" style={{ color: '#669999' }}>Your stance:</div>
      {error ? (
        <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{error}</div>
      ) : position ? (
        <div className="text-12 fade-in" style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.5' }}>
          {position}
        </div>
      ) : (
        <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No clear position identified.</div>
      )}
    </div>
  );
};

// Component to evaluate quality/style/biases of assistant answers and suggest system prompt improvements
const LLMEvaluation: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const [review, setReview] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
  const hasAssistant = React.useMemo(() =>
    genaiChat.messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0),
    [genaiChat.messages]
  );
  const lastAssistant = React.useMemo(() => {
    const msgs = genaiChat.messages.filter((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0);
    return msgs.length > 0 ? msgs[msgs.length - 1] : null;
  }, [genaiChat.messages]);
  const lastReviewedId = React.useRef<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      if (!currentUser?.uid) return;
      // Trigger only AFTER streaming completes and a new assistant message is available
      if (genaiChat.streaming) return;
      if (!lastAssistant) return;
      if (lastReviewedId.current === lastAssistant.id) return;
      setLoading(true);
      setError(null);
      try {
        // Prefer local interactions (assistant), fallback to chat messages
        const st = useGenaiChatStore.getState();
        const assistantInteractions = st.interactions.filter((it) => it.kind === 'assistant');
        let texts: string[] = [];
        if (assistantInteractions.length > 0) {
          const last = assistantInteractions.slice(Math.max(0, assistantInteractions.length - 5));
          texts = last.map((it) => (it.summary && it.summary.trim()) ? it.summary : it.content);
        } else {
          const all = genaiChat.messages.filter((m) => m.role === 'assistant' && m.content && m.content.trim());
          const last = all.slice(Math.max(0, all.length - 5));
          texts = last.map((m) => m.content);
        }
        if (texts.length === 0) {
          setError('No assistant replies to evaluate yet.');
          return;
        }
        const transcript = texts.join('\n\n---\n\n');
        const lang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
        const nowISO = new Date().toISOString();
        const prompt = `You are an expert LLM output reviewer. Language: ${lang}.
Current date/time (trustworthy, real-time): ${nowISO}.
The ASSISTANT model that produced these replies had access to up-to-date information via search grounding (web search).

Evaluate the following ASSISTANT responses (most recent last). Provide:
1) Brief quality assessment (strengths/weaknesses)
2) Style characteristics (tone, structure, clarity)
3) Potential biases/safety concerns
4) 3-5 concrete improvements to the SYSTEM INSTRUCTION to improve future answers

Important: Do not critique formatting or output length; formatting/verbosity are enforced elsewhere.

Assistant responses:
${transcript}

Respond concisely in the specified language (${lang}). Use short bullet points where appropriate. Return plain text only (no JSON/markdown). If unsure, write 'No issues found'.`;

        const response = await api.genaiComplete({
          prompt,
          options: {
            modelOverride: 'gpt_quality',
            groundingEnabled: false,
            temperature: 0.2,
            maxOutputTokens: 4096,
            systemInstruction: 'You are an expert LLM output reviewer. Always return a non-empty plain text answer in the specified language with short bullet points where appropriate.'
          },
        });
        if (!cancelled) {
          const content = (response?.content || '').trim();
          if (content) {
            setReview(content);
            setError(null);
            if (lastReviewedId.current !== lastAssistant?.id) {
              onFirstResult && onFirstResult();
            }
            lastReviewedId.current = lastAssistant?.id || null;
          } else {
            setError('The model returned an empty review.');
          }
        }
      } catch (err: any) {
        console.error('[LLMEvaluation] Error:', err);
        if (!cancelled) setError(err?.message || 'Failed to evaluate answers.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.streaming, lastAssistant?.id, currentUser?.uid]);

  if (!hasAssistant) return null;

  return (
    <div className="mt-3 p-3">
      <div className="text-13 mb-2" style={{ color: '#669999' }}>Assistant’s stance:</div>
      {error ? (
        <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{error}</div>
      ) : review ? (
        <div className="text-12 fade-in" style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.5' }}>
          {review}
        </div>
      ) : (
        <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No review yet.</div>
      )}
    </div>
  );
};

// Inline lightweight component to fetch and display title suggestions
const TitleSuggestions: React.FC = () => {
  const [titles, setTitles] = React.useState<string[] | null>(null);
  const [loading, setLoading] = React.useState(false);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
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
        if (!cancelled) setTitles(res);
      } catch {
        if (!cancelled) setTitles([]);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.messages.length, currentUser?.uid, hasAssistant]);

  // Do not render until first assistant answer is present
  if (!hasAssistant) return null;

  // During the very first request do not show the block until data arrives
  if (loading && (!titles || titles.length === 0)) return null;
  if (!titles || titles.length === 0) return null;
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
  const palette = isDark
    ? [
        '#F9A8D4', // pink-300 (used as border/text on dark)
        '#A5B4FC', // indigo-300
        '#86EFAC', // green-300
        '#FDA4AF', // rose-300
        '#FCD34D', // amber-300
        '#7DD3FC', // sky-300
        '#F0ABFC', // fuchsia-300
        '#C4B5FD', // violet-300
        '#94A3B8', // slate-400
        '#FDBA74', // orange-300
      ]
    : [
        '#FCE7F3', // pink-100
        '#E0E7FF', // indigo-100
        '#DCFCE7', // green-100
        '#FFE4E6', // rose-100
        '#FEF3C7', // amber-100
        '#E0F2FE', // sky-100
        '#FAE8FF', // fuchsia-100
        '#F3E8FF', // violet-100
        '#F1F5F9', // slate-100
        '#FFF7ED', // orange-100
      ];
  return (
    <div className="mt-3 p-3 fade-in">
      <div className="text-13 mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>How would you name this topic?</div>
      <div className="flex flex-wrap gap-1">
        {titles.slice(0, 5).map((t, i) => {
          const tone = palette[i % palette.length];
          const styleVars = isDark
            ? { ['--chip-color' as any]: tone }
            : { ['--chip-bg' as any]: tone };
          return (
            <span key={i} className="chip" style={styleVars as React.CSSProperties} title={t}>
              {t}
            </span>
          );
        })}
      </div>
    </div>
  );
};

const DiscoveryPage: React.FC = () => {
  const { currentUser } = useAuth();
  const { projects, currentProject, fetchProjects, deleteProject, setCurrentProject } = useProjectsStore();
  const { sources, loading, executeDiscovery, fetchProjectSources, fetchProjectInteractions, clearSources, connectWs, answer, correlationId, interactions, sendChatMessage } = useDiscoveryStore();
  const { viewMode } = useUIStore();
  
  const [queryText, setQueryText] = useState('');
  const [sideOpen, setSideOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [thinkingSeconds, setThinkingSeconds] = useState(0);
  const [genaiThinkingSeconds, setGenaiThinkingSeconds] = useState(0);
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const discoveryTimeoutRef = useRef<number | null>(null);
  const genaiThinkingIntervalRef = useRef<number | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [staticPage, setStaticPage] = useState<null | 'privacy' | 'terms'>(null);
  const correlationRef = useRef<string | null>(null);
  const wsInitialized = useRef(false);
  const [previewSource, setPreviewSource] = useState<any | null>(null);
  const [showAddSourceMenu, setShowAddSourceMenu] = useState(false);
  const addMenuRef = useRef<HTMLDivElement | null>(null);
  const [showAddTelegramModal, setShowAddTelegramModal] = useState(false);
  const [tgInput, setTgInput] = useState('');
  const [tgSubmitting, setTgSubmitting] = useState(false);
  const [tgError, setTgError] = useState<string | null>(null);
  // GenAI chat state (for Unfold view)
  const genaiChat = useGenaiChatStore();
  const genaiSettings = useGenAISettings();
  // Toggles for collapsible analysis sections
  const [showUserAnalysis, setShowUserAnalysis] = useState(false);
  const [showLLMReview, setShowLLMReview] = useState(false);
  const [badgeUser, setBadgeUser] = useState(false);
  const [badgeLLM, setBadgeLLM] = useState(false);
  const [availUserAnalysis, setAvailUserAnalysis] = useState(false);
  const [availLLMReview, setAvailLLMReview] = useState(false);
  const hasAssistant = React.useMemo(
    () => genaiChat.messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0),
    [genaiChat.messages]
  );

  useEffect(() => {
    fetchProjects('discovery');
  }, [fetchProjects]);

  // Background-load user preferred language and set app language (does not block UI)
  useEffect(() => {
    const loadLang = async () => {
      try {
        const uid = currentUser?.uid;
        if (!uid) return;
        const user = await api.getUser(uid);
        const code = (user?.preferredLanguage || localStorage.getItem('preferredLanguage') || 'EN').toUpperCase();
        if (code && code.length === 2) {
          localStorage.setItem('preferredLanguage', code);
          document.documentElement.setAttribute('lang', code.toLowerCase());
        }
      } catch {}
    };
    loadLang();
  }, [currentUser?.uid]);

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

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    }
  }, [interactions.length, running, genaiChat.messages]);

  // GenAI thinking seconds timer: runs until first chunk arrives
  useEffect(() => {
    const msgs = genaiChat.messages;
    const last = msgs[msgs.length - 1];
    const awaitingFirstChunk = !!(genaiChat.streaming && last && last.role === 'assistant' && !last.content);
    if (awaitingFirstChunk) {
      if (!genaiThinkingIntervalRef.current) {
        setGenaiThinkingSeconds(0);
        genaiThinkingIntervalRef.current = window.setInterval(() => {
          setGenaiThinkingSeconds((s) => s + 1);
        }, 1000) as unknown as number;
      }
    } else if (genaiThinkingIntervalRef.current) {
      window.clearInterval(genaiThinkingIntervalRef.current);
      genaiThinkingIntervalRef.current = null;
    }
  }, [genaiChat.streaming, genaiChat.messages.length]);

  // Cleanup genai thinking timer on unmount
  useEffect(() => {
    return () => {
      if (genaiThinkingIntervalRef.current) {
        window.clearInterval(genaiThinkingIntervalRef.current);
        genaiThinkingIntervalRef.current = null;
      }
    };
  }, []);

  // Close add source menu on outside click or Escape
  useEffect(() => {
    function onDocMouseDown(e: MouseEvent) {
      if (!showAddSourceMenu) return;
      if (addMenuRef.current && !addMenuRef.current.contains(e.target as Node)) {
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

  const getPreferredLanguage = () => {
    const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
    return preferred && preferred.length === 2 ? preferred : 'en';
  };

  const normalizeTelegramInput = (value: string): string | null => {
    const v = (value || '').trim();
    if (!v) return null;
    const linkMatch = v.match(/(?:https?:\/\/)?(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/i);
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
    const pending = { id: handle, kind: 'telegram' as const, status: 'fetching' };
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
    } catch (e: any) {
      setTgError(e?.message || 'Failed to add channel');
    } finally {
      setTgSubmitting(false);
    }
  };

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
      const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
      const lang = preferred && preferred.length === 2 ? preferred : 'en';
      const result = await executeDiscovery({
        projectId,
        queryText,
        language: lang,
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

      // Keep running=true until first answer arrives via WS or timeout fires
      if (discoveryTimeoutRef.current) {
        window.clearTimeout(discoveryTimeoutRef.current);
      }
      discoveryTimeoutRef.current = window.setTimeout(() => {
        console.warn('[Discovery] Timeout waiting for first answer');
        setRunning(false);
        discoveryTimeoutRef.current = null;
      }, 90000) as unknown as number;
    } catch (error) {
      console.error('[Discovery] Failed to execute:', error);
      setRunning(false);
    }
  };

  // Stop running when the first answer arrives (from WS store)
  useEffect(() => {
    if (answer && running) {
      if (discoveryTimeoutRef.current) {
        window.clearTimeout(discoveryTimeoutRef.current);
        discoveryTimeoutRef.current = null;
      }
      setRunning(false);
    }
  }, [answer, running]);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (discoveryTimeoutRef.current) {
        window.clearTimeout(discoveryTimeoutRef.current);
        discoveryTimeoutRef.current = null;
      }
    };
  }, []);

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
        className="text-xs rounded-2xl cursor-pointer"
        style={{
          backgroundColor: 'var(--bg-sidebar)',
          border: '1px solid var(--overlay-line-90)',
        }}
        role="button"
        onClick={() => setPreviewSource(source)}
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
          ) : !isFetched && (source as any).__firstProfile && (source as any).__hint === 'profilingNewSource' ? (
            <span style={{ color: 'var(--text-secondary-strong)' }}>
              Our system is establishing the channel’s analytical profile based on its content.
            </span>
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

  React.useEffect(() => {
    const handler = (e: any) => {
      const kind = e?.detail?.kind as 'privacy' | 'terms';
      if (kind === 'privacy' || kind === 'terms') {
        setShowSettings(false);
        setStaticPage(kind);
      }
    };
    window.addEventListener('ui.openPage', handler as any);
    return () => window.removeEventListener('ui.openPage', handler as any);
  }, []);

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
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}
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
                  // Reset Unfold chat state and analysis artifacts
                  try {
                    useGenaiChatStore.getState().reset();
                  } catch {}
                  setShowUserAnalysis(false);
                  setShowLLMReview(false);
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
          className="flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center ml-2 mr-2 transition-all duration-300"
          style={{ backgroundColor: '#669999', color: 'white', alignSelf: 'flex-start', marginTop: '20px' }}
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M9 5l7 7-7 7" />
          </svg>
        </button>
      )}

      {/* Floating Settings button (visible only when sidebar is hidden) */}
      {!sideOpen && !showSettings && (
        <button
          type="button"
          aria-label="Settings"
          onClick={() => setShowSettings(true)}
          className="rounded-lg transition-colors flex items-center justify-center"
          style={{
            position: 'fixed',
            left: 8,
            bottom: 48,
            width: 32,
            height: 32,
            backgroundColor: 'transparent',
            color: 'var(--text-heading)',
            border: 'none',
            zIndex: 40,
          }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <circle cx="12" cy="12" r="4.5" strokeWidth={2} />
            <path strokeLinecap="round" strokeWidth={2} d="M12 3v2M12 19v2M3 12h2M19 12h2M5.636 5.636l1.414 1.414M16.95 7.05l1.414-1.414M18.364 18.364l-1.414-1.414M7.05 16.95l-1.414 1.414" />
          </svg>
        </button>
      )}

      {/* Grid wrapper to the right of the sidebar: Sources | Center | Right */}
      <div
        className="flex-1 min-h-0"
        style={{
          position: 'relative',
          display: 'grid',
          gridTemplateColumns: `minmax(320px, 25%) var(--center-col-width, minmax(640px, 1fr)) minmax(320px, 25%)`,
          gap: '16px',
        }}
      >
        {/* Sources column (scrollable) */}
        <div className="min-h-0 h-full overflow-y-auto" style={{ background: 'transparent' }}>
          {sources.length > 0 && (
            <div className="space-y-2">
              {sources.map((source) => renderSourceCard(source))}
            </div>
          )}

          {/* Suggested chat titles (after 2+ messages in Unfold chat) */}
          <TitleSuggestions />

          {/* Analysis toggles under suggestions, left-aligned */}
          {hasAssistant && (
            <div className="px-3 mt-1 flex items-center gap-2">
              {availUserAnalysis && (
                <button
                  type="button"
                  aria-label="Toggle user position analysis"
                  title="Show user's position analysis"
                  onClick={() => { setShowUserAnalysis((v) => !v); setBadgeUser(false); }}
                  className="relative rounded-lg transition-colors flex items-center justify-center"
                  style={{ width: 32, height: 32, backgroundColor: showUserAnalysis ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = showUserAnalysis ? 'var(--white-20)' : 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = showUserAnalysis ? 'var(--white-10)' : 'transparent'; }}
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <circle cx="12" cy="8" r="3.5" strokeWidth={2} />
                    <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth={2} strokeLinecap="round" />
                  </svg>
                  {badgeUser && (
                    <span style={{ position: 'absolute', top: -4, right: -4, width: 8, height: 8, backgroundColor: '#669999', borderRadius: 9999 }} />
                  )}
                </button>
              )}
              {availLLMReview && (
                <button
                  type="button"
                  aria-label="Toggle LLM answer review"
                  title="Show LLM answer review"
                  onClick={() => { setShowLLMReview((v) => !v); setBadgeLLM(false); }}
                  className="relative rounded-lg transition-colors flex items-center justify-center"
                  style={{ width: 32, height: 32, backgroundColor: showLLMReview ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = showLLMReview ? 'var(--white-20)' : 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = showLLMReview ? 'var(--white-10)' : 'transparent'; }}
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path d="M9 6a3 3 0 00-6 0 3 3 0 003 3" strokeWidth={2} strokeLinecap="round" />
                    <path d="M15 6a3 3 0 116 0 3 3 0 01-3 3" strokeWidth={2} strokeLinecap="round" />
                    <path d="M12 7c-1.657 0-3 1.343-3 3v7a2 2 0 002 2h1" strokeWidth={2} strokeLinecap="round" />
                    <path d="M12 7c1.657 0 3 1.343 3 3v7a2 2 0 01-2 2h-1" strokeWidth={2} strokeLinecap="round" />
                  </svg>
                  {badgeLLM && (
                    <span style={{ position: 'absolute', top: -4, right: -4, width: 8, height: 8, backgroundColor: '#669999', borderRadius: 9999 }} />
                  )}
                </button>
              )}
            </div>
          )}

          {/* Collapsible analysis sections (hidden, not unmounted) */}
          {hasAssistant && (
          <div style={{ display: showUserAnalysis ? 'block' : 'none' }} aria-hidden={!showUserAnalysis}>
            <UserPositionAnalysis onFirstResult={() => { setBadgeUser(true); setAvailUserAnalysis(true); }} />
          </div>
          )}
          {hasAssistant && (
          <div style={{ display: showLLMReview ? 'block' : 'none' }} aria-hidden={!showLLMReview}>
            <LLMEvaluation onFirstResult={() => { setBadgeLLM(true); setAvailLLMReview(true); }} />
          </div>
          )}

          {/* Add source button (only when chat exists and has sources) */}
          {currentProject?.id && sources.length > 0 && (
            <div className="mt-2" ref={addMenuRef}>
              <button
                type="button"
                title="Add source"
                aria-haspopup="true"
                aria-expanded={showAddSourceMenu}
                onClick={() => setShowAddSourceMenu((v) => !v)}
                className="rounded-lg transition-colors flex items-center justify-center"
                style={{
                  width: '32px',
                  height: '32px',
                  backgroundColor: 'transparent',
                  color: 'var(--text-heading)',
                  border: '1px solid var(--overlay-line-90)'
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path strokeLinecap="round" strokeWidth={2} d="M12 6v12M6 12h12" />
                </svg>
              </button>

              {showAddSourceMenu && (
                <div
                  role="menu"
                  aria-label="Add source options"
                  className="mt-2 rounded-xl shadow-lg"
                  style={{
                    backgroundColor: 'var(--bg-elevated)',
                    border: '1px solid var(--overlay-line-90)'
                  }}
                >
                  {/* Telegram Channel */}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => { setShowAddSourceMenu(false); setShowAddTelegramModal(true); }}
                    className="w-full flex items-center gap-2 px-3 py-2 rounded-t-xl text-left transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 11l18-7-7 18-4-8-7-3z" />
                    </svg>
                    <span className="text-13">Telegram Channel</span>
                  </button>

                  {/* Web Page */}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => setShowAddSourceMenu(false)}
                    className="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <circle cx="12" cy="12" r="9" strokeWidth={2} />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 12h18M12 3a15 15 0 010 18M12 3a15 15 0 000 18" />
                    </svg>
                    <span className="text-13">Web Page</span>
                  </button>

                  {/* RSS */}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => setShowAddSourceMenu(false)}
                    className="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 5c7.18 0 13 5.82 13 13" />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 11a7 7 0 017 7" />
                      <circle cx="6" cy="18" r="1.5" fill="currentColor" />
                    </svg>
                    <span className="text-13">RSS</span>
                  </button>

                  {/* Document (disabled) */}
                  <div
                    aria-disabled
                    className="w-full flex items-center gap-2 px-3 py-2 rounded-b-xl text-left"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', opacity: 0.5, cursor: 'not-allowed' }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 3h7l4 4v14a2 2 0 01-2 2H7a2 2 0 01-2-2V5a2 2 0 012-2z" />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14 3v4h4" />
                    </svg>
                    <span className="text-13">Document</span>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Main center column: renders discussion or graph placeholder (spans center+right when graph) */}
        <div
          className="min-h-0 flex flex-col"
          style={{ position: 'relative', gridColumn: viewMode === 'graph' ? '2 / 4' : '2 / 3' }}
        >
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
        ) : staticPage ? (
          <div className="w-full px-4 mt-2 mb-2">
            <div className="mb-2 flex items-center">
              <button
                onClick={() => setStaticPage(null)}
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
              <div className="ml-2 text-13" style={{ color: 'var(--text-heading)' }}>{staticPage === 'privacy' ? 'Privacy Notice' : 'Terms of Usage'}</div>
            </div>
            <div className="text-12" style={{ color: 'var(--text-secondary-strong)', lineHeight: 1.6 }}>
              {staticPage === 'privacy' ? (
                <div>
                  <p>We respect your privacy. We only process the minimum data required to provide the service. Authentication is handled by Firebase; chat content is stored in your tenant for product features only.</p>
                  <p className="mt-2">We do not sell your data. For deletion/export requests, contact support.</p>
                </div>
              ) : (
                <div>
                  <p>By using this application you agree to act lawfully and not to abuse the system. The service is provided “as is”, without warranties. You are responsible for reviewing outputs and decisions.</p>
                  <p className="mt-2">Some features rely on third‑party AI providers; their terms may also apply.</p>
                </div>
              )}
            </div>
          </div>
        ) : (
        <>
        {/* Sources moved to the left grid column */}

        {/* Graph placeholder when in graph mode */}
        {viewMode === 'graph' ? (
          <div className="w-full h-full flex items-center justify-center" style={{ background: 'var(--bg-elevated)', border: '1px dashed var(--overlay-line-90)', borderRadius: '12px' }}>
            <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>Graph will appear here</div>
          </div>
        ) : viewMode === 'unfold' ? (
          <div className="w-full px-4 flex-1 flex flex-col min-h-0 pb-2" style={{ position: 'relative' }}>
            {/* GenAI chat history */}
            <div ref={messagesRef} className="flex-1 overflow-y-auto px-1 pb-2 min-h-[160px]">
              <div className="space-y-2">
                {genaiChat.messages.map((m) => {
                  const isUser = m.role === 'user';
                  const containerStyle: React.CSSProperties = {
                    backgroundColor: isUser ? 'var(--toggle-15)' : 'transparent',
                    border: isUser ? '1px solid var(--toggle-25)' : 'none',
                    maxWidth: isUser ? '75%' : '100%',
                    width: isUser ? 'fit-content' : '100%',
                    paddingLeft: 14,
                    paddingRight: 14,
                  };
                  const isAssistantPlaceholder = !isUser && genaiChat.streaming && !m.content;
                  const isAssistantError = !isUser && !genaiChat.streaming && !m.content && !!genaiChat.error;
                  if (isAssistantError) {
                    (containerStyle as any).backgroundColor = 'var(--danger-10)';
                    (containerStyle as any).border = '1px solid var(--danger-30)';
                  }
                  return (
                    <div key={m.id} className={`rounded-2xl px-4 py-3 ${isUser ? 'ml-auto' : ''}`} style={containerStyle}>
                      <div className="text-[10px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
                        {isUser ? 'you' : (isAssistantPlaceholder ? `assistant (thinking ${genaiThinkingSeconds} sec)` : 'assistant')}
                      </div>
                      {isAssistantPlaceholder ? (
                        <div className="space-y-1">
                          <div style={{ width: 160, height: 10, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                            <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                          </div>
                          <div style={{ width: 220, height: 10, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                            <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                          </div>
                        </div>
                      ) : isAssistantError ? (
                        <div className="text-13" style={{ color: 'var(--text-heading)' }}>
                          К сожалению, не удалось обработать запрос. Мы зафиксировали техническую неполадку и уже
                          анализируем ситуацию. Пожалуйста, повторите попытку чуть позже.
                          <div className="mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                            Техническая справка: {genaiChat.error}
                          </div>
                        </div>
                      ) : (
                        <>
                          <div className="text-13 markdown" style={{ color: 'var(--text-heading)' }}>
                            <ReactMarkdown
                              remarkPlugins={[remarkGfm]}
                              components={{
                                p: (props: React.DetailedHTMLProps<React.HTMLAttributes<HTMLParagraphElement>, HTMLParagraphElement>) => (
                                  <div {...props} className="md-p" />
                                ),
                                a: (props: React.DetailedHTMLProps<React.AnchorHTMLAttributes<HTMLAnchorElement>, HTMLAnchorElement>) => (
                                  <a {...props} target="_blank" rel="noreferrer" className="underline" />
                                ),
                                code: (
                                  props: {
                                    inline?: boolean;
                                    className?: string;
                                    children?: React.ReactNode;
                                  } & React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement>
                                ) => {
                                  const { inline, className, children, ...rest } = props;
                                  if (inline) {
                                    return (
                                      <code {...rest} style={{ backgroundColor: 'var(--white-10)', padding: '0 3px', borderRadius: 4 }}>
                                        {children}
                                      </code>
                                    );
                                  }
                                  return (
                                    <pre style={{ backgroundColor: 'var(--bg-elevated)', padding: 8, borderRadius: 8, overflowX: 'auto', border: '1px solid var(--overlay-line-90)' }}>
                                      <code {...rest} className={className}>{children}</code>
                                    </pre>
                                  );
                                },
                                ul: (props: React.DetailedHTMLProps<React.HTMLAttributes<HTMLUListElement>, HTMLUListElement>) => (
                                  <ul {...props} style={{ paddingLeft: '1.25rem' }} />
                                ),
                                ol: (props: React.DetailedHTMLProps<React.OlHTMLAttributes<HTMLOListElement>, HTMLOListElement>) => (
                                  <ol {...props} style={{ paddingLeft: '1.25rem' }} />
                                ),
                              } as Components}
                            >
                              {m.content}
                            </ReactMarkdown>
                          </div>
                          {!isUser && !genaiChat.streaming && !!m.content && (
                            <div className="mt-1 flex justify-end">
                              <button
                                type="button"
                                aria-label="Copy assistant answer"
                                title="Copy"
                                onClick={() => {
                                  try { navigator.clipboard.writeText(m.content); } catch {}
                                }}
                                className="rounded-lg transition-colors flex items-center justify-center"
                                style={{ width: 28, height: 28, backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                              >
                                {/* Copy outline icon */}
                                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                  <rect x="9" y="9" width="11" height="11" rx="2" ry="2" strokeWidth={2} />
                                  <path d="M6 15H5a2 2 0 01-2-2V5a2 2 0 012-2h8a2 2 0 012 2v1" strokeWidth={2} strokeLinecap="round" />
                                </svg>
                              </button>
                            </div>
                          )}
                        </>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
            {/* Tool calls / results */}
            {(genaiChat.toolCalls.length > 0 || genaiChat.toolResults.length > 0) && (
              <div className="px-1 pb-2">
                {genaiChat.toolCalls.length > 0 && (
                  <div className="text-[11px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                    Tool calls:
                    <ul className="list-disc ml-4">
                      {genaiChat.toolCalls.map((c, i) => (
                        <li key={`tc-${i}`}>{c.name}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {genaiChat.toolResults.length > 0 && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    Tool results:
                    <ul className="list-disc ml-4">
                      {genaiChat.toolResults.map((c, i) => (
                        <li key={`tr-${i}`}><span className="font-mono">{c.name}</span></li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}
            {/* Thinking indicator moved inside assistant bubble during streaming */}
            {/* Usage and attributions after completion */}
            {!genaiChat.streaming && (genaiChat.usage || genaiChat.attributions.length > 0) && (
              <div className="px-1 pb-2 space-y-1">
                {genaiChat.usage && (
                  <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                    Tokens: prompt {genaiChat.usage.promptTokens}, output {genaiChat.usage.outputTokens}, total {genaiChat.usage.totalTokens}
                  </div>
                )}
                {genaiChat.attributions.length > 0 && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    Sources:
                    <ul className="list-disc ml-4">
                      {genaiChat.attributions.map((a, i) => (
                        <li key={`att-${i}`}><a href={a.url} target="_blank" rel="noreferrer" className="underline">{a.title || a.url}</a></li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}
            {/* Input */}
            <UnfoldRequest
              value={queryText}
              onChange={setQueryText}
              onSubmit={async () => {
                if (genaiChat.streaming || !queryText.trim()) return;
                const tools = genaiSettings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                // Map UI model to backend aliases
                const model = genaiSettings.model;
                const options: any = {
                  // profile is used only for gemini fallback; keep for compatibility
                  profile: model === 'gemini_quality' ? 'quality' : 'fast',
                  modelOverride: model,
                  groundingEnabled: genaiSettings.groundingEnabled,
                  temperature: genaiSettings.temperature,
                  maxOutputTokens: genaiSettings.maxOutputTokens,
                  systemInstruction: genaiSettings.systemInstruction || undefined,
                };
                const prompt = queryText;
                setQueryText('');
                await genaiChat.startStream({ prompt, tools, options });
              }}
              disabled={genaiChat.streaming || !queryText.trim()}
            />
            {/* Resizer handle at right edge of center column */}
            <div
              className="chat-resizer"
              style={{ right: -4 }}
              onMouseDown={(e) => {
                const root = document.documentElement;
                const startX = e.clientX;
                const startWidth = (messagesRef.current?.parentElement?.clientWidth || 0);
                const initial = getComputedStyle(root).getPropertyValue('--center-col-width');
                const onMove = (ev: MouseEvent) => {
                  const delta = ev.clientX - startX;
                  const newWidth = Math.max(480, startWidth + delta);
                  root.style.setProperty('--center-col-width', `${newWidth}px`);
                };
                const onUp = () => {
                  window.removeEventListener('mousemove', onMove);
                  window.removeEventListener('mouseup', onUp);
                };
                window.addEventListener('mousemove', onMove);
                window.addEventListener('mouseup', onUp);
              }}
            />
          </div>
        ) : (
        /* Interactions + input container with internal scroll */
        <div className="w-full px-4 flex-1 flex flex-col min-h-0 pb-2">
          {/* Interactions history (LLM answers etc.) */}
          <div ref={messagesRef} className="flex-1 overflow-y-auto px-1 pb-2 min-h-[160px]">
            <div className="space-y-2">
              {interactions
                .filter((it) => it.kind === 'discovery.query' || it.kind === 'discovery.answer')
                .map((it) => {
                  const isUser = it.kind === 'discovery.query';
                  const containerStyle: React.CSSProperties = {
                    backgroundColor: isUser ? 'var(--brand-10)' : 'transparent',
                    border: isUser ? '1px solid var(--brand-25)' : 'none',
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
              })
            }
            </div>
          </div>

          {/* Status indicator */}
          {running && (
            <div className="px-1 pb-2">
              <div className="text-xs flex items-center gap-2" style={{ textAlign: 'left', color: 'var(--text-secondary-strong)' }}>
                <span className="animate-pulse" style={{ width: '8px', height: '8px', borderRadius: '9999px', backgroundColor: 'var(--text-secondary-strong)' }} />
                <span>Thinking... {thinkingSeconds}s</span>
              </div>
            </div>
          )}

          {/* Input box */}
          <DiscoveryRequest
            value={queryText}
            onChange={setQueryText}
            onSubmit={async () => {
              if (running || !queryText.trim()) return;
              if (currentProject?.id) {
                const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
                const lang = preferred && preferred.length === 2 ? preferred : 'en';
                setRunning(true);
                try {
                  await sendChatMessage(currentProject.id, queryText, lang);
                  setQueryText('');
                } finally {
                  setRunning(false);
                }
              } else {
                handleExecuteDiscovery();
              }
            }}
            disabled={running || !queryText.trim()}
          />
        </div>
        )}
        {previewSource && (
          <SourcePreview
            source={previewSource}
            onClose={() => setPreviewSource(null)}
            style={{ top: 0, left: '12px', width: 760, maxWidth: 760 }}
          />
        )}

        {/* Add Telegram modal */}
        {showAddTelegramModal && (
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Add Telegram Channel"
            className="fixed inset-0 flex items-center justify-center"
            style={{ zIndex: 50 }}
          >
            <div
              className="absolute inset-0"
              style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}
              onClick={() => !tgSubmitting && setShowAddTelegramModal(false)}
            />
            <div
              className="relative rounded-xl shadow-xl w-full max-w-sm"
              style={{ backgroundColor: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}
            >
              <div className="px-4 py-3" style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
                <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Add Telegram Channel</div>
              </div>
              <div className="p-4 space-y-3">
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                  Enter @username or a link like t.me/username
                </div>
                <div className="flex items-center gap-2">
                  <div
                    className="flex items-center justify-center rounded-lg"
                    style={{ width: 32, height: 32, background: 'linear-gradient(135deg, #2AABEE 0%, #229ED9 100%)', color: 'white' }}
                  >
                    <svg viewBox="0 0 24 24" width={16} height={16} fill="none" stroke="currentColor" strokeWidth={2}>
                      <path d="M3 11l18-7-7 18-4-8-7-3z" stroke="currentColor" />
                    </svg>
                  </div>
                  <input
                    type="text"
                    value={tgInput}
                    onChange={(e) => setTgInput(e.target.value)}
                    placeholder="@channel or t.me/channel"
                    className="flex-1 bg-transparent outline-none text-sm"
                    style={{ color: 'var(--text-heading)' }}
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addTelegramSource(); } }}
                    autoFocus
                  />
                </div>
                {tgError && (
                  <div className="text-xs" style={{ color: 'var(--accent-danger)' }}>{tgError}</div>
                )}
              </div>
              <div className="px-4 py-3 flex items-center justify-end gap-2" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
                <button
                  type="button"
                  className="px-3 py-1.5 rounded-lg text-xs transition-colors"
                  onClick={() => setShowAddTelegramModal(false)}
                  disabled={tgSubmitting}
                  style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="px-3 py-1.5 rounded-lg text-xs"
                  onClick={addTelegramSource}
                  disabled={tgSubmitting}
                  style={{ backgroundColor: '#2AABEE', color: 'white', opacity: tgSubmitting ? 0.7 : 1 }}
                >
                  {tgSubmitting ? 'Adding…' : 'Add' }
                </button>
              </div>
            </div>
          </div>
        )}
        </>
        )}
        </div>

        {/* Right column (buffer) - hidden when graph spans it */}
        {viewMode !== 'graph' && <div className="min-h-0" />}
      </div>
    </div>
  );
};

export default DiscoveryPage;
