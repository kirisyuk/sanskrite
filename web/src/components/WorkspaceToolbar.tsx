/**
 * Общий компонент Toolbar для WorkspaceCanvas и WorkspaceLibrary
 * Предоставляет кнопки для добавления документов и источников
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export interface WorkspaceToolbarProps {
  projectId: string | null;
  onDocumentAdded?: () => void;
  onSourceAdded?: () => void;
  onShowTelegramModal?: () => void;
  className?: string;
}

export const WorkspaceToolbar: React.FC<WorkspaceToolbarProps> = ({
  projectId,
  onDocumentAdded,
  onSourceAdded,
  onShowTelegramModal,
  className = '',
}) => {
  const { t } = useTranslation();
  const [showAddSourceMenu, setShowAddSourceMenu] = React.useState(false);
  const addMenuRef = React.useRef<HTMLDivElement | null>(null);

  // Close menu when clicking outside
  React.useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (addMenuRef.current && !addMenuRef.current.contains(event.target as Node)) {
        setShowAddSourceMenu(false);
      }
    };

    if (showAddSourceMenu) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => {
        document.removeEventListener('mousedown', handleClickOutside);
      };
    }
  }, [showAddSourceMenu]);

  const handleDocumentUpload = async (file: File) => {
    if (!projectId || projectId === 'local-unfold') return;
    
    try {
      const doc = await api.docsUpload(projectId, file);
      // Parsing is triggered automatically by backend after upload
      onDocumentAdded?.();
    } catch (err) {
      console.error('[WorkspaceToolbar] Document upload failed', err);
      throw err;
    }
  };

  if (!projectId || projectId === 'local-unfold') {
    return null;
  }

  return (
    <div className={`mb-3 px-3 py-2 rounded-lg ${className}`} style={{ backgroundColor: 'var(--toolbar-bg)' }}>
      <div
        ref={addMenuRef}
        style={{
          display: 'flex',
          justifyContent: 'flex-start',
          gap: 6,
          position: 'relative',
        }}
      >
        {/* Add Document button */}
        <button
          type="button"
          aria-label="Add document"
          title="Add document"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={() => {
            const inputId = `workspace-toolbar-doc-upload-${projectId}`;
            document.getElementById(inputId)?.click();
          }}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"
              strokeWidth="2"
            />
            <path
              d="M14 2v6h6M12 11v6M9 14h6"
              strokeWidth="2"
            />
          </svg>
        </button>

        {/* Add Source button */}
        <button
          type="button"
          aria-label="Add source"
          title={t('unfold.sections.addSource')}
          aria-haspopup="true"
          aria-expanded={showAddSourceMenu}
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={() => setShowAddSourceMenu(!showAddSourceMenu)}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              strokeLinecap="round"
              strokeWidth={2}
              d="M12 6v12M6 12h12"
            />
          </svg>
        </button>

        {/* Hidden file input for document upload */}
        <input
          id={`workspace-toolbar-doc-upload-${projectId}`}
          type="file"
          accept=".txt,.md,.csv,.tsv,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,text/plain,text/markdown,text/csv,text/tab-separated-values"
          style={{ display: 'none' }}
          onChange={async (e) => {
            const file = e.target.files?.[0];
            if (!file) return;
            try {
              await handleDocumentUpload(file);
            } catch (err) {
              console.error('[WorkspaceToolbar] Failed to upload document', err);
            } finally {
              (e.target as HTMLInputElement).value = '';
            }
          }}
        />

        {/* Add source menu dropdown */}
        {showAddSourceMenu && (
          <div
            role="menu"
            aria-label={t('unfold.sections.addSourceOptions')}
            className="absolute top-full left-0 mt-2 rounded-xl shadow-lg z-50"
            style={{
              backgroundColor: 'var(--bg-elevated)',
              border: '1px solid var(--overlay-line-90)',
              minWidth: '200px',
            }}
          >
            {/* Telegram Channel */}
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setShowAddSourceMenu(false);
                onShowTelegramModal?.();
              }}
              className="w-full flex items-center gap-2 px-3 py-2 rounded-t-xl text-left transition-colors"
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
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M3 11l18-7-7 18-4-8-7-3z"
                />
              </svg>
              <span className="text-13">Telegram Channel</span>
            </button>

            {/* Web Page */}
            <button
              type="button"
              role="menuitem"
              onClick={() => setShowAddSourceMenu(false)}
              className="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors"
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
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
              >
                <circle cx="12" cy="12" r="9" strokeWidth={2} />
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M3 12h18M12 3a15 15 0 010 18M12 3a15 15 0 000 18"
                />
              </svg>
              <span className="text-13">Web Page</span>
            </button>

            {/* RSS */}
            <button
              type="button"
              role="menuitem"
              onClick={() => setShowAddSourceMenu(false)}
              className="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors"
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
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M5 5c7.18 0 13 5.82 13 13"
                />
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M5 11a7 7 0 017 7"
                />
                <circle
                  cx="6"
                  cy="18"
                  r="1.5"
                  fill="currentColor"
                />
              </svg>
              <span className="text-13">RSS</span>
            </button>

            {/* Document upload */}
            <label
              className="w-full flex items-center gap-2 px-3 py-2 rounded-b-xl text-left cursor-pointer"
              style={{
                backgroundColor: 'transparent',
                color: 'var(--text-heading)',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLLabelElement).style.backgroundColor =
                  'var(--white-10)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLLabelElement).style.backgroundColor =
                  'transparent';
              }}
            >
              <input
                type="file"
                accept=".txt,.md,.csv,.tsv,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,text/plain,text/markdown,text/csv,text/tab-separated-values"
                style={{ display: 'none' }}
                onChange={async (e) => {
                  const file = e.target.files?.[0];
                  if (!file) return;
                  try {
                    setShowAddSourceMenu(false);
                    await handleDocumentUpload(file);
                  } catch (err) {
                    console.error('[WorkspaceToolbar] Document upload failed', err);
                  } finally {
                    (e.target as HTMLInputElement).value = '';
                  }
                }}
              />
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M7 3h7l4 4v14a2 2 0 01-2 2H7a2 2 0 01-2-2V5a2 2 0 012-2z"
                />
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M14 3v4h4"
                />
              </svg>
              <span className="text-13">Document</span>
            </label>
          </div>
        )}
      </div>
    </div>
  );
};

