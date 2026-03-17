import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { api } from '../lib/api';

type ModalType = 'settings' | 'account' | 'admin' | 'workspace' | null;
type StaticPageType = 'privacy' | 'terms' | 'technical-disclosure' | null;

export interface SettingsModalParams {
  initialTab?: 'personal' | 'corporate' | 'guardians';
  highlightCheckbox?: 'disableBiasEvaluation' | 'disableFrameExpansion' | 'disableComplianceEvaluation';
}

/**
 * Hook для управления модальными окнами и статическими страницами
 */
export function useModals() {
  const { currentUser } = useAuth();
  const [activeModal, setActiveModal] = useState<ModalType>(null);
  const [staticPage, setStaticPage] = useState<StaticPageType>(null);
  const [isAdmin, setIsAdmin] = useState(false);
  const [workspaceProjectId, setWorkspaceProjectId] = useState<string | null>(null);
  const [settingsParams, setSettingsParams] = useState<SettingsModalParams | null>(null);

  // Background-load user preferred language and admin status
  useEffect(() => {
    const loadUserData = async () => {
      try {
        const uid = currentUser?.uid;
        if (!uid) return;
        const user = await api.getUser(uid);
        try {
          const role = (user as any)?.role || '';
          setIsAdmin(
            typeof role === 'string' && role.toLowerCase() === 'admin',
          );
        } catch {}
        const code = (
          user?.preferredLanguage ||
          localStorage.getItem('preferredLanguage') ||
          'EN'
        ).toUpperCase();
        if (code && code.length === 2) {
          localStorage.setItem('preferredLanguage', code);
        }
      } catch {}
    };
    loadUserData();
  }, [currentUser?.uid]);

  const closeAllModals = useCallback(() => {
    setActiveModal(null);
    setStaticPage(null);
    setSettingsParams(null);
  }, []);

  const openSettings = useCallback((params?: SettingsModalParams) => {
    closeAllModals();
    setSettingsParams(params || null);
    setActiveModal('settings');
  }, [closeAllModals]);

  const openAccount = useCallback(() => {
    closeAllModals();
    setActiveModal('account');
  }, [closeAllModals]);

  const openAdmin = useCallback(() => {
    closeAllModals();
    setActiveModal('admin');
  }, [closeAllModals]);

  const openWorkspace = useCallback((projectId?: string | null) => {
    console.log('[useModals] openWorkspace called', { projectId });
    closeAllModals();
    setWorkspaceProjectId(projectId || null);
    setActiveModal('workspace');
    console.log('[useModals] openWorkspace completed, modal should be workspace');
  }, [closeAllModals]);

  const closeWorkspace = useCallback(() => {
    setActiveModal(null);
    setWorkspaceProjectId(null);
  }, []);

  return {
    showSettings: activeModal === 'settings',
    showAccount: activeModal === 'account',
    showAdmin: activeModal === 'admin',
    showWorkspace: activeModal === 'workspace',
    workspaceProjectId,
    settingsParams,
    staticPage,
    setStaticPage,
    isAdmin,
    openSettings,
    openAccount,
    openAdmin,
    openWorkspace,
    closeAllModals,
    closeWorkspace,
  };
}
