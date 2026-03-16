import { create } from 'zustand';

type ViewMode = 'unfold' | 'discussion' | 'graph';

interface UIState {
  viewMode: ViewMode;
  setViewMode: (mode: ViewMode) => void;
  uiLanguage: string;
  setUiLanguage: (lang: string) => void;
  /** Admin modal: open from TopBar, sidebar, or Horizon Modeler. */
  showAdminModal: boolean;
  setShowAdminModal: (v: boolean) => void;
  openAdminModal: () => void;
  showStrategicInbox: boolean;
  setShowStrategicInbox: (v: boolean) => void;
  openStrategicInbox: () => void;
}

export const useUIStore = create<UIState>((set) => ({
  viewMode: 'unfold',
  setViewMode: (mode) => set({ viewMode: mode }),
  uiLanguage: 'en',
  setUiLanguage: (lang) => set({ uiLanguage: lang }),
  showAdminModal: false,
  setShowAdminModal: (v) => set({ showAdminModal: v }),
  openAdminModal: () => set({ showAdminModal: true }),
  showStrategicInbox: false,
  setShowStrategicInbox: (v) => set({ showStrategicInbox: v }),
  openStrategicInbox: () => set({ showStrategicInbox: true }),
}));

