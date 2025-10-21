import React from 'react';
import { useWsStore } from '../stores/wsStore';

export const BottomBar: React.FC = () => {
  const { connected } = useWsStore();

  return (
    <div className="bottombar">
      {/* Left side - Connection status indicator (aligned with topbar logo/settings spacing) */}
      <div className="flex items-center gap-3 px-5">
        <div
          style={{
            width: '5px',
            height: '5px',
            borderRadius: '50%',
            backgroundColor: connected ? 'var(--text-primary)' : 'var(--accent-danger)',
            transition: 'background-color 300ms ease',
          }}
          title={connected ? 'Connected' : 'Connection lost'}
          aria-label={connected ? 'Connected' : 'Connection lost'}
        />
      </div>

      {/* Center - Empty for now */}
      <div className="flex items-center gap-4" />

      {/* Right side - Links */}
      <div className="flex items-center gap-3 px-6">
        <a href="#" className="text-12 underline" style={{ color: 'var(--text-secondary-strong)' }} onClick={(e) => { e.preventDefault(); const ev = new CustomEvent('ui.openPage', { detail: { kind: 'privacy' } }); window.dispatchEvent(ev); }}>Privacy Notice</a>
        <a href="#" className="text-12 underline" style={{ color: 'var(--text-secondary-strong)' }} onClick={(e) => { e.preventDefault(); const ev = new CustomEvent('ui.openPage', { detail: { kind: 'terms' } }); window.dispatchEvent(ev); }}>Terms of Usage</a>
      </div>
    </div>
  );
};

