import React from 'react';
import { ThemeContext } from './ThemeProvider';

interface DiscoveryRequestProps {
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void;
  disabled?: boolean;
}

const DiscoveryRequest: React.FC<DiscoveryRequestProps> = ({ value, onChange, onSubmit, disabled }) => {
  const { theme } = React.useContext(ThemeContext);
  const containerStyle: React.CSSProperties = {
    borderColor: 'var(--overlay-line-90)',
    backgroundColor: 'var(--bg-elevated)',
    boxShadow: theme === 'light' ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
  };

  return (
    <div className="rounded-xl border overflow-hidden mt-2" style={containerStyle}>
      <div className="p-3">
        <div className="flex items-end gap-3">
          <div className="flex-1">
            <textarea
              rows={3}
              value={value}
              onChange={(e) => onChange(e.target.value)}
              placeholder="What would you like to discover? Include URLs or @telegram handles..."
              className="w-full resize-none bg-transparent outline-none"
              style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)' }}
              aria-label="Discovery query"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  onSubmit();
                }
              }}
            />
          </div>
          <button
            type="button"
            aria-label="Send"
            className="flex items-center justify-center rounded-full"
            onClick={() => { if (!disabled) onSubmit(); }}
            disabled={disabled}
            style={{
              width: '32px',
              height: '32px',
              backgroundColor: 'transparent',
              opacity: disabled ? 0.6 : 1,
            }}
          >
            <svg className="w-6 h-6" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M8 5v14l11-7z" fill="var(--text-secondary-strong)" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  );
};

export default DiscoveryRequest;


