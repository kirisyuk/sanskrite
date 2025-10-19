import React from 'react';
import { ThemeContext } from './ThemeProvider';
import { useAuth } from '../contexts/AuthContext';
import logoDarkTheme from '../assets/hatl-dark-theme.png';
import logoLightTheme from '../assets/hatl-light-theme.png';
import { UserProfile } from './UserProfile';
import { useUIStore } from '../stores/uiStore';

export const TopBar: React.FC = () => {
  const { theme, setTheme } = React.useContext(ThemeContext);
  const { currentUser } = useAuth();
  const { viewMode, setViewMode } = useUIStore();

  const toggleTheme = React.useCallback(() => {
    const next = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
  }, [theme, setTheme]);

  return (
    <div className="topbar">
      {/* Left side - Logo */}
      <div className="flex items-center gap-3 px-6">
        <img
          src={theme === 'dark' ? logoDarkTheme : logoLightTheme}
          alt="Sanskrite"
          className="h-[12px] w-auto object-contain"
          style={{
            filter: theme === 'dark' ? 'drop-shadow(0 0 2px rgba(0,0,0,0.6))' : 'none',
            opacity: 0.7,
          }}
        />
      </div>

      {/* Center - View mode toggle */}
      <div className="flex items-center">
        <div
          className="flex rounded-lg overflow-hidden"
          style={{ border: '1px solid var(--overlay-line-90)' }}
          role="tablist"
          aria-label="View mode"
        >
          <button
            role="tab"
            aria-selected={viewMode === 'unfold'}
            aria-pressed={viewMode === 'unfold'}
            className="px-3 py-1.5 text-13 font-medium"
            style={{
              backgroundColor: viewMode === 'unfold' ? 'var(--white-10)' : 'transparent',
              color: 'var(--text-heading)'
            }}
            onClick={() => setViewMode('unfold')}
            onMouseEnter={(e) => { if (viewMode !== 'unfold') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { if (viewMode !== 'unfold') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
          >
            Unfold
          </button>
          <div style={{ width: '1px', background: 'var(--overlay-line-90)' }} />
          <button
            role="tab"
            aria-selected={viewMode === 'discussion'}
            aria-pressed={viewMode === 'discussion'}
            className="px-3 py-1.5 text-13 font-medium"
            style={{
              backgroundColor: viewMode === 'discussion' ? 'var(--white-10)' : 'transparent',
              color: 'var(--text-heading)'
            }}
            onClick={() => setViewMode('discussion')}
            onMouseEnter={(e) => { if (viewMode !== 'discussion') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { if (viewMode !== 'discussion') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
          >
            As Discussion
          </button>
          <div style={{ width: '1px', background: 'var(--overlay-line-90)' }} />
          <button
            role="tab"
            aria-selected={viewMode === 'graph'}
            aria-pressed={viewMode === 'graph'}
            className="px-3 py-1.5 text-13 font-medium"
            style={{
              backgroundColor: viewMode === 'graph' ? 'var(--white-10)' : 'transparent',
              color: 'var(--text-heading)',
              
            }}
            onClick={() => setViewMode('graph')}
            onMouseEnter={(e) => { if (viewMode !== 'graph') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { if (viewMode !== 'graph') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
          >
            As Graph
          </button>
        </div>
      </div>

      {/* Right - Theme toggle and User profile */}
      <div className="flex items-center gap-3 px-2">
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
        {currentUser && <UserProfile />}
      </div>
    </div>
  );
};

