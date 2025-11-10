import React from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { api } from '../lib/api';
import WorkspaceLibrary from './WorkspaceLibrary';

interface WorkspacePanelProps {
  sources: any[];
  currentProjectId: string | null;
  addMenuRef: React.RefObject<HTMLDivElement | null>;
  showAddSourceMenu: boolean;
  onShowAddSourceMenuChange: (value: boolean) => void;
  onShowAddTelegramModalChange: (value: boolean) => void;
  onPreviewSource: (source: any) => void;
  onOpenDocument: (doc: any) => void;
}

const getSourceIcon = (kind?: string) => {
  if (kind === 'web') {
    return (
      <svg
        className="w-3 h-3"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M6 5c7.18 0 13 5.82 13 13"
        />
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M6 11a7 7 0 017 7"
        />
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M5 18a1 1 0 11-2 0 1 1 0 012 0z"
        />
      </svg>
    );
  } else if (kind === 'telegram') {
    return (
      <svg
        className="w-3 h-3"
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
    );
  }
  return (
    <svg
      className="w-3 h-3"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
    >
      <circle cx="12" cy="12" r="9" strokeWidth={2} />
    </svg>
  );
};

const renderSourceCard = (
  source: any,
  onPreviewSource: (source: any) => void,
) => {
  const isFetched = source.status === 'completed' || !!source.profile;
  const inferredKind =
    source.kind ||
    (typeof source.id === 'string' && source.id.startsWith('@')
      ? 'telegram'
      : undefined);

  return (
    <div
      key={source.id}
      className="text-xs rounded-2xl cursor-pointer"
      style={{
        backgroundColor: 'var(--bg-sidebar)',
        border: '1px solid var(--overlay-line-90)',
      }}
      role="button"
      onClick={() => onPreviewSource(source)}
    >
      <div className="flex items-center gap-2 px-3 py-2">
        {isFetched ? (
          <svg
            className="w-3 h-3"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            style={{ color: 'var(--accent-success)' }}
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9 12l2 2 4-4"
            />
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M12 22C6.477 22 2 17.523 2 12S6.477 2 12 2s10 4.477 10 10-4.477 10-10 10z"
            />
          </svg>
        ) : (
          getSourceIcon(inferredKind)
        )}
        <span>
          <strong style={{ fontWeight: 500 }}>
            {inferredKind || 'source'}
          </strong>{' '}
          {source.displayName || source.id}
        </span>
        <span className="ml-auto">
          {source.status === 'processing' && (
            <span
              className="animate-pulse"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              ...
            </span>
          )}
          {source.status === 'failed' && (
            <span style={{ color: 'var(--accent-danger)' }}>failed</span>
          )}
        </span>
      </div>

      <div
        className="px-3 pb-2"
        style={{ color: 'var(--text-secondary-strong)' }}
      >
        {source.profile?.motivation ? (
          <span>{source.profile.motivation}</span>
        ) : source.profile?.agenda?.description ? (
          <span>{source.profile.agenda.description}</span>
        ) : source.status === 'processing' ? (
          <span
            className="animate-pulse"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            ...
          </span>
        ) : !isFetched &&
          (source as any).__firstProfile &&
          (source as any).__hint === 'profilingNewSource' ? (
          <span style={{ color: 'var(--text-secondary-strong)' }}>
            Our system is establishing the channel's analytical profile based
            on its content.
          </span>
        ) : source.description ? (
          <span>{source.description}</span>
        ) : null}
      </div>

      {source.profile?.agenda?.primaryGoal && (
        <div
          className="ml-5 mt-1 rounded mb-1"
          style={{ color: 'var(--text-heading)' }}
        >
          <div>
            <span style={{ color: 'var(--text-secondary-strong)' }}>
              primaryGoal:
            </span>{' '}
            {source.profile.agenda.primaryGoal}
          </div>
        </div>
      )}

      {source.profile && (
        <div className="px-3 pb-3 space-y-2 mt-2">
          {source.profile.topics && source.profile.topics.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {source.profile.topics.slice(0, 6).map((t: any, i: number) => (
                <span
                  key={i}
                  className="px-2 py-0.5 rounded-full text-[10px]"
                  style={{
                    backgroundColor: 'var(--white-10)',
                    color: 'var(--text-secondary-strong)',
                  }}
                >
                  {t.name}
                  {typeof t.share === 'number'
                    ? ` ${Math.round(t.share * 100)}%`
                    : ''}
                </span>
              ))}
            </div>
          )}

          {(typeof source.profile.trustScore === 'number' ||
            typeof source.profile.goalAlignmentScore === 'number') && (
            <div
              className="flex items-center gap-3 text-[10px]"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              {typeof source.profile.goalAlignmentScore === 'number' && (
                <div className="flex items-center gap-1">
                  <span>alignment</span>
                  <div
                    className="w-24 h-1.5 rounded"
                    style={{ backgroundColor: 'var(--overlay-line-90)' }}
                  >
                    <div
                      className="h-1.5 rounded"
                      style={{
                        width: `${Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100}%`,
                        backgroundColor: 'var(--accent-success)',
                      }}
                    />
                  </div>
                  <span>
                    {Math.round(
                      Math.max(
                        0,
                        Math.min(1, source.profile.goalAlignmentScore),
                      ) * 100,
                    )}
                    %
                  </span>
                </div>
              )}
              {typeof source.profile.trustScore === 'number' && (
                <div className="flex items-center gap-1">
                  <span>trust</span>
                  <div
                    className="w-24 h-1.5 rounded"
                    style={{ backgroundColor: 'var(--overlay-line-90)' }}
                  >
                    <div
                      className="h-1.5 rounded"
                      style={{
                        width: `${Math.max(0, Math.min(1, source.profile.trustScore)) * 100}%`,
                        backgroundColor: 'var(--accent-success)',
                      }}
                    />
                  </div>
                  <span>
                    {Math.round(
                      Math.max(0, Math.min(1, source.profile.trustScore)) * 100,
                    )}
                    %
                  </span>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export const WorkspacePanel: React.FC<WorkspacePanelProps> = ({
  sources,
  currentProjectId,
  addMenuRef,
  showAddSourceMenu,
  onShowAddSourceMenuChange,
  onShowAddTelegramModalChange,
  onPreviewSource,
  onOpenDocument,
}) => {
  const { t } = useTranslation();
  const genaiChat = useGenaiChatStore();

  return (
    <>
      {/* Toolbar */}
      <div
        className="mb-3 px-3 py-2 rounded-lg"
        style={{ backgroundColor: 'var(--toolbar-bg)' }}
      >
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-start',
            gap: 6,
          }}
        >
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
              document.getElementById('doc-upload-input')?.click();
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
        </div>
      </div>

      {/* Sources section */}
      {sources.length > 0 && (
        <div className="mb-4">
          <div
            className="text-[11px] uppercase tracking-wide mb-2"
            style={{
              color: 'var(--text-tertiary-weak)',
            }}
          >
            Sources
          </div>
          <div className="space-y-2">
            {sources.map((source) =>
              renderSourceCard(source, onPreviewSource),
            )}
          </div>
        </div>
      )}

      {/* Add source button */}
      {currentProjectId && (
        <div className="mb-4" ref={addMenuRef}>
          <button
            type="button"
            title={t('unfold.sections.addSource')}
            aria-haspopup="true"
            aria-expanded={showAddSourceMenu}
            onClick={() => onShowAddSourceMenuChange(!showAddSourceMenu)}
            className="w-full rounded-lg transition-colors flex items-center justify-center gap-2 px-3 py-2"
            style={{
              backgroundColor: 'transparent',
              color: 'var(--text-heading)',
              border: '1px solid var(--overlay-line-90)',
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
                strokeWidth={2}
                d="M12 6v12M6 12h12"
              />
            </svg>
            <span className="text-12">
              {t('unfold.sections.addSource')}
            </span>
          </button>

          {showAddSourceMenu && (
            <div
              role="menu"
              aria-label={t('unfold.sections.addSourceOptions')}
              className="mt-2 rounded-xl shadow-lg"
              style={{
                backgroundColor: 'var(--bg-elevated)',
                border: '1px solid var(--overlay-line-90)',
              }}
            >
              {/* Telegram Channel */}
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  onShowAddSourceMenuChange(false);
                  onShowAddTelegramModalChange(true);
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
                onClick={() => onShowAddSourceMenuChange(false)}
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
                onClick={() => onShowAddSourceMenuChange(false)}
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
                      onShowAddSourceMenuChange(false);
                      const pid =
                        useGenaiChatStore.getState().projectId ||
                        (await useGenaiChatStore.getState().ensureProject());
                      const doc = await api.docsUpload(pid as string, file);
                      try {
                        await api.docsParse(pid as string, doc.id);
                      } catch {}
                    } catch (err) {
                      console.error(
                        '[Discovery] Document upload failed',
                        err,
                      );
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
      )}

      <WorkspaceLibrary
        projectId={genaiChat.projectId}
        onDocumentDelete={(docId: string) => {
          console.log('[DiscoveryPage] Document deleted', { docId });
          // Optionally refresh interactions or handle cleanup
        }}
        onOpenDocument={onOpenDocument}
      />
    </>
  );
};

