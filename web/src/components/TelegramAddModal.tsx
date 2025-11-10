/**
 * Модальное окно для добавления Telegram канала
 * Вынесено из DiscoveryPage для улучшения читаемости
 */

import React from 'react';

export interface TelegramAddModalProps {
  isOpen: boolean;
  onClose: () => void;
  input: string;
  onInputChange: (value: string) => void;
  onSubmit: () => void;
  isSubmitting: boolean;
  error: string | null;
}

export const TelegramAddModal: React.FC<TelegramAddModalProps> = ({
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
      aria-label="Add Telegram Channel"
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
            Add Telegram Channel
          </div>
        </div>

        {/* Body */}
        <div className="p-4 space-y-3">
          <div
            className="text-xs"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            Enter @username or a link like t.me/username
          </div>
          
          <div className="flex items-center gap-2">
            {/* Telegram icon */}
            <div
              className="flex items-center justify-center rounded-lg"
              style={{
                width: 32,
                height: 32,
                background:
                  'linear-gradient(135deg, #2AABEE 0%, #229ED9 100%)',
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
                <path
                  d="M3 11l18-7-7 18-4-8-7-3z"
                  stroke="currentColor"
                />
              </svg>
            </div>
            
            {/* Input */}
            <input
              type="text"
              value={input}
              onChange={(e) => onInputChange(e.target.value)}
              placeholder="@channel or t.me/channel"
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
              backgroundColor: '#2AABEE',
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

