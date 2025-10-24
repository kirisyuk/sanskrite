import { create } from 'zustand';

type ViewMode = 'unfold' | 'discussion' | 'graph';

interface UIState {
  viewMode: ViewMode;
  setViewMode: (mode: ViewMode) => void;
  uiLanguage: string;
  setUiLanguage: (lang: string) => void;
}

export const useUIStore = create<UIState>((set) => ({
  viewMode: 'unfold',
  setViewMode: (mode) => set({ viewMode: mode }),
  uiLanguage: 'en',
  setUiLanguage: (lang) => set({ uiLanguage: lang }),
}));


