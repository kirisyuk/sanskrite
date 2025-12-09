/**
 * Store for ScenarioData - temporary frontend-only storage
 * Data is stored per project-scenario combination
 */

import { create } from 'zustand';
import type { ScenarioData } from '../types/scenarioData';

interface ScenarioDataState {
  // Map: projectId -> scenarioId -> ScenarioData[]
  data: Map<string, Map<string, ScenarioData[]>>;
  
  // Actions
  addScenarioData: (data: ScenarioData) => void;
  updateScenarioData: (id: string, updater: (data: ScenarioData) => ScenarioData) => void;
  getScenarioData: (projectId: string, scenarioId: string) => ScenarioData[];
  clearScenarioData: (projectId: string, scenarioId: string) => void;
  clearProjectData: (projectId: string) => void;
}

export const useScenarioDataStore = create<ScenarioDataState>((set, get) => ({
  data: new Map(),
  
  addScenarioData: (scenarioData: ScenarioData) => {
    set((state) => {
      const newData = new Map(state.data);
      
      // Get or create project map
      let projectMap = newData.get(scenarioData.projectId);
      if (!projectMap) {
        projectMap = new Map();
        newData.set(scenarioData.projectId, projectMap);
      }
      
      // Get or create scenario array
      let scenarioArray = projectMap.get(scenarioData.scenarioId);
      if (!scenarioArray) {
        scenarioArray = [];
        projectMap.set(scenarioData.scenarioId, scenarioArray);
      }
      
      // Check if data with same ID already exists (for updates)
      const existingIndex = scenarioArray.findIndex(d => d.id === scenarioData.id);
      if (existingIndex >= 0) {
        // Update existing
        scenarioArray[existingIndex] = scenarioData;
      } else {
        // Add new data at the end (newest at bottom)
        scenarioArray = [...scenarioArray, scenarioData];
      }
      projectMap.set(scenarioData.scenarioId, scenarioArray);
      
      return { data: newData };
    });
  },
  
  updateScenarioData: (id: string, updater: (data: ScenarioData) => ScenarioData) => {
    set((state) => {
      const newData = new Map(state.data);
      
      // Find and update the scenario data
      for (const projectMap of newData.values()) {
        for (const scenarioArray of projectMap.values()) {
          const index = scenarioArray.findIndex(d => d.id === id);
          if (index >= 0) {
            scenarioArray[index] = updater(scenarioArray[index]);
            return { data: newData };
          }
        }
      }
      
      return { data: newData };
    });
  },
  
  getScenarioData: (projectId: string, scenarioId: string) => {
    const state = get();
    const projectMap = state.data.get(projectId);
    if (!projectMap) return [];
    
    return projectMap.get(scenarioId) || [];
  },
  
  clearScenarioData: (projectId: string, scenarioId: string) => {
    set((state) => {
      const newData = new Map(state.data);
      const projectMap = newData.get(projectId);
      if (projectMap) {
        projectMap.delete(scenarioId);
        if (projectMap.size === 0) {
          newData.delete(projectId);
        }
      }
      return { data: newData };
    });
  },
  
  clearProjectData: (projectId: string) => {
    set((state) => {
      const newData = new Map(state.data);
      newData.delete(projectId);
      return { data: newData };
    });
  },
}));

