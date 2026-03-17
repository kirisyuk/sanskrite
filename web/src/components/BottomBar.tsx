import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useWsStore } from '../stores/wsStore';

export const BottomBar: React.FC = () => {
  const { health } = useWsStore();
  const navigate = useNavigate();

  const handleLegalClick = (
    e: React.MouseEvent<HTMLAnchorElement>,
    type: 'terms' | 'privacy' | 'technical-disclosure'
  ) => {
    e.preventDefault();
    navigate(`/${type}`);
  };

  return (
    <div
      className="bottombar"
      style={{ userSelect: 'none' }}
      onMouseDown={(e) => {
        // Глобально предотвращаем старт выделения текста в нижней панели,
        // но не мешаем клику по дочерним элементам
        e.preventDefault();
      }}
    >
      {/* Left side - Connection status indicator (aligned with topbar logo/settings spacing) */}
      <div className="flex items-center gap-3 px-4">
        <div
          style={{
            width: '5px',
            height: '5px',
            borderRadius: '50%',
            backgroundColor:
              health === 'ok'
                ? 'var(--text-primary)'
                : health === 'degraded'
                  ? 'var(--accent-warning)'
                  : 'var(--accent-danger)',
            transition: 'background-color 300ms ease',
          }}
          title={health === 'ok' ? 'Connected' : health === 'degraded' ? 'Degraded' : 'Disconnected'}
          aria-label={health === 'ok' ? 'Connected' : health === 'degraded' ? 'Degraded' : 'Disconnected'}
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
        <a
          href="/technical-disclosure"
          className="underline"
          style={{ color: 'var(--text-secondary-strong)', fontSize: '11px', lineHeight: '1' }}
          onClick={(e) => handleLegalClick(e, 'technical-disclosure')}
        >
          Technical Disclosure
        </a>
      </div>
    </div>
  );
};
