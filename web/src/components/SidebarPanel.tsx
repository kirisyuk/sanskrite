import React from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'react-router-dom';
import { useProjectsStore } from '../stores/projectsStore';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import { api } from '../lib/api';
import { isArtifact, type Artifact } from '../types/artifact';
import type { CoreDoc, Project } from '../types/generated';
import type { ProjectScenario } from '../types/projectScenario';

interface SidebarPanelProps {
  isOpen: boolean;
  onOpen: () => void;
  onClose: () => void;
  isMobile: boolean;
  isAdmin: boolean;
  onResetChat: () => void;
  onShowWorkspace?: () => void;
  onShowSettings: () => void;
  onShowAccount: () => void;
  onShowAdmin: () => void;
  onCloseAllModals?: () => void;
  editingProjectId: string | null;
  editingProjectName: string;
  editingInputRef: React.RefObject<HTMLInputElement | null>;
  onEditingProjectIdChange: (id: string | null) => void;
  onEditingProjectNameChange: (name: string) => void;
  onDeleteChat: (projectId: string, projectName: string) => void;
  onRenameChat: (projectId: string, newName: string) => void;
  onStartEditingProject: (projectId: string, currentName: string) => void;
  currentMode?: 'chat' | 'horizon' | 'perspectives' | 'models' | 'pages' | 'scenarios';
  projectId?: string | null;
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
  onSelectJob?: (taskId: string | null) => void;
  selectedJobTaskId?: string | null;
  selectedScenarioId?: string | null;
  onSelectScenario?: (scenarioId: string | null) => void;
  onSelectLens?: (lensId: string | null) => void;
  selectedLensId?: string | null;
  onNewLens?: () => void; // Optional: callback for creating new lens
  onProjectSelected?: () => void;
  onOpenWorldMap?: () => void;
  isWorldMapActive?: boolean;
}

type SidebarDensity = 'comfortable' | 'compact';

// Helper functions moved outside component to avoid recreation
const getDaysSinceLastInteraction = (project: Project): number => {
  const now = new Date();
  const projectDate = project.lastInteractionAt 
    ? new Date(project.lastInteractionAt) 
    : new Date(project.createdAt);
  const diffTime = Math.abs(now.getTime() - projectDate.getTime());
  const diffDays = Math.floor(diffTime / (1000 * 60 * 60 * 24));
  return diffDays;
};

const formatTimeAgo = (days: number): string => {
  if (days < 14) {
    return `${days}d`;
  } else if (days < 30) {
    const weeks = Math.floor(days / 7);
    return `${weeks}w`;
  } else {
    const months = Math.floor(days / 30);
    return `${months}m`;
  }
};

const safeLower = (v: unknown) => String(v ?? '').toLowerCase().trim();

// Memoized project row component moved outside to prevent unnecessary re-renders and recreation
const ProjectRow = React.memo<{
  project: Project;
  isDeleting: boolean;
  isSelected: boolean;
  isEditing: boolean;
  editingProjectName: string;
  editingInputRef: React.RefObject<HTMLInputElement | null>;
  onSelect: () => void;
  onStartEditing: () => void;
  onDelete: () => void;
  onRename: (newName: string) => void;
  onCancelEditing: () => void;
  onEditingNameChange: (name: string) => void;
  onCreateLens?: () => void; // Optional: for creating new lens (horizon mode only)
  t: any; // Pass t function as prop
  currentMode?: 'chat' | 'horizon' | 'perspectives' | 'models' | 'pages' | 'scenarios';
}>(({ 
  project, 
  isDeleting, 
  isSelected, 
  isEditing, 
  editingProjectName,
  editingInputRef,
  onSelect,
  onStartEditing,
  onDelete,
  onRename,
  onCancelEditing,
  onEditingNameChange,
  onCreateLens,
  t,
  currentMode,
}) => {
  const daysAgo = getDaysSinceLastInteraction(project);
  const timeAgoLabel = daysAgo > 0 ? formatTimeAgo(daysAgo) : null;
  return (
    <div
      className="px-2 py-1 rounded text-xs flex items-center gap-2 group cursor-pointer relative"
      title={project.displayName}
      onClick={() => {
        if (!isEditing && !isDeleting) {
          onSelect();
        }
      }}
      style={{
        backgroundColor: isSelected ? 'var(--white-10)' : 'transparent',
        opacity: isDeleting ? 0.5 : 1,
        cursor: isDeleting ? 'default' : 'pointer',
      }}
      onMouseEnter={(e) => {
        if (!isSelected && !isEditing && !isDeleting) {
          (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)';
        }
      }}
      onMouseLeave={(e) => {
        if (!isSelected && !isEditing) {
          (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent';
        }
      }}
    >
      {isEditing ? (
        <>
          <input
            ref={editingInputRef}
            type="text"
            value={editingProjectName}
            onChange={(e) => {
              // Update local state for editing
              onEditingNameChange(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                onRename(editingProjectName);
              } else if (e.key === 'Escape') {
                onCancelEditing();
              }
            }}
            onBlur={() => {
              onRename(editingProjectName);
            }}
            className="flex-1 px-1 py-0.5 rounded text-xs"
            style={{
              backgroundColor: 'var(--bg-input, #1A1A1A)',
              color: 'var(--text-heading)',
              border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
            }}
            placeholder={currentMode === 'horizon' ? 'Project name...' : t('unfold.sections.renameChatPlaceholder')}
            onClick={(e) => e.stopPropagation()}
          />
        </>
      ) : (
        <>
          {/* Project type icons */}
          {project.projectType === 'studio' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#40E0D0', // Turquoise
                color: '#000',
              }}
              title="Studio Project"
            >
              S
            </div>
          )}
          {project.projectType === 'personal' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#9B59B6', // Purple
                color: '#FFF',
              }}
              title="Personal Project"
            >
              P
            </div>
          )}
          {project.projectType === 'team' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#3498DB', // Blue
                color: '#FFF',
              }}
              title="Team Project"
            >
              T
            </div>
          )}
          {project.projectType === 'research' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#E67E22', // Orange
                color: '#FFF',
              }}
              title="Research Project"
            >
              R
            </div>
          )}
          {project.projectType === 'learn' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#2ECC71', // Green
                color: '#FFF',
              }}
              title="Learn Project"
            >
              L
            </div>
          )}
          {project.projectType === 'writing' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#F39C12', // Orange/Yellow
                color: '#000',
              }}
              title="Writing Project"
            >
              W
            </div>
          )}
          {project.projectType === 'horizon' && (
            <div
              className="flex-shrink-0 w-4 h-4 rounded-sm flex items-center justify-center text-[10px] font-bold"
              style={{
                backgroundColor: '#E74C3C', // Red
                color: '#FFF',
              }}
              title="Horizon Project"
            >
              H
            </div>
          )}
          <span
            className="flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap transition-all duration-200 group-hover:pr-14"
            style={{
              color: isDeleting ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
              textDecoration: isDeleting ? 'line-through' : 'none',
            }}
          >
            {project.displayName}
          </span>
          {!isDeleting && (
            <>
              {timeAgoLabel && (
                <span
                  className="text-[10px] px-1.5 py-0.5 rounded flex-shrink-0 group-hover:opacity-0 group-hover:pointer-events-none transition-opacity"
                  style={{
                    color: 'var(--text-tertiary-weak)',
                    backgroundColor: 'var(--white-5)',
                  }}
                >
                  {timeAgoLabel}
                </span>
              )}
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none group-hover:pointer-events-auto z-10">
                {/* Create Lens button - only for horizon projects in horizon mode */}
                {currentMode === 'horizon' && project.projectType === 'horizon' && onCreateLens && (
                  <button
                    className="p-1 rounded"
                    title="Create New Lens"
                    style={{ backgroundColor: 'transparent' }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                    }}
                    onClick={(e) => {
                      e.stopPropagation();
                      // Ensure project is selected before creating lens
                      if (!isSelected) {
                        onSelect();
                      }
                      onCreateLens();
                    }}
                  >
                    <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={2}
                        d="M12 4v16m8-8H4"
                      />
                    </svg>
                  </button>
                )}
                <button
                  className="p-1 rounded"
                  title={currentMode === 'horizon' ? 'Rename Project' : t('unfold.sections.renameChat')}
                  style={{ backgroundColor: 'transparent' }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                  }}
                  onClick={(e) => {
                    e.stopPropagation();
                    onStartEditing();
                  }}
                >
                  <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                    />
                  </svg>
                </button>
                <button
                  className="p-1 rounded"
                  title={currentMode === 'horizon' ? 'Delete Project' : t('unfold.sections.deleteChat')}
                  style={{ backgroundColor: 'transparent' }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                  }}
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete();
                  }}
                >
                  <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12"
                    />
                  </svg>
                </button>
              </div>
            </>
          )}
        </>
      )}
    </div>
  );
}, (prevProps, nextProps) => {
  // Custom comparison to prevent re-renders when unrelated projects change
  // Only compare editingProjectName if the project is being edited
  const editingNameEqual = prevProps.isEditing && nextProps.isEditing
    ? prevProps.editingProjectName === nextProps.editingProjectName
    : !prevProps.isEditing && !nextProps.isEditing;
  
  return (
    prevProps.project.id === nextProps.project.id &&
    prevProps.project.displayName === nextProps.project.displayName &&
    prevProps.project.lastInteractionAt === nextProps.project.lastInteractionAt &&
    prevProps.project.createdAt === nextProps.project.createdAt &&
    prevProps.project.projectType === nextProps.project.projectType &&
    prevProps.isDeleting === nextProps.isDeleting &&
    prevProps.isSelected === nextProps.isSelected &&
    prevProps.isEditing === nextProps.isEditing &&
    editingNameEqual &&
    prevProps.onCreateLens === nextProps.onCreateLens &&
    prevProps.t === nextProps.t // Compare translation function ref
  );
});

export const SidebarPanel: React.FC<SidebarPanelProps> = ({
  isOpen,
  onOpen,
  onClose,
  isMobile,
  isAdmin,
  onResetChat,
  onShowWorkspace,
  onShowSettings,
  onShowAccount,
  onShowAdmin,
  onCloseAllModals,
  editingProjectId,
  editingProjectName,
  editingInputRef,
  onEditingProjectIdChange,
  onEditingProjectNameChange,
  onDeleteChat,
  onRenameChat,
  onStartEditingProject,
  currentMode,
  projectId,
  onOpenDocument,
  onSelectJob,
  selectedJobTaskId,
  selectedScenarioId,
  onSelectScenario,
  onSelectLens,
  selectedLensId,
  onNewLens,
  onProjectSelected,
  onOpenWorldMap,
  isWorldMapActive,
}) => {
  const { t } = useTranslation();
  const location = useLocation();

  const isHorizonPage = location.pathname === '/horizon';
  const isHorizonMode = currentMode === 'horizon' || isHorizonPage;
  const isPagesMode = currentMode === 'pages';
  const isScenariosMode = currentMode === 'scenarios';
  const isModernSidebarMode = !isPagesMode && !isScenariosMode; // modernize Horizon + discovery (default/chat)
  const isMac = React.useMemo(() => {
    try {
      return /mac/i.test((navigator as any)?.platform || '');
    } catch {
      return false;
    }
  }, []);
  
  // PERFORMANCE: Subscribe to projects array directly, but use shallow comparison
  // We need to track displayName changes for renamed projects
  const projects = useProjectsStore((s) => s.projects);
  const projectsLoading = useProjectsStore((s) => s.loading);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const deletingProjectIdsArray = useProjectsStore((s) => s.deletingProjectIds);
  const deletingProjectIds = React.useMemo(() => new Set(deletingProjectIdsArray), [deletingProjectIdsArray]);
  
  // Filter projects by type based on current route
  // On /horizon page, show only horizon projects; otherwise show all except horizon
  const filteredProjects = React.useMemo(() => {
    return projects.filter((p) => {
      if (isHorizonPage) {
        return p.projectType === 'horizon';
      }
      // On other pages, exclude horizon projects (they belong to /horizon page)
      return p.projectType !== 'horizon';
    });
  }, [projects, isHorizonPage]);
  
  // Memoize projects array with a stable reference to prevent unnecessary re-renders
  // Create a signature string that includes displayName and projectType to detect changes
  const projectsSignature = React.useMemo(() => 
    filteredProjects.map((p) => `${p.id}:${p.displayName}:${p.lastInteractionAt || p.createdAt}:${p.projectType || ''}`).join(','),
    [filteredProjects]
  );
  
  // Memoized projects array - will update when signature changes
  const memoizedProjects = React.useMemo(() => filteredProjects, [projectsSignature]);

  // Sidebar preferences (pins + density) stored locally
  const [sidebarDensity, setSidebarDensity] = React.useState<SidebarDensity>(() => {
    try {
      const raw = localStorage.getItem('ui.sidebar.density');
      return raw === 'compact' ? 'compact' : 'comfortable';
    } catch {
      return 'comfortable';
    }
  });
  React.useEffect(() => {
    try {
      localStorage.setItem('ui.sidebar.density', sidebarDensity);
    } catch {
      // ignore
    }
  }, [sidebarDensity]);

  const [pinnedProjectIds, setPinnedProjectIds] = React.useState<Set<string>>(() => {
    try {
      const raw = localStorage.getItem('ui.sidebar.pinnedProjects');
      const ids = raw ? (JSON.parse(raw) as unknown) : [];
      const arr = Array.isArray(ids) ? ids : [];
      return new Set(arr.map((x) => String(x || '').trim()).filter(Boolean));
    } catch {
      return new Set();
    }
  });
  React.useEffect(() => {
    try {
      localStorage.setItem('ui.sidebar.pinnedProjects', JSON.stringify(Array.from(pinnedProjectIds)));
    } catch {
      // ignore
    }
  }, [pinnedProjectIds]);

  const togglePinProject = React.useCallback((projectId: string) => {
    const pid = String(projectId || '').trim();
    if (!pid) return;
    setPinnedProjectIds((prev) => {
      const next = new Set(prev);
      if (next.has(pid)) next.delete(pid);
      else next.add(pid);
      return next;
    });
  }, []);

  // Modern: projects list (pinned + recent)
  const sortedProjects = React.useMemo(() => {
    const list = [...memoizedProjects];
    const ts = (p: Project) => {
      const t = p.lastInteractionAt || p.createdAt;
      const n = new Date(t).getTime();
      return Number.isFinite(n) ? n : 0;
    };
    list.sort((a, b) => ts(b) - ts(a));
    return list;
  }, [memoizedProjects]);

  const pinnedProjects = React.useMemo(() => {
    if (pinnedProjectIds.size === 0) return [];
    return sortedProjects.filter((p) => pinnedProjectIds.has(p.id));
  }, [sortedProjects, pinnedProjectIds]);

  const recentProjects = React.useMemo(() => {
    if (pinnedProjectIds.size === 0) return sortedProjects;
    return sortedProjects.filter((p) => !pinnedProjectIds.has(p.id));
  }, [sortedProjects, pinnedProjectIds]);

  // Modern navigation: primary tab inside sidebar (projects vs nested entities)
  const [sidebarMainTab, setSidebarMainTab] = React.useState<'projects' | 'details'>(() => {
    try {
      const raw = localStorage.getItem('ui.sidebar.mainTab');
      return raw === 'details' ? 'details' : 'projects';
    } catch {
      return 'projects';
    }
  });
  React.useEffect(() => {
    try {
      localStorage.setItem('ui.sidebar.mainTab', sidebarMainTab);
    } catch {
      // ignore
    }
  }, [sidebarMainTab]);

  React.useEffect(() => {
    // Horizon: if a project is selected, default to details view (lenses) since that's the primary workspace.
    // Discovery: DO NOT auto-switch to details (which currently shows Jobs) when selecting a chat/project.
    if (!isModernSidebarMode) return;
    if (isHorizonMode) {
      if (currentProject?.id && currentProject.id !== 'local-unfold') setSidebarMainTab('details');
      else setSidebarMainTab('projects');
    } else {
      setSidebarMainTab('projects');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentProject?.id, isModernSidebarMode, isHorizonMode]);

  const hasSelectedProject = Boolean(currentProject?.id && currentProject.id !== 'local-unfold');
  const isDetailsView = sidebarMainTab === 'details' && hasSelectedProject;
  const isProjectsView = !isDetailsView;

  // Projects list virtualization (only for long lists)
  const projectsListRef = React.useRef<HTMLDivElement | null>(null);
  const projectsScrollRafRef = React.useRef<number | null>(null);
  const [projectsScrollTop, setProjectsScrollTop] = React.useState(0);
  const [projectsViewportH, setProjectsViewportH] = React.useState(360);

  React.useEffect(() => {
    const el = projectsListRef.current;
    if (!el) return;

    const measure = () => {
      setProjectsViewportH(el.clientHeight || 360);
    };

    measure();
    const ro = new ResizeObserver(() => measure());
    ro.observe(el);

    return () => ro.disconnect();
  }, []);

  const onProjectsScroll = React.useCallback((e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    if (projectsScrollRafRef.current !== null) return;
    projectsScrollRafRef.current = window.requestAnimationFrame(() => {
      projectsScrollRafRef.current = null;
      setProjectsScrollTop(el.scrollTop || 0);
    });
  }, []);

  const projectRowH = sidebarDensity === 'compact' ? 30 : 36;
  const shouldVirtualizeProjects = recentProjects.length >= 80;
  const virtualProjects = React.useMemo(() => {
    if (!shouldVirtualizeProjects) {
      return {
        padTop: 0,
        padBottom: 0,
        slice: recentProjects,
        totalH: recentProjects.length * projectRowH,
      };
    }
    const overscan = 10;
    const start = Math.max(0, Math.floor(projectsScrollTop / projectRowH) - overscan);
    const visible = Math.ceil(projectsViewportH / projectRowH) + overscan * 2;
    const end = Math.min(recentProjects.length, start + visible);
    const padTop = start * projectRowH;
    const padBottom = Math.max(0, (recentProjects.length - end) * projectRowH);
    return {
      padTop,
      padBottom,
      slice: recentProjects.slice(start, end),
      totalH: recentProjects.length * projectRowH,
      start,
      end,
    };
  }, [shouldVirtualizeProjects, recentProjects, projectsScrollTop, projectsViewportH, projectRowH]);
  
  // Agent jobs state
  const [agentJobs, setAgentJobs] = React.useState<any[]>([]);
  const [agentJobsLoading, setAgentJobsLoading] = React.useState(false);
  const [deletingJobIds, setDeletingJobIds] = React.useState<Set<string>>(new Set());
  // Track which project the agentJobs belong to, to prevent showing wrong agents during project switch
  const [agentJobsProjectId, setAgentJobsProjectId] = React.useState<string | null>(null);

  // Horizon lenses state
  const [lenses, setLenses] = React.useState<Array<{
    id: string;
    tenantId?: string;
    projectId?: string;
    name: string;
    status: string;
    scope: string[];
    createdAt?: string;
    updatedAt: string;
    viewConfig?: Record<string, any>;
    alertConfig?: Record<string, any>;
    scopeFragments?: Record<string, string>;
    resolvedQueries?: Record<string, string[]>;
    lastResolved?: string;
  }>>([]);
  const [lensesLoading, setLensesLoading] = React.useState(false);
  const [deletingLensIds, setDeletingLensIds] = React.useState<Set<string>>(new Set());
  const [editingLensId, setEditingLensId] = React.useState<string | null>(null);
  const [editingLensName, setEditingLensName] = React.useState('');
  const editingLensInputRef = React.useRef<HTMLInputElement | null>(null);
  // Track which project the lenses belong to
  const [lensesProjectId, setLensesProjectId] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (editingLensId && editingLensInputRef.current) {
      editingLensInputRef.current.focus();
      editingLensInputRef.current.select();
    }
  }, [editingLensId]);

  // Group projects by date (Today, Yesterday, Later)
  const groupedProjects = React.useMemo(() => {
    if (!memoizedProjects || memoizedProjects.length === 0) {
      return { today: [], yesterday: [], later: [] };
    }

    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(today);
    yesterday.setDate(yesterday.getDate() - 1);

    // Sort projects by lastInteractionAt (if exists) or createdAt
    const sortedProjects = [...memoizedProjects].sort((a, b) => {
      const getTimestamp = (project: typeof projects[0]) => {
        if (project.lastInteractionAt) {
          return new Date(project.lastInteractionAt).getTime();
        }
        return new Date(project.createdAt).getTime();
      };
      return getTimestamp(b) - getTimestamp(a); // Newest first
    });

    const grouped = { today: [] as typeof memoizedProjects, yesterday: [] as typeof memoizedProjects, later: [] as typeof memoizedProjects };

    for (const project of sortedProjects) {
      const getDate = (project: typeof projects[0]) => {
        if (project.lastInteractionAt) {
          return new Date(project.lastInteractionAt);
        }
        return new Date(project.createdAt);
      };

      const projectDate = getDate(project);
      const projectDateOnly = new Date(projectDate.getFullYear(), projectDate.getMonth(), projectDate.getDate());

      if (projectDateOnly.getTime() === today.getTime()) {
        grouped.today.push(project);
      } else if (projectDateOnly.getTime() === yesterday.getTime()) {
        grouped.yesterday.push(project);
      } else {
        grouped.later.push(project);
      }
    }

    return grouped;
  }, [memoizedProjects]);
  
  // Helper functions moved outside component

  // Wrapper function to render project row with proper callbacks
  // IMPORTANT: Only pass editingProjectName to the project being edited to prevent unnecessary re-renders
  const renderProjectRow = React.useCallback((project: Project) => {
    const isDeleting = deletingProjectIds.has(project.id);
    const isSelected = currentProject?.id === project.id;
    const isEditing = editingProjectId === project.id;
    
    return (
      <ProjectRow
        key={project.id}
        project={project}
        isDeleting={isDeleting}
        isSelected={isSelected}
        isEditing={isEditing}
        editingProjectName={isEditing ? editingProjectName : ''}
        editingInputRef={editingInputRef}
        onEditingNameChange={onEditingProjectNameChange}
        t={t}
        currentMode={currentMode}
        onSelect={() => {
          console.log('[SidebarPanel] Clicked on project', {
            projectId: project.id,
            projectName: project.displayName,
            currentProjectId: currentProject?.id,
          });
          onProjectSelected?.();
          onCloseAllModals?.();
          setCurrentProject(project);
          if (isMobile) {
            onClose();
          }
        }}
        onStartEditing={() => {
          onStartEditingProject(project.id, project.displayName);
        }}
        onDelete={() => {
          onDeleteChat(project.id, project.displayName);
        }}
        onRename={(newName: string) => {
          onRenameChat(project.id, newName);
        }}
        onCancelEditing={() => {
          onEditingProjectIdChange(null);
          onEditingProjectNameChange('');
        }}
        onCreateLens={currentMode === 'horizon' && project.projectType === 'horizon' ? onNewLens : undefined}
      />
    );
  }, [deletingProjectIds, currentProject, editingProjectId, editingProjectName, editingInputRef, onProjectSelected, onCloseAllModals, setCurrentProject, isMobile, onClose, onStartEditingProject, onDeleteChat, onRenameChat, onEditingProjectIdChange, onEditingProjectNameChange, currentMode, onNewLens]);

  // Modern row renderer (projects) - larger, calmer visual style
  const renderModernProjectRow = React.useCallback((project: Project) => {
    const isDeleting = deletingProjectIds.has(project.id);
    const isSelected = currentProject?.id === project.id;
    const isEditing = editingProjectId === project.id;
    const daysAgo = getDaysSinceLastInteraction(project);
    const timeAgoLabel = daysAgo > 0 ? formatTimeAgo(daysAgo) : null;
    const typeLabel = String(project.projectType || '').trim();
    const typePill = typeLabel ? typeLabel.toUpperCase() : undefined;
    const rowClass = `sidebar-row has-actions ${sidebarDensity === 'compact' ? 'compact' : ''} ${isSelected ? 'is-selected' : ''}`;

    return (
      <div
        key={project.id}
        className={rowClass}
        style={{ opacity: isDeleting ? 0.6 : 1, cursor: isDeleting ? 'default' : 'pointer' }}
        onClick={() => {
          if (isDeleting) return;
          if (isEditing) return;
          // If clicking the already selected project, just switch to details (lenses/jobs).
          // This fixes the "Back -> click same project does nothing" UX, since currentProject.id doesn't change.
          if (isSelected) {
            onProjectSelected?.();
            if (isHorizonMode) setSidebarMainTab('details');
            return;
          }
          onProjectSelected?.();
          onCloseAllModals?.();
          setCurrentProject(project);
          // Immediately switch to details view; don't wait for currentProject.id effect.
          if (isHorizonMode) setSidebarMainTab('details');
          if (isMobile) onClose();
        }}
        title={project.displayName}
      >
        <div
          style={{
            width: 18,
            height: 18,
            borderRadius: 6,
            border: '1px solid var(--border-subtle)',
            background: 'var(--white-06)',
            color: 'var(--text-secondary-strong)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 10,
            fontWeight: 800,
            flexShrink: 0,
          }}
          title={typePill}
        >
          {(typePill || 'P').slice(0, 1)}
        </div>

        {isEditing ? (
          <input
            ref={editingInputRef}
            type="text"
            value={editingProjectName}
            onChange={(e) => onEditingProjectNameChange(e.target.value)}
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
            style={{
              flex: 1,
              minWidth: 0,
              background: 'transparent',
              border: '1px solid var(--border-subtle)',
              borderRadius: 10,
              padding: '6px 10px',
              color: 'var(--text-heading)',
              outline: 'none',
              fontSize: 12,
            }}
            onClick={(e) => e.stopPropagation()}
          />
        ) : (
          <div className="sidebar-row-title" style={{ textDecoration: isDeleting ? 'line-through' : 'none' }}>
            {project.displayName || 'Untitled Project'}
          </div>
        )}

        {timeAgoLabel && !isEditing && (
          <div className="sidebar-row-subtitle sidebar-timepill" title="Last active">
            {timeAgoLabel}
          </div>
        )}

        {!isEditing && !isDeleting && (
          <div className="sidebar-row-actions">
            <button
              type="button"
              className="sidebar-icon-btn"
              title={pinnedProjectIds.has(project.id) ? 'Unpin' : 'Pin'}
              onClick={(e) => {
                e.stopPropagation();
                togglePinProject(project.id);
              }}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 17v5" />
                <path d="M9 3h6l1 7-2 2v5H10v-5L8 10l1-7Z" />
              </svg>
            </button>
            {isHorizonMode && !isModernSidebarMode && project.projectType === 'horizon' && onNewLens && (
              <button
                type="button"
                className="sidebar-icon-btn"
                title="Create new lens"
                onClick={(e) => {
                  e.stopPropagation();
                  if (!isSelected) setCurrentProject(project);
                  onNewLens();
                }}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 5v14" /><path d="M5 12h14" />
                </svg>
              </button>
            )}
            <button
              type="button"
              className="sidebar-icon-btn"
              title={isHorizonMode ? 'Rename project' : t('unfold.sections.renameChat')}
              onClick={(e) => {
                e.stopPropagation();
                onStartEditingProject(project.id, project.displayName);
              }}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 20h9" />
                <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5Z" />
              </svg>
            </button>
            <button
              type="button"
              className="sidebar-icon-btn"
              title={isHorizonMode ? 'Delete project' : t('unfold.sections.deleteChat')}
              onClick={(e) => {
                e.stopPropagation();
                onDeleteChat(project.id, project.displayName);
              }}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3 6h18" />
                <path d="M8 6V4h8v2" />
                <path d="M19 6l-1 14H6L5 6" />
                <path d="M10 11v6" />
                <path d="M14 11v6" />
              </svg>
            </button>
          </div>
        )}
      </div>
    );
  }, [
    deletingProjectIds,
    currentProject?.id,
    editingProjectId,
    editingProjectName,
    editingInputRef,
    sidebarDensity,
    onCloseAllModals,
    onProjectSelected,
    setCurrentProject,
    isMobile,
    onClose,
    onEditingProjectIdChange,
    onEditingProjectNameChange,
    onDeleteChat,
    onRenameChat,
    onStartEditingProject,
    pinnedProjectIds,
    togglePinProject,
    isHorizonMode,
    onNewLens,
    t,
  ]);

  // Load agent jobs for the current project (only on project selection, updates via WebSocket)
  React.useEffect(() => {
    // Immediately clear agent jobs when project changes (synchronously)
    if (!currentProject?.id || currentProject.id === 'local-unfold') {
      setAgentJobs([]);
      setAgentJobsProjectId(null);
      return;
    }

    // Clear agent jobs immediately if switching to a different project
    if (agentJobsProjectId !== currentProject.id) {
      setAgentJobs([]);
      setAgentJobsProjectId(null);
    }

    const loadJobs = async () => {
      // Capture the project ID at the start of the async operation
      const projectIdForLoading = currentProject.id;
      setAgentJobsLoading(true);
      try {
        const result = await api.listAgentJobs(projectIdForLoading, { limit: 50 });
        // Only set agent jobs if we're still on the same project (prevent race conditions)
        if (currentProject?.id === projectIdForLoading) {
          setAgentJobs(result.jobs || []);
          setAgentJobsProjectId(projectIdForLoading);
        }
      } catch (err) {
        console.error('[SidebarPanel] Failed to load agent jobs', err);
        // Only clear if we're still on the same project
        if (currentProject?.id === projectIdForLoading) {
          setAgentJobs([]);
          setAgentJobsProjectId(null);
        }
      } finally {
        setAgentJobsLoading(false);
      }
    };

    loadJobs();
  }, [currentProject?.id]);

  // Function to load lenses (can be called manually)
  const loadLenses = React.useCallback(async () => {
    // Only load lenses in horizon mode
    if (currentMode !== 'horizon') {
      setLenses([]);
      setLensesProjectId(null);
      setEditingLensId(null);
      setEditingLensName('');
      setDeletingLensIds(new Set());
      return;
    }

    // Immediately clear lenses when project changes (synchronously)
    if (!currentProject?.id || currentProject.id === 'local-unfold') {
      setLenses([]);
      setLensesProjectId(null);
      setEditingLensId(null);
      setEditingLensName('');
      setDeletingLensIds(new Set());
      return;
    }

    // Clear lenses immediately if switching to a different project
    if (lensesProjectId !== currentProject.id) {
      setLenses([]);
      setLensesProjectId(null);
      setEditingLensId(null);
      setEditingLensName('');
      setDeletingLensIds(new Set());
    }

    const projectIdForLoading = currentProject.id;
    setLensesLoading(true);
    try {
      const result = await api.listHorizonLenses(projectIdForLoading, { limit: 50 });
      // Only set lenses if we're still on the same project (prevent race conditions)
      if (currentProject?.id === projectIdForLoading) {
        setLenses(result.lenses || []);
        setLensesProjectId(projectIdForLoading);
      }
    } catch (err) {
      console.error('[SidebarPanel] Failed to load lenses', err);
      // Only clear if we're still on the same project
      if (currentProject?.id === projectIdForLoading) {
        setLenses([]);
        setLensesProjectId(null);
      }
    } finally {
      setLensesLoading(false);
    }
  }, [currentProject?.id, currentMode, lensesProjectId]);

  const startEditingLens = React.useCallback((lens: any) => {
    const id = String(lens?.id || '').trim();
    if (!id) return;
    setEditingLensId(id);
    setEditingLensName(String(lens?.name || '').trim());
    window.setTimeout(() => editingLensInputRef.current?.focus(), 0);
  }, []);

  const cancelEditingLens = React.useCallback(() => {
    setEditingLensId(null);
    setEditingLensName('');
  }, []);

  const renameLens = React.useCallback(async (lens: any, nextNameRaw: string) => {
    const id = String(lens?.id || '').trim();
    if (!id) return;
    const nextName = String(nextNameRaw || '').trim();
    const prevName = String(lens?.name || '').trim();
    if (!nextName || nextName === prevName) {
      cancelEditingLens();
      return;
    }
    try {
      // Optimistic update
      setLenses((prev) => prev.map((l) => (l.id === id ? { ...l, name: nextName } : l)));
      let tenantId = String(lens?.tenantId || '').trim();
      let projectId = String(lens?.projectId || '').trim();
      let fullLens: any = lens;
      if (!tenantId || !projectId) {
        fullLens = await api.getHorizonLens(id);
        tenantId = String(fullLens?.tenantId || '').trim();
        projectId = String(fullLens?.projectId || '').trim();
      }
      await api.updateHorizonLens(id, {
        tenantId,
        projectId,
        name: nextName,
        scope: Array.isArray(fullLens?.scope) ? fullLens.scope : [],
        viewConfig: fullLens?.viewConfig,
        alertConfig: fullLens?.alertConfig,
        status: String(fullLens?.status || 'ACTIVE'),
        scopeFragments: fullLens?.scopeFragments,
        resolvedQueries: fullLens?.resolvedQueries,
        lastResolved: fullLens?.lastResolved,
      });
    } catch (e) {
      // Best-effort: revert
      setLenses((prev) => prev.map((l) => (l.id === id ? { ...l, name: prevName } : l)));
    } finally {
      cancelEditingLens();
    }
  }, [cancelEditingLens]);

  const deleteLens = React.useCallback(async (lens: any) => {
    const id = String(lens?.id || '').trim();
    if (!id) return;
    const name = String(lens?.name || 'Untitled Lens');
    if (!window.confirm(`Delete lens "${name}"?`)) return;

    setDeletingLensIds((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });
    try {
      await api.deleteHorizonLens(id);
      setLenses((prev) => prev.filter((l) => l.id !== id));
      if (selectedLensId === id) {
        onSelectLens?.(null);
      }
    } catch {
      // ignore (keep lens)
    } finally {
      setDeletingLensIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }, [onSelectLens, selectedLensId]);

  // Transfer lens state
  const [transferringLens, setTransferringLens] = React.useState<{ id: string; name: string; projectId: string } | null>(null);
  const [transferInProgress, setTransferInProgress] = React.useState(false);

  const openTransferDialog = React.useCallback((lens: any) => {
    const id = String(lens?.id || '').trim();
    const name = String(lens?.name || 'Untitled Lens');
    const pid = String(lens?.projectId || '').trim();
    if (!id) return;
    setTransferringLens({ id, name, projectId: pid });
  }, []);

  const closeTransferDialog = React.useCallback(() => {
    setTransferringLens(null);
  }, []);

  const transferLens = React.useCallback(async (targetProjectId: string) => {
    if (!transferringLens) return;
    const { id, name, projectId: sourceProjectId } = transferringLens;
    if (!targetProjectId || targetProjectId === sourceProjectId) {
      closeTransferDialog();
      return;
    }

    setTransferInProgress(true);
    try {
      await api.transferHorizonLens(id, targetProjectId);
      // Remove lens from current project's list
      setLenses((prev) => prev.filter((l) => l.id !== id));
      if (selectedLensId === id) {
        onSelectLens?.(null);
      }
      closeTransferDialog();
    } catch (err) {
      console.error(`[Sidebar] Failed to transfer lens "${name}":`, err);
      alert(`Failed to transfer lens: ${err instanceof Error ? err.message : 'Unknown error'}`);
    } finally {
      setTransferInProgress(false);
    }
  }, [transferringLens, closeTransferDialog, onSelectLens, selectedLensId]);

  // Command palette (sidebar)
  type PaletteItem =
    | { kind: 'project'; id: string; title: string; subtitle?: string; project: Project }
    | { kind: 'lens'; id: string; title: string; subtitle?: string; lensId: string }
    | { kind: 'job'; id: string; title: string; subtitle?: string; taskId: string }
    | { kind: 'create_project'; id: string; title: string; subtitle?: string; name: string };

  const [paletteOpen, setPaletteOpen] = React.useState(false);
  const [paletteQuery, setPaletteQuery] = React.useState('');
  const [paletteActiveIndex, setPaletteActiveIndex] = React.useState(0);
  const [paletteMode, setPaletteMode] = React.useState<'search' | 'create_project'>('search');
  const paletteInputRef = React.useRef<HTMLInputElement | null>(null);
  const [paletteAnchorRect, setPaletteAnchorRect] = React.useState<{
    left: number;
    top: number;
    width: number;
    height: number;
  } | null>(null);

  const openPalette = React.useCallback(() => {
    setPaletteOpen(true);
    setPaletteMode('search');
    setPaletteQuery('');
    setPaletteActiveIndex(0);
    window.setTimeout(() => paletteInputRef.current?.focus(), 0);
  }, []);

  const openCreateProjectPalette = React.useCallback(() => {
    setPaletteOpen(true);
    setPaletteMode('create_project');
    setPaletteQuery('');
    setPaletteActiveIndex(0);
    window.setTimeout(() => paletteInputRef.current?.focus(), 0);
  }, []);

  const closePalette = React.useCallback(() => {
    setPaletteOpen(false);
    setPaletteMode('search');
    setPaletteQuery('');
    setPaletteActiveIndex(0);
  }, []);

  React.useEffect(() => {
    if (!isOpen) {
      closePalette();
    }
  }, [isOpen, closePalette]);

  React.useEffect(() => {
    if (!paletteOpen || !isModernSidebarMode || !isHorizonMode) {
      setPaletteAnchorRect(null);
      return;
    }

    let rafId: number | null = null;
    const getAnchor = () =>
      document.querySelector<HTMLElement>('[data-horizon-graph-area="true"]');

    const updateAnchorRect = () => {
      const el = getAnchor();
      if (!el) {
        setPaletteAnchorRect(null);
        return;
      }
      const rect = el.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0) {
        setPaletteAnchorRect(null);
        return;
      }
      const next = {
        left: rect.left,
        top: rect.top,
        width: rect.width,
        height: rect.height,
      };
      setPaletteAnchorRect((prev) => {
        if (
          prev &&
          Math.abs(prev.left - next.left) < 0.5 &&
          Math.abs(prev.top - next.top) < 0.5 &&
          Math.abs(prev.width - next.width) < 0.5 &&
          Math.abs(prev.height - next.height) < 0.5
        ) {
          return prev;
        }
        return next;
      });
    };

    const scheduleAnchorRectUpdate = () => {
      if (rafId !== null) return;
      rafId = window.requestAnimationFrame(() => {
        rafId = null;
        updateAnchorRect();
      });
    };

    updateAnchorRect();
    window.addEventListener('resize', scheduleAnchorRectUpdate);
    window.addEventListener('scroll', scheduleAnchorRectUpdate, true);

    const anchor = getAnchor();
    const resizeObserver = anchor ? new ResizeObserver(scheduleAnchorRectUpdate) : null;
    if (anchor && resizeObserver) {
      resizeObserver.observe(anchor);
    }

    return () => {
      if (rafId !== null) window.cancelAnimationFrame(rafId);
      window.removeEventListener('resize', scheduleAnchorRectUpdate);
      window.removeEventListener('scroll', scheduleAnchorRectUpdate, true);
      resizeObserver?.disconnect();
    };
  }, [isHorizonMode, isModernSidebarMode, paletteOpen]);

  // Global hotkey: Ctrl/Cmd + K
  React.useEffect(() => {
    if (!isModernSidebarMode) return;
    const onKeyDown = (e: KeyboardEvent) => {
      const isK = (e.key || '').toLowerCase() === 'k';
      if (!isK) return;
      if (!(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      if (paletteOpen) closePalette();
      else openPalette();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isModernSidebarMode, paletteOpen, openPalette, closePalette]);

  const paletteItems = React.useMemo(() => {
    if (!isModernSidebarMode) return [] as PaletteItem[];
    const q = safeLower(paletteQuery);

    const match = (hay: string) => {
      if (!q) return true;
      return safeLower(hay).includes(q);
    };

    const out: PaletteItem[] = [];

    if (paletteMode === 'create_project') {
      const name = String(paletteQuery || '').trim();
      if (name) {
        out.push({
          kind: 'create_project',
          id: `create_project:${name.toLowerCase()}`,
          title: `Create project “${name}”`,
          subtitle: 'Horizon',
          name,
        });
      }
      return out;
    }

    // Projects (pinned first, then recent)
    const allProjects = [...pinnedProjects, ...recentProjects];
    for (const p of allProjects) {
      const title = p.displayName || 'Untitled Project';
      if (!match(`${title} ${p.id}`)) continue;
      out.push({
        kind: 'project',
        id: `project:${p.id}`,
        title,
        subtitle: p.projectType ? String(p.projectType).toUpperCase() : undefined,
        project: p,
      });
    }

    // Horizon: lenses for current project (already loaded)
    if (isHorizonMode && currentProject?.id && lensesProjectId === currentProject.id) {
      for (const l of lenses) {
        const title = l.name || 'Untitled Lens';
        if (!match(`${title} ${l.id}`)) continue;
        out.push({
          kind: 'lens',
          id: `lens:${l.id}`,
          title,
          subtitle: 'Lens',
          lensId: l.id,
        });
      }
    }

    // Discovery: jobs for current project (already loaded)
    if (!isHorizonMode && currentProject?.id && agentJobsProjectId === currentProject.id) {
      for (const j of agentJobs) {
        const title = j.title || j.userQuery || 'Untitled Job';
        const taskId = j.taskId || j.id;
        if (!match(`${title} ${taskId}`)) continue;
        out.push({
          kind: 'job',
          id: `job:${taskId}`,
          title,
          subtitle: 'Job',
          taskId,
        });
      }
    }

    return out.slice(0, 80);
  }, [
    isModernSidebarMode,
    paletteQuery,
    paletteMode,
    pinnedProjects,
    recentProjects,
    isHorizonMode,
    currentProject?.id,
    lensesProjectId,
    lenses,
    agentJobsProjectId,
    agentJobs,
  ]);

  const createProject = useProjectsStore((s) => s.createProject);
  const updateProject = useProjectsStore((s) => s.updateProject);

  const handleCreateHorizonProject = React.useCallback(() => {
    if (isModernSidebarMode) {
      openCreateProjectPalette();
      return;
    }
    const name = window.prompt('Project name') || '';
    const trimmed = name.trim();
    if (!trimmed) return;
    onCloseAllModals?.();
    createProject({ displayName: trimmed, view: 'horizon' })
      .then((project) => updateProject(project.id, { projectType: 'horizon' })
        .then((updated) => {
          setCurrentProject(updated);
        })
        .catch((e) => {
          console.error('[SidebarPanel] Failed to set projectType=horizon', e);
          setCurrentProject({ ...project, projectType: 'horizon' } as Project);
        }))
      .catch((e) => {
        console.error('[SidebarPanel] Failed to create project', e);
      })
      .finally(() => {
        if (isMobile) onClose();
      });
  }, [createProject, isMobile, isModernSidebarMode, onClose, onCloseAllModals, openCreateProjectPalette, setCurrentProject]);

  const runPaletteSelection = React.useCallback(
    (item: PaletteItem) => {
      if (item.kind === 'project') {
        onCloseAllModals?.();
        setCurrentProject(item.project);
        if (isMobile) onClose();
        closePalette();
        return;
      }
      if (item.kind === 'lens') {
        if (onSelectLens) onSelectLens(item.lensId);
        if (isMobile) onClose();
        closePalette();
        return;
      }
      if (item.kind === 'job') {
        onSelectJob?.(item.taskId);
        if (isMobile) onClose();
        closePalette();
        return;
      }
      if (item.kind === 'create_project') {
        const name = String(item.name || '').trim();
        if (!name) return;
        onCloseAllModals?.();
        createProject({ displayName: name, view: 'horizon' })
          .then((project) => updateProject(project.id, { projectType: 'horizon' })
            .then((updated) => {
              setCurrentProject(updated);
            })
            .catch((e) => {
              console.error('[SidebarPanel] Failed to set projectType=horizon', e);
              setCurrentProject({ ...project, projectType: 'horizon' } as Project);
            }))
          .catch((e) => {
            console.error('[SidebarPanel] Failed to create project', e);
          })
          .finally(() => {
            if (isMobile) onClose();
            closePalette();
          });
        return;
      }
    },
    [closePalette, createProject, isMobile, onClose, onCloseAllModals, onSelectJob, onSelectLens, setCurrentProject],
  );

  // Load lenses for the current project in horizon mode
  React.useEffect(() => {
    loadLenses();
  }, [loadLenses]);

  // Subscribe to Horizon realtime store for assembly status updates
  const assemblyState = useHorizonRealtimeStore((s) => s.assembly);
  
  // Listen for lens creation events to refresh the list
  React.useEffect(() => {
    const handleLensCreated = (event: CustomEvent) => {
      const { projectId: eventProjectId } = event.detail || {};
      // Only refresh if the event is for the current project
      if (eventProjectId && currentProject?.id === eventProjectId && currentMode === 'horizon') {
        console.log('[SidebarPanel] Lens created event received, refreshing lenses list');
        loadLenses();
      }
    };

    window.addEventListener('horizon:lens-created', handleLensCreated as EventListener);
    return () => {
      window.removeEventListener('horizon:lens-created', handleLensCreated as EventListener);
    };
  }, [currentProject?.id, currentMode, loadLenses]);

  // Update lens assembly status in real-time when assembly.ready event is received
  React.useEffect(() => {
    if (!assemblyState?.lensId || currentMode !== 'horizon') return;
    
    // Find the lens in the list and update its viewConfig
    setLenses((prevLenses) => {
      const lensIndex = prevLenses.findIndex((l) => l.id === assemblyState.lensId);
      if (lensIndex === -1) return prevLenses;
      
      const updatedLenses = [...prevLenses];
      const lens = { ...updatedLenses[lensIndex] };
      
      if (!lens.viewConfig) {
        lens.viewConfig = {};
      }
      
      lens.viewConfig.assemblyStatus = {
        phase: assemblyState.phase,
        progress: assemblyState.progress,
        completedAt: assemblyState.phase === 'ready' ? new Date().toISOString() : undefined,
        counters: assemblyState.counters,
      };
      
      updatedLenses[lensIndex] = lens;
      return updatedLenses;
    });
  }, [assemblyState?.lensId, assemblyState?.phase, assemblyState?.progress, currentMode]);

  // Render lens row
  const renderLensRow = React.useCallback((lens: {
    id: string;
    name: string;
    status: string;
    scope: string[];
    updatedAt: string;
    viewConfig?: Record<string, any>;
  }) => {
    const isSelected = selectedLensId === lens.id;
    
    // Get assembly status from viewConfig
    const assemblyStatus = lens.viewConfig?.assemblyStatus as {
      phase?: string;
      progress?: number;
      completedAt?: string;
    } | undefined;
    
    const getStatusIcon = () => {
      // Show assembly status if available (scouting/ready), otherwise show lens lifecycle status
      if (assemblyStatus?.phase === 'ready') {
        // Assembly completed - show green checkmark
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: '#2ECC71' }}>
            <circle cx="12" cy="12" r="10" strokeWidth="2" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        );
      } else if (assemblyStatus?.phase === 'scouting') {
        // Assembly in progress - show spinner
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
            <circle cx="12" cy="12" r="10" strokeWidth="2" strokeDasharray="32" strokeDashoffset="32" strokeLinecap="round">
              <animate attributeName="stroke-dasharray" dur="1.5s" values="0 32;16 16;0 32;0 32" repeatCount="indefinite" />
              <animate attributeName="stroke-dashoffset" dur="1.5s" values="0;-16;-32;-32" repeatCount="indefinite" />
            </circle>
          </svg>
        );
      } else if (lens.status === 'ACTIVE') {
        // Lens is active but no assembly status - show green checkmark
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: '#2ECC71' }}>
            <circle cx="12" cy="12" r="10" strokeWidth="2" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        );
      } else if (lens.status === 'ARCHIVED') {
        // Lens is archived
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-tertiary-weak)' }}>
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
          </svg>
        );
      }
      return null;
    };

    return (
      <div
        key={lens.id}
        className="px-2 py-1 rounded text-xs flex items-center gap-2 ml-4 cursor-pointer group"
        style={{
          backgroundColor: isSelected ? 'var(--white-10)' : 'transparent',
          color: 'var(--text-secondary-strong)',
        }}
        title={lens.name}
        onClick={() => {
          if (onSelectLens) {
            onSelectLens(isSelected ? null : lens.id);
          }
          if (isMobile) {
            onClose();
          }
        }}
        onMouseEnter={(e) => {
          if (!isSelected) {
            (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)';
          }
        }}
        onMouseLeave={(e) => {
          if (!isSelected) {
            (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent';
          }
        }}
      >
        {getStatusIcon()}
        <span 
          className="flex-1 truncate" 
          style={{ 
            color: 'var(--text-secondary-strong)',
          }}
        >
          {lens.name}
        </span>
      </div>
    );
  }, [selectedLensId, onSelectLens, isMobile, onClose]);

  // Render agent job row
  const renderAgentJobRow = (job: any) => {
    const getStatusIcon = () => {
      if (job.status === 'running' || job.status === 'pending') {
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
            <circle cx="12" cy="12" r="10" strokeWidth="2" strokeDasharray="32" strokeDashoffset="32" strokeLinecap="round">
              <animate attributeName="stroke-dasharray" dur="1.5s" values="0 32;16 16;0 32;0 32" repeatCount="indefinite" />
              <animate attributeName="stroke-dashoffset" dur="1.5s" values="0;-16;-32;-32" repeatCount="indefinite" />
            </circle>
          </svg>
        );
      } else if (job.status === 'completed') {
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        );
      } else if (job.status === 'failed') {
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: '#f87171' }}>
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
          </svg>
        );
      } else if (job.status === 'partial') {
        return (
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        );
      }
      return null;
    };

    const jobTitle = job.title || job.userQuery || 'Untitled Job';
    // Use taskId from job if available, otherwise use id
    const taskId = job.taskId || job.id;
    const isSelected = selectedJobTaskId === taskId;
    const isDeleting = deletingJobIds.has(job.id);
    
    const handleDeleteJob = async (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      
      const targetProjectId = projectId || currentProject?.id;
      if (!targetProjectId || targetProjectId === 'local-unfold' || isDeleting) {
        console.log('[SidebarPanel] Cannot delete job:', { projectId, currentProjectId: currentProject?.id, isDeleting });
        return;
      }
      
      // Validate that we have a job ID and it's not a project ID
      if (!job.id || job.id === targetProjectId) {
        console.error('[SidebarPanel] Invalid job ID for deletion:', { 
          jobId: job.id, 
          projectId: targetProjectId,
          job: job,
          fullJob: JSON.stringify(job, null, 2)
        });
        alert('Cannot delete: invalid job ID. Job ID is missing or equals project ID.');
        return;
      }
      
      // Use job.id for deletion (it's the Firestore document ID)
      const deleteUrl = `/api/v1/projects/${targetProjectId}/agent-jobs/${job.id}`;
      console.log('[SidebarPanel] Deleting job:', { 
        jobId: job.id, 
        projectId: targetProjectId, 
        jobTitle: jobTitle,
        deleteUrl: deleteUrl,
        job: job
      });
      
      // Add to deleting set (show strikethrough)
      setDeletingJobIds(prev => new Set(prev).add(job.id));
      
      // If this job is currently open, close it
      if (isSelected) {
        onSelectJob?.(null);
      }
      
      try {
        await api.deleteAgentJob(targetProjectId, job.id);
        console.log('[SidebarPanel] Job deleted successfully:', { jobId: job.id });
        // Remove from list and deleting set on success
        setAgentJobs(prev => prev.filter(j => j.id !== job.id));
        setDeletingJobIds(prev => {
          const next = new Set(prev);
          next.delete(job.id);
          return next;
        });
      } catch (err) {
        console.error('[SidebarPanel] Failed to delete job', err);
        alert('Failed to delete job');
        // Remove from deleting set on error
        setDeletingJobIds(prev => {
          const next = new Set(prev);
          next.delete(job.id);
          return next;
        });
      }
    };
    
    return (
      <div
        key={job.id}
        className="px-2 py-1 rounded text-xs flex items-center gap-2 ml-4 cursor-pointer group"
        style={{
          backgroundColor: isSelected ? 'var(--white-10)' : 'transparent',
          color: 'var(--text-secondary-strong)',
          opacity: isDeleting ? 0.5 : 1,
          cursor: isDeleting ? 'default' : 'pointer',
        }}
        title={jobTitle}
        onClick={(e) => {
          if (!isDeleting) {
            e.stopPropagation();
            onSelectJob?.(taskId);
          }
        }}
        onMouseEnter={(e) => {
          if (!isSelected && !isDeleting) {
            (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)';
          }
        }}
        onMouseLeave={(e) => {
          if (!isSelected && !isDeleting) {
            (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent';
          }
        }}
      >
        {getStatusIcon()}
        <span 
          className="flex-1 truncate" 
          style={{ 
            color: isDeleting ? 'var(--text-tertiary-weak)' : 'var(--text-secondary-strong)',
            textDecoration: isDeleting ? 'line-through' : 'none',
          }}
        >
          {jobTitle}
        </span>
        {!isDeleting && (
          <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity" style={{ position: 'relative', zIndex: 10 }}>
            <button
              className="p-1 rounded"
              title="Delete job"
              style={{ backgroundColor: 'transparent', position: 'relative', zIndex: 10 }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                handleDeleteJob(e);
              }}
            >
              <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12"
                />
              </svg>
            </button>
          </div>
        )}
      </div>
    );
  };

  const renderProjectGroup = React.useCallback((items: Project[], label: string) => {
    if (!items || items.length === 0) {
      return null;
    }
    return (
      <>
        <div className="text-[11px] uppercase tracking-wide px-2" style={{ color: 'var(--text-tertiary-weak)' }}>
          {label}
        </div>
        <div className="space-y-1">
          {items.map((project) => {
            const isSelected = currentProject?.id === project.id;
            // Only show agent jobs if they belong to the currently selected project
            const projectJobs = (isSelected && agentJobsProjectId === project.id) ? agentJobs : [];
            // Only show lenses if they belong to the currently selected project in horizon mode
            const projectLenses = (isSelected && currentMode === 'horizon' && lensesProjectId === project.id) ? lenses : [];
            return (
              <div key={project.id}>
                {renderProjectRow(project)}
                {isSelected && currentMode === 'horizon' && projectLenses.length > 0 && (
                  <div className="mt-1 space-y-0.5">
                    {projectLenses.map(renderLensRow)}
                  </div>
                )}
                {isSelected && currentMode !== 'horizon' && projectJobs.length > 0 && (
                  <div className="mt-1 space-y-0.5">
                    {projectJobs.map(renderAgentJobRow)}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </>
    );
  }, [currentProject, agentJobsProjectId, agentJobs, lensesProjectId, lenses, currentMode, renderProjectRow, renderAgentJobRow, renderLensRow, selectedLensId, onSelectLens]);
  const [pages, setPages] = React.useState<Artifact[]>([]);
  const [pagesLoading, setPagesLoading] = React.useState(false);
  const [editingPageId, setEditingPageId] = React.useState<string | null>(null);
  const [editingPageName, setEditingPageName] = React.useState<string>('');
  const editingPageInputRef = React.useRef<HTMLInputElement>(null);
  const showPages = currentMode === 'pages';
  const showScenarios = currentMode === 'scenarios';
  
  // Scenarios state
  const [scenarios, setScenarios] = React.useState<ProjectScenario[]>([]);
  const [scenariosLoading, setScenariosLoading] = React.useState(false);
  const [expandedScenarios, setExpandedScenarios] = React.useState<Set<string>>(new Set());
  const [showNewScenarioModal, setShowNewScenarioModal] = React.useState(false);
  const [newScenarioTitle, setNewScenarioTitle] = React.useState('');
  const [newScenarioParentId, setNewScenarioParentId] = React.useState<string | null>(null);
  const [creatingScenario, setCreatingScenario] = React.useState(false);
  
  // Find project by projectId if currentProject doesn't match
  const displayProject = React.useMemo(() => {
    if (!projectId || projectId === 'local-unfold') return null;
    // First check if currentProject matches
    if (currentProject && currentProject.id === projectId) {
      return currentProject;
    }
    // Otherwise find in projects list
    return memoizedProjects.find(p => p.id === projectId) || null;
  }, [projectId, currentProject, memoizedProjects]);

  // Load pages function
  const loadPages = React.useCallback(async () => {
    if (!showPages || !projectId || projectId === 'local-unfold') {
      setPages([]);
      return;
    }

    setPagesLoading(true);
    try {
      const docs = await api.docsList(projectId);
      const artifactsList = docs.filter((doc: CoreDoc) => isArtifact(doc)) as Artifact[];
      setPages(artifactsList);
    } catch (err) {
      console.error('[SidebarPanel] Failed to load pages', err);
      setPages([]);
    } finally {
      setPagesLoading(false);
    }
  }, [showPages, projectId]);

  // Load pages when in pages mode or projectId changes
  React.useEffect(() => {
    loadPages();
  }, [loadPages]);

  // Listen for content.updated event to refresh list
  React.useEffect(() => {
    const handleContentUpdated = (event: CustomEvent) => {
      // Only refresh if the event is for the current project
      if (event.detail?.projectId === projectId && showPages) {
        loadPages();
      }
    };

    window.addEventListener('content.updated', handleContentUpdated as EventListener);
    return () => {
      window.removeEventListener('content.updated', handleContentUpdated as EventListener);
    };
  }, [projectId, showPages, loadPages]);

  // Focus input when editing starts
  React.useEffect(() => {
    if (editingPageId && editingPageInputRef.current) {
      editingPageInputRef.current.focus();
      editingPageInputRef.current.select();
    }
  }, [editingPageId]);

  // Load scenarios function
  const loadScenarios = React.useCallback(async (forceLoad = false) => {
    // Allow force load even if not in scenarios mode (for event-driven updates)
    if (!forceLoad && (!showScenarios || !projectId || projectId === 'local-unfold')) {
      console.log('[SidebarPanel] loadScenarios skipped', { showScenarios, projectId, forceLoad });
      if (!showScenarios) {
        setScenarios([]);
      }
      return;
    }

    if (!projectId || projectId === 'local-unfold') {
      setScenarios([]);
      return;
    }

    console.log('[SidebarPanel] Loading scenarios for project', projectId);
    setScenariosLoading(true);
    try {
      // Load ALL scenarios for the project (not just root)
      // Backend will return all scenarios, we'll filter root ones on client side
      const allScenariosResponse = await api.scenariosList(projectId, null);
      
      // Ensure response is an array (handle null/undefined from empty list)
      const allScenariosFlat = Array.isArray(allScenariosResponse) ? allScenariosResponse : [];
      console.log('[SidebarPanel] Loaded all scenarios', allScenariosFlat.length);
      
      // Filter root scenarios (no parentId or parentId is null/empty)
      const rootScenariosArray = allScenariosFlat.filter(s => !s.parentId || s.parentId === null || s.parentId === '');
      console.log('[SidebarPanel] Root scenarios count', rootScenariosArray.length);
      
      // Build hierarchical structure from flat list (we already have all scenarios)
      // No need to make additional API calls - we have everything
      const allScenarios: ProjectScenario[] = [...allScenariosFlat];
      
      console.log('[SidebarPanel] Total scenarios loaded', allScenarios.length);
      setScenarios(allScenarios);
    } catch (err) {
      console.error('[SidebarPanel] Failed to load scenarios', err);
      setScenarios([]);
    } finally {
      setScenariosLoading(false);
    }
  }, [showScenarios, projectId, expandedScenarios]);

  // Load scenarios when in scenarios mode or projectId changes
  React.useEffect(() => {
    if (showScenarios) {
      loadScenarios();
    }
  }, [showScenarios, projectId, loadScenarios]);

  // Listen for scenarios.updated event to refresh list
  React.useEffect(() => {
    const handleScenariosUpdated = (event: CustomEvent) => {
      // Only refresh if the event is for the current project
      const eventProjectId = event.detail?.projectId;
      console.log('[SidebarPanel] scenarios.updated event received', { eventProjectId, projectId, showScenarios, currentMode });
      if (eventProjectId === projectId) {
        // Force reload even if not currently showing scenarios
        // This ensures sidebar updates when user switches to scenarios mode
        console.log('[SidebarPanel] Reloading scenarios after update event (force load)');
        loadScenarios(true);
      } else {
        console.log('[SidebarPanel] Event projectId mismatch, skipping reload', { eventProjectId, projectId });
      }
    };

    window.addEventListener('scenarios.updated', handleScenariosUpdated as EventListener);
    return () => {
      window.removeEventListener('scenarios.updated', handleScenariosUpdated as EventListener);
    };
  }, [projectId, loadScenarios, showScenarios, currentMode]);

  // Build hierarchical tree structure from flat list
  const buildScenarioTree = React.useCallback((scenariosList: ProjectScenario[]): Array<ProjectScenario & { children?: ProjectScenario[] }> => {
    const scenarioMap = new Map<string, ProjectScenario & { children?: ProjectScenario[] }>();
    const roots: Array<ProjectScenario & { children?: ProjectScenario[] }> = [];

    // First pass: create map of all scenarios
    scenariosList.forEach(scenario => {
      scenarioMap.set(scenario.id, { ...scenario, children: [] });
    });

    // Second pass: build tree
    scenariosList.forEach(scenario => {
      const node = scenarioMap.get(scenario.id)!;
      if (!scenario.parentId) {
        roots.push(node);
      } else {
        const parent = scenarioMap.get(scenario.parentId);
        if (parent) {
          if (!parent.children) parent.children = [];
          parent.children.push(node);
        }
      }
    });

    // Sort by order, then by createdAt
    const sortScenarios = (nodes: Array<ProjectScenario & { children?: ProjectScenario[] }>) => {
      nodes.sort((a, b) => {
        if (a.order !== b.order) return a.order - b.order;
        return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime();
      });
      nodes.forEach(node => {
        if (node.children) sortScenarios(node.children);
      });
    };

    sortScenarios(roots);
    return roots;
  }, []);

  const scenarioTree = React.useMemo(() => buildScenarioTree(scenarios), [scenarios, buildScenarioTree]);

  // Handle scenario creation
  const handleCreateScenario = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold' || !newScenarioTitle.trim() || creatingScenario) {
      return;
    }

    setCreatingScenario(true);
    try {
      const newScenario = await api.scenarioCreate(projectId, {
        title: newScenarioTitle.trim(),
        parentId: newScenarioParentId,
        status: 'active',
      });
      
      // Refresh scenarios list
      await loadScenarios();
      
      // Dispatch event for other components
      window.dispatchEvent(new CustomEvent('scenarios.updated', { detail: { projectId } }));
      
      // Close modal
      setShowNewScenarioModal(false);
      setNewScenarioTitle('');
      setNewScenarioParentId(null);
      
      // Expand parent if scenario was created as child
      if (newScenarioParentId) {
        setExpandedScenarios(prev => new Set(prev).add(newScenarioParentId!));
      }
    } catch (err) {
      console.error('[SidebarPanel] Failed to create scenario', err);
      alert(t('unfold.sections.failedToCreateScenario'));
    } finally {
      setCreatingScenario(false);
    }
  }, [projectId, newScenarioTitle, newScenarioParentId, creatingScenario, loadScenarios]);

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
            // Close all modals (workspace, admin, settings, account) when creating new chat
            onCloseAllModals?.();
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
      <style>{`
        .sidebar-modern .sidebar-section-title {
          font-size: 11px;
          letter-spacing: 0.08em;
          text-transform: uppercase;
          color: var(--text-tertiary-weak);
        }
        .sidebar-modern .sidebar-row {
          position: relative;
          display: flex;
          align-items: center;
          gap: 10px;
          border-radius: 10px;
          padding: 8px 10px;
          cursor: pointer;
          transition: background 120ms ease, color 120ms ease;
          outline: none;
        }
        .sidebar-modern .sidebar-row.compact {
          padding: 6px 10px;
        }
        .sidebar-modern .sidebar-row:hover {
          background: var(--white-10);
        }
        .sidebar-modern .sidebar-row.is-selected {
          background: var(--white-10);
        }
        .sidebar-modern .sidebar-row .sidebar-row-title {
          min-width: 0;
          flex: 1;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
          color: var(--text-heading);
          font-size: 12px;
          font-weight: 600;
        }
        .sidebar-modern .sidebar-row.has-actions:hover .sidebar-row-title,
        .sidebar-modern .sidebar-row.has-actions:focus-within .sidebar-row-title {
          padding-right: 160px; /* make room for actions overlay */
        }
        /* Lens rows have fewer actions than projects; reserve less space on hover. */
        .sidebar-modern .sidebar-row.lens-row.has-actions:hover .sidebar-row-title,
        .sidebar-modern .sidebar-row.lens-row.has-actions:focus-within .sidebar-row-title {
          padding-right: 96px;
        }
        .sidebar-modern .sidebar-row .sidebar-row-subtitle {
          font-size: 10px;
          color: var(--text-tertiary-weak);
          padding: 2px 6px;
          border-radius: 999px;
          border: 1px solid var(--border-subtle);
          background: var(--white-05);
          flex-shrink: 0;
        }
        .sidebar-modern .sidebar-row .sidebar-timepill {
          transition: opacity 120ms ease;
        }
        .sidebar-modern .sidebar-row.has-actions:hover .sidebar-timepill,
        .sidebar-modern .sidebar-row.has-actions:focus-within .sidebar-timepill {
          opacity: 0;
          pointer-events: none;
        }
        .sidebar-modern .sidebar-row .sidebar-row-actions {
          position: absolute;
          right: 8px;
          top: 50%;
          transform: translateY(-50%);
          display: flex;
          align-items: center;
          gap: 6px;
          opacity: 0;
          pointer-events: none;
          transition: opacity 120ms ease;
        }
        .sidebar-modern .sidebar-row:hover .sidebar-row-actions,
        .sidebar-modern .sidebar-row:focus-within .sidebar-row-actions {
          opacity: 1;
          pointer-events: auto;
        }
        .sidebar-modern .sidebar-icon-btn {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          width: 30px;
          height: 30px;
          border-radius: 10px;
          border: 1px solid transparent;
          background: transparent;
          color: var(--text-secondary-strong);
          transition: background 120ms ease, border 120ms ease;
          cursor: pointer;
        }
        .sidebar-modern .sidebar-icon-btn:hover {
          background: var(--white-10);
          border-color: var(--border-subtle);
        }
      `}</style>

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
        className={`flex-shrink-0 rounded-xl mr-4 overflow-hidden relative sidebar-panel ${isModernSidebarMode ? 'sidebar-modern' : ''}`}
        style={{
          backgroundColor: isModernSidebarMode ? 'var(--bg-elevated)' : 'var(--bg-input)',
          border: isMobile ? undefined : '1px solid var(--border-subtle)',
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
          <div className={`flex items-center justify-between ${isModernSidebarMode ? 'mb-4' : 'mb-6'}`}>
            {currentMode === 'pages' ? (
              <div className="flex items-center gap-2">
                <button
                  onClick={() => {
                    // Switch back to chat mode via callback prop
                    // Parent component should handle mode switching
                    if (onOpenDocument) {
                      // Trigger mode switch by dispatching event that parent listens to
                      window.dispatchEvent(new CustomEvent('ui.switchMode', { detail: { mode: 'chat' } }));
                    }
                  }}
                  className="p-1 rounded transition-colors"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--text-secondary-strong)',
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                      'var(--white-10)';
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                      'transparent';
                  }}
                  title="Back to chats"
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
                <h3
                  className="text-13 font-medium"
                  style={{ color: 'var(--text-heading)' }}
                >
                  Pages
                </h3>
              </div>
            ) : currentMode === 'scenarios' ? (
              <div className="flex items-center gap-2">
                <button
                  onClick={() => {
                    // Switch back to chat mode
                    window.dispatchEvent(new CustomEvent('ui.switchMode', { detail: { mode: 'chat' } }));
                  }}
                  className="p-1 rounded transition-colors"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--text-secondary-strong)',
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                      'var(--white-10)';
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                      'transparent';
                  }}
                  title="Back to chats"
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
                <h3
                  className="text-13 font-medium"
                  style={{ color: 'var(--text-heading)' }}
                >
                  {t('unfold.sections.scenarios')}
                </h3>
              </div>
            ) : (
            <h3
              className="text-13 font-medium"
              style={{ color: 'var(--text-heading)' }}
            >
              {t('unfold.sections.navigation')}
            </h3>
            )}
            {isModernSidebarMode ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <button
                  type="button"
                  className="sidebar-icon-btn"
                  title="Search (Ctrl/Cmd+K)"
                  onClick={() => openPalette()}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="11" cy="11" r="8" />
                    <path d="m21 21-4.3-4.3" />
                  </svg>
                </button>
                <button
                  type="button"
                  className="sidebar-icon-btn"
                  title={
                    currentMode === 'horizon'
                      ? (isDetailsView ? 'New Lens' : 'New Project')
                      : 'New Chat'
                  }
                  aria-label={
                    currentMode === 'horizon'
                      ? (isDetailsView ? 'New Lens' : 'New Project')
                      : 'New Chat'
                  }
                  onClick={() => {
                    onCloseAllModals?.();
                    if (currentMode === 'horizon') {
                      if (isDetailsView && onNewLens) {
                        onNewLens();
                      } else {
                        handleCreateHorizonProject();
                      }
                    } else {
                      onResetChat();
                    }
                    if (isMobile) onClose();
                  }}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M12 5v14" />
                    <path d="M5 12h14" />
                  </svg>
                </button>
                <button
                  type="button"
                  className="sidebar-icon-btn"
                  title={sidebarDensity === 'compact' ? 'Comfortable density' : 'Compact density'}
                  onClick={() => setSidebarDensity((d) => (d === 'compact' ? 'comfortable' : 'compact'))}
                >
                  {sidebarDensity === 'compact' ? (
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M4 6h16" /><path d="M4 12h16" /><path d="M4 18h16" />
                    </svg>
                  ) : (
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M4 6h16" /><path d="M4 12h10" /><path d="M4 18h16" />
                    </svg>
                  )}
                </button>
                <button
                  type="button"
                  className="sidebar-icon-btn"
                  onClick={onClose}
                  title="Close"
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M18 6 6 18" />
                    <path d="M6 6l12 12" />
                  </svg>
                </button>
              </div>
            ) : (
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
            )}
          </div>

          {/* New Chat action or Project name in Pages/Scenarios mode */}
          <div className="mb-3">
            {showPages || showScenarios ? (
              <div
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg"
                style={{
                  backgroundColor: 'transparent',
                  color: 'var(--text-heading)',
                }}
              >
                <span
                  className="text-13 font-medium flex-1 truncate"
                  style={{ color: 'var(--text-heading)' }}
                  title={displayProject?.displayName || ''}
                >
                  {displayProject?.displayName || 'Untitled Project'}
                </span>
              </div>
            ) : (
              <>
                {isModernSidebarMode ? (
                  <div style={{ height: 1, background: 'var(--border-subtle)', marginTop: 4 }} />
                ) : (
                  <>
                    <button
                      type="button"
                      aria-label={
                        currentMode === 'horizon'
                          ? (isDetailsView ? 'New Lens' : 'New Project')
                          : t('unfold.sections.newChat')
                      }
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
                      onClick={() => {
                        // Close all modals (workspace, admin, settings, account) when creating new chat/project
                        onCloseAllModals?.();
                        if (currentMode === 'horizon') {
                          if (isDetailsView && onNewLens) {
                            onNewLens();
                          } else {
                            handleCreateHorizonProject();
                          }
                        } else {
                          onResetChat();
                        }
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
                        {currentMode === 'horizon' ? (isDetailsView ? 'New Lens' : 'New Project') : t('unfold.sections.newChat')}
                      </span>
                    </button>
                    {onShowWorkspace && (
                      <button
                        type="button"
                        aria-label={t('unfold.sections.workspace')}
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
                          if (onShowWorkspace) {
                            onShowWorkspace();
                          }
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
                          strokeWidth={2}
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
                          <polyline points="9 22 9 12 15 12 15 22" />
                        </svg>
                        <span
                          className="text-13 font-medium"
                          style={{ color: 'var(--text-heading)' }}
                        >
                          {t('unfold.sections.workspace')}
                        </span>
                      </button>
                    )}
                    {/* Divider */}
                    <div
                      className="mt-3 mb-3"
                      style={{
                        height: '1px',
                        backgroundColor: 'var(--white-10)',
                      }}
                    />
                  </>
                )}
              </>
            )}
          </div>

          {/* Scrollable Chats section */}
          <div
            className={`mb-4 flex-1 min-h-0 ${isModernSidebarMode ? 'overflow-hidden' : 'overflow-y-auto'}`}
            style={{ scrollbarGutter: 'stable' }}
          >
            {showPages ? (
              <>
                <div
                  className="text-[11px] uppercase tracking-wide mb-2"
                  style={{ color: 'var(--text-tertiary-weak)' }}
                >
                  Dynamic Content ({pages.length})
                </div>
                {pagesLoading ? (
                  <div
                    className="text-xs"
                    style={{ color: 'var(--text-secondary-strong)' }}
                  >
                    Loading...
                  </div>
                ) : pages.length === 0 ? (
                  <div
                    className="text-xs"
                    style={{ color: 'var(--text-secondary-strong)' }}
                  >
                    No pages yet
                  </div>
                ) : (
                  <div className="space-y-1">
                    {pages.map((page) => (
                      <div
                        key={page.id}
                        className="px-2 py-1 rounded text-xs flex items-center gap-2 group"
                        title={page.filename}
                        style={{
                          backgroundColor: 'transparent',
                        }}
                        onMouseEnter={(e) => {
                          (e.currentTarget as HTMLDivElement).style.backgroundColor =
                            'var(--white-10)';
                        }}
                        onMouseLeave={(e) => {
                          (e.currentTarget as HTMLDivElement).style.backgroundColor =
                            'transparent';
                        }}
                      >
                        <div
                          className="flex items-center gap-2 flex-1 min-w-0 cursor-pointer"
                          onClick={() => {
                            if (editingPageId !== page.id) {
                              onOpenDocument?.(page);
                              if (isMobile) {
                                onClose();
                              }
                            }
                          }}
                        >
                          <svg
                            className="w-3 h-3"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            style={{ color: 'var(--text-secondary)' }}
                          >
                            <path
                              d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                            />
                            <path
                              d="M14 2v6h6M16 13H8M16 17H8M10 9H8"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                            />
                          </svg>
                          {editingPageId === page.id ? (
                            <input
                              ref={editingPageInputRef}
                              type="text"
                              value={editingPageName}
                              onChange={(e) => setEditingPageName(e.target.value)}
                              onKeyDown={async (e) => {
                                if (e.key === 'Enter') {
                                  e.stopPropagation();
                                  if (!projectId || projectId === 'local-unfold') return;
                                  const newName = editingPageName.trim();
                                  if (newName && newName !== page.filename) {
                                    try {
                                      await api.docsUpdate(projectId, page.id, { filename: newName });
                                      loadPages();
                                      window.dispatchEvent(new CustomEvent('content.updated', { detail: { projectId } }));
                                    } catch (err) {
                                      console.error('[SidebarPanel] Failed to rename page', err);
                                      alert('Failed to rename page');
                                    }
                                  }
                                  setEditingPageId(null);
                                  setEditingPageName('');
                                } else if (e.key === 'Escape') {
                                  e.stopPropagation();
                                  setEditingPageId(null);
                                  setEditingPageName('');
                                }
                              }}
                              onBlur={async () => {
                                if (!projectId || projectId === 'local-unfold') return;
                                const newName = editingPageName.trim();
                                if (newName && newName !== page.filename) {
                                  try {
                                    await api.docsUpdate(projectId, page.id, { filename: newName });
                                    loadPages();
                                    window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));
                                  } catch (err) {
                                    console.error('[SidebarPanel] Failed to rename page', err);
                                    alert('Failed to rename page');
                                  }
                                }
                                setEditingPageId(null);
                                setEditingPageName('');
                              }}
                              className="flex-1 px-1 py-0.5 rounded text-xs"
                              style={{
                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                color: 'var(--text-heading)',
                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                              }}
                              onClick={(e) => e.stopPropagation()}
                            />
                          ) : (
                            <span
                              className="flex-1 truncate"
                              style={{ color: 'var(--text-heading)' }}
                            >
                              {page.filename}
                            </span>
                          )}
                        </div>
                        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              setEditingPageId(page.id);
                              setEditingPageName(page.filename);
                              setTimeout(() => editingPageInputRef.current?.focus(), 0);
                            }}
                            className="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                            style={{ color: 'var(--text-secondary)' }}
                            aria-label="Rename page"
                            title="Rename page"
                          >
                            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                              <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                            </svg>
                          </button>
                          <button
                            type="button"
                            onClick={async (e) => {
                              e.stopPropagation();
                              if (!projectId || projectId === 'local-unfold') return;
                              if (!confirm(`Delete "${page.filename}"?`)) return;
                              try {
                                await api.docsDelete(projectId, page.id);
                                loadPages();
                                window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));
                              } catch (err) {
                                console.error('[SidebarPanel] Failed to delete page', err);
                                alert('Failed to delete page');
                              }
                            }}
                            className="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                            style={{ color: 'var(--text-secondary)' }}
                            aria-label="Delete page"
                            title="Delete page"
                          >
                            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                              <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                            </svg>
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </>
            ) : showScenarios ? (
              <>
                <div className="mb-3">
                  <button
                    type="button"
                    aria-label={t('unfold.sections.newScenario')}
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
                    onClick={() => {
                      setShowNewScenarioModal(true);
                      setNewScenarioTitle('');
                      setNewScenarioParentId(null);
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
                      className="text-xs flex-1 text-left"
                      style={{ color: 'var(--text-heading)' }}
                    >
                      {t('unfold.sections.newScenario')}
                    </span>
                  </button>
                </div>
                <div
                  className="text-[11px] uppercase tracking-wide mb-2"
                  style={{ color: 'var(--text-tertiary-weak)' }}
                >
                  {t('unfold.sections.scenarios')} ({scenarios.filter(s => !s.parentId).length})
                </div>
                {scenariosLoading ? (
                  <div
                    className="text-xs"
                    style={{ color: 'var(--text-secondary-strong)' }}
                  >
                    {t('unfold.sections.loadingScenarios')}
                  </div>
                ) : scenarioTree.length === 0 ? (
                  <div
                    className="text-xs"
                    style={{ color: 'var(--text-secondary-strong)' }}
                  >
                    {t('unfold.sections.noScenariosYet')}
                  </div>
                ) : (
                  <div className="space-y-1">
                    {scenarioTree.map((scenario) => {
                      const ScenarioItem: React.FC<{ scenario: ProjectScenario & { children?: ProjectScenario[] }; level?: number }> = ({ scenario, level = 0 }) => {
                        const hasChildren = scenario.children && scenario.children.length > 0;
                        const isExpanded = expandedScenarios.has(scenario.id);
                        const isSelected = selectedScenarioId === scenario.id;
                        
                        return (
                          <div key={scenario.id}>
                            <div
                              className="px-2 py-1 rounded text-xs flex items-center gap-2 group"
                              style={{
                                backgroundColor: isSelected ? 'var(--white-10)' : 'transparent',
                                paddingLeft: `${8 + level * 16}px`,
                              }}
                              onMouseEnter={(e) => {
                                if (!isSelected) {
                                  (e.currentTarget as HTMLDivElement).style.backgroundColor =
                                    'var(--white-10)';
                                }
                              }}
                              onMouseLeave={(e) => {
                                if (!isSelected) {
                                  (e.currentTarget as HTMLDivElement).style.backgroundColor =
                                    'transparent';
                                }
                              }}
                            >
                              <div className="flex items-center gap-1 flex-1 min-w-0">
                                {hasChildren && (
                                  <button
                                    type="button"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      const wasExpanded = isExpanded;
                                      setExpandedScenarios(prev => {
                                        const next = new Set(prev);
                                        if (wasExpanded) {
                                          next.delete(scenario.id);
                                        } else {
                                          next.add(scenario.id);
                                        }
                                        return next;
                                      });
                                      // Children will be loaded automatically when expandedScenarios changes
                                      // (via useEffect dependency on expandedScenarios)
                                    }}
                                    className="flex-shrink-0 w-3 h-3 flex items-center justify-center"
                                    style={{ color: 'var(--text-secondary)' }}
                                  >
                                    <svg
                                      className="w-2 h-2"
                                      fill="none"
                                      stroke="currentColor"
                                      viewBox="0 0 24 24"
                                      style={{
                                        transform: isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                                        transition: 'transform 0.2s',
                                      }}
                                    >
                                      <path
                                        strokeLinecap="round"
                                        strokeLinejoin="round"
                                        strokeWidth={2}
                                        d="M9 5l7 7-7 7"
                                      />
                                    </svg>
                                  </button>
                                )}
                                {!hasChildren && <div className="w-3 h-3" />}
                                <span
                                  className="flex-1 truncate cursor-pointer"
                                  style={{ color: 'var(--text-heading)' }}
                                  onClick={() => {
                                    console.log('[SidebarPanel] Scenario clicked', scenario.id);
                                    onSelectScenario?.(scenario.id);
                                    if (isMobile) {
                                      onClose();
                                    }
                                  }}
                                >
                                  {scenario.title}
                                </span>
                              </div>
                            </div>
                            {hasChildren && isExpanded && scenario.children && (
                              <div>
                                {scenario.children.map((child) => (
                                  <ScenarioItem key={child.id} scenario={child} level={level + 1} />
                                ))}
                              </div>
                            )}
                          </div>
                        );
                      };
                      
                      return <ScenarioItem key={scenario.id} scenario={scenario} />;
                    })}
                  </div>
                )}
              </>
            ) : (
              <>
                {isModernSidebarMode ? (
                  <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
                    <div
                      ref={projectsListRef}
                      onScroll={isProjectsView ? onProjectsScroll : undefined}
                      style={{
                        flex: 1,
                        minHeight: 0,
                        overflow: 'auto',
                        paddingRight: 4,
                        overflowX: 'hidden',
                      }}
                    >
                      {isProjectsView ? (
                        <>
                          {isHorizonMode && onOpenWorldMap && (
                            <>
                              <button
                                type="button"
                                className="w-full flex items-center justify-between gap-2 px-2 py-2 rounded-lg transition-colors mb-2"
                                aria-pressed={Boolean(isWorldMapActive)}
                                style={{
                                  backgroundColor: 'transparent',
                                  border: 'none',
                                  color: 'var(--text-secondary-strong)',
                                }}
                                onMouseEnter={(e) => {
                                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                                }}
                                onMouseLeave={(e) => {
                                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                                }}
                                onClick={() => {
                                  onCloseAllModals?.();
                                  onOpenWorldMap();
                                  if (isMobile) onClose();
                                }}
                              >
                                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ color: 'var(--text-tertiary-weak)' }}>
                                    <circle cx="12" cy="12" r="9" />
                                    <path d="M3 12h18" />
                                    <path d="M12 3a12 12 0 0 1 0 18" />
                                    <path d="M12 3a12 12 0 0 0 0 18" />
                                  </svg>
                                  <span style={{ fontSize: 12, fontWeight: 650, color: 'var(--text-secondary-strong)' }}>World map</span>
                                </div>
                              </button>
                              <div style={{ height: 1, background: 'var(--border-subtle)', marginBottom: 10 }} />
                            </>
                          )}
                          <div className="sidebar-section-title" style={{ marginBottom: 10 }}>
                            {isHorizonMode ? 'Projects' : t('unfold.sections.chats')}
                          </div>
                          {projectsLoading && memoizedProjects.length === 0 ? (
                            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                              {isHorizonMode ? 'Loading projects…' : t('unfold.sections.loadingChats')}
                            </div>
                          ) : memoizedProjects.length === 0 ? (
                            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                              {isHorizonMode ? 'No projects yet' : t('unfold.sections.noChatsYet')}
                            </div>
                          ) : (
                            <>
                              {pinnedProjects.length > 0 && (
                                <div style={{ marginBottom: 14 }}>
                                  <div className="sidebar-section-title" style={{ marginBottom: 8 }}>
                                    Pinned
                                  </div>
                                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                                    {/* pinned rows already rendered above */}
                                    {pinnedProjects.map((project) => renderModernProjectRow(project))}
                                  </div>
                                </div>
                              )}
                              <div style={{ marginBottom: 10 }}>
                                <div className="sidebar-section-title" style={{ marginBottom: 8 }}>
                                  {pinnedProjects.length > 0 ? 'Recent' : 'All'}
                                </div>
                                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                                  {shouldVirtualizeProjects && virtualProjects.padTop > 0 && <div style={{ height: virtualProjects.padTop }} />}
                                  {virtualProjects.slice.map((project) => renderModernProjectRow(project))}
                                  {shouldVirtualizeProjects && virtualProjects.padBottom > 0 && <div style={{ height: virtualProjects.padBottom }} />}
                                </div>
                              </div>
                            </>
                          )}
                        </>
                      ) : (
                        <>
                          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 12 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                              <button
                                type="button"
                                className="sidebar-icon-btn"
                                title="Back to projects"
                                onClick={() => setSidebarMainTab('projects')}
                              >
                                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                                  <path d="M15 18l-6-6 6-6" />
                                </svg>
                              </button>
                              <div style={{ minWidth: 0 }}>
                                <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                  {currentProject?.displayName || 'Untitled Project'}
                                </div>
                                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                  {isHorizonMode ? 'Horizon' : 'Discovery'}
                                </div>
                              </div>
                            </div>
                            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                              <button type="button" className="sidebar-icon-btn" title="Switch project (Ctrl/Cmd+K)" onClick={() => openPalette()}>
                                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                                  <circle cx="11" cy="11" r="8" />
                                  <path d="m21 21-4.3-4.3" />
                                </svg>
                              </button>
                              {isHorizonMode && onNewLens && (
                                <button type="button" className="sidebar-icon-btn" title="New lens" onClick={() => onNewLens()}>
                                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                                    <path d="M12 5v14" /><path d="M5 12h14" />
                                  </svg>
                                </button>
                              )}
                            </div>
                          </div>

                          {isHorizonMode ? (
                            <>
                              <div className="sidebar-section-title" style={{ marginBottom: 8 }}>
                                Lenses
                              </div>
                              {lensesLoading ? (
                                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>Loading lenses…</div>
                              ) : lenses.length === 0 ? (
                                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>No lenses yet</div>
                              ) : (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                                  {lenses.map((lens) => {
                                    const isSelected = selectedLensId === lens.id;
                                    const isDeleting = deletingLensIds.has(lens.id);
                                    const isEditing = editingLensId === lens.id;
                                    const statusU = String(lens.status || '').toUpperCase();
                                    const statusShort = statusU === 'ACTIVE' ? 'A' : (statusU || 'LENS');
                                    return (
                                      <div
                                        key={lens.id}
                                        className={`sidebar-row lens-row has-actions ${sidebarDensity === 'compact' ? 'compact' : ''} ${isSelected ? 'is-selected' : ''}`}
                                        style={{ opacity: isDeleting ? 0.6 : 1, cursor: isDeleting ? 'default' : 'pointer' }}
                                        onClick={() => {
                                          if (isDeleting) return;
                                          if (isEditing) return;
                                          onSelectLens?.(isSelected ? null : lens.id);
                                          if (isMobile) onClose();
                                        }}
                                        title={lens.name}
                                      >
                                        {isEditing ? (
                                          <input
                                            ref={editingLensInputRef}
                                            type="text"
                                            value={editingLensName}
                                            onChange={(e) => setEditingLensName(e.target.value)}
                                            onKeyDown={(e) => {
                                              if (e.key === 'Enter') {
                                                void renameLens(lens, editingLensName);
                                              } else if (e.key === 'Escape') {
                                                cancelEditingLens();
                                              }
                                            }}
                                            onBlur={() => {
                                              void renameLens(lens, editingLensName);
                                            }}
                                            style={{
                                              flex: 1,
                                              minWidth: 0,
                                              background: 'transparent',
                                              border: '1px solid var(--border-subtle)',
                                              borderRadius: 10,
                                              padding: '6px 10px',
                                              color: 'var(--text-heading)',
                                              outline: 'none',
                                              fontSize: 12,
                                            }}
                                            onClick={(e) => e.stopPropagation()}
                                          />
                                        ) : (
                                          <div className="sidebar-row-title" style={{ fontWeight: 650, textDecoration: isDeleting ? 'line-through' : 'none' }}>
                                            {lens.name || 'Untitled Lens'}
                                          </div>
                                        )}

                                        {!isEditing && (
                                          <div className="sidebar-row-subtitle" title="Status">
                                            {statusShort}
                                          </div>
                                        )}

                                        {!isEditing && !isDeleting && (
                                          <div className="sidebar-row-actions">
                                            <button
                                              type="button"
                                              className="sidebar-icon-btn"
                                              title="Edit lens name"
                                              onClick={(e) => {
                                                e.stopPropagation();
                                                startEditingLens(lens);
                                              }}
                                            >
                                              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                                <path d="M12 20h9" />
                                                <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5Z" />
                                              </svg>
                                            </button>
                                            <button
                                              type="button"
                                              className="sidebar-icon-btn"
                                              title="Transfer to another project"
                                              onClick={(e) => {
                                                e.stopPropagation();
                                                openTransferDialog(lens);
                                              }}
                                            >
                                              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                                <path d="m16 3 4 4-4 4" />
                                                <path d="M20 7H4" />
                                                <path d="m8 21-4-4 4-4" />
                                                <path d="M4 17h16" />
                                              </svg>
                                            </button>
                                            <button
                                              type="button"
                                              className="sidebar-icon-btn"
                                              title="Delete lens"
                                              onClick={(e) => {
                                                e.stopPropagation();
                                                void deleteLens(lens);
                                              }}
                                            >
                                              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                                <path d="M3 6h18" />
                                                <path d="M8 6V4h8v2" />
                                                <path d="M19 6l-1 14H6L5 6" />
                                                <path d="M10 11v6" />
                                                <path d="M14 11v6" />
                                              </svg>
                                            </button>
                                          </div>
                                        )}
                                      </div>
                                    );
                                  })}
                                </div>
                              )}
                            </>
                          ) : (
                            <>
                              <div className="sidebar-section-title" style={{ marginBottom: 8 }}>
                                Jobs
                              </div>
                              {agentJobsLoading ? (
                                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>Loading jobs…</div>
                              ) : agentJobs.length === 0 ? (
                                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>No jobs yet</div>
                              ) : (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                                  {agentJobs.map((job) => renderAgentJobRow(job))}
                                </div>
                              )}
                            </>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                ) : (
                  <>
                    {isHorizonMode && onOpenWorldMap && (
                      <>
                        <button
                          type="button"
                          className="w-full flex items-center justify-between gap-2 px-3 py-2 rounded-lg transition-colors mb-2"
                          aria-pressed={Boolean(isWorldMapActive)}
                          style={{
                            backgroundColor: 'transparent',
                            color: 'var(--text-secondary-strong)',
                            border: 'none',
                          }}
                          onMouseEnter={(e) => {
                            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                          }}
                          onMouseLeave={(e) => {
                            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                          }}
                          onClick={() => {
                            onCloseAllModals?.();
                            onOpenWorldMap();
                            if (isMobile) onClose();
                          }}
                        >
                          <span className="flex items-center gap-2 text-13 font-medium" style={{ color: 'var(--text-secondary-strong)' }}>
                            <svg
                              className="w-4 h-4"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth={2}
                              style={{ color: 'var(--text-tertiary-weak)' }}
                            >
                              <circle cx="12" cy="12" r="9" />
                              <path d="M3 12h18" />
                              <path d="M12 3a12 12 0 0 1 0 18" />
                              <path d="M12 3a12 12 0 0 0 0 18" />
                            </svg>
                            World map
                          </span>
                        </button>
                        <div
                          className="mb-3"
                          style={{
                            height: '1px',
                            backgroundColor: 'var(--white-10)',
                          }}
                        />
                      </>
                    )}
                    <div
                      className="text-[11px] uppercase tracking-wide mb-2"
                      style={{ color: 'var(--text-heading)' }}
                    >
                      {currentMode === 'horizon' ? 'Projects' : t('unfold.sections.chats')}
                    </div>

                    {projectsLoading && memoizedProjects.length === 0 ? (
                      <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                        {currentMode === 'horizon' ? 'Loading projects...' : t('unfold.sections.loadingChats')}
                      </div>
                    ) : memoizedProjects.length === 0 ? (
                      <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                        {currentMode === 'horizon' ? 'No projects yet' : t('unfold.sections.noChatsYet')}
                      </div>
                    ) : (
                      <div className="space-y-3">
                        {renderProjectGroup(groupedProjects.today, t('unfold.sections.today', 'Today'))}
                        {renderProjectGroup(groupedProjects.yesterday, t('unfold.sections.yesterday', 'Yesterday'))}
                        {renderProjectGroup(groupedProjects.later, t('unfold.sections.later', 'Later'))}
                      </div>
                    )}
                  </>
                )}
              </>
            )}
          </div>

          {/* Bottom settings button — flex-shrink-0 so Admin/Account/Settings stay visible */}
          <div className="mt-auto pt-4 flex-shrink-0">
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

      {/* Command palette (projects / lenses / jobs) */}
      {paletteOpen && isModernSidebarMode && createPortal(
        <div
          role="dialog"
          aria-modal="true"
          onClick={() => closePalette()}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9999,
            background: 'rgba(0,0,0,0.35)',
            backdropFilter: 'blur(6px)',
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              position: 'absolute',
              left: paletteAnchorRect
                ? `${Math.round(paletteAnchorRect.left + paletteAnchorRect.width / 2)}px`
                : '50%',
              top: paletteAnchorRect
                ? `${Math.max(16, Math.round(paletteAnchorRect.top + paletteAnchorRect.height * 0.14))}px`
                : '14vh',
              transform: 'translateX(-50%)',
              width: 680,
              maxWidth: paletteAnchorRect
                ? `${Math.max(320, Math.floor(paletteAnchorRect.width - 28))}px`
                : 'min(680px, calc(100vw - 28px))',
              borderRadius: 18,
              background: 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
              boxShadow: '0 20px 70px rgba(0,0,0,0.45)',
              overflow: 'hidden',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 12,
                padding: '14px 16px',
                borderBottom: '1px solid var(--border-subtle)',
              }}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--text-secondary-strong)' }}>
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
              </svg>
              <input
                ref={paletteInputRef}
                value={paletteQuery}
                onChange={(e) => {
                  setPaletteQuery(e.target.value);
                  setPaletteActiveIndex(0);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    closePalette();
                    return;
                  }
                  if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    setPaletteActiveIndex((i) => Math.min((paletteItems.length || 1) - 1, i + 1));
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    setPaletteActiveIndex((i) => Math.max(0, i - 1));
                    return;
                  }
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    const picked = paletteItems[paletteActiveIndex];
                    if (picked) runPaletteSelection(picked);
                    return;
                  }
                }}
                placeholder={
                  paletteMode === 'create_project'
                    ? 'Project name…'
                    : (isHorizonMode ? 'Search projects and lenses…' : 'Search chats and jobs…')
                }
                autoComplete="off"
                spellCheck={false}
                style={{
                  flex: 1,
                  border: 'none',
                  outline: 'none',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  fontSize: 16,
                  fontWeight: 600,
                  minWidth: 0,
                }}
              />
              <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexShrink: 0 }}>
                <kbd style={{ fontSize: 10, padding: '2px 6px', borderRadius: 8, border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-secondary-strong)' }}>
                  {isMac ? '⌘K' : 'Ctrl K'}
                </kbd>
                <kbd style={{ fontSize: 10, padding: '2px 6px', borderRadius: 8, border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-secondary-strong)' }}>
                  esc
                </kbd>
              </div>
            </div>

            <div style={{ maxHeight: 'min(520px, 60vh)', overflow: 'auto', padding: '8px 0' }}>
              {paletteItems.length === 0 ? (
                <div style={{ padding: '26px 16px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                  {paletteMode === 'create_project'
                    ? 'Type a project name to create a new Horizon project.'
                    : 'No results.'}
                </div>
              ) : (
                (() => {
                  let lastKind: string | null = null;
                  return paletteItems.map((item, idx) => {
                    const kind = item.kind;
                    const showHeader = kind !== lastKind;
                    lastKind = kind;
                    const isActive = idx === paletteActiveIndex;
                    const sectionLabel =
                      kind === 'project' ? 'Projects'
                      : kind === 'lens' ? 'Lenses'
                      : kind === 'job' ? 'Jobs'
                      : 'Create';

                    return (
                      <React.Fragment key={item.id}>
                        {showHeader && (
                          <div
                            style={{
                              padding: '10px 16px 6px 16px',
                              fontSize: 11,
                              letterSpacing: '0.08em',
                              textTransform: 'uppercase',
                              color: 'var(--text-tertiary-weak)',
                            }}
                          >
                            {sectionLabel}
                          </div>
                        )}
                        <button
                          type="button"
                          onClick={() => runPaletteSelection(item)}
                          onMouseEnter={() => setPaletteActiveIndex(idx)}
                          style={{
                            width: '100%',
                            textAlign: 'left',
                            padding: '10px 16px',
                            border: 'none',
                            background: isActive ? 'var(--white-10)' : 'transparent',
                            cursor: 'pointer',
                            display: 'flex',
                            alignItems: 'baseline',
                            justifyContent: 'space-between',
                            gap: 12,
                            borderLeft: `3px solid ${isActive ? 'var(--accent-primary)' : 'transparent'}`,
                          }}
                        >
                          <div style={{ minWidth: 0 }}>
                            <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {item.title}
                            </div>
                            {item.subtitle && (
                              <div style={{ marginTop: 2, fontSize: 12, color: 'var(--text-secondary-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {item.subtitle}
                              </div>
                            )}
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-tertiary-weak)', flexShrink: 0 }}>
                            {kind === 'project' ? '↵ Select' : '↵ Open'}
                          </div>
                        </button>
                      </React.Fragment>
                    );
                  });
                })()
              )}
            </div>

            <div
              style={{
                padding: '10px 12px',
                borderTop: '1px solid var(--border-subtle)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                color: 'var(--text-secondary-strong)',
                fontSize: 11,
              }}
            >
              <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                <span><strong>↑</strong>/<strong>↓</strong> to navigate</span>
                <span><strong>Enter</strong> to select</span>
              </div>
              <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                <span><strong>Esc</strong> to close</span>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* New Scenario Modal */}
      {showNewScenarioModal && createPortal(
        <div 
          className="fixed inset-0 z-[80] flex items-center justify-center" 
          role="dialog" 
          aria-modal="true"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowNewScenarioModal(false);
            }
          }}
        >
          <div 
            className="absolute inset-0 bg-black/30" 
            onClick={() => setShowNewScenarioModal(false)} 
          />
          <div 
            className="relative w-full max-w-md rounded-xl border p-6" 
            style={{ 
              backgroundColor: 'var(--bg-elevated)', 
              borderColor: 'var(--overlay-line-90)', 
              boxShadow: '0 8px 32px rgba(0, 0, 0, 0.35)',
              maxHeight: '90vh',
              overflow: 'hidden',
              display: 'flex',
              flexDirection: 'column',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between mb-4 flex-shrink-0">
              <div className="text-lg font-medium" style={{ color: 'var(--text-heading)' }}>
                {t('unfold.sections.newScenario')}
              </div>
              <button 
                onClick={() => setShowNewScenarioModal(false)} 
                aria-label="Close" 
                className="rounded-full p-1 transition-colors"
                style={{ 
                  color: 'var(--text-secondary-strong)',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                }}
              >
                <svg viewBox="0 0 24 24" className="w-5 h-5" fill="none" stroke="currentColor">
                  <path d="M6 6l12 12M18 6L6 18" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
                </svg>
              </button>
            </div>

            <div className="flex-1 overflow-y-auto space-y-4" style={{ minHeight: 0 }}>
              {/* Title input */}
              <div>
                <label
                  className="block text-sm mb-2"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  {t('unfold.sections.scenarioTitle')} *
                </label>
                <input
                  type="text"
                  value={newScenarioTitle}
                  onChange={(e) => setNewScenarioTitle(e.target.value)}
                  placeholder={t('unfold.sections.scenarioTitlePlaceholder')}
                  className="w-full px-3 py-2 rounded-lg text-sm"
                  style={{
                    backgroundColor: 'var(--bg-input, #1A1A1A)',
                    color: 'var(--text-heading)',
                    border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                    outline: 'none',
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && newScenarioTitle.trim() && !creatingScenario) {
                      handleCreateScenario();
                    }
                  }}
                  autoFocus
                />
              </div>

              {/* Parent selection */}
              <div>
                <label
                  className="block text-sm mb-2"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  {t('unfold.sections.parentScenario')}
                </label>
                <div className="space-y-2 max-h-48 overflow-y-auto">
                  <label className="flex items-center gap-2 cursor-pointer py-1">
                    <input
                      type="radio"
                      name="parent"
                      checked={newScenarioParentId === null}
                      onChange={() => setNewScenarioParentId(null)}
                      className="w-4 h-4"
                      style={{ accentColor: 'var(--accent)' }}
                    />
                    <span className="text-sm" style={{ color: 'var(--text-heading)' }}>
                      {t('unfold.sections.rootNoParent')}
                    </span>
                  </label>
                  {scenarioTree.map((scenario) => {
                    const renderScenarioOption = (s: ProjectScenario & { children?: ProjectScenario[] }, level = 0) => (
                      <React.Fragment key={s.id}>
                        <label 
                          className="flex items-center gap-2 cursor-pointer py-1"
                          style={{ paddingLeft: `${level * 20}px` }}
                        >
                          <input
                            type="radio"
                            name="parent"
                            checked={newScenarioParentId === s.id}
                            onChange={() => setNewScenarioParentId(s.id)}
                            className="w-4 h-4"
                            style={{ accentColor: 'var(--accent)' }}
                          />
                          <span className="text-sm truncate" style={{ color: 'var(--text-heading)' }}>
                            {s.title}
                          </span>
                        </label>
                        {s.children && s.children.map((child) => renderScenarioOption(child, level + 1))}
                      </React.Fragment>
                    );
                    return renderScenarioOption(scenario);
                  })}
                </div>
              </div>
            </div>

            {/* Actions */}
            <div className="flex items-center justify-end gap-2 mt-4 pt-4 border-t flex-shrink-0" style={{ borderColor: 'var(--overlay-line-90)' }}>
              <button
                onClick={() => setShowNewScenarioModal(false)}
                className="px-4 py-2 rounded-lg transition-colors text-sm"
                style={{
                  backgroundColor: 'transparent',
                  color: 'var(--text-secondary-strong)',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                }}
                disabled={creatingScenario}
              >
                {t('unfold.sections.renameChatCancel')}
              </button>
              <button
                onClick={handleCreateScenario}
                disabled={!newScenarioTitle.trim() || creatingScenario}
                className="px-4 py-2 rounded-lg transition-colors text-sm font-medium"
                style={{
                  backgroundColor: newScenarioTitle.trim() && !creatingScenario 
                    ? 'var(--accent)' 
                    : 'var(--white-06)',
                  color: newScenarioTitle.trim() && !creatingScenario 
                    ? 'white' 
                    : 'var(--text-tertiary-weak)',
                  cursor: newScenarioTitle.trim() && !creatingScenario ? 'pointer' : 'not-allowed',
                }}
                onMouseEnter={(e) => {
                  if (newScenarioTitle.trim() && !creatingScenario) {
                    (e.currentTarget as HTMLButtonElement).style.opacity = '0.9';
                  }
                }}
                onMouseLeave={(e) => {
                  if (newScenarioTitle.trim() && !creatingScenario) {
                    (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                  }
                }}
              >
                {creatingScenario ? t('unfold.sections.creating') : t('unfold.sections.create')}
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* Transfer lens dialog */}
      {transferringLens && createPortal(
        <div
          role="dialog"
          aria-modal="true"
          onClick={() => closeTransferDialog()}
          style={{
            position: 'fixed',
            inset: 0,
            backgroundColor: 'rgba(0,0,0,0.6)',
            zIndex: 10000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              backgroundColor: 'var(--bg-elevated)',
              borderRadius: 12,
              padding: 20,
              minWidth: 320,
              maxWidth: 400,
              maxHeight: '80vh',
              overflow: 'auto',
              border: '1px solid var(--border-subtle)',
              boxShadow: '0 18px 50px rgba(0,0,0,0.35)',
            }}
          >
            <div style={{ fontWeight: 600, fontSize: 16, marginBottom: 12, color: 'var(--text-heading)' }}>
              Transfer Lens
            </div>
            <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)', marginBottom: 16 }}>
              Move "<strong>{transferringLens.name}</strong>" to another project:
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, maxHeight: 300, overflowY: 'auto' }}>
              {memoizedProjects
                .filter((p) => p.id !== transferringLens.projectId)
                .map((project) => (
                  <button
                    key={project.id}
                    type="button"
                    disabled={transferInProgress}
                    onClick={() => void transferLens(project.id)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                      padding: '10px 12px',
                      borderRadius: 8,
                      border: 'none',
                      backgroundColor: 'var(--white-06)',
                      color: 'var(--text-heading)',
                      cursor: transferInProgress ? 'not-allowed' : 'pointer',
                      textAlign: 'left',
                      fontSize: 13,
                      opacity: transferInProgress ? 0.6 : 1,
                    }}
                    onMouseEnter={(e) => {
                      if (!transferInProgress) {
                        (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                      }
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-06)';
                    }}
                  >
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
                    </svg>
                    <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {project.displayName || 'Untitled Project'}
                    </span>
                  </button>
                ))}
              {memoizedProjects.filter((p) => p.id !== transferringLens.projectId).length === 0 && (
                <div style={{ fontSize: 13, color: 'var(--text-tertiary-weak)', textAlign: 'center', padding: 20 }}>
                  No other projects available
                </div>
              )}
            </div>
            <div style={{ marginTop: 16, display: 'flex', justifyContent: 'flex-end' }}>
              <button
                type="button"
                onClick={() => closeTransferDialog()}
                disabled={transferInProgress}
                style={{
                  padding: '8px 16px',
                  borderRadius: 6,
                  border: 'none',
                  backgroundColor: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                  fontSize: 13,
                }}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}
    </>
  );
};
