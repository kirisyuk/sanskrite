import React from 'react';

export const HorizonLoading: React.FC = () => {
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
      <div style={{ textAlign: 'center' }}>
        <div
          style={{
            width: 24,
            height: 24,
            margin: '0 auto 16px',
            border: '2px solid var(--text-tertiary-weak)',
            borderTopColor: 'var(--text-heading)',
            borderRadius: '50%',
            animation: 'spin 0.8s linear infinite',
          }}
        />
        <div
          style={{
            fontSize: '14px',
            color: 'var(--text-secondary-strong)',
            fontWeight: 400,
          }}
        >
          Loading project...
        </div>
      </div>
      <style>{`
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </div>
  );
};
