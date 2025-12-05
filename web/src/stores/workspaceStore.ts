import { create } from 'zustand';
import type { Project } from '../types';
import type { CoreDoc } from '../types/generated';
import type { DataSource } from '../types/generated';

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

interface DocumentBigQueryStatus {
  available: boolean;
  loading: boolean;
  lastChecked: number;
}

interface DocumentVertexSearchAvailabilityStatus {
  available: boolean;
  loading: boolean;
  lastChecked: number;
}

interface Source {
  id: string;
  displayName: string;
  kind: string;
  status: 'active' | 'paused' | 'completed' | 'fetching' | 'embedded' | 'processing';
  lastPolledAt?: string;
  nextPollAt?: string;
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
  bigqueryStatuses: Record<string, DocumentBigQueryStatus>; // docId -> status
  vertexSearchAvailabilityStatuses: Record<string, DocumentVertexSearchAvailabilityStatus>; // docId -> status
  projectSources: Record<string, DataSource[]>; // projectId -> sources
  sourceStatusUpdates: Record<string, number>; // sourceId -> timestamp of last status change
  
  // Actions
  setProjectsWithDocs: (projects: ProjectWithDocs[]) => void;
  setLoading: (loading: boolean) => void;
  updateProjectDocs: (project: Project, documents: CoreDoc[], discussionFrame?: string) => void;
  setDocumentIndexStatus: (docId: string, indexed: boolean, loading?: boolean) => void;
  setDocumentBigQueryStatus: (docId: string, available: boolean, loading?: boolean) => void;
  setDocumentVertexSearchAvailabilityStatus: (docId: string, available: boolean, loading?: boolean) => void;
  setProjectSources: (projectId: string, sources: DataSource[]) => void;
  updateSourceStatus: (sourceId: string, newStatus: 'active' | 'paused') => void;
  clear: () => void;
}

export const useWorkspaceStore = create<WorkspaceState>((set, get) => ({
  projectsWithDocs: [],
  loading: false,
  lastFetchTime: null,
  indexStatuses: {},
  bigqueryStatuses: {},
  vertexSearchAvailabilityStatuses: {},
  projectSources: {},
  sourceStatusUpdates: {},

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

  setDocumentBigQueryStatus: (docId, available, loading = false) => set((state) => ({
    bigqueryStatuses: {
      ...state.bigqueryStatuses,
      [docId]: {
        available,
        loading,
        lastChecked: Date.now(),
      },
    },
  })),

  setDocumentVertexSearchAvailabilityStatus: (docId, available, loading = false) => set((state) => ({
    vertexSearchAvailabilityStatuses: {
      ...state.vertexSearchAvailabilityStatuses,
      [docId]: {
        available,
        loading,
        lastChecked: Date.now(),
      },
    },
  })),

  setProjectSources: (projectId, sources) => set((state) => ({
    projectSources: {
      ...state.projectSources,
      [projectId]: sources,
    },
  })),

  updateSourceStatus: (sourceId, newStatus) => set((state) => {
    // Update status in all projects that have this source
    const updatedProjectSources: Record<string, DataSource[]> = {};
    for (const [projectId, sources] of Object.entries(state.projectSources)) {
      const updatedSources = sources.map(s => 
        s.id === sourceId ? { ...s, status: newStatus } : s
      );
      if (updatedSources.some(s => s.id === sourceId)) {
        updatedProjectSources[projectId] = updatedSources;
      }
    }
    
    return {
      projectSources: {
        ...state.projectSources,
        ...updatedProjectSources,
      },
      sourceStatusUpdates: {
        ...state.sourceStatusUpdates,
        [sourceId]: Date.now(),
      },
    };
  }),

  clear: () => set({ 
    projectsWithDocs: [],
    loading: false,
    lastFetchTime: null,
    indexStatuses: {},
    bigqueryStatuses: {},
    vertexSearchAvailabilityStatuses: {},
    projectSources: {},
    sourceStatusUpdates: {},
  }),
}));

