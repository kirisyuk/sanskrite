import axios, { AxiosInstance } from 'axios';
import { auth } from './firebase';
import type { User, Project, DataSource } from '../types';
import type { AnalyzeRequest, AnalyzeResponse } from '../types/stance';

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

    // Add response interceptor to handle errors
    this.client.interceptors.response.use(
      (response) => {
        // On successful response, mark server as available
        import('../stores/serverStatusStore').then(({ useServerStatusStore }) => {
          useServerStatusStore.getState().setServerAvailable();
        }).catch(() => {});
        return response;
      },
      (error) => {
        // Check if it's a network error (CORS, server down, etc.)
        if (!error.response) {
          // Network error - no response from server
          import('../stores/serverStatusStore').then(({ useServerStatusStore }) => {
            useServerStatusStore.getState().setServerUnavailable('Server is temporarily unavailable. Please try again later.');
          }).catch(() => {});
        } else if (error.response.status >= 500) {
          // Server error (5xx)
          import('../stores/serverStatusStore').then(({ useServerStatusStore }) => {
            useServerStatusStore.getState().setServerUnavailable('Server error. Please try again later.');
          }).catch(() => {});
        }
        return Promise.reject(error);
      }
    );
  }

  // Waitlist
  async waitlistJoin(data: { email: string; note?: string }): Promise<{ ok: boolean }> {
    const response = await this.client.post<{ ok: boolean }>(`/api/v1/waitlist`, data);
    return response.data;
  }

  async waitlistCheck(email: string): Promise<{ found: boolean; status?: string }> {
    const response = await this.client.get<{ found: boolean; status?: string }>(`/api/v1/waitlist`, { params: { email } });
    return response.data;
  }

  async waitlistList(status?: 'pending' | 'approved' | 'rejected'): Promise<Array<{ id: string; email: string; status: string; note?: string; invitedBy?: string; createdAt: string; updatedAt: string }>> {
    const params = status ? { status } : {};
    const response = await this.client.get<{ items: Array<{ id: string; email: string; status: string; note?: string; invitedBy?: string; createdAt: string; updatedAt: string }> | null | undefined }>(`/api/v1/waitlist/list`, { params });
    return Array.isArray(response.data?.items) ? response.data.items : [];
  }

  async waitlistApprove(email: string): Promise<{ ok: boolean }> {
    const response = await this.client.post<{ ok: boolean }>(`/api/v1/waitlist/approve`, { email });
    return response.data;
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

  async getProjectInteractions(projectId: string): Promise<{ id: string; kind: string; content: string; createdAt: string; sectionId?: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; evaluations?: Record<string, any>; metadata?: { pinned?: boolean }; originalMessageId?: string; subkind?: string; contentSanitized?: string; relations?: Array<{ type: string; targetId: string }>; language?: string; systemInstructionApplied?: string; generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean }; highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] }; coreDocIds?: string[] }[]> {
    const response = await this.client.get<{ items: { id: string; kind: string; content: string; createdAt: string; sectionId?: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; evaluations?: Record<string, any>; metadata?: { pinned?: boolean }; originalMessageId?: string; subkind?: string; contentSanitized?: string; relations?: Array<{ type: string; targetId: string }>; language?: string; systemInstructionApplied?: string; generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean }; highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] }; coreDocIds?: string[] }[] }>(`/api/v1/projects/${projectId}/interactions`);
    return response.data.items;
  }

  async createProjectInteraction(projectId: string, data: { kind: 'user'|'assistant'|'system'|'note'|'section_start'|'section_end'|'action'; content: string; sectionId?: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; originalMessageId?: string; subkind?: string; contentSanitized?: string; relations?: Array<{ type: string; targetId: string }>; language?: string; systemInstructionApplied?: string; generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean }; highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] }; evaluations?: Record<string, any>; coreDocIds?: string[]; metadata?: { pinned?: boolean } }): Promise<{ id: string }> {
    const response = await this.client.post<{ id: string }>(`/api/v1/projects/${projectId}/interactions`, data);
    return response.data;
  }

  async deleteProjectInteraction(projectId: string, interactionId: string): Promise<{ success: boolean }> {
    const response = await this.client.delete<{ success: boolean }>(`/api/v1/projects/${projectId}/interactions/${interactionId}`);
    return response.data;
  }

  async updateProjectInteraction(
    projectId: string,
    interactionId: string,
    data: Partial<{
      kind: 'user'|'assistant'|'system'|'note'|'section_start'|'section_end'|'action';
      content: string;
      summary?: string;
      model?: string;
      usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number };
      subkind?: string;
      contentSanitized?: string;
      author?: { participantId: string; kind: string; displayName?: string };
      recipients?: string[];
      replyTo?: string;
      agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> };
      evaluations?: Record<string, any>;
      metadata?: { pinned?: boolean; notes?: string[] };
      relations?: Array<{ type: string; targetId: string }>;
      language?: string;
      systemInstructionApplied?: string;
      generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean };
      highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] };
      originalMessageId?: string;
      coreDocIds?: string[];
    }>
  ): Promise<{ success: boolean }> {
    const response = await this.client.patch<{ success: boolean }>(`/api/v1/projects/${projectId}/interactions/${interactionId}`, data);
    return response.data;
  }

  async sendChatMessage(projectId: string, message: string, language?: string): Promise<{ answer: string }> {
    const response = await this.client.post<{ answer: string }>(`/api/v1/projects/${projectId}/chat`, { message, language });
    return response.data;
  }

  // --- Meta analysis ---
  async metaAnalyze(projectId: string, data: AnalyzeRequest): Promise<AnalyzeResponse> {
    const res = await this.client.post<AnalyzeResponse>(`/api/v1/projects/${projectId}/meta:analyze`, data);
    return res.data;
  }

  async metaListEvents(projectId: string): Promise<any[]> {
    const res = await this.client.get<any[]>(`/api/v1/projects/${projectId}/meta/events`);
    return res.data as any[];
  }

  async metaGetAggregate(projectId: string): Promise<any> {
    const res = await this.client.get<any>(`/api/v1/projects/${projectId}/meta/aggregate`);
    return res.data;
  }

  // GenAI
  async aiStructured(params: { prompt: string; minCapabilityScore?: number; maxOutputSize?: number }): Promise<{ ok: boolean; result: any }>{
    const res = await this.client.post<{ ok: boolean; result: any }>(`/api/v1/ai/structured`, {
      prompt: params.prompt,
      minCapabilityScore: params.minCapabilityScore ?? 8,
      maxOutputSize: params.maxOutputSize ?? 8192,
    });
    return res.data;
  }

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

  // --- Analysis: Entities ---
  async analyzeEntities(projectId: string, prompt?: string): Promise<{ queued: boolean }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/analyze/entities`, { prompt: prompt || '' });
    return res.data as any;
  }

  // --- Analysis: Facts/Events/Metrics/Decisions ---
  async analyzeFacts(projectId: string, prompt?: string): Promise<{ queued: boolean }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/analyze/facts`, { prompt: prompt || '' });
    return res.data as any;
  }
  async analyzeEvents(projectId: string, prompt?: string): Promise<{ queued: boolean }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/analyze/events`, { prompt: prompt || '' });
    return res.data as any;
  }
  async analyzeMetrics(projectId: string, prompt?: string): Promise<{ queued: boolean }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/analyze/metrics`, { prompt: prompt || '' });
    return res.data as any;
  }
  async analyzeDecisions(projectId: string, prompt?: string): Promise<{ queued: boolean }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/analyze/decisions`, { prompt: prompt || '' });
    return res.data as any;
  }

  async genaiStream(data: {
    prompt: string;
    tools?: Array<{ name: string; description?: string; parameters: any }>;
    options?: any;
    messages?: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>;
    projectId?: string;
    streamId?: string;
  }) {
    // Fire-and-forget HTTP 200 triggers Pub/Sub/WS stream events
    const response = await this.client.post(`/api/v1/genai/stream`, data);
    return response.data;
  }

  // --- Core Docs (project attachments) ---
  async docsList(projectId: string) {
    const res = await this.client.get(`/api/v1/projects/${projectId}/docs`);
    return res.data as any[];
  }

  async docsCreateFromText(projectId: string, data: { filename: string; contentType: string; text: string; description?: string }) {
    const res = await this.client.post(`/api/v1/projects/${projectId}/docs/from-text`, data);
    return res.data as any;
  }

  async docsCreate(projectId: string, data: { filename: string; contentType: string; description?: string; extractedText?: string }) {
    const res = await this.client.post(`/api/v1/projects/${projectId}/docs`, data);
    return res.data as any;
  }

  async docsUpload(projectId: string, file: File, description?: string) {
    const form = new FormData();
    form.append('file', file);
    if (description) form.append('description', description);
    const res = await this.client.post(`/api/v1/projects/${projectId}/docs/upload`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    return res.data as any;
  }

  async docsParse(projectId: string, docId: string) {
    const res = await this.client.post(`/api/v1/projects/${projectId}/docs/${docId}/parse`);
    return res.data as any;
  }

  async docsDelete(projectId: string, docId: string) {
    const res = await this.client.delete(`/api/v1/projects/${projectId}/docs/${docId}`);
    return res.data as any;
  }

  async docsDownload(projectId: string, docId: string): Promise<Blob> {
    try {
      const res = await this.client.get(`/api/v1/projects/${projectId}/docs/${docId}/download`, { responseType: 'blob' });
      return res.data as Blob;
    } catch (e: any) {
      // Fallback: if server returns 400 NO_STORAGE, fetch doc and synthesize blob from extractedText
      const status = e?.response?.status;
      if (status === 400) {
        try {
          const meta = await this.client.get(`/api/v1/projects/${projectId}/docs`);
          const arr = (meta.data as any[]) || [];
          const found = arr.find((d: any) => d && (d.id === docId));
          if (found && typeof found.extractedText === 'string') {
            const ct = typeof found.contentType === 'string' && found.contentType ? found.contentType : 'application/octet-stream';
            return new Blob([found.extractedText], { type: ct });
          }
        } catch {}
      }
      throw e;
    }
  }

  async suggestChatTitles(messages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>, language?: string): Promise<string[]> {
    const transcript = messages.map(m => `- ${m.role}: ${m.text}`).join('\n');
    const lang = (language || (localStorage.getItem('preferredLanguage') || 'EN')).toLowerCase();
    const prompt = `User requirement: you should create your answer in ${lang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are naming a chat based on its conversation.\n\nTranscript (most recent last):\n${transcript}\n\nTask: Propose 3 concise, specific, non-generic titles that reflect the supposed user's intent behind the conversation. Each <= 35 characters. Titles must: (1) reflect the user's specific interest in the topic overall based on the user's own requests; (2) present distinct viewpoints/angles on the user's context (not duplicates, different perspectives).\nReturn strictly JSON only, no markdown/code fences, matching this shape: { "titles": ["title1", "title2", "title3"] }.`;
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

  // --- Artifact Templates ---
  async listArtifactTemplates(category?: string): Promise<any[]> {
    const params = category ? { category } : {};
    const res = await this.client.get(`/api/v1/artifact-templates`, { params });
    return res.data as any[];
  }

  async getArtifactTemplate(templateId: string): Promise<any> {
    const res = await this.client.get(`/api/v1/artifact-templates/${templateId}`);
    return res.data;
  }

  async createArtifactTemplate(template: any): Promise<{ id: string }> {
    const res = await this.client.post(`/api/v1/artifact-templates`, template);
    return res.data;
  }

  async updateArtifactTemplate(templateId: string, template: any): Promise<{ success: boolean }> {
    const res = await this.client.put(`/api/v1/artifact-templates/${templateId}`, template);
    return res.data;
  }

  async deleteArtifactTemplate(templateId: string): Promise<{ success: boolean }> {
    const res = await this.client.delete(`/api/v1/artifact-templates/${templateId}`);
    return res.data;
  }

  async publishArtifactTemplate(templateId: string): Promise<{ success: boolean }> {
    const res = await this.client.post(`/api/v1/artifact-templates/${templateId}/publish`);
    return res.data;
  }

  async generateArtifactStructure(requirements: {
    documentType: string;
    topic: string;
    audience: string;
    detailLevel: string;
    language: string;
  }): Promise<{ sections: any[] }> {
    const res = await this.client.post(`/api/v1/artifact-templates/generate-structure`, requirements);
    return res.data;
  }

  async createTemplateFromArtifact(request: {
    artifactId: string;
    templateName: string;
    description: string;
    category: string;
    includeExamples?: boolean;
    includePromptHistory?: boolean;
    includeSettings?: boolean;
    visibility?: 'private' | 'team' | 'public';
    sectionCustomizations?: Record<string, any>;
  }): Promise<{ id: string }> {
    const res = await this.client.post(`/api/v1/artifact-templates/from-artifact`, request);
    return res.data;
  }

  // --- Artifact Generation ---
  async startArtifactGeneration(artifactId: string, mode: 'auto' | 'interactive', projectId: string): Promise<any> {
    const res = await this.client.post(`/api/v1/artifacts/${artifactId}/generate`, { mode, projectId });
    return res.data;
  }

  async generateArtifactSection(
    artifactId: string,
    sectionId: string,
    sessionId: string,
    projectId: string,
    promptOverride?: string,
    modelOverride?: string
  ): Promise<any> {
    const res = await this.client.post(`/api/v1/artifacts/${artifactId}/sections/${sectionId}/generate`, {
      sessionId,
      projectId,
      promptOverride,
      modelOverride,
    });
    return res.data;
  }

  async regenerateArtifactSection(
    artifactId: string,
    sectionId: string,
    sessionId: string,
    projectId: string,
    newPrompt: string,
    model?: string
  ): Promise<any> {
    const res = await this.client.post(`/api/v1/artifacts/${artifactId}/sections/${sectionId}/regenerate`, {
      sessionId,
      projectId,
      newPrompt,
      model,
    });
    return res.data;
  }

  async assembleArtifactDocument(artifactId: string, sessionId: string): Promise<{ content: string }> {
    const res = await this.client.post(`/api/v1/artifacts/${artifactId}/assemble`, { sessionId });
    return res.data;
  }

  async saveArtifactVersion(artifactId: string, sessionId: string, trigger: 'manual' | 'auto' | 'scheduled' = 'manual'): Promise<any> {
    const res = await this.client.post(`/api/v1/artifacts/${artifactId}/versions`, { sessionId, trigger });
    return res.data;
  }

  async restoreArtifactVersion(artifactId: string, versionId: string): Promise<{ success: boolean }> {
    const res = await this.client.post(`/api/v1/artifacts/${artifactId}/versions/${versionId}/restore`);
    return res.data;
  }

  async getGenerationSession(sessionId: string): Promise<any> {
    const res = await this.client.get(`/api/v1/generation-sessions/${sessionId}`);
    return res.data;
  }

  async cancelGeneration(sessionId: string): Promise<{ success: boolean }> {
    const res = await this.client.post(`/api/v1/generation-sessions/${sessionId}/cancel`);
    return res.data;
  }
}

export const api = new ApiClient();

