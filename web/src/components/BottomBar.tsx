import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useWsStore } from '../stores/wsStore';

export const BottomBar: React.FC = () => {
  const { connected, health, modalOpen, setModalOpen, getDiagnostics } = useWsStore();
  const navigate = useNavigate();

  const handleLegalClick = (e: React.MouseEvent<HTMLAnchorElement>, type: 'terms' | 'privacy') => {
    e.preventDefault();
    navigate(`/${type}`);
  };

  return (
    <div className="bottombar">
      {/* Left side - Connection status indicator (aligned with topbar logo/settings spacing) */}
      <div className="flex items-center gap-3 px-4">
        <div
          style={{
            width: '5px',
            height: '5px',
            borderRadius: '50%',
            backgroundColor: health === 'ok' ? 'var(--text-primary)' : (health === 'degraded' ? 'var(--accent-warning)' : 'var(--accent-danger)'),
            transition: 'background-color 300ms ease',
          }}
          title={health === 'ok' ? 'Connected' : (health === 'degraded' ? 'Degraded' : 'Disconnected')}
          aria-label={health === 'ok' ? 'Connected' : (health === 'degraded' ? 'Degraded' : 'Disconnected')}
          onClick={() => setModalOpen(!modalOpen)}
          role="button"
          tabIndex={0}
        />
      </div>

      {/* Center - Empty for now */}
      <div className="flex items-center gap-4" />

      {/* Right side - Links */}
      <div className="flex items-center gap-3 px-4">
        <a 
          href="/privacy" 
          className="underline" 
          style={{ color: 'var(--text-secondary-strong)', fontSize: '11px', lineHeight: '1' }} 
          onClick={(e) => handleLegalClick(e, 'privacy')}
        >
          Privacy Notice
        </a>
        <a 
          href="/terms" 
          className="underline" 
          style={{ color: 'var(--text-secondary-strong)', fontSize: '11px', lineHeight: '1' }} 
          onClick={(e) => handleLegalClick(e, 'terms')}
        >
          Terms of Usage
        </a>
      </div>

      {modalOpen && (
        <div style={{ position: 'fixed', left: 0, top: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.35)', zIndex: 1000 }} onClick={() => setModalOpen(false)}>
          <div style={{ position: 'absolute', right: 16, bottom: 48, width: 520, maxWidth: '90vw', background: 'var(--bg-elevated)', color: 'var(--text-primary)', borderRadius: 8, padding: 12, boxShadow: '0 6px 24px rgba(0,0,0,0.2)' }} onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between" style={{ marginBottom: 8 }}>
              <div className="text-14" style={{ fontWeight: 600 }}>WebSocket diagnostics</div>
              <button className="text-12" onClick={() => setModalOpen(false)} style={{ color: 'var(--text-secondary)' }}>Close</button>
            </div>
            <textarea readOnly value={getDiagnostics()} style={{ width: '100%', height: 260, fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace', fontSize: 12, lineHeight: 1.4, background: 'var(--bg)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 6, padding: 8 }} />
            <div className="flex items-center justify-end gap-2" style={{ marginTop: 8 }}>
              <button className="text-12" onClick={() => { try { navigator.clipboard.writeText(getDiagnostics()); } catch {} }}>
                Copy
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

