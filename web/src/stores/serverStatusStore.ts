import { create } from 'zustand';

interface ServerStatusState {
  isAvailable: boolean;
  errorMessage: string | null;
  lastErrorTime: number | null;
  
  // Actions
  setServerUnavailable: (message?: string) => void;
  setServerAvailable: () => void;
  clearError: () => void;
}

export const useServerStatusStore = create<ServerStatusState>((set) => ({
  isAvailable: true,
  errorMessage: null,
  lastErrorTime: null,

  setServerUnavailable: (message = 'Server is temporarily unavailable') => {
    set({
      isAvailable: false,
      errorMessage: message,
      lastErrorTime: Date.now(),
    });
  },

  setServerAvailable: () => {
    set({
      isAvailable: true,
      errorMessage: null,
      lastErrorTime: null,
    });
  },

  clearError: () => {
    set({
      errorMessage: null,
    });
  },
}));

