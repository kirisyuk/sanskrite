import React, { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useProjectsStore } from '../stores/projectsStore';
import { ArtifactsPanelToolbar } from './ArtifactsPanelToolbar';
import { WorkspacePanel } from './WorkspacePanel';
import { AnalysisPanel } from './AnalysisPanel';
import FindingsPanel from './FindingsPanel';
import { ExperimentalPanel } from './ExperimentalPanel';
import { ContextPanel } from './ContextPanel';
import { PendingActionsPanel } from './PendingActionsPanel';
import { ModelContextPanel } from './ModelContextPanel';

interface RightDrawerProps {
  rightPanelType: 'workspace' | 'artifacts' | 'experimental' | 'analysis' | 'context' | 'pending-actions' | 'model-context' | null;
  viewMode: string;
  showAccount: boolean;
  showSettings: boolean;
  isMobile: boolean;
  drawerWidth: number;
  setDrawerWidth: (width: number) => void;
  closeAllRightPanels: () => void;
  getPanelVisibilityStyles: (visible: boolean) => React.CSSProperties;
  // Workspace panel props
  currentProjectId: string | null;
  addMenuRef: React.RefObject<HTMLDivElement | null>;
  showAddSourceMenu: boolean;
  onShowAddSourceMenuChange: (show: boolean) => void;
  onShowAddTelegramModalChange: (show: boolean) => void;
  onPreviewSource: (source: any) => void;
  onOpenDocument: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string }) => void;
  onExpandWorkspace?: (projectId: string | null) => void;
  // Analysis panel props
  handleAnalyzeEntities: () => Promise<void>;
  handleAnalyzeFacts: () => Promise<void>;
  handleAnalyzeEvents: () => Promise<void>;
  handleAnalyzeMetrics: () => Promise<void>;
  handleAnalyzeDecisions: () => Promise<void>;
  analysisTab: 'user' | 'flow';
  onAnalysisTabChange: (tab: 'user' | 'flow') => void;
  onFirstResultUser: () => void;
  onRiskUpdate: (risk: any) => void;
  onFirstResultDiscussionFlow: () => void;
  onAddTextTask: (task: any) => void;
  onRemoveTextTask: (taskId: string) => void;
  textTasks: any[];
  queuedTasks?: any[];
  onApplyTasks?: (taskIds: string[]) => void;
  textTasksApplying?: boolean;
  // Experimental panel props
  experimentalResponse: any;
  experimentalLoading: boolean;
  // Context panel props
  sectionStarts: any[];
  pinnedInteractions: any[];
  messagesRef: React.RefObject<HTMLDivElement | null>;
  scrollToInteraction: (id: string) => void;
  truncateCenterWords: (s: string, headWords?: number, tailWords?: number) => string;
  onRemoveNote: (interactionId: string, noteIndex: number) => Promise<void>;
}

export const RightDrawer: React.FC<RightDrawerProps> = ({
  rightPanelType,
  viewMode,
  showAccount,
  showSettings,
  isMobile,
  drawerWidth,
  setDrawerWidth,
  closeAllRightPanels,
  getPanelVisibilityStyles,
  currentProjectId,
  onShowAddTelegramModalChange,
  onPreviewSource,
  onOpenDocument,
  onExpandWorkspace,
  handleAnalyzeEntities,
  handleAnalyzeFacts,
  handleAnalyzeEvents,
  handleAnalyzeMetrics,
  handleAnalyzeDecisions,
  analysisTab,
  onAnalysisTabChange,
  onFirstResultUser,
  onRiskUpdate,
  onFirstResultDiscussionFlow,
  onAddTextTask,
  onRemoveTextTask,
  textTasks,
  queuedTasks = [],
  onApplyTasks,
  textTasksApplying = false,
  experimentalResponse,
  experimentalLoading,
  sectionStarts,
  pinnedInteractions,
  messagesRef,
  scrollToInteraction,
  truncateCenterWords,
  onRemoveNote,
}) => {
  const { t } = useTranslation();
  const genaiChat = useGenaiChatStore();
  const drawerRef = useRef<HTMLDivElement>(null);
  
  // Get current project for project type selector (workspace panel only)
  const currentProject = useProjectsStore((s) => 
    rightPanelType === 'workspace' && currentProjectId 
      ? s.projects.find((p) => p.id === currentProjectId) 
      : null
  );
  const updateProject = useProjectsStore((s) => s.updateProject);
  
  // Handle project type change with optimistic update
  const handleProjectTypeChange = React.useCallback(async (newType: "studio" | null) => {
    if (!currentProjectId || currentProjectId === 'local-unfold' || !currentProject) return;
    
    try {
      // updateProject from store already does optimistic update and syncs with server
      await updateProject(currentProjectId, { projectType: newType || undefined });
    } catch (error) {
      console.error('[RightDrawer] Failed to update project type', error);
      // Error handling and rollback is already done in updateProject
    }
  }, [currentProjectId, currentProject, updateProject]);

  // Sync drawer width when resized natively
  React.useEffect(() => {
    const drawer = drawerRef.current;
    if (!drawer) return;
    
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width;
        if (Math.abs(width - drawerWidth) > 1) {
          // Avoid infinite loops
          setDrawerWidth(width);
        }
      }
    });
    
    observer.observe(drawer);
    return () => observer.disconnect();
  }, [drawerWidth, setDrawerWidth]);

  if (
    rightPanelType === null ||
    viewMode === 'graph' ||
    showAccount ||
    showSettings
  ) {
    return null;
  }

  return (
    <>
      {/* Mobile overlay backdrop */}
      {isMobile && (
        <div
          onClick={closeAllRightPanels}
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
        ref={drawerRef}
        className="min-h-0 flex flex-col drawer-resizable"
        style={{
          width: `${drawerWidth}px`,
          minWidth: '300px',
          maxWidth: '800px',
          flexShrink: 0,
          flexGrow: 0,
          position: isMobile ? 'fixed' : 'relative',
          top: isMobile ? 0 : 'auto',
          right: isMobile ? 0 : 'auto',
          bottom: isMobile ? 0 : 'auto',
          height: '100%',
          maxHeight: '100%',
          overflow: 'hidden',
          zIndex: isMobile ? 50 : 'auto',
          backgroundColor: isMobile
            ? 'var(--bg-sidebar)'
            : 'transparent',
          borderLeft: isMobile ? 'none' : '1px solid var(--border-subtle)',
          ...(isMobile
            ? {
                maxWidth: '85vw',
                borderRadius: 0,
              }
            : {}),
        }}
      >
        {/* Resize handle on left edge */}
        {!isMobile && (
          <div
            onMouseDown={(e) => {
              e.preventDefault();
              const startX = e.clientX;
              const startWidth =
                drawerRef.current?.clientWidth || drawerWidth;
              
              const onMove = (ev: MouseEvent) => {
                const delta = startX - ev.clientX; // Handle on left: moving right (ev.clientX > startX) decreases width
                const newWidth = Math.max(
                  300,
                  Math.min(800, startWidth + delta),
                );
                if (drawerRef.current) {
                  drawerRef.current.style.width = `${newWidth}px`;
                  setDrawerWidth(newWidth);
                }
              };
              
              const onUp = () => {
                window.removeEventListener('mousemove', onMove);
                window.removeEventListener('mouseup', onUp);
              };
              
              window.addEventListener('mousemove', onMove);
              window.addEventListener('mouseup', onUp);
            }}
            style={{
              position: 'absolute',
              left: -4,
              top: 0,
              bottom: 0,
              width: '8px',
              cursor: 'col-resize',
              zIndex: 10,
            }}
          />
        )}
        {/* Drawer content */}
        <div
          className="min-h-0 flex flex-col"
          style={{
            background: 'transparent',
            height: '100%',
            overflow: 'hidden',
          }}
        >
          {/* Right panel container - ALWAYS in DOM, never removed */}
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              flex: rightPanelType !== null ? 1 : 0,
              minWidth: rightPanelType !== null ? 'auto' : 0,
              visibility:
                rightPanelType !== null ? 'visible' : 'hidden',
              pointerEvents: rightPanelType !== null ? 'auto' : 'none',
              height: '100%',
              minHeight: 0,
              overflow: 'hidden',
            }}
          >
            {/* Empty spacer matching Chat | Perspectives toggle height - placed before entire panel to shift it down */}
            <div
              className="flex items-center justify-center gap-1 mb-2 w-full"
              role="tablist"
              aria-label="Spacer"
              style={{
                visibility: 'hidden',
                pointerEvents: 'none',
                height: '28px',
              }}
            >
              <div className="px-2 py-1 text-12 rounded" />
              <div className="px-2 py-1 text-12 rounded" />
            </div>
            <div
              className="mt-3 p-3"
              style={{
                display: 'flex',
                flexDirection: 'column',
                flex: 1,
                minHeight: 0,
                overflowY: 'auto',
              }}
            >
              <div
                className="flex items-center justify-between"
                style={{
                  position: 'sticky',
                  top: 0,
                  paddingBottom: 8,
                  marginBottom: 8,
                  zIndex: 1,
                }}
              >
                <div className="flex items-center gap-3 flex-1 min-w-0">
                  <h3
                    className="text-13 font-medium"
                    style={{ color: 'var(--text-heading)' }}
                  >
                    {rightPanelType === 'artifacts'
                      ? 'Workspace Artifacts'
                      : rightPanelType === 'experimental'
                        ? 'Experimental Response'
                        : rightPanelType === 'analysis'
                          ? t('unfold.sections.analysis')
                          : rightPanelType === 'context'
                            ? t('unfold.sections.contextNavigation', {
                                defaultValue: 'Context Navigation',
                              })
                            : rightPanelType === 'pending-actions'
                              ? t('unfold.sections.pendingActions', {
                                  defaultValue: 'Pending Actions',
                                })
                              : rightPanelType === 'model-context'
                                ? 'Context'
                                : 'Workspace'}
                  </h3>
                  
                  {/* Project Type Selector - only for workspace panel */}
                  {rightPanelType === 'workspace' && currentProject && (
                    <div className="flex items-center gap-2">
                      <label className="text-[10px] font-medium" style={{ color: 'var(--text-secondary-strong)' }}>
                        Type:
                      </label>
                      <select
                        value={currentProject.projectType || ''}
                        onChange={(e) => {
                          const value = e.target.value;
                          handleProjectTypeChange(value === 'studio' ? 'studio' : null);
                        }}
                        className="px-2 py-1 rounded text-[11px] transition-colors"
                        style={{
                          backgroundColor: 'var(--bg-input, #1A1A1A)',
                          color: 'var(--text-heading)',
                          border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                          outline: 'none',
                        }}
                        onMouseEnter={(e) => {
                          e.currentTarget.style.borderColor = 'var(--border-input-hover, rgba(255, 255, 255, 0.2))';
                        }}
                        onMouseLeave={(e) => {
                          e.currentTarget.style.borderColor = 'var(--border-input, rgba(255, 255, 255, 0.1))';
                        }}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <option value="">None</option>
                        <option value="studio">Studio</option>
                      </select>
                    </div>
                  )}
                </div>
                
                <button
                  type="button"
                  aria-label="Close right panel"
                  title="Close"
                  className="p-2 rounded-lg transition-colors flex-shrink-0"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--text-secondary-strong)',
                  }}
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
                  onClick={closeAllRightPanels}
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
                      d="M6 6l12 12M18 6L6 18"
                    />
                  </svg>
                </button>
              </div>
              
              {/* Workspace Artifacts toolbar (top) */}
              {rightPanelType === 'artifacts' && (
                <ArtifactsPanelToolbar
                  onAnalyzeEntities={handleAnalyzeEntities}
                  onAnalyzeFacts={handleAnalyzeFacts}
                  onAnalyzeEvents={handleAnalyzeEvents}
                  onAnalyzeMetrics={handleAnalyzeMetrics}
                  onAnalyzeDecisions={handleAnalyzeDecisions}
                />
              )}
              
              {/* Panel content - always render all panels, hide inactive ones */}
              <div
                style={{
                  flex: 1,
                  minHeight: 0,
                  overflowY: 'auto',
                  paddingRight: 4,
                }}
              >
                {/* Workspace Panel */}
                {rightPanelType === 'workspace' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <WorkspacePanel
                      currentProjectId={currentProjectId}
                      onShowAddTelegramModalChange={onShowAddTelegramModalChange}
                      onPreviewSource={onPreviewSource}
                      onOpenDocument={onOpenDocument}
                      onExpandWorkspace={onExpandWorkspace}
                    />
                  </div>
                )}

                {/* Analysis Panel */}
                {rightPanelType === 'analysis' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <AnalysisPanel
                      analysisTab={analysisTab}
                      onAnalysisTabChange={onAnalysisTabChange}
                      onFirstResultUser={onFirstResultUser}
                      onRiskUpdate={onRiskUpdate}
                      onFirstResultDiscussionFlow={onFirstResultDiscussionFlow}
                      onAddTextTask={onAddTextTask}
                      onRemoveTextTask={onRemoveTextTask}
                      textTasks={textTasks}
                    />
                  </div>
                )}

                {/* Workspace Artifacts Panel */}
                {rightPanelType === 'artifacts' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <FindingsPanel projectId={genaiChat.projectId} />
                  </div>
                )}

                {/* Experimental Response Panel */}
                {rightPanelType === 'experimental' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <ExperimentalPanel
                      response={experimentalResponse}
                      loading={experimentalLoading}
                    />
                  </div>
                )}

                {/* Context Navigation Panel */}
                {rightPanelType === 'context' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <ContextPanel
                      sectionStarts={sectionStarts}
                      pinnedInteractions={pinnedInteractions}
                      messagesRef={messagesRef}
                      scrollToInteraction={scrollToInteraction}
                      truncateCenterWords={truncateCenterWords}
                      onRemoveNote={onRemoveNote}
                    />
                  </div>
                )}

                {/* Pending Actions Panel */}
                {rightPanelType === 'pending-actions' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <PendingActionsPanel
                      tasks={queuedTasks}
                      onRemove={onRemoveTextTask}
                      onApply={onApplyTasks || (() => {})}
                      isApplying={textTasksApplying}
                    />
                  </div>
                )}

                {/* Model Context Panel */}
                {rightPanelType === 'model-context' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <ModelContextPanel />
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

