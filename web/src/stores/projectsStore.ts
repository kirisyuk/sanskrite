import { create } from 'zustand';
import { api } from '../lib/api';
import type { Project } from '../types';

interface ProjectsState {
  projects: Project[];
  currentProject: Project | null;
  loading: boolean;
  error: string | null;
  deletingProjectIds: string[]; // Track projects being deleted for visual feedback
  
  // Actions
  fetchProjects: (view?: string) => Promise<void>;
  createProject: (data: { displayName: string; description?: string; view?: string }) => Promise<Project>;
  updateProject: (projectId: string, data: Partial<Project>) => Promise<Project>;
  deleteProject: (projectId: string) => Promise<void>;
  setCurrentProject: (project: Project | null) => void;
}

export const useProjectsStore = create<ProjectsState>((set, get) => ({
  projects: [],
  currentProject: null,
  loading: false,
  error: null,
  deletingProjectIds: [],

  fetchProjects: async (view?: string) => {
    set({ loading: true, error: null });
    try {
      const projects = await api.listProjects(view);
      set({ projects, loading: false });
    } catch (error: any) {
      // Network errors are handled by the global error interceptor
      // Only set local error for non-network issues
      if (error.response) {
        set({ error: error.message, loading: false });
      } else {
        // Network error - just stop loading, global banner will show
        set({ loading: false });
      }
    }
  },

  createProject: async (data) => {
    set({ loading: true, error: null });
    try {
      const project = await api.createProject(data);
      set((state) => ({
        projects: [project, ...state.projects],
        loading: false,
      }));
      return project;
    } catch (error: any) {
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message, loading: false });
      } else {
        set({ loading: false });
      }
      throw error;
    }
  },

  updateProject: async (projectId, data) => {
    set({ loading: true, error: null });
    try {
      const updatedProject = await api.updateProject(projectId, data);
      set((state) => ({
        projects: state.projects.map((p) => (p.id === projectId ? updatedProject : p)),
        currentProject: state.currentProject?.id === projectId ? updatedProject : state.currentProject,
        loading: false,
      }));
      return updatedProject;
    } catch (error: any) {
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message, loading: false });
      } else {
        set({ loading: false });
      }
      throw error;
    }
  },

  deleteProject: async (projectId) => {
    // Mark project as being deleted for visual feedback
    set((state) => ({
      deletingProjectIds: [...state.deletingProjectIds, projectId],
      loading: true,
      error: null,
    }));
    
    try {
      await api.deleteProject(projectId);
      set((state) => ({
        projects: state.projects.filter((p) => p.id !== projectId),
        currentProject: state.currentProject?.id === projectId ? null : state.currentProject,
        loading: false,
        deletingProjectIds: state.deletingProjectIds.filter((id) => id !== projectId),
      }));
    } catch (error: any) {
      // Remove from deleting array on error too
      set((state) => {
        const newDeletingIds = state.deletingProjectIds.filter((id) => id !== projectId);
        // Network errors are handled by the global error interceptor
        if (error.response) {
          return { error: error.message, loading: false, deletingProjectIds: newDeletingIds };
        } else {
          return { loading: false, deletingProjectIds: newDeletingIds };
        }
      });
      throw error;
    }
  },

  setCurrentProject: (project) => {
    console.log('[ProjectsStore] setCurrentProject called', { 
      projectId: project?.id, 
      projectName: project?.displayName 
    });
    set({ currentProject: project });
  },
}));

