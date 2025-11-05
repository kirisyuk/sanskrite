import React from 'react';
import { useLocation } from 'react-router-dom';
import { ThemeContext } from './ThemeProvider';
import { useAuth } from '../contexts/AuthContext';
import logoDarkTheme from '../assets/hatl-dark-theme.png';
import logoLightTheme from '../assets/hatl-light-theme.png';
import { UserProfile } from './UserProfile';
import { useTranslation } from 'react-i18next';
import { useUIStore } from '../stores/uiStore';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useProjectsStore } from '../stores/projectsStore';
import { api } from '../lib/api';

export const TopBar: React.FC<{ showUser?: boolean }> = ({ showUser = true }) => {
  const { theme, setTheme } = React.useContext(ThemeContext);
  const { currentUser } = useAuth();
  const location = useLocation();
  const { i18n } = useTranslation();
  const uiLanguage = useUIStore((s) => s.uiLanguage);
  const setUiLanguage = useUIStore((s) => s.setUiLanguage);
  // PERFORMANCE FIX: Subscribe only to specific values, not entire store
  const genaiChatProjectId = useGenaiChatStore((s) => s.projectId);
  const genaiChatInteractionsLength = useGenaiChatStore((s) => s.interactions.length);
  const genaiChatMessagesLength = useGenaiChatStore((s) => s.messages.length);
  const { currentProject } = useProjectsStore();
  const [shareBusy, setShareBusy] = React.useState(false);
  const [shareOpen, setShareOpen] = React.useState(false);
  const [shareUrl, setShareUrl] = React.useState<string | null>(null);
  const [copied, setCopied] = React.useState(false);
  const shareRef = React.useRef<HTMLDivElement | null>(null);
  const isChat = React.useMemo(() => {
    try {
      const params = new URLSearchParams(location.search);
      const isUnfold = params.get('app') === 'unfold';
      return location.pathname === '/' && (isUnfold || !!currentUser);
    } catch {
      return !!currentUser;
    }
  }, [location.pathname, location.search, currentUser]);

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
  const canShare = React.useMemo(() => {
    try {
      if (!currentUser) return false;
      // Use currentProject?.id if available, otherwise fallback to genaiChatProjectId
      const projectId = currentProject?.id || genaiChatProjectId;
      if (!projectId || projectId === 'local-unfold') return false;
      return genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0;
    } catch { return false; }
  }, [currentProject?.id, genaiChatProjectId, genaiChatInteractionsLength, genaiChatMessagesLength, currentUser]);

  const createShare = React.useCallback(async () => {
    try {
      if (shareBusy) return;
      setShareBusy(true);
      // Use currentProject?.id if available (most reliable), otherwise fallback to genaiChatProjectId
      const pid = (currentProject?.id || genaiChatProjectId) as string;
      if (!pid || pid === 'local-unfold') { setShareBusy(false); return; }
      // default: 7 days from now
      const exp = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString();
      const res = await api.shareCreate(pid, exp);
      const rel = res?.shareToken ? `/shared/${res.shareToken}` : null;
      setShareUrl(rel);
      setShareOpen(true);
    } catch {
      setShareUrl(null);
      setShareOpen(true);
    } finally {
      setShareBusy(false);
    }
  }, [currentProject?.id, genaiChatProjectId, shareBusy]);

  React.useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (!shareOpen) return;
      const el = shareRef.current;
      if (el && !el.contains(e.target as Node)) setShareOpen(false);
    };
    document.addEventListener('mousedown', onDocClick, { capture: true });
    return () => document.removeEventListener('mousedown', onDocClick, { capture: true } as any);
  }, [shareOpen]);

  return (
    <div className="topbar">
      {/* Left side - Logo (aligned to sidebar toggle spacing) */}
      <div className="flex items-center gap-3 px-3">
        <a href="/" aria-label="Home" className="flex items-center" style={{ textDecoration: 'none' }}>
          <img
            src={theme === 'dark' ? logoDarkTheme : logoLightTheme}
            alt="Sanskrite"
            className="h-[12px] w-auto object-contain"
            style={{
              filter: theme === 'dark' ? 'drop-shadow(0 0 2px rgba(0,0,0,0.6))' : 'none',
              opacity: 0.7,
            }}
          />
        </a>
        {/* Link to Unfold chat when logged in */}
        {currentUser && !isChat && (
          <a
            href="/?app=unfold"
            className="text-12 px-2 py-1 rounded"
            style={{ color: 'var(--text-heading)', textDecoration: 'none' }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLAnchorElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLAnchorElement).style.backgroundColor = 'transparent'; }}
          >
            Unfold
          </a>
        )}
      </div>

      {/* Center - View mode toggle temporarily disabled */}
      <div />

      {/* Right - Share, Language switcher, Theme toggle and User profile */}
      <div className="flex items-center gap-3 px-2">
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
          className="text-12 px-2 py-1 rounded"
          value={selectedUi}
          onChange={handleLanguageChange}
          style={{ backgroundColor: 'transparent', color: 'var(--text-heading)', border: '1px solid var(--white-10)' }}
       >
          <option value="en">EN</option>
          <option value="de">DE</option>
          <option value="es">ES</option>
          <option value="ru">RU</option>
          <option value="kz">KZ</option>
        </select>
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

