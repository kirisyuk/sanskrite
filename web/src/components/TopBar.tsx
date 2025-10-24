import React from 'react';
import { useLocation } from 'react-router-dom';
import { ThemeContext } from './ThemeProvider';
import { useAuth } from '../contexts/AuthContext';
import logoDarkTheme from '../assets/hatl-dark-theme.png';
import logoLightTheme from '../assets/hatl-light-theme.png';
import { UserProfile } from './UserProfile';

export const TopBar: React.FC<{ showUser?: boolean }> = ({ showUser = true }) => {
  const { theme, setTheme } = React.useContext(ThemeContext);
  const { currentUser } = useAuth();
  const location = useLocation();
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
        {showUser && currentUser && <UserProfile />}
      </div>
    </div>
  );
};

