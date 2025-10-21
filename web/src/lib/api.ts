import axios, { AxiosInstance } from 'axios';
import { auth } from './firebase';
import type { User, Project, DataSource } from '../types';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';

class ApiClient {
  private client: AxiosInstance;

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE_URL,
      headers: {
        'Content-Type': 'application/json',
      },
    });

    // Add auth token interceptor
    this.client.interceptors.request.use(async (config) => {
      const user = auth?.currentUser;
      if (user) {
        const token = await user.getIdToken();
        config.headers.Authorization = `Bearer ${token}`;
      }
      return config;
    });
  }

  // Users
  async createUser(data: {
    firebaseUid: string;
    email: string;
    accountType: 'personal' | 'corporate';
    displayName?: string;
  }): Promise<User> {
    const response = await this.client.post<User>('/api/v1/users', data);
    return response.data;
  }

  async getUser(userId: string): Promise<User> {
    const response = await this.client.get<User>(`/api/v1/users/${userId}`);
    return response.data;
  }

  async updateUser(userId: string, data: Partial<User>): Promise<User> {
    const response = await this.client.put<User>(`/api/v1/users/${userId}`, data);
    return response.data;
  }

  // Projects
  async createProject(data: {
    displayName: string;
    description?: string;
    view?: string;
  }): Promise<Project> {
    const response = await this.client.post<Project>('/api/v1/projects', data);
    return response.data;
  }

  async listProjects(view?: string): Promise<Project[]> {
    const params = view ? { view } : {};
    const response = await this.client.get<Project[]>('/api/v1/projects', { params });
    return response.data;
  }

  async getProject(projectId: string): Promise<Project> {
    const response = await this.client.get<Project>(`/api/v1/projects/${projectId}`);
    return response.data;
  }

  async updateProject(projectId: string, data: Partial<Project>): Promise<Project> {
    const response = await this.client.put<Project>(`/api/v1/projects/${projectId}`, data);
    return response.data;
  }

  async deleteProject(projectId: string): Promise<void> {
    const response = await this.client.delete(`/api/v1/projects/${projectId}`);
    return response.data;
  }

  // Discovery
  async executeDiscovery(data: {
    projectId: string;
    queryText: string;
    language?: string;
    sources: {
      web: string[];
      telegram: string[];
    };
    correlationId?: string;
  }): Promise<{ correlationId: string }> {
    console.log('[API] Sending discovery request:', { ...data, queryText: data.queryText.substring(0, 50) + '...' });
    const response = await this.client.post<{ correlationId: string }>('/api/v1/sources-discovery/execute', data);
    console.log('[API] Discovery response:', response.data);
    return response.data;
  }

  async getProjectSources(projectId: string): Promise<DataSource[]> {
    const response = await this.client.get<DataSource[]>(`/api/v1/projects/${projectId}/datasources`);
    return response.data;
  }

  async getProjectInteractions(projectId: string): Promise<{ id: string; kind: string; content: string; createdAt: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; evaluations?: Record<string, any> }[]> {
    const response = await this.client.get<{ items: { id: string; kind: string; content: string; createdAt: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; evaluations?: Record<string, any> }[] }>(`/api/v1/projects/${projectId}/interactions`);
    return response.data.items;
  }

  async createProjectInteraction(projectId: string, data: { kind: 'user'|'assistant'|'system'|'note'; content: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }): Promise<{ id: string }> {
    const response = await this.client.post<{ id: string }>(`/api/v1/projects/${projectId}/interactions`, data);
    return response.data;
  }

  async deleteProjectInteraction(projectId: string, interactionId: string): Promise<{ success: boolean }> {
    const response = await this.client.delete<{ success: boolean }>(`/api/v1/projects/${projectId}/interactions/${interactionId}`);
    return response.data;
  }

  async sendChatMessage(projectId: string, message: string, language?: string): Promise<{ answer: string }> {
    const response = await this.client.post<{ answer: string }>(`/api/v1/projects/${projectId}/chat`, { message, language });
    return response.data;
  }

  // GenAI
  async genaiGenerate(data: {
    model: string;
    prompt: string;
    systemInstruction?: string;
    temperature?: number;
    maxOutputTokens?: number;
    webSearchEnabled?: boolean;
    toolCallingEnabled?: boolean;
    strictJSONEnabled?: boolean;
    jsonSchema?: any;
    tools?: Array<{ name: string; description?: string; parameters: any }>;
    toolChoice?: 'auto'|'required';
    parallelToolCalls?: boolean;
  }): Promise<{ content?: string; model: string; usage?: { promptTokens: number; outputTokens: number; totalTokens: number }; sources?: string[]; attributions?: Array<{ title?: string; url?: string }>; rawJSON?: string; }>
  {
    const body = {
      model: data.model,
      prompt: data.prompt,
      systemInstruction: data.systemInstruction,
      temperature: data.temperature,
      maxOutputTokens: data.maxOutputTokens,
      webSearchEnabled: !!data.webSearchEnabled,
      toolCallingEnabled: !!data.toolCallingEnabled,
      strictJSONEnabled: !!data.strictJSONEnabled,
      jsonSchema: data.jsonSchema,
      tools: data.tools,
      toolChoice: data.toolChoice,
      parallelToolCalls: data.parallelToolCalls,
    };
    const response = await this.client.post(`/api/v1/genai/generate`, body);
    return response.data;
  }
  async genaiAdhocJSON(data: {
    prompt: string;
    options?: {
      profile?: 'fast' | 'quality';
      modelOverride?: string;
      groundingEnabled?: boolean;
      temperature?: number;
      maxOutputTokens?: number;
      systemInstruction?: string;
    };
  }): Promise<{ ok: boolean; model: string; usage: { promptTokens: number; outputTokens: number; totalTokens: number }; validJson: boolean; json: any; raw: string; }> {
    const response = await this.client.post(`/api/v1/genai/adhoc-json`, data);
    return response.data;
  }

  async genaiComplete(data: {
    prompt: string;
    tools?: Array<{ name: string; description?: string; parameters: any }>;
    options?: any;
  }) {
    const response = await this.client.post(`/api/v1/genai/complete`, data);
    return response.data;
  }

  async genaiCompleteJSON(data: {
    prompt: string;
    tools?: Array<{ name: string; description?: string; parameters: any }>;
    jsonSchema?: any;
    options?: any;
  }) {
    const response = await this.client.post(`/api/v1/genai/complete-json`, data);
    return response.data;
  }

  async genaiCompleteWithTools(data: {
    prompt: string;
    tools: Array<{ name: string; description?: string; parameters: any }>;
    options?: any;
  }) {
    const response = await this.client.post(`/api/v1/genai/complete-with-tools`, data);
    return response.data;
  }

  async genaiStream(data: {
    prompt: string;
    tools?: Array<{ name: string; description?: string; parameters: any }>;
    options?: any;
    messages?: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>;
  }) {
    // Fire-and-forget HTTP 200 triggers Pub/Sub/WS stream events
    const response = await this.client.post(`/api/v1/genai/stream`, data);
    return response.data;
  }

  async suggestChatTitles(messages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>, language?: string): Promise<string[]> {
    const transcript = messages.map(m => `- ${m.role}: ${m.text}`).join('\n');
    const lang = (language || (localStorage.getItem('preferredLanguage') || 'EN')).toLowerCase();
    const prompt = `You are naming a chat based on its conversation. Language: ${lang}.\n\nTranscript (most recent last):\n${transcript}\n\nTask: Propose 3 concise, specific, non-generic titles that reflect the supposed user's intent behind the conversation. Each <= 35 characters. Titles must: (1) reflect the user's specific interest in the topic overall based on the user's own requests; (2) present distinct viewpoints/angles on the user's context (not duplicates, different perspectives).\nReturn strictly JSON only, no markdown/code fences, matching this shape: { "titles": ["title1", "title2", "title3"] }.`;
    try {
      const res = await this.client.post<{ ok: boolean; result: any }>(`/api/v1/ai/structured`, {
        prompt,
        minCapabilityScore: 6,
        maxOutputSize: 512,
      });
      const obj = res.data?.result;
      const arr = Array.isArray(obj?.titles) ? obj.titles : [];
      return arr.filter((x: any) => typeof x === 'string');
    } catch {
      return [];
    }
  }
}

export const api = new ApiClient();

