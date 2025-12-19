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

const HorizonPage: React.FC = () => {
  const { t } = useTranslation();
  const { currentUser } = useAuth();
  const { theme } = React.useContext(ThemeContext);
  const projects = useProjectsStore((s) => s.projects);
  const fetchProjects = useProjectsStore((s) => s.fetchProjects);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const currentProject = useProjectsStore((s) => s.currentProject);
  
  const [sidebarOpen, setSidebarOpen] = React.useState(true);
  const [showSettings, setShowSettings] = React.useState(false);
  const [showAccount, setShowAccount] = React.useState(false);
  const [showAdmin, setShowAdmin] = React.useState(false);
  
  const isAdmin = currentUser?.email === 'igor@hatl.one' || currentUser?.email === 'admin@hatl.one';
  const isMobile = React.useMemo(() => window.innerWidth < 768, []);
  
  // Detect smartphone (smaller than Galaxy Fold, typically < 600px width)
  const [isSmartphone, setIsSmartphone] = React.useState(false);
  React.useEffect(() => {
    const checkSmartphone = () => {
      setIsSmartphone(window.innerWidth <= 600);
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
      await useProjectsStore.getState().deleteProject(projectId);
    } catch (err) {
      console.error('Failed to delete project:', err);
    }
  }, []);
  
  const handleRenameChat = React.useCallback(async (projectId: string, newName: string) => {
    if (!newName.trim()) {
      setEditingProjectId(null);
      setEditingProjectName('');
      return;
    }
    try {
      await useProjectsStore.getState().updateProject(projectId, { displayName: newName.trim() });
      setEditingProjectId(null);
      setEditingProjectName('');
    } catch (err) {
      console.error('Failed to rename project:', err);
    }
  }, []);
  
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
  }, []);
  
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
        currentMode="chat"
        projectId={currentProject?.id || null}
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
          }}
        >
          {/* Main content - placeholder for now */}
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
                Select a Horizon project from the sidebar to begin.
              </p>
            </div>
          </div>
        </div>
      </div>
      
      {showSettings && <SettingsModal onClose={() => setShowSettings(false)} />}
      {showAccount && <AccountModal onClose={() => setShowAccount(false)} />}
      {showAdmin && isAdmin && <AdminModal onClose={() => setShowAdmin(false)} />}
    </div>
  );
};

export default HorizonPage;

