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
  const [selectedLensId, setSelectedLensId] = React.useState<string | null>(null);
  const assembly = useHorizonRealtimeStore((s) => s.assembly);
  const events = useHorizonRealtimeStore((s) => s.events);
  const resetEvents = useHorizonRealtimeStore((s) => s.resetEvents);
  const [lensStarted, setLensStarted] = React.useState(false);
  const feedRef = React.useRef<HTMLDivElement | null>(null);
  
  const isAdmin = currentUser?.email === 'igor@hatl.one' || currentUser?.email === 'admin@hatl.one';
  const isMobile = React.useMemo(() => window.innerWidth < 768, []);
  
  // Detect smartphone (smaller than Galaxy Fold, typically < 600px width)
  // Also use fold mode (medium) for portrait orientation (height > width)
  const [isSmartphone, setIsSmartphone] = React.useState(false);
  React.useEffect(() => {
    const checkSmartphone = () => {
      const isPortrait = window.innerHeight > window.innerWidth;
      // Use fold mode (medium) if portrait orientation, otherwise check width
      setIsSmartphone(window.innerWidth <= 600 && !isPortrait);
    };
    checkSmartphone();
    window.addEventListener('resize', checkSmartphone);
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
  }, [setCurrentProject]);
  
  const handleCloseAllModals = React.useCallback(() => {
    setShowSettings(false);
    setShowAccount(false);
    setShowAdmin(false);
    setSelectedLensId(null);
  }, []);

  const handleAssembleLens = React.useCallback(async (request: AssembleLensRequest) => {
    try {
      // Assemble via NL query -> intent -> lens (backend precheck).
      // Realtime assembly/scout progress is delivered via horizon.* websocket events.
      const correlationId = (globalThis.crypto && 'randomUUID' in globalThis.crypto)
        ? (globalThis.crypto as any).randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
      const sessionId = (window as any).__horizonSessionId || correlationId;
      (window as any).__horizonSessionId = sessionId;

      resetEvents();
      setLensStarted(true);

      await api.createHorizonLensFromQuery(
        {
          projectId: request.projectId,
          query: request.query,
          maxRecursionDepth: request.filters?.maxRecursionDepth ?? 2,
        },
        { correlationId, sessionId }
      );

      // Keep Hook UI visible; LensCanvas can be opened manually from Sidebar lens list.
      setSelectedLensId(null);
    } catch (error) {
      console.error('Failed to assemble lens:', error);
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
        onSelectLens={setSelectedLensId}
        selectedLensId={selectedLensId}
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
          {/* Lens Canvas overlay */}
          {selectedLensId ? (
            <LensCanvas
              isOpen={true}
              lensId={selectedLensId}
              onClose={() => setSelectedLensId(null)}
            />
          ) : (
            /* Main content - Assemble Lens Request */
            <div style={{ 
              flex: 1,
              display: 'flex',
              flexDirection: 'column',
              minHeight: 0,
              height: '100%',
            }}>
              {/* Minimal realtime debug for assembly (can be replaced by dedicated Sled UI) */}
              {assembly?.phase && (
                <div style={{
                  position: 'absolute',
                  top: 12,
                  right: 24,
                  padding: '8px 12px',
                  borderRadius: 10,
                  border: '1px solid var(--overlay-line-90)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-secondary)',
                  fontSize: 12,
                  zIndex: 10,
                }}>
                  <div><strong>Assembly</strong></div>
                  <div>phase: {assembly.phase}</div>
                  {typeof assembly.progress === 'number' && <div>progress: {(assembly.progress * 100).toFixed(0)}%</div>}
                </div>
              )}

              {/* Horizon realtime WS messages (shown above input after start) */}
              {lensStarted && (
                <div
                  ref={feedRef}
                  style={{
                    flex: 1,
                    minHeight: 0,
                    width: '100%',
                    overflowY: 'auto',
                    padding: '16px 24px 0 24px',
                  }}
                >
                  {(events || [])
                    .filter((ev) => !currentProject?.id || !ev.projectId || ev.projectId === currentProject.id)
                    .map((ev) => (
                      <div
                        key={ev.id}
                        style={{
                          padding: '8px 10px',
                          borderRadius: 10,
                          border: '1px solid var(--overlay-line-90)',
                          background: 'var(--bg-elevated)',
                          color: 'var(--text-secondary-strong)',
                          fontSize: 13,
                          marginBottom: 8,
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ fontWeight: 600, color: 'var(--text-heading)', fontSize: 12 }}>
                            {ev.type}
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>
                            {new Date(ev.ts).toLocaleTimeString()}
                          </div>
                        </div>
                        <div style={{ marginTop: 4, whiteSpace: 'pre-wrap' }}>{ev.text}</div>
                      </div>
                    ))}
                </div>
              )}
              {currentProject ? (
                <AssembleLensRequestComponent
                  projectId={currentProject.id}
                  onAssemble={handleAssembleLens}
                  disabled={false}
                  pinnedBottom={lensStarted}
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

