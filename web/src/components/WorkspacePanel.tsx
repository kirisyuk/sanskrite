import React from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { api } from '../lib/api';
import { useModals } from '../hooks/useModals';
import { WorkspaceToolbar } from './WorkspaceToolbar';
import WorkspaceLibrary from './WorkspaceLibrary';

interface WorkspacePanelProps {
  currentProjectId: string | null;
  onShowAddTelegramModalChange: (value: boolean) => void;
  onPreviewSource: (source: any) => void;
  onOpenDocument: (doc: any) => void;
  onExpandWorkspace?: (projectId: string | null) => void;
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
        <div className="flex-shrink-0" style={{ color: 'var(--text-secondary)' }}>
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
        </div>
        <div className="flex-1 min-w-0">
          <div className="font-medium truncate" style={{ color: 'var(--text-heading)' }} title={source.displayName || source.id}>
            {source.displayName || source.id}
          </div>
          <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
            {inferredKind || 'source'}
            {source.status && ` • ${source.status}`}
          </div>
        </div>
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
  currentProjectId,
  onShowAddTelegramModalChange,
  onPreviewSource,
  onOpenDocument,
  onExpandWorkspace,
}) => {
  const { t } = useTranslation();
  const genaiChat = useGenaiChatStore();
  // Use onExpandWorkspace from props if provided, otherwise fallback to useModals
  const { openWorkspace: fallbackOpenWorkspace } = useModals();
  const openWorkspace = onExpandWorkspace || fallbackOpenWorkspace;

  // Handler for document added callback
  const handleDocumentAdded = React.useCallback(() => {
    // WorkspaceLibrary will reload its own data
  }, []);

  // Handler for source added callback
  const handleSourceAdded = React.useCallback(() => {
    // WorkspaceLibrary will reload its own data
  }, []);

  return (
    <>
      {/* Toolbar */}
      <WorkspaceToolbar
        projectId={genaiChat.projectId}
        onDocumentAdded={handleDocumentAdded}
        onSourceAdded={handleSourceAdded}
        onShowTelegramModal={() => onShowAddTelegramModalChange(true)}
      />

      {/* Sources are now shown in WorkspaceLibrary below to avoid duplication */}
      <WorkspaceLibrary
        projectId={genaiChat.projectId}
        onDocumentDelete={(docId: string) => {
          console.log('[DiscoveryPage] Document deleted', { docId });
          // Optionally refresh interactions or handle cleanup
        }}
        onOpenDocument={onOpenDocument}
        onExpand={(projectId) => {
          // Open WorkspaceCanvas with project filter - use the projectId from WorkspaceLibrary
          console.log('[WorkspacePanel] onExpand called', { projectId });
          openWorkspace(projectId);
        }}
      />
    </>
  );
};

