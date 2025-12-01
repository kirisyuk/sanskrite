import React from 'react';
import { ThemeContext } from './ThemeProvider';

interface AuthLoadingProps {
  message?: string;
}

const START_MESSAGES = [
  'Thinking differently...',
  'Not agreeing. Thinking.',
  'Beyond yes or no',
  'This is HATL',
  'hatl.one',
  'Pushing boundaries',
  'That one',
  'Unusual chat',
  'Questions you haven\'t thought of.',
];

export const AuthLoading: React.FC<AuthLoadingProps> = ({ message = 'Signing you in…' }) => {
  const { theme } = React.useContext(ThemeContext);
  
  // Pick random message on mount
  const [randomMessage] = React.useState(() => {
    return START_MESSAGES[Math.floor(Math.random() * START_MESSAGES.length)];
  });
  
  // Render message with special formatting
  const renderMessage = () => {
    if (randomMessage === 'hatl.one') {
      return (
        <>
          hatl<span style={{ color: '#20b2aa' }}>.</span>one
        </>
      );
    }
    
    if (randomMessage === 'Unusual chat') {
      return (
        <>
          <span style={{ color: '#20b2aa' }}>Un</span>usual chat
        </>
      );
    }
    
    return randomMessage;
  };
  
  return (
    <div className="auth-container">
      <div className="flex flex-col items-center">
        <div className="font-medium mb-6" style={{ color: 'var(--text-heading)', fontSize: '3rem' }}>
          {renderMessage()}
        </div>
        <div className="w-48 h-1 rounded-full overflow-hidden mt-4" style={{ backgroundColor: 'var(--bg-elevated)' }}>
          <div className="h-full w-2/5 animate-pulse" style={{ backgroundColor: '#a8a8a8' }}></div>
        </div>
      </div>
    </div>
  );
};

