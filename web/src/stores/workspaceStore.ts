import { create } from 'zustand';
import type { Project } from '../types';
import type { CoreDoc } from '../types/generated';

interface ProjectWithDocs {
  project: Project;
  documents: CoreDoc[];
  discussionFrame?: string;
  lastUpdated: number;
}

interface DocumentIndexStatus {
  indexed: boolean;
  loading: boolean;
  lastChecked: number;
}

const areDocsEqual = (a: CoreDoc[], b: CoreDoc[]) => {
  if (a === b) return true;
  if (a.length !== b.length) return false;

  const map = new Map<string, CoreDoc>();
  a.forEach((doc) => map.set(doc.id, doc));

  for (const docB of b) {
    const docA = map.get(docB.id);
    if (!docA) {
      return false;
    }
    if (
      docA.filename !== docB.filename ||
      docA.parseStatus !== docB.parseStatus ||
      docA.storagePath !== docB.storagePath ||
      docA.sizeBytes !== docB.sizeBytes
    ) {
      return false;
    }
  }
  return true;
};

interface WorkspaceState {
  projectsWithDocs: ProjectWithDocs[];
  loading: boolean;
  lastFetchTime: number | null;
  indexStatuses: Record<string, DocumentIndexStatus>; // docId -> status
  
  // Actions
  setProjectsWithDocs: (projects: ProjectWithDocs[]) => void;
  setLoading: (loading: boolean) => void;
  updateProjectDocs: (project: Project, documents: CoreDoc[], discussionFrame?: string) => void;
  setDocumentIndexStatus: (docId: string, indexed: boolean, loading?: boolean) => void;
  clear: () => void;
}

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  projectsWithDocs: [],
  loading: false,
  lastFetchTime: null,
  indexStatuses: {},

  setProjectsWithDocs: (projects) => set({ 
    projectsWithDocs: projects,
    lastFetchTime: Date.now(),
    loading: false,
  }),

  setLoading: (loading) => set({ loading }),

  updateProjectDocs: (project, documents, discussionFrame) => set((state) => {
    const existingIndex = state.projectsWithDocs.findIndex(
      (p) => p.project.id === project.id
    );
    
    const existing = existingIndex >= 0 ? state.projectsWithDocs[existingIndex] : undefined;
    const docsUnchanged = existing ? areDocsEqual(existing.documents, documents) : false;
    const frameUnchanged = existing ? existing.discussionFrame === discussionFrame : false;

    // If nothing changed, return current state to avoid re-renders
    if (existing && docsUnchanged && frameUnchanged) {
      return state;
    }

    const updated: ProjectWithDocs = {
      project,
      documents,
      discussionFrame,
      lastUpdated: Date.now(),
    };

    if (existingIndex >= 0) {
      const newProjects = [...state.projectsWithDocs];
      newProjects[existingIndex] = updated;
      return { projectsWithDocs: newProjects };
    } else {
      return { projectsWithDocs: [...state.projectsWithDocs, updated] };
    }
  }),

  setDocumentIndexStatus: (docId, indexed, loading = false) => set((state) => ({
    indexStatuses: {
      ...state.indexStatuses,
      [docId]: {
        indexed,
        loading,
        lastChecked: Date.now(),
      },
    },
  })),

  clear: () => set({ 
    projectsWithDocs: [],
    loading: false,
    lastFetchTime: null,
    indexStatuses: {},
  }),
}));

