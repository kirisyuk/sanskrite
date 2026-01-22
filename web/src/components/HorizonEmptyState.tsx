import React from 'react';
import { ThemeContext } from './ThemeProvider';

interface HorizonEmptyStateProps {
  onCreateProject: () => void;
}

export const HorizonEmptyState: React.FC<HorizonEmptyStateProps> = ({ onCreateProject }) => {
  const { theme } = React.useContext(ThemeContext);
  
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '2rem',
      }}
    >
      <div style={{ textAlign: 'center', maxWidth: 480 }}>
        <div
          style={{
            fontSize: 'clamp(32px, 5vw, 48px)',
            fontWeight: 500,
            color: 'var(--text-heading)',
            marginBottom: '12px',
            lineHeight: 1.1,
          }}
        >
          Horizon
        </div>
        <div
          style={{
            fontSize: '14px',
            color: 'var(--text-secondary-strong)',
            marginBottom: '32px',
            lineHeight: 1.5,
          }}
        >
          Risk monitoring and dependency analysis
        </div>
        <button
          onClick={onCreateProject}
          className="rounded-lg"
          style={{
            padding: '10px 20px',
            fontSize: '14px',
            fontWeight: 500,
            backgroundColor: 'var(--accent-primary, #3B82F6)',
            color: '#FFFFFF',
            border: 'none',
            cursor: 'pointer',
            transition: 'opacity 0.2s',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.opacity = '0.9';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.opacity = '1';
          }}
        >
          Create new project
        </button>
      </div>
    </div>
  );
};
