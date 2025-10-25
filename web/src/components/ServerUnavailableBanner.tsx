import React, { useEffect } from 'react';
import { useServerStatusStore } from '../stores/serverStatusStore';

export const ServerUnavailableBanner: React.FC = () => {
  const { isAvailable, errorMessage, clearError } = useServerStatusStore();

  useEffect(() => {
    // Auto-hide error after 10 seconds
    if (!isAvailable && errorMessage) {
      const timer = setTimeout(() => {
        clearError();
      }, 10000);

      return () => clearTimeout(timer);
    }
  }, [isAvailable, errorMessage, clearError]);

  if (isAvailable || !errorMessage) {
    return null;
  }

  return (
    <div
      style={{
        position: 'fixed',
        top: '60px',
        left: '50%',
        transform: 'translateX(-50%)',
        backgroundColor: '#fee',
        border: '1px solid #fcc',
        borderRadius: '8px',
        padding: '12px 20px',
        boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
        zIndex: 9999,
        display: 'flex',
        alignItems: 'center',
        gap: '12px',
        maxWidth: '90%',
        animation: 'slideDown 0.3s ease-out',
      }}
    >
      <div
        style={{
          width: '20px',
          height: '20px',
          borderRadius: '50%',
          backgroundColor: '#f44',
          flexShrink: 0,
        }}
      />
      <div style={{ flex: 1, color: '#600', fontSize: '14px', fontWeight: 500 }}>
        {errorMessage}
      </div>
      <button
        onClick={clearError}
        style={{
          background: 'none',
          border: 'none',
          color: '#600',
          cursor: 'pointer',
          fontSize: '18px',
          padding: '0 4px',
          lineHeight: 1,
        }}
        aria-label="Close"
      >
        ×
      </button>
      <style>{`
        @keyframes slideDown {
          from {
            opacity: 0;
            transform: translateX(-50%) translateY(-20px);
          }
          to {
            opacity: 1;
            transform: translateX(-50%) translateY(0);
          }
        }
      `}</style>
    </div>
  );
};

