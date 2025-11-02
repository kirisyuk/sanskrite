import React from 'react';
import { ThemeContext } from './ThemeProvider';
import lightLogo from '../assets/hatl-light-theme.png';
import darkLogo from '../assets/hatl-dark-theme.png';

interface AuthLoadingProps {
  message?: string;
}

export const AuthLoading: React.FC<AuthLoadingProps> = ({ message = 'Signing you in…' }) => {
  const { theme } = React.useContext(ThemeContext);
  
  return (
    <div className="auth-container" style={{ backgroundColor: theme === 'dark' ? 'var(--bg-base)' : 'var(--bg-base)' }}>
      <div className="flex flex-col items-center">
        <img src={theme === 'dark' ? darkLogo : lightLogo} alt="HATL" className="h-8 w-auto mb-4" />
        <div className="text-sm mb-4" style={{ color: 'var(--text-secondary)' }}>{message}</div>
        <div className="w-48 h-1 rounded-full overflow-hidden" style={{ backgroundColor: 'var(--bg-elevated)' }}>
          <div className="h-full w-2/5 animate-pulse" style={{ backgroundColor: 'var(--accent-brand)' }}></div>
        </div>
      </div>
    </div>
  );
};

