import { create } from 'zustand';

type ViewMode = 'unfold' | 'discussion' | 'graph';

interface UIState {
  viewMode: ViewMode;
  setViewMode: (mode: ViewMode) => void;
}

export const useUIStore = create<UIState>((set) => ({
  viewMode: 'discussion',
  setViewMode: (mode) => set({ viewMode: mode }),
}));


