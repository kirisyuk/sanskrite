import React from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { useProjectsStore } from '../stores/projectsStore';
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
  currentMode?: 'chat' | 'perspectives' | 'models' | 'pages' | 'scenarios';
  projectId?: string | null;
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
  onSelectJob?: (taskId: string | null) => void;
  selectedJobTaskId?: string | null;
}

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
  t: any; // Pass t function as prop
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
  t,
}) => {
  const daysAgo = getDaysSinceLastInteraction(project);
  const timeAgoLabel = daysAgo > 0 ? formatTimeAgo(daysAgo) : null;
  return (
    <div
      className="px-2 py-1 rounded text-xs flex items-center gap-2 group cursor-pointer"
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
            placeholder={t('unfold.sections.renameChatPlaceholder')}
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
          <span
            className="flex-1 truncate"
            style={{
              color: isDeleting ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
              textDecoration: isDeleting ? 'line-through' : 'none',
            }}
          >
            {project.displayName}
          </span>
          {!isDeleting && (
            <div className="relative flex items-center justify-end flex-shrink-0" style={{ width: '56px' }}>
              {timeAgoLabel && (
                <span
                  className="text-[10px] px-1.5 py-0.5 rounded absolute right-0 group-hover:opacity-0 group-hover:pointer-events-none transition-opacity"
                  style={{
                    color: 'var(--text-tertiary-weak)',
                    backgroundColor: 'var(--white-5)',
                  }}
                >
                  {timeAgoLabel}
                </span>
              )}
              <div className="absolute right-0 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                  className="p-1 rounded"
                  title={t('unfold.sections.renameChat')}
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
                  title={t('unfold.sections.deleteChat')}
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
            </div>
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
}) => {
  const { t } = useTranslation();
  // PERFORMANCE: Subscribe to projects array directly, but use shallow comparison
  // We need to track displayName changes for renamed projects
  const projects = useProjectsStore((s) => s.projects);
  const projectsLoading = useProjectsStore((s) => s.loading);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const deletingProjectIdsArray = useProjectsStore((s) => s.deletingProjectIds);
  const deletingProjectIds = React.useMemo(() => new Set(deletingProjectIdsArray), [deletingProjectIdsArray]);
  
  // Memoize projects array with a stable reference to prevent unnecessary re-renders
  // Create a signature string that includes displayName and projectType to detect changes
  const projectsSignature = React.useMemo(() => 
    projects.map((p) => `${p.id}:${p.displayName}:${p.lastInteractionAt || p.createdAt}:${p.projectType || ''}`).join(','),
    [projects]
  );
  
  // Memoized projects array - will update when signature changes
  const memoizedProjects = React.useMemo(() => projects, [projectsSignature]);
  
  // Agent jobs state
  const [agentJobs, setAgentJobs] = React.useState<any[]>([]);
  const [agentJobsLoading, setAgentJobsLoading] = React.useState(false);
  const [deletingJobIds, setDeletingJobIds] = React.useState<Set<string>>(new Set());
  // Track which project the agentJobs belong to, to prevent showing wrong agents during project switch
  const [agentJobsProjectId, setAgentJobsProjectId] = React.useState<string | null>(null);

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
        onSelect={() => {
          console.log('[SidebarPanel] Clicked on project', {
            projectId: project.id,
            projectName: project.displayName,
            currentProjectId: currentProject?.id,
          });
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
      />
    );
  }, [deletingProjectIds, currentProject, editingProjectId, editingProjectName, editingInputRef, onCloseAllModals, setCurrentProject, isMobile, onClose, onStartEditingProject, onDeleteChat, onRenameChat, onEditingProjectIdChange, onEditingProjectNameChange]);

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
            return (
              <div key={project.id}>
                {renderProjectRow(project)}
                {isSelected && projectJobs.length > 0 && (
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
  }, [currentProject, agentJobsProjectId, agentJobs, renderProjectRow, renderAgentJobRow]);
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

  // Listen for pages.updated event to refresh list
  React.useEffect(() => {
    const handlePagesUpdated = (event: CustomEvent) => {
      // Only refresh if the event is for the current project
      if (event.detail?.projectId === projectId && showPages) {
        loadPages();
      }
    };

    window.addEventListener('pages.updated', handlePagesUpdated as EventListener);
    return () => {
      window.removeEventListener('pages.updated', handlePagesUpdated as EventListener);
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
              <button
                type="button"
                aria-label={t('unfold.sections.newChat')}
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
                  // Close all modals (workspace, admin, settings, account) when creating new chat
                  onCloseAllModals?.();
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
          </div>

          {/* Scrollable Chats section */}
          <div
            className="mb-4 flex-1 min-h-0 overflow-y-auto"
            style={{ scrollbarGutter: 'stable' }}
          >
            {showPages ? (
              <>
                <div
                  className="text-[11px] uppercase tracking-wide mb-2"
                  style={{ color: 'var(--text-tertiary-weak)' }}
                >
                  Pages ({pages.length})
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
                                      window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));
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
                        
                        return (
                          <div key={scenario.id}>
                            <div
                              className="px-2 py-1 rounded text-xs flex items-center gap-2 group"
                              style={{
                                backgroundColor: 'transparent',
                                paddingLeft: `${8 + level * 16}px`,
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
                                    // TODO: Handle scenario click
                                    console.log('[SidebarPanel] Scenario clicked', scenario.id);
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
                <div
                  className="text-[11px] uppercase tracking-wide mb-2"
                  style={{ color: 'var(--text-heading)' }}
                >
                  {t('unfold.sections.chats')}
                </div>

                {projectsLoading && memoizedProjects.length === 0 ? (
                  <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                    {t('unfold.sections.loadingChats')}
                  </div>
                ) : memoizedProjects.length === 0 ? (
                  <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                    {t('unfold.sections.noChatsYet')}
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
    </>
  );
};

