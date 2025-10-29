import { create } from 'zustand';

export interface ArtifactSection {
  id: string;
  title: string;
  status: 'pending' | 'generating' | 'completed' | 'error';
  content?: string;
  error?: string;
}

export interface ArtifactGenerationState {
  sessionId: string | null;
  artifactId: string | null;
  status: 'idle' | 'started' | 'generating_section' | 'assembling' | 'completed' | 'error';
  error: string | null;
  sections: ArtifactSection[];
  currentSectionId: string | null;
  progress: number; // 0-1
  assembledContent: string | null;

  // Actions
  handleEvent: (event: any) => void;
  reset: () => void;
}

const initialState = {
  sessionId: null,
  artifactId: null,
  status: 'idle' as const,
  error: null,
  sections: [],
  currentSectionId: null,
  progress: 0,
  assembledContent: null,
};

export const useArtifactStore = create<ArtifactGenerationState>((set, get) => ({
  ...initialState,
  reset: () => set(initialState),
  handleEvent: (event) => {
    const { type, data } = event;
    if (!type || !data) return;

    switch (type) {
      case 'artifact.generation.started':
        set({
          sessionId: data.sessionId,
          artifactId: data.artifactId,
          status: 'started',
          error: null,
          progress: 0,
          sections: (data.sections || []).map((s: any) => ({
            id: s.id,
            title: s.title,
            status: 'pending',
          })),
        });
        break;

      case 'artifact.section.generating':
        set((state) => ({
          status: 'generating_section',
          currentSectionId: data.sectionId,
          sections: state.sections.map((s) =>
            s.id === data.sectionId ? { ...s, status: 'generating' } : s
          ),
        }));
        break;

      case 'artifact.section.chunk':
        set((state) => ({
          sections: state.sections.map((s) =>
            s.id === data.sectionId
              ? { ...s, content: (s.content || '') + data.chunk }
              : s
          ),
        }));
        break;

      case 'artifact.section.completed':
        set((state) => ({
          sections: state.sections.map((s) =>
            s.id === data.sectionId ? { ...s, status: 'completed' as const } : s
          ),
          progress: ((): number => {
            const newSections = state.sections.map((s) =>
              s.id === data.sectionId ? { ...s, status: 'completed' as const } : s
            );
            const completedCount = newSections.filter((s) => s.status === 'completed').length;
            return newSections.length > 0 ? completedCount / newSections.length : 0;
          })(),
          currentSectionId: null,
        }));
        break;

      case 'artifact.assembly.started':
        set({ status: 'assembling' });
        break;

      case 'artifact.assembly.chunk':
        set((state) => ({
            assembledContent: (state.assembledContent || '') + data.chunk,
        }));
        break;

      case 'artifact.assembly.completed':
        set((state) => ({
            status: 'completed',
            progress: 1,
            assembledContent: data.content || state.assembledContent,
        }));
        break;

      case 'artifact.generation.completed':
        set({
          status: 'completed',
          progress: 1,
          currentSectionId: null,
        });
        break;

      case 'artifact.generation.error':
        set((state) => ({
          status: 'error',
          error: data.error,
          sections: state.sections.map((s) =>
            s.id === data.sectionId ? { ...s, status: 'error', error: data.error } : s
          ),
        }));
        break;
    }
  },
}));
