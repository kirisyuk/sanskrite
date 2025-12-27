import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ThemeContext } from '../../../../components/ThemeProvider';

interface AssembleLensRequestProps {
  projectId: string;
  onAssemble: (request: AssembleLensRequest) => void;
  disabled?: boolean;
  pinnedBottom?: boolean;
}

export interface AssembleLensRequest {
  projectId: string;
  query: string;
  name?: string;
  filters: {
    maxRecursionDepth: number;
  };
}

const PLACEHOLDER_VARIANTS = [
  "What are you analyzing?",
  "What are you investigating?",
  "What would you like to explore?",
  "What is your focus area?",
  "What are you looking at?",
];

const SUGGESTIONS = [
  "Tesla supply chain risks",
  "Nvidia AI chips competitors",
  "Apple smartphone suppliers",
  "BYD vs Tesla battery tech",
  "OpenAI corporate structure",
  "Microsoft cloud market share",
  "SpaceX regulatory challenges",
  "TSMC manufacturing capacity",
  "Volkswagen EV strategy",
  "Amazon logistics network",
];

const AssembleLensRequestComponent: React.FC<AssembleLensRequestProps> = ({ 
  projectId, 
  onAssemble, 
  disabled = false,
  pinnedBottom = false,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [inputValue, setInputValue] = useState('');
  const [filters, setFilters] = useState({
    maxRecursionDepth: 1,
  });
  const [showSettings, setShowSettings] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const [placeholder] = useState(() => 
    PLACEHOLDER_VARIANTS[Math.floor(Math.random() * PLACEHOLDER_VARIANTS.length)]
  );
  const [suggestions] = useState(() => 
    [...SUGGESTIONS].sort(() => 0.5 - Math.random()).slice(0, 3)
  );

  // Focus input on mount
  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.focus();
    }
  }, []);

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value);
  };

  const handleAssemble = () => {
    const q = inputValue.trim();
    if (!q) {
      return;
    }

    const request: AssembleLensRequest = {
      projectId,
      query: q,
      filters,
    };

    onAssemble(request);
  };

  const containerStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: pinnedBottom ? 'flex-end' : 'center',
    // When pinned at the bottom, this block must NOT take full height,
    // otherwise it will push the realtime feed outside the viewport.
    height: pinnedBottom ? 'auto' : '100%',
    flexShrink: pinnedBottom ? 0 : undefined,
    width: '100%',
    padding: pinnedBottom ? '0 24px 24px 24px' : '2rem',
  };

  const inputContainerStyle: React.CSSProperties = {
    width: '100%',
    maxWidth: '960px',
    position: 'relative',
  };

  const inputStyle: React.CSSProperties = {
    width: '100%',
    // extra right padding for the "send" arrow button and settings button
    padding: '1.5rem 3.75rem 1.5rem 1.25rem',
    fontSize: '1.25rem',
    backgroundColor: 'transparent', // Transparent since container has bg
    border: 'none', // No border since container has border
    borderRadius: '12px',
    outline: 'none',
    color: 'var(--text-heading)',
  };

  const containerStyleForInput: React.CSSProperties = {
    width: '100%',
    maxWidth: '960px',
    borderRadius: '12px',
    overflow: 'hidden',
    transition: 'all 0.2s ease',
    backgroundColor: theme === 'dark' ? 'var(--bg-input, #1A1A1A)' : 'var(--bg-input, #FFFFFF)',
    boxShadow: theme === 'light' ? '0 4px 16px rgba(0,0,0,0.12)' : 'none',
  };

  const sendBtnDisabled = disabled || !inputValue.trim();

  return (
    <div style={containerStyle}>
      <div style={inputContainerStyle}>
        <div 
          className="rounded-xl overflow-hidden unfold-request-gradient-border relative"
          style={containerStyleForInput}
        >
          <input
            ref={inputRef}
            type="text"
            value={inputValue}
            onChange={handleInputChange}
            placeholder={placeholder}
            style={inputStyle}
            disabled={disabled}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !disabled) {
                e.preventDefault();
                handleAssemble();
              }
            }}
          />

          {/* Settings button (gear icon) */}
          <button
            type="button"
            aria-label="Settings"
            className="flex items-center justify-center rounded-full"
            onClick={() => setShowSettings(true)}
            style={{
              position: 'absolute',
              right: 54,
              top: 30,
              width: '32px',
              height: '32px',
              backgroundColor: 'transparent',
            }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
              <circle cx="12" cy="12" r="3" />
            </svg>
          </button>

          {/* Send arrow (same look/placement as UnfoldRequest) */}
          <button
            type="button"
            aria-label="Start"
            className="flex items-center justify-center rounded-full"
            onClick={handleAssemble}
            disabled={sendBtnDisabled}
            style={{
              position: 'absolute',
              right: 14,
              top: 30,
              width: '32px',
              height: '32px',
              backgroundColor: 'transparent',
              opacity: sendBtnDisabled ? 0.6 : 1,
              cursor: sendBtnDisabled ? 'not-allowed' : 'pointer',
            }}
            onMouseEnter={(e) => {
              if (sendBtnDisabled) return;
              const svg = e.currentTarget.querySelector('svg path');
              if (svg) {
                (svg as SVGPathElement).style.fill = theme === 'light' ? '#FFB366' : '#FF9538';
              }
            }}
            onMouseLeave={(e) => {
              if (sendBtnDisabled) return;
              const svg = e.currentTarget.querySelector('svg path');
              if (svg) {
                (svg as SVGPathElement).style.fill = theme === 'dark' ? '#FF9538' : '#FFB366';
              }
            }}
          >
            <svg className="w-6 h-6" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M8 5v14l11-7z" style={{ fill: theme === 'dark' ? '#FF9538' : '#FFB366' }} />
            </svg>
          </button>
        </div>
      </div>

      {/* Suggestions (only when centered) */}
      {!pinnedBottom && (
        <div style={{ 
          marginTop: 16, 
          fontSize: 13, 
          color: 'var(--text-tertiary)', 
          textAlign: 'center',
          maxWidth: '800px',
          lineHeight: 1.6 
        }}>
          <span style={{ marginRight: 6 }}>Try:</span>
          {suggestions.map((s, i) => (
            <React.Fragment key={s}>
              <span 
                style={{ 
                  cursor: 'pointer', 
                  color: 'var(--text-secondary)',
                  borderBottom: '1px dashed var(--border-subtle, rgba(255,255,255,0.2))',
                  transition: 'color 0.2s',
                }}
                onClick={() => {
                  setInputValue(s);
                  if (inputRef.current) inputRef.current.focus();
                }}
                onMouseEnter={(e) => e.currentTarget.style.color = 'var(--text-heading)'}
                onMouseLeave={(e) => e.currentTarget.style.color = 'var(--text-secondary)'}
              >
                {s}
              </span>
              {i < suggestions.length - 1 && <span style={{ margin: '0 8px', color: 'var(--text-disabled, rgba(255,255,255,0.1))' }}>|</span>}
            </React.Fragment>
          ))}
        </div>
      )}

      {/* Settings Modal */}
      {showSettings && createPortal(
        <div className="fixed inset-0 z-[80] flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0 bg-black/30" onClick={() => setShowSettings(false)} />
          <div className="relative w-full max-w-lg rounded-xl border p-4" style={{ backgroundColor: 'var(--bg-elevated)', borderColor: 'var(--overlay-line-90)', boxShadow: '0 8px 32px rgba(0, 0, 0, 0.35)' }}>
            <div className="flex items-center justify-between mb-3">
              <div className="text-lg" style={{ color: 'var(--text-heading)' }}>Settings</div>
              <button onClick={() => setShowSettings(false)} aria-label="Close" className="rounded-full" style={{ width: 32, height: 32 }}>
                <svg viewBox="0 0 24 24" className="w-6 h-6"><path d="M6 6l12 12M18 6L6 18" stroke="var(--text-secondary-strong)" strokeWidth="2"/></svg>
              </button>
            </div>
            <div className="space-y-3">
              <div className="rounded-lg p-3" style={{ backgroundColor: 'var(--white-06)' }}>
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>Max recursion depth</label>
                  <input
                    type="number"
                    min={0}
                    max={5}
                    value={filters.maxRecursionDepth}
                    onChange={(e) =>
                      setFilters({
                        ...filters,
                        maxRecursionDepth: Math.max(0, Math.min(5, parseInt(e.target.value || '1', 10))),
                      })
                    }
                    className="w-full"
                    style={{
                      backgroundColor: 'var(--bg-input)',
                      color: 'var(--text-heading)',
                      border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                      borderRadius: '8px',
                      padding: '8px 12px',
                      outline: 'none',
                      fontSize: '14px',
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.borderColor = 'var(--overlay-line-80)';
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.borderColor = 'var(--border-input, rgba(255, 255, 255, 0.1))';
                    }}
                    onFocus={(e) => {
                      e.currentTarget.style.borderColor = 'var(--overlay-line-80)';
                    }}
                    onBlur={(e) => {
                      e.currentTarget.style.borderColor = 'var(--border-input, rgba(255, 255, 255, 0.1))';
                    }}
                  />
                  <div className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
                    Maximum depth for recursive fragment expansion (0-5)
                  </div>
                </div>
              </div>
            </div>
            <div className="mt-4 text-right">
              <button
                className="px-3 py-2 rounded transition-colors"
                onClick={() => setShowSettings(false)}
                style={{ background: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { 
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                }}
                onMouseLeave={(e) => { 
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}
    </div>
  );
};

export default AssembleLensRequestComponent;

