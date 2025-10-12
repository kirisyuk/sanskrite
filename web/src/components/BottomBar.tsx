import React from 'react';
import { useWsStore } from '../stores/wsStore';

export const BottomBar: React.FC = () => {
  const { connected } = useWsStore();

  return (
    <div className="bottombar">
      {/* Left side - Connection status indicator */}
      <div className="flex items-center gap-3 px-6">
        <div
          style={{
            width: '5px',
            height: '5px',
            borderRadius: '50%',
            backgroundColor: connected ? 'var(--text-primary)' : 'var(--accent-danger)',
            transition: 'background-color 300ms ease',
          }}
          title={connected ? 'Connected' : 'Disconnected'}
        />
      </div>

      {/* Center - Empty for now */}
      <div className="flex items-center gap-4" />

      {/* Right side - Empty for now */}
      <div className="flex items-center gap-3 px-6" />
    </div>
  );
};

