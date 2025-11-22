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
    // Optimistic update: update locally first
    const state = get();
    const project = state.projects.find((p) => p.id === projectId);
    const optimisticProject = project ? { ...project, ...data } : null;
    
    if (optimisticProject) {
      set((state) => ({
        projects: state.projects.map((p) => (p.id === projectId ? optimisticProject as Project : p)),
        currentProject: state.currentProject?.id === projectId ? optimisticProject as Project : state.currentProject,
      }));
    }
    
    // Then sync with server in background
    try {
      const updatedProject = await api.updateProject(projectId, data);
      set((state) => ({
        projects: state.projects.map((p) => (p.id === projectId ? updatedProject : p)),
        currentProject: state.currentProject?.id === projectId ? updatedProject : state.currentProject,
      }));
      return updatedProject;
    } catch (error: any) {
      // Rollback on error
      if (project) {
        set((state) => ({
          projects: state.projects.map((p) => (p.id === projectId ? project : p)),
          currentProject: state.currentProject?.id === projectId ? project : state.currentProject,
        }));
      }
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message });
      }
      throw error;
    }
  },

  deleteProject: async (projectId) => {
    // Optimistic update: remove from list immediately
    const state = get();
    const projectToDelete = state.projects.find((p) => p.id === projectId);
    const wasCurrentProject = state.currentProject?.id === projectId;
    const previousCurrentProject = state.currentProject;
    
    // Mark project as being deleted for visual feedback
    set((state) => ({
      deletingProjectIds: [...state.deletingProjectIds, projectId],
      error: null,
    }));
    
    // Remove from list optimistically
    set((state) => ({
      projects: state.projects.filter((p) => p.id !== projectId),
      currentProject: wasCurrentProject ? null : state.currentProject,
    }));
    
    // Then sync with server in background
    try {
      await api.deleteProject(projectId);
      set((state) => ({
        deletingProjectIds: state.deletingProjectIds.filter((id) => id !== projectId),
      }));
    } catch (error: any) {
      // Rollback on error: restore project to list and currentProject if needed
      if (projectToDelete) {
        set((state) => ({
          projects: [...state.projects, projectToDelete],
          currentProject: wasCurrentProject ? previousCurrentProject : state.currentProject,
          deletingProjectIds: state.deletingProjectIds.filter((id) => id !== projectId),
        }));
      } else {
        set((state) => ({
          currentProject: wasCurrentProject ? previousCurrentProject : state.currentProject,
          deletingProjectIds: state.deletingProjectIds.filter((id) => id !== projectId),
        }));
      }
        // Network errors are handled by the global error interceptor
        if (error.response) {
        set({ error: error.message });
        }
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

