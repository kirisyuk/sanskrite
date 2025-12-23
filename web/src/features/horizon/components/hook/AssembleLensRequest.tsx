import React, { useEffect, useRef, useState } from 'react';
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

const AssembleLensRequestComponent: React.FC<AssembleLensRequestProps> = ({ 
  projectId, 
  onAssemble, 
  disabled = false,
  pinnedBottom = false,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [inputValue, setInputValue] = useState('');
  const [filters, setFilters] = useState({
    maxRecursionDepth: 2,
  });
  const inputRef = useRef<HTMLInputElement>(null);

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
    maxWidth: '800px',
    position: 'relative',
  };

  const inputStyle: React.CSSProperties = {
    width: '100%',
    // extra right padding for the "send" arrow button
    padding: '1.5rem 3.75rem 1.5rem 1.25rem',
    fontSize: '1.25rem',
    backgroundColor: 'var(--bg-elevated)',
    border: `2px solid var(--overlay-line-90)`,
    borderRadius: '12px',
    outline: 'none',
    color: 'var(--text-heading)',
    transition: 'border-color 0.2s, box-shadow 0.2s',
    boxShadow: theme === 'light' ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
  };

  const sendBtnDisabled = disabled || !inputValue.trim();

  const controlsRowStyle: React.CSSProperties = {
    width: '100%',
    maxWidth: '800px',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'flex-start',
    gap: 12,
    marginTop: 12,
  };

  const labelStyle: React.CSSProperties = {
    fontSize: 12,
    color: 'var(--text-secondary)',
  };

  const smallInputStyle: React.CSSProperties = {
    width: 110,
    padding: '8px 10px',
    borderRadius: 10,
    border: '1px solid var(--overlay-line-90)',
    background: 'var(--bg-elevated)',
    color: 'var(--text-heading)',
    outline: 'none',
  };

  return (
    <div style={containerStyle}>
      <div style={inputContainerStyle}>
        <input
          ref={inputRef}
          type="text"
          value={inputValue}
          onChange={handleInputChange}
          placeholder="Define your Lens intent (English)..."
          style={inputStyle}
          disabled={disabled}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !disabled) {
              e.preventDefault();
              handleAssemble();
            }
          }}
        />

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

        <div style={controlsRowStyle}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            <span style={labelStyle}>Max recursion depth</span>
            <input
              type="number"
              min={0}
              max={5}
              value={filters.maxRecursionDepth}
              onChange={(e) =>
                setFilters({
                  ...filters,
                  maxRecursionDepth: Math.max(0, Math.min(5, parseInt(e.target.value || '0', 10))),
                })
              }
              style={smallInputStyle}
              disabled={disabled}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

export default AssembleLensRequestComponent;

