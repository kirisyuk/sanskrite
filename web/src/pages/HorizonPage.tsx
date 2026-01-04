import React from 'react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../contexts/AuthContext';
import { ThemeContext } from '../components/ThemeProvider';
import { SidebarPanel } from '../components/SidebarPanel';
import { useProjectsStore } from '../stores/projectsStore';
import { useUIStore } from '../stores/uiStore';
import { SettingsModal } from '../components/SettingsModal';
import { AccountModal } from '../components/AccountModal';
import { AdminModal } from '../components/AdminModal';
import { LensCanvas } from '../components/LensCanvas';
import AssembleLensRequestComponent, { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';
import { api } from '../lib/api';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import { useWebSocketConnection } from '../hooks/useWebSocketConnection';

const HorizonPage: React.FC = () => {
  const { t } = useTranslation();
  const { currentUser } = useAuth();
  const { theme } = React.useContext(ThemeContext);
  const projects = useProjectsStore((s) => s.projects);
  const fetchProjects = useProjectsStore((s) => s.fetchProjects);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const deleteProject = useProjectsStore((s) => s.deleteProject);
  const updateProject = useProjectsStore((s) => s.updateProject);
  
  const [sidebarOpen, setSidebarOpen] = React.useState(true);
  const [showSettings, setShowSettings] = React.useState(false);
  const [showAccount, setShowAccount] = React.useState(false);
  const [showAdmin, setShowAdmin] = React.useState(false);
  
  // Load saved Horizon state from localStorage
  const getSavedHorizonState = React.useCallback(() => {
    try {
      const savedProjectId = localStorage.getItem('horizon.selectedProjectId');
      const savedLensId = localStorage.getItem('horizon.selectedLensId');
      return {
        projectId: savedProjectId || null,
        lensId: savedLensId || null,
      };
    } catch {
      return { projectId: null, lensId: null };
    }
  }, []);

  // Save Horizon state to localStorage
  const saveHorizonState = React.useCallback((projectId: string | null, lensId: string | null) => {
    try {
      if (projectId) {
        localStorage.setItem('horizon.selectedProjectId', projectId);
      } else {
        localStorage.removeItem('horizon.selectedProjectId');
      }
      if (lensId) {
        localStorage.setItem('horizon.selectedLensId', lensId);
      } else {
        localStorage.removeItem('horizon.selectedLensId');
      }
    } catch (err) {
      console.warn('[HorizonPage] Failed to save state to localStorage:', err);
    }
  }, []);

  const [selectedLensId, setSelectedLensIdState] = React.useState<string | null>(() => {
    return getSavedHorizonState().lensId;
  });
  
  // Wrapper for setSelectedLensId that also saves to localStorage
  const setSelectedLensId = React.useCallback((lensId: string | null) => {
    setSelectedLensIdState(lensId);
    const currentProjectId = currentProject?.id || null;
    saveHorizonState(currentProjectId, lensId);
  }, [currentProject?.id, saveHorizonState]);

  const assembly = useHorizonRealtimeStore((s) => s.assembly);
  const events = useHorizonRealtimeStore((s) => s.events);
  const resetEvents = useHorizonRealtimeStore((s) => s.resetEvents);
  const [lensStarted, setLensStarted] = React.useState(false);
  const [isCreatingLens, setIsCreatingLens] = React.useState(false);
  const [activeCreateCorrelationId, setActiveCreateCorrelationId] = React.useState<string | null>(null);
  const [activeCreateSessionId, setActiveCreateSessionId] = React.useState<string | null>(null);
  const creatingLensRef = React.useRef(false);
  const feedRef = React.useRef<HTMLDivElement | null>(null);
  
  const isAdmin = currentUser?.email === 'igor@hatl.one' || currentUser?.email === 'admin@hatl.one';
  const isMobile = React.useMemo(() => window.innerWidth < 768, []);
  
  // Detect smartphone (smaller than Galaxy Fold, typically < 600px width)
  // Rules:
  // 1. Desktop (> 768px): desktop interface
  // 2. Tablet/Galaxy Fold (600-768px): fold mode (medium) - reduced but no major simplifications
  // 3. Small smartphone (< 600px): simplified smartphone interface (both portrait and landscape)
  //    Simplified interface is especially useful in portrait orientation
  const [isSmartphone, setIsSmartphone] = React.useState(false);
  React.useEffect(() => {
    const checkSmartphone = () => {
      // Simplified smartphone interface for screens <= 600px (both orientations)
      setIsSmartphone(window.innerWidth <= 600);
    };
    checkSmartphone();
    window.addEventListener('resize', checkSmartphone);
    // Also listen to orientation changes to update when device rotates
    if (window.screen?.orientation) {
      window.screen.orientation.addEventListener('change', checkSmartphone);
      return () => {
        window.removeEventListener('resize', checkSmartphone);
        window.screen.orientation.removeEventListener('change', checkSmartphone);
      };
    }
    return () => window.removeEventListener('resize', checkSmartphone);
  }, []);
  
  // Editing state for projects
  const [editingProjectId, setEditingProjectId] = React.useState<string | null>(null);
  const [editingProjectName, setEditingProjectName] = React.useState('');
  const editingInputRef = React.useRef<HTMLInputElement | null>(null);
  
  // Load projects on mount
  React.useEffect(() => {
    if (currentUser) {
      fetchProjects();
    }
  }, [currentUser, fetchProjects]);

  // Restore saved Horizon state on mount and when projects are loaded
  const [hasRestoredProject, setHasRestoredProject] = React.useState(false);
  const [hasRestoredLens, setHasRestoredLens] = React.useState(false);
  
  // Step 1: Restore project selection
  React.useEffect(() => {
    if (!currentUser || projects.length === 0 || hasRestoredProject) return;
    
    const saved = getSavedHorizonState();
    
    if (saved.projectId && !currentProject) {
      const project = projects.find((p) => p.id === saved.projectId && p.projectType === 'horizon');
      if (project) {
        setCurrentProject(project);
        setHasRestoredProject(true);
      } else {
        // Project not found, clear saved state
        setHasRestoredProject(true);
      }
    } else {
      setHasRestoredProject(true);
    }
  }, [currentUser, projects, currentProject, getSavedHorizonState, setCurrentProject, hasRestoredProject]);

  // Step 2: Restore lens selection after project is restored
  React.useEffect(() => {
    if (!hasRestoredProject || hasRestoredLens || !currentProject) return;
    
    const saved = getSavedHorizonState();
    
    if (currentProject.id === saved.projectId && saved.lensId) {
      setSelectedLensId(saved.lensId);
    } else if (currentProject.id !== saved.projectId) {
      // Project changed, clear lens selection
      setSelectedLensId(null);
    }
    
    setHasRestoredLens(true);
  }, [hasRestoredProject, hasRestoredLens, currentProject, getSavedHorizonState, setSelectedLensId]);

  // Track previous project ID to detect project changes
  const prevProjectIdRef = React.useRef<string | null>(null);
  
  // Save project selection when currentProject changes (from SidebarPanel)
  React.useEffect(() => {
    if (!hasRestoredLens) {
      // Store current project ID for next render
      prevProjectIdRef.current = currentProject?.id || null;
      return; // Don't save during initial restore
    }
    
    const currentProjectId = currentProject?.id || null;
    const projectChanged = prevProjectIdRef.current !== currentProjectId;
    
    if (currentProject?.projectType === 'horizon') {
      // If project changed, clear lens selection (lens belongs to old project)
      if (projectChanged && prevProjectIdRef.current !== null) {
        setSelectedLensId(null);
        saveHorizonState(currentProjectId, null);
      } else {
        // Same project, save current lens selection
        const currentLensId = selectedLensId;
        saveHorizonState(currentProjectId, currentLensId);
      }
    } else if (!currentProject) {
      // Project was cleared, also clear lens
      setSelectedLensId(null);
      saveHorizonState(null, null);
    }
    
    prevProjectIdRef.current = currentProjectId;
  }, [currentProject, selectedLensId, saveHorizonState, hasRestoredLens, setSelectedLensId]);

  // Initialize WebSocket connection (same flow as Discovery).
  useWebSocketConnection();
  
  // Focus input when editing starts
  React.useEffect(() => {
    if (editingProjectId && editingInputRef.current) {
      editingInputRef.current.focus();
      editingInputRef.current.select();
    }
  }, [editingProjectId]);
  
  const handleDeleteChat = React.useCallback(async (projectId: string, projectName: string) => {
    if (!confirm(`Delete "${projectName}"?`)) return;
    try {
      // Check if the deleted project is currently open
      const isCurrentProject = currentProject?.id === projectId;
      
      await deleteProject(projectId);
      
      // If we deleted the current project, reset it
      if (isCurrentProject) {
        setCurrentProject(null);
        setSelectedLensId(null);
        saveHorizonState(null, null);
      }
    } catch (err) {
      console.error('Failed to delete project:', err);
    }
  }, [currentProject, deleteProject, setCurrentProject]);
  
  const handleRenameChat = React.useCallback(async (projectId: string, newName: string) => {
    if (!newName.trim()) {
      setEditingProjectId(null);
      setEditingProjectName('');
      return;
    }
    try {
      await updateProject(projectId, { displayName: newName.trim() });
      setEditingProjectId(null);
      setEditingProjectName('');
    } catch (err) {
      console.error('Failed to rename project:', err);
    }
  }, [updateProject]);
  
  const startEditingProject = React.useCallback((projectId: string, currentName: string) => {
    setEditingProjectId(projectId);
    setEditingProjectName(currentName);
  }, []);
  
  const handleResetChat = React.useCallback(() => {
    setCurrentProject(null);
    setSelectedLensId(null);
    saveHorizonState(null, null);
  }, [setCurrentProject, setSelectedLensId, saveHorizonState]);
  
  const handleCloseAllModals = React.useCallback(() => {
    setShowSettings(false);
    setShowAccount(false);
    setShowAdmin(false);
    setSelectedLensId(null);
  }, []);

  const handleAssembleLens = React.useCallback(async (request: AssembleLensRequest) => {
    // Guard against double-submit (e.g. double Enter / fast double click)
    if (creatingLensRef.current) return;
    creatingLensRef.current = true;
    setIsCreatingLens(true);

    try {
      // Two creation modes:
      // 1) Prefetch-only (chips only): create lens with explicit scope (no scouting).
      // 2) Query (+ optional prefetch scope): create lens from NL query (scouting), merging prefetchScope into final scope.
      const correlationId = (globalThis.crypto && 'randomUUID' in globalThis.crypto)
        ? (globalThis.crypto as any).randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
      const sessionId = (window as any).__horizonSessionId || correlationId;
      (window as any).__horizonSessionId = sessionId;

      // Mark current create attempt so Progress view can ignore unrelated background jobs.
      setActiveCreateCorrelationId(correlationId);
      setActiveCreateSessionId(sessionId);

      resetEvents();
      setLensStarted(true);

      const q = String(request.query || '').trim();
      const prefetchScope = Array.from(
        new Set(
          (request.prefetchScope || [])
            .map((x) => String(x || '').trim())
            .filter(Boolean),
        ),
      );

      // Mode 1: no free-text query — create a lens directly from preselected scope.
      if (!q && prefetchScope.length > 0) {
        const defaultName = prefetchScope.length === 1
          ? prefetchScope[0].split('/').filter(Boolean).slice(-1)[0] || 'Lens'
          : `Lens (${prefetchScope.length})`;
        const lens = await api.createHorizonLens(
          {
            projectId: request.projectId,
            name: String(request.name || '').trim() || defaultName,
            scope: prefetchScope,
            status: 'ACTIVE',
          },
          { correlationId, sessionId },
        );

        if (lens?.id) {
          setSelectedLensId(lens.id);
          window.dispatchEvent(new CustomEvent('horizon:lens-created', {
            detail: { lensId: lens.id, projectId: request.projectId },
          }));
        } else {
          setSelectedLensId(null);
        }
        return;
      }

      // Mode 2: query-based lens creation (scouting) — merge prefetch scope into final scope server-side.
      const result = await api.createHorizonLensFromQuery(
        {
          projectId: request.projectId,
          query: q,
          maxRecursionDepth: request.filters?.maxRecursionDepth ?? 1,
          prefetchScope: prefetchScope.length > 0 ? prefetchScope : undefined,
        },
        { correlationId, sessionId }
      );

      // Save new lens ID and open it in Progress view
      if (result?.lens?.id) {
        setSelectedLensId(result.lens.id);
        // Dispatch event to refresh lenses list in SidebarPanel
        window.dispatchEvent(new CustomEvent('horizon:lens-created', { 
          detail: { lensId: result.lens.id, projectId: request.projectId } 
        }));
      } else {
        // Keep Hook UI visible; LensCanvas can be opened manually from Sidebar lens list.
        setSelectedLensId(null);
      }
    } catch (error) {
      console.error('Failed to assemble lens:', error);
    } finally {
      creatingLensRef.current = false;
      setIsCreatingLens(false);
      setActiveCreateCorrelationId(null);
      setActiveCreateSessionId(null);
    }
  }, [resetEvents]);

  // Auto-scroll realtime feed to bottom while started.
  React.useEffect(() => {
    if (!lensStarted) return;
    const el = feedRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lensStarted, events.length]);
  
  return (
    <div
      className="flex h-full"
      style={{ padding: isMobile ? '8px 0' : '16px 0' }}
    >
      {/* Side panel */}
      <SidebarPanel
        isOpen={sidebarOpen}
        onOpen={() => setSidebarOpen(true)}
        onClose={() => setSidebarOpen(false)}
        isMobile={isMobile}
        isAdmin={isAdmin}
        onResetChat={handleResetChat}
        onShowSettings={() => {
          handleCloseAllModals();
          setShowSettings(true);
        }}
        onShowAccount={() => {
          handleCloseAllModals();
          setShowAccount(true);
        }}
        onShowAdmin={() => {
          handleCloseAllModals();
          setShowAdmin(true);
        }}
        onCloseAllModals={handleCloseAllModals}
        editingProjectId={editingProjectId}
        editingProjectName={editingProjectName}
        editingInputRef={editingInputRef}
        onEditingProjectIdChange={setEditingProjectId}
        onEditingProjectNameChange={setEditingProjectName}
        onDeleteChat={handleDeleteChat}
        onRenameChat={handleRenameChat}
        onStartEditingProject={startEditingProject}
        currentMode="horizon"
        projectId={currentProject?.id || null}
        onSelectLens={(lensId) => {
          setSelectedLensId(lensId);
        }}
        selectedLensId={selectedLensId}
        onNewLens={() => {
          // Ensure project is selected (should already be selected when hovering, but double-check)
          // Clear states and show input field for new lens
          setSelectedLensId(null);
          setLensStarted(false);
          resetEvents();
          setActiveCreateCorrelationId(null);
          setActiveCreateSessionId(null);
          // Note: currentProject should already be set from SidebarPanel project selection
          // LensCanvas will use currentProject.id as projectId for creating the lens
        }}
      />

      {/* Main content area */}
      <div
        className="flex-1 min-h-0"
        style={{
          position: 'relative',
          display: 'flex',
          gap: '16px',
        }}
      >
        {/* 2% left margin for breathing room (hidden on smartphones) */}
        {!isSmartphone && <div style={{ width: '2%', minWidth: '2%', flexShrink: 0 }} />}

        {/* Content area - flexible width */}
        <div
          className="flex-1 min-h-0"
          style={{
            display: 'flex',
            flexDirection: 'column',
            minWidth: 0,
            position: 'relative',
          }}
        >
          {/* Lens Canvas overlay - show Progress view for both project and lens selection */}
          {currentProject ? (
            <LensCanvas
              isOpen={true}
              lensId={selectedLensId}
              projectId={currentProject.id}
              initialViewMode={selectedLensId ? 'cockpit' : 'progress'}
              showInputInProgress={true}
              onAssemble={handleAssembleLens}
              isCreatingLens={isCreatingLens}
              activeCreateCorrelationId={activeCreateCorrelationId}
              activeCreateSessionId={activeCreateSessionId}
              onClose={() => {
                // Only close lens if one is selected, otherwise just reset state
                if (selectedLensId) {
                  setSelectedLensId(null);
                }
                setLensStarted(false);
                setActiveCreateCorrelationId(null);
                setActiveCreateSessionId(null);
              }}
            />
          ) : (
            <div style={{ 
              flex: 1,
              display: 'flex',
              alignItems: 'center', 
              justifyContent: 'center',
              padding: '2rem',
              color: 'var(--text-secondary)'
            }}>
              <div style={{ textAlign: 'center' }}>
                <h1 style={{ fontSize: '2rem', marginBottom: '1rem', color: 'var(--text-primary)' }}>
                  Horizon
                </h1>
                <p>Risk monitoring and dependency analysis</p>
                <p style={{ marginTop: '1rem', fontSize: '0.875rem' }}>
                  Select a project from the sidebar to begin.
                </p>
              </div>
            </div>
          )}
        </div>
      </div>
      
      {showSettings && <SettingsModal isOpen={showSettings} onClose={() => setShowSettings(false)} />}
      {showAccount && <AccountModal isOpen={showAccount} onClose={() => setShowAccount(false)} />}
      {showAdmin && isAdmin && <AdminModal isOpen={showAdmin} onClose={() => setShowAdmin(false)} />}
    </div>
  );
};

export default HorizonPage;

