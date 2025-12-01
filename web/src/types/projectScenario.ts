/**
 * Project Scenario types
 * Represents scenarios within a project that can be organized hierarchically
 */

export interface ProjectScenario {
  id: string;
  tenantId: string;
  projectId: string;
  parentId?: string | null; // Optional parent scenario for hierarchy
  
  title: string;
  description?: string | null;
  status: string; // active, draft, archived
  
  // Order within parent (for sorting)
  order: number;
  
  // Metadata
  tags?: string[];
  labels?: Record<string, string>; // lang -> string
  
  // Timestamps
  createdAt: string; // ISO 8601 timestamp
  updatedAt: string; // ISO 8601 timestamp
  createdBy: string; // User ID
  updatedBy: string; // User ID
}

export interface CreateProjectScenarioRequest {
  title: string;
  description?: string;
  parentId?: string | null;
  status?: string;
  order?: number;
  tags?: string[];
  labels?: Record<string, string>;
}

export interface UpdateProjectScenarioRequest {
  title?: string;
  description?: string | null;
  parentId?: string | null;
  status?: string;
  order?: number;
  tags?: string[];
  labels?: Record<string, string>;
}

