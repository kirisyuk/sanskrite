import React from 'react';

export const HorizonLoading: React.FC = () => {
  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
    >
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 10,
          padding: '16px 20px',
          borderRadius: 16,
          border: '1px solid var(--border-subtle)',
          background: 'var(--bg-elevated)',
          boxShadow: '0 12px 40px rgba(0,0,0,0.18)',
          minWidth: 220,
        }}
      >
        <style>{`
          @keyframes horizon-dot-bounce {
            0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
            40% { transform: translateY(-6px); opacity: 1; }
          }
        `}</style>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              style={{
                width: 10,
                height: 10,
                borderRadius: 999,
                background: 'var(--accent-primary, #3B82F6)',
                animation: 'horizon-dot-bounce 1.1s infinite ease-in-out',
                animationDelay: `${i * 0.14}s`,
              }}
            />
          ))}
        </div>
        <div style={{ fontSize: 12, fontWeight: 650, color: 'var(--text-secondary-strong)' }}>
          Loading project...
        </div>
      </div>
    </div>
  );
};
