/**
 * Модальное окно для добавления RSS источника
 */

import React from 'react';

export interface RSSAddModalProps {
  isOpen: boolean;
  onClose: () => void;
  input: string;
  onInputChange: (value: string) => void;
  onSubmit: () => void;
  isSubmitting: boolean;
  error: string | null;
}

export const RSSAddModal: React.FC<RSSAddModalProps> = ({
  isOpen,
  onClose,
  input,
  onInputChange,
  onSubmit,
  isSubmitting,
  error,
}) => {
  if (!isOpen) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Add RSS Feed"
      className="fixed inset-0 flex items-center justify-center"
      style={{ zIndex: 50 }}
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0"
        style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}
        onClick={() => !isSubmitting && onClose()}
      />
      
      {/* Modal content */}
      <div
        className="relative rounded-xl shadow-xl w-full max-w-sm"
        style={{
          backgroundColor: 'var(--bg-elevated)',
          border: '1px solid var(--overlay-line-90)',
        }}
      >
        {/* Header */}
        <div
          className="px-4 py-3"
          style={{
            borderBottom: '1px solid var(--overlay-line-90)',
          }}
        >
          <div
            className="text-13 font-medium"
            style={{ color: 'var(--text-heading)' }}
          >
            Add RSS Feed
          </div>
        </div>

        {/* Body */}
        <div className="p-4 space-y-3">
          <div
            className="text-xs"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            Enter RSS feed URL (e.g. https://example.com/feed)
          </div>
          
          <div className="flex items-center gap-2">
            {/* RSS icon */}
            <div
              className="flex items-center justify-center rounded-lg"
              style={{
                width: 32,
                height: 32,
                background:
                  'linear-gradient(135deg, #FFA500 0%, #FF8C00 100%)',
                color: 'white',
              }}
            >
              <svg
                viewBox="0 0 24 24"
                width={16}
                height={16}
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
              >
                <circle cx="6" cy="18" r="3" />
                <path d="M6 9a9 9 0 0 1 9 9" />
                <path d="M6 3a15 15 0 0 1 15 15" />
              </svg>
            </div>
            
            {/* Input */}
            <input
              type="text"
              value={input}
              onChange={(e) => onInputChange(e.target.value)}
              placeholder="https://example.com/feed.xml"
              className="flex-1 bg-transparent outline-none text-sm"
              style={{ color: 'var(--text-heading)' }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  onSubmit();
                }
              }}
              autoFocus
            />
          </div>
          
          {/* Error message */}
          {error && (
            <div
              className="text-xs"
              style={{ color: 'var(--accent-danger)' }}
            >
              {error}
            </div>
          )}
        </div>

        {/* Footer */}
        <div
          className="px-4 py-3 flex items-center justify-end gap-2"
          style={{
            borderTop: '1px solid var(--overlay-line-90)',
          }}
        >
          <button
            type="button"
            className="px-3 py-1.5 rounded-lg text-xs transition-colors"
            onClick={onClose}
            disabled={isSubmitting}
            style={{
              backgroundColor: 'transparent',
              color: 'var(--text-heading)',
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'var(--white-10)';
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'transparent';
            }}
          >
            Cancel
          </button>
          <button
            type="button"
            className="px-3 py-1.5 rounded-lg text-xs"
            onClick={onSubmit}
            disabled={isSubmitting}
            style={{
              backgroundColor: '#FF8C00',
              color: 'white',
              opacity: isSubmitting ? 0.7 : 1,
            }}
          >
            {isSubmitting ? 'Adding…' : 'Add'}
          </button>
        </div>
      </div>
    </div>
  );
};

