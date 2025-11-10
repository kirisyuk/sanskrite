import React from 'react';
import { useTranslation } from 'react-i18next';
import { useProjectsStore } from '../stores/projectsStore';

interface SidebarPanelProps {
  isOpen: boolean;
  onOpen: () => void;
  onClose: () => void;
  isMobile: boolean;
  isAdmin: boolean;
  onResetChat: () => void;
  onShowSettings: () => void;
  onShowAccount: () => void;
  onShowAdmin: () => void;
  editingProjectId: string | null;
  editingProjectName: string;
  editingInputRef: React.RefObject<HTMLInputElement | null>;
  onEditingProjectIdChange: (id: string | null) => void;
  onEditingProjectNameChange: (name: string) => void;
  onDeleteChat: (projectId: string, projectName: string) => void;
  onRenameChat: (projectId: string, newName: string) => void;
  onStartEditingProject: (projectId: string, currentName: string) => void;
}

export const SidebarPanel: React.FC<SidebarPanelProps> = ({
  isOpen,
  onOpen,
  onClose,
  isMobile,
  isAdmin,
  onResetChat,
  onShowSettings,
  onShowAccount,
  onShowAdmin,
  editingProjectId,
  editingProjectName,
  editingInputRef,
  onEditingProjectIdChange,
  onEditingProjectNameChange,
  onDeleteChat,
  onRenameChat,
  onStartEditingProject,
}) => {
  const { t } = useTranslation();
  const projects = useProjectsStore((s) => s.projects);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);

  if (!isOpen) {
    return (
      <div
        id="left-toggle-anchor"
        className="flex-shrink-0 ml-2 mr-2 flex flex-col"
        style={{ alignSelf: 'flex-start', marginTop: '20px' }}
      >
        <button
          onClick={onOpen}
          className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
          style={{ backgroundColor: '#669999', color: 'white' }}
        >
          <svg
            className="w-4 h-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2.5}
              d="M9 5l7 7-7 7"
            />
          </svg>
        </button>

        <button
          onClick={() => {
            onResetChat();
          }}
          className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
          style={{
            backgroundColor: 'var(--newchat-bg)',
            color: 'var(--text-heading)',
            marginTop: '8px',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--newchat-bg-hover)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--newchat-bg)';
          }}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <rect
              x="3.5"
              y="3.5"
              width="17"
              height="17"
              rx="3"
              strokeWidth={2}
            />
            <path
              strokeLinecap="round"
              strokeWidth={2}
              d="M12 8v8M8 12h8"
            />
          </svg>
        </button>
      </div>
    );
  }

  return (
    <>
      {/* Mobile overlay backdrop */}
      {isMobile && (
        <div
          onClick={onClose}
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.5)',
            zIndex: 40,
          }}
        />
      )}
      <div
        className="flex-shrink-0 rounded-xl mr-4 overflow-hidden relative sidebar-panel"
        style={{
          backgroundColor: 'var(--bg-input)',
          width: '320px',
          minWidth: '260px',
          maxWidth: '420px',
          ...(isMobile
            ? {
                position: 'fixed',
                top: 0,
                left: 0,
                bottom: 0,
                zIndex: 50,
                marginRight: 0,
                borderRadius: 0,
                maxWidth: '85vw',
              }
            : {}),
        }}
      >
        <div className="p-6 h-full flex flex-col">
          <div className="flex items-center justify-between mb-6">
            <h3
              className="text-13 font-medium"
              style={{ color: 'var(--text-heading)' }}
            >
              {t('unfold.sections.navigation')}
            </h3>
            <button
              onClick={onClose}
              className="p-2 rounded-lg transition-colors"
              style={{
                backgroundColor: 'var(--white-10)',
                color: 'var(--text-secondary-strong)',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                  'var(--white-20)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                  'var(--white-10)';
              }}
            >
              <svg
                className="w-4 h-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M15 19l-7-7 7-7"
                />
              </svg>
            </button>
          </div>

          {/* New Chat action */}
          <div className="mb-3">
            <button
              type="button"
              aria-label={t('unfold.sections.newChat')}
              className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
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
              onClick={() => {
                onResetChat();
                if (isMobile) {
                  onClose();
                }
              }}
            >
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
              >
                <rect
                  x="3.5"
                  y="3.5"
                  width="17"
                  height="17"
                  rx="3"
                  strokeWidth={2}
                />
                <path
                  strokeLinecap="round"
                  strokeWidth={2}
                  d="M12 8v8M8 12h8"
                />
              </svg>
              <span
                className="text-13 font-medium"
                style={{ color: 'var(--text-heading)' }}
              >
                {t('unfold.sections.newChat')}
              </span>
            </button>
          </div>

          {/* Scrollable Chats section */}
          <div
            className="mb-4 flex-1 min-h-0 overflow-y-auto"
            style={{ scrollbarGutter: 'stable' }}
          >
            <div
              className="text-[11px] uppercase tracking-wide mb-2"
              style={{ color: 'var(--text-tertiary-weak)' }}
            >
              {t('unfold.sections.chats')}
            </div>

            {projects.length === 0 ? (
              <div
                className="text-xs"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                {t('unfold.sections.noChatsYet')}
              </div>
            ) : (
              <div className="space-y-1">
                {projects.map((project) => (
                  <div
                    key={project.id}
                    className="px-2 py-1 rounded text-xs flex items-center gap-2 group cursor-pointer"
                    title={project.displayName}
                    onClick={() => {
                      if (editingProjectId !== project.id) {
                        setCurrentProject(project);
                        if (isMobile) {
                          onClose();
                        }
                      }
                    }}
                    style={{
                      backgroundColor:
                        currentProject?.id === project.id
                          ? 'var(--white-10)'
                          : 'transparent',
                    }}
                    onMouseEnter={(e) => {
                      if (
                        currentProject?.id !== project.id &&
                        editingProjectId !== project.id
                      )
                        (e.currentTarget as HTMLDivElement).style.backgroundColor =
                          'var(--white-10)';
                    }}
                    onMouseLeave={(e) => {
                      if (
                        currentProject?.id !== project.id &&
                        editingProjectId !== project.id
                      )
                        (e.currentTarget as HTMLDivElement).style.backgroundColor =
                          'transparent';
                    }}
                  >
                    {editingProjectId === project.id ? (
                      <>
                        <input
                          ref={editingInputRef}
                          type="text"
                          value={editingProjectName}
                          onChange={(e) =>
                            onEditingProjectNameChange(e.target.value)
                          }
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              onRenameChat(project.id, editingProjectName);
                            } else if (e.key === 'Escape') {
                              onEditingProjectIdChange(null);
                              onEditingProjectNameChange('');
                            }
                          }}
                          onBlur={() => {
                            onRenameChat(project.id, editingProjectName);
                          }}
                          className="flex-1 px-1 py-0.5 rounded text-xs"
                          style={{
                            backgroundColor: 'var(--bg-input, #1A1A1A)',
                            color: 'var(--text-heading)',
                            border:
                              '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                          }}
                          placeholder={t(
                            'unfold.sections.renameChatPlaceholder',
                          )}
                          onClick={(e) => e.stopPropagation()}
                        />
                      </>
                    ) : (
                      <>
                        <span
                          className="flex-1 truncate"
                          style={{ color: 'var(--text-heading)' }}
                        >
                          {project.displayName}
                        </span>
                        <button
                          className="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded"
                          title={t('unfold.sections.renameChat')}
                          style={{ backgroundColor: 'transparent' }}
                          onMouseEnter={(e) => {
                            (
                              e.currentTarget as HTMLButtonElement
                            ).style.backgroundColor = 'var(--white-10)';
                          }}
                          onMouseLeave={(e) => {
                            (
                              e.currentTarget as HTMLButtonElement
                            ).style.backgroundColor = 'transparent';
                          }}
                          onClick={(e) => {
                            e.stopPropagation();
                            onStartEditingProject(project.id, project.displayName);
                          }}
                        >
                          <svg
                            className="w-3.5 h-3.5"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                          >
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                              d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                            />
                          </svg>
                        </button>
                        <button
                          className="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded"
                          title={t('unfold.sections.deleteChat')}
                          style={{ backgroundColor: 'transparent' }}
                          onMouseEnter={(e) => {
                            (
                              e.currentTarget as HTMLButtonElement
                            ).style.backgroundColor = 'var(--white-10)';
                          }}
                          onMouseLeave={(e) => {
                            (
                              e.currentTarget as HTMLButtonElement
                            ).style.backgroundColor = 'transparent';
                          }}
                          onClick={(e) => {
                            e.stopPropagation();
                            onDeleteChat(project.id, project.displayName);
                          }}
                        >
                          <svg
                            className="w-3.5 h-3.5"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                          >
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                              d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12"
                            />
                          </svg>
                        </button>
                      </>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Bottom settings button */}
          <div className="mt-auto pt-4">
            {isAdmin && (
              <button
                type="button"
                aria-label={t('unfold.sections.adminAccess')}
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
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
                onClick={onShowAdmin}
              >
                <svg
                  className="w-4 h-4"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                >
                  <path
                    d="M12 3l7 3v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-3z"
                    strokeWidth={2}
                  />
                  <path
                    d="M9.5 12.5l1.8 1.8 3.2-3.3"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
                <span
                  className="text-13 font-medium"
                  style={{ color: 'var(--text-heading)' }}
                >
                  {t('unfold.sections.adminAccess')}
                </span>
              </button>
            )}
            <button
              type="button"
              aria-label="Account"
              className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
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
              onClick={onShowAccount}
            >
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                <circle cx="12" cy="7" r="4" />
              </svg>
              <span
                className="text-13 font-medium"
                style={{ color: 'var(--text-heading)' }}
              >
                {t('unfold.sections.account')}
              </span>
            </button>
            <button
              type="button"
              aria-label={t('unfold.sections.settings')}
              className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
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
              onClick={onShowSettings}
            >
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
                <circle cx="12" cy="12" r="3" />
              </svg>
              <span
                className="text-13 font-medium"
                style={{ color: 'var(--text-heading)' }}
              >
                {t('unfold.sections.settings')}
              </span>
            </button>
          </div>
        </div>
      </div>
    </>
  );
};

